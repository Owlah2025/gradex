//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
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
)

func allTables() []string {
	all := append([]string{}, initTables...)
	all = append(all, identityTables...)
	all = append(all, auditTables...)
	all = append(all, sessionTables...)
	all = append(all, admissionTables...)
	all = append(all, staffTables...)
	return append(all, catalogTables...)
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
