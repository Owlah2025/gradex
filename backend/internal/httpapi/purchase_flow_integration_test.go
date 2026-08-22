//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// This retained HTTP journey starts from a published Course and a public
// request. It does not seed any Purchase Request, invitation, or entitlement.
func TestManualPurchaseFlowHTTPAPI_RealPostgreSQL(t *testing.T) {
	ts, pool, adminID, _, courseID, adminToken, studentToken := setupAdminAccessAPIServer(t)
	ctx := context.Background()
	client := ts.Client()
	const origin = "https://gradex.example"
	admissionCookie, admissionCSRF := purchaseAdmission(t, client, ts.URL)

	if _, err := pool.Exec(ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en)
		VALUES ('20000000-0000-0000-0000-000000000010', $1::uuid, 'APPROVED', 1, 'نظم التشغيل', 'Operating Systems')
	`, courseID); err != nil {
		t.Fatalf("creating published Course revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE courses
		   SET lifecycle = 'PUBLISHED', live_revision_id = '20000000-0000-0000-0000-000000000010'::uuid,
		       default_access_ends_at = $1
		 WHERE id = $2::uuid
	`, time.Now().UTC().Add(30*24*time.Hour), courseID); err != nil {
		t.Fatalf("publishing Course: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason)
		VALUES ($1::uuid, 25000, $2::uuid, 'initial public price')
	`, courseID, adminID); err != nil {
		t.Fatalf("setting Course price: %v", err)
	}

	requestBody := []byte(`{"course_id":"` + courseID + `","email":"Student-Access@Example.com"}`)
	// The same valid payload without the browser-bound anonymous capability is
	// rejected before it can reach business persistence.
	unbound := purchaseFlowRequest(t, client, ts.URL+"/api/v1/purchase-requests", "", "", requestBody)
	if unbound.StatusCode != http.StatusForbidden {
		unbound.Body.Close()
		t.Fatalf("unbound public purchase request status = %d, want 403", unbound.StatusCode)
	}
	unbound.Body.Close()
	created := purchaseFlowAdmittedRequest(t, client, ts.URL+"/api/v1/purchase-requests", admissionCookie, admissionCSRF, requestBody)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create purchase request status = %d, want 201", created.StatusCode)
	}
	var createdBody struct {
		Reference   string `json:"reference"`
		WhatsAppURL string `json:"whatsapp_url"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdBody); err != nil {
		created.Body.Close()
		t.Fatalf("decoding public purchase response: %v", err)
	}
	created.Body.Close()
	if !regexp.MustCompile(`^GRX-[A-F0-9]{16}$`).MatchString(createdBody.Reference) {
		t.Fatalf("reference = %q, want non-sequential human-safe GRX reference", createdBody.Reference)
	}
	handoff, err := url.Parse(createdBody.WhatsAppURL)
	if err != nil || handoff.Host != "wa.me" || handoff.Path != "/15550000000" {
		t.Fatalf("WhatsApp URL = %q (err=%v), want configured safe test number", createdBody.WhatsAppURL, err)
	}
	message := handoff.Query().Get("text")
	for _, required := range []string{"Operating Systems", "25.000 KWD", "Student-Access@Example.com", createdBody.Reference} {
		if !strings.Contains(message, required) {
			t.Fatalf("WhatsApp message %q does not contain %q", message, required)
		}
	}
	for _, forbidden := range []string{courseID, "token=", "invitation"} {
		if strings.Contains(strings.ToLower(message), strings.ToLower(forbidden)) {
			t.Fatalf("WhatsApp message leaked internal value %q: %q", forbidden, message)
		}
	}

	var requestID, normalizedEmail string
	var snapshot int64
	if err := pool.QueryRow(ctx, `
		SELECT id::text, normalized_email, price_minor_units
		  FROM purchase_requests WHERE reference_code = $1
	`, createdBody.Reference).Scan(&requestID, &normalizedEmail, &snapshot); err != nil {
		t.Fatalf("reading persisted Purchase Request: %v", err)
	}
	if normalizedEmail != "student-access@example.com" || snapshot != 25000 {
		t.Fatalf("persisted request = email %q, price %d; want normalized email and 25000 fils", normalizedEmail, snapshot)
	}

	// A retry is externally indistinguishable from a fresh success, but reuses
	// the existing active request and does not create another audit record.
	retried := purchaseFlowAdmittedRequest(t, client, ts.URL+"/api/v1/purchase-requests", admissionCookie, admissionCSRF, requestBody)
	if retried.StatusCode != http.StatusCreated {
		t.Fatalf("duplicate purchase request status = %d, want 201", retried.StatusCode)
	}
	var retryBody struct {
		Reference string `json:"reference"`
	}
	if err := json.NewDecoder(retried.Body).Decode(&retryBody); err != nil {
		retried.Body.Close()
		t.Fatalf("decoding duplicate purchase response: %v", err)
	}
	retried.Body.Close()
	if retryBody.Reference != createdBody.Reference {
		t.Fatalf("duplicate reference = %q, want existing %q", retryBody.Reference, createdBody.Reference)
	}
	var requestCount, createAuditCount int
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM purchase_requests WHERE normalized_email = 'student-access@example.com' AND course_id = $1::uuid),
		  (SELECT count(*) FROM audit_events WHERE action = 'PURCHASE_REQUEST_CREATED' AND target_id = $2)
	`, courseID, requestID).Scan(&requestCount, &createAuditCount); err != nil {
		t.Fatalf("counting dedupe state: %v", err)
	}
	if requestCount != 1 || createAuditCount != 1 {
		t.Fatalf("dedupe produced requests=%d audits=%d, want 1/1", requestCount, createAuditCount)
	}

	// The historical request remains priced at the amount the browser never sent.
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_price_changes (course_id, old_value_minor_units, new_value_minor_units, changed_by_account_id, reason)
		VALUES ($1::uuid, 25000, 30000, $2::uuid, 'later price change')
	`, courseID, adminID); err != nil {
		t.Fatalf("changing current Course price: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT price_minor_units FROM purchase_requests WHERE id = $1::uuid`, requestID).Scan(&snapshot); err != nil || snapshot != 25000 {
		t.Fatalf("price snapshot = %d (err=%v), want 25000", snapshot, err)
	}
	persistedHandoff := purchaseFlowAdmittedRequest(t, client, ts.URL+"/api/v1/purchase-requests", admissionCookie, admissionCSRF, requestBody)
	if persistedHandoff.StatusCode != http.StatusCreated {
		persistedHandoff.Body.Close()
		t.Fatalf("post-reprice retry status = %d, want 201", persistedHandoff.StatusCode)
	}
	var persistedHandoffBody struct {
		Reference   string `json:"reference"`
		WhatsAppURL string `json:"whatsapp_url"`
	}
	if err := json.NewDecoder(persistedHandoff.Body).Decode(&persistedHandoffBody); err != nil {
		persistedHandoff.Body.Close()
		t.Fatalf("decoding post-reprice retry: %v", err)
	}
	persistedHandoff.Body.Close()
	persistedURL, err := url.Parse(persistedHandoffBody.WhatsAppURL)
	if err != nil {
		t.Fatalf("parsing historical WhatsApp handoff: %v", err)
	}
	if persistedHandoffBody.Reference != createdBody.Reference || !strings.Contains(persistedURL.Query().Get("text"), "25.000 KWD") {
		t.Fatalf("historical WhatsApp handoff was repriced: %+v", persistedHandoffBody)
	}

	queue := purchaseFlowGet(t, client, ts.URL+"/api/v1/admin/purchase-requests?q="+url.QueryEscape(createdBody.Reference), adminToken)
	if queue.StatusCode != http.StatusOK {
		queue.Body.Close()
		t.Fatalf("Admin queue status = %d, want 200", queue.StatusCode)
	}
	var queueBody struct {
		PurchaseRequests []struct {
			Reference string `json:"reference"`
			Email     string `json:"email"`
			Course    string `json:"course_title"`
			Price     int64  `json:"price_minor_units"`
			State     string `json:"state"`
		} `json:"purchase_requests"`
	}
	if err := json.NewDecoder(queue.Body).Decode(&queueBody); err != nil {
		queue.Body.Close()
		t.Fatalf("decoding Admin queue: %v", err)
	}
	queue.Body.Close()
	if len(queueBody.PurchaseRequests) != 1 || queueBody.PurchaseRequests[0].Reference != createdBody.Reference ||
		queueBody.PurchaseRequests[0].Email != "Student-Access@Example.com" || queueBody.PurchaseRequests[0].Course != "Operating Systems" ||
		queueBody.PurchaseRequests[0].Price != 25000 || queueBody.PurchaseRequests[0].State != "WAITING_PAYMENT" {
		t.Fatalf("Admin queue did not expose the factual request snapshot: %+v", queueBody.PurchaseRequests)
	}

	confirmURL := ts.URL + "/api/v1/admin/purchase-requests/" + requestID + "/confirm-payment"
	confirmed := purchaseFlowRequest(t, client, confirmURL, adminToken, origin, nil)
	if confirmed.StatusCode != http.StatusOK {
		confirmed.Body.Close()
		t.Fatalf("confirm payment status = %d, want 200", confirmed.StatusCode)
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
		confirmed.Body.Close()
		t.Fatalf("decoding confirmation: %v", err)
	}
	confirmed.Body.Close()
	if confirmation.PurchaseRequest.State != "INVITATION_CREATED" || confirmation.Invitation.ID == "" {
		t.Fatalf("confirmation response = %+v, want linked invitation", confirmation)
	}

	// Repeating the semantic command is idempotent: one invitation and one
	// invitation-email event remain committed.
	repeatedConfirmation := purchaseFlowRequest(t, client, confirmURL, adminToken, origin, nil)
	if repeatedConfirmation.StatusCode != http.StatusOK {
		repeatedConfirmation.Body.Close()
		t.Fatalf("repeat confirmation status = %d, want 200", repeatedConfirmation.StatusCode)
	}
	repeatedConfirmation.Body.Close()
	var invitationCount, invitationOutboxCount int
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM course_access_invitations WHERE id = $1::uuid),
		  (SELECT count(*) FROM outbox_events WHERE event_type = 'access.invitation_issued' AND aggregate_id = $1::uuid)
	`, confirmation.Invitation.ID).Scan(&invitationCount, &invitationOutboxCount); err != nil {
		t.Fatalf("counting confirmed invitation: %v", err)
	}
	if invitationCount != 1 || invitationOutboxCount != 1 {
		t.Fatalf("confirmation retry made invitations=%d email-events=%d, want 1/1", invitationCount, invitationOutboxCount)
	}

	acceptanceToken := purchaseInvitationToken(t, ctx, pool, confirmation.Invitation.ID)
	acceptURL := ts.URL + "/api/v1/me/course-access-invitations/" + confirmation.Invitation.ID + "/accept"
	acceptBody := []byte(`{"acceptance_token":"` + acceptanceToken + `"}`)
	otherStudentToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x54}, 32))
	wrongIdentity := purchaseFlowRequest(t, client, acceptURL, otherStudentToken, origin, acceptBody)
	if wrongIdentity.StatusCode != http.StatusNotFound {
		wrongIdentity.Body.Close()
		t.Fatalf("wrong identity acceptance status = %d, want indistinguishable 404", wrongIdentity.StatusCode)
	}
	wrongIdentity.Body.Close()
	var noEntitlement int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM entitlements WHERE source_invitation_id = $1::uuid`, confirmation.Invitation.ID).Scan(&noEntitlement); err != nil || noEntitlement != 0 {
		t.Fatalf("wrong identity changed entitlement count=%d err=%v, want zero", noEntitlement, err)
	}

	accepted := purchaseFlowRequest(t, client, acceptURL, studentToken, origin, acceptBody)
	if accepted.StatusCode != http.StatusOK {
		accepted.Body.Close()
		t.Fatalf("purchase invitation acceptance status = %d, want 200", accepted.StatusCode)
	}
	var acceptedBody struct {
		State    string `json:"state"`
		CourseID string `json:"course_id"`
	}
	if err := json.NewDecoder(accepted.Body).Decode(&acceptedBody); err != nil {
		accepted.Body.Close()
		t.Fatalf("decoding acceptance: %v", err)
	}
	accepted.Body.Close()
	if acceptedBody.State != "APPROVED" || acceptedBody.CourseID != courseID {
		t.Fatalf("purchase acceptance = %+v, want approved invitation for Course", acceptedBody)
	}
	var requestState, invitationState string
	var grantCount int
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT state::text FROM purchase_requests WHERE id = $1::uuid),
		  (SELECT state::text FROM course_access_invitations WHERE id = $2::uuid),
		  (SELECT count(*) FROM entitlements WHERE source_invitation_id = $2::uuid AND grant_source = 'PURCHASE_REQUEST' AND state = 'ACTIVE')
	`, requestID, confirmation.Invitation.ID).Scan(&requestState, &invitationState, &grantCount); err != nil {
		t.Fatalf("reading completed purchase state: %v", err)
	}
	if requestState != "ACCESS_GRANTED" || invitationState != "APPROVED" || grantCount != 1 {
		t.Fatalf("completed purchase state = request %s invitation %s grants %d, want ACCESS_GRANTED/APPROVED/1", requestState, invitationState, grantCount)
	}

	// Public eligibility remains server-enforced after any UI state is stale.
	if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle = 'DRAFT' WHERE id = $1::uuid`, courseID); err != nil {
		t.Fatalf("withdrawing Course: %v", err)
	}
	nonPublic := purchaseFlowAdmittedRequest(t, client, ts.URL+"/api/v1/purchase-requests", admissionCookie, admissionCSRF, []byte(`{"course_id":"`+courseID+`","email":"new-buyer@example.com"}`))
	if nonPublic.StatusCode != http.StatusNotFound {
		nonPublic.Body.Close()
		t.Fatalf("non-public Course request status = %d, want 404", nonPublic.StatusCode)
	}
	nonPublic.Body.Close()
}

func TestPurchaseInvitationCancellationTerminatesRequestAndAllowsFreshIntent_RealPostgreSQL(t *testing.T) {
	ts, pool, adminID, _, courseID, adminToken, studentToken := setupAdminAccessAPIServer(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en)
		VALUES ('20000000-0000-0000-0000-000000000011', $1::uuid, 'APPROVED', 1, 'نظم التشغيل', 'Operating Systems')
	`, courseID); err != nil {
		t.Fatalf("creating Course revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle='PUBLISHED', live_revision_id='20000000-0000-0000-0000-000000000011'::uuid, default_access_ends_at=$1 WHERE id=$2::uuid`, time.Now().UTC().Add(30*24*time.Hour), courseID); err != nil {
		t.Fatalf("publishing Course: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason) VALUES ($1::uuid, 25000, $2::uuid, 'initial public price')`, courseID, adminID); err != nil {
		t.Fatalf("pricing Course: %v", err)
	}
	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox writer: %v", err)
	}
	repo, err := access.NewRepository(pool, writer)
	if err != nil {
		t.Fatalf("access repository: %v", err)
	}
	request, err := repo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{CourseID: courseID, Email: "student-access@example.com", Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	confirmed, err := repo.ConfirmPurchaseRequest(ctx, access.ConfirmPurchaseRequestParams{PurchaseRequestID: request.ID, AdminAccountID: adminID, Locale: "en", Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("confirming payment: %v", err)
	}
	client := ts.Client()
	cancelURL := ts.URL + "/api/v1/admin/course-access-invitations/" + confirmed.Invitation.ID + "/cancel"
	cancelled := purchaseFlowRequest(t, client, cancelURL, adminToken, "https://gradex.example", nil)
	if cancelled.StatusCode != http.StatusOK {
		cancelled.Body.Close()
		t.Fatalf("cancelling purchase invitation status=%d, want 200", cancelled.StatusCode)
	}
	cancelled.Body.Close()
	var requestState, invitationState string
	var grants int
	if err := pool.QueryRow(ctx, `SELECT (SELECT state FROM purchase_requests WHERE id=$1::uuid), (SELECT state FROM course_access_invitations WHERE id=$2::uuid), (SELECT count(*) FROM entitlements WHERE source_invitation_id=$2::uuid)`, request.ID, confirmed.Invitation.ID).Scan(&requestState, &invitationState, &grants); err != nil {
		t.Fatalf("reading cancelled state: %v", err)
	}
	if requestState != "CANCELLED" || invitationState != "CANCELLED" || grants != 0 {
		t.Fatalf("cancelled states request=%s invitation=%s grants=%d; want CANCELLED/CANCELLED/0", requestState, invitationState, grants)
	}
	token := purchaseInvitationToken(t, ctx, pool, confirmed.Invitation.ID)
	accept := purchaseFlowRequest(t, client, ts.URL+"/api/v1/me/course-access-invitations/"+confirmed.Invitation.ID+"/accept", studentToken, "https://gradex.example", []byte(`{"acceptance_token":"`+token+`"}`))
	if accept.StatusCode != http.StatusConflict {
		accept.Body.Close()
		t.Fatalf("accepting cancelled purchase invitation status=%d, want 409", accept.StatusCode)
	}
	accept.Body.Close()
	fresh, err := repo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{CourseID: courseID, Email: "student-access@example.com", Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("creating fresh request after cancellation: %v", err)
	}
	if fresh.ID == request.ID || fresh.State != access.PurchaseRequestWaitingPayment {
		t.Fatalf("fresh request=%+v, want a new waiting request", fresh)
	}
	// Repeating the original terminal-invitation command is rejected without
	// changing either purchase fact, proving no partial retry state exists.
	retry := purchaseFlowRequest(t, client, cancelURL, adminToken, "https://gradex.example", nil)
	if retry.StatusCode != http.StatusConflict {
		retry.Body.Close()
		t.Fatalf("retrying cancelled invitation status=%d, want 409", retry.StatusCode)
	}
	retry.Body.Close()
}

func TestAdminPurchaseRequestCancellationIsIdempotent_RealPostgreSQL(t *testing.T) {
	ts, pool, adminID, _, courseID, adminToken, _ := setupAdminAccessAPIServer(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ('20000000-0000-0000-0000-000000000012', $1::uuid, 'APPROVED', 1, 'نظم التشغيل', 'Operating Systems')`, courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle='PUBLISHED', live_revision_id='20000000-0000-0000-0000-000000000012'::uuid, default_access_ends_at=$1 WHERE id=$2::uuid`, time.Now().UTC().Add(30*24*time.Hour), courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason) VALUES ($1::uuid, 25000, $2::uuid, 'initial public price')`, courseID, adminID); err != nil {
		t.Fatal(err)
	}
	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := access.NewRepository(pool, writer)
	if err != nil {
		t.Fatal(err)
	}
	request, err := repo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{CourseID: courseID, Email: "recovery@example.com", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	cancelURL := ts.URL + "/api/v1/admin/purchase-requests/" + request.ID + "/cancel"
	for attempt := 0; attempt < 2; attempt++ {
		response := purchaseFlowRequest(t, ts.Client(), cancelURL, adminToken, "https://gradex.example", nil)
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			t.Fatalf("admin cancellation attempt %d status=%d, want 200", attempt+1, response.StatusCode)
		}
		response.Body.Close()
	}
	var state string
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT state, (SELECT count(*) FROM audit_events WHERE action='PURCHASE_REQUEST_CANCELLED' AND target_id=$1) FROM purchase_requests WHERE id=$1::uuid`, request.ID).Scan(&state, &auditCount); err != nil {
		t.Fatal(err)
	}
	if state != "CANCELLED" || auditCount != 1 {
		t.Fatalf("admin recovery state=%s audits=%d, want CANCELLED/1", state, auditCount)
	}
}

func TestPurchasePaymentConfirmationMapsIneligibleRecipientToConflict_RealPostgreSQL(t *testing.T) {
	ts, pool, adminID, _, courseID, adminToken, _ := setupAdminAccessAPIServer(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ('20000000-0000-0000-0000-000000000013', $1::uuid, 'APPROVED', 1, 'نظم التشغيل', 'Operating Systems')`, courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle='PUBLISHED', live_revision_id='20000000-0000-0000-0000-000000000013'::uuid, default_access_ends_at=$1 WHERE id=$2::uuid`, time.Now().UTC().Add(30*24*time.Hour), courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason) VALUES ($1::uuid, 25000, $2::uuid, 'initial public price')`, courseID, adminID); err != nil {
		t.Fatal(err)
	}
	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := access.NewRepository(pool, writer)
	if err != nil {
		t.Fatal(err)
	}
	request, err := repo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{CourseID: courseID, Email: "admin-access@example.com", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	response := purchaseFlowRequest(t, ts.Client(), ts.URL+"/api/v1/admin/purchase-requests/"+request.ID+"/confirm-payment", adminToken, "https://gradex.example", nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("ineligible recipient confirmation status=%d, want 409", response.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(purchaseFlowJSON(t, body))), "admin") {
		t.Fatalf("conflict response exposed recipient role: %v", body)
	}
}

func purchaseFlowJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func purchaseFlowRequest(t *testing.T, client *http.Client, endpoint, token, origin string, body []byte) *http.Response {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, reader)
	if err != nil {
		t.Fatalf("creating HTTP request: %v", err)
	}
	req.Header.Set("Accept-Language", "en")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if token != "" {
		req.Header.Set("X-CSRF-Token", token)
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token, Secure: true})
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("executing HTTP request: %v", err)
	}
	return response
}

func purchaseAdmission(t *testing.T, client *http.Client, baseURL string) (*http.Cookie, string) {
	t.Helper()
	response, err := client.Get(baseURL + "/api/v1/session/bootstrap")
	if err != nil {
		t.Fatalf("bootstrapping purchase admission: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("purchase admission bootstrap status = %d", response.StatusCode)
	}
	var body struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.CSRF == "" || len(response.Cookies()) == 0 {
		t.Fatalf("decoding purchase admission bootstrap: csrf=%q err=%v cookies=%d", body.CSRF, err, len(response.Cookies()))
	}
	return response.Cookies()[0], body.CSRF
}

func purchaseFlowAdmittedRequest(t *testing.T, client *http.Client, endpoint string, cookie *http.Cookie, csrf string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("creating admitted purchase request: %v", err)
	}
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://gradex.example")
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(cookie)
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("executing admitted purchase request: %v", err)
	}
	return response
}

func purchaseFlowGet(t *testing.T, client *http.Client, endpoint, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("creating HTTP read request: %v", err)
	}
	req.Header.Set("Accept-Language", "en")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token, Secure: true})
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("executing HTTP read request: %v", err)
	}
	return response
}

func purchaseInvitationToken(t *testing.T, ctx context.Context, pool *pgxpool.Pool, invitationID string) string {
	t.Helper()
	var event outbox.Event
	var safePayload []byte
	var payload outbox.StoredProtectedPayload
	if err := pool.QueryRow(ctx, `
		SELECT e.id::text, e.event_type, e.schema_version, e.source_module, e.aggregate_type,
		       e.aggregate_id::text, e.aggregate_revision, e.correlation_id, e.safe_payload,
		       p.key_version, p.nonce, p.ciphertext
		  FROM outbox_events e
		  JOIN outbox_protected_payloads p ON p.event_id = e.id
		 WHERE e.event_type = 'access.invitation_issued' AND e.aggregate_id = $1::uuid
	`, invitationID).Scan(
		&event.ID, &event.Type, &event.SchemaVersion, &event.SourceModule, &event.AggregateType,
		&event.AggregateID, &event.AggregateRevision, &event.CorrelationID, &safePayload,
		&payload.KeyVersion, &payload.Nonce, &payload.Ciphertext,
	); err != nil {
		t.Fatalf("loading encrypted invitation email: %v", err)
	}
	if err := json.Unmarshal(safePayload, &event.SafePayload); err != nil {
		t.Fatalf("decoding safe invitation metadata: %v", err)
	}
	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("constructing test outbox reader: %v", err)
	}
	var delivery outbox.VerificationDelivery
	if err := writer.OpenProtectedPayload(ctx, event, payload, &delivery); err != nil {
		t.Fatalf("opening encrypted invitation email in test harness: %v", err)
	}
	if delivery.VerificationToken == "" {
		t.Fatal("purchase invitation outbox event carried no acceptance token")
	}
	return delivery.VerificationToken
}
