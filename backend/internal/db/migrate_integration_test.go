//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgconn"
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
	staffTables   = []string{"staff_invitations"}
	catalogTables = []string{
		"taxonomy_terms",
		"course_revisions",
		"course_section_identities",
		"course_lesson_identities",
		"course_sections",
		"course_lessons",
		"lesson_files",
		"course_price_changes",
	}
	mediaTables = []string{
		"media_assets", "media_asset_versions", "upload_intents", "media_callback_receipts",
		"media_outbox_dispatches", "scan_attempts", "processing_attempts", "video_renditions",
		"legacy_media_mappings", "entitlements", "entitlement_adjustments",
	}
	protectedLearningTables  = []string{"enrollments", "content_reports"}
	courseAccessGrantTables  = []string{"course_access_invitations"}
	transactionalEmailTables = []string{"transactional_email_deliveries", "transactional_email_attempts"}
	purchaseRequestTables    = []string{"purchase_requests"}
)

func allTables() []string {
	all := append([]string{}, initTables...)
	all = append(all, identityTables...)
	all = append(all, auditTables...)
	all = append(all, sessionTables...)
	all = append(all, admissionTables...)
	all = append(all, staffTables...)
	all = append(all, catalogTables...)
	all = append(all, mediaTables...)
	all = append(all, protectedLearningTables...)
	all = append(all, courseAccessGrantTables...)
	all = append(all, transactionalEmailTables...)
	return append(all, purchaseRequestTables...)
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

// TestMediaMigrationRollbackHandlesD7Data proves 0012 can be rolled back from
// a database that actually contains its owned media rows and committed outbox
// work. The pre-D7 source-module constraint cannot be restored while a D7
// MEDIA_AND_ASSETS event remains, so this exercises the required cleanup order.
func TestMediaMigrationRollbackHandlesD7Data(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(CatalogSearchSchemaVersion)); err != nil {
		t.Fatalf("migrating to pre-D7 schema: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	const (
		instructorID = "11111111-1111-1111-1111-111111111111"
		courseID     = "22222222-2222-2222-2222-222222222222"
		assetID      = "33333333-3333-3333-3333-333333333333"
		versionID    = "44444444-4444-4444-4444-444444444444"
		eventID      = "55555555-5555-5555-5555-555555555555"
	)
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ($1::uuid, 'd7-rollback@example.test', 'd7-rollback@example.test', 'INSTRUCTOR', 'ACTIVE', 'D7 rollback', 'en', now())
	`, instructorID); err != nil {
		t.Fatalf("seeding D7 rollback instructor: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle)
		VALUES ($1::uuid, $2::uuid, 'DRAFT')
	`, courseID, instructorID); err != nil {
		t.Fatalf("seeding D7 rollback course: %v", err)
	}
	if err := m.Migrate(uint(MediaAndEntitlementSchemaVersion)); err != nil {
		t.Fatalf("migrating up to 0012: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES ($1::uuid, 'RESOURCE', $2::uuid, $3::uuid, 'PROTECTED')
	`, assetID, instructorID, courseID); err != nil {
		t.Fatalf("inserting representative media asset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes
		) VALUES ($1::uuid, $2::uuid, 'RESOURCE', 'UPLOADED', 'quarantine/rollback/source', 'object-v1', 'application/pdf', 1)
	`, versionID, assetID); err != nil {
		t.Fatalf("inserting representative media version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO outbox_events (
			id, event_type, schema_version, source_module, aggregate_type, aggregate_id,
			aggregate_revision, safe_payload, correlation_id
		) VALUES ($1::uuid, 'media.scan_requested', 1, 'MEDIA_AND_ASSETS', 'MEDIA_ASSET_VERSION',
			$2::uuid, 1, '{}'::jsonb, 'd7-rollback')
	`, eventID, versionID); err != nil {
		t.Fatalf("inserting representative D7 outbox event: %v", err)
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("rolling back 0012 with representative D7 data: %v", err)
	}
	state, err := ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema after 0012 down: %v", err)
	}
	if state.Version != CatalogSearchSchemaVersion || state.Dirty {
		t.Fatalf("schema after 0012 down = %+v, want clean version %d", state, CatalogSearchSchemaVersion)
	}
	var staleEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE source_module = 'MEDIA_AND_ASSETS'`).Scan(&staleEvents); err != nil {
		t.Fatalf("checking rollback outbox cleanup: %v", err)
	}
	if staleEvents != 0 {
		t.Fatalf("MEDIA_AND_ASSETS outbox rows survived rollback: %d", staleEvents)
	}
	var sourceCheck string
	if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'outbox_events_source_module'`).Scan(&sourceCheck); err != nil {
		t.Fatalf("reading restored source-module constraint: %v", err)
	}
	if strings.Contains(sourceCheck, "MEDIA_AND_ASSETS") {
		t.Fatalf("pre-D7 source-module constraint still accepts media rows: %s", sourceCheck)
	}

	if err := m.Steps(1); err != nil {
		t.Fatalf("reapplying 0012 after data-bearing rollback: %v", err)
	}
	state, err = ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema after 0012 up: %v", err)
	}
	if state.Version != MediaAndEntitlementSchemaVersion || state.Dirty {
		t.Fatalf("schema after 0012 up = %+v, want clean version %d", state, MediaAndEntitlementSchemaVersion)
	}
	if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'outbox_events_source_module'`).Scan(&sourceCheck); err != nil {
		t.Fatalf("reading re-applied source-module constraint: %v", err)
	}
	if !strings.Contains(sourceCheck, "MEDIA_AND_ASSETS") {
		t.Fatalf("re-applied source-module constraint does not accept media rows: %s", sourceCheck)
	}
}

func TestProtectedLearningMigrationSupportsUpgrade(t *testing.T) {
	for _, startVersion := range []int{
		0,                              // a clean install must still apply 0013 before 0014.
		RevisionIntegritySchemaVersion, // the supported pre-S5 upgrade boundary.
	} {
		t.Run(fmt.Sprintf("from-%04d", startVersion), func(t *testing.T) {
			freshDatabase(t)
			m := openMigrator(t)
			if startVersion > 0 {
				if err := m.Migrate(uint(startVersion)); err != nil {
					t.Fatalf("migrating to pre-S5 schema %d: %v", startVersion, err)
				}
			}

			// Stop at 0013 rather than only calling Up: this proves the
			// enrollment prerequisite exists before 0014 can install Progress.
			if err := m.Migrate(uint(EnrollmentSchemaVersion)); err != nil {
				t.Fatalf("migrating through enrollments: %v", err)
			}
			pool := openPool(t)
			if !tableExists(t, pool, "enrollments") || !tableExists(t, pool, "progress") {
				t.Fatal("0013 did not leave the enrollment and legacy progress prerequisites in place")
			}
			assertProgressUsesLegacyShape(t, pool)

			if err := m.Steps(1); err != nil {
				t.Fatalf("applying 0014 after enrollments: %v", err)
			}
			for _, table := range protectedLearningTables {
				if !tableExists(t, pool, table) {
					t.Errorf("table %s is missing after protected-learning upgrade", table)
				}
			}
			assertProtectedLearningSchema(t, pool)
		})
	}
}

func TestProgressUsesStableLessonIdentity(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("migrating protected-learning schema: %v", err)
	}
	pool := openPool(t)

	assertProtectedLearningSchema(t, pool)
}

func assertProtectedLearningSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var columns []string
	rows, err := pool.Query(ctx, `
		SELECT column_name || ':' || data_type || ':' || is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'progress'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatalf("reading progress columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scanning progress column: %v", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating progress columns: %v", err)
	}
	wantColumns := []string{
		"id:uuid:NO", "enrollment_id:uuid:NO", "course_lesson_identity_id:uuid:NO",
		"max_position_seconds:numeric:NO", "last_position_seconds:numeric:NO",
		"completed_at:timestamp with time zone:YES", "completing_asset_version_id:uuid:YES",
		"last_watched_at:timestamp with time zone:YES", "updated_at:timestamp with time zone:NO",
	}
	if strings.Join(columns, ",") != strings.Join(wantColumns, ",") {
		t.Fatalf("progress columns = %v, want %v", columns, wantColumns)
	}

	var definition string
	if err := pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'progress'::regclass AND conname = 'progress_course_lesson_identity_id_fkey'
	`).Scan(&definition); err != nil {
		t.Fatalf("reading progress lesson identity foreign key: %v", err)
	}
	if !strings.Contains(definition, "course_lesson_identities") {
		t.Fatalf("progress lesson identity foreign key = %q", definition)
	}
	for _, constraint := range []struct {
		name string
		want string
	}{
		{"progress_enrollment_id_fkey", "REFERENCES enrollments(id)"},
		{"prog_identity", "UNIQUE (enrollment_id, course_lesson_identity_id)"},
		{"prog_max_non_negative", "max_position_seconds >="},
		{"prog_last_non_negative", "last_position_seconds >="},
		{"prog_max_ge_last", "max_position_seconds >= last_position_seconds"},
		{"prog_completion_pair", "completed_at IS NULL"},
	} {
		if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'progress'::regclass AND conname = $1`, constraint.name).Scan(&definition); err != nil {
			t.Fatalf("reading progress constraint %s: %v", constraint.name, err)
		}
		if !strings.Contains(definition, constraint.want) {
			t.Fatalf("progress constraint %s = %q, want %q", constraint.name, definition, constraint.want)
		}
	}
	var progressIndex bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND tablename = 'progress' AND indexname = 'idx_progress_enrollment')`).Scan(&progressIndex); err != nil {
		t.Fatalf("checking progress enrollment index: %v", err)
	}
	if !progressIndex {
		t.Fatal("progress enrollment index is missing")
	}
	var legacyForeignKeys int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_constraint
		WHERE conrelid = 'progress'::regclass
		  AND contype = 'f'
		  AND confrelid IN ('lessons'::regclass, 'course_lessons'::regclass)
	`).Scan(&legacyForeignKeys); err != nil {
		t.Fatalf("counting legacy progress foreign keys: %v", err)
	}
	if legacyForeignKeys != 0 {
		t.Fatalf("progress has %d legacy or revision-row foreign keys", legacyForeignKeys)
	}
}

func assertProgressUsesLegacyShape(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var legacyColumn bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'progress' AND column_name = 'lesson_id')`).Scan(&legacyColumn); err != nil {
		t.Fatalf("checking pre-cutover progress shape: %v", err)
	}
	if !legacyColumn {
		t.Fatal("0014 was applied before the explicit 0014 step")
	}
}

func TestEnrollmentsShapeMatchesS6Contract(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("migrating schema: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'enrollments'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatalf("reading enrollment shape: %v", err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var name, typ, nullable string
		if err := rows.Scan(&name, &typ, &nullable); err != nil {
			t.Fatalf("scanning enrollment column: %v", err)
		}
		columns = append(columns, name+":"+typ+":"+nullable)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating enrollment columns: %v", err)
	}
	want := []string{"id:uuid:NO", "student_account_id:uuid:NO", "course_id:uuid:NO", "created_at:timestamp with time zone:NO"}
	if strings.Join(columns, ",") != strings.Join(want, ",") {
		t.Fatalf("enrollment columns = %v, want %v", columns, want)
	}
	var definition string
	if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'enr_one_per_student_course'`).Scan(&definition); err != nil {
		t.Fatalf("reading enrollment unique constraint: %v", err)
	}
	if !strings.Contains(definition, "UNIQUE (student_account_id, course_id)") {
		t.Fatalf("enrollment unique constraint = %q", definition)
	}
	for _, foreignKey := range []struct {
		name string
		want string
	}{
		{"enrollments_student_account_id_fkey", "REFERENCES accounts(id)"},
		{"enrollments_course_id_fkey", "REFERENCES courses(id)"},
	} {
		if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'enrollments'::regclass AND conname = $1`, foreignKey.name).Scan(&definition); err != nil {
			t.Fatalf("reading enrollment foreign key %s: %v", foreignKey.name, err)
		}
		if !strings.Contains(definition, foreignKey.want) {
			t.Fatalf("enrollment foreign key %s = %q, want %q", foreignKey.name, definition, foreignKey.want)
		}
	}
}

func TestContentReportsSchemaMatchesS5Contract(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("migrating schema: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	for _, constraint := range []struct {
		name string
		want string
	}{
		{"rep_other_needs_explanation", "length(btrim(explanation)) > 0"},
	} {
		var definition string
		if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'content_reports'::regclass AND conname = $1`, constraint.name).Scan(&definition); err != nil {
			t.Fatalf("reading content report constraint %s: %v", constraint.name, err)
		}
		if !strings.Contains(definition, constraint.want) {
			t.Fatalf("content report constraint %s = %q, want %q", constraint.name, definition, constraint.want)
		}
	}
	assertClosedCheckValues(t, pool, "rep_target_kind", []string{"COURSE", "LAB_MATERIAL", "LESSON", "RESOURCE", "VIDEO"})
	assertClosedCheckValues(t, pool, "rep_reason", []string{"broken_unavailable", "inaccurate", "inappropriate", "other", "suspected_copyright_violation"})
	var indexDefinition string
	if err := pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND tablename = 'content_reports' AND indexname = 'rep_no_duplicate_open'`).Scan(&indexDefinition); err != nil {
		t.Fatalf("reading content report partial unique index: %v", err)
	}
	if !strings.Contains(indexDefinition, "UNIQUE INDEX") || !strings.Contains(indexDefinition, "WHERE (resolved_at IS NULL)") {
		t.Fatalf("content report partial unique index = %q", indexDefinition)
	}
}

func assertClosedCheckValues(t *testing.T, pool *pgxpool.Pool, constraint string, want []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var definition string
	if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'content_reports'::regclass AND conname = $1`, constraint).Scan(&definition); err != nil {
		t.Fatalf("reading content report constraint %s: %v", constraint, err)
	}
	matches := regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(definition, -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("content report constraint %s values = %v, want fixed set %v", constraint, got, want)
	}
}

func TestProtectedLearningRollbackRestoresPreCutoverSchema(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(ProtectedLearningSchemaVersion)); err != nil {
		t.Fatalf("migrating through protected learning: %v", err)
	}
	pool := openPool(t)
	if err := m.Steps(-1); err != nil {
		t.Fatalf("rolling back protected learning: %v", err)
	}
	if tableExists(t, pool, "content_reports") {
		t.Fatal("content_reports survived the 0014 rollback")
	}
	assertProgressUsesLegacyShape(t, pool)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	state, err := ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema after 0014 rollback: %v", err)
	}
	if state.Version != EnrollmentSchemaVersion || state.Dirty {
		t.Fatalf("schema after 0014 rollback = %+v, want clean version %d", state, EnrollmentSchemaVersion)
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("reapplying protected learning after rollback: %v", err)
	}
	assertProtectedLearningSchema(t, pool)
}

func TestMaxSchemaVersionTracksCurrentSchema(t *testing.T) {
	// Tests elsewhere compare the live database state to MaxSchemaVersion,
	// rather than a literal. This makes the capability boundary explicit too.
	if TransactionalEmailMonitorTerminalSchemaVersion != SubjectCodeIdentitySchemaVersion+1 {
		t.Fatalf("transactional email monitor schema = %d, want one past subject code identity %d",
			TransactionalEmailMonitorTerminalSchemaVersion, SubjectCodeIdentitySchemaVersion)
	}
	if ReportModerationSchemaVersion != TransactionalEmailMonitorTerminalSchemaVersion+1 {
		t.Fatalf("report moderation schema = %d, want one past transactional email monitor %d",
			ReportModerationSchemaVersion, TransactionalEmailMonitorTerminalSchemaVersion)
	}
	if StudentEmailOTPSchemaVersion != ReportModerationSchemaVersion+1 {
		t.Fatalf("student email OTP schema = %d, want one past report moderation %d",
			StudentEmailOTPSchemaVersion, ReportModerationSchemaVersion)
	}
	if AuthenticatedPurchaseSchemaVersion != StudentEmailOTPSchemaVersion+1 {
		t.Fatalf("authenticated purchase schema = %d, want one past student email OTP %d",
			AuthenticatedPurchaseSchemaVersion, StudentEmailOTPSchemaVersion)
	}
	if MaxSchemaVersion != AuthenticatedPurchaseSchemaVersion {
		t.Fatalf("MaxSchemaVersion = %d, want current schema %d",
			MaxSchemaVersion, AuthenticatedPurchaseSchemaVersion)
	}
	if MailpitEmailSchemaVersion != EmailActivationSchemaVersion+1 {
		t.Fatalf("Mailpit email schema = %d, want one past email activation %d",
			MailpitEmailSchemaVersion, EmailActivationSchemaVersion)
	}
	// D-088 lands as two steps on purpose: PostgreSQL refuses to use a new enum
	// value in the transaction that created it, so the VALIDATED label is added
	// alone before anything can reference it.
	if MediaValidatedStateSchemaVersion != MailpitEmailSchemaVersion+1 {
		t.Fatalf("validated state schema = %d, want one past Mailpit email %d",
			MediaValidatedStateSchemaVersion, MailpitEmailSchemaVersion)
	}
	if TrustedValidationSchemaVersion != MediaValidatedStateSchemaVersion+1 {
		t.Fatalf("trusted validation schema = %d, want one past the validated state %d",
			TrustedValidationSchemaVersion, MediaValidatedStateSchemaVersion)
	}
	if ManualPurchaseRequestsSchemaVersion != TrustedValidationSchemaVersion+1 {
		t.Fatalf("manual purchase schema = %d, want one past trusted validation %d",
			ManualPurchaseRequestsSchemaVersion, TrustedValidationSchemaVersion)
	}
	if RevisionScopedPreviewSchemaVersion != ManualPurchaseRequestsSchemaVersion+1 {
		t.Fatalf("revision-scoped public preview schema = %d, want one past manual purchase %d",
			RevisionScopedPreviewSchemaVersion, ManualPurchaseRequestsSchemaVersion)
	}
	// D-091 T1 lands as one additive step on top of the preview schema.
	if AcademicCatalogSchemaVersion != RevisionScopedPreviewSchemaVersion+1 {
		t.Fatalf("academic catalog schema = %d, want one past revision-scoped preview %d",
			AcademicCatalogSchemaVersion, RevisionScopedPreviewSchemaVersion)
	}
	// D-092 T3 lands as one additive step on top of the academic catalog.
	if StudentAcademicProfileSchemaVersion != AcademicCatalogSchemaVersion+1 {
		t.Fatalf("student academic profile schema = %d, want one past the academic catalog %d",
			StudentAcademicProfileSchemaVersion, AcademicCatalogSchemaVersion)
	}
	// D-093 T4-A lands as one additive step on top of the Student profile.
	if CourseAcademicIdentitySchemaVersion != StudentAcademicProfileSchemaVersion+1 {
		t.Fatalf("course academic identity schema = %d, want one past the student profile %d",
			CourseAcademicIdentitySchemaVersion, StudentAcademicProfileSchemaVersion)
	}
	// T4-A.1 hardens Subject code identity in its own migration rather than by
	// editing 0025, which is already accepted and proven.
	if SubjectCodeIdentitySchemaVersion != CourseAcademicIdentitySchemaVersion+1 {
		t.Fatalf("subject code identity schema = %d, want one past course academic identity %d",
			SubjectCodeIdentitySchemaVersion, CourseAcademicIdentitySchemaVersion)
	}
}

func TestTransactionalEmailMonitorTerminalMigrationIsAdditiveAndReversible(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	assertStateAndIndex := func(label string, wantVersion int64, wantIndex bool) {
		t.Helper()
		state, err := ReadSchemaState(ctx, pool)
		if err != nil {
			t.Fatalf("%s: reading schema state: %v", label, err)
		}
		if state.Version != wantVersion || state.Dirty {
			t.Fatalf("%s: schema state = %+v, want clean version %d", label, state, wantVersion)
		}

		var indexDefinition string
		if err := pool.QueryRow(ctx, `
			SELECT COALESCE((
				SELECT indexdef
				FROM pg_indexes
				WHERE schemaname = 'public'
				  AND tablename = 'transactional_email_deliveries'
				  AND indexname = 'transactional_email_monitor_terminal_idx'
			), '')
		`).Scan(&indexDefinition); err != nil {
			t.Fatalf("%s: reading terminal monitor index: %v", label, err)
		}
		if !wantIndex {
			if indexDefinition != "" {
				t.Fatalf("%s: terminal monitor index still exists: %q", label, indexDefinition)
			}
			return
		}
		for _, fragment := range []string{
			"transactional_email_deliveries",
			"terminal_at",
			"event_id",
			"PERMANENT_FAILED",
			"EXHAUSTED",
		} {
			if !strings.Contains(indexDefinition, fragment) {
				t.Fatalf("%s: terminal monitor index = %q, missing %q", label, indexDefinition, fragment)
			}
		}
	}

	if err := m.Up(); err != nil {
		t.Fatalf("migrating to current schema: %v", err)
	}
	assertStateAndIndex("after up", MaxSchemaVersion, true)

	if err := m.Up(); !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("reapplying current migrations: %v, want ErrNoChange", err)
	}
	assertStateAndIndex("after repeated up", MaxSchemaVersion, true)

	if err := m.Migrate(uint(SubjectCodeIdentitySchemaVersion)); err != nil {
		t.Fatalf("rolling back migration 0027: %v", err)
	}
	assertStateAndIndex("after 0027 down", SubjectCodeIdentitySchemaVersion, false)

	if err := m.Migrate(uint(TransactionalEmailMonitorTerminalSchemaVersion)); err != nil {
		t.Fatalf("reapplying migration 0027: %v", err)
	}
	assertStateAndIndex("after 0027 reapply", TransactionalEmailMonitorTerminalSchemaVersion, true)
}

func TestManualPurchaseRollbackGuardRefusesLivePurchaseEntitlementWithoutDirtyingSchema(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("migrating up: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	const adminID = "41000000-0000-0000-0000-000000000001"
	const studentID = "41000000-0000-0000-0000-000000000002"
	const courseID = "42000000-0000-0000-0000-000000000001"
	var invitationID string
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		($1::uuid, 'rollback-admin@example.test', 'rollback-admin@example.test', 'ADMIN', 'ACTIVE', 'Rollback Admin'),
		($2::uuid, 'rollback-student@example.test', 'rollback-student@example.test', 'STUDENT', 'ACTIVE', 'Rollback Student')
	`, adminID, studentID); err != nil {
		t.Fatalf("seeding rollback accounts: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, courseID, adminID); err != nil {
		t.Fatalf("seeding rollback Course: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_access_invitations (normalized_email, email, course_id, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		VALUES ('rollback-student@example.test', 'rollback-student@example.test', $1::uuid, $2::uuid, $3::uuid, $2::uuid, 'APPROVED')
		RETURNING id::text
	`, courseID, adminID, studentID).Scan(&invitationID); err != nil {
		t.Fatalf("seeding purchase invitation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'PURCHASE_REQUEST', $3::uuid, now() + interval '1 day', now() + interval '1 day', now(), 'ACTIVE')
	`, studentID, courseID, invitationID); err != nil {
		t.Fatalf("creating live PURCHASE_REQUEST entitlement: %v", err)
	}
	if err := CheckManualPurchaseRollbackSafety(ctx, pool); err == nil || !strings.Contains(err.Error(), "PURCHASE_REQUEST entitlements exist") {
		t.Fatalf("rollback guard error = %v, want actionable purchase-entitlement refusal", err)
	}
	state, err := ReadSchemaState(ctx, pool)
	// The property is that a refused rollback leaves the fully-migrated marker
	// untouched and clean, so this tracks MaxSchemaVersion rather than whichever
	// migration happened to be last when the test was written.
	if err != nil || state.Version != MaxSchemaVersion || state.Dirty {
		t.Fatalf("schema marker after refused rollback = %+v (err=%v), want clean version %d", state, err, MaxSchemaVersion)
	}
	if !tableExists(t, pool, "purchase_requests") {
		t.Fatal("purchase request schema changed before rollback guard refused")
	}
	var grants int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM entitlements WHERE grant_source='PURCHASE_REQUEST'`).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("live purchase entitlement after refusal = %d (err=%v), want 1", grants, err)
	}
}

// This searches every S5 production owner, not a single known implementation
// file. It intentionally excludes historical migrations and 0014 down: those
// represent the pre-cutover schema only and are required for rollback.
func TestS5ProductionHasNoLegacyProgressOwnership(t *testing.T) {
	paths := []string{"../learning", "../httpapi", "migrations/0014_protected_learning.up.sql"}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stating %s: %v", path, err)
		}
		if !info.IsDir() {
			assertNoLegacyProgressOwnership(t, path)
			continue
		}
		err = filepath.Walk(path, func(file string, entry os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
				return nil
			}
			assertNoLegacyProgressOwnership(t, file)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", path, err)
		}
	}
}

func assertNoLegacyProgressOwnership(t *testing.T, path string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	for _, forbidden := range []string{
		"ON CONFLICT (enrollment_id, lesson_id)",
		"INSERT INTO progress (enrollment_id, lesson_id",
		"course_lessons.id AS progress",
		"REFERENCES lessons(id)",
		"REFERENCES course_lessons",
	} {
		if strings.Contains(string(contents), forbidden) {
			t.Fatalf("%s reintroduces legacy or revision-scoped Progress ownership: %q", path, forbidden)
		}
	}
}

func TestLegacyProgressGuardRefusesNonEmptyTable(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(EnrollmentSchemaVersion)); err != nil {
		t.Fatalf("migrating through enrollments: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ('11111111-1111-1111-1111-111111111111', 'legacy@example.test', 'legacy@example.test', 'INSTRUCTOR', 'ACTIVE', 'Legacy')
	`); err != nil {
		t.Fatalf("seeding owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', 'DRAFT')`); err != nil {
		t.Fatalf("seeding course: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sections (id, course_id, title, "order") VALUES ('33333333-3333-3333-3333-333333333333', '22222222-2222-2222-2222-222222222222', 'Legacy', 0)`); err != nil {
		t.Fatalf("seeding legacy section: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO lessons (id, section_id, title, "order") VALUES ('44444444-4444-4444-4444-444444444444', '33333333-3333-3333-3333-333333333333', 'Legacy', 0)`); err != nil {
		t.Fatalf("seeding legacy lesson: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO progress (user_id, lesson_id) VALUES ('55555555-5555-5555-5555-555555555555', '44444444-4444-4444-4444-444444444444')`); err != nil {
		t.Fatalf("seeding legacy progress: %v", err)
	}
	err := m.Steps(1)
	if err == nil || !strings.Contains(err.Error(), "legacy progress table contains 1 row") {
		t.Fatalf("migration error = %v", err)
	}
	state, stateErr := ReadSchemaState(ctx, pool)
	if stateErr != nil {
		t.Fatalf("reading failed migration state: %v", stateErr)
	}
	if state.Version != ProtectedLearningSchemaVersion || !state.Dirty {
		t.Fatalf("failed cutover state = %+v, want dirty version %d", state, ProtectedLearningSchemaVersion)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM progress`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("legacy progress rows = %d, err = %v", rows, err)
	}
}

func TestCatalogSearchMigrationSupportsCleanInstallAndUpgrade(t *testing.T) {
	const input = "  أإآٱ ىة ٠١٢٣٤٥٦٧٨٩ مَدْرَسـٌ  MIXED\tText  "
	const wantNormalized = "اااا يه 0123456789 مدرس mixed text"

	t.Run("clean install", func(t *testing.T) {
		freshDatabase(t)
		m := openMigrator(t)
		if err := m.Up(); err != nil {
			t.Fatalf("clean install: %v", err)
		}
		pool := openPool(t)
		assertCatalogNormalization(t, pool, input, wantNormalized)
		assertSearchTextColumn(t, pool)
		assertCourseSlugColumn(t, pool)
	})

	t.Run("upgrade with existing revisions", func(t *testing.T) {
		freshDatabase(t)
		m := openMigrator(t)
		if err := m.Migrate(uint(RevisionIntegritySchemaVersion)); err != nil {
			t.Fatalf("migrating to schema 10: %v", err)
		}
		pool := openPool(t)
		seeded := seedPreCatalogSearchRevisions(t, pool)

		if err := m.Migrate(uint(CatalogSearchSchemaVersion)); err != nil {
			t.Fatalf("upgrading to schema 11: %v", err)
		}
		assertCatalogNormalization(t, pool, input, wantNormalized)
		assertSearchTextColumn(t, pool)
		assertCourseSlugColumn(t, pool)

		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		var emptyDocuments int
		if err := pool.QueryRow(ctx, `
			SELECT count(*)
			FROM course_revisions
			WHERE search_text IS NULL OR length(trim(search_text)) = 0
		`).Scan(&emptyDocuments); err != nil {
			t.Fatalf("counting backfilled documents: %v", err)
		}
		if emptyDocuments != 0 {
			t.Errorf("pre-existing revisions with empty search_text = %d, want 0", emptyDocuments)
		}

		var publishedText string
		if err := pool.QueryRow(ctx, `SELECT search_text FROM course_revisions WHERE id = $1::uuid`, seeded.publishedRevisionID).Scan(&publishedText); err != nil {
			t.Fatalf("reading normalized Arabic title: %v", err)
		}
		if !strings.Contains(publishedText, "احياء 101") {
			t.Errorf("published search_text = %q, want folded Arabic title containing %q", publishedText, "احياء 101")
		}

		for _, tc := range []struct {
			name        string
			query       string
			wantResults int
		}{
			{name: "published live revision", query: "أحياء ١٠١", wantResults: 1},
			{name: "draft revision", query: "مسودة", wantResults: 0},
			{name: "superseded revision", query: "قديم", wantResults: 0},
		} {
			t.Run(tc.name, func(t *testing.T) {
				query := fmt.Sprintf(`
					SELECT count(*)
					FROM courses c
					JOIN course_revisions cr ON cr.course_id = c.id
					WHERE %s
					  AND cr.search_text LIKE '%%' || catalog_normalize_ar($1) || '%%'
				`, catalogpublic.PublishedOnly("c", "cr"))
				var results int
				if err := pool.QueryRow(ctx, query, tc.query).Scan(&results); err != nil {
					t.Fatalf("querying backfilled document: %v", err)
				}
				if results != tc.wantResults {
					t.Errorf("results = %d, want %d", results, tc.wantResults)
				}
			})
		}
		assertStableCourseSlug(t, pool, seeded)

		if err := m.Steps(-1); err != nil {
			t.Fatalf("reverting schema 11: %v", err)
		}
		state, err := ReadSchemaState(ctx, pool)
		if err != nil {
			t.Fatalf("reading schema state after reverting schema 11: %v", err)
		}
		if state.Version != RevisionIntegritySchemaVersion {
			t.Fatalf("version after reverting schema 11 = %d, want %d", state.Version, RevisionIntegritySchemaVersion)
		}
		assertCatalogSearchRemoved(t, pool)

		if err := m.Steps(1); err != nil {
			t.Fatalf("reapplying schema 11: %v", err)
		}
		assertCatalogNormalization(t, pool, input, wantNormalized)
		assertSearchTextColumn(t, pool)
	})
}

func TestCatalogSearchIndexSupportsLongDocumentsAndSubstringQuery(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("migrating to schema 11: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ('77777777-7777-7777-7777-777777777777', 'long-search@example.test', 'long-search@example.test', 'INSTRUCTOR', 'ACTIVE', 'Long Search')
	`); err != nil {
		t.Fatalf("seeding long-document owner: %v", err)
	}
	var courseID, revisionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO courses (owner_account_id, lifecycle)
		VALUES ('77777777-7777-7777-7777-777777777777', 'DRAFT')
		RETURNING id::text
	`).Scan(&courseID); err != nil {
		t.Fatalf("creating long-document course: %v", err)
	}

	longArabic := strings.Repeat("وصف عربي مطول ", 600)
	longEnglish := strings.Repeat("long English description ", 600) + " indexed-needle "
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES ($1::uuid, 'DRAFT', 1, 'عنوان', 'Long document', $2, $3)
		RETURNING id::text
	`, courseID, longArabic, longEnglish).Scan(&revisionID); err != nil {
		t.Fatalf("inserting multi-thousand-character revision: %v", err)
	}

	updatedArabic := strings.Repeat("تحديث عربي مطول ", 700)
	updatedEnglish := strings.Repeat("updated English description ", 700) + " replacement-needle "
	if _, err := pool.Exec(ctx, `
		UPDATE course_revisions
		SET description_ar = $1, description_en = $2
		WHERE id = $3::uuid
	`, updatedArabic, updatedEnglish, revisionID); err != nil {
		t.Fatalf("updating multi-thousand-character revision: %v", err)
	}

	var matchedID string
	if err := pool.QueryRow(ctx, `
		SELECT id::text
		FROM course_revisions
		WHERE search_text LIKE '%' || catalog_normalize_ar($1) || '%'
	`, "replacement-needle").Scan(&matchedID); err != nil {
		t.Fatalf("querying normalized substring: %v", err)
	}
	if matchedID != revisionID {
		t.Errorf("normalized substring row = %q, want %q", matchedID, revisionID)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring connection for explain: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatalf("preferring approved search index: %v", err)
	}
	rows, err := conn.Query(ctx, `
		EXPLAIN (COSTS OFF)
		SELECT id
		FROM course_revisions
		WHERE search_text LIKE '%' || catalog_normalize_ar($1) || '%'
	`, "replacement-needle")
	if err != nil {
		t.Fatalf("explaining normalized substring query: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scanning explain line: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading explain plan: %v", err)
	}
	if !strings.Contains(strings.Join(plan, "\n"), "course_revisions_search_text_trgm_idx") {
		t.Errorf("normalized substring plan does not use the approved trigram index:\n%s", strings.Join(plan, "\n"))
	}
}

type preCatalogSearchRevisions struct {
	publishedRevisionID string
	publishedCourseID   string
}

func seedPreCatalogSearchRevisions(t *testing.T, pool *pgxpool.Pool) preCatalogSearchRevisions {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	const ownerID = "11111111-1111-1111-1111-111111111111"
	const publishedCourseID = "22222222-2222-2222-2222-222222222222"
	const publishedRevisionID = "33333333-3333-3333-3333-333333333333"
	const supersededRevisionID = "44444444-4444-4444-4444-444444444444"
	const draftCourseID = "55555555-5555-5555-5555-555555555555"
	const draftRevisionID = "66666666-6666-6666-6666-666666666666"

	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, 'catalog-owner@example.test', 'catalog-owner@example.test', 'INSTRUCTOR', 'ACTIVE', 'Catalogue Owner')
	`, ownerID); err != nil {
		t.Fatalf("seeding owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle) VALUES
		($1::uuid, $3::uuid, 'DRAFT'),
		($2::uuid, $3::uuid, 'DRAFT')
	`, publishedCourseID, draftCourseID, ownerID); err != nil {
		t.Fatalf("seeding courses: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_revisions
			(id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES
			($1::uuid, $2::uuid, 'APPROVED', 1, 'أحيَاء ١٠١', 'Biology', 'وصف', 'Live revision'),
			($3::uuid, $2::uuid, 'SUPERSEDED', 2, 'قديم', 'Withdrawn', 'وصف قديم', 'Superseded revision'),
			($4::uuid, $5::uuid, 'DRAFT', 1, 'مسودة', 'Draft', 'وصف مسودة', 'Draft revision')
	`, publishedRevisionID, publishedCourseID, supersededRevisionID, draftRevisionID, draftCourseID); err != nil {
		t.Fatalf("seeding pre-migration revisions: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE courses
		SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid
		WHERE id = $2::uuid
	`, publishedRevisionID, publishedCourseID); err != nil {
		t.Fatalf("publishing seeded course: %v", err)
	}
	return preCatalogSearchRevisions{publishedRevisionID: publishedRevisionID, publishedCourseID: publishedCourseID}
}

func assertCourseSlugColumn(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var generated string
	if err := pool.QueryRow(ctx, `SELECT is_generated FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'courses' AND column_name = 'slug'`).Scan(&generated); err != nil {
		t.Fatalf("reading course slug metadata: %v", err)
	}
	if generated != "ALWAYS" {
		t.Errorf("courses.slug is_generated = %q, want ALWAYS", generated)
	}
	var revisionSlug bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'course_revisions' AND column_name = 'slug')`).Scan(&revisionSlug); err != nil {
		t.Fatalf("checking revision slug absence: %v", err)
	}
	if revisionSlug {
		t.Error("course_revisions unexpectedly owns slug")
	}
}

func assertStableCourseSlug(t *testing.T, pool *pgxpool.Pool, seeded preCatalogSearchRevisions) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	want := "course-" + strings.ReplaceAll(seeded.publishedCourseID, "-", "")
	var slug string
	if err := pool.QueryRow(ctx, `SELECT slug FROM courses WHERE id = $1::uuid`, seeded.publishedCourseID).Scan(&slug); err != nil || slug != want {
		t.Fatalf("upgraded course slug = %q (%v), want %q", slug, err, want)
	}
	var newCourseID, newSlug string
	if err := pool.QueryRow(ctx, `INSERT INTO courses (owner_account_id, lifecycle) VALUES ('11111111-1111-1111-1111-111111111111', 'DRAFT') RETURNING id::text, slug`).Scan(&newCourseID, &newSlug); err != nil {
		t.Fatalf("creating slugged course: %v", err)
	}
	if newSlug != "course-"+strings.ReplaceAll(newCourseID, "-", "") {
		t.Errorf("new course slug = %q", newSlug)
	}
	if _, err := pool.Exec(ctx, `UPDATE course_revisions SET title_en = 'Changed title' WHERE id = $1::uuid`, seeded.publishedRevisionID); err != nil {
		t.Fatalf("changing title: %v", err)
	}
	var newRevisionID string
	if err := pool.QueryRow(ctx, `INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, 'APPROVED', 3, 'عنوان جديد', 'New revision') RETURNING id::text`, seeded.publishedCourseID).Scan(&newRevisionID); err != nil {
		t.Fatalf("creating new revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, newRevisionID, seeded.publishedCourseID); err != nil {
		t.Fatalf("changing live revision: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT slug FROM courses WHERE id = $1::uuid`, seeded.publishedCourseID).Scan(&slug); err != nil || slug != want {
		t.Errorf("stable slug after revision changes = %q (%v), want %q", slug, err, want)
	}
}

func assertCatalogNormalization(t *testing.T, pool *pgxpool.Pool, input, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var got string
	if err := pool.QueryRow(ctx, `SELECT catalog_normalize_ar($1)`, input).Scan(&got); err != nil {
		t.Fatalf("normalizing catalogue text: %v", err)
	}
	if got != want {
		t.Errorf("catalog_normalize_ar(%q) = %q, want %q", input, got, want)
	}
}

func assertSearchTextColumn(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var generated string
	if err := pool.QueryRow(ctx, `
		SELECT is_generated
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'course_revisions' AND column_name = 'search_text'
	`).Scan(&generated); err != nil {
		t.Fatalf("reading search_text column metadata: %v", err)
	}
	if generated != "ALWAYS" {
		t.Errorf("search_text is_generated = %q, want ALWAYS", generated)
	}
}

func assertCatalogSearchRemoved(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'course_revisions' AND column_name = 'search_text'
		)
	`).Scan(&exists); err != nil {
		t.Fatalf("checking removed search_text column: %v", err)
	}
	if exists {
		t.Error("search_text column survived schema 11 down migration")
	}

	if err := pool.QueryRow(ctx, `SELECT to_regprocedure('catalog_normalize_ar(text)') IS NULL`).Scan(&exists); err != nil {
		t.Fatalf("checking removed catalog normalizer: %v", err)
	}
	if !exists {
		t.Error("catalog_normalize_ar function survived schema 11 down migration")
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

func TestCapabilityAwareSchemaMinimum(t *testing.T) {
	freshDatabase(t)
	pool := openPool(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(SessionSchemaVersion)); err != nil {
		t.Fatalf("migrating to the session capability schema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if err := CheckSchemaAtLeast(ctx, pool, SessionSchemaVersion); err != nil {
		t.Fatalf("session capability rejected schema %d: %v", SessionSchemaVersion, err)
	}
	err := CheckSchemaAtLeast(ctx, pool, AdmissionSchemaVersion)
	if !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("admission capability accepted schema %d: %v", SessionSchemaVersion, err)
	}

	if err := m.Migrate(uint(AdmissionSchemaVersion)); err != nil {
		t.Fatalf("migrating to the admission capability schema: %v", err)
	}
	if err := CheckSchemaAtLeast(ctx, pool, AdmissionSchemaVersion); err != nil {
		t.Fatalf("admission capability rejected schema %d: %v", AdmissionSchemaVersion, err)
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

func assertConstraintViolation(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	expectedConstraint string,
	statement string,
	args ...any,
) {
	t.Helper()
	_, err := pool.Exec(ctx, statement, args...)
	if err == nil {
		t.Fatalf("statement unexpectedly succeeded, expected constraint violation %s: %s", expectedConstraint, statement)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected pgconn.PgError for constraint %s, got %v", expectedConstraint, err)
	}
	if pgErr.ConstraintName != expectedConstraint {
		t.Fatalf("expected constraint violation %s, got constraint %s (code %s, msg: %s)", expectedConstraint, pgErr.ConstraintName, pgErr.Code, pgErr.Message)
	}
}

func TestStaffLifecycleSchemaInvariants(t *testing.T) {
	freshDatabase(t)
	pool := openPool(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("up: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var adminAccountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (normalized_email, email, role, status, display_name)
		 VALUES ('admin@example.com', 'Admin@Example.com', 'ADMIN', 'ACTIVE', 'Admin User')
		 RETURNING id::text`,
	).Scan(&adminAccountID); err != nil {
		t.Fatalf("creating Admin Account fixture: %v", err)
	}

	// 1. STAFF_INVITATION action secret with NULL account_id succeeds.
	var secretID1 string
	if err := pool.QueryRow(ctx,
		`INSERT INTO identity_action_secrets
		   (account_id, purpose, secret_digest, expires_at)
		 VALUES (NULL, 'STAFF_INVITATION', decode(repeat('a1', 32), 'hex'), now() + interval '1 hour')
		 RETURNING id::text`,
	).Scan(&secretID1); err != nil {
		t.Fatalf("creating STAFF_INVITATION secret: %v", err)
	}

	// EMAIL_VERIFICATION action secret with NULL account_id must fail.
	assertStatementFails(t, pool, ctx,
		`INSERT INTO identity_action_secrets
		   (account_id, purpose, secret_digest, expires_at)
		 VALUES (NULL, 'EMAIL_VERIFICATION', decode(repeat('a2', 32), 'hex'), now() + interval '1 hour')`,
	)

	// 2. staff_invitations insertion and partial unique index on PENDING normalized_email.
	var invID1 string
	if err := pool.QueryRow(ctx,
		`INSERT INTO staff_invitations
		   (normalized_email, email, invited_role, inviter_account_id, state, action_secret_id)
		 VALUES ('instructor@example.com', 'Instructor@Example.com', 'INSTRUCTOR', $1, 'PENDING', $2)
		 RETURNING id::text`,
		adminAccountID, secretID1,
	).Scan(&invID1); err != nil {
		t.Fatalf("creating staff_invitation: %v", err)
	}

	// Inserting second PENDING invitation for same normalized email must fail.
	var secretID2 string
	if err := pool.QueryRow(ctx,
		`INSERT INTO identity_action_secrets
		   (account_id, purpose, secret_digest, expires_at)
		 VALUES (NULL, 'STAFF_INVITATION', decode(repeat('b1', 32), 'hex'), now() + interval '1 hour')
		 RETURNING id::text`,
	).Scan(&secretID2); err != nil {
		t.Fatalf("creating second STAFF_INVITATION secret: %v", err)
	}

	assertStatementFails(t, pool, ctx,
		`INSERT INTO staff_invitations
		   (normalized_email, email, invited_role, inviter_account_id, state, action_secret_id)
		 VALUES ('instructor@example.com', 'Instructor@Example.com', 'INSTRUCTOR', $1, 'PENDING', $2)`,
		adminAccountID, secretID2,
	)

	// Transitioning first invitation to SUPERSEDED allows new PENDING invitation.
	if _, err := pool.Exec(ctx,
		`UPDATE staff_invitations SET state = 'SUPERSEDED' WHERE id = $1`,
		invID1,
	); err != nil {
		t.Fatalf("updating invitation state to SUPERSEDED: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO staff_invitations
		   (normalized_email, email, invited_role, inviter_account_id, state, action_secret_id)
		 VALUES ('instructor@example.com', 'Instructor@Example.com', 'INSTRUCTOR', $1, 'PENDING', $2)`,
		adminAccountID, secretID2,
	); err != nil {
		t.Fatalf("inserting second PENDING invitation after first was SUPERSEDED: %v", err)
	}

	// 3. Security events for ACCOUNT_SUSPENDED and ACCOUNT_REINSTATED.
	if _, err := pool.Exec(ctx,
		`INSERT INTO identity_security_events
		   (event_type, account_id, account_revision, request_id, evidence)
		 VALUES ('ACCOUNT_SUSPENDED', $1, 1, 'req-1', '{"reason":"ACCOUNT_SUSPENDED"}'),
		        ('ACCOUNT_REINSTATED', $1, 1, 'req-2', '{"reason":"REINSTATED"}')`,
		adminAccountID,
	); err != nil {
		t.Fatalf("creating suspension security events: %v", err)
	}
}

func TestCourseAccessGrantSchemaInvariants(t *testing.T) {
	freshDatabase(t)
	pool := openPool(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("up: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var adminAccountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (normalized_email, email, role, status, display_name)
		 VALUES ('admin-s6@example.com', 'Admin-S6@Example.com', 'ADMIN', 'ACTIVE', 'Admin S6')
		 RETURNING id::text`,
	).Scan(&adminAccountID); err != nil {
		t.Fatalf("creating Admin Account fixture: %v", err)
	}

	var courseID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO courses (owner_account_id, lifecycle)
		 VALUES ($1, 'DRAFT')
		 RETURNING id::text`,
		adminAccountID,
	).Scan(&courseID); err != nil {
		t.Fatalf("creating Course fixture: %v", err)
	}

	// 1. COURSE_ACCESS_INVITATION action secret with NULL account_id succeeds.
	var secretID1 string
	if err := pool.QueryRow(ctx,
		`INSERT INTO identity_action_secrets
		   (account_id, purpose, secret_digest, expires_at)
		 VALUES (NULL, 'COURSE_ACCESS_INVITATION', decode(repeat('c1', 32), 'hex'), now() + interval '1 hour')
		 RETURNING id::text`,
	).Scan(&secretID1); err != nil {
		t.Fatalf("creating COURSE_ACCESS_INVITATION secret: %v", err)
	}

	// 2. course_access_invitations insertion and partial unique index on non-terminal pair.
	var invID1 string
	if err := pool.QueryRow(ctx,
		`INSERT INTO course_access_invitations
		   (normalized_email, email, course_id, created_by_account_id, state, action_secret_id)
		 VALUES ('student@example.com', 'Student@Example.com', $1, $2, 'PENDING_STUDENT_ACCEPTANCE', $3)
		 RETURNING id::text`,
		courseID, adminAccountID, secretID1,
	).Scan(&invID1); err != nil {
		t.Fatalf("creating course_access_invitation: %v", err)
	}

	// Inserting second PENDING invitation for same (normalized_email, course_id) must fail with cai_one_non_terminal_per_pair.
	var secretID2 string
	if err := pool.QueryRow(ctx,
		`INSERT INTO identity_action_secrets
		   (account_id, purpose, secret_digest, expires_at)
		 VALUES (NULL, 'COURSE_ACCESS_INVITATION', decode(repeat('c2', 32), 'hex'), now() + interval '1 hour')
		 RETURNING id::text`,
	).Scan(&secretID2); err != nil {
		t.Fatalf("creating second COURSE_ACCESS_INVITATION secret: %v", err)
	}

	assertConstraintViolation(t, pool, ctx, "cai_one_non_terminal_per_pair",
		`INSERT INTO course_access_invitations
		   (normalized_email, email, course_id, created_by_account_id, state, action_secret_id)
		 VALUES ('student@example.com', 'Student@Example.com', $1, $2, 'PENDING_STUDENT_ACCEPTANCE', $3)`,
		courseID, adminAccountID, secretID2,
	)

	// 3. Exact constraint assertions on course_access_invitations
	// cai_state_valid
	assertConstraintViolation(t, pool, ctx, "cai_state_valid",
		`INSERT INTO course_access_invitations (normalized_email, email, course_id, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		 VALUES ('invalid@example.com', 'invalid@example.com', $1, $2, $2, $2, 'INVALID_STATE')`,
		courseID, adminAccountID,
	)

	// cai_rejection_needs_reason
	assertConstraintViolation(t, pool, ctx, "cai_rejection_needs_reason",
		`INSERT INTO course_access_invitations (normalized_email, email, course_id, created_by_account_id, accepted_by_account_id, decided_by_account_id, state, decision_reason)
		 VALUES ('rej@example.com', 'rej@example.com', $1, $2, $2, $2, 'REJECTED', NULL)`,
		courseID, adminAccountID,
	)

	// cai_decided_has_actor
	assertConstraintViolation(t, pool, ctx, "cai_decided_has_actor",
		`INSERT INTO course_access_invitations (normalized_email, email, course_id, created_by_account_id, state, decided_by_account_id, accepted_by_account_id)
		 VALUES ('dec@example.com', 'dec@example.com', $1, $2, 'APPROVED', NULL, $2)`,
		courseID, adminAccountID,
	)

	// cai_accepted_has_actor
	assertConstraintViolation(t, pool, ctx, "cai_accepted_has_actor",
		`INSERT INTO course_access_invitations (normalized_email, email, course_id, created_by_account_id, state, decided_by_account_id, accepted_by_account_id)
		 VALUES ('acc@example.com', 'acc@example.com', $1, $2, 'APPROVED', $2, NULL)`,
		courseID, adminAccountID,
	)

	// cai_email_present
	assertConstraintViolation(t, pool, ctx, "cai_email_present",
		`INSERT INTO course_access_invitations (normalized_email, email, course_id, created_by_account_id, state)
		 VALUES ('', '', $1, $2, 'PENDING_STUDENT_ACCEPTANCE')`,
		courseID, adminAccountID,
	)

	// 4. Foreign key fk_entitlements_source_invitation and ent_manual_needs_invitation check.
	assertConstraintViolation(t, pool, ctx, "ent_manual_needs_invitation",
		`INSERT INTO entitlements
		   (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at)
		 VALUES ($1, 'COURSE', $2, $2, 'MANUAL_INVITATION', NULL, now() + interval '1 day', now() + interval '1 day', now())`,
		adminAccountID, courseID,
	)
	assertConstraintViolation(t, pool, ctx, "ent_purchase_needs_invitation",
		`INSERT INTO entitlements
		   (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at)
		 VALUES ($1, 'COURSE', $2, $2, 'PURCHASE_REQUEST', NULL, now() + interval '1 day', now() + interval '1 day', now())`,
		adminAccountID, courseID,
	)

	// 5. Verification that courses.default_access_ends_at column exists.
	if _, err := pool.Exec(ctx, `UPDATE courses SET default_access_ends_at = now() + interval '30 days' WHERE id = $1`, courseID); err != nil {
		t.Fatalf("updating default_access_ends_at: %v", err)
	}
}

func TestPre0015EntitlementMigrationAndConstraints(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)

	// 1. Migrate through version 14.
	if err := m.Migrate(uint(ProtectedLearningSchemaVersion)); err != nil {
		t.Fatalf("migrating through version 14: %v", err)
	}

	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var adminID, courseID string
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (normalized_email, email, role, status, display_name) VALUES ('pre15@example.com', 'pre15@example.com', 'ADMIN', 'ACTIVE', 'Pre15 User') RETURNING id::text`).Scan(&adminID); err != nil {
		t.Fatalf("seeding pre-0015 account: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO courses (owner_account_id, lifecycle) VALUES ($1::uuid, 'DRAFT') RETURNING id::text`, adminID).Scan(&courseID); err != nil {
		t.Fatalf("seeding pre-0015 course: %v", err)
	}

	// 2. Insert valid pre-0015 Entitlement with null source_invitation_id (MANUAL_INVITATION without invitation ID on v14 schema).
	var pre15EntitlementID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO entitlements
			(student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', NULL, now() + interval '30 days', now() + interval '30 days', now(), 'ACTIVE')
		RETURNING id::text
	`, adminID, courseID).Scan(&pre15EntitlementID); err != nil {
		t.Fatalf("seeding pre-0015 entitlement: %v", err)
	}

	// 3. Migrate to version 15 successfully.
	if err := m.Migrate(uint(CourseAccessGrantSchemaVersion)); err != nil {
		t.Fatalf("migrating to version 15 with pre-0015 entitlement: %v", err)
	}

	// 4. Confirm dirty = false and version = 15.
	state, err := ReadSchemaState(ctx, pool)
	if err != nil || state.Version != CourseAccessGrantSchemaVersion || state.Dirty {
		t.Fatalf("schema state = %+v (err=%v), want clean version 15", state, err)
	}

	// 5. Confirm legacy row remains intact.
	var legacySourceInvID *string
	if err := pool.QueryRow(ctx, `SELECT source_invitation_id::text FROM entitlements WHERE id = $1::uuid`, pre15EntitlementID).Scan(&legacySourceInvID); err != nil {
		t.Fatalf("reading pre-0015 entitlement: %v", err)
	}
	if legacySourceInvID != nil {
		t.Errorf("legacy entitlement source_invitation_id = %v, want nil", legacySourceInvID)
	}

	// 6. Confirm new manual-invitation Entitlement without invitation ID is rejected on ent_manual_needs_invitation.
	var newStudentID string
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (normalized_email, email, role, status, display_name) VALUES ('newstudent@example.com', 'newstudent@example.com', 'STUDENT', 'ACTIVE', 'New Student') RETURNING id::text`).Scan(&newStudentID); err != nil {
		t.Fatalf("seeding new student account: %v", err)
	}

	assertConstraintViolation(t, pool, ctx, "ent_manual_needs_invitation",
		`INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		 VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', NULL, now() + interval '1 day', now() + interval '1 day', now(), 'ACTIVE')`,
		newStudentID, courseID,
	)

	// 7. Confirm valid new manual-invitation Entitlement with invitation ID succeeds.
	var invID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_access_invitations (normalized_email, email, course_id, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		VALUES ('newstudent@example.com', 'newstudent@example.com', $1::uuid, $2::uuid, $3::uuid, $2::uuid, 'APPROVED')
		RETURNING id::text
	`, courseID, adminID, newStudentID).Scan(&invID); err != nil {
		t.Fatalf("creating invitation fixture: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, now() + interval '1 day', now() + interval '1 day', now(), 'ACTIVE')
	`, newStudentID, courseID, invID); err != nil {
		t.Fatalf("inserting valid invitation-backed entitlement: %v", err)
	}
}

func TestCourseAccessGrantRollbackAndReUpgradeSafe(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)

	// 1. Migrate to version 15.
	if err := m.Migrate(uint(CourseAccessGrantSchemaVersion)); err != nil {
		t.Fatalf("migrating to version 15: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var adminID, courseID string
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (normalized_email, email, role, status, display_name) VALUES ('rollback@example.com', 'rollback@example.com', 'ADMIN', 'ACTIVE', 'Rollback User') RETURNING id::text`).Scan(&adminID); err != nil {
		t.Fatalf("seeding account: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO courses (owner_account_id, lifecycle) VALUES ($1::uuid, 'DRAFT') RETURNING id::text`, adminID).Scan(&courseID); err != nil {
		t.Fatalf("seeding course: %v", err)
	}

	// 2. Create valid S6 invitation.
	var invID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_access_invitations (normalized_email, email, course_id, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		VALUES ('rollback-inv@example.com', 'rollback-inv@example.com', $1::uuid, $2::uuid, $2::uuid, $2::uuid, 'APPROVED')
		RETURNING id::text
	`, courseID, adminID).Scan(&invID); err != nil {
		t.Fatalf("seeding invitation: %v", err)
	}

	// 3. Create Entitlement referencing that invitation.
	var entID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, now() + interval '1 day', now() + interval '1 day', now(), 'ACTIVE')
		RETURNING id::text
	`, adminID, courseID, invID).Scan(&entID); err != nil {
		t.Fatalf("seeding entitlement: %v", err)
	}

	// 4. Migrate down one version (to 14).
	if err := m.Steps(-1); err != nil {
		t.Fatalf("rolling back to version 14: %v", err)
	}

	// 5. Verify Entitlement still exists.
	var entCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM entitlements WHERE id = $1::uuid`, entID).Scan(&entCount); err != nil || entCount != 1 {
		t.Fatalf("entitlement count after rollback = %d (err=%v), want 1", entCount, err)
	}

	// 6. Verify source_invitation_id is null.
	var rolledBackInvID *string
	if err := pool.QueryRow(ctx, `SELECT source_invitation_id::text FROM entitlements WHERE id = $1::uuid`, entID).Scan(&rolledBackInvID); err != nil {
		t.Fatalf("reading entitlement source_invitation_id after rollback: %v", err)
	}
	if rolledBackInvID != nil {
		t.Errorf("source_invitation_id after rollback = %v, want nil", rolledBackInvID)
	}

	// 7. Migrate back to version 15.
	if err := m.Steps(1); err != nil {
		t.Fatalf("re-upgrading to version 15: %v", err)
	}

	// 8. Verify version 15 is clean.
	state, err := ReadSchemaState(ctx, pool)
	if err != nil || state.Version != CourseAccessGrantSchemaVersion || state.Dirty {
		t.Fatalf("re-upgrade schema state = %+v (err=%v), want clean version 15", state, err)
	}

	// 9. Verify recreated foreign key works.
	var studentBAccountID string
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (normalized_email, email, role, status, display_name) VALUES ('studentb@example.com', 'studentb@example.com', 'STUDENT', 'ACTIVE', 'Student B') RETURNING id::text`).Scan(&studentBAccountID); err != nil {
		t.Fatalf("seeding student B account: %v", err)
	}

	var newInvID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_access_invitations (normalized_email, email, course_id, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		VALUES ('studentb@example.com', 'studentb@example.com', $1::uuid, $2::uuid, $3::uuid, $2::uuid, 'APPROVED')
		RETURNING id::text
	`, courseID, adminID, studentBAccountID).Scan(&newInvID); err != nil {
		t.Fatalf("creating new invitation after re-upgrade: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, now() + interval '1 day', now() + interval '1 day', now(), 'ACTIVE')
	`, studentBAccountID, courseID, newInvID); err != nil {
		t.Fatalf("inserting entitlement with recreated FK: %v", err)
	}

	var studentCAccountID string
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (normalized_email, email, role, status, display_name) VALUES ('studentc@example.com', 'studentc@example.com', 'STUDENT', 'ACTIVE', 'Student C') RETURNING id::text`).Scan(&studentCAccountID); err != nil {
		t.Fatalf("seeding student C account: %v", err)
	}

	badFKID := "99999999-9999-9999-9999-999999999999"
	assertConstraintViolation(t, pool, ctx, "fk_entitlements_source_invitation", `
		INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, now() + interval '1 day', now() + interval '1 day', now(), 'ACTIVE')
	`, studentCAccountID, courseID, badFKID)
}
