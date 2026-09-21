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

// databaseNow reads the transaction's clock.
//
// Retry schedules are derived from it rather than from w.now() because
// outbox_events enforces `available_at >= occurred_at`, and occurred_at is
// written by the database. A worker whose clock runs behind would compute an
// available_at in the database's past and fail that constraint, aborting the
// whole recovery transaction — so a clock-skewed worker would not merely
// schedule badly, it would stop recovering work at all. One clock for the whole
// decision removes that failure mode.
func databaseNow(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var at time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("reading database time: %w", err)
	}
	return at, nil
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
	base, err := databaseNow(ctx, tx)
	if err != nil {
		return err
	}
	availableAt := base.Add(retryBackoff(attempts))
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
	// Staleness is judged by the database clock, never by w.now(). A worker whose
	// clock runs fast would otherwise select and recover leases the database
	// still considers live, tearing down healthy work; one running slow would
	// leave genuinely expired leases unrecovered.
	//
	// SCANNING and PROCESSING keep the unleased arm of the predicate. For them a
	// NULL lease is genuinely recoverable work: a row that entered either state
	// before 0035 introduced the lease columns is in flight with nothing left to
	// expire, and leaving it unrecovered would strand it forever.
	//
	// PLAYABLE is the opposite case, because it is the one in-flight state that
	// an asset stays in after its work ends. A PLAYABLE row holding no claim is
	// not abandoned work: it is the terminal, already-recorded shape of a failed
	// enhancement — its attempt was made terminal and its claim cleared by
	// `markProcessingFailed` or by `recoverStalePlayable` itself. Recovering it
	// again has nothing to terminalize, so it would mint a fresh
	// `recovery:<uuid>` operation identity, slip past the
	// `ON CONFLICT DO NOTHING` that only deduplicates a repeated identity, and
	// append another FAILED `processing_attempts` row on every tick, without
	// bound. PLAYABLE therefore requires positive evidence of a claimed
	// operation that has since expired. Scheduling enhancement retry for those
	// terminal rows is deliberately not done here; it is Phase 3C's separate
	// concern.
	rows, err := w.db.Query(ctx, `
		SELECT id::text
		FROM media_asset_versions
		WHERE (
		        state IN ('SCANNING', 'PROCESSING')
		        AND (work_lease_expires_at IS NULL OR work_lease_expires_at <= now())
		      )
		   OR (
		        state = 'PLAYABLE'
		        AND work_claim_token IS NOT NULL
		        AND work_lease_expires_at IS NOT NULL
		        AND work_lease_expires_at <= now()
		      )
		ORDER BY COALESCE(work_lease_expires_at, created_at), id
		LIMIT $1
	`, limit)
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
	// leaseStillLive is computed by PostgreSQL inside this transaction, against
	// the row this statement has just locked. Reading the timestamp out and
	// comparing it in Go would reintroduce the worker clock as the authority for
	// a decision that tears down another worker's claim.
	var leaseStillLive bool
	err = tx.QueryRow(ctx, `
		SELECT id::text, kind, state, work_claim_token,
		       COALESCE(work_lease_expires_at > now(), false),
		       scan_attempt_count, processing_attempt_count,
		       successful_validation_attempt_id IS NOT NULL
		FROM media_asset_versions WHERE id=$1::uuid FOR UPDATE
	`, assetVersionID).Scan(&work.id, &work.kind, &work.state, &work.token, &leaseStillLive,
		&work.scanAttempts, &work.processingAttempts, &work.hasValidationEvidence)
	if err != nil {
		return false, fmt.Errorf("locking stale media work: %w", err)
	}
	if work.state != StateScanning && work.state != StateProcessing && work.state != StatePlayable {
		return false, nil
	}
	if leaseStillLive {
		return false, nil
	}
	// The same rule the selection applies, re-asserted under the row lock. The
	// selection ran in an earlier snapshot, so a PLAYABLE row that held a claim
	// when it was chosen may have had that claim cleared — and its attempt made
	// terminal — by its own worker's failure path between the two. Without this
	// the recovered-again row would be given a fresh recovery operation identity
	// and append a second, invented FAILED attempt for work that already
	// recorded its own outcome.
	if work.state == StatePlayable && work.token == nil {
		return false, nil
	}
	if work.state == StateScanning {
		if err := w.recoverStaleScan(ctx, tx, work); err != nil {
			return false, err
		}
	} else if work.state == StateProcessing {
		if err := w.recoverStaleProcessing(ctx, tx, work); err != nil {
			return false, err
		}
	} else if work.state == StatePlayable {
		if err := w.recoverStalePlayable(ctx, tx, work); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("committing stale media recovery: %w", err)
	}
	if (work.state == StateProcessing || work.state == StatePlayable) && work.token != nil {
		// Data correctness no longer depends on this prefix: the recovered
		// attempt has a new identity. Prefer a bounded leak over undoing the
		// committed recovery when object deletion is unavailable. Only delete
		// if PostgreSQL proves zero durable video_renditions reference the prefix.
		_, _ = w.CleanupAttemptSafe(ctx, work.id, *work.token)
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
	base, err := databaseNow(ctx, tx)
	if err != nil {
		return err
	}
	availableAt := base.Add(retryBackoff(attempt))
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
	// processing_attempt_count is the CURRENT CYCLE's consumed-attempt count.
	//
	// INVARIANT: a processing retry cycle receives exactly MaxWorkAttempts
	// attempts. Historical rows in processing_attempts belonging to earlier
	// cycles remain auditable but never consume the current cycle's budget.
	//
	// An earlier revision reconciled this counter against
	// `count(*) FROM processing_attempts`, which is append-only and therefore
	// lifetime-global. Admin Retry deliberately resets the counter to zero to
	// open a fresh cycle (see Service.applyRetry), so counting history made a
	// freshly retried asset inherit every attempt of every previous cycle and be
	// exhausted before its first new attempt ran.
	//
	// The only case the counter genuinely understates is a row that entered
	// PROCESSING before 0035: the claim that put it there could not increment a
	// column that did not exist yet, so its in-flight attempt is uncounted. That
	// case is identified precisely — a PROCESSING row holding no claim token —
	// and charged exactly one attempt. A row claimed under 0035 always carries
	// its token and was already counted at claim time, so it is never
	// double-charged, and no historical row is ever consulted.
	var attemptsConsumed int
	if err := tx.QueryRow(ctx, `
		UPDATE media_asset_versions SET state='PROCESS_FAILED',
		  processing_stage=NULL, processing_progress_percent=NULL, processing_updated_at=NULL, processing_attempt_token=NULL,
		  work_claim_token=NULL, work_claimed_at=NULL, work_lease_expires_at=NULL,
		  last_failure_category='WORKER_INTERRUPTED',
		  processing_attempt_count=processing_attempt_count
		    + CASE WHEN work_claim_token IS NULL THEN 1 ELSE 0 END
		WHERE id=$1::uuid AND state='PROCESSING'
		RETURNING processing_attempt_count
	`, work.id).Scan(&attemptsConsumed); err != nil {
		return fmt.Errorf("failing interrupted processing: %w", err)
	}
	if attemptsConsumed >= MaxWorkAttempts {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state='QUARANTINED' WHERE id=$1::uuid AND state='PROCESS_FAILED'`, work.id); err != nil {
		return fmt.Errorf("resetting interrupted processing: %w", err)
	}
	base, err := databaseNow(ctx, tx)
	if err != nil {
		return err
	}
	availableAt := base.Add(retryBackoff(attemptsConsumed))
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

func (w *Worker) recoverStalePlayable(ctx context.Context, tx pgx.Tx, work staleWork) error {
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
		UPDATE media_asset_versions SET
		  work_claim_token=NULL, work_claimed_at=NULL, work_lease_expires_at=NULL,
		  last_failure_category='WORKER_INTERRUPTED'
		WHERE id=$1::uuid AND state='PLAYABLE'
	`, work.id); err != nil {
		return fmt.Errorf("clearing claim for interrupted playable: %w", err)
	}
	return nil
}
