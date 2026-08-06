//go:build !production

package entitlement

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedEvaluationRecord creates a real schema-conforming fixture for S4 tests.
// It is intentionally unexported and absent from production builds. S6 remains
// the only production grant producer.
func seedEvaluationRecord(ctx context.Context, pool *pgxpool.Pool, record Record) error {
	if pool == nil || record.ID == "" || record.StudentAccountID == "" || record.ScopeID == "" ||
		record.CourseID == "" || !record.ScopeKind.Valid() || record.GrantSource != GrantSourceManualInvitation ||
		record.OriginalAccessEndsAt.IsZero() || record.AccessEndsAt.IsZero() || record.RetirementEligibilityAt.IsZero() ||
		!record.State.Valid() || record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		return errors.New("complete valid non-production entitlement fixture is required")
	}
	invID := stringValue(record.SourceInvitationID)
	if record.GrantSource == GrantSourceManualInvitation {
		if invID == "" {
			invID = record.ID
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO course_access_invitations (
				id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state, created_at, updated_at
			) VALUES ($1::uuid, $2::uuid, 'fixture-student@example.com', 'fixture-student@example.com', $3::uuid, $3::uuid, $3::uuid, 'APPROVED', $4, $4)
			ON CONFLICT (id) DO NOTHING
		`, invID, record.CourseID, record.StudentAccountID, record.CreatedAt)
		if err != nil {
			return fmt.Errorf("seeding invitation fixture: %w", err)
		}
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO entitlements (
			id, student_account_id, scope_kind, scope_id, course_id, grant_source,
			source_invitation_id, original_access_ends_at, access_ends_at, revoked_at,
			retirement_eligibility_at, state, created_at, updated_at
		) VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::uuid, $6, NULLIF($7, '')::uuid,
			$8, $9, $10, $11, $12, $13, $14)
	`, record.ID, record.StudentAccountID, record.ScopeKind, record.ScopeID, record.CourseID,
		record.GrantSource, invID, record.OriginalAccessEndsAt,
		record.AccessEndsAt, record.RevokedAt, record.RetirementEligibilityAt, record.State,
		record.CreatedAt, record.UpdatedAt)
	if err != nil {
		return fmt.Errorf("seeding non-production entitlement fixture: %w", err)
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
