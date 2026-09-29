//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"
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
}
