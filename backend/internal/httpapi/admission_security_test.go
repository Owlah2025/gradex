package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newTestAnonymousSecurity(t *testing.T) *anonymousSecurity {
	t.Helper()
	security, err := newAnonymousSecurity(
		"https://gradex.example",
		[]byte(strings.Repeat("a", 32)),
		[]byte(strings.Repeat("b", 32)),
		30*time.Minute,
	)
	if err != nil {
		t.Fatalf("constructing anonymous security: %v", err)
	}
	return security
}

func TestAnonymousBootstrapCookieAndCSRFContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	security := newTestAnonymousSecurity(t)
	router := gin.New()
	router.GET("/api/v1/session/bootstrap", security.bootstrapHandler())

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/session/bootstrap", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", first.Code, first.Body.String())
	}
	if got := first.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	cookies := first.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != anonymousCookieName || !cookie.Secure || !cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("anonymous cookie has unsafe attributes: %#v", cookie)
	}

	var firstBody struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if firstBody.CSRFToken == "" {
		t.Fatal("bootstrap returned no CSRF token")
	}
	if strings.Contains(cookie.Value, firstBody.CSRFToken) {
		t.Fatal("cookie contains the browser-readable CSRF value")
	}

	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session/bootstrap", nil)
	secondRequest.AddCookie(cookie)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, secondRequest)

	var secondBody struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondBody); err != nil {
		t.Fatalf("decoding reused response: %v", err)
	}
	if secondBody.CSRFToken != firstBody.CSRFToken {
		t.Fatal("valid anonymous state did not reuse its CSRF token")
	}
}

func TestAdmissionSecurityRequiresExactOriginAndBoundCSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	security := newTestAnonymousSecurity(t)

	bootstrapRouter := gin.New()
	bootstrapRouter.GET("/bootstrap", security.bootstrapHandler())
	bootstrap := httptest.NewRecorder()
	bootstrapRouter.ServeHTTP(bootstrap, httptest.NewRequest(http.MethodGet, "/bootstrap", nil))
	cookie := bootstrap.Result().Cookies()[0]
	var body struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(bootstrap.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding bootstrap: %v", err)
	}

	tests := map[string]struct {
		origin  string
		referer string
		csrf    string
		cookie  *http.Cookie
		want    int
	}{
		"exact origin": {
			origin: "https://gradex.example", csrf: body.CSRFToken, cookie: cookie, want: http.StatusNoContent,
		},
		"HTTPS referer fallback": {
			referer: "https://gradex.example/register?from=home", csrf: body.CSRFToken, cookie: cookie, want: http.StatusNoContent,
		},
		"sibling origin": {
			origin: "https://evil.gradex.example", csrf: body.CSRFToken, cookie: cookie, want: http.StatusForbidden,
		},
		"origin with trusted referer still fails": {
			origin: "https://evil.example", referer: "https://gradex.example/register", csrf: body.CSRFToken, cookie: cookie, want: http.StatusForbidden,
		},
		"missing browser origin": {
			csrf: body.CSRFToken, cookie: cookie, want: http.StatusForbidden,
		},
		"missing cookie": {
			origin: "https://gradex.example", csrf: body.CSRFToken, want: http.StatusForbidden,
		},
		"wrong CSRF": {
			origin: "https://gradex.example", csrf: "wrong", cookie: cookie, want: http.StatusForbidden,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			reached := false
			router.POST("/admission", security.requireAdmission(), func(c *gin.Context) {
				reached = true
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodPost, "/admission", strings.NewReader(`{}`))
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				request.Header.Set("Referer", tt.referer)
			}
			if tt.csrf != "" {
				request.Header.Set(csrfHeaderName, tt.csrf)
			}
			if tt.cookie != nil {
				request.AddCookie(tt.cookie)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, tt.want, recorder.Body.String())
			}
			if reached != (tt.want == http.StatusNoContent) {
				t.Fatalf("next-handler reached = %v for status %d", reached, tt.want)
			}
		})
	}
}

// BR-003/FR-014: structural admission precedes browser-security admission, so
// malformed input is not mislabeled as CSRF and never reaches domain work.
func TestAdmissionMiddlewareOrderIsStructureThenBrowserSecurity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	security := newTestAnonymousSecurity(t)
	router := gin.New()
	reached := false
	router.POST(
		"/admission",
		strictJSONMiddleware(func() any { return &strictRequest{} }, 64),
		security.requireAdmission(),
		func(c *gin.Context) {
			reached = true
			c.Status(http.StatusNoContent)
		},
	)

	malformed := httptest.NewRequest(http.MethodPost, "/admission", strings.NewReader(`{"email":`))
	malformed.Header.Set("Content-Type", "application/json")
	malformedResponse := httptest.NewRecorder()
	router.ServeHTTP(malformedResponse, malformed)
	if malformedResponse.Code != http.StatusBadRequest {
		t.Fatalf("malformed status = %d, want 400: %s",
			malformedResponse.Code, malformedResponse.Body.String())
	}

	valid := httptest.NewRequest(
		http.MethodPost,
		"/admission",
		strings.NewReader(`{"email":"student@example.com"}`),
	)
	valid.Header.Set("Content-Type", "application/json")
	validResponse := httptest.NewRecorder()
	router.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusForbidden {
		t.Fatalf("untrusted valid status = %d, want 403: %s",
			validResponse.Code, validResponse.Body.String())
	}
	if reached {
		t.Fatal("domain handler ran before structural and browser admission completed")
	}
}

func TestAnonymousBootstrapRejectsUnsupportedMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.NoMethod(methodNotAllowedHandler(router))
	router.GET("/api/v1/session/bootstrap", newTestAnonymousSecurity(t).bootstrapHandler())

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/session/bootstrap",
		strings.NewReader(`{}`),
	))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405: %s", response.Code, response.Body.String())
	}
	var got struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding method problem: %v", err)
	}
	if got.Code != "METHOD_NOT_ALLOWED" {
		t.Fatalf("code = %q, want METHOD_NOT_ALLOWED", got.Code)
	}
}
