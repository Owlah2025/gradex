package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

// FakeAuthenticator trusts a plain header instead of validating a real JWT.
// Dev/test only — deliberately not shaped like a JWT so nobody mistakes it
// for real auth. Swap for a real implementation of Authenticator later; every
// caller of this interface is unaffected.
type FakeAuthenticator struct{}

func NewFakeAuthenticator() *FakeAuthenticator { return &FakeAuthenticator{} }

func (f *FakeAuthenticator) UserFromRequest(c *gin.Context) (string, error) {
	userID := c.GetHeader("X-Debug-User-ID")
	if userID == "" {
		return "", fmt.Errorf("missing X-Debug-User-ID header (fake auth mode)")
	}
	now := time.Now().UTC()
	c.Set("authenticated_session", identity.Session{
		ID:                "fake-session-id",
		AccountID:         userID,
		State:             identity.SessionActive,
		AuthenticatedAt:   now,
		IdleExpiresAt:     now.Add(24 * time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	})
	// The fake stands in for a fully established session, which in production
	// means a browser that has already completed device trust. Leaving the
	// device state unset would instead make every fake-authenticated request
	// fail closed on device policy, which tests something the fake was never
	// meant to model.
	c.Set(SessionDeviceTrustKey, identity.DeviceTrustEstablished)
	c.Set(SessionTrustedDeviceKey, fakeTrustedDeviceID)
	return userID, nil
}

// fakeTrustedDeviceID is a fixed, obviously synthetic device identity. It is
// never written to the database and exists only so fake-authenticated requests
// carry the same shape a real trusted session does.
const fakeTrustedDeviceID = "00000000-0000-4000-8000-0000000000fa"

// FakeEntitlementChecker reads from the fake_entitlements table seeded by
// scripts/seed.sql, so state survives process restarts and is inspectable
// via psql. Swap for a real implementation backed by purchase/enrollment
// records later; every caller of this interface is unaffected.
type FakeEntitlementChecker struct {
	db *pgxpool.Pool
}

func NewFakeEntitlementChecker(db *pgxpool.Pool) *FakeEntitlementChecker {
	return &FakeEntitlementChecker{db: db}
}

func (f *FakeEntitlementChecker) HasAccess(ctx context.Context, userID, lessonID string) (bool, error) {
	return f.hasRole(ctx, userID, lessonID, "student")
}

func (f *FakeEntitlementChecker) IsInstructorForLesson(ctx context.Context, userID, lessonID string) (bool, error) {
	return f.hasRole(ctx, userID, lessonID, "instructor")
}

func (f *FakeEntitlementChecker) hasRole(ctx context.Context, userID, lessonID, role string) (bool, error) {
	var exists bool
	err := f.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM fake_entitlements WHERE user_id = $1::uuid AND lesson_id = $2::uuid AND role = $3)`,
		userID, lessonID, role,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking fake entitlement: %w", err)
	}
	return exists, nil
}
