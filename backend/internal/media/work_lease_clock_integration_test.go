//go:build integration

package media

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// D-103 H2 (second pass). Lease authority belongs to the database clock at the
// authoritative mutation, and to nothing else.
//
// These tests move the WORKER's injected clock by large amounts and prove it
// changes no lease decision, then move the DATABASE-stored lease window and
// prove that does. A test that only advanced the injected clock would pass
// against an implementation where the application clock is still the authority,
// which is exactly the gap the independent review identified.

// skewableWorker returns a worker whose lease is long enough in database terms
// that only deliberate database-side expiry can end it, and whose application
// clock the test can move arbitrarily.
func skewableWorker(t *testing.T, f *mediaFixture, now *time.Time) *Worker {
	t.Helper()
	scanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{
			AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion,
			Outcome: ScanPassed, ScannerIdentity: "clock-skew-integration-scanner",
		}, nil
	}))
	worker, err := NewWorker(WorkerOptions{
		DB: f.pool, Scanner: scanner, Process: integrationProcessorFunc(successfulAttemptProcessor),
		Outbox: f.writer,
		// A one-hour lease: nothing in these tests can expire it by elapsed real
		// time, so any expiry observed is one the test caused in the database.
		ProcessingTimeout: time.Second, WorkLeaseDuration: time.Hour,
		Now: func() time.Time { return *now },
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return worker
}

func progressObservation(t *testing.T, f *mediaFixture, assetVersionID string) (string, int) {
	t.Helper()
	var stage *string
	var percent *int16
	if err := f.pool.QueryRow(f.ctx, `
		SELECT processing_stage, processing_progress_percent
		FROM media_asset_versions WHERE id = $1::uuid
	`, assetVersionID).Scan(&stage, &percent); err != nil {
		t.Fatalf("reading progress observation: %v", err)
	}
	stageText := ""
	if stage != nil {
		stageText = *stage
	}
	value := -1
	if percent != nil {
		value = int(*percent)
	}
	return stageText, value
}

func leaseWindowFromDatabase(t *testing.T, f *mediaFixture, assetVersionID string) (time.Time, time.Time) {
	t.Helper()
	var claimedAt, expiresAt time.Time
	if err := f.pool.QueryRow(f.ctx, `
		SELECT work_claimed_at, work_lease_expires_at FROM media_asset_versions WHERE id = $1::uuid
	`, assetVersionID).Scan(&claimedAt, &expiresAt); err != nil {
		t.Fatalf("reading lease window: %v", err)
	}
	return claimedAt, expiresAt
}

// TestD103LeaseWindowIsMintedByTheDatabaseClock proves the stored lease window
// is the database's, not the worker's. The worker's clock is a year out; the
// stored window must still sit around database now().
func TestD103LeaseWindowIsMintedByTheDatabaseClock(t *testing.T) {
	f, _, _, versionID := validatedVideoReadyForProcessing(t)
	skewed := time.Now().UTC().Add(365 * 24 * time.Hour)
	worker := skewableWorker(t, f, &skewed)

	if _, applied, err := worker.beginTranscode(f.ctx, versionID, uuid.NewString()); err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}

	claimedAt, expiresAt := leaseWindowFromDatabase(t, f, versionID)
	var databaseNow time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT now()`).Scan(&databaseNow); err != nil {
		t.Fatalf("reading database time: %v", err)
	}
	if claimedAt.Sub(databaseNow).Abs() > time.Minute {
		t.Fatalf("work_claimed_at %s is not database time %s — the worker clock minted it", claimedAt, databaseNow)
	}
	expected := databaseNow.Add(time.Hour)
	if expiresAt.Sub(expected).Abs() > time.Minute {
		t.Fatalf("work_lease_expires_at %s is not database now()+lease %s", expiresAt, expected)
	}
}

// TestD103ApplicationClockAheadCannotRecoverALiveLease — skew case 1.
func TestD103ApplicationClockAheadCannotRecoverALiveLease(t *testing.T) {
	f, _, _, versionID := validatedVideoReadyForProcessing(t)
	now := time.Now().UTC()
	worker := skewableWorker(t, f, &now)

	operation := uuid.NewString()
	if _, applied, err := worker.beginTranscode(f.ctx, versionID, operation); err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}

	// The worker now believes a year has passed. The database disagrees.
	now = now.Add(365 * 24 * time.Hour)
	recovered, err := worker.RecoverStale(f.ctx, 10)
	if err != nil {
		t.Fatalf("RecoverStale: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("recovered %d rows on a database-valid lease — the application clock decided staleness", recovered)
	}
	if got := mediaState(t, f.pool, versionID); got != StateProcessing {
		t.Fatalf("state = %s, want PROCESSING untouched", got)
	}
	if got := claimToken(t, f, versionID); got != operation {
		t.Fatalf("claim token = %q, want the live worker's own", got)
	}
	if got := processingAttemptCount(t, f, versionID); got != 1 {
		t.Fatalf("attempt count = %d, want 1 — a live lease was charged again", got)
	}
}

// TestD103ApplicationClockBehindCannotProlongAnExpiredLease — skew case 2.
func TestD103ApplicationClockBehindCannotProlongAnExpiredLease(t *testing.T) {
	f, _, _, versionID := validatedVideoReadyForProcessing(t)
	now := time.Now().UTC()
	worker := skewableWorker(t, f, &now)

	if _, applied, err := worker.beginTranscode(f.ctx, versionID, uuid.NewString()); err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	expireLease(t, f, versionID)

	// The worker believes it is a year before the lease was even taken.
	now = now.Add(-365 * 24 * time.Hour)
	recovered, err := worker.RecoverStale(f.ctx, 10)
	if err != nil {
		t.Fatalf("RecoverStale: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered %d rows on a database-expired lease — the application clock prolonged it", recovered)
	}
	if got := mediaState(t, f.pool, versionID); got != StateValidated {
		t.Fatalf("state = %s, want VALIDATED after recovery", got)
	}
}

// TestD103ExpiredWorkerCannotWriteProgressBeforeRecovery — skew case 3, and the
// specific gap the review called out: the progress write was token-fenced but
// not expiry-fenced, so an expired worker kept reporting until recovery ran.
func TestD103ExpiredWorkerCannotWriteProgressBeforeRecovery(t *testing.T) {
	f, _, _, versionID := validatedVideoReadyForProcessing(t)
	now := time.Now().UTC()
	worker := skewableWorker(t, f, &now)

	operation := uuid.NewString()
	if _, applied, err := worker.beginTranscode(f.ctx, versionID, operation); err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	// A legitimate in-lease write establishes the baseline.
	if err := writeProcessingProgress(f.ctx, f.pool, versionID, operation, StageTranscoding, 40); err != nil {
		t.Fatalf("in-lease progress write: %v", err)
	}
	if stage, percent := progressObservation(t, f, versionID); stage != string(StageTranscoding) || percent != 40 {
		t.Fatalf("baseline observation = %s/%d, want TRANSCODING/40", stage, percent)
	}

	// The lease lapses. Recovery has NOT run, so the row still carries this
	// worker's own token: only expiry can refuse the write.
	expireLease(t, f, versionID)
	if got := claimToken(t, f, versionID); got != operation {
		t.Fatalf("claim token = %q, want the expired worker's own still recorded", got)
	}
	if err := writeProcessingProgress(f.ctx, f.pool, versionID, operation, StageTranscoding, 90); err != nil {
		t.Fatalf("expired progress write returned an error: %v", err)
	}
	if stage, percent := progressObservation(t, f, versionID); stage != string(StageTranscoding) || percent != 40 {
		t.Fatalf("observation after the expired write = %s/%d, want the unchanged TRANSCODING/40", stage, percent)
	}
}

// TestD103StaleTokenCannotWriteProgressUnderALiveLease — skew case 6 on the
// progress path: identity alone is sufficient to fence.
func TestD103StaleTokenCannotWriteProgressUnderALiveLease(t *testing.T) {
	f, _, _, versionID := validatedVideoReadyForProcessing(t)
	now := time.Now().UTC()
	worker := skewableWorker(t, f, &now)

	operation := uuid.NewString()
	if _, applied, err := worker.beginTranscode(f.ctx, versionID, operation); err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	if err := writeProcessingProgress(f.ctx, f.pool, versionID, operation, StageTranscoding, 25); err != nil {
		t.Fatalf("in-lease progress write: %v", err)
	}

	// The lease is untouched and live; only the identity is wrong.
	if err := writeProcessingProgress(f.ctx, f.pool, versionID, uuid.NewString(), StageTranscoding, 95); err != nil {
		t.Fatalf("stale-token progress write returned an error: %v", err)
	}
	if stage, percent := progressObservation(t, f, versionID); stage != string(StageTranscoding) || percent != 25 {
		t.Fatalf("observation after a stale-token write = %s/%d, want the unchanged TRANSCODING/25", stage, percent)
	}
}

// TestD103ValidWorkerBeforeDatabaseExpiryWritesAndFinalizes — skew case 5. The
// fences must not cost a healthy worker anything, even one whose own clock is
// badly wrong in the direction that would have expired it.
func TestD103ValidWorkerBeforeDatabaseExpiryWritesAndFinalizes(t *testing.T) {
	f, _, _, versionID := validatedVideoReadyForProcessing(t)
	skewed := time.Now().UTC().Add(365 * 24 * time.Hour)
	worker := skewableWorker(t, f, &skewed)

	operation := uuid.NewString()
	version, applied, err := worker.beginTranscode(f.ctx, versionID, operation)
	if err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	if err := writeProcessingProgress(f.ctx, f.pool, versionID, operation, StageTranscoding, 60); err != nil {
		t.Fatalf("progress write: %v", err)
	}
	if stage, percent := progressObservation(t, f, versionID); stage != string(StageTranscoding) || percent != 60 {
		t.Fatalf("observation = %s/%d, want TRANSCODING/60", stage, percent)
	}

	result, _ := successfulAttemptProcessor(f.ctx, version.Object)
	if err := worker.CompleteTranscode(f.ctx, versionID, operation, result); err != nil {
		t.Fatalf("completion inside a database-valid lease was refused: %v", err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateReady {
		t.Fatalf("state = %s, want READY", got)
	}
}
