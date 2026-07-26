package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type fakeAdmissionService struct {
	registerErr error
	requestErr  error
	verifyErr   error
}

func (f *fakeAdmissionService) RegisterStudent(
	context.Context,
	identity.StudentRegistration,
) error {
	return f.registerErr
}

func (f *fakeAdmissionService) RequestEmailVerification(
	context.Context,
	identity.VerificationRequest,
) error {
	return f.requestErr
}

func (f *fakeAdmissionService) VerifyEmail(context.Context, string, string) error {
	return f.verifyErr
}

func admissionHandlerRouter(t *testing.T, service *fakeAdmissionService) *gin.Engine {
	t.Helper()
	english, arabic := identityPolicySets()
	policies, err := identity.NewStaticPolicySetResolver(english, arabic)
	if err != nil {
		t.Fatalf("constructing policies: %v", err)
	}
	handlers := &identityHandlers{service: service, policies: policies}
	router := gin.New()
	router.GET("/policy", handlers.currentPolicySet)
	router.POST("/register", func(c *gin.Context) {
		var request studentRegistrationRequest
		if bindStrictJSON(c, &request, registrationBodyLimit) {
			handlers.registerStudent(c, &request)
		}
	})
	router.POST("/request", func(c *gin.Context) {
		var request verificationRequestBody
		if bindStrictJSON(c, &request, verificationRequestBodyLimit) {
			handlers.requestVerification(c, &request)
		}
	})
	router.POST("/verify", func(c *gin.Context) {
		var request verificationConsumptionBody
		if bindStrictJSON(c, &request, verificationConsumptionBodyLimit) {
			handlers.consumeVerification(c, &request)
		}
	})
	return router
}

func identityPolicySets() (identity.RegistrationPolicySet, identity.RegistrationPolicySet) {
	policies := []identity.RegistrationPolicy{
		{
			Kind: identity.PolicyPrivacyNotice, Version: "privacy-v1",
			Label: "Privacy", URL: "/legal/privacy",
		},
		{
			Kind: identity.PolicyTermsOfService, Version: "terms-v1",
			Label: "Terms", URL: "/legal/terms",
		},
	}
	english := identity.RegistrationPolicySet{
		ID: "registration-v1", Locale: identity.LocaleEnglish,
		Policies: append([]identity.RegistrationPolicy(nil), policies...),
	}
	arabic := identity.RegistrationPolicySet{
		ID: "registration-v1", Locale: identity.LocaleArabic,
		Policies: append([]identity.RegistrationPolicy(nil), policies...),
	}
	arabic.Policies[0].Label = "الخصوصية"
	arabic.Policies[1].Label = "الشروط"
	return english, arabic
}

// BR-001: hidden registration and resend outcomes share one byte-identical
// acknowledgment with no cookie, identifier, Account, or delivery claim.
func TestAdmissionAcknowledgmentsAreFixedAndNoStore(t *testing.T) {
	tests := map[string]struct {
		path     string
		body     string
		wantBody string
	}{
		"registration": {
			path: "/register",
			body: `{"display_name":"Nora Ahmed","email":"student@example.com",` +
				`"password":"correct horse battery staple","locale":"en",` +
				`"policy_set_id":"registration-v1"}`,
			wantBody: `{"code":"REGISTRATION_REQUEST_ACCEPTED"}`,
		},
		"verification request": {
			path: "/request", body: `{"email":"student@example.com"}`,
			wantBody: `{"code":"VERIFICATION_REQUEST_ACCEPTED"}`,
		},
	}
	for scenario, test := range tests {
		t.Run(scenario, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			admissionHandlerRouter(t, &fakeAdmissionService{}).ServeHTTP(response, request)
			if response.Code != http.StatusAccepted ||
				strings.TrimSpace(response.Body.String()) != test.wantBody {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			if len(response.Result().Cookies()) != 0 || response.Header().Get("Location") != "" {
				t.Fatal("uniform acknowledgment exposed cookie or Location state")
			}
		})
	}
}

func TestVerificationConsumptionReturnsSuccessWithoutSession(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost, "/verify", strings.NewReader(`{"token":"`+strings.Repeat("A", 43)+`"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	admissionHandlerRouter(t, &fakeAdmissionService{}).ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		strings.TrimSpace(response.Body.String()) != `{"status":"VERIFIED"}` {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("verification issued a session cookie")
	}
}

func TestAdmissionDomainErrorsMapToSafeProblemClasses(t *testing.T) {
	tests := map[string]struct {
		service    *fakeAdmissionService
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		"stale policy": {
			service: &fakeAdmissionService{registerErr: identity.ErrPolicySetStale},
			path:    "/register",
			body: `{"display_name":"Nora Ahmed","email":"student@example.com",` +
				`"password":"correct horse battery staple","locale":"en",` +
				`"policy_set_id":"old-v1"}`,
			wantStatus: http.StatusUnprocessableEntity, wantCode: "VALIDATION_FAILED",
		},
		"credential dependency": {
			service: &fakeAdmissionService{registerErr: identity.ErrAdmissionUnavailable},
			path:    "/register",
			body: `{"display_name":"Nora Ahmed","email":"student@example.com",` +
				`"password":"correct horse battery staple","locale":"en",` +
				`"policy_set_id":"registration-v1"}`,
			wantStatus: http.StatusServiceUnavailable, wantCode: "REGISTRATION_UNAVAILABLE",
		},
		"delivery admission": {
			service: &fakeAdmissionService{requestErr: identity.ErrDeliveryUnavailable},
			path:    "/request", body: `{"email":"student@example.com"}`,
			wantStatus: http.StatusServiceUnavailable, wantCode: "TRANSACTIONAL_DELIVERY_UNAVAILABLE",
		},
		"invalid token": {
			service: &fakeAdmissionService{verifyErr: identity.ErrTokenInvalid},
			path:    "/verify", body: `{"token":"` + strings.Repeat("A", 43) + `"}`,
			wantStatus: http.StatusBadRequest, wantCode: "TOKEN_INVALID",
		},
	}
	for scenario, test := range tests {
		t.Run(scenario, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			admissionHandlerRouter(t, test.service).ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			var got problem.Problem
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatalf("decoding problem: %v", err)
			}
			if got.Code != test.wantCode {
				t.Fatalf("code = %q, want %q", got.Code, test.wantCode)
			}
			if test.wantStatus == http.StatusServiceUnavailable &&
				response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("503 response is cacheable")
			}
		})
	}
}

func TestCurrentPolicySetNegotiatesArabicAndRejectsUnsupportedLanguage(t *testing.T) {
	t.Run("Arabic", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/policy", nil)
		request.Header.Set("Accept-Language", "ar")
		response := httptest.NewRecorder()
		admissionHandlerRouter(t, &fakeAdmissionService{}).ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Content-Language") != "ar" ||
			!strings.Contains(response.Body.String(), "الخصوصية") {
			t.Fatalf("Arabic policy response = %d %q", response.Code, response.Body.String())
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/policy", nil)
		request.Header.Set("Accept-Language", "fr")
		response := httptest.NewRecorder()
		admissionHandlerRouter(t, &fakeAdmissionService{}).ServeHTTP(response, request)
		if response.Code != http.StatusNotAcceptable {
			t.Fatalf("status = %d, want 406", response.Code)
		}
	})
}

var _ admissionCommands = (*fakeAdmissionService)(nil)

func TestAdmissionFakeDoesNotMaskUnexpectedErrors(t *testing.T) {
	unexpected := errors.New("database fault")
	request := httptest.NewRequest(
		http.MethodPost, "/request", strings.NewReader(`{"email":"student@example.com"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	admissionHandlerRouter(t, &fakeAdmissionService{requestErr: unexpected}).ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected domain error mapped to %d, want 500", response.Code)
	}
}
