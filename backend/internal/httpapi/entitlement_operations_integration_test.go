//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// The AD07 elevated-Admin operations are proved against the real protected
// learning router: extend, shorten and revoke are only meaningful if the
// Student's playback and progress follow them.

type entitlementOperationsFixture struct {
	learning      learningIntegrationFixture
	repo          *access.Repository
	adminID       string
	entitlementID string
	courseID      string
	studentID     string
}

func newEntitlementOperationsFixture(t *testing.T) entitlementOperationsFixture {
	t.Helper()
	learningFixture := newLearningIntegrationFixture(t)
	ctx := context.Background()

	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}
	repo, err := access.NewRepository(learningFixture.pool, writer)
	if err != nil {
		t.Fatalf("access.NewRepository: %v", err)
	}

	adminID := "0a000000-0000-4000-8000-00000000ad01"
	if _, err := learningFixture.pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, 'ad07-admin@example.test', 'ad07-admin@example.test', 'ADMIN', 'ACTIVE', 'AD07 Admin')
	`, adminID); err != nil {
		t.Fatalf("seeding AD07 admin: %v", err)
	}

	var entitlementID string
	if err := learningFixture.pool.QueryRow(ctx,
		`SELECT id::text FROM entitlements WHERE student_account_id = $1::uuid AND course_id = $2::uuid`,
		learningFixture.studentID, learningFixture.courseID,
	).Scan(&entitlementID); err != nil {
		t.Fatalf("loading seeded entitlement: %v", err)
	}

	return entitlementOperationsFixture{
		learning: learningFixture, repo: repo, adminID: adminID,
		entitlementID: entitlementID,
		courseID:      learningFixture.courseID, studentID: learningFixture.studentID,
	}
}

// playbackAllowed exercises the production protected playback route as the
// entitled Student. It is the only access answer that matters.
func (f entitlementOperationsFixture) playbackAllowed(t *testing.T) bool {
	t.Helper()
	route := playbackRoute(t, f.learning)
	method, path, body := protectedLearningRequest(t, f.learning, route)
	response := f.learning.request(method, path, body)
	switch response.Code {
	case http.StatusOK, http.StatusCreated:
		return true
	case http.StatusForbidden, http.StatusNotFound, http.StatusGone:
		return false
	default:
		t.Fatalf("protected playback returned unexpected status %d: %s", response.Code, response.Body.String())
		return false
	}
}

func (f entitlementOperationsFixture) entitlementRow(t *testing.T) (state string, accessEndsAt time.Time, revokedAt *time.Time, revision int64) {
	t.Helper()
	if err := f.learning.pool.QueryRow(context.Background(), `
		SELECT state, access_ends_at, revoked_at, revision FROM entitlements WHERE id = $1::uuid
	`, f.entitlementID).Scan(&state, &accessEndsAt, &revokedAt, &revision); err != nil {
		t.Fatalf("reading entitlement row: %v", err)
	}
	return state, accessEndsAt, revokedAt, revision
}

func (f entitlementOperationsFixture) auditCount(t *testing.T, action string) int {
	t.Helper()
	var count int
	if err := f.learning.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action = $1 AND target_id = $2 AND target_type = 'ENTITLEMENT'`,
		action, f.entitlementID,
	).Scan(&count); err != nil {
		t.Fatalf("counting %s audit events: %v", action, err)
	}
	return count
}

func (f entitlementOperationsFixture) outboxCount(t *testing.T, eventType string) int {
	t.Helper()
	var count int
	if err := f.learning.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_events WHERE event_type = $1 AND aggregate_id = $2::uuid`,
		eventType, f.entitlementID,
	).Scan(&count); err != nil {
		t.Fatalf("counting %s outbox events: %v", eventType, err)
	}
	return count
}

// kuwaitDateFor renders the Kuwait-local calendar date whose exclusive
// boundary the Admin would pick to end access at roughly the given instant.
func kuwaitDateFor(instant time.Time) string {
	return instant.In(access.KuwaitLocation).AddDate(0, 0, -1).Format("2006-01-02")
}

func TestEntitlementExtendKeepsAccessAndRecordsAdjustment(t *testing.T) {
	f := newEntitlementOperationsFixture(t)
	now := f.learning.clock.Now()
	_, originalEnd, _, originalRevision := f.entitlementRow(t)

	if !f.playbackAllowed(t) {
		t.Fatal("protected playback was denied before any adjustment")
	}

	later := now.Add(90 * 24 * time.Hour)
	detail, err := f.repo.AdjustEntitlementExpiry(context.Background(), access.AdjustEntitlementExpiryParams{
		EntitlementID:  f.entitlementID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		NewAccessEndsAt: mustKuwaitBoundary(t, kuwaitDateFor(later)),
		Reason:          "Semester extended for the whole cohort",
		Now:             now,
	})
	if err != nil {
		t.Fatalf("AdjustEntitlementExpiry(extend): %v", err)
	}

	state, accessEndsAt, revokedAt, revision := f.entitlementRow(t)
	if state != "ACTIVE" || revokedAt != nil {
		t.Fatalf("extend changed entitlement state to %q (revoked_at %v)", state, revokedAt)
	}
	if !accessEndsAt.After(originalEnd) {
		t.Fatalf("extended expiry %s is not later than %s", accessEndsAt, originalEnd)
	}
	if revision != originalRevision+1 {
		t.Fatalf("revision = %d, want %d", revision, originalRevision+1)
	}
	if detail.Entitlement.OriginalAccessEndsAt.UTC() != originalEnd.UTC() {
		t.Fatalf("original_access_ends_at moved to %s; it is never editable", detail.Entitlement.OriginalAccessEndsAt)
	}

	// BR-026 adjustment record: old expiry, new expiry, reason, actor, timestamp.
	if len(detail.Adjustments) != 1 {
		t.Fatalf("adjustment history has %d entries, want 1", len(detail.Adjustments))
	}
	adjustment := detail.Adjustments[0]
	if adjustment.OldAccessEndsAt.UTC() != originalEnd.UTC() || adjustment.NewAccessEndsAt.UTC() != accessEndsAt.UTC() {
		t.Fatalf("adjustment recorded %s -> %s, want %s -> %s",
			adjustment.OldAccessEndsAt, adjustment.NewAccessEndsAt, originalEnd, accessEndsAt)
	}
	if adjustment.Reason == "" || adjustment.ActorAccountID != f.adminID || adjustment.AdjustedAt.IsZero() {
		t.Fatalf("adjustment lacks actor/reason/timestamp evidence: %+v", adjustment)
	}
	if got := f.auditCount(t, "ENTITLEMENT_EXPIRY_ADJUSTED"); got != 1 {
		t.Fatalf("ENTITLEMENT_EXPIRY_ADJUSTED audit events = %d, want 1", got)
	}
	if got := f.outboxCount(t, "access.entitlement_adjusted"); got != 1 {
		t.Fatalf("adjustment notification events = %d, want 1", got)
	}

	// Enrollment and the protected route are unaffected by a later expiry.
	snapshot := f.learning.authoritySnapshot(t)
	if snapshot.enrollments == "[]" {
		t.Fatal("extend removed the Student's enrollment")
	}
	if !f.playbackAllowed(t) {
		t.Fatal("protected playback was denied after extending access")
	}
}

func TestEntitlementShortenEndsAccessAtTheNewInstant(t *testing.T) {
	f := newEntitlementOperationsFixture(t)
	now := f.learning.clock.Now()

	// Start from a long grant so "earlier" is unambiguous.
	if _, err := f.repo.AdjustEntitlementExpiry(context.Background(), access.AdjustEntitlementExpiryParams{
		EntitlementID:  f.entitlementID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		NewAccessEndsAt: mustKuwaitBoundary(t, kuwaitDateFor(now.Add(90*24*time.Hour))),
		Reason:          "Baseline cohort period",
		Now:             now,
	}); err != nil {
		t.Fatalf("AdjustEntitlementExpiry(baseline): %v", err)
	}
	_, originalEnd, _, _ := f.entitlementRow(t)

	// Shortened, but still in the future: access continues.
	stillValid := now.Add(3 * 24 * time.Hour)
	if _, err := f.repo.AdjustEntitlementExpiry(context.Background(), access.AdjustEntitlementExpiryParams{
		EntitlementID:  f.entitlementID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		NewAccessEndsAt: mustKuwaitBoundary(t, kuwaitDateFor(stillValid)),
		Reason:          "Cohort finishes earlier than planned",
		Now:             now,
	}); err != nil {
		t.Fatalf("AdjustEntitlementExpiry(shorten): %v", err)
	}
	state, shortened, _, _ := f.entitlementRow(t)
	if state != "ACTIVE" {
		t.Fatalf("shorten changed state to %q, want ACTIVE", state)
	}
	if !shortened.Before(originalEnd) || !shortened.After(now) {
		t.Fatalf("shortened expiry %s is not earlier than %s and later than %s", shortened, originalEnd, now)
	}
	if !f.playbackAllowed(t) {
		t.Fatal("protected playback was denied while the shortened period is still open")
	}

	// Moved into the past: access ends immediately (BR-026), and nothing is deleted.
	before := f.learning.authoritySnapshot(t)
	if _, err := f.repo.AdjustEntitlementExpiry(context.Background(), access.AdjustEntitlementExpiryParams{
		EntitlementID:  f.entitlementID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		NewAccessEndsAt: mustKuwaitBoundary(t, kuwaitDateFor(now.Add(-10*24*time.Hour))),
		Reason:          "Access ended immediately at support request",
		Now:             now,
	}); err != nil {
		t.Fatalf("AdjustEntitlementExpiry(shorten into the past): %v", err)
	}
	state, expired, revokedAt, _ := f.entitlementRow(t)
	if state != "ACTIVE" || revokedAt != nil {
		t.Fatalf("past-dating an expiry must not revoke: state %q revoked_at %v", state, revokedAt)
	}
	if !expired.Before(now) {
		t.Fatalf("expiry %s was not moved into the past relative to %s", expired, now)
	}
	if f.playbackAllowed(t) {
		t.Fatal("protected playback was allowed after the effective expiry moved into the past")
	}

	after := f.learning.authoritySnapshot(t)
	if after.enrollments != before.enrollments || after.progress != before.progress {
		t.Fatalf("shortening mutated enrollment or progress:\nbefore %+v\nafter  %+v", before, after)
	}
	var adjustments int
	if err := f.learning.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM entitlement_adjustments WHERE entitlement_id = $1::uuid`, f.entitlementID,
	).Scan(&adjustments); err != nil {
		t.Fatalf("counting adjustments: %v", err)
	}
	if adjustments != 3 {
		t.Fatalf("adjustment history has %d entries, want 3", adjustments)
	}
}

func TestEntitlementRevocationDeniesAccessAndPreservesHistory(t *testing.T) {
	f := newEntitlementOperationsFixture(t)
	now := f.learning.clock.Now()

	if !f.playbackAllowed(t) {
		t.Fatal("protected playback was denied before revocation")
	}
	before := f.learning.authoritySnapshot(t)

	support := "SUPPORT-4711"
	detail, err := f.repo.RevokeEntitlement(context.Background(), access.RevokeEntitlementParams{
		EntitlementID:  f.entitlementID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		Reason: "Access ended after out-of-band refund", SupportReference: &support,
		Now: now,
	})
	if err != nil {
		t.Fatalf("RevokeEntitlement: %v", err)
	}
	if detail.Entitlement.State != "REVOKED" || detail.Entitlement.RevokedAt == nil {
		t.Fatalf("revocation result is not REVOKED with an instant: %+v", detail.Entitlement)
	}

	state, _, revokedAt, _ := f.entitlementRow(t)
	if state != "REVOKED" || revokedAt == nil {
		t.Fatalf("stored entitlement state = %q revoked_at = %v, want REVOKED with an instant", state, revokedAt)
	}
	if f.playbackAllowed(t) {
		t.Fatal("protected playback was allowed after revocation")
	}

	if got := f.auditCount(t, "ENTITLEMENT_REVOKED"); got != 1 {
		t.Fatalf("ENTITLEMENT_REVOKED audit events = %d, want 1", got)
	}
	if got := f.outboxCount(t, "access.entitlement_revoked"); got != 1 {
		t.Fatalf("revocation notification events = %d, want 1", got)
	}

	// The grant row survives as history, and no unrelated record is touched.
	after := f.learning.authoritySnapshot(t)
	if after.enrollments != before.enrollments || after.progress != before.progress {
		t.Fatalf("revocation mutated enrollment or progress:\nbefore %+v\nafter  %+v", before, after)
	}
	var entitlements, invitations int
	if err := f.learning.pool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM entitlements WHERE id = $1::uuid),
		        (SELECT count(*) FROM course_access_invitations WHERE course_id = $2::uuid)`,
		f.entitlementID, f.courseID,
	).Scan(&entitlements, &invitations); err != nil {
		t.Fatalf("counting preserved records: %v", err)
	}
	if entitlements != 1 || invitations != 1 {
		t.Fatalf("revocation deleted records: entitlements %d invitations %d", entitlements, invitations)
	}
}

func TestEntitlementMutationsRefuseInvalidTransitions(t *testing.T) {
	f := newEntitlementOperationsFixture(t)
	now := f.learning.clock.Now()
	ctx := context.Background()

	missing := "0a000000-0000-4000-8000-0000000000ff"
	if _, err := f.repo.RevokeEntitlement(ctx, access.RevokeEntitlementParams{
		EntitlementID: missing, AdminAccountID: f.adminID, Reason: "r", Now: now,
	}); !errors.Is(err, access.ErrEntitlementNotFound) {
		t.Fatalf("revoking an unknown entitlement = %v, want ErrEntitlementNotFound", err)
	}
	if _, err := f.repo.AdjustEntitlementExpiry(ctx, access.AdjustEntitlementExpiryParams{
		EntitlementID: f.entitlementID, AdminAccountID: f.adminID,
		NewAccessEndsAt: now.Add(time.Hour), Reason: "   ", Now: now,
	}); !errors.Is(err, access.ErrReasonRequired) {
		t.Fatalf("adjusting without a reason = %v, want ErrReasonRequired", err)
	}

	// Stale revision: the Admin acted on a view that no longer matches.
	_, _, _, revision := f.entitlementRow(t)
	if _, err := f.repo.AdjustEntitlementExpiry(ctx, access.AdjustEntitlementExpiryParams{
		EntitlementID: f.entitlementID, AdminAccountID: f.adminID,
		NewAccessEndsAt: now.Add(time.Hour), Reason: "stale write", ExpectedRevision: revision + 5, Now: now,
	}); !errors.Is(err, access.ErrEntitlementStale) {
		t.Fatalf("stale adjustment = %v, want ErrEntitlementStale", err)
	}
	if _, _, _, unchanged := f.entitlementRow(t); unchanged != revision {
		t.Fatalf("refused mutations changed revision %d -> %d", revision, unchanged)
	}

	// Revoked grants are terminal for both operations.
	if _, err := f.repo.RevokeEntitlement(ctx, access.RevokeEntitlementParams{
		EntitlementID: f.entitlementID, AdminAccountID: f.adminID, Reason: "first", Now: now,
	}); err != nil {
		t.Fatalf("RevokeEntitlement: %v", err)
	}
	if _, err := f.repo.RevokeEntitlement(ctx, access.RevokeEntitlementParams{
		EntitlementID: f.entitlementID, AdminAccountID: f.adminID, Reason: "second", Now: now,
	}); !errors.Is(err, access.ErrEntitlementRevoked) {
		t.Fatalf("second revocation = %v, want ErrEntitlementRevoked", err)
	}
	if _, err := f.repo.AdjustEntitlementExpiry(ctx, access.AdjustEntitlementExpiryParams{
		EntitlementID: f.entitlementID, AdminAccountID: f.adminID,
		NewAccessEndsAt: now.Add(time.Hour), Reason: "extend a revoked grant", Now: now,
	}); !errors.Is(err, access.ErrEntitlementRevoked) {
		t.Fatalf("adjusting a revoked grant = %v, want ErrEntitlementRevoked", err)
	}
	if got := f.auditCount(t, "ENTITLEMENT_REVOKED"); got != 1 {
		t.Fatalf("refused mutations wrote extra revocation audit events: %d", got)
	}
}

// TestEntitlementOperationsOverTheProductionAPI drives the whole AD07 journey
// through the mounted routes: the grant is created by Admin Approval, then
// extended, then revoked, and every unauthorized or invalid call is refused.
func TestEntitlementOperationsOverTheProductionAPI(t *testing.T) {
	ts, pool, adminID, studentID, _, adminToken, studentToken := setupAdminAccessAPIServer(t)
	ctx := context.Background()
	client := ts.Client()
	origin := "https://gradex.example"

	writer, err := outbox.NewWriter("key-v1", []byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}
	repo, err := access.NewRepository(pool, writer)
	if err != nil {
		t.Fatalf("access.NewRepository: %v", err)
	}

	courseID := "20000000-0000-0000-0000-0000000000a7"
	createTestCourseWithExpiry(t, pool, courseID, adminID, time.Now().Add(30*24*time.Hour).UTC())

	invitation, token, err := repo.CreateInvitation(ctx, access.CreateInvitationParams{
		CourseID: courseID, Email: "student-access@example.com", AdminAccountID: adminID,
	})
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if _, err := repo.AcceptInvitation(ctx, access.AcceptInvitationParams{
		InvitationID: invitation.ID, AcceptanceToken: token, CallerAccountID: studentID,
	}); err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	approved, err := repo.ApproveInvitation(ctx, access.ApproveInvitationParams{
		InvitationID: invitation.ID, AdminAccountID: adminID, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("ApproveInvitation: %v", err)
	}
	entitlementID := approved.Entitlement.ID

	expiryURL := ts.URL + "/api/v1/admin/entitlements/" + entitlementID + "/expiry"
	revokeURL := ts.URL + "/api/v1/admin/entitlements/" + entitlementID + "/revocation"
	newDate := time.Now().Add(120 * 24 * time.Hour).UTC().Format("2006-01-02")

	t.Run("the Admin queue reaches the grant without anyone handling an identifier", func(t *testing.T) {
		invitations, _, err := repo.ListAdminInvitations(ctx, access.ListAdminInvitationsFilter{Limit: 50})
		if err != nil {
			t.Fatalf("ListAdminInvitations: %v", err)
		}
		var linked bool
		for _, listed := range invitations {
			if listed.ID != invitation.ID {
				continue
			}
			if listed.EntitlementID == nil || *listed.EntitlementID != entitlementID {
				t.Fatalf("approved invitation carries entitlement %v, want %s", listed.EntitlementID, entitlementID)
			}
			linked = true
		}
		if !linked {
			t.Fatal("the approved invitation is missing from the Admin queue")
		}
	})

	t.Run("a Student cannot adjust or revoke another actor's grant", func(t *testing.T) {
		for _, call := range []struct {
			method, url, body string
		}{
			{http.MethodPut, expiryURL, `{"date":"` + newDate + `","reason":"student attempt"}`},
			{http.MethodPost, revokeURL, `{"reason":"student attempt"}`},
		} {
			response := doPricingRequest(t, client, call.method, call.url, studentToken, origin, studentToken, []byte(call.body))
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("%s as Student = %d, want 403", call.method, response.StatusCode)
			}
			response.Body.Close()
		}
		state, _ := entitlementStateAndExpiry(t, pool, entitlementID)
		if state != "ACTIVE" {
			t.Fatalf("refused Student calls changed entitlement state to %q", state)
		}
	})

	t.Run("invalid input is refused with the existing problem conventions", func(t *testing.T) {
		cases := []struct {
			name, method, url, body string
			want                    int
		}{
			{"expiry without a reason", http.MethodPut, expiryURL, `{"date":"` + newDate + `","reason":"  "}`, http.StatusUnprocessableEntity},
			{"expiry without a date", http.MethodPut, expiryURL, `{"date":"","reason":"no date"}`, http.StatusUnprocessableEntity},
			{"expiry with a malformed date", http.MethodPut, expiryURL, `{"date":"31-12-2026","reason":"bad format"}`, http.StatusUnprocessableEntity},
			{"revocation without a reason", http.MethodPost, revokeURL, `{"reason":""}`, http.StatusUnprocessableEntity},
			{"unknown entitlement", http.MethodPost,
				ts.URL + "/api/v1/admin/entitlements/20000000-0000-0000-0000-0000000000ff/revocation",
				`{"reason":"missing"}`, http.StatusNotFound},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				response := doPricingRequest(t, client, testCase.method, testCase.url, adminToken, origin, adminToken, []byte(testCase.body))
				defer response.Body.Close()
				if response.StatusCode != testCase.want {
					t.Fatalf("status = %d, want %d", response.StatusCode, testCase.want)
				}
			})
		}
		state, _ := entitlementStateAndExpiry(t, pool, entitlementID)
		if state != "ACTIVE" {
			t.Fatalf("a refused call changed entitlement state to %q", state)
		}
	})

	t.Run("the Admin extends and then revokes the grant", func(t *testing.T) {
		_, before := entitlementStateAndExpiry(t, pool, entitlementID)
		response := doPricingRequest(t, client, http.MethodPut, expiryURL, adminToken, origin, adminToken,
			[]byte(`{"date":"`+newDate+`","reason":"Extended for the launch cohort","support_reference":"SUPPORT-1"}`))
		if response.StatusCode != http.StatusOK {
			t.Fatalf("extend status = %d, want 200", response.StatusCode)
		}
		response.Body.Close()
		state, extended := entitlementStateAndExpiry(t, pool, entitlementID)
		if state != "ACTIVE" || !extended.After(before) {
			t.Fatalf("extend left state %q expiry %s (was %s)", state, extended, before)
		}

		revoke := doPricingRequest(t, client, http.MethodPost, revokeURL, adminToken, origin, adminToken,
			[]byte(`{"reason":"Access ended after out-of-band refund"}`))
		if revoke.StatusCode != http.StatusOK {
			t.Fatalf("revoke status = %d, want 200", revoke.StatusCode)
		}
		revoke.Body.Close()
		if state, _ := entitlementStateAndExpiry(t, pool, entitlementID); state != "REVOKED" {
			t.Fatalf("entitlement state = %q, want REVOKED", state)
		}

		// A revoked grant is terminal for both operations.
		repeat := doPricingRequest(t, client, http.MethodPost, revokeURL, adminToken, origin, adminToken,
			[]byte(`{"reason":"again"}`))
		if repeat.StatusCode != http.StatusConflict {
			t.Fatalf("second revoke status = %d, want 409", repeat.StatusCode)
		}
		repeat.Body.Close()
		adjust := doPricingRequest(t, client, http.MethodPut, expiryURL, adminToken, origin, adminToken,
			[]byte(`{"date":"`+newDate+`","reason":"extend a revoked grant"}`))
		if adjust.StatusCode != http.StatusConflict {
			t.Fatalf("adjust after revoke status = %d, want 409", adjust.StatusCode)
		}
		adjust.Body.Close()
	})
}

func entitlementStateAndExpiry(t *testing.T, pool *pgxpool.Pool, entitlementID string) (string, time.Time) {
	t.Helper()
	var state string
	var accessEndsAt time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT state, access_ends_at FROM entitlements WHERE id = $1::uuid`, entitlementID,
	).Scan(&state, &accessEndsAt); err != nil {
		t.Fatalf("reading entitlement: %v", err)
	}
	return state, accessEndsAt
}

func mustKuwaitBoundary(t *testing.T, date string) time.Time {
	t.Helper()
	boundary, err := access.ConvertKuwaitDateToUTCBoundary(date)
	if err != nil {
		t.Fatalf("converting Kuwait date %q: %v", date, err)
	}
	return boundary
}
