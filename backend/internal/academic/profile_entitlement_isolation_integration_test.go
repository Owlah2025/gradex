//go:build integration

package academic_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
)

// D-092 §1 release gate.
//
// The Student academic profile is discovery-only. This exercises the REAL
// production entitlement evaluator — the same decision point protected playback,
// progress writes, and protected downloads call — before and after every kind of
// profile mutation, and proves the decision is byte-for-byte identical.
//
// It deliberately does not grep for imports. An import check proves the code
// does not mention the profile today; this proves the decision does not move.

const (
	isolationDSN      = "postgres://gradex:gradex@localhost:5432/gradex_profile_isolation_test?sslmode=disable"
	isolationAdminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	isolationDBName   = "gradex_profile_isolation_test"
)

type isolationFixture struct {
	pool          *pgxpool.Pool
	repo          *academic.Repository
	evaluator     *entitlement.Evaluator
	student       string
	lesson        string
	course        string
	institutionID string
	programA      string
	programB      string
	collegeA      string
}

func newIsolationFixture(t *testing.T) *isolationFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, isolationAdminDSN)
	if err != nil {
		t.Skipf("PostgreSQL is unavailable: %v", err)
	}
	_, _ = admin.Exec(ctx,
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", isolationDBName)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+isolationDBName); err != nil {
		t.Fatalf("dropping: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+isolationDBName); err != nil {
		t.Fatalf("creating: %v", err)
	}
	admin.Close()

	migrator, err := migrate.New("file://../db/migrations", isolationDSN)
	if err != nil {
		t.Fatalf("opening migrations: %v", err)
	}
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating: %v", err)
	}
	_, _ = migrator.Close()

	pool, err := pgxpool.New(ctx, isolationDSN)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)

	f := &isolationFixture{pool: pool}
	if f.repo, err = academic.NewRepository(pool); err != nil {
		t.Fatalf("academic repository: %v", err)
	}
	entitlementRepository, err := entitlement.NewRepository(pool)
	if err != nil {
		t.Fatalf("entitlement repository: %v", err)
	}
	if f.evaluator, err = entitlement.NewEvaluator(entitlementRepository); err != nil {
		t.Fatalf("entitlement evaluator: %v", err)
	}

	// A real Student holding a real ACTIVE Course entitlement on a real Lesson.
	var instructor string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('iso-inst@example.test', 'iso-inst@example.test', 'INSTRUCTOR', 'ACTIVE', 'Instructor')
		RETURNING id::text`).Scan(&instructor); err != nil {
		t.Fatalf("seeding instructor: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('iso-student@example.test', 'iso-student@example.test', 'STUDENT', 'ACTIVE', 'Student')
		RETURNING id::text`).Scan(&f.student); err != nil {
		t.Fatalf("seeding student: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO courses (owner_account_id, lifecycle) VALUES ($1::uuid, 'DRAFT') RETURNING id::text`,
		instructor).Scan(&f.course); err != nil {
		t.Fatalf("seeding course: %v", err)
	}
	var revision, section, sectionIdentity, lessonIdentity string
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en)
		VALUES ($1::uuid, 'APPROVED', 1, 'دورة', 'Course') RETURNING id::text`, f.course).Scan(&revision); err != nil {
		t.Fatalf("seeding revision: %v", err)
	}
	// Published and live in one statement: the courses_published_has_live_revision
	// CHECK refuses a PUBLISHED Course that names no live revision.
	if _, err := pool.Exec(ctx,
		`UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid WHERE id = $2::uuid`,
		revision, f.course); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	// Sections and Lessons carry stable identities across revisions, and the
	// entitlement evaluator resolves a Lesson by its identity rather than by the
	// per-revision row, so both are seeded exactly as production does.
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_section_identities (course_id) VALUES ($1::uuid) RETURNING id::text`,
		f.course).Scan(&sectionIdentity); err != nil {
		t.Fatalf("seeding section identity: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_sections (revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'قسم', 'Section', 0) RETURNING id::text`,
		revision, f.course, sectionIdentity).Scan(&section); err != nil {
		t.Fatalf("seeding section: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_lesson_identities (course_id, section_identity_id) VALUES ($1::uuid, $2::uuid)
		RETURNING id::text`, f.course, sectionIdentity).Scan(&lessonIdentity); err != nil {
		t.Fatalf("seeding lesson identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_lessons (section_id, course_id, section_identity_id, lesson_identity_id,
			title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'درس', 'Lesson', 0)`,
		section, f.course, sectionIdentity, lessonIdentity); err != nil {
		t.Fatalf("seeding lesson: %v", err)
	}
	f.lesson = lessonIdentity
	// The entitlement is granted the way production grants one: through an
	// approved Course Access Invitation, which the schema requires for a
	// MANUAL_INVITATION grant. Seeding it also gives the snapshot a real
	// invitation row to prove the profile never disturbs.
	var adminAccount, invitation string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('iso-admin@example.test', 'iso-admin@example.test', 'ADMIN', 'ACTIVE', 'Admin')
		RETURNING id::text`).Scan(&adminAccount); err != nil {
		t.Fatalf("seeding admin: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_access_invitations (normalized_email, email, course_id,
			created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		VALUES ('iso-student@example.test', 'iso-student@example.test', $1::uuid,
			$2::uuid, $3::uuid, $2::uuid, 'APPROVED') RETURNING id::text`,
		f.course, adminAccount, f.student).Scan(&invitation); err != nil {
		t.Fatalf("seeding invitation: %v", err)
	}
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `
		INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source,
			source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1::uuid, $2::uuid, 'COURSE', $3::uuid, $3::uuid, 'MANUAL_INVITATION',
			$4::uuid, $5, $5, $6, 'ACTIVE')`,
		uuid.NewString(), f.student, f.course, invitation,
		now.Add(720*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("seeding entitlement: %v", err)
	}

	// A catalog the Student can point a profile at, and a second Program to
	// switch to so a Program change is exercised, not just a level edit.
	if err := pool.QueryRow(ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en, max_academic_level)
		VALUES ('KW', 'iso-university', 'ج', 'Isolation University', 5) RETURNING id::text`).
		Scan(&f.institutionID); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	seedProgram := func(collegeSlug, programSlug string) (string, string) {
		var college, program string
		if err := pool.QueryRow(ctx, `
			INSERT INTO academic_units (institution_id, kind, slug, name_ar, name_en)
			VALUES ($1::uuid, 'COLLEGE', $2, $2, $2) RETURNING id::text`,
			f.institutionID, collegeSlug).Scan(&college); err != nil {
			t.Fatalf("seeding college: %v", err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO programs (institution_id, owning_unit_id, slug, name_ar, name_en, degree_kind)
			VALUES ($1::uuid, $2::uuid, $3, $3, $3, 'BSC') RETURNING id::text`,
			f.institutionID, college, programSlug).Scan(&program); err != nil {
			t.Fatalf("seeding program: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO curricula (program_id, institution_id, version_label, status)
			VALUES ($1::uuid, $2::uuid, '2026', 'ACTIVE')`, program, f.institutionID); err != nil {
			t.Fatalf("seeding curriculum: %v", err)
		}
		return college, program
	}
	f.collegeA, f.programA = seedProgram("college-a", "program-a")
	_, f.programB = seedProgram("college-b", "program-b")
	return f
}

// accessSnapshot captures every access-relevant fact the profile must not move.
type accessSnapshot struct {
	allowed        bool
	reason         string
	readState      string
	readReason     string
	courseReads    int
	entitlements   int
	entitlementRow string
	enrollments    int
	invitations    int
	purchases      int
	progressRows   int
}

func (f *isolationFixture) snapshot(t *testing.T) accessSnapshot {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	// The real production decision point.
	decision := f.evaluator.Evaluate(ctx, f.student, f.lesson, now)
	read := f.evaluator.EvaluateRead(ctx, f.student, f.lesson, now)
	reads, err := f.evaluator.EvaluateCourseReads(ctx, f.student, now)
	if err != nil {
		t.Fatalf("evaluating course reads: %v", err)
	}

	snap := accessSnapshot{
		allowed:     decision.Allowed,
		reason:      string(decision.Reason),
		readState:   string(read.State),
		readReason:  string(read.Reason),
		courseReads: len(reads),
	}
	if err := f.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM entitlements WHERE student_account_id = $1::uuid),
			(SELECT coalesce(max(state::text || '|' || access_ends_at::text), '')
			   FROM entitlements WHERE student_account_id = $1::uuid),
			(SELECT count(*) FROM enrollments WHERE student_account_id = $1::uuid),
			(SELECT count(*) FROM course_access_invitations WHERE accepted_by_account_id = $1::uuid),
			(SELECT count(*) FROM purchase_requests),
			(SELECT count(*) FROM progress p
			   JOIN enrollments e ON e.id = p.enrollment_id
			  WHERE e.student_account_id = $1::uuid)
	`, f.student).Scan(&snap.entitlements, &snap.entitlementRow, &snap.enrollments,
		&snap.invitations, &snap.purchases, &snap.progressRows); err != nil {
		t.Fatalf("snapshotting access records: %v", err)
	}
	return snap
}

// TestAcademicProfileMutationDoesNotAffectEntitlementEvaluation is the D-092 §1
// release gate. Every profile mutation a Student can perform is applied to a
// Student who holds a real entitlement, and the real evaluator's decision plus
// every access record is proved unchanged.
func TestAcademicProfileMutationDoesNotAffectEntitlementEvaluation(t *testing.T) {
	f := newIsolationFixture(t)
	ctx := context.Background()

	before := f.snapshot(t)
	// The gate is only meaningful if access is actually granted to begin with.
	if !before.allowed {
		t.Fatalf("the fixture Student holds no access (%s); this gate would pass vacuously", before.reason)
	}
	if before.entitlements != 1 {
		t.Fatalf("fixture entitlements = %d, want 1", before.entitlements)
	}

	level := func(n int) *int { return &n }
	mutations := []struct {
		name string
		run  func() error
	}{
		{"first completion as enrolled", func() error {
			_, err := f.repo.SaveProfile(ctx, academic.SaveProfileRequest{
				AccountID: f.student, InstitutionID: f.institutionID,
				EnrollmentStatus: academic.EnrollmentEnrolled, ProgramID: f.programA, CurrentLevel: level(1),
			})
			return err
		}},
		{"level change", func() error {
			_, err := f.repo.SaveProfile(ctx, academic.SaveProfileRequest{
				AccountID: f.student, InstitutionID: f.institutionID,
				EnrollmentStatus: academic.EnrollmentEnrolled, ProgramID: f.programA, CurrentLevel: level(5),
			})
			return err
		}},
		{"program change", func() error {
			_, err := f.repo.SaveProfile(ctx, academic.SaveProfileRequest{
				AccountID: f.student, InstitutionID: f.institutionID,
				EnrollmentStatus: academic.EnrollmentEnrolled, ProgramID: f.programB, CurrentLevel: level(2),
			})
			return err
		}},
		{"becoming undeclared", func() error {
			_, err := f.repo.SaveProfile(ctx, academic.SaveProfileRequest{
				AccountID: f.student, InstitutionID: f.institutionID,
				EnrollmentStatus: academic.EnrollmentUndeclared, AcademicUnitID: f.collegeA,
			})
			return err
		}},
		{"becoming non-degree", func() error {
			_, err := f.repo.SaveProfile(ctx, academic.SaveProfileRequest{
				AccountID: f.student, InstitutionID: f.institutionID,
				EnrollmentStatus: academic.EnrollmentNonDegree,
			})
			return err
		}},
		{"skipping onboarding, which clears the whole profile", func() error {
			_, err := f.repo.SkipOnboarding(ctx, f.student)
			return err
		}},
	}

	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			if err := mutation.run(); err != nil {
				t.Fatalf("%s: %v", mutation.name, err)
			}
			after := f.snapshot(t)
			if after != before {
				t.Fatalf("%s changed access.\n before = %+v\n after  = %+v", mutation.name, before, after)
			}
		})
	}

	// Finally, a Student with no profile row at all must be identical too.
	if _, err := f.pool.Exec(ctx,
		`DELETE FROM student_academic_profiles WHERE account_id = $1::uuid`, f.student); err != nil {
		t.Fatalf("clearing the profile: %v", err)
	}
	if after := f.snapshot(t); after != before {
		t.Fatalf("removing the profile entirely changed access.\n before = %+v\n after = %+v", before, after)
	}
}

// TestCurriculumIsNeverAnAccessInput guards D-091 explicitly: a Course whose
// Subject is absent from the Student's curriculum must still be accessible.
// This is the anti-regression for "if Course Subject not in Student Curriculum
// then deny", which would silently turn discovery data into an access input.
func TestCurriculumIsNeverAnAccessInput(t *testing.T) {
	f := newIsolationFixture(t)
	ctx := context.Background()

	// The Student enrols on a plan that maps one Subject, and the Course they
	// hold access to is related to none of it.
	if _, err := f.repo.SaveProfile(ctx, academic.SaveProfileRequest{
		AccountID: f.student, InstitutionID: f.institutionID,
		EnrollmentStatus: academic.EnrollmentEnrolled, ProgramID: f.programA,
	}); err != nil {
		t.Fatalf("saving profile: %v", err)
	}
	var subject, curriculum string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1::uuid, '9999-999', 'غير ذات صلة', 'Unrelated Subject') RETURNING id::text`,
		f.institutionID).Scan(&subject); err != nil {
		t.Fatalf("seeding subject: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `
		SELECT id::text FROM curricula WHERE program_id = $1::uuid AND status = 'ACTIVE'`,
		f.programA).Scan(&curriculum); err != nil {
		t.Fatalf("reading curriculum: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO curriculum_subjects (curriculum_id, subject_id, institution_id, requirement_kind)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'MAJOR_CORE')`,
		curriculum, subject, f.institutionID); err != nil {
		t.Fatalf("mapping subject: %v", err)
	}

	decision := f.evaluator.Evaluate(ctx, f.student, f.lesson, time.Now().UTC())
	if !decision.Allowed {
		t.Fatalf("access denied (%s) for a Course outside the Student's curriculum; "+
			"curriculum is discovery data and must never gate access", decision.Reason)
	}

	// And the Academic Catalog holds no foreign key into Course or access tables
	// through which such a dependency could later be introduced quietly.
	var links int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.table_constraints tc
		JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_name = 'student_academic_profiles'
		  AND ccu.table_name IN ('courses','course_revisions','entitlements','enrollments',
		                         'course_access_invitations','purchase_requests','lesson_progress')`).
		Scan(&links); err != nil {
		t.Fatalf("inspecting profile foreign keys: %v", err)
	}
	if links != 0 {
		t.Fatalf("the Student profile holds %d foreign keys into Course/access tables", links)
	}
}
