package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type fakeSessionRepository struct {
	loginGrant identity.SessionGrant
	loginErr   error
	view       identity.SessionView
	resolveErr error
	renewGrant identity.SessionGrant
	renewErr   error
	logoutErr  error

	loginCalls   int
	resolveCalls int
	renewCalls   int
	logoutCalls  int
}

func (r *fakeSessionRepository) Login(
	_ context.Context,
	_ identity.LoginRequest,
) (identity.SessionGrant, error) {
	r.loginCalls++
	return r.loginGrant, r.loginErr
}

func (r *fakeSessionRepository) Resolve(
	_ context.Context,
	_ string,
	_ identity.CredentialUseKind,
	_ string,
) (identity.SessionView, error) {
	r.resolveCalls++
	return r.view, r.resolveErr
}

func (r *fakeSessionRepository) Renew(
	_ context.Context,
	_ identity.SessionMutation,
) (identity.SessionGrant, error) {
	r.renewCalls++
	return r.renewGrant, r.renewErr
}

func (r *fakeSessionRepository) Logout(
	_ context.Context,
	_ identity.SessionMutation,
) error {
	r.logoutCalls++
	return r.logoutErr
}

func sessionFixture() (
	identity.AuthenticatedSession,
	config.Secret,
	config.Secret,
) {
	credentialBytes := bytes.Repeat([]byte{0x41}, 32)
	csrfBytes := bytes.Repeat([]byte{0x42}, 32)
	return identity.AuthenticatedSession{
			AccountID:   "00000000-0000-0000-0000-000000000001",
			SessionID:   "00000000-0000-0000-0000-000000000002",
			DisplayName: "Session Student", Role: identity.RoleStudent,
			Generation:        1,
			IdleExpiresAt:     time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
			AbsoluteExpiresAt: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC),
		},
		config.NewSecret(base64.RawURLEncoding.EncodeToString(credentialBytes)),
		config.NewSecret(base64.RawURLEncoding.EncodeToString(csrfBytes))
}

func mountedSessionRouter(
	t *testing.T,
	repository *fakeSessionRepository,
	store admissionRateStore,
) *gin.Engine {
	t.Helper()
	limiter, err := ratelimit.New(
		store, bytes.Repeat([]byte{0x31}, 32), time.Second,
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

func admittedLoginRequest(
	t *testing.T,
	router *gin.Engine,
) *http.Request {
	t.Helper()
	cookie, csrf := bootstrapAdmissionBrowser(t, router)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sessions",
		strings.NewReader(`{"email":"student@example.com","password":"correct password"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://gradex.example")
	request.Header.Set(csrfHeaderName, csrf)
	request.AddCookie(cookie)
	return request
}

func TestSessionLoginCommitsBeforeHardenedCookieAndClearsAnonymousCookie(t *testing.T) {
	session, credential, csrf := sessionFixture()
	repository := &fakeSessionRepository{loginGrant: identity.SessionGrant{
		Session: session, Credential: credential, CSRFToken: csrf,
	}}
	router := mountedSessionRouter(t, repository, admissionRateStore{allowed: true})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, admittedLoginRequest(t, router))

	if response.Code != http.StatusCreated || repository.loginCalls != 1 {
		t.Fatalf("login response = %d calls=%d: %s", response.Code, repository.loginCalls, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" ||
		strings.Contains(response.Body.String(), credential.Expose()) {
		t.Fatal("login response is cacheable or exposes its credential")
	}
	cookies := response.Result().Cookies()
	var authenticated, anonymous *http.Cookie
	for _, cookie := range cookies {
		switch cookie.Name {
		case auth.SessionCookieName:
			authenticated = cookie
		case anonymousCookieName:
			anonymous = cookie
		}
	}
	if authenticated == nil || !authenticated.Secure || !authenticated.HttpOnly ||
		authenticated.Path != "/" || authenticated.Domain != "" ||
		authenticated.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe authenticated cookie: %#v", authenticated)
	}
	if anonymous == nil || anonymous.MaxAge >= 0 {
		t.Fatalf("anonymous cookie was not expired after commit: %#v", anonymous)
	}
}

func TestSessionResolutionRehydratesWithoutRotation(t *testing.T) {
	session, credential, csrf := sessionFixture()
	repository := &fakeSessionRepository{view: identity.SessionView{
		Session: session, CSRFToken: csrf,
	}}
	router := mountedSessionRouter(t, repository, admissionRateStore{allowed: true})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.AddCookie(&http.Cookie{
		Name: auth.SessionCookieName, Value: credential.Expose(),
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || repository.resolveCalls != 1 {
		t.Fatalf("resolution = %d calls=%d: %s", response.Code, repository.resolveCalls, response.Body.String())
	}
	if response.Header().Get("Set-Cookie") != "" {
		t.Fatal("session read rotated the cookie")
	}
	if !strings.Contains(response.Body.String(), csrf.Expose()) ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("session read did not safely rehydrate browser memory")
	}
}

func TestSessionMutationRejectsOriginAndCSRFFailureBeforeRepository(t *testing.T) {
	_, credential, csrf := sessionFixture()
	repository := &fakeSessionRepository{}
	router := mountedSessionRouter(t, repository, admissionRateStore{allowed: true})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/session-renewals", nil)
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set(csrfHeaderName, csrf.Expose())
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: credential.Expose()})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden ||
		!strings.Contains(response.Body.String(), `"code":"ORIGIN_NOT_ALLOWED"`) ||
		repository.renewCalls != 0 {
		t.Fatalf("origin rejection = %d calls=%d: %s", response.Code, repository.renewCalls, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/session-renewals", nil)
	request.Header.Set("Origin", "https://gradex.example")
	request.Header.Set(csrfHeaderName, "malformed")
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: credential.Expose()})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden ||
		!strings.Contains(response.Body.String(), `"code":"CSRF_FAILED"`) ||
		repository.renewCalls != 0 {
		t.Fatalf("CSRF rejection = %d calls=%d: %s", response.Code, repository.renewCalls, response.Body.String())
	}
}

func TestSessionRenewalRotatesCookie(t *testing.T) {
	session, credential, csrf := sessionFixture()
	nextCredential := config.NewSecret(
		base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x43}, 32)),
	)
	nextCSRF := config.NewSecret(
		base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x44}, 32)),
	)
	repository := &fakeSessionRepository{renewGrant: identity.SessionGrant{
		Session: session, Credential: nextCredential, CSRFToken: nextCSRF,
	}}
	router := mountedSessionRouter(t, repository, admissionRateStore{allowed: true})
	request := authenticatedMutationRequest(
		http.MethodPost, "/api/v1/session-renewals", credential, csrf,
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || repository.renewCalls != 1 {
		t.Fatalf("renewal = %d calls=%d: %s", response.Code, repository.renewCalls, response.Body.String())
	}
	if cookie := response.Result().Cookies()[0]; cookie.Value != nextCredential.Expose() {
		t.Fatal("renewal did not issue the committed replacement")
	}
}

func TestSessionLogoutClearsOnlyAfterCommit(t *testing.T) {
	_, credential, csrf := sessionFixture()
	repository := &fakeSessionRepository{}
	router := mountedSessionRouter(t, repository, admissionRateStore{allowed: true})
	repository.logoutErr = errors.New("database unavailable")
	request := authenticatedMutationRequest(
		http.MethodDelete, "/api/v1/session", credential, csrf,
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		response.Header().Get("Set-Cookie") != "" {
		t.Fatal("failed revocation cleared a still-usable browser cookie")
	}

	repository.logoutErr = nil
	request = authenticatedMutationRequest(
		http.MethodDelete, "/api/v1/session", credential, csrf,
	)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d: %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.SessionCookieName ||
		cookies[0].MaxAge >= 0 {
		t.Fatalf("logout did not expire authenticated cookie: %#v", cookies)
	}
}

func authenticatedMutationRequest(
	method string,
	path string,
	credential config.Secret,
	csrf config.Secret,
) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("Origin", "https://gradex.example")
	request.Header.Set(csrfHeaderName, csrf.Expose())
	request.AddCookie(&http.Cookie{
		Name: auth.SessionCookieName, Value: credential.Expose(),
	})
	return request
}

func TestSessionRateDenialPreventsCredentialVerification(t *testing.T) {
	repository := &fakeSessionRepository{}
	browserRouter := mountedSessionRouter(
		t, &fakeSessionRepository{}, admissionRateStore{allowed: true},
	)
	router := mountedSessionRouter(t, repository, admissionRateStore{allowed: false})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, admittedLoginRequest(t, browserRouter))
	if response.Code != http.StatusTooManyRequests || repository.loginCalls != 0 ||
		response.Header().Get("Retry-After") == "" {
		t.Fatalf("rate denial = %d calls=%d headers=%#v", response.Code, repository.loginCalls, response.Header())
	}
}

func TestAuthenticatedSessionRateDenialPreventsResolution(t *testing.T) {
	_, credential, _ := sessionFixture()
	repository := &fakeSessionRepository{}
	router := mountedSessionRouter(t, repository, admissionRateStore{allowed: false})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.AddCookie(&http.Cookie{
		Name: auth.SessionCookieName, Value: credential.Expose(),
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests ||
		repository.resolveCalls != 0 ||
		response.Header().Get("Retry-After") == "" {
		t.Fatalf(
			"session rate denial = %d calls=%d headers=%#v",
			response.Code, repository.resolveCalls, response.Header(),
		)
	}
}
