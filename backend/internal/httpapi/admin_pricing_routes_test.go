package httpapi

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func makeTestReq(t *testing.T, method, path string, body string) *http.Request {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Origin", "https://gradex.example")
	validToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))
	req.AddCookie(&http.Cookie{
		Name:  auth.SessionCookieName,
		Value: validToken,
	})
	req.Header.Set("X-CSRF-Token", validToken)
	return req
}

func TestAdminPricingRoutesAuthorization(t *testing.T) {
	instructorAccountID := "inst-account-1"
	r, _ := authzRouterWithSession(t, fixedPrincipals{
		principal: identity.Principal{
			AccountID:       instructorAccountID,
			Role:            identity.RoleInstructor,
			Status:          identity.StatusActive,
			CredentialState: identity.CredentialActive,
		},
	}, identity.Session{
		AccountID:         instructorAccountID,
		AuthenticatedAt:   time.Now().UTC(),
		IdleExpiresAt:     time.Now().UTC().Add(24 * time.Hour),
		AbsoluteExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	})

	t.Run("Instructor direct write to course price is refused with 403", func(t *testing.T) {
		req := makeTestReq(t, http.MethodPut, "/api/v1/admin/courses/course-1/price", `{"price_minor_units": 1000, "reason": "test"}`)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("got status %d, want 403 Forbidden", rec.Code)
		}
	})

	t.Run("Instructor direct write to section price is refused with 403", func(t *testing.T) {
		req := makeTestReq(t, http.MethodPut, "/api/v1/admin/courses/course-1/sections/sec-1/price", `{"price_minor_units": 500, "reason": "test"}`)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("got status %d, want 403 Forbidden", rec.Code)
		}
	})

	t.Run("Instructor read of price history is refused with 403", func(t *testing.T) {
		req := makeTestReq(t, http.MethodGet, "/api/v1/admin/courses/course-1/price-history", "")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("got status %d, want 403 Forbidden", rec.Code)
		}
	})
}

func TestAdminPricingInputValidations(t *testing.T) {
	adminAccountID := "admin-account-1"
	r, _ := authzRouterWithSession(t, fixedPrincipals{
		principal: identity.Principal{
			AccountID:       adminAccountID,
			Role:            identity.RoleAdmin,
			Status:          identity.StatusActive,
			CredentialState: identity.CredentialActive,
		},
	}, identity.Session{
		AccountID:         adminAccountID,
		AuthenticatedAt:   time.Now().UTC(),
		IdleExpiresAt:     time.Now().UTC().Add(24 * time.Hour),
		AbsoluteExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	})

	t.Run("Refuses negative price", func(t *testing.T) {
		req := makeTestReq(t, http.MethodPut, "/api/v1/admin/courses/course-1/price", `{"price_minor_units": -100, "reason": "test"}`)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("got status %d (body: %s), want 422 Unprocessable Entity", rec.Code, rec.Body.String())
		}
	})

	t.Run("Refuses empty reason", func(t *testing.T) {
		req := makeTestReq(t, http.MethodPut, "/api/v1/admin/courses/course-1/price", `{"price_minor_units": 1000, "reason": "   "}`)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("got status %d (body: %s), want 422 Unprocessable Entity", rec.Code, rec.Body.String())
		}
	})
}
