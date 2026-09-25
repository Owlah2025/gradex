//go:build integration

package media

import (
	"errors"
	"testing"
	"time"
)

// countProcessingAttempts reports how many attempts exist for an Asset Version.
// processing_attempts is append-only, so an unchanged count is proof that no
// execution was recorded.
func countProcessingAttempts(t *testing.T, f *mediaFixture, assetVersionID string) int {
	t.Helper()
	var attempts int
	if err := f.pool.QueryRow(f.ctx,
		"SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid",
		assetVersionID).Scan(&attempts); err != nil {
		t.Fatalf("counting processing attempts: %v", err)
	}
	return attempts
}

func countRenditions(t *testing.T, f *mediaFixture, assetVersionID string) int {
	t.Helper()
	var rungs int
	if err := f.pool.QueryRow(f.ctx,
		"SELECT count(*) FROM video_renditions WHERE asset_version_id=$1::uuid",
		assetVersionID).Scan(&rungs); err != nil {
		t.Fatalf("counting renditions: %v", err)
	}
	return rungs
}

func assertNoMediaWorkTaken(t *testing.T, f *mediaFixture, assetVersionID string, attempts, rungs int) {
	t.Helper()
	var claim *string
	var kind *string
	if err := f.pool.QueryRow(f.ctx,
		"SELECT work_claim_token, active_processing_attempt_kind FROM media_asset_versions WHERE id=$1::uuid",
		assetVersionID).Scan(&claim, &kind); err != nil {
		t.Fatalf("reading the media row: %v", err)
	}
	if claim != nil {
		t.Fatalf("a disabled automatic task took the media claim: %v", *claim)
	}
	if kind != nil {
		t.Fatalf("a disabled automatic task set active_processing_attempt_kind: %v", *kind)
	}
	if got := countProcessingAttempts(t, f, assetVersionID); got != attempts {
		t.Fatalf("processing attempts went %d -> %d; a disabled automatic task recorded an execution", attempts, got)
	}
	if got := countRenditions(t, f, assetVersionID); got != rungs {
		t.Fatalf("renditions went %d -> %d; a disabled automatic task encoded", rungs, got)
	}
}

// TestDisabledFlagStopsQueuedAutomaticTask is the G1-2 regression.
//
// Setting MEDIA_AUTO_ENHANCEMENT_RECOVERY_ENABLED=false stopped the scheduler
// loop from starting, but a task committed while the feature was enabled
// survived in the queue and still executed: it claimed the media, wrote a
// processing attempt and encoded. Disabling the feature has to stop work that is
// already in flight, which is the whole point of the switch.
func TestDisabledFlagStopsQueuedAutomaticTask(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	probe := EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}
	enabled, _ := autoRecoveryWorker(t, f, &now, &enhancementIntegrationProcessor{probe: probe, failAfter: -1})

	if scheduled, err := enabled.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("scheduling while enabled: scheduled=%d err=%v", scheduled, err)
	}
	queuedIntent := latestAutoIntent(t, f, versionID)
	markIntentDispatched(t, f, queuedIntent)

	attempts := countProcessingAttempts(t, f, versionID)
	rungs := countRenditions(t, f, versionID)

	// The operator switches the feature off. The task is already queued.
	disabled := workerWithAutoRecovery(t, f, &now, &enhancementIntegrationProcessor{probe: probe, failAfter: -1}, false)
	if err := disabled.RetryEnhancementsForIntent(f.ctx, versionID, queuedIntent); !errors.Is(err, ErrAutoEnhancementRecoveryDisabled) {
		t.Fatalf("disabled automatic task error = %v, want ErrAutoEnhancementRecoveryDisabled", err)
	}
	assertNoMediaWorkTaken(t, f, versionID, attempts, rungs)

	// No failure was charged, and the row is safely recoverable rather than
	// stranded SCHEDULED until its lease expires.
	row, present := readAutoRecoveryRow(t, f, versionID)
	if !present {
		t.Fatal("the scheduler row vanished")
	}
	if row.failures != 0 {
		t.Fatalf("a disabled automatic task charged %d failures", row.failures)
	}
	if row.state != autoRecoveryBackoff {
		t.Fatalf("scheduler state after a disabled task = %q, want a recoverable BACKOFF", row.state)
	}
	if row.intentID != nil {
		t.Fatalf("the paused row still names intent %v, so the acknowledged task could become active later", *row.intentID)
	}

	// The acknowledged task cannot come back to life, even if it is redelivered
	// after the feature is switched on again.
	if err := enabled.RetryEnhancementsForIntent(f.ctx, versionID, queuedIntent); !errors.Is(err, ErrEnhancementIntentSuperseded) {
		t.Fatalf("redelivered paused task error = %v, want ErrEnhancementIntentSuperseded", err)
	}
	assertNoMediaWorkTaken(t, f, versionID, attempts, rungs)

	// Re-enabling produces exactly ONE valid new intent, which executes normally.
	scheduled, err := enabled.ScheduleAutoEnhancementRecovery(f.ctx, 25)
	if err != nil || scheduled != 1 {
		t.Fatalf("re-enabled scheduler: scheduled=%d err=%v", scheduled, err)
	}
	if again, err := enabled.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || again != 0 {
		t.Fatalf("re-enabling produced a duplicate intent: scheduled=%d err=%v", again, err)
	}
	resumed := latestAutoIntent(t, f, versionID)
	if resumed == queuedIntent {
		t.Fatal("the resumed intent reused the paused identity")
	}
	if err := enabled.RetryEnhancementsForIntent(f.ctx, versionID, resumed); err != nil {
		t.Fatalf("the resumed intent did not execute: %v", err)
	}
}

// TestDisabledFlagStillAllowsManualRecovery proves the switch is targeted. It
// disables AUTOMATIC recovery; the Administrator's manual action is the operator
// escape hatch and must keep working.
func TestDisabledFlagStillAllowsManualRecovery(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	probe := EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}
	disabled := workerWithAutoRecovery(t, f, &now, &enhancementIntegrationProcessor{probe: probe, failAfter: -1}, false)

	adminRetry(t, f, versionID)
	if row, present := readAutoRecoveryRow(t, f, versionID); !present || row.state != autoRecoveryManualPending {
		t.Fatalf("manual admission while disabled = %+v present=%t, want MANUAL_PENDING", row, present)
	}
	if err := disabled.RetryEnhancements(f.ctx, versionID); err != nil {
		t.Fatalf("manual recovery while automatic recovery is disabled: %v", err)
	}
	if countRenditions(t, f, versionID) == 0 {
		t.Fatal("the manual recovery produced nothing")
	}
}

// TestDisabledFlagWithOutstandingManualRequest is Part 3 case A: an automatic
// task queued before a manual request arrives, with the feature then switched
// off. The stale automatic task must no-op and the manual work must still run.
func TestDisabledFlagWithOutstandingManualRequest(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	probe := EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}
	enabled, _ := autoRecoveryWorker(t, f, &now, &enhancementIntegrationProcessor{probe: probe, failAfter: -1})

	if scheduled, err := enabled.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("seeding the automatic intent: scheduled=%d err=%v", scheduled, err)
	}
	staleAuto := latestAutoIntent(t, f, versionID)
	markIntentDispatched(t, f, staleAuto)

	adminRetry(t, f, versionID)

	attempts := countProcessingAttempts(t, f, versionID)
	rungs := countRenditions(t, f, versionID)

	disabled := workerWithAutoRecovery(t, f, &now, &enhancementIntegrationProcessor{probe: probe, failAfter: -1}, false)
	// The stale automatic task is refused by the flag gate before anything else
	// is consulted, so it cannot claim.
	if err := disabled.RetryEnhancementsForIntent(f.ctx, versionID, staleAuto); !errors.Is(err, ErrAutoEnhancementRecoveryDisabled) {
		t.Fatalf("stale automatic task while disabled = %v, want ErrAutoEnhancementRecoveryDisabled", err)
	}
	assertNoMediaWorkTaken(t, f, versionID, attempts, rungs)

	// The flag gate must not have disturbed the manual suppression.
	if row, _ := readAutoRecoveryRow(t, f, versionID); row.state != autoRecoveryManualPending {
		t.Fatalf("scheduler state = %q, want the manual suppression intact", row.state)
	}

	// And the accepted manual work still executes.
	if err := disabled.RetryEnhancements(f.ctx, versionID); err != nil {
		t.Fatalf("the accepted manual recovery could not execute: %v", err)
	}
}

// TestManualRequestOverridesPausedAutomaticState is Part 3 case B.
func TestManualRequestOverridesPausedAutomaticState(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	probe := EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}
	enabled, _ := autoRecoveryWorker(t, f, &now, &enhancementIntegrationProcessor{probe: probe, failAfter: -1})
	if scheduled, err := enabled.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("seeding the automatic intent: scheduled=%d err=%v", scheduled, err)
	}
	pausedIntent := latestAutoIntent(t, f, versionID)

	disabled := workerWithAutoRecovery(t, f, &now, &enhancementIntegrationProcessor{probe: probe, failAfter: -1}, false)
	if err := disabled.RetryEnhancementsForIntent(f.ctx, versionID, pausedIntent); !errors.Is(err, ErrAutoEnhancementRecoveryDisabled) {
		t.Fatalf("pausing the automatic task: %v", err)
	}

	// A manual request lands on the paused row and overrides it cleanly.
	adminRetry(t, f, versionID)
	row, _ := readAutoRecoveryRow(t, f, versionID)
	if row.state != autoRecoveryManualPending {
		t.Fatalf("manual request over a paused row = %q, want MANUAL_PENDING", row.state)
	}
	if row.intentID != nil || row.failures != 0 {
		t.Fatalf("contradictory scheduler state after the override: %+v", row)
	}
	if err := disabled.RetryEnhancements(f.ctx, versionID); err != nil {
		t.Fatalf("manual recovery after overriding a paused row: %v", err)
	}
}
