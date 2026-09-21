package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckEnhancementRecoveryRollbackSafety is the preflight for a rollback that
// would cross schema 41 back to 40.
//
// `0041_enhancement_recovery_foundation.down.sql` already refuses this case, and
// that refusal stays: it is what protects correctness when some other runner
// executes the migration directly. But golang-migrate marks the target version
// dirty BEFORE it executes the migration body, so letting the refusal happen
// inside the SQL turns an expected safety decision into operational damage —
// the evidence survives, and the schema marker is left dirty, which then blocks
// every subsequent migration command until an operator clears it by hand.
//
// Running the same question here, on a separate connection before the step
// begins, keeps the marker clean. It mirrors CheckThumbnailRollbackSafety and
// CheckManualPurchaseRollbackSafety, which exist for exactly this reason.
//
// THE BOUNDARY: schema 41 may be downgraded to schema 40 only while every row
// in processing_attempts is attempt_kind = FULL. Once any ENHANCEMENT or
// FINALIZATION attempt row is persisted — regardless of its outcome, FAILED
// included — the schema-40 rollback floor is closed.
//
// A FAILED ENHANCEMENT row is refused even though schema 40's restored
// `processing_attempt_result_coherent` would accept its remaining columns.
// Schema 40 has no attempt_kind at all, so dropping the column would silently
// reinterpret that row as a legacy whole-ladder attempt: the row would survive
// while the claim it makes about what the worker was asked to do would change.
// Losing that distinction is not a lesser failure than losing the row.
func CheckEnhancementRecoveryRollbackSafety(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("database pool is required")
	}
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM processing_attempts WHERE attempt_kind <> 'FULL')
	`).Scan(&exists); err != nil {
		return fmt.Errorf("checking enhancement recovery rollback safety: %w", err)
	}
	if exists {
		return errors.New("enhancement or finalization processing attempts exist; schema 40 has no attempt kind and would record them as whole-ladder attempts. Retain schema 41 and use an application build compatible with that schema")
	}
	return nil
}
