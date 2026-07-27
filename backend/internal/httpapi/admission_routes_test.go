package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type admissionRateStore struct {
	allowed bool
	err     error
}

func (s admissionRateStore) Decide(context.Context, []ratelimit.Entry) (bool, error) {
	return s.allowed, s.err
}

func mountedAdmissionRouter(
	t *testing.T,
	store admissionRateStore,
	localMaxKeys int,
) *gin.Engine {
	t.Helper()
	return mountedAdmissionRouterWithObserver(t, store, localMaxKeys, nil)
}

func mountedAdmissionRouterWithObserver(
	t *testing.T,
	store admissionRateStore,
	localMaxKeys int,
	observe func(*gin.Context),
) *gin.Engine {
	t.Helper()
	english, arabic := identityPolicySets()
	policies, err := identity.NewStaticPolicySetResolver(english, arabic)
	if err != nil {
		t.Fatalf("constructing policy resolver: %v", err)
	}
	limiter, err := ratelimit.New(store, bytes.Repeat([]byte{0x31}, 32), time.Second)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	endpointPolicies := make(map[string]ratelimit.Policy)
	for _, endpoint := range []string{
		"student-registrations", "email-verification-requests", "email-verifications",
		"password-reset-requests",
	} {
		policy := ratelimit.DevelopmentAdmissionPolicy(endpoint)
		policy.LocalMaxKeys = localMaxKeys
		endpointPolicies[endpoint] = policy
	}
	readPolicy := ratelimit.DevelopmentPolicySetReadPolicy()
	readPolicy.LocalMaxKeys = localMaxKeys
	endpointPolicies[readPolicy.Endpoint] = readPolicy
	bootstrapPolicy := ratelimit.DevelopmentAnonymousBootstrapPolicy()
	bootstrapPolicy.LocalMaxKeys = localMaxKeys
	endpointPolicies[bootstrapPolicy.Endpoint] = bootstrapPolicy

	foundation, err := NewAdmissionFoundation(AdmissionFoundationOptions{
		PublicOrigin:        "https://gradex.example",
		CookieSigningKey:    bytes.Repeat([]byte("a"), 32),
		CSRFKey:             bytes.Repeat([]byte("b"), 32),
		AnonymousSessionTTL: time.Hour,
		Policies:            policies,
		Service:             &fakeAdmissionService{},
		Recovery:            &fakeRecoveryService{},
		Limiter:             limiter,
		EndpointPolicies:    endpointPolicies,
	})
	if err != nil {
		t.Fatalf("constructing admission foundation: %v", err)
	}
	router := gin.New()
	if observe != nil {
		router.Use(func(c *gin.Context) {
			c.Next()
			observe(c)
		})
	}
	mountAdmissionRoutes(router.Group("/api/v1"), foundation)
	return router
}

func bootstrapAdmissionBrowser(t *testing.T, router *gin.Engine) (*http.Cookie, string) {
	t.Helper()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet, "/api/v1/session/bootstrap", nil,
	))
	if response.Code != http.StatusOK {
		t.Fatalf("bootstrap status = %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding bootstrap: %v", err)
	}
	return response.Result().Cookies()[0], body.CSRF
}

func admittedRegistrationRequest(
	t *testing.T,
	router *gin.Engine,
) *http.Request {
	t.Helper()
	cookie, csrf := bootstrapAdmissionBrowser(t, router)
	body := `{"display_name":"Nora Ahmed","email":"student@example.com",` +
		`"password":"correct horse battery staple","locale":"en",` +
		`"policy_set_id":"registration-v1"}`
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/student-registrations", strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://gradex.example")
	request.Header.Set(csrfHeaderName, csrf)
	request.AddCookie(cookie)
	return request
}

// FR-014: a real distributed quota decision yields 429; infrastructure failure
// with no bounded local capacity yields 503 instead of a fabricated denial.
func TestMountedAdmissionRoutesDistinguishLimiterDenyFromUnavailable(t *testing.T) {
	t.Run("distributed deny", func(t *testing.T) {
		browserRouter := mountedAdmissionRouter(t, admissionRateStore{allowed: true}, 64)
		router := mountedAdmissionRouter(t, admissionRateStore{allowed: false}, 64)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, admittedRegistrationRequest(t, browserRouter))
		if response.Code != http.StatusTooManyRequests ||
			response.Header().Get("Retry-After") == "" {
			t.Fatalf("deny response = %d headers %#v", response.Code, response.Header())
		}
	})
	t.Run("unsafe fallback", func(t *testing.T) {
		browserRouter := mountedAdmissionRouter(t, admissionRateStore{allowed: true}, 64)
		router := mountedAdmissionRouter(
			t, admissionRateStore{err: errors.New("dependency unavailable")}, 1,
		)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, admittedRegistrationRequest(t, browserRouter))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("unavailable response = %d: %s", response.Code, response.Body.String())
		}
	})
}

func TestAnonymousBootstrapIsRateLimitedBeforeCapabilityIssuance(t *testing.T) {
	router := mountedAdmissionRouter(t, admissionRateStore{allowed: false}, 64)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet, "/api/v1/session/bootstrap", nil,
	))
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("bootstrap limit response = %d: %s", response.Code, response.Body.String())
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("rate-limited bootstrap issued an anonymous capability cookie")
	}
}

func TestMountedAdmissionRouteRunsStructureBeforeSecurityAndLimiter(t *testing.T) {
	var observedStage string
	router := mountedAdmissionRouterWithObserver(
		t,
		admissionRateStore{allowed: true},
		64,
		func(c *gin.Context) {
			observedStage = string(admissionFailureStageOf(c))
		},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/student-registrations",
		strings.NewReader(`{"email":`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want malformed 400: %s", response.Code, response.Body.String())
	}
	if observedStage != string(admissionFailureStageStructure) {
		t.Fatalf("failure stage = %q, want %q", observedStage, admissionFailureStageStructure)
	}
}
