package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type fakeDeviceCommands struct {
	overviewResult      identity.DeviceOverview
	overviewErr         error
	completeTrustResult identity.DeviceTrustResult
	completeTrustErr    error
	resendResult        identity.DeviceChallenge
	resendErr           error
	adoptResult         identity.DeviceAdmissionResult
	adoptErr            error
	removeErr           error

	adminOverviewResult identity.AdminDeviceOverview
	adminOverviewErr    error
	adminRevokeErr      error
	adminRevokeAllCount int
	adminRevokeAllErr   error
	adminResetErr       error

	removeCalls int
	removedIDs  []string
}

func (f *fakeDeviceCommands) Overview(_ context.Context, _, _ string, _ time.Time) (identity.DeviceOverview, error) {
	return f.overviewResult, f.overviewErr
}

func (f *fakeDeviceCommands) CompleteTrust(_ context.Context, _ identity.DeviceTrustRequest) (identity.DeviceTrustResult, error) {
	return f.completeTrustResult, f.completeTrustErr
}

func (f *fakeDeviceCommands) ResendForAccount(_ context.Context, _, _, _ string) (identity.DeviceChallenge, error) {
	return f.resendResult, f.resendErr
}

func (f *fakeDeviceCommands) AdoptForSession(_ context.Context, _, _ string, _ identity.DeviceContext, _ string) (identity.DeviceAdmissionResult, error) {
	return f.adoptResult, f.adoptErr
}

func (f *fakeDeviceCommands) Remove(_ context.Context, request identity.RemoveRequest) error {
	f.removeCalls++
	f.removedIDs = append(f.removedIDs, request.DeviceID)
	return f.removeErr
}

func (f *fakeDeviceCommands) AdminOverview(_ context.Context, _ string, _ time.Time) (identity.AdminDeviceOverview, error) {
	return f.adminOverviewResult, f.adminOverviewErr
}

func (f *fakeDeviceCommands) AdminRevokeDevice(_ context.Context, _ identity.AdminDeviceCommand) error {
	return f.adminRevokeErr
}

func (f *fakeDeviceCommands) AdminRevokeAllDevices(_ context.Context, _ identity.AdminDeviceCommand) (int, error) {
	return f.adminRevokeAllCount, f.adminRevokeAllErr
}

func (f *fakeDeviceCommands) AdminResetReplacementCooldown(_ context.Context, _ identity.AdminDeviceCommand) error {
	return f.adminResetErr
}

type configurableTestAuth struct {
	userID     string
	trustState identity.SessionDeviceTrust
	deviceID   string
}

func (a configurableTestAuth) UserFromRequest(c *gin.Context) (string, error) {
	if a.userID == "" {
		return "", errors.New("missing session cookie")
	}
	now := time.Now().UTC()
	c.Set("authenticated_session", identity.Session{
		ID:                "test-session-id",
		AccountID:         a.userID,
		State:             identity.SessionActive,
		AuthenticatedAt:   now,
		IdleExpiresAt:     now.Add(24 * time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	})
	c.Set(auth.SessionDeviceTrustKey, a.trustState)
	if a.deviceID != "" {
		c.Set(auth.SessionTrustedDeviceKey, a.deviceID)
	}
	return a.userID, nil
}

func mountedDeviceTestRouter(
	t *testing.T,
	devices *fakeDeviceCommands,
	authenticator auth.Authenticator,
	principal identity.Principal,
) *gin.Engine {
	t.Helper()
	foundation, err := NewDeviceFoundation(devices, time.Now)
	if err != nil {
		t.Fatalf("constructing device foundation: %v", err)
	}

	limiter, err := ratelimit.New(admissionRateStore{allowed: true}, bytes.Repeat([]byte{0x31}, 32), time.Second)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	sessionFoundation, err := NewSessionFoundation(SessionFoundationOptions{
		PublicOrigin:        "https://gradex.example",
		CookieSigningKey:    bytes.Repeat([]byte("a"), 32),
		AnonymousCSRFKey:    bytes.Repeat([]byte("b"), 32),
		AnonymousSessionTTL: time.Hour,
		Repository:          &fakeSessionRepository{},
		Compromised:         testCompromisedSource(t),
		Limiter:             limiter,
		EndpointPolicies:    testSessionEndpointPolicies(),
	})
	if err != nil {
		t.Fatalf("constructing session foundation: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestIDMiddleware())
	v1 := router.Group("/api/v1")
	logger := logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info"))

	err = mountDeviceRoutes(
		v1,
		foundation,
		sessionFoundation,
		authenticator,
		fixedPrincipals{principal: principal},
		logger,
	)
	if err != nil {
		t.Fatalf("mounting device routes: %v", err)
	}
	return router
}

func validOpaqueCookie() string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))
}

func validCSRFHeader() string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
}

func TestPendingStudentCanInspectOverviewButCannotStandaloneRemove(t *testing.T) {
	devices := &fakeDeviceCommands{
		overviewResult: identity.DeviceOverview{
			Devices: []identity.DeviceSummary{
				{ID: "dev-a", Label: "Device A", BrowserFamily: "Chrome", PlatformFamily: "Linux"},
				{ID: "dev-b", Label: "Device B", BrowserFamily: "Firefox", PlatformFamily: "macOS"},
			},
			DeviceLimit:      2,
			ReplacementReady: true,
		},
	}
	studentPrincipal := identity.Principal{
		AccountID:       "student-account-1",
		Role:            identity.RoleStudent,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}
	pendingAuth := configurableTestAuth{
		userID:     "student-account-1",
		trustState: identity.DeviceTrustPending,
		deviceID:   "", // pending browser has no established device ID yet
	}

	router := mountedDeviceTestRouter(t, devices, pendingAuth, studentPrincipal)

	// 1. Pending browser GET /api/v1/me/devices -> 200 OK with coarse overview
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/me/devices", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("pending browser GET /me/devices = %d, want 200: %s", getRec.Code, getRec.Body.String())
	}
	var overview identity.DeviceOverview
	if err := json.Unmarshal(getRec.Body.Bytes(), &overview); err != nil {
		t.Fatalf("unmarshaling overview: %v", err)
	}
	if len(overview.Devices) != 2 {
		t.Fatalf("overview devices count = %d, want 2", len(overview.Devices))
	}

	// 2. Pending browser DELETE /api/v1/me/devices/dev-b -> 403 Forbidden with NOT_AUTHORIZED
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/me/devices/dev-b", nil)
	delReq.Header.Set("Origin", "https://gradex.example")
	delReq.Header.Set("X-CSRF-Token", validCSRFHeader())
	delReq.AddCookie(&http.Cookie{
		Name:  auth.SessionCookieName,
		Value: validOpaqueCookie(),
	})
	delRec := httptest.NewRecorder()
	router.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusForbidden {
		t.Fatalf("pending browser DELETE /me/devices/dev-b = %d, want 403: %s", delRec.Code, delRec.Body.String())
	}
	var problemBody struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(delRec.Body.Bytes(), &problemBody); err != nil {
		t.Fatalf("unmarshaling problem: %v", err)
	}
	if problemBody.Code != "NOT_AUTHORIZED" {
		t.Fatalf("problem code = %q, want NOT_AUTHORIZED", problemBody.Code)
	}
	if devices.removeCalls != 0 {
		t.Fatalf("Remove() was called %d times by a pending browser, want 0", devices.removeCalls)
	}
}

func TestTrustedStudentCanRemoveDevice(t *testing.T) {
	devices := &fakeDeviceCommands{
		overviewResult: identity.DeviceOverview{
			Devices: []identity.DeviceSummary{
				{ID: "dev-a", Label: "Device A", BrowserFamily: "Chrome", PlatformFamily: "Linux"},
				{ID: "dev-b", Label: "Device B", BrowserFamily: "Firefox", PlatformFamily: "macOS"},
			},
			DeviceLimit:      2,
			ReplacementReady: true,
		},
	}
	studentPrincipal := identity.Principal{
		AccountID:       "student-account-1",
		Role:            identity.RoleStudent,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}
	trustedAuth := configurableTestAuth{
		userID:     "student-account-1",
		trustState: identity.DeviceTrustEstablished,
		deviceID:   "dev-a",
	}

	router := mountedDeviceTestRouter(t, devices, trustedAuth, studentPrincipal)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/me/devices/dev-b", nil)
	delReq.Header.Set("Origin", "https://gradex.example")
	delReq.Header.Set("X-CSRF-Token", validCSRFHeader())
	delReq.AddCookie(&http.Cookie{
		Name:  auth.SessionCookieName,
		Value: validOpaqueCookie(),
	})
	delRec := httptest.NewRecorder()
	router.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("trusted browser DELETE /me/devices/dev-b = %d, want 200: %s", delRec.Code, delRec.Body.String())
	}
	if devices.removeCalls != 1 {
		t.Fatalf("Remove() calls = %d, want 1", devices.removeCalls)
	}
	if len(devices.removedIDs) != 1 || devices.removedIDs[0] != "dev-b" {
		t.Fatalf("removed IDs = %v, want ['dev-b']", devices.removedIDs)
	}
}
