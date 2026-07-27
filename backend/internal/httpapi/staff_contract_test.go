package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func adminPrincipal() identity.Principal {
	return identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleAdmin,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}
}

// These tests cover the three review findings that moved the staff surface onto
// its frozen contract in specs/002-auth-rbac/s1c/spec.md §7. The route paths and
// methods themselves are asserted by TestAuthorizationMatrixMatchesMountedRouter,
// which derives them from the live router; what is asserted here is the
// behaviour those routes were rejected for.

// The preview bearer moved from a request body to a header because §7 specifies
// GET. A GET body is not reliably carried, and a query parameter would put a
// one-time secret into access logs, referrers, and history.
func TestPreviewAcceptsBearerHeaderAndRefusesWithoutIt(t *testing.T) {
	t.Run("with bearer header", func(t *testing.T) {
		r, _ := authzRouter(t, fixedPrincipals{principal: adminPrincipal()})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff-invitations/preview", nil)
		req.Header.Set(invitationBearerHeader, strings.Repeat("a", 43))
		rec := do(r, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		// The response describes the state of a one-time secret, so it must never
		// be cacheable.
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	})

	t.Run("without bearer header", func(t *testing.T) {
		r, _ := authzRouter(t, fixedPrincipals{principal: adminPrincipal()})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff-invitations/preview", nil)
		rec := do(r, req)

		if rec.Code == http.StatusOK {
			t.Fatal("a preview with no bearer header returned 200")
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store on the refusal path too", got)
		}
	})
}

// Both suspension routes previously bypassed strict binding entirely, so their
// declared body limits were unreferenced and the body was unbounded. Oversized
// input must now be refused before any handler work.
func TestSuspensionRoutesBoundTheirRequestBodies(t *testing.T) {
	oversized := []byte(`{"reason":"` + strings.Repeat("x", int(staffSuspendBodyLimit)+64) + `"}`)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"suspend", http.MethodPost, "/api/v1/accounts/acct-1/suspension"},
		{"reinstate", http.MethodDelete, "/api/v1/accounts/acct-1/suspension"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := authzRouter(t, fixedPrincipals{principal: adminPrincipal()})
			req := newAuthenticatedRequest(tc.method, tc.path, oversized)
			rec := do(r, req)

			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Reinstatement accepted an empty reason, which let a privileged account-status
// change be recorded with no explanation. It is now required, as suspension
// already was.
func TestReinstatementRequiresAReason(t *testing.T) {
	r, _ := authzRouter(t, fixedPrincipals{principal: adminPrincipal()})
	req := newAuthenticatedRequest(http.MethodDelete, "/api/v1/accounts/acct-1/suspension", []byte(`{}`))
	rec := do(r, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
}

// A body that is absent entirely is refused for the same reason: the reason is
// mandatory, and an absent body cannot carry one.
func TestReinstatementRefusesAnAbsentBody(t *testing.T) {
	r, _ := authzRouter(t, fixedPrincipals{principal: adminPrincipal()})
	req := newAuthenticatedRequest(http.MethodDelete, "/api/v1/accounts/acct-1/suspension", nil)
	rec := do(r, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want a refusal: %s", rec.Code, rec.Body.String())
	}
}
