//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

const httpSessionPassword = "correct session login passphrase 9"

func realSessionRouter(
	t *testing.T,
	pool *pgxpool.Pool,
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
	endpointPolicies := map[string]ratelimit.Policy{
		"session-bootstrap":  ratelimit.DevelopmentAnonymousBootstrapPolicy(),
		"sessions":           ratelimit.DevelopmentLoginPolicy(),
		"session-resolution": ratelimit.DevelopmentSessionPolicy("session-resolution"),
		"session-renewals":   ratelimit.DevelopmentSessionPolicy("session-renewals"),
		"session-logout":     ratelimit.DevelopmentSessionPolicy("session-logout"),
	}
	foundation, err := NewSessionFoundation(SessionFoundationOptions{
		PublicOrigin:        "https://gradex.example",
		CookieSigningKey:    bytes.Repeat([]byte("a"), 32),
		AnonymousCSRFKey:    bytes.Repeat([]byte("b"), 32),
		AnonymousSessionTTL: time.Hour,
		Repository:          repository,
		Limiter:             limiter,
		EndpointPolicies:    endpointPolicies,
	})
	if err != nil {
		t.Fatalf("constructing session foundation: %v", err)
	}
	router := gin.New()
	router.Use(requestIDMiddleware())
	mountSessionRoutes(router.Group("/api/v1"), foundation)
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
