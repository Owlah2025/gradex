//go:build integration

package learning

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	learningAdminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	learningTestDSN  = "postgres://gradex:gradex@localhost:5432/gradex_learning_test?sslmode=disable"
	learningSource   = "file://../db/migrations"
)

type learningFixture struct {
	repository *Repository
	studentID  string
	courseID   string
	lessonID   string
}

func newLearningFixture(t *testing.T) learningFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, learningAdminDSN)
	if err != nil {
		t.Fatalf("opening learning test admin database: %v", err)
	}
	t.Cleanup(admin.Close)
	_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'gradex_learning_test'`)
	_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS gradex_learning_test`)
	if _, err := admin.Exec(ctx, `CREATE DATABASE gradex_learning_test`); err != nil {
		t.Fatalf("creating learning test database: %v", err)
	}
	m, err := migrate.New(learningSource, learningTestDSN)
	if err != nil {
		t.Fatalf("opening learning migrations: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil {
		t.Fatalf("migrating learning test database: %v", err)
	}
	pool, err := pgxpool.New(ctx, learningTestDSN)
	if err != nil {
		t.Fatalf("opening learning test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repository, err := NewRepository(pool)
	if err != nil {
		t.Fatalf("constructing learning repository: %v", err)
	}
	fixture := learningFixture{
		repository: repository,
		studentID:  "11111111-1111-1111-1111-111111111111",
		courseID:   "22222222-2222-2222-2222-222222222222",
		lessonID:   "33333333-3333-3333-3333-333333333333",
	}
	seedLearningFixture(t, ctx, pool, fixture)
	return fixture
}

func seedLearningFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture learningFixture) {
	t.Helper()
	const instructorID = "44444444-4444-4444-4444-444444444444"
	const revisionID = "55555555-5555-5555-5555-555555555555"
	const sectionID = "66666666-6666-6666-6666-666666666666"
	const sectionRowID = "77777777-7777-7777-7777-777777777777"
	for _, account := range []struct{ id, email, role string }{
		{fixture.studentID, "student@example.test", "STUDENT"},
		{instructorID, "instructor@example.test", "INSTRUCTOR"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES ($1::uuid, $2, $2, $3, 'ACTIVE', 'Learning test')`, account.id, account.email, account.role); err != nil {
			t.Fatalf("seeding %s account: %v", account.role, err)
		}
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, []any{fixture.courseID, instructorID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, 'دورة', 'Course')`, []any{revisionID, fixture.courseID}},
		{`UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`, []any{revisionID, fixture.courseID}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionID, fixture.courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{fixture.lessonID, fixture.courseID, sectionID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم', 'Section', 0)`, []any{sectionRowID, revisionID, fixture.courseID, sectionID}},
		{`INSERT INTO course_lessons (section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'درس', 'Lesson', 0)`, []any{sectionRowID, fixture.courseID, sectionID, fixture.lessonID}},
		{`INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{fixture.studentID, fixture.courseID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding learning graph: %v\n%s", err, statement.query)
		}
	}
}

func TestEnrollmentForLessonResolvesOnlyLiveStudentCourseGraph(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	var before int
	if err := fixture.repository.pool.QueryRow(ctx, `SELECT count(*) FROM enrollments`).Scan(&before); err != nil {
		t.Fatalf("counting enrollments before resolution: %v", err)
	}
	enrollment, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, fixture.lessonID)
	if err != nil {
		t.Fatalf("resolving valid enrollment: %v", err)
	}
	if enrollment.StudentID != fixture.studentID || enrollment.CourseID != fixture.courseID || enrollment.ID == "" {
		t.Fatalf("resolved enrollment = %+v", enrollment)
	}
	if _, err := fixture.repository.EnrollmentForLesson(ctx, "99999999-9999-9999-9999-999999999999", fixture.lessonID); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("missing student error = %v, want ErrEnrollmentNotFound", err)
	}
	if _, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, "99999999-9999-9999-9999-999999999999"); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("missing lesson error = %v, want ErrEnrollmentNotFound", err)
	}
	var after int
	if err := fixture.repository.pool.QueryRow(ctx, `SELECT count(*) FROM enrollments`).Scan(&after); err != nil {
		t.Fatalf("counting enrollments after resolution: %v", err)
	}
	if after != before {
		t.Fatalf("read-only enrollment resolution changed rows: before=%d after=%d", before, after)
	}
}

func TestEnrollmentForLessonFailsClosedForWrongCourseWithoutRepair(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	const wrongCourseID = "99999999-9999-9999-9999-999999999998"
	if _, err := fixture.repository.pool.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle)
		VALUES ($1::uuid, '44444444-4444-4444-4444-444444444444', 'DRAFT')
	`, wrongCourseID); err != nil {
		t.Fatalf("seeding wrong course: %v", err)
	}
	if _, err := fixture.repository.pool.Exec(ctx, `
		UPDATE enrollments SET course_id = $1::uuid WHERE student_account_id = $2::uuid
	`, wrongCourseID, fixture.studentID); err != nil {
		t.Fatalf("moving fixture enrollment to wrong course: %v", err)
	}
	if _, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, fixture.lessonID); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("wrong-course enrollment error = %v, want ErrEnrollmentNotFound", err)
	}
	var rows int
	if err := fixture.repository.pool.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE student_account_id = $1::uuid AND course_id = $2::uuid`, fixture.studentID, wrongCourseID).Scan(&rows); err != nil {
		t.Fatalf("checking resolver did not repair enrollment: %v", err)
	}
	if rows != 1 {
		t.Fatalf("resolver repaired or replaced wrong-course enrollment; rows=%d", rows)
	}
}

func TestEnrollmentForLessonFailsClosedWhenLiveRevisionIsRemoved(t *testing.T) {
	fixture := newLearningFixture(t)
	pool := fixture.repository.pool
	if _, err := pool.Exec(context.Background(), `UPDATE courses SET live_revision_id = NULL, lifecycle = 'DRAFT' WHERE id = $1::uuid`, fixture.courseID); err != nil {
		t.Fatalf("removing live revision: %v", err)
	}
	if _, err := fixture.repository.EnrollmentForLesson(context.Background(), fixture.studentID, fixture.lessonID); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("invalid live graph error = %v, want ErrEnrollmentNotFound", err)
	}
}

func TestProgressSurvivesLiveRevisionReplacement(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	enrollment, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, fixture.lessonID)
	if err != nil {
		t.Fatalf("resolving enrollment: %v", err)
	}
	if err := fixture.repository.SaveProgress(ctx, ProgressWrite{
		EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 42,
	}); err != nil {
		t.Fatalf("saving initial progress: %v", err)
	}
	pool := fixture.repository.pool
	const replacementRevisionID = "88888888-8888-8888-8888-888888888888"
	const replacementSectionID = "99999999-9999-9999-9999-999999999999"
	const sectionIdentityID = "66666666-6666-6666-6666-666666666666"
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en)
		VALUES ($1::uuid, $2::uuid, 'APPROVED', 2, 'دورة محدثة', 'Updated Course')
	`, replacementRevisionID, fixture.courseID); err != nil {
		t.Fatalf("creating replacement revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم محدث', 'Updated Section', 0)
	`, replacementSectionID, replacementRevisionID, fixture.courseID, sectionIdentityID); err != nil {
		t.Fatalf("creating replacement section: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_lessons (section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'درس محدث', 'Updated Lesson', 0)
	`, replacementSectionID, fixture.courseID, sectionIdentityID, fixture.lessonID); err != nil {
		t.Fatalf("creating replacement lesson row: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, replacementRevisionID, fixture.courseID); err != nil {
		t.Fatalf("swapping live revision: %v", err)
	}
	if _, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, fixture.lessonID); err != nil {
		t.Fatalf("resolving replacement live lesson: %v", err)
	}
	progress, err := fixture.repository.ProgressForLesson(ctx, enrollment.ID, fixture.lessonID)
	if err != nil {
		t.Fatalf("reading retained progress: %v", err)
	}
	if progress.LastPositionSeconds != 42 || progress.MaxPositionSeconds != 42 {
		t.Fatalf("retained progress = %+v", progress)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM progress WHERE enrollment_id = $1::uuid AND course_lesson_identity_id = $2::uuid`, enrollment.ID, fixture.lessonID).Scan(&rows); err != nil {
		t.Fatalf("counting progress rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("progress rows after revision replacement = %d, want 1", rows)
	}
}

func TestSaveProgressUsesOneStableRowAndWriteOnceCompletion(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	enrollment, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, fixture.lessonID)
	if err != nil {
		t.Fatalf("resolving enrollment: %v", err)
	}
	firstVersion := seedProgressAssetVersion(t, ctx, fixture, "88888888-8888-8888-8888-888888888881")
	secondVersion := seedProgressAssetVersion(t, ctx, fixture, "88888888-8888-8888-8888-888888888882")
	for _, write := range []ProgressWrite{
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 70, Completed: true, CompletingAssetVersionID: firstVersion},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 15},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 90, Completed: true, CompletingAssetVersionID: secondVersion},
	} {
		if err := fixture.repository.SaveProgress(ctx, write); err != nil {
			t.Fatalf("saving progress: %v", err)
		}
	}

	progress, err := fixture.repository.ProgressForLesson(ctx, enrollment.ID, fixture.lessonID)
	if err != nil {
		t.Fatalf("reading saved progress: %v", err)
	}
	if progress.MaxPositionSeconds != 90 || progress.LastPositionSeconds != 90 || !progress.Completed {
		t.Fatalf("saved progress = %+v, want monotonic maximum, latest resume, and completion", progress)
	}
	var completingVersion string
	var rows int
	if err := fixture.repository.pool.QueryRow(ctx, `
		SELECT completing_asset_version_id::text, count(*) OVER ()
		FROM progress
		WHERE enrollment_id = $1::uuid AND course_lesson_identity_id = $2::uuid
	`, enrollment.ID, fixture.lessonID).Scan(&completingVersion, &rows); err != nil {
		t.Fatalf("reading completion evidence: %v", err)
	}
	if completingVersion != firstVersion || rows != 1 {
		t.Fatalf("completion evidence = version %s rows %d, want first version and one durable row", completingVersion, rows)
	}
}

func TestProgressConcurrentWritersPreserveMonotonicMaximum(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	enrollment, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, fixture.lessonID)
	if err != nil {
		t.Fatalf("resolving enrollment: %v", err)
	}

	completingVersion := seedProgressAssetVersion(t, ctx, fixture, "88888888-8888-8888-8888-888888888883")
	start := make(chan struct{})
	writes := []ProgressWrite{
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 25},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 85, Completed: true, CompletingAssetVersionID: completingVersion},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 40},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 85},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 10},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 70},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 85},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 55},
	}
	errs := make(chan error, len(writes))
	var writers sync.WaitGroup
	for _, write := range writes {
		writers.Add(1)
		go func(write ProgressWrite) {
			defer writers.Done()
			<-start
			errs <- fixture.repository.SaveProgressGuarded(ctx, write, func(context.Context, pgx.Tx) error { return nil })
		}(write)
	}
	close(start)
	writers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent progress write: %v", err)
		}
	}

	progress, err := fixture.repository.ProgressForLesson(ctx, enrollment.ID, fixture.lessonID)
	if err != nil {
		t.Fatalf("reading converged progress: %v", err)
	}
	if progress.MaxPositionSeconds != 85 || !progress.Completed {
		t.Fatalf("converged progress = %+v, want maximum 85 and write-once completion", progress)
	}
	var rows int
	var completedAt *time.Time
	var storedVersion string
	if err := fixture.repository.pool.QueryRow(ctx, `SELECT count(*) FROM progress WHERE enrollment_id = $1::uuid AND course_lesson_identity_id = $2::uuid`, enrollment.ID, fixture.lessonID).Scan(&rows); err != nil {
		t.Fatalf("counting concurrent progress rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("concurrent writes created %d rows, want one", rows)
	}
	if err := fixture.repository.pool.QueryRow(ctx, `
		SELECT completed_at, completing_asset_version_id::text
		FROM progress WHERE enrollment_id = $1::uuid AND course_lesson_identity_id = $2::uuid
	`, enrollment.ID, fixture.lessonID).Scan(&completedAt, &storedVersion); err != nil {
		t.Fatalf("reading concurrent completion evidence: %v", err)
	}
	if completedAt == nil || storedVersion != completingVersion {
		t.Fatalf("concurrent completion evidence = completed_at %v version %q", completedAt, storedVersion)
	}
}

func TestSaveProgressRejectsNonFinitePositionBeforeWriting(t *testing.T) {
	fixture := newLearningFixture(t)
	for _, position := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := fixture.repository.SaveProgress(context.Background(), ProgressWrite{
			EnrollmentID: fixture.studentID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: position,
		}); !errors.Is(err, ErrProgressUnavailable) {
			t.Fatalf("non-finite position %v error = %v, want %v", position, err, ErrProgressUnavailable)
		}
	}
	var rows int
	if err := fixture.repository.pool.QueryRow(context.Background(), `SELECT count(*) FROM progress`).Scan(&rows); err != nil {
		t.Fatalf("counting progress after rejected inputs: %v", err)
	}
	if rows != 0 {
		t.Fatalf("rejected positions wrote %d Progress rows", rows)
	}
}

func seedProgressAssetVersion(t *testing.T, ctx context.Context, fixture learningFixture, versionID string) string {
	t.Helper()
	var assetID string
	if err := fixture.repository.pool.QueryRow(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility)
		VALUES (gen_random_uuid(), 'VIDEO', '44444444-4444-4444-4444-444444444444', $1::uuid, $2::uuid, 'PROTECTED')
		RETURNING id::text
	`, fixture.courseID, fixture.lessonID).Scan(&assetID); err != nil {
		t.Fatalf("seeding progress asset: %v", err)
	}
	if _, err := fixture.repository.pool.Exec(ctx, `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, $2::uuid, 'VIDEO', 'UPLOADED', $3, 'v1', 'video/mp4', 1)
	`, versionID, assetID, "progress/"+versionID); err != nil {
		t.Fatalf("seeding progress asset version: %v", err)
	}
	return versionID
}
