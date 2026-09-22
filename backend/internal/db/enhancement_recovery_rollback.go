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

// PendingEnhancementIntent is one durable manual-enhancement request that the
// media dispatcher has not yet turned into a queue task.
type PendingEnhancementIntent struct {
	EventID        string
	AssetVersionID string
}

// pendingEnhancementIntentSample bounds what a refusal prints. The count it
// reports alongside is exact; the identifiers are a sample, because an operator
// needs enough to begin resolving the work, not an entire backlog in a shell.
const pendingEnhancementIntentSample = 20

// PendingEnhancementIntents reports every committed media.enhancement_requested
// outbox event with no dispatch receipt, as an exact count plus a bounded
// sample. It is read-only and mutates no processing evidence.
//
// It deliberately ignores available_at. A future-dated intent is still durable:
// it survives a downgrade and becomes dispatchable later, which is exactly the
// case a rollback gate must catch rather than defer.
func PendingEnhancementIntents(ctx context.Context, pool *pgxpool.Pool) (int, []PendingEnhancementIntent, error) {
	if pool == nil {
		return 0, nil, errors.New("database pool is required")
	}
	var total int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM outbox_events e
		LEFT JOIN media_outbox_dispatches md ON md.event_id = e.id
		WHERE e.source_module = 'MEDIA_AND_ASSETS'
		  AND e.event_type = 'media.enhancement_requested'
		  AND md.event_id IS NULL
	`).Scan(&total); err != nil {
		return 0, nil, fmt.Errorf("counting pending enhancement intents: %w", err)
	}
	if total == 0 {
		return 0, nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT e.id::text, e.aggregate_id::text
		FROM outbox_events e
		LEFT JOIN media_outbox_dispatches md ON md.event_id = e.id
		WHERE e.source_module = 'MEDIA_AND_ASSETS'
		  AND e.event_type = 'media.enhancement_requested'
		  AND md.event_id IS NULL
		ORDER BY e.occurred_at, e.id
		LIMIT $1
	`, pendingEnhancementIntentSample)
	if err != nil {
		return 0, nil, fmt.Errorf("listing pending enhancement intents: %w", err)
	}
	defer rows.Close()
	var pending []PendingEnhancementIntent
	for rows.Next() {
		var intent PendingEnhancementIntent
		if err := rows.Scan(&intent.EventID, &intent.AssetVersionID); err != nil {
			return 0, nil, fmt.Errorf("reading pending enhancement intents: %w", err)
		}
		pending = append(pending, intent)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("iterating pending enhancement intents: %w", err)
	}
	return total, pending, nil
}

// CheckNoPendingEnhancementIntents is the hard 42 -> 41 gate for undispatched
// manual enhancement work, and it is not advisory.
//
// The deployed 3C-A media dispatcher has no case for
// media.enhancement_requested. It does not skip that row: dispatchEvent returns
// "unsupported media outbox event", DispatchPending returns the error, and the
// batch stops. The query that feeds it is ordered by occurred_at and selects
// only rows without a dispatch receipt, so the surviving intent is the first row
// of every subsequent batch too. One such row therefore does not merely fail to
// run — it permanently blocks scan and transcode dispatch for the whole module
// until an operator removes it.
//
// Proving zero before the downgrade is the only safe ordering. This command does
// not delete or reschedule intents: rollback must not quietly discard work an
// Administrator asked for.
func CheckNoPendingEnhancementIntents(ctx context.Context, pool *pgxpool.Pool) error {
	total, pending, err := PendingEnhancementIntents(ctx, pool)
	if err != nil {
		return err
	}
	if total == 0 {
		return nil
	}
	described := make([]string, 0, len(pending))
	for _, intent := range pending {
		described = append(described, intent.EventID+" (asset version "+intent.AssetVersionID+")")
	}
	return fmt.Errorf("%d undispatched media.enhancement_requested outbox event(s) remain (%s); the schema-41 dispatcher aborts its batch on that event type and would block all later media outbox dispatch. Resolve them before rolling back; this command will not delete them",
		total, strings.Join(described, ", "))
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
