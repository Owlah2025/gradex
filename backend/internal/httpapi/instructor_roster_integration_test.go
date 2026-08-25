//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

const (
	rosterOwnerID       = "71111111-1111-1111-1111-111111111111"
	rosterOtherOwnerID  = "72222222-2222-2222-2222-222222222222"
	rosterAdminID       = "73333333-3333-3333-3333-333333333333"
	rosterActiveID      = "74444444-4444-4444-4444-444444444444"
	rosterExpiredID     = "75555555-5555-5555-5555-555555555555"
	rosterRevokedID     = "76666666-6666-6666-6666-666666666666"
	rosterSuspendedID   = "79999999-9999-9999-9999-999999999999"
	rosterCourseID      = "77777777-7777-7777-7777-777777777777"
	rosterOtherCourseID = "78888888-8888-8888-8888-888888888888"
)

type instructorRosterHTTPResponse struct {
	Items []struct {
		DisplayName     string  `json:"display_name"`
		AccessStatus    string  `json:"access_status"`
		EnrolledAt      string  `json:"enrolled_at"`
		AccessStartedAt *string `json:"access_started_at"`
		AccessEndsAt    *string `json:"access_ends_at"`
	} `json:"items"`
	Page     int  `json:"page"`
	PageSize int  `json:"page_size"`
	HasNext  bool `json:"has_next"`
}

func TestInstructorCourseRosterHTTPAPIRealPostgreSQL(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedInstructorRoster(t, pool, ctx)

	ownerServer := buildTestRouterWithAccount(t, pool, rosterOwnerID, identity.RoleInstructor, identity.StatusActive)
	otherOwnerServer := buildTestRouterWithAccount(t, pool, rosterOtherOwnerID, identity.RoleInstructor, identity.StatusActive)
	studentServer := buildTestRouterWithAccount(t, pool, rosterActiveID, identity.RoleStudent, identity.StatusActive)
	suspendedServer := buildTestRouterWithAccount(t, pool, rosterSuspendedID, identity.RoleInstructor, identity.StatusSuspended)

	t.Run("owner sees archived Course roster with bounded pagination and no sensitive fields", func(t *testing.T) {
		first := rosterRequest(t, ownerServer.URL+"/api/v1/courses/"+rosterCourseID+"/students?page=1&page_size=2")
		if first.StatusCode != http.StatusOK {
			t.Fatalf("owner page 1 status = %d, want 200", first.StatusCode)
		}
		body := readBody(t, first)
		assertRosterDoesNotLeak(t, body)

		var page instructorRosterHTTPResponse
		if err := json.Unmarshal(body, &page); err != nil {
			t.Fatalf("decode page 1: %v", err)
		}
		if page.Page != 1 || page.PageSize != 2 || !page.HasNext {
			t.Fatalf("page metadata = %+v, want page 1 size 2 with next page", page)
		}
		if len(page.Items) != 2 {
			t.Fatalf("page 1 item count = %d, want 2", len(page.Items))
		}
		if page.Items[0].DisplayName != "Active Student" || page.Items[0].AccessStatus != "ACTIVE" {
			t.Fatalf("first row = %+v, want Active Student/ACTIVE", page.Items[0])
		}
		if page.Items[1].DisplayName != "Expired Student" || page.Items[1].AccessStatus != "EXPIRED" {
			t.Fatalf("second row = %+v, want Expired Student/EXPIRED", page.Items[1])
		}
		if page.Items[0].AccessEndsAt == nil || page.Items[0].AccessStartedAt == nil || page.Items[0].EnrolledAt == "" {
			t.Fatal("active row is missing authoritative access or enrollment dates")
		}

		second := rosterRequest(t, ownerServer.URL+"/api/v1/courses/"+rosterCourseID+"/students?page=2&page_size=2")
		if second.StatusCode != http.StatusOK {
			t.Fatalf("owner page 2 status = %d, want 200", second.StatusCode)
		}
		var secondPage instructorRosterHTTPResponse
		if err := json.NewDecoder(second.Body).Decode(&secondPage); err != nil {
			t.Fatalf("decode page 2: %v", err)
		}
		second.Body.Close()
		if secondPage.HasNext || len(secondPage.Items) != 1 {
			t.Fatalf("page 2 metadata/items = %+v, want one final row", secondPage)
		}
		if secondPage.Items[0].DisplayName != "Revoked Student" || secondPage.Items[0].AccessStatus != "REVOKED" {
			t.Fatalf("latest historical row = %+v, want one Revoked Student/REVOKED", secondPage.Items[0])
		}
	})

	t.Run("Course access suspension changes display state without mutating entitlements", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE courses SET access_suspended_at = $2, access_suspension_reason = $3 WHERE id = $1`, rosterCourseID, time.Now().UTC(), "Roster integration proof"); err != nil {
			t.Fatalf("suspending Course access: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `UPDATE courses SET access_suspended_at = NULL WHERE id = $1`, rosterCourseID)
		})

		response := rosterRequest(t, ownerServer.URL+"/api/v1/courses/"+rosterCourseID+"/students?page_size=1")
		var page instructorRosterHTTPResponse
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatalf("decode suspended roster: %v", err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK || len(page.Items) != 1 || page.Items[0].AccessStatus != "SUSPENDED" {
			t.Fatalf("suspended roster = status %d, page %+v, want 200 with SUSPENDED", response.StatusCode, page)
		}
		var entitlementState string
		if err := pool.QueryRow(ctx, `SELECT state FROM entitlements WHERE student_account_id = $1 AND course_id = $2 AND scope_kind = 'COURSE' AND state = 'ACTIVE'`, rosterActiveID, rosterCourseID).Scan(&entitlementState); err != nil {
			t.Fatalf("reading active entitlement after suspension: %v", err)
		}
		if entitlementState != "ACTIVE" {
			t.Fatalf("entitlement state = %q, want ACTIVE", entitlementState)
		}
	})

	t.Run("empty owned Course returns a successful empty page", func(t *testing.T) {
		response := rosterRequest(t, otherOwnerServer.URL+"/api/v1/courses/"+rosterOtherCourseID+"/students")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("empty roster status = %d, want 200", response.StatusCode)
		}
		var page instructorRosterHTTPResponse
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatalf("decode empty roster: %v", err)
		}
		response.Body.Close()
		if page.Items == nil || len(page.Items) != 0 || page.HasNext {
			t.Fatalf("empty roster = %+v, want items [] and no next page", page)
		}
	})

	t.Run("all non-owner and non-Instructor principals receive canonical denial", func(t *testing.T) {
		cases := []struct {
			name   string
			server string
		}{
			{name: "other Instructor requesting Course A", server: otherOwnerServer.URL + "/api/v1/courses/" + rosterCourseID + "/students"},
			{name: "owner requesting Course B", server: ownerServer.URL + "/api/v1/courses/" + rosterOtherCourseID + "/students"},
			{name: "Student", server: studentServer.URL + "/api/v1/courses/" + rosterCourseID + "/students"},
			{name: "suspended Instructor", server: suspendedServer.URL + "/api/v1/courses/" + rosterCourseID + "/students"},
		}
		for _, tc := range cases {
			response := rosterRequest(t, tc.server)
			if response.StatusCode != http.StatusForbidden {
				t.Errorf("%s status = %d, want 403", tc.name, response.StatusCode)
			}
			response.Body.Close()
		}

		anonymous, err := http.Get(ownerServer.URL + "/api/v1/courses/" + rosterCourseID + "/students")
		if err != nil {
			t.Fatalf("anonymous request: %v", err)
		}
		defer anonymous.Body.Close()
		if anonymous.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous status = %d, want 401", anonymous.StatusCode)
		}
	})
}

func seedInstructorRoster(t *testing.T, pool *pgxpool.Pool, ctx context.Context) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	exec := func(query string, args ...any) {
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed roster fixture: %v", err)
		}
	}

	exec(`
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
		VALUES
		  ($1, 'roster-owner@example.test', 'roster-owner@example.test', 'INSTRUCTOR', 'ACTIVE', 'Roster Owner', $8),
		  ($2, 'roster-other@example.test', 'roster-other@example.test', 'INSTRUCTOR', 'ACTIVE', 'Other Instructor', $8),
		  ($3, 'roster-admin@example.test', 'roster-admin@example.test', 'ADMIN', 'ACTIVE', 'Roster Admin', $8),
		  ($4, 'roster-active@example.test', 'roster-active@example.test', 'STUDENT', 'ACTIVE', 'Active Student', $8),
		  ($5, 'roster-expired@example.test', 'roster-expired@example.test', 'STUDENT', 'ACTIVE', 'Expired Student', $8),
		  ($6, 'roster-revoked@example.test', 'roster-revoked@example.test', 'STUDENT', 'ACTIVE', 'Revoked Student', $8),
		  ($7, 'roster-suspended@example.test', 'roster-suspended@example.test', 'INSTRUCTOR', 'SUSPENDED', 'Suspended Instructor', $8)
	`, rosterOwnerID, rosterOtherOwnerID, rosterAdminID, rosterActiveID, rosterExpiredID, rosterRevokedID, rosterSuspendedID, now)
	exec(`
		INSERT INTO courses (id, owner_account_id, lifecycle)
		VALUES ($1, $2, 'ARCHIVED'), ($3, $4, 'DRAFT')
	`, rosterCourseID, rosterOwnerID, rosterOtherCourseID, rosterOtherOwnerID)

	insertInvitation := func(id, email, studentID string, createdAt time.Time) {
		exec(`
			INSERT INTO course_access_invitations (
				id, normalized_email, email, course_id, created_by_account_id,
				decided_by_account_id, accepted_by_account_id, state, created_at, accepted_at, decided_at
			)
			VALUES ($1, $2, $2, $3, $4, $4, $5, 'APPROVED', $6, $6, $6)
		`, id, email, rosterCourseID, rosterAdminID, studentID, createdAt)
	}
	activeInvitationID := "81111111-1111-1111-1111-111111111111"
	expiredInvitationID := "82222222-2222-2222-2222-222222222222"
	oldRevokedInvitationID := "83333333-3333-3333-3333-333333333333"
	latestRevokedInvitationID := "84444444-4444-4444-4444-444444444444"
	insertInvitation(activeInvitationID, "roster-active@example.test", rosterActiveID, now.Add(-3*24*time.Hour))
	insertInvitation(expiredInvitationID, "roster-expired@example.test", rosterExpiredID, now.Add(-2*24*time.Hour))
	insertInvitation(oldRevokedInvitationID, "roster-revoked@example.test", rosterRevokedID, now.Add(-4*24*time.Hour))
	insertInvitation(latestRevokedInvitationID, "roster-revoked@example.test", rosterRevokedID, now.Add(-3*24*time.Hour))

	insertEntitlement := func(id, studentID, invitationID, state string, endsAt, grantedAt time.Time, revokedAt *time.Time, updatedAt time.Time) {
		exec(`
			INSERT INTO entitlements (
				id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id,
				original_access_ends_at, access_ends_at, retirement_eligibility_at, state, revoked_at, created_at, updated_at
			)
			VALUES ($1, $2, 'COURSE', $3, $3, 'MANUAL_INVITATION', $4, $5, $5, $6, $7, $8, $6, $9)
		`, id, studentID, rosterCourseID, invitationID, endsAt, grantedAt, state, revokedAt, updatedAt)
	}
	insertEntitlement(
		"85555555-5555-5555-5555-555555555555", rosterActiveID, activeInvitationID, "ACTIVE",
		now.Add(24*time.Hour), now.Add(-3*24*time.Hour), nil, now.Add(-3*24*time.Hour),
	)
	insertEntitlement(
		"86666666-6666-6666-6666-666666666666", rosterExpiredID, expiredInvitationID, "ACTIVE",
		now.Add(-time.Hour), now.Add(-2*24*time.Hour), nil, now.Add(-2*24*time.Hour),
	)
	oldRevokedAt := now.Add(-2 * time.Hour)
	insertEntitlement(
		"87777777-7777-7777-7777-777777777777", rosterRevokedID, oldRevokedInvitationID, "REVOKED",
		now.Add(48*time.Hour), now.Add(-4*24*time.Hour), &oldRevokedAt, now.Add(-2*time.Hour),
	)
	latestRevokedAt := now.Add(-time.Hour)
	insertEntitlement(
		"88888888-8888-8888-8888-888888888888", rosterRevokedID, latestRevokedInvitationID, "REVOKED",
		now.Add(48*time.Hour), now.Add(-3*24*time.Hour), &latestRevokedAt, now.Add(-time.Hour),
	)

	exec(`
		INSERT INTO enrollments (student_account_id, course_id, created_at)
		VALUES
		  ($1, $4, $5),
		  ($2, $4, $6),
		  ($3, $4, $7)
	`, rosterActiveID, rosterExpiredID, rosterRevokedID, rosterCourseID,
		now.Add(-3*24*time.Hour), now.Add(-2*24*time.Hour), now.Add(-24*time.Hour))
}

func rosterRequest(t *testing.T, url string) *http.Response {
	t.Helper()
	return makeAuthRequest(t, url)
}

func readBody(t *testing.T, response *http.Response) []byte {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return body
}

func assertRosterDoesNotLeak(t *testing.T, body []byte) {
	t.Helper()
	text := string(body)
	for _, forbidden := range []string{
		"password_hash", "phone", "payment", "purchase", "admin_note", "session", "token",
		"security", "student_account_id", "entitlement_id", "course_id", rosterActiveID, rosterExpiredID,
		rosterRevokedID, "active@example", "expired@example", "revoked@example",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("roster response contains forbidden value %q: %s", forbidden, text)
		}
	}
}
