//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func TestT6InstructorDashboardAnalyticsAndAnnouncementOwnership(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedInstructorRoster(t, pool, ctx)

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
		if body.Definitions["watch_time"].En == "" || body.Definitions["watch_time"].Ar == "" || len(body.LessonReach) != 1 || body.LessonReach[0].LessonTitleEn != "Lesson One" || body.LessonReach[0].StudentsReached != 1 || body.LessonReach[0].StudentsCompleted != 0 || body.LessonReach[0].DropOffPercent != 0 {
			t.Fatalf("analytics lesson definitions/reach = %+v, want populated ordered lesson", body)
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
		var announcements []struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(list.Body).Decode(&announcements); err != nil {
			t.Fatalf("decode owner announcements: %v", err)
		}
		if list.StatusCode != http.StatusOK || len(announcements) != 1 || announcements[0].Title != "Welcome" {
			t.Fatalf("owner announcements = status %d items %+v", list.StatusCode, announcements)
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
	var announcements []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(response.Body).Decode(&announcements); err != nil {
		t.Fatalf("decode entitled announcements: %v", err)
	}
	if len(announcements) != 1 || announcements[0].Title != "Welcome" {
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
}
