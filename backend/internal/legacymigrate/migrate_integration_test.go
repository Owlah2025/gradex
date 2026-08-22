//go:build integration

package legacymigrate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// T5 — legacy taxonomy migration, against real PostgreSQL.
//
// Every case here builds a real legacy Course through the same schema the
// product uses and then runs the real planner. Nothing is mocked: the outcomes
// under test are database outcomes, and the constraints that make the cutover
// safe (0025's CHECKs, the Subject immutability trigger) only exist there.

const (
	adminDSN   = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	testDBName = "gradex_legacymigrate_test"
	testDSN    = "postgres://gradex:gradex@localhost:5432/" + testDBName + "?sslmode=disable"
	sourceURL  = "file://../db/migrations"
)

type fixture struct {
	pool          *pgxpool.Pool
	ctx           context.Context
	institutionID string
	instructorID  string
	programID     string
	subjectID     string // canonical 0418-320, active
	retiredID     string // canonical 0418-999, retired
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Skipf("PostgreSQL is unavailable: %v", err)
	}
	_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, testDBName)
	_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+testDBName)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+testDBName); err != nil {
		t.Fatalf("creating test database: %v", err)
	}
	admin.Close()

	m, err := migrate.New(sourceURL, testDSN)
	if err != nil {
		t.Fatalf("opening migrations: %v", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating: %v", err)
	}
	_, _ = m.Close()

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)

	f := &fixture{pool: pool, ctx: ctx}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	scan := func(dest *string, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}

	scan(&f.instructorID, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('t5@example.test', 't5@example.test', 'INSTRUCTOR', 'ACTIVE', 'T5 Instructor')
		RETURNING id::text`)
	scan(&f.institutionID, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en)
		VALUES ('KW', 'kuwait-university', 'جامعة الكويت', 'Kuwait University') RETURNING id::text`)
	scan(&f.programID, `
		INSERT INTO programs (institution_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1::uuid, 'computer-science', 'علوم الحاسوب', 'Computer Science', 'BSC')
		RETURNING id::text`, f.institutionID)
	scan(&f.subjectID, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1::uuid, '0418-320', 'مبادئ', 'Principles of Computer Systems') RETURNING id::text`, f.institutionID)
	scan(&f.retiredID, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en, retired_at)
		VALUES ($1::uuid, '0418-999', 'متقاعدة', 'Retired Subject', now()) RETURNING id::text`, f.institutionID)
	exec(`INSERT INTO curricula (id, program_id, institution_id, version_label, status)
	      VALUES (gen_random_uuid(), $1::uuid, $2::uuid, '2024', 'ACTIVE')`, f.programID, f.institutionID)
	return f
}

// legacyTerm creates a legacy SUBJECT or MAJOR vocabulary row.
func (f *fixture) legacyTerm(t *testing.T, kind, labelEn, code string) string {
	t.Helper()
	var id string
	var codeArg any
	if code != "" {
		codeArg = code
	}
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO taxonomy_terms (kind, label_ar, label_en, academic_code)
		VALUES ($1, $2, $2, $3) RETURNING id::text`, kind, labelEn, codeArg).Scan(&id); err != nil {
		t.Fatalf("seeding legacy term: %v", err)
	}
	return id
}

// legacyCourseFixture builds a real LEGACY_TAXONOMY Course with one revision.
func (f *fixture) legacyCourseFixture(t *testing.T, titleEn string, subjectTerm, majorTerm *string) (string, string) {
	t.Helper()
	var courseID, revisionID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO courses (owner_account_id, lifecycle, classification_model)
		VALUES ($1::uuid, 'DRAFT', 'LEGACY_TAXONOMY') RETURNING id::text`, f.instructorID).Scan(&courseID); err != nil {
		t.Fatalf("seeding legacy course: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en,
		                              subject_term_id, major_term_id, study_year)
		VALUES ($1::uuid, 'DRAFT', 1, $2, $2, $3::uuid, $4::uuid, 'YEAR_2') RETURNING id::text`,
		courseID, titleEn, subjectTerm, majorTerm).Scan(&revisionID); err != nil {
		t.Fatalf("seeding legacy revision: %v", err)
	}
	return courseID, revisionID
}

func (f *fixture) classification(t *testing.T, courseID string) (string, *string, *string) {
	t.Helper()
	var model string
	var institution, subject *string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT classification_model::text, institution_id::text, subject_id::text
		FROM courses WHERE id = $1::uuid`, courseID).Scan(&model, &institution, &subject); err != nil {
		t.Fatalf("reading classification: %v", err)
	}
	return model, institution, subject
}

func mappingFor(subjects []SubjectMapping, majors []MajorMapping) *Mapping {
	return &Mapping{
		ID: "t5-test", Version: "1.0.0", InstitutionSlug: "kuwait-university",
		Subjects: subjects, Majors: majors,
	}
}

func outcomeFor(plan *Plan, courseID string) (Outcome, string) {
	for _, step := range plan.Steps {
		if step.CourseID == courseID {
			return step.Outcome, step.Detail
		}
	}
	return "", "no step"
}

// --- The outcome matrix ---------------------------------------------------

func TestT5PlannerClassifiesEveryLegacyOutcome(t *testing.T) {
	f := newFixture(t)

	coded := f.legacyTerm(t, "SUBJECT", "Principles", "0418-320")
	unmappedTerm := f.legacyTerm(t, "SUBJECT", "Unmapped Subject", "9999-999")
	codeless := f.legacyTerm(t, "SUBJECT", "Codeless Subject", "")
	retiredTerm := f.legacyTerm(t, "SUBJECT", "Retired", "0418-999")
	absentTerm := f.legacyTerm(t, "SUBJECT", "Absent", "0000-000")
	major := f.legacyTerm(t, "MAJOR", "Computer Science", "")

	exact, _ := f.legacyCourseFixture(t, "Exact", &coded, &major)
	unmapped, _ := f.legacyCourseFixture(t, "Unmapped", &unmappedTerm, nil)
	noCode, _ := f.legacyCourseFixture(t, "Codeless", &codeless, nil)
	noSubject, _ := f.legacyCourseFixture(t, "No Subject", nil, &major)
	retired, _ := f.legacyCourseFixture(t, "Retired", &retiredTerm, nil)
	absent, _ := f.legacyCourseFixture(t, "Absent", &absentTerm, nil)

	// A Course whose revisions disagree about their legacy Subject.
	ambiguous, _ := f.legacyCourseFixture(t, "Ambiguous", &coded, nil)
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE course_revisions SET state = 'SUPERSEDED' WHERE course_id = $1::uuid`, ambiguous); err != nil {
		t.Fatalf("superseding first revision: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en, subject_term_id)
		VALUES ($1::uuid, 'DRAFT', 2, 'ثان', 'Second', $2::uuid)`, ambiguous, unmappedTerm); err != nil {
		t.Fatalf("seeding second revision: %v", err)
	}

	mapping := mappingFor([]SubjectMapping{
		{TermCode: "0418-320", TermLabelEn: "Principles", SubjectCode: "0418-320"},
		{TermCode: "0418-999", TermLabelEn: "Retired", SubjectCode: "0418-999"},
		{TermCode: "0000-000", TermLabelEn: "Absent", SubjectCode: "1111-111"},
	}, []MajorMapping{{TermLabelEn: "Computer Science", ProgramSlugs: []string{"computer-science"}}})

	migrator, err := New(f.pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plan, err := migrator.Run(f.ctx, mapping, Options{})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if plan.Applied {
		t.Fatalf("a report must never report itself as applied")
	}

	for _, tc := range []struct {
		name     string
		courseID string
		want     Outcome
	}{
		{"exact translation migrates", exact, OutcomeMigrate},
		{"unmapped legacy code", unmapped, OutcomeUnmapped},
		{"legacy term without a code", noCode, OutcomeUnmapped},
		{"no legacy Subject at all", noSubject, OutcomeUnmapped},
		{"mapped Subject is retired", retired, OutcomeIneligible},
		{"mapped Subject absent from the Institution", absent, OutcomeIneligible},
		{"revisions disagree about the Subject", ambiguous, OutcomeAmbiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, detail := outcomeFor(plan, tc.courseID)
			if got != tc.want {
				t.Fatalf("outcome = %s (%s), want %s", got, detail, tc.want)
			}
		})
	}

	// A report writes nothing: every Course is still legacy afterwards.
	for _, id := range []string{exact, unmapped, noCode, noSubject, retired, absent, ambiguous} {
		if model, institution, subject := f.classification(t, id); model != "LEGACY_TAXONOMY" ||
			institution != nil || subject != nil {
			t.Fatalf("the report mutated course %s: model=%s institution=%v subject=%v",
				id, model, institution, subject)
		}
	}
}

// --- Apply, idempotency, and identity preservation ------------------------

func TestT5ApplyMigratesOnlyExactCoursesAndIsIdempotent(t *testing.T) {
	f := newFixture(t)
	coded := f.legacyTerm(t, "SUBJECT", "Principles", "0418-320")
	unmappedTerm := f.legacyTerm(t, "SUBJECT", "Unmapped", "9999-999")
	major := f.legacyTerm(t, "MAJOR", "Computer Science", "")

	exact, exactRevision := f.legacyCourseFixture(t, "Exact", &coded, &major)
	untouched, _ := f.legacyCourseFixture(t, "Unmapped", &unmappedTerm, nil)

	// Give the exact Course published history and a purchase-adjacent record, so
	// the run has to preserve identity rather than merely classification.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, exactRevision); err != nil {
		t.Fatalf("approving revision: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`,
		exactRevision, exact); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	mapping := mappingFor(
		[]SubjectMapping{{TermCode: "0418-320", SubjectCode: "0418-320"}},
		[]MajorMapping{{TermLabelEn: "Computer Science", ProgramSlugs: []string{"computer-science"}}},
	)
	migrator, _ := New(f.pool)

	plan, err := migrator.Run(f.ctx, mapping, Options{Apply: true, ActorDescriptor: "t5-test"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !plan.Applied || plan.Counts.Migrate != 1 {
		t.Fatalf("apply plan = %+v, want one migrated Course", plan.Counts)
	}

	// The migrated Course is Academic and carries canonical identity.
	model, institution, subject := f.classification(t, exact)
	if model != "ACADEMIC_CATALOG" || institution == nil || *institution != f.institutionID ||
		subject == nil || *subject != f.subjectID {
		t.Fatalf("migrated course: model=%s institution=%v subject=%v", model, institution, subject)
	}

	// It is the SAME Course and the SAME revision: identity was never replaced.
	var liveRevision, lifecycle string
	var revisionCount int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT c.live_revision_id::text, c.lifecycle::text,
		       (SELECT count(*) FROM course_revisions WHERE course_id = c.id)
		FROM courses c WHERE c.id = $1::uuid`, exact).Scan(&liveRevision, &lifecycle, &revisionCount); err != nil {
		t.Fatalf("re-reading course: %v", err)
	}
	if liveRevision != exactRevision || lifecycle != "PUBLISHED" || revisionCount != 1 {
		t.Fatalf("identity drifted: live=%s lifecycle=%s revisions=%d", liveRevision, lifecycle, revisionCount)
	}

	// The legacy revision columns are untouched — this is a migration, not an
	// erasure, and revision history stays readable.
	var legacySubject, legacyMajor, studyYear *string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT subject_term_id::text, major_term_id::text, study_year::text
		FROM course_revisions WHERE id = $1::uuid`, exactRevision).Scan(&legacySubject, &legacyMajor, &studyYear); err != nil {
		t.Fatalf("reading legacy columns: %v", err)
	}
	if legacySubject == nil || legacyMajor == nil || studyYear == nil {
		t.Fatalf("the migration erased legacy revision history")
	}

	// The mapped Major became revision-scoped audience metadata.
	var targets int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM course_program_targets WHERE revision_id = $1::uuid`, exactRevision).Scan(&targets); err != nil {
		t.Fatalf("counting targets: %v", err)
	}
	if targets != 1 {
		t.Fatalf("audience targets = %d, want 1 from the mapped Major", targets)
	}

	// The unmapped Course was left alone entirely.
	if model, institution, subject := f.classification(t, untouched); model != "LEGACY_TAXONOMY" ||
		institution != nil || subject != nil {
		t.Fatalf("an unmapped Course was migrated: %s %v %v", model, institution, subject)
	}

	// Idempotency: a rerun migrates nothing more and reports the Course as
	// already Academic rather than silently ignoring it.
	rerun, err := migrator.Run(f.ctx, mapping, Options{Apply: true, ActorDescriptor: "t5-test"})
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if rerun.Counts.Migrate != 0 {
		t.Fatalf("rerun migrated %d Courses; the run is not idempotent", rerun.Counts.Migrate)
	}
	if rerun.Counts.AlreadyAcademic != 1 {
		t.Fatalf("rerun already-academic = %d, want 1", rerun.Counts.AlreadyAcademic)
	}
	if rerun.Counts.Unmapped != 1 {
		t.Fatalf("rerun still had to report the unmapped Course; got %d", rerun.Counts.Unmapped)
	}

	// And the audit records the cutover.
	var audits int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM audit_events
		WHERE action = 'COURSE_TAXONOMY_MIGRATED' AND target_id = $1`, exact).Scan(&audits); err != nil {
		t.Fatalf("counting audits: %v", err)
	}
	if audits != 1 {
		t.Fatalf("migration audit rows = %d, want exactly 1", audits)
	}
}

// A migrated published Course immediately inherits the T4 Subject lock.
func TestT5MigratedPublishedCourseSubjectBecomesImmutable(t *testing.T) {
	f := newFixture(t)
	coded := f.legacyTerm(t, "SUBJECT", "Principles", "0418-320")
	course, revision := f.legacyCourseFixture(t, "Published", &coded, nil)
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, revision); err != nil {
		t.Fatalf("approving: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`,
		revision, course); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	migrator, _ := New(f.pool)
	if _, err := migrator.Run(f.ctx, mappingFor(
		[]SubjectMapping{{TermCode: "0418-320", SubjectCode: "0418-320"}}, nil,
	), Options{Apply: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// The cutover itself was permitted because a legacy Course has no Subject to
	// protect. From this moment the T4-A trigger owns the Subject.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE courses SET subject_id = $1::uuid WHERE id = $2::uuid`, f.retiredID, course); err == nil {
		t.Fatalf("a migrated published Course allowed its Subject to change")
	}
}

// The mapping file itself must fail closed rather than migrate ambiguously.
func TestT5MappingValidationRejectsAmbiguousTranslation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mapping *Mapping
	}{
		{"one legacy term mapped twice", mappingFor([]SubjectMapping{
			{TermCode: "0418-320", SubjectCode: "0418-320"},
			{TermCode: "0418320", SubjectCode: "0418-321"},
		}, nil)},
		{"entry with no subject code", mappingFor([]SubjectMapping{
			{TermCode: "0418-320", SubjectCode: ""},
		}, nil)},
		{"major mapped twice", mappingFor(nil, []MajorMapping{
			{TermLabelEn: "CS", ProgramSlugs: []string{"a"}},
			{TermLabelEn: "CS", ProgramSlugs: []string{"b"}},
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mapping.Validate(); err == nil {
				t.Fatalf("an ambiguous mapping was accepted")
			}
		})
	}
}

// The checked-in mapping must always load and validate.
func TestT5EmbeddedMappingLoads(t *testing.T) {
	available, err := Available()
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if len(available) == 0 {
		t.Fatalf("no embedded legacy mapping is checked in")
	}
	for _, id := range available {
		mapping, err := Load(id)
		if err != nil {
			t.Fatalf("loading %s: %v", id, err)
		}
		if mapping.InstitutionSlug == "" {
			t.Fatalf("mapping %s has no institution", id)
		}
	}
	if _, err := Load("../../etc/passwd"); err == nil {
		t.Fatalf("a traversal identifier resolved")
	}
	if _, err := Load("no-such-mapping"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("an unknown mapping did not fail cleanly: %v", err)
	}
}

// A mapping naming an Institution the catalog does not have fails closed.
func TestT5UnknownInstitutionFailsClosed(t *testing.T) {
	f := newFixture(t)
	migrator, _ := New(f.pool)
	_, err := migrator.Run(f.ctx, &Mapping{
		ID: "bad", InstitutionSlug: "no-such-university",
	}, Options{Apply: true})
	if err == nil || !strings.Contains(err.Error(), "not in the Academic Catalog") {
		t.Fatalf("unknown institution error = %v", err)
	}
}
