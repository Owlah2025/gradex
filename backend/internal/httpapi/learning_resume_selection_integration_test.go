//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Selection tests for the MVP-F15 continue-learning pointer.
//
// Every case drives the real `GET /api/v1/learn/dashboard` route, so it exercises the production
// chain end to end: `ListStudentResumeCandidates`' single SQL statement, the authoritative
// entitlement evaluator, and `resumeReadModel`. Nothing here re-implements the ordering rules —
// a duplicated algorithm would agree with itself while the shipped one drifted.

// resumeOf returns the Dashboard's resume pointer, or nil when the route omitted it.
func resumeOf(t *testing.T, f learningIntegrationFixture) *dashboardResumeResponse {
	t.Helper()
	response := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/dashboard", "", map[string]string{"Accept-Language": "en"})
	assertReadSuccess(t, response)
	var dashboard dashboardResponse
	if err := json.Unmarshal(response.Body.Bytes(), &dashboard); err != nil {
		t.Fatalf("decoding dashboard: %v; body=%s", err, response.Body.String())
	}
	return dashboard.Resume
}

func requireResume(t *testing.T, f learningIntegrationFixture, description string) dashboardResumeResponse {
	t.Helper()
	resume := resumeOf(t, f)
	if resume == nil {
		t.Fatalf("%s: expected a resume pointer, got none", description)
	}
	return *resume
}

func requireNoResume(t *testing.T, f learningIntegrationFixture, description string) {
	t.Helper()
	if resume := resumeOf(t, f); resume != nil {
		t.Fatalf("%s: expected no resume pointer, got %+v", description, *resume)
	}
}

// enrollmentOf resolves the Student's Enrollment for an arbitrary Course, so Progress can be
// seeded for a Course other than the fixture's own.
func enrollmentOf(t *testing.T, f learningIntegrationFixture, courseID string) string {
	t.Helper()
	var enrollmentID string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT id::text FROM enrollments WHERE student_account_id = $1::uuid AND course_id = $2::uuid`,
		f.studentID, courseID,
	).Scan(&enrollmentID); err != nil {
		t.Fatalf("resolving enrollment for course %s: %v", courseID, err)
	}
	return enrollmentID
}

// seedProgressAt writes one Progress row with an explicit watch instant, which is what the
// ordering rules are actually decided on.
func seedProgressAt(t *testing.T, f learningIntegrationFixture, courseID, lessonID string, position float64, completed bool, watchedAt time.Time) {
	t.Helper()
	var completedAt, versionID any
	if completed {
		completedAt = watchedAt
		versionID = f.versionID
	}
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, completed_at, completing_asset_version_id, last_watched_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, $3, $4, $5::uuid, $6, $6)`,
		enrollmentOf(t, f, courseID), lessonID, position, completedAt, versionID, watchedAt,
	); err != nil {
		t.Fatalf("seeding progress for lesson %s: %v", lessonID, err)
	}
}

// seedResumeCourse builds a second published Course with one Lesson, enrolls the fixture's
// Student and grants active Course-wide access.
func seedResumeCourse(t *testing.T, f learningIntegrationFixture, titleEn string) (courseID, lessonID string) {
	t.Helper()
	ctx := context.Background()
	var ownerID string
	if err := f.pool.QueryRow(ctx, `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&ownerID); err != nil {
		t.Fatalf("reading course owner: %v", err)
	}
	courseID, lessonID = uuid.NewString(), uuid.NewString()
	revisionID, sectionIdentityID := uuid.NewString(), uuid.NewString()
	sectionRowID, lessonRowID, invID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, []any{courseID, ownerID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, $3, $4)`, []any{revisionID, courseID, "دورة " + titleEn, titleEn}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentityID, courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{lessonID, courseID, sectionIdentityID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, 0)`, []any{sectionRowID, revisionID, courseID, sectionIdentityID, "قسم " + titleEn, "Section " + titleEn}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7, 0)`, []any{lessonRowID, sectionRowID, courseID, sectionIdentityID, lessonID, "درس " + titleEn, "Lesson " + titleEn}},
		{`UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`, []any{revisionID, courseID}},
		{`INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{f.studentID, courseID}},
		{`INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state) VALUES ($1::uuid, $2::uuid, 'student@example.com', 'student@example.com', $3::uuid, $4::uuid, $3::uuid, 'APPROVED')`, []any{invID, courseID, ownerID, f.studentID}},
		{`INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, $4, $4, $5, 'ACTIVE')`, []any{f.studentID, courseID, invID, f.clock.Now().Add(time.Hour), f.clock.Now().Add(-time.Hour)}},
	} {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding resume Course %s: %v", titleEn, err)
		}
	}
	return courseID, lessonID
}

//  1. A Student who has never watched anything still gets a pointer — at the Course's first
//     Lesson, marked unstarted so the surface says "start" rather than "continue".
func TestResumeStudentWithNoProgressPointsAtFirstLessonUnstarted(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	addLearningSectionLesson(t, f, 1, "two")

	resume := requireResume(t, f, "Student with no Progress")
	if resume.CourseID != f.courseID || resume.LessonID != f.lessonID {
		t.Fatalf("resume = %+v, want the first Lesson of the entitled Course", resume)
	}
	if resume.Started {
		t.Fatalf("resume.Started = true for a Student who has watched nothing; want false")
	}
	if resume.LessonTitle != "Lesson" || resume.CourseTitle != "Course" {
		t.Fatalf("resume titles = %q / %q, want the live-revision Course and Lesson titles", resume.CourseTitle, resume.LessonTitle)
	}
}

// 2. One part-finished Lesson is the pointer, and it reports as started.
func TestResumeSelectsTheSinglePartFinishedLesson(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	secondLessonID := addLearningSectionLesson(t, f, 1, "two")
	seedProgressAt(t, f, f.courseID, secondLessonID, 25, false, f.clock.Now().Add(-10*time.Minute))

	resume := requireResume(t, f, "one in-progress Lesson")
	if resume.LessonID != secondLessonID {
		t.Fatalf("resume lesson = %s, want the part-finished Lesson %s", resume.LessonID, secondLessonID)
	}
	if !resume.Started {
		t.Fatalf("resume.Started = false for a Lesson with recorded watch time; want true")
	}
}

// 3. With several part-finished Lessons the most recently watched one wins.
func TestResumeAcrossMultipleLessonsPicksTheMostRecentlyWatched(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	secondLessonID := addLearningSectionLesson(t, f, 1, "two")
	thirdLessonID := addLearningSectionLesson(t, f, 2, "three")

	seedProgressAt(t, f, f.courseID, f.lessonID, 10, false, f.clock.Now().Add(-3*time.Hour))
	seedProgressAt(t, f, f.courseID, thirdLessonID, 15, false, f.clock.Now().Add(-1*time.Hour))
	seedProgressAt(t, f, f.courseID, secondLessonID, 20, false, f.clock.Now().Add(-2*time.Hour))

	resume := requireResume(t, f, "three in-progress Lessons")
	if resume.LessonID != thirdLessonID {
		t.Fatalf("resume lesson = %s, want the most recently watched Lesson %s", resume.LessonID, thirdLessonID)
	}
}

// 4. A completed Lesson is never the pointer, even when it is the most recent activity.
func TestResumeExcludesCompletedLesson(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	secondLessonID := addLearningSectionLesson(t, f, 1, "two")

	// The completed Lesson is deliberately the *newest* activity, so exclusion cannot be an
	// accident of ordering.
	seedProgressAt(t, f, f.courseID, secondLessonID, 30, false, f.clock.Now().Add(-2*time.Hour))
	seedProgressAt(t, f, f.courseID, f.lessonID, 60, true, f.clock.Now().Add(-1*time.Minute))

	resume := requireResume(t, f, "one completed and one in-progress Lesson")
	if resume.LessonID != secondLessonID {
		t.Fatalf("resume lesson = %s, want the incomplete Lesson %s and never the completed one", resume.LessonID, secondLessonID)
	}
}

//  5. When the only activity is a completed Lesson, the pointer moves to the next incomplete
//     Lesson in canonical section/lesson order rather than disappearing.
func TestResumeSelectsNextIncompleteLessonInCanonicalOrder(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	secondLessonID := addLearningSectionLesson(t, f, 1, "two")
	addLearningSectionLesson(t, f, 2, "three")

	seedProgressAt(t, f, f.courseID, f.lessonID, 60, true, f.clock.Now().Add(-5*time.Minute))

	resume := requireResume(t, f, "first Lesson completed, remainder untouched")
	if resume.LessonID != secondLessonID {
		t.Fatalf("resume lesson = %s, want the next incomplete Lesson %s in canonical order", resume.LessonID, secondLessonID)
	}
	if resume.Started {
		t.Fatalf("resume.Started = true for a Lesson never watched; want false")
	}
}

// 6. A fully completed Course yields no pointer at all — no stale "continue" into finished work.
func TestResumeFullyCompletedCourseYieldsNoPointer(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	secondLessonID := addLearningSectionLesson(t, f, 1, "two")

	seedProgressAt(t, f, f.courseID, f.lessonID, 60, true, f.clock.Now().Add(-2*time.Hour))
	seedProgressAt(t, f, f.courseID, secondLessonID, 60, true, f.clock.Now().Add(-1*time.Hour))

	requireNoResume(t, f, "every Lesson completed")
}

// 7. With two active Courses, a part-finished Lesson outranks an untouched Course.
func TestResumeWithTwoActiveCoursesPrefersPartFinishedWork(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	secondCourseID, secondCourseLessonID := seedResumeCourse(t, f, "Second Course")

	seedProgressAt(t, f, secondCourseID, secondCourseLessonID, 12, false, f.clock.Now().Add(-30*time.Minute))

	resume := requireResume(t, f, "two active Courses, one part-finished")
	if resume.CourseID != secondCourseID || resume.LessonID != secondCourseLessonID {
		t.Fatalf("resume = %+v, want the part-finished Course %s", resume, secondCourseID)
	}
	if !resume.Started {
		t.Fatalf("resume.Started = false for part-finished work; want true")
	}
	if resume.CourseTitle != "Second Course" {
		t.Fatalf("resume course title = %q, want %q", resume.CourseTitle, "Second Course")
	}
}

// 8. Between two part-finished Courses, the more recent learning activity wins.
func TestResumeMoreRecentLearningActivityWinsAcrossCourses(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	secondCourseID, secondCourseLessonID := seedResumeCourse(t, f, "Second Course")

	seedProgressAt(t, f, secondCourseID, secondCourseLessonID, 12, false, f.clock.Now().Add(-4*time.Hour))
	seedProgressAt(t, f, f.courseID, f.lessonID, 8, false, f.clock.Now().Add(-15*time.Minute))

	resume := requireResume(t, f, "two part-finished Courses")
	if resume.CourseID != f.courseID || resume.LessonID != f.lessonID {
		t.Fatalf("resume = %+v, want the more recently active Course %s", resume, f.courseID)
	}

	// Reverse the recency and the pointer must follow it.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE progress SET last_watched_at = $1 WHERE course_lesson_identity_id = $2::uuid`,
		f.clock.Now().Add(-1*time.Minute), secondCourseLessonID,
	); err != nil {
		t.Fatalf("moving second Course activity forward: %v", err)
	}
	moved := requireResume(t, f, "second Course now most recently active")
	if moved.CourseID != secondCourseID {
		t.Fatalf("resume = %+v, want the newly most-recent Course %s", moved, secondCourseID)
	}
}

// 9. Expired access keeps its retained history but yields no pointer.
func TestResumeExcludesExpiredEntitlement(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	seedProgressAt(t, f, f.courseID, f.lessonID, 20, false, f.clock.Now().Add(-1*time.Hour))
	requireResume(t, f, "active entitlement before expiry")

	if _, err := f.pool.Exec(context.Background(),
		`UPDATE entitlements SET access_ends_at = $1, original_access_ends_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`,
		f.clock.Now(), f.studentID, f.courseID,
	); err != nil {
		t.Fatalf("expiring entitlement: %v", err)
	}

	requireNoResume(t, f, "expired entitlement")
}

// 10. Revoked access yields no pointer.
func TestResumeExcludesRevokedEntitlement(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	seedProgressAt(t, f, f.courseID, f.lessonID, 20, false, f.clock.Now().Add(-1*time.Hour))
	requireResume(t, f, "active entitlement before revocation")

	if _, err := f.pool.Exec(context.Background(),
		`UPDATE entitlements SET state = 'REVOKED', revoked_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`,
		f.clock.Now(), f.studentID, f.courseID,
	); err != nil {
		t.Fatalf("revoking entitlement: %v", err)
	}

	requireNoResume(t, f, "revoked entitlement")
}

//  11. A Lesson that exists only in an Instructor's unapproved candidate revision can never
//     become a Student's resume target, and the pointer keeps the live revision's titles.
func TestResumeIgnoresLessonsOutsideTheLiveApprovedRevision(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	ctx := context.Background()

	var sectionIdentityID string
	if err := f.pool.QueryRow(ctx, `SELECT section_identity_id::text FROM course_lesson_identities WHERE id = $1::uuid`, f.lessonID).Scan(&sectionIdentityID); err != nil {
		t.Fatalf("resolving section identity: %v", err)
	}

	// A candidate revision that adds a Lesson and renames everything. It is never made live.
	candidateRevisionID, candidateSectionRowID := uuid.NewString(), uuid.NewString()
	candidateLessonIdentityID := uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'DRAFT', 2, 'مسودة', 'Draft Course')`, []any{candidateRevisionID, f.courseID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم مسودة', 'Draft Section', 0)`, []any{candidateSectionRowID, candidateRevisionID, f.courseID, sectionIdentityID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{candidateLessonIdentityID, f.courseID, sectionIdentityID}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس مسودة', 'Draft Lesson', 0)`, []any{uuid.NewString(), candidateSectionRowID, f.courseID, sectionIdentityID, candidateLessonIdentityID}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس معاد تسميته', 'Renamed Lesson', 1)`, []any{uuid.NewString(), candidateSectionRowID, f.courseID, sectionIdentityID, f.lessonID}},
	} {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding candidate revision: %v", err)
		}
	}

	resume := requireResume(t, f, "candidate revision present")
	if resume.LessonID == candidateLessonIdentityID {
		t.Fatalf("resume pointed at a Lesson that exists only in an unapproved candidate revision")
	}
	if resume.LessonID != f.lessonID {
		t.Fatalf("resume lesson = %s, want the live-revision Lesson %s", resume.LessonID, f.lessonID)
	}
	if resume.LessonTitle != "Lesson" || resume.CourseTitle != "Course" {
		t.Fatalf("resume titles = %q / %q, want the live revision's titles, not the candidate's", resume.CourseTitle, resume.LessonTitle)
	}
}

// 12. One Student's Progress can never become another Student's pointer.
func TestResumeIsScopedToTheRequestingStudent(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	ctx := context.Background()
	secondLessonID := addLearningSectionLesson(t, f, 1, "two")

	// Student B is enrolled in the same Course and is deep into the second Lesson.
	otherStudentID := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'Other Student')`, otherStudentID, otherStudentID+"@example.test"); err != nil {
		t.Fatalf("seeding second Student: %v", err)
	}
	var otherEnrollmentID string
	if err := f.pool.QueryRow(ctx, `INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid) RETURNING id::text`, otherStudentID, f.courseID).Scan(&otherEnrollmentID); err != nil {
		t.Fatalf("seeding second Student Enrollment: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at, updated_at)
		VALUES ($1::uuid, $2::uuid, 42, 42, $3, $3)`, otherEnrollmentID, secondLessonID, f.clock.Now().Add(-1*time.Minute)); err != nil {
		t.Fatalf("seeding second Student Progress: %v", err)
	}

	// Student A has watched nothing, so A's pointer must be A's own unstarted first Lesson.
	resume := requireResume(t, f, "Student A with another Student's Progress present")
	if resume.LessonID != f.lessonID {
		t.Fatalf("resume lesson = %s, want Student A's own first Lesson %s and never Student B's %s", resume.LessonID, f.lessonID, secondLessonID)
	}
	if resume.Started {
		t.Fatalf("resume.Started = true, but Student A has watched nothing — another Student's Progress leaked into the pointer")
	}
}
