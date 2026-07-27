package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
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

	staffLimiter, _ := ratelimit.New(fakeRateStore{}, bytes.Repeat([]byte{0x31}, 32), time.Second)
	staffPolicies := map[string]ratelimit.Policy{
		"staff-invitations-create":   ratelimit.DevelopmentStaffInvitationPolicy("staff-invitations-create"),
		"staff-invitations-preview":  ratelimit.DevelopmentStaffInvitationPolicy("staff-invitations-preview"),
		"staff-invitations-complete": ratelimit.DevelopmentStaffInvitationPolicy("staff-invitations-complete"),
	}
	staffFoundation, err := NewStaffFoundation(StaffFoundationOptions{
		Service:          fakeStaffService{},
		Limiter:          staffLimiter,
		EndpointPolicies: staffPolicies,
	})
	if err != nil {
		t.Fatalf("constructing staff foundation: %v", err)
	}

	// Entitlements allow everything, so nothing downstream of the capability
	// policy can be the reason a request is refused. If a call is denied here,
	// the policy denied it.
	r, err := NewRouter(cfg, logger, reporter, fakeService{}, fakeAuth{},
		fakeEntitlements{allowed: true}, principals,
		WithStaffFoundation(staffFoundation),
	)
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	return r, buf
}

type fakeRateStore struct{}

func (fakeRateStore) Decide(context.Context, []ratelimit.Entry) (bool, error) {
	return true, nil
}

type fakeStaffService struct{}

func (fakeStaffService) CreateStaffInvitation(context.Context, identity.CreateStaffInvitationRequest) (identity.IssuedStaffInvitation, error) {
	return identity.IssuedStaffInvitation{}, nil
}
func (fakeStaffService) PreviewStaffInvitation(context.Context, string, time.Time) (identity.StaffInvitationPreview, error) {
	return identity.StaffInvitationPreview{}, nil
}
func (fakeStaffService) CompleteStaffInvitation(context.Context, identity.CompleteStaffInvitationRequest) (identity.CompleteStaffInvitationResult, error) {
	return identity.CompleteStaffInvitationResult{}, nil
}
func (fakeStaffService) RevokeStaffInvitation(context.Context, identity.RevokeStaffInvitationRequest) error {
	return nil
}
func (fakeStaffService) SuspendAccount(context.Context, identity.SuspendAccountRequest) (identity.SuspendAccountResult, error) {
	return identity.SuspendAccountResult{}, nil
}
func (fakeStaffService) ReinstateAccount(context.Context, identity.ReinstateAccountRequest) (identity.ReinstateAccountResult, error) {
	return identity.ReinstateAccountResult{}, nil
}
func (fakeStaffService) ListPendingInvitations(context.Context, identity.Principal) ([]identity.StaffInvitation, error) {
	return nil, nil
}

// protectedRoutes is every route that requires a capability decision. Bootstrap
// close condition 3 asserts against these — real routes running the real
// middleware and policy chain — not against Authorize() in isolation.
var protectedRoutes = []struct{ method, path string }{
	{http.MethodPost, "/api/v1/lessons/lesson-99/video/upload-url"},
	{http.MethodPost, "/api/v1/lessons/lesson-99/video/complete"},
	{http.MethodPost, "/api/v1/lessons/lesson-99/video/retry"},
	{http.MethodPost, "/api/v1/lessons/lesson-99/video/publish"},
	{http.MethodGet, "/api/v1/lessons/lesson-99/video/playback-url"},
	{http.MethodPost, "/api/v1/lessons/lesson-99/progress"},
	{http.MethodPost, "/api/v1/staff/invitations"},
	{http.MethodGet, "/api/v1/staff/invitations"},
	{http.MethodPost, "/api/v1/staff/invitations/inv-99/revoke"},
	{http.MethodPost, "/api/v1/staff/acct-99/suspend"},
	{http.MethodPost, "/api/v1/staff/acct-99/reinstate"},
}

// Bootstrap close condition 3, initial end-to-end denial evidence.
//
// The restricted bootstrap Administrator authenticates successfully and is then
// refused every protected route that exists today. This is the *initial*
// evidence: the full protected Identity and staff surface does not exist until
// S1C, and this assertion is rerun and expanded there. Passing here proves the
// deny path works through the real chain; it does not prove coverage.
func TestRestrictedBootstrapAdminIsDeniedOnRealProtectedRoutes(t *testing.T) {
	bootstrapAdmin := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleAdmin,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialChangeRequired,
	}

	if len(protectedRoutes) == 0 {
		t.Fatal("no protected routes to assert against; this test would pass vacuously")
	}

	for _, route := range protectedRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			r, buf := authzRouter(t, fixedPrincipals{principal: bootstrapAdmin})

			rec := do(r, httptest.NewRequest(route.method, route.path, nil))

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
			}

			// The refusal is uniform. §5 forbids a response that distinguishes
			// "must change password" from "wrong role" from "no such Account".
			p := assertProblemEnvelope(t, rec)
			if p.Code != "NOT_AUTHORIZED" {
				t.Errorf("code = %q, want NOT_AUTHORIZED", p.Code)
			}
			body := rec.Body.String()
			for _, leak := range []string{
				"PASSWORD_CHANGE_REQUIRED", "CHANGE_REQUIRED", "bootstrap",
				"ADMIN", "suspended", "credential",
			} {
				if strings.Contains(body, leak) {
					t.Errorf("response leaked policy state %q: %s", leak, body)
				}
			}

			// The typed reason belongs in security monitoring instead (§6.1).
			assertDenyLogged(t, buf, "PASSWORD_CHANGE_REQUIRED")
		})
	}
}

// The same Account, one state change later, is allowed. Without this the test
// above would pass against a policy that simply denies everything.
func TestUnrestrictedAdminReachesContentManagementRoutes(t *testing.T) {
	admin := identity.Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            identity.RoleAdmin,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}

	r, _ := authzRouter(t, fixedPrincipals{principal: admin})
	rec := do(r, httptest.NewRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/publish", nil))

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

	for _, route := range protectedRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			r, buf := authzRouter(t, fixedPrincipals{principal: suspended})
			rec := do(r, httptest.NewRequest(route.method, route.path, nil))

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			assertDenyLogged(t, buf, "ACCOUNT_SUSPENDED")
		})
	}
}

// An authenticated identifier with no Account is refused, not admitted. This is
// the fail-closed half of deny-by-default: authentication is not authorization.
func TestUnknownPrincipalIsDenied(t *testing.T) {
	r, buf := authzRouter(t, fixedPrincipals{err: identity.ErrPrincipalNotFound})

	rec := do(r, httptest.NewRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/publish", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	assertDenyLogged(t, buf, "PRINCIPAL_NOT_FOUND")
}

// A resolution failure is a fault, not a denial. Counting it as a refusal would
// make a database outage look like an authorization event.
func TestPrincipalResolutionFailureIsAFaultNotADenial(t *testing.T) {
	r, buf := authzRouter(t, fixedPrincipals{
		err: errors.New(`pq: duplicate key value violates unique constraint "videos_lesson_id_key"`),
	})

	rec := do(r, httptest.NewRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/publish", nil))

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

// assertDenyLogged checks the typed reason reached security monitoring, and
// that the log line carries no Account identifier.
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
		// Correlation happens through the request ID and Audit. A log shipped
		// to a monitoring provider must not become a directory of who was
		// refused what.
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

// The policy runs before ownership and Entitlement checks. If it ran after, a
// restricted principal's refusal would depend on an unrelated lookup happening
// to fail first — and would silently start passing whenever that lookup
// succeeded.
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

	// The entitlement checker fails loudly if it is ever consulted.
	var entitlementConsulted bool
	tripwire := trippingEntitlements{consulted: &entitlementConsulted}

	r, err := NewRouter(cfg, logger, reporter, fakeService{}, fakeAuth{}, tripwire,
		fixedPrincipals{principal: restricted})
	if err != nil {
		t.Fatalf("router: %v", err)
	}

	rec := do(r, httptest.NewRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/publish", nil))

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

// A router without a principal resolver must not be constructible. Otherwise a
// future call site could ship protected routes with no capability decision by
// omitting an argument.
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

var _ = video.Service(fakeService{})
