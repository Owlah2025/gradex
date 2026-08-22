//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
)

// 0023 is the D-091 T1 Academic Catalog Foundation migration. Every assertion
// here exists because the corresponding invariant is load-bearing for a later
// tranche: if the schema does not refuse these on its own, application code
// becomes the only guard and the guarantee is not real.

func TestAcademicCatalogMigrationIsAdditiveAndReversible(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	// Stops at 0023 rather than at HEAD, because the assertions below describe
	// what T1 itself does and does not add. T4-A's 0025 legitimately adds
	// courses.subject_id, so running to HEAD would test the wrong migration.
	if err := m.Migrate(uint(AcademicCatalogSchemaVersion)); err != nil {
		t.Fatalf("migrating up: %v", err)
	}
	pool := openPool(t)
	ctx := context.Background()

	for _, table := range []string{
		"institutions", "academic_units", "programs", "curricula", "subjects", "curriculum_subjects",
	} {
		if !tableExists(t, pool, table) {
			t.Fatalf("0023 did not create %s", table)
		}
	}

	// The legacy taxonomy is untouched: T1 is additive and the old model stays
	// authoritative for Courses until the T5 cutover.
	for _, table := range []string{"taxonomy_terms", "courses", "course_revisions"} {
		if !tableExists(t, pool, table) {
			t.Fatalf("0023 removed pre-existing table %s", table)
		}
	}
	for _, column := range []string{"major_term_id", "subject_term_id", "study_year"} {
		var present bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_name = 'course_revisions' AND column_name = $1)`, column).Scan(&present); err != nil {
			t.Fatalf("checking legacy column %s: %v", column, err)
		}
		if !present {
			t.Fatalf("0023 removed the legacy course_revisions.%s column", column)
		}
	}

	// T1 deliberately does not add courses.subject_id. That column belongs to
	// T4/T5 and adding it early would put an unused column in the Course write
	// path. T4-A's migration 0025 is what introduces it, which the T4-A
	// migration proof covers.
	var subjectOnCourse bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_name = 'courses' AND column_name = 'subject_id')`).Scan(&subjectOnCourse); err != nil {
		t.Fatalf("checking courses.subject_id: %v", err)
	}
	if subjectOnCourse {
		t.Fatal("0023 added courses.subject_id; that column belongs to T4/T5, not T1")
	}

	// down → up must restore exactly, so a T1 rollback is genuinely cheap.
	// Targeted by version rather than by "one step from the top", so a later
	// additive migration cannot silently redirect this at the wrong one.
	if err := m.Migrate(uint(RevisionScopedPreviewSchemaVersion)); err != nil {
		t.Fatalf("rolling 0023 back: %v", err)
	}
	for _, table := range []string{
		"institutions", "academic_units", "programs", "curricula", "subjects", "curriculum_subjects",
	} {
		if tableExists(t, pool, table) {
			t.Fatalf("0023 down left %s behind", table)
		}
	}
	// 0011 owns catalog_normalize_ar and the existing catalogue search path
	// still uses it, so 0023 down must not drop it.
	var normalizeArPresent bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'catalog_normalize_ar')`).Scan(&normalizeArPresent); err != nil {
		t.Fatalf("checking catalog_normalize_ar: %v", err)
	}
	if !normalizeArPresent {
		t.Fatal("0023 down dropped catalog_normalize_ar, which 0011 owns")
	}
	var normalizeCodePresent bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'academic_normalize_code')`).Scan(&normalizeCodePresent); err != nil {
		t.Fatalf("checking academic_normalize_code: %v", err)
	}
	if normalizeCodePresent {
		t.Fatal("0023 down left its own academic_normalize_code function behind")
	}

	if err := m.Migrate(uint(AcademicCatalogSchemaVersion)); err != nil {
		t.Fatalf("re-applying 0023: %v", err)
	}
	if !tableExists(t, pool, "subjects") {
		t.Fatal("0023 up after down did not restore subjects")
	}
}

func TestAcademicCatalogMigrationPreservesExistingCourseData(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	// Stop one short of 0023 so real legacy rows exist before T1 lands.
	if err := m.Migrate(uint(RevisionScopedPreviewSchemaVersion)); err != nil {
		t.Fatalf("migrating to %d: %v", RevisionScopedPreviewSchemaVersion, err)
	}
	pool := openPool(t)
	ctx := context.Background()

	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('t1-owner@example.test', 't1-owner@example.test', 'INSTRUCTOR', 'ACTIVE', 'T1 Owner')
		RETURNING id::text`).Scan(&accountID); err != nil {
		t.Fatalf("seeding account: %v", err)
	}
	var termID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO taxonomy_terms (kind, label_ar, label_en, academic_code)
		VALUES ('SUBJECT', 'تفاضل', 'Calculus', 'LEGACY-1') RETURNING id::text`).Scan(&termID); err != nil {
		t.Fatalf("seeding taxonomy term: %v", err)
	}
	var courseID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO courses (owner_account_id, lifecycle) VALUES ($1::uuid, 'DRAFT') RETURNING id::text`,
		accountID).Scan(&courseID); err != nil {
		t.Fatalf("seeding course: %v", err)
	}
	var revisionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_revisions (course_id, revision_number, title_ar, title_en, subject_term_id, study_year)
		VALUES ($1::uuid, 1, 'دورة', 'Course', $2::uuid, 'YEAR_1') RETURNING id::text`,
		courseID, termID).Scan(&revisionID); err != nil {
		t.Fatalf("seeding course revision: %v", err)
	}

	if err := m.Steps(1); err != nil {
		t.Fatalf("applying 0023 over existing data: %v", err)
	}

	var storedTerm, storedYear string
	if err := pool.QueryRow(ctx, `
		SELECT subject_term_id::text, study_year::text FROM course_revisions WHERE id = $1::uuid`,
		revisionID).Scan(&storedTerm, &storedYear); err != nil {
		t.Fatalf("re-reading course revision after 0023: %v", err)
	}
	if storedTerm != termID || storedYear != "YEAR_1" {
		t.Fatalf("0023 mutated legacy classification: term %q year %q", storedTerm, storedYear)
	}

	// Rolling back with legacy data present must also be safe.
	if err := m.Steps(-1); err != nil {
		t.Fatalf("rolling 0023 back with legacy data present: %v", err)
	}
	var surviving int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM course_revisions WHERE id = $1::uuid`,
		revisionID).Scan(&surviving); err != nil {
		t.Fatalf("counting revisions after rollback: %v", err)
	}
	if surviving != 1 {
		t.Fatalf("0023 rollback destroyed course data: %d revisions remain", surviving)
	}
}

func TestAcademicCatalogSchemaInvariants(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("migrating up: %v", err)
	}
	pool := openPool(t)
	ctx := context.Background()

	newInstitution := func(slug string, maxLevel int) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO institutions (country_code, slug, name_ar, name_en, max_academic_level)
			VALUES ('KW', $1, $1, $1, $2) RETURNING id::text`, slug, maxLevel).Scan(&id); err != nil {
			t.Fatalf("seeding institution %s: %v", slug, err)
		}
		return id
	}
	alpha := newInstitution("alpha-university", 5)
	beta := newInstitution("beta-university", 4)

	newUnit := func(institution, slug string, parent *string) (string, error) {
		var id string
		err := pool.QueryRow(ctx, `
			INSERT INTO academic_units (institution_id, parent_unit_id, kind, slug, name_ar, name_en)
			VALUES ($1::uuid, $2::uuid, 'COLLEGE', $3, $3, $3) RETURNING id::text`,
			institution, parent, slug).Scan(&id)
		return id, err
	}

	alphaCollege, err := newUnit(alpha, "engineering", nil)
	if err != nil {
		t.Fatalf("creating root unit: %v", err)
	}
	betaCollege, err := newUnit(beta, "science", nil)
	if err != nil {
		t.Fatalf("creating beta unit: %v", err)
	}

	// Cross-institution parent must be structurally impossible.
	if _, err := newUnit(alpha, "cross-parent", &betaCollege); err == nil {
		t.Fatal("the schema accepted an academic unit parented across institutions")
	}

	// Self-parent.
	if _, err := pool.Exec(ctx,
		`UPDATE academic_units SET parent_unit_id = id WHERE id = $1::uuid`, alphaCollege); err == nil {
		t.Fatal("the schema accepted a self-parented academic unit")
	}

	// Multi-node cycle A -> B -> C -> A.
	unitB, err := newUnit(alpha, "unit-b", &alphaCollege)
	if err != nil {
		t.Fatalf("creating unit B: %v", err)
	}
	unitC, err := newUnit(alpha, "unit-c", &unitB)
	if err != nil {
		t.Fatalf("creating unit C: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE academic_units SET parent_unit_id = $1::uuid WHERE id = $2::uuid`, unitC, alphaCollege); err == nil {
		t.Fatal("the schema accepted a multi-node academic unit cycle")
	} else if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle rejection did not name the cycle: %v", err)
	}

	// Program owning unit must share the institution.
	var alphaProgram string
	if err := pool.QueryRow(ctx, `
		INSERT INTO programs (institution_id, owning_unit_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1::uuid, $2::uuid, 'computer-engineering', 'ح', 'CpE', 'BSC') RETURNING id::text`,
		alpha, alphaCollege).Scan(&alphaProgram); err != nil {
		t.Fatalf("creating program: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO programs (institution_id, owning_unit_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1::uuid, $2::uuid, 'cross-program', 'x', 'x', 'BSC')`, alpha, betaCollege); err == nil {
		t.Fatal("the schema accepted a program owned by a unit in another institution")
	}

	// Exactly one ACTIVE curriculum per program.
	var activeCurriculum string
	if err := pool.QueryRow(ctx, `
		INSERT INTO curricula (program_id, institution_id, version_label, status)
		VALUES ($1::uuid, $2::uuid, '2026', 'ACTIVE') RETURNING id::text`,
		alphaProgram, alpha).Scan(&activeCurriculum); err != nil {
		t.Fatalf("creating curriculum: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO curricula (program_id, institution_id, version_label, status)
		VALUES ($1::uuid, $2::uuid, '2027', 'ACTIVE')`, alphaProgram, alpha); err == nil {
		t.Fatal("the schema accepted a second ACTIVE curriculum for one program")
	}

	// Curriculum may not claim a program from another institution.
	if _, err := pool.Exec(ctx, `
		INSERT INTO curricula (program_id, institution_id, version_label, status)
		VALUES ($1::uuid, $2::uuid, '2026-cross', 'ACTIVE')`, alphaProgram, beta); err == nil {
		t.Fatal("the schema accepted a curriculum whose institution differs from its program")
	}

	newSubject := func(institution string, code *string, titleAr, titleEn string) (string, error) {
		var id string
		err := pool.QueryRow(ctx, `
			INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
			VALUES ($1::uuid, $2, $3, $4) RETURNING id::text`,
			institution, code, titleAr, titleEn).Scan(&id)
		return id, err
	}
	code := "0410-101"
	alphaCalculus, err := newSubject(alpha, &code, "حساب ١", "Calculus I")
	if err != nil {
		t.Fatalf("creating subject: %v", err)
	}

	// Same normalized code in the same institution is refused, whatever the
	// punctuation or spacing the Admin typed.
	for _, variant := range []string{"0410-101", "0410101", "0410 101", "0410--101"} {
		v := variant
		if _, err := newSubject(alpha, &v, "مختلف", "Different Title"); err == nil {
			t.Fatalf("the schema accepted duplicate normalized code %q", variant)
		}
	}
	// The same code in a different institution is a different Subject.
	betaCode := "0410-101"
	if _, err := newSubject(beta, &betaCode, "حساب ١", "Calculus I"); err != nil {
		t.Fatalf("the schema refused the same code in another institution: %v", err)
	}

	// Code-less Subjects dedupe per normalized title, in either language.
	if _, err := newSubject(alpha, nil, "مادة بلا رمز", "Codeless Subject"); err != nil {
		t.Fatalf("creating code-less subject: %v", err)
	}
	if _, err := newSubject(alpha, nil, "مادة بلا رمز", "Totally Different"); err == nil {
		t.Fatal("the schema accepted a duplicate code-less Arabic title")
	}
	if _, err := newSubject(alpha, nil, "عنوان مختلف", "Codeless Subject"); err == nil {
		t.Fatal("the schema accepted a duplicate code-less English title")
	}

	// Cross-institution curriculum mapping must be structurally impossible.
	if _, err := pool.Exec(ctx, `
		INSERT INTO curriculum_subjects (curriculum_id, subject_id, institution_id, requirement_kind)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'MAJOR_CORE')`,
		activeCurriculum, alphaCalculus, beta); err == nil {
		t.Fatal("the schema accepted a cross-institution curriculum mapping")
	}

	// recommended_level is bounded by the owning institution's maximum.
	if _, err := pool.Exec(ctx, `
		INSERT INTO curriculum_subjects (curriculum_id, subject_id, institution_id, requirement_kind, recommended_level)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'MAJOR_CORE', 9)`,
		activeCurriculum, alphaCalculus, alpha); err == nil {
		t.Fatal("the schema accepted a recommended level above the institution maximum")
	}
	// Level 5 is valid here because this institution declares five levels,
	// which is the real Kuwait University shape.
	if _, err := pool.Exec(ctx, `
		INSERT INTO curriculum_subjects (curriculum_id, subject_id, institution_id, requirement_kind, recommended_level)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'MAJOR_CORE', 5)`,
		activeCurriculum, alphaCalculus, alpha); err != nil {
		t.Fatalf("the schema refused a valid level-5 recommendation: %v", err)
	}
	// One Subject appears at most once per curriculum.
	if _, err := pool.Exec(ctx, `
		INSERT INTO curriculum_subjects (curriculum_id, subject_id, institution_id, requirement_kind)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'MAJOR_ELECTIVE')`,
		activeCurriculum, alphaCalculus, alpha); err == nil {
		t.Fatal("the schema accepted the same subject twice in one curriculum")
	}
}
