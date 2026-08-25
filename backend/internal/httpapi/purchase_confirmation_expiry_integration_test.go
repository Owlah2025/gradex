//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// KNOWN-BASELINE-01. A Course may be published and purchasable before an Admin
// has configured its default access expiry. Payment confirmation must then
// refuse the command as an expected business conflict — never as an internal
// error — and must leave the Purchase Request completely untouched so the Admin
// can configure the expiry and retry.

// publishPurchasableCourseWithoutExpiry publishes the seeded Course and prices
// it while deliberately leaving courses.default_access_ends_at NULL.
func publishPurchasableCourseWithoutExpiry(t *testing.T, ctx context.Context, pool *pgxpool.Pool, courseID, adminID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en)
		VALUES ('20000000-0000-0000-0000-000000000010', $1::uuid, 'APPROVED', 1, 'نظم التشغيل', 'Operating Systems')
	`, courseID); err != nil {
		t.Fatalf("creating published Course revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE courses
		   SET lifecycle = 'PUBLISHED', live_revision_id = '20000000-0000-0000-0000-000000000010'::uuid,
		       default_access_ends_at = NULL
		 WHERE id = $1::uuid
	`, courseID); err != nil {
		t.Fatalf("publishing Course without default access expiry: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason)
		VALUES ($1::uuid, 25000, $2::uuid, 'initial public price')
	`, courseID, adminID); err != nil {
		t.Fatalf("setting Course price: %v", err)
	}
	var configured *time.Time
	if err := pool.QueryRow(ctx, `SELECT default_access_ends_at FROM courses WHERE id = $1::uuid`, courseID).Scan(&configured); err != nil {
		t.Fatalf("reading Course default access expiry: %v", err)
	}
	if configured != nil {
		t.Fatalf("default_access_ends_at = %v, want NULL for this scenario", configured)
	}
}

// purchaseConfirmationSideEffects counts every mutation payment confirmation is
// allowed to make, so a refused confirmation can be proven to have made none.
type purchaseConfirmationSideEffects struct {
	State           string
	ConfirmedAt     *time.Time
	InvitationID    *string
	SnapshotExpiry  *time.Time
	Invitations     int
	Entitlements    int
	OutboxEvents    int
	ConfirmedAudits int
}

func readPurchaseConfirmationSideEffects(t *testing.T, ctx context.Context, pool *pgxpool.Pool, requestID, courseID string) purchaseConfirmationSideEffects {
	t.Helper()
	var observed purchaseConfirmationSideEffects
	if err := pool.QueryRow(ctx, `
		SELECT pr.state, pr.payment_confirmed_at, pr.invitation_id::text, pr.access_ends_at_snapshot,
		       (SELECT count(*) FROM course_access_invitations WHERE course_id = $2::uuid),
		       (SELECT count(*) FROM entitlements WHERE course_id = $2::uuid),
		       (SELECT count(*) FROM outbox_events),
		       (SELECT count(*) FROM audit_events WHERE action = 'PURCHASE_REQUEST_PAYMENT_CONFIRMED' AND target_id = $1)
		  FROM purchase_requests pr
		 WHERE pr.id = $1::uuid
	`, requestID, courseID).Scan(
		&observed.State, &observed.ConfirmedAt, &observed.InvitationID, &observed.SnapshotExpiry,
		&observed.Invitations, &observed.Entitlements, &observed.OutboxEvents, &observed.ConfirmedAudits,
	); err != nil {
		t.Fatalf("reading purchase confirmation side effects: %v", err)
	}
	return observed
}

func assertNoPurchaseConfirmationSideEffects(t *testing.T, stage string, observed purchaseConfirmationSideEffects) {
	t.Helper()
	if observed.State != string(access.PurchaseRequestWaitingPayment) {
		t.Fatalf("%s: purchase request state = %q, want WAITING_PAYMENT", stage, observed.State)
	}
	if observed.ConfirmedAt != nil || observed.InvitationID != nil || observed.SnapshotExpiry != nil {
		t.Fatalf("%s: refused confirmation wrote confirmation state: %+v", stage, observed)
	}
	if observed.Invitations != 0 {
		t.Fatalf("%s: refused confirmation created %d invitation(s), want 0", stage, observed.Invitations)
	}
	if observed.Entitlements != 0 {
		t.Fatalf("%s: refused confirmation created %d entitlement(s), want 0", stage, observed.Entitlements)
	}
	if observed.OutboxEvents != 0 {
		t.Fatalf("%s: refused confirmation emitted %d outbox event(s), want 0", stage, observed.OutboxEvents)
	}
	if observed.ConfirmedAudits != 0 {
		t.Fatalf("%s: refused confirmation wrote %d confirmation audit event(s), want 0", stage, observed.ConfirmedAudits)
	}
}

// TestConfirmPurchaseRequestRequiresDefaultAccessExpiry proves the domain
// contract directly against real PostgreSQL: a NULL default access expiry is an
// expected ErrExpiryRequired refusal with zero partial mutation, and the same
// request confirms normally once the Admin configures the expiry.
func TestConfirmPurchaseRequestRequiresDefaultAccessExpiry(t *testing.T) {
	_, pool, adminID, _, courseID, _, _ := setupAdminAccessAPIServer(t)
	ctx := context.Background()

	outboxWriter, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}
	repo, err := access.NewRepository(pool, outboxWriter)
	if err != nil {
		t.Fatalf("access.NewRepository: %v", err)
	}

	publishPurchasableCourseWithoutExpiry(t, ctx, pool, courseID, adminID)

	request, err := repo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{
		CourseID: courseID,
		Email:    "Student-Access@Example.com",
		Now:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("creating purchase request: %v", err)
	}
	if request.State != access.PurchaseRequestWaitingPayment {
		t.Fatalf("seeded request state = %q, want WAITING_PAYMENT", request.State)
	}

	confirmParams := access.ConfirmPurchaseRequestParams{
		PurchaseRequestID: request.ID,
		AdminAccountID:    adminID,
		Locale:            identity.LocaleEnglish,
		Now:               time.Now().UTC(),
	}
	_, err = repo.ConfirmPurchaseRequest(ctx, confirmParams)
	if !errors.Is(err, access.ErrExpiryRequired) {
		t.Fatalf("ConfirmPurchaseRequest with NULL default_access_ends_at returned %v, want ErrExpiryRequired", err)
	}

	assertNoPurchaseConfirmationSideEffects(t, "after NULL-expiry refusal",
		readPurchaseConfirmationSideEffects(t, ctx, pool, request.ID, courseID))

	// The supported recovery: configure the expiry through the canonical domain
	// command, then retry the same confirmation.
	expiry := time.Now().UTC().Add(180 * 24 * time.Hour).Truncate(time.Second)
	if err := repo.SetCourseDefaultAccessExpiry(ctx, access.SetCourseDefaultAccessExpiryParams{
		CourseID:            courseID,
		AdminAccountID:      adminID,
		ActorDescriptor:     adminID,
		DefaultAccessEndsAt: expiry,
		Reason:              "configuring default access expiry before payment confirmation",
	}); err != nil {
		t.Fatalf("configuring Course default access expiry: %v", err)
	}

	confirmParams.Now = time.Now().UTC()
	result, err := repo.ConfirmPurchaseRequest(ctx, confirmParams)
	if err != nil {
		t.Fatalf("retrying confirmation after configuring expiry: %v", err)
	}
	if result.PurchaseRequest.State != access.PurchaseRequestInvitationCreated {
		t.Fatalf("retried confirmation state = %q, want INVITATION_CREATED", result.PurchaseRequest.State)
	}
	if result.Invitation.ID == "" {
		t.Fatalf("retried confirmation produced no invitation: %+v", result)
	}
	if result.PurchaseRequest.AccessEndsAtSnapshot == nil || !result.PurchaseRequest.AccessEndsAtSnapshot.Equal(expiry) {
		t.Fatalf("access expiry snapshot = %v, want the configured %v", result.PurchaseRequest.AccessEndsAtSnapshot, expiry)
	}

	var invitationCount, invitationOutboxCount int
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM course_access_invitations WHERE id = $1::uuid),
		  (SELECT count(*) FROM outbox_events WHERE event_type = 'access.invitation_issued' AND aggregate_id = $1::uuid)
	`, result.Invitation.ID).Scan(&invitationCount, &invitationOutboxCount); err != nil {
		t.Fatalf("counting retried confirmation effects: %v", err)
	}
	if invitationCount != 1 || invitationOutboxCount != 1 {
		t.Fatalf("retried confirmation made invitations=%d email-events=%d, want 1/1", invitationCount, invitationOutboxCount)
	}
}

// TestConfirmPaymentReturnsConflictWhenDefaultExpiryMissing proves the real
// production HTTP mapping: the Admin receives a canonical 409 business conflict
// with actionable guidance, never a 500 and never a leaked database error.
func TestConfirmPaymentReturnsConflictWhenDefaultExpiryMissing(t *testing.T) {
	ts, pool, adminID, _, courseID, adminToken, _ := setupAdminAccessAPIServer(t)
	ctx := context.Background()
	client := ts.Client()
	const origin = "https://gradex.example"

	outboxWriter, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}
	repo, err := access.NewRepository(pool, outboxWriter)
	if err != nil {
		t.Fatalf("access.NewRepository: %v", err)
	}

	publishPurchasableCourseWithoutExpiry(t, ctx, pool, courseID, adminID)

	request, err := repo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{
		CourseID: courseID,
		Email:    "Student-Access@Example.com",
		Now:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("creating purchase request: %v", err)
	}

	confirmURL := ts.URL + "/api/v1/admin/purchase-requests/" + request.ID + "/confirm-payment"
	refused := purchaseFlowRequest(t, client, confirmURL, adminToken, origin, nil)
	defer refused.Body.Close()
	if refused.StatusCode != http.StatusConflict {
		t.Fatalf("confirm payment with NULL default expiry status = %d, want 409", refused.StatusCode)
	}
	if refused.Header.Get("X-Request-Id") == "" {
		t.Fatalf("refused confirmation dropped the correlation request id")
	}
	var body struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Status int    `json:"status"`
	}
	raw := bytes.Buffer{}
	if _, err := raw.ReadFrom(refused.Body); err != nil {
		t.Fatalf("reading problem body: %v", err)
	}
	if err := json.Unmarshal(raw.Bytes(), &body); err != nil {
		t.Fatalf("decoding problem body %q: %v", raw.String(), err)
	}
	if body.Status != http.StatusConflict {
		t.Fatalf("problem status = %d, want 409", body.Status)
	}
	// The Admin must be told what to do, not merely that something conflicted.
	if !bytes.Contains(bytes.ToLower(raw.Bytes()), []byte("expiry")) {
		t.Fatalf("problem response gives the Admin no expiry guidance: %s", raw.String())
	}
	// No internal detail may reach the Admin browser.
	for _, leaked := range []string{"default_access_ends_at", "time.Time", "pgx", "SQL", "scan", "courses c", "goroutine"} {
		if bytes.Contains(bytes.ToLower(raw.Bytes()), bytes.ToLower([]byte(leaked))) {
			t.Fatalf("problem response leaked internal detail %q: %s", leaked, raw.String())
		}
	}

	assertNoPurchaseConfirmationSideEffects(t, "after HTTP 409",
		readPurchaseConfirmationSideEffects(t, ctx, pool, request.ID, courseID))

	// The Admin configures the expiry through the existing canonical control and retries.
	expiry := time.Now().UTC().Add(180 * 24 * time.Hour).Truncate(time.Second)
	if err := repo.SetCourseDefaultAccessExpiry(ctx, access.SetCourseDefaultAccessExpiryParams{
		CourseID:            courseID,
		AdminAccountID:      adminID,
		ActorDescriptor:     adminID,
		DefaultAccessEndsAt: expiry,
		Reason:              "configuring default access expiry before payment confirmation",
	}); err != nil {
		t.Fatalf("configuring Course default access expiry: %v", err)
	}

	confirmed := purchaseFlowRequest(t, client, confirmURL, adminToken, origin, nil)
	defer confirmed.Body.Close()
	if confirmed.StatusCode != http.StatusOK {
		t.Fatalf("retried confirm payment status = %d, want 200", confirmed.StatusCode)
	}
	var confirmation struct {
		PurchaseRequest struct {
			State string `json:"state"`
		} `json:"purchase_request"`
		Invitation struct {
			ID string `json:"id"`
		} `json:"invitation"`
	}
	if err := json.NewDecoder(confirmed.Body).Decode(&confirmation); err != nil {
		t.Fatalf("decoding retried confirmation: %v", err)
	}
	if confirmation.PurchaseRequest.State != "INVITATION_CREATED" || confirmation.Invitation.ID == "" {
		t.Fatalf("retried confirmation = %+v, want linked invitation", confirmation)
	}
}
