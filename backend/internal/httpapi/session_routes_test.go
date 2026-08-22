package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type fakeSessionRepository struct {
	loginGrant  identity.SessionGrant
	loginErr    error
	view        identity.SessionView
	resolveErr  error
	renewGrant  identity.SessionGrant
	renewErr    error
	logoutErr   error
	changeGrant identity.SessionGrant
	changeErr   error

	loginCalls   int
	resolveCalls int
	renewCalls   int
	logoutCalls  int
	changeCalls  int

	// lastChange records the command the handler built, so a test can prove
	// which digests and plaintexts crossed the boundary.
	lastChange identity.PasswordChangeCommand
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

func (r *fakeSessionRepository) ChangePassword(
	_ context.Context,
	command identity.PasswordChangeCommand,
) (identity.SessionGrant, error) {
	r.changeCalls++
	r.lastChange = command
	return r.changeGrant, r.changeErr
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
	return mountedSessionRouterWithPolicies(t, repository, store, testSessionEndpointPolicies())
}

func mountedSessionRouterWithPolicies(
	t *testing.T,
	repository *fakeSessionRepository,
	store admissionRateStore,
	policies map[string]ratelimit.Policy,
) *gin.Engine {
	t.Helper()
	limiter, err := ratelimit.New(
		store, bytes.Repeat([]byte{0x31}, 32), time.Second,
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
		Compromised:         testCompromisedSource(t),
		Limiter:             limiter,
		EndpointPolicies:    policies,
	})
	if err != nil {
		t.Fatalf("constructing session foundation: %v", err)
	}
	router := gin.New()
	router.Use(requestIDMiddleware())
	mountSessionRoutes(
		router.Group("/api/v1"), foundation,
		fakeAuth{},
		fixedPrincipals{principal: identity.Principal{
			AccountID: "user-1", Role: identity.RoleStudent,
			Status: identity.StatusActive, CredentialState: identity.CredentialActive,
		}},
		logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info")),
	)
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

const (
	currentPasswordFixture = "the temporary bootstrap passphrase 7"
	newPasswordFixture     = "a brand new launch passphrase 9"
)

func passwordChangeRequestBody(current, next string) string {
	return `{"current_password":` + quotedJSONValue(current) +
		`,"new_password":` + quotedJSONValue(next) + `}`
}

func quotedJSONValue(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func passwordChangeHTTPRequest(
	credential config.Secret,
	csrf config.Secret,
	body string,
) *http.Request {
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/password-changes", strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://gradex.example")
	request.Header.Set(csrfHeaderName, csrf.Expose())
	request.AddCookie(&http.Cookie{
		Name: auth.SessionCookieName, Value: credential.Expose(),
	})
	return request
}

// A successful change replaces the browser's credentials in the same response,
// and the response reports the principal is no longer restricted. Both matter:
// the old cookie is superseded server-side, so a browser that kept it would be
// signed out on its next request, and a browser that kept
// password_change_required would bounce straight back to the change screen.
func TestPasswordChangeRotatesTheBrowserCredentialsAndClearsTheRestriction(t *testing.T) {
	session, credential, csrf := sessionFixture()
	session.CredentialState = identity.CredentialActive
	nextCredential := config.NewSecret(
		base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x45}, 32)),
	)
	nextCSRF := config.NewSecret(
		base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x46}, 32)),
	)
	repository := &fakeSessionRepository{changeGrant: identity.SessionGrant{
		Session: session, Credential: nextCredential, CSRFToken: nextCSRF,
	}}
	router := mountedSessionRouter(t, repository, admissionRateStore{allowed: true})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, passwordChangeHTTPRequest(
		credential, csrf,
		passwordChangeRequestBody(currentPasswordFixture, newPasswordFixture),
	))

	if response.Code != http.StatusOK || repository.changeCalls != 1 {
		t.Fatalf("change = %d calls=%d: %s",
			response.Code, repository.changeCalls, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.SessionCookieName ||
		cookies[0].Value != nextCredential.Expose() {
		t.Fatalf("change did not install the committed replacement cookie: %#v", cookies)
	}
	if !cookies[0].Secure || !cookies[0].HttpOnly ||
		cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Domain != "" {
		t.Fatalf("replacement cookie lost its hardening: %#v", cookies[0])
	}
	body := response.Body.String()
	if !strings.Contains(body, `"password_change_required":false`) {
		t.Errorf("a completed change still reports the restriction: %s", body)
	}
	if !strings.Contains(body, nextCSRF.Expose()) {
		t.Errorf("the browser was not given the replacement CSRF token: %s", body)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Error("the password-change response is cacheable")
	}

	// The digests the handler forwarded are digests, not the cookie and token
	// themselves, and both plaintexts arrived wrapped.
	if repository.lastChange.CredentialDigest == credential.Expose() ||
		repository.lastChange.CSRFDigest == csrf.Expose() {
		t.Error("the handler forwarded browser secrets instead of their digests")
	}
	if repository.lastChange.CredentialDigest != identity.DigestToken(credential.Expose()) {
		t.Error("the handler did not forward the presented credential's digest")
	}
}

// Neither password may appear in the response on any path — including the
// success path, where the new one has just been accepted.
func TestPasswordChangeNeverEchoesEitherPassword(t *testing.T) {
	session, credential, csrf := sessionFixture()
	for name, repository := range map[string]*fakeSessionRepository{
		"accepted": {changeGrant: identity.SessionGrant{
			Session: session, Credential: credential, CSRFToken: csrf,
		}},
		"wrong current password": {changeErr: identity.ErrCurrentPasswordIncorrect},
		"refused new password":   {changeErr: identity.ErrPasswordPolicy},
		"operational fault":      {changeErr: errors.New("dial tcp 10.0.0.4:5432: connection refused")},
	} {
		t.Run(name, func(t *testing.T) {
			router := mountedSessionRouter(t, repository, admissionRateStore{allowed: true})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, passwordChangeHTTPRequest(
				credential, csrf,
				passwordChangeRequestBody(currentPasswordFixture, newPasswordFixture),
			))
			body := response.Body.String()
			for _, secret := range []string{currentPasswordFixture, newPasswordFixture} {
				if strings.Contains(body, secret) {
					t.Fatalf("response echoed a password: %s", body)
				}
			}
			if strings.Contains(body, "10.0.0.4") || strings.Contains(body, "connection refused") {
				t.Fatalf("response leaked an operational detail: %s", body)
			}
		})
	}
}

// The two failures the caller can act on are reported apart, and everything
// else collapses. A wrong current password must not read as a weak new one.
func TestPasswordChangeRefusalsMapToDistinctProblems(t *testing.T) {
	_, credential, csrf := sessionFixture()
	for name, tc := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"wrong current password": {
			identity.ErrCurrentPasswordIncorrect, http.StatusUnauthorized, "AUTHENTICATION_FAILED",
		},
		"missing current password": {
			identity.ErrCurrentPasswordRequired, http.StatusUnauthorized, "AUTHENTICATION_FAILED",
		},
		"weak or compromised new password": {
			identity.ErrPasswordPolicy, http.StatusUnprocessableEntity, "VALIDATION_FAILED",
		},
		"authenticated too long ago": {
			identity.ErrRecentAuthRequired, http.StatusForbidden, "NOT_AUTHORIZED",
		},
		"session no longer usable": {
			identity.ErrSessionNotUsable, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED",
		},
		"credential generation rotated underneath": {
			identity.ErrStaleGeneration, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED",
		},
		"session credential reused": {
			identity.ErrSessionReuseDetected, http.StatusUnauthorized, "SESSION_REUSE_DETECTED",
		},
		"operational fault": {
			errors.New("relation \"password_credentials\" does not exist"),
			http.StatusInternalServerError, "INTERNAL_ERROR",
		},
	} {
		t.Run(name, func(t *testing.T) {
			repository := &fakeSessionRepository{changeErr: tc.err}
			router := mountedSessionRouter(t, repository, admissionRateStore{allowed: true})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, passwordChangeHTTPRequest(
				credential, csrf,
				passwordChangeRequestBody(currentPasswordFixture, newPasswordFixture),
			))
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if got := assertProblemEnvelope(t, response); got.Code != tc.code {
				t.Fatalf("code = %q, want %q", got.Code, tc.code)
			}
			if len(response.Result().Cookies()) != 0 {
				t.Fatal("a refused change still rotated the browser cookie")
			}
		})
	}
}

// Origin, CSRF, and body admission all run before the repository is called, so
// a rejected request cannot consume an Argon2id verification.
func TestPasswordChangeRefusesBadAdmissionBeforeTheRepository(t *testing.T) {
	_, credential, csrf := sessionFixture()

	for name, tc := range map[string]struct {
		mutate func(*http.Request)
		status int
		code   string
	}{
		"foreign origin": {
			func(r *http.Request) { r.Header.Set("Origin", "https://attacker.example") },
			http.StatusForbidden, "ORIGIN_NOT_ALLOWED",
		},
		"malformed CSRF token": {
			func(r *http.Request) { r.Header.Set(csrfHeaderName, "not-a-token") },
			http.StatusForbidden, "CSRF_FAILED",
		},
		"no session cookie": {
			func(r *http.Request) { r.Header.Del("Cookie") },
			http.StatusUnauthorized, "AUTHENTICATION_REQUIRED",
		},
	} {
		t.Run(name, func(t *testing.T) {
			repository := &fakeSessionRepository{}
			router := mountedSessionRouter(t, repository, admissionRateStore{allowed: true})
			request := passwordChangeHTTPRequest(
				credential, csrf,
				passwordChangeRequestBody(currentPasswordFixture, newPasswordFixture),
			)
			tc.mutate(request)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != tc.status || repository.changeCalls != 0 {
				t.Fatalf("status = %d calls = %d, want %d and 0: %s",
					response.Code, repository.changeCalls, tc.status, response.Body.String())
			}
			if got := assertProblemEnvelope(t, response); got.Code != tc.code {
				t.Fatalf("code = %q, want %q", got.Code, tc.code)
			}
		})
	}
}

// The body boundary is strict: both fields are required, unknown members are
// refused, and an oversized document is rejected unread.
func TestPasswordChangeBodyBoundaryIsStrictAndBounded(t *testing.T) {
	_, credential, csrf := sessionFixture()
	oversized := strings.Repeat("x", int(passwordChangeBodyLimit)+1)

	for name, tc := range map[string]struct {
		body   string
		status int
	}{
		"missing current password": {`{"new_password":"a replacement passphrase"}`, http.StatusUnprocessableEntity},
		"missing new password":     {`{"current_password":"the temporary one"}`, http.StatusUnprocessableEntity},
		"empty new password":       {`{"current_password":"a","new_password":""}`, http.StatusUnprocessableEntity},
		"unknown member":           {`{"current_password":"a","new_password":"b","account_id":"other"}`, http.StatusBadRequest},
		"not JSON":                 {`current_password=a`, http.StatusBadRequest},
		"oversized": {
			passwordChangeRequestBody(currentPasswordFixture, oversized),
			http.StatusRequestEntityTooLarge,
		},
	} {
		t.Run(name, func(t *testing.T) {
			repository := &fakeSessionRepository{}
			router := mountedSessionRouter(t, repository, admissionRateStore{allowed: true})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, passwordChangeHTTPRequest(credential, csrf, tc.body))

			if response.Code != tc.status || repository.changeCalls != 0 {
				t.Fatalf("status = %d calls = %d, want %d and 0: %s",
					response.Code, repository.changeCalls, tc.status, response.Body.String())
			}
			if strings.Contains(response.Body.String(), oversized[:64]) {
				t.Fatal("the refusal echoed the rejected body")
			}
		})
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

func TestProductionLoginFailsBeforeCredentialVerificationWhenRedisIsUnavailable(t *testing.T) {
	repository := &fakeSessionRepository{}
	browserRouter := mountedSessionRouter(t, &fakeSessionRepository{}, admissionRateStore{allowed: true})
	policies := testSessionEndpointPolicies()
	policies["sessions"] = ratelimit.ProductionLoginPolicy()
	router := mountedSessionRouterWithPolicies(
		t, repository, admissionRateStore{err: errors.New("redis unavailable")}, policies,
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, admittedLoginRequest(t, browserRouter))
	if response.Code != http.StatusServiceUnavailable || repository.loginCalls != 0 {
		t.Fatalf("dependency failure = %d calls=%d, want 503 and no credential work",
			response.Code, repository.loginCalls)
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
