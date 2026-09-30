//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func TestT6InstructorDashboardAnalyticsAndAnnouncementOwnership(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedInstructorRoster(t, pool, ctx)
	seedT6AnalyticsCurriculum(t, pool, ctx)

	ownerServer := buildTestRouterWithAccount(t, pool, rosterOwnerID, identity.RoleInstructor, identity.StatusActive)
	otherServer := buildTestRouterWithAccount(t, pool, rosterOtherOwnerID, identity.RoleInstructor, identity.StatusActive)
	studentServer := buildTestRouterWithAccount(t, pool, rosterActiveID, identity.RoleStudent, identity.StatusActive)

	t.Run("dashboard is owner scoped and reports durable metrics", func(t *testing.T) {
		response := makeAuthRequest(t, ownerServer.URL+"/api/v1/instructor/dashboard")
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("dashboard status = %d, want 200", response.StatusCode)
		}
		var body struct {
			Courses []struct {
				CourseID                 string  `json:"course_id"`
				Lifecycle                string  `json:"lifecycle"`
				Enrollments              int     `json:"enrollments"`
				LearningActiveStudents7d int     `json:"learning_active_students_7d"`
				AverageProgress          float64 `json:"average_progress"`
				Completions              int     `json:"completions"`
			} `json:"courses"`
		}
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("decode dashboard: %v", err)
		}
		if len(body.Courses) != 1 || body.Courses[0].CourseID != rosterCourseID {
			t.Fatalf("dashboard courses = %+v, want only owned Course A", body.Courses)
		}
		course := body.Courses[0]
		if course.Lifecycle != "PUBLISHED" || course.Enrollments != 3 || course.LearningActiveStudents7d != 1 || course.AverageProgress != 0 || course.Completions != 1 {
			t.Fatalf("dashboard metrics = %+v, want published/3/1/0/1", course)
		}

		response = makeAuthRequest(t, otherServer.URL+"/api/v1/instructor/dashboard")
		defer response.Body.Close()
		var other struct {
			Courses []struct {
				CourseID string `json:"course_id"`
			} `json:"courses"`
		}
		if err := json.NewDecoder(response.Body).Decode(&other); err != nil {
			t.Fatalf("decode other dashboard: %v", err)
		}
		if len(other.Courses) != 1 || other.Courses[0].CourseID != rosterOtherCourseID {
			t.Fatalf("other dashboard courses = %+v, want only Course B", other.Courses)
		}
	})

	t.Run("analytics is owner scoped, ordered, and watch-time honest", func(t *testing.T) {
		response := makeAuthRequest(t, ownerServer.URL+"/api/v1/courses/"+rosterCourseID+"/analytics")
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("analytics status = %d, want 200: %s", response.StatusCode, body)
		}
		var body struct {
			Enrolled           int                                `json:"enrolled"`
			Started            int                                `json:"started"`
			LearningActive     int                                `json:"learning_active_students_7d"`
			Completed          int                                `json:"completed"`
			WatchTimeCollected bool                               `json:"watch_time_collected"`
			Definitions        map[string]struct{ Ar, En string } `json:"definitions"`
			LessonReach        []struct {
				LessonTitleEn     string  `json:"lesson_title_en"`
				StudentsReached   int     `json:"students_reached"`
				StudentsCompleted int     `json:"students_completed"`
				DropOffPercent    float64 `json:"drop_off_percent"`
			} `json:"lesson_reach"`
		}
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("decode analytics: %v", err)
		}
		if body.Enrolled != 3 || body.Started != 1 || body.LearningActive != 1 || body.Completed != 1 || body.WatchTimeCollected {
			t.Fatalf("analytics totals = %+v, want 3/1/1/1 and no watch time", body)
		}
		if body.Definitions["watch_time"].En == "" || body.Definitions["watch_time"].Ar == "" || len(body.LessonReach) != 3 {
			t.Fatalf("analytics lesson definitions/reach = %+v, want three curriculum lessons", body)
		}
		if body.LessonReach[0].LessonTitleEn != "Lesson One" || body.LessonReach[0].StudentsReached != 1 || body.LessonReach[0].StudentsCompleted != 0 || body.LessonReach[0].DropOffPercent != 0 ||
			body.LessonReach[1].LessonTitleEn != "Lesson Two A" || body.LessonReach[1].StudentsReached != 1 || body.LessonReach[1].DropOffPercent != 0 ||
			body.LessonReach[2].LessonTitleEn != "Lesson Two B" || body.LessonReach[2].StudentsReached != 0 || body.LessonReach[2].DropOffPercent <= 0 {
			t.Fatalf("analytics lesson ordering/reach = %+v, want ordered reach and drop-off", body.LessonReach)
		}

		response = makeAuthRequest(t, otherServer.URL+"/api/v1/courses/"+rosterCourseID+"/analytics")
		defer response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("other analytics status = %d, want 403", response.StatusCode)
		}
	})

	t.Run("announcement validation, publication and ownership", func(t *testing.T) {
		bad, _ := doAuthReq(ownerServer, http.MethodPost, "/api/v1/courses/"+rosterCourseID+"/announcements", []byte(`{"title":"","body":""}`))
		if bad.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("invalid announcement status = %d, want 422", bad.StatusCode)
		}
		created, _ := doAuthReq(ownerServer, http.MethodPost, "/api/v1/courses/"+rosterCourseID+"/announcements", []byte(`{"title":"Welcome","body":"The first lesson is ready."}`))
		if created.StatusCode != http.StatusCreated {
			t.Fatalf("created announcement status = %d, want 201", created.StatusCode)
		}
		list := makeAuthRequest(t, ownerServer.URL+"/api/v1/courses/"+rosterCourseID+"/announcements")
		defer list.Body.Close()
		var announcements struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
			HasMore bool `json:"has_more"`
		}
		if err := json.NewDecoder(list.Body).Decode(&announcements); err != nil {
			t.Fatalf("decode owner announcements: %v", err)
		}
		if list.StatusCode != http.StatusOK || len(announcements.Items) != 1 || announcements.Items[0].Title != "Welcome" || announcements.HasMore {
			t.Fatalf("owner announcements = status %d items %+v", list.StatusCode, announcements)
		}
		draftList := makeAuthRequest(t, otherServer.URL+"/api/v1/courses/"+rosterOtherCourseID+"/announcements")
		defer draftList.Body.Close()
		var draftAnnouncements struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
		}
		if err := json.NewDecoder(draftList.Body).Decode(&draftAnnouncements); err != nil {
			t.Fatalf("decode draft announcements: %v", err)
		}
		if draftList.StatusCode != http.StatusOK || len(draftAnnouncements.Items) != 0 {
			t.Fatalf("draft owner announcements = status %d items %+v, want empty history", draftList.StatusCode, draftAnnouncements)
		}
		draftPost, _ := doAuthReq(otherServer, http.MethodPost, "/api/v1/courses/"+rosterOtherCourseID+"/announcements", []byte(`{"title":"Draft","body":"Not publishable"}`))
		if draftPost.StatusCode != http.StatusConflict {
			t.Fatalf("draft announcement post status = %d, want 409", draftPost.StatusCode)
		}
		forbidden, _ := doAuthReq(otherServer, http.MethodPost, "/api/v1/courses/"+rosterCourseID+"/announcements", []byte(`{"title":"No","body":"No"}`))
		if forbidden.StatusCode != http.StatusForbidden {
			t.Fatalf("other owner announcement status = %d, want 403", forbidden.StatusCode)
		}
		forbiddenList := makeAuthRequest(t, otherServer.URL+"/api/v1/courses/"+rosterCourseID+"/announcements")
		defer forbiddenList.Body.Close()
		if forbiddenList.StatusCode != http.StatusForbidden {
			t.Fatalf("other owner announcement list status = %d, want 403", forbiddenList.StatusCode)
		}
		studentList := makeAuthRequest(t, studentServer.URL+"/api/v1/learn/courses/"+rosterCourseID+"/announcements")
		defer studentList.Body.Close()
		if studentList.StatusCode != http.StatusNotFound {
			t.Fatalf("student route without learning foundation status = %d, want 404", studentList.StatusCode)
		}
	})
}

func TestT6InstructorDashboardAlertsUseCurrentFacts(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedInstructorRoster(t, pool, ctx)
	seedT6DashboardAlertFixtures(t, pool, ctx)
	ownerServer := buildTestRouterWithAccount(t, pool, rosterOwnerID, identity.RoleInstructor, identity.StatusActive)

	response := makeAuthRequest(t, ownerServer.URL+"/api/v1/instructor/dashboard")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("dashboard alert status = %d, want 200", response.StatusCode)
	}
	var body struct {
		Courses []struct {
			CourseID               string `json:"course_id"`
			MediaProcessingFailure int    `json:"media_processing_failures"`
		} `json:"courses"`
		Alerts []struct {
			Kind     string  `json:"kind"`
			CourseID *string `json:"course_id"`
		} `json:"alerts"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode dashboard alerts: %v", err)
	}
	if len(body.Courses) != 3 {
		t.Fatalf("dashboard alert courses = %d, want 3", len(body.Courses))
	}
	byCourse := make(map[string]int, len(body.Courses))
	for _, course := range body.Courses {
		byCourse[course.CourseID] = course.MediaProcessingFailure
	}
	if byCourse[rosterCourseID] != 1 {
		t.Fatalf("current-course media failures = %d, want only referenced failure", byCourse[rosterCourseID])
	}
	if byCourse["79999999-9999-9999-9999-999999999991"] != 1 {
		t.Fatalf("scan-failure course media failures = %d, want referenced scan failure", byCourse["79999999-9999-9999-9999-999999999991"])
	}
	seen := map[string]bool{}
	for _, alert := range body.Alerts {
		seen[alert.Kind] = true
	}
	for _, kind := range []string{"CHANGES_REQUESTED", "AWAITING_REVIEW", "MEDIA_PROCESSING_FAILED", "PROFILE_CHANGES_REQUESTED"} {
		if !seen[kind] {
			t.Errorf("dashboard alerts missing %s: %+v", kind, body.Alerts)
		}
	}
}

func TestT6StudentAnnouncementsAreEntitlementScoped(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x52}, 32))
	if err != nil {
		t.Fatalf("outbox writer: %v", err)
	}
	repository, err := catalog.NewRepository(pool, writer)
	if err != nil {
		t.Fatalf("catalog repository: %v", err)
	}
	f := newLearningIntegrationFixtureWith(t, learningFixtureOptions{pool: pool, announcements: repository})
	var ownerID string
	if err := f.pool.QueryRow(context.Background(), `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&ownerID); err != nil {
		t.Fatalf("course owner: %v", err)
	}
	if _, err := repository.CreateCourseAnnouncement(context.Background(), catalog.CreateAnnouncementRequest{
		CourseID: f.courseID, AuthorAccountID: ownerID, Title: "Welcome", Body: "You are enrolled.", Now: f.clock.Now(),
	}); err != nil {
		t.Fatalf("create entitled announcement: %v", err)
	}
	response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/announcements", "")
	if response.Code != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("entitled student status = %d, want 200: %s", response.Code, body)
	}
	var announcements struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&announcements); err != nil {
		t.Fatalf("decode entitled announcements: %v", err)
	}
	if len(announcements.Items) != 1 || announcements.Items[0].Title != "Welcome" {
		t.Fatalf("entitled announcements = %+v, want one Welcome", announcements)
	}
	if _, err := f.pool.Exec(context.Background(), `
		UPDATE entitlements SET state = 'REVOKED', revoked_at = $2
		WHERE student_account_id = $1::uuid AND course_id = $3::uuid
	`, f.studentID, f.clock.Now(), f.courseID); err != nil {
		t.Fatalf("revoke entitlement: %v", err)
	}
	response = f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/announcements", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("unentitled student status = %d, want 404", response.Code)
	}

	const neverHadAccessID = "9a111111-1111-1111-1111-111111111111"
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, 'never-had-access@example.test', 'never-had-access@example.test', 'STUDENT', 'ACTIVE', 'Never Had Access')
	`, neverHadAccessID); err != nil {
		t.Fatalf("seed never-entitled student: %v", err)
	}
	never := newLearningIntegrationFixtureWith(t, learningFixtureOptions{
		pool: f.pool, studentID: neverHadAccessID, announcements: repository,
	})
	var neverOwnerID string
	if err := never.pool.QueryRow(context.Background(), `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, never.courseID).Scan(&neverOwnerID); err != nil {
		t.Fatalf("never-entitled course owner: %v", err)
	}
	if _, err := repository.CreateCourseAnnouncement(context.Background(), catalog.CreateAnnouncementRequest{
		CourseID: never.courseID, AuthorAccountID: neverOwnerID, Title: "Private update", Body: "Not for this student.", Now: never.clock.Now(),
	}); err != nil {
		t.Fatalf("create never-entitled announcement: %v", err)
	}
	if _, err := never.pool.Exec(context.Background(), `DELETE FROM entitlements WHERE student_account_id = $1::uuid AND course_id = $2::uuid`, never.studentID, never.courseID); err != nil {
		t.Fatalf("remove never-entitled access: %v", err)
	}
	neverResponse := never.request(http.MethodGet, "/api/v1/learn/courses/"+never.courseID+"/announcements", "")
	if neverResponse.Code != http.StatusNotFound {
		t.Fatalf("never-entitled student status = %d, want 404", neverResponse.Code)
	}
}

func seedT6AnalyticsCurriculum(t *testing.T, pool *pgxpool.Pool, ctx context.Context) {
	t.Helper()
	const (
		sectionIdentityID = "8b111111-1111-1111-1111-111111111111"
		firstLessonID     = "8b222222-2222-2222-2222-222222222222"
		secondLessonID    = "8b333333-3333-3333-3333-333333333333"
		sectionRowID      = "8b444444-4444-4444-4444-444444444444"
		firstLessonRowID  = "8b555555-5555-5555-5555-555555555555"
		secondLessonRowID = "8b666666-6666-6666-6666-666666666666"
	)
	var revisionID string
	if err := pool.QueryRow(ctx, `SELECT live_revision_id::text FROM courses WHERE id = $1::uuid`, rosterCourseID).Scan(&revisionID); err != nil {
		t.Fatalf("reading T6 live revision: %v", err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seeding T6 analytics curriculum: %v", err)
		}
	}
	exec(`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, sectionIdentityID, rosterCourseID)
	exec(`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid), ($4::uuid, $2::uuid, $3::uuid)`, firstLessonID, rosterCourseID, sectionIdentityID, secondLessonID)
	exec(`
		INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'الوحدة الثانية', 'Unit Two', 1)
	`, sectionRowID, revisionID, rosterCourseID, sectionIdentityID)
	exec(`
		INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position)
		VALUES
			($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'الدرس الثاني أ', 'Lesson Two A', 0),
			($6::uuid, $2::uuid, $3::uuid, $4::uuid, $7::uuid, 'الدرس الثاني ب', 'Lesson Two B', 1)
	`, firstLessonRowID, sectionRowID, rosterCourseID, sectionIdentityID, firstLessonID, secondLessonRowID, secondLessonID)
	exec(`
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at)
		SELECT id, $2::uuid, 36, 18, now() - interval '90 minutes'
		FROM enrollments
		WHERE student_account_id = $1::uuid AND course_id = $3::uuid
	`, rosterActiveID, firstLessonID, rosterCourseID)
}

func seedT6DashboardAlertFixtures(t *testing.T, pool *pgxpool.Pool, ctx context.Context) {
	t.Helper()
	const (
		changesCourseID = "79999999-9999-9999-9999-999999999990"
		scanCourseID    = "79999999-9999-9999-9999-999999999991"
		changesRevID    = "79999999-9999-9999-9999-999999999992"
		reviewRevID     = "79999999-9999-9999-9999-999999999993"
		processAssetID  = "79999999-9999-9999-9999-999999999994"
		processVersion  = "79999999-9999-9999-9999-999999999995"
		staleAssetID    = "79999999-9999-9999-9999-999999999996"
		staleVersion    = "79999999-9999-9999-9999-999999999997"
		scanAssetID     = "79999999-9999-9999-9999-999999999998"
		scanVersion     = "79999999-9999-9999-9999-999999999999"
	)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seeding dashboard alert fixture: %v", err)
		}
	}
	exec(`
		INSERT INTO courses (id, owner_account_id, lifecycle)
		VALUES ($1::uuid, $3::uuid, 'CHANGES_REQUESTED'), ($2::uuid, $3::uuid, 'PENDING_REVIEW')
	`, changesCourseID, scanCourseID, rosterOwnerID)
	exec(`
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en, review_reason)
		VALUES
			($1::uuid, $3::uuid, 'CHANGES_REQUESTED', 1, 'تعديلات', 'Changes', 'Add a clearer introduction.'),
			($2::uuid, $4::uuid, 'PENDING_REVIEW', 1, 'مراجعة', 'Review', NULL)
	`, changesRevID, reviewRevID, changesCourseID, scanCourseID)
	exec(`
		INSERT INTO instructor_profiles (account_id, publication_state, decision_note)
		VALUES ($1::uuid, 'CHANGES_REQUESTED', 'Add a bilingual headline.')
	`, rosterOwnerID)
	exec(`
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES
			($1::uuid, 'PREVIEW', $4::uuid, $5::uuid, 'PUBLIC_PREVIEW'),
			($2::uuid, 'PREVIEW', $4::uuid, $5::uuid, 'PUBLIC_PREVIEW'),
			($3::uuid, 'PREVIEW', $4::uuid, $6::uuid, 'PUBLIC_PREVIEW')
	`, processAssetID, staleAssetID, scanAssetID, rosterOwnerID, rosterCourseID, scanCourseID)
	exec(`
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes
		)
		VALUES
			($1::uuid, $4::uuid, 'PREVIEW', 'PROCESS_FAILED', 'failed/process', 'v1', 'video/mp4', 1),
			($2::uuid, $5::uuid, 'PREVIEW', 'PROCESS_FAILED', 'failed/stale', 'v1', 'video/mp4', 1),
			($3::uuid, $6::uuid, 'PREVIEW', 'SCAN_FAILED', 'failed/scan', 'v1', 'video/mp4', 1)
	`, processVersion, staleVersion, scanVersion, processAssetID, staleAssetID, scanAssetID)
	exec(`UPDATE course_revisions SET preview_asset_version_id = $2::uuid WHERE id = $1::uuid`, "89999999-9999-9999-9999-999999999999", processVersion)
	exec(`UPDATE course_revisions SET preview_asset_version_id = $2::uuid WHERE id = $1::uuid`, reviewRevID, scanVersion)
}
