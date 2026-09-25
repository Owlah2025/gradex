//go:build integration

package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// autoRecoveryWorker builds a worker with 3C-C enabled. The flag is set here
// explicitly and never defaults on: production configuration keeps it false, and
// a test that inherited a true default would be proving the wrong thing.
func autoRecoveryWorker(t *testing.T, f *mediaFixture, now *time.Time, processor Processor) (*Worker, *autoRecoveryLog) {
	t.Helper()
	log := &autoRecoveryLog{}
	scanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{
			AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion,
			Outcome: ScanPassed, ScannerIdentity: "auto-recovery-integration-scanner",
		}, nil
	}))
	worker, err := NewWorker(WorkerOptions{
		DB: f.pool, Scanner: scanner, Process: processor, Outbox: f.writer,
		ProcessingTimeout: time.Second, WorkLeaseDuration: 2 * time.Second,
		Now:                            func() time.Time { return *now },
		AutoRecoveryStateAvailable:     true,
		AutoEnhancementRecoveryEnabled: true,
		ObserveAutoRecovery:            log.record,
	})
	if err != nil {
		t.Fatalf("NewWorker with automatic recovery: %v", err)
	}
	return worker, log
}

type autoRecoveryLog struct {
	mu           sync.Mutex
	observations []AutoRecoveryObservation
}

func (l *autoRecoveryLog) record(observation AutoRecoveryObservation) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.observations = append(l.observations, observation)
}

func (l *autoRecoveryLog) phases() []AutoRecoveryPhase {
	l.mu.Lock()
	defer l.mu.Unlock()
	phases := make([]AutoRecoveryPhase, 0, len(l.observations))
	for _, observation := range l.observations {
		phases = append(phases, observation.Phase)
	}
	return phases
}

func (l *autoRecoveryLog) saw(phase AutoRecoveryPhase) bool {
	for _, seen := range l.phases() {
		if seen == phase {
			return true
		}
	}
	return false
}

type autoRecoveryRow struct {
	state           string
	intentID        *string
	operationID     *string
	failures        int
	attemptNumber   int
	nextAttemptAt   *time.Time
	failureCategory *string
	progressBase    int
	intentExpiresAt *time.Time
}

func readAutoRecoveryRow(t *testing.T, f *mediaFixture, assetVersionID string) (autoRecoveryRow, bool) {
	t.Helper()
	var row autoRecoveryRow
	err := f.pool.QueryRow(f.ctx, `
		SELECT state, current_intent_id::text, executing_operation_id, consecutive_failures,
		       attempt_number, next_attempt_at, last_failure_category, progress_rendition_count,
		       intent_expires_at
		FROM media_auto_enhancement_recovery WHERE asset_version_id = $1::uuid
	`, assetVersionID).Scan(&row.state, &row.intentID, &row.operationID, &row.failures,
		&row.attemptNumber, &row.nextAttemptAt, &row.failureCategory, &row.progressBase,
		&row.intentExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return autoRecoveryRow{}, false
	}
	if err != nil {
		t.Fatalf("reading automatic recovery row: %v", err)
	}
	return row, true
}

// enhancementIntents returns every committed automatic enhancement intent, read
// the way the dispatcher reads it: from the committed outbox event's safe payload,
// which is what becomes the queue task body.
func enhancementIntents(t *testing.T, f *mediaFixture, assetVersionID string) []EnhancementWork {
	t.Helper()
	rows, err := f.pool.Query(f.ctx, `
		SELECT id::text, safe_payload, correlation_id
		FROM outbox_events
		WHERE event_type = 'media.enhancement_requested' AND aggregate_id = $1::uuid
		ORDER BY occurred_at, id
	`, assetVersionID)
	if err != nil {
		t.Fatalf("reading enhancement intents: %v", err)
	}
	defer rows.Close()
	var work []EnhancementWork
	for rows.Next() {
		var eventID, correlation string
		var payload []byte
		if err := rows.Scan(&eventID, &payload, &correlation); err != nil {
			t.Fatalf("scanning enhancement intent: %v", err)
		}
		var decoded EnhancementWork
		// Decoded exactly as handleEnhancementTask decodes the queue task body.
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("decoding enhancement task payload: %v", err)
		}
		if decoded.AutoRecoveryIntentID != "" {
			// The intent identity IS the outbox event id. If these ever diverge the
			// linkage has two identities and attribution stops being provable.
			if decoded.AutoRecoveryIntentID != eventID {
				t.Fatalf("automatic intent %s is not the event id %s", decoded.AutoRecoveryIntentID, eventID)
			}
			if correlation != autoEnhancementRecoveryCorrelation {
				t.Fatalf("automatic intent correlation = %q, want %q", correlation, autoEnhancementRecoveryCorrelation)
			}
		}
		work = append(work, decoded)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating enhancement intents: %v", err)
	}
	return work
}

func countEnhancementIntents(t *testing.T, f *mediaFixture, assetVersionID string) int {
	t.Helper()
	return len(enhancementIntents(t, f, assetVersionID))
}

// latestAutoIntent returns the newest committed automatic intent id.
func latestAutoIntent(t *testing.T, f *mediaFixture, assetVersionID string) string {
	t.Helper()
	intents := enhancementIntents(t, f, assetVersionID)
	for index := len(intents) - 1; index >= 0; index-- {
		if intents[index].AutoRecoveryIntentID != "" {
			return intents[index].AutoRecoveryIntentID
		}
	}
	t.Fatal("no automatic enhancement intent was committed")
	return ""
}

// TestAutoRecoveryDisabledWritesNothing is the flag contract. With the flag off
// there is no candidate query, no scheduler row, and no outbox intent — not a
// scheduled intent that is then ignored.
func TestAutoRecoveryDisabledWritesNothing(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	scanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "s"}, nil
	}))
	disabled, err := NewWorker(WorkerOptions{
		DB: f.pool, Scanner: scanner, Process: processor, Outbox: f.writer,
		ProcessingTimeout: time.Second, WorkLeaseDuration: 2 * time.Second,
		Now: func() time.Time { return now },
		// The state exists; only the flag is off. This isolates the flag from the
		// schema requirement.
		AutoRecoveryStateAvailable:     true,
		AutoEnhancementRecoveryEnabled: false,
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if disabled.AutoEnhancementRecoveryEnabled() {
		t.Fatal("automatic recovery reported enabled while the flag is false")
	}
	for tick := 0; tick < 5; tick++ {
		scheduled, err := disabled.ScheduleAutoEnhancementRecovery(f.ctx, 25)
		if err != nil || scheduled != 0 {
			t.Fatalf("disabled tick %d scheduled=%d err=%v", tick, scheduled, err)
		}
	}
	if _, present := readAutoRecoveryRow(t, f, versionID); present {
		t.Fatal("a disabled reconciler wrote automatic recovery state")
	}
	if got := countEnhancementIntents(t, f, versionID); got != 0 {
		t.Fatalf("a disabled reconciler wrote %d enhancement intents", got)
	}
}

// TestAutoRecoveryEnabledRequiresSchema45 proves the capability gate fails closed.
// A worker that started with the flag set against a schema that cannot hold the
// state would look like automatic recovery was running while no candidate could
// ever be scheduled.
func TestAutoRecoveryEnabledRequiresSchema45(t *testing.T) {
	f := newMediaFixture(t)
	scanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "s"}, nil
	}))
	_, err := NewWorker(WorkerOptions{
		DB: f.pool, Scanner: scanner, Process: &enhancementIntegrationProcessor{failAfter: -1}, Outbox: f.writer,
		ProcessingTimeout: time.Second, WorkLeaseDuration: 2 * time.Second,
		AutoRecoveryStateAvailable:     false,
		AutoEnhancementRecoveryEnabled: true,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("NewWorker error = %v, want a validation refusal", err)
	}
}

// TestAutoRecoveryEligibility is the selection contract.
//
// The case that matters most is a healthy progressive FULL operation that has
// reached PLAYABLE while still holding its claim. That is in-flight work, not a
// recovery candidate, and it is excluded because the claim is held — not because
// of any guess about how long ago it started. There is no settle-age heuristic
// anywhere in the predicate.
func TestAutoRecoveryEligibility(t *testing.T) {
	cases := []struct {
		name     string
		mutate   string
		eligible bool
	}{
		{name: "settled claim-free PLAYABLE", mutate: "", eligible: true},
		{
			name: "active progressive FULL still holding its claim",
			mutate: `UPDATE media_asset_versions SET work_claim_token='full-op', processing_attempt_token='full-op',
			           work_claimed_at=now(), work_lease_expires_at=now()+interval '1 hour',
			           active_processing_attempt_kind='FULL' WHERE id=$1::uuid`,
		},
		{
			name: "enhancement claim held by another worker",
			mutate: `UPDATE media_asset_versions SET work_claim_token='enh-op', processing_attempt_token='enh-op',
			           work_claimed_at=now(), work_lease_expires_at=now()+interval '1 hour',
			           active_processing_attempt_kind='ENHANCEMENT' WHERE id=$1::uuid`,
		},
		{
			name:   "retired logical asset",
			mutate: `UPDATE media_assets SET retired_at=now() WHERE id=(SELECT logical_asset_id FROM media_asset_versions WHERE id=$1::uuid)`,
		},
		{
			name: "expired lease still recorded against a held claim",
			mutate: `UPDATE media_asset_versions SET work_claim_token='enh-op', processing_attempt_token='enh-op',
			           work_claimed_at=now()-interval '2 hours', work_lease_expires_at=now()-interval '1 hour',
			           active_processing_attempt_kind='ENHANCEMENT' WHERE id=$1::uuid`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			f, _, versionID := seedPlayableEnhancementAsset(t, []string{"720p", "480p"})
			now := time.Now().UTC()
			processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
			worker, _ := autoRecoveryWorker(t, f, &now, processor)
			if testCase.mutate != "" {
				if _, err := f.pool.Exec(f.ctx, testCase.mutate, versionID); err != nil {
					t.Fatalf("staging %s: %v", testCase.name, err)
				}
			}
			scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25)
			if err != nil {
				t.Fatalf("scheduling: %v", err)
			}
			want := 0
			if testCase.eligible {
				want = 1
			}
			if scheduled != want {
				t.Fatalf("scheduled=%d, want %d", scheduled, want)
			}
			_, present := readAutoRecoveryRow(t, f, versionID)
			if present != testCase.eligible {
				t.Fatalf("scheduler row present=%t, want %t", present, testCase.eligible)
			}
			if got := countEnhancementIntents(t, f, versionID); got != want {
				t.Fatalf("enhancement intents=%d, want %d", got, want)
			}
		})
	}
}

// TestAutoRecoveryNonPlayableStatesAreExcluded covers the states a PLAYABLE row
// cannot be mutated into.
//
// The schema refuses PLAYABLE -> UPLOADED, PLAYABLE -> PROCESS_FAILED, and a
// READY row without successful processing evidence, so these are seeded directly
// rather than by mutating a candidate. That refusal is worth stating: a PLAYABLE
// VIDEO also cannot lack its checksum or its scan/validation provenance, because
// the same constraints forbid clearing them, so the corresponding clauses in the
// eligibility predicate are defence in depth against a row shape the database
// already makes unrepresentable.
func TestAutoRecoveryNonPlayableStatesAreExcluded(t *testing.T) {
	for _, state := range []string{"UPLOADED", "QUARANTINED", "SCANNING", "PROCESSING", "PROCESS_FAILED", "SCAN_ERROR", "VALIDATED"} {
		t.Run(state, func(t *testing.T) {
			f := newMediaFixture(t)
			now := time.Now().UTC()
			worker, _ := autoRecoveryWorker(t, f, &now, &enhancementIntegrationProcessor{failAfter: -1})
			var assetID, versionID string
			if err := f.pool.QueryRow(f.ctx, `
				INSERT INTO media_assets (kind, owner_account_id, course_id, visibility)
				VALUES ('VIDEO', $1::uuid, $2::uuid, 'PROTECTED') RETURNING id::text
			`, f.instructorID, f.courseID).Scan(&assetID); err != nil {
				t.Fatalf("seeding asset: %v", err)
			}
			if err := f.pool.QueryRow(f.ctx, `
				INSERT INTO media_asset_versions
				  (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes, sha256_hex)
				VALUES ($1::uuid, 'VIDEO', $2::media_asset_version_state, 'k', 'v', 'video/mp4', 100,
				        'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
				RETURNING id::text
			`, assetID, state).Scan(&versionID); err != nil {
				t.Fatalf("seeding %s version: %v", state, err)
			}
			scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25)
			if err != nil || scheduled != 0 {
				t.Fatalf("%s scheduled=%d err=%v", state, scheduled, err)
			}
			if _, present := readAutoRecoveryRow(t, f, versionID); present {
				t.Fatalf("%s produced automatic recovery state", state)
			}
		})
	}
}

// TestAutoRecoveryNonVideoIsExcluded keeps the automatic path away from every
// other asset kind. Enhancement has no meaning for a resource or a thumbnail.
func TestAutoRecoveryNonVideoIsExcluded(t *testing.T) {
	f := newMediaFixture(t)
	now := time.Now().UTC()
	worker, _ := autoRecoveryWorker(t, f, &now, &enhancementIntegrationProcessor{failAfter: -1})
	var assetID, versionID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO media_assets (kind, owner_account_id, course_id, visibility)
		VALUES ('RESOURCE', $1::uuid, $2::uuid, 'PROTECTED') RETURNING id::text
	`, f.instructorID, f.courseID).Scan(&assetID); err != nil {
		t.Fatalf("seeding resource asset: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO media_asset_versions
		  (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes, sha256_hex)
		VALUES ($1::uuid, 'RESOURCE', 'PLAYABLE', 'k', 'v', 'application/pdf', 10,
		        'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
		RETURNING id::text
	`, assetID).Scan(&versionID); err != nil {
		t.Fatalf("seeding resource version: %v", err)
	}
	scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25)
	if err != nil || scheduled != 0 {
		t.Fatalf("non-video scheduled=%d err=%v", scheduled, err)
	}
}

// TestAutoRecoveryRepeatedTicksStayBounded is the boundedness guarantee.
//
// The dedupe is the primary key plus the conditional ON CONFLICT, both in
// PostgreSQL. Redis task ids are not consulted, so losing Redis cannot cause a
// duplicate intent, and a hundred ticks over one unchanged asset produce exactly
// one row and one event.
func TestAutoRecoveryRepeatedTicksStayBounded(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, log := autoRecoveryWorker(t, f, &now, processor)

	total := 0
	for tick := 0; tick < 100; tick++ {
		scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25)
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		total += scheduled
	}
	if total != 1 {
		t.Fatalf("100 ticks scheduled %d intents, want 1", total)
	}
	row, present := readAutoRecoveryRow(t, f, versionID)
	if !present || row.state != autoRecoveryScheduled || row.attemptNumber != 1 {
		t.Fatalf("scheduler row after 100 ticks = %+v present=%t", row, present)
	}
	if got := countEnhancementIntents(t, f, versionID); got != 1 {
		t.Fatalf("100 ticks wrote %d enhancement intents, want 1", got)
	}
	var rows int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM media_auto_enhancement_recovery").Scan(&rows); err != nil {
		t.Fatalf("counting scheduler rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("scheduler rows = %d, want 1", rows)
	}
	// A live intent is excluded at candidate selection, so the later ticks do not
	// even reach the compare-and-set. Exactly one candidate was ever discovered,
	// which is the read-level half of the dedupe; the compare-and-set half is what
	// TestAutoRecoveryConcurrentReconcilersProduceOneIntent proves.
	discovered := 0
	for _, phase := range log.phases() {
		if phase == AutoRecoveryCandidateDiscovered {
			discovered++
		}
	}
	if discovered != 1 {
		t.Fatalf("100 ticks discovered %d candidates, want 1", discovered)
	}
}

// TestAutoRecoveryConcurrentReconcilersProduceOneIntent removes the
// single-worker assumption. Production runs one worker today; correctness must
// not depend on that.
func TestAutoRecoveryConcurrentReconcilersProduceOneIntent(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"720p", "480p"})
	now := time.Now().UTC()
	probe := EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}
	first, _ := autoRecoveryWorker(t, f, &now, &enhancementIntegrationProcessor{probe: probe, failAfter: -1})
	second, _ := autoRecoveryWorker(t, f, &now, &enhancementIntegrationProcessor{probe: probe, failAfter: -1})

	start := make(chan struct{})
	results := make(chan int, 2)
	var wait sync.WaitGroup
	for _, worker := range []*Worker{first, second} {
		wait.Add(1)
		go func(w *Worker) {
			defer wait.Done()
			<-start
			scheduled, err := w.ScheduleAutoEnhancementRecovery(f.ctx, 25)
			if err != nil {
				t.Errorf("concurrent reconciler: %v", err)
			}
			results <- scheduled
		}(worker)
	}
	close(start)
	wait.Wait()
	close(results)
	total := 0
	for scheduled := range results {
		total += scheduled
	}
	if total != 1 {
		t.Fatalf("two concurrent reconcilers scheduled %d intents, want 1", total)
	}
	if got := countEnhancementIntents(t, f, versionID); got != 1 {
		t.Fatalf("two concurrent reconcilers wrote %d intents, want 1", got)
	}
}

// TestAutoRecoveryLinksIntentThroughToReady walks the whole durable chain and
// then proves the automatic state is closed rather than left outstanding.
func TestAutoRecoveryLinksIntentThroughToReady(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, log := autoRecoveryWorker(t, f, &now, processor)

	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("scheduling: scheduled=%d err=%v", scheduled, err)
	}
	intentID := latestAutoIntent(t, f, versionID)
	row, _ := readAutoRecoveryRow(t, f, versionID)
	if row.intentID == nil || *row.intentID != intentID || row.operationID != nil {
		t.Fatalf("scheduled row = %+v, want intent %s and no bound operation", row, intentID)
	}
	if row.progressBase != 3 {
		t.Fatalf("progress baseline = %d, want the 3 canonical rungs that exist", row.progressBase)
	}

	if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, intentID); err != nil {
		t.Fatalf("automatic execution: %v", err)
	}

	// Only the missing rung was encoded.
	processor.mu.Lock()
	missing := append([][]string(nil), processor.missing...)
	operation := processor.lastOp
	processor.mu.Unlock()
	if len(missing) != 1 || len(missing[0]) != 1 || missing[0][0] != "240p" {
		t.Fatalf("automatic enhancement encoded %v, want only the missing 240p rung", missing)
	}

	// The terminal attempt is attributed to the intent that caused it.
	var attemptIntent *string
	var attemptKind, attemptState string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT auto_recovery_intent_id::text, attempt_kind::text, state::text
		FROM processing_attempts WHERE asset_version_id=$1::uuid AND operation_id=$2
	`, versionID, operation).Scan(&attemptIntent, &attemptKind, &attemptState); err != nil {
		t.Fatalf("reading the terminal attempt: %v", err)
	}
	if attemptIntent == nil || *attemptIntent != intentID {
		t.Fatalf("terminal attempt intent = %v, want %s", attemptIntent, intentID)
	}
	if attemptKind != "ENHANCEMENT" || attemptState != "SUCCEEDED" {
		t.Fatalf("terminal attempt kind=%s state=%s", attemptKind, attemptState)
	}

	var state string
	if err := f.pool.QueryRow(f.ctx, "SELECT state::text FROM media_asset_versions WHERE id=$1::uuid", versionID).Scan(&state); err != nil {
		t.Fatalf("reading media state: %v", err)
	}
	if state != "READY" {
		t.Fatalf("media state = %s, want READY", state)
	}
	// READY closes automatic recovery. A permanently outstanding row would be a
	// false claim about work that is finished.
	if _, present := readAutoRecoveryRow(t, f, versionID); present {
		t.Fatal("automatic recovery state outlived a successful READY")
	}
	if !log.saw(AutoRecoveryExecutionLinked) || !log.saw(AutoRecoveryReady) {
		t.Fatalf("observations = %v, want a linked execution and a READY", log.phases())
	}
}

// TestAutoRecoveryFinalizationNeedsNoOutput is the zero-output path: the ladder
// is already complete and only the READY transition is missing. The automatic
// route must reach it without re-encoding anything.
func TestAutoRecoveryFinalizationNeedsNoOutput(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p", "240p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, _ := autoRecoveryWorker(t, f, &now, processor)

	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("scheduling: scheduled=%d err=%v", scheduled, err)
	}
	intentID := latestAutoIntent(t, f, versionID)
	if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, intentID); err != nil {
		t.Fatalf("automatic finalization: %v", err)
	}
	processor.mu.Lock()
	encodes := processor.encodes
	processor.mu.Unlock()
	if encodes != 0 {
		t.Fatalf("automatic finalization encoded %d times, want 0", encodes)
	}
	var state, kind string
	var renditions int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT mav.state::text,
		       (SELECT pa.attempt_kind::text FROM processing_attempts pa
		         WHERE pa.asset_version_id=mav.id AND pa.state='SUCCEEDED' ORDER BY pa.completed_at DESC LIMIT 1),
		       (SELECT count(*) FROM video_renditions vr WHERE vr.asset_version_id=mav.id)
		FROM media_asset_versions mav WHERE mav.id=$1::uuid
	`, versionID).Scan(&state, &kind, &renditions); err != nil {
		t.Fatalf("reading finalization result: %v", err)
	}
	if state != "READY" || kind != "FINALIZATION" || renditions != 4 {
		t.Fatalf("finalization result: state=%s kind=%s renditions=%d", state, kind, renditions)
	}
	if _, present := readAutoRecoveryRow(t, f, versionID); present {
		t.Fatal("automatic recovery state outlived a successful finalization")
	}
}

// TestAutoRecoveryBudgetBackoffAndExhaustion pins the automatic retry policy.
//
// The deadlines are read back from the database rather than computed in the test
// process, because a schedule held in any process would not survive a restart.
func TestAutoRecoveryBudgetBackoffAndExhaustion(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	// failAfter 0 means the encoder dies before writing anything, so every attempt
	// is a no-progress failure and the budget really is consumed.
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: 0}
	worker, log := autoRecoveryWorker(t, f, &now, processor)

	wantBackoff := []time.Duration{15 * time.Minute, time.Hour, 0}
	for attempt := 1; attempt <= MaxAutoEnhancementFailures; attempt++ {
		if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
			t.Fatalf("attempt %d scheduling: scheduled=%d err=%v", attempt, scheduled, err)
		}
		intentID := latestAutoIntent(t, f, versionID)
		if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, intentID); err == nil {
			t.Fatalf("attempt %d unexpectedly succeeded", attempt)
		}
		row, present := readAutoRecoveryRow(t, f, versionID)
		if !present {
			t.Fatalf("attempt %d left no scheduler row", attempt)
		}
		if row.failures != attempt {
			t.Fatalf("attempt %d consecutive_failures=%d, want %d", attempt, row.failures, attempt)
		}
		if row.failureCategory == nil || *row.failureCategory != string(failureTranscode) {
			t.Fatalf("attempt %d failure category=%v, want %s", attempt, row.failureCategory, failureTranscode)
		}
		if attempt < MaxAutoEnhancementFailures {
			if row.state != autoRecoveryBackoff || row.nextAttemptAt == nil {
				t.Fatalf("attempt %d state=%s next=%v, want BACKOFF with a deadline", attempt, row.state, row.nextAttemptAt)
			}
			// The deadline is a database timestamp. Compared against the database
			// clock, with a tolerance only for the seconds the test itself took.
			var delta time.Duration
			if err := f.pool.QueryRow(f.ctx,
				"SELECT next_attempt_at - now() FROM media_auto_enhancement_recovery WHERE asset_version_id=$1::uuid",
				versionID).Scan(&delta); err != nil {
				t.Fatalf("reading backoff deadline: %v", err)
			}
			want := wantBackoff[attempt-1]
			if delta > want || delta < want-time.Minute {
				t.Fatalf("attempt %d backoff = %s, want about %s", attempt, delta, want)
			}
			// Not due yet, so the reconciler must not schedule again.
			if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 0 {
				t.Fatalf("attempt %d rescheduled before its deadline: scheduled=%d err=%v", attempt, scheduled, err)
			}
			// Bring the deadline forward the way the passage of time would.
			if _, err := f.pool.Exec(f.ctx,
				"UPDATE media_auto_enhancement_recovery SET next_attempt_at=now()-interval '1 second' WHERE asset_version_id=$1::uuid",
				versionID); err != nil {
				t.Fatalf("advancing the deadline: %v", err)
			}
			continue
		}
		// The third consecutive failure does not schedule a fourth attempt.
		if row.state != autoRecoveryNeedsOperator {
			t.Fatalf("attempt %d state=%s, want NEEDS_OPERATOR", attempt, row.state)
		}
		if row.nextAttemptAt != nil || row.intentID != nil || row.operationID != nil {
			t.Fatalf("exhausted row still carries schedule or intent: %+v", row)
		}
	}
	// Exhausted means exhausted: further ticks write nothing.
	before := countEnhancementIntents(t, f, versionID)
	for tick := 0; tick < 10; tick++ {
		if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 0 {
			t.Fatalf("exhausted tick %d scheduled=%d err=%v", tick, scheduled, err)
		}
	}
	if after := countEnhancementIntents(t, f, versionID); after != before {
		t.Fatalf("exhausted asset gained %d intents", after-before)
	}
	if !log.saw(AutoRecoveryExhausted) {
		t.Fatalf("observations = %v, want an exhaustion", log.phases())
	}
	// And the exhaustion is auditable.
	var audits int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM audit_events
		WHERE action='MEDIA_AUTO_ENHANCEMENT_EXHAUSTED' AND target_id=$1 AND actor_role='SYSTEM'
	`, versionID).Scan(&audits); err != nil {
		t.Fatalf("reading exhaustion audit: %v", err)
	}
	if audits != 1 {
		t.Fatalf("exhaustion audit events = %d, want 1", audits)
	}
}

// TestAutoRecoveryProgressResetsBudget is the progress rule. A rung that was
// really committed is real forward movement even when the operation that wrote it
// then died, and the evidence is the canonical video_renditions row — never the
// encoder's own account of itself.
func TestAutoRecoveryProgressResetsBudget(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p"})
	now := time.Now().UTC()
	// Fails after writing exactly one of the two missing rungs.
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: 1}
	worker, log := autoRecoveryWorker(t, f, &now, processor)

	// First, a no-progress failure to put a charge on the budget.
	processor.mu.Lock()
	processor.failAfter = 0
	processor.mu.Unlock()
	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("first scheduling: scheduled=%d err=%v", scheduled, err)
	}
	if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, latestAutoIntent(t, f, versionID)); err == nil {
		t.Fatal("the no-progress attempt unexpectedly succeeded")
	}
	row, _ := readAutoRecoveryRow(t, f, versionID)
	if row.failures != 1 {
		t.Fatalf("consecutive_failures after a no-progress failure = %d, want 1", row.failures)
	}

	// Now an attempt that commits one new rung and then fails.
	if _, err := f.pool.Exec(f.ctx,
		"UPDATE media_auto_enhancement_recovery SET next_attempt_at=now()-interval '1 second' WHERE asset_version_id=$1::uuid",
		versionID); err != nil {
		t.Fatalf("advancing the deadline: %v", err)
	}
	processor.mu.Lock()
	processor.failAfter = 1
	processor.mu.Unlock()
	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("second scheduling: scheduled=%d err=%v", scheduled, err)
	}
	before, _ := readAutoRecoveryRow(t, f, versionID)
	if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, latestAutoIntent(t, f, versionID)); err == nil {
		t.Fatal("the partial-progress attempt unexpectedly succeeded")
	}
	after, _ := readAutoRecoveryRow(t, f, versionID)
	var rungs int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM video_renditions WHERE asset_version_id=$1::uuid", versionID).Scan(&rungs); err != nil {
		t.Fatalf("counting canonical rungs: %v", err)
	}
	if rungs <= before.progressBase {
		t.Fatalf("canonical rungs = %d, want more than the %d baseline", rungs, before.progressBase)
	}
	if after.failures != 0 {
		t.Fatalf("consecutive_failures after progress = %d, want the budget reset to 0", after.failures)
	}
	if after.state != autoRecoveryBackoff {
		t.Fatalf("state after progress = %s, want BACKOFF", after.state)
	}
	if after.progressBase != rungs {
		t.Fatalf("progress baseline = %d, want the new canonical count %d", after.progressBase, rungs)
	}
	if !log.saw(AutoRecoveryProgressMade) {
		t.Fatalf("observations = %v, want progress", log.phases())
	}
}

// TestAutoRecoveryPermanentFailureDoesNotLoop keeps the budget off failures that
// another attempt cannot change. Re-encoding a source whose bytes are not the
// bytes the scan passed produces the same failure forever.
func TestAutoRecoveryPermanentFailureDoesNotLoop(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &invalidMediaEnhancementProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}}
	worker, _ := autoRecoveryWorker(t, f, &now, processor)

	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("scheduling: scheduled=%d err=%v", scheduled, err)
	}
	if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, latestAutoIntent(t, f, versionID)); err == nil {
		t.Fatal("an invalid-media attempt unexpectedly succeeded")
	}
	row, present := readAutoRecoveryRow(t, f, versionID)
	if !present || row.state != autoRecoveryNeedsOperator {
		t.Fatalf("state after a permanent failure = %+v present=%t, want NEEDS_OPERATOR on the first failure", row, present)
	}
	if row.failures != 1 {
		t.Fatalf("consecutive_failures = %d, want 1; the budget must not be silently spent", row.failures)
	}
	if row.nextAttemptAt != nil {
		t.Fatal("a permanent failure scheduled another automatic attempt")
	}
	for tick := 0; tick < 5; tick++ {
		if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 0 {
			t.Fatalf("permanent-failure tick %d scheduled=%d err=%v", tick, scheduled, err)
		}
	}
}

type invalidMediaEnhancementProcessor struct {
	probe EnhancementProbe
}

func (p *invalidMediaEnhancementProcessor) Transcode(context.Context, ObjectVersion) (TranscodeResult, error) {
	return TranscodeResult{}, errors.New("full path unused")
}

func (p *invalidMediaEnhancementProcessor) ProbeExpected(context.Context, ObjectVersion) (EnhancementProbe, error) {
	return p.probe, nil
}

func (p *invalidMediaEnhancementProcessor) TranscodeMissing(context.Context, ObjectVersion, []string, ProgressSink, VerifiedRenditionSink) (TranscodeResult, error) {
	return TranscodeResult{}, fmt.Errorf("%w: source checksum does not match the scanned bytes", ErrInvalidMedia)
}

// TestAutoRecoveryStaleRecoveryReconcilesCrashedExecution is the crash edge.
//
// The worker takes the claim for an automatic intent and then disappears. It will
// never report its own outcome — but the claim token IS the operation the
// scheduler row was bound to, so the interrupted attempt is still attributable by
// stale recovery, and the intent does not stay EXECUTING forever.
func TestAutoRecoveryStaleRecoveryReconcilesCrashedExecution(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, log := autoRecoveryWorker(t, f, &now, processor)

	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("scheduling: scheduled=%d err=%v", scheduled, err)
	}
	intentID := latestAutoIntent(t, f, versionID)

	// Take the claim and then stop, which is what a crash looks like from the
	// database's point of view.
	operationID := uuid.NewString()
	if _, claimed, err := worker.beginEnhancement(f.ctx, versionID, operationID, intentID); err != nil || !claimed {
		t.Fatalf("automatic claim: claimed=%t err=%v", claimed, err)
	}
	row, _ := readAutoRecoveryRow(t, f, versionID)
	if row.state != autoRecoveryExecuting || row.operationID == nil || *row.operationID != operationID {
		t.Fatalf("row after the claim = %+v, want EXECUTING bound to %s", row, operationID)
	}

	// The linkage is a committed fact, so it survives losing the process entirely.
	// A brand new worker — nothing in common with the one that crashed, and no
	// Redis at all — reconciles it.
	restarted, restartedLog := autoRecoveryWorker(t, f, &now, processor)
	expireLease(t, f, versionID)
	if recovered, err := restarted.RecoverStale(f.ctx, 25); err != nil || recovered != 1 {
		t.Fatalf("stale recovery: recovered=%d err=%v", recovered, err)
	}

	after, present := readAutoRecoveryRow(t, f, versionID)
	if !present {
		t.Fatal("stale recovery deleted the automatic recovery row")
	}
	if after.state != autoRecoveryBackoff {
		t.Fatalf("state after stale recovery = %s, want BACKOFF; EXECUTING must not be permanent", after.state)
	}
	if after.failures != 1 {
		t.Fatalf("consecutive_failures after an interrupted execution = %d, want 1", after.failures)
	}
	if after.failureCategory == nil || *after.failureCategory != string(failureWorkerInterrupted) {
		t.Fatalf("failure category = %v, want %s", after.failureCategory, failureWorkerInterrupted)
	}
	if after.operationID != nil || after.intentID != nil {
		t.Fatalf("reconciled row still carries an execution: %+v", after)
	}

	// The interrupted attempt is attributed to the intent, by the authoritative
	// link rather than by anything the crashed process could have told us.
	var attemptIntent *string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT auto_recovery_intent_id::text FROM processing_attempts
		WHERE asset_version_id=$1::uuid AND operation_id=$2 AND state='FAILED'
	`, versionID, operationID).Scan(&attemptIntent); err != nil {
		t.Fatalf("reading the interrupted attempt: %v", err)
	}
	if attemptIntent == nil || *attemptIntent != intentID {
		t.Fatalf("interrupted attempt intent = %v, want %s", attemptIntent, intentID)
	}
	_ = log
	if !restartedLog.saw(AutoRecoveryBackoffScheduled) {
		t.Fatalf("restarted worker observations = %v, want a backoff", restartedLog.phases())
	}
}

// TestAutoRecoveryRedisLossReissuesIntentAndSupersedesTheOldTask is the Redis
// edge. The queue is gone; the scheduler row is not. The asset is scheduled again
// under a NEW intent, and the old task — if it ever arrives — is a no-op.
// markIntentDispatched records the durable dispatch receipt the media dispatcher
// writes when it has actually handed an outbox event to the queue. The intent id
// IS the event id, so this is the same row the production dispatcher inserts.
//
// It is the discriminator the scheduler uses to tell "the queue accepted this and
// lost it" from "this never left the outbox".
func markIntentDispatched(t *testing.T, f *mediaFixture, intentID string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx,
		"INSERT INTO media_outbox_dispatches (event_id) VALUES ($1::uuid) ON CONFLICT (event_id) DO NOTHING",
		intentID); err != nil {
		t.Fatalf("recording the dispatch receipt: %v", err)
	}
}

func intentIsDispatched(t *testing.T, f *mediaFixture, intentID string) bool {
	t.Helper()
	var present bool
	if err := f.pool.QueryRow(f.ctx,
		"SELECT EXISTS (SELECT 1 FROM media_outbox_dispatches WHERE event_id = $1::uuid)",
		intentID).Scan(&present); err != nil {
		t.Fatalf("reading the dispatch receipt: %v", err)
	}
	return present
}

func TestAutoRecoveryRedisLossReissuesIntentAndSupersedesTheOldTask(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, log := autoRecoveryWorker(t, f, &now, processor)

	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("scheduling: scheduled=%d err=%v", scheduled, err)
	}
	lostIntent := latestAutoIntent(t, f, versionID)
	// The dispatcher published it and Redis then lost the task. The receipt is
	// what makes this a lost DISPATCHED task rather than an event still sitting
	// undispatched in the outbox, and only the former may be re-issued.
	markIntentDispatched(t, f, lostIntent)

	// While the intent is live the asset is not a candidate, whatever happened to
	// the queue: the database is the authority on what is outstanding.
	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 0 {
		t.Fatalf("a live intent was re-scheduled: scheduled=%d err=%v", scheduled, err)
	}
	// Past its lease, the intent is no longer believable and the asset is
	// scheduled again — with a new identity, not the lost one.
	if _, err := f.pool.Exec(f.ctx,
		"UPDATE media_auto_enhancement_recovery SET intent_expires_at=now()-interval '1 second' WHERE asset_version_id=$1::uuid",
		versionID); err != nil {
		t.Fatalf("expiring the intent: %v", err)
	}
	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("expired intent was not re-issued: scheduled=%d err=%v", scheduled, err)
	}
	reissued := latestAutoIntent(t, f, versionID)
	if reissued == lostIntent {
		t.Fatal("the re-issued intent reused the lost identity")
	}
	row, _ := readAutoRecoveryRow(t, f, versionID)
	if row.attemptNumber != 2 {
		t.Fatalf("attempt_number after re-issue = %d, want 2", row.attemptNumber)
	}
	if row.failures != 0 {
		t.Fatalf("re-issuing an unstarted intent charged %d failures, want 0", row.failures)
	}

	// The lost task finally arrives. It must claim nothing and charge nothing.
	err := worker.RetryEnhancementsForIntent(f.ctx, versionID, lostIntent)
	if !errors.Is(err, ErrEnhancementIntentSuperseded) {
		t.Fatalf("superseded task error = %v, want ErrEnhancementIntentSuperseded", err)
	}
	stillThere, _ := readAutoRecoveryRow(t, f, versionID)
	if stillThere.state != autoRecoveryScheduled || stillThere.intentID == nil || *stillThere.intentID != reissued {
		t.Fatalf("a superseded task disturbed the live intent: %+v", stillThere)
	}
	if stillThere.failures != 0 {
		t.Fatalf("a superseded task charged %d failures", stillThere.failures)
	}
	var claim *string
	if err := f.pool.QueryRow(f.ctx, "SELECT work_claim_token FROM media_asset_versions WHERE id=$1::uuid", versionID).Scan(&claim); err != nil {
		t.Fatalf("reading the claim: %v", err)
	}
	if claim != nil {
		t.Fatalf("a superseded task took the media claim: %v", claim)
	}
	if !log.saw(AutoRecoveryIntentScheduled) {
		t.Fatalf("observations = %v", log.phases())
	}
	// And the live intent still runs.
	if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, reissued); err != nil {
		t.Fatalf("the re-issued intent did not execute: %v", err)
	}
}

// TestAutoRecoveryLegacyManualPayloadStillWorks is the payload compatibility
// contract. A task written before 3C-C carries only asset_version_id, and the
// currently deployed 3C-B path keeps writing exactly that shape.
func TestAutoRecoveryLegacyManualPayloadStillWorks(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, _ := autoRecoveryWorker(t, f, &now, processor)

	// The exact bytes the deployed 3C-B dispatcher hands the worker.
	var legacy EnhancementWork
	if err := json.Unmarshal([]byte(`{"asset_version_id":"`+versionID+`"}`), &legacy); err != nil {
		t.Fatalf("decoding a legacy enhancement payload: %v", err)
	}
	if legacy.AssetVersionID != versionID || legacy.AutoRecoveryIntentID != "" {
		t.Fatalf("legacy payload decoded to %+v", legacy)
	}
	if err := worker.RetryEnhancementsForIntent(f.ctx, legacy.AssetVersionID, legacy.AutoRecoveryIntentID); err != nil {
		t.Fatalf("legacy manual payload execution: %v", err)
	}
	var state string
	if err := f.pool.QueryRow(f.ctx, "SELECT state::text FROM media_asset_versions WHERE id=$1::uuid", versionID).Scan(&state); err != nil {
		t.Fatalf("reading media state: %v", err)
	}
	if state != "READY" {
		t.Fatalf("media state after a legacy manual payload = %s, want READY", state)
	}
	// A manual attempt is attributed to no automatic intent.
	var attributed int
	if err := f.pool.QueryRow(f.ctx,
		"SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid AND auto_recovery_intent_id IS NOT NULL",
		versionID).Scan(&attributed); err != nil {
		t.Fatalf("counting attributed attempts: %v", err)
	}
	if attributed != 0 {
		t.Fatalf("a manual execution attributed %d attempts to an automatic intent", attributed)
	}
}

// TestAutoRecoveryManualOverrideSupersedesAutomaticState is the operator
// override.
//
// Manual retry stays available from NEEDS_OPERATOR, resets the automatic budget,
// supersedes any queued automatic task, and does not delete historical evidence.
func TestAutoRecoveryManualOverrideSupersedesAutomaticState(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, log := autoRecoveryWorker(t, f, &now, processor)

	// Put the asset in NEEDS_OPERATOR with a queued automatic intent outstanding,
	// and leave a failed automatic attempt behind as historical evidence.
	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("scheduling: scheduled=%d err=%v", scheduled, err)
	}
	staleIntent := latestAutoIntent(t, f, versionID)
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_auto_enhancement_recovery
		SET state='NEEDS_OPERATOR', current_intent_id=NULL, executing_operation_id=NULL,
		    intent_expires_at=NULL, next_attempt_at=NULL, consecutive_failures=3,
		    last_failure_category='TRANSCODE_FAILED'
		WHERE asset_version_id=$1::uuid
	`, versionID); err != nil {
		t.Fatalf("staging NEEDS_OPERATOR: %v", err)
	}
	var historicalBefore int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid", versionID).Scan(&historicalBefore); err != nil {
		t.Fatalf("counting historical attempts: %v", err)
	}

	// The queued automatic task from before the operator stepped in is superseded,
	// because NEEDS_OPERATOR names no intent.
	if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, staleIntent); !errors.Is(err, ErrEnhancementIntentSuperseded) {
		t.Fatalf("stale automatic task from NEEDS_OPERATOR = %v, want superseded", err)
	}

	// Manual retry is available from NEEDS_OPERATOR — it is the override.
	manualOperation := uuid.NewString()
	if _, claimed, err := worker.beginEnhancement(f.ctx, versionID, manualOperation, ""); err != nil || !claimed {
		t.Fatalf("manual claim from NEEDS_OPERATOR: claimed=%t err=%v", claimed, err)
	}
	row, present := readAutoRecoveryRow(t, f, versionID)
	if !present {
		t.Fatal("the manual override removed the automatic state row entirely")
	}
	if row.state != autoRecoveryBackoff || row.failures != 0 {
		t.Fatalf("row after the manual override = %+v, want BACKOFF with the budget reset", row)
	}
	if row.nextAttemptAt == nil {
		t.Fatal("the manual override left the scheduler free to mint a duplicate intent immediately")
	}
	if row.intentID != nil || row.operationID != nil {
		t.Fatalf("the manual override left an automatic intent outstanding: %+v", row)
	}
	// The scheduler does not immediately mint a duplicate behind the operator.
	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 0 {
		t.Fatalf("the scheduler raced the operator: scheduled=%d err=%v", scheduled, err)
	}
	// Historical evidence is untouched.
	var historicalAfter int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid", versionID).Scan(&historicalAfter); err != nil {
		t.Fatalf("counting historical attempts: %v", err)
	}
	if historicalAfter != historicalBefore {
		t.Fatalf("the manual override changed historical attempts from %d to %d", historicalBefore, historicalAfter)
	}
	if !log.saw(AutoRecoveryManualOverride) {
		t.Fatalf("observations = %v, want a manual override", log.phases())
	}
}

// TestAutoRecoveryClaimRaceIsNotCharged keeps the budget off races the scheduler
// was right to open. Another worker owning the work is not an automatic failure.
func TestAutoRecoveryClaimRaceIsNotCharged(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, log := autoRecoveryWorker(t, f, &now, processor)

	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("scheduling: scheduled=%d err=%v", scheduled, err)
	}
	intentID := latestAutoIntent(t, f, versionID)
	// Another worker got there first and holds a live claim.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET work_claim_token='other-worker', processing_attempt_token='other-worker',
		  work_claimed_at=now(), work_lease_expires_at=now()+interval '1 hour',
		  active_processing_attempt_kind='ENHANCEMENT'
		WHERE id=$1::uuid
	`, versionID); err != nil {
		t.Fatalf("staging the competing claim: %v", err)
	}

	err := worker.RetryEnhancementsForIntent(f.ctx, versionID, intentID)
	if !errors.Is(err, ErrEnhancementActive) {
		t.Fatalf("claim race error = %v, want ErrEnhancementActive", err)
	}
	row, present := readAutoRecoveryRow(t, f, versionID)
	if !present {
		t.Fatal("a claim race removed the automatic state")
	}
	if row.failures != 0 {
		t.Fatalf("a claim race charged %d automatic failures", row.failures)
	}
	// The intent is released rather than left SCHEDULED until its lease expires.
	if row.state != autoRecoveryBackoff || row.nextAttemptAt == nil {
		t.Fatalf("row after a claim race = %+v, want BACKOFF with a deadline", row)
	}
	if !log.saw(AutoRecoveryIntentReleased) {
		t.Fatalf("observations = %v, want a released intent", log.phases())
	}
}

// TestAutoRecoverySchedulerNeverTakesTheClaim is the separation of duties: the
// reconciler writes intent, and the execution-time worker is the only claimant.
func TestAutoRecoverySchedulerNeverTakesTheClaim(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, _ := autoRecoveryWorker(t, f, &now, processor)

	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("scheduling: scheduled=%d err=%v", scheduled, err)
	}
	var claim, token, kind *string
	var stage *string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT work_claim_token, processing_attempt_token, active_processing_attempt_kind::text, processing_stage::text
		FROM media_asset_versions WHERE id=$1::uuid
	`, versionID).Scan(&claim, &token, &kind, &stage); err != nil {
		t.Fatalf("reading media work state: %v", err)
	}
	if claim != nil || kind != nil {
		t.Fatalf("the scheduler took a media claim: claim=%v kind=%v", claim, kind)
	}
	var attempts int
	if err := f.pool.QueryRow(f.ctx,
		"SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid AND state='SUCCEEDED'",
		versionID).Scan(&attempts); err != nil {
		t.Fatalf("counting attempts: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("the scheduler produced %d successful attempts", attempts)
	}
	// And it wrote its decision to the audit log without a human actor.
	var audits int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM audit_events
		WHERE action='MEDIA_AUTO_ENHANCEMENT_SCHEDULED' AND target_id=$1
		  AND actor_role='SYSTEM' AND actor_account_id IS NULL
	`, versionID).Scan(&audits); err != nil {
		t.Fatalf("reading scheduling audit: %v", err)
	}
	if audits != 1 {
		t.Fatalf("scheduling audit events = %d, want 1", audits)
	}
}

// TestAutoRecoveryUndispatchedIntentStaysBoundedAcrossExpiry is the G0-1
// regression.
//
// The failure it pins: during a sustained dispatcher or Redis outage the
// enhancement event never leaves the outbox, but scheduler time keeps elapsing.
// If expiry alone authorised replacement, every lease window would mint a new
// intent and a new enhancement event while the previous ones stayed
// undispatched and still dispatchable. When the dispatcher recovered, every one
// of those distinct events would dispatch — unbounded growth from a pure
// outage, which the bounded-growth invariant forbids.
//
// Elapsed time is therefore not sufficient. Replacement additionally requires a
// durable dispatch receipt for the intent's event.
func TestAutoRecoveryUndispatchedIntentStaysBoundedAcrossExpiry(t *testing.T) {
	f, _, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p"})
	now := time.Now().UTC()
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker, _ := autoRecoveryWorker(t, f, &now, processor)

	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("initial scheduling: scheduled=%d err=%v", scheduled, err)
	}
	original := latestAutoIntent(t, f, versionID)
	// The dispatcher is down for the whole of this test: no receipt is ever
	// written, exactly as production would leave it during a Redis outage.
	if intentIsDispatched(t, f, original) {
		t.Fatal("the fixture dispatched the intent; this test requires an undispatched one")
	}

	// Drive DB-authoritative time through many expiry windows. Expiry is read
	// from the database clock, so expiring the row is how the scheduler sees
	// elapsed leases.
	for tick := 0; tick < 1000; tick++ {
		if _, err := f.pool.Exec(f.ctx,
			"UPDATE media_auto_enhancement_recovery SET intent_expires_at=now()-interval '1 hour' WHERE asset_version_id=$1::uuid",
			versionID); err != nil {
			t.Fatalf("tick %d: advancing past the lease: %v", tick, err)
		}
		scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25)
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if scheduled != 0 {
			t.Fatalf("tick %d minted a new intent for an undispatched event", tick)
		}
	}

	// One logical current intent, and one relevant undispatched outbox event.
	row, present := readAutoRecoveryRow(t, f, versionID)
	if !present || row.state != autoRecoveryScheduled {
		t.Fatalf("scheduler row after 1000 expiry windows = %+v present=%t", row, present)
	}
	if row.intentID == nil || *row.intentID != original {
		t.Fatalf("the durable intent identity changed: %+v, want %s", row.intentID, original)
	}
	if row.attemptNumber != 1 {
		t.Fatalf("attempt_number = %d, want 1; the budget was charged by elapsed time alone", row.attemptNumber)
	}
	if row.failures != 0 {
		t.Fatalf("an undispatched intent charged %d failures", row.failures)
	}
	if got := countEnhancementIntents(t, f, versionID); got != 1 {
		t.Fatalf("1000 expiry windows wrote %d enhancement events, want exactly 1", got)
	}
	var schedulerRows int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM media_auto_enhancement_recovery").Scan(&schedulerRows); err != nil {
		t.Fatalf("counting scheduler rows: %v", err)
	}
	if schedulerRows != 1 {
		t.Fatalf("scheduler rows = %d, want 1", schedulerRows)
	}

	// Now the dispatcher comes back and publishes that same event. Only after the
	// receipt exists does an expired lease authorise a new identity — which is
	// the "dispatched but never claimed" case, and is bounded because each
	// re-issue costs a real dispatch first.
	markIntentDispatched(t, f, original)
	if _, err := f.pool.Exec(f.ctx,
		"UPDATE media_auto_enhancement_recovery SET intent_expires_at=now()-interval '1 second' WHERE asset_version_id=$1::uuid",
		versionID); err != nil {
		t.Fatalf("expiring the dispatched intent: %v", err)
	}
	if scheduled, err := worker.ScheduleAutoEnhancementRecovery(f.ctx, 25); err != nil || scheduled != 1 {
		t.Fatalf("a dispatched, unclaimed, expired intent was not re-issued: scheduled=%d err=%v", scheduled, err)
	}
	reissued := latestAutoIntent(t, f, versionID)
	if reissued == original {
		t.Fatal("the re-issued intent reused the dispatched identity")
	}

	// Restored dispatcher: the superseded work must no-op through the existing
	// intent identity check, so only the authoritative logical work is effective.
	if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, original); !errors.Is(err, ErrEnhancementIntentSuperseded) {
		t.Fatalf("the superseded task error = %v, want ErrEnhancementIntentSuperseded", err)
	}
	var claim *string
	if err := f.pool.QueryRow(f.ctx, "SELECT work_claim_token FROM media_asset_versions WHERE id=$1::uuid", versionID).Scan(&claim); err != nil {
		t.Fatalf("reading the claim: %v", err)
	}
	if claim != nil {
		t.Fatalf("a superseded task took the media claim: %v", claim)
	}
	if err := worker.RetryEnhancementsForIntent(f.ctx, versionID, reissued); err != nil {
		t.Fatalf("the authoritative intent did not execute: %v", err)
	}
}
