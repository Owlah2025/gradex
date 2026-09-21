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

// productionRollbackHarness is the fully-migrated disposable database plus a
// configuration that really declares APP_ENV=production, because the
// acknowledgement rule under test exists only there.
func productionRollbackHarness(t *testing.T, ctx context.Context) (*migrate.Migrate, *config.Config, *pgxpool.Pool) {
	t.Helper()
	m, _, pool := migrateCommandHarness(t, ctx)
	return m, migrateCommandConfig(t, "production"), pool
}

func assertSchema(t *testing.T, m *migrate.Migrate, wantVersion int, wantDirty bool, context string) {
	t.Helper()
	version, dirty, err := m.Version()
	if err != nil || version != uint(wantVersion) || dirty != wantDirty {
		t.Fatalf("%s: schema = version=%d dirty=%t err=%v, want version=%d dirty=%t",
			context, version, dirty, err, wantVersion, wantDirty)
	}
}

func confirmed() []string { return []string{"-confirm-production=schema-41-to-40"} }

// TestSupervisedRollbackCrossesSchema41InProduction is the proof the release
// plan could not previously make: the documented emergency rollback is actually
// executable against a database that declares APP_ENV=production.
func TestSupervisedRollbackCrossesSchema41InProduction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := productionRollbackHarness(t, ctx)

	versionID := seedMediaVersion(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'full-op', 'SUCCEEDED', 'FULL', 1, 1000, 'media/x/hls/abc')
	`, versionID); err != nil {
		t.Fatalf("seeding whole-ladder attempt: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms, processing_operation_id)
		VALUES ($1::uuid, '720p', 'media/x/hls/abc/720p/playlist.m3u8', 1280, 720, 2800, 1000, 'full-op')
	`, versionID); err != nil {
		t.Fatalf("seeding provenance-carrying rendition: %v", err)
	}

	if err := rollbackSchema41(m, cfg, confirmed()); err != nil {
		t.Fatalf("supervised production rollback: %v", err)
	}
	assertSchema(t, m, db.MediaPlayableFoundationSchemaVersion, false, "after supervised rollback")

	// Evidence survives; only the additive column is gone.
	var attempts, renditions int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid", versionID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("attempts after rollback = %d (err=%v), want 1", attempts, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM video_renditions WHERE asset_version_id=$1::uuid", versionID).Scan(&renditions); err != nil || renditions != 1 {
		t.Fatalf("renditions after rollback = %d (err=%v), want 1", renditions, err)
	}
	var hasProvenance bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_name='video_renditions' AND column_name='processing_operation_id')
	`).Scan(&hasProvenance); err != nil || hasProvenance {
		t.Fatalf("provenance column survived the rollback (present=%t err=%v)", hasProvenance, err)
	}
}

// TestGenericDownRemainsProhibitedInProduction proves the narrow command did not
// open a generic escape hatch. The supervised path is the only production
// downgrade in this binary, and it reverts exactly one migration.
func TestGenericDownRemainsProhibitedInProduction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, _ := productionRollbackHarness(t, ctx)

	for _, steps := range [][]string{{"1"}, {"2"}, nil} {
		err := down(m, cfg, steps)
		if err == nil || !strings.Contains(err.Error(), "down migrations are not permitted when APP_ENV=production") {
			t.Fatalf("down %v error = %v, want the production prohibition", steps, err)
		}
	}
	assertSchema(t, m, db.MaxSchemaVersion, false, "after refused generic down")
}

// TestSupervisedRollbackRequiresExactAcknowledgement pins the fail-closed
// behaviour of the acknowledgement. A missing, empty, misspelled, or generic
// value is refused; only the exact transition name authorizes the operation.
func TestSupervisedRollbackRequiresExactAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, _ := productionRollbackHarness(t, ctx)

	for _, args := range [][]string{
		nil,
		{"-confirm-production="},
		{"-confirm-production=true"},
		{"-confirm-production=yes"},
		{"-confirm-production=schema-41-to-39"},
		{"-confirm-production=SCHEMA-41-TO-40"},
	} {
		err := rollbackSchema41(m, cfg, args)
		if err == nil || !strings.Contains(err.Error(), "requires -confirm-production=schema-41-to-40") {
			t.Fatalf("rollback with %v error = %v, want the acknowledgement refusal", args, err)
		}
		assertSchema(t, m, db.MaxSchemaVersion, false, "after refused acknowledgement")
	}

	// Outside production the flag must not be passed at all, so it cannot become
	// a habit that is always present and therefore means nothing.
	development := migrateCommandConfig(t, "development")
	if err := rollbackSchema41(m, development, confirmed()); err == nil ||
		!strings.Contains(err.Error(), "-confirm-production was passed but APP_ENV=") {
		t.Fatalf("development rollback with acknowledgement error = %v, want the misuse refusal", err)
	}
	assertSchema(t, m, db.MaxSchemaVersion, false, "after refused development acknowledgement")
}

// TestSupervisedRollbackRefusesPositionalArguments proves the command cannot be
// coaxed into a step count or a target version.
func TestSupervisedRollbackRefusesPositionalArguments(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, _ := productionRollbackHarness(t, ctx)

	for _, args := range [][]string{
		{"-confirm-production=schema-41-to-40", "2"},
		{"-confirm-production=schema-41-to-40", "39"},
	} {
		err := rollbackSchema41(m, cfg, args)
		if err == nil || !strings.Contains(err.Error(), "takes no positional arguments") {
			t.Fatalf("rollback with %v error = %v, want the positional-argument refusal", args, err)
		}
		assertSchema(t, m, db.MaxSchemaVersion, false, "after refused positional arguments")
	}
}

// TestSupervisedRollbackRefusesWrongSchemaVersion proves the command is bound to
// exactly one transition: it will not act below 41, above 41, or on a marker it
// cannot trust.
func TestSupervisedRollbackRefusesWrongSchemaVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := productionRollbackHarness(t, ctx)

	// Already at 40: nothing for this command to revert.
	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET version = $1, dirty = false",
		db.MediaPlayableFoundationSchemaVersion); err != nil {
		t.Fatalf("setting schema marker to 40: %v", err)
	}
	if err := rollbackSchema41(m, cfg, confirmed()); err == nil ||
		!strings.Contains(err.Error(), "is not authorized to cross any other version") {
		t.Fatalf("rollback at schema 40 error = %v, want the version refusal", err)
	}
	assertSchema(t, m, db.MediaPlayableFoundationSchemaVersion, false, "after refusal at schema 40")

	// Above 41: a future migration must not be crossed by this command.
	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET version = $1, dirty = false",
		db.EnhancementRecoveryFoundationSchemaVersion+1); err != nil {
		t.Fatalf("setting schema marker above 41: %v", err)
	}
	if err := rollbackSchema41(m, cfg, confirmed()); err == nil ||
		!strings.Contains(err.Error(), "is not authorized to cross any other version") {
		t.Fatalf("rollback above schema 41 error = %v, want the version refusal", err)
	}
	assertSchema(t, m, db.EnhancementRecoveryFoundationSchemaVersion+1, false, "after refusal above schema 41")

	// Dirty at 41: the recorded version may not describe the database at all.
	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET version = $1, dirty = true",
		db.EnhancementRecoveryFoundationSchemaVersion); err != nil {
		t.Fatalf("setting dirty schema marker: %v", err)
	}
	if err := rollbackSchema41(m, cfg, confirmed()); err == nil ||
		!strings.Contains(err.Error(), "will not act on a half-applied schema") {
		t.Fatalf("rollback on a dirty schema error = %v, want the dirty refusal", err)
	}
	assertSchema(t, m, db.EnhancementRecoveryFoundationSchemaVersion, true, "after refusal on a dirty schema")
}

// TestSupervisedRollbackRefusesNonFullEvidence is the rollback-floor rule
// applied through the supervised path: the first ENHANCEMENT or FINALIZATION
// attempt closes it, regardless of outcome.
func TestSupervisedRollbackRefusesNonFullEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := productionRollbackHarness(t, ctx)

	versionID := seedMediaVersion(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, error_reason)
		VALUES ($1::uuid, 'enhance-failed', 'FAILED', 'ENHANCEMENT', 0, 'enhancement encoder died')
	`, versionID); err != nil {
		t.Fatalf("seeding failed enhancement evidence: %v", err)
	}

	err := rollbackSchema41(m, cfg, confirmed())
	if err == nil || !strings.Contains(err.Error(), "enhancement or finalization processing attempts exist") {
		t.Fatalf("supervised rollback error = %v, want the non-FULL evidence refusal", err)
	}
	assertSchema(t, m, db.MaxSchemaVersion, false, "after refused non-FULL rollback")

	var kind, state, reason string
	if err := pool.QueryRow(ctx, `
		SELECT attempt_kind::text, state::text, error_reason FROM processing_attempts
		WHERE asset_version_id = $1::uuid AND operation_id = 'enhance-failed'
	`, versionID).Scan(&kind, &state, &reason); err != nil {
		t.Fatalf("enhancement evidence did not survive the refusal: %v", err)
	}
	if kind != "ENHANCEMENT" || state != "FAILED" || reason != "enhancement encoder died" {
		t.Fatalf("enhancement evidence was rewritten: kind=%s state=%s error_reason=%q", kind, state, reason)
	}
}

// TestSupervisedRollbackRefusesActiveMediaClaim proves the operator-error gate.
// A claimed Asset Version means a producer was not actually stopped, and the
// schema must not move underneath it.
func TestSupervisedRollbackRefusesActiveMediaClaim(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := productionRollbackHarness(t, ctx)

	versionID := seedMediaVersion(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'full-op', 'SUCCEEDED', 'FULL', 1, 1000, 'media/x/hls/abc')
	`, versionID); err != nil {
		t.Fatalf("seeding whole-ladder attempt: %v", err)
	}
	// A coherent live claim under the schema-40 claim constraint: token,
	// claimed_at and an expiry in the future, on a state that may hold one.
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions
		SET work_claim_token = 'live-op', work_claimed_at = now(), work_lease_expires_at = now() + interval '1 hour'
		WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("seeding an active media claim: %v", err)
	}

	err := rollbackSchema41(m, cfg, confirmed())
	if err == nil || !strings.Contains(err.Error(), "active media work is claimed") {
		t.Fatalf("supervised rollback error = %v, want the active-claim refusal", err)
	}
	assertSchema(t, m, db.MaxSchemaVersion, false, "after refused active-claim rollback")

	var token *string
	if err := pool.QueryRow(ctx,
		"SELECT work_claim_token FROM media_asset_versions WHERE id = $1::uuid", versionID).Scan(&token); err != nil {
		t.Fatalf("reading the claim after the refusal: %v", err)
	}
	if token == nil || *token != "live-op" {
		t.Fatalf("claim after refusal = %v, want it untouched", token)
	}
}
