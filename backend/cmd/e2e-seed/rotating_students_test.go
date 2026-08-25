//go:build !production

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Rate-limit-safe rotating Student fixtures.
//
// Production rate limits are part of what the E2E suite exists to respect, so they are never
// weakened, widened, or bypassed for a test. The limits that bind a repeated Lesson Player run
// are per Student:
//
//	playback issuance   30 per 10 minutes per Student
//	Progress writes     12 per minute per (Student, stable Lesson)
//
// A single Student cannot carry a repeated suite: ten playback-issuing executions per repeat,
// five repeats, all inside one ten-minute window, is fifty issuances against a budget of thirty.
// The fix is more Students, not a larger budget — each test execution is handed its own Student
// whose budgets no other execution touches.
//
// Sizing. Every execution that authenticates as a Student gets its own Student, not only the
// ones that issue playback: session resolution is itself bounded per Student at 30 per minute,
// and a repeated suite runs its executions back to back.
//
//	active-Student test slots per repeat
//	  Lesson Player matrix (4 viewports x 2 locales)   8
//	  generic-unavailable matrix (4 x 2)               8
//	  Progress persistence                             1
//	  media and lifecycle cleanup                      1
//	  rendered viewport evidence (4 viewports)         4
//	                                                  --
//	                                                  22
//	greatest supported repeat count                    10
//	required distinct active Students        22 x 10 = 220
//	provisioned                                       240 (9% headroom)
//
//	expired-Student test slots per repeat (4 x 2)       8
//	required distinct expired Students        8 x 10 = 80
//	provisioned                                       100 (25% headroom)
//
// T3 adds six Student academic profile slots (24-29). Allocation is
// slot*repeats + repeat, so slots 0-23 keep indices 0-239 unchanged and the new
// slots occupy 240-299; no existing execution is reassigned.
//
// T8A / MVP-F24A adds four Entitlement lifecycle slots (30-33), one per BR-026
// operation, so no case observes another's mutated Entitlement:
//
//	30  expiry extension
//	31  expiry shortening while access stays open
//	32  expiry moved into the past, ending access immediately
//	33  revocation
//
// By the same allocation identity slots 0-29 keep indices 0-299 unchanged and the
// four new slots occupy 300-339.
// AD-14 adds one report-resolution Student slot (34), occupying index 340-349.
//
// Per-Student consumption stays far inside the production limits rather than near them: at most
// two playback issuances of the thirty allowed per ten minutes, and at most four Progress writes
// of the twelve allowed per minute for a Lesson.
const (
	// rotatingStudentPoolSize is the provisioned active pool. It must be at least
	// rotatingTestSlots * rotatingMaxRepeats.
	rotatingStudentPoolSize = 350
	// rotatingTestSlots is the number of registered active-Student test identities.
	rotatingTestSlots = 35
	// rotatingMaxRepeats is the greatest `--repeat-each` the pool supports.
	rotatingMaxRepeats = 10

	// rotatingExpiredPoolSize is the provisioned expired pool.
	rotatingExpiredPoolSize = 100
	// rotatingExpiredSlots is the number of registered expired-Student test identities.
	rotatingExpiredSlots = 8

	// The Admin Course Access queue reads `ORDER BY created_at DESC LIMIT 100`. Left at the
	// column default every pool Invitation would carry the single transaction timestamp, so
	// which hundred the Admin sees would be decided arbitrarily by the plan rather than by the
	// fixture — and the highest active slots, which the T8A Entitlement lifecycle journey owns,
	// would normally fall outside the page. Each Invitation therefore gets an explicit
	// created_at derived from its pool index: strictly in the past, strictly ordered, highest
	// index newest. The two pools occupy disjoint windows so their orderings never interleave.
	//
	// rotatingActiveQueueOffsetSeconds is how far behind the seed instant the newest active
	// Invitation sits.
	rotatingActiveQueueOffsetSeconds = 60
	// rotatingExpiredQueueOffsetSeconds is the same for the expired pool, chosen so every
	// expired Invitation is older than every active one.
	rotatingExpiredQueueOffsetSeconds = 600
)

// rotatingInvitationCreatedAt places pool slot `index` in the Admin queue's ordering. Higher
// indices are newer, so growing a pool never reorders the rows already provisioned below it.
func rotatingInvitationCreatedAt(now time.Time, index, poolSize, offsetSeconds int) time.Time {
	return now.Add(-time.Duration(offsetSeconds+(poolSize-1-index)) * time.Second)
}

// rotatingStudentID is the deterministic account identifier for pool slot `index`. Identifiers
// and credentials are generated for the isolated per-run database only and exist nowhere else.
func rotatingStudentID(index int) string {
	return fmt.Sprintf("a1000000-0000-0000-0000-%012d", index)
}

func rotatingEnrollmentID(index int) string {
	return fmt.Sprintf("b1000000-0000-0000-0000-%012d", index)
}

func rotatingEntitlementID(index int) string {
	return fmt.Sprintf("e1000000-0000-0000-0000-%012d", index)
}

func rotatingStudentEmail(index int) string {
	return fmt.Sprintf("student-rotating-%03d@example.test", index)
}

// The expired pool uses its own identifier space so an expired fixture can never be mistaken for
// an active one.
func rotatingExpiredStudentID(index int) string {
	return fmt.Sprintf("a2000000-0000-0000-0000-%012d", index)
}

func rotatingExpiredEnrollmentID(index int) string {
	return fmt.Sprintf("b2000000-0000-0000-0000-%012d", index)
}

func rotatingExpiredEntitlementID(index int) string {
	return fmt.Sprintf("e2000000-0000-0000-0000-%012d", index)
}

func rotatingExpiredStudentEmail(index int) string {
	return fmt.Sprintf("student-rotating-expired-%03d@example.test", index)
}

// seedRotatingStudents creates the pool in one bounded loop. Every Student is an active Account
// with an Enrollment and an active Entitlement for the same deterministic Course, so each has the
// same access to the same Lessons and media as the shared Active Student — and no Progress row,
// so an execution begins from a known absent state it can assert.
//
// The Argon2id hash is computed once by the caller and reused: hashing is deliberately expensive,
// and every fixture Student shares one password that exists only in this throwaway database.
func seedRotatingStudents(
	ctx context.Context,
	tx pgx.Tx,
	courseID string,
	passwordHash string,
	now time.Time,
	accessEndsAt time.Time,
	expiredAccessEndsAt time.Time,
) error {
	if rotatingStudentPoolSize < rotatingTestSlots*rotatingMaxRepeats {
		return fmt.Errorf(
			"rotating pool of %d cannot serve %d test slots at %d repeats",
			rotatingStudentPoolSize, rotatingTestSlots, rotatingMaxRepeats,
		)
	}

	for index := 0; index < rotatingStudentPoolSize; index++ {
		accountID := rotatingStudentID(index)
		email := rotatingStudentEmail(index)

		if _, err := tx.Exec(ctx, `
			INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
			VALUES ($1, $2, $2, 'STUDENT', 'ACTIVE', $3, $4)
		`, accountID, email, fmt.Sprintf("Rotating Student %03d", index), now); err != nil {
			return fmt.Errorf("insert rotating student %d: %w", index, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO password_credentials (account_id, password_hash, state)
			VALUES ($1, $2, 'ACTIVE')
		`, accountID, passwordHash); err != nil {
			return fmt.Errorf("insert rotating student %d credentials: %w", index, err)
		}

		invID := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state, created_at)
			VALUES ($1, $2, $4, $4, $3, $3, $3, 'APPROVED', $5)
		`, invID, courseID, accountID, email,
			rotatingInvitationCreatedAt(now, index, rotatingStudentPoolSize, rotatingActiveQueueOffsetSeconds),
		); err != nil {
			return fmt.Errorf("insert rotating student %d invitation: %w", index, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
			VALUES ($1, $2, 'COURSE', $3, $3, 'MANUAL_INVITATION', $4, $5, $5, $5, 'ACTIVE')
		`, rotatingEntitlementID(index), accountID, courseID, invID, accessEndsAt); err != nil {
			return fmt.Errorf("insert rotating student %d entitlement: %w", index, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO enrollments (id, student_account_id, course_id)
			VALUES ($1, $2, $3)
		`, rotatingEnrollmentID(index), accountID, courseID); err != nil {
			return fmt.Errorf("insert rotating student %d enrollment: %w", index, err)
		}
	}

	return seedRotatingExpiredStudents(ctx, tx, courseID, passwordHash, now, expiredAccessEndsAt)
}

// seedRotatingExpiredStudents mirrors the active pool for the expired-access matrix: an active
// Account with a retained Enrollment and an Entitlement whose access has already ended. The
// Account itself stays active, because it is the Entitlement's expiry — not the Account — that
// the expired rendering state must be driven by.
func seedRotatingExpiredStudents(
	ctx context.Context,
	tx pgx.Tx,
	courseID string,
	passwordHash string,
	now time.Time,
	expiredAccessEndsAt time.Time,
) error {
	if rotatingExpiredPoolSize < rotatingExpiredSlots*rotatingMaxRepeats {
		return fmt.Errorf(
			"expired pool of %d cannot serve %d test slots at %d repeats",
			rotatingExpiredPoolSize, rotatingExpiredSlots, rotatingMaxRepeats,
		)
	}

	for index := 0; index < rotatingExpiredPoolSize; index++ {
		accountID := rotatingExpiredStudentID(index)
		email := rotatingExpiredStudentEmail(index)

		if _, err := tx.Exec(ctx, `
			INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
			VALUES ($1, $2, $2, 'STUDENT', 'ACTIVE', $3, $4)
		`, accountID, email, fmt.Sprintf("Rotating Expired Student %03d", index), now); err != nil {
			return fmt.Errorf("insert rotating expired student %d: %w", index, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO password_credentials (account_id, password_hash, state)
			VALUES ($1, $2, 'ACTIVE')
		`, accountID, passwordHash); err != nil {
			return fmt.Errorf("insert rotating expired student %d credentials: %w", index, err)
		}

		expiredInvID := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state, created_at)
			VALUES ($1, $2, $4, $4, $3, $3, $3, 'APPROVED', $5)
		`, expiredInvID, courseID, accountID, email,
			rotatingInvitationCreatedAt(now, index, rotatingExpiredPoolSize, rotatingExpiredQueueOffsetSeconds),
		); err != nil {
			return fmt.Errorf("insert rotating expired student %d invitation: %w", index, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
			VALUES ($1, $2, 'COURSE', $3, $3, 'MANUAL_INVITATION', $4, $5, $5, $5, 'ACTIVE')
		`, rotatingExpiredEntitlementID(index), accountID, courseID, expiredInvID, expiredAccessEndsAt); err != nil {
			return fmt.Errorf("insert rotating expired student %d entitlement: %w", index, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO enrollments (id, student_account_id, course_id)
			VALUES ($1, $2, $3)
		`, rotatingExpiredEnrollmentID(index), accountID, courseID); err != nil {
			return fmt.Errorf("insert rotating expired student %d enrollment: %w", index, err)
		}
	}

	return nil
}
