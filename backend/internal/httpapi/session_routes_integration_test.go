//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

const httpSessionPassword = "correct session login passphrase 9"

func realSessionRouter(t *testing.T, pool *pgxpool.Pool) *gin.Engine {
	t.Helper()
	return realSessionRouterWithScreening(t, pool, testCompromisedSource(t))
}

// realSessionRouterWithScreening takes the compromised-password source
// explicitly, so a test can prove that screening actually runs on the
// replacement password rather than inferring it from a policy refusal.
func realSessionRouterWithScreening(
	t *testing.T,
	pool *pgxpool.Pool,
	compromised identity.CompromisedRangeSource,
) *gin.Engine {
	t.Helper()
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading session settings: %v", err)
	}
	repository, err := identity.NewSessionRepository(identity.SessionRepositoryOptions{
		Pool: pool, Settings: cfg.Sessions(),
		CSRFKey: bytes.Repeat([]byte{0x61}, 32), Now: time.Now,
	})
	if err != nil {
		t.Fatalf("constructing session repository: %v", err)
	}
	limiter, err := ratelimit.New(
		admissionRateStore{allowed: true},
		bytes.Repeat([]byte{0x31}, 32),
		time.Second,
	)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	foundation, err := NewSessionFoundation(SessionFoundationOptions{
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
	// The real cookie authenticator and the real database principal resolver.
	// The password-change route is only meaningful against them: it is the
	// resolver that reports CHANGE_REQUIRED and the policy that decides what a
	// principal in that state may reach.
	mountSessionRoutes(
		router.Group("/api/v1"), foundation,
		foundation.authenticator, identity.NewDBPrincipalResolver(pool),
		logging.New(io.Discard, "gradex-api-test", "development", logging.LevelFromString("info")),
	)
	return router
}

func insertHTTPSessionAccount(
	t *testing.T,
	pool *pgxpool.Pool,
	email string,
	status identity.AccountStatus,
	verified bool,
) {
	t.Helper()
	hash, err := identity.HashPassword(httpSessionPassword)
	if err != nil {
		t.Fatalf("hashing session fixture: %v", err)
	}
	var verifiedAt *time.Time
	if verified {
		now := time.Now().UTC()
		verifiedAt = &now
	}
	_, err = pool.Exec(context.Background(),
		`WITH account AS (
		   INSERT INTO accounts
		     (normalized_email, email, role, status, display_name, email_verified_at)
		   VALUES ($1, $1, 'STUDENT', $2, 'Session Student', $3)
		   RETURNING id
		 )
		 INSERT INTO password_credentials (account_id, password_hash, state)
		 SELECT id, $4, 'ACTIVE' FROM account`,
		email, status, verifiedAt, hash.Expose(),
	)
	if err != nil {
		t.Fatalf("creating session Account: %v", err)
	}
}

type sessionHTTPResponse struct {
	status       int
	body         []byte
	contentType  string
	cacheControl string
	challenge    string
	setCookie    []string
}

func postSessionLogin(
	t *testing.T,
	router *gin.Engine,
	email string,
	password string,
) sessionHTTPResponse {
	t.Helper()
	cookie, csrf := bootstrapAdmissionBrowser(t, router)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sessions",
		strings.NewReader(`{"email":`+quotedJSON(email)+`,"password":`+quotedJSON(password)+`}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://gradex.example")
	request.Header.Set(csrfHeaderName, csrf)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return sessionHTTPResponse{
		status: response.Code, body: response.Body.Bytes(),
		contentType:  response.Header().Get("Content-Type"),
		cacheControl: response.Header().Get("Cache-Control"),
		challenge:    response.Header().Get("WWW-Authenticate"),
		setCookie:    response.Header().Values("Set-Cookie"),
	}
}

func quotedJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func normalizedAuthenticationProblem(
	t *testing.T,
	response sessionHTTPResponse,
) problem.Problem {
	t.Helper()
	var got problem.Problem
	if err := json.Unmarshal(response.body, &got); err != nil {
		t.Fatalf("decoding authentication problem: %v", err)
	}
	got.RequestID = ""
	got.Instance = ""
	return got
}

func TestSessionHiddenLoginStatesHaveEquivalentHTTPContract(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	insertHTTPSessionAccount(t, pool, "active@example.com", identity.StatusActive, true)
	insertHTTPSessionAccount(t, pool, "unverified@example.com", identity.StatusActive, false)
	insertHTTPSessionAccount(t, pool, "inactive@example.com", identity.StatusSuspended, true)
	router := realSessionRouter(t, pool)

	responses := []sessionHTTPResponse{
		postSessionLogin(t, router, "unknown@example.com", httpSessionPassword),
		postSessionLogin(t, router, "active@example.com", "wrong password"),
		postSessionLogin(t, router, "unverified@example.com", httpSessionPassword),
		postSessionLogin(t, router, "inactive@example.com", httpSessionPassword),
	}
	wantProblem := normalizedAuthenticationProblem(t, responses[0])
	for i, response := range responses {
		if response.status != http.StatusUnauthorized ||
			response.contentType != problem.ContentType ||
			response.cacheControl != "no-store" ||
			response.challenge != `GradexSession realm="gradex-web"` ||
			len(response.setCookie) != 0 {
			t.Fatalf("hidden response %d differs: %+v", i, response)
		}
		if got := normalizedAuthenticationProblem(t, response); !reflect.DeepEqual(got, wantProblem) {
			t.Fatalf("hidden problem %d = %#v, want %#v", i, got, wantProblem)
		}
	}

	var families int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&families); err != nil {
		t.Fatalf("counting hidden-state families: %v", err)
	}
	if families != 0 {
		t.Fatalf("hidden login failures created %d session families", families)
	}
}

func TestSessionHTTPLifecycleStoresOnlyDigestsAndRevokesReuse(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	insertHTTPSessionAccount(t, pool, "student@example.com", identity.StatusActive, true)
	router := realSessionRouter(t, pool)

	login := postSessionLogin(t, router, "student@example.com", httpSessionPassword)
	if login.status != http.StatusCreated || login.cacheControl != "no-store" {
		t.Fatalf("login = %+v body=%s", login, login.body)
	}
	credential := authenticatedCookie(t, login.setCookie)
	csrf := csrfFromSessionResponse(t, login.body)

	var storedCredential, storedCSRF string
	if err := pool.QueryRow(context.Background(),
		`SELECT credential_digest, csrf_digest FROM session_credentials`,
	).Scan(&storedCredential, &storedCSRF); err != nil {
		t.Fatalf("loading stored session digests: %v", err)
	}
	if storedCredential == credential.Value || storedCSRF == csrf ||
		strings.Contains(string(login.body), credential.Value) {
		t.Fatal("session plaintext crossed into PostgreSQL or JSON")
	}

	resolved := serveSessionRequest(router, sessionRequest(
		http.MethodGet, "/api/v1/session", credential, "",
	))
	if resolved.Code != http.StatusOK || resolved.Header().Get("Set-Cookie") != "" ||
		csrfFromSessionResponse(t, resolved.Body.Bytes()) != csrf {
		t.Fatalf("session resolution rotated or failed: %d %s", resolved.Code, resolved.Body.String())
	}

	renewed := serveSessionRequest(router, sessionRequest(
		http.MethodPost, "/api/v1/session-renewals", credential, csrf,
	))
	if renewed.Code != http.StatusOK {
		t.Fatalf("renewal = %d: %s", renewed.Code, renewed.Body.String())
	}
	replacement := authenticatedCookie(t, renewed.Header().Values("Set-Cookie"))
	if replacement.Value == credential.Value {
		t.Fatal("renewal did not rotate the opaque credential")
	}

	firstStale := serveSessionRequest(router, sessionRequest(
		http.MethodGet, "/api/v1/session", credential, "",
	))
	if firstStale.Code != http.StatusUnauthorized ||
		!strings.Contains(firstStale.Body.String(), `"code":"SESSION_REPLACED"`) {
		t.Fatalf("first stale read = %d: %s", firstStale.Code, firstStale.Body.String())
	}
	reused := serveSessionRequest(router, sessionRequest(
		http.MethodGet, "/api/v1/session", credential, "",
	))
	if reused.Code != http.StatusUnauthorized ||
		!strings.Contains(reused.Body.String(), `"code":"SESSION_REUSE_DETECTED"`) {
		t.Fatalf("confirmed reuse = %d: %s", reused.Code, reused.Body.String())
	}
	revoked := serveSessionRequest(router, sessionRequest(
		http.MethodGet, "/api/v1/session", replacement, "",
	))
	if revoked.Code != http.StatusUnauthorized ||
		!strings.Contains(revoked.Body.String(), `"code":"AUTHENTICATION_REQUIRED"`) {
		t.Fatalf("replacement survived family revocation = %d: %s", revoked.Code, revoked.Body.String())
	}
}

func TestConcurrentHTTPRenewalHasOneWinner(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	insertHTTPSessionAccount(t, pool, "student@example.com", identity.StatusActive, true)
	router := realSessionRouter(t, pool)
	login := postSessionLogin(t, router, "student@example.com", httpSessionPassword)
	credential := authenticatedCookie(t, login.setCookie)
	csrf := csrfFromSessionResponse(t, login.body)

	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- serveSessionRequest(router, sessionRequest(
				http.MethodPost, "/api/v1/session-renewals", credential, csrf,
			))
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	successes, reuseDenials := 0, 0
	for result := range results {
		switch {
		case result.Code == http.StatusOK:
			successes++
		case result.Code == http.StatusUnauthorized &&
			strings.Contains(result.Body.String(), `"code":"SESSION_REUSE_DETECTED"`):
			reuseDenials++
		default:
			t.Fatalf("unexpected concurrent renewal = %d: %s", result.Code, result.Body.String())
		}
	}
	if successes != 1 || reuseDenials != 1 {
		t.Fatalf("renewal race successes=%d reuse=%d", successes, reuseDenials)
	}
}

func TestSessionLogoutRevokesBeforeCookieClear(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	insertHTTPSessionAccount(t, pool, "student@example.com", identity.StatusActive, true)
	router := realSessionRouter(t, pool)
	login := postSessionLogin(t, router, "student@example.com", httpSessionPassword)
	credential := authenticatedCookie(t, login.setCookie)
	csrf := csrfFromSessionResponse(t, login.body)
	logout := serveSessionRequest(router, sessionRequest(
		http.MethodDelete, "/api/v1/session", credential, csrf,
	))
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d: %s", logout.Code, logout.Body.String())
	}
	cleared := authenticatedCookie(t, logout.Header().Values("Set-Cookie"))
	if cleared.MaxAge >= 0 {
		t.Fatalf("logout cookie was not expired: %#v", cleared)
	}
	after := serveSessionRequest(router, sessionRequest(
		http.MethodGet, "/api/v1/session", credential, "",
	))
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out credential resolved with %d", after.Code)
	}
}

// insertRestrictedHTTPAccount creates an Account in exactly the state
// cmd/bootstrap-admin leaves behind: verified, ACTIVE, and holding a
// CHANGE_REQUIRED credential.
func insertRestrictedHTTPAccount(
	t *testing.T,
	pool *pgxpool.Pool,
	email string,
	role identity.Role,
) string {
	t.Helper()
	hash, err := identity.HashPassword(httpSessionPassword)
	if err != nil {
		t.Fatalf("hashing restricted fixture: %v", err)
	}
	now := time.Now().UTC()
	var accountID string
	err = pool.QueryRow(context.Background(),
		`WITH account AS (
		   INSERT INTO accounts
		     (normalized_email, email, role, status, display_name, email_verified_at)
		   VALUES ($1, $1, $2, 'ACTIVE', 'Restricted Principal', $3)
		   RETURNING id
		 ), credential AS (
		   INSERT INTO password_credentials (account_id, password_hash, state)
		   SELECT id, $4, 'CHANGE_REQUIRED' FROM account
		 )
		 SELECT id::text FROM account`,
		email, string(role), now, hash.Expose(),
	).Scan(&accountID)
	if err != nil {
		t.Fatalf("creating restricted Account: %v", err)
	}
	return accountID
}

func credentialStateOf(t *testing.T, pool *pgxpool.Pool, accountID string) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(context.Background(),
		`SELECT state::text FROM password_credentials WHERE account_id = $1::uuid`,
		accountID,
	).Scan(&state); err != nil {
		t.Fatalf("reading credential state: %v", err)
	}
	return state
}

func postPasswordChange(
	router *gin.Engine,
	cookie *http.Cookie,
	csrf string,
	current string,
	next string,
) *httptest.ResponseRecorder {
	body := `{"current_password":` + quotedJSON(current) +
		`,"new_password":` + quotedJSON(next) + `}`
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/password-changes", strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://gradex.example")
	request.Header.Set(csrfHeaderName, csrf)
	request.AddCookie(cookie)
	return serveSessionRequest(router, request)
}

// The launch defect, end to end against a real database.
//
// A bootstrap Administrator could sign in and then do nothing: every privileged
// request answered 403 PASSWORD_CHANGE_REQUIRED and no route existed to clear
// the state. This walks the whole recovery — sign in, observe the restriction
// in the session representation, change the password, observe it cleared — and
// asserts the credential really reached ACTIVE in PostgreSQL rather than the
// response merely claiming so.
func TestRestrictedAdminChangesItsPasswordAndBecomesActive(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	accountID := insertRestrictedHTTPAccount(t, pool, "bootstrap-admin@example.com", identity.RoleAdmin)
	router := realSessionRouter(t, pool)

	login := postSessionLogin(t, router, "bootstrap-admin@example.com", httpSessionPassword)
	if login.status != http.StatusCreated {
		t.Fatalf("the bootstrap Administrator could not sign in: %d %s", login.status, login.body)
	}
	// The browser learns it is restricted from the login response itself, which
	// is what lets it redirect instead of walking into repeated 403s.
	if !strings.Contains(string(login.body), `"password_change_required":true`) {
		t.Fatalf("login did not report the restriction: %s", login.body)
	}
	credential := authenticatedCookie(t, login.setCookie)
	csrf := csrfFromSessionResponse(t, login.body)

	// The session read reports the same fact, so a page reload cannot lose it.
	resolved := serveSessionRequest(router, sessionRequest(
		http.MethodGet, "/api/v1/session", credential, "",
	))
	if !strings.Contains(resolved.Body.String(), `"password_change_required":true`) {
		t.Fatalf("session resolution did not report the restriction: %s", resolved.Body.String())
	}

	changed := postPasswordChange(
		router, credential, csrf, httpSessionPassword, "a brand new launch passphrase 9",
	)
	if changed.Code != http.StatusOK {
		t.Fatalf("password change = %d: %s", changed.Code, changed.Body.String())
	}
	if !strings.Contains(changed.Body.String(), `"password_change_required":false`) {
		t.Fatalf("the completed change still reports the restriction: %s", changed.Body.String())
	}
	if state := credentialStateOf(t, pool, accountID); state != "ACTIVE" {
		t.Fatalf("credential state = %q, want ACTIVE", state)
	}

	// The change rotated the family, exactly as a password change must: the
	// browser holds new credentials and the old ones are superseded.
	replacement := authenticatedCookie(t, changed.Header().Values("Set-Cookie"))
	replacementCSRF := csrfFromSessionResponse(t, changed.Body.Bytes())
	if replacement.Value == credential.Value || replacementCSRF == csrf {
		t.Fatal("the password change did not rotate the browser credentials")
	}

	// The rotated session is immediately usable — including its derived CSRF
	// token, which a replacement minted outside the session authority's own
	// derivation would have broken on the very next read.
	afterChange := serveSessionRequest(router, sessionRequest(
		http.MethodGet, "/api/v1/session", replacement, "",
	))
	if afterChange.Code != http.StatusOK {
		t.Fatalf("the rotated session did not resolve: %d %s", afterChange.Code, afterChange.Body.String())
	}
	if csrfFromSessionResponse(t, afterChange.Body.Bytes()) != replacementCSRF {
		t.Fatal("the rotated session's CSRF token could not be re-derived")
	}

	// The superseded credential is refused rather than silently accepted.
	stale := serveSessionRequest(router, sessionRequest(
		http.MethodGet, "/api/v1/session", credential, "",
	))
	if stale.Code != http.StatusUnauthorized {
		t.Fatalf("the pre-change credential still resolved: %d", stale.Code)
	}

	// The old password stops authenticating and the new one starts.
	oldPassword := postSessionLogin(t, router, "bootstrap-admin@example.com", httpSessionPassword)
	if oldPassword.status != http.StatusUnauthorized {
		t.Fatalf("the replaced password still authenticates: %d", oldPassword.status)
	}
	newPassword := postSessionLogin(
		t, router, "bootstrap-admin@example.com", "a brand new launch passphrase 9",
	)
	if newPassword.status != http.StatusCreated {
		t.Fatalf("the new password does not authenticate: %d %s", newPassword.status, newPassword.body)
	}
	if !strings.Contains(string(newPassword.body), `"password_change_required":false`) {
		t.Fatalf("a re-login after the change is still restricted: %s", newPassword.body)
	}
}

// The same lifecycle applies to an Instructor, so a staff account created in a
// restricted state is not stranded either.
func TestRestrictedInstructorFollowsTheSamePasswordChangeLifecycle(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	accountID := insertRestrictedHTTPAccount(t, pool, "restricted-instructor@example.com", identity.RoleInstructor)
	router := realSessionRouter(t, pool)

	login := postSessionLogin(t, router, "restricted-instructor@example.com", httpSessionPassword)
	if login.status != http.StatusCreated ||
		!strings.Contains(string(login.body), `"password_change_required":true`) {
		t.Fatalf("restricted Instructor login = %d: %s", login.status, login.body)
	}
	changed := postPasswordChange(
		router,
		authenticatedCookie(t, login.setCookie),
		csrfFromSessionResponse(t, login.body),
		httpSessionPassword,
		"an instructor replacement passphrase 4",
	)
	if changed.Code != http.StatusOK {
		t.Fatalf("Instructor password change = %d: %s", changed.Code, changed.Body.String())
	}
	if state := credentialStateOf(t, pool, accountID); state != "ACTIVE" {
		t.Fatalf("Instructor credential state = %q, want ACTIVE", state)
	}
	if !strings.Contains(changed.Body.String(), `"role":"INSTRUCTOR"`) {
		t.Fatalf("the rotated session lost its role: %s", changed.Body.String())
	}
}

// Every refusal must leave the credential, the restriction, and the session
// exactly as they were. A password change that half-fails is worse than one
// that fails.
func TestRefusedPasswordChangeLeavesTheAccountUntouched(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	accountID := insertRestrictedHTTPAccount(t, pool, "refusals@example.com", identity.RoleAdmin)
	router := realSessionRouter(t, pool)
	login := postSessionLogin(t, router, "refusals@example.com", httpSessionPassword)
	credential := authenticatedCookie(t, login.setCookie)
	csrf := csrfFromSessionResponse(t, login.body)

	for name, tc := range map[string]struct {
		current string
		next    string
		status  int
		code    string
	}{
		"wrong current password": {
			"not the current password", "a brand new launch passphrase 9",
			http.StatusUnauthorized, "AUTHENTICATION_FAILED",
		},
		"new password fails policy": {
			httpSessionPassword, "short",
			http.StatusUnprocessableEntity, "VALIDATION_FAILED",
		},
		// Reuse is refused by the domain even though the caller proved the
		// value: re-entering the temporary password would satisfy the workflow
		// while changing nothing.
		"new password repeats the current one": {
			httpSessionPassword, httpSessionPassword,
			http.StatusUnprocessableEntity, "VALIDATION_FAILED",
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := postPasswordChange(router, credential, csrf, tc.current, tc.next)
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("body lacks %s: %s", tc.code, response.Body.String())
			}
			for _, secret := range []string{httpSessionPassword, tc.current, tc.next} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("the refusal echoed a password: %s", response.Body.String())
				}
			}
			if state := credentialStateOf(t, pool, accountID); state != "CHANGE_REQUIRED" {
				t.Fatalf("a refused change moved the credential to %q", state)
			}
			// The session was not rotated, so the browser can retry with the
			// credentials it already holds.
			if len(response.Result().Cookies()) != 0 {
				t.Fatal("a refused change rotated the browser cookie")
			}
			retry := serveSessionRequest(router, sessionRequest(
				http.MethodGet, "/api/v1/session", credential, "",
			))
			if retry.Code != http.StatusOK {
				t.Fatalf("a refused change invalidated the caller's session: %d", retry.Code)
			}
		})
	}

	// After all of that the original password still works.
	if login := postSessionLogin(t, router, "refusals@example.com", httpSessionPassword); login.status != http.StatusCreated {
		t.Fatalf("the original password stopped working after refused changes: %d", login.status)
	}
}

// A compromised replacement is refused by the screening source, not merely by
// the length and shape rules.
func TestCompromisedReplacementPasswordIsRefused(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	accountID := insertRestrictedHTTPAccount(t, pool, "screened@example.com", identity.RoleAdmin)

	const breached = "a widely breached launch passphrase 3"
	digest := sha256.Sum256([]byte(breached))
	source, err := identity.NewDeterministicCompromisedSource(
		strings.ToUpper(hex.EncodeToString(digest[:])),
	)
	if err != nil {
		t.Fatalf("constructing breached fixture: %v", err)
	}
	router := realSessionRouterWithScreening(t, pool, source)

	login := postSessionLogin(t, router, "screened@example.com", httpSessionPassword)
	response := postPasswordChange(
		router,
		authenticatedCookie(t, login.setCookie),
		csrfFromSessionResponse(t, login.body),
		httpSessionPassword,
		breached,
	)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a breached replacement was accepted: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), breached) {
		t.Fatalf("the refusal echoed the rejected password: %s", response.Body.String())
	}
	if state := credentialStateOf(t, pool, accountID); state != "CHANGE_REQUIRED" {
		t.Fatalf("a screened-out password still changed the credential to %q", state)
	}

	// The same password with screening satisfied is accepted, so the refusal
	// above is the screening source and not an unrelated policy rule.
	permissive := realSessionRouter(t, pool)
	permissiveLogin := postSessionLogin(t, permissive, "screened@example.com", httpSessionPassword)
	accepted := postPasswordChange(
		permissive,
		authenticatedCookie(t, permissiveLogin.setCookie),
		csrfFromSessionResponse(t, permissiveLogin.body),
		httpSessionPassword,
		breached,
	)
	if accepted.Code != http.StatusOK {
		t.Fatalf("the same password was refused without screening too: %d %s",
			accepted.Code, accepted.Body.String())
	}
}

// A password change revokes every other family the Account holds, so a stolen
// session elsewhere does not survive the recovery.
func TestPasswordChangeRevokesTheAccountsOtherSessions(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	insertRestrictedHTTPAccount(t, pool, "multi-session@example.com", identity.RoleAdmin)
	router := realSessionRouter(t, pool)

	elsewhere := postSessionLogin(t, router, "multi-session@example.com", httpSessionPassword)
	elsewhereCookie := authenticatedCookie(t, elsewhere.setCookie)

	here := postSessionLogin(t, router, "multi-session@example.com", httpSessionPassword)
	changed := postPasswordChange(
		router,
		authenticatedCookie(t, here.setCookie),
		csrfFromSessionResponse(t, here.body),
		httpSessionPassword,
		"a brand new launch passphrase 9",
	)
	if changed.Code != http.StatusOK {
		t.Fatalf("password change = %d: %s", changed.Code, changed.Body.String())
	}

	survivor := serveSessionRequest(router, sessionRequest(
		http.MethodGet, "/api/v1/session", elsewhereCookie, "",
	))
	if survivor.Code != http.StatusUnauthorized {
		t.Fatalf("another session family survived the password change: %d", survivor.Code)
	}
}

func authenticatedCookie(t *testing.T, setCookies []string) *http.Cookie {
	t.Helper()
	response := &http.Response{Header: http.Header{"Set-Cookie": setCookies}}
	for _, cookie := range response.Cookies() {
		if cookie.Name == auth.SessionCookieName {
			return cookie
		}
	}
	t.Fatal("authenticated session cookie is missing")
	return nil
}

func csrfFromSessionResponse(t *testing.T, body []byte) string {
	t.Helper()
	var response struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.CSRFToken == "" {
		t.Fatalf("decoding session response: %v body=%s", err, body)
	}
	return response.CSRFToken
}

func sessionRequest(
	method string,
	path string,
	cookie *http.Cookie,
	csrf string,
) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.AddCookie(cookie)
	if method != http.MethodGet {
		request.Header.Set("Origin", "https://gradex.example")
		request.Header.Set(csrfHeaderName, csrf)
	}
	return request
}

func serveSessionRequest(
	router *gin.Engine,
	request *http.Request,
) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
