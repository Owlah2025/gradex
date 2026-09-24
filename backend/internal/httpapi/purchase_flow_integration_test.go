//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
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
	// The anonymous purchase route no longer exists. It accepted a
	// caller-supplied address and returned a WhatsApp handoff in the same
	// response, which meant any browser could put any mailbox into the Admin
	// sales queue and be handed the conversation that ends in Course access.
	//
	// 404 rather than 403 is the assertion that matters: a 403 would mean the
	// route is still mounted and merely refusing this caller. Both the
	// unadmitted and the fully admitted anonymous request must find nothing
	// there at all.
	unbound := purchaseFlowRequest(t, client, ts.URL+"/api/v1/purchase-requests", "", "", requestBody)
	if unbound.StatusCode != http.StatusNotFound {
		unbound.Body.Close()
		t.Fatalf("unbound public purchase request status = %d, want 404", unbound.StatusCode)
	}
	unbound.Body.Close()
	admitted := purchaseFlowAdmittedRequest(t, client, ts.URL+"/api/v1/purchase-requests", admissionCookie, admissionCSRF, requestBody)
	if admitted.StatusCode != http.StatusNotFound {
		admitted.Body.Close()
		t.Fatalf("admitted anonymous purchase request status = %d, want 404", admitted.StatusCode)
	}
	admitted.Body.Close()

	// The Admin half of the lifecycle still has to work against a request in
	// the pre-authentication shape, because rows of exactly that shape are
	// already in the production table.
	historicalWriter, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox writer: %v", err)
	}
	historicalRepo, err := access.NewRepository(pool, historicalWriter)
	if err != nil {
		t.Fatalf("access repository: %v", err)
	}
	historical, err := historicalRepo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{
		CourseID: courseID, Email: "Student-Access@Example.com", Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("creating historical purchase request: %v", err)
	}
	historicalHandoff, err := access.WhatsAppHandoffURL("15550000000", historical, identity.LocaleEnglish)
	if err != nil {
		t.Fatalf("building historical WhatsApp handoff: %v", err)
	}
	createdBody := struct {
		Reference   string
		WhatsAppURL string
	}{Reference: historical.ReferenceCode, WhatsAppURL: historicalHandoff}
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

	// A repeated request reuses the existing active one rather than creating a
	// second. This is what keeps a double click, a refresh, or a back-and-
	// forward from filling the Admin sales queue with duplicates of one sale.
	retry, err := historicalRepo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{
		CourseID: courseID, Email: "Student-Access@Example.com", Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("repeating purchase request: %v", err)
	}
	if retry.ReferenceCode != createdBody.Reference {
		t.Fatalf("duplicate reference = %q, want existing %q", retry.ReferenceCode, createdBody.Reference)
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
	// The handoff for an existing request quotes the snapshot the request was
	// created with, not the Course's current price. A Student who was quoted
	// 25.000 must not arrive in WhatsApp being asked for 30.000.
	repriced, err := historicalRepo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{
		CourseID: courseID, Email: "Student-Access@Example.com", Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("repeating purchase request after reprice: %v", err)
	}
	repricedHandoff, err := access.WhatsAppHandoffURL("15550000000", repriced, identity.LocaleEnglish)
	if err != nil {
		t.Fatalf("building post-reprice WhatsApp handoff: %v", err)
	}
	persistedURL, err := url.Parse(repricedHandoff)
	if err != nil {
		t.Fatalf("parsing historical WhatsApp handoff: %v", err)
	}
	if repriced.ReferenceCode != createdBody.Reference || !strings.Contains(persistedURL.Query().Get("text"), "25.000 KWD") {
		t.Fatalf("historical WhatsApp handoff was repriced: %+v", repriced)
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
		Invitation  *struct{} `json:"invitation"`
		CourseGrant *struct {
			EntitlementID string `json:"entitlement_id"`
			CourseID      string `json:"course_id"`
			Disposition   string `json:"disposition"`
		} `json:"course_grant"`
	}
	if err := json.NewDecoder(confirmed.Body).Decode(&confirmation); err != nil {
		confirmed.Body.Close()
		t.Fatalf("decoding confirmation: %v", err)
	}
	confirmed.Body.Close()
	// Admin confirmation is the grant. The request is terminal immediately and
	// the response carries the Entitlement, not an invitation to be accepted.
	if confirmation.PurchaseRequest.State != "ACCESS_GRANTED" {
		t.Fatalf("confirmation state = %q, want ACCESS_GRANTED", confirmation.PurchaseRequest.State)
	}
	if confirmation.Invitation != nil {
		t.Fatalf("confirmation returned an invitation; Admin confirmation must grant access directly")
	}
	if confirmation.CourseGrant == nil || confirmation.CourseGrant.EntitlementID == "" ||
		confirmation.CourseGrant.CourseID != courseID || confirmation.CourseGrant.Disposition != "GRANTED" {
		t.Fatalf("confirmation grant = %+v, want a GRANTED Entitlement for the Course", confirmation.CourseGrant)
	}
	entitlementID := confirmation.CourseGrant.EntitlementID

	// The access exists the moment the Admin confirms: an ACTIVE Entitlement
	// whose provenance is the purchase request and not an invitation, an
	// Enrollment, and no invitation row anywhere for this Course and Student.
	var activeGrants, enrollments, invitations int
	var grantSource string
	var sourceInvitation *string
	var sourcePurchase string
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM entitlements WHERE id = $1::uuid AND state = 'ACTIVE' AND scope_kind = 'COURSE'),
		  (SELECT count(*) FROM enrollments WHERE course_id = $2::uuid),
		  (SELECT count(*) FROM course_access_invitations WHERE course_id = $2::uuid),
		  (SELECT grant_source FROM entitlements WHERE id = $1::uuid),
		  (SELECT source_invitation_id::text FROM entitlements WHERE id = $1::uuid),
		  (SELECT source_purchase_request_id::text FROM entitlements WHERE id = $1::uuid)
	`, entitlementID, courseID).Scan(&activeGrants, &enrollments, &invitations, &grantSource, &sourceInvitation, &sourcePurchase); err != nil {
		t.Fatalf("reading granted purchase state: %v", err)
	}
	if activeGrants != 1 || enrollments != 1 {
		t.Fatalf("after confirmation entitlements=%d enrollments=%d, want 1/1", activeGrants, enrollments)
	}
	if invitations != 0 {
		t.Fatalf("confirmation created %d invitations, want none", invitations)
	}
	if grantSource != "PURCHASE_REQUEST" || sourceInvitation != nil || sourcePurchase != requestID {
		t.Fatalf("entitlement provenance = %s invitation=%v purchase=%s, want PURCHASE_REQUEST/nil/%s",
			grantSource, sourceInvitation, sourcePurchase, requestID)
	}

	// The Student is told access is ready, not that an invitation is waiting.
	var grantedEvents, invitationEvents int
	var purchaseBacked bool
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM outbox_events WHERE event_type = 'access.granted' AND aggregate_id = $1::uuid),
		  (SELECT count(*) FROM outbox_events WHERE event_type = 'access.invitation_issued'),
		  (SELECT COALESCE((safe_payload ->> 'purchase_backed')::boolean, false)
		     FROM outbox_events WHERE event_type = 'access.granted' AND aggregate_id = $1::uuid)
	`, entitlementID).Scan(&grantedEvents, &invitationEvents, &purchaseBacked); err != nil {
		t.Fatalf("reading grant notification: %v", err)
	}
	if grantedEvents != 1 || invitationEvents != 0 || !purchaseBacked {
		t.Fatalf("notification granted=%d invitation=%d purchase_backed=%v, want 1/0/true",
			grantedEvents, invitationEvents, purchaseBacked)
	}

	// Both halves of what happened are on the record, and neither invitation
	// event is, because neither happened.
	var confirmAudits, grantAudits, invitationAudits int
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM audit_events WHERE action = 'PURCHASE_REQUEST_PAYMENT_CONFIRMED' AND target_id = $1),
		  (SELECT count(*) FROM audit_events WHERE action = 'ENTITLEMENT_GRANTED' AND target_id = $2),
		  (SELECT count(*) FROM audit_events WHERE action IN ('COURSE_ACCESS_INVITATION_ISSUED', 'PURCHASE_BACKED_INVITATION_ACCEPTED'))
	`, requestID, entitlementID).Scan(&confirmAudits, &grantAudits, &invitationAudits); err != nil {
		t.Fatalf("reading grant audit: %v", err)
	}
	if confirmAudits != 1 || grantAudits != 1 || invitationAudits != 0 {
		t.Fatalf("audit confirmed=%d granted=%d invitation=%d, want 1/1/0", confirmAudits, grantAudits, invitationAudits)
	}

	// Repeating the command answers with the same access rather than granting
	// again or refusing. This is the accidental double-click.
	repeated := purchaseFlowRequest(t, client, confirmURL, adminToken, origin, nil)
	if repeated.StatusCode != http.StatusOK {
		repeated.Body.Close()
		t.Fatalf("repeat confirmation status = %d, want 200", repeated.StatusCode)
	}
	var repeatedBody struct {
		PurchaseRequest struct {
			State string `json:"state"`
		} `json:"purchase_request"`
		CourseGrant *struct {
			EntitlementID string `json:"entitlement_id"`
		} `json:"course_grant"`
	}
	if err := json.NewDecoder(repeated.Body).Decode(&repeatedBody); err != nil {
		repeated.Body.Close()
		t.Fatalf("decoding repeat confirmation: %v", err)
	}
	repeated.Body.Close()
	if repeatedBody.PurchaseRequest.State != "ACCESS_GRANTED" ||
		repeatedBody.CourseGrant == nil || repeatedBody.CourseGrant.EntitlementID != entitlementID {
		t.Fatalf("repeat confirmation = %+v, want the same Entitlement %s", repeatedBody, entitlementID)
	}
	var afterRetryEntitlements, afterRetryEnrollments, afterRetryEvents, afterRetryAudits int
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM entitlements WHERE source_purchase_request_id = $1::uuid),
		  (SELECT count(*) FROM enrollments WHERE course_id = $2::uuid),
		  (SELECT count(*) FROM outbox_events WHERE event_type = 'access.granted'),
		  (SELECT count(*) FROM audit_events WHERE action = 'ENTITLEMENT_GRANTED')
	`, requestID, courseID).Scan(&afterRetryEntitlements, &afterRetryEnrollments, &afterRetryEvents, &afterRetryAudits); err != nil {
		t.Fatalf("counting after retry: %v", err)
	}
	if afterRetryEntitlements != 1 || afterRetryEnrollments != 1 || afterRetryEvents != 1 || afterRetryAudits != 1 {
		t.Fatalf("retry duplicated state entitlements=%d enrollments=%d events=%d audits=%d, want 1/1/1/1",
			afterRetryEntitlements, afterRetryEnrollments, afterRetryEvents, afterRetryAudits)
	}

	// The Student needs no second action: their own access projection already
	// reports the Course as active, with no invitation to accept.
	history := purchaseFlowGet(t, client, ts.URL+"/api/v1/me/course-access", studentToken)
	if history.StatusCode != http.StatusOK {
		history.Body.Close()
		t.Fatalf("access history status = %d, want 200", history.StatusCode)
	}
	var historyBody struct {
		Items []struct {
			CourseID        string `json:"course_id"`
			HasActiveAccess bool   `json:"has_active_access"`
			Invitation      *struct {
				State string `json:"state"`
			} `json:"invitation"`
		} `json:"items"`
	}
	if err := json.NewDecoder(history.Body).Decode(&historyBody); err != nil {
		history.Body.Close()
		t.Fatalf("decoding access history: %v", err)
	}
	history.Body.Close()
	var found bool
	for _, item := range historyBody.Items {
		if item.CourseID != courseID {
			continue
		}
		found = true
		if !item.HasActiveAccess {
			t.Fatalf("Student access history reports no active access immediately after confirmation")
		}
		if item.Invitation != nil {
			t.Fatalf("Student access history carries invitation %+v; nothing should be awaiting acceptance", item.Invitation)
		}
	}
	if !found {
		t.Fatalf("Student access history does not include the purchased Course %s", courseID)
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
	// A purchase request that reached INVITATION_CREATED before Admin
	// confirmation began granting access directly. Confirmation no longer
	// produces this shape, so it is written the way schema 43 wrote it; the
	// point of the test is that the Admin lifecycle still governs rows already
	// standing in the table when the behaviour changed.
	request, err := repo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{CourseID: courseID, Email: "student-access@example.com", Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	historicalInvitationID, historicalToken := seedHistoricalPurchaseInvitation(t, ctx, pool, request.ID, courseID, adminID)

	client := ts.Client()
	cancelURL := ts.URL + "/api/v1/admin/course-access-invitations/" + historicalInvitationID + "/cancel"
	cancelled := purchaseFlowRequest(t, client, cancelURL, adminToken, "https://gradex.example", nil)
	if cancelled.StatusCode != http.StatusOK {
		cancelled.Body.Close()
		t.Fatalf("cancelling purchase invitation status=%d, want 200", cancelled.StatusCode)
	}
	cancelled.Body.Close()
	var requestState, invitationState string
	var grants int
	if err := pool.QueryRow(ctx, `SELECT (SELECT state FROM purchase_requests WHERE id=$1::uuid), (SELECT state FROM course_access_invitations WHERE id=$2::uuid), (SELECT count(*) FROM entitlements WHERE source_invitation_id=$2::uuid)`, request.ID, historicalInvitationID).Scan(&requestState, &invitationState, &grants); err != nil {
		t.Fatalf("reading cancelled state: %v", err)
	}
	if requestState != "CANCELLED" || invitationState != "CANCELLED" || grants != 0 {
		t.Fatalf("cancelled states request=%s invitation=%s grants=%d; want CANCELLED/CANCELLED/0", requestState, invitationState, grants)
	}
	accept := purchaseFlowRequest(t, client, ts.URL+"/api/v1/me/course-access-invitations/"+historicalInvitationID+"/accept", studentToken, "https://gradex.example", []byte(`{"acceptance_token":"`+historicalToken+`"}`))
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

// seedHistoricalPurchaseInvitation writes the schema-43 shape that Admin
// confirmation used to produce: a PENDING_STUDENT_ACCEPTANCE invitation, its
// single-use acceptance secret, and the purchase request linked to it in
// INVITATION_CREATED. Nothing in the product creates this any more, so tests
// that assert the lifecycle still honours such a row must build it themselves.
func seedHistoricalPurchaseInvitation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	requestID, courseID, adminID string,
) (string, string) {
	t.Helper()
	raw := bytes.Repeat([]byte{0x7a}, 32)
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	secretID := uuid.NewString()
	invitationID := uuid.NewString()
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `
		INSERT INTO identity_action_secrets (id, account_id, purpose, secret_digest, issued_at, expires_at)
		VALUES ($1::uuid, NULL, 'COURSE_ACCESS_INVITATION', $2, $3, $4)
	`, secretID, digest[:], now, now.Add(7*24*time.Hour)); err != nil {
		t.Fatalf("seeding historical invitation secret: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_access_invitations (
			id, normalized_email, email, course_id, created_by_account_id, state, action_secret_id, created_at
		) VALUES ($1::uuid, 'student-access@example.com', 'student-access@example.com', $2::uuid, $3::uuid,
		          'PENDING_STUDENT_ACCEPTANCE', $4::uuid, $5)
	`, invitationID, courseID, adminID, secretID, now); err != nil {
		t.Fatalf("seeding historical invitation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE purchase_requests
		   SET state = 'INVITATION_CREATED', payment_confirmed_by_account_id = $1::uuid,
		       payment_confirmed_at = $2, invitation_id = $3::uuid, invitation_created_at = $2,
		       access_ends_at_snapshot = $4, updated_at = $2
		 WHERE id = $5::uuid
	`, adminID, now, invitationID, now.Add(30*24*time.Hour), requestID); err != nil {
		t.Fatalf("linking historical invitation: %v", err)
	}
	return invitationID, token
}
