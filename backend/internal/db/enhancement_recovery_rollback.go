package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// CheckActiveProcessingKindRollbackSafety keeps a refused 42 -> 41 downgrade
// from dirtying golang-migrate's marker. Terminal progress tokens are allowed:
// only an in-flight processing operation needs the schema-42 kind column.
func CheckActiveProcessingKindRollbackSafety(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("database pool is required")
	}
	var active bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM media_asset_versions
			WHERE state='PROCESSING' OR (state='PLAYABLE' AND work_claim_token IS NOT NULL)
		)
	`).Scan(&active); err != nil {
		return fmt.Errorf("checking active processing rollback safety: %w", err)
	}
	if active {
		return errors.New("active processing operation requires schema 42; settle media work before rolling back to 41")
	}
	return nil
}

// ActiveMediaClaim is one Asset Version holding a live work claim at the moment
// the query ran.
type ActiveMediaClaim struct {
	AssetVersionID string
	State          string
}

// CheckNoActiveMediaClaims refuses a supervised schema rollback while any media
// work is claimed.
//
// It is a safety gate against operator error, NOT distributed synchronization.
// A producer that is still running can acquire a claim in the instant after this
// query returns, so the operational contract remains what it has always been:
// stop the API and worker before invoking a supervised rollback. This check
// catches the case where that step was skipped or only partly completed — which
// is the realistic failure, and the one that would otherwise let a live worker
// mutate processing evidence while the schema changes underneath it.
//
// PostgreSQL is the authority. The migration binary deliberately does not
// inspect processes: it cannot see a worker on another host, and a container
// that is up but idle is not the question being asked.
func CheckNoActiveMediaClaims(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("database pool is required")
	}
	rows, err := pool.Query(ctx, `
		SELECT id::text, state::text
		FROM media_asset_versions
		WHERE work_claim_token IS NOT NULL
		ORDER BY state, id
		LIMIT 20
	`)
	if err != nil {
		return fmt.Errorf("checking active media claims: %w", err)
	}
	defer rows.Close()
	var claims []ActiveMediaClaim
	for rows.Next() {
		var claim ActiveMediaClaim
		if err := rows.Scan(&claim.AssetVersionID, &claim.State); err != nil {
			return fmt.Errorf("reading active media claims: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating active media claims: %w", err)
	}
	if len(claims) == 0 {
		return nil
	}
	described := make([]string, 0, len(claims))
	for _, claim := range claims {
		described = append(described, claim.State+" "+claim.AssetVersionID)
	}
	return fmt.Errorf("active media work is claimed (%s); stop the API and worker, let in-flight media settle, and retry",
		strings.Join(described, ", "))
}
