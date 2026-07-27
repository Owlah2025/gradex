package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

type recordingSessionResolver struct {
	view       identity.SessionView
	err        error
	gotDigest  string
	gotUseKind identity.CredentialUseKind
}

func (r *recordingSessionResolver) Resolve(
	_ context.Context,
	digest string,
	useKind identity.CredentialUseKind,
	_ string,
) (identity.SessionView, error) {
	r.gotDigest = digest
	r.gotUseKind = useKind
	return r.view, r.err
}

func TestSessionAuthenticatorPreservesUserIDSeam(t *testing.T) {
	credential := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	resolver := &recordingSessionResolver{view: identity.SessionView{
		Session: identity.AuthenticatedSession{AccountID: "account-1"},
	}}
	authenticator, err := NewSessionAuthenticator(resolver)
	if err != nil {
		t.Fatalf("NewSessionAuthenticator: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: credential})
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	userID, err := authenticator.UserFromRequest(context)
	if err != nil {
		t.Fatalf("UserFromRequest: %v", err)
	}
	if userID != "account-1" {
		t.Errorf("user ID = %q, want account-1", userID)
	}
	if resolver.gotDigest != identity.DigestToken(credential) ||
		resolver.gotUseKind != identity.UseReadOnly {
		t.Errorf("resolution input = %q/%v", resolver.gotDigest, resolver.gotUseKind)
	}
}

func TestSessionCredentialDigestRejectsAmbiguousOrMalformedCookies(t *testing.T) {
	valid := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	tests := map[string][]*http.Cookie{
		"missing":   nil,
		"duplicate": {{Name: SessionCookieName, Value: valid}, {Name: SessionCookieName, Value: valid}},
		"short":     {{Name: SessionCookieName, Value: "c2hvcnQ"}},
		"padded":    {{Name: SessionCookieName, Value: valid + "="}},
		"wrong alphabet": {{
			Name: SessionCookieName, Value: strings.Repeat("/", 43),
		}},
	}
	for name, cookies := range tests {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, cookie := range cookies {
				request.AddCookie(cookie)
			}
			if _, err := SessionCredentialDigest(request); !errors.Is(
				err, identity.ErrAuthenticationRequired,
			) {
				t.Errorf("error = %v, want authentication required", err)
			}
		})
	}
}

func TestWriteSessionResponseUsesHardenedCookieAndNeverReflectsCredential(t *testing.T) {
	credentialBytes := make([]byte, 32)
	csrfBytes := make([]byte, 32)
	for i := range csrfBytes {
		csrfBytes[i] = 1
	}
	credential := config.NewSecret(base64.RawURLEncoding.EncodeToString(credentialBytes))
	csrf := config.NewSecret(base64.RawURLEncoding.EncodeToString(csrfBytes))
	recorder := httptest.NewRecorder()
	err := WriteSessionResponse(
		recorder,
		http.StatusCreated,
		identity.AuthenticatedSession{
			DisplayName: "Session Student",
			Role:        identity.RoleStudent,
			IdleExpiresAt: time.Date(
				2026, 8, 1, 12, 0, 0, 0, time.UTC,
			),
			AbsoluteExpiresAt: time.Date(
				2026, 8, 30, 12, 0, 0, 0, time.UTC,
			),
		},
		&credential,
		csrf,
	)
	if err != nil {
		t.Fatalf("WriteSessionResponse: %v", err)
	}
	if strings.Contains(recorder.Body.String(), credential.Expose()) {
		t.Fatal("JSON response reflected the session credential")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != SessionCookieName || !cookie.Secure || !cookie.HttpOnly ||
		cookie.Path != "/" || cookie.Domain != "" ||
		cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie attributes are unsafe: %#v", cookie)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}
