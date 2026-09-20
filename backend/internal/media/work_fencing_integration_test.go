//go:build integration

package media

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// These tests cover the two D-103 review blockers that the original recovery
// suite could not reach:
//
//	H1 — a redelivered, already-superseded processing operation ID must never
//	     reacquire processing authority over its asset version.
//	H2 — every lease-protected state transition must prove, in the same database
//	     mutation that performs it, that the lease is still valid according to
//	     DATABASE time. The worker's own clock is never the authority.
//
// Both are asserted against the real claim and completion statements. Lease
// expiry is simulated by moving the stored lease window into the past, which is
// the only way to exercise a database-time predicate: advancing the injected
// application clock deliberately has no effect on it, and a test that passed by
// moving the fake clock would be proving the wrong thing.

// markValidated gives a version the immutable trusted-validation provenance that
// makes recovery re-queue processing directly, rather than sending the asset
// back through a rescan. That is the shape in which the reviewer's scenario is
// sharpest: the replacement work item is itself a processing operation.
func markValidated(t *testing.T, f *mediaFixture, assetVersionID string) {
	t.Helper()
	// Every column is sourced from the Asset Version and its upload intent. The
	// 0020 provenance trigger requires the attempt's checksum, verified size,
	// declared type, profile, object version, and configured bound all to agree
	// with the row it is attached to, so this builds genuine evidence rather
	// than a shape that merely reads like a pass.
	var attemptID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO validation_attempts (
			asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex
		)
		SELECT mav.id, 1, $2, mav.storage_object_version, 'PASSED',
		       'fencing-integration-validator', $3, mav.content_type, mav.size_bytes,
		       ui.max_size_bytes, mav.sha256_hex
		FROM media_asset_versions mav
		JOIN upload_intents ui ON ui.asset_version_id = mav.id
		WHERE mav.id = $1::uuid
		RETURNING id::text
	`, assetVersionID, uuid.NewString(), TrustedValidationProfile).Scan(&attemptID); err != nil {
		t.Fatalf("seeding validation evidence: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET successful_validation_attempt_id = $2::uuid, state = 'VALIDATED'
		WHERE id = $1::uuid
	`, assetVersionID, attemptID); err != nil {
		t.Fatalf("marking version validated: %v", err)
	}
}

// expireLease moves the stored lease window wholly into the past. work_claimed_at
// moves with it so the 0035 coherence constraint still holds.
func expireLease(t *testing.T, f *mediaFixture, assetVersionID string) {
	t.Helper()
	commandTag, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET work_claimed_at = now() - interval '2 hours',
		    work_lease_expires_at = now() - interval '1 hour'
		WHERE id = $1::uuid AND work_claim_token IS NOT NULL
	`, assetVersionID)
	if err != nil {
		t.Fatalf("expiring work lease: %v", err)
	}
	if commandTag.RowsAffected() != 1 {
		t.Fatalf("expiring work lease affected %d rows, want 1", commandTag.RowsAffected())
	}
}

// extendLease pushes the lease comfortably into the future without changing the
// claiming identity, so "still leased" can be asserted independently of "token
// matches".
func extendLease(t *testing.T, f *mediaFixture, assetVersionID string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET work_lease_expires_at = now() + interval '1 hour'
		WHERE id = $1::uuid AND work_claim_token IS NOT NULL
	`, assetVersionID); err != nil {
		t.Fatalf("extending work lease: %v", err)
	}
}

func processingAttemptCount(t *testing.T, f *mediaFixture, assetVersionID string) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT processing_attempt_count FROM media_asset_versions WHERE id = $1::uuid
	`, assetVersionID).Scan(&count); err != nil {
		t.Fatalf("reading processing attempt count: %v", err)
	}
	return count
}

func processingAttemptRows(t *testing.T, f *mediaFixture, assetVersionID string) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM processing_attempts WHERE asset_version_id = $1::uuid
	`, assetVersionID).Scan(&count); err != nil {
		t.Fatalf("counting processing attempts: %v", err)
	}
	return count
}

func claimToken(t *testing.T, f *mediaFixture, assetVersionID string) string {
	t.Helper()
	var token *string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT work_claim_token FROM media_asset_versions WHERE id = $1::uuid
	`, assetVersionID).Scan(&token); err != nil {
		t.Fatalf("reading work claim token: %v", err)
	}
	if token == nil {
		return ""
	}
	return *token
}

// validatedVideoReadyForProcessing returns a fixture, worker, and asset version
// sitting in VALIDATED with no processing attempts yet consumed.
func validatedVideoReadyForProcessing(t *testing.T) (*mediaFixture, *Worker, *time.Time, string) {
	t.Helper()
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	now := time.Now().UTC()
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(successfulAttemptProcessor))
	markValidated(t, f, request.AssetVersionID)
	return f, worker, &now, request.AssetVersionID
}

// TestD103StaleProcessingOperationIsFencedAndReplacementCompletes walks the
// exact reviewer scenario end to end: A claims, A goes stale, recovery fails A
// and enqueues B, A is redelivered, A is fenced, and B still completes.
func TestD103StaleProcessingOperationIsFencedAndReplacementCompletes(t *testing.T) {
	f, worker, now, versionID := validatedVideoReadyForProcessing(t)

	// 1. Operation A claims processing.
	operationA := uuid.NewString()
	if _, applied, err := worker.beginTranscode(f.ctx, versionID, operationA); err != nil || !applied {
		t.Fatalf("operation A beginTranscode applied=%t err=%v", applied, err)
	}
	if got := processingAttemptCount(t, f, versionID); got != 1 {
		t.Fatalf("attempt count after A claimed = %d, want 1", got)
	}

	// 2. A becomes stale. 3. Recovery records A failed and creates operation B.
	expireLease(t, f, versionID)
	*now = now.Add(3 * time.Second)
	if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
		t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateValidated {
		t.Fatalf("state after recovery = %s, want VALIDATED", got)
	}
	var recordedState string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT state::text FROM processing_attempts WHERE asset_version_id=$1::uuid AND operation_id=$2
	`, versionID, operationA).Scan(&recordedState); err != nil {
		t.Fatalf("loading operation A terminal record: %v", err)
	}
	if recordedState != "FAILED" {
		t.Fatalf("operation A recorded state = %s, want FAILED", recordedState)
	}
	operationB := transcodeOperationID(t, f, versionID, 0)
	if operationB == operationA {
		t.Fatal("recovery reused the superseded processing identity")
	}

	countBeforeRedelivery := processingAttemptCount(t, f, versionID)
	rowsBeforeRedelivery := processingAttemptRows(t, f, versionID)

	// 4. A is redelivered. 5. A is fenced: a harmless no-op, not an error that
	// would send the queue into another redelivery loop.
	if err := worker.Transcode(f.ctx, versionID, operationA); err != nil {
		t.Fatalf("redelivered stale operation A returned an error instead of a no-op: %v", err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateValidated {
		t.Fatalf("state after stale redelivery = %s, want VALIDATED unchanged", got)
	}
	if got := processingAttemptCount(t, f, versionID); got != countBeforeRedelivery {
		t.Fatalf("stale redelivery consumed the replacement's attempt budget: %d, want %d", got, countBeforeRedelivery)
	}
	if got := processingAttemptRows(t, f, versionID); got != rowsBeforeRedelivery {
		t.Fatalf("stale redelivery wrote a new attempt row: %d, want %d", got, rowsBeforeRedelivery)
	}
	if got := claimToken(t, f, versionID); got != "" {
		t.Fatalf("stale redelivery overwrote the lease token with %q, want none", got)
	}

	// 6. B can still claim and complete. 7. Attempt accounting stays correct.
	if err := worker.Transcode(f.ctx, versionID, operationB); err != nil {
		t.Fatalf("replacement operation B: %v", err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateReady {
		t.Fatalf("state after replacement = %s, want READY", got)
	}
	if got := processingAttemptCount(t, f, versionID); got != 2 {
		t.Fatalf("attempt count after replacement = %d, want 2 (A consumed one, B consumed one)", got)
	}
}

// TestD103StaleProcessingOperationCannotConsumeTheFinalAttempt drives the
// budget to exhaustion and proves a redelivered stale operation neither
// consumes an attempt on the way nor revives an exhausted asset.
func TestD103StaleProcessingOperationCannotConsumeTheFinalAttempt(t *testing.T) {
	f, worker, now, versionID := validatedVideoReadyForProcessing(t)

	operations := make([]string, 0, MaxWorkAttempts)
	operation := uuid.NewString()
	for attempt := 1; attempt <= MaxWorkAttempts; attempt++ {
		if _, applied, err := worker.beginTranscode(f.ctx, versionID, operation); err != nil || !applied {
			t.Fatalf("attempt %d beginTranscode applied=%t err=%v", attempt, applied, err)
		}
		if got := processingAttemptCount(t, f, versionID); got != attempt {
			t.Fatalf("attempt count after claim %d = %d, want %d", attempt, got, attempt)
		}
		operations = append(operations, operation)

		expireLease(t, f, versionID)
		*now = now.Add(3 * time.Second)
		if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
			t.Fatalf("attempt %d RecoverStale recovered=%d err=%v", attempt, recovered, err)
		}
		if got := processingAttemptCount(t, f, versionID); got != attempt {
			t.Fatalf("attempt count after recovery %d = %d, want %d", attempt, got, attempt)
		}

		// Every superseded identity so far stays fenced, including after later
		// attempts have come and gone.
		for _, stale := range operations {
			if err := worker.Transcode(f.ctx, versionID, stale); err != nil {
				t.Fatalf("redelivered stale operation returned an error: %v", err)
			}
		}
		if got := processingAttemptCount(t, f, versionID); got != attempt {
			t.Fatalf("stale redelivery consumed an attempt at %d: count = %d", attempt, got)
		}

		if attempt < MaxWorkAttempts {
			if got := mediaState(t, f.pool, versionID); got != StateValidated {
				t.Fatalf("state after recovery %d = %s, want VALIDATED", attempt, got)
			}
			operation = transcodeOperationID(t, f, versionID, attempt-1)
		}
	}

	// The budget is spent: the asset stops, and nothing queued a fourth attempt.
	if got := mediaState(t, f.pool, versionID); got != StateProcessFailed {
		t.Fatalf("state after exhausting the budget = %s, want PROCESS_FAILED", got)
	}
	if got := processingAttemptCount(t, f, versionID); got != MaxWorkAttempts {
		t.Fatalf("final attempt count = %d, want %d", got, MaxWorkAttempts)
	}
	if got := mediaEventCount(t, f, "media.transcode_requested", versionID); got != MaxWorkAttempts-1 {
		t.Fatalf("queued transcode work = %d, want %d replacements and no fourth attempt", got, MaxWorkAttempts-1)
	}

	// A final redelivery of the last spent identity must not revive the asset.
	for _, stale := range operations {
		if err := worker.Transcode(f.ctx, versionID, stale); err != nil {
			t.Fatalf("redelivered exhausted operation returned an error: %v", err)
		}
	}
	if got := mediaState(t, f.pool, versionID); got != StateProcessFailed {
		t.Fatalf("state after exhausted redelivery = %s, want PROCESS_FAILED", got)
	}
	if got := processingAttemptCount(t, f, versionID); got != MaxWorkAttempts {
		t.Fatalf("attempt count after exhausted redelivery = %d, want %d", got, MaxWorkAttempts)
	}
}

// TestD103ExpiredLeaseAloneRefusesProcessingCompletion is the H2 race proof: the
// worker holds the currently-recorded token, recovery has NOT run, and the
// database refuses the transition for one reason only — the lease has expired.
func TestD103ExpiredLeaseAloneRefusesProcessingCompletion(t *testing.T) {
	f, worker, _, versionID := validatedVideoReadyForProcessing(t)

	operation := uuid.NewString()
	version, applied, err := worker.beginTranscode(f.ctx, versionID, operation)
	if err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	result, _ := successfulAttemptProcessor(f.ctx, version.Object)

	expireLease(t, f, versionID)
	// Recovery has deliberately not run: the row still carries this worker's own
	// token, so the token check cannot be what refuses the completion.
	if got := claimToken(t, f, versionID); got != operation {
		t.Fatalf("claim token = %q, want the worker's own operation still recorded", got)
	}

	err = worker.CompleteTranscode(f.ctx, versionID, operation, result)
	if err == nil {
		t.Fatal("completion succeeded on an expired lease")
	}
	if got := mediaState(t, f.pool, versionID); got != StateProcessing {
		t.Fatalf("state after refused completion = %s, want PROCESSING unchanged", got)
	}
	var ready bool
	if err := f.pool.QueryRow(f.ctx, `
		SELECT EXISTS (SELECT 1 FROM video_renditions WHERE asset_version_id = $1::uuid)
	`, versionID).Scan(&ready); err != nil {
		t.Fatalf("checking renditions: %v", err)
	}
	if ready {
		t.Fatal("expired completion published renditions")
	}

	// Recovery still converges afterwards, so the refusal strands nothing.
	if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
		t.Fatalf("RecoverStale after refusal recovered=%d err=%v", recovered, err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateValidated {
		t.Fatalf("state after recovery = %s, want VALIDATED", got)
	}
}

// TestD103CompletionImmediatelyBeforeExpirySucceeds is the other side of the
// boundary: a lease that is still valid by database time completes normally.
func TestD103CompletionImmediatelyBeforeExpirySucceeds(t *testing.T) {
	f, worker, _, versionID := validatedVideoReadyForProcessing(t)

	operation := uuid.NewString()
	version, applied, err := worker.beginTranscode(f.ctx, versionID, operation)
	if err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	result, _ := successfulAttemptProcessor(f.ctx, version.Object)

	// Leave only a sliver of lease, measured by the database's own clock.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET work_lease_expires_at = now() + interval '30 seconds'
		WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("shortening work lease: %v", err)
	}
	for _, r := range result.Renditions {
		if err := worker.PersistVerifiedRendition(f.ctx, versionID, operation, r); err != nil {
			t.Fatalf("persisting verified rendition: %v", err)
		}
	}
	if err := worker.CompleteTranscode(f.ctx, versionID, operation, result); err != nil {
		t.Fatalf("completion inside a valid lease was refused: %v", err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateReady {
		t.Fatalf("state after in-lease completion = %s, want READY", got)
	}
}

// TestD103StaleTokenAndExpiredLeaseFenceIndependently proves the two conditions
// are each sufficient on their own, so neither is load-bearing for the other.
func TestD103StaleTokenAndExpiredLeaseFenceIndependently(t *testing.T) {
	t.Run("stale token alone, lease still valid", func(t *testing.T) {
		f, worker, _, versionID := validatedVideoReadyForProcessing(t)
		operation := uuid.NewString()
		version, applied, err := worker.beginTranscode(f.ctx, versionID, operation)
		if err != nil || !applied {
			t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
		}
		result, _ := successfulAttemptProcessor(f.ctx, version.Object)

		// A different identity now owns the row; the lease itself is healthy.
		replacement := uuid.NewString()
		if _, err := f.pool.Exec(f.ctx, `
			UPDATE media_asset_versions SET work_claim_token = $2 WHERE id = $1::uuid
		`, versionID, replacement); err != nil {
			t.Fatalf("installing replacement token: %v", err)
		}
		extendLease(t, f, versionID)

		if err := worker.CompleteTranscode(f.ctx, versionID, operation, result); err == nil {
			t.Fatal("completion succeeded with a stale token under a valid lease")
		}
		if got := mediaState(t, f.pool, versionID); got != StateProcessing {
			t.Fatalf("state = %s, want PROCESSING unchanged", got)
		}
	})

	t.Run("expired lease alone, token still current", func(t *testing.T) {
		f, worker, _, versionID := validatedVideoReadyForProcessing(t)
		operation := uuid.NewString()
		version, applied, err := worker.beginTranscode(f.ctx, versionID, operation)
		if err != nil || !applied {
			t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
		}
		result, _ := successfulAttemptProcessor(f.ctx, version.Object)

		expireLease(t, f, versionID)
		if got := claimToken(t, f, versionID); got != operation {
			t.Fatalf("claim token = %q, want unchanged", got)
		}
		if err := worker.CompleteTranscode(f.ctx, versionID, operation, result); err == nil {
			t.Fatal("completion succeeded with a current token under an expired lease")
		}
		if got := mediaState(t, f.pool, versionID); got != StateProcessing {
			t.Fatalf("state = %s, want PROCESSING unchanged", got)
		}
	})
}

// TestD103ExpiredLeaseRefusesScanCompletion repeats the H2 proof on the scan
// path, which carries its own claim token and its own lease.
func TestD103ExpiredLeaseRefusesScanCompletion(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	now := time.Now().UTC()
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(successfulAttemptProcessor))

	workID := scanWorkID(t, f.pool, request.AssetVersionID, 0)
	version, attempt, applied, err := worker.beginScan(f.ctx, request.AssetVersionID, workID)
	if err != nil || !applied {
		t.Fatalf("beginScan applied=%t err=%v", applied, err)
	}
	if got := claimToken(t, f, request.AssetVersionID); got != workID {
		t.Fatalf("scan claim token = %q, want the scan work identity", got)
	}

	expireLease(t, f, request.AssetVersionID)

	observation := ScanObservation{
		AssetVersionID: version.Object.AssetVersionID, StorageObjectVersion: version.Object.StorageObjectVersion,
		Outcome: ScanPassed, ScannerIdentity: "fencing-integration-scanner",
	}
	err = worker.finishScan(f.ctx, version, attempt, workID, observation, StateScanPassed)
	if err == nil {
		t.Fatal("scan completion succeeded on an expired lease")
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateScanning {
		t.Fatalf("state after refused scan completion = %s, want SCANNING unchanged", got)
	}
	if got := mediaEventCount(t, f, "media.transcode_requested", request.AssetVersionID); got != 0 {
		t.Fatalf("expired scan queued %d transcode work items, want 0", got)
	}

	// Recovery converges and the retried scan then completes normally, proving
	// the fence costs nothing once ownership is re-established.
	now = now.Add(3 * time.Second)
	if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
		t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
	}
	retryWorkID := scanWorkID(t, f.pool, request.AssetVersionID, 1)
	if err := worker.scan(f.ctx, request.AssetVersionID, retryWorkID, false); err != nil {
		t.Fatalf("retried scan: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateScanPassed {
		t.Fatalf("state after retried scan = %s, want SCAN_PASSED", got)
	}
}

// TestD103LegacyInFlightProcessingAttemptConsumesItsAttempt is the M1 proof. A
// row that entered PROCESSING before 0035 carries the schema default counter of
// zero while a real attempt was in flight; recovering it must charge that
// attempt, not hand it back for free.
func TestD103LegacyInFlightProcessingAttemptConsumesItsAttempt(t *testing.T) {
	f, worker, now, versionID := validatedVideoReadyForProcessing(t)

	// A pre-0035 in-flight row: PROCESSING, no lease columns, counter still 0.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET state = 'PROCESSING', work_claim_token = NULL, work_claimed_at = NULL,
		    work_lease_expires_at = NULL, processing_attempt_count = 0
		WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("simulating a legacy in-flight processing row: %v", err)
	}
	if got := processingAttemptCount(t, f, versionID); got != 0 {
		t.Fatalf("legacy attempt count = %d, want 0", got)
	}

	*now = now.Add(3 * time.Second)
	if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
		t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
	}

	// The invariant: processing_attempt_count is attempts CONSUMED, and is never
	// below the durable attempt record.
	if got := processingAttemptCount(t, f, versionID); got != 1 {
		t.Fatalf("attempt count after legacy recovery = %d, want 1 — the legacy attempt was not charged", got)
	}
	if got := processingAttemptRows(t, f, versionID); got != 1 {
		t.Fatalf("processing attempt rows after legacy recovery = %d, want 1", got)
	}
	if got := mediaState(t, f.pool, versionID); got != StateValidated {
		t.Fatalf("state after legacy recovery = %s, want VALIDATED", got)
	}
}

// TestD103LegacyRecoveryStillStopsAtTheAttemptBudget walks the whole budget
// starting from a legacy attempt: legacy + second + third allowed, no fourth.
func TestD103LegacyRecoveryStillStopsAtTheAttemptBudget(t *testing.T) {
	f, worker, now, versionID := validatedVideoReadyForProcessing(t)

	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET state = 'PROCESSING', work_claim_token = NULL, work_claimed_at = NULL,
		    work_lease_expires_at = NULL, processing_attempt_count = 0
		WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("simulating a legacy in-flight processing row: %v", err)
	}

	// Attempt 1: the legacy in-flight attempt, charged by recovery.
	*now = now.Add(3 * time.Second)
	if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
		t.Fatalf("legacy RecoverStale recovered=%d err=%v", recovered, err)
	}
	if got := processingAttemptCount(t, f, versionID); got != 1 {
		t.Fatalf("attempt count after legacy recovery = %d, want 1", got)
	}

	// Attempts 2 and 3 are the remainder of the budget.
	for attempt := 2; attempt <= MaxWorkAttempts; attempt++ {
		operation := transcodeOperationID(t, f, versionID, attempt-2)
		if _, applied, err := worker.beginTranscode(f.ctx, versionID, operation); err != nil || !applied {
			t.Fatalf("attempt %d beginTranscode applied=%t err=%v", attempt, applied, err)
		}
		if got := processingAttemptCount(t, f, versionID); got != attempt {
			t.Fatalf("attempt count after claim %d = %d, want %d", attempt, got, attempt)
		}
		expireLease(t, f, versionID)
		*now = now.Add(3 * time.Second)
		if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
			t.Fatalf("attempt %d RecoverStale recovered=%d err=%v", attempt, recovered, err)
		}
		if got := processingAttemptCount(t, f, versionID); got != attempt {
			t.Fatalf("attempt count after recovery %d = %d, want %d", attempt, got, attempt)
		}
	}

	// The legacy attempt counted, so the budget is spent after three, not four.
	if got := mediaState(t, f.pool, versionID); got != StateProcessFailed {
		t.Fatalf("state after the budget = %s, want PROCESS_FAILED", got)
	}
	if got := processingAttemptCount(t, f, versionID); got != MaxWorkAttempts {
		t.Fatalf("final attempt count = %d, want %d", got, MaxWorkAttempts)
	}
	if got := mediaEventCount(t, f, "media.transcode_requested", versionID); got != MaxWorkAttempts-1 {
		t.Fatalf("queued transcode work = %d, want %d — a fourth processing attempt was scheduled", got, MaxWorkAttempts-1)
	}
}
