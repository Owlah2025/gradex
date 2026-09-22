//go:build integration

package media

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// D-103 M2. The RUNBOOK claimed that an Admin Retry recovers work left in
// SCANNING or PROCESSING by a 0035 rollback. It does not: Retry accepts only
// SCAN_FAILED, SCAN_ERROR, and PROCESS_FAILED, and a row stranded mid-flight is
// in none of them. Rolling back 0035 also drops the lease columns, so the D-103
// recovery pass is gone too — under D-102 binaries nothing at all reclaims those
// rows, and the asset is stuck until someone touches the database by hand.
//
// These tests pin down what is actually true, and prove the real supported
// procedure the RUNBOOK now documents: the supervised rollback transaction
// settles any still-in-flight row into its Retry-eligible terminal state, and
// Admin Retry recovers it from there.
//
// Retry's own state validation is deliberately NOT relaxed. The first and last
// cases exist to keep it that way.
//
// Every in-flight state here is reached through the real claim path rather than
// written directly, because the 0012/0031/0033 transition trigger rejects
// invented edges — which is also why the settle statement below is safe: it
// takes only SCANNING -> SCAN_ERROR and PROCESSING -> PROCESS_FAILED, two edges
// the state machine already permits.

func retryRequestFor(f *mediaFixture, assetVersionID string) RetryRequest {
	return RetryRequest{
		AssetVersionID:  assetVersionID,
		AdminAccountID:  f.adminID,
		ActorDescriptor: "rollback-recovery-integration",
	}
}

// scanningAsset leaves a freshly uploaded video claimed for scanning.
func scanningAsset(t *testing.T) (*mediaFixture, *Worker, string) {
	t.Helper()
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	now := time.Now().UTC()
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(successfulAttemptProcessor))
	workID := scanWorkID(t, f.pool, request.AssetVersionID, 0)
	if _, _, applied, err := worker.beginScan(f.ctx, request.AssetVersionID, workID); err != nil || !applied {
		t.Fatalf("beginScan applied=%t err=%v", applied, err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateScanning {
		t.Fatalf("state = %s, want SCANNING", got)
	}
	return f, worker, request.AssetVersionID
}

// processingAsset leaves a scanned video claimed for processing.
//
// It deliberately reaches PROCESSING through the scan path rather than through
// trusted validation, because this fixture is a scanner-mode deployment. An
// Asset Version carrying D-088 trusted-validation provenance is only retryable
// where the trusted path still applies — see
// TestRollbackRecoveryTrustedProvenanceIsNotRetryableInAScannerDeployment,
// which pins that limitation down, and the RUNBOOK, which records it.
func processingAsset(t *testing.T) (*mediaFixture, *Worker, string) {
	t.Helper()
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	now := time.Now().UTC()
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(successfulAttemptProcessor))
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	operation := transcodeOperationID(t, f, request.AssetVersionID, 0)
	if _, applied, err := worker.beginTranscode(f.ctx, request.AssetVersionID, operation); err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateProcessing {
		t.Fatalf("state = %s, want PROCESSING", got)
	}
	return f, worker, request.AssetVersionID
}

func inFlightAsset(t *testing.T, state AssetVersionState) (*mediaFixture, *Worker, string) {
	t.Helper()
	if state == StateScanning {
		return scanningAsset(t)
	}
	return processingAsset(t)
}

// settleInFlightWorkForRollback is the exact statement the RUNBOOK's supervised
// 0035 rollback runs, transcribed here so the documented procedure is executed
// by a test rather than asserted by prose. It lands every still-in-flight row in
// the terminal state its own path would have reached, which is precisely the set
// Admin Retry accepts.
//
// It runs BEFORE the 0035 down SQL, in its own supervised transaction (RUNBOOK
// step 4, gated by the verification in step 5), and clears the lease columns in
// the same statement. Both details are load-bearing: settling after the drop
// could not reference those columns at all, and leaving them populated on a row
// that is no longer SCANNING or PROCESSING violates the 0035
// media_asset_versions_work_claim_coherent constraint, which is still in force
// at that point.
func settleInFlightWorkForRollback(t *testing.T, f *mediaFixture) int64 {
	t.Helper()
	commandTag, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET state = CASE state
		        WHEN 'SCANNING' THEN 'SCAN_ERROR'
		        WHEN 'PROCESSING' THEN 'PROCESS_FAILED'
		    END::media_asset_version_state,
		    processing_stage = NULL,
		    processing_progress_percent = NULL,
		    processing_updated_at = NULL,
		    processing_attempt_token = NULL,
		    active_processing_attempt_kind = NULL,
		    work_claim_token = NULL,
		    work_claimed_at = NULL,
		    work_lease_expires_at = NULL,
		    last_failure_category = 'WORKER_INTERRUPTED'
		WHERE state IN ('SCANNING', 'PROCESSING')
	`)
	if err != nil {
		t.Fatalf("settling in-flight media work: %v", err)
	}
	return commandTag.RowsAffected()
}

func TestRollbackRecoveryRetryRefusesWorkStillInFlight(t *testing.T) {
	for _, inFlight := range []AssetVersionState{StateScanning, StateProcessing} {
		t.Run(string(inFlight), func(t *testing.T) {
			f, _, versionID := inFlightAsset(t, inFlight)
			err := f.service.Retry(f.ctx, retryRequestFor(f, versionID))
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("Retry from %s error = %v, want ErrConflict — Retry must not accept in-flight work", inFlight, err)
			}
			if got := mediaState(t, f.pool, versionID); got != inFlight {
				t.Fatalf("state after refused retry = %s, want %s unchanged", got, inFlight)
			}
		})
	}
}

// TestRollbackRecoverySettledWorkIsRetryable proves the documented procedure end
// to end: settle, retry, and the asset re-enters its own safety path and
// genuinely reaches READY again.
func TestRollbackRecoverySettledWorkIsRetryable(t *testing.T) {
	for _, testCase := range []struct {
		inFlight AssetVersionState
		settled  AssetVersionState
	}{
		{StateScanning, StateScanError},
		{StateProcessing, StateProcessFailed},
	} {
		t.Run(string(testCase.inFlight), func(t *testing.T) {
			f, worker, versionID := inFlightAsset(t, testCase.inFlight)

			if rows := settleInFlightWorkForRollback(t, f); rows != 1 {
				t.Fatalf("settle affected %d rows, want 1", rows)
			}
			if got := mediaState(t, f.pool, versionID); got != testCase.settled {
				t.Fatalf("state after settle = %s, want %s", got, testCase.settled)
			}

			scanWorkBefore := mediaEventCount(t, f, "media.scan_requested", versionID)
			transcodeWorkBefore := mediaEventCount(t, f, "media.transcode_requested", versionID)
			if err := f.service.Retry(f.ctx, retryRequestFor(f, versionID)); err != nil {
				t.Fatalf("Retry from settled %s: %v", testCase.settled, err)
			}

			// A scan-path asset re-enters through quarantine and a fresh scan; a
			// trusted-validation asset re-enters through its own immutable
			// validation provenance. Both are the asset's real safety path.
			switch mediaState(t, f.pool, versionID) {
			case StateQuarantined:
				if got := mediaEventCount(t, f, "media.scan_requested", versionID); got != scanWorkBefore+1 {
					t.Fatalf("scan work after retry = %d, want %d", got, scanWorkBefore+1)
				}
				retryScanWork := scanWorkID(t, f.pool, versionID, scanWorkBefore)
				if err := worker.scan(f.ctx, versionID, retryScanWork, false); err != nil {
					t.Fatalf("scan after retry: %v", err)
				}
			case StateValidated:
				if got := mediaEventCount(t, f, "media.transcode_requested", versionID); got != transcodeWorkBefore+1 {
					t.Fatalf("transcode work after retry = %d, want %d", got, transcodeWorkBefore+1)
				}
			default:
				t.Fatalf("state after retry = %s, want QUARANTINED or VALIDATED", mediaState(t, f.pool, versionID))
			}

			operation := transcodeOperationID(t, f, versionID, mediaEventCount(t, f, "media.transcode_requested", versionID)-1)
			if err := worker.Transcode(f.ctx, versionID, operation); err != nil {
				t.Fatalf("transcode after retry: %v", err)
			}
			if got := mediaState(t, f.pool, versionID); got != StateReady {
				t.Fatalf("state after recovered processing = %s, want READY", got)
			}
		})
	}
}

// TestRollbackRecoveryRetryResetsTheAttemptBudget records the other half of the
// documented procedure: a recovered asset starts from a clean budget, so an
// operator is not handed an asset that can only attempt once more.
func TestRollbackRecoveryRetryResetsTheAttemptBudget(t *testing.T) {
	f, _, versionID := processingAsset(t)
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET processing_attempt_count = $2, scan_attempt_count = $2 WHERE id = $1::uuid
	`, versionID, MaxWorkAttempts); err != nil {
		t.Fatalf("exhausting the attempt budget: %v", err)
	}
	settleInFlightWorkForRollback(t, f)

	if err := f.service.Retry(f.ctx, retryRequestFor(f, versionID)); err != nil {
		t.Fatalf("Retry of an exhausted asset: %v", err)
	}
	if got := processingAttemptCount(t, f, versionID); got != 0 {
		t.Fatalf("processing attempt count after retry = %d, want 0", got)
	}
	var scanAttempts int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT scan_attempt_count FROM media_asset_versions WHERE id = $1::uuid
	`, versionID).Scan(&scanAttempts); err != nil {
		t.Fatalf("reading scan attempt count: %v", err)
	}
	if scanAttempts != 0 {
		t.Fatalf("scan attempt count after retry = %d, want 0", scanAttempts)
	}
}

// TestRollbackRecoveryRetryStillRefusesHealthyStates guards the boundary from
// the other side: settling must not become a licence to retry anything.
func TestRollbackRecoveryRetryStillRefusesHealthyStates(t *testing.T) {
	t.Run("QUARANTINED", func(t *testing.T) {
		f := newMediaFixture(t)
		request, _ := f.beginVideoUpload("object-v1")
		if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
			t.Fatalf("CompleteUpload: %v", err)
		}
		if err := f.service.Retry(f.ctx, retryRequestFor(f, request.AssetVersionID)); !errors.Is(err, ErrConflict) {
			t.Fatalf("Retry from QUARANTINED error = %v, want ErrConflict", err)
		}
	})

	t.Run("VALIDATED", func(t *testing.T) {
		f, _, _, versionID := validatedVideoReadyForProcessing(t)
		if err := f.service.Retry(f.ctx, retryRequestFor(f, versionID)); !errors.Is(err, ErrConflict) {
			t.Fatalf("Retry from VALIDATED error = %v, want ErrConflict", err)
		}
	})

	t.Run("READY", func(t *testing.T) {
		f, worker, _, versionID := validatedVideoReadyForProcessing(t)
		operation := uuid.NewString()
		if err := worker.Transcode(f.ctx, versionID, operation); err != nil {
			t.Fatalf("Transcode: %v", err)
		}
		if got := mediaState(t, f.pool, versionID); got != StateReady {
			t.Fatalf("state = %s, want READY", got)
		}
		if err := f.service.Retry(f.ctx, retryRequestFor(f, versionID)); !errors.Is(err, ErrConflict) {
			t.Fatalf("Retry from READY error = %v, want ErrConflict", err)
		}
	})
}

// TestRollbackRecoveryTrustedProvenanceIsNotRetryableInAScannerDeployment is a
// real limit on the documented procedure, not a defect: settling makes a
// stranded row Retry-eligible by STATE, but an Asset Version carrying D-088
// trusted-validation provenance still cannot re-enter its safety path in a
// deployment where the trusted path no longer applies. An operator rolling back
// 0035 needs to know that before counting on Retry to clear the backlog.
func TestRollbackRecoveryTrustedProvenanceIsNotRetryableInAScannerDeployment(t *testing.T) {
	f, worker, _, versionID := validatedVideoReadyForProcessing(t)
	if _, applied, err := worker.beginTranscode(f.ctx, versionID, uuid.NewString()); err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	if rows := settleInFlightWorkForRollback(t, f); rows != 1 {
		t.Fatalf("settle affected %d rows, want 1", rows)
	}
	if got := mediaState(t, f.pool, versionID); got != StateProcessFailed {
		t.Fatalf("state after settle = %s, want PROCESS_FAILED", got)
	}

	err := f.service.Retry(f.ctx, retryRequestFor(f, versionID))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Retry of trusted provenance in a scanner deployment error = %v, want ErrConflict", err)
	}
	// The refusal is total: the asset is left exactly as the settle found it,
	// so nothing is half-recovered.
	if got := mediaState(t, f.pool, versionID); got != StateProcessFailed {
		t.Fatalf("state after refused retry = %s, want PROCESS_FAILED unchanged", got)
	}
}

func TestRollbackRecoveryRetryRequiresAnActiveAdmin(t *testing.T) {
	f, _, versionID := processingAsset(t)
	settleInFlightWorkForRollback(t, f)

	request := retryRequestFor(f, versionID)
	request.AdminAccountID = uuid.NewString()
	if err := f.service.Retry(f.ctx, request); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Retry by an unknown account error = %v, want ErrNotAuthorized", err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateProcessFailed {
		t.Fatalf("state after unauthorized retry = %s, want PROCESS_FAILED unchanged", got)
	}
}
