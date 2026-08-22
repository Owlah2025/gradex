package media

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/queue"
)

// Worker owns media scan and processing callbacks. It consumes only stable
// operation payloads emitted by a committed PostgreSQL transaction.
type Worker struct {
	db                *pgxpool.Pool
	scanner           *ScannerAdapter
	process           Processor
	outbox            *outbox.Writer
	processingTimeout time.Duration
	transcodeGate     *concurrencyGate
}

type WorkerOptions struct {
	DB                   *pgxpool.Pool
	Scanner              *ScannerAdapter
	Process              Processor
	Outbox               *outbox.Writer
	ProcessingTimeout    time.Duration
	TranscodeConcurrency int
}

func NewWorker(options WorkerOptions) (*Worker, error) {
	if options.DB == nil {
		return nil, errors.New("media worker database is required")
	}
	if options.Scanner == nil {
		return nil, ErrScannerRequired
	}
	if options.Process == nil {
		return nil, errors.New("media processor is required")
	}
	if options.Outbox == nil {
		return nil, errors.New("media worker outbox writer is required")
	}
	timeout := options.ProcessingTimeout
	if timeout == 0 {
		timeout = DefaultProcessingTimeout
	}
	if timeout <= 0 {
		return nil, errors.New("media processing timeout must be positive")
	}
	concurrency := options.TranscodeConcurrency
	if concurrency == 0 {
		concurrency = 2
	}
	if concurrency < 0 {
		return nil, errors.New("media transcode concurrency must be positive")
	}
	return &Worker{
		db: options.DB, scanner: options.Scanner, process: options.Process, outbox: options.Outbox,
		processingTimeout: timeout, transcodeGate: newConcurrencyGate(concurrency),
	}, nil
}

type concurrencyGate struct{ slots chan struct{} }

func newConcurrencyGate(limit int) *concurrencyGate {
	return &concurrencyGate{slots: make(chan struct{}, limit)}
}

func (g *concurrencyGate) run(ctx context.Context, work func() error) error {
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
		return work()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) Register(mux *asynq.ServeMux) error {
	if mux == nil {
		return errors.New("media worker mux is required")
	}
	mux.HandleFunc(queue.TypeMediaScan, w.handleScanTask)
	mux.HandleFunc(queue.TypeMediaTranscode, w.handleTranscodeTask)
	return nil
}

func (w *Worker) handleScanTask(ctx context.Context, task *asynq.Task) error {
	var work ScanWork
	if err := json.Unmarshal(task.Payload(), &work); err != nil {
		return fmt.Errorf("decoding media scan work: %w", err)
	}
	if strings.TrimSpace(work.AssetVersionID) == "" || strings.TrimSpace(work.ScanWorkID) == "" {
		return fmt.Errorf("%w: media scan work identity is required", ErrValidation)
	}
	return w.scan(ctx, work.AssetVersionID, work.ScanWorkID)
}

func (w *Worker) handleTranscodeTask(ctx context.Context, task *asynq.Task) error {
	var work TranscodeWork
	if err := json.Unmarshal(task.Payload(), &work); err != nil {
		return fmt.Errorf("decoding media transcode work: %w", err)
	}
	processingCtx, cancel := context.WithTimeout(ctx, w.processingTimeout)
	defer cancel()
	return w.Transcode(processingCtx, work.AssetVersionID, work.OperationID)
}

// Scan transitions QUARANTINED -> SCANNING under a database CAS, scans the
// exact object identity, and records immutable attempt evidence. A passed
// video schedules transcode through a committed outbox event; it never pushes
// a Redis task from the state-changing transaction.
func (w *Worker) Scan(ctx context.Context, assetVersionID string) error {
	return w.scan(ctx, assetVersionID, uuid.NewString())
}

// scan is the durable scan-work boundary. Production tasks carry the committed
// outbox event ID; the exported Scan convenience method is retained for
// in-process callers and creates one distinct work identity.
func (w *Worker) scan(ctx context.Context, assetVersionID, scanWorkID string) error {
	version, attempt, applied, err := w.beginScan(ctx, assetVersionID, scanWorkID)
	if err != nil || !applied {
		return err
	}

	observation, scanErr := w.scanner.Scan(ctx, version.Object)
	if scanErr != nil && observation.Outcome == "" {
		observation = ScanObservation{
			AssetVersionID:       version.Object.AssetVersionID,
			StorageObjectVersion: version.Object.StorageObjectVersion,
			Outcome:              ScanError,
			ScannerIdentity:      "media-adapter",
			Reason:               scanErr.Error(),
		}
	}
	if observation.ScannerIdentity == "" {
		observation.ScannerIdentity = "media-adapter"
	}
	if observation.Reason == "" && scanErr != nil {
		observation.Reason = scanErr.Error()
	}

	next, applyErr := ApplyScanObservation(StateScanning, version.Object, observation)
	if applyErr != nil {
		// A stale callback is diagnosable but cannot advance the replacement.
		return w.recordScanFailure(ctx, version, attempt, scanWorkID, ScanError, observation.ScannerIdentity, applyErr.Error())
	}
	if err := w.finishScan(ctx, version, attempt, scanWorkID, observation, next); err != nil {
		return err
	}
	if scanErr != nil {
		return scanErr
	}
	return nil
}

type versionRecord struct {
	ID      string
	Kind    AssetKind
	State   AssetVersionState
	Object  ObjectVersion
	OwnerID string
}

func (w *Worker) beginScan(ctx context.Context, assetVersionID, scanWorkID string) (versionRecord, int, bool, error) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return versionRecord{}, 0, false, fmt.Errorf("beginning scan claim: %w", err)
	}
	defer tx.Rollback(ctx)
	var version versionRecord
	err = tx.QueryRow(ctx, `
		SELECT mav.id::text, mav.kind, mav.state, mav.storage_object_key,
		       mav.storage_object_version, ma.owner_account_id::text
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		WHERE mav.id = $1::uuid
		FOR UPDATE OF mav
	`, assetVersionID).Scan(&version.ID, &version.Kind, &version.State, &version.Object.StorageObjectKey, &version.Object.StorageObjectVersion, &version.OwnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return versionRecord{}, 0, false, ErrNotFound
	}
	if err != nil {
		return versionRecord{}, 0, false, fmt.Errorf("loading media version for scan: %w", err)
	}
	version.Object.AssetVersionID = version.ID
	var priorAssetVersionID string
	err = tx.QueryRow(ctx, `
		SELECT asset_version_id::text
		FROM scan_attempts
		WHERE work_id = $1
		FOR SHARE
	`, scanWorkID).Scan(&priorAssetVersionID)
	if err == nil {
		if priorAssetVersionID != version.ID {
			return versionRecord{}, 0, false, fmt.Errorf("%w: scan work belongs to a different asset version", ErrConflict)
		}
		if err := tx.Commit(ctx); err != nil {
			return versionRecord{}, 0, false, fmt.Errorf("committing duplicate scan work lookup: %w", err)
		}
		return version, 0, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return versionRecord{}, 0, false, fmt.Errorf("checking duplicate scan work: %w", err)
	}
	if version.State != StateQuarantined {
		return version, 0, false, nil
	}
	var attempt int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(attempt_number), 0) + 1 FROM scan_attempts WHERE asset_version_id = $1::uuid`, assetVersionID).Scan(&attempt); err != nil {
		return versionRecord{}, 0, false, fmt.Errorf("allocating scan attempt: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid AND state = 'QUARANTINED'`, assetVersionID); err != nil {
		return versionRecord{}, 0, false, fmt.Errorf("claiming media version for scan: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return versionRecord{}, 0, false, fmt.Errorf("committing scan claim: %w", err)
	}
	return version, attempt, true, nil
}

func (w *Worker) finishScan(ctx context.Context, version versionRecord, attempt int, scanWorkID string, observation ScanObservation, next AssetVersionState) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning scan result transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	evidence, err := recordScanEvidence(ctx, tx, version, attempt, scanWorkID, observation, next)
	if err != nil {
		return err
	}
	if err := w.applyScanState(ctx, tx, version, evidence); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing scan result: %w", err)
	}
	return nil
}

type scanEvidence struct {
	attemptID   string
	observation ScanObservation
	next        AssetVersionState
}

func recordScanEvidence(ctx context.Context, tx pgx.Tx, version versionRecord, attempt int, scanWorkID string, observation ScanObservation, next AssetVersionState) (scanEvidence, error) {
	var attemptID string
	err := tx.QueryRow(ctx, `
		INSERT INTO scan_attempts (
			asset_version_id, attempt_number, work_id, storage_object_version,
			outcome, scanner_identity, reason
		) VALUES ($1::uuid, $2, $3, $4, $5::media_scan_outcome, $6, NULLIF($7, ''))
		ON CONFLICT DO NOTHING
		RETURNING id::text
	`, version.ID, attempt, scanWorkID, version.Object.StorageObjectVersion, observation.Outcome, observation.ScannerIdentity, observation.Reason).Scan(&attemptID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return loadDuplicateScanEvidence(ctx, tx, version, attempt, scanWorkID, observation)
		}
		return scanEvidence{}, fmt.Errorf("recording scan attempt: %w", err)
	}
	return scanEvidence{attemptID: attemptID, observation: observation, next: next}, nil
}

func loadDuplicateScanEvidence(ctx context.Context, tx pgx.Tx, version versionRecord, attempt int, scanWorkID string, observation ScanObservation) (scanEvidence, error) {
	var evidence scanEvidence
	var persistedOutcome ScanOutcome
	var persistedAssetVersionID, persistedObjectVersion, persistedScannerIdentity, persistedReason string
	var persistedAttempt int
	if err := tx.QueryRow(ctx, `
		SELECT id::text, asset_version_id::text, attempt_number, storage_object_version,
		       outcome, scanner_identity, COALESCE(reason, '')
		FROM scan_attempts
		WHERE work_id = $1
	`, scanWorkID).Scan(&evidence.attemptID, &persistedAssetVersionID, &persistedAttempt, &persistedObjectVersion, &persistedOutcome, &persistedScannerIdentity, &persistedReason); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return scanEvidence{}, fmt.Errorf("%w: scan attempt identity already exists for a different work item", ErrConflict)
		}
		return scanEvidence{}, fmt.Errorf("loading duplicate scan evidence: %w", err)
	}
	if persistedAssetVersionID != version.ID || persistedAttempt != attempt || persistedObjectVersion != version.Object.StorageObjectVersion ||
		persistedOutcome != observation.Outcome || persistedScannerIdentity != observation.ScannerIdentity || persistedReason != observation.Reason {
		return scanEvidence{}, fmt.Errorf("%w: scan work was replayed with different evidence", ErrConflict)
	}
	evidence.observation.AssetVersionID = version.ID
	evidence.observation.StorageObjectVersion = version.Object.StorageObjectVersion
	evidence.observation.Outcome = persistedOutcome
	evidence.observation.ScannerIdentity = persistedScannerIdentity
	evidence.observation.Reason = persistedReason
	next, err := ScanTransition(persistedOutcome)
	if err != nil {
		return scanEvidence{}, err
	}
	evidence.next = next
	return evidence, nil
}

func (w *Worker) applyScanState(ctx context.Context, tx pgx.Tx, version versionRecord, evidence scanEvidence) error {
	if evidence.next == StateScanPassed {
		if version.Kind == KindVideo {
			commandTag, err := tx.Exec(ctx, `
				UPDATE media_asset_versions
				SET state = 'SCAN_PASSED', successful_scan_attempt_id = $1::uuid
				WHERE id = $2::uuid AND state = 'SCANNING'
			`, evidence.attemptID, version.ID)
			if err != nil {
				return fmt.Errorf("recording successful video scan: %w", err)
			}
			if commandTag.RowsAffected() != 1 {
				return ErrConcurrentModification
			}
			if err := appendTranscodeWork(ctx, tx, w.outbox, version.ID, "scan:"+evidence.attemptID); err != nil {
				return err
			}
			return nil
		}
		commandTag, err := tx.Exec(ctx, `
			UPDATE media_asset_versions
			SET state = 'SCAN_PASSED', successful_scan_attempt_id = $1::uuid
			WHERE id = $2::uuid AND state = 'SCANNING'
		`, evidence.attemptID, version.ID)
		if err != nil {
			return fmt.Errorf("recording successful non-video scan: %w", err)
		}
		if commandTag.RowsAffected() != 1 {
			return ErrConcurrentModification
		}
		commandTag, err = tx.Exec(ctx, `
			UPDATE media_asset_versions
			SET state = 'READY'
			WHERE id = $1::uuid AND state = 'SCAN_PASSED' AND kind <> 'VIDEO'
		`, version.ID)
		if err != nil {
			return fmt.Errorf("marking successful non-video scan ready: %w", err)
		}
		if commandTag.RowsAffected() != 1 {
			return ErrConcurrentModification
		}
		return nil
	}
	commandTag, err := tx.Exec(ctx, `
		UPDATE media_asset_versions SET state = $1::media_asset_version_state
		WHERE id = $2::uuid AND state = 'SCANNING'
	`, evidence.next, version.ID)
	if err != nil {
		return fmt.Errorf("recording failed scan state: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return ErrConcurrentModification
	}
	return nil
}

func (w *Worker) recordScanFailure(ctx context.Context, version versionRecord, attempt int, scanWorkID string, outcome ScanOutcome, scannerIdentity, reason string) error {
	return w.finishScan(ctx, version, attempt, scanWorkID, ScanObservation{
		AssetVersionID: version.ID, StorageObjectVersion: version.Object.StorageObjectVersion,
		Outcome: outcome, ScannerIdentity: scannerIdentity, Reason: reason,
	}, StateScanError)
}

// Transcode claims one version that already holds legitimate safety evidence —
// SCAN_PASSED from the scanner path or VALIDATED from the D-088 path — runs the
// trusted processor, and records a single immutable processing result. Zero
// outputs and all provider errors become PROCESS_FAILED and never READY.
//
// The worker is deliberately mode-agnostic. It reads the provenance the
// database already holds rather than the deployment's operating mode, so it
// cannot start processing an asset whose safety path was never satisfied, and
// it never has to decide which path an asset should have taken.
func (w *Worker) Transcode(ctx context.Context, assetVersionID, operationID string) error {
	if strings.TrimSpace(operationID) == "" {
		return fmt.Errorf("%w: transcode operation ID is required", ErrValidation)
	}
	return w.transcodeGate.run(ctx, func() error {
		return w.transcode(ctx, assetVersionID, operationID)
	})
}

func (w *Worker) transcode(ctx context.Context, assetVersionID, operationID string) error {
	version, applied, err := w.beginTranscode(ctx, assetVersionID)
	if err != nil || !applied {
		return err
	}
	processingCtx, cancel := context.WithTimeout(ctx, w.processingTimeout)
	defer cancel()
	result, processErr := w.process.Transcode(processingCtx, version.Object)
	if processErr != nil {
		return w.recordProcessingFailure(ctx, version.ID, operationID, processErr)
	}
	if result.OperationID != "" && result.OperationID != operationID {
		return w.recordProcessingFailure(ctx, version.ID, operationID, errors.New("processor operation identity mismatch"))
	}
	if err := validateTranscodeCompletion(operationID, result); err != nil {
		return w.recordProcessingFailure(ctx, version.ID, operationID, err)
	}
	return w.CompleteTranscode(ctx, version.ID, operationID, result)
}

func (w *Worker) beginTranscode(ctx context.Context, assetVersionID string) (versionRecord, bool, error) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return versionRecord{}, false, fmt.Errorf("beginning transcode claim: %w", err)
	}
	defer tx.Rollback(ctx)
	var version versionRecord
	err = tx.QueryRow(ctx, `
		SELECT mav.id::text, mav.kind, mav.state, mav.storage_object_key,
		       mav.storage_object_version, ma.owner_account_id::text
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		WHERE mav.id = $1::uuid
		FOR UPDATE OF mav
	`, assetVersionID).Scan(&version.ID, &version.Kind, &version.State, &version.Object.StorageObjectKey, &version.Object.StorageObjectVersion, &version.OwnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return versionRecord{}, false, ErrNotFound
	}
	if err != nil {
		return versionRecord{}, false, fmt.Errorf("loading media version for transcode: %w", err)
	}
	version.Object.AssetVersionID = version.ID
	// Only a version that already holds its own safety evidence may be claimed.
	// A quarantined, scanning, failed, or arbitrary version is left alone.
	if version.State != StateScanPassed && version.State != StateValidated {
		return version, false, nil
	}
	claimed, err := tx.Exec(ctx, `
		UPDATE media_asset_versions SET state = 'PROCESSING'
		WHERE id = $1::uuid AND state = $2::media_asset_version_state
		  AND (successful_scan_attempt_id IS NOT NULL OR successful_validation_attempt_id IS NOT NULL)
	`, assetVersionID, version.State)
	if err != nil {
		return versionRecord{}, false, fmt.Errorf("claiming media version for transcode: %w", err)
	}
	if claimed.RowsAffected() != 1 {
		return version, false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return versionRecord{}, false, fmt.Errorf("committing transcode claim: %w", err)
	}
	return version, true, nil
}

// CompleteTranscode is the provider/callback idempotency boundary. Replaying
// the same operation returns success without adding a version, duration, or
// rendition; conflicting evidence is rejected.
func (w *Worker) CompleteTranscode(ctx context.Context, assetVersionID, operationID string, result TranscodeResult) error {
	if err := validateTranscodeCompletion(operationID, result); err != nil {
		return err
	}
	fingerprint, err := transcodeFingerprint(result)
	if err != nil {
		return err
	}
	completion := transcodeCompletion{
		assetVersionID: assetVersionID, operationID: operationID,
		fingerprint: fingerprint, result: result,
	}
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transcode completion: %w", err)
	}
	defer tx.Rollback(ctx)
	duplicate, err := checkTranscodeReceipt(ctx, tx, completion)
	if err != nil {
		return err
	}
	if duplicate {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing duplicate transcode callback: %w", err)
		}
		return nil
	}
	if err := insertTranscodeReceipt(ctx, tx, completion); err != nil {
		if isUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			return w.resolveDuplicateTranscode(ctx, completion)
		}
		return err
	}
	if err := w.recordSuccessfulProcessing(ctx, tx, completion); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transcode completion: %w", err)
	}
	return nil
}

type transcodeCompletion struct {
	assetVersionID string
	operationID    string
	fingerprint    string
	result         TranscodeResult
}

func validateTranscodeCompletion(operationID string, result TranscodeResult) error {
	if strings.TrimSpace(operationID) == "" {
		return fmt.Errorf("%w: transcode operation ID is required", ErrValidation)
	}
	if result.TrustedDurationMS <= 0 || len(result.Renditions) == 0 || strings.TrimSpace(result.OutputPrefix) == "" {
		return fmt.Errorf("%w: transcode output is not successful", ErrValidation)
	}
	for _, rendition := range result.Renditions {
		if strings.TrimSpace(rendition.Name) == "" || strings.TrimSpace(rendition.StorageObjectKey) == "" {
			return fmt.Errorf("%w: rendition identity is incomplete", ErrValidation)
		}
	}
	return nil
}

func checkTranscodeReceipt(ctx context.Context, tx pgx.Tx, completion transcodeCompletion) (bool, error) {
	var assetVersionID, fingerprint string
	err := tx.QueryRow(ctx, `
		SELECT asset_version_id::text, request_fingerprint
		FROM media_callback_receipts
		WHERE callback_kind = $1 AND provider_event_id = $2
	`, callbackTranscode, completion.operationID).Scan(&assetVersionID, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking transcode callback receipt: %w", err)
	}
	if assetVersionID != completion.assetVersionID || fingerprint != completion.fingerprint {
		return false, fmt.Errorf("%w: transcode callback was replayed with different evidence", ErrConflict)
	}
	return true, nil
}

func insertTranscodeReceipt(ctx context.Context, tx pgx.Tx, completion transcodeCompletion) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_callback_receipts (
			provider_event_id, callback_kind, asset_version_id, request_fingerprint
		) VALUES ($1, $2, $3::uuid, $4)
	`, completion.operationID, callbackTranscode, completion.assetVersionID, completion.fingerprint); err != nil {
		return fmt.Errorf("recording transcode callback receipt: %w", err)
	}
	return nil
}

func (w *Worker) recordSuccessfulProcessing(ctx context.Context, tx pgx.Tx, completion transcodeCompletion) error {
	if err := requireProcessingProvenance(ctx, tx, completion.assetVersionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO processing_attempts (
			asset_version_id, operation_id, state, output_prefix,
			rendition_count, trusted_duration_ms
		) VALUES ($1::uuid, $2, 'SUCCEEDED', $3, $4, $5)
		ON CONFLICT (asset_version_id, operation_id) DO NOTHING
	`, completion.assetVersionID, completion.operationID, completion.result.OutputPrefix, len(completion.result.Renditions), completion.result.TrustedDurationMS); err != nil {
		return fmt.Errorf("recording successful processing attempt: %w", err)
	}
	var attemptID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text FROM processing_attempts
		WHERE asset_version_id = $1::uuid AND operation_id = $2 AND state = 'SUCCEEDED'
	`, completion.assetVersionID, completion.operationID).Scan(&attemptID); err != nil {
		return fmt.Errorf("loading successful processing attempt: %w", err)
	}
	if err := recordRenditions(ctx, tx, completion); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE media_asset_versions
		SET trusted_duration_ms = $1, successful_processing_attempt_id = $2::uuid, state = 'READY'
		WHERE id = $3::uuid AND state = 'PROCESSING'
		  AND (successful_scan_attempt_id IS NOT NULL OR successful_validation_attempt_id IS NOT NULL)
	`, completion.result.TrustedDurationMS, attemptID, completion.assetVersionID); err != nil {
		return fmt.Errorf("marking media asset ready: %w", err)
	}
	return nil
}

// requireProcessingProvenance refuses to record a successful processing result
// for a version that is not in PROCESSING or that holds neither legitimate
// safety evidence. It accepts either provenance without confusing them: the two
// columns stay distinct, and nothing here writes or reads one as the other.
func requireProcessingProvenance(ctx context.Context, tx pgx.Tx, assetVersionID string) error {
	var state AssetVersionState
	var scanEvidence, validationEvidence *string
	if err := tx.QueryRow(ctx, `
		SELECT state, successful_scan_attempt_id::text, successful_validation_attempt_id::text
		FROM media_asset_versions WHERE id = $1::uuid FOR UPDATE
	`, assetVersionID).Scan(&state, &scanEvidence, &validationEvidence); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("loading transcode target: %w", err)
	}
	if state != StateProcessing {
		return fmt.Errorf("%w: transcode target is not PROCESSING", ErrConflict)
	}
	if scanEvidence == nil && validationEvidence == nil {
		return fmt.Errorf("%w: transcode target lacks successful scan or validation evidence", ErrConflict)
	}
	return nil
}

func recordRenditions(ctx context.Context, tx pgx.Tx, completion transcodeCompletion) error {
	for _, rendition := range completion.result.Renditions {
		if strings.TrimSpace(rendition.Name) == "" || strings.TrimSpace(rendition.StorageObjectKey) == "" {
			return fmt.Errorf("%w: rendition identity is incomplete", ErrValidation)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO video_renditions (
				asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms
			) VALUES ($1::uuid, $2, $3, NULLIF($4, 0), NULLIF($5, 0), NULLIF($6, 0), $7)
			ON CONFLICT (asset_version_id, name) DO NOTHING
		`, completion.assetVersionID, rendition.Name, rendition.StorageObjectKey, rendition.Width, rendition.Height, rendition.BitrateKbps, rendition.DurationMS); err != nil {
			return fmt.Errorf("recording video rendition: %w", err)
		}
	}
	return nil
}

func (w *Worker) recordProcessingFailure(ctx context.Context, assetVersionID, operationID string, cause error) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning processing failure: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO processing_attempts (
			asset_version_id, operation_id, state, rendition_count, error_reason
		) VALUES ($1::uuid, $2, 'FAILED', 0, $3)
		ON CONFLICT (asset_version_id, operation_id) DO NOTHING
	`, assetVersionID, operationID, cause.Error()); err != nil {
		return fmt.Errorf("recording processing failure: %w", err)
	}
	// PROCESS_FAILED is reachable from PROCESSING and from either pre-processing
	// state, so a failure recorded before or after the claim leaves the asset
	// non-deliverable either way.
	if _, err := tx.Exec(ctx, `
		UPDATE media_asset_versions SET state = 'PROCESS_FAILED'
		WHERE id = $1::uuid AND state IN ('PROCESSING', 'SCAN_PASSED', 'VALIDATED')
	`, assetVersionID); err != nil {
		return fmt.Errorf("marking processing failure: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing processing failure: %w", err)
	}
	return cause
}

func transcodeFingerprint(result TranscodeResult) (string, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("fingerprinting transcode result: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

func (w *Worker) resolveDuplicateTranscode(ctx context.Context, completion transcodeCompletion) error {
	var priorAsset, priorFingerprint string
	err := w.db.QueryRow(ctx, `
		SELECT asset_version_id::text, request_fingerprint
		FROM media_callback_receipts
		WHERE callback_kind = $1 AND provider_event_id = $2
	`, callbackTranscode, completion.operationID).Scan(&priorAsset, &priorFingerprint)
	if err != nil {
		return fmt.Errorf("loading duplicate transcode callback: %w", err)
	}
	if priorAsset != completion.assetVersionID || priorFingerprint != completion.fingerprint {
		return fmt.Errorf("%w: transcode callback was replayed with different evidence", ErrConflict)
	}
	return nil
}
