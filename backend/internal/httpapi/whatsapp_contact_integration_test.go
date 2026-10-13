//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// D-117: Student WhatsApp contact, the purchase-request contact snapshot, and
// the Admin surfaces that read them. Every case runs against a real migrated
// PostgreSQL; nothing below the HTTP router is mocked.

type contactProblem struct {
	Status int `json:"status"`
	Errors []struct {
		Code      string `json:"code"`
		Parameter string `json:"parameter"`
	} `json:"errors"`
}

func decodeContactProblem(t *testing.T, raw []byte) contactProblem {
	t.Helper()
	var decoded contactProblem
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding problem %s: %v", raw, err)
	}
	return decoded
}

func hasViolation(problem contactProblem, code string) bool {
	for _, violation := range problem.Errors {
		if violation.Code == code && violation.Parameter == "whatsapp_number" {
			return true
		}
	}
	return false
}

func TestWhatsAppContactStudentProfile(t *testing.T) {
	env := setupAcademicAPIServer(t)
	ctx := context.Background()
	const studentID = "20000000-0000-0000-0000-000000000003"

	readNumber := func() *string {
		t.Helper()
		var number *string
		if err := env.pool.QueryRow(ctx, `SELECT contact_phone FROM accounts WHERE id=$1::uuid`, studentID).Scan(&number); err != nil {
			t.Fatalf("reading stored number: %v", err)
		}
		return number
	}

	status, raw := env.call(t, http.MethodGet, "/api/v1/me/profile", env.studentToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET profile status = %d body %s", status, raw)
	}
	var profile map[string]any
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	if value, present := profile["whatsapp_number"]; !present || value != nil {
		t.Fatalf("existing Student profile contact_phone = %#v (present=%v), want explicit null", value, present)
	}

	t.Run("invalid numbers are refused without echoing or storing them", func(t *testing.T) {
		for _, input := range []string{"22123456", "123", "abc", "+999 1234 5678", ""} {
			status, raw := env.call(t, http.MethodPut, "/api/v1/me/profile", env.studentToken, map[string]any{"whatsapp_number": input})
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("PUT %q status = %d, want 422; body %s", input, status, raw)
			}
			if !hasViolation(decodeContactProblem(t, raw), "INVALID_WHATSAPP_NUMBER") {
				t.Fatalf("PUT %q problem = %s, want INVALID_WHATSAPP_NUMBER", input, raw)
			}
			if input != "" && bytes.Contains(raw, []byte(input)) {
				t.Fatalf("problem echoed the submitted value: %s", raw)
			}
		}
		if number := readNumber(); number != nil {
			t.Fatalf("an invalid submission stored %q", *number)
		}
	})

	t.Run("a Kuwaiti local number is normalized to E.164 and audited by field name only", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPut, "/api/v1/me/profile", env.studentToken, map[string]any{"whatsapp_number": "5001 2345"})
		if status != http.StatusOK {
			t.Fatalf("PUT status = %d body %s", status, raw)
		}
		var updated struct {
			WhatsAppNumber *string `json:"whatsapp_number"`
		}
		if err := json.Unmarshal(raw, &updated); err != nil || updated.WhatsAppNumber == nil || *updated.WhatsAppNumber != "+96550012345" {
			t.Fatalf("updated profile = %s err=%v", raw, err)
		}
		if number := readNumber(); number == nil || *number != "+96550012345" {
			t.Fatalf("stored number = %v", number)
		}
		var metadata string
		if err := env.pool.QueryRow(ctx, `
			SELECT metadata::text FROM audit_events
			 WHERE action='ACCOUNT_PROFILE_UPDATED' AND target_id=$1
			 ORDER BY occurred_at DESC LIMIT 1`, studentID).Scan(&metadata); err != nil {
			t.Fatalf("reading audit: %v", err)
		}
		if !strings.Contains(metadata, "whatsapp_number") || strings.Contains(metadata, "50012345") {
			t.Fatalf("audit metadata = %s, want the field name and never the number", metadata)
		}
	})

	t.Run("a Student can replace the number with an international one", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPut, "/api/v1/me/profile", env.studentToken, map[string]any{"whatsapp_number": "+20 100 123 4567"})
		if status != http.StatusOK {
			t.Fatalf("PUT status = %d body %s", status, raw)
		}
		if number := readNumber(); number == nil || *number != "+201001234567" {
			t.Fatalf("stored number = %v", number)
		}
		var updatedAt *time.Time
		if err := env.pool.QueryRow(ctx, `SELECT contact_phone_updated_at FROM accounts WHERE id=$1::uuid`, studentID).Scan(&updatedAt); err != nil || updatedAt == nil {
			t.Fatalf("whatsapp_number_updated_at = %v err=%v", updatedAt, err)
		}
	})

	t.Run("unknown fields cannot smuggle another account's identity", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPut, "/api/v1/me/profile", env.studentToken, map[string]any{
			"whatsapp_number": "+96550012345", "account_id": "20000000-0000-0000-0000-000000000001",
		})
		if status < 400 || status >= 500 {
			t.Fatalf("mass-assignment attempt status = %d body %s, want a 4xx refusal", status, raw)
		}
		var adminNumber *string
		if err := env.pool.QueryRow(ctx, `SELECT contact_phone FROM accounts WHERE id='20000000-0000-0000-0000-000000000001'::uuid`).Scan(&adminNumber); err != nil || adminNumber != nil {
			t.Fatalf("admin account number = %v err=%v", adminNumber, err)
		}
		if number := readNumber(); number == nil || *number != "+201001234567" {
			t.Fatalf("refused request changed the Student number to %v", number)
		}
	})

	t.Run("staff and anonymous callers cannot use the Student profile", func(t *testing.T) {
		if status, _ := env.call(t, http.MethodPut, "/api/v1/me/profile", env.adminToken, map[string]any{"whatsapp_number": "+96550012345"}); status != http.StatusForbidden {
			t.Fatalf("Admin PUT /me/profile status = %d, want 403", status)
		}
		if status, _ := env.call(t, http.MethodPut, "/api/v1/me/profile", env.instructorToken, map[string]any{"whatsapp_number": "+96550012345"}); status != http.StatusForbidden {
			t.Fatalf("Instructor PUT /me/profile status = %d, want 403", status)
		}
		if status, _ := env.call(t, http.MethodPut, "/api/v1/me/profile", "", map[string]any{"whatsapp_number": "+96550012345"}); status != http.StatusUnauthorized && status != http.StatusForbidden {
			t.Fatalf("anonymous PUT /me/profile status = %d, want 401/403", status)
		}
	})

	t.Run("the database refuses malformed and staff numbers from any writer", func(t *testing.T) {
		for _, statement := range []string{
			`UPDATE accounts SET contact_phone='96550012345', contact_phone_updated_at=now() WHERE id='` + studentID + `'::uuid`,
			`UPDATE accounts SET contact_phone='+965 5001 2345', contact_phone_updated_at=now() WHERE id='` + studentID + `'::uuid`,
			`UPDATE accounts SET contact_phone='+96550012345', contact_phone_updated_at=NULL WHERE id='` + studentID + `'::uuid`,
			`UPDATE accounts SET contact_phone='+96550012345', contact_phone_updated_at=now() WHERE id='20000000-0000-0000-0000-000000000001'::uuid`,
		} {
			_, err := env.pool.Exec(ctx, statement)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("%s: err = %v, want check_violation", statement, err)
			}
		}
	})
}

// publishContactTestCourse turns the access fixture's draft Course into a
// published, priced one so the Student purchase route can request it.
func publishContactTestCourse(t *testing.T, pool *pgxpool.Pool, courseID, adminID string) {
	t.Helper()
	ctx := context.Background()
	const revisionID = "26000000-0000-0000-0000-000000000001"
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE accounts SET email_verified_at=now() WHERE role='STUDENT'`, nil},
		{`INSERT INTO course_revisions (id,course_id,state,revision_number,title_ar,title_en)
		  VALUES ($1::uuid,$2::uuid,'APPROVED',1,'تواصل واتساب','WhatsApp Contact Course')`, []any{revisionID, courseID}},
		{`UPDATE courses SET lifecycle='PUBLISHED',live_revision_id=$1::uuid,default_access_ends_at=now()+interval '60 days' WHERE id=$2::uuid`, []any{revisionID, courseID}},
		{`INSERT INTO course_price_changes (course_id,new_value_minor_units,changed_by_account_id,reason) VALUES ($1::uuid,25000,$2::uuid,'Course price')`, []any{courseID, adminID}},
	} {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("publishing contact test Course: %v", err)
		}
	}
}

func readContactBody(t *testing.T, response *http.Response) []byte {
	t.Helper()
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWhatsAppContactPurchaseRequests(t *testing.T) {
	ts, pool, adminID, studentID, courseID, adminToken, studentToken := setupAdminAccessAPIServer(t)
	ctx := context.Background()
	client := ts.Client()
	const origin = "https://gradex.example"
	endpoint := ts.URL + "/api/v1/me/purchase-requests"
	otherStudentToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x54}, 32))
	publishContactTestCourse(t, pool, courseID, adminID)

	countRequests := func() int {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM purchase_requests WHERE requester_account_id=$1::uuid`, studentID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	t.Run("a Student with no number cannot create an unreachable request", func(t *testing.T) {
		raw := readContactBody(t, purchaseFlowRequest(t, client, endpoint, studentToken, origin, []byte(`{"course_id":"`+courseID+`"}`)))
		problem := decodeContactProblem(t, raw)
		if problem.Status != http.StatusUnprocessableEntity || !hasViolation(problem, "WHATSAPP_NUMBER_REQUIRED") {
			t.Fatalf("missing-contact request = %s, want 422 WHATSAPP_NUMBER_REQUIRED", raw)
		}
		raw = readContactBody(t, purchaseFlowRequest(t, client, endpoint, studentToken, origin, []byte(`{"course_id":"`+courseID+`","whatsapp_number":"123"}`)))
		if problem := decodeContactProblem(t, raw); problem.Status != http.StatusUnprocessableEntity || !hasViolation(problem, "INVALID_WHATSAPP_NUMBER") {
			t.Fatalf("invalid-contact request = %s, want 422 INVALID_WHATSAPP_NUMBER", raw)
		}
		if count := countRequests(); count != 0 {
			t.Fatalf("refused requests persisted %d rows", count)
		}
		var number *string
		if err := pool.QueryRow(ctx, `SELECT contact_phone FROM accounts WHERE id=$1::uuid`, studentID).Scan(&number); err != nil || number != nil {
			t.Fatalf("refused request stored number %v err=%v", number, err)
		}
	})

	var reference string
	t.Run("a number supplied with the request is saved and snapshotted atomically", func(t *testing.T) {
		response := purchaseFlowRequest(t, client, endpoint, studentToken, origin,
			[]byte(`{"course_id":"`+courseID+`","whatsapp_number":"5001 2345"}`))
		raw := readContactBody(t, response)
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d body %s", response.StatusCode, raw)
		}
		var created struct {
			Reference   string `json:"reference"`
			WhatsAppURL string `json:"whatsapp_url"`
			State       string `json:"state"`
			Reused      bool   `json:"reused"`
		}
		if err := json.Unmarshal(raw, &created); err != nil {
			t.Fatal(err)
		}
		reference = created.Reference
		if created.State != "WAITING_PAYMENT" || created.Reused || reference == "" {
			t.Fatalf("created = %+v", created)
		}
		parsed, err := url.Parse(created.WhatsAppURL)
		if err != nil || parsed.Host != "wa.me" || parsed.Path != "/15550000000" {
			t.Fatalf("whatsapp_url = %q err=%v", created.WhatsAppURL, err)
		}
		if strings.Contains(parsed.RawQuery, "+") {
			t.Fatalf("whatsapp_url query %q encodes spaces as '+'", parsed.RawQuery)
		}
		if text := parsed.Query().Get("text"); !strings.Contains(text, reference) || !strings.Contains(text, "WhatsApp Contact Course") {
			t.Fatalf("handoff text = %q, want the Course and reference", text)
		}
		var snapshot, profile *string
		if err := pool.QueryRow(ctx, `
			SELECT pr.contact_phone, a.contact_phone
			  FROM purchase_requests pr JOIN accounts a ON a.id = pr.requester_account_id
			 WHERE pr.reference_code=$1`, reference).Scan(&snapshot, &profile); err != nil {
			t.Fatal(err)
		}
		if snapshot == nil || *snapshot != "+96550012345" || profile == nil || *profile != "+96550012345" {
			t.Fatalf("snapshot=%v profile=%v, want both +96550012345", snapshot, profile)
		}
		// A request is not access. Nothing about creating it, or about the
		// handoff it returns, may grant the Course.
		var entitlements, enrollments int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM entitlements WHERE student_account_id=$1::uuid`, studentID).Scan(&entitlements); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE student_account_id=$1::uuid`, studentID).Scan(&enrollments); err != nil {
			t.Fatal(err)
		}
		if entitlements != 0 || enrollments != 0 {
			t.Fatalf("request creation granted access: entitlements=%d enrollments=%d", entitlements, enrollments)
		}
	})

	t.Run("a retry reuses the request and never replaces its snapshot", func(t *testing.T) {
		raw := readContactBody(t, purchaseFlowRequest(t, client, endpoint, studentToken, origin, []byte(`{"course_id":"`+courseID+`"}`)))
		var retried struct {
			Reference string `json:"reference"`
			Reused    bool   `json:"reused"`
		}
		if err := json.Unmarshal(raw, &retried); err != nil || retried.Reference != reference || !retried.Reused {
			t.Fatalf("retry = %s err=%v, want reuse of %s", raw, err, reference)
		}
		raw = readContactBody(t, purchaseFlowRequest(t, client, endpoint, studentToken, origin,
			[]byte(`{"course_id":"`+courseID+`","whatsapp_number":"+20 100 123 4567"}`)))
		if err := json.Unmarshal(raw, &retried); err != nil || retried.Reference != reference {
			t.Fatalf("retry with a new number = %s err=%v", raw, err)
		}
		if count := countRequests(); count != 1 {
			t.Fatalf("retries produced %d requests, want 1", count)
		}
		var snapshot, profile string
		if err := pool.QueryRow(ctx, `
			SELECT pr.contact_phone, a.contact_phone
			  FROM purchase_requests pr JOIN accounts a ON a.id = pr.requester_account_id
			 WHERE pr.reference_code=$1`, reference).Scan(&snapshot, &profile); err != nil {
			t.Fatal(err)
		}
		if snapshot != "+96550012345" || profile != "+201001234567" {
			t.Fatalf("snapshot=%s profile=%s, want history kept and profile updated", snapshot, profile)
		}
		_, err := pool.Exec(ctx, `UPDATE purchase_requests SET contact_phone='+201001234567' WHERE reference_code=$1`, reference)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
			t.Fatalf("rewriting a recorded snapshot err = %v, want restrict_violation", err)
		}
	})

	t.Run("a pending request returns its WhatsApp handoff to its owner only", func(t *testing.T) {
		raw := readContactBody(t, purchaseFlowGet(t, client, endpoint, studentToken))
		var own struct {
			Requests []access.PurchaseRequest `json:"purchase_requests"`
		}
		if err := json.Unmarshal(raw, &own); err != nil || len(own.Requests) != 1 {
			t.Fatalf("own requests = %s err=%v", raw, err)
		}
		if !strings.HasPrefix(own.Requests[0].WhatsAppURL, "https://wa.me/15550000000?text=") ||
			own.Requests[0].ContactWhatsAppNumber == nil || own.Requests[0].RequesterWhatsAppNumber != nil {
			t.Fatalf("own pending request = %+v", own.Requests[0])
		}
		raw = readContactBody(t, purchaseFlowGet(t, client, endpoint, otherStudentToken))
		if err := json.Unmarshal(raw, &own); err != nil || len(own.Requests) != 0 {
			t.Fatalf("other Student read = %s err=%v", raw, err)
		}
	})

	historicalRepo, err := access.NewRepository(pool, mustContactOutbox(t))
	if err != nil {
		t.Fatal(err)
	}
	var historical access.PurchaseRequest
	t.Run("historical requests without a snapshot stay intact and discoverable", func(t *testing.T) {
		// The pre-authentication shape: an address, no owner, no snapshot.
		historical, err = historicalRepo.CreatePurchaseRequest(ctx, access.CreatePurchaseRequestParams{
			CourseID: courseID, Email: "other-student@example.com", Now: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("creating historical request: %v", err)
		}
		if historical.ContactWhatsAppNumber != nil {
			t.Fatalf("historical request invented a snapshot %q", *historical.ContactWhatsAppNumber)
		}
		missing := adminContactList(t, client, ts.URL, adminToken, "MISSING")
		if len(missing) != 1 || missing[0].ReferenceCode != historical.ReferenceCode {
			t.Fatalf("MISSING filter = %+v, want only %s", missing, historical.ReferenceCode)
		}
		available := adminContactList(t, client, ts.URL, adminToken, "AVAILABLE")
		if len(available) != 1 || available[0].ReferenceCode != reference ||
			available[0].ContactWhatsAppNumber == nil || *available[0].ContactWhatsAppNumber != "+96550012345" {
			t.Fatalf("AVAILABLE filter = %+v", available)
		}

		// The Student later provides a number on their profile. The Admin can
		// now reach them through it, and the historical row is not rewritten.
		if _, err := pool.Exec(ctx, `UPDATE accounts SET contact_phone='+966501234567', contact_phone_updated_at=now() WHERE normalized_email='other-student@example.com'`); err != nil {
			t.Fatal(err)
		}
		available = adminContactList(t, client, ts.URL, adminToken, "AVAILABLE")
		var recovered *access.PurchaseRequest
		for index := range available {
			if available[index].ReferenceCode == historical.ReferenceCode {
				recovered = &available[index]
			}
		}
		if recovered == nil || recovered.ContactWhatsAppNumber != nil ||
			recovered.RequesterWhatsAppNumber == nil || *recovered.RequesterWhatsAppNumber != "+966501234567" {
			t.Fatalf("recovered historical request = %+v", recovered)
		}
		if missing := adminContactList(t, client, ts.URL, adminToken, "MISSING"); len(missing) != 0 {
			t.Fatalf("MISSING filter after recovery = %+v", missing)
		}
		var state string
		var snapshot *string
		if err := pool.QueryRow(ctx, `SELECT state, contact_phone FROM purchase_requests WHERE id=$1::uuid`, historical.ID).Scan(&state, &snapshot); err != nil {
			t.Fatal(err)
		}
		if state != "WAITING_PAYMENT" || snapshot != nil {
			t.Fatalf("historical row state=%s snapshot=%v, want untouched", state, snapshot)
		}
	})

	t.Run("Admin contact reads are Admin-only and validated", func(t *testing.T) {
		response := purchaseFlowGet(t, client, ts.URL+"/api/v1/admin/purchase-requests?contact=MISSING", studentToken)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("Student Admin list status = %d, want 403", response.StatusCode)
		}
		response = purchaseFlowGet(t, client, ts.URL+"/api/v1/admin/purchase-requests?contact=SOMETIMES", adminToken)
		response.Body.Close()
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("unknown contact filter status = %d, want 422", response.StatusCode)
		}
	})

	t.Run("payment confirmation is unchanged and is the only access grant", func(t *testing.T) {
		var requestID string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM purchase_requests WHERE reference_code=$1`, reference).Scan(&requestID); err != nil {
			t.Fatal(err)
		}
		confirmed, err := historicalRepo.ConfirmPurchaseRequest(ctx, access.ConfirmPurchaseRequestParams{
			PurchaseRequestID: requestID, AdminAccountID: adminID, Now: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("confirming payment: %v", err)
		}
		if confirmed.PurchaseRequest.State != access.PurchaseRequestAccessGranted || confirmed.CourseGrant == nil {
			t.Fatalf("confirmation = %+v", confirmed)
		}
		if confirmed.PurchaseRequest.ContactWhatsAppNumber == nil || *confirmed.PurchaseRequest.ContactWhatsAppNumber != "+96550012345" {
			t.Fatalf("confirmation lost the contact snapshot: %+v", confirmed.PurchaseRequest.ContactWhatsAppNumber)
		}
		raw := readContactBody(t, purchaseFlowGet(t, client, endpoint, studentToken))
		var own struct {
			Requests []access.PurchaseRequest `json:"purchase_requests"`
		}
		if err := json.Unmarshal(raw, &own); err != nil || len(own.Requests) != 1 || own.Requests[0].WhatsAppURL != "" {
			t.Fatalf("granted request still offers a payment handoff: %s err=%v", raw, err)
		}
	})
}

func mustContactOutbox(t *testing.T) *outbox.Writer {
	t.Helper()
	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return writer
}

func adminContactList(t *testing.T, client *http.Client, baseURL, adminToken, contact string) []access.PurchaseRequest {
	t.Helper()
	response := purchaseFlowGet(t, client, baseURL+"/api/v1/admin/purchase-requests?contact="+contact, adminToken)
	raw := readContactBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Admin list %s status = %d body %s", contact, response.StatusCode, raw)
	}
	var decoded struct {
		Requests []access.PurchaseRequest `json:"purchase_requests"`
		Total    int                      `json:"total"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Total != len(decoded.Requests) {
		t.Fatalf("Admin list %s total=%d rows=%d", contact, decoded.Total, len(decoded.Requests))
	}
	return decoded.Requests
}

func TestWhatsAppContactAdminDirectoryUser360AndMetrics(t *testing.T) {
	ts, pool, adminID, _, courseID, _, adminToken, instructorToken := setupAdminPricingAPIServer(t)
	ctx := context.Background()
	client := ts.Client()
	const (
		providedID = "10000000-0000-0000-0000-000000000901"
		missingID  = "10000000-0000-0000-0000-000000000902"
	)
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at, contact_phone, contact_phone_updated_at) VALUES
		($1::uuid, 'contact-provided@example.com', 'contact-provided@example.com', 'STUDENT', 'ACTIVE', 'Contact Provided', 'en', now(), '+96550012345', now()),
		($2::uuid, 'contact-missing@example.com', 'contact-missing@example.com', 'STUDENT', 'ACTIVE', 'Contact Missing', 'ar', now(), NULL, NULL)
	`, providedID, missingID); err != nil {
		t.Fatalf("seeding Students: %v", err)
	}
	// A request that predates D-117 from the Student who never gave a number:
	// the unreachable case this whole change is about.
	if _, err := pool.Exec(ctx, `
		INSERT INTO purchase_requests (reference_code, course_id, email, normalized_email, requester_account_id,
		                               course_title_ar, course_title_en, price_minor_units, currency, state)
		VALUES ('GRX-CONTACTMISSING01', $1::uuid, 'contact-missing@example.com', 'contact-missing@example.com', $2::uuid,
		        'مقرر', 'Course', 25000, 'KWD', 'WAITING_PAYMENT')
	`, courseID, missingID); err != nil {
		t.Fatalf("seeding historical request: %v", err)
	}

	directory := func(query string) []map[string]any {
		t.Helper()
		response := doPricingRequest(t, client, http.MethodGet, ts.URL+"/api/v1/admin/accounts?"+query, adminToken, "", adminToken, nil)
		var decoded struct {
			Accounts []map[string]any `json:"accounts"`
		}
		decodeAdminResponse(t, response, http.StatusOK, &decoded)
		return decoded.Accounts
	}
	ids := func(accounts []map[string]any) []string {
		result := make([]string, 0, len(accounts))
		for _, account := range accounts {
			result = append(result, account["id"].(string))
		}
		return result
	}

	if got := ids(directory("whatsapp=PROVIDED")); len(got) != 1 || got[0] != providedID {
		t.Fatalf("PROVIDED filter = %v", got)
	}
	missing := directory("whatsapp=MISSING")
	if got := ids(missing); len(got) != 1 || got[0] != missingID {
		t.Fatalf("MISSING filter = %v, want only the Student (never staff)", got)
	}
	if _, present := missing[0]["whatsapp_number"]; present {
		t.Fatalf("missing Student row carries a number: %v", missing[0])
	}
	pending := directory("pendingPayment=true")
	if got := ids(pending); len(got) != 1 || got[0] != missingID || pending[0]["pending_payment_requests"] != float64(1) {
		t.Fatalf("pendingPayment filter = %v", pending)
	}
	if got := ids(directory("pendingPayment=true&whatsapp=MISSING")); len(got) != 1 || got[0] != missingID {
		t.Fatalf("unreachable pending filter = %v", got)
	}
	if got := ids(directory("pendingPayment=true&whatsapp=PROVIDED")); len(got) != 0 {
		t.Fatalf("reachable pending filter = %v, want none", got)
	}
	all := directory("role=STUDENT")
	for _, account := range all {
		if account["id"] == providedID && account["whatsapp_number"] != "+96550012345" {
			t.Fatalf("provided Student row = %v", account)
		}
	}
	for _, query := range []string{"whatsapp=VERIFIED", "pendingPayment=yes"} {
		response := doPricingRequest(t, client, http.MethodGet, ts.URL+"/api/v1/admin/accounts?"+query, adminToken, "", adminToken, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s status = %d, want 422", query, response.StatusCode)
		}
	}

	user360 := func(accountID string) map[string]any {
		t.Helper()
		response := doPricingRequest(t, client, http.MethodGet, ts.URL+"/api/v1/admin/accounts/"+accountID, adminToken, "", adminToken, nil)
		var decoded map[string]any
		decodeAdminResponse(t, response, http.StatusOK, &decoded)
		return decoded
	}
	providedView := user360(providedID)["identity"].(map[string]any)
	if providedView["whatsapp_number"] != "+96550012345" {
		t.Fatalf("User 360 identity = %v", providedView)
	}
	missingView := user360(missingID)
	if value, present := missingView["identity"].(map[string]any)["whatsapp_number"]; !present || value != nil {
		t.Fatalf("missing User 360 contact_phone = %#v (present=%v), want explicit null", value, present)
	}
	requests := missingView["student"].(map[string]any)["purchase_requests"].([]any)
	if len(requests) != 1 || requests[0].(map[string]any)["contact_whatsapp_number"] != nil {
		t.Fatalf("historical request in User 360 = %v", requests)
	}

	for _, path := range []string{"/api/v1/admin/accounts?whatsapp=PROVIDED", "/api/v1/admin/accounts/" + providedID} {
		response := doPricingRequest(t, client, http.MethodGet, ts.URL+path, instructorToken, "", instructorToken, nil)
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden || bytes.Contains(raw, []byte("+96550012345")) {
			t.Fatalf("Instructor %s status = %d body %s, want 403 without contact data", path, response.StatusCode, raw)
		}
	}

	export := doPricingRequest(t, client, http.MethodGet, ts.URL+"/api/v1/admin/accounts/export?role=STUDENT", adminToken, "", adminToken, nil)
	raw, _ := io.ReadAll(export.Body)
	export.Body.Close()
	if export.StatusCode == http.StatusOK && (bytes.Contains(raw, []byte("whatsapp")) || bytes.Contains(raw, []byte("96550012345"))) {
		t.Fatalf("account export carries WhatsApp contact: %s", raw)
	}

	response := doPricingRequest(t, client, http.MethodGet, ts.URL+"/api/v1/admin/metrics/overview", adminToken, "", adminToken, nil)
	var overview struct {
		Metrics []struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		} `json:"metrics"`
	}
	decodeAdminResponse(t, response, http.StatusOK, &overview)
	metrics := map[string]json.RawMessage{}
	for _, metric := range overview.Metrics {
		metrics[metric.Key] = metric.Value
	}
	assertMetricInt(t, metrics, "students.whatsapp_provided", 1)
	assertMetricInt(t, metrics, "students.whatsapp_missing", 1)
	assertMetricInt(t, metrics, "purchase_requests.waiting_payment_without_contact", 1)
	_ = adminID
}
