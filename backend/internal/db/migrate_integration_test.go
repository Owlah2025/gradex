//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The migration test runs against its own database so it can drop and recreate
// the whole schema without touching a developer's working data.
const (
	adminDSN   = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	testDBName = "gradex_migrate_test"
	testDSN    = "postgres://gradex:gradex@localhost:5432/" + testDBName + "?sslmode=disable"
	sourceURL  = "file://migrations"
	opTimeout  = 30 * time.Second
)

// freshDatabase drops and recreates the test database so every run starts from
// genuinely empty, not from whatever a previous failure left behind.
func freshDatabase(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connecting to the admin database: %v", err)
	}
	defer admin.Close()

	// Terminate stragglers first; DROP DATABASE fails while sessions remain.
	_, _ = admin.Exec(ctx,
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", testDBName)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+testDBName); err != nil {
		t.Fatalf("dropping the test database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+testDBName); err != nil {
		t.Fatalf("creating the test database: %v", err)
	}
}

func openMigrator(t *testing.T) *migrate.Migrate {
	t.Helper()
	m, err := migrate.New(sourceURL, testDSN)
	if err != nil {
		t.Fatalf("opening the migrator: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	return m
}

func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("connecting to the test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var exists bool
	err := pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)",
		name).Scan(&exists)
	if err != nil {
		t.Fatalf("checking for table %s: %v", name, err)
	}
	return exists
}

// The tables each applied migration is responsible for. These describe
// migrations that already exist and must not be edited; they are not a
// specification for them.
var (
	initTables      = []string{"courses", "sections", "lessons", "videos", "progress", "fake_entitlements"}
	identityTables  = []string{"accounts", "password_credentials", "bootstrap_operations"}
	auditTables     = []string{"audit_events"}
	sessionTables   = []string{"sessions", "session_credentials"}
	admissionTables = []string{
		"policy_acceptances",
		"identity_action_secrets",
		"identity_security_events",
		"outbox_events",
		"outbox_protected_payloads",
	}
)

func allTables() []string {
	all := append([]string{}, initTables...)
	all = append(all, identityTables...)
	all = append(all, auditTables...)
	all = append(all, sessionTables...)
	return append(all, admissionTables...)
}

// TestMigrateUpDownUp walks the full lifecycle the release process depends on,
// without any hand-installed binary: the whole thing runs from `go test`.
func TestMigrateUpDownUp(t *testing.T) {
	freshDatabase(t)
	pool := openPool(t)

	// 1. The database begins empty.
	for _, table := range allTables() {
		if tableExists(t, pool, table) {
			t.Fatalf("table %s exists before any migration ran", table)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if _, err := ReadSchemaState(ctx, pool); !errors.Is(err, ErrSchemaMissing) {
		t.Fatalf("empty database reported %v, want ErrSchemaMissing", err)
	}
	if err := CheckSchema(ctx, pool); err == nil {
		t.Fatal("an unmigrated database must not pass the schema check")
	}

	// 2. up applies 0001_init.
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("first up: %v", err)
	}

	// 3. The expected tables and version exist.
	for _, table := range allTables() {
		if !tableExists(t, pool, table) {
			t.Errorf("table %s is missing after up", table)
		}
	}
	state, err := ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state after up: %v", err)
	}
	// Migrating up goes to the newest migration, which is what this build
	// declares as its maximum supported version.
	if state.Version != MaxSchemaVersion {
		t.Errorf("version = %d, want %d", state.Version, MaxSchemaVersion)
	}
	if state.Dirty {
		t.Error("schema is dirty immediately after a successful up")
	}
	if err := CheckSchema(ctx, pool); err != nil {
		t.Errorf("schema check failed on a freshly migrated database: %v", err)
	}

	// The constraint the video pipeline depends on is present, so "the tables
	// exist" is not mistaken for "the schema is correct".
	var fkCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.table_constraints
		 WHERE table_schema = 'public' AND table_name = 'videos' AND constraint_type = 'FOREIGN KEY'`,
	).Scan(&fkCount); err != nil {
		t.Fatalf("counting foreign keys: %v", err)
	}
	if fkCount == 0 {
		t.Error("videos has no foreign key after 0001_init")
	}

	// 4. Re-running up is safe. A release step may run more than once.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("second up: %v", err)
	}
	if err := CheckSchema(ctx, pool); err != nil {
		t.Errorf("schema check failed after a repeated up: %v", err)
	}

	// 5. down returns the database to the expected empty state.
	//
	// Down() rather than Steps(-1): with more than one migration applied, a
	// single step reverts only the newest, and asserting emptiness after it
	// would fail for the wrong reason.
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("down: %v", err)
	}
	for _, table := range allTables() {
		if tableExists(t, pool, table) {
			t.Errorf("table %s survived down", table)
		}
	}

	// 6. up succeeds again after down.
	if err := m.Up(); err != nil {
		t.Fatalf("up after down: %v", err)
	}
	if err := CheckSchema(ctx, pool); err != nil {
		t.Errorf("schema check failed after up/down/up: %v", err)
	}
}

// A migration that failed partway leaves the database in an unknown shape.
// Serving traffic against it risks acting on a half-applied schema.
func TestDirtySchemaFailsReadiness(t *testing.T) {
	freshDatabase(t)
	pool := openPool(t)

	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("up: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if _, err := pool.Exec(ctx, "UPDATE "+schemaMigrationsTable+" SET dirty = true"); err != nil {
		t.Fatalf("marking the schema dirty: %v", err)
	}

	err := CheckSchema(ctx, pool)
	if !errors.Is(err, ErrSchemaDirty) {
		t.Fatalf("CheckSchema returned %v, want ErrSchemaDirty", err)
	}
	// The database is still reachable, so this must be the schema check
	// failing and not the connectivity check.
	if err := Ping(ctx, pool); err != nil {
		t.Errorf("Ping failed on a reachable database: %v", err)
	}
}

// A build whose supported range does not include the database's version must
// refuse traffic rather than guess at the shape it finds.
func TestSchemaOutsideSupportedRangeFailsReadiness(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int64
	}{
		{name: "below minimum", version: int64(MinSchemaVersion - 1)},
		{name: "above maximum", version: int64(MaxSchemaVersion + 5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			freshDatabase(t)
			pool := openPool(t)

			m := openMigrator(t)
			if err := m.Up(); err != nil {
				t.Fatalf("up: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
			defer cancel()

			if _, err := pool.Exec(ctx,
				"UPDATE "+schemaMigrationsTable+" SET version = $1", tc.version); err != nil {
				t.Fatalf("setting schema version: %v", err)
			}

			err := CheckSchema(ctx, pool)
			if !errors.Is(err, ErrSchemaIncompatible) {
				t.Fatalf("CheckSchema returned %v, want ErrSchemaIncompatible", err)
			}
			if msg := err.Error(); !strings.Contains(msg, fmt.Sprint(tc.version)) {
				t.Errorf("the error should name the version found, got %q", msg)
			}
		})
	}
}

func TestStudentAdmissionSchemaInvariants(t *testing.T) {
	freshDatabase(t)
	pool := openPool(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("up: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var fingerprintColumns int
	if err := pool.QueryRow(ctx,
		`SELECT count(*)
		   FROM information_schema.columns
		  WHERE table_schema = 'public'
		    AND table_name = 'bootstrap_operations'
		    AND column_name IN ('fingerprint_version', 'request_fingerprint')`,
	).Scan(&fingerprintColumns); err != nil {
		t.Fatalf("checking bootstrap fingerprint columns: %v", err)
	}
	if fingerprintColumns != 2 {
		t.Fatalf("bootstrap fingerprint column count = %d, want 2", fingerprintColumns)
	}

	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (normalized_email, email, role, status, display_name)
		 VALUES ('student@example.com', 'Student@Example.com', 'STUDENT', 'PENDING_VERIFICATION', 'Student Name')
		 RETURNING id::text`,
	).Scan(&accountID); err != nil {
		t.Fatalf("creating Account fixture: %v", err)
	}

	var acceptanceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO policy_acceptances
		   (account_id, policy_set_id, policy_kind, policy_version, locale, request_id)
		 VALUES ($1, 'dev-v1', 'TERMS_OF_SERVICE', 'v1', 'en', 'request-1')
		 RETURNING id::text`,
		accountID,
	).Scan(&acceptanceID); err != nil {
		t.Fatalf("creating policy acceptance: %v", err)
	}
	assertStatementFails(t, pool, ctx,
		"UPDATE policy_acceptances SET policy_version = 'v2' WHERE id = $1", acceptanceID)
	assertStatementFails(t, pool, ctx,
		"DELETE FROM policy_acceptances WHERE id = $1", acceptanceID)

	var firstSecretID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO identity_action_secrets
		   (account_id, purpose, secret_digest, expires_at)
		 VALUES ($1, 'EMAIL_VERIFICATION', decode(repeat('aa', 32), 'hex'), now() + interval '1 hour')
		 RETURNING id::text`,
		accountID,
	).Scan(&firstSecretID); err != nil {
		t.Fatalf("creating action secret: %v", err)
	}
	assertStatementFails(t, pool, ctx,
		`INSERT INTO identity_action_secrets
		   (account_id, purpose, secret_digest, expires_at)
		 VALUES ($1, 'EMAIL_VERIFICATION', decode(repeat('bb', 32), 'hex'), now() + interval '1 hour')`,
		accountID,
	)
	if _, err := pool.Exec(ctx,
		`UPDATE identity_action_secrets SET consumed_at = now() WHERE id = $1`,
		firstSecretID,
	); err != nil {
		t.Fatalf("consuming action secret: %v", err)
	}
	assertStatementFails(t, pool, ctx,
		`UPDATE identity_action_secrets SET consumed_at = consumed_at + interval '1 second' WHERE id = $1`,
		firstSecretID,
	)

	var eventID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO identity_security_events
		   (event_type, account_id, account_revision, request_id, evidence)
		 VALUES ('STUDENT_REGISTRATION_ACCEPTED', $1, 1, 'request-1', '{"schema_version":1}')
		 RETURNING id::text`,
		accountID,
	).Scan(&eventID); err != nil {
		t.Fatalf("creating Identity event: %v", err)
	}
	assertStatementFails(t, pool, ctx,
		`UPDATE identity_security_events SET evidence = '{}' WHERE id = $1`,
		eventID,
	)

	var outboxID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO outbox_events
		   (event_type, schema_version, source_module, aggregate_type, aggregate_id,
		    aggregate_revision, safe_payload, correlation_id)
		 VALUES ('identity.email_verification_requested', 1, 'IDENTITY_AND_ACCESS',
		         'ACCOUNT', $1, 1, '{"purpose":"EMAIL_VERIFICATION"}', 'request-1')
		 RETURNING id::text`,
		accountID,
	).Scan(&outboxID); err != nil {
		t.Fatalf("creating outbox event: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO outbox_protected_payloads (event_id, key_version, nonce, ciphertext)
		 VALUES ($1, 'dev-v1', decode(repeat('cc', 12), 'hex'), decode(repeat('dd', 32), 'hex'))`,
		outboxID,
	); err != nil {
		t.Fatalf("creating protected payload: %v", err)
	}
	assertStatementFails(t, pool, ctx,
		`UPDATE outbox_events SET safe_payload = '{}' WHERE id = $1`,
		outboxID,
	)
	assertStatementFails(t, pool, ctx,
		`INSERT INTO outbox_protected_payloads (event_id, key_version, nonce, ciphertext)
		 VALUES ($1, 'dev-v1', decode(repeat('ee', 12), 'hex'), decode(repeat('ff', 32), 'hex'))`,
		outboxID,
	)
}

func assertStatementFails(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	statement string,
	args ...any,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, statement, args...); err == nil {
		t.Fatalf("statement unexpectedly succeeded: %s", statement)
	}
}
