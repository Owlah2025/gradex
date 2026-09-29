package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	adminread "github.com/Owlah2025/gradex/backend/internal/admin"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

type fakeAdminService struct{}

func (fakeAdminService) SearchAccounts(context.Context, adminread.AccountDirectoryRequest) (adminread.AccountDirectoryResult, error) {
	return adminread.AccountDirectoryResult{Accounts: []adminread.AccountDirectoryEntry{}, Page: 1, Limit: 25}, nil
}

func (fakeAdminService) GetAccount(
	context.Context,
	adminread.AccountIdentityRequest,
) (adminread.AccountDirectoryEntry, error) {
	return adminread.AccountDirectoryEntry{
		ID: "10000000-0000-0000-0000-000000000001",
	}, nil
}

func (fakeAdminService) ListAuditEvents(context.Context, adminread.AuditEventRequest) (adminread.AuditEventResult, error) {
	return adminread.AuditEventResult{Events: []adminread.AuditEvent{}, Page: 1, Limit: 25}, nil
}

func TestAdminOperationsRoutesAuthorization(t *testing.T) {
	paths := []string{
		"/api/v1/admin/accounts",
		"/api/v1/admin/accounts/10000000-0000-0000-0000-000000000001",
		"/api/v1/admin/audit-events",
	}
	t.Run("anonymous is refused", func(t *testing.T) {
		router, _ := authzRouterWithSessionAndAuthenticator(
			t,
			fixedPrincipals{principal: adminTestPrincipal()},
			identity.Session{AccountID: "10000000-0000-0000-0000-000000000001"},
			fakeAuth{err: errors.New("anonymous")},
		)
		for _, path := range paths {
			t.Run(path, func(t *testing.T) {
				response := serveAdminReadRequest(t, router, path)
				if response.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401", response.Code)
				}
			})
		}
	})
	for _, role := range []identity.Role{identity.RoleStudent, identity.RoleInstructor} {
		t.Run(string(role)+" is refused", func(t *testing.T) {
			router, _ := authzRouterWithSession(
				t,
				fixedPrincipals{principal: identity.Principal{
					AccountID:       "10000000-0000-0000-0000-000000000001",
					Role:            role,
					Status:          identity.StatusActive,
					CredentialState: identity.CredentialActive,
				}},
				identity.Session{AccountID: "10000000-0000-0000-0000-000000000001"},
			)
			for _, path := range paths {
				t.Run(path, func(t *testing.T) {
					response := serveAdminReadRequest(t, router, path)
					if response.Code != http.StatusForbidden {
						t.Fatalf("status = %d, want 403", response.Code)
					}
				})
			}
		})
	}
	t.Run("admin can read all routes", func(t *testing.T) {
		router, _ := authzRouter(t, fixedPrincipals{principal: adminTestPrincipal()})
		for _, path := range paths {
			t.Run(path, func(t *testing.T) {
				response := serveAdminReadRequest(t, router, path)
				if response.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
				}
			})
		}
	})
}

func adminTestPrincipal() identity.Principal {
	return identity.Principal{
		AccountID:       "10000000-0000-0000-0000-000000000001",
		Role:            identity.RoleAdmin,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}
}

func serveAdminReadRequest(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	request := makeTestReq(t, http.MethodGet, path, "")
	request.Header.Set("Accept-Language", "en")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
