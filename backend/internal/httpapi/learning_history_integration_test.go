//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/learning"
)

func TestLearningHistoryClassifiesOwnCompletedInProgressExpiredAndRevokedAccess(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	ctx := context.Background()
	if err := f.repository.SaveProgress(ctx, learning.ProgressWrite{
		EnrollmentID: f.enrollmentID(t), CourseLessonIdentityID: f.lessonID,
		PositionSeconds: 90, Completed: true, CompletingAssetVersionID: f.versionID,
	}); err != nil {
		t.Fatalf("completing fixture course: %v", err)
	}
	inProgressCourse := seedHistoryCourse(t, f, f.studentID, "ACTIVE", f.clock.Now().Add(time.Hour), nil)
	expiredCourse := seedHistoryCourse(t, f, f.studentID, "ACTIVE", f.clock.Now().Add(-time.Hour), nil)
	revokedAt := f.clock.Now().Add(-2 * time.Hour)
	revokedCourse := seedHistoryCourse(t, f, f.studentID, "REVOKED", f.clock.Now().Add(time.Hour), &revokedAt)

	otherStudentID := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'Other History Student')`, otherStudentID, otherStudentID+"@example.test"); err != nil {
		t.Fatalf("seeding other history student: %v", err)
	}
	otherCourse := seedHistoryCourse(t, f, otherStudentID, "ACTIVE", f.clock.Now().Add(time.Hour), nil)

	response := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/history", "", map[string]string{"Accept-Language": "en"})
	assertReadSuccess(t, response)
	var history learningHistoryResponse
	if err := json.Unmarshal(response.Body.Bytes(), &history); err != nil {
		t.Fatalf("decoding learning history: %v; body=%s", err, response.Body.String())
	}
	if !hasHistoryCourse(history.Completed, f.courseID) {
		t.Fatalf("completed history = %+v, want fixture course", history.Completed)
	}
	if !hasHistoryCourse(history.InProgress, inProgressCourse) {
		t.Fatalf("in-progress history = %+v, want active unfinished course", history.InProgress)
	}
	ended := historyByID(history.EndedAccess)
	if ended[expiredCourse].AccessEndedReason != "expired" || ended[expiredCourse].AccessEndedAt == nil {
		t.Fatalf("expired history = %+v, want authoritative expiry instant", ended[expiredCourse])
	}
	if ended[revokedCourse].AccessEndedReason != "revoked" || ended[revokedCourse].AccessEndedAt == nil {
		t.Fatalf("revoked history = %+v, want authoritative revoked instant", ended[revokedCourse])
	}
	if hasHistoryCourse(history.Completed, otherCourse) || hasHistoryCourse(history.InProgress, otherCourse) || hasHistoryCourse(history.EndedAccess, otherCourse) {
		t.Fatalf("history exposed another student's course %s: %+v", otherCourse, history)
	}
	completed := historyByID(history.Completed)[f.courseID]
	if completed.Completion == nil || completed.Completion.CompletedAt.IsZero() {
		t.Fatalf("completed history omitted durable completion: %+v", completed)
	}
}

func (f learningIntegrationFixture) enrollmentID(t *testing.T) string {
	t.Helper()
	var enrollmentID string
	if err := f.pool.QueryRow(context.Background(), `
		SELECT id::text FROM enrollments WHERE student_account_id = $1::uuid AND course_id = $2::uuid`, f.studentID, f.courseID).Scan(&enrollmentID); err != nil {
		t.Fatalf("resolving history enrollment: %v", err)
	}
	return enrollmentID
}

func hasHistoryCourse(courses []learningHistoryCourseResponse, courseID string) bool {
	for _, course := range courses {
		if course.CourseID == courseID {
			return true
		}
	}
	return false
}

func historyByID(courses []learningHistoryCourseResponse) map[string]learningHistoryCourseResponse {
	result := make(map[string]learningHistoryCourseResponse, len(courses))
	for _, course := range courses {
		result[course.CourseID] = course
	}
	return result
}

func seedHistoryCourse(t *testing.T, f learningIntegrationFixture, studentID, state string, accessEndsAt time.Time, revokedAt *time.Time) string {
	t.Helper()
	ctx := context.Background()
	var ownerID string
	if err := f.pool.QueryRow(ctx, `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&ownerID); err != nil {
		t.Fatalf("reading history course owner: %v", err)
	}
	courseID, revisionID, sectionIdentityID, lessonID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	sectionRowID, lessonRowID, invitationID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, []any{courseID, ownerID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, 'سجل التعلّم', 'History Course')`, []any{revisionID, courseID}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentityID, courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{lessonID, courseID, sectionIdentityID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم السجل', 'History Section', 0)`, []any{sectionRowID, revisionID, courseID, sectionIdentityID}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس السجل', 'History Lesson', 0)`, []any{lessonRowID, sectionRowID, courseID, sectionIdentityID, lessonID}},
		{`UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid WHERE id = $2::uuid`, []any{revisionID, courseID}},
		{`INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{studentID, courseID}},
		{`INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state) VALUES ($1::uuid, $2::uuid, $3 || '@example.test', $3 || '@example.test', $4::uuid, $5::uuid, $4::uuid, 'APPROVED')`, []any{invitationID, courseID, studentID, ownerID, studentID}},
		{`INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, revoked_at, state)
			VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, $4, $4, $5, $6, $7)`, []any{studentID, courseID, invitationID, accessEndsAt, f.clock.Now().Add(-3 * time.Hour), revokedAt, state}},
	} {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding history course: %v\n%s", err, statement.query)
		}
	}
	return courseID
}
