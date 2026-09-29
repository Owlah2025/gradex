//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func TestAdminUser360ShapesNotesAndAuthorization(t *testing.T) {
	ts, pool, adminID, instructorID, _, _, adminToken, instructorToken := setupAdminPricingAPIServer(t)
	studentID := "10000000-0000-0000-0000-000000000801"
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ($1::uuid, 'user360-student@example.com', 'user360-student@example.com', 'STUDENT', 'ACTIVE', 'User 360 Student', 'en', $2)`, studentID, time.Now().UTC()); err != nil {
		t.Fatalf("seeding User 360 student: %v", err)
	}
	client := ts.Client()

	studentResponse := doPricingRequest(t, client, http.MethodGet,
		ts.URL+"/api/v1/admin/accounts/"+studentID, adminToken, "", adminToken, nil)
	var student struct {
		Identity struct {
			DisplayName string `json:"display_name"`
			Role        string `json:"role"`
		} `json:"identity"`
		Student *struct {
			Courses []any `json:"courses"`
			Notes   []any `json:"notes"`
		} `json:"student"`
		Instructor any `json:"instructor"`
	}
	decodeAdminResponse(t, studentResponse, http.StatusOK, &student)
	if student.Identity.DisplayName != "User 360 Student" || student.Identity.Role != "STUDENT" || student.Student == nil || student.Instructor != nil {
		t.Fatalf("student User 360 shape = %+v", student)
	}

	instructorResponse := doPricingRequest(t, client, http.MethodGet,
		ts.URL+"/api/v1/admin/accounts/"+instructorID, adminToken, "", adminToken, nil)
	var instructor struct {
		Identity struct {
			Role string `json:"role"`
		} `json:"identity"`
		Student    any `json:"student"`
		Instructor *struct {
			OwnedCourses []any `json:"owned_courses"`
		} `json:"instructor"`
	}
	decodeAdminResponse(t, instructorResponse, http.StatusOK, &instructor)
	if instructor.Identity.Role != "INSTRUCTOR" || instructor.Instructor == nil || instructor.Student != nil {
		t.Fatalf("instructor User 360 shape = %+v", instructor)
	}

	noteResponse := doPricingRequest(t, client, http.MethodPost,
		ts.URL+"/api/v1/admin/accounts/"+studentID+"/notes", adminToken,
		"https://gradex.example", adminToken, []byte(`{"body":"Review access history before granting."}`))
	var note struct {
		Body string `json:"body"`
	}
	decodeAdminResponse(t, noteResponse, http.StatusCreated, &note)
	if note.Body != "Review access history before granting." {
		t.Fatalf("created note = %+v", note)
	}

	var noteID string
	if err := pool.QueryRow(context.Background(), `SELECT id::text FROM admin_notes WHERE subject_account_id = $1::uuid`, studentID).Scan(&noteID); err != nil {
		t.Fatalf("reading note id: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE admin_notes SET body = 'changed' WHERE id = $1::uuid`, noteID); err == nil {
		t.Fatal("admin note update unexpectedly succeeded")
	}
	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'ADMIN_NOTE_ADDED' AND target_id = $1`, studentID).Scan(&auditCount); err != nil {
		t.Fatalf("counting note audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("note audit count = %d", auditCount)
	}

	denied := doPricingRequest(t, client, http.MethodGet,
		ts.URL+"/api/v1/admin/accounts/"+studentID, instructorToken, "", instructorToken, nil)
	defer denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("instructor User 360 status = %d, want 403", denied.StatusCode)
	}
	for _, path := range []string{
		"/api/v1/admin/accounts/" + studentID + "/notes",
		"/api/v1/admin/accounts/" + studentID + "/security-events",
		"/api/v1/admin/accounts/" + studentID + "/course-options",
		"/api/v1/admin/accounts/" + studentID + "/access-diagnostics?courseId=" + "00000000-0000-0000-0000-000000000001",
	} {
		response := doPricingRequest(t, client, http.MethodGet, ts.URL+path, instructorToken, "", instructorToken, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("instructor route %s status = %d, want 403", path, response.StatusCode)
		}
	}
	for _, path := range []string{
		"/api/v1/admin/accounts/" + studentID + "/notes",
		"/api/v1/admin/accounts/" + studentID + "/session-revocations",
	} {
		response := doPricingRequest(t, client, http.MethodPost, ts.URL+path, instructorToken, "https://gradex.example", instructorToken, []byte(`{"body":"not allowed","reason":"not allowed"}`))
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("instructor mutation %s status = %d, want 403", path, response.StatusCode)
		}
	}

	var viewedCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'ADMIN_USER_VIEWED' AND actor_account_id = $1::uuid AND target_id = $2`, adminID, studentID).Scan(&viewedCount); err != nil {
		t.Fatalf("counting User 360 audit: %v", err)
	}
	if viewedCount != 1 {
		t.Fatalf("User 360 audit count = %d, want 1", viewedCount)
	}
}

func TestAdminSessionRevocationBumpsEpochAndRefusesSelf(t *testing.T) {
	ts, pool, adminID, _, _, _, adminToken, _ := setupAdminPricingAPIServer(t)
	targetID := "10000000-0000-0000-0000-000000000811"
	sessionID := "10000000-0000-0000-0000-000000000812"
	now := time.Now().UTC()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ($1::uuid, 'session-target@example.com', 'session-target@example.com', 'STUDENT', 'ACTIVE', 'Session Target', 'en', $2)`, targetID, now); err != nil {
		t.Fatalf("seeding session target: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO sessions (id, account_id, admitted_epoch, authenticated_at, last_activity_at, idle_expires_at, absolute_expires_at)
		VALUES ($1::uuid, $2::uuid, 1, $3::timestamptz, $3::timestamptz, $3::timestamptz + interval '30 minutes', $3::timestamptz + interval '12 hours')`, sessionID, targetID, now); err != nil {
		t.Fatalf("seeding target session: %v", err)
	}
	missingReason := doPricingRequest(t, ts.Client(), http.MethodPost,
		ts.URL+"/api/v1/admin/accounts/"+targetID+"/session-revocations", adminToken,
		"https://gradex.example", adminToken, []byte(`{"reason":""}`))
	missingReason.Body.Close()
	if missingReason.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("missing revocation reason status = %d, want 422", missingReason.StatusCode)
	}
	response := doPricingRequest(t, ts.Client(), http.MethodPost,
		ts.URL+"/api/v1/admin/accounts/"+targetID+"/session-revocations", adminToken,
		"https://gradex.example", adminToken, []byte(`{"reason":"Investigated suspicious sign-in"}`))
	var result struct {
		Epoch   int `json:"epoch"`
		Revoked int `json:"revoked_session_count"`
	}
	decodeAdminResponse(t, response, http.StatusOK, &result)
	if result.Epoch != 2 || result.Revoked != 1 {
		t.Fatalf("session revocation result = %+v", result)
	}
	var state, reason string
	if err := pool.QueryRow(context.Background(), `SELECT state::text, revocation_reason::text FROM sessions WHERE id = $1::uuid`, sessionID).Scan(&state, &reason); err != nil {
		t.Fatalf("reading revoked session: %v", err)
	}
	if state != "REVOKED" || reason != "ADMIN_REVOKED" {
		t.Fatalf("revoked session facts = %s/%s", state, reason)
	}
	var eventCount, auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM identity_security_events WHERE event_type = 'ADMIN_SESSIONS_REVOKED' AND account_id = $1::uuid`, targetID).Scan(&eventCount); err != nil {
		t.Fatalf("counting session security event: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'ADMIN_SESSIONS_REVOKED' AND target_id = $1`, targetID).Scan(&auditCount); err != nil {
		t.Fatalf("counting session audit event: %v", err)
	}
	if eventCount != 1 || auditCount != 1 {
		t.Fatalf("session evidence counts = security %d audit %d", eventCount, auditCount)
	}

	self := doPricingRequest(t, ts.Client(), http.MethodPost,
		ts.URL+"/api/v1/admin/accounts/"+adminID+"/session-revocations", adminToken,
		"https://gradex.example", adminToken, []byte(`{"reason":"self test"}`))
	defer self.Body.Close()
	if self.StatusCode != http.StatusForbidden {
		t.Fatalf("self session revocation status = %d, want 403", self.StatusCode)
	}

	selfUpper := doPricingRequest(t, ts.Client(), http.MethodPost,
		ts.URL+"/api/v1/admin/accounts/"+strings.ToUpper(adminID)+"/session-revocations", adminToken,
		"https://gradex.example", adminToken, []byte(`{"reason":"uppercase self test"}`))
	selfUpper.Body.Close()
	if selfUpper.StatusCode != http.StatusForbidden {
		t.Fatalf("uppercase self session revocation status = %d, want 403", selfUpper.StatusCode)
	}

	missing := doPricingRequest(t, ts.Client(), http.MethodPost,
		ts.URL+"/api/v1/admin/accounts/10000000-0000-0000-0000-000000000815/session-revocations", adminToken,
		"https://gradex.example", adminToken, []byte(`{"reason":"missing account test"}`))
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing account session revocation status = %d, want 404", missing.StatusCode)
	}
}

func TestAdminSessionRevocationRejectsStaleAuthWithDistinctProblem(t *testing.T) {
	ts, pool, _, _, _, _, adminToken, _ := setupAdminPricingAPIServerWithRecentAuthWindow(t, time.Nanosecond)
	targetID := "10000000-0000-0000-0000-000000000813"
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, 'stale-target@example.com', 'stale-target@example.com', 'STUDENT', 'ACTIVE', 'Stale Target')`, targetID); err != nil {
		t.Fatalf("seeding stale-auth target: %v", err)
	}

	response := doPricingRequest(t, ts.Client(), http.MethodPost,
		ts.URL+"/api/v1/admin/accounts/"+targetID+"/session-revocations", adminToken,
		"https://gradex.example", adminToken, []byte(`{"reason":"Stale-auth test"}`))
	var problem struct {
		Code string `json:"code"`
	}
	decodeAdminResponse(t, response, http.StatusForbidden, &problem)
	if problem.Code != "RECENT_AUTHENTICATION_REQUIRED" {
		t.Fatalf("stale session problem code = %q, want RECENT_AUTHENTICATION_REQUIRED", problem.Code)
	}

}

type populatedAdminDeviceReader struct {
	overview identity.AdminDeviceOverview
}

func (r populatedAdminDeviceReader) AdminOverview(context.Context, string, time.Time) (identity.AdminDeviceOverview, error) {
	return r.overview, nil
}

func seedAdminUser360Course(t *testing.T, pool *pgxpool.Pool, ownerID, title string) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	courseID, revisionID := uuid.NewString(), uuid.NewString()
	sectionIdentityID, sectionRowID := uuid.NewString(), uuid.NewString()
	lessonID, lessonRowID := uuid.NewString(), uuid.NewString()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, []any{courseID, ownerID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, $3, $3)`, []any{revisionID, courseID, title}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentityID, courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{lessonID, courseID, sectionIdentityID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $5, 0)`, []any{sectionRowID, revisionID, courseID, sectionIdentityID, title + " section"}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $6, 0)`, []any{lessonRowID, sectionRowID, courseID, sectionIdentityID, lessonID, title + " lesson"}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding User 360 course %s: %v", title, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid WHERE id = $2::uuid`, revisionID, courseID); err != nil {
		t.Fatalf("publishing User 360 course %s: %v", title, err)
	}
	return courseID, lessonID, sectionIdentityID
}

func seedAdminUser360Grant(t *testing.T, pool *pgxpool.Pool, studentID, actorID, courseID string, endsAt, retirementEligibilityAt time.Time, state, accountID string) {
	t.Helper()
	ctx := context.Background()
	invitationID := uuid.NewString()
	revokedAt := any(nil)
	if state == "REVOKED" {
		revokedAt = endsAt.Add(-time.Hour)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_access_invitations (
			id, normalized_email, email, course_id, created_by_account_id,
			decided_by_account_id, accepted_by_account_id, state, external_reference
		) VALUES ($1::uuid, 'populated-student@example.com', 'populated-student@example.com', $2::uuid, $3::uuid, $3::uuid, $4::uuid, 'APPROVED', 'INV-360')`, invitationID, courseID, actorID, studentID); err != nil {
		t.Fatalf("seeding User 360 invitation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO entitlements (
			student_account_id, scope_kind, scope_id, course_id, grant_source,
			source_invitation_id, original_access_ends_at, access_ends_at,
			retirement_eligibility_at, state, revoked_at
		) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, $4, $4, $5, $6, $7)`, studentID, courseID, invitationID, endsAt, retirementEligibilityAt, state, revokedAt); err != nil {
		t.Fatalf("seeding User 360 entitlement for %s: %v", accountID, err)
	}
}

func TestAdminUser360PopulatedViewAndDiagnosticCodes(t *testing.T) {
	deviceNow := time.Now().UTC()
	deviceReader := populatedAdminDeviceReader{overview: identity.AdminDeviceOverview{
		Devices: []identity.AdminDeviceView{{
			ID: uuid.NewString(), Label: "Office browser", BrowserFamily: "Chrome", PlatformFamily: "Linux",
			State: "TRUSTED", FirstSeenAt: deviceNow.Add(-24 * time.Hour), LastActiveAt: deviceNow.Add(-time.Minute),
		}}, DeviceLimit: 2,
	}}
	ts, pool, adminID, instructorID, unpublishedCourseID, _, adminToken, _ := setupAdminPricingAPIServerWithDevices(t, 15*time.Minute, deviceReader)
	ctx := context.Background()
	now := time.Now().UTC()
	studentID := "10000000-0000-0000-0000-000000000814"
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ($1::uuid, 'populated-student@example.com', 'populated-student@example.com', 'STUDENT', 'ACTIVE', 'Populated Student', 'en', $2)`, studentID, now); err != nil {
		t.Fatalf("seeding populated User 360 student: %v", err)
	}

	activeCourse, activeLesson, _ := seedAdminUser360Course(t, pool, instructorID, "Diagnostic Active")
	expiredCourse, _, _ := seedAdminUser360Course(t, pool, instructorID, "Diagnostic Expired")
	revokedCourse, _, _ := seedAdminUser360Course(t, pool, instructorID, "Diagnostic Revoked")
	suspendedCourse, _, _ := seedAdminUser360Course(t, pool, instructorID, "Diagnostic Suspended")
	noGrantCourse, _, _ := seedAdminUser360Course(t, pool, instructorID, "Diagnostic No Grant")
	retiredCourse, _, _ := seedAdminUser360Course(t, pool, instructorID, "Diagnostic Retired")

	activeEnrollmentID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO enrollments (id, student_account_id, course_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, activeEnrollmentID, studentID, activeCourse); err != nil {
		t.Fatalf("seeding active enrollment: %v", err)
	}
	watchedAt := now.Add(-2 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at) VALUES ($1::uuid, $2::uuid, 30, 30, $3)`, activeEnrollmentID, activeLesson, watchedAt); err != nil {
		t.Fatalf("seeding watched progress: %v", err)
	}
	for _, courseID := range []string{expiredCourse, revokedCourse, suspendedCourse, noGrantCourse, retiredCourse} {
		if _, err := pool.Exec(ctx, `INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)`, studentID, courseID); err != nil {
			t.Fatalf("seeding diagnostic enrollment: %v", err)
		}
	}

	seedAdminUser360Grant(t, pool, studentID, adminID, activeCourse, now.Add(24*time.Hour), now.Add(-2*time.Hour), "ACTIVE", "active")
	seedAdminUser360Grant(t, pool, studentID, adminID, expiredCourse, now.Add(-time.Hour), now.Add(-2*time.Hour), "ACTIVE", "expired")
	seedAdminUser360Grant(t, pool, studentID, adminID, revokedCourse, now.Add(24*time.Hour), now.Add(-2*time.Hour), "REVOKED", "revoked")
	seedAdminUser360Grant(t, pool, studentID, adminID, suspendedCourse, now.Add(24*time.Hour), now.Add(-2*time.Hour), "ACTIVE", "course suspended")
	retiredAt := now.Add(-time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE courses SET access_suspended_at = $1, access_suspension_reason = 'integration test suspension' WHERE id = $2::uuid`, now, suspendedCourse); err != nil {
		t.Fatalf("suspending diagnostic course: %v", err)
	}
	seedAdminUser360Grant(t, pool, studentID, adminID, retiredCourse, now.Add(24*time.Hour), retiredAt.Add(-time.Hour), "ACTIVE", "retired")
	if _, err := pool.Exec(ctx, `UPDATE courses SET retired_at = $1 WHERE id = $2::uuid`, retiredAt, retiredCourse); err != nil {
		t.Fatalf("retiring diagnostic course: %v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO purchase_requests (reference_code, course_id, email, normalized_email, requester_account_id, course_title_ar, course_title_en, price_minor_units, currency, state) VALUES ('PR-360', $1::uuid, 'populated-student@example.com', 'populated-student@example.com', $2::uuid, 'طلب', 'Diagnostic Active', 1000, 'KWD', 'WAITING_PAYMENT')`, activeCourse, studentID); err != nil {
		t.Fatalf("seeding User 360 purchase request: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_notes (subject_account_id, author_account_id, body) VALUES ($1::uuid, $2::uuid, 'Seeded investigation note')`, studentID, adminID); err != nil {
		t.Fatalf("seeding User 360 note: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO identity_security_events (event_type, account_id, request_id, evidence) VALUES ('SESSION_CREATED', $1::uuid, 'user360-seed', '{}'::jsonb)`, studentID); err != nil {
		t.Fatalf("seeding User 360 security event: %v", err)
	}

	response := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/accounts/"+studentID, adminToken, "", adminToken, nil)
	var view struct {
		Student *struct {
			Courses []struct {
				LastWatchedAt *time.Time `json:"last_watched_at"`
			} `json:"courses"`
			Entitlements     []map[string]any `json:"entitlements"`
			Invitations      []map[string]any `json:"invitations"`
			PurchaseRequests []map[string]any `json:"purchase_requests"`
			Devices          struct {
				Devices []map[string]any `json:"devices"`
			} `json:"devices"`
			SecurityEvents []map[string]any `json:"security_events"`
			Notes          []map[string]any `json:"notes"`
		} `json:"student"`
	}
	decodeAdminResponse(t, response, http.StatusOK, &view)
	watchedCourse := false
	if view.Student != nil {
		for _, course := range view.Student.Courses {
			watchedCourse = watchedCourse || course.LastWatchedAt != nil
		}
	}
	if view.Student == nil || len(view.Student.Courses) == 0 || !watchedCourse || len(view.Student.Entitlements) < 5 || len(view.Student.Invitations) == 0 || len(view.Student.PurchaseRequests) == 0 || len(view.Student.Devices.Devices) != 1 || len(view.Student.SecurityEvents) == 0 || len(view.Student.Notes) == 0 {
		t.Fatalf("populated User 360 view omitted seeded evidence: courses=%d watched=%v entitlements=%d invitations=%d purchases=%d devices=%d security=%d notes=%d student_nil=%t", len(view.Student.Courses), watchedCourse, len(view.Student.Entitlements), len(view.Student.Invitations), len(view.Student.PurchaseRequests), len(view.Student.Devices.Devices), len(view.Student.SecurityEvents), len(view.Student.Notes), view.Student == nil)
	}

	diagnose := func(courseID string) (string, bool) {
		t.Helper()
		result := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/accounts/"+studentID+"/access-diagnostics?courseId="+courseID, adminToken, "", adminToken, nil)
		var diagnostic struct {
			PrimaryReasonCode string `json:"primary_reason_code"`
			Allowed           bool   `json:"allowed"`
		}
		decodeAdminResponse(t, result, http.StatusOK, &diagnostic)
		return diagnostic.PrimaryReasonCode, diagnostic.Allowed
	}
	cases := []struct {
		name, courseID, want string
		allowed              bool
	}{
		{"unpublished", unpublishedCourseID, "NOT_PUBLISHED", false},
		{"expired", expiredCourse, "EXPIRED", false},
		{"revoked", revokedCourse, "REVOKED", false},
		{"course suspended", suspendedCourse, "COURSE_ACCESS_SUSPENDED", false},
		{"no entitlement", noGrantCourse, "NO_ENTITLEMENT", false},
		{"retired grant remains eligible", retiredCourse, "ACTIVE", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, allowed := diagnose(tc.courseID)
			if got != tc.want || allowed != tc.allowed {
				t.Fatalf("diagnostic = %s/%t, want %s/%t", got, allowed, tc.want, tc.allowed)
			}
		})
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, studentID); err != nil {
		t.Fatalf("suspending diagnostic student: %v", err)
	}
	if got, allowed := diagnose(activeCourse); got != "ACCOUNT_SUSPENDED" || allowed {
		t.Fatalf("suspended account diagnostic = %s/%t, want ACCOUNT_SUSPENDED/false", got, allowed)
	}
}
