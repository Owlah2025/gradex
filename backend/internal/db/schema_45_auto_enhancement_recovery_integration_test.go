//go:build integration

package db

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedMediaAssetVersionFixture creates the minimum Account, Course, logical
// Asset and Asset Version an automatic recovery row can reference. The label
// keeps the fixture addresses unique across tests sharing one database.
func seedMediaAssetVersionFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	label string,
) string {
	t.Helper()
	var accountID, courseID, assetID, versionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ($1, $1, 'ADMIN', 'ACTIVE', 'Auto Recovery Owner') RETURNING id::text
	`, label+"-owner@example.test").Scan(&accountID); err != nil {
		t.Fatalf("seeding %s account: %v", label, err)
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO courses (owner_account_id, lifecycle) VALUES ($1::uuid, 'DRAFT') RETURNING id::text",
		accountID).Scan(&courseID); err != nil {
		t.Fatalf("seeding %s Course: %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_assets (kind, owner_account_id, course_id, visibility)
		VALUES ('VIDEO', $1::uuid, $2::uuid, 'PROTECTED') RETURNING id::text
	`, accountID, courseID).Scan(&assetID); err != nil {
		t.Fatalf("seeding %s media asset: %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_asset_versions
		  (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, 'VIDEO', 'SCANNING', 'k', 'v', 'video/mp4', 100) RETURNING id::text
	`, assetID).Scan(&versionID); err != nil {
		t.Fatalf("seeding %s asset version: %v", label, err)
	}
	return versionID
}

// TestSchema45AutoEnhancementRecoveryRepresentation proves 0045 adds exactly the
// durable state the scheduler needs and refuses every contradictory shape.
//
// The state machine is enforced by maer_state_coherent rather than by Go, which
// is the point: a row claiming to execute an operation it never bound, or
// claiming an intent is outstanding with nothing to identify it, would make
// attribution heuristic. The constraint is what makes that unrepresentable.
func TestSchema45AutoEnhancementRecoveryRepresentation(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(AutoEnhancementRecoverySchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 45: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	versionID := seedMediaAssetVersionFixture(t, ctx, pool, "auto45")
	intent := "45000000-0000-0000-0000-00000000aaaa"

	// The legal shapes, one per state.
	legal := []struct {
		name   string
		insert string
		args   []any
	}{
		{
			name: "SCHEDULED carries an intent, no operation, and an expiry",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, current_intent_id, intent_expires_at, attempt_number)
			 VALUES ($1::uuid, 'SCHEDULED', $2::uuid, now() + interval '1 hour', 1)`,
			args: []any{versionID, intent},
		},
		{
			name: "EXECUTING binds the operation the worker minted",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, current_intent_id, executing_operation_id, attempt_number)
			 VALUES ($1::uuid, 'EXECUTING', $2::uuid, 'op-1', 1)`,
			args: []any{versionID, intent},
		},
		{
			name: "BACKOFF has no outstanding intent and a deadline",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, consecutive_failures, attempt_number, next_attempt_at)
			 VALUES ($1::uuid, 'BACKOFF', 1, 1, now() + interval '15 minutes')`,
			args: []any{versionID},
		},
		{
			name: "NEEDS_OPERATOR has nothing outstanding and no deadline",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, consecutive_failures, attempt_number)
			 VALUES ($1::uuid, 'NEEDS_OPERATOR', 3, 3)`,
			args: []any{versionID},
		},
	}
	for _, shape := range legal {
		if _, err := pool.Exec(ctx, shape.insert, shape.args...); err != nil {
			t.Fatalf("schema 45 refused a legal shape (%s): %v", shape.name, err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM media_auto_enhancement_recovery WHERE asset_version_id = $1::uuid", versionID); err != nil {
			t.Fatalf("clearing %s: %v", shape.name, err)
		}
	}

	// The contradictory shapes. Each one is a way attribution could have been
	// made heuristic, and each is refused by name.
	contradictory := []struct {
		name       string
		constraint string
		insert     string
		args       []any
	}{
		{
			name:       "EXECUTING without a bound operation",
			constraint: "maer_state_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, current_intent_id, attempt_number)
			 VALUES ($1::uuid, 'EXECUTING', $2::uuid, 1)`,
			args: []any{versionID, intent},
		},
		{
			name:       "EXECUTING without an intent",
			constraint: "maer_state_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, executing_operation_id, attempt_number)
			 VALUES ($1::uuid, 'EXECUTING', 'op-1', 1)`,
			args: []any{versionID},
		},
		{
			name:       "SCHEDULED without an intent",
			constraint: "maer_state_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, intent_expires_at, attempt_number)
			 VALUES ($1::uuid, 'SCHEDULED', now() + interval '1 hour', 1)`,
			args: []any{versionID},
		},
		{
			name:       "SCHEDULED without an expiry cannot be recovered after Redis loss",
			constraint: "maer_state_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, current_intent_id, attempt_number)
			 VALUES ($1::uuid, 'SCHEDULED', $2::uuid, 1)`,
			args: []any{versionID, intent},
		},
		{
			name:       "SCHEDULED at attempt zero",
			constraint: "maer_state_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, current_intent_id, intent_expires_at, attempt_number)
			 VALUES ($1::uuid, 'SCHEDULED', $2::uuid, now() + interval '1 hour', 0)`,
			args: []any{versionID, intent},
		},
		{
			name:       "BACKOFF still holding an intent",
			constraint: "maer_state_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, current_intent_id, attempt_number, next_attempt_at)
			 VALUES ($1::uuid, 'BACKOFF', $2::uuid, 1, now() + interval '15 minutes')`,
			args: []any{versionID, intent},
		},
		{
			name:       "BACKOFF with no deadline would never become eligible again",
			constraint: "maer_state_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, attempt_number)
			 VALUES ($1::uuid, 'BACKOFF', 1)`,
			args: []any{versionID},
		},
		{
			name:       "NEEDS_OPERATOR with a deadline would resume automatically",
			constraint: "maer_state_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, attempt_number, next_attempt_at)
			 VALUES ($1::uuid, 'NEEDS_OPERATOR', 3, now() + interval '4 hours')`,
			args: []any{versionID},
		},
		{
			// MANUAL_PENDING must name the manual request doing the suppressing.
			// An anonymous suppression could never be released by the work that
			// caused it, and could never be told apart from a newer one.
			name:       "MANUAL_PENDING without a manual identity is anonymous suppression",
			constraint: "maer_manual_columns_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, attempt_number)
			 VALUES ($1::uuid, 'MANUAL_PENDING', 1)`,
			args: []any{versionID},
		},
		{
			name:       "MANUAL_PENDING without a deadline would suppress forever",
			constraint: "maer_manual_columns_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, attempt_number, manual_intent_id)
			 VALUES ($1::uuid, 'MANUAL_PENDING', 1, gen_random_uuid())`,
			args: []any{versionID},
		},
		{
			// The manual columns belong to MANUAL_PENDING alone. A row that was
			// both manually suppressed and due would be read as due.
			name:       "a manual identity on a due row",
			constraint: "maer_manual_columns_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, attempt_number, next_attempt_at, manual_intent_id, manual_expires_at)
			 VALUES ($1::uuid, 'BACKOFF', 1, now(), gen_random_uuid(), now() + interval '2 hours')`,
			args: []any{versionID},
		},
		{
			// An accepted manual request supersedes the automatic intent, so
			// MANUAL_PENDING owns no automatic identity at all.
			name:       "MANUAL_PENDING still naming an automatic intent",
			constraint: "maer_state_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, attempt_number, current_intent_id, manual_intent_id, manual_expires_at)
			 VALUES ($1::uuid, 'MANUAL_PENDING', 1, gen_random_uuid(), gen_random_uuid(), now() + interval '2 hours')`,
			args: []any{versionID},
		},
		{
			name:       "MANUAL_PENDING with an automatic deadline would resume behind the operator",
			constraint: "maer_state_coherent",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, attempt_number, next_attempt_at, manual_intent_id, manual_expires_at)
			 VALUES ($1::uuid, 'MANUAL_PENDING', 1, now(), gen_random_uuid(), now() + interval '2 hours')`,
			args: []any{versionID},
		},
		{
			name:       "a negative failure count",
			constraint: "maer_counters_non_negative",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, consecutive_failures, attempt_number, next_attempt_at)
			 VALUES ($1::uuid, 'BACKOFF', -1, 1, now())`,
			args: []any{versionID},
		},
		{
			name:       "a blank operation identity",
			constraint: "maer_operation_present",
			insert: `INSERT INTO media_auto_enhancement_recovery
			   (asset_version_id, state, current_intent_id, executing_operation_id, attempt_number)
			 VALUES ($1::uuid, 'EXECUTING', $2::uuid, '   ', 1)`,
			args: []any{versionID, intent},
		},
	}
	for _, shape := range contradictory {
		assertConstraintViolation(t, pool, ctx, shape.constraint, shape.insert, shape.args...)
	}

	// An unknown state is refused without pinning which constraint reports it:
	// maer_state_valid and maer_state_coherent both reject it, because the
	// coherence rule enumerates the states it knows, and PostgreSQL does not
	// promise which of two violated CHECKs it names.
	assertStatementFails(t, pool, ctx,
		`INSERT INTO media_auto_enhancement_recovery
		   (asset_version_id, state, attempt_number, next_attempt_at)
		 VALUES ($1::uuid, 'RETRYING', 1, now())`,
		versionID,
	)

	// One row per Asset Version, guaranteed by the primary key rather than by the
	// scheduler being careful. This is the hard boundedness guarantee: no number
	// of ticks and no number of concurrent scheduler instances can produce a
	// second outstanding intent.
	if _, err := pool.Exec(ctx, `INSERT INTO media_auto_enhancement_recovery
	   (asset_version_id, state, current_intent_id, intent_expires_at, attempt_number)
	 VALUES ($1::uuid, 'SCHEDULED', $2::uuid, now() + interval '1 hour', 1)`, versionID, intent); err != nil {
		t.Fatalf("seeding the first intent: %v", err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO media_auto_enhancement_recovery
	   (asset_version_id, state, current_intent_id, intent_expires_at, attempt_number)
	 VALUES ($1::uuid, 'SCHEDULED', '45000000-0000-0000-0000-00000000bbbb'::uuid, now() + interval '1 hour', 2)`, versionID)
	if err == nil {
		t.Fatal("a second outstanding automatic intent was accepted for the same Asset Version")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second intent error = %v, want a unique violation", err)
	}

	// Attribution on the attempt, nullable because every attempt that exists
	// before schema 45 was manual or FULL and processing_attempts is append-only.
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts
		  (asset_version_id, operation_id, state, attempt_kind, rendition_count, error_reason, auto_recovery_intent_id)
		VALUES ($1::uuid, 'auto-op-1', 'FAILED', 'ENHANCEMENT', 0, 'encoder died', $2::uuid)
	`, versionID, intent); err != nil {
		t.Fatalf("recording an automatic attempt: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts
		  (asset_version_id, operation_id, state, attempt_kind, rendition_count, error_reason)
		VALUES ($1::uuid, 'manual-op-1', 'FAILED', 'ENHANCEMENT', 0, 'encoder died')
	`, versionID); err != nil {
		t.Fatalf("a manual attempt was refused without an automatic intent: %v", err)
	}

	// The attempt evidence must outlive the scheduler state it was produced
	// under, which is why the column is not a foreign key: the row is deleted
	// when the asset reaches READY, and the audit trail has to survive that.
	if _, err := pool.Exec(ctx,
		"DELETE FROM media_auto_enhancement_recovery WHERE asset_version_id = $1::uuid", versionID); err != nil {
		t.Fatalf("deleting the scheduler row on success: %v", err)
	}
	var retained string
	if err := pool.QueryRow(ctx, `
		SELECT auto_recovery_intent_id::text FROM processing_attempts
		 WHERE asset_version_id = $1::uuid AND operation_id = 'auto-op-1'
	`, versionID).Scan(&retained); err != nil {
		t.Fatalf("automatic attribution did not survive the scheduler row: %v", err)
	}
	if retained != intent {
		t.Fatalf("retained attribution = %q, want %q", retained, intent)
	}
}

// TestSchema45DownRefusesOutstandingIntents pins the rollback floor. Discarding
// a SCHEDULED or EXECUTING row would destroy the budget, the backoff deadline and
// the NEEDS_OPERATOR verdict for work the queue may still be about to run, and an
// asset an operator had been told to look at would look untouched.
func TestSchema45DownRefusesOutstandingIntents(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(AutoEnhancementRecoverySchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 45: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	versionID := seedMediaAssetVersionFixture(t, ctx, pool, "auto45down")
	if _, err := pool.Exec(ctx, `INSERT INTO media_auto_enhancement_recovery
	   (asset_version_id, state, current_intent_id, executing_operation_id, attempt_number)
	 VALUES ($1::uuid, 'EXECUTING', '45000000-0000-0000-0000-00000000cccc'::uuid, 'op-live', 2)`, versionID); err != nil {
		t.Fatalf("seeding an outstanding intent: %v", err)
	}

	if err := m.Migrate(uint(DirectPurchaseAccessGrantSchemaVersion)); err == nil {
		t.Fatal("0045 rolled back underneath an outstanding automatic intent")
	}
	// golang-migrate marks the version dirty before running the body, so the
	// refusal is expected to leave a dirty marker here; the operator clears it
	// deliberately. What must not happen is silent data loss.
	var outstanding int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM media_auto_enhancement_recovery WHERE state = 'EXECUTING'").Scan(&outstanding); err != nil {
		t.Fatalf("reading outstanding intents after the refusal: %v", err)
	}
	if outstanding != 1 {
		t.Fatalf("outstanding intents after the refused rollback = %d, want 1", outstanding)
	}
}

// TestSchema45DownCrossesWhenNothingIsOutstanding proves the floor is genuinely
// open while no intent is outstanding. BACKOFF and NEEDS_OPERATOR rows have
// nothing queued for them, so discarding them with the table is honest: without
// the producer there is no automatic recovery to schedule.
func TestSchema45DownCrossesWhenNothingIsOutstanding(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(AutoEnhancementRecoverySchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 45: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	versionID := seedMediaAssetVersionFixture(t, ctx, pool, "auto45open")
	if _, err := pool.Exec(ctx, `INSERT INTO media_auto_enhancement_recovery
	   (asset_version_id, state, consecutive_failures, attempt_number)
	 VALUES ($1::uuid, 'NEEDS_OPERATOR', 3, 3)`, versionID); err != nil {
		t.Fatalf("seeding an exhausted row: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts
		  (asset_version_id, operation_id, state, attempt_kind, rendition_count, error_reason, auto_recovery_intent_id)
		VALUES ($1::uuid, 'auto-op-history', 'FAILED', 'ENHANCEMENT', 0, 'encoder died',
		        '45000000-0000-0000-0000-00000000dddd'::uuid)
	`, versionID); err != nil {
		t.Fatalf("seeding automatic attempt history: %v", err)
	}

	if err := m.Migrate(uint(DirectPurchaseAccessGrantSchemaVersion)); err != nil {
		t.Fatalf("45 -> 44 with nothing outstanding: %v", err)
	}
	state, err := ReadSchemaState(ctx, pool)
	if err != nil || state.Version != int64(DirectPurchaseAccessGrantSchemaVersion) || state.Dirty {
		t.Fatalf("schema after rollback = %+v (err=%v), want clean %d", state, err, DirectPurchaseAccessGrantSchemaVersion)
	}

	// The attempts survive; only the attribution column is gone. Their kind,
	// outcome and operation identity are the audit evidence and are untouched.
	var attempts int
	var kind, attemptState string
	if err := pool.QueryRow(ctx, `
		SELECT count(*) OVER (), attempt_kind::text, state::text FROM processing_attempts
		 WHERE asset_version_id = $1::uuid AND operation_id = 'auto-op-history'
	`, versionID).Scan(&attempts, &kind, &attemptState); err != nil {
		t.Fatalf("automatic attempt history did not survive the rollback: %v", err)
	}
	if attempts != 1 || kind != "ENHANCEMENT" || attemptState != "FAILED" {
		t.Fatalf("attempt after rollback: count=%d kind=%s state=%s", attempts, kind, attemptState)
	}
	var hasColumn, hasTable bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_name='processing_attempts' AND column_name='auto_recovery_intent_id')
	`).Scan(&hasColumn); err != nil || hasColumn {
		t.Fatalf("attribution column survived the rollback (present=%t err=%v)", hasColumn, err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='media_auto_enhancement_recovery')",
	).Scan(&hasTable); err != nil || hasTable {
		t.Fatalf("scheduler table survived the rollback (present=%t err=%v)", hasTable, err)
	}

	// And forward again, so the boundary is not one-way.
	if err := m.Migrate(uint(AutoEnhancementRecoverySchemaVersion)); err != nil {
		t.Fatalf("reapplying 0045 after rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_auto_enhancement_recovery
	   (asset_version_id, state, consecutive_failures, attempt_number, next_attempt_at)
	 VALUES ($1::uuid, 'BACKOFF', 0, 1, now() + interval '15 minutes')`, versionID); err != nil {
		t.Fatalf("scheduler state is unusable after reapplying 0045: %v", err)
	}
}
