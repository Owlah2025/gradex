package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func (fakeAdminService) ListEmailDeliveries(context.Context, adminread.EmailDeliveriesRequest) (adminread.EmailDeliveriesResult, error) {
	return adminread.EmailDeliveriesResult{Items: []adminread.EmailDelivery{}, Page: 1, Limit: 25}, nil
}

func (fakeAdminService) ListMediaFailures(context.Context, adminread.MediaFailuresRequest) (adminread.MediaFailuresResult, error) {
	return adminread.MediaFailuresResult{Items: []adminread.MediaFailure{}, Page: 1, Limit: 25}, nil
}

func (fakeAdminService) ExportAccounts(context.Context, adminread.AccountDirectoryRequest) (adminread.AccountExportResult, error) {
	return adminread.AccountExportResult{Accounts: []adminread.AccountDirectoryEntry{}}, nil
}

func (fakeAdminService) GetMetricsOverview(context.Context, adminread.MetricsOverviewRequest) (adminread.MetricsOverviewResult, error) {
	return adminread.MetricsOverviewResult{Metrics: []adminread.Metric{}, AsOf: time.Now().UTC()}, nil
}

func (fakeAdminService) ListMetricsCourses(context.Context, adminread.MetricsCoursesRequest) (adminread.MetricsCoursesResult, error) {
	return adminread.MetricsCoursesResult{Items: []adminread.CourseMetric{}, Page: 1, Limit: 25}, nil
}

func (fakeAdminService) ListMetricsInstructors(context.Context, adminread.MetricsInstructorsRequest) (adminread.MetricsInstructorsResult, error) {
	return adminread.MetricsInstructorsResult{Items: []adminread.InstructorMetric{}, Page: 1, Limit: 25}, nil
}

func (fakeAdminService) GetInbox(context.Context, adminread.InboxRequest) (adminread.InboxResult, error) {
	return adminread.InboxResult{Sections: []adminread.InboxSection{}}, nil
}

func (fakeAdminService) GetSearchMetrics(context.Context, adminread.SearchMetricsRequest) (adminread.SearchMetricsResult, error) {
	return adminread.SearchMetricsResult{TopQueries: []adminread.SearchQueryMetric{}, ZeroResultQueries: []adminread.SearchQueryMetric{}}, nil
}

func (fakeAdminService) GetUser360(context.Context, adminread.User360Request) (adminread.User360, error) {
	return adminread.User360{}, nil
}

func (fakeAdminService) ListNotes(context.Context, adminread.NoteListRequest) (adminread.NoteListResult, error) {
	return adminread.NoteListResult{Notes: []adminread.AdminNote{}}, nil
}

func (fakeAdminService) AddNote(context.Context, adminread.AddNoteRequest) (adminread.AdminNote, error) {
	return adminread.AdminNote{}, nil
}

func (fakeAdminService) RevokeAccountSessions(context.Context, adminread.SessionRevocationRequest) (adminread.SessionRevocationResult, error) {
	return adminread.SessionRevocationResult{}, nil
}

func (fakeAdminService) ListCourseOptions(context.Context, adminread.CourseOptionsRequest) ([]adminread.CourseOption, error) {
	return []adminread.CourseOption{}, nil
}

func (fakeAdminService) DiagnoseAccess(context.Context, adminread.AccessDiagnosticRequest) (adminread.AccessDiagnostic, error) {
	return adminread.AccessDiagnostic{}, nil
}

func (fakeAdminService) ListSecurityEvents(context.Context, adminread.SecurityEventsRequest) (adminread.SecurityEventsResult, error) {
	return adminread.SecurityEventsResult{Events: []adminread.SecurityEvent{}}, nil
}

func TestAdminOperationsRoutesAuthorization(t *testing.T) {
	paths := []string{
		"/api/v1/admin/accounts",
		"/api/v1/admin/accounts/10000000-0000-0000-0000-000000000001",
		"/api/v1/admin/audit-events",
		"/api/v1/admin/metrics/overview",
		"/api/v1/admin/metrics/courses",
		"/api/v1/admin/metrics/instructors",
		"/api/v1/admin/inbox",
		"/api/v1/admin/email-deliveries",
		"/api/v1/admin/media/failures",
		"/api/v1/admin/accounts/export",
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
