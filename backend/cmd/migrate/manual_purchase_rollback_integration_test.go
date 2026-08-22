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
	migrateCommandAdminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	migrateCommandDBName   = "gradex_migrate_command_test"
	migrateCommandDSN      = "postgres://gradex:gradex@localhost:5432/" + migrateCommandDBName + "?sslmode=disable"
)

func TestDownRefusesLivePurchaseEntitlementBeforeMigrationStateChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, migrateCommandAdminDSN)
	if err != nil {
		t.Fatalf("opening disposable migration command database: %v", err)
	}
	t.Cleanup(func() { admin.Close() })
	if _, err := admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1", migrateCommandDBName); err != nil {
		t.Fatalf("terminating disposable migration command connections: %v", err)
	}
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+migrateCommandDBName); err != nil {
		t.Fatalf("dropping disposable migration command database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+migrateCommandDBName); err != nil {
		t.Fatalf("creating disposable migration command database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = admin.Exec(cleanupCtx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1", migrateCommandDBName)
		_, _ = admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+migrateCommandDBName)
	})

	m, err := migrate.New("file://../../internal/db/migrations", migrateCommandDSN)
	if err != nil {
		t.Fatalf("opening migration command migrator: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil {
		t.Fatalf("migrating disposable command database up: %v", err)
	}

	pool, err := pgxpool.New(ctx, migrateCommandDSN)
	if err != nil {
		t.Fatalf("opening disposable migration command pool: %v", err)
	}
	t.Cleanup(pool.Close)
	seedLivePurchaseEntitlement(t, ctx, pool)

	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "PUBLIC_ORIGIN": "https://gradex.example",
		"REDIS_ADDR": "localhost:6379", "S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
		"PASSWORD_SCREEN_MODE": "deterministic", "OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION": "key-v1",
	}), config.MapSecretResolver{
		"DATABASE_URL": migrateCommandDSN, "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "b",
		"PLAYBACK_TOKEN_SECRET": "c", "OUTBOX_PROTECTED_PAYLOAD_KEY": strings.Repeat("a", 32),
	})
	if err != nil {
		t.Fatalf("loading disposable migration command configuration: %v", err)
	}
	if err := down(m, cfg, []string{"1"}); err == nil || !strings.Contains(err.Error(), "PURCHASE_REQUEST entitlements exist") {
		t.Fatalf("down error = %v, want actionable live-purchase rollback refusal", err)
	}

	version, dirty, err := m.Version()
	// Tracks the fully-migrated top rather than a literal, so adding an additive
	// migration cannot make a refused-rollback guard look broken.
	if err != nil || version != db.MaxSchemaVersion || dirty {
		t.Fatalf("schema state after refused command down = version=%d dirty=%t err=%v, want clean %d", version, dirty, err, db.MaxSchemaVersion)
	}
	var requests, grants int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM purchase_requests").Scan(&requests); err != nil || requests != 0 {
		t.Fatalf("purchase-request schema was changed by refused command down: rows=%d err=%v", requests, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM entitlements WHERE grant_source='PURCHASE_REQUEST'").Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("live purchase entitlement after refused command down = %d (err=%v), want 1", grants, err)
	}
}

func seedLivePurchaseEntitlement(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	const adminID = "51000000-0000-0000-0000-000000000001"
	const studentID = "51000000-0000-0000-0000-000000000002"
	const courseID = "52000000-0000-0000-0000-000000000001"
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		($1::uuid, 'command-rollback-admin@example.test', 'command-rollback-admin@example.test', 'ADMIN', 'ACTIVE', 'Command Rollback Admin'),
		($2::uuid, 'command-rollback-student@example.test', 'command-rollback-student@example.test', 'STUDENT', 'ACTIVE', 'Command Rollback Student')
	`, adminID, studentID); err != nil {
		t.Fatalf("seeding migration command accounts: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')", courseID, adminID); err != nil {
		t.Fatalf("seeding migration command Course: %v", err)
	}
	var invitationID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_access_invitations (normalized_email, email, course_id, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		VALUES ('command-rollback-student@example.test', 'command-rollback-student@example.test', $1::uuid, $2::uuid, $3::uuid, $2::uuid, 'APPROVED')
		RETURNING id::text
	`, courseID, adminID, studentID).Scan(&invitationID); err != nil {
		t.Fatalf("seeding migration command purchase invitation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'PURCHASE_REQUEST', $3::uuid, now() + interval '1 day', now() + interval '1 day', now(), 'ACTIVE')
	`, studentID, courseID, invitationID); err != nil {
		t.Fatalf("creating migration command live purchase entitlement: %v", err)
	}
}
