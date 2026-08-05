//go:build integration

package e2equery

import (
	"context"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	queryAdminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	queryTestDSN  = "postgres://gradex:gradex@localhost:5432/gradex_e2equery_test?sslmode=disable"
	querySource   = "file://../migrations"
)

// Fixture identities. Two Students, two Courses, and three stable Lesson Identities, so
// every isolation dimension the helper must respect has a real neighbouring row that a
// broken query would wrongly return.
const (
	studentA = "a0000000-0000-0000-0000-0000000000a1"
	studentB = "a0000000-0000-0000-0000-0000000000a2"

	courseOne = "c0000000-0000-0000-0000-0000000000c1"
	courseTwo = "c0000000-0000-0000-0000-0000000000c2"

	lessonPartial   = "30000000-0000-0000-0000-0000000000e1"
	lessonCompleted = "30000000-0000-0000-0000-0000000000e2"
	lessonNoRow     = "30000000-0000-0000-0000-0000000000e3"
	lessonCourseTwo = "30000000-0000-0000-0000-0000000000e4"

	assetVersion = "60000000-0000-0000-0000-0000000000f1"
)

func newQueryFixture(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	admin, err := pgxpool.New(ctx, queryAdminDSN)
	if err != nil {
		t.Fatalf("opening e2equery admin database: %v", err)
	}
	t.Cleanup(admin.Close)
	_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'gradex_e2equery_test'`)
	_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS gradex_e2equery_test`)
	if _, err := admin.Exec(ctx, `CREATE DATABASE gradex_e2equery_test`); err != nil {
		t.Fatalf("creating e2equery test database: %v", err)
	}

	m, err := migrate.New(querySource, queryTestDSN)
	if err != nil {
		t.Fatalf("opening production migrations: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil {
		t.Fatalf("migrating e2equery test database: %v", err)
	}

	pool, err := pgxpool.New(ctx, queryTestDSN)
	if err != nil {
		t.Fatalf("opening e2equery test pool: %v", err)
	}
	t.Cleanup(pool.Close)

	seedQueryFixture(ctx, t, pool)
	return pool
}

func seedQueryFixture(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)

	exec := func(description, sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", description, err)
		}
	}

	for _, account := range []struct{ id, email string }{
		{studentA, "e2equery-a@example.test"},
		{studentB, "e2equery-b@example.test"},
	} {
		exec("insert account", `
			INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
			VALUES ($1, $2, $2, 'STUDENT', 'ACTIVE', 'E2E Query Student', $3)
		`, account.id, account.email, now)
	}
	exec("insert instructor", `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
		VALUES ('a0000000-0000-0000-0000-0000000000a9', 'e2equery-i@example.test', 'e2equery-i@example.test', 'INSTRUCTOR', 'ACTIVE', 'E2E Query Instructor', $1)
	`, now)

	for _, course := range []string{courseOne, courseTwo} {
		exec("insert course", `
			INSERT INTO courses (id, owner_account_id, lifecycle)
			VALUES ($1, 'a0000000-0000-0000-0000-0000000000a9', 'DRAFT')
		`, course)
		exec("insert section identity", `
			INSERT INTO course_section_identities (id, course_id) VALUES (gen_random_uuid(), $1)
		`, course)
	}

	for _, lesson := range []struct{ id, course string }{
		{lessonPartial, courseOne},
		{lessonCompleted, courseOne},
		{lessonNoRow, courseOne},
		{lessonCourseTwo, courseTwo},
	} {
		exec("insert lesson identity", `
			INSERT INTO course_lesson_identities (id, course_id, section_identity_id)
			VALUES ($1, $2, (SELECT id FROM course_section_identities WHERE course_id = $2 LIMIT 1))
		`, lesson.id, lesson.course)
	}

	exec("insert media asset", `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES ('50000000-0000-0000-0000-0000000000f1', 'VIDEO', 'a0000000-0000-0000-0000-0000000000a9', $1, 'PROTECTED')
	`, courseOne)
	exec("insert asset version", `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1, '50000000-0000-0000-0000-0000000000f1', 'VIDEO', 'READY', 'e2equery/master.m3u8', 'v1', 'application/vnd.apple.mpegurl', 1024)
	`, assetVersion)

	// Student A is enrolled in both Courses; Student B is enrolled in Course One. Every
	// isolation assertion therefore has a genuine competing row rather than an empty table.
	enrollments := []struct{ id, student, course string }{
		{"b0000000-0000-0000-0000-0000000000b1", studentA, courseOne},
		{"b0000000-0000-0000-0000-0000000000b2", studentA, courseTwo},
		{"b0000000-0000-0000-0000-0000000000b3", studentB, courseOne},
	}
	for _, enrollment := range enrollments {
		exec("insert enrollment", `
			INSERT INTO enrollments (id, student_account_id, course_id) VALUES ($1, $2, $3)
		`, enrollment.id, enrollment.student, enrollment.course)
	}

	// Student A, Course One, partial Lesson.
	exec("insert partial progress", `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at)
		VALUES ('b0000000-0000-0000-0000-0000000000b1', $1, 45.5, 42.25, $2)
	`, lessonPartial, now)

	// Student A, Course One, completed Lesson with its Asset Version binding.
	exec("insert completed progress", `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, completed_at, completing_asset_version_id, last_watched_at)
		VALUES ('b0000000-0000-0000-0000-0000000000b1', $1, 300, 300, $2, $3, $2)
	`, lessonCompleted, now, assetVersion)

	// Student B holds a deliberately different position on the same Course and Lesson.
	exec("insert other student progress", `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at)
		VALUES ('b0000000-0000-0000-0000-0000000000b3', $1, 999, 999, $2)
	`, lessonPartial, now)

	// Student A also holds Progress in Course Two, on a Lesson Identity of that Course.
	exec("insert other course progress", `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at)
		VALUES ('b0000000-0000-0000-0000-0000000000b2', $1, 777, 777, $2)
	`, lessonCourseTwo, now)
}

// TestReadProgressFindsExistingRowThroughRealSchema covers requirements 1–4: the row is found
// through the authoritative Enrollment and Course Lesson Identity join, and position,
// completion, and Asset Version binding are all read correctly.
func TestReadProgressFindsExistingRowThroughRealSchema(t *testing.T) {
	pool := newQueryFixture(t)
	ctx := context.Background()

	partial, err := ReadProgress(ctx, pool, studentA, courseOne, lessonPartial)
	if err != nil {
		t.Fatalf("reading partial progress: %v", err)
	}
	if !partial.Found {
		t.Fatal("partial Progress row exists in PostgreSQL but the helper reported found:false")
	}
	if partial.MaxPositionSeconds != 45.5 {
		t.Fatalf("max position = %v, want 45.5", partial.MaxPositionSeconds)
	}
	if partial.PositionSeconds != 42.25 {
		t.Fatalf("resume position = %v, want 42.25", partial.PositionSeconds)
	}
	if partial.Completed {
		t.Fatal("partial row reported as completed")
	}
	if partial.CompletedAt != "" || partial.AssetVersionID != "" {
		t.Fatalf("partial row carries completion state: completed_at=%q asset=%q", partial.CompletedAt, partial.AssetVersionID)
	}
	if partial.UpdatedAt == "" {
		t.Fatal("updated_at was not read")
	}

	completed, err := ReadProgress(ctx, pool, studentA, courseOne, lessonCompleted)
	if err != nil {
		t.Fatalf("reading completed progress: %v", err)
	}
	if !completed.Found || !completed.Completed {
		t.Fatalf("completed row not read correctly: %+v", completed)
	}
	if completed.PositionSeconds != 300 {
		t.Fatalf("completed position = %v, want 300", completed.PositionSeconds)
	}
	if completed.AssetVersionID != assetVersion {
		t.Fatalf("completing Asset Version binding = %q, want %q", completed.AssetVersionID, assetVersion)
	}
	if completed.CompletedAt == "" {
		t.Fatal("completed_at was not read")
	}
}

// TestReadProgressIsolatesStudentCourseAndLesson covers requirements 5–7.
func TestReadProgressIsolatesStudentCourseAndLesson(t *testing.T) {
	pool := newQueryFixture(t)
	ctx := context.Background()

	// 5. Another Student's row is not returned in place of the requested Student's.
	otherStudent, err := ReadProgress(ctx, pool, studentB, courseOne, lessonPartial)
	if err != nil {
		t.Fatalf("reading other Student progress: %v", err)
	}
	if !otherStudent.Found || otherStudent.PositionSeconds != 999 {
		t.Fatalf("Student B's own row not resolved: %+v", otherStudent)
	}
	mine, err := ReadProgress(ctx, pool, studentA, courseOne, lessonPartial)
	if err != nil {
		t.Fatalf("reading Student A progress: %v", err)
	}
	if mine.PositionSeconds == otherStudent.PositionSeconds {
		t.Fatalf("Student isolation failed: both Students resolved to %v", mine.PositionSeconds)
	}

	// 6. Another Course's row is not returned: the Lesson Identity belongs to Course Two, so
	// asking for it under Course One must not match even though the Student is enrolled in both.
	crossCourse, err := ReadProgress(ctx, pool, studentA, courseOne, lessonCourseTwo)
	if err != nil {
		t.Fatalf("reading cross-Course progress: %v", err)
	}
	if crossCourse.Found {
		t.Fatalf("Course isolation failed: Course Two Lesson resolved under Course One: %+v", crossCourse)
	}
	sameCourse, err := ReadProgress(ctx, pool, studentA, courseTwo, lessonCourseTwo)
	if err != nil {
		t.Fatalf("reading Course Two progress: %v", err)
	}
	if !sameCourse.Found || sameCourse.PositionSeconds != 777 {
		t.Fatalf("Course Two row not resolved under its own Course: %+v", sameCourse)
	}

	// 7. Another stable Lesson's row is not returned.
	if mine.PositionSeconds == 300 {
		t.Fatal("Lesson isolation failed: partial Lesson resolved the completed Lesson's row")
	}
	otherLesson, err := ReadProgress(ctx, pool, studentA, courseOne, lessonCompleted)
	if err != nil {
		t.Fatalf("reading other Lesson progress: %v", err)
	}
	if otherLesson.PositionSeconds == mine.PositionSeconds {
		t.Fatalf("Lesson isolation failed: both Lessons resolved to %v", mine.PositionSeconds)
	}
}

// TestReadProgressReportsMissingRowExplicitly covers requirement 8: absence is reported as
// found:false and never as a zero-valued row that an assertion could mistake for persistence.
func TestReadProgressReportsMissingRowExplicitly(t *testing.T) {
	pool := newQueryFixture(t)
	ctx := context.Background()

	missing, err := ReadProgress(ctx, pool, studentA, courseOne, lessonNoRow)
	if err != nil {
		t.Fatalf("reading missing progress: %v", err)
	}
	if missing.Found {
		t.Fatalf("no Progress row exists for this Lesson but the helper reported one: %+v", missing)
	}

	// A Student with no Enrollment for the Course resolves to absence, not to someone else's row.
	unenrolled, err := ReadProgress(ctx, pool, studentB, courseTwo, lessonCourseTwo)
	if err != nil {
		t.Fatalf("reading unenrolled progress: %v", err)
	}
	if unenrolled.Found {
		t.Fatalf("unenrolled Student resolved a Progress row: %+v", unenrolled)
	}
}

// TestReadProgressRejectsLegacyColumnAssumptions is the regression guard for the defect this
// package replaces: a query against `lesson_progress`, `student_id`, or `lesson_id` cannot be
// written against this schema without failing loudly.
func TestReadProgressRejectsLegacyColumnAssumptions(t *testing.T) {
	pool := newQueryFixture(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `SELECT 1 FROM lesson_progress LIMIT 1`); err == nil {
		t.Fatal("table lesson_progress unexpectedly exists; the helper contract must be revisited")
	}
	if _, err := pool.Exec(ctx, `SELECT student_id, lesson_id FROM progress LIMIT 1`); err == nil {
		t.Fatal("progress unexpectedly exposes student_id/lesson_id; the helper contract must be revisited")
	}
}

// TestReadProgressReportsQueryFailureRatherThanAbsence is the guarantee that makes every other
// assertion in this package meaningful. The defect this replaces caught its query error and
// returned `{"found": false}`, so a test asserting "the other Student's Progress is unchanged"
// passed against a query that never ran. A failing query must surface as an error; only a
// successful query that matched nothing may report absence.
func TestReadProgressReportsQueryFailureRatherThanAbsence(t *testing.T) {
	pool := newQueryFixture(t)

	// A cancelled context cannot execute, so it cannot have established that no row exists.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	snapshot, err := ReadProgress(cancelled, pool, studentA, courseOne, lessonPartial)
	if err == nil {
		t.Fatalf("a query that could not run reported a usable snapshot instead of failing: %+v", snapshot)
	}
	if snapshot.Found {
		t.Fatalf("a failed query must not report a found row: %+v", snapshot)
	}

	// A malformed identifier is a database-level failure, not evidence of absence.
	if _, err := ReadProgress(context.Background(), pool, "not-a-uuid", courseOne, lessonPartial); err == nil {
		t.Fatal("an invalid Student identifier was silently reported as absence rather than failing")
	}

	// A closed pool cannot answer the question either.
	closed, err := pgxpool.New(context.Background(), queryTestDSN)
	if err != nil {
		t.Fatalf("opening pool for closed-pool case: %v", err)
	}
	closed.Close()
	if _, err := ReadProgress(context.Background(), closed, studentA, courseOne, lessonPartial); err == nil {
		t.Fatal("a closed pool reported absence instead of failing")
	}

	// The same identity still resolves on a healthy pool, proving the fixture row really exists
	// and the failures above were failures, not genuine absence.
	healthy, err := ReadProgress(context.Background(), pool, studentA, courseOne, lessonPartial)
	if err != nil {
		t.Fatalf("reading progress on a healthy pool: %v", err)
	}
	if !healthy.Found {
		t.Fatal("the fixture Progress row is missing; the failure cases above prove nothing")
	}
}
