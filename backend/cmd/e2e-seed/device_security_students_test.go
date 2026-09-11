//go:build !production

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	deviceSecurityTestSlots = 5
	deviceSecurityPoolSize  = deviceSecurityTestSlots * rotatingMaxRepeats
)

func deviceSecurityStudentID(index int) string {
	return fmt.Sprintf("ad000000-0000-0000-0000-%012d", index)
}

func deviceSecurityStudentEmail(index int) string {
	return fmt.Sprintf("student-device-security-%03d@example.test", index)
}

// seedDeviceSecurityStudents keeps device-test capacity outside the rotating
// Student pool. Its deliberately old invitation timestamps keep these records
// out of the Admin queue's newest-100 window, so adding device journeys cannot
// reorder or hide unrelated T8A fixtures.
func seedDeviceSecurityStudents(
	ctx context.Context,
	tx pgx.Tx,
	courseID string,
	passwordHash string,
	now time.Time,
	accessEndsAt time.Time,
) error {
	for index := 0; index < deviceSecurityPoolSize; index++ {
		accountID := deviceSecurityStudentID(index)
		email := deviceSecurityStudentEmail(index)
		if _, err := tx.Exec(ctx, `
			INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
			VALUES ($1, $2, $2, 'STUDENT', 'ACTIVE', $3, $4)
		`, accountID, email, fmt.Sprintf("Device Security Student %03d", index), now); err != nil {
			return fmt.Errorf("insert device security student %d: %w", index, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO password_credentials (account_id, password_hash, state)
			VALUES ($1, $2, 'ACTIVE')
		`, accountID, passwordHash); err != nil {
			return fmt.Errorf("insert device security credentials %d: %w", index, err)
		}

		invitationID := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_access_invitations
			  (id, course_id, email, normalized_email, created_by_account_id,
			   accepted_by_account_id, decided_by_account_id, state, created_at)
			VALUES ($1, $2, $4, $4, $3, $3, $3, 'APPROVED', $5)
		`, invitationID, courseID, accountID, email, now.Add(-7*24*time.Hour-time.Duration(index)*time.Second)); err != nil {
			return fmt.Errorf("insert device security invitation %d: %w", index, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO entitlements
			  (id, student_account_id, scope_kind, scope_id, course_id, grant_source,
			   source_invitation_id, original_access_ends_at, access_ends_at,
			   retirement_eligibility_at, state)
			VALUES ($1, $2, 'COURSE', $3, $3, 'MANUAL_INVITATION', $4, $5, $5, $5, 'ACTIVE')
		`, fmt.Sprintf("ed000000-0000-0000-0000-%012d", index), accountID, courseID, invitationID, accessEndsAt); err != nil {
			return fmt.Errorf("insert device security entitlement %d: %w", index, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO enrollments (id, student_account_id, course_id)
			VALUES ($1, $2, $3)
		`, fmt.Sprintf("bd000000-0000-0000-0000-%012d", index), accountID, courseID); err != nil {
			return fmt.Errorf("insert device security enrollment %d: %w", index, err)
		}
	}
	return nil
}
