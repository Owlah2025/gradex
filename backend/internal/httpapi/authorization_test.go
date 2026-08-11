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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
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

	sessionPolicies := testSessionEndpointPolicies()
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
		Compromised:         testCompromisedSource(t),
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
	pool, err := pgxpool.New(context.Background(), "postgres://gradex:gradex@127.0.0.1:1/gradex?sslmode=disable")
	if err != nil {
		t.Fatalf("constructing lazy catalog pool: %v", err)
	}
	t.Cleanup(pool.Close)
	catalogRepository, err := catalog.NewRepository(pool, outboxWriter)
	if err != nil {
		t.Fatalf("constructing catalog repository: %v", err)
	}

	catalogFoundation, err := NewCatalogFoundation(CatalogFoundationOptions{
		Repository:     catalogRepository,
		Ownership:      fakeOwnershipChecker{},
		AssetValidator: fakeAssetValidator{},
	})
	if err != nil {
		t.Fatalf("constructing catalog foundation: %v", err)
	}
	learningRepository, err := learning.NewRepository(pool)
	if err != nil {
		t.Fatalf("constructing learning repository: %v", err)
	}
	learningFoundation, err := NewLearningFoundation(LearningFoundationOptions{
		ReportContexts: testReportContextIssuer(t),
		Repository:     learningRepository,
		Evaluator:      learningFoundationEvaluator{},
		Media:          learningFoundationMedia{},
		Limiter:        testLearningLimiter(t),
		Policies:       testLearningPolicies(),
	})
	if err != nil {
		t.Fatalf("constructing learning foundation: %v", err)
	}

	accessRepo, err := access.NewRepository(pool, outboxWriter)
	if err != nil {
		t.Fatalf("constructing access repository: %v", err)
	}
	accessFoundation, err := NewAccessFoundation(AccessFoundationOptions{
		Repository: accessRepo,
	})
	if err != nil {
		t.Fatalf("constructing access foundation: %v", err)
	}

	r, err := NewRouter(cfg, logger, reporter, sessionFoundation.authenticator, principals,
		WithStaffFoundation(staffFoundation),
		WithSessionFoundation(sessionFoundation),
		WithAdmissionFoundation(admissionFoundation),
		WithCatalogFoundation(catalogFoundation),
		WithLearningFoundation(learningFoundation),
		WithAccessFoundation(accessFoundation),
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
	"GET /healthz":                              {Method: http.MethodGet, Path: "/healthz", Class: ClassAnonymous},
	"GET /readyz":                               {Method: http.MethodGet, Path: "/readyz", Class: ClassAnonymous},
	"GET /api/v1/session/bootstrap":             {Method: http.MethodGet, Path: "/api/v1/session/bootstrap", Class: ClassAnonymous},
	"GET /api/v1/registration-policy-set":       {Method: http.MethodGet, Path: "/api/v1/registration-policy-set", Class: ClassAnonymous},
	"POST /api/v1/student-registrations":        {Method: http.MethodPost, Path: "/api/v1/student-registrations", Class: ClassAnonymous},
	"POST /api/v1/email-verification-requests":  {Method: http.MethodPost, Path: "/api/v1/email-verification-requests", Class: ClassAnonymous},
	"POST /api/v1/email-verifications":          {Method: http.MethodPost, Path: "/api/v1/email-verifications", Class: ClassAnonymous},
	"POST /api/v1/password-reset-requests":      {Method: http.MethodPost, Path: "/api/v1/password-reset-requests", Class: ClassAnonymous},
	"POST /api/v1/password-resets":              {Method: http.MethodPost, Path: "/api/v1/password-resets", Class: ClassAnonymous},
	"POST /api/v1/sessions":                     {Method: http.MethodPost, Path: "/api/v1/sessions", Class: ClassAnonymous},
	"GET /api/v1/staff-invitations/preview":     {Method: http.MethodGet, Path: "/api/v1/staff-invitations/preview", Class: ClassAnonymous},
	"POST /api/v1/staff-invitation-completions": {Method: http.MethodPost, Path: "/api/v1/staff-invitation-completions", Class: ClassAnonymous},

	"GET /api/v1/session":           {Method: http.MethodGet, Path: "/api/v1/session", Class: ClassAuthenticatedSessionLifecycle},
	"POST /api/v1/session-renewals": {Method: http.MethodPost, Path: "/api/v1/session-renewals", Class: ClassAuthenticatedSessionLifecycle},
	"DELETE /api/v1/session":        {Method: http.MethodDelete, Path: "/api/v1/session", Class: ClassAuthenticatedSessionLifecycle},

	// The authenticated password change is session lifecycle, not a product
	// capability: it rotates the caller's own session and is one of the two
	// things §4.5 permits a restricted principal to do.
	//
	// The class matters here, and it is not bookkeeping. Every route classified
	// as capability- or ownership-protected is swept by
	// TestRestrictedBootstrapAdminIsDeniedOnRealProtectedRoutes, which asserts a
	// CHANGE_REQUIRED principal is refused. This route must NOT be refused —
	// refusing it is the launch defect — so it belongs with the other lifecycle
	// routes. That it is genuinely reachable in that state is asserted directly
	// by TestRestrictedPrincipalReachesOnlyThePasswordChangeRoute below, so
	// nothing about this classification removes the route from proof.
	"POST /api/v1/password-changes": {Method: http.MethodPost, Path: "/api/v1/password-changes", Class: ClassAuthenticatedSessionLifecycle},

	"GET /api/v1/staff-invitations": {Method: http.MethodGet, Path: "/api/v1/staff-invitations", Class: ClassCapabilityProtected},

	"POST /api/v1/staff-invitations":         {Method: http.MethodPost, Path: "/api/v1/staff-invitations", Class: ClassRecentAuthRequired},
	"DELETE /api/v1/staff-invitations/:id":   {Method: http.MethodDelete, Path: "/api/v1/staff-invitations/:id", Class: ClassRecentAuthRequired},
	"POST /api/v1/accounts/:id/suspension":   {Method: http.MethodPost, Path: "/api/v1/accounts/:id/suspension", Class: ClassRecentAuthRequired},
	"DELETE /api/v1/accounts/:id/suspension": {Method: http.MethodDelete, Path: "/api/v1/accounts/:id/suspension", Class: ClassRecentAuthRequired},

	"POST /api/v1/courses":                                                       {Method: http.MethodPost, Path: "/api/v1/courses", Class: ClassCapabilityProtected},
	"GET /api/v1/courses":                                                        {Method: http.MethodGet, Path: "/api/v1/courses", Class: ClassCapabilityProtected},
	"GET /api/v1/taxonomy/terms":                                                 {Method: http.MethodGet, Path: "/api/v1/taxonomy/terms", Class: ClassCapabilityProtected},
	"GET /api/v1/learn/dashboard":                                                {Method: http.MethodGet, Path: "/api/v1/learn/dashboard", Class: ClassCapabilityProtected},
	"GET /api/v1/learn/courses/:courseId":                                        {Method: http.MethodGet, Path: "/api/v1/learn/courses/:courseId", Class: ClassCapabilityProtected},
	"GET /api/v1/learn/courses/:courseId/lessons/:lessonId":                      {Method: http.MethodGet, Path: "/api/v1/learn/courses/:courseId/lessons/:lessonId", Class: ClassCapabilityProtected},
	"POST /api/v1/learn/lessons/:lessonId/playback":                              {Method: http.MethodPost, Path: "/api/v1/learn/lessons/:lessonId/playback", Class: ClassCapabilityProtected},
	"PUT /api/v1/learn/lessons/:lessonId/progress":                               {Method: http.MethodPut, Path: "/api/v1/learn/lessons/:lessonId/progress", Class: ClassCapabilityProtected},
	"POST /api/v1/learn/reports":                                                 {Method: http.MethodPost, Path: "/api/v1/learn/reports", Class: ClassCapabilityProtected},
	"GET /api/v1/courses/:id":                                                    {Method: http.MethodGet, Path: "/api/v1/courses/:id", Class: ClassOwnershipProtected},
	"PUT /api/v1/courses/:id/candidate":                                          {Method: http.MethodPut, Path: "/api/v1/courses/:id/candidate", Class: ClassOwnershipProtected},
	"PATCH /api/v1/courses/:id/revisions/:revisionId":                            {Method: http.MethodPatch, Path: "/api/v1/courses/:id/revisions/:revisionId", Class: ClassOwnershipProtected},
	"POST /api/v1/courses/:id/revisions/:revisionId/sections":                    {Method: http.MethodPost, Path: "/api/v1/courses/:id/revisions/:revisionId/sections", Class: ClassOwnershipProtected},
	"PATCH /api/v1/courses/:id/revisions/:revisionId/sections/:sectionId":        {Method: http.MethodPatch, Path: "/api/v1/courses/:id/revisions/:revisionId/sections/:sectionId", Class: ClassOwnershipProtected},
	"DELETE /api/v1/courses/:id/revisions/:revisionId/sections/:sectionId":       {Method: http.MethodDelete, Path: "/api/v1/courses/:id/revisions/:revisionId/sections/:sectionId", Class: ClassOwnershipProtected},
	"POST /api/v1/courses/:id/revisions/:revisionId/sections/:sectionId/lessons": {Method: http.MethodPost, Path: "/api/v1/courses/:id/revisions/:revisionId/sections/:sectionId/lessons", Class: ClassOwnershipProtected},
	"PATCH /api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId":          {Method: http.MethodPatch, Path: "/api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId", Class: ClassOwnershipProtected},
	"DELETE /api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId":         {Method: http.MethodDelete, Path: "/api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId", Class: ClassOwnershipProtected},
	"PUT /api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/video":      {Method: http.MethodPut, Path: "/api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/video", Class: ClassOwnershipProtected},
	"PUT /api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/files":      {Method: http.MethodPut, Path: "/api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/files", Class: ClassOwnershipProtected},
	"DELETE /api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/files":   {Method: http.MethodDelete, Path: "/api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/files", Class: ClassOwnershipProtected},
	"PUT /api/v1/courses/:id/revisions/:revisionId/preview":                      {Method: http.MethodPut, Path: "/api/v1/courses/:id/revisions/:revisionId/preview", Class: ClassOwnershipProtected},
	"DELETE /api/v1/courses/:id/revisions/:revisionId/preview":                   {Method: http.MethodDelete, Path: "/api/v1/courses/:id/revisions/:revisionId/preview", Class: ClassOwnershipProtected},
	"POST /api/v1/courses/:id/revisions/:revisionId/submit":                      {Method: http.MethodPost, Path: "/api/v1/courses/:id/revisions/:revisionId/submit", Class: ClassOwnershipProtected},

	"GET /api/v1/admin/review/queue":                                                {Method: http.MethodGet, Path: "/api/v1/admin/review/queue", Class: ClassCapabilityProtected},
	"GET /api/v1/admin/review/courses/:id/revisions/:revisionId":                    {Method: http.MethodGet, Path: "/api/v1/admin/review/courses/:id/revisions/:revisionId", Class: ClassCapabilityProtected},
	"GET /api/v1/admin/review/playback-manifests/:playbackSession/index.m3u8":       {Method: http.MethodGet, Path: "/api/v1/admin/review/playback-manifests/:playbackSession/index.m3u8", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/review/courses/:id/revisions/:revisionId/approve":           {Method: http.MethodPost, Path: "/api/v1/admin/review/courses/:id/revisions/:revisionId/approve", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/review/courses/:id/revisions/:revisionId/request-changes":   {Method: http.MethodPost, Path: "/api/v1/admin/review/courses/:id/revisions/:revisionId/request-changes", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/review/courses/:id/revisions/:revisionId/preview/:lessonId": {Method: http.MethodPost, Path: "/api/v1/admin/review/courses/:id/revisions/:revisionId/preview/:lessonId", Class: ClassCapabilityProtected},

	"PUT /api/v1/admin/courses/:id/price":                     {Method: http.MethodPut, Path: "/api/v1/admin/courses/:id/price", Class: ClassCapabilityProtected},
	"PUT /api/v1/admin/courses/:id/default-access-expiry":     {Method: http.MethodPut, Path: "/api/v1/admin/courses/:id/default-access-expiry", Class: ClassCapabilityProtected},
	"PUT /api/v1/admin/courses/:id/sections/:sectionId/price": {Method: http.MethodPut, Path: "/api/v1/admin/courses/:id/sections/:sectionId/price", Class: ClassCapabilityProtected},
	"GET /api/v1/admin/courses/:id/price-history":             {Method: http.MethodGet, Path: "/api/v1/admin/courses/:id/price-history", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/courses/:id/delist":                   {Method: http.MethodPost, Path: "/api/v1/admin/courses/:id/delist", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/courses/:id/relist":                   {Method: http.MethodPost, Path: "/api/v1/admin/courses/:id/relist", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/courses/:id/retire":                   {Method: http.MethodPost, Path: "/api/v1/admin/courses/:id/retire", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/courses/:id/archive":                  {Method: http.MethodPost, Path: "/api/v1/admin/courses/:id/archive", Class: ClassCapabilityProtected},
	"DELETE /api/v1/admin/courses/:id":                        {Method: http.MethodDelete, Path: "/api/v1/admin/courses/:id", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/courses/:id/owner":                    {Method: http.MethodPost, Path: "/api/v1/admin/courses/:id/owner", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/courses/:id/access-suspension":        {Method: http.MethodPost, Path: "/api/v1/admin/courses/:id/access-suspension", Class: ClassCapabilityProtected},
	"DELETE /api/v1/admin/courses/:id/access-suspension":      {Method: http.MethodDelete, Path: "/api/v1/admin/courses/:id/access-suspension", Class: ClassCapabilityProtected},
	"PUT /api/v1/admin/courses/:id/taxonomy":                  {Method: http.MethodPut, Path: "/api/v1/admin/courses/:id/taxonomy", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/taxonomy/terms":                       {Method: http.MethodPost, Path: "/api/v1/admin/taxonomy/terms", Class: ClassCapabilityProtected},
	"PATCH /api/v1/admin/taxonomy/terms/:id":                  {Method: http.MethodPatch, Path: "/api/v1/admin/taxonomy/terms/:id", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/taxonomy/terms/:id/retire":            {Method: http.MethodPost, Path: "/api/v1/admin/taxonomy/terms/:id/retire", Class: ClassCapabilityProtected},
	"DELETE /api/v1/admin/taxonomy/terms/:id":                 {Method: http.MethodDelete, Path: "/api/v1/admin/taxonomy/terms/:id", Class: ClassCapabilityProtected},

	"POST /api/v1/admin/course-access-invitations":             {Method: http.MethodPost, Path: "/api/v1/admin/course-access-invitations", Class: ClassCapabilityProtected},
	"GET /api/v1/admin/course-access-invitations":              {Method: http.MethodGet, Path: "/api/v1/admin/course-access-invitations", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/course-access-invitations/:id/approve": {Method: http.MethodPost, Path: "/api/v1/admin/course-access-invitations/:id/approve", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/course-access-invitations/:id/reject":  {Method: http.MethodPost, Path: "/api/v1/admin/course-access-invitations/:id/reject", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/course-access-invitations/:id/cancel":  {Method: http.MethodPost, Path: "/api/v1/admin/course-access-invitations/:id/cancel", Class: ClassCapabilityProtected},
	"POST /api/v1/admin/course-access-invitations/:id/resend":  {Method: http.MethodPost, Path: "/api/v1/admin/course-access-invitations/:id/resend", Class: ClassCapabilityProtected},
	"GET /api/v1/admin/entitlements/:id":                       {Method: http.MethodGet, Path: "/api/v1/admin/entitlements/:id", Class: ClassCapabilityProtected},
	"GET /api/v1/me/course-access-invitations":                 {Method: http.MethodGet, Path: "/api/v1/me/course-access-invitations", Class: ClassCapabilityProtected},
	"GET /api/v1/me/course-access-invitations/:id":             {Method: http.MethodGet, Path: "/api/v1/me/course-access-invitations/:id", Class: ClassCapabilityProtected},
	"POST /api/v1/me/course-access-invitations/:id/accept":     {Method: http.MethodPost, Path: "/api/v1/me/course-access-invitations/:id/accept", Class: ClassCapabilityProtected},
	"GET /api/v1/me/course-access":                             {Method: http.MethodGet, Path: "/api/v1/me/course-access", Class: ClassCapabilityProtected},
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
		execPath := materializeRouteParameters(rt.Path, "route-99")
		result = append(result, struct{ method, path string }{
			method: rt.Method,
			path:   execPath,
		})
	}
	return result
}

func isProtectedLearningRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/learn/")
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

// TestCatalogAdminMutationRoutesDenyInstructor derives the full Admin catalog
// mutation set from the mounted router. A 403 here proves the capability gate
// runs before request binding and repository work; no handler-specific body is
// needed to reach that boundary.
func TestCatalogAdminMutationRoutesDenyInstructor(t *testing.T) {
	instructor := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleInstructor,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}
	r, _ := authzRouter(t, fixedPrincipals{principal: instructor})

	var routes []gin.RouteInfo
	for _, route := range r.Routes() {
		if route.Method == http.MethodGet || !strings.HasPrefix(route.Path, "/api/v1/admin/") {
			continue
		}
		routes = append(routes, route)
	}
	if len(routes) == 0 {
		t.Fatal("no Admin catalog mutation routes were derived from the router")
	}

	for _, route := range routes {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			req := newAuthenticatedRequest(route.Method, materializeAuthorizationRoute(route.Path), nil)
			rec := do(r, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("Instructor status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestCatalogAdminReadRoutesDenyInstructor is the read half of the sweep above.
//
// The Admin Catalog surface now loads its review queue and its submitted
// revision graphs from the server rather than from component state, so those
// GET routes carry real Course data and an Instructor must not reach them —
// not the queue of everyone else's submissions, and not another Instructor's
// revision graph. The set is derived from the mounted router, so a future
// Admin read route is covered the moment it is mounted.
func TestCatalogAdminReadRoutesDenyInstructor(t *testing.T) {
	instructor := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleInstructor,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}
	r, _ := authzRouter(t, fixedPrincipals{principal: instructor})

	var routes []gin.RouteInfo
	for _, route := range r.Routes() {
		if route.Method != http.MethodGet || !strings.HasPrefix(route.Path, "/api/v1/admin/") {
			continue
		}
		routes = append(routes, route)
	}
	if len(routes) == 0 {
		t.Fatal("no Admin catalog read routes were derived from the router")
	}

	for _, route := range routes {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			req := newAuthenticatedRequest(route.Method, materializeAuthorizationRoute(route.Path), nil)
			rec := do(r, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("Instructor status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func materializeAuthorizationRoute(path string) string {
	return materializeRouteParameters(path, "route-99")
}

// materializeRouteParameters replaces every Gin path parameter, including a
// future parameter whose name this test does not know yet. A protected-route
// sweep must never accidentally test a literal ":newParameter" path.
func materializeRouteParameters(path, replacement string) string {
	for {
		start := strings.IndexByte(path, ':')
		if start < 0 {
			return path
		}
		end := start + 1
		for end < len(path) {
			ch := path[end]
			if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' {
				break
			}
			end++
		}
		path = path[:start] + replacement + path[end:]
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

			if isProtectedLearningRoute(route.path) {
				if rec.Code != http.StatusNotFound {
					t.Fatalf("learning status = %d, want uniform 404 (body %s)", rec.Code, rec.Body.String())
				}
				if !strings.Contains(rec.Body.String(), `"code":"NOT_FOUND"`) {
					t.Errorf("learning denial body lacks NOT_FOUND code: %s", rec.Body.String())
				}
				assertDenyLogged(t, buf, "PASSWORD_CHANGE_REQUIRED")
				return
			}
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

// TestRestrictedPrincipalReachesOnlyThePasswordChangeRoute is the other half of
// the test above, and the one the launch defect needed.
//
// Proving that a CHANGE_REQUIRED Administrator is refused everywhere is only
// half a policy. §4.5 grants it PASSWORD_CHANGE precisely so the restriction is
// escapable, and before this route existed it was not: the bootstrap
// Administrator authenticated, was refused every screen, and had nowhere to go.
// So the same principal, on the same router, must reach this one route.
func TestRestrictedPrincipalReachesOnlyThePasswordChangeRoute(t *testing.T) {
	bootstrapAdmin := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleAdmin,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialChangeRequired,
	}

	r, buf := authzRouter(t, fixedPrincipals{principal: bootstrapAdmin})
	rec := do(r, newAuthenticatedRequest(
		http.MethodPost, "/api/v1/password-changes",
		[]byte(`{"current_password":"the temporary one","new_password":"a replacement passphrase 9"}`),
	))

	if rec.Code == http.StatusForbidden {
		t.Fatalf("the one route that clears CHANGE_REQUIRED was refused to a "+
			"CHANGE_REQUIRED principal, which makes the state terminal: %s", rec.Body.String())
	}
	if strings.Contains(buf.String(), string(identity.DenyPasswordChangeRequired)) {
		t.Errorf("the password-change route logged a PASSWORD_CHANGE_REQUIRED denial: %s", buf.String())
	}

	// And the grant is specific, not a hole: the same principal on the same
	// router is still refused an ordinary Admin route.
	adminRec := do(r, newAuthenticatedRequest(http.MethodGet, "/api/v1/staff-invitations", nil))
	if adminRec.Code != http.StatusForbidden {
		t.Fatalf("restricted Admin reached an ADMIN_OPERATIONS route: status %d", adminRec.Code)
	}
}

// The route must not become an unauthenticated password-reset surface. It
// changes a credential, so a caller that cannot prove a session gets nothing —
// checked before any principal is resolved or any body is trusted.
func TestPasswordChangeRouteRefusesAnUnauthenticatedCaller(t *testing.T) {
	r, _ := authzRouter(t, fixedPrincipals{err: identity.ErrPrincipalNotFound})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-changes",
		bytes.NewReader([]byte(`{"current_password":"a","new_password":"b"}`)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://gradex.example")

	rec := do(r, request)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a request carrying no session", rec.Code)
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
	req := newAuthenticatedRequest(http.MethodGet, "/api/v1/admin/review/queue", nil)
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

			if isProtectedLearningRoute(route.path) {
				if rec.Code != http.StatusNotFound {
					t.Fatalf("learning status = %d, want uniform 404", rec.Code)
				}
				assertDenyLogged(t, buf, "ACCOUNT_SUSPENDED")
				return
			}
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

// TestStaffInvitationRoutesDenyInstructorAndStudent proves the staff invitation
// surface stays an ADMIN_OPERATIONS capability for every role that is not Admin.
// It is asserted separately from the catalog sweep because these routes are now
// composed in development on the environment alone, independently of Student
// registration, so nothing about the Student admission gate limits who reaches
// them.
func TestStaffInvitationRoutesDenyInstructorAndStudent(t *testing.T) {
	routes := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"list-invitations", http.MethodGet, "/api/v1/staff-invitations", nil},
		{
			"create-invitation", http.MethodPost, "/api/v1/staff-invitations",
			[]byte(`{"email":"staff@example.com","role":"INSTRUCTOR"}`),
		},
		{"revoke-invitation", http.MethodDelete, "/api/v1/staff-invitations/inv-99", nil},
	}

	for _, role := range []identity.Role{identity.RoleInstructor, identity.RoleStudent} {
		principal := identity.Principal{
			AccountID:       "11111111-1111-1111-1111-111111111111",
			Role:            role,
			Status:          identity.StatusActive,
			CredentialState: identity.CredentialActive,
		}
		for _, route := range routes {
			t.Run(string(role)+"_"+route.name, func(t *testing.T) {
				r, _ := authzRouter(t, fixedPrincipals{principal: principal})
				rec := do(r, newAuthenticatedRequest(route.method, route.path, route.body))
				if rec.Code != http.StatusForbidden {
					t.Fatalf("%s status = %d, want 403 (body %s)", role, rec.Code, rec.Body.String())
				}
			})
		}
	}
}

func TestUnknownPrincipalIsDenied(t *testing.T) {
	r, buf := authzRouter(t, fixedPrincipals{err: identity.ErrPrincipalNotFound})

	req := newAuthenticatedRequest(http.MethodGet, "/api/v1/admin/review/queue", nil)
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

	req := newAuthenticatedRequest(http.MethodGet, "/api/v1/admin/review/queue", nil)
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

	if _, err := NewRouter(cfg, logger, reporter, fakeAuth{}, nil); err == nil {
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

func TestSubmitRouteIsMountedAndProtected(t *testing.T) {
	instructor := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleInstructor,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}

	r, _ := authzRouter(t, fixedPrincipals{principal: instructor})
	req := newAuthenticatedRequest(http.MethodPost, "/api/v1/courses/course-99/revisions/rev-99/submit", []byte(`{}`))
	rec := do(r, req)

	// Since fakeOwnershipChecker returns false for isOwner, status must be 403 Forbidden
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /api/v1/courses/course-99/revisions/rev-99/submit returned status %d, want 403 (ownership enforcement)", rec.Code)
	}
}
