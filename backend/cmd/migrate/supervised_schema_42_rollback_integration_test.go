//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schema42RollbackHarness is the disposable database staged at exactly schema
// 42 — where the 3C-B release lives — plus a configuration that really declares
// APP_ENV=production, because the acknowledgement rule under test exists only
// there. The exact version matters: rollbackSchema42 reverts 42 to 41 and
// refuses to cross any other boundary, so a head-staged fixture proves nothing
// about it.
func schema42RollbackHarness(t *testing.T, ctx context.Context) (*migrate.Migrate, *config.Config, *pgxpool.Pool) {
	t.Helper()
	m, _, pool := migrateCommandHarness(t, ctx)
	stageSchema42(t, m)
	return m, migrateCommandConfig(t, "production"), pool
}

func confirmed42() []string { return []string{"-confirm-production=schema-42-to-41"} }

// seedPendingEnhancementIntent writes the durable manual-enhancement request an
// Administrator's retry action commits, with no dispatch receipt — the exact row
// that would stop the schema-41 media dispatcher.
func seedPendingEnhancementIntent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, assetVersionID string) string {
	t.Helper()
	var eventID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO outbox_events (event_type, schema_version, source_module, aggregate_type,
		                           aggregate_id, aggregate_revision, safe_payload, correlation_id)
		VALUES ('media.enhancement_requested', 1, 'MEDIA_AND_ASSETS', 'MEDIA_ASSET_VERSION',
		        $1::uuid, 1, jsonb_build_object('asset_version_id', $1), 'schema42-rollback-fixture')
		RETURNING id::text
	`, assetVersionID).Scan(&eventID); err != nil {
		t.Fatalf("seeding pending enhancement intent: %v", err)
	}
	return eventID
}

// TestSupervisedSchema42RollbackCrossesSchema42InProduction is the proof the
// release plan could not previously make: the documented 3C-B rollback is really
// executable against a database that declares APP_ENV=production, and it stops
// at a clean 41 rather than continuing toward 40.
func TestSupervisedSchema42RollbackCrossesSchema42InProduction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := schema42RollbackHarness(t, ctx)

	versionID := seedMediaVersion(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'full-op', 'SUCCEEDED', 'FULL', 1, 1000, 'media/x/hls/abc')
	`, versionID); err != nil {
		t.Fatalf("seeding whole-ladder attempt: %v", err)
	}
	// Terminal enhancement history is compatible with schema 41 and must not be
	// deleted to make a rollback possible.
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, error_reason)
		VALUES ($1::uuid, 'enhance-history', 'FAILED', 'ENHANCEMENT', 0, 'enhancement encoder died')
	`, versionID); err != nil {
		t.Fatalf("seeding terminal enhancement history: %v", err)
	}
	// A dispatched enhancement intent is not pending work in the database: its
	// receipt exists. The queue side of that case is proven in internal/media.
	dispatched := seedPendingEnhancementIntent(t, ctx, pool, versionID)
	if _, err := pool.Exec(ctx, "INSERT INTO media_outbox_dispatches (event_id) VALUES ($1::uuid)", dispatched); err != nil {
		t.Fatalf("recording the dispatch receipt: %v", err)
	}

	if err := rollbackSchema42(m, cfg, confirmed42()); err != nil {
		t.Fatalf("supervised production rollback: %v", err)
	}
	assertSchema(t, m, db.EnhancementRecoveryFoundationSchemaVersion, false, "after supervised schema 42 rollback")

	var attempts int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid", versionID).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("attempts after rollback = %d (err=%v), want 2", attempts, err)
	}
	var hasActiveKind bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_name='media_asset_versions' AND column_name='active_processing_attempt_kind')
	`).Scan(&hasActiveKind); err != nil || hasActiveKind {
		t.Fatalf("the schema-42 active kind column survived the rollback (present=%t err=%v)", hasActiveKind, err)
	}

	// The command ends here. Reaching schema 40 is a different command with a
	// stricter rule, and the historical ENHANCEMENT attempt keeps it closed.
	if err := db.CheckEnhancementRecoveryRollbackSafety(ctx, pool); err == nil {
		t.Fatal("41 -> 40 rollback reopened after a 42 -> 41 rollback with enhancement history")
	}
	if err := rollbackSchema41(m, cfg, confirmed()); err == nil ||
		!strings.Contains(err.Error(), "enhancement or finalization processing attempts exist") {
		t.Fatalf("supervised 41 -> 40 after 42 -> 41 error = %v, want the non-FULL evidence refusal", err)
	}
	assertSchema(t, m, db.EnhancementRecoveryFoundationSchemaVersion, false, "after refusing to cascade to schema 40")
}

// TestSupervisedSchema42RollbackRefusesPendingEnhancementIntent is the hard
// outbox gate. One undispatched media.enhancement_requested row is enough: the
// schema-41 dispatcher aborts its batch on that event type and every later media
// outbox event stops dispatching, so this is a refusal, not a warning.
func TestSupervisedSchema42RollbackRefusesPendingEnhancementIntent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := schema42RollbackHarness(t, ctx)

	versionID := seedMediaVersion(t, ctx, pool)
	eventID := seedPendingEnhancementIntent(t, ctx, pool, versionID)

	err := rollbackSchema42(m, cfg, confirmed42())
	if err == nil || !strings.Contains(err.Error(), "undispatched media.enhancement_requested outbox event") {
		t.Fatalf("supervised rollback error = %v, want the pending enhancement intent refusal", err)
	}
	if !strings.Contains(err.Error(), eventID) {
		t.Fatalf("refusal did not identify the surviving event: %v", err)
	}
	assertSchema(t, m, db.ActiveProcessingKindSchemaVersion, false, "after refused pending-intent rollback")

	// The gate proves; it does not clean up. Deleting an Administrator's request
	// to make a rollback possible would be a silent loss of requested work.
	var surviving int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM outbox_events WHERE id=$1::uuid", eventID).Scan(&surviving); err != nil || surviving != 1 {
		t.Fatalf("pending intent count after refusal = %d (err=%v), want it untouched", surviving, err)
	}

	// Recording the dispatch receipt clears this gate — and only this gate. The
	// queue side of a dispatched intent is proven in internal/media.
	if _, err := pool.Exec(ctx, "INSERT INTO media_outbox_dispatches (event_id) VALUES ($1::uuid)", eventID); err != nil {
		t.Fatalf("recording the dispatch receipt: %v", err)
	}

	// A future-dated intent is equally durable: it survives the downgrade and
	// becomes dispatchable later, so availability must not exempt it.
	var deferredID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO outbox_events (event_type, schema_version, source_module, aggregate_type,
		                           aggregate_id, aggregate_revision, safe_payload, correlation_id, available_at)
		VALUES ('media.enhancement_requested', 1, 'MEDIA_AND_ASSETS', 'MEDIA_ASSET_VERSION',
		        $1::uuid, 1, jsonb_build_object('asset_version_id', $1), 'schema42-deferred-fixture',
		        now() + interval '7 days')
		RETURNING id::text
	`, versionID).Scan(&deferredID); err != nil {
		t.Fatalf("seeding a deferred enhancement intent: %v", err)
	}
	if err := rollbackSchema42(m, cfg, confirmed42()); err == nil ||
		!strings.Contains(err.Error(), deferredID) {
		t.Fatalf("supervised rollback with a deferred intent error = %v, want it named in the refusal", err)
	}
	assertSchema(t, m, db.ActiveProcessingKindSchemaVersion, false, "after refused deferred-intent rollback")
	if _, err := pool.Exec(ctx, "INSERT INTO media_outbox_dispatches (event_id) VALUES ($1::uuid)", deferredID); err != nil {
		t.Fatalf("recording the deferred dispatch receipt: %v", err)
	}
	if err := rollbackSchema42(m, cfg, confirmed42()); err != nil {
		t.Fatalf("supervised rollback after dispatch: %v", err)
	}
	assertSchema(t, m, db.EnhancementRecoveryFoundationSchemaVersion, false, "after dispatched-intent rollback")
}

// TestSupervisedSchema42RollbackRequiresExactAcknowledgement pins the
// fail-closed behaviour of the acknowledgement, including the schema-41
// transition name, which must not authorize this one.
func TestSupervisedSchema42RollbackRequiresExactAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, _ := schema42RollbackHarness(t, ctx)

	for _, args := range [][]string{
		nil,
		{"-confirm-production="},
		{"-confirm-production=true"},
		{"-confirm-production=yes"},
		{"-confirm-production=schema-41-to-40"},
		{"-confirm-production=schema-42-to-40"},
		{"-confirm-production=SCHEMA-42-TO-41"},
	} {
		err := rollbackSchema42(m, cfg, args)
		if err == nil || !strings.Contains(err.Error(), "requires -confirm-production=schema-42-to-41") {
			t.Fatalf("rollback with %v error = %v, want the acknowledgement refusal", args, err)
		}
		assertSchema(t, m, db.ActiveProcessingKindSchemaVersion, false, "after refused acknowledgement")
	}

	development := migrateCommandConfig(t, "development")
	if err := rollbackSchema42(m, development, confirmed42()); err == nil ||
		!strings.Contains(err.Error(), "-confirm-production was passed but APP_ENV=") {
		t.Fatalf("development rollback with acknowledgement error = %v, want the misuse refusal", err)
	}
	assertSchema(t, m, db.ActiveProcessingKindSchemaVersion, false, "after refused development acknowledgement")
}

// TestSupervisedSchema42RollbackRefusesPositionalArguments proves the command
// cannot be coaxed into a step count or a target version — there is no 42 -> 40.
func TestSupervisedSchema42RollbackRefusesPositionalArguments(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, _ := schema42RollbackHarness(t, ctx)

	for _, args := range [][]string{
		{"-confirm-production=schema-42-to-41", "2"},
		{"-confirm-production=schema-42-to-41", "40"},
	} {
		err := rollbackSchema42(m, cfg, args)
		if err == nil || !strings.Contains(err.Error(), "takes no positional arguments") {
			t.Fatalf("rollback with %v error = %v, want the positional-argument refusal", args, err)
		}
		assertSchema(t, m, db.ActiveProcessingKindSchemaVersion, false, "after refused positional arguments")
	}
}

// TestSupervisedSchema42RollbackRefusesWrongSchemaVersion proves the command is
// bound to exactly one transition, and will not act on a marker it cannot trust.
func TestSupervisedSchema42RollbackRefusesWrongSchemaVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := schema42RollbackHarness(t, ctx)

	// Already at 41: nothing for this command to revert, and it is certainly not
	// authorized to continue to 40.
	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET version = $1, dirty = false",
		db.EnhancementRecoveryFoundationSchemaVersion); err != nil {
		t.Fatalf("setting schema marker to 41: %v", err)
	}
	if err := rollbackSchema42(m, cfg, confirmed42()); err == nil ||
		!strings.Contains(err.Error(), "is not authorized to cross any other version") {
		t.Fatalf("rollback at schema 41 error = %v, want the version refusal", err)
	}
	assertSchema(t, m, db.EnhancementRecoveryFoundationSchemaVersion, false, "after refusal at schema 41")

	// Above 42: a future migration must not be crossed by this command.
	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET version = $1, dirty = false",
		db.ActiveProcessingKindSchemaVersion+1); err != nil {
		t.Fatalf("setting schema marker above 42: %v", err)
	}
	if err := rollbackSchema42(m, cfg, confirmed42()); err == nil ||
		!strings.Contains(err.Error(), "is not authorized to cross any other version") {
		t.Fatalf("rollback above schema 42 error = %v, want the version refusal", err)
	}
	assertSchema(t, m, db.ActiveProcessingKindSchemaVersion+1, false, "after refusal above schema 42")

	// Dirty at 42: the recorded version may not describe the database at all.
	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET version = $1, dirty = true",
		db.ActiveProcessingKindSchemaVersion); err != nil {
		t.Fatalf("setting dirty schema marker: %v", err)
	}
	if err := rollbackSchema42(m, cfg, confirmed42()); err == nil ||
		!strings.Contains(err.Error(), "will not act on a half-applied schema") {
		t.Fatalf("rollback on a dirty schema error = %v, want the dirty refusal", err)
	}
	assertSchema(t, m, db.ActiveProcessingKindSchemaVersion, true, "after refusal on a dirty schema")
}

// TestSupervisedSchema42RollbackRefusesActiveProcessing is the schema-42 kind
// gate reached through the supervised command: an in-flight operation is what
// needs the column, and the refusal must land before the marker moves.
func TestSupervisedSchema42RollbackRefusesActiveProcessing(t *testing.T) {
	for _, kind := range []string{"FULL", "ENHANCEMENT"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			m, cfg, pool := schema42RollbackHarness(t, ctx)
			versionID := seedActiveProcessingKind(t, ctx, pool, kind)

			err := rollbackSchema42(m, cfg, confirmed42())
			if err == nil || !strings.Contains(err.Error(), "active processing operation requires schema 42") {
				t.Fatalf("supervised rollback with an active %s error = %v, want the active-processing refusal", kind, err)
			}
			assertSchema(t, m, db.ActiveProcessingKindSchemaVersion, false, "after refused active-processing rollback")

			var activeKind string
			if err := pool.QueryRow(ctx,
				"SELECT active_processing_attempt_kind::text FROM media_asset_versions WHERE id=$1::uuid", versionID).Scan(&activeKind); err != nil || activeKind != kind {
				t.Fatalf("active kind after refusal = %q (err=%v), want it untouched", activeKind, err)
			}
		})
	}
}

// TestSupervisedSchema42RollbackRefusesActiveMediaClaim proves the
// operator-error gate: a claimed Asset Version means a producer was not really
// stopped, and the schema must not move underneath it.
//
// The fixture is a scan claim on purpose. It carries no active processing kind,
// so CheckActiveProcessingKindRollbackSafety has nothing to say about it — this
// is the case that gate cannot see, and the one an incomplete quiesce actually
// produces.
func TestSupervisedSchema42RollbackRefusesActiveMediaClaim(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := schema42RollbackHarness(t, ctx)

	versionID := seedMediaVersion(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions
		SET work_claim_token='live-op', work_claimed_at=now(), work_lease_expires_at=now() + interval '1 hour'
		WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("seeding an active media claim: %v", err)
	}

	err := rollbackSchema42(m, cfg, confirmed42())
	if err == nil || !strings.Contains(err.Error(), "active media work is claimed") {
		t.Fatalf("supervised rollback error = %v, want the active-claim refusal", err)
	}
	assertSchema(t, m, db.ActiveProcessingKindSchemaVersion, false, "after refused active-claim rollback")

	var token *string
	if err := pool.QueryRow(ctx,
		"SELECT work_claim_token FROM media_asset_versions WHERE id = $1::uuid", versionID).Scan(&token); err != nil {
		t.Fatalf("reading the claim after the refusal: %v", err)
	}
	if token == nil || *token != "live-op" {
		t.Fatalf("claim after refusal = %v, want it untouched", token)
	}
}

// TestGenericDownRemainsProhibitedAtSchema42 proves the new command did not open
// a generic escape hatch at this boundary either.
func TestGenericDownRemainsProhibitedAtSchema42(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, _ := schema42RollbackHarness(t, ctx)

	for _, steps := range [][]string{{"1"}, {"2"}, {"42"}, nil} {
		err := down(m, cfg, steps)
		if err == nil || !strings.Contains(err.Error(), "down migrations are not permitted when APP_ENV=production") {
			t.Fatalf("down %v error = %v, want the production prohibition", steps, err)
		}
	}
	assertSchema(t, m, db.ActiveProcessingKindSchemaVersion, false, "after refused generic down")
}
