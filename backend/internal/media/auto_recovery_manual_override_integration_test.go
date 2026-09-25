//go:build integration

package media

import (
	"errors"
	"testing"
	"time"
)

// readManualSuppression returns the manual suppression identity a scheduler row
// currently carries, if any.
func readManualSuppression(t *testing.T, f *mediaFixture, assetVersionID string) (string, bool) {
	t.Helper()
	var manualID *string
	err := f.pool.QueryRow(f.ctx,
		"SELECT manual_intent_id::text FROM media_auto_enhancement_recovery WHERE asset_version_id=$1::uuid",
		assetVersionID).Scan(&manualID)
	if err != nil || manualID == nil {
		return "", false
	}
	return *manualID, true
}

func adminRetry(t *testing.T, f *mediaFixture, assetVersionID string) {
	t.Helper()
	if err := f.service.RetryEnhancements(f.ctx, RetryRequest{
		AssetVersionID: assetVersionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	}); err != nil {
		t.Fatalf("admitting the manual recovery request: %v", err)
	}
}

// TestManualRequestAtomicallySuppressesAutomaticScheduling is the G1-1
// regression, and it pins the exact race the independent review described.
//
// The defect: RetryEnhancements committed the manual outbox event but changed no
// scheduler coordination until the manual WORKER later took the media claim.
// Between admission and execution — across outbox dispatch, queue latency and
// worker scheduling — the scheduler was free to mint a fresh automatic intent,
// and that automatic task could take the claim first. An accepted operator
// action could therefore lose to the machine it was meant to override.
func TestManualRequestAtomicallySuppressesAutomaticScheduling(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, _ := autoRecoveryWorker(t, f, &now, processor)

	// An automatic intent already exists and its task is in flight.
	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("seeding the automatic intent: scheduled=%d err=%v", scheduled, err)
	}
	staleAutoIntent := latestAutoIntent(t, f, versionID)
	markIntentDispatched(t, f, staleAutoIntent)

	// The Administrator's request is accepted. Its task has NOT executed yet.
	adminRetry(t, f, versionID)

	row, present := readAutoRecoveryRow(t, f, versionID)
	if !present || row.state != autoRecoveryManualPending {
		t.Fatalf("scheduler row after manual admission = %+v present=%t, want MANUAL_PENDING", row, present)
	}
	if row.intentID != nil {
		t.Fatalf("MANUAL_PENDING still names an automatic intent: %v", *row.intentID)
	}
	manualID, ok := readManualSuppression(t, f, versionID)
	if !ok || manualID == "" {
		t.Fatal("the accepted manual request recorded no suppression identity")
	}

	// The scheduler ticks repeatedly in the window before the manual task runs.
	// It must not mint a replacement, however long that window lasts.
	autoEventsBefore := countEnhancementIntents(t, f, versionID)
	for tick := 0; tick < 200; tick++ {
		scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25)
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if scheduled != 0 {
			t.Fatalf("tick %d scheduled automatic work behind an accepted manual request", tick)
		}
	}
	if got := countEnhancementIntents(t, f, versionID); got != autoEventsBefore {
		t.Fatalf("enhancement events grew from %d to %d while a manual request was outstanding", autoEventsBefore, got)
	}
	if after, _ := readAutoRecoveryRow(t, f, versionID); after.state != autoRecoveryManualPending {
		t.Fatalf("scheduler row left MANUAL_PENDING on its own: %+v", after)
	}

	// The pre-manual automatic task finally arrives. It must no-op before taking
	// any claim, and charge nothing.
	if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, staleAutoIntent); !errors.Is(err, ErrEnhancementIntentSuperseded) {
		t.Fatalf("superseded automatic task error = %v, want ErrEnhancementIntentSuperseded", err)
	}
	var claim *string
	if err := f.pool.QueryRow(f.ctx, "SELECT work_claim_token FROM media_asset_versions WHERE id=$1::uuid", versionID).Scan(&claim); err != nil {
		t.Fatalf("reading the claim: %v", err)
	}
	if claim != nil {
		t.Fatalf("a superseded automatic task took the media claim: %v", claim)
	}
	if survived, _ := readAutoRecoveryRow(t, f, versionID); survived.state != autoRecoveryManualPending {
		t.Fatalf("a superseded automatic task disturbed the manual suppression: %+v", survived)
	}
	if survived, _ := readAutoRecoveryRow(t, f, versionID); survived.failures != 0 {
		t.Fatalf("a superseded automatic task charged %d failures", survived.failures)
	}

	// The accepted manual work executes and reaches its normal terminal state.
	if err := worker.RetryEnhancements(f.ctx, versionID); err != nil {
		t.Fatalf("the accepted manual recovery could not execute: %v", err)
	}
	if _, stillSuppressing := readManualSuppression(t, f, versionID); stillSuppressing {
		t.Fatal("the manual suppression outlived the manual execution")
	}
}

// TestManualRequestRollbackLeavesNoSuppression proves the suppression and the
// manual work are one atomic fact. A request that does not commit must leave the
// scheduler exactly as it found it.
func TestManualRequestRollbackLeavesNoSuppression(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})

	// An Administrator who is not active is refused, so the transaction rolls
	// back after the eligibility read and before any commit.
	err := f.service.RetryEnhancements(f.ctx, RetryRequest{
		AssetVersionID: versionID, AdminAccountID: f.instructorID, ActorDescriptor: f.instructorID,
	})
	if err == nil {
		t.Fatal("a non-Administrator manual recovery request was accepted")
	}
	if _, present := readAutoRecoveryRow(t, f, versionID); present {
		t.Fatal("a rolled-back manual request left scheduler suppression behind")
	}
	if got := countEnhancementIntents(t, f, versionID); got != 0 {
		t.Fatalf("a rolled-back manual request wrote %d enhancement events", got)
	}
}

// TestManualSuppressionReleasesOnlyOnDispatchedExpiry proves the suppression
// cannot block automatic recovery forever, and that its release obeys the same
// boundedness rule as an automatic intent.
//
// An UNDISPATCHED manual event has not been lost: the outbox dispatcher still
// owns it and is going to deliver it. Releasing on elapsed time alone would let
// automatic work be scheduled alongside a manual request that is still coming.
func TestManualSuppressionReleasesOnlyOnDispatchedExpiry(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, _ := autoRecoveryWorker(t, f, &now, processor)

	adminRetry(t, f, versionID)
	manualID, ok := readManualSuppression(t, f, versionID)
	if !ok {
		t.Fatal("no manual suppression was recorded")
	}

	// Expired, but the manual event never left the outbox.
	if _, err := f.pool.Exec(f.ctx,
		"UPDATE media_auto_enhancement_recovery SET manual_expires_at=now()-interval '1 hour' WHERE asset_version_id=$1::uuid",
		versionID); err != nil {
		t.Fatalf("expiring the manual suppression: %v", err)
	}
	for tick := 0; tick < 50; tick++ {
		if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 0 {
			t.Fatalf("tick %d released an undispatched manual suppression: scheduled=%d err=%v", tick, scheduled, err)
		}
	}

	// Once the manual event has actually been dispatched and its task is lost,
	// automatic recovery may resume — exactly once.
	markIntentDispatched(t, f, manualID)
	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("a dispatched, expired manual suppression did not release: scheduled=%d err=%v", scheduled, err)
	}
	row, _ := readAutoRecoveryRow(t, f, versionID)
	if row.state != autoRecoveryScheduled {
		t.Fatalf("scheduler row after release = %+v, want SCHEDULED", row)
	}
	if _, stillSuppressing := readManualSuppression(t, f, versionID); stillSuppressing {
		t.Fatal("the released suppression identity was left behind")
	}
}

// TestManualRequestFromNeedsOperatorResetsBudgetAndSuppresses is Part 3 case C.
func TestManualRequestFromNeedsOperatorResetsBudgetAndSuppresses(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, _ := autoRecoveryWorker(t, f, &now, processor)

	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_auto_enhancement_recovery
		    (asset_version_id, state, consecutive_failures, attempt_number, last_failure_category, updated_at)
		VALUES ($1::uuid, 'NEEDS_OPERATOR', 3, 3, 'TRANSCODE_FAILED', now())
	`, versionID); err != nil {
		t.Fatalf("seeding NEEDS_OPERATOR: %v", err)
	}

	adminRetry(t, f, versionID)

	row, _ := readAutoRecoveryRow(t, f, versionID)
	if row.state != autoRecoveryManualPending {
		t.Fatalf("state after manual request from NEEDS_OPERATOR = %q, want MANUAL_PENDING", row.state)
	}
	if row.failures != 0 {
		t.Fatalf("the manual override left %d consecutive failures, want the budget reset", row.failures)
	}
	for tick := 0; tick < 25; tick++ {
		if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 0 {
			t.Fatalf("tick %d scheduled automatic work while a manual request was outstanding", tick)
		}
	}
	if err := worker.RetryEnhancements(f.ctx, versionID); err != nil {
		t.Fatalf("manual recovery from NEEDS_OPERATOR: %v", err)
	}
}
