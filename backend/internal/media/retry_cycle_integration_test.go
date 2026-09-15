//go:build integration

package media

import (
	"testing"
	"time"
)

// D-103 M1 (second pass). Admin Retry opens a FRESH processing cycle.
//
// That reading is not inferred from one counter. Three independent sources
// agree:
//
//   - the D-103 design's state table lists PROCESS_FAILED as "Terminal until
//     intentional action", resolved by "Admin retry/re-upload", and describes
//     an Admin-requested retry as re-reading and revalidating the exact bytes
//     rather than resuming anything;
//   - Service.applyRetry resets BOTH scan_attempt_count and
//     processing_attempt_count to zero, clears the failure category, and
//     re-enters the asset's own safety path from quarantine;
//   - BR-163 requires retry to APPEND evidence, which is why processing_attempts
//     is append-only and must not be pruned to make a count convenient.
//
// The defect was that recovery reconciled the per-cycle counter against
// `count(*) FROM processing_attempts`, which is lifetime-global. A freshly
// retried asset therefore inherited every attempt of every earlier cycle and
// was exhausted before its first new attempt ran.
//
// INVARIANT: a processing retry cycle receives exactly MaxWorkAttempts attempts.
// Historical attempts from earlier cycles stay auditable and never consume the
// new cycle's budget.

func latestTranscodeOperation(t *testing.T, f *mediaFixture, versionID string) string {
	t.Helper()
	count := mediaEventCount(t, f, "media.transcode_requested", versionID)
	if count == 0 {
		t.Fatal("no transcode work has been queued")
	}
	return transcodeOperationID(t, f, versionID, count-1)
}

func latestScanWork(t *testing.T, f *mediaFixture, versionID string) string {
	t.Helper()
	count := mediaEventCount(t, f, "media.scan_requested", versionID)
	if count == 0 {
		t.Fatal("no scan work has been queued")
	}
	return scanWorkID(t, f.pool, versionID, count-1)
}

// scanProvenanceAssetReadyForProcessing returns a scanner-gated video sitting in
// SCAN_PASSED with its first transcode operation queued. The scan path is used
// deliberately: Admin Retry is only available to an asset whose safety path this
// deployment can still re-enter, and this fixture is scanner-mode.
func scanProvenanceAssetReadyForProcessing(t *testing.T) (*mediaFixture, *Worker, *time.Time, string) {
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
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateScanPassed {
		t.Fatalf("state = %s, want SCAN_PASSED", got)
	}
	return f, worker, &now, request.AssetVersionID
}

// spendOneProcessingAttempt claims the currently queued processing operation,
// lets its lease lapse, and recovers it — one whole consumed attempt. It returns
// the operation identity it spent.
//
// A scanner-gated asset re-enters through QUARANTINED and a fresh scan between
// attempts, which is the real shape of its recovery, so the helper performs that
// rescan rather than pretending processing work is queued directly.
func spendOneProcessingAttempt(t *testing.T, f *mediaFixture, worker *Worker, now *time.Time, versionID string) string {
	t.Helper()
	if mediaState(t, f.pool, versionID) == StateQuarantined {
		if err := worker.scan(f.ctx, versionID, latestScanWork(t, f, versionID), false); err != nil {
			t.Fatalf("rescan between attempts: %v", err)
		}
	}
	operation := latestTranscodeOperation(t, f, versionID)
	if _, applied, err := worker.beginTranscode(f.ctx, versionID, operation); err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	expireLease(t, f, versionID)
	*now = now.Add(3 * time.Second)
	if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
		t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
	}
	return operation
}

// TestD103AdminRetryOpensAFreshProcessingBudget is the whole M1 sequence: an old
// cycle consumes its budget and fails, Admin Retry opens a new one, and the new
// cycle gets exactly three effective attempts and no fourth.
func TestD103AdminRetryOpensAFreshProcessingBudget(t *testing.T) {
	f, worker, now, versionID := scanProvenanceAssetReadyForProcessing(t)

	// The old cycle spends its whole budget.
	oldCycleOperations := make([]string, 0, MaxWorkAttempts)
	for attempt := 1; attempt <= MaxWorkAttempts; attempt++ {
		oldCycleOperations = append(oldCycleOperations, spendOneProcessingAttempt(t, f, worker, now, versionID))
		if got := processingAttemptCount(t, f, versionID); got != attempt {
			t.Fatalf("old cycle attempt count after %d = %d, want %d", attempt, got, attempt)
		}
	}
	if got := mediaState(t, f.pool, versionID); got != StateProcessFailed {
		t.Fatalf("state after the old cycle = %s, want PROCESS_FAILED", got)
	}
	historicalRows := processingAttemptRows(t, f, versionID)
	if historicalRows != MaxWorkAttempts {
		t.Fatalf("historical attempt rows = %d, want %d", historicalRows, MaxWorkAttempts)
	}

	// Admin Retry opens a fresh cycle.
	if err := f.service.Retry(f.ctx, retryRequestFor(f, versionID)); err != nil {
		t.Fatalf("Admin Retry: %v", err)
	}
	if got := processingAttemptCount(t, f, versionID); got != 0 {
		t.Fatalf("attempt count after Admin Retry = %d, want 0", got)
	}
	// Audit history is preserved, not pruned to make the count convenient.
	if got := processingAttemptRows(t, f, versionID); got != historicalRows {
		t.Fatalf("attempt rows after retry = %d, want %d preserved for audit", got, historicalRows)
	}

	// The fresh cycle receives exactly MaxWorkAttempts effective attempts. If
	// recovery still reconciled against lifetime history, the very first fresh
	// recovery would jump the counter straight to the historical total and the
	// cycle would be exhausted immediately.
	freshOperations := make([]string, 0, MaxWorkAttempts)
	for attempt := 1; attempt <= MaxWorkAttempts; attempt++ {
		freshOperations = append(freshOperations, spendOneProcessingAttempt(t, f, worker, now, versionID))
		if got := processingAttemptCount(t, f, versionID); got != attempt {
			t.Fatalf("fresh cycle attempt count after %d = %d, want %d — history leaked into the new cycle", attempt, got, attempt)
		}
	}

	// And no fourth attempt in the fresh cycle.
	if got := mediaState(t, f.pool, versionID); got != StateProcessFailed {
		t.Fatalf("state after the fresh cycle = %s, want PROCESS_FAILED", got)
	}
	transcodeAfter := mediaEventCount(t, f, "media.transcode_requested", versionID)
	stateBefore := mediaState(t, f.pool, versionID)
	countBefore := processingAttemptCount(t, f, versionID)

	// Every operation identity from BOTH cycles is spent and can consume nothing.
	for _, stale := range append(append([]string{}, oldCycleOperations...), freshOperations...) {
		if err := worker.Transcode(f.ctx, versionID, stale); err != nil {
			t.Fatalf("spent operation returned an error instead of a no-op: %v", err)
		}
	}
	if got := processingAttemptCount(t, f, versionID); got != countBefore {
		t.Fatalf("a spent operation consumed an attempt: %d, want %d", got, countBefore)
	}
	if got := mediaState(t, f.pool, versionID); got != stateBefore {
		t.Fatalf("a spent operation changed state to %s, want %s", got, stateBefore)
	}
	if got := mediaEventCount(t, f, "media.transcode_requested", versionID); got != transcodeAfter {
		t.Fatalf("a spent operation queued more work: %d, want %d", got, transcodeAfter)
	}
}

// TestD103FreshCycleRecoveryReconcilesWithoutHistory isolates the reconciliation
// itself: one recovery inside a fresh cycle charges exactly the one attempt it
// interrupted, however much append-only history the asset already carries.
func TestD103FreshCycleRecoveryReconcilesWithoutHistory(t *testing.T) {
	f, worker, now, versionID := scanProvenanceAssetReadyForProcessing(t)

	// Build real history in a first cycle, then retry out of it.
	for attempt := 1; attempt <= MaxWorkAttempts; attempt++ {
		spendOneProcessingAttempt(t, f, worker, now, versionID)
	}
	history := processingAttemptRows(t, f, versionID)
	if history < MaxWorkAttempts {
		t.Fatalf("attempt history = %d, want at least %d; the test would prove nothing", history, MaxWorkAttempts)
	}
	if err := f.service.Retry(f.ctx, retryRequestFor(f, versionID)); err != nil {
		t.Fatalf("Admin Retry: %v", err)
	}

	// Exactly one attempt consumed by one fresh recovery.
	spendOneProcessingAttempt(t, f, worker, now, versionID)
	if got := processingAttemptCount(t, f, versionID); got != 1 {
		t.Fatalf("fresh-cycle attempt count after one recovery = %d, want 1 — history leaked into the new cycle", got)
	}
	if got := processingAttemptRows(t, f, versionID); got <= history {
		t.Fatalf("attempt rows = %d, want more than the %d preserved from the earlier cycle", got, history)
	}

	// The fresh cycle still has budget left, and uses it.
	if got := mediaState(t, f.pool, versionID); got != StateQuarantined {
		t.Fatalf("state = %s, want QUARANTINED with budget remaining", got)
	}
	if err := worker.scan(f.ctx, versionID, latestScanWork(t, f, versionID), false); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if err := worker.Transcode(f.ctx, versionID, latestTranscodeOperation(t, f, versionID)); err != nil {
		t.Fatalf("second fresh attempt: %v", err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateReady {
		t.Fatalf("state = %s, want READY — the fresh cycle could not finish", got)
	}
}
