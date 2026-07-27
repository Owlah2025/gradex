package problem

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdmissionProblemsHaveFixedSafeContracts(t *testing.T) {
	tests := map[string]struct {
		got        Problem
		wantStatus int
		wantCode   string
	}{
		"malformed JSON":                   {Malformed(), http.StatusBadRequest, "MALFORMED_JSON"},
		"content too large":                {ContentTooLarge(), http.StatusRequestEntityTooLarge, "CONTENT_TOO_LARGE"},
		"unsupported media type":           {UnsupportedMediaType(), http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE"},
		"not acceptable":                   {NotAcceptable(), http.StatusNotAcceptable, "NOT_ACCEPTABLE"},
		"invalid token":                    {TokenInvalid(), http.StatusBadRequest, "TOKEN_INVALID"},
		"rate limited":                     {RateLimited(), http.StatusTooManyRequests, "RATE_LIMITED"},
		"rate limiting unavailable":        {RateLimitingUnavailable(), http.StatusServiceUnavailable, "RATE_LIMITING_UNAVAILABLE"},
		"registration unavailable":         {RegistrationUnavailable(), http.StatusServiceUnavailable, "REGISTRATION_UNAVAILABLE"},
		"transactional delivery unsafe":    {TransactionalDeliveryUnavailable(), http.StatusServiceUnavailable, "TRANSACTIONAL_DELIVERY_UNAVAILABLE"},
		"anonymous CSRF validation failed": {CSRFValidationFailed(), http.StatusForbidden, "CSRF_VALIDATION_FAILED"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.got.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d", tt.got.Status, tt.wantStatus)
			}
			if tt.got.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", tt.got.Code, tt.wantCode)
			}
			if tt.got.Type != typeBase+strings.ToLower(strings.ReplaceAll(tt.wantCode, "_", "-")) {
				t.Errorf("type = %q, want code-derived type", tt.got.Type)
			}
			for _, forbidden := range []string{
				"redis", "postgres", "database", "provider", "email exists",
				"screening", "queue", "backlog", "account state",
			} {
				if strings.Contains(strings.ToLower(tt.got.Title+" "+tt.got.Detail), forbidden) {
					t.Errorf("public text discloses %q: %#v", forbidden, tt.got)
				}
			}
		})
	}
}

func TestTokenInvalidDoesNotDistinguishSecretState(t *testing.T) {
	got := TokenInvalid()
	if got.Title != "Link unavailable" {
		t.Errorf("title = %q, want fixed safe title", got.Title)
	}
	if got.Detail != "This verification link cannot be used." {
		t.Errorf("detail = %q, want fixed safe detail", got.Detail)
	}
}

func TestAuthenticatedSessionProblemsHaveFixedSafeContracts(t *testing.T) {
	tests := map[string]struct {
		got        Problem
		wantStatus int
		wantCode   string
	}{
		"login failed":          {AuthenticationFailed(), http.StatusUnauthorized, "AUTHENTICATION_FAILED"},
		"authentication needed": {Unauthenticated(), http.StatusUnauthorized, "AUTHENTICATION_REQUIRED"},
		"session replaced":      {SessionReplaced(), http.StatusUnauthorized, "SESSION_REPLACED"},
		"reuse detected":        {SessionReuseDetected(), http.StatusUnauthorized, "SESSION_REUSE_DETECTED"},
		"CSRF failed":           {SessionCSRFFailed(), http.StatusForbidden, "CSRF_FAILED"},
		"origin rejected":       {OriginNotAllowed(), http.StatusForbidden, "ORIGIN_NOT_ALLOWED"},
		"auth unavailable":      {AuthenticationUnavailable(), http.StatusServiceUnavailable, "AUTHENTICATION_UNAVAILABLE"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.got.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d", tt.got.Status, tt.wantStatus)
			}
			if tt.got.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", tt.got.Code, tt.wantCode)
			}
			if tt.got.Type != typeBase+strings.ToLower(strings.ReplaceAll(tt.wantCode, "_", "-")) {
				t.Errorf("type = %q, want code-derived type", tt.got.Type)
			}
			for _, forbidden := range []string{
				"email exists", "unverified", "inactive", "suspended", "generation",
				"credential", "csrf token", "database", "postgres",
			} {
				if strings.Contains(strings.ToLower(tt.got.Title+" "+tt.got.Detail), forbidden) {
					t.Errorf("public text discloses %q: %#v", forbidden, tt.got)
				}
			}
		})
	}
}

func TestAuthenticationProblemsUseOneStableSessionChallenge(t *testing.T) {
	problems := []Problem{
		AuthenticationFailed(),
		Unauthenticated(),
		SessionReplaced(),
		SessionReuseDetected(),
	}
	for _, p := range problems {
		recorder := httptest.NewRecorder()
		if err := Write(recorder, p); err != nil {
			t.Fatalf("Write(%s): %v", p.Code, err)
		}
		if got := recorder.Header().Get("WWW-Authenticate"); got != `GradexSession realm="gradex-web"` {
			t.Errorf("%s challenge = %q, want fixed GradexSession challenge", p.Code, got)
		}
	}

	recorder := httptest.NewRecorder()
	if err := Write(recorder, SessionCSRFFailed()); err != nil {
		t.Fatalf("Write(CSRF_FAILED): %v", err)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != "" {
		t.Errorf("403 challenge = %q, want no authentication challenge", got)
	}
}
