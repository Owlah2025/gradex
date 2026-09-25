package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Phase 3C-C — automatic enhancement recovery.
//
// A PLAYABLE video is deliverable but incomplete. 3C-B gave an Administrator one
// manual action to finish it; this schedules that same action automatically,
// through the same outbox event, the same queue task, and the same worker
// execution path. There is no second recovery engine, no second FFmpeg
// invocation, and no second READY proof.
//
// The scheduler never takes the media work claim. It writes intent; the
// execution-time worker remains the only claimant.
//
// The design this implements is docs/media-auto-enhancement-recovery.md, and the
// durable state is migration 0045.

// Automatic recovery scheduler states. The database enforces which column
// combinations each one permits (maer_state_coherent), so these are names for
// rows PostgreSQL has already validated rather than the only thing standing
// between the scheduler and a contradictory row.
const (
	autoRecoveryScheduled     = "SCHEDULED"
	autoRecoveryExecuting     = "EXECUTING"
	autoRecoveryBackoff       = "BACKOFF"
	autoRecoveryNeedsOperator = "NEEDS_OPERATOR"
	// autoRecoveryManualPending means an Administrator's RetryEnhancements
	// request has been accepted and has not yet executed. Automatic scheduling is
	// suppressed for as long as the row is in this state.
	autoRecoveryManualPending = "MANUAL_PENDING"
)

// autoRecoveryManualLease bounds how long an accepted manual request suppresses
// automatic scheduling.
//
// It is the safety valve for a manual task that was dispatched and then lost:
// without it, one accepted request whose task never arrived would block automatic
// recovery for that asset forever. Like the intent lease it is a deadline rather
// than a licence — expiry permits release only once the manual event carries a
// durable dispatch receipt, because an undispatched manual event has not been
// lost at all and the outbox dispatcher is still going to deliver it.
const autoRecoveryManualLease = 2 * time.Hour

// MaxAutoEnhancementFailures is the consecutive automatic failure budget.
//
// Three failed automatic executions is the maximum. Failure #1 and #2 schedule a
// backoff; failure #3 lands on NEEDS_OPERATOR immediately rather than scheduling
// a fourth execution. There is deliberately no fourth automatic attempt and
// therefore no third backoff stage.
const MaxAutoEnhancementFailures = 3

// autoRecoveryIntentLease bounds how long a DISPATCHED SCHEDULED intent stays
// believable.
//
// The claim transaction is atomic, so an intent that never reached EXECUTING
// never ran and charged nothing. Past this deadline the asset may be scheduled
// again under a NEW intent id, which is how the scheduler recovers from a task
// Redis accepted and then lost. The superseded task, if it ever arrives, is a
// no-op because the worker refuses an intent the row no longer names.
//
// ELAPSED TIME ALONE IS NOT SUFFICIENT.
//
// Expiry permits replacement only once media_outbox_dispatches holds a receipt
// for the intent's event. The intent id IS that event id, so the receipt is an
// exact, durable, DB-authoritative answer to "did this work ever leave the
// outbox". Without it the two cases are indistinguishable by time:
//
//   - never dispatched: the event is still pending in the outbox and the
//     existing dispatcher owns retrying THAT event. Minting a second intent
//     would leave the first event undispatched and dispatchable, so a sustained
//     dispatcher or Redis outage would accumulate one new enhancement event per
//     expiry window and deliver all of them when Redis returned. Bounded growth
//     is a hard invariant, so this case must keep and reuse the durable intent
//     however many ticks pass.
//   - dispatched, never claimed: the queue accepted the task and lost it.
//     Re-issuing under a new identity is the only way forward, and is bounded
//     because each re-issue first required a real dispatch.
//
// Redis is never consulted for this. The receipt is the authority.
//
// It is deliberately shorter than the media processing timeout. A long-running
// execution holds the media claim, and a claimed asset is excluded from
// eligibility, so a legitimate multi-hour transcode can never be re-scheduled
// underneath itself — only a claim-free row stuck past this deadline can.
const autoRecoveryIntentLease = time.Hour

// autoEnhancementRecoveryCorrelation distinguishes automatic intents from the
// manual Admin retry in the outbox. Manual work keeps its existing correlation,
// which is its own event id; changing that would alter the behaviour of the
// already-deployed 3C-B path for no benefit.
const autoEnhancementRecoveryCorrelation = "auto-enhancement-recovery"

// ErrEnhancementIntentSuperseded means a queued automatic task carries an intent
// the authoritative scheduler row no longer names — because the intent expired
// and was re-issued, because an Administrator overrode it, or because the asset
// has since reached READY and the row is gone.
//
// It is benign. The task claims nothing, charges no automatic attempt, and is
// acknowledged rather than retried.
var ErrEnhancementIntentSuperseded = errors.New("media enhancement intent is superseded")

// ErrAutoEnhancementRecoveryDisabled means a queued AUTOMATIC recovery task was
// delivered while MEDIA_AUTO_ENHANCEMENT_RECOVERY_ENABLED is false.
//
// Disabling the feature has to stop work that is already in flight, not merely
// stop the scheduler loop from starting. A task that ran anyway would take the
// media claim, write a processing attempt and encode — which is exactly what an
// operator turning the flag off is trying to prevent.
//
// It is benign: the task claims nothing, charges no automatic attempt, and is
// acknowledged rather than retried. Manual recovery is unaffected.
var ErrAutoEnhancementRecoveryDisabled = errors.New("automatic media enhancement recovery is disabled")

// autoEnhancementBackoff is the automatic retry schedule. Deadlines are written
// as database timestamps, so the schedule is not held in any process.
//
// The schedule is total over the only inputs that can reach it. A backoff is
// scheduled only for a failure count strictly below MaxAutoEnhancementFailures,
// so the reachable failure arguments are exactly 1 and 2; failure #3 reaches
// NEEDS_OPERATOR without consulting this schedule. The override and release
// paths ask for the first-failure interval explicitly.
//
// A third backoff stage would be unreachable, so it is not offered: a schedule
// that named an interval no execution can ever wait is a claim the runtime does
// not honour.
func autoEnhancementBackoff(consecutiveFailures int) time.Duration {
	if consecutiveFailures >= 2 {
		return time.Hour
	}
	return 15 * time.Minute
}

// AutoRecoveryPhase names one safe, structured observation from the automatic
// recovery runtime.
type AutoRecoveryPhase string

const (
	AutoRecoveryCandidateDiscovered AutoRecoveryPhase = "CANDIDATE_DISCOVERED"
	AutoRecoveryIntentScheduled     AutoRecoveryPhase = "INTENT_SCHEDULED"
	AutoRecoveryIntentDeduped       AutoRecoveryPhase = "INTENT_DEDUPED"
	AutoRecoveryExecutionLinked     AutoRecoveryPhase = "EXECUTION_LINKED"
	AutoRecoveryIntentSuperseded    AutoRecoveryPhase = "INTENT_SUPERSEDED"
	AutoRecoveryIntentReleased      AutoRecoveryPhase = "INTENT_RELEASED"
	AutoRecoveryProgressMade        AutoRecoveryPhase = "PROGRESS_MADE"
	AutoRecoveryReady               AutoRecoveryPhase = "READY"
	AutoRecoveryFailed              AutoRecoveryPhase = "FAILED"
	AutoRecoveryBackoffScheduled    AutoRecoveryPhase = "BACKOFF_SCHEDULED"
	AutoRecoveryExhausted           AutoRecoveryPhase = "EXHAUSTED"
	AutoRecoveryManualOverride      AutoRecoveryPhase = "MANUAL_OVERRIDE"
)

// AutoRecoveryObservation is advisory telemetry. It carries identities and
// closed classifications only — never a storage key, a signed URL, a checksum,
// a token, or raw error text.
type AutoRecoveryObservation struct {
	Phase           AutoRecoveryPhase
	AssetVersionID  string
	IntentID        string
	OperationID     string
	AttemptNumber   int
	Failures        int
	FailureCategory string
	NextAttemptIn   time.Duration
}

// AutoRecoveryObserver receives advisory automatic recovery telemetry.
type AutoRecoveryObserver func(AutoRecoveryObservation)

func (w *Worker) observeAutoRecovery(observation AutoRecoveryObservation) {
	if w.observeAutoRecoveryFn != nil {
		w.observeAutoRecoveryFn(observation)
	}
}

// AutoEnhancementRecoveryEnabled reports whether the reconciler may run. It is
// MEDIA_AUTO_ENHANCEMENT_RECOVERY_ENABLED and the schema-45 requirement
// combined, because both are hard preconditions and neither is meaningful alone.
func (w *Worker) AutoEnhancementRecoveryEnabled() bool {
	return w.autoRecoveryEnabled && w.autoRecoveryStateAvailable
}

// autoRecoveryAttribution carries the SQL fragments that attribute a processing
// attempt to the automatic intent whose execution produced it.
//
// processing_attempts is append-only, enforced by a trigger, so the value has to
// be written by the INSERT that creates the row rather than added afterwards. The
// value is resolved from the authoritative link — the scheduler row whose
// executing_operation_id is this exact operation — so the stale-recovery path,
// which never sees an intent id in Go, attributes its attempt correctly too.
//
// Both fragments are empty below schema 45, where the column does not exist.
type autoRecoveryAttribution struct {
	column string
	value  string
}

func (w *Worker) autoRecoveryAttribution() autoRecoveryAttribution {
	if !w.autoRecoveryStateAvailable {
		return autoRecoveryAttribution{}
	}
	return autoRecoveryAttribution{
		column: ", auto_recovery_intent_id",
		value: `, (SELECT r.current_intent_id FROM media_auto_enhancement_recovery r
		          WHERE r.asset_version_id = $1::uuid AND r.executing_operation_id = $2)`,
	}
}

// ScheduleAutoEnhancementRecovery is one bounded reconciler pass.
//
// It selects settled recovery candidates, and for each one commits — in a single
// transaction — an automatic intent row and the outbox event that carries it.
// It takes no media claim.
func (w *Worker) ScheduleAutoEnhancementRecovery(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("automatic enhancement recovery limit must be positive")
	}
	// Disabled means silent. No candidate SELECT, no scheduler write, no outbox
	// write. The caller also does not start the loop, so this is the second of two
	// independent gates rather than the only one.
	if !w.AutoEnhancementRecoveryEnabled() {
		return 0, nil
	}
	candidates, err := w.autoRecoveryCandidates(ctx, limit)
	if err != nil {
		return 0, err
	}
	scheduled := 0
	for _, assetVersionID := range candidates {
		w.observeAutoRecovery(AutoRecoveryObservation{
			Phase: AutoRecoveryCandidateDiscovered, AssetVersionID: assetVersionID,
		})
		applied, err := w.scheduleAutoRecoveryIntent(ctx, assetVersionID)
		if err != nil {
			return scheduled, fmt.Errorf("scheduling automatic enhancement recovery for %s: %w", assetVersionID, err)
		}
		if applied {
			scheduled++
		}
	}
	return scheduled, nil
}

// autoRecoveryCandidates reads the settled candidates. The read is advisory: the
// per-candidate transaction re-proves every predicate under a row lock, and the
// compare-and-set against the scheduler row is what actually decides.
//
// Settledness is not a time heuristic. A claim-free PLAYABLE row is the
// discriminator: a healthy progressive FULL operation that has reached PLAYABLE
// still holds its claim, so it is excluded by construction rather than by
// guessing how long ago it started.
func (w *Worker) autoRecoveryCandidates(ctx context.Context, limit int) ([]string, error) {
	rows, err := w.db.Query(ctx, `
		SELECT mav.id::text
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		LEFT JOIN media_auto_enhancement_recovery r ON r.asset_version_id = mav.id
		WHERE mav.kind = 'VIDEO'
		  AND mav.state = 'PLAYABLE'
		  AND ma.retired_at IS NULL
		  AND mav.work_claim_token IS NULL
		  AND (mav.work_lease_expires_at IS NULL OR mav.work_lease_expires_at <= now())
		  AND COALESCE(mav.sha256_hex, '') <> ''
		  AND (mav.successful_scan_attempt_id IS NOT NULL
		       OR mav.successful_validation_attempt_id IS NOT NULL)
		  AND (
		        r.asset_version_id IS NULL
		     OR (r.state = 'BACKOFF' AND r.next_attempt_at <= now())
		     OR (r.state = 'SCHEDULED' AND r.intent_expires_at <= now()
		         AND EXISTS (SELECT 1 FROM media_outbox_dispatches md
		                     WHERE md.event_id = r.current_intent_id))
		     OR (r.state = 'EXECUTING' AND r.intent_expires_at <= now())
		     OR (r.state = 'MANUAL_PENDING' AND r.manual_expires_at <= now()
		         AND EXISTS (SELECT 1 FROM media_outbox_dispatches md
		                     WHERE md.event_id = r.manual_intent_id))
		  )
		ORDER BY COALESCE(r.next_attempt_at, mav.created_at), mav.id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("loading automatic enhancement recovery candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("reading automatic enhancement recovery candidate: %w", err)
		}
		candidates = append(candidates, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating automatic enhancement recovery candidates: %w", err)
	}
	return candidates, nil
}

func (w *Worker) scheduleAutoRecoveryIntent(ctx context.Context, assetVersionID string) (bool, error) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("beginning automatic recovery scheduling: %w", err)
	}
	defer tx.Rollback(ctx)

	// SKIP LOCKED rather than waiting. A second reconciler holding this asset is
	// already deciding it, so blocking would only serialise two loops that have
	// other work to do — and the compare-and-set below would refuse this one
	// anyway.
	var eligible bool
	err = tx.QueryRow(ctx, `
		SELECT true
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		WHERE mav.id = $1::uuid
		  AND mav.kind = 'VIDEO'
		  AND mav.state = 'PLAYABLE'
		  AND ma.retired_at IS NULL
		  AND mav.work_claim_token IS NULL
		  AND (mav.work_lease_expires_at IS NULL OR mav.work_lease_expires_at <= now())
		  AND COALESCE(mav.sha256_hex, '') <> ''
		  AND (mav.successful_scan_attempt_id IS NOT NULL
		       OR mav.successful_validation_attempt_id IS NOT NULL)
		FOR UPDATE OF mav SKIP LOCKED
	`, assetVersionID).Scan(&eligible)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either the asset stopped being eligible between the read and now, or
		// another reconciler holds its lock. Both are ordinary.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("locking automatic recovery candidate: %w", err)
	}

	intentID := uuid.NewString()
	// The intent identity IS the outbox event id, and both rows are committed
	// together below, so the scheduler row can never name an intent that was not
	// committed and the event can never exist without its row.
	//
	// ON CONFLICT ... DO UPDATE ... WHERE is the compare-and-set. The primary key
	// means at most one row per Asset Version exists at all, and the WHERE means
	// only a row that is genuinely due may be replaced. A row that is SCHEDULED
	// and unexpired, EXECUTING and unexpired, in BACKOFF before its deadline, or
	// NEEDS_OPERATOR yields nothing and writes nothing — which is what keeps a
	// hundred ticks over one unchanged asset bounded.
	var attemptNumber, failures int
	err = tx.QueryRow(ctx, `
		INSERT INTO media_auto_enhancement_recovery (
			asset_version_id, state, current_intent_id, intent_expires_at,
			attempt_number, progress_rendition_count, next_attempt_at, updated_at
		) VALUES (
			$1::uuid, 'SCHEDULED', $2::uuid, now() + make_interval(secs => $3),
			1, (SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid), NULL, now()
		)
		ON CONFLICT (asset_version_id) DO UPDATE
		SET state = 'SCHEDULED',
		    current_intent_id = EXCLUDED.current_intent_id,
		    executing_operation_id = NULL,
		    manual_intent_id = NULL,
		    manual_expires_at = NULL,
		    intent_expires_at = EXCLUDED.intent_expires_at,
		    attempt_number = media_auto_enhancement_recovery.attempt_number + 1,
		    next_attempt_at = NULL,
		    progress_rendition_count = EXCLUDED.progress_rendition_count,
		    updated_at = now()
		WHERE (media_auto_enhancement_recovery.state = 'BACKOFF'
		       AND media_auto_enhancement_recovery.next_attempt_at <= now())
		   OR (media_auto_enhancement_recovery.state = 'SCHEDULED'
		       AND media_auto_enhancement_recovery.intent_expires_at <= now()
		       AND EXISTS (SELECT 1 FROM media_outbox_dispatches md
		                   WHERE md.event_id = media_auto_enhancement_recovery.current_intent_id))
		   OR (media_auto_enhancement_recovery.state = 'EXECUTING'
		       AND media_auto_enhancement_recovery.intent_expires_at <= now())
		   OR (media_auto_enhancement_recovery.state = 'MANUAL_PENDING'
		       AND media_auto_enhancement_recovery.manual_expires_at <= now()
		       AND EXISTS (SELECT 1 FROM media_outbox_dispatches md
		                   WHERE md.event_id = media_auto_enhancement_recovery.manual_intent_id))
		RETURNING attempt_number, consecutive_failures
	`, assetVersionID, intentID, autoRecoveryIntentLease.Seconds()).Scan(&attemptNumber, &failures)
	if errors.Is(err, pgx.ErrNoRows) {
		w.observeAutoRecovery(AutoRecoveryObservation{
			Phase: AutoRecoveryIntentDeduped, AssetVersionID: assetVersionID,
		})
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("committing automatic recovery intent: %w", err)
	}

	if _, err := appendEnhancementWorkAt(ctx, tx, w.outbox, enhancementSchedule{
		assetVersionID:       assetVersionID,
		eventID:              intentID,
		correlation:          autoEnhancementRecoveryCorrelation,
		autoRecoveryIntentID: intentID,
	}); err != nil {
		return false, err
	}
	if err := appendAutoRecoveryAudit(ctx, tx, "MEDIA_AUTO_ENHANCEMENT_SCHEDULED", assetVersionID,
		"Automatic enhancement recovery scheduled", map[string]any{
			"automatic": true, "attempt_number": attemptNumber, "consecutive_failures": failures,
		}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("committing automatic recovery scheduling: %w", err)
	}
	w.observeAutoRecovery(AutoRecoveryObservation{
		Phase: AutoRecoveryIntentScheduled, AssetVersionID: assetVersionID,
		IntentID: intentID, AttemptNumber: attemptNumber, Failures: failures,
	})
	return true, nil
}

// linkAutoRecoveryExecution binds the operation the worker just minted to the
// intent that asked for it.
//
// It runs inside the transaction that takes the media claim, so the intent can
// never name an operation that did not actually acquire the claim, and the claim
// can never be taken for an automatic intent without being attributable. A
// compare-and-set on current_intent_id is what rejects a superseded task.
func linkAutoRecoveryExecution(ctx context.Context, tx pgx.Tx, assetVersionID, operationID, intentID string) error {
	linked, err := tx.Exec(ctx, `
		UPDATE media_auto_enhancement_recovery
		SET state = 'EXECUTING', executing_operation_id = $3, updated_at = now()
		WHERE asset_version_id = $1::uuid
		  AND state = 'SCHEDULED'
		  AND current_intent_id = $2::uuid
	`, assetVersionID, intentID, operationID)
	if err != nil {
		return fmt.Errorf("linking automatic recovery execution: %w", err)
	}
	if linked.RowsAffected() != 1 {
		return ErrEnhancementIntentSuperseded
	}
	return nil
}

// overrideAutoRecoveryForManualClaim is the operator override, applied when a
// manual enhancement takes the claim.
//
// One write does three things: it resets the automatic budget, it supersedes any
// automatic task still queued against the old intent, and it stops the scheduler
// from immediately minting a duplicate intent behind the operator's back. The row
// is upserted rather than updated, so a manual retry on an asset the scheduler
// has never touched also suppresses an immediate automatic duplicate.
//
// It deletes no historical evidence. processing_attempts is untouched.
func overrideAutoRecoveryForManualClaim(ctx context.Context, tx pgx.Tx, assetVersionID string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_auto_enhancement_recovery (
			asset_version_id, state, consecutive_failures, attempt_number,
			next_attempt_at, progress_rendition_count, updated_at
		) VALUES (
			$1::uuid, 'BACKOFF', 0, 0, now() + make_interval(secs => $2),
			(SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid), now()
		)
		ON CONFLICT (asset_version_id) DO UPDATE
		SET state = 'BACKOFF',
		    consecutive_failures = 0,
		    current_intent_id = NULL,
		    executing_operation_id = NULL,
		    intent_expires_at = NULL,
		    manual_intent_id = NULL,
		    manual_expires_at = NULL,
		    next_attempt_at = EXCLUDED.next_attempt_at,
		    last_failure_category = NULL,
		    updated_at = now()
	`, assetVersionID, autoEnhancementBackoff(1).Seconds()); err != nil {
		return fmt.Errorf("overriding automatic recovery state for a manual claim: %w", err)
	}
	return nil
}

// releaseAutoRecoveryIntent withdraws an intent whose execution never started.
//
// A claim race, an asset that stopped being eligible, or a worker that decided
// not to run is not an automatic failure, so the budget is untouched. Returning
// the row to BACKOFF rather than leaving it SCHEDULED until its lease expires
// means the next attempt comes at the ordinary deadline instead of an hour later.
func releaseAutoRecoveryIntent(ctx context.Context, tx pgx.Tx, assetVersionID, intentID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE media_auto_enhancement_recovery
		SET state = 'BACKOFF',
		    current_intent_id = NULL,
		    executing_operation_id = NULL,
		    intent_expires_at = NULL,
		    next_attempt_at = now() + make_interval(secs => $3),
		    updated_at = now()
		WHERE asset_version_id = $1::uuid
		  AND state = 'SCHEDULED'
		  AND current_intent_id = $2::uuid
	`, assetVersionID, intentID, autoEnhancementBackoff(1).Seconds()); err != nil {
		return fmt.Errorf("releasing automatic recovery intent: %w", err)
	}
	return nil
}

// closeAutoRecoveryOnReady clears automatic state for an asset that reached
// READY.
//
// Automatic recovery is finished for it: READY is excluded from eligibility, so
// it can never be a candidate again, and a permanently outstanding row would be a
// false claim. The audit trail of what happened lives in processing_attempts and
// in the audit log, which is why processing_attempts.auto_recovery_intent_id is
// deliberately not a foreign key to this row.
//
// It is keyed by Asset Version rather than by operation, so a manual retry that
// succeeded also clears the BACKOFF row its own override wrote.
func closeAutoRecoveryOnReady(ctx context.Context, tx pgx.Tx, assetVersionID string) error {
	if _, err := tx.Exec(ctx,
		"DELETE FROM media_auto_enhancement_recovery WHERE asset_version_id = $1::uuid",
		assetVersionID); err != nil {
		return fmt.Errorf("closing automatic recovery state on ready: %w", err)
	}
	return nil
}

// autoRecoveryOutcome is what the scheduler row is reconciled to after a linked
// execution ended.
type autoRecoveryOutcome struct {
	assetVersionID string
	operationID    string
	category       failureCategory
	permanent      bool
}

// reconcileAutoRecoveryFailure updates the scheduler row from the actual outcome
// of the operation that was linked to it.
//
// It is keyed by executing_operation_id, which is the authoritative link. A
// manual operation matches no row, a superseded operation matches no row, and a
// second worker's operation matches no row — so none of them can charge or clear
// an automatic budget. Nothing here infers attribution from timing or from the
// mere existence of an open row.
//
// Progress is read from canonical database evidence: a newly committed
// video_renditions row against the baseline the intent recorded. FFmpeg output is
// never consulted for it, and progress resets the budget even when the operation
// later failed, because a rung that was really written is real forward movement.
func reconcileAutoRecoveryFailure(ctx context.Context, tx pgx.Tx, outcome autoRecoveryOutcome) (AutoRecoveryObservation, error) {
	var intentID string
	var failures, baseline, attemptNumber int
	err := tx.QueryRow(ctx, `
		SELECT current_intent_id::text, consecutive_failures, progress_rendition_count, attempt_number
		FROM media_auto_enhancement_recovery
		WHERE asset_version_id = $1::uuid AND executing_operation_id = $2
		FOR UPDATE
	`, outcome.assetVersionID, outcome.operationID).Scan(&intentID, &failures, &baseline, &attemptNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		return AutoRecoveryObservation{}, nil
	}
	if err != nil {
		return AutoRecoveryObservation{}, fmt.Errorf("loading automatic recovery state for reconciliation: %w", err)
	}

	var rungs int
	if err := tx.QueryRow(ctx,
		"SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid",
		outcome.assetVersionID).Scan(&rungs); err != nil {
		return AutoRecoveryObservation{}, fmt.Errorf("reading canonical progress evidence: %w", err)
	}
	progressed := rungs > baseline
	if progressed {
		failures = 0
	} else {
		failures++
	}

	observation := AutoRecoveryObservation{
		AssetVersionID: outcome.assetVersionID, IntentID: intentID,
		OperationID: outcome.operationID, AttemptNumber: attemptNumber,
		Failures: failures, FailureCategory: string(outcome.category),
	}

	// A permanent failure never loops. Re-encoding a source whose checksum does
	// not match, or whose immutable version is gone, or whose canonical evidence
	// contradicts itself, produces the same failure every time and only burns
	// worker capacity until an operator looks at it.
	if outcome.permanent || failures >= MaxAutoEnhancementFailures {
		if _, err := tx.Exec(ctx, `
			UPDATE media_auto_enhancement_recovery
			SET state = 'NEEDS_OPERATOR',
			    current_intent_id = NULL,
			    executing_operation_id = NULL,
			    intent_expires_at = NULL,
			    next_attempt_at = NULL,
			    consecutive_failures = $3,
			    last_failure_category = $4,
			    last_outcome_at = now(),
			    updated_at = now()
			WHERE asset_version_id = $1::uuid AND executing_operation_id = $2
		`, outcome.assetVersionID, outcome.operationID, failures, string(outcome.category)); err != nil {
			return AutoRecoveryObservation{}, fmt.Errorf("exhausting automatic recovery: %w", err)
		}
		if err := appendAutoRecoveryAudit(ctx, tx, "MEDIA_AUTO_ENHANCEMENT_EXHAUSTED", outcome.assetVersionID,
			"Automatic enhancement recovery requires an operator", map[string]any{
				"automatic": true, "attempt_number": attemptNumber,
				"consecutive_failures": failures, "failure_category": string(outcome.category),
				"permanent": outcome.permanent,
			}); err != nil {
			return AutoRecoveryObservation{}, err
		}
		observation.Phase = AutoRecoveryExhausted
		return observation, nil
	}

	backoff := autoEnhancementBackoff(failures)
	if _, err := tx.Exec(ctx, `
		UPDATE media_auto_enhancement_recovery
		SET state = 'BACKOFF',
		    current_intent_id = NULL,
		    executing_operation_id = NULL,
		    intent_expires_at = NULL,
		    next_attempt_at = now() + make_interval(secs => $5),
		    consecutive_failures = $3,
		    progress_rendition_count = $6,
		    last_failure_category = $4,
		    last_outcome_at = now(),
		    updated_at = now()
		WHERE asset_version_id = $1::uuid AND executing_operation_id = $2
	`, outcome.assetVersionID, outcome.operationID, failures, string(outcome.category),
		backoff.Seconds(), rungs); err != nil {
		return AutoRecoveryObservation{}, fmt.Errorf("scheduling automatic recovery backoff: %w", err)
	}
	observation.Phase = AutoRecoveryBackoffScheduled
	observation.NextAttemptIn = backoff
	if progressed {
		observation.Phase = AutoRecoveryProgressMade
	}
	return observation, nil
}

// autoRecoveryFailureIsPermanent classifies a failure the automatic budget must
// not be spent on repeating.
//
// The retryable set is the transient one — storage unavailable, process timeout,
// worker interrupted, transcode failed. Everything below is a fact about the
// source or about the database that another attempt cannot change.
func autoRecoveryFailureIsPermanent(cause error, category failureCategory) bool {
	switch {
	case category == failureInvalidMedia:
		// Includes a source checksum mismatch: the bytes are not the bytes the
		// scan passed, so re-encoding them proves nothing.
		return true
	case errors.Is(cause, ErrNotFound):
		// The immutable source or its Asset Version is gone.
		return true
	case errors.Is(cause, ErrConflict):
		// Canonical evidence contradicts itself — a rendition row that does not
		// match its rung, a duplicate, or a ladder of the wrong size.
		return true
	case errors.Is(cause, ErrValidation):
		// A database invariant or payload invariant was violated.
		return true
	default:
		return false
	}
}

// autoRecoveryFailureIsBenign classifies an outcome that is not an automatic
// attempt at all: nobody ran, so nobody is charged.
func autoRecoveryFailureIsBenign(cause error) bool {
	return errors.Is(cause, ErrEnhancementNotEligible) ||
		errors.Is(cause, ErrEnhancementActive) ||
		errors.Is(cause, ErrEnhancementIntentSuperseded) ||
		errors.Is(cause, ErrConcurrentModification)
}

// appendAutoRecoveryAudit records automatic recovery decisions with no human
// actor, following the existing SYSTEM-actor pattern. The metadata carries
// identities and closed classifications only.
func appendAutoRecoveryAudit(
	ctx context.Context,
	tx pgx.Tx,
	action, assetVersionID, reason string,
	metadata map[string]any,
) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encoding automatic recovery audit metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor, action, module,
			target_type, target_id, reason, metadata
		) VALUES (NULL, 'SYSTEM', 'gradex-media-auto-recovery', $1, 'MEDIA_AND_ASSETS',
		          'MEDIA_ASSET_VERSION', $2, $3, $4::jsonb)
	`, action, assetVersionID, reason, encoded); err != nil {
		return fmt.Errorf("writing automatic recovery audit evidence: %w", err)
	}
	return nil
}

// suppressAutoRecoveryForManualRequest makes an accepted Admin RetryEnhancements
// request an immediate, durable automatic-scheduling suppression.
//
// It runs in the SAME transaction that appends the manual
// media.enhancement_requested event, so the two commit together or not at all.
// That is the whole point: until this existed, the scheduler was free to mint a
// fresh automatic intent in the window between the operator's request being
// accepted and the manual worker taking the media claim, and the automatic task
// could win that race — the machine overriding the operator, rather than the
// other way round.
//
// One write does everything the override needs:
//
//   - it supersedes any outstanding automatic intent, because MANUAL_PENDING
//     carries no current_intent_id, so a queued automatic task's compare-and-set
//     on that identity fails and the task no-ops before taking any claim;
//   - it resets the automatic failure budget, which is the approved
//     manual-override semantic and is why a request from NEEDS_OPERATOR works;
//   - it names the manual outbox event that is doing the suppressing, so the
//     suppression is attributable rather than anonymous;
//   - it records when the suppression stops being believable.
//
// The row is upserted, so a manual request against an asset the scheduler has
// never touched also suppresses an immediate automatic duplicate.
//
// It deletes no historical evidence. processing_attempts is untouched.
func suppressAutoRecoveryForManualRequest(ctx context.Context, tx pgx.Tx, assetVersionID, manualEventID string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_auto_enhancement_recovery (
			asset_version_id, state, consecutive_failures, attempt_number,
			manual_intent_id, manual_expires_at, progress_rendition_count, updated_at
		) VALUES (
			$1::uuid, 'MANUAL_PENDING', 0, 0,
			$2::uuid, now() + make_interval(secs => $3),
			(SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid), now()
		)
		ON CONFLICT (asset_version_id) DO UPDATE
		SET state = 'MANUAL_PENDING',
		    consecutive_failures = 0,
		    current_intent_id = NULL,
		    executing_operation_id = NULL,
		    intent_expires_at = NULL,
		    next_attempt_at = NULL,
		    manual_intent_id = EXCLUDED.manual_intent_id,
		    manual_expires_at = EXCLUDED.manual_expires_at,
		    last_failure_category = NULL,
		    updated_at = now()
	`, assetVersionID, manualEventID, autoRecoveryManualLease.Seconds()); err != nil {
		return fmt.Errorf("suppressing automatic recovery for an accepted manual request: %w", err)
	}
	return nil
}

// settleManualSuppression releases a manual suppression whose work will never
// run, so automatic recovery is not blocked forever.
//
// The manual task reached execution and declined it: the asset had stopped being
// eligible, or another claimant held it. Nothing failed automatically, so no
// budget is charged; the row simply becomes due again.
//
// It is keyed by the manual event identity, so a settle can never release a
// DIFFERENT, newer manual request that superseded this one.
func settleManualSuppression(ctx context.Context, tx pgx.Tx, assetVersionID, manualEventID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE media_auto_enhancement_recovery
		SET state = 'BACKOFF',
		    manual_intent_id = NULL,
		    manual_expires_at = NULL,
		    next_attempt_at = now() + make_interval(secs => $3),
		    updated_at = now()
		WHERE asset_version_id = $1::uuid
		  AND state = 'MANUAL_PENDING'
		  AND manual_intent_id = $2::uuid
	`, assetVersionID, manualEventID, autoEnhancementBackoff(1).Seconds()); err != nil {
		return fmt.Errorf("settling manual recovery suppression: %w", err)
	}
	return nil
}

// pauseAutoRecoveryWhileDisabled closes an automatic intent whose task arrived
// while the feature was switched off.
//
// Acknowledging the task and leaving the row SCHEDULED would strand it: the
// intent would sit outstanding until its lease expired, and re-enabling the
// feature would not produce work until then. Charging a failure would be a lie,
// because nothing ran.
//
// So the intent is explicitly closed and the row becomes due immediately. The
// automatic budget is untouched, no operation is bound, no claim is taken, and
// the acknowledged task can never become active later because the identity it
// carries is no longer the one the row names. Re-enabling the feature therefore
// yields exactly one fresh intent on the next tick.
func pauseAutoRecoveryWhileDisabled(ctx context.Context, tx pgx.Tx, assetVersionID, intentID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE media_auto_enhancement_recovery
		SET state = 'BACKOFF',
		    current_intent_id = NULL,
		    executing_operation_id = NULL,
		    intent_expires_at = NULL,
		    next_attempt_at = now(),
		    updated_at = now()
		WHERE asset_version_id = $1::uuid
		  AND state = 'SCHEDULED'
		  AND current_intent_id = $2::uuid
	`, assetVersionID, intentID); err != nil {
		return fmt.Errorf("pausing automatic recovery while disabled: %w", err)
	}
	return nil
}
