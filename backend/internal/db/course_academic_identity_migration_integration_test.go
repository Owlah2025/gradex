//go:build integration

package db

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// T4-A (MVP-F20) migration 0025 proof.
//
// The properties that matter are that 0025 is additive to every existing
// Course, that it is reversible, and that the one rule it TIGHTENS — official
// Subject code permanence, D-093 §7 — is genuinely enforced across active and
// retired rows.

// seedPreT4Course writes a published legacy Course with full revision-scoped
// taxonomy at schema 24, so the upgrade can be shown not to touch it.
func seedPreT4Course(t *testing.T) {
	t.Helper()
	pool, ctx := openPoolCtx(t)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding pre-T4 course: %v", err)
		}
	}
	exec(`INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
	      VALUES ('99990000-0000-0000-0000-000000000001', 'legacy@t4a.test', 'legacy@t4a.test',
	              'INSTRUCTOR', 'ACTIVE', 'Legacy Instructor')`)
	exec(`INSERT INTO taxonomy_terms (id, kind, label_ar, label_en, academic_code) VALUES
	      ('99990000-0000-0000-0000-000000000011', 'MAJOR', 'هندسة', 'Engineering', NULL),
	      ('99990000-0000-0000-0000-000000000012', 'SUBJECT', 'تفاضل', 'Calculus', 'MATH101')`)
	exec(`INSERT INTO courses (id, owner_account_id, lifecycle)
	      VALUES ('99990000-0000-0000-0000-000000000021', '99990000-0000-0000-0000-000000000001', 'DRAFT')`)
	exec(`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en,
	                                    major_term_id, subject_term_id, study_year)
	      VALUES ('99990000-0000-0000-0000-000000000031', '99990000-0000-0000-0000-000000000021',
	              'APPROVED', 1, 'عنوان', 'Legacy Title',
	              '99990000-0000-0000-0000-000000000011', '99990000-0000-0000-0000-000000000012', 'YEAR_2')`)
	exec(`UPDATE courses SET live_revision_id = '99990000-0000-0000-0000-000000000031', lifecycle = 'PUBLISHED'
	      WHERE id = '99990000-0000-0000-0000-000000000021'`)
}

// legacyCourseSnapshot is every fact about the seeded Course that 0025 must not
// change.
type legacyCourseSnapshot struct {
	lifecycle, liveRevision      string
	major, subject, studyYear    string
	revisionCount, taxonomyCount int
}

func readLegacySnapshot(t *testing.T) legacyCourseSnapshot {
	t.Helper()
	pool, ctx := openPoolCtx(t)
	var s legacyCourseSnapshot
	if err := pool.QueryRow(ctx, `
		SELECT c.lifecycle::text, c.live_revision_id::text,
		       r.major_term_id::text, r.subject_term_id::text, r.study_year::text,
		       (SELECT count(*) FROM course_revisions WHERE course_id = c.id),
		       (SELECT count(*) FROM taxonomy_terms)
		FROM courses c
		JOIN course_revisions r ON r.id = c.live_revision_id
		WHERE c.id = '99990000-0000-0000-0000-000000000021'`).Scan(
		&s.lifecycle, &s.liveRevision, &s.major, &s.subject, &s.studyYear,
		&s.revisionCount, &s.taxonomyCount); err != nil {
		t.Fatalf("reading legacy snapshot: %v", err)
	}
	return s
}

func TestCourseAcademicIdentityMigrationIsAdditiveAndReversible(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)

	// Clean install to the schema immediately before T4-A, then seed a real
	// legacy Course exactly as production holds one.
	if err := m.Migrate(uint(StudentAcademicProfileSchemaVersion)); err != nil {
		t.Fatalf("migrating to the pre-T4 schema: %v", err)
	}
	seedPreT4Course(t)
	before := readLegacySnapshot(t)

	// Up.
	if err := m.Migrate(uint(CourseAcademicIdentitySchemaVersion)); err != nil {
		t.Fatalf("migrating up to T4-A: %v", err)
	}

	after := readLegacySnapshot(t)
	if before != after {
		t.Fatalf("0025 changed existing Course data:\n before=%+v\n after =%+v", before, after)
	}

	pool, ctx := openPoolCtx(t)

	// The existing Course is classified legacy and carries no academic identity.
	var model string
	var institution, subject *string
	if err := pool.QueryRow(ctx, `
		SELECT classification_model::text, institution_id::text, subject_id::text
		FROM courses WHERE id = '99990000-0000-0000-0000-000000000021'`).Scan(
		&model, &institution, &subject); err != nil {
		t.Fatalf("reading classification: %v", err)
	}
	if model != "LEGACY_TAXONOMY" || institution != nil || subject != nil {
		t.Fatalf("existing Course became academic: model=%s institution=%v subject=%v", model, institution, subject)
	}

	// Down: every T4-A object disappears and nothing else does.
	if err := m.Migrate(uint(StudentAcademicProfileSchemaVersion)); err != nil {
		t.Fatalf("migrating T4-A down: %v", err)
	}

	for _, table := range []string{"course_program_targets", "subject_requests"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table).Scan(&exists); err != nil {
			t.Fatalf("checking %s: %v", table, err)
		}
		if exists {
			t.Fatalf("%s survived the T4-A rollback", table)
		}
	}
	for _, column := range []string{"classification_model", "institution_id", "subject_id"} {
		var exists bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
			               WHERE table_name = 'courses' AND column_name = $1)`, column).Scan(&exists); err != nil {
			t.Fatalf("checking courses.%s: %v", column, err)
		}
		if exists {
			t.Fatalf("courses.%s survived the T4-A rollback", column)
		}
	}

	// The prior tranches survive the rollback untouched.
	for _, table := range []string{"institutions", "subjects", "curricula", "curriculum_subjects", "student_academic_profiles"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table).Scan(&exists); err != nil {
			t.Fatalf("checking %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("T4-A rollback removed %s, which belongs to T1/T3", table)
		}
	}

	// And the legacy Course is still exactly itself.
	if rolled := readLegacySnapshot(t); rolled != before {
		t.Fatalf("rollback changed existing Course data:\n before=%+v\n after =%+v", before, rolled)
	}

	// Up again.
	if err := m.Migrate(uint(CourseAcademicIdentitySchemaVersion)); err != nil {
		t.Fatalf("re-applying T4-A: %v", err)
	}
	if again := readLegacySnapshot(t); again != before {
		t.Fatalf("re-applying 0025 changed existing Course data:\n before=%+v\n after =%+v", before, again)
	}
}

// D-093 §7. The Founder decision this migration implements: an official Subject
// code, once used, permanently identifies that Subject within its Institution.
func TestCourseAcademicIdentityMigrationMakesSubjectCodesPermanent(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("migrating up: %v", err)
	}
	pool, ctx := openPoolCtx(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO institutions (id, country_code, slug, name_ar, name_en) VALUES
		  ('88880000-0000-0000-0000-000000000001', 'KW', 'ku-codes', 'ج', 'KU'),
		  ('88880000-0000-0000-0000-000000000002', 'KW', 'auk-codes', 'ج2', 'AUK')`); err != nil {
		t.Fatalf("seeding institutions: %v", err)
	}
	insertSubject := func(institution, code, titleEn string) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
			VALUES ($1::uuid, $2, 'عنوان', $3)`, institution, code, titleEn)
		return err
	}

	// 21. A duplicate code among live Subjects is refused, as before.
	if err := insertSubject("88880000-0000-0000-0000-000000000001", "0418-320", "Principles"); err != nil {
		t.Fatalf("seeding first subject: %v", err)
	}
	if err := insertSubject("88880000-0000-0000-0000-000000000001", "0418320", "Duplicate Active"); err == nil {
		t.Fatalf("duplicate active normalized code must be refused")
	}

	// 22/23. Retiring the original does NOT release its code. This is the whole
	// point of the change: before 0025 this insert succeeded.
	if _, err := pool.Exec(ctx, `
		UPDATE subjects SET retired_at = now()
		WHERE institution_id = '88880000-0000-0000-0000-000000000001'::uuid AND code_normalized = '0418320'`); err != nil {
		t.Fatalf("retiring subject: %v", err)
	}
	if err := insertSubject("88880000-0000-0000-0000-000000000001", "0418-320", "Reuse After Retire"); err == nil {
		t.Fatalf("a retired Subject's official code must stay reserved (D-093 §7)")
	}

	// 24. The reservation is scoped to the Institution, never global.
	if err := insertSubject("88880000-0000-0000-0000-000000000002", "0418-320", "Other Institution"); err != nil {
		t.Fatalf("same code in a different Institution must be allowed: %v", err)
	}

	// 25. Display formatting stays independent of identity: the stored code
	// keeps the dashed form a Student would recognise while matching is
	// normalized.
	var stored string
	if err := pool.QueryRow(ctx, `
		SELECT official_code FROM subjects
		WHERE institution_id = '88880000-0000-0000-0000-000000000002'::uuid`).Scan(&stored); err != nil {
		t.Fatalf("reading stored code: %v", err)
	}
	if stored != "0418-320" {
		t.Fatalf("stored official_code = %q, want the dashed display form", stored)
	}

	// Codeless Subjects are deliberately NOT covered by this rule, so their
	// 0023 title-based identity is unchanged.
	if _, err := pool.Exec(ctx, `
		INSERT INTO subjects (institution_id, title_ar, title_en)
		VALUES ('88880000-0000-0000-0000-000000000001'::uuid, 'مواضيع', 'Special Topics')`); err != nil {
		t.Fatalf("seeding codeless subject: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE subjects SET retired_at = now()
		WHERE institution_id = '88880000-0000-0000-0000-000000000001'::uuid AND code_normalized IS NULL`); err != nil {
		t.Fatalf("retiring codeless subject: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO subjects (institution_id, title_ar, title_en)
		VALUES ('88880000-0000-0000-0000-000000000001'::uuid, 'مواضيع', 'Special Topics')`); err != nil {
		t.Fatalf("a retired codeless title must remain reusable under the unchanged 0023 rule: %v", err)
	}
}

// 26. The preflight the migration performs before tightening the index. It must
// name conflicting rows rather than fail with an opaque duplicate-key error,
// and it must never resolve a conflict by deleting or merging data.
func TestCourseAcademicIdentityMigrationRefusesPreexistingCodeConflicts(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(StudentAcademicProfileSchemaVersion)); err != nil {
		t.Fatalf("migrating to the pre-T4 schema: %v", err)
	}
	pool, ctx := openPoolCtx(t)

	// Manufacture exactly the state 0025 must refuse: one live and one retired
	// Subject sharing a normalized code. The 0023 index permits this.
	if _, err := pool.Exec(ctx, `
		INSERT INTO institutions (id, country_code, slug, name_ar, name_en)
		VALUES ('77770000-0000-0000-0000-000000000001', 'KW', 'ku-conflict', 'ج', 'KU')`); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO subjects (id, institution_id, official_code, title_ar, title_en, retired_at)
		VALUES ('77770000-0000-0000-0000-000000000011', '77770000-0000-0000-0000-000000000001',
		        '0418-320', 'أ', 'Retired Holder', now())`); err != nil {
		t.Fatalf("seeding retired subject: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO subjects (id, institution_id, official_code, title_ar, title_en)
		VALUES ('77770000-0000-0000-0000-000000000012', '77770000-0000-0000-0000-000000000001',
		        '0418320', 'ب', 'Live Reuser')`); err != nil {
		t.Fatalf("seeding live subject: %v", err)
	}

	err := m.Migrate(uint(CourseAcademicIdentitySchemaVersion))
	if err == nil {
		t.Fatalf("0025 must refuse to tighten the code index over conflicting data")
	}
	if !strings.Contains(err.Error(), "FOUNDER_DATA_RESOLUTION_REQUIRED") {
		t.Fatalf("0025 failure must name the required resolution, got: %v", err)
	}

	// Critically: neither Subject was deleted or merged to make the migration
	// pass. The data is exactly as the operator left it.
	var remaining int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM subjects WHERE institution_id = '77770000-0000-0000-0000-000000000001'::uuid`).Scan(&remaining); err != nil {
		t.Fatalf("counting subjects: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("the failed migration changed Subject data: %d rows remain, want 2", remaining)
	}
}

// openPoolCtx pairs the shared pool helper with a bounded context, which every
// query in this file needs.
func openPoolCtx(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	t.Cleanup(cancel)
	return openPool(t), ctx
}
