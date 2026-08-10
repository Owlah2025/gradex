//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// realJourneyRouter mounts the admission and session boundaries on one engine,
// in the same order and with the same shared anonymous keys NewRouter uses.
//
// The per-boundary integration tests each build half of this. The Student
// journey only exists where the halves meet, so it needs the whole surface: a
// browser that registers must be able to verify, sign in, rotate, log out, and
// recover without changing origin or cookie jar.
func realJourneyRouter(t *testing.T, pool *pgxpool.Pool) *gin.Engine {
	t.Helper()
	english, arabic := identityPolicySets()
	policies, err := identity.NewStaticPolicySetResolver(english, arabic)
	if err != nil {
		t.Fatalf("constructing policies: %v", err)
	}
	compromised, err := identity.NewDeterministicCompromisedSource()
	if err != nil {
		t.Fatalf("constructing password source: %v", err)
	}
	writer, err := outbox.NewWriter("test-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("constructing outbox writer: %v", err)
	}

	admissionRandom := make([]byte, 0, 32*16)
	for offset := byte(0); offset < 16; offset++ {
		admissionRandom = append(admissionRandom, bytes.Repeat([]byte{0x71 + offset}, 32)...)
	}
	admissionService, err := identity.NewAdmissionService(identity.AdmissionServiceOptions{
		Pool: pool, Policies: policies, Compromised: compromised, Outbox: writer,
		VerificationTTL: time.Hour, Now: time.Now,
		Random: bytes.NewReader(admissionRandom),
	})
	if err != nil {
		t.Fatalf("constructing admission service: %v", err)
	}

	recoveryRandom := make([]byte, 0, 32*16)
	for offset := byte(0); offset < 16; offset++ {
		recoveryRandom = append(recoveryRandom, bytes.Repeat([]byte{0xA1 + offset}, 32)...)
	}
	recoveryService, err := identity.NewRecoveryService(identity.RecoveryServiceOptions{
		Pool: pool, Outbox: writer, Compromised: compromised,
		ResetTTL: time.Hour, Now: time.Now,
		Random: bytes.NewReader(recoveryRandom),
	})
	if err != nil {
		t.Fatalf("constructing recovery service: %v", err)
	}

	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading journey settings: %v", err)
	}
	repository, err := identity.NewSessionRepository(identity.SessionRepositoryOptions{
		Pool: pool, Settings: cfg.Sessions(),
		CSRFKey: bytes.Repeat([]byte{0x61}, 32), Now: time.Now,
	})
	if err != nil {
		t.Fatalf("constructing session repository: %v", err)
	}

	limiter, err := ratelimit.New(
		admissionRateStore{allowed: true}, bytes.Repeat([]byte{0x31}, 32), time.Second,
	)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}

	admissionPolicies := make(map[string]ratelimit.Policy)
	for _, endpoint := range []string{
		"student-registrations", "email-verification-requests", "email-verifications",
		"password-reset-requests",
	} {
		admissionPolicies[endpoint] = ratelimit.DevelopmentAdmissionPolicy(endpoint)
	}
	admissionPolicies["password-resets"] = ratelimit.DevelopmentPasswordResetCompletionPolicy()
	readPolicy := ratelimit.DevelopmentPolicySetReadPolicy()
	admissionPolicies[readPolicy.Endpoint] = readPolicy
	bootstrapPolicy := ratelimit.DevelopmentAnonymousBootstrapPolicy()
	admissionPolicies[bootstrapPolicy.Endpoint] = bootstrapPolicy

	admissionFoundation, err := NewAdmissionFoundation(AdmissionFoundationOptions{
		PublicOrigin: "https://gradex.example", CookieSigningKey: bytes.Repeat([]byte("a"), 32),
		CSRFKey: bytes.Repeat([]byte("b"), 32), AnonymousSessionTTL: time.Hour,
		Policies: policies, Service: admissionService, Recovery: recoveryService,
		Limiter: limiter, EndpointPolicies: admissionPolicies,
	})
	if err != nil {
		t.Fatalf("constructing admission foundation: %v", err)
	}

	sessionFoundation, err := NewSessionFoundation(SessionFoundationOptions{
		PublicOrigin:        "https://gradex.example",
		CookieSigningKey:    bytes.Repeat([]byte("a"), 32),
		AnonymousCSRFKey:    bytes.Repeat([]byte("b"), 32),
		AnonymousSessionTTL: time.Hour,
		Repository:          repository,
		Compromised:         compromised,
		Limiter:             limiter,
		EndpointPolicies:    testSessionEndpointPolicies(),
	})
	if err != nil {
		t.Fatalf("constructing session foundation: %v", err)
	}

	router := gin.New()
	router.Use(requestIDMiddleware())
	v1 := router.Group("/api/v1")
	// Session routes first, then admission without its own bootstrap route, so
	// the anonymous bootstrap is registered exactly once — the same ordering
	// NewRouter applies when both boundaries are enabled.
	mountSessionRoutes(
		v1, sessionFoundation,
		sessionFoundation.authenticator, identity.NewDBPrincipalResolver(pool),
		logging.New(io.Discard, "gradex-api-test", "development", logging.LevelFromString("info")),
	)
	mountAdmissionRoutesWithBootstrap(v1, admissionFoundation, false)
	return router
}

type journeyBrowser struct {
	t       *testing.T
	router  *gin.Engine
	cookies map[string]*http.Cookie
	csrf    string
}

func newJourneyBrowser(t *testing.T, router *gin.Engine) *journeyBrowser {
	t.Helper()
	browser := &journeyBrowser{
		t: t, router: router, cookies: map[string]*http.Cookie{},
	}
	browser.bootstrap()
	return browser
}

// bootstrap obtains the anonymous capability the admission boundary requires.
func (b *journeyBrowser) bootstrap() {
	b.t.Helper()
	response := b.send(http.MethodGet, "/api/v1/session/bootstrap", "")
	if response.Code != http.StatusOK {
		b.t.Fatalf("anonymous bootstrap = %d, want 200", response.Code)
	}
	var body struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		b.t.Fatalf("decoding bootstrap: %v", err)
	}
	if body.CSRFToken == "" {
		b.t.Fatal("bootstrap returned no CSRF token")
	}
	b.csrf = body.CSRFToken
}

// send performs one request carrying the browser's whole cookie jar, and
// absorbs any Set-Cookie the response returns, so credential rotation is
// followed exactly as a browser would follow it.
func (b *journeyBrowser) send(method, path, body string) *httptest.ResponseRecorder {
	b.t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Origin", "https://gradex.example")
	if b.csrf != "" {
		request.Header.Set(csrfHeaderName, b.csrf)
	}
	for _, cookie := range b.cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	b.router.ServeHTTP(response, request)
	for _, raw := range response.Result().Cookies() {
		if raw.Value == "" {
			delete(b.cookies, raw.Name)
			continue
		}
		b.cookies[raw.Name] = raw
	}
	return response
}

// sessionCookieName finds the authenticated cookie currently held.
func (b *journeyBrowser) sessionCookie() *http.Cookie {
	for name, cookie := range b.cookies {
		if strings.Contains(name, "session") && strings.HasPrefix(name, "__Host-") {
			return cookie
		}
	}
	return nil
}

const (
	journeyEmail       = "journey.student@example.com"
	journeyPassword    = "correct horse battery staple"
	journeyNewPassword = "a completely different long passphrase"
)

func journeyRegistration() string {
	return `{"display_name":"Nora Ahmed","email":"` + journeyEmail + `",` +
		`"password":"` + journeyPassword + `","locale":"en",` +
		`"policy_set_id":"registration-v1"}`
}

// TestCompleteStudentAuthenticationJourney runs the whole S1B chain in one
// browser against real PostgreSQL:
//
//	register → verify → login → rotate → reject superseded → logout →
//	recover → login with the new password
//
// Each S1B slice has per-route coverage already. What only this proves is that
// the slices compose: that the artifacts one step produces are the artifacts
// the next step accepts, through one origin and one cookie jar.
func TestCompleteStudentAuthenticationJourney(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	router := realJourneyRouter(t, pool)
	dispatcher, sender := transactionalEmailHarness(t, pool, "test-v1", bytes.Repeat([]byte{0x42}, 32))
	browser := newJourneyBrowser(t, router)
	ctx := context.Background()

	// 1. Register.
	if response := browser.send(
		http.MethodPost, "/api/v1/student-registrations", journeyRegistration(),
	); response.Code != http.StatusAccepted {
		t.Fatalf("registration = %d, want 202 (%s)", response.Code, response.Body)
	}

	// 2. Dispatch and follow the credential from the actual rendered email.
	dispatchTransactionalEmail(t, dispatcher)
	verificationToken := actionCredential(t, sender.Messages(), "/verify-email/result")
	if response := browser.send(
		http.MethodPost, "/api/v1/email-verifications",
		`{"token":"`+verificationToken+`"}`,
	); response.Code != http.StatusOK {
		t.Fatalf("verification = %d, want 200 (%s)", response.Code, response.Body)
	}

	// 3. Sign in.
	login := `{"email":"` + journeyEmail + `","password":"` + journeyPassword + `"}`
	response := browser.send(http.MethodPost, "/api/v1/sessions", login)
	if response.Code != http.StatusCreated {
		t.Fatalf("login = %d, want 201 (%s)", response.Code, response.Body)
	}
	browser.csrf = csrfFromSessionResponse(t, response.Body.Bytes())
	superseded := browser.sessionCookie()
	if superseded == nil {
		t.Fatal("login issued no session cookie")
	}

	// 4. Rotate the credential deliberately.
	renewal := browser.send(http.MethodPost, "/api/v1/session-renewals", "")
	if renewal.Code != http.StatusOK {
		t.Fatalf("renewal = %d, want 200 (%s)", renewal.Code, renewal.Body)
	}
	browser.csrf = csrfFromSessionResponse(t, renewal.Body.Bytes())
	current := browser.sessionCookie()
	if current == nil || current.Value == superseded.Value {
		t.Fatal("renewal did not replace the session credential")
	}

	// 5. The superseded credential is refused. A separate request carries the
	//    old cookie deliberately, without disturbing the browser's own jar.
	stale := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	stale.Header.Set("Origin", "https://gradex.example")
	stale.AddCookie(superseded)
	staleResponse := httptest.NewRecorder()
	router.ServeHTTP(staleResponse, stale)
	if staleResponse.Code == http.StatusOK {
		t.Fatal("a superseded session credential was accepted")
	}

	// 6. Log out with the current credential.
	logout := browser.send(http.MethodDelete, "/api/v1/session", "")
	if logout.Code != http.StatusOK && logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d (%s)", logout.Code, logout.Body)
	}
	after := browser.send(http.MethodGet, "/api/v1/session", "")
	if after.Code == http.StatusOK {
		t.Fatal("session resolved after logout")
	}

	// 7. Sign in again, so recovery has a live family to invalidate.
	//
	// Without this the next assertion would be vacuous: logout already revoked
	// the only family, so "no live families after recovery" would hold whether
	// or not recovery revoked anything.
	browser.cookies = map[string]*http.Cookie{}
	browser.csrf = ""
	browser.bootstrap()
	second := browser.send(http.MethodPost, "/api/v1/sessions", login)
	if second.Code != http.StatusCreated {
		t.Fatalf("second login = %d, want 201 (%s)", second.Code, second.Body)
	}
	browser.csrf = csrfFromSessionResponse(t, second.Body.Bytes())
	if live := countJourneySessions(t, pool, "ACTIVE"); live != 1 {
		t.Fatalf("live families before recovery = %d, want 1", live)
	}

	// 8. Recover from a different browser, as someone locked out would. The
	//    signed-in browser above is left untouched so recovery's effect on it
	//    is observable.
	other := newJourneyBrowser(t, router)
	if response := other.send(
		http.MethodPost, "/api/v1/password-reset-requests",
		`{"email":"`+journeyEmail+`"}`,
	); response.Code != http.StatusAccepted {
		t.Fatalf("reset request = %d, want 202 (%s)", response.Code, response.Body)
	}
	dispatchTransactionalEmail(t, dispatcher)
	resetToken := actionCredential(t, sender.Messages(), "/recover/reset")
	completion := other.send(
		http.MethodPost, "/api/v1/password-resets",
		`{"token":"`+resetToken+`","password":"`+journeyNewPassword+`"}`,
	)
	if completion.Code != http.StatusOK {
		t.Fatalf("reset completion = %d, want 200 (%s)", completion.Code, completion.Body)
	}
	// Recovery must not hand back a session.
	if other.sessionCookie() != nil {
		t.Fatal("password recovery issued a session cookie")
	}
	if live := countJourneySessions(t, pool, "ACTIVE"); live != 0 {
		t.Fatal("a session family survived password recovery")
	}
	// The still-open browser is now dead too, not merely marked dead in the
	// database.
	if stillOpen := browser.send(http.MethodGet, "/api/v1/session", ""); stillOpen.Code == http.StatusOK {
		t.Fatal("a session opened before recovery still resolves after it")
	}

	// 9. The old password is dead and the new one works.
	stalePassword := other.send(http.MethodPost, "/api/v1/sessions", login)
	if stalePassword.Code == http.StatusCreated {
		t.Fatal("the pre-recovery password still signs in")
	}
	newLogin := `{"email":"` + journeyEmail + `","password":"` + journeyNewPassword + `"}`
	final := other.send(http.MethodPost, "/api/v1/sessions", newLogin)
	if final.Code != http.StatusCreated {
		t.Fatalf("login after recovery = %d, want 201 (%s)", final.Code, final.Body)
	}

	// The journey left the evidence trail S1B promises, in order.
	assertJourneyEvidence(t, ctx, pool)
}

func countJourneySessions(t *testing.T, pool *pgxpool.Pool, state string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions s
		   JOIN accounts a ON a.id = s.account_id
		  WHERE a.normalized_email = $1 AND s.state = $2::session_state`,
		journeyEmail, state,
	).Scan(&count); err != nil {
		t.Fatalf("counting %s families: %v", state, err)
	}
	return count
}

// assertJourneyEvidence checks that the security record a reviewer would read
// after the fact actually describes the journey that happened.
func assertJourneyEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT e.event_type
		   FROM identity_security_events e
		   JOIN accounts a ON a.id = e.account_id
		  WHERE a.normalized_email = $1
		  ORDER BY e.occurred_at, e.id`,
		journeyEmail,
	)
	if err != nil {
		t.Fatalf("reading journey evidence: %v", err)
	}
	defer rows.Close()
	var seen []string
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatalf("scanning evidence: %v", err)
		}
		seen = append(seen, eventType)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating evidence: %v", err)
	}

	for _, required := range []string{
		"STUDENT_REGISTRATION_ACCEPTED",
		"STUDENT_EMAIL_VERIFIED",
		"SESSION_CREATED",
		"SESSION_RENEWED",
		"SESSION_LOGGED_OUT",
		"PASSWORD_RESET_REQUESTED",
		"PASSWORD_RESET_COMPLETED",
	} {
		found := false
		for _, event := range seen {
			if event == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("journey recorded no %s; saw %v", required, seen)
		}
	}
}
