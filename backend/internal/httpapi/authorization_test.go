package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
	"github.com/Owlah2025/gradex/backend/internal/video"
)

// fixedPrincipals returns one principal for every identifier, or an error.
type fixedPrincipals struct {
	principal identity.Principal
	err       error
}

func (f fixedPrincipals) ResolvePrincipal(context.Context, string) (identity.Principal, error) {
	if f.err != nil {
		return identity.Principal{}, f.err
	}
	return f.principal, nil
}

func authzRouter(t *testing.T, principals identity.PrincipalResolver) (*gin.Engine, *syncBuffer) {
	now := time.Now().UTC()
	return authzRouterWithSession(t, principals, identity.Session{
		ID:                "fake-session-id",
		AccountID:         "11111111-1111-1111-1111-111111111111",
		State:             identity.SessionActive,
		AuthenticatedAt:   now,
		AdmittedEpoch:     0,
		IdleExpiresAt:     now.Add(24 * time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	})
}

func authzRouterWithSession(t *testing.T, principals identity.PrincipalResolver, session identity.Session) (*gin.Engine, *syncBuffer) {
	t.Helper()

	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	buf := &syncBuffer{}
	logger := logging.New(buf, "gradex-api-test", "development", logging.LevelFromString("info"))
	reporter := health.New(time.Second)
	reporter.MarkStarted()

	limiter, _ := ratelimit.New(fakeRateStore{}, bytes.Repeat([]byte{0x31}, 32), time.Second)
	staffPolicies := map[string]ratelimit.Policy{
		"staff-invitations-create":   ratelimit.DevelopmentStaffInvitationPolicy("staff-invitations-create"),
		"staff-invitations-preview":  ratelimit.DevelopmentStaffInvitationPolicy("staff-invitations-preview"),
		"staff-invitations-complete": ratelimit.DevelopmentStaffInvitationPolicy("staff-invitations-complete"),
	}
	staffFoundation, err := NewStaffFoundation(StaffFoundationOptions{
		Service:          fakeStaffService{},
		Limiter:          limiter,
		EndpointPolicies: staffPolicies,
		RecentAuthWindow: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("constructing staff foundation: %v", err)
	}

	sessionPolicies := map[string]ratelimit.Policy{
		"session-bootstrap":  ratelimit.DevelopmentAnonymousBootstrapPolicy(),
		"sessions":           ratelimit.DevelopmentLoginPolicy(),
		"session-resolution": ratelimit.DevelopmentSessionPolicy("session-resolution"),
		"session-renewals":   ratelimit.DevelopmentSessionPolicy("session-renewals"),
		"session-logout":     ratelimit.DevelopmentSessionPolicy("session-logout"),
	}
	sessionRepo := &fakeSessionRepository{
		view: identity.SessionView{
			Session: identity.AuthenticatedSession{
				AccountID:         session.AccountID,
				SessionID:         session.ID,
				Role:              identity.RoleAdmin,
				CredentialState:   identity.CredentialActive,
				AuthenticatedAt:   session.AuthenticatedAt,
				ReauthenticatedAt: session.ReauthenticatedAt,
				IdleExpiresAt:     session.IdleExpiresAt,
				AbsoluteExpiresAt: session.AbsoluteExpiresAt,
			},
		},
	}
	sessionFoundation, err := NewSessionFoundation(SessionFoundationOptions{
		PublicOrigin:        "https://gradex.example",
		CookieSigningKey:    bytes.Repeat([]byte{0x31}, 32),
		AnonymousCSRFKey:    bytes.Repeat([]byte{0x32}, 32),
		AnonymousSessionTTL: 24 * time.Hour,
		Repository:          sessionRepo,
		Limiter:             limiter,
		EndpointPolicies:    sessionPolicies,
	})
	if err != nil {
		t.Fatalf("constructing session foundation: %v", err)
	}

	admissionPolicies := make(map[string]ratelimit.Policy)
	for _, endpoint := range []string{
		"student-registrations", "email-verification-requests", "email-verifications",
		"password-reset-requests", "password-resets",
	} {
		admissionPolicies[endpoint] = ratelimit.DevelopmentAdmissionPolicy(endpoint)
	}
	admissionPolicies["registration-policy-set"] = ratelimit.DevelopmentPolicySetReadPolicy()
	admissionPolicies["session-bootstrap"] = ratelimit.DevelopmentAnonymousBootstrapPolicy()

	english, arabic := identityPolicySets()
	policies, err := identity.NewStaticPolicySetResolver(english, arabic)
	if err != nil {
		t.Fatalf("constructing policy resolver: %v", err)
	}

	admissionFoundation, err := NewAdmissionFoundation(AdmissionFoundationOptions{
		PublicOrigin:        "https://gradex.example",
		CookieSigningKey:    bytes.Repeat([]byte{0x31}, 32),
		CSRFKey:             bytes.Repeat([]byte{0x32}, 32),
		AnonymousSessionTTL: 24 * time.Hour,
		Policies:            policies,
		Service:             &fakeAdmissionService{},
		Recovery:            &fakeRecoveryService{},
		Limiter:             limiter,
		EndpointPolicies:    admissionPolicies,
	})
	if err != nil {
		t.Fatalf("constructing admission foundation: %v", err)
	}

	outboxWriter, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("constructing outbox writer: %v", err)
	}

	catalogFoundation, err := NewCatalogFoundation(CatalogFoundationOptions{
		Ownership:      fakeOwnershipChecker{},
		AssetValidator: fakeAssetValidator{},
		OutboxWriter:   outboxWriter,
	})
	if err != nil {
		t.Fatalf("constructing catalog foundation: %v", err)
	}

	r, err := NewRouter(cfg, logger, reporter, fakeService{}, sessionFoundation.authenticator,
		fakeEntitlements{allowed: true}, principals,
		WithStaffFoundation(staffFoundation),
		WithSessionFoundation(sessionFoundation),
		WithAdmissionFoundation(admissionFoundation),
		WithCatalogFoundation(catalogFoundation),
	)
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	return r, buf
}

func newAuthenticatedRequest(method, path string, body []byte) *http.Request {
	var req *http.Request
	if len(body) > 0 {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
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

type fakeRateStore struct{}

func (fakeRateStore) Decide(context.Context, []ratelimit.Entry) (bool, error) {
	return true, nil
}

type fakeStaffService struct{}

func (fakeStaffService) CreateStaffInvitation(_ context.Context, req identity.CreateStaffInvitationRequest) (identity.IssuedStaffInvitation, error) {
	if err := identity.CheckRecentAuthentication(req.ActorSession, req.RecentAuthWindow, req.Now); err != nil {
		return identity.IssuedStaffInvitation{}, err
	}
	return identity.IssuedStaffInvitation{}, nil
}

func (fakeStaffService) PreviewStaffInvitation(context.Context, string, time.Time) (identity.StaffInvitationPreview, error) {
	return identity.StaffInvitationPreview{}, nil
}

func (fakeStaffService) CompleteStaffInvitation(context.Context, identity.CompleteStaffInvitationRequest) (identity.CompleteStaffInvitationResult, error) {
	return identity.CompleteStaffInvitationResult{}, nil
}

func (fakeStaffService) RevokeStaffInvitation(_ context.Context, req identity.RevokeStaffInvitationRequest) error {
	return identity.CheckRecentAuthentication(req.ActorSession, req.RecentAuthWindow, req.Now)
}

func (fakeStaffService) SuspendAccount(_ context.Context, req identity.SuspendAccountRequest) (identity.SuspendAccountResult, error) {
	if err := identity.CheckRecentAuthentication(req.ActorSession, req.RecentAuthWindow, req.Now); err != nil {
		return identity.SuspendAccountResult{}, err
	}
	return identity.SuspendAccountResult{}, nil
}

func (fakeStaffService) ReinstateAccount(_ context.Context, req identity.ReinstateAccountRequest) (identity.ReinstateAccountResult, error) {
	if err := identity.CheckRecentAuthentication(req.ActorSession, req.RecentAuthWindow, req.Now); err != nil {
		return identity.ReinstateAccountResult{}, err
	}
	return identity.ReinstateAccountResult{}, nil
}

func (fakeStaffService) ListPendingInvitations(context.Context, identity.Principal) ([]identity.StaffInvitation, error) {
	return nil, nil
}

type RouteClass string

const (
	ClassAnonymous                     RouteClass = "ANONYMOUS"
	ClassAuthenticatedSessionLifecycle RouteClass = "AUTHENTICATED_SESSION_LIFECYCLE"
	ClassCapabilityProtected           RouteClass = "CAPABILITY_PROTECTED"
	ClassOwnershipProtected            RouteClass = "OWNERSHIP_PROTECTED"
	ClassRecentAuthRequired            RouteClass = "RECENT_AUTH_REQUIRED"
)

type RouteMatrixEntry struct {
	Method string
	Path   string
	Class  RouteClass
}

var expectedRouteMatrix = map[string]RouteMatrixEntry{
	"GET /healthz":                                   {Method: http.MethodGet, Path: "/healthz", Class: ClassAnonymous},
	"GET /readyz":                                    {Method: http.MethodGet, Path: "/readyz", Class: ClassAnonymous},
	"GET /api/v1/session/bootstrap":                  {Method: http.MethodGet, Path: "/api/v1/session/bootstrap", Class: ClassAnonymous},
	"GET /api/v1/registration-policy-set":            {Method: http.MethodGet, Path: "/api/v1/registration-policy-set", Class: ClassAnonymous},
	"POST /api/v1/student-registrations":             {Method: http.MethodPost, Path: "/api/v1/student-registrations", Class: ClassAnonymous},
	"POST /api/v1/email-verification-requests":       {Method: http.MethodPost, Path: "/api/v1/email-verification-requests", Class: ClassAnonymous},
	"POST /api/v1/email-verifications":               {Method: http.MethodPost, Path: "/api/v1/email-verifications", Class: ClassAnonymous},
	"POST /api/v1/password-reset-requests":           {Method: http.MethodPost, Path: "/api/v1/password-reset-requests", Class: ClassAnonymous},
	"POST /api/v1/password-resets":                   {Method: http.MethodPost, Path: "/api/v1/password-resets", Class: ClassAnonymous},
	"POST /api/v1/sessions":                          {Method: http.MethodPost, Path: "/api/v1/sessions", Class: ClassAnonymous},
	"GET /api/v1/staff-invitations/preview":          {Method: http.MethodGet, Path: "/api/v1/staff-invitations/preview", Class: ClassAnonymous},
	"POST /api/v1/staff-invitation-completions":      {Method: http.MethodPost, Path: "/api/v1/staff-invitation-completions", Class: ClassAnonymous},
	"GET /api/v1/videos/:videoID/manifest/*filepath": {Method: http.MethodGet, Path: "/api/v1/videos/:videoID/manifest/*filepath", Class: ClassAnonymous},

	"GET /api/v1/session":           {Method: http.MethodGet, Path: "/api/v1/session", Class: ClassAuthenticatedSessionLifecycle},
	"POST /api/v1/session-renewals": {Method: http.MethodPost, Path: "/api/v1/session-renewals", Class: ClassAuthenticatedSessionLifecycle},
	"DELETE /api/v1/session":        {Method: http.MethodDelete, Path: "/api/v1/session", Class: ClassAuthenticatedSessionLifecycle},

	"GET /api/v1/staff-invitations": {Method: http.MethodGet, Path: "/api/v1/staff-invitations", Class: ClassCapabilityProtected},

	"POST /api/v1/lessons/:lessonID/video/upload-url":  {Method: http.MethodPost, Path: "/api/v1/lessons/:lessonID/video/upload-url", Class: ClassOwnershipProtected},
	"POST /api/v1/lessons/:lessonID/video/complete":    {Method: http.MethodPost, Path: "/api/v1/lessons/:lessonID/video/complete", Class: ClassOwnershipProtected},
	"POST /api/v1/lessons/:lessonID/video/retry":       {Method: http.MethodPost, Path: "/api/v1/lessons/:lessonID/video/retry", Class: ClassOwnershipProtected},
	"POST /api/v1/lessons/:lessonID/video/publish":     {Method: http.MethodPost, Path: "/api/v1/lessons/:lessonID/video/publish", Class: ClassOwnershipProtected},
	"GET /api/v1/lessons/:lessonID/video/playback-url": {Method: http.MethodGet, Path: "/api/v1/lessons/:lessonID/video/playback-url", Class: ClassOwnershipProtected},
	"POST /api/v1/lessons/:lessonID/progress":          {Method: http.MethodPost, Path: "/api/v1/lessons/:lessonID/progress", Class: ClassOwnershipProtected},

	"POST /api/v1/staff-invitations":         {Method: http.MethodPost, Path: "/api/v1/staff-invitations", Class: ClassRecentAuthRequired},
	"DELETE /api/v1/staff-invitations/:id":   {Method: http.MethodDelete, Path: "/api/v1/staff-invitations/:id", Class: ClassRecentAuthRequired},
	"POST /api/v1/accounts/:id/suspension":   {Method: http.MethodPost, Path: "/api/v1/accounts/:id/suspension", Class: ClassRecentAuthRequired},
	"DELETE /api/v1/accounts/:id/suspension": {Method: http.MethodDelete, Path: "/api/v1/accounts/:id/suspension", Class: ClassRecentAuthRequired},

	"POST /api/v1/courses":                                 {Method: http.MethodPost, Path: "/api/v1/courses", Class: ClassCapabilityProtected},
	"GET /api/v1/courses":                                  {Method: http.MethodGet, Path: "/api/v1/courses", Class: ClassCapabilityProtected},
	"GET /api/v1/taxonomy/terms":                           {Method: http.MethodGet, Path: "/api/v1/taxonomy/terms", Class: ClassCapabilityProtected},
	"GET /api/v1/courses/:id":                              {Method: http.MethodGet, Path: "/api/v1/courses/:id", Class: ClassOwnershipProtected},
	"PATCH /api/v1/courses/:id":                            {Method: http.MethodPatch, Path: "/api/v1/courses/:id", Class: ClassOwnershipProtected},
	"POST /api/v1/courses/:id/sections":                    {Method: http.MethodPost, Path: "/api/v1/courses/:id/sections", Class: ClassOwnershipProtected},
	"PATCH /api/v1/courses/:id/sections/:sectionId":        {Method: http.MethodPatch, Path: "/api/v1/courses/:id/sections/:sectionId", Class: ClassOwnershipProtected},
	"DELETE /api/v1/courses/:id/sections/:sectionId":       {Method: http.MethodDelete, Path: "/api/v1/courses/:id/sections/:sectionId", Class: ClassOwnershipProtected},
	"POST /api/v1/courses/:id/sections/:sectionId/lessons": {Method: http.MethodPost, Path: "/api/v1/courses/:id/sections/:sectionId/lessons", Class: ClassOwnershipProtected},
	"PATCH /api/v1/courses/:id/lessons/:lessonId":          {Method: http.MethodPatch, Path: "/api/v1/courses/:id/lessons/:lessonId", Class: ClassOwnershipProtected},
	"DELETE /api/v1/courses/:id/lessons/:lessonId":         {Method: http.MethodDelete, Path: "/api/v1/courses/:id/lessons/:lessonId", Class: ClassOwnershipProtected},
	"PUT /api/v1/courses/:id/lessons/:lessonId/video":      {Method: http.MethodPut, Path: "/api/v1/courses/:id/lessons/:lessonId/video", Class: ClassOwnershipProtected},
	"PUT /api/v1/courses/:id/lessons/:lessonId/files":      {Method: http.MethodPut, Path: "/api/v1/courses/:id/lessons/:lessonId/files", Class: ClassOwnershipProtected},
	"DELETE /api/v1/courses/:id/lessons/:lessonId/files":   {Method: http.MethodDelete, Path: "/api/v1/courses/:id/lessons/:lessonId/files", Class: ClassOwnershipProtected},
	"PUT /api/v1/courses/:id/preview":                      {Method: http.MethodPut, Path: "/api/v1/courses/:id/preview", Class: ClassOwnershipProtected},
	"DELETE /api/v1/courses/:id/preview":                   {Method: http.MethodDelete, Path: "/api/v1/courses/:id/preview", Class: ClassOwnershipProtected},
}

type fakeOwnershipChecker struct{}

func (fakeOwnershipChecker) IsCourseOwner(context.Context, string, string) (bool, error) {
	return false, nil
}

type fakeAssetValidator struct{}

func (fakeAssetValidator) ValidateAssetVersion(context.Context, string) error {
	return nil
}

func derivedProtectedRoutes(r *gin.Engine) []struct{ method, path string } {
	routes := r.Routes()
	var result []struct{ method, path string }
	for _, rt := range routes {
		key := rt.Method + " " + rt.Path
		entry, ok := expectedRouteMatrix[key]
		if !ok {
			continue
		}
		if entry.Class == ClassAnonymous || entry.Class == ClassAuthenticatedSessionLifecycle {
			continue
		}
		execPath := rt.Path
		execPath = strings.ReplaceAll(execPath, ":lessonID", "lesson-99")
		execPath = strings.ReplaceAll(execPath, ":lessonId", "lesson-99")
		execPath = strings.ReplaceAll(execPath, ":sectionId", "section-99")
		execPath = strings.ReplaceAll(execPath, ":id", "acct-99")
		result = append(result, struct{ method, path string }{
			method: rt.Method,
			path:   execPath,
		})
	}
	return result
}

func TestAuthorizationMatrixMatchesMountedRouter(t *testing.T) {
	r, _ := authzRouter(t, fixedPrincipals{})
	routes := r.Routes()

	mountedMap := make(map[string]bool)
	for _, rt := range routes {
		key := rt.Method + " " + rt.Path
		mountedMap[key] = true
		if _, ok := expectedRouteMatrix[key]; !ok {
			t.Fatalf("mounted route %s has no authorization matrix row", key)
		}
	}

	for key := range expectedRouteMatrix {
		if !mountedMap[key] {
			t.Fatalf("matrix row %s references a route no longer mounted", key)
		}
	}
}

// Bootstrap close condition 3, initial end-to-end denial evidence.
func TestRestrictedBootstrapAdminIsDeniedOnRealProtectedRoutes(t *testing.T) {
	bootstrapAdmin := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleAdmin,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialChangeRequired,
	}

	r, _ := authzRouter(t, fixedPrincipals{principal: bootstrapAdmin})
	protectedRoutes := derivedProtectedRoutes(r)

	if len(protectedRoutes) == 0 {
		t.Fatal("no protected routes derived from router; this test would pass vacuously")
	}

	for _, route := range protectedRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			r, buf := authzRouter(t, fixedPrincipals{principal: bootstrapAdmin})

			var body []byte
			if route.method == http.MethodPost {
				body = []byte(`{"email":"test@example.com","role":"INSTRUCTOR","reason":"test"}`)
			}
			req := newAuthenticatedRequest(route.method, route.path, body)
			rec := do(r, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
			}

			p := assertProblemEnvelope(t, rec)
			if p.Code != "NOT_AUTHORIZED" {
				t.Errorf("code = %q, want NOT_AUTHORIZED", p.Code)
			}
			bodyStr := rec.Body.String()
			for _, leak := range []string{
				"PASSWORD_CHANGE_REQUIRED", "CHANGE_REQUIRED", "bootstrap",
				"ADMIN", "suspended", "credential",
			} {
				if strings.Contains(bodyStr, leak) {
					t.Errorf("response leaked policy state %q: %s", leak, bodyStr)
				}
			}

			assertDenyLogged(t, buf, "PASSWORD_CHANGE_REQUIRED")
		})
	}
}

func TestUnrestrictedAdminReachesContentManagementRoutes(t *testing.T) {
	admin := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleAdmin,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}

	r, _ := authzRouter(t, fixedPrincipals{principal: admin})
	req := newAuthenticatedRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/publish", nil)
	rec := do(r, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("an ordinary Admin was refused content management: %s", rec.Body.String())
	}
}

func TestSuspendedAccountIsDeniedOnRealProtectedRoutes(t *testing.T) {
	suspended := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleInstructor,
		Status:          identity.StatusSuspended,
		CredentialState: identity.CredentialActive,
	}

	r, _ := authzRouter(t, fixedPrincipals{principal: suspended})
	protectedRoutes := derivedProtectedRoutes(r)

	for _, route := range protectedRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			r, buf := authzRouter(t, fixedPrincipals{principal: suspended})
			var body []byte
			if route.method == http.MethodPost {
				body = []byte(`{"email":"test@example.com","role":"INSTRUCTOR","reason":"test"}`)
			}
			req := newAuthenticatedRequest(route.method, route.path, body)
			rec := do(r, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			assertDenyLogged(t, buf, "ACCOUNT_SUSPENDED")
		})
	}
}

func TestFreshAdminSucceedsAndStaleAdminIsRefusedOnStaffEndpoints(t *testing.T) {
	admin := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleAdmin,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}

	now := time.Now().UTC()
	freshSession := identity.Session{
		ID:              "fresh-session",
		AccountID:       admin.AccountID,
		State:           identity.SessionActive,
		AuthenticatedAt: now,
	}

	staleSession := identity.Session{
		ID:              "stale-session",
		AccountID:       admin.AccountID,
		State:           identity.SessionActive,
		AuthenticatedAt: now.Add(-2 * time.Hour),
	}

	endpoints := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"create-invitation", http.MethodPost, "/api/v1/staff-invitations", []byte(`{"email":"staff@example.com","role":"INSTRUCTOR"}`)},
		{"revoke-invitation", http.MethodDelete, "/api/v1/staff-invitations/inv-99", nil},
		{"suspend-account", http.MethodPost, "/api/v1/accounts/acct-99/suspension", []byte(`{"reason":"violation"}`)},
		{"reinstate-account", http.MethodDelete, "/api/v1/accounts/acct-99/suspension", []byte(`{"reason":"remedied"}`)},
	}

	for _, ep := range endpoints {
		t.Run(ep.name+"_FRESH_ADMIN_SUCCEEDS", func(t *testing.T) {
			r, _ := authzRouterWithSession(t, fixedPrincipals{principal: admin}, freshSession)
			req := newAuthenticatedRequest(ep.method, ep.path, ep.body)
			rec := do(r, req)

			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Fatalf("fresh admin was refused with status %d: %s", rec.Code, rec.Body.String())
			}
		})

		t.Run(ep.name+"_STALE_ADMIN_REFUSED", func(t *testing.T) {
			r, _ := authzRouterWithSession(t, fixedPrincipals{principal: admin}, staleSession)
			req := newAuthenticatedRequest(ep.method, ep.path, ep.body)
			rec := do(r, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("stale admin got status %d, want 403: %s", rec.Code, rec.Body.String())
			}
			p := assertProblemEnvelope(t, rec)
			if p.Code != "NOT_AUTHORIZED" {
				t.Errorf("code = %q, want NOT_AUTHORIZED", p.Code)
			}
		})
	}
}

func TestUnknownPrincipalIsDenied(t *testing.T) {
	r, buf := authzRouter(t, fixedPrincipals{err: identity.ErrPrincipalNotFound})

	req := newAuthenticatedRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/publish", nil)
	rec := do(r, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	assertDenyLogged(t, buf, "PRINCIPAL_NOT_FOUND")
}

func TestPrincipalResolutionFailureIsAFaultNotADenial(t *testing.T) {
	r, buf := authzRouter(t, fixedPrincipals{
		err: errors.New(`pq: duplicate key value violates unique constraint "videos_lesson_id_key"`),
	})

	req := newAuthenticatedRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/publish", nil)
	rec := do(r, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if p := assertProblemEnvelope(t, rec); p.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %q, want INTERNAL_ERROR", p.Code)
	}

	logged := buf.String()
	if !strings.Contains(logged, "authorization_fault") {
		t.Errorf("expected an authorization_fault log line, got %s", logged)
	}
	if strings.Contains(logged, "authorization_denied") {
		t.Error("a resolution fault was logged as a denial")
	}
}

func assertDenyLogged(t *testing.T, buf *syncBuffer, wantReason string) {
	t.Helper()

	var found bool
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["msg"] != "authorization_denied" {
			continue
		}
		found = true

		if rec["deny_reason"] != wantReason {
			t.Errorf("deny_reason = %v, want %q", rec["deny_reason"], wantReason)
		}
		if rec["capability"] == nil || rec["capability"] == "" {
			t.Error("the denial log has no capability")
		}
		for _, forbidden := range []string{"account_id", "principal", "email"} {
			if _, present := rec[forbidden]; present {
				t.Errorf("the denial log carries %q", forbidden)
			}
		}
	}

	if !found {
		t.Errorf("no authorization_denied line was logged; got %s", buf.String())
	}
}

func TestCapabilityPolicyRunsBeforeOwnershipChecks(t *testing.T) {
	restricted := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleAdmin,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialChangeRequired,
	}

	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	buf := &syncBuffer{}
	logger := logging.New(buf, "gradex-api-test", "development", logging.LevelFromString("info"))
	reporter := health.New(time.Second)
	reporter.MarkStarted()

	var entitlementConsulted bool
	tripwire := trippingEntitlements{consulted: &entitlementConsulted}

	r, err := NewRouter(cfg, logger, reporter, fakeService{}, fakeAuth{},
		tripwire, fixedPrincipals{principal: restricted})
	if err != nil {
		t.Fatalf("router: %v", err)
	}

	req := newAuthenticatedRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/publish", nil)
	rec := do(r, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if entitlementConsulted {
		t.Error("the ownership check ran before the capability policy denied the request")
	}
}

type trippingEntitlements struct{ consulted *bool }

func (e trippingEntitlements) HasAccess(context.Context, string, string) (bool, error) {
	*e.consulted = true
	return true, nil
}

func (e trippingEntitlements) IsInstructorForLesson(context.Context, string, string) (bool, error) {
	*e.consulted = true
	return true, nil
}

func TestRouterRefusesToBuildWithoutAPrincipalResolver(t *testing.T) {
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	logger := logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info"))
	reporter := health.New(time.Second)

	if _, err := NewRouter(cfg, logger, reporter, fakeService{}, fakeAuth{},
		fakeEntitlements{allowed: true}, nil); err == nil {
		t.Fatal("a router was built with no principal resolver")
	}
}

func TestOwnedResourceRoutesDerivedSweep(t *testing.T) {
	instructor := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleInstructor,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}

	r, _ := authzRouter(t, fixedPrincipals{principal: instructor})
	routes := r.Routes()

	for _, rt := range routes {
		if !(strings.HasPrefix(rt.Path, "/api/v1/courses/:id") || strings.HasPrefix(rt.Path, "/api/v1/courses/:courseID")) {
			continue
		}

		execPath := rt.Path
		execPath = strings.ReplaceAll(execPath, ":id", "course-99")
		execPath = strings.ReplaceAll(execPath, ":courseID", "course-99")

		var body []byte
		if rt.Method == http.MethodPost || rt.Method == http.MethodPut || rt.Method == http.MethodPatch {
			body = []byte(`{}`)
		}

		req := newAuthenticatedRequest(rt.Method, execPath, body)
		rec := do(r, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("owned route %s %s did not enforce RequireCourseOwnership (status = %d, want 403)",
				rt.Method, rt.Path, rec.Code)
		}
	}
}

func TestSubmitRouteIsUnmountedAndReturns404(t *testing.T) {
	instructor := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleInstructor,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}

	r, _ := authzRouter(t, fixedPrincipals{principal: instructor})
	req := newAuthenticatedRequest(http.MethodPost, "/api/v1/courses/course-99/submit", []byte(`{}`))
	rec := do(r, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /api/v1/courses/course-99/submit returned status %d, want 404", rec.Code)
	}
}

var _ = video.Service(fakeService{})
