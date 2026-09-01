package httpapi

import (
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
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type fakeAdmissionService struct {
	registerErr error
	requestErr  error
	verifyErr   error
	resendErr   error
	codeErr     error
	grant       identity.SessionGrant
	challenge   identity.VerificationChallenge
}

func (f *fakeAdmissionService) RegisterStudent(
	context.Context,
	identity.StudentRegistration,
) (identity.VerificationChallenge, error) {
	return f.challenge, f.registerErr
}

func (f *fakeAdmissionService) RequestEmailVerification(
	context.Context,
	identity.VerificationRequest,
) (identity.VerificationChallenge, error) {
	return f.challenge, f.requestErr
}

func (f *fakeAdmissionService) ResendEmailVerificationOTP(
	context.Context, string, string,
) (identity.VerificationChallenge, error) {
	return f.challenge, f.resendErr
}

func (f *fakeAdmissionService) VerifyEmailOTP(
	context.Context, string, string, string,
) (identity.SessionGrant, error) {
	return f.grant, f.codeErr
}

func (f *fakeAdmissionService) VerifyEmail(context.Context, string, string) error {
	return f.verifyErr
}

// fakeRecoveryService stands in for RecoveryService at the route boundary. It
// exposes only the reset request, mirroring recoveryCommands: there is no
// completion operation to fake because none is routable yet.
type fakeRecoveryService struct {
	requestErr  error
	completeErr error
	requests    int
	completions int
}

func (f *fakeRecoveryService) RequestPasswordReset(
	context.Context,
	identity.PasswordResetRequest,
) error {
	f.requests++
	return f.requestErr
}

func (f *fakeRecoveryService) CompletePasswordReset(
	context.Context,
	identity.PasswordResetCompletion,
) error {
	f.completions++
	return f.completeErr
}

func admissionHandlerRouter(t *testing.T, service *fakeAdmissionService) *gin.Engine {
	t.Helper()
	english, arabic := identityPolicySets()
	policies, err := identity.NewStaticPolicySetResolver(english, arabic)
	if err != nil {
		t.Fatalf("constructing policies: %v", err)
	}
	handlers := &identityHandlers{
		service: service, recovery: &fakeRecoveryService{}, policies: policies,
	}
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
		ID: "registration-v1", Version: "set-v1", EffectiveDate: "2026-08-09",
		MinimumAge: 18, PrimaryLocale: identity.LocaleArabic, Locale: identity.LocaleEnglish,
		Policies: append([]identity.RegistrationPolicy(nil), policies...),
	}
	arabic := identity.RegistrationPolicySet{
		ID: "registration-v1", Version: "set-v1", EffectiveDate: "2026-08-09",
		MinimumAge: 18, PrimaryLocale: identity.LocaleArabic, Locale: identity.LocaleArabic,
		Policies: append([]identity.RegistrationPolicy(nil), policies...),
	}
	arabic.Policies[0].Label = "الخصوصية"
	arabic.Policies[1].Label = "الشروط"
	return english, arabic
}

// BR-001: hidden registration and resend outcomes share one acknowledgment
// shape with no cookie, identifier, Account, or delivery claim.
//
// The body is no longer byte-identical, because the response now carries the
// verification context the screen needs to render a code prompt without asking
// for the address a second time. The invariant that mattered is unchanged and
// is asserted more directly below: the acknowledgment carries a fixed key set,
// and the *same* key set with structurally identical values whether the address
// was new or already registered. The domain guarantees that by answering a
// duplicate with a synthetic challenge; this test guarantees the boundary does
// not add a distinguishing field on top of it.
func TestAdmissionAcknowledgmentsAreFixedAndNoStore(t *testing.T) {
	challenge := identity.VerificationChallenge{
		ChallengeID:       "3f1d0a86-6f4a-4a1f-9d0b-2f3a4b5c6d7e",
		MaskedEmail:       "st***@e***.com",
		ExpiresAt:         time.Date(2026, 9, 1, 12, 10, 0, 0, time.UTC),
		ResendAvailableAt: time.Date(2026, 9, 1, 12, 1, 0, 0, time.UTC),
	}
	tests := map[string]struct {
		path     string
		body     string
		wantCode string
	}{
		"registration": {
			path: "/register",
			body: `{"display_name":"Nora Ahmed","email":"student@example.com",` +
				`"password":"correct horse battery staple","locale":"en",` +
				`"policy_set_id":"registration-v1"}`,
			wantCode: "REGISTRATION_REQUEST_ACCEPTED",
		},
		"verification request": {
			path: "/request", body: `{"email":"student@example.com"}`,
			wantCode: "VERIFICATION_REQUEST_ACCEPTED",
		},
	}
	for scenario, test := range tests {
		t.Run(scenario, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			admissionHandlerRouter(t, &fakeAdmissionService{challenge: challenge}).ServeHTTP(response, request)
			if response.Code != http.StatusAccepted {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("acknowledgment is not JSON: %v", err)
			}
			if body["code"] != test.wantCode {
				t.Fatalf("code = %v, want %s", body["code"], test.wantCode)
			}
			if len(body) != 2 {
				t.Fatalf("acknowledgment carries unexpected top-level keys: %v", body)
			}
			verification, ok := body["verification"].(map[string]any)
			if !ok {
				t.Fatal("acknowledgment carries no verification context")
			}
			wantKeys := []string{
				"challenge_id", "masked_email", "expires_at",
				"resend_available_at", "code_length", "maximum_attempts",
			}
			if len(verification) != len(wantKeys) {
				t.Fatalf("verification context key set changed: %v", verification)
			}
			for _, key := range wantKeys {
				if _, present := verification[key]; !present {
					t.Fatalf("verification context is missing %q", key)
				}
			}
			// Nothing in the acknowledgment may name the Account, the address,
			// or the code.
			raw := response.Body.String()
			for _, forbidden := range []string{"student@example.com", "Nora Ahmed"} {
				if strings.Contains(raw, forbidden) {
					t.Fatalf("acknowledgment echoed %q", forbidden)
				}
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

// TestRegistrationAcknowledgmentDoesNotDistinguishAKnownAddress is the
// enumeration guard the fixed-body assertion used to provide.
//
// The domain answers a duplicate address with a synthetic challenge, so both
// outcomes reach this boundary as an ordinary VerificationChallenge. What is
// proved here is that the boundary emits the same shape for both, with values
// that differ only in ways an attacker already controls or already knows.
func TestRegistrationAcknowledgmentDoesNotDistinguishAKnownAddress(t *testing.T) {
	body := `{"display_name":"Nora Ahmed","email":"student@example.com",` +
		`"password":"correct horse battery staple","locale":"en",` +
		`"policy_set_id":"registration-v1"}`
	shapes := make([]string, 0, 2)
	for _, challenge := range []identity.VerificationChallenge{
		{
			ChallengeID:       "11111111-1111-4111-8111-111111111111",
			MaskedEmail:       "st***@e***.com",
			ExpiresAt:         time.Date(2026, 9, 1, 12, 10, 0, 0, time.UTC),
			ResendAvailableAt: time.Date(2026, 9, 1, 12, 1, 0, 0, time.UTC),
		},
		{
			ChallengeID:       "22222222-2222-4222-8222-222222222222",
			MaskedEmail:       "st***@e***.com",
			ExpiresAt:         time.Date(2026, 9, 1, 12, 10, 0, 0, time.UTC),
			ResendAvailableAt: time.Date(2026, 9, 1, 12, 1, 0, 0, time.UTC),
		},
	} {
		request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		admissionHandlerRouter(t, &fakeAdmissionService{challenge: challenge}).ServeHTTP(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("response = %d", response.Code)
		}
		var parsed map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &parsed); err != nil {
			t.Fatal(err)
		}
		verification := parsed["verification"].(map[string]any)
		// The challenge id is expected to differ — it is a fresh opaque handle
		// either way. Everything else must match, or it is a signal.
		delete(verification, "challenge_id")
		normalized, err := json.Marshal(parsed)
		if err != nil {
			t.Fatal(err)
		}
		shapes = append(shapes, string(normalized))
	}
	if shapes[0] != shapes[1] {
		t.Fatalf("a known address is distinguishable from a new one:\n%s\n%s", shapes[0], shapes[1])
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
			!strings.Contains(response.Body.String(), "الخصوصية") ||
			!strings.Contains(response.Body.String(), `"version":"set-v1"`) ||
			!strings.Contains(response.Body.String(), `"effective_date":"2026-08-09"`) {
			t.Fatalf("Arabic policy response = %d %q", response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" ||
			response.Header().Get("Vary") != "Accept-Language" {
			t.Fatalf("Arabic policy cache headers = %#v", response.Header())
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
