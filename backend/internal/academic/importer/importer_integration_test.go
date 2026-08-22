//go:build integration

package importer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/academic/manifest"
)

// Every assertion here runs against real PostgreSQL through the real repository
// and the real T1 schema constraints. Nothing re-implements SQL in Go.

const testDSN = "postgres://gradex:gradex@localhost:5432/gradex_importer_test?sslmode=disable"

const adminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"

func freshImporterDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Skipf("PostgreSQL is unavailable: %v", err)
	}
	_, _ = admin.Exec(ctx,
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'gradex_importer_test'")
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS gradex_importer_test"); err != nil {
		t.Fatalf("dropping the importer test database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE gradex_importer_test"); err != nil {
		t.Fatalf("creating the importer test database: %v", err)
	}
	admin.Close()

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("connecting to the importer test database: %v", err)
	}
	t.Cleanup(pool.Close)
	applySchema(t, pool)
	return pool
}

// applySchema runs the checked-in migrations with the same tool the application
// uses, so the importer is tested against the real schema rather than a
// hand-written approximation of it.
func applySchema(t *testing.T, _ *pgxpool.Pool) {
	t.Helper()
	migrator, err := migrate.New("file://../../db/migrations", testDSN)
	if err != nil {
		t.Fatalf("opening migrations: %v", err)
	}
	defer func() {
		sourceErr, dbErr := migrator.Close()
		if sourceErr != nil || dbErr != nil {
			t.Logf("closing migrator: source=%v db=%v", sourceErr, dbErr)
		}
	}()
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating the importer test database: %v", err)
	}
}

func newImporter(t *testing.T, pool *pgxpool.Pool) *Importer {
	t.Helper()
	repository, err := academic.NewRepository(pool)
	if err != nil {
		t.Fatalf("academic.NewRepository: %v", err)
	}
	catalogImporter, err := New(repository)
	if err != nil {
		t.Fatalf("importer.New: %v", err)
	}
	return catalogImporter
}

func systemOptions(apply bool) Options {
	return Options{Actor: academic.SystemActor("system:catalog-import-test"), Apply: apply}
}

func counts(t *testing.T, pool *pgxpool.Pool) map[string]int {
	t.Helper()
	ctx := context.Background()
	result := map[string]int{}
	for _, table := range []string{
		"institutions", "academic_units", "programs", "curricula", "subjects", "curriculum_subjects",
	} {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		result[table] = n
	}
	return result
}

func launchPackage(t *testing.T) *manifest.Package {
	t.Helper()
	pkg, err := manifest.Load("kuwait-university-launch-v1")
	if err != nil {
		t.Fatalf("loading the launch manifest: %v", err)
	}
	return pkg
}

func TestDryRunWritesNothing(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)

	plan, err := catalogImporter.Run(context.Background(), launchPackage(t), systemOptions(false))
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if plan.Applied {
		t.Fatal("a dry run reported itself as applied")
	}
	if plan.Counts.Create == 0 {
		t.Fatal("a dry run against an empty catalog planned no creates")
	}
	for table, n := range counts(t, pool) {
		if n != 0 {
			t.Fatalf("dry run wrote %d rows into %s", n, table)
		}
	}
	// An audit event would also be a write.
	var audits int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action LIKE 'ACADEMIC_%'`).Scan(&audits); err != nil {
		t.Fatalf("counting audits: %v", err)
	}
	if audits != 0 {
		t.Fatalf("dry run wrote %d audit events", audits)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()

	first, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true))
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if first.Counts.Create == 0 || first.Counts.Update != 0 {
		t.Fatalf("first apply counts = %+v, want creates only", first.Counts)
	}
	afterFirst := counts(t, pool)

	second, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true))
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	// The manifest carries no database identifier, so a repeated import resolves
	// the same rows through natural keys instead of inserting new ones.
	if second.Counts.Create != 0 || second.Counts.Update != 0 {
		t.Fatalf("second apply counts = %+v, want no creates and no updates", second.Counts)
	}
	if second.Counts.Noop != first.Counts.Create {
		t.Fatalf("second apply noop = %d, want %d", second.Counts.Noop, first.Counts.Create)
	}
	afterSecond := counts(t, pool)
	for table, n := range afterFirst {
		if afterSecond[table] != n {
			t.Fatalf("%s changed from %d to %d on a repeated import", table, n, afterSecond[table])
		}
	}
}

func TestSharedSubjectIsOneRowAcrossPrograms(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()
	if _, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// The release-blocking invariant: 0410-101 Calculus I is required by all five
	// launch programmes — Computer Science, Cybersecurity, Data Science and AI,
	// Computer Engineering, and Electrical Engineering — and must exist once.
	var rows, mappings, programs int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM subjects WHERE code_normalized = '0410101'`).Scan(&rows); err != nil {
		t.Fatalf("counting Calculus rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("0410-101 exists %d times; it must be one canonical Subject", rows)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT c.program_id)
		FROM curriculum_subjects cs
		JOIN subjects s ON s.id = cs.subject_id
		JOIN curricula c ON c.id = cs.curriculum_id
		WHERE s.code_normalized = '0410101'`).Scan(&mappings, &programs); err != nil {
		t.Fatalf("counting Calculus mappings: %v", err)
	}
	if mappings != 5 || programs != 5 {
		t.Fatalf("0410-101 has %d mappings across %d programs, want 5 and 5", mappings, programs)
	}

	// And no Institution holds two Subjects with the same canonical code.
	var collisions int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT institution_id, code_normalized FROM subjects
			WHERE code_normalized IS NOT NULL
			GROUP BY 1, 2 HAVING count(*) > 1
		) duplicated`).Scan(&collisions); err != nil {
		t.Fatalf("checking code collisions: %v", err)
	}
	if collisions != 0 {
		t.Fatalf("%d canonical Subject codes are duplicated", collisions)
	}
}

func TestImportRecordsSystemActorAudit(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()
	if _, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	var total, systemRows, nullAccounts int
	if err := pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE actor_role = 'SYSTEM'),
		       count(*) FILTER (WHERE actor_account_id IS NULL)
		FROM audit_events WHERE action LIKE 'ACADEMIC_%'`).
		Scan(&total, &systemRows, &nullAccounts); err != nil {
		t.Fatalf("counting import audits: %v", err)
	}
	if total == 0 {
		t.Fatal("the importer bypassed audit entirely")
	}
	// No fabricated Admin: the CLI has no human operator, so the audit must say
	// SYSTEM and leave actor_account_id NULL.
	if systemRows != total || nullAccounts != total {
		t.Fatalf("import audits: %d total, %d SYSTEM, %d with a NULL account", total, systemRows, nullAccounts)
	}
	var descriptor string
	if err := pool.QueryRow(ctx, `
		SELECT DISTINCT actor_descriptor FROM audit_events WHERE action LIKE 'ACADEMIC_%'`).
		Scan(&descriptor); err != nil {
		t.Fatalf("reading import descriptor: %v", err)
	}
	if !strings.HasPrefix(descriptor, "system:") {
		t.Fatalf("import audit descriptor = %q, want a system principal", descriptor)
	}
}

func TestSafeMetadataUpdateIsApplied(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()
	if _, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	pkg := launchPackage(t)
	for index := range pkg.Manifest.Subjects {
		if pkg.Manifest.Subjects[index].Key == "ku-0410-101" {
			pkg.Manifest.Subjects[index].TitleEn = "Calculus I (revised title)"
		}
	}
	plan, err := catalogImporter.Run(ctx, pkg, systemOptions(true))
	if err != nil {
		t.Fatalf("metadata update: %v", err)
	}
	if plan.Counts.Update != 1 {
		t.Fatalf("update count = %d, want exactly 1", plan.Counts.Update)
	}
	var title string
	if err := pool.QueryRow(ctx,
		`SELECT title_en FROM subjects WHERE code_normalized = '0410101'`).Scan(&title); err != nil {
		t.Fatalf("re-reading subject: %v", err)
	}
	if title != "Calculus I (revised title)" {
		t.Fatalf("title = %q, want the updated display metadata", title)
	}
}

func TestOfficialCodeReformattingKeepsOneSubject(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()
	if _, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Re-formatting a code is a display change while normalized identity holds.
	pkg := launchPackage(t)
	for index := range pkg.Manifest.Subjects {
		if pkg.Manifest.Subjects[index].Key == "ku-0410-101" {
			pkg.Manifest.Subjects[index].OfficialCode = "0410 101"
		}
	}
	if _, err := catalogImporter.Run(ctx, pkg, systemOptions(true)); err != nil {
		t.Fatalf("reformat apply: %v", err)
	}
	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM subjects WHERE code_normalized = '0410101'`).Scan(&rows); err != nil {
		t.Fatalf("counting after reformat: %v", err)
	}
	if rows != 1 {
		t.Fatalf("reformatting the display code produced %d rows", rows)
	}
}

func TestIdentityChangingUpdateFailsClosed(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()
	if _, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	before := counts(t, pool)

	// Re-parenting Computer Engineering under the College of Science would move
	// every Program and Subject beneath it. That needs a human decision.
	pkg := launchPackage(t)
	for index := range pkg.Manifest.Programs {
		if pkg.Manifest.Programs[index].Key == "ku-computer-engineering" {
			pkg.Manifest.Programs[index].OwningUnit = "ku-computer-science-dept"
		}
	}
	_, err := catalogImporter.Run(ctx, pkg, systemOptions(true))
	if !errors.Is(err, ErrIdentityRebind) {
		t.Fatalf("identity rebind error = %v, want ErrIdentityRebind", err)
	}
	// Fail closed means the whole import unwinds, not that half of it lands.
	for table, n := range counts(t, pool) {
		if n != before[table] {
			t.Fatalf("%s changed from %d to %d during a refused import", table, before[table], n)
		}
	}
}

func TestFailureMidImportRollsBackEverything(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()

	// A mapping that points at a curriculum the manifest never declares fails
	// after institutions, units, programs, and subjects have already been
	// inserted inside the transaction.
	pkg := launchPackage(t)
	pkg.Manifest.Mappings = append(pkg.Manifest.Mappings, manifest.Mapping{
		CurriculumKey: "ku-cs-2024", SubjectKey: "ku-does-not-exist",
		Requirement: "MAJOR_CORE", Sources: []string{"ku-cs-major-2024"},
	})
	if _, err := catalogImporter.Run(ctx, pkg, systemOptions(true)); err == nil {
		t.Fatal("an import referencing an undeclared subject succeeded")
	}
	for table, n := range counts(t, pool) {
		if n != 0 {
			t.Fatalf("a failed import left %d rows in %s; Kuwait University was half-imported", n, table)
		}
	}
}

func TestAbsenceFromManifestNeverRetires(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()
	if _, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	pkg := launchPackage(t)
	// Drop a Subject and every mapping that referenced it.
	kept := pkg.Manifest.Subjects[:0]
	for _, subject := range pkg.Manifest.Subjects {
		if subject.Key != "ku-0430-107" {
			kept = append(kept, subject)
		}
	}
	pkg.Manifest.Subjects = kept
	keptMappings := pkg.Manifest.Mappings[:0]
	for _, mapping := range pkg.Manifest.Mappings {
		if mapping.SubjectKey != "ku-0430-107" {
			keptMappings = append(keptMappings, mapping)
		}
	}
	pkg.Manifest.Mappings = keptMappings

	plan, err := catalogImporter.Run(ctx, pkg, systemOptions(true))
	if err != nil {
		t.Fatalf("apply after removal: %v", err)
	}
	if plan.Counts.Drift == 0 {
		t.Fatal("the removed Subject was not reported as drift")
	}
	// Reported, not retired. Academic data is never deleted by omission.
	var retired *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT retired_at FROM subjects WHERE code_normalized = '0430107'`).Scan(&retired); err != nil {
		t.Fatalf("re-reading the omitted subject: %v", err)
	}
	if retired != nil {
		t.Fatal("omitting a Subject from the manifest retired it; absence is not deletion")
	}
}

func TestOnlyOneActiveCurriculumPerProgramSurvivesImport(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()
	if _, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	var offenders int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT program_id FROM curricula WHERE status = 'ACTIVE' AND retired_at IS NULL
			GROUP BY program_id HAVING count(*) > 1
		) multiple`).Scan(&offenders); err != nil {
		t.Fatalf("checking active curricula: %v", err)
	}
	if offenders != 0 {
		t.Fatalf("%d programs hold more than one ACTIVE curriculum", offenders)
	}
}

func TestConcurrentImportsDoNotDuplicate(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()

	const racers = 4
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for index := 0; index < racers; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			_, errs[slot] = catalogImporter.Run(ctx, launchPackage(t), systemOptions(true))
		}(index)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		// Losing a race is acceptable; corrupting state is not.
		t.Logf("one concurrent import returned: %v", err)
	}
	if succeeded == 0 {
		t.Fatalf("every concurrent import failed: %v", errs)
	}
	final := counts(t, pool)
	if final["institutions"] != 1 {
		t.Fatalf("concurrent imports produced %d institutions", final["institutions"])
	}
	pkg := launchPackage(t)
	if final["subjects"] != len(pkg.Manifest.Subjects) {
		t.Fatalf("concurrent imports produced %d subjects, want %d",
			final["subjects"], len(pkg.Manifest.Subjects))
	}
	if final["curriculum_subjects"] != len(pkg.Manifest.Mappings) {
		t.Fatalf("concurrent imports produced %d mappings, want %d",
			final["curriculum_subjects"], len(pkg.Manifest.Mappings))
	}
}

func TestImportedKuwaitUniversityDataIsSemanticallyCorrect(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()
	if _, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	var nameEn, country string
	var maxLevel int
	var foundation bool
	if err := pool.QueryRow(ctx, `
		SELECT name_en, country_code, max_academic_level, has_foundation_stage
		FROM institutions WHERE slug = 'kuwait-university'`).
		Scan(&nameEn, &country, &maxLevel, &foundation); err != nil {
		t.Fatalf("reading the institution: %v", err)
	}
	if nameEn != "Kuwait University" || country != "KW" {
		t.Fatalf("institution = %s/%s", nameEn, country)
	}
	if maxLevel != 5 {
		t.Fatalf("max_academic_level = %d, want the 5 credit-derived levels the Student Manual defines", maxLevel)
	}
	if foundation {
		t.Fatal("Kuwait University must not claim a foundation stage")
	}

	// Every Program sits under the College its official pages place it in.
	// Cybersecurity is conferred by the Computer Science department, so it sits
	// under the College of Science alongside Computer Science.
	expected := map[string]string{
		"computer-science": "College of Science",
		"cybersecurity":    "College of Science",
		"data-science-and-artificial-intelligence": "College of Life Sciences",
		"computer-engineering":                     "College of Engineering and Petroleum",
		"electrical-engineering":                   "College of Engineering and Petroleum",
	}
	for programSlug, wantCollege := range expected {
		var college string
		if err := pool.QueryRow(ctx, `
			SELECT parent.name_en FROM programs p
			JOIN academic_units department ON department.id = p.owning_unit_id
			JOIN academic_units parent ON parent.id = department.parent_unit_id
			WHERE p.slug = $1`, programSlug).Scan(&college); err != nil {
			t.Fatalf("resolving the college for %s: %v", programSlug, err)
		}
		if college != wantCollege {
			t.Fatalf("%s sits under %q, want %q", programSlug, college, wantCollege)
		}
	}

	// No Gradex commercial category leaked in as academic structure. Cybersecurity
	// is deliberately absent from this list: Kuwait University confers a real
	// B.Sc. in Cybersecurity, so it is a Program rather than an invented label.
	for _, invented := range []string{"Software", "Software Engineering", "Data Science", "Programming"} {
		var found int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM academic_units WHERE name_en = $1`, invented).Scan(&found); err != nil {
			t.Fatalf("checking for invented unit %s: %v", invented, err)
		}
		if found != 0 {
			t.Fatalf("%q was imported as an academic unit; it is a Gradex topic, not a Kuwait University department", invented)
		}
		var programs int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM programs WHERE name_en = $1`, invented).Scan(&programs); err != nil {
			t.Fatalf("checking for invented program %s: %v", invented, err)
		}
		if programs != 0 {
			t.Fatalf("%q was imported as a degree Program with no Kuwait University evidence", invented)
		}
	}

	// Placement exists only where Kuwait University publishes a study plan: the
	// Computer Science 2024 Suggested Study Plan and the Data Science and AI
	// 8-Semester Plan. No other programme may carry one.
	var placedElsewhere int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM curriculum_subjects cs
		JOIN curricula c ON c.id = cs.curriculum_id
		JOIN programs p ON p.id = c.program_id
		WHERE (cs.recommended_level IS NOT NULL OR cs.recommended_semester IS NOT NULL)
		  AND p.slug NOT IN ('computer-science', 'data-science-and-artificial-intelligence')`).
		Scan(&placedElsewhere); err != nil {
		t.Fatalf("counting placement metadata: %v", err)
	}
	if placedElsewhere != 0 {
		t.Fatalf("%d mappings were placed with no Kuwait University placement source", placedElsewhere)
	}

	// The two plans sequence the same shared Subject differently, which is
	// precisely why placement lives on the mapping and not on the Subject.
	var csSemester, dsaiSemester int
	if err := pool.QueryRow(ctx, `
		SELECT cs.recommended_semester FROM curriculum_subjects cs
		JOIN subjects s ON s.id = cs.subject_id
		JOIN curricula c ON c.id = cs.curriculum_id
		JOIN programs p ON p.id = c.program_id
		WHERE s.code_normalized = '0410101' AND p.slug = 'computer-science'`).Scan(&csSemester); err != nil {
		t.Fatalf("reading the Computer Science placement: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT cs.recommended_semester FROM curriculum_subjects cs
		JOIN subjects s ON s.id = cs.subject_id
		JOIN curricula c ON c.id = cs.curriculum_id
		JOIN programs p ON p.id = c.program_id
		WHERE s.code_normalized = '0410101'
		  AND p.slug = 'data-science-and-artificial-intelligence'`).Scan(&dsaiSemester); err != nil {
		t.Fatalf("reading the Data Science placement: %v", err)
	}
	if csSemester != 1 || dsaiSemester != 2 {
		t.Fatalf("Calculus placement CS=%d DSAI=%d, want the plans' own 1 and 2", csSemester, dsaiSemester)
	}

	// The Data Science degree sits in its real college, and no invented
	// computing hierarchy was created to host it.
	var dsaiUnits int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM academic_units
		WHERE name_en IN ('Data Science', 'Artificial Intelligence', 'Computing', 'AI')`).
		Scan(&dsaiUnits); err != nil {
		t.Fatalf("checking for invented units: %v", err)
	}
	if dsaiUnits != 0 {
		t.Fatalf("%d invented computing units were imported", dsaiUnits)
	}

	// Founder Decision 2: Mathematics majors stay out of launch scope, while the
	// Mathematics department that owns the shared Subjects stays in.
	var mathPrograms, mathDepartment int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM programs WHERE name_en IN ('Mathematics', 'Financial Mathematics')),
		       (SELECT count(*) FROM academic_units WHERE name_en = 'Mathematics')`).
		Scan(&mathPrograms, &mathDepartment); err != nil {
		t.Fatalf("checking Mathematics scope: %v", err)
	}
	if mathPrograms != 0 {
		t.Fatalf("%d Mathematics degree Programs were imported; they are out of launch scope", mathPrograms)
	}
	if mathDepartment != 1 {
		t.Fatal("the Mathematics department must remain: it owns the shared Math Subjects")
	}
	// And the placement that does exist is real: Calculus I is a Freshman Fall
	// course on the official plan.
	var level, semester int
	if err := pool.QueryRow(ctx, `
		SELECT cs.recommended_level, cs.recommended_semester
		FROM curriculum_subjects cs
		JOIN subjects s ON s.id = cs.subject_id
		JOIN curricula c ON c.id = cs.curriculum_id
		WHERE s.code_normalized = '0410101' AND c.version_label = '2024'`).Scan(&level, &semester); err != nil {
		t.Fatalf("reading the Calculus I placement: %v", err)
	}
	if level != 1 || semester != 1 {
		t.Fatalf("Calculus I placed at level %d semester %d, want the plan's Freshman Fall (1, 1)", level, semester)
	}

	// Official display formatting survives the round trip.
	var code string
	if err := pool.QueryRow(ctx,
		`SELECT official_code FROM subjects WHERE code_normalized = '0410101'`).Scan(&code); err != nil {
		t.Fatalf("reading the official code: %v", err)
	}
	if code != "0410-101" {
		t.Fatalf("stored official code = %q, want the dashed display form", code)
	}
}

func TestImporterDoesNotTouchCourseOrLegacyTaxonomy(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	ctx := context.Background()

	var termID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO taxonomy_terms (kind, label_ar, label_en, academic_code)
		VALUES ('SUBJECT', 'تفاضل', 'Calculus', 'LEGACY-T2') RETURNING id::text`).Scan(&termID); err != nil {
		t.Fatalf("seeding legacy taxonomy: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('t2@example.test', 't2@example.test', 'INSTRUCTOR', 'ACTIVE', 'T2')
		RETURNING id::text`).Scan(&accountID); err != nil {
		t.Fatalf("seeding account: %v", err)
	}
	var courseID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO courses (owner_account_id, lifecycle) VALUES ($1::uuid, 'DRAFT') RETURNING id::text`,
		accountID).Scan(&courseID); err != nil {
		t.Fatalf("seeding course: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_revisions (course_id, revision_number, title_ar, title_en, subject_term_id, study_year)
		VALUES ($1::uuid, 1, 'دورة', 'Course', $2::uuid, 'YEAR_1')`, courseID, termID); err != nil {
		t.Fatalf("seeding revision: %v", err)
	}

	if _, err := catalogImporter.Run(ctx, launchPackage(t), systemOptions(true)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	var storedTerm, storedYear string
	if err := pool.QueryRow(ctx, `
		SELECT subject_term_id::text, study_year::text FROM course_revisions WHERE course_id = $1::uuid`,
		courseID).Scan(&storedTerm, &storedYear); err != nil {
		t.Fatalf("re-reading the revision: %v", err)
	}
	if storedTerm != termID || storedYear != "YEAR_1" {
		t.Fatalf("the importer mutated legacy Course classification: %s/%s", storedTerm, storedYear)
	}
	var terms int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM taxonomy_terms`).Scan(&terms); err != nil {
		t.Fatalf("counting legacy terms: %v", err)
	}
	if terms != 1 {
		t.Fatalf("legacy taxonomy term count = %d, want 1", terms)
	}
}

func TestImporterRefusesAnUnvalidatedManifest(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)

	pkg := launchPackage(t)
	// A dangling citation is a curation defect the importer must not paper over.
	pkg.Manifest.Subjects[0].Sources = []string{"no-such-source"}
	if _, err := catalogImporter.Run(context.Background(), pkg, systemOptions(true)); err == nil {
		t.Fatal("the importer applied a manifest that fails validation")
	}
	for table, n := range counts(t, pool) {
		if n != 0 {
			t.Fatalf("a refused manifest wrote %d rows into %s", n, table)
		}
	}
}

func TestImporterRequiresAnActor(t *testing.T) {
	pool := freshImporterDatabase(t)
	catalogImporter := newImporter(t, pool)
	_, err := catalogImporter.Run(context.Background(), launchPackage(t), Options{Apply: true})
	if err == nil {
		t.Fatal("the importer ran without an audited actor")
	}
	if !strings.Contains(fmt.Sprint(err), "Admin") && !errors.Is(err, academic.ErrAdminRequired) {
		t.Fatalf("actorless import error = %v, want an authorization refusal", err)
	}
}
