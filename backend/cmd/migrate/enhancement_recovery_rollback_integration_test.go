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
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	rollbackCommandDBName = "gradex_migrate_rollback41_test"
	rollbackCommandDSN    = "postgres://gradex:gradex@localhost:5432/" + rollbackCommandDBName + "?sslmode=disable"
)

// migrateCommandHarness provisions a disposable fully-migrated database and the
// exact (migrator, config) pair the `down` subcommand receives in production, so
// these tests exercise the real command path rather than the migration SQL
// alone.
func migrateCommandHarness(t *testing.T, ctx context.Context) (*migrate.Migrate, *config.Config, *pgxpool.Pool) {
	t.Helper()
	admin, err := pgxpool.New(ctx, migrateCommandAdminDSN)
	if err != nil {
		t.Fatalf("opening disposable rollback admin database: %v", err)
	}
	t.Cleanup(func() { admin.Close() })
	if _, err := admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1", rollbackCommandDBName); err != nil {
		t.Fatalf("terminating disposable rollback connections: %v", err)
	}
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+rollbackCommandDBName); err != nil {
		t.Fatalf("dropping disposable rollback database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+rollbackCommandDBName); err != nil {
		t.Fatalf("creating disposable rollback database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = admin.Exec(cleanupCtx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1", rollbackCommandDBName)
		_, _ = admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+rollbackCommandDBName)
	})

	m, err := migrate.New("file://../../internal/db/migrations", rollbackCommandDSN)
	if err != nil {
		t.Fatalf("opening rollback migrator: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil {
		t.Fatalf("migrating disposable rollback database up: %v", err)
	}

	pool, err := pgxpool.New(ctx, rollbackCommandDSN)
	if err != nil {
		t.Fatalf("opening disposable rollback pool: %v", err)
	}
	t.Cleanup(pool.Close)

	cfg := migrateCommandConfig(t, "development")
	return m, cfg, pool
}

// migrateCommandConfig builds the typed configuration the subcommands receive,
// for a declared APP_ENV. Production is a real value here rather than a test
// shim, because the acknowledgement rule this exercises only exists in
// production.
func migrateCommandConfig(t *testing.T, appEnv string) *config.Config {
	t.Helper()
	settings := map[string]string{
		"APP_ENV": appEnv, "PUBLIC_ORIGIN": "https://gradex.example",
		"REDIS_ADDR": "localhost:6379", "S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
		"PASSWORD_SCREEN_MODE": "deterministic", "OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION": "key-v1",
	}
	secrets := config.MapSecretResolver{
		"DATABASE_URL": rollbackCommandDSN, "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "b",
		"PLAYBACK_TOKEN_SECRET": "c", "OUTBOX_PROTECTED_PAYLOAD_KEY": strings.Repeat("a", 32),
	}
	if appEnv != "development" {
		// Production configuration is validated in full, so a production-mode
		// test must satisfy the real contract rather than a relaxed one. These
		// are the settings the validator requires outside development; none of
		// them affect the migration path under test.
		settings["PASSWORD_SCREEN_MODE"] = "unavailable"
		settings["SALES_WHATSAPP_NUMBER"] = "96500000000"
		settings["S3_ENDPOINT"] = "https://s3.gradex.example"
		settings["S3_PRESIGN_ENDPOINT"] = "https://s3.gradex.example"
		settings["REDIS_TLS_ENABLED"] = "true"
		settings["LEGAL_OPERATOR_NAME"] = "Gradex"
		settings["LEGAL_REGISTRATION_NUMBER"] = "CR-000000"
		settings["LEGAL_REGISTERED_ADDRESS"] = "Kuwait City"
		settings["PRIVACY_EMAIL"] = "privacy@gradex.example"
		settings["SUPPORT_EMAIL"] = "support@gradex.example"
		settings["SECURITY_EMAIL"] = "security@gradex.example"
		secrets["REDIS_PASSWORD"] = "redis-password"
		secrets["SESSION_CSRF_KEY"] = strings.Repeat("b", 32)
		secrets["ANONYMOUS_COOKIE_SIGNING_KEY"] = strings.Repeat("c", 32)
		secrets["ANONYMOUS_CSRF_KEY"] = strings.Repeat("d", 32)
		secrets["ADMISSION_LIMITER_HMAC_KEY"] = strings.Repeat("e", 32)
	}
	cfg, err := config.LoadFrom(config.MapLookup(settings), secrets)
	if err != nil {
		t.Fatalf("loading disposable rollback configuration for APP_ENV=%s: %v", appEnv, err)
	}
	return cfg
}

// seedMediaVersion creates the minimum Asset Version a processing attempt can
// reference, and returns its id.
func seedMediaVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	const ownerID = "53000000-0000-0000-0000-000000000001"
	const courseID = "53000000-0000-0000-0000-000000000002"
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, 'rollback41-owner@example.test', 'rollback41-owner@example.test', 'ADMIN', 'ACTIVE', 'Rollback Owner')
	`, ownerID); err != nil {
		t.Fatalf("seeding rollback account: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')",
		courseID, ownerID); err != nil {
		t.Fatalf("seeding rollback Course: %v", err)
	}
	var assetID, versionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_assets (kind, owner_account_id, course_id, visibility)
		VALUES ('VIDEO', $1::uuid, $2::uuid, 'PROTECTED') RETURNING id::text
	`, ownerID, courseID).Scan(&assetID); err != nil {
		t.Fatalf("seeding rollback media asset: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_asset_versions (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, 'VIDEO', 'SCANNING', 'k', 'v', 'video/mp4', 100) RETURNING id::text
	`, assetID).Scan(&versionID); err != nil {
		t.Fatalf("seeding rollback media asset version: %v", err)
	}
	return versionID
}

// TestDownRefusesNonFullAttemptBeforeMigrationStateChanges is the operational
// half of the schema-41 rollback floor. The migration SQL already refuses, but
// golang-migrate marks the version dirty before it runs the body — so a refusal
// raised from inside the SQL would preserve the evidence and still leave the
// schema marker dirty. The command must decide first.
//
// The fixture is a FAILED ENHANCEMENT on purpose: schema 40's restored
// coherence constraint would accept its remaining columns, so this proves the
// boundary is the first non-FULL row of any outcome, not the first successful
// finalization.
func TestDownRefusesNonFullAttemptBeforeMigrationStateChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := migrateCommandHarness(t, ctx)

	versionID := seedMediaVersion(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, error_reason)
		VALUES ($1::uuid, 'enhance-failed', 'FAILED', 'ENHANCEMENT', 0, 'enhancement encoder died')
	`, versionID); err != nil {
		t.Fatalf("seeding failed enhancement evidence: %v", err)
	}

	err := down(m, cfg, []string{"1"})
	if err == nil || !strings.Contains(err.Error(), "enhancement or finalization processing attempts exist") {
		t.Fatalf("down error = %v, want the actionable schema-41 rollback refusal", err)
	}

	// The whole point: the marker is clean, so the next migration command works.
	version, dirty, versionErr := m.Version()
	if versionErr != nil || version != uint(db.MaxSchemaVersion) || dirty {
		t.Fatalf("schema after refused command down = version=%d dirty=%t err=%v, want clean %d",
			version, dirty, versionErr, db.MaxSchemaVersion)
	}

	// The evidence is untouched.
	var kind, state, reason string
	var renditionCount int
	if err := pool.QueryRow(ctx, `
		SELECT attempt_kind::text, state::text, rendition_count, error_reason FROM processing_attempts
		WHERE asset_version_id = $1::uuid AND operation_id = 'enhance-failed'
	`, versionID).Scan(&kind, &state, &renditionCount, &reason); err != nil {
		t.Fatalf("enhancement evidence did not survive the refused command down: %v", err)
	}
	if kind != "ENHANCEMENT" || state != "FAILED" || renditionCount != 0 || reason != "enhancement encoder died" {
		t.Fatalf("enhancement evidence was rewritten: kind=%s state=%s rendition_count=%d error_reason=%q",
			kind, state, renditionCount, reason)
	}

	// No partial destructive DDL ran: the 0041 column and type are still there.
	var hasColumn, hasType, hasProvenance bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_name='processing_attempts' AND column_name='attempt_kind')
	`).Scan(&hasColumn); err != nil || !hasColumn {
		t.Fatalf("processing_attempts.attempt_kind missing after refused command down (err=%v)", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_type WHERE typname='media_processing_attempt_kind')").Scan(&hasType); err != nil || !hasType {
		t.Fatalf("media_processing_attempt_kind missing after refused command down (err=%v)", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_name='video_renditions' AND column_name='processing_operation_id')
	`).Scan(&hasProvenance); err != nil || !hasProvenance {
		t.Fatalf("video_renditions.processing_operation_id missing after refused command down (err=%v)", err)
	}
}

// TestDownCrossesSchema41WhenEveryAttemptIsFull proves the floor is genuinely
// open while no non-FULL evidence exists, including when renditions written
// after 0041 carry provenance. If provenance alone blocked rollback, the floor
// would close the moment the first video was processed on schema 41.
func TestDownCrossesSchema41WhenEveryAttemptIsFull(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := migrateCommandHarness(t, ctx)

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

	if err := down(m, cfg, []string{"1"}); err != nil {
		t.Fatalf("down across 41 -> 40 with only FULL attempts: %v", err)
	}

	version, dirty, err := m.Version()
	if err != nil || version != uint(db.MediaPlayableFoundationSchemaVersion) || dirty {
		t.Fatalf("schema after command down = version=%d dirty=%t err=%v, want clean %d",
			version, dirty, err, db.MediaPlayableFoundationSchemaVersion)
	}

	// Evidence survives; only the added column is gone.
	var attempts, renditions int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid", versionID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("attempts after rollback = %d (err=%v), want 1", attempts, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM video_renditions WHERE asset_version_id=$1::uuid", versionID).Scan(&renditions); err != nil || renditions != 1 {
		t.Fatalf("renditions after rollback = %d (err=%v), want 1", renditions, err)
	}
	var hasProvenance, hasKind bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_name='video_renditions' AND column_name='processing_operation_id')
	`).Scan(&hasProvenance); err != nil || hasProvenance {
		t.Fatalf("provenance column survived the rollback (present=%t err=%v)", hasProvenance, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_name='processing_attempts' AND column_name='attempt_kind')
	`).Scan(&hasKind); err != nil || hasKind {
		t.Fatalf("attempt_kind column survived the rollback (present=%t err=%v)", hasKind, err)
	}

	// The restored schema-40 coherence rule is authoritative again.
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'post-down-zero', 'SUCCEEDED', 0, 1000, NULL)
	`, versionID); err == nil {
		t.Fatal("expected restored schema 40 to refuse a zero-rendition successful attempt")
	}
}
