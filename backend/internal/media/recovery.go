package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type failureCategory string

const (
	failureInvalidMedia       failureCategory = "INVALID_MEDIA"
	failureStorageUnavailable failureCategory = "STORAGE_UNAVAILABLE"
	failureProcessTimeout     failureCategory = "PROCESS_TIMEOUT"
	failureTranscode          failureCategory = "TRANSCODE_FAILED"
	failureScanRejected       failureCategory = "SCAN_REJECTED"
	failureScanUnavailable    failureCategory = "SCAN_UNAVAILABLE"
	failureWorkerInterrupted  failureCategory = "WORKER_INTERRUPTED"
)

func processingFailureCategory(err error) failureCategory {
	switch {
	case errors.Is(err, ErrStorageUnavailable):
		return failureStorageUnavailable
	case errors.Is(err, ErrProcessTimeout), errors.Is(err, context.DeadlineExceeded):
		return failureProcessTimeout
	case errors.Is(err, ErrInvalidMedia):
		return failureInvalidMedia
	default:
		return failureTranscode
	}
}

func retryBackoff(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 5 * time.Second
	case attempt == 2:
		return 30 * time.Second
	default:
		return 2 * time.Minute
	}
}

func (w *Worker) scheduleScanRetry(ctx context.Context, assetVersionID string) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning scan retry: %w", err)
	}
	defer tx.Rollback(ctx)
	var state AssetVersionState
	var kind AssetKind
	var attempts int
	if err := tx.QueryRow(ctx, `
		SELECT state, kind, scan_attempt_count
		FROM media_asset_versions WHERE id=$1::uuid FOR UPDATE
	`, assetVersionID).Scan(&state, &kind, &attempts); err != nil {
		return fmt.Errorf("loading scan retry target: %w", err)
	}
	if state != StateScanError || attempts >= MaxWorkAttempts {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE media_asset_versions SET state='QUARANTINED'
		WHERE id=$1::uuid AND state='SCAN_ERROR'
	`, assetVersionID); err != nil {
		return fmt.Errorf("resetting transient scan failure: %w", err)
	}
	availableAt := w.now().UTC().Add(retryBackoff(attempts))
	if err := appendScanWorkAt(ctx, tx, w.outbox, workSchedule{
		assetVersionID: assetVersionID, kind: kind, correlation: "automatic-scan-retry", availableAt: &availableAt,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing scan retry: %w", err)
	}
	return fmt.Errorf("%w after %s", ErrRetryScheduled, retryBackoff(attempts))
}

type staleWork struct {
	id                    string
	kind                  AssetKind
	state                 AssetVersionState
	token                 *string
	scanAttempts          int
	processingAttempts    int
	hasValidationEvidence bool
}

// RecoverStale converts expired or legacy unleased in-flight rows into a
// deterministic failure and, while the bounded attempt budget remains,
// appends the next attempt to the existing PostgreSQL outbox. Each row is
// locked and recovered atomically; concurrent recovery loops converge.
func (w *Worker) RecoverStale(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("media recovery limit must be positive")
	}
	rows, err := w.db.Query(ctx, `
		SELECT id::text
		FROM media_asset_versions
		WHERE state IN ('SCANNING', 'PROCESSING')
		  AND (work_lease_expires_at IS NULL OR work_lease_expires_at <= $1)
		ORDER BY COALESCE(work_lease_expires_at, created_at), id
		LIMIT $2
	`, w.now().UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("loading stale media work: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("reading stale media work: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterating stale media work: %w", err)
	}
	recovered := 0
	for _, id := range ids {
		applied, err := w.recoverOne(ctx, id)
		if err != nil {
			return recovered, fmt.Errorf("recovering stale media work %s: %w", id, err)
		}
		if applied {
			recovered++
		}
	}
	return recovered, nil
}

func (w *Worker) recoverOne(ctx context.Context, assetVersionID string) (bool, error) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("beginning stale media recovery: %w", err)
	}
	defer tx.Rollback(ctx)
	var work staleWork
	var leaseExpiresAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT id::text, kind, state, work_claim_token, work_lease_expires_at,
		       scan_attempt_count, processing_attempt_count,
		       successful_validation_attempt_id IS NOT NULL
		FROM media_asset_versions WHERE id=$1::uuid FOR UPDATE
	`, assetVersionID).Scan(&work.id, &work.kind, &work.state, &work.token, &leaseExpiresAt,
		&work.scanAttempts, &work.processingAttempts, &work.hasValidationEvidence)
	if err != nil {
		return false, fmt.Errorf("locking stale media work: %w", err)
	}
	if work.state != StateScanning && work.state != StateProcessing {
		return false, nil
	}
	if leaseExpiresAt != nil && leaseExpiresAt.After(w.now().UTC()) {
		return false, nil
	}
	if work.state == StateScanning {
		if err := w.recoverStaleScan(ctx, tx, work); err != nil {
			return false, err
		}
	} else if err := w.recoverStaleProcessing(ctx, tx, work); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("committing stale media recovery: %w", err)
	}
	if work.state == StateProcessing && work.token != nil {
		if cleaner, ok := w.process.(interface {
			CleanupAttempt(context.Context, string, string) error
		}); ok {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			// Data correctness no longer depends on this prefix: the recovered
			// attempt has a new identity. Prefer a bounded leak over undoing the
			// committed recovery when object deletion is unavailable.
			_ = cleaner.CleanupAttempt(cleanupCtx, work.id, *work.token)
		}
	}
	return true, nil
}

func (w *Worker) recoverStaleScan(ctx context.Context, tx pgx.Tx, work staleWork) error {
	attempt := work.scanAttempts
	if attempt < 1 {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(attempt_number),0)+1 FROM scan_attempts WHERE asset_version_id=$1::uuid`, work.id).Scan(&attempt); err != nil {
			return fmt.Errorf("allocating interrupted scan attempt: %w", err)
		}
	}
	workID := "recovery:" + uuid.NewString()
	if work.token != nil {
		workID = *work.token
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO scan_attempts (asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity, reason)
		SELECT id, $2, $3, storage_object_version, 'ERROR', 'gradex-worker-recovery', 'worker lease expired before scan completion'
		FROM media_asset_versions WHERE id=$1::uuid
		ON CONFLICT DO NOTHING
	`, work.id, attempt, workID); err != nil {
		return fmt.Errorf("recording interrupted scan attempt: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE media_asset_versions SET state='SCAN_ERROR', scan_attempt_count=GREATEST(scan_attempt_count,$2),
		  work_claim_token=NULL, work_claimed_at=NULL, work_lease_expires_at=NULL,
		  last_failure_category='WORKER_INTERRUPTED'
		WHERE id=$1::uuid AND state='SCANNING'
	`, work.id, attempt); err != nil {
		return fmt.Errorf("failing interrupted scan: %w", err)
	}
	if attempt >= MaxWorkAttempts {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state='QUARANTINED' WHERE id=$1::uuid AND state='SCAN_ERROR'`, work.id); err != nil {
		return fmt.Errorf("resetting interrupted scan: %w", err)
	}
	availableAt := w.now().UTC().Add(retryBackoff(attempt))
	return appendScanWorkAt(ctx, tx, w.outbox, workSchedule{
		assetVersionID: work.id, kind: work.kind, correlation: "stale-scan-recovery", availableAt: &availableAt,
	})
}

func (w *Worker) recoverStaleProcessing(ctx context.Context, tx pgx.Tx, work staleWork) error {
	operationID := "recovery:" + uuid.NewString()
	if work.token != nil {
		operationID = *work.token
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, rendition_count, error_reason)
		VALUES ($1::uuid,$2,'FAILED',0,'worker lease expired before processing completion')
		ON CONFLICT (asset_version_id, operation_id) DO NOTHING
	`, work.id, operationID); err != nil {
		return fmt.Errorf("recording interrupted processing attempt: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE media_asset_versions SET state='PROCESS_FAILED',
		  processing_stage=NULL, processing_progress_percent=NULL, processing_updated_at=NULL, processing_attempt_token=NULL,
		  work_claim_token=NULL, work_claimed_at=NULL, work_lease_expires_at=NULL,
		  last_failure_category='WORKER_INTERRUPTED'
		WHERE id=$1::uuid AND state='PROCESSING'
	`, work.id); err != nil {
		return fmt.Errorf("failing interrupted processing: %w", err)
	}
	if work.processingAttempts >= MaxWorkAttempts {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state='QUARANTINED' WHERE id=$1::uuid AND state='PROCESS_FAILED'`, work.id); err != nil {
		return fmt.Errorf("resetting interrupted processing: %w", err)
	}
	availableAt := w.now().UTC().Add(retryBackoff(work.processingAttempts))
	if work.hasValidationEvidence {
		if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state='VALIDATED' WHERE id=$1::uuid AND state='QUARANTINED'`, work.id); err != nil {
			return fmt.Errorf("restoring immutable validation provenance: %w", err)
		}
		return appendTranscodeWorkAt(ctx, tx, w.outbox, workSchedule{
			assetVersionID: work.id, correlation: "stale-processing-recovery", availableAt: &availableAt,
		})
	}
	return appendScanWorkAt(ctx, tx, w.outbox, workSchedule{
		assetVersionID: work.id, kind: work.kind, correlation: "stale-processing-rescan", availableAt: &availableAt,
	})
}
