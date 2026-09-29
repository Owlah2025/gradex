//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	adminread "github.com/Owlah2025/gradex/backend/internal/admin"
	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func TestAdminMetricsReadModelsUseStudentWindowsAndGateInbox(t *testing.T) {
	ts, pool, adminID, instructorID, setupCourseID, _, adminToken, instructorToken := setupAdminPricingAPIServer(t)
	ctx := context.Background()
	fixture := seedAdminMetricsFixture(t, pool, adminID, instructorID, setupCourseID)
	logAdminMetricsExplain(t, pool, "signed-in activity", `
		WITH params AS (SELECT now() AS as_of), sign_in_activity AS (
			SELECT
				count(DISTINCT a.id) FILTER (WHERE ise.occurred_at >= date_trunc('day', p.as_of, 'Asia/Kuwait')) AS today,
				count(DISTINCT a.id) FILTER (WHERE ise.occurred_at >= p.as_of - interval '7 days') AS days_7,
				count(DISTINCT a.id) FILTER (WHERE ise.occurred_at >= p.as_of - interval '30 days') AS days_30
			FROM params p
			LEFT JOIN identity_security_events ise
			  ON ise.event_type IN ('SESSION_CREATED', 'SESSION_RENEWED')
			 AND ise.occurred_at >= p.as_of - interval '30 days'
			LEFT JOIN accounts a ON a.id = ise.account_id AND a.role = 'STUDENT'
		)
		SELECT * FROM sign_in_activity`)
	logAdminMetricsExplain(t, pool, "course progress", `
		SELECT e.id, count(DISTINCT cli.id),
		       count(DISTINCT p.course_lesson_identity_id) FILTER (WHERE p.completed_at IS NOT NULL)
		FROM enrollments e
		JOIN accounts a ON a.id = e.student_account_id AND a.role = 'STUDENT'
		JOIN courses c ON c.id = e.course_id
		JOIN course_revisions cr ON cr.id = c.live_revision_id AND cr.state = 'APPROVED'
		JOIN course_sections cs ON cs.revision_id = cr.id
		JOIN course_lessons cl ON cl.section_id = cs.id
		JOIN course_lesson_identities cli ON cli.id = cl.lesson_identity_id
		LEFT JOIN progress p ON p.enrollment_id = e.id AND p.course_lesson_identity_id = cli.id
		GROUP BY e.id`)
	logAdminMetricsExplain(t, pool, "access invitation inbox", `
		SELECT count(*) OVER (), i.id, a.display_name, i.created_at
		FROM course_access_invitations i
		LEFT JOIN accounts a ON a.id = i.accepted_by_account_id
		WHERE i.state = 'PENDING_ADMIN_APPROVAL'
		ORDER BY i.created_at ASC, i.id ASC
		LIMIT 5`)

	response := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/metrics/overview", adminToken, "", adminToken, nil)
	var overview struct {
		Metrics []struct {
			Key           string          `json:"key"`
			Value         json.RawMessage `json:"value"`
			DefinitionKey string          `json:"definition_key"`
		} `json:"metrics"`
	}
	decodeAdminResponse(t, response, http.StatusOK, &overview)
	removedWindow := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/metrics/overview?window=7d", adminToken, "", adminToken, nil)
	defer removedWindow.Body.Close()
	if removedWindow.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("removed overview window status = %d, want 422", removedWindow.StatusCode)
	}
	metrics := make(map[string]json.RawMessage, len(overview.Metrics))
	definitions := make(map[string]string, len(overview.Metrics))
	for _, metric := range overview.Metrics {
		if metric.DefinitionKey == "" {
			t.Fatalf("metric %q has no definition key", metric.Key)
		}
		metrics[metric.Key] = metric.Value
		definitions[metric.Key] = metric.DefinitionKey
	}
	assertMetricInt(t, metrics, "students.total", 4)
	assertMetricInt(t, metrics, "students.active", 2)
	assertMetricInt(t, metrics, "students.pending_verification", 1)
	assertMetricInt(t, metrics, "students.suspended", 1)
	assertMetricInt(t, metrics, "registrations.new_7d", 2)
	assertMetricInt(t, metrics, "registrations.new_30d", 3)
	assertMetricInt(t, metrics, "signed_in_activity.today", 1)
	assertMetricInt(t, metrics, "signed_in_activity.7d", 2)
	assertMetricInt(t, metrics, "signed_in_activity.30d", 2)
	assertMetricInt(t, metrics, "learning_activity.7d", 2)
	assertMetricInt(t, metrics, "learning_activity.30d", 2)
	assertMetricInt(t, metrics, "students.started_lessons", 2)
	assertMetricFloat(t, metrics, "learning.average_course_progress", 75)
	assertMetricInt(t, metrics, "learning.completions", 1)
	assertMetricInt(t, metrics, "courses.draft", 1)
	assertMetricInt(t, metrics, "courses.pending_review", 1)
	assertMetricInt(t, metrics, "courses.revisions_pending_review", 1)
	assertMetricInt(t, metrics, "courses.published", 1)
	assertMetricInt(t, metrics, "enrollments.total", 2)
	assertMetricInt(t, metrics, "enrollments.new_30d", 2)
	assertMetricInt(t, metrics, "instructors.active", 1)
	assertMetricInt(t, metrics, "instructors.with_published_course", 1)
	assertMetricInt(t, metrics, "purchase_requests.waiting_payment", 1)
	assertMetricInt(t, metrics, "access_invitations.pending_admin_approval", 1)
	assertMetricInt(t, metrics, "access_invitations.approved", 1)
	assertMetricInt(t, metrics, "entitlements.manual_invitation.active", 1)
	assertMetricInt(t, metrics, "entitlements.purchase_request.revoked", 1)
	assertMetricInt(t, metrics, "entitlements.bundle_purchase.active", 1)
	assertMetricInt(t, metrics, "entitlements.bundle_purchase.revoked", 0)
	if definition := definitions["courses.revisions_pending_review"]; definition != "courses.revisions_pending_review" {
		t.Fatalf("pending-review revision definition = %q, want courses.revisions_pending_review", definition)
	}
	var demand []struct {
		Students int `json:"students"`
	}
	if err := json.Unmarshal(metrics["subject_demand.top"], &demand); err != nil {
		t.Fatalf("decoding top subject demand: %v", err)
	}
	if len(demand) != 1 || demand[0].Students != 2 {
		t.Fatalf("top subject demand = %+v, want one subject with two students", demand)
	}

	for _, sortCase := range []string{"title", "instructor", "lifecycle", "enrolled", "started", "learning_active_7d", "average_progress", "completed"} {
		for _, direction := range []string{"asc", "desc"} {
			t.Run("course sort "+sortCase+" "+direction, func(t *testing.T) {
				response := doPricingRequest(t, ts.Client(), http.MethodGet,
					ts.URL+"/api/v1/admin/metrics/courses?sort="+sortCase+"&direction="+direction+"&page=1&limit=50",
					adminToken, "", adminToken, nil)
				var page struct {
					Items []adminCourseMetric `json:"items"`
				}
				decodeAdminResponse(t, response, http.StatusOK, &page)
				assertCourseMetricOrder(t, page.Items, sortCase, direction)
			})
		}
	}

	courseResponse := doPricingRequest(t, ts.Client(), http.MethodGet,
		ts.URL+"/api/v1/admin/metrics/courses?sort=average_progress&direction=desc&page=1&limit=10",
		adminToken, "", adminToken, nil)
	var courses struct {
		Items []struct {
			ID               string  `json:"id"`
			Enrolled         int     `json:"enrolled"`
			Started          int     `json:"started"`
			LearningActive7d int     `json:"learning_active_7d"`
			AverageProgress  float64 `json:"average_progress"`
			Completed        int     `json:"completed"`
		} `json:"items"`
	}
	decodeAdminResponse(t, courseResponse, http.StatusOK, &courses)
	var publishedCourse struct {
		Enrolled, Started, LearningActive7d, Completed int
		AverageProgress                                float64
	}
	for _, item := range courses.Items {
		if item.ID == fixture.courseID {
			publishedCourse.Enrolled = item.Enrolled
			publishedCourse.Started = item.Started
			publishedCourse.LearningActive7d = item.LearningActive7d
			publishedCourse.AverageProgress = item.AverageProgress
			publishedCourse.Completed = item.Completed
		}
	}
	if publishedCourse.Enrolled != 2 || publishedCourse.Started != 2 || publishedCourse.LearningActive7d != 2 ||
		publishedCourse.AverageProgress != 75 || publishedCourse.Completed != 1 {
		t.Fatalf("published course metric = %+v", publishedCourse)
	}

	draftResponse := doAdminMetricsLanguageRequest(t, ts.Client(),
		ts.URL+"/api/v1/admin/metrics/courses?sort=title&direction=asc&page=1&limit=50", adminToken, "en")
	var draftCourses struct {
		Items []adminCourseMetric `json:"items"`
	}
	decodeAdminResponse(t, draftResponse, http.StatusOK, &draftCourses)
	var draftCourse adminCourseMetric
	for _, item := range draftCourses.Items {
		if item.ID == fixture.draftCourseID {
			draftCourse = item
		}
	}
	if draftCourse.Title != "Draft Analytics Course" {
		t.Fatalf("draft course title = %q, want latest revision title", draftCourse.Title)
	}

	instructorResponse := doPricingRequest(t, ts.Client(), http.MethodGet,
		ts.URL+"/api/v1/admin/metrics/instructors?sort=name&direction=asc&page=1&limit=10",
		adminToken, "", adminToken, nil)
	var instructors struct {
		Items []struct {
			ID                       string `json:"id"`
			PublishedCourses         int    `json:"published_courses"`
			TotalEnrollments         int    `json:"total_enrollments"`
			LearningActiveStudents7d int    `json:"learning_active_students_7d"`
		} `json:"items"`
	}
	decodeAdminResponse(t, instructorResponse, http.StatusOK, &instructors)
	var instructorMetric struct{ PublishedCourses, TotalEnrollments, LearningActiveStudents7d int }
	for _, item := range instructors.Items {
		if item.ID == instructorID {
			instructorMetric.PublishedCourses = item.PublishedCourses
			instructorMetric.TotalEnrollments = item.TotalEnrollments
			instructorMetric.LearningActiveStudents7d = item.LearningActiveStudents7d
		}
	}
	if instructorMetric.PublishedCourses != 1 || instructorMetric.TotalEnrollments != 2 || instructorMetric.LearningActiveStudents7d != 2 {
		t.Fatalf("instructor metric = %+v", instructorMetric)
	}

	for _, sortCase := range []string{"name", "published_courses", "total_enrollments", "learning_active_students_7d"} {
		for _, direction := range []string{"asc", "desc"} {
			t.Run("instructor sort "+sortCase+" "+direction, func(t *testing.T) {
				response := doPricingRequest(t, ts.Client(), http.MethodGet,
					ts.URL+"/api/v1/admin/metrics/instructors?sort="+sortCase+"&direction="+direction+"&page=1&limit=50",
					adminToken, "", adminToken, nil)
				var page struct {
					Items []adminInstructorMetric `json:"items"`
				}
				decodeAdminResponse(t, response, http.StatusOK, &page)
				assertInstructorMetricOrder(t, page.Items, sortCase, direction)
			})
		}
	}

	inboxResponse := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/inbox?limit=3", adminToken, "", adminToken, nil)
	var inbox struct {
		Sections []struct {
			Key   string `json:"key"`
			Count int    `json:"count"`
			Items []struct {
				Kind     string `json:"kind"`
				Label    string `json:"label"`
				Route    string `json:"route"`
				TargetID string `json:"target_id"`
			} `json:"items"`
		} `json:"sections"`
	}
	decodeAdminResponse(t, inboxResponse, http.StatusOK, &inbox)
	counts := make(map[string]int, len(inbox.Sections))
	for _, section := range inbox.Sections {
		counts[section.Key] = section.Count
		for _, item := range section.Items {
			if strings.Contains(item.Label, fixture.studentID) || strings.Contains(item.Label, fixture.courseID) {
				t.Fatalf("inbox rendered an identifier: %+v", item)
			}
			switch item.Kind {
			case "course_review":
				if item.Route != "/admin/courses/:courseId/review" || item.TargetID == "" {
					t.Fatalf("course review target = %+v, want review route and target id", item)
				}
			case "media_processing_failure":
				if item.Route != "" {
					t.Fatalf("media failure target = %+v, want no dead-end link", item)
				}
			}
		}
	}
	for key, want := range map[string]int{
		"course_review": 1, "purchase_requests": 1, "access_invitations": 1,
		"reported_content": 1, "subject_requests": 1, "media_processing_failures": 2,
	} {
		if counts[key] != want {
			t.Fatalf("inbox %s count = %d, want %d; all counts = %+v", key, counts[key], want, counts)
		}
	}

	englishInboxResponse := doAdminMetricsLanguageRequest(t, ts.Client(), ts.URL+"/api/v1/admin/inbox?limit=3", adminToken, "en")
	var englishInbox struct {
		Sections []struct {
			Key   string `json:"key"`
			Items []struct {
				Label string `json:"label"`
			} `json:"items"`
		} `json:"sections"`
	}
	decodeAdminResponse(t, englishInboxResponse, http.StatusOK, &englishInbox)
	for _, section := range englishInbox.Sections {
		for _, item := range section.Items {
			if strings.TrimSpace(item.Label) == "" {
				t.Fatalf("English inbox item %s has an empty label", section.Key)
			}
		}
	}

	denied := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/metrics/overview", instructorToken, "", instructorToken, nil)
	defer denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("instructor overview status = %d, want 403", denied.StatusCode)
	}
	anonymous := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/inbox", "", "", "", nil)
	defer anonymous.Body.Close()
	if anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous inbox status = %d, want 401", anonymous.StatusCode)
	}

	repository, err := adminread.NewRepository(pool)
	if err != nil {
		t.Fatalf("admin repository: %v", err)
	}
	instructorInbox, err := repository.GetInbox(ctx, adminread.InboxRequest{
		Principal: identity.Principal{AccountID: instructorID, Role: identity.RoleInstructor, Status: identity.StatusActive, CredentialState: identity.CredentialActive},
		Locale:    identity.LocaleEnglish, Limit: 3,
	})
	if err != nil {
		t.Fatalf("instructor inbox read: %v", err)
	}
	if len(instructorInbox.Sections) != 0 {
		t.Fatalf("instructor inbox sections = %+v, want none", instructorInbox.Sections)
	}
}

type adminMetricsFixture struct {
	studentID     string
	courseID      string
	draftCourseID string
}

type adminCourseMetric struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	Instructor       string  `json:"instructor"`
	Lifecycle        string  `json:"lifecycle"`
	Enrolled         int     `json:"enrolled"`
	Started          int     `json:"started"`
	LearningActive7d int     `json:"learning_active_7d"`
	AverageProgress  float64 `json:"average_progress"`
	Completed        int     `json:"completed"`
}

type adminInstructorMetric struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	PublishedCourses         int    `json:"published_courses"`
	TotalEnrollments         int    `json:"total_enrollments"`
	LearningActiveStudents7d int    `json:"learning_active_students_7d"`
}

func assertCourseMetricOrder(t *testing.T, items []adminCourseMetric, sortKey, direction string) {
	t.Helper()
	expected := append([]adminCourseMetric(nil), items...)
	sort.SliceStable(expected, func(i, j int) bool {
		comparison := compareCourseMetrics(expected[i], expected[j], sortKey)
		if comparison == 0 {
			comparison = strings.Compare(expected[i].ID, expected[j].ID)
		}
		if direction == "desc" {
			return comparison > 0
		}
		return comparison < 0
	})
	for index := range items {
		if items[index].ID != expected[index].ID {
			t.Fatalf("%s %s order = %v, want %v", sortKey, direction, courseMetricIDs(items), courseMetricIDs(expected))
		}
	}
}

func compareCourseMetrics(left, right adminCourseMetric, sortKey string) int {
	switch sortKey {
	case "title":
		return strings.Compare(strings.ToLower(left.Title), strings.ToLower(right.Title))
	case "instructor":
		return strings.Compare(strings.ToLower(left.Instructor), strings.ToLower(right.Instructor))
	case "lifecycle":
		return strings.Compare(left.Lifecycle, right.Lifecycle)
	case "enrolled":
		return compareInts(left.Enrolled, right.Enrolled)
	case "started":
		return compareInts(left.Started, right.Started)
	case "learning_active_7d":
		return compareInts(left.LearningActive7d, right.LearningActive7d)
	case "average_progress":
		return compareFloats(left.AverageProgress, right.AverageProgress)
	case "completed":
		return compareInts(left.Completed, right.Completed)
	default:
		return 0
	}
}

func assertInstructorMetricOrder(t *testing.T, items []adminInstructorMetric, sortKey, direction string) {
	t.Helper()
	expected := append([]adminInstructorMetric(nil), items...)
	sort.SliceStable(expected, func(i, j int) bool {
		comparison := compareInstructorMetrics(expected[i], expected[j], sortKey)
		if comparison == 0 {
			comparison = strings.Compare(expected[i].ID, expected[j].ID)
		}
		if direction == "desc" {
			return comparison > 0
		}
		return comparison < 0
	})
	for index := range items {
		if items[index].ID != expected[index].ID {
			t.Fatalf("%s %s order = %v, want %v", sortKey, direction, instructorMetricIDs(items), instructorMetricIDs(expected))
		}
	}
}

func compareInstructorMetrics(left, right adminInstructorMetric, sortKey string) int {
	switch sortKey {
	case "name":
		return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
	case "published_courses":
		return compareInts(left.PublishedCourses, right.PublishedCourses)
	case "total_enrollments":
		return compareInts(left.TotalEnrollments, right.TotalEnrollments)
	case "learning_active_students_7d":
		return compareInts(left.LearningActiveStudents7d, right.LearningActiveStudents7d)
	default:
		return 0
	}
}

func compareInts(left, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareFloats(left, right float64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func courseMetricIDs(items []adminCourseMetric) []string {
	ids := make([]string, len(items))
	for index, item := range items {
		ids[index] = item.ID
	}
	return ids
}

func instructorMetricIDs(items []adminInstructorMetric) []string {
	ids := make([]string, len(items))
	for index, item := range items {
		ids[index] = item.ID
	}
	return ids
}

func seedAdminMetricsFixture(t *testing.T, pool *pgxpool.Pool, adminID, instructorID, setupCourseID string) adminMetricsFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	studentIDs := []string{
		"10000000-0000-0000-0000-000000000901",
		"10000000-0000-0000-0000-000000000902",
		"10000000-0000-0000-0000-000000000903",
		"10000000-0000-0000-0000-000000000904",
	}
	createdAt := []time.Time{
		now.Add(-24 * time.Hour),
		now.Add(-20 * 24 * time.Hour),
		now.Add(-30*24*time.Hour - time.Hour),
		now.Add(-48 * time.Hour),
	}
	statuses := []string{"ACTIVE", "ACTIVE", "PENDING_VERIFICATION", "SUSPENDED"}
	for index, studentID := range studentIDs {
		verifiedAt := any(now)
		if statuses[index] == "PENDING_VERIFICATION" {
			verifiedAt = nil
		}
		mustExecAdminMetrics(t, pool, `
			INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at, created_at)
			VALUES ($1::uuid, $2, $2, 'STUDENT', $3::account_status, $4, 'en', $5, $6)`,
			studentID, fmt.Sprintf("metrics-student-%d@example.com", index), statuses[index], fmt.Sprintf("Metrics Student %d", index+1), verifiedAt, createdAt[index])
	}
	emptyInstructorID := "10000000-0000-0000-0000-000000000938"
	mustExecAdminMetrics(t, pool, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at, created_at)
		VALUES ($1::uuid, 'metrics-empty-instructor@example.com', 'metrics-empty-instructor@example.com', 'INSTRUCTOR', 'PENDING_VERIFICATION', 'Metrics Empty Instructor', 'en', NULL, $2)`,
		emptyInstructorID, now.Add(-48*time.Hour))

	securityEvents := []struct {
		accountID string
		occurred  time.Time
		requestID string
	}{
		{studentIDs[0], now.Add(-time.Hour), "metrics-session-student-a"},
		{studentIDs[1], now.Add(-6 * 24 * time.Hour), "metrics-session-student-b"},
		{instructorID, now.Add(-time.Hour), "metrics-session-instructor"},
	}
	for _, event := range securityEvents {
		mustExecAdminMetrics(t, pool, `
			INSERT INTO identity_security_events (event_type, account_id, request_id, evidence, occurred_at)
			VALUES ('SESSION_CREATED', $1::uuid, $2, '{}'::jsonb, $3)`, event.accountID, event.requestID, event.occurred)
	}

	var setupRevisionID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM course_revisions WHERE course_id = $1::uuid ORDER BY revision_number DESC LIMIT 1`, setupCourseID).Scan(&setupRevisionID); err != nil {
		t.Fatalf("reading setup revision: %v", err)
	}
	mustExecAdminMetrics(t, pool, `UPDATE course_revisions SET state = 'PENDING_REVIEW', submitted_at = $2 WHERE id = $1::uuid`, setupRevisionID, now.Add(-48*time.Hour))
	mustExecAdminMetrics(t, pool, `UPDATE courses SET lifecycle = 'PENDING_REVIEW' WHERE id = $1::uuid`, setupCourseID)

	courseID := "10000000-0000-0000-0000-000000000912"
	revisionID := "10000000-0000-0000-0000-000000000913"
	sectionIdentityID := "10000000-0000-0000-0000-000000000914"
	lessonIdentityOne := "10000000-0000-0000-0000-000000000915"
	lessonIdentityTwo := "10000000-0000-0000-0000-000000000916"
	sectionID := "10000000-0000-0000-0000-000000000917"
	lessonOneID := "10000000-0000-0000-0000-000000000918"
	lessonTwoID := "10000000-0000-0000-0000-000000000919"
	mustExecAdminMetrics(t, pool, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, courseID, instructorID)
	mustExecAdminMetrics(t, pool, `INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, 'مقرر التحليلات', 'Analytics Course')`, revisionID, courseID)
	mustExecAdminMetrics(t, pool, `UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $2::uuid WHERE id = $1::uuid`, courseID, revisionID)
	mustExecAdminMetrics(t, pool, `INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, sectionIdentityID, courseID)
	mustExecAdminMetrics(t, pool, `INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid), ($4::uuid, $2::uuid, $3::uuid)`, lessonIdentityOne, courseID, sectionIdentityID, lessonIdentityTwo)
	mustExecAdminMetrics(t, pool, `INSERT INTO course_sections (id, revision_id, title_ar, title_en, position, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, 'القسم', 'Section', 1, $3::uuid, $4::uuid)`, sectionID, revisionID, courseID, sectionIdentityID)
	mustExecAdminMetrics(t, pool, `INSERT INTO course_lessons (id, section_id, title_ar, title_en, position, course_id, section_identity_id, lesson_identity_id) VALUES ($1::uuid, $2::uuid, 'الدرس الأول', 'Lesson One', 1, $3::uuid, $4::uuid, $5::uuid), ($6::uuid, $2::uuid, 'الدرس الثاني', 'Lesson Two', 2, $3::uuid, $4::uuid, $7::uuid)`, lessonOneID, sectionID, courseID, sectionIdentityID, lessonIdentityOne, lessonTwoID, lessonIdentityTwo)

	enrollmentA := "10000000-0000-0000-0000-000000000920"
	enrollmentB := "10000000-0000-0000-0000-000000000921"
	mustExecAdminMetrics(t, pool, `INSERT INTO enrollments (id, student_account_id, course_id, created_at) VALUES ($1::uuid, $2::uuid, $3::uuid, $4), ($5::uuid, $6::uuid, $3::uuid, $4)`, enrollmentA, studentIDs[0], courseID, now.Add(-48*time.Hour), enrollmentB, studentIDs[1])
	failedVersionID := "10000000-0000-0000-0000-000000000922"
	assetID := "10000000-0000-0000-0000-000000000923"
	mustExecAdminMetrics(t, pool, `INSERT INTO media_assets (id, kind, owner_account_id, course_id) VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid)`, assetID, instructorID, courseID)
	mustExecAdminMetrics(t, pool, `INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, $2::uuid, 'VIDEO', 'PROCESS_FAILED', 'metrics/failed.mp4', 'metrics-v1', 'video/mp4', 1)`, failedVersionID, assetID)

	draftCourseID := "10000000-0000-0000-0000-000000000930"
	draftRevisionID := "10000000-0000-0000-0000-000000000931"
	draftFailedVersionID := "10000000-0000-0000-0000-000000000932"
	draftAssetID := "10000000-0000-0000-0000-000000000933"
	retiredFailedVersionID := "10000000-0000-0000-0000-000000000934"
	retiredAssetID := "10000000-0000-0000-0000-000000000935"
	mustExecAdminMetrics(t, pool, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, draftCourseID, instructorID)
	mustExecAdminMetrics(t, pool, `INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'DRAFT', 1, 'مقرر تحليلات مسودة', 'Draft Analytics Course')`, draftRevisionID, draftCourseID)
	mustExecAdminMetrics(t, pool, `INSERT INTO media_assets (id, kind, owner_account_id, course_id) VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid)`, draftAssetID, instructorID, draftCourseID)
	mustExecAdminMetrics(t, pool, `INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, $2::uuid, 'VIDEO', 'PROCESS_FAILED', 'metrics/draft-failed.mp4', 'metrics-draft-v1', 'video/mp4', 1)`, draftFailedVersionID, draftAssetID)
	mustExecAdminMetrics(t, pool, `INSERT INTO media_assets (id, kind, owner_account_id, course_id, retired_at) VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid, $4)`, retiredAssetID, instructorID, courseID, now.Add(-time.Hour))
	mustExecAdminMetrics(t, pool, `INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, $2::uuid, 'VIDEO', 'PROCESS_FAILED', 'metrics/retired-failed.mp4', 'metrics-retired-v1', 'video/mp4', 1)`, retiredFailedVersionID, retiredAssetID)
	progressRows := []struct {
		enrollmentID string
		lessonID     string
		position     float64
		completedAt  any
		watchedAt    time.Time
	}{
		{enrollmentA, lessonIdentityOne, 100, now.Add(-2 * time.Hour), now.Add(-2 * time.Hour)},
		{enrollmentA, lessonIdentityTwo, 100, now.Add(-2 * time.Hour), now.Add(-2 * time.Hour)},
		{enrollmentB, lessonIdentityOne, 100, now.Add(-6 * 24 * time.Hour), now.Add(-6 * 24 * time.Hour)},
	}
	for _, row := range progressRows {
		mustExecAdminMetrics(t, pool, `
			INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, completed_at, completing_asset_version_id, last_watched_at)
			VALUES ($1::uuid, $2::uuid, $3, $3, $4, $5::uuid, $6)`, row.enrollmentID, row.lessonID, row.position, row.completedAt, failedVersionID, row.watchedAt)
	}

	institutionID := "10000000-0000-0000-0000-000000000910"
	subjectID := "10000000-0000-0000-0000-000000000911"
	mustExecAdminMetrics(t, pool, `INSERT INTO institutions (id, country_code, slug, name_ar, name_en) VALUES ($1::uuid, 'KW', 'metrics-university', 'جامعة التحليلات', 'Metrics University')`, institutionID)
	mustExecAdminMetrics(t, pool, `INSERT INTO subjects (id, institution_id, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'تفاضل', 'Calculus')`, subjectID, institutionID)
	for _, studentID := range studentIDs[:2] {
		mustExecAdminMetrics(t, pool, `INSERT INTO subject_demand_signals (account_id, subject_id, institution_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, studentID, subjectID, institutionID)
	}

	pendingInvitationID := "10000000-0000-0000-0000-000000000924"
	approvedInvitationID := "10000000-0000-0000-0000-000000000925"
	mustExecAdminMetrics(t, pool, `
		INSERT INTO course_access_invitations (id, normalized_email, email, course_id, created_by_account_id, accepted_by_account_id, state, accepted_at, created_at)
		VALUES ($1::uuid, $2, $2, $3::uuid, $4::uuid, $5::uuid, 'PENDING_ADMIN_APPROVAL', $6, $6)`, pendingInvitationID, "metrics-student-1@example.com", courseID, adminID, studentIDs[0], now.Add(-24*time.Hour))
	mustExecAdminMetrics(t, pool, `
		INSERT INTO course_access_invitations (id, normalized_email, email, course_id, created_by_account_id, decided_by_account_id, accepted_by_account_id, state, accepted_at, decided_at, created_at)
		VALUES ($1::uuid, $2, $2, $3::uuid, $4::uuid, $4::uuid, $5::uuid, 'APPROVED', $6, $6, $6)`, approvedInvitationID, "metrics-student-2@example.com", courseID, adminID, studentIDs[1], now.Add(-3*24*time.Hour))
	purchaseID := "10000000-0000-0000-0000-000000000926"
	mustExecAdminMetrics(t, pool, `INSERT INTO purchase_requests (id, reference_code, course_id, email, normalized_email, requester_account_id, course_title_ar, course_title_en, price_minor_units, currency, state, requested_at) VALUES ($1::uuid, 'METRICS-001', $2::uuid, 'metrics-student-1@example.com', 'metrics-student-1@example.com', $3::uuid, 'مقرر التحليلات', 'Analytics Course', 1000, 'KWD', 'WAITING_PAYMENT', $4)`, purchaseID, courseID, studentIDs[0], now.Add(-12*time.Hour))
	mustExecAdminMetrics(t, pool, `INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, $4, $4, $4, 'ACTIVE')`, studentIDs[0], courseID, pendingInvitationID, now.Add(30*24*time.Hour))
	mustExecAdminMetrics(t, pool, `INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, revoked_at, state) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'PURCHASE_REQUEST', $3::uuid, $4, $4, $4, $5, 'REVOKED')`, studentIDs[1], courseID, approvedInvitationID, now.Add(30*24*time.Hour), now.Add(-time.Hour))
	bundleID := "10000000-0000-0000-0000-000000000936"
	bundlePurchaseID := "10000000-0000-0000-0000-000000000937"
	mustExecAdminMetrics(t, pool, `
		INSERT INTO bundles (id, lifecycle, title_ar, title_en, created_by_account_id, updated_by_account_id)
		VALUES ($1::uuid, 'PUBLISHED', 'حزمة التحليلات', 'Analytics Bundle', $2::uuid, $2::uuid)`, bundleID, adminID)
	mustExecAdminMetrics(t, pool, `INSERT INTO bundle_courses (bundle_id, course_id, position) VALUES ($1::uuid, $2::uuid, 0)`, bundleID, courseID)
	mustExecAdminMetrics(t, pool, `
		INSERT INTO purchase_requests (
			id, reference_code, target_kind, bundle_id, bundle_revision, bundle_title_ar, bundle_title_en,
			email, normalized_email, requester_account_id, price_minor_units, currency, state,
			payment_confirmed_by_account_id, payment_confirmed_at, access_granted_at, requested_at
		) VALUES ($1::uuid, 'METRICS-BUNDLE-001', 'BUNDLE', $2::uuid, 1, 'حزمة التحليلات', 'Analytics Bundle',
			'metrics-student-2@example.com', 'metrics-student-2@example.com', $3::uuid, 2000, 'KWD', 'ACCESS_GRANTED',
			$4::uuid, $5, $5, $5)`, bundlePurchaseID, bundleID, studentIDs[1], adminID, now.Add(-2*time.Hour))
	mustExecAdminMetrics(t, pool, `
		INSERT INTO entitlements (
			student_account_id, scope_kind, scope_id, course_id, grant_source, source_purchase_request_id,
			original_access_ends_at, access_ends_at, retirement_eligibility_at, state
		) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'BUNDLE_PURCHASE', $3::uuid, $4, $4, $4, 'ACTIVE')`,
		studentIDs[1], courseID, bundlePurchaseID, now.Add(-time.Hour))
	mustExecAdminMetrics(t, pool, `INSERT INTO content_reports (reporter_account_id, target_kind, target_id, target_revision_ref, reason, created_at) VALUES ($1::uuid, 'COURSE', $2::uuid, $3::uuid, 'broken_unavailable', $4)`, studentIDs[0], courseID, revisionID, now.Add(-18*time.Hour))
	mustExecAdminMetrics(t, pool, `INSERT INTO subject_requests (requester_account_id, institution_id, proposed_title_ar, proposed_title_en, status, created_at, updated_at) VALUES ($1::uuid, $2::uuid, 'إحصاء', 'Statistics', 'PENDING', $3, $3)`, instructorID, institutionID, now.Add(-20*time.Hour))

	return adminMetricsFixture{studentID: studentIDs[0], courseID: courseID, draftCourseID: draftCourseID}
}

func mustExecAdminMetrics(t *testing.T, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("admin metrics fixture query failed: %v", err)
	}
}

func doAdminMetricsLanguageRequest(t *testing.T, client *http.Client, url, token, language string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	request.Header.Set("Accept-Language", language)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token, Secure: true})
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	return response
}

func logAdminMetricsExplain(t *testing.T, pool *pgxpool.Pool, name, query string) {
	t.Helper()
	rows, err := pool.Query(context.Background(), "EXPLAIN (COSTS OFF) "+query)
	if err != nil {
		t.Fatalf("EXPLAIN %s: %v", name, err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scanning EXPLAIN %s: %v", name, err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating EXPLAIN %s: %v", name, err)
	}
	t.Logf("EXPLAIN %s:\n%s", name, plan.String())
}

func assertMetricInt(t *testing.T, metrics map[string]json.RawMessage, key string, want int) {
	t.Helper()
	var got int
	if err := json.Unmarshal(metrics[key], &got); err != nil {
		t.Fatalf("metric %s decode: %v", key, err)
	}
	if got != want {
		t.Fatalf("metric %s = %d, want %d", key, got, want)
	}
}

func assertMetricFloat(t *testing.T, metrics map[string]json.RawMessage, key string, want float64) {
	t.Helper()
	var got float64
	if err := json.Unmarshal(metrics[key], &got); err != nil {
		t.Fatalf("metric %s decode: %v", key, err)
	}
	if got != want {
		t.Fatalf("metric %s = %v, want %v", key, got, want)
	}
}
