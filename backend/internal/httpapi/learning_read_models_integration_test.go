//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (f learningIntegrationFixture) requestWithHeaders(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

func addLearningSectionLesson(t *testing.T, f learningIntegrationFixture, position int, title string) string {
	t.Helper()
	ctx := context.Background()
	var revisionID string
	if err := f.pool.QueryRow(ctx, `SELECT live_revision_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&revisionID); err != nil {
		t.Fatalf("reading live revision: %v", err)
	}
	sectionIdentityID, sectionRowID, lessonID, lessonRowID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentityID, f.courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{lessonID, f.courseID, sectionIdentityID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7)`, []any{sectionRowID, revisionID, f.courseID, sectionIdentityID, "قسم " + title, "Section " + title, position}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7, 0)`, []any{lessonRowID, sectionRowID, f.courseID, sectionIdentityID, lessonID, "درس " + title, "Lesson " + title}},
	}
	for _, statement := range statements {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding section/lesson %s: %v", title, err)
		}
	}
	return lessonID
}

func seedLearningProgress(t *testing.T, f learningIntegrationFixture, lessonID string, position float64, completed bool) {
	t.Helper()
	enrollment, err := f.repository.EnrollmentForLesson(context.Background(), f.studentID, f.lessonID)
	if err != nil {
		t.Fatalf("resolving enrollment for progress seed: %v", err)
	}
	var completedAt any
	var versionID any
	if completed {
		completedAt = f.clock.Now()
		versionID = f.versionID
	}
	_, err = f.pool.Exec(context.Background(), `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, completed_at, completing_asset_version_id, last_watched_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, $3, $4, $5::uuid, $6, $6)`, enrollment.ID, lessonID, position, completedAt, versionID, f.clock.Now())
	if err != nil {
		t.Fatalf("seeding progress: %v", err)
	}
}

func assertJSONKeys(t *testing.T, body []byte, want []string) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatalf("invalid JSON: %v; body=%s", err, body)
	}
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)
	wantCopy := append([]string(nil), want...)
	sort.Strings(wantCopy)
	if !reflect.DeepEqual(got, wantCopy) {
		t.Fatalf("JSON keys = %v, want %v", got, wantCopy)
	}
	return object
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func assertReadSuccess(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("read response = status %d headers=%v body=%s, want 200 JSON no-store", response.Code, response.Header(), response.Body.String())
	}
}

func TestLearningReadModelsMatchD063SchemasAndStableProgress(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	secondLessonID := addLearningSectionLesson(t, f, 1, "two")
	thirdLessonID := addLearningSectionLesson(t, f, 2, "three")
	seedLearningProgress(t, f, f.lessonID, 60, true)
	seedLearningProgress(t, f, secondLessonID, 10, false)
	before := f.authoritySnapshot(t)

	f.queries.reset()
	home := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "", map[string]string{"Accept-Language": "en"})
	assertReadSuccess(t, home)
	if f.queries.get("learning.graph") != 2 || f.queries.get("learning.course-progress") != 1 || f.queries.get("learning.lesson-progress") != 0 || f.queries.get("learning.enrollment") != 1 {
		t.Fatalf("Course Home query counts = graph:%d course-progress:%d lesson-progress:%d enrollment:%d, want 2/1/0/1", f.queries.get("learning.graph"), f.queries.get("learning.course-progress"), f.queries.get("learning.lesson-progress"), f.queries.get("learning.enrollment"))
	}
	// An active Course Home carries the opaque COURSE report context (D-065). It is the only new
	// public key, and it exposes no internal identity.
	homeObject := assertJSONKeys(t, home.Body.Bytes(), []string{"course_id", "expires_at", "learning_status", "progress", "report_context", "sections", "title"})
	var homeProgress struct {
		CompletedLessons int `json:"completed_lessons"`
		TotalLessons     int `json:"total_lessons"`
		Percent          int `json:"percent"`
	}
	if err := json.Unmarshal(homeObject["progress"], &homeProgress); err != nil {
		t.Fatal(err)
	}
	if homeProgress != (struct {
		CompletedLessons int `json:"completed_lessons"`
		TotalLessons     int `json:"total_lessons"`
		Percent          int `json:"percent"`
	}{1, 3, 33}) {
		t.Fatalf("course progress = %+v, want one of three and floor 33%%", homeProgress)
	}
	var sections []struct {
		SectionID string `json:"section_id"`
		Lessons   []struct {
			LessonID string `json:"lesson_id"`
			Title    string `json:"title"`
			Progress struct {
				Position float64 `json:"position_seconds"`
				Done     bool    `json:"completed"`
			} `json:"progress"`
		} `json:"lessons"`
	}
	if err := json.Unmarshal(homeObject["sections"], &sections); err != nil {
		t.Fatal(err)
	}
	if len(sections) != 3 || len(sections[0].Lessons) != 1 || sections[0].Lessons[0].LessonID != f.lessonID || sections[0].Lessons[0].Progress.Position != 60 || !sections[0].Lessons[0].Progress.Done || sections[1].Lessons[0].LessonID != secondLessonID || sections[2].Lessons[0].LessonID != thirdLessonID {
		t.Fatalf("authored Course Home graph/progress = %+v", sections)
	}
	var rawSections []map[string]json.RawMessage
	if err := json.Unmarshal(homeObject["sections"], &rawSections); err != nil {
		t.Fatal(err)
	}
	for _, rawSection := range rawSections {
		section := assertJSONKeys(t, mustJSON(t, rawSection), []string{"section_id", "title", "lessons"})
		var rawLessons []map[string]json.RawMessage
		if err := json.Unmarshal(section["lessons"], &rawLessons); err != nil {
			t.Fatal(err)
		}
		for _, rawLesson := range rawLessons {
			lesson := assertJSONKeys(t, mustJSON(t, rawLesson), []string{"lab_materials", "lesson_id", "progress", "resources", "title"})
			if string(lesson["resources"]) != "[]" || string(lesson["lab_materials"]) != "[]" {
				t.Fatalf("materials for fixture without files = resources:%s labs:%s, want empty lists", lesson["resources"], lesson["lab_materials"])
			}
			assertJSONKeys(t, lesson["progress"], []string{"completed", "position_seconds"})
		}
	}
	if string(homeObject["title"]) != `"Course"` || string(homeObject["learning_status"]) != `"active"` {
		t.Fatalf("Course Home presentation = title %s status %s", homeObject["title"], homeObject["learning_status"])
	}

	lessonPaths := []string{f.lessonID, secondLessonID, thirdLessonID}
	for index, lessonID := range lessonPaths {
		f.queries.reset()
		response := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+lessonID, "", map[string]string{"Accept-Language": "en"})
		assertReadSuccess(t, response)
		if f.queries.get("learning.graph") != 2 || f.queries.get("learning.lesson-progress") != 1 || f.queries.get("learning.course-progress") != 0 || f.queries.get("learning.enrollment") != 1 {
			t.Fatalf("Lesson query counts = graph:%d lesson-progress:%d course-progress:%d enrollment:%d, want 2/1/0/1", f.queries.get("learning.graph"), f.queries.get("learning.lesson-progress"), f.queries.get("learning.course-progress"), f.queries.get("learning.enrollment"))
		}
		object := assertJSONKeys(t, response.Body.Bytes(), []string{"course_id", "expires_at", "lab_materials", "learning_status", "lesson_id", "navigation", "progress", "report_contexts", "resources", "section", "title"})
		if string(object["resources"]) != "[]" || string(object["lab_materials"]) != "[]" {
			t.Fatalf("materials for fixture without files = resources:%s labs:%s, want empty lists", object["resources"], object["lab_materials"])
		}
		var navigation struct {
			Previous *string `json:"previous_lesson_id"`
			Next     *string `json:"next_lesson_id"`
		}
		if err := json.Unmarshal(object["navigation"], &navigation); err != nil {
			t.Fatal(err)
		}
		if index == 0 && navigation.Previous != nil || index > 0 && (navigation.Previous == nil || *navigation.Previous != lessonPaths[index-1]) || index == len(lessonPaths)-1 && navigation.Next != nil || index < len(lessonPaths)-1 && (navigation.Next == nil || *navigation.Next != lessonPaths[index+1]) {
			t.Fatalf("lesson %s navigation = %+v", lessonID, navigation)
		}
		if string(object["lesson_id"]) != `"`+lessonID+`"` {
			t.Fatalf("lesson identity = %s, want %s", object["lesson_id"], lessonID)
		}
		assertJSONKeys(t, object["section"], []string{"section_id", "title"})
		assertJSONKeys(t, object["progress"], []string{"completed", "position_seconds"})
	}
	if after := f.authoritySnapshot(t); before != after {
		t.Fatalf("read routes mutated authority: before=%+v after=%+v", before, after)
	}
}

func TestLearningDashboardScopesOrdersAndRetainsExpiry(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	seedLearningProgress(t, f, f.lessonID, 30, false)
	if _, err := f.pool.Exec(context.Background(), `UPDATE enrollments SET created_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.clock.Now().Add(-time.Hour), f.studentID, f.courseID); err != nil {
		t.Fatalf("setting enrollment order: %v", err)
	}
	otherCourseID := uuid.NewString()
	var ownerID string
	if err := f.pool.QueryRow(context.Background(), `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&ownerID); err != nil {
		t.Fatalf("reading course owner: %v", err)
	}
	otherRevisionID, otherSectionID, otherSectionRowID, otherLessonID, otherLessonRowID, invID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, []any{otherCourseID, ownerID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, 'دورة ثانية', 'Second Course')`, []any{otherRevisionID, otherCourseID}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{otherSectionID, otherCourseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{otherLessonID, otherCourseID, otherSectionID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم ثانية', 'Second Section', 0)`, []any{otherSectionRowID, otherRevisionID, otherCourseID, otherSectionID}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس ثانية', 'Second Lesson', 0)`, []any{otherLessonRowID, otherSectionRowID, otherCourseID, otherSectionID, otherLessonID}},
		{`UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`, []any{otherRevisionID, otherCourseID}},
		{`INSERT INTO enrollments (student_account_id, course_id, created_at) VALUES ($1::uuid, $2::uuid, $3)`, []any{f.studentID, otherCourseID, f.clock.Now().Add(-2 * time.Hour)}},
		{`INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state) VALUES ($1::uuid, $2::uuid, 'student@example.com', 'student@example.com', $3::uuid, $4::uuid, $3::uuid, 'APPROVED')`, []any{invID, otherCourseID, ownerID, f.studentID}},
		{`INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, $4, $4, $5, 'ACTIVE')`, []any{f.studentID, otherCourseID, invID, f.clock.Now().Add(time.Hour), f.clock.Now().Add(-time.Hour)}},
	} {
		if _, err := f.pool.Exec(context.Background(), statement.query, statement.args...); err != nil {
			t.Fatalf("seeding dashboard Course: %v", err)
		}
	}
	otherStudentID := uuid.NewString()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'Other Student')`, otherStudentID, otherStudentID+"@example.test"); err != nil {
		t.Fatalf("seeding second Student: %v", err)
	}
	var otherEnrollmentID string
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid) RETURNING id::text`, otherStudentID, f.courseID).Scan(&otherEnrollmentID); err != nil {
		t.Fatalf("seeding second Student Enrollment: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, completed_at, completing_asset_version_id) VALUES ($1::uuid, $2::uuid, 60, 60, $3, $4::uuid)`, otherEnrollmentID, f.lessonID, f.clock.Now(), f.versionID); err != nil {
		t.Fatalf("seeding second Student Progress: %v", err)
	}
	f.queries.reset()
	response := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/dashboard", "", map[string]string{"Accept-Language": "en"})
	assertReadSuccess(t, response)
	if f.queries.get("learning.dashboard") != 1 || f.queries.get("entitlement.dashboard") != 1 || f.queries.get("learning.enrollment") != 0 || f.queries.get("learning.lesson-progress") != 0 {
		t.Fatalf("Dashboard query counts = learning:%d entitlement:%d enrollment:%d lesson-progress:%d, want 1/1/0/0", f.queries.get("learning.dashboard"), f.queries.get("entitlement.dashboard"), f.queries.get("learning.enrollment"), f.queries.get("learning.lesson-progress"))
	}
	// The resume pointer costs exactly one bounded query for the whole Dashboard, whatever the
	// number of Courses or Lessons. Two Courses are enrolled here, so a per-Course lookup would
	// show up as 2 and this is what stops the pointer becoming an N+1.
	if f.queries.get("learning.resume") != 1 {
		t.Fatalf("resume query count = %d, want exactly 1 for the whole Dashboard", f.queries.get("learning.resume"))
	}
	// The Student has a part-finished Lesson, so the Dashboard carries the resume pointer
	// alongside the Course list. Both keys are pinned exactly: `resume` is omitted entirely when
	// there is nothing to continue, which the empty-Dashboard assertion at the end of this test
	// still holds to `{"courses":[]}`.
	object := assertJSONKeys(t, response.Body.Bytes(), []string{"courses", "resume"})
	resume := assertJSONKeys(t, object["resume"], []string{"course_id", "course_title", "lesson_id", "lesson_title", "started"})
	var resumePointer dashboardResumeResponse
	if err := json.Unmarshal(object["resume"], &resumePointer); err != nil {
		t.Fatal(err)
	}
	if resumePointer.CourseID != f.courseID || resumePointer.LessonID != f.lessonID ||
		resumePointer.CourseTitle != "Course" || resumePointer.LessonTitle != "Lesson" || !resumePointer.Started {
		t.Fatalf("resume pointer = %+v, want the part-finished Lesson of the entitled Course, started", resumePointer)
	}
	if string(resume["started"]) != "true" {
		t.Fatalf("resume started = %s, want true for a Lesson with recorded watch time", resume["started"])
	}
	var courses []struct {
		CourseID string `json:"course_id"`
		Title    string `json:"title"`
		Status   string `json:"learning_status"`
		Progress struct {
			Completed int `json:"completed_lessons"`
			Total     int `json:"total_lessons"`
			Percent   int `json:"percent"`
		} `json:"progress"`
	}
	if err := json.Unmarshal(object["courses"], &courses); err != nil {
		t.Fatal(err)
	}
	if len(courses) != 2 || courses[0].CourseID != f.courseID || courses[1].CourseID != otherCourseID || courses[0].Progress.Percent != 0 || courses[0].Progress.Total != 1 || courses[0].Title != "Course" || courses[1].Title != "Second Course" {
		t.Fatalf("dashboard courses = %+v, want enrollment order and Student-scoped progress", courses)
	}
	var rawCourses []map[string]json.RawMessage
	if err := json.Unmarshal(object["courses"], &rawCourses); err != nil {
		t.Fatal(err)
	}
	for _, rawCourse := range rawCourses {
		course := assertJSONKeys(t, mustJSON(t, rawCourse), []string{"course_id", "expires_at", "learning_status", "progress", "title"})
		assertJSONKeys(t, course["progress"], []string{"completed_lessons", "percent", "total_lessons"})
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET access_ends_at = $1, original_access_ends_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.clock.Now(), f.studentID, f.courseID); err != nil {
		t.Fatalf("expiring dashboard Course: %v", err)
	}
	response = f.request(http.MethodGet, "/api/v1/learn/dashboard", "")
	assertReadSuccess(t, response)
	if !strings.Contains(response.Body.String(), `"learning_status":"expired"`) || !strings.Contains(response.Body.String(), f.courseID) {
		t.Fatalf("dashboard retained expiry = %s", response.Body.String())
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET state = 'REVOKED', revoked_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.clock.Now(), f.studentID, f.courseID); err != nil {
		t.Fatalf("revoking dashboard Course: %v", err)
	}
	response = f.request(http.MethodGet, "/api/v1/learn/dashboard", "")
	assertReadSuccess(t, response)
	var remaining dashboardResponse
	if err := json.Unmarshal(response.Body.Bytes(), &remaining); err != nil || len(remaining.Courses) != 1 || remaining.Courses[0].CourseID != otherCourseID {
		t.Fatalf("revoked Course remained in dashboard: %s", response.Body.String())
	}
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM entitlements WHERE student_account_id = $1::uuid`, f.studentID); err != nil {
		t.Fatalf("removing retained entitlements: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM progress WHERE enrollment_id IN (SELECT id FROM enrollments WHERE student_account_id = $1::uuid)`, f.studentID); err != nil {
		t.Fatalf("removing retained progress: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM enrollments WHERE student_account_id = $1::uuid`, f.studentID); err != nil {
		t.Fatalf("removing retained enrollments: %v", err)
	}
	response = f.request(http.MethodGet, "/api/v1/learn/dashboard", "")
	assertReadSuccess(t, response)
	if response.Body.String() != `{"courses":[]}` {
		t.Fatalf("empty dashboard = %s, want exact empty object", response.Body.String())
	}
}

func TestExpiredReadModelsNeverGrantPlaybackOrProgress(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	seedLearningProgress(t, f, f.lessonID, 20, false)
	expiresAt := f.clock.Now()
	if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET access_ends_at = $1, original_access_ends_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, expiresAt, f.studentID, f.courseID); err != nil {
		t.Fatalf("expiring entitlement: %v", err)
	}
	for _, path := range []string{
		"/api/v1/learn/dashboard",
		"/api/v1/learn/courses/" + f.courseID,
		"/api/v1/learn/courses/" + f.courseID + "/lessons/" + f.lessonID,
	} {
		response := f.request(http.MethodGet, path, "")
		assertReadSuccess(t, response)
		if !strings.Contains(response.Body.String(), `"learning_status":"expired"`) || strings.Contains(response.Body.String(), "entitlement") || strings.Contains(response.Body.String(), "authorized") {
			t.Fatalf("expired read %s = %s", path, response.Body.String())
		}
	}
	playback := f.request(http.MethodPost, "/api/v1/learn/lessons/"+f.lessonID+"/playback", "")
	assertProtectedUnavailable(t, playback)
	progress := f.request(http.MethodPut, "/api/v1/learn/lessons/"+f.lessonID+"/progress", `{"position_seconds":30,"asset_version_id":"`+f.versionID+`"}`)
	assertProtectedUnavailable(t, progress)
}

func TestReadModelsUseCurrentRevisionAndFailClosedForStaleLesson(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	seedLearningProgress(t, f, f.lessonID, 60, true)
	removedLessonID := addLearningSectionLesson(t, f, 1, "removed")
	seedLearningProgress(t, f, removedLessonID, 60, true)
	f.replaceLiveRevision(t)
	response := f.requestWithHeaders(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "", map[string]string{"Accept-Language": "en"})
	assertReadSuccess(t, response)
	if !strings.Contains(response.Body.String(), `"title":"Updated Course"`) || !strings.Contains(response.Body.String(), `"lesson_id":"`+f.lessonID+`"`) || strings.Contains(response.Body.String(), removedLessonID) || !strings.Contains(response.Body.String(), `"completed_lessons":1`) || !strings.Contains(response.Body.String(), `"total_lessons":1`) {
		t.Fatalf("replacement Course Home did not use current live graph: %s", response.Body.String())
	}
	stale := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+uuid.NewString(), "")
	assertProtectedUnavailable(t, stale)
	var rows int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM progress`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("read route changed durable Progress rows: %d", rows)
	}
}

func TestContentUnavailableLessonRemainsReadableWithoutPlayback(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	if _, err := f.pool.Exec(context.Background(), `UPDATE course_lessons SET video_asset_version_id = NULL WHERE lesson_identity_id = $1::uuid`, f.lessonID); err != nil {
		t.Fatalf("removing lesson media binding: %v", err)
	}
	response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+f.lessonID, "")
	assertReadSuccess(t, response)
	if !strings.Contains(response.Body.String(), `"lesson_id":"`+f.lessonID+`"`) || strings.Contains(response.Body.String(), "manifest") || strings.Contains(response.Body.String(), "asset_version") {
		t.Fatalf("content-unavailable Lesson leaked media or became unreachable: %s", response.Body.String())
	}
	home := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "")
	assertReadSuccess(t, home)
	playback := f.request(http.MethodPost, "/api/v1/learn/lessons/"+f.lessonID+"/playback", "")
	assertProtectedUnavailable(t, playback)
}

func TestReadModelsKeepQualifyingDelistedAccessAndDenyEmergencySuspension(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	retiredAt := f.clock.Now()
	if _, err := f.pool.Exec(context.Background(), `UPDATE courses SET lifecycle = 'DELISTED', retired_at = $1 WHERE id = $2::uuid`, retiredAt, f.courseID); err != nil {
		t.Fatalf("delisting Course: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET retirement_eligibility_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, retiredAt.Add(-time.Second), f.studentID, f.courseID); err != nil {
		t.Fatalf("preserving retirement eligibility: %v", err)
	}
	response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "")
	assertReadSuccess(t, response)
	if !strings.Contains(response.Body.String(), `"learning_status":"active"`) {
		t.Fatalf("qualifying delisted Course Home = %s", response.Body.String())
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE courses SET access_suspended_at = $1, access_suspension_reason = 'maintenance' WHERE id = $2::uuid`, f.clock.Now(), f.courseID); err != nil {
		t.Fatalf("suspending Course access: %v", err)
	}
	response = f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "")
	assertProtectedUnavailable(t, response)
	if response.Header().Get("WWW-Authenticate") != "" || strings.Contains(response.Body.String(), "susp") {
		t.Fatalf("emergency denial leaked internal state: headers=%v body=%s", response.Header(), response.Body.String())
	}
}

func TestCourseHomeFailsClosedForIncompleteLiveGraph(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	var revisionID string
	if err := f.pool.QueryRow(context.Background(), `SELECT live_revision_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	sectionIdentityID, sectionRowID := uuid.NewString(), uuid.NewString()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, sectionIdentityID, f.courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم ناقص', 'Incomplete', 1)`, sectionRowID, revisionID, f.courseID, sectionIdentityID); err != nil {
		t.Fatal(err)
	}
	response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "")
	assertProtectedUnavailable(t, response)
}

func TestReadRouteDenialsUseTheSingleProtectedUnavailableResponse(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/learn/courses/" + uuid.NewString()},
		{http.MethodGet, "/api/v1/learn/courses/" + f.courseID + "/lessons/" + uuid.NewString()},
		{http.MethodPost, "/api/v1/learn/lessons/" + uuid.NewString() + "/playback"},
	}
	var baseline *learningWireResponse
	for _, request := range paths {
		response := f.request(request.method, request.path, "")
		wire := assertProtectedUnavailable(t, response)
		if baseline == nil {
			baseline = &wire
		} else {
			assertSameLearningWire(t, *baseline, wire)
		}
	}
}

func TestLessonReadHonorsSectionScopeWhileCourseHomeRequiresCourseScope(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	var sectionID string
	if err := f.pool.QueryRow(context.Background(), `SELECT section_identity_id::text FROM course_lesson_identities WHERE id = $1::uuid`, f.lessonID).Scan(&sectionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET scope_kind = 'SECTION', scope_id = $1::uuid WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, sectionID, f.studentID, f.courseID); err != nil {
		t.Fatal(err)
	}
	courseHome := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "")
	assertProtectedUnavailable(t, courseHome)
	lesson := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+f.lessonID, "")
	assertReadSuccess(t, lesson)
	if !strings.Contains(lesson.Body.String(), `"learning_status":"active"`) {
		t.Fatalf("section-scoped Lesson read = %s", lesson.Body.String())
	}
}
