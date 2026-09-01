//go:build integration

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	transactionalemail "github.com/Owlah2025/gradex/backend/internal/email"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/httpapi"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/jackc/pgx/v5/pgxpool"
)

const t108Pass = "a long T108 integration passphrase"
const t108CompromisedPass = "cider lagoon harbor falcon 527"

func t108Config(t *testing.T, redis tlsRedisFixture) *config.Config {
	t.Helper()
	s := map[string]string{"APP_ENV": "production", "PUBLIC_ORIGIN": "https://gradex.example", "CORS_ALLOWED_ORIGINS": "https://gradex.example", "CORS_ALLOW_CREDENTIALS": "true", "REDIS_ADDR": redis.addr, "REDIS_TLS_ENABLED": "true", "REDIS_TLS_SERVER_NAME": "localhost", "REDIS_TLS_CA_CERT_FILE": redis.caFile, "S3_ENDPOINT": "https://storage.example", "S3_BUCKET": "gradex-media", "SALES_WHATSAPP_NUMBER": "15550000000", "AUTH_FAKE_MODE": "false", "PASSWORD_SCREEN_MODE": "adapter", "COMPROMISED_PASSWORD_ADAPTER_APPROVED": "true", "OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION": "t108-v1", "EMAIL_ENABLED": "true", "EMAIL_PROVIDER": "resend", "EMAIL_FROM_ADDRESS": "notifications@gradex.kw", "EMAIL_FROM_NAME": "Gradex", "LEGAL_IDENTITY_MODE": "public", "LEGAL_OPERATOR_NAME": "Gradex", "LEGAL_REGISTRATION_NUMBER": "T108", "LEGAL_REGISTERED_ADDRESS": "Kuwait", "PRIVACY_EMAIL": "privacy@gradex.example", "SUPPORT_EMAIL": "support@gradex.example", "SECURITY_EMAIL": "security@gradex.example"}
	sec := config.MapSecretResolver{"DATABASE_URL": apiTestDSN, "REDIS_PASSWORD": redis.password, "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": strings.Repeat("p", 32), "SESSION_CSRF_KEY": strings.Repeat("s", 32), "ANONYMOUS_COOKIE_SIGNING_KEY": strings.Repeat("a", 32), "ANONYMOUS_CSRF_KEY": strings.Repeat("b", 32), "ADMISSION_LIMITER_HMAC_KEY": strings.Repeat("c", 32), "OUTBOX_PROTECTED_PAYLOAD_KEY": strings.Repeat("d", 32), "IDENTITY_OTP_PEPPER": strings.Repeat("o", 32), "EMAIL_API_KEY": "test-key"}
	cfg, err := config.LoadFrom(config.MapLookup(s), sec)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func t108Seed(t *testing.T, p *pgxpool.Pool, email string, role identity.Role) {
	t.Helper()
	h, e := identity.HashPassword(t108Pass)
	if e != nil {
		t.Fatal(e)
	}
	_, e = p.Exec(t.Context(), `WITH a AS (INSERT INTO accounts(normalized_email,email,role,status,display_name,email_verified_at) VALUES($1,$1,$2,'ACTIVE',$3,now()) RETURNING id) INSERT INTO password_credentials(account_id,password_hash,state) SELECT id,$4,'ACTIVE' FROM a`, email, role, string(role), h.Expose())
	if e != nil {
		t.Fatal(e)
	}
}

type t108Client struct {
	h    http.Handler
	c    []*http.Cookie
	csrf string
}

func (b *t108Client) call(m, p string, v any, mut bool) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(v)
	r := httptest.NewRequest(m, p, bytes.NewReader(raw))
	r.Header.Set("Origin", "https://gradex.example")
	r.Header.Set("Content-Type", "application/json")
	if mut {
		r.Header.Set("X-CSRF-Token", b.csrf)
	}
	for _, c := range b.c {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	b.h.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			b.c = []*http.Cookie{c}
		} else {
			b.c = append(b.c, c)
		}
	}
	return w
}
func (b *t108Client) login(t *testing.T, email string) {
	x := b.call("GET", "/api/v1/session/bootstrap", nil, false)
	var q struct {
		CSRFToken string `json:"csrf_token"`
	}
	json.Unmarshal(x.Body.Bytes(), &q)
	b.csrf = q.CSRFToken
	x = b.call("POST", "/api/v1/sessions", map[string]string{"email": email, "password": t108Pass}, true)
	if x.Code != 201 {
		t.Fatalf("login %d %s", x.Code, x.Body.String())
	}
	json.Unmarshal(x.Body.Bytes(), &q)
	b.csrf = q.CSRFToken
}

func t108Status(t *testing.T, got *httptest.ResponseRecorder, want int, operation string) {
	t.Helper()
	if got.Code != want {
		t.Fatalf("%s status = %d, want %d: %s", operation, got.Code, want, got.Body.String())
	}
}

func t108Denied(t *testing.T, got *httptest.ResponseRecorder, operation string) {
	t.Helper()
	if got.Code != http.StatusUnauthorized && got.Code != http.StatusForbidden {
		t.Fatalf("%s status = %d, want authentication or authorization denial: %s", operation, got.Code, got.Body.String())
	}
}

func t108InvitationAction(
	t *testing.T,
	repo *transactionalemail.Repository,
	writer *outbox.Writer,
) string {
	t.Helper()
	claims, err := repo.Claim(t.Context(), transactionalemail.ClaimOptions{
		Provider: "resend", Now: time.Now(), LeaseDuration: time.Minute, Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range claims {
		if claim.Event.Type != "identity.staff_invitation_created" {
			continue
		}
		var payload transactionalemail.DeliveryPayload
		if err := writer.OpenProtectedPayload(t.Context(), claim.Event, claim.Protected, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.VerificationToken == "" {
			t.Fatal("staff invitation delivery payload did not contain the action credential")
		}
		return payload.VerificationToken
	}
	t.Fatal("durable staff invitation email intent was not claimable")
	return ""
}
func TestT108ProductionStaffLifecycle(t *testing.T) {
	r := newTLSRedisFixture(t)
	cfg := t108Config(t, r)
	if !cfg.Environment().IsProduction() || cfg.AuthFakeMode() {
		t.Fatal("not production")
	}
	freshAPISchema(t)
	p, _ := apiPool(t)
	conn, e := queue.NewConnection(cfg.Redis())
	if e != nil {
		t.Fatal(e)
	}
	canaryDigest := sha256.Sum256([]byte(t108CompromisedPass))
	src, e := identity.NewDeterministicCompromisedSource(hex.EncodeToString(canaryDigest[:]))
	if e != nil {
		t.Fatal(e)
	}
	pf, e := buildProductionFoundationsWithStaffSource(cfg, p, conn, func(*config.Config) (identity.CompromisedRangeSource, error) { return src, nil })
	if e != nil {
		t.Fatal(e)
	}
	defer pf.Close()
	if pf.SessionRedis == nil || pf.StaffRedis == nil {
		t.Fatal("production foundations did not compose real session and staff Redis dependencies")
	}
	rep := health.New(time.Second)
	rep.MarkStarted()
	a, e := auth.NewSessionAuthenticator(pf.SessionRepository)
	if e != nil {
		t.Fatal(e)
	}
	logs := &bytes.Buffer{}
	router, e := httpapi.NewRouter(cfg, logging.New(logs, "t108", "production", logging.LevelFromString("info")), rep, a, identity.NewDBPrincipalResolver(p), pf.Options...)
	if e != nil {
		t.Fatal(e)
	}
	runID := time.Now().UTC().UnixNano()
	adminEmail := fmt.Sprintf("t108-admin-%d@example.com", runID)
	studentEmail := fmt.Sprintf("t108-student-%d@example.com", runID)
	instructorEmail := fmt.Sprintf("t108-instructor-%d@example.com", runID)
	staleAdminEmail := fmt.Sprintf("t108-stale-admin-%d@example.com", runID)
	t108Seed(t, p, adminEmail, identity.RoleAdmin)
	t108Seed(t, p, studentEmail, identity.RoleStudent)
	t108Seed(t, p, staleAdminEmail, identity.RoleAdmin)
	admin := &t108Client{h: router}
	student := &t108Client{h: router}
	staleAdmin := &t108Client{h: router}
	unauthenticated := &t108Client{h: router}
	admin.login(t, adminEmail)
	student.login(t, studentEmail)
	staleAdmin.login(t, staleAdminEmail)
	if _, err := p.Exec(t.Context(), `UPDATE sessions SET authenticated_at=now() - interval '2 hours' WHERE account_id=(SELECT id FROM accounts WHERE normalized_email=$1)`, staleAdminEmail); err != nil {
		t.Fatal(err)
	}
	t108Denied(t, unauthenticated.call("POST", "/api/v1/staff-invitations", map[string]string{"email": instructorEmail, "role": "INSTRUCTOR"}, true), "unauthenticated invite")
	t108Denied(t, student.call("POST", "/api/v1/staff-invitations", map[string]string{"email": instructorEmail, "role": "INSTRUCTOR"}, true), "student invite")
	t108Denied(t, staleAdmin.call("POST", "/api/v1/staff-invitations", map[string]string{"email": instructorEmail, "role": "INSTRUCTOR"}, true), "stale Admin invite")
	invite := admin.call("POST", "/api/v1/staff-invitations", map[string]string{"email": instructorEmail, "role": "INSTRUCTOR"}, true)
	t108Status(t, invite, http.StatusCreated, "admin invite")
	if strings.Contains(strings.ToLower(invite.Body.String()), "bearer") || strings.Contains(invite.Body.String(), "token") || strings.Contains(invite.Body.String(), "secret") {
		t.Fatal("invitation create response exposed a secret-bearing field")
	}
	var invitation struct {
		ID          string        `json:"id"`
		Email       string        `json:"email"`
		InvitedRole identity.Role `json:"invited_role"`
	}
	if err := json.Unmarshal(invite.Body.Bytes(), &invitation); err != nil {
		t.Fatal(err)
	}
	if invitation.ID == "" || invitation.Email != instructorEmail || invitation.InvitedRole != identity.RoleInstructor {
		t.Fatal("admin invite did not return the safe Instructor invitation projection")
	}
	w, e := outbox.NewWriter(cfg.Admission().ProtectedPayloadKeyVersion(), []byte(cfg.Admission().ProtectedPayloadKey().Expose()))
	if e != nil {
		t.Fatal(e)
	}
	repo, e := transactionalemail.NewRepository(p)
	if e != nil {
		t.Fatal(e)
	}
	var durableIntent, invitationAudit int
	if err := p.QueryRow(t.Context(), `SELECT count(*) FROM outbox_events WHERE event_type='identity.staff_invitation_created' AND aggregate_id=$1::uuid`, invitation.ID).Scan(&durableIntent); err != nil || durableIntent != 1 {
		t.Fatalf("durable invitation email intent = %d, err=%v", durableIntent, err)
	}
	if err := p.QueryRow(t.Context(), `SELECT count(*) FROM identity_security_events WHERE event_type='STAFF_INVITATION_CREATED' AND evidence->>'invitation_id'=$1`, invitation.ID).Scan(&invitationAudit); err != nil || invitationAudit != 1 {
		t.Fatalf("staff invitation audit evidence = %d, err=%v", invitationAudit, err)
	}
	action := t108InvitationAction(t, repo, w)
	canaryEmail := fmt.Sprintf("t108-compromised-%d@example.com", runID)
	canaryInvite := admin.call("POST", "/api/v1/staff-invitations", map[string]string{"email": canaryEmail, "role": "INSTRUCTOR"}, true)
	t108Status(t, canaryInvite, http.StatusCreated, "compromised-password invitation")
	canaryAction := t108InvitationAction(t, repo, w)
	canaryBootstrap := (&t108Client{h: router})
	bootstrapResponse := canaryBootstrap.call("GET", "/api/v1/session/bootstrap", nil, false)
	var canaryBootstrapPayload struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(bootstrapResponse.Body.Bytes(), &canaryBootstrapPayload); err != nil {
		t.Fatal(err)
	}
	canaryBootstrap.csrf = canaryBootstrapPayload.CSRFToken
	t108Status(t, canaryBootstrap.call("POST", "/api/v1/staff-invitation-completions", map[string]string{"bearer": canaryAction, "display_name": "Canary Instructor", "password": t108CompromisedPass}, true), http.StatusUnprocessableEntity, "compromised-password onboarding")
	var canaryPending int
	if err := p.QueryRow(t.Context(), `SELECT count(*) FROM staff_invitations WHERE normalized_email=$1 AND state='PENDING'`, canaryEmail).Scan(&canaryPending); err != nil || canaryPending != 1 {
		t.Fatalf("compromised-password invitation state = %d, err=%v", canaryPending, err)
	}
	anon := &t108Client{h: router}
	pr := httptest.NewRequest("GET", "/api/v1/staff-invitations/preview", nil)
	pr.Header.Set("X-Gradex-Invitation-Bearer", action)
	pw := httptest.NewRecorder()
	router.ServeHTTP(pw, pr)
	t108Status(t, pw, http.StatusOK, "invitation preview")
	var preview struct {
		InvitedRole identity.Role `json:"invited_role"`
		State       string        `json:"state"`
	}
	if err := json.Unmarshal(pw.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.InvitedRole != identity.RoleInstructor || preview.State != string(identity.InvitationPending) {
		t.Fatal("preview did not return the pending Instructor invitation")
	}
	bootstrap := anon.call("GET", "/api/v1/session/bootstrap", nil, false)
	var anonymousSession struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(bootstrap.Body.Bytes(), &anonymousSession)
	anon.csrf = anonymousSession.CSRFToken
	complete := anon.call("POST", "/api/v1/staff-invitation-completions", map[string]string{"bearer": action, "display_name": "Test Instructor", "password": t108Pass}, true)
	if complete.Code < 200 || complete.Code >= 300 {
		t.Fatalf("complete %d %s logs=%s", complete.Code, complete.Body.String(), logs.String())
	}
	if replay := anon.call("POST", "/api/v1/staff-invitation-completions", map[string]string{"bearer": action, "display_name": "Test Instructor", "password": t108Pass}, true); replay.Code != http.StatusBadRequest {
		t.Fatalf("consumed invitation completion = %d, want 400", replay.Code)
	}
	invalidPreview := httptest.NewRequest("GET", "/api/v1/staff-invitations/preview", nil)
	invalidPreview.Header.Set("X-Gradex-Invitation-Bearer", "malformed")
	invalidWriter := httptest.NewRecorder()
	router.ServeHTTP(invalidWriter, invalidPreview)
	t108Status(t, invalidWriter, http.StatusBadRequest, "malformed invitation preview")
	var accountID, role, status string
	if err := p.QueryRow(t.Context(), `SELECT id::text, role::text, status::text FROM accounts WHERE normalized_email=$1`, instructorEmail).Scan(&accountID, &role, &status); err != nil {
		t.Fatal(err)
	}
	if role != string(identity.RoleInstructor) || status != string(identity.StatusActive) {
		t.Fatalf("completed account = %s/%s, want INSTRUCTOR/ACTIVE", role, status)
	}
	var consumed int
	if err := p.QueryRow(t.Context(), `SELECT count(*) FROM staff_invitations WHERE id=$1::uuid AND state='CONSUMED'`, invitation.ID).Scan(&consumed); err != nil || consumed != 1 {
		t.Fatalf("consumed invitation count = %d, err=%v", consumed, err)
	}
	instructor := &t108Client{h: router}
	instructor.login(t, instructorEmail)
	t108Status(t, instructor.call("GET", "/api/v1/courses", nil, false), http.StatusOK, "active Instructor authoring route")
	t108Denied(t, unauthenticated.call("GET", "/api/v1/courses", nil, false), "unauthenticated Instructor authoring route")
	t108Denied(t, student.call("GET", "/api/v1/courses", nil, false), "student Instructor authoring route")
	t108Denied(t, instructor.call("POST", "/api/v1/staff-invitations", map[string]string{"email": "t108-denied@example.com", "role": "INSTRUCTOR"}, true), "Instructor invite")

	t108Denied(t, unauthenticated.call("GET", "/api/v1/staff-invitations/instructors", nil, false), "unauthenticated Instructor list")
	t108Denied(t, student.call("GET", "/api/v1/staff-invitations/instructors", nil, false), "student Instructor list")
	t108Denied(t, instructor.call("GET", "/api/v1/staff-invitations/instructors", nil, false), "Instructor Instructor list")
	list := admin.call("GET", "/api/v1/staff-invitations/instructors", nil, false)
	t108Status(t, list, http.StatusOK, "Admin Instructor list")
	lowerList := strings.ToLower(list.Body.String())
	if strings.Contains(lowerList, "password") || strings.Contains(lowerList, "token") || strings.Contains(lowerList, "secret") {
		t.Fatal("Instructor operational list exposed credential or action-secret fields")
	}
	var instructors struct {
		Instructors []struct {
			ID          string    `json:"id"`
			Email       string    `json:"email"`
			DisplayName string    `json:"display_name"`
			Status      string    `json:"status"`
			CreatedAt   time.Time `json:"created_at"`
		} `json:"instructors"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &instructors); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range instructors.Instructors {
		if item.ID == accountID && item.Email == instructorEmail && item.DisplayName == "Test Instructor" && item.Status == string(identity.StatusActive) && item.CreatedAt.IsZero() == false {
			found = true
		}
	}
	if !found {
		t.Fatal("Admin Instructor list omitted the operational Instructor projection")
	}

	suspendPath := "/api/v1/accounts/" + accountID + "/suspension"
	t108Denied(t, unauthenticated.call("POST", suspendPath, map[string]string{"reason": "test"}, true), "unauthenticated suspension")
	t108Denied(t, student.call("POST", suspendPath, map[string]string{"reason": "test"}, true), "student suspension")
	t108Denied(t, instructor.call("POST", suspendPath, map[string]string{"reason": "test"}, true), "Instructor suspension")
	t108Status(t, admin.call("POST", suspendPath, map[string]string{"reason": "T108 verified suspension"}, true), http.StatusOK, "Admin suspension")
	t108Denied(t, instructor.call("GET", "/api/v1/courses", nil, false), "pre-suspension Instructor session after suspension")
	if err := p.QueryRow(t.Context(), `SELECT role::text, status::text FROM accounts WHERE id=$1::uuid`, accountID).Scan(&role, &status); err != nil {
		t.Fatal(err)
	}
	if role != string(identity.RoleInstructor) || status != string(identity.StatusSuspended) {
		t.Fatalf("suspended account = %s/%s, want INSTRUCTOR/SUSPENDED", role, status)
	}

	t108Denied(t, unauthenticated.call("DELETE", suspendPath, map[string]string{"reason": "test"}, true), "unauthenticated reinstatement")
	t108Denied(t, student.call("DELETE", suspendPath, map[string]string{"reason": "test"}, true), "student reinstatement")
	t108Denied(t, instructor.call("DELETE", suspendPath, map[string]string{"reason": "test"}, true), "Instructor reinstatement")
	t108Status(t, admin.call("DELETE", suspendPath, map[string]string{"reason": "T108 verified reinstatement"}, true), http.StatusOK, "Admin reinstatement")
	t108Denied(t, instructor.call("GET", "/api/v1/courses", nil, false), "revoked old Instructor session after reinstatement")
	instructor.login(t, instructorEmail)
	t108Status(t, instructor.call("GET", "/api/v1/courses", nil, false), http.StatusOK, "re-authenticated Instructor authoring route")
	if err := p.QueryRow(t.Context(), `SELECT role::text, status::text FROM accounts WHERE id=$1::uuid`, accountID).Scan(&role, &status); err != nil {
		t.Fatal(err)
	}
	if role != string(identity.RoleInstructor) || status != string(identity.StatusActive) {
		t.Fatalf("reinstated account = %s/%s, want INSTRUCTOR/ACTIVE", role, status)
	}
	var credentialState string
	if err := p.QueryRow(t.Context(), `SELECT state::text FROM password_credentials WHERE account_id=$1::uuid`, accountID).Scan(&credentialState); err != nil {
		t.Fatal(err)
	}
	if credentialState != string(identity.CredentialActive) {
		t.Fatalf("onboarded Instructor credential state = %s, want ACTIVE", credentialState)
	}
	var safeEvidence string
	if err := p.QueryRow(t.Context(), `SELECT coalesce(string_agg(payload, ''), '') FROM (
		SELECT safe_payload::text AS payload FROM outbox_events
		UNION ALL
		SELECT evidence::text AS payload FROM identity_security_events
	) evidence`).Scan(&safeEvidence); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(safeEvidence, t108Pass) || strings.Contains(safeEvidence, t108CompromisedPass) {
		t.Fatal("password plaintext appeared in durable safe evidence")
	}
	if !strings.Contains(logs.String(), `"limiter_outcome":"ALLOW"`) {
		t.Fatal("production staff journey did not emit Redis-backed rate-limit decisions")
	}
}
