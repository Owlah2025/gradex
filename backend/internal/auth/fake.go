package auth

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
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
	return userID, nil
}

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
