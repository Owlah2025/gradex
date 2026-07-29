package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func TestAdminTaxonomyOverrideRouteRejectsInstructor(t *testing.T) {
	instructorID := "instructor-account-1"
	instructorRouter, _ := authzRouterWithSession(t, fixedPrincipals{principal: identity.Principal{
		AccountID: instructorID, Role: identity.RoleInstructor, Status: identity.StatusActive, CredentialState: identity.CredentialActive,
	}}, identity.Session{AccountID: instructorID, AuthenticatedAt: time.Now().UTC(), IdleExpiresAt: time.Now().UTC().Add(time.Hour), AbsoluteExpiresAt: time.Now().UTC().Add(time.Hour)})

	req := makeTestReq(t, http.MethodPut, "/api/v1/admin/courses/course-1/taxonomy", `{"revision_id":"revision-1","major_term_id":"major-1","subject_term_id":"subject-1"}`)
	rec := httptest.NewRecorder()
	instructorRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Instructor override status = %d, want 403", rec.Code)
	}
}

func TestAdminTaxonomyOverrideRouteRequiresRevisionID(t *testing.T) {
	adminID := "admin-account-1"
	adminRouter, _ := authzRouterWithSession(t, fixedPrincipals{principal: identity.Principal{
		AccountID: adminID, Role: identity.RoleAdmin, Status: identity.StatusActive, CredentialState: identity.CredentialActive,
	}}, identity.Session{AccountID: adminID, AuthenticatedAt: time.Now().UTC(), IdleExpiresAt: time.Now().UTC().Add(time.Hour), AbsoluteExpiresAt: time.Now().UTC().Add(time.Hour)})

	req := makeTestReq(t, http.MethodPut, "/api/v1/admin/courses/course-1/taxonomy", `{"major_term_id":"major-1","subject_term_id":"subject-1"}`)
	rec := httptest.NewRecorder()
	adminRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing revision_id status = %d, want 422", rec.Code)
	}
}

func TestAdminTaxonomyTermRoutesValidateRequestBodies(t *testing.T) {
	adminID := "admin-account-1"
	adminRouter, _ := authzRouterWithSession(t, fixedPrincipals{principal: identity.Principal{
		AccountID: adminID, Role: identity.RoleAdmin, Status: identity.StatusActive, CredentialState: identity.CredentialActive,
	}}, identity.Session{AccountID: adminID, AuthenticatedAt: time.Now().UTC(), IdleExpiresAt: time.Now().UTC().Add(time.Hour), AbsoluteExpiresAt: time.Now().UTC().Add(time.Hour)})

	for _, request := range []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "malformed create", method: http.MethodPost, path: "/api/v1/admin/taxonomy/terms", body: `{`, want: http.StatusBadRequest},
		{name: "invalid create", method: http.MethodPost, path: "/api/v1/admin/taxonomy/terms", body: `{"kind":"MAJOR","label_ar":"","label_en":""}`, want: http.StatusUnprocessableEntity},
		{name: "invalid rename", method: http.MethodPatch, path: "/api/v1/admin/taxonomy/terms/term-1", body: `{"label_ar":"","label_en":""}`, want: http.StatusUnprocessableEntity},
	} {
		t.Run(request.name, func(t *testing.T) {
			req := makeTestReq(t, request.method, request.path, request.body)
			rec := httptest.NewRecorder()
			adminRouter.ServeHTTP(rec, req)
			if rec.Code != request.want {
				t.Fatalf("%s status = %d, want %d", request.name, rec.Code, request.want)
			}
		})
	}
}
