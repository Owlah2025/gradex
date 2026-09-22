package media

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
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
	workLeaseDuration time.Duration
	now               func() time.Time
	transcodeGate     *concurrencyGate
	observeTranscode  TranscodeObserver
}

// TranscodePhase identifies the bounded lifecycle observations emitted around
// the existing transcode gate.
type TranscodePhase string

const (
	TranscodeStarted  TranscodePhase = "STARTED"
	TranscodeFinished TranscodePhase = "FINISHED"
)

// TranscodeObservation is safe operational telemetry for one gated operation.
// It contains no media object identity or storage capability.
type TranscodeObservation struct {
	Phase       TranscodePhase
	OperationID string
	Active      int
	Limit       int
	Outcome     string
}

// TranscodeObserver receives advisory transcode lifecycle telemetry.
type TranscodeObserver func(TranscodeObservation)

type WorkerOptions struct {
	DB                   *pgxpool.Pool
	Scanner              *ScannerAdapter
	Process              Processor
	Outbox               *outbox.Writer
	ProcessingTimeout    time.Duration
	WorkLeaseDuration    time.Duration
	TranscodeConcurrency int
	ObserveTranscode     TranscodeObserver
	Now                  func() time.Time
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
	lease := options.WorkLeaseDuration
	if lease == 0 {
		lease = timeout + DefaultWorkLeaseGrace
	}
	if lease <= timeout {
		return nil, errors.New("media work lease must exceed the processing timeout")
	}
	now := options.Now
	if now == nil {
		now = time.Now
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
		processingTimeout: timeout, workLeaseDuration: lease, now: now,
		transcodeGate: newConcurrencyGate(concurrency), observeTranscode: options.ObserveTranscode,
	}, nil
}

type concurrencyGate struct {
	slots    chan struct{}
	active   atomic.Int32
	inFlight atomic.Int32
}

func newConcurrencyGate(limit int) *concurrencyGate {
	return &concurrencyGate{slots: make(chan struct{}, limit)}
}

func (g *concurrencyGate) run(ctx context.Context, work func() error) error {
	g.inFlight.Add(1)
	defer g.inFlight.Add(-1)
	select {
	case g.slots <- struct{}{}:
		g.active.Add(1)
		defer func() {
			g.active.Add(-1)
			<-g.slots
		}()
		return work()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *concurrencyGate) Active() int { return int(g.active.Load()) }

func (g *concurrencyGate) Limit() int { return cap(g.slots) }

// InFlight reports callers that are either waiting for or holding a gate slot.
// It is intended for deterministic tests and diagnostics, not scheduling.
func (g *concurrencyGate) InFlight() int { return int(g.inFlight.Load()) }

func (w *Worker) Register(mux *asynq.ServeMux) error {
	if mux == nil {
		return errors.New("media worker mux is required")
	}
	mux.HandleFunc(queue.TypeMediaScan, w.handleScanTask)
	mux.HandleFunc(queue.TypeMediaTranscode, w.handleTranscodeTask)
	mux.HandleFunc(queue.TypeMediaEnhancement, w.handleEnhancementTask)
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
	scanCtx, cancel := context.WithTimeout(ctx, w.processingTimeout)
	defer cancel()
	err := w.scan(scanCtx, work.AssetVersionID, work.ScanWorkID, true)
	if errors.Is(err, ErrRetryScheduled) {
		return nil
	}
	return err
}

func (w *Worker) handleTranscodeTask(ctx context.Context, task *asynq.Task) error {
	var work TranscodeWork
	if err := json.Unmarshal(task.Payload(), &work); err != nil {
		return fmt.Errorf("decoding media transcode work: %w", err)
	}
	processingCtx, cancel := context.WithTimeout(ctx, w.processingTimeout)
	defer cancel()
	err := w.Transcode(processingCtx, work.AssetVersionID, work.OperationID)
	if errors.Is(err, ErrRetryScheduled) {
		return nil
	}
	return err
}

func (w *Worker) handleEnhancementTask(ctx context.Context, task *asynq.Task) error {
	var work EnhancementWork
	if err := json.Unmarshal(task.Payload(), &work); err != nil {
		return fmt.Errorf("decoding media enhancement work: %w", err)
	}
	if strings.TrimSpace(work.AssetVersionID) == "" {
		return fmt.Errorf("%w: media enhancement asset version is required", ErrValidation)
	}
	processingCtx, cancel := context.WithTimeout(ctx, w.processingTimeout)
	defer cancel()
	err := w.RetryEnhancements(processingCtx, work.AssetVersionID)
	if errors.Is(err, ErrEnhancementNotEligible) || errors.Is(err, ErrEnhancementActive) {
		return nil
	}
	return err
}

// Scan transitions QUARANTINED -> SCANNING under a database CAS, scans the
// exact object identity, and records immutable attempt evidence. A passed
// video schedules transcode through a committed outbox event; it never pushes
// a Redis task from the state-changing transaction.
func (w *Worker) Scan(ctx context.Context, assetVersionID string) error {
	return w.scan(ctx, assetVersionID, uuid.NewString(), false)
}

// scan is the durable scan-work boundary. Production tasks carry the committed
// outbox event ID; the exported Scan convenience method is retained for
// in-process callers and creates one distinct work identity.
func (w *Worker) scan(ctx context.Context, assetVersionID, scanWorkID string, automaticRetry bool) error {
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
	observation.Reason = truncateMediaOutput(observation.Reason, 2000)

	next, applyErr := ApplyScanObservation(StateScanning, version.Object, observation)
	if applyErr != nil {
		// A stale callback is diagnosable but cannot advance the replacement.
		return w.recordScanFailure(ctx, version, attempt, scanWorkID, ScanError, observation.ScannerIdentity, applyErr.Error())
	}
	resultCtx := ctx
	cancelResult := func() {}
	if ctx.Err() != nil {
		resultCtx, cancelResult = context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	}
	defer cancelResult()
	if err := w.finishScan(resultCtx, version, attempt, scanWorkID, observation, next); err != nil {
		return err
	}
	if scanErr != nil {
		if automaticRetry && errors.Is(scanErr, ErrScannerUnavailable) {
			if err := w.scheduleScanRetry(resultCtx, version.ID); err != nil {
				return fmt.Errorf("%w: %v", err, scanErr)
			}
		}
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
	// The scan lease window is generated by the database clock for the same
	// reason the processing lease is; see beginTranscode.
	var attempt int
	if err := tx.QueryRow(ctx, `
		UPDATE media_asset_versions
		SET state = 'SCANNING', work_claim_token = $2, work_claimed_at = now(),
		    work_lease_expires_at = now() + make_interval(secs => $3),
		    scan_attempt_count = GREATEST(scan_attempt_count, (
		      SELECT COALESCE(max(sa.attempt_number), 0) FROM scan_attempts sa WHERE sa.asset_version_id = $1::uuid
		    )) + 1,
		    last_failure_category = NULL
		WHERE id = $1::uuid AND state = 'QUARANTINED'
		RETURNING scan_attempt_count
	`, assetVersionID, scanWorkID, w.workLeaseDuration.Seconds()).Scan(&attempt); err != nil {
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
	if err := w.applyScanState(ctx, tx, version, scanWorkID, evidence); err != nil {
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

// applyScanState commits the scan outcome. Every transition out of SCANNING is
// fenced in the database by the claiming work identity AND by database-time
// lease validity, so a worker whose lease has already expired cannot commit a
// result even while the recovery pass has not yet reclaimed the row. The
// application clock is never the authority here: `now()` is evaluated by
// PostgreSQL inside this transaction.
func (w *Worker) applyScanState(ctx context.Context, tx pgx.Tx, version versionRecord, scanWorkID string, evidence scanEvidence) error {
	if evidence.next == StateScanPassed {
		if version.Kind == KindVideo {
			commandTag, err := tx.Exec(ctx, `
				UPDATE media_asset_versions
				SET state = 'SCAN_PASSED', successful_scan_attempt_id = $1::uuid,
				    work_claim_token = NULL, work_claimed_at = NULL, work_lease_expires_at = NULL
				WHERE id = $2::uuid AND state = 'SCANNING'
				  AND work_claim_token = $3
				  AND work_lease_expires_at > now()
			`, evidence.attemptID, version.ID, scanWorkID)
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
			SET state = 'SCAN_PASSED', successful_scan_attempt_id = $1::uuid,
			    work_claim_token = NULL, work_claimed_at = NULL, work_lease_expires_at = NULL
			WHERE id = $2::uuid AND state = 'SCANNING'
			  AND work_claim_token = $3
			  AND work_lease_expires_at > now()
		`, evidence.attemptID, version.ID, scanWorkID)
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
		UPDATE media_asset_versions SET state = $1::media_asset_version_state,
		    work_claim_token = NULL, work_claimed_at = NULL, work_lease_expires_at = NULL,
		    last_failure_category = CASE WHEN $1::media_asset_version_state = 'SCAN_FAILED' THEN 'SCAN_REJECTED' ELSE 'SCAN_UNAVAILABLE' END
		WHERE id = $2::uuid AND state = 'SCANNING'
		  AND work_claim_token = $3
		  AND work_lease_expires_at > now()
	`, evidence.next, version.ID, scanWorkID)
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
	started := false
	err := w.transcodeGate.run(ctx, func() error {
		started = true
		w.notifyTranscode(TranscodeObservation{
			Phase: TranscodeStarted, OperationID: operationID,
			Active: w.transcodeGate.Active(), Limit: w.transcodeGate.Limit(),
		})
		return w.transcode(ctx, assetVersionID, operationID)
	})
	if started {
		w.notifyTranscode(TranscodeObservation{
			Phase: TranscodeFinished, OperationID: operationID,
			Active: w.transcodeGate.Active(), Limit: w.transcodeGate.Limit(),
			Outcome: transcodeOutcome(err),
		})
	}
	return err
}

// RetryEnhancements executes one explicit manual recovery intent. The queue
// carries only the Asset Version; claim, source proof, ladder planning and
// operation identity are all decided against current database/storage truth.
func (w *Worker) RetryEnhancements(ctx context.Context, assetVersionID string) error {
	operationID := ""
	started := false
	err := w.transcodeGate.run(ctx, func() error {
		started = true
		operationID = uuid.NewString()
		w.notifyTranscode(TranscodeObservation{
			Phase: TranscodeStarted, OperationID: operationID,
			Active: w.transcodeGate.Active(), Limit: w.transcodeGate.Limit(),
		})
		return w.retryEnhancements(ctx, assetVersionID, operationID)
	})
	if started {
		w.notifyTranscode(TranscodeObservation{
			Phase: TranscodeFinished, OperationID: operationID,
			Active: w.transcodeGate.Active(), Limit: w.transcodeGate.Limit(),
			Outcome: transcodeOutcome(err),
		})
	}
	return err
}

func (w *Worker) retryEnhancements(ctx context.Context, assetVersionID, operationID string) error {
	processor, ok := w.process.(EnhancementProcessor)
	if !ok {
		return fmt.Errorf("%w: enhancement processor capability is unavailable", ErrUnavailable)
	}
	target, claimed, err := w.beginEnhancement(ctx, assetVersionID, operationID)
	if err != nil || !claimed {
		return err
	}
	probe, err := processor.ProbeExpected(ctx, target.object)
	if err != nil {
		return w.failEnhancement(ctx, assetVersionID, operationID, err)
	}
	missing, err := w.missingEnhancementRenditions(ctx, assetVersionID, probe.ExpectedRenditions)
	if err != nil {
		return w.failEnhancement(ctx, assetVersionID, operationID, err)
	}
	if len(missing) == 0 {
		if err := w.beginFinalization(ctx, assetVersionID, operationID); err != nil {
			return w.failEnhancement(ctx, assetVersionID, operationID, err)
		}
		if err := w.completeFinalization(ctx, assetVersionID, operationID, probe.TrustedDurationMS, probe.ExpectedRenditions); err != nil {
			return w.failFinalization(ctx, assetVersionID, operationID, err)
		}
		return nil
	}
	target.object.ProcessingOperationID = operationID
	result, err := processor.TranscodeMissing(ctx, target.object, missing, nil, &workerRenditionSink{
		worker: w, assetVersionID: assetVersionID, operationID: operationID,
	})
	if err != nil {
		return w.failEnhancement(ctx, assetVersionID, operationID, err)
	}
	result.ExpectedRenditions = append([]string(nil), probe.ExpectedRenditions...)
	result.TrustedDurationMS = probe.TrustedDurationMS
	result.OutputPrefix = processingOutputPrefix(assetVersionID, operationID)
	if err := w.completeEnhancement(ctx, assetVersionID, operationID, result); err != nil {
		return w.failEnhancement(ctx, assetVersionID, operationID, err)
	}
	return nil
}

type enhancementTarget struct{ object ObjectVersion }

func (w *Worker) beginEnhancement(ctx context.Context, assetVersionID, operationID string) (enhancementTarget, bool, error) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return enhancementTarget{}, false, fmt.Errorf("beginning enhancement claim: %w", err)
	}
	defer tx.Rollback(ctx)
	var target enhancementTarget
	var kind AssetKind
	var state AssetVersionState
	var retiredAt *time.Time
	var claimToken *string
	var leaseValid bool
	var checksum string
	var scanEvidence, validationEvidence *string
	err = tx.QueryRow(ctx, `
		SELECT mav.kind, mav.state, mav.storage_object_key, mav.storage_object_version,
		       COALESCE(mav.sha256_hex, ''), ma.retired_at, mav.work_claim_token,
		       mav.successful_scan_attempt_id::text, mav.successful_validation_attempt_id::text,
		       COALESCE(mav.work_lease_expires_at > now(), false)
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		WHERE mav.id = $1::uuid
		FOR UPDATE OF mav
	`, assetVersionID).Scan(&kind, &state, &target.object.StorageObjectKey, &target.object.StorageObjectVersion,
		&checksum, &retiredAt, &claimToken, &scanEvidence, &validationEvidence, &leaseValid)
	if errors.Is(err, pgx.ErrNoRows) {
		return enhancementTarget{}, false, ErrNotFound
	}
	if err != nil {
		return enhancementTarget{}, false, fmt.Errorf("loading enhancement target: %w", err)
	}
	if kind != KindVideo || retiredAt != nil || state != StatePlayable || (scanEvidence == nil && validationEvidence == nil) || strings.TrimSpace(checksum) == "" {
		return target, false, ErrEnhancementNotEligible
	}
	if claimToken != nil || leaseValid {
		return target, false, ErrEnhancementActive
	}
	target.object.AssetVersionID = assetVersionID
	target.object.ExpectedSHA256Hex = checksum
	claimed, err := tx.Exec(ctx, `
		UPDATE media_asset_versions
		SET processing_stage = 'TRANSCODING', processing_progress_percent = 0,
		    processing_updated_at = now(), processing_attempt_token = $2,
		    active_processing_attempt_kind = 'ENHANCEMENT',
		    work_claim_token = $2, work_claimed_at = now(),
		    work_lease_expires_at = now() + make_interval(secs => $3),
		    processing_attempt_count = processing_attempt_count + 1,
		    last_failure_category = NULL
		WHERE id = $1::uuid AND state = 'PLAYABLE'
		  AND work_claim_token IS NULL
		  AND (work_lease_expires_at IS NULL OR work_lease_expires_at <= now())
	`, assetVersionID, operationID, w.workLeaseDuration.Seconds())
	if err != nil {
		return enhancementTarget{}, false, fmt.Errorf("claiming enhancement target: %w", err)
	}
	if claimed.RowsAffected() != 1 {
		return target, false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return enhancementTarget{}, false, fmt.Errorf("committing enhancement claim: %w", err)
	}
	return target, true, nil
}

func (w *Worker) beginFinalization(ctx context.Context, assetVersionID, operationID string) error {
	updated, err := w.db.Exec(ctx, `
		UPDATE media_asset_versions SET active_processing_attempt_kind='FINALIZATION'
		WHERE id=$1::uuid AND state='PLAYABLE' AND work_claim_token=$2
		  AND work_lease_expires_at > now()
		  AND active_processing_attempt_kind='ENHANCEMENT'
	`, assetVersionID, operationID)
	if err != nil {
		return fmt.Errorf("classifying finalization claim: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return ErrConcurrentModification
	}
	return nil
}

func (w *Worker) missingEnhancementRenditions(ctx context.Context, assetVersionID string, expected []string) ([]string, error) {
	rows, err := w.db.Query(ctx, `SELECT name FROM video_renditions WHERE asset_version_id = $1::uuid`, assetVersionID)
	if err != nil {
		return nil, fmt.Errorf("loading canonical enhancement renditions: %w", err)
	}
	defer rows.Close()
	existing := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning canonical enhancement rendition: %w", err)
		}
		existing[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating canonical enhancement renditions: %w", err)
	}
	return missingRenditionNames(expected, existing), nil
}

func missingRenditionNames(expected []string, existing map[string]struct{}) []string {
	missing := make([]string, 0, len(expected))
	for _, name := range expected {
		if _, ok := existing[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

func (w *Worker) completeEnhancement(ctx context.Context, assetVersionID, operationID string, result TranscodeResult) error {
	return w.completeRecoveryAttempt(ctx, assetVersionID, operationID, result, "ENHANCEMENT", result.OutputPrefix, len(result.Renditions))
}

func (w *Worker) completeFinalization(ctx context.Context, assetVersionID, operationID string, durationMS int64, expected []string) error {
	result := TranscodeResult{ExpectedRenditions: expected, TrustedDurationMS: durationMS}
	return w.completeRecoveryAttempt(ctx, assetVersionID, operationID, result, "FINALIZATION", "", 0)
}

func (w *Worker) completeRecoveryAttempt(ctx context.Context, assetVersionID, operationID string, result TranscodeResult, attemptKind, outputPrefix string, renditionCount int) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning recovery completion: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := verifyCanonicalRecoveryLadder(ctx, tx, assetVersionID, result.ExpectedRenditions, result.TrustedDurationMS); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, output_prefix, rendition_count, trusted_duration_ms)
		VALUES ($1::uuid, $2, 'SUCCEEDED', $3::media_processing_attempt_kind, NULLIF($4, ''), $5, $6)
		ON CONFLICT (asset_version_id, operation_id) DO NOTHING
	`, assetVersionID, operationID, attemptKind, outputPrefix, renditionCount, result.TrustedDurationMS); err != nil {
		return fmt.Errorf("recording successful recovery attempt: %w", err)
	}
	var attemptID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM processing_attempts WHERE asset_version_id=$1::uuid AND operation_id=$2 AND state='SUCCEEDED'`, assetVersionID, operationID).Scan(&attemptID); err != nil {
		return fmt.Errorf("loading successful recovery attempt: %w", err)
	}
	updated, err := tx.Exec(ctx, `
		UPDATE media_asset_versions
		SET state='READY', trusted_duration_ms=$1, successful_processing_attempt_id=$2::uuid,
		    processing_stage='PACKAGING', processing_progress_percent=100,
		    processing_updated_at=now(), processing_attempt_token=$4,
		    active_processing_attempt_kind=NULL,
		    work_claim_token=NULL, work_claimed_at=NULL, work_lease_expires_at=NULL,
		    last_failure_category=NULL
		WHERE id=$3::uuid AND state='PLAYABLE' AND work_claim_token=$4
		  AND active_processing_attempt_kind=$5::media_processing_attempt_kind
		  AND work_lease_expires_at > now()
	`, result.TrustedDurationMS, attemptID, assetVersionID, operationID, attemptKind)
	if err != nil {
		return fmt.Errorf("marking recovered media ready: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return ErrConcurrentModification
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing recovered media ready state: %w", err)
	}
	return nil
}

func verifyCanonicalRecoveryLadder(ctx context.Context, tx pgx.Tx, assetVersionID string, expected []string, durationMS int64) error {
	rows, err := tx.Query(ctx, `SELECT name, storage_object_key, COALESCE(width,0), COALESCE(height,0), COALESCE(bitrate_kbps,0), duration_ms FROM video_renditions WHERE asset_version_id=$1::uuid`, assetVersionID)
	if err != nil {
		return fmt.Errorf("loading canonical recovery ladder: %w", err)
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	for rows.Next() {
		var name, key string
		var width, height, bitrate int
		var rowDuration int64
		if err := rows.Scan(&name, &key, &width, &height, &bitrate, &rowDuration); err != nil {
			return fmt.Errorf("scanning canonical recovery ladder: %w", err)
		}
		rung, ok := hlsRungByName(name)
		if !ok || key == "" || width != rung.Width || height != rung.Height || bitrate != rung.VideoKbps || rowDuration != durationMS {
			return fmt.Errorf("%w: canonical recovery rendition %q is inconsistent", ErrConflict, name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("%w: duplicate canonical recovery rendition %q", ErrConflict, name)
		}
		seen[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating canonical recovery ladder: %w", err)
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("%w: canonical recovery ladder has %d rows, expected %d", ErrConflict, len(seen), len(expected))
	}
	for _, name := range expected {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("%w: canonical recovery ladder is missing %q", ErrConflict, name)
		}
	}
	return nil
}

func (w *Worker) failEnhancement(ctx context.Context, assetVersionID, operationID string, cause error) error {
	return w.failRecovery(ctx, assetVersionID, operationID, cause, "ENHANCEMENT")
}

func (w *Worker) failFinalization(ctx context.Context, assetVersionID, operationID string, cause error) error {
	return w.failRecovery(ctx, assetVersionID, operationID, cause, "FINALIZATION")
}

func (w *Worker) failRecovery(ctx context.Context, assetVersionID, operationID string, cause error, attemptKind string) error {
	if errors.Is(cause, context.Canceled) {
		return cause
	}
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning enhancement failure: %w", err)
	}
	defer tx.Rollback(ctx)
	spent, err := processingOperationSpent(ctx, tx, assetVersionID, operationID)
	if err != nil {
		return err
	}
	if spent {
		return nil
	}
	if err := recordFailedProcessingAttempt(ctx, tx, processingFailure{assetVersionID: assetVersionID, operationID: operationID, cause: cause, category: processingFailureCategory(cause), attemptKind: attemptKind}); err != nil {
		return err
	}
	updated, err := tx.Exec(ctx, `
		UPDATE media_asset_versions SET state='PLAYABLE', active_processing_attempt_kind=NULL,
		  work_claim_token=NULL, work_claimed_at=NULL,
		  work_lease_expires_at=NULL, last_failure_category=$3
		WHERE id=$1::uuid AND state='PLAYABLE' AND work_claim_token=$2 AND work_lease_expires_at > now()
		  AND active_processing_attempt_kind=$4::media_processing_attempt_kind
	`, assetVersionID, operationID, processingFailureCategory(cause), attemptKind)
	if err != nil {
		return fmt.Errorf("clearing enhancement claim after failure: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return ErrConcurrentModification
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing enhancement failure: %w", err)
	}
	_, _ = w.CleanupAttemptSafe(ctx, assetVersionID, operationID)
	return cause
}

func (w *Worker) notifyTranscode(observation TranscodeObservation) {
	if w.observeTranscode != nil {
		w.observeTranscode(observation)
	}
}

func transcodeOutcome(err error) string {
	if err == nil {
		return "SUCCEEDED"
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELLED"
	}
	return "FAILED"
}

type workerRenditionSink struct {
	worker         *Worker
	assetVersionID string
	operationID    string
}

func (s *workerRenditionSink) PersistVerifiedRendition(ctx context.Context, rendition Rendition) error {
	return s.worker.PersistVerifiedRendition(ctx, s.assetVersionID, s.operationID, rendition)
}

func (w *Worker) transcode(ctx context.Context, assetVersionID, operationID string) error {
	version, applied, err := w.beginTranscode(ctx, assetVersionID, operationID)
	if err != nil || !applied {
		return err
	}
	processingCtx, cancel := context.WithTimeout(ctx, w.processingTimeout)
	defer cancel()
	// Progress is persisted against this exact operation identity, so a report
	// arriving late from an abandoned attempt cannot overwrite the current one.
	// The observation channel is advisory: a processor that cannot report
	// progress simply does not, and the attempt is unaffected.
	var result TranscodeResult
	var processErr error
	sink := &workerRenditionSink{worker: w, assetVersionID: version.ID, operationID: operationID}
	if progressive, ok := w.process.(ProgressiveProcessor); ok {
		result, processErr = progressive.TranscodeProgressive(
			processingCtx, version.Object,
			newProgressWriter(w.db, version.ID, operationID),
			sink,
		)
	} else if reporting, ok := w.process.(ProgressProcessor); ok {
		result, processErr = reporting.TranscodeWithProgress(
			processingCtx, version.Object, newProgressWriter(w.db, version.ID, operationID),
		)
	} else {
		result, processErr = w.process.Transcode(processingCtx, version.Object)
	}
	if processErr != nil {
		return w.failTranscode(ctx, version.ID, operationID, processErr)
	}
	if result.OperationID != "" && result.OperationID != operationID {
		return w.failTranscode(ctx, version.ID, operationID, errors.New("processor operation identity mismatch"))
	}
	// For non-progressive test doubles that did not invoke the sink during transcoding,
	// persist the verified renditions progressively now.
	if _, ok := w.process.(ProgressiveProcessor); !ok {
		for _, rendition := range result.Renditions {
			if err := w.PersistVerifiedRendition(processingCtx, version.ID, operationID, rendition); err != nil {
				return w.failTranscode(ctx, version.ID, operationID, err)
			}
		}
	}
	if err := validateTranscodeCompletion(assetVersionID, operationID, result); err != nil {
		return w.failTranscode(ctx, version.ID, operationID, err)
	}
	return w.CompleteTranscode(ctx, version.ID, operationID, result)
}

func (w *Worker) failTranscode(ctx context.Context, assetVersionID, operationID string, cause error) error {
	if errors.Is(cause, context.Canceled) {
		return cause
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return w.recordProcessingFailure(persistCtx, assetVersionID, operationID, cause)
}

func (w *Worker) beginTranscode(ctx context.Context, assetVersionID, operationID string) (versionRecord, bool, error) {
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
	// The row lock is held by the SELECT ... FOR UPDATE above, so this question
	// is answered against a snapshot that includes everything the previous lock
	// holder committed — including a recovery pass that has just superseded this
	// operation and queued a replacement. Asking before the lock, or asking in
	// the same statement that takes it, would let a stale answer through.
	spent, err := processingOperationSpentLocked(ctx, tx, assetVersionID, operationID)
	if err != nil {
		return versionRecord{}, false, err
	}
	if spent {
		// A harmless refusal: no state change, no lease, no attempt consumed.
		return version, false, nil
	}
	// The claim also opens this attempt's progress observation. Resetting it to
	// zero under a fresh operation identity is what makes a retry start from
	// zero rather than inheriting a previous attempt's abandoned percentage.
	//
	// The lease window is generated by PostgreSQL, not by w.now(). A lease
	// created from the worker's clock and later judged against the database's
	// clock is only as correct as the skew between them: a worker running ahead
	// would mint leases that are already expired to the database, and one
	// running behind would mint leases the database still honours long after the
	// worker believed they had lapsed. Both ends of the lease lifecycle now read
	// the same clock.
	// The operation-identity fence (D-103 H1). State and safety evidence alone
	// cannot tell a first delivery apart from a redelivery of an operation that
	// recovery has already superseded: recovery returns the row to VALIDATED or
	// QUARANTINED, which is exactly the shape the claim predicate accepts. A
	// terminal `processing_attempts` row is the authoritative, already-durable
	// record that an operation identity has been spent — recovery, failure, and
	// completion all write one — so requiring its absence inside the claiming
	// UPDATE makes reacquisition unrepresentable rather than merely unlikely.
	// It is evaluated by PostgreSQL in the same statement that takes the row,
	// under the FOR UPDATE lock above, so it cannot be raced; it deliberately
	// does not depend on any queue or broker deduplication.
	claimed, err := tx.Exec(ctx, `
		UPDATE media_asset_versions
		SET state = 'PROCESSING',
		    processing_stage = 'TRANSCODING',
		    processing_progress_percent = 0,
		    processing_updated_at = now(),
		    processing_attempt_token = $3,
		    active_processing_attempt_kind = 'FULL',
		    work_claim_token = $3,
		    work_claimed_at = now(),
		    work_lease_expires_at = now() + make_interval(secs => $4),
		    processing_attempt_count = processing_attempt_count + 1,
		    last_failure_category = NULL
		WHERE id = $1::uuid AND state = $2::media_asset_version_state
		  AND (successful_scan_attempt_id IS NOT NULL OR successful_validation_attempt_id IS NOT NULL)
		  AND NOT EXISTS (
		      SELECT 1 FROM processing_attempts pa
		      WHERE pa.asset_version_id = media_asset_versions.id
		        AND pa.operation_id = $3
		  )
	`, assetVersionID, version.State, operationID, w.workLeaseDuration.Seconds())
	if err != nil {
		return versionRecord{}, false, fmt.Errorf("claiming media version for transcode: %w", err)
	}
	if claimed.RowsAffected() != 1 {
		return version, false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return versionRecord{}, false, fmt.Errorf("committing transcode claim: %w", err)
	}
	version.Object.ProcessingOperationID = operationID
	return version, true, nil
}

// CompleteTranscode is the provider/callback idempotency boundary. Replaying
// the same operation returns success without adding a version, duration, or
// rendition; conflicting evidence is rejected.
func (w *Worker) CompleteTranscode(ctx context.Context, assetVersionID, operationID string, result TranscodeResult) error {
	if err := validateTranscodeCompletion(assetVersionID, operationID, result); err != nil {
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

func validateTranscodeCompletion(assetVersionID, operationID string, result TranscodeResult) error {
	if strings.TrimSpace(operationID) == "" {
		return fmt.Errorf("%w: transcode operation ID is required", ErrValidation)
	}
	if result.TrustedDurationMS <= 0 || len(result.Renditions) == 0 || strings.TrimSpace(result.OutputPrefix) == "" {
		return fmt.Errorf("%w: transcode output is not successful", ErrValidation)
	}
	if len(result.ExpectedRenditions) == 0 {
		return fmt.Errorf("%w: transcode expected rendition plan is required", ErrValidation)
	}
	if len(result.Renditions) != len(result.ExpectedRenditions) {
		return fmt.Errorf("%w: transcode rendition count (%d) does not match expected ladder count (%d)", ErrValidation, len(result.Renditions), len(result.ExpectedRenditions))
	}
	expectedPrefix := processingOutputPrefix(assetVersionID, operationID)
	if result.OutputPrefix != expectedPrefix {
		return fmt.Errorf("%w: transcode output prefix is not scoped to this attempt (got %q, want %q)", ErrValidation, result.OutputPrefix, expectedPrefix)
	}
	for i, rendition := range result.Renditions {
		if rendition.Name != result.ExpectedRenditions[i] {
			return fmt.Errorf("%w: rendition at index %d (%q) does not match expected ladder (%q)", ErrValidation, i, rendition.Name, result.ExpectedRenditions[i])
		}
		if strings.TrimSpace(rendition.Name) == "" || strings.TrimSpace(rendition.StorageObjectKey) == "" {
			return fmt.Errorf("%w: rendition identity is incomplete", ErrValidation)
		}
		if rendition.StorageObjectKey != expectedPrefix+"/"+rendition.Name+"/playlist.m3u8" {
			return fmt.Errorf("%w: rendition object key is not scoped to this attempt", ErrValidation)
		}
		if _, err := parseRenditionID(rendition.Name); err != nil {
			return fmt.Errorf("%w: rendition identity is invalid", ErrValidation)
		}
		if rendition.Width <= 0 || rendition.Height <= 0 || rendition.BitrateKbps <= 0 || rendition.DurationMS <= 0 {
			return fmt.Errorf("%w: rendition metadata must be positive", ErrValidation)
		}
	}
	return nil
}

// PersistVerifiedRendition durably persists one verified progressive rendition in
// PostgreSQL. For the first rendition on a PROCESSING asset, it atomically
// inserts the canonical video_renditions row and transitions the asset to PLAYABLE
// while retaining the existing work claim and lease. For subsequent renditions, it
// verifies the asset remains PLAYABLE under the same valid claim and inserts the row
// exact-idempotently.
func (w *Worker) PersistVerifiedRendition(ctx context.Context, assetVersionID, operationID string, rendition Rendition) error {
	if strings.TrimSpace(assetVersionID) == "" {
		return fmt.Errorf("%w: asset version ID is required", ErrValidation)
	}
	if _, err := uuid.Parse(assetVersionID); err != nil {
		return fmt.Errorf("%w: invalid asset version UUID %q", ErrValidation, assetVersionID)
	}
	if strings.TrimSpace(operationID) == "" {
		return fmt.Errorf("%w: operation ID is required", ErrValidation)
	}
	if strings.TrimSpace(rendition.Name) == "" {
		return fmt.Errorf("%w: rendition name is required", ErrValidation)
	}
	if _, err := parseRenditionID(rendition.Name); err != nil {
		return fmt.Errorf("%w: invalid rendition name %q", ErrValidation, rendition.Name)
	}
	if rendition.Width <= 0 || rendition.Height <= 0 || rendition.BitrateKbps <= 0 || rendition.DurationMS <= 0 {
		return fmt.Errorf("%w: rendition metadata must be positive (width=%d, height=%d, bitrate=%d, duration=%d)",
			ErrValidation, rendition.Width, rendition.Height, rendition.BitrateKbps, rendition.DurationMS)
	}
	expectedPrefix := processingOutputPrefix(assetVersionID, operationID)
	expectedKey := expectedPrefix + "/" + rendition.Name + "/playlist.m3u8"
	if rendition.StorageObjectKey != expectedKey {
		return fmt.Errorf("%w: rendition storage key %q does not match canonical key %q", ErrValidation, rendition.StorageObjectKey, expectedKey)
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning verified rendition transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var state AssetVersionState
	var claimToken *string
	var claimedAt, expiresAt *time.Time
	var scanEvidence, validationEvidence *string
	var leaseValid bool

	err = tx.QueryRow(ctx, `
		SELECT state, work_claim_token, work_claimed_at, work_lease_expires_at,
		       successful_scan_attempt_id::text, successful_validation_attempt_id::text,
		       COALESCE(work_lease_expires_at > now(), false)
		FROM media_asset_versions
		WHERE id = $1::uuid
		FOR UPDATE
	`, assetVersionID).Scan(&state, &claimToken, &claimedAt, &expiresAt, &scanEvidence, &validationEvidence, &leaseValid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("locking asset version for rendition persistence: %w", err)
	}

	// 1. Claim token check
	if claimToken == nil || *claimToken != operationID {
		return ErrConcurrentModification
	}
	// 2. Lease check
	if !leaseValid {
		return ErrLeaseExpired
	}
	// 3. Claim coherence check
	if claimedAt == nil || expiresAt == nil || !expiresAt.After(*claimedAt) {
		return ErrConcurrentModification
	}
	// 4. Provenance check
	if scanEvidence == nil && validationEvidence == nil {
		return fmt.Errorf("%w: transcode target lacks successful scan or validation evidence", ErrConflict)
	}
	// 5. Operation spent check
	spent, err := processingOperationSpentLocked(ctx, tx, assetVersionID, operationID)
	if err != nil {
		return err
	}
	if spent {
		return ErrConcurrentModification
	}

	// 6. State check: must be PROCESSING or PLAYABLE
	if state != StateProcessing && state != StatePlayable {
		return fmt.Errorf("%w: cannot persist rendition in state %s", ErrConflict, state)
	}

	// 7. Check if row already exists in video_renditions
	var existingKey string
	var existingWidth, existingHeight, existingBitrate int
	var existingDuration int64
	// Schema 41 provenance. NULL means the row predates the column, which is
	// true of every rendition written before 0041 and of nothing written after
	// it; a legacy row is therefore compared on its bytes alone, exactly as it
	// was before this column existed.
	var existingOperationID *string
	err = tx.QueryRow(ctx, `
		SELECT storage_object_key, COALESCE(width, 0), COALESCE(height, 0),
		       COALESCE(bitrate_kbps, 0), duration_ms, processing_operation_id
		FROM video_renditions
		WHERE asset_version_id = $1::uuid AND name = $2
	`, assetVersionID, rendition.Name).Scan(&existingKey, &existingWidth, &existingHeight, &existingBitrate, &existingDuration, &existingOperationID)

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("checking existing rendition: %w", err)
	}

	rowExists := (err == nil)
	if rowExists {
		// Idempotency / conflict check
		exactMatch := existingKey == rendition.StorageObjectKey &&
			existingWidth == rendition.Width &&
			existingHeight == rendition.Height &&
			existingBitrate == rendition.BitrateKbps &&
			existingDuration == rendition.DurationMS &&
			strings.HasPrefix(existingKey, expectedPrefix+"/") &&
			// Recorded provenance must agree when it exists. The storage key
			// already pins the operation through its hashed prefix, so this can
			// only disagree if the two records of the same fact contradict each
			// other, which is a conflict rather than a replay.
			(existingOperationID == nil || *existingOperationID == operationID)

		if !exactMatch {
			return fmt.Errorf("%w: conflicting video_renditions row already exists for %s", ErrConflict, rendition.Name)
		}

		// Row already exists with exact match. If state is PROCESSING, promote to PLAYABLE atomically.
		if state == StateProcessing {
			cmd, err := tx.Exec(ctx, `
				UPDATE media_asset_versions
				SET state = 'PLAYABLE',
				    processing_updated_at = now()
				WHERE id = $1::uuid
				  AND state = 'PROCESSING'
				  AND work_claim_token = $2
				  AND work_lease_expires_at > now()
			`, assetVersionID, operationID)
			if err != nil {
				return fmt.Errorf("promoting asset to PLAYABLE on idempotent replay: %w", err)
			}
			if cmd.RowsAffected() != 1 {
				return ErrConcurrentModification
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing idempotent rendition callback: %w", err)
		}
		return nil
	}

	// Row does not exist. Insert it, recording the operation that is committing
	// it. The column is write-only at this version: nothing reads it to make a
	// delivery, readiness, or publication decision, and no recovery producer
	// exists yet. It is written now so that when one does exist, the canonical
	// rows it must reason about already carry their own provenance instead of
	// being inferred from a hashed storage prefix.
	if _, err := tx.Exec(ctx, `
		INSERT INTO video_renditions (
			asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms,
			processing_operation_id
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
	`, assetVersionID, rendition.Name, rendition.StorageObjectKey, rendition.Width, rendition.Height, rendition.BitrateKbps, rendition.DurationMS, operationID); err != nil {
		return fmt.Errorf("inserting video rendition: %w", err)
	}

	// If state == PROCESSING, promote to PLAYABLE atomically in this same transaction.
	if state == StateProcessing {
		cmd, err := tx.Exec(ctx, `
			UPDATE media_asset_versions
			SET state = 'PLAYABLE',
			    processing_updated_at = now()
			WHERE id = $1::uuid
			  AND state = 'PROCESSING'
			  AND work_claim_token = $2
			  AND work_lease_expires_at > now()
		`, assetVersionID, operationID)
		if err != nil {
			return fmt.Errorf("promoting asset to PLAYABLE: %w", err)
		}
		if cmd.RowsAffected() != 1 {
			return ErrConcurrentModification
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing rendition persistence: %w", err)
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
	if err := requireProcessingProvenance(ctx, tx, completion.assetVersionID, completion.operationID); err != nil {
		return err
	}
	if err := verifyStrictRenditionsLocked(ctx, tx, completion); err != nil {
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
	// READY closes the observation at 100 in the same statement that makes the
	// version deliverable, so no reader ever sees a READY asset still reporting
	// a partial percentage — and the row is immutable from here on.
	commandTag, err := tx.Exec(ctx, `
		UPDATE media_asset_versions
		SET trusted_duration_ms = $1, successful_processing_attempt_id = $2::uuid, state = 'READY',
		    processing_stage = 'PACKAGING',
		    processing_progress_percent = 100,
		    processing_updated_at = now(),
		    processing_attempt_token = $4,
		    active_processing_attempt_kind = NULL,
		    work_claim_token = NULL,
		    work_claimed_at = NULL,
		    work_lease_expires_at = NULL,
		    last_failure_category = NULL
		WHERE id = $3::uuid AND state = 'PLAYABLE'
		  AND work_claim_token = $4
		  AND active_processing_attempt_kind = 'FULL'
		  AND work_lease_expires_at > now()
		  AND (successful_scan_attempt_id IS NOT NULL OR successful_validation_attempt_id IS NOT NULL)
	`, completion.result.TrustedDurationMS, attemptID, completion.assetVersionID, completion.operationID)
	if err != nil {
		return fmt.Errorf("marking media asset ready: %w", err)
	}
	// A stale worker must fail the whole transaction, including its callback
	// receipt, attempt, and rendition rows. Otherwise a later legitimate retry
	// could be mistaken for an already-applied completion.
	if commandTag.RowsAffected() != 1 {
		return ErrConcurrentModification
	}
	return nil
}

// requireProcessingProvenance refuses to record a successful processing result
// for a version that is not in PLAYABLE or that holds neither legitimate
// safety evidence. It accepts either provenance without confusing them: the two
// columns stay distinct, and nothing here writes or reads one as the other.
func requireProcessingProvenance(ctx context.Context, tx pgx.Tx, assetVersionID, operationID string) error {
	var state AssetVersionState
	var scanEvidence, validationEvidence *string
	var claimToken *string
	var leaseValid bool
	// The lease comparison is made by PostgreSQL against its own clock in this
	// transaction. Reading the timestamp out and comparing it in Go would make
	// the worker's own clock the authority, which is precisely the drift this
	// fence exists to remove.
	if err := tx.QueryRow(ctx, `
		SELECT state, successful_scan_attempt_id::text, successful_validation_attempt_id::text, work_claim_token,
		       COALESCE(work_lease_expires_at > now(), false)
		FROM media_asset_versions WHERE id = $1::uuid FOR UPDATE
	`, assetVersionID).Scan(&state, &scanEvidence, &validationEvidence, &claimToken, &leaseValid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("loading transcode target: %w", err)
	}
	if state != StatePlayable {
		return fmt.Errorf("%w: transcode target is not PLAYABLE (state=%s)", ErrConflict, state)
	}
	if scanEvidence == nil && validationEvidence == nil {
		return fmt.Errorf("%w: transcode target lacks successful scan or validation evidence", ErrConflict)
	}
	if claimToken == nil || *claimToken != operationID {
		return ErrConcurrentModification
	}
	// A stale token and an expired lease are independently sufficient to refuse.
	if !leaseValid {
		return ErrLeaseExpired
	}
	// The row lock is held by the SELECT ... FOR UPDATE above. A spent identity
	// cannot finalize even if it somehow still matched the token.
	spent, err := processingOperationSpentLocked(ctx, tx, assetVersionID, operationID)
	if err != nil {
		return err
	}
	if spent {
		return ErrConcurrentModification
	}
	return nil
}

type persistedRendition struct {
	storageObjectKey string
	width            int
	height           int
	bitrateKbps      int
	durationMS       int64
}

func verifyStrictRenditionsLocked(ctx context.Context, tx pgx.Tx, completion transcodeCompletion) error {
	rows, err := tx.Query(ctx, `
		SELECT name, storage_object_key, COALESCE(width, 0), COALESCE(height, 0),
		       COALESCE(bitrate_kbps, 0), duration_ms
		FROM video_renditions
		WHERE asset_version_id = $1::uuid
	`, completion.assetVersionID)
	if err != nil {
		return fmt.Errorf("querying persisted video renditions: %w", err)
	}
	defer rows.Close()

	persisted := make(map[string]persistedRendition)
	for rows.Next() {
		var name string
		var r persistedRendition
		if err := rows.Scan(&name, &r.storageObjectKey, &r.width, &r.height, &r.bitrateKbps, &r.durationMS); err != nil {
			return fmt.Errorf("scanning video rendition: %w", err)
		}
		persisted[name] = r
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating video renditions: %w", err)
	}

	// Check E: The expected rendition-name set is EXACT (count matches)
	if len(persisted) != len(completion.result.ExpectedRenditions) {
		return fmt.Errorf("%w: persisted rendition count %d does not match expected ladder count %d",
			ErrConflict, len(persisted), len(completion.result.ExpectedRenditions))
	}

	expectedMap := make(map[string]bool, len(completion.result.ExpectedRenditions))
	for _, name := range completion.result.ExpectedRenditions {
		expectedMap[name] = true
		// Check F: Every expected video_renditions row exists
		if _, ok := persisted[name]; !ok {
			return fmt.Errorf("%w: missing expected rendition %q in database", ErrConflict, name)
		}
	}

	// Check G: No unexpected/rogue rendition names exist
	for name := range persisted {
		if !expectedMap[name] {
			return fmt.Errorf("%w: unexpected rogue rendition %q in database", ErrConflict, name)
		}
	}

	expectedPrefix := processingOutputPrefix(completion.assetVersionID, completion.operationID)

	// Check H & I: Every storage key belongs to current operation prefix and DB row metadata matches verified final processor result
	for _, resultRendition := range completion.result.Renditions {
		dbRow, ok := persisted[resultRendition.Name]
		if !ok {
			return fmt.Errorf("%w: missing DB row for rendition %q", ErrConflict, resultRendition.Name)
		}
		canonicalKey := expectedPrefix + "/" + resultRendition.Name + "/playlist.m3u8"
		if dbRow.storageObjectKey != canonicalKey {
			return fmt.Errorf("%w: DB rendition %q storage key %q does not match canonical operation prefix %q",
				ErrConflict, resultRendition.Name, dbRow.storageObjectKey, canonicalKey)
		}
		if resultRendition.StorageObjectKey != canonicalKey {
			return fmt.Errorf("%w: result rendition %q storage key %q does not match canonical operation prefix %q",
				ErrConflict, resultRendition.Name, resultRendition.StorageObjectKey, canonicalKey)
		}
		if dbRow.width != resultRendition.Width {
			return fmt.Errorf("%w: rendition %q width mismatch (db=%d, result=%d)", ErrConflict, resultRendition.Name, dbRow.width, resultRendition.Width)
		}
		if dbRow.height != resultRendition.Height {
			return fmt.Errorf("%w: rendition %q height mismatch (db=%d, result=%d)", ErrConflict, resultRendition.Name, dbRow.height, resultRendition.Height)
		}
		if dbRow.bitrateKbps != resultRendition.BitrateKbps {
			return fmt.Errorf("%w: rendition %q bitrate mismatch (db=%d, result=%d)", ErrConflict, resultRendition.Name, dbRow.bitrateKbps, resultRendition.BitrateKbps)
		}
		if dbRow.durationMS != resultRendition.DurationMS {
			return fmt.Errorf("%w: rendition %q duration mismatch (db=%d, result=%d)", ErrConflict, resultRendition.Name, dbRow.durationMS, resultRendition.DurationMS)
		}
	}

	return nil
}

func (w *Worker) recordProcessingFailure(ctx context.Context, assetVersionID, operationID string, cause error) error {
	if errors.Is(cause, context.Canceled) {
		// Shutdown may interrupt the process after the durable claim. Leave the
		// lease in place; the recovery loop will distinguish it from live work.
		return cause
	}
	failure := processingFailure{
		assetVersionID: assetVersionID, operationID: operationID,
		cause: cause, category: processingFailureCategory(cause),
	}
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning processing failure: %w", err)
	}
	defer tx.Rollback(ctx)
	// The same operation-identity fence as the claim boundary, applied before
	// this attempt writes its own terminal row. Without it a redelivered stale
	// operation could fail a SCAN_PASSED or VALIDATED row that its replacement
	// has not claimed yet, stranding the replacement; the claim predicate's
	// NOT EXISTS cannot be reused here because this transaction is about to
	// insert exactly the row it would test for.
	fenced, err := processingOperationSpent(ctx, tx, failure.assetVersionID, failure.operationID)
	if err != nil {
		return err
	}
	if fenced {
		return nil
	}
	if err := recordFailedProcessingAttempt(ctx, tx, failure); err != nil {
		return err
	}
	applied, err := markProcessingFailed(ctx, tx, failure)
	if err != nil {
		return err
	}
	if !applied {
		// Recovery or another attempt already took ownership; this stale result
		// rolls back its evidence instead of changing the newer state.
		return nil
	}
	if failure.category == failureStorageUnavailable {
		scheduled, err := w.scheduleProcessingRetry(ctx, tx, failure)
		if err != nil {
			return err
		}
		if scheduled {
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("committing processing retry: %w", err)
			}
			_, _ = w.CleanupAttemptSafe(ctx, failure.assetVersionID, failure.operationID)
			return fmt.Errorf("%w: %v", ErrRetryScheduled, cause)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing processing failure: %w", err)
	}
	_, _ = w.CleanupAttemptSafe(ctx, failure.assetVersionID, failure.operationID)
	return cause
}

// processingOperationSpent reports whether this exact operation identity has
// already been recorded terminal for this asset version.
//
// The invariant it enforces: once a processing operation ID has been recorded
// terminal — superseded by recovery, failed, or completed — that exact ID may
// never again acquire processing authority over that asset version. The version
// row is locked first so the answer cannot change under a concurrent claim.
func processingOperationSpent(ctx context.Context, tx pgx.Tx, assetVersionID, operationID string) (bool, error) {
	if err := lockAssetVersion(ctx, tx, assetVersionID); err != nil {
		return false, err
	}
	return processingOperationSpentLocked(ctx, tx, assetVersionID, operationID)
}

// lockAssetVersion takes the authoritative row lock and nothing else.
//
// It is a statement of its own on purpose. A previous revision of this fence
// combined the lock and the spent check into one statement:
//
//	SELECT EXISTS (SELECT 1 FROM processing_attempts WHERE ...)
//	FROM media_asset_versions WHERE id = $1 FOR UPDATE
//
// PostgreSQL is free to evaluate that uncorrelated EXISTS as an InitPlan
// *before* the scan that takes FOR UPDATE, so the answer could be computed
// against a snapshot older than the lock. That reopened exactly the race the
// fence exists to close: operation A reads "not spent", blocks on the lock,
// recovery takes the lock, records A terminal, creates replacement B, commits,
// and A then proceeds on its stale pre-lock answer and mutates B's state.
//
// Splitting the statements removes the dependence on planner behaviour. After
// this call returns, the row lock is held, so any query issued afterwards in
// this transaction sees a snapshot that includes every committed change made by
// whoever held the lock before us.
func lockAssetVersion(ctx context.Context, tx pgx.Tx, assetVersionID string) error {
	var locked string
	if err := tx.QueryRow(ctx, `
		SELECT id::text FROM media_asset_versions WHERE id = $1::uuid FOR UPDATE
	`, assetVersionID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("locking media asset version: %w", err)
	}
	return nil
}

// processingOperationSpentLocked answers the spent question. The caller MUST
// already hold the asset-version row lock; this runs as its own statement after
// that lock so the result cannot predate it.
//
// INVARIANT: once (asset_version_id, operation_id) is recorded terminal, that
// exact operation ID may never again acquire or mutate processing authority for
// that asset version. The decision is made by the database, after the
// authoritative lock, and never by queue deduplication, broker delivery
// guarantees, or application-level synchronization.
func processingOperationSpentLocked(ctx context.Context, tx pgx.Tx, assetVersionID, operationID string) (bool, error) {
	var spent bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM processing_attempts
		    WHERE asset_version_id = $1::uuid AND operation_id = $2
		)
	`, assetVersionID, operationID).Scan(&spent); err != nil {
		return false, fmt.Errorf("checking superseded processing operation: %w", err)
	}
	return spent, nil
}

type processingFailure struct {
	assetVersionID string
	operationID    string
	cause          error
	category       failureCategory
	attemptKind    string
}

func recordFailedProcessingAttempt(ctx context.Context, tx pgx.Tx, failure processingFailure) error {
	reason := truncateMediaOutput(failure.cause.Error(), 2000)
	attemptKind := failure.attemptKind
	if attemptKind == "" {
		attemptKind = "FULL"
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO processing_attempts (
			asset_version_id, operation_id, state, attempt_kind, rendition_count, error_reason
		) VALUES ($1::uuid, $2, 'FAILED', $3::media_processing_attempt_kind, 0, $4)
		ON CONFLICT (asset_version_id, operation_id) DO NOTHING
	`, failure.assetVersionID, failure.operationID, attemptKind, reason); err != nil {
		return fmt.Errorf("recording processing failure: %w", err)
	}
	return nil
}

func markProcessingFailed(ctx context.Context, tx pgx.Tx, failure processingFailure) (bool, error) {
	// A PROCESSING row may only be failed by the identity that currently holds a
	// live lease on it. Once the lease has expired the row belongs to recovery,
	// so an expired worker's failure is refused here rather than racing it.
	commandTag, err := tx.Exec(ctx, `
		UPDATE media_asset_versions SET
		    state = CASE WHEN state = 'PLAYABLE' THEN 'PLAYABLE'::media_asset_version_state ELSE 'PROCESS_FAILED'::media_asset_version_state END,
		    work_claim_token = NULL, work_claimed_at = NULL, work_lease_expires_at = NULL,
		    active_processing_attempt_kind = NULL,
		    last_failure_category = $3
		WHERE id = $1::uuid AND state IN ('PROCESSING', 'PLAYABLE', 'SCAN_PASSED', 'VALIDATED')
		  AND (
		        (state IN ('PROCESSING', 'PLAYABLE') AND work_claim_token = $2 AND work_lease_expires_at > now())
		     OR state NOT IN ('PROCESSING', 'PLAYABLE')
		  )
	`, failure.assetVersionID, failure.operationID, failure.category)
	if err != nil {
		return false, fmt.Errorf("marking processing failure: %w", err)
	}
	return commandTag.RowsAffected() == 1, nil
}

type processingRetryTarget struct {
	attempts int
	kind     AssetKind
	trusted  bool
}

func (w *Worker) scheduleProcessingRetry(ctx context.Context, tx pgx.Tx, failure processingFailure) (bool, error) {
	var target processingRetryTarget
	if err := tx.QueryRow(ctx, `
		SELECT processing_attempt_count,kind,successful_validation_attempt_id IS NOT NULL
		FROM media_asset_versions WHERE id=$1::uuid
	`, failure.assetVersionID).Scan(&target.attempts, &target.kind, &target.trusted); err != nil {
		return false, fmt.Errorf("loading processing retry budget: %w", err)
	}
	if target.attempts >= MaxWorkAttempts {
		return false, nil
	}
	commandTag, err := tx.Exec(ctx, `
		UPDATE media_asset_versions SET state='QUARANTINED',
		  processing_stage=NULL,processing_progress_percent=NULL,processing_updated_at=NULL,processing_attempt_token=NULL
		WHERE id=$1::uuid AND state='PROCESS_FAILED'
	`, failure.assetVersionID)
	if err != nil {
		return false, fmt.Errorf("resetting transient processing failure: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return false, nil
	}
	base, err := databaseNow(ctx, tx)
	if err != nil {
		return false, err
	}
	availableAt := base.Add(retryBackoff(target.attempts))
	if target.trusted {
		return true, w.scheduleValidatedTranscode(ctx, tx, failure.assetVersionID, availableAt)
	}
	return true, appendScanWorkAt(ctx, tx, w.outbox, workSchedule{
		assetVersionID: failure.assetVersionID, kind: target.kind,
		correlation: "automatic-processing-rescan", availableAt: &availableAt,
	})
}

func (w *Worker) scheduleValidatedTranscode(ctx context.Context, tx pgx.Tx, assetVersionID string, availableAt time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state='VALIDATED' WHERE id=$1::uuid AND state='QUARANTINED'`, assetVersionID); err != nil {
		return fmt.Errorf("restoring immutable validation provenance: %w", err)
	}
	return appendTranscodeWorkAt(ctx, tx, w.outbox, workSchedule{
		assetVersionID: assetVersionID, correlation: "automatic-processing-retry", availableAt: &availableAt,
	})
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
