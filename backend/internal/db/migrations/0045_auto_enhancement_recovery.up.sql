-- Phase 3C-C / durable automatic enhancement recovery state.
--
-- 3C-B gave an Administrator one manual action to finish a PLAYABLE video whose
-- canonical ladder is incomplete. 3C-C schedules that same action
-- automatically, and this migration is the only place the scheduler's knowledge
-- lives. It adds durable state and one nullable attribution column. It changes
-- no existing behaviour, produces no work by itself, and the producer ships
-- disabled in every environment.
--
-- WHY THE STATE MUST BE HERE RATHER THAN IN REDIS
--
-- The question "is automatic recovery outstanding for this Asset Version, and
-- what happened to the last attempt" has to survive a worker crash, a Redis
-- flush, a task retry, a process restart, and a stale-lease recovery. Redis
-- holds queued tasks; it cannot answer that question after losing them. A
-- scheduler that inferred the answer from "there is an open row for this asset"
-- or from time proximity would attribute a manual retry, a second worker's
-- operation, or an unrelated stale-recovery terminalization to an automatic
-- attempt that never ran, and would then charge or clear a budget on that basis.
--
-- WHY THE INTENT IDENTITY IS THE OUTBOX EVENT ID
--
-- Enhancement execution mints its own operation id at claim time, on purpose:
-- claim, source proof and ladder planning are decided against current database
-- truth rather than against a stale queue payload. So the intent cannot know the
-- operation in advance. Instead the scheduler commits its row and the outbox
-- event in ONE transaction, sharing one identity, and the worker writes the
-- operation it minted back onto the row inside the same transaction that takes
-- the media claim. Every edge of intent -> event -> task -> operation -> terminal
-- attempt is then a committed PostgreSQL fact.

CREATE TABLE media_auto_enhancement_recovery (
    -- One row per Asset Version, and the primary key is the dedupe guarantee:
    -- no number of scheduler ticks or concurrent scheduler instances can produce
    -- a second outstanding intent for the same asset. There is deliberately no
    -- IDLE or ELIGIBLE state — the ABSENCE of a row means eligible — so a
    -- repeated scan of an asset that has never been scheduled writes nothing.
    asset_version_id     UUID PRIMARY KEY REFERENCES media_asset_versions (id) ON DELETE CASCADE,

    state                TEXT NOT NULL,

    -- The outstanding intent, which is also the outbox event id that carries it.
    -- NULL means nothing is outstanding.
    current_intent_id    UUID,

    -- The processing operation the worker minted for current_intent_id, written
    -- in the claim transaction. This is the authoritative intent -> execution
    -- link: a terminal processing_attempts row for this operation belongs to this
    -- intent, and to no other.
    executing_operation_id TEXT,

    -- When a SCHEDULED intent stops being believable. The claim transaction is
    -- atomic, so an intent that never reached EXECUTING never ran and charged
    -- nothing; past this deadline the asset may be scheduled again under a NEW
    -- intent id. The superseded task is then a no-op, because the worker refuses
    -- an intent the row no longer names.
    intent_expires_at    TIMESTAMPTZ,

    -- Consecutive AUTOMATIC failures only. A manual retry resets it, and a new
    -- canonical rendition resets it, because real forward progress is not failure
    -- however the operation ended.
    consecutive_failures INT NOT NULL DEFAULT 0,

    -- Which automatic attempt the outstanding intent is. Monotonic for the life
    -- of the row; it is evidence, not the budget.
    attempt_number       INT NOT NULL DEFAULT 0,

    -- Database-clock deadline for the next automatic attempt. Never a
    -- process-local timer: a worker whose clock runs fast would otherwise
    -- schedule attempts the database still considers future.
    next_attempt_at      TIMESTAMPTZ,

    -- The canonical rendition count observed when the outstanding intent was
    -- created. Progress is a comparison against this baseline and against
    -- video_renditions, never against FFmpeg output.
    progress_rendition_count INT NOT NULL DEFAULT 0,

    last_failure_category TEXT,
    last_outcome_at      TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT maer_state_valid CHECK (
        state IN ('SCHEDULED', 'EXECUTING', 'BACKOFF', 'NEEDS_OPERATOR')
    ),
    CONSTRAINT maer_counters_non_negative CHECK (
        consecutive_failures >= 0 AND attempt_number >= 0 AND progress_rendition_count >= 0
    ),
    CONSTRAINT maer_operation_present CHECK (
        executing_operation_id IS NULL OR length(trim(executing_operation_id)) > 0
    ),
    CONSTRAINT maer_failure_category_present CHECK (
        last_failure_category IS NULL OR length(trim(last_failure_category)) > 0
    ),

    -- The state machine, enforced by the database rather than by Go. No code path
    -- may leave a row claiming to execute an operation it never bound, or
    -- claiming an intent is outstanding with nothing to identify it.
    CONSTRAINT maer_state_coherent CHECK (
        (state = 'SCHEDULED'
            AND current_intent_id IS NOT NULL
            AND executing_operation_id IS NULL
            AND intent_expires_at IS NOT NULL
            AND attempt_number > 0)
        OR (state = 'EXECUTING'
            AND current_intent_id IS NOT NULL
            AND executing_operation_id IS NOT NULL
            AND attempt_number > 0)
        OR (state = 'BACKOFF'
            AND current_intent_id IS NULL
            AND executing_operation_id IS NULL
            AND intent_expires_at IS NULL
            AND next_attempt_at IS NOT NULL)
        OR (state = 'NEEDS_OPERATOR'
            AND current_intent_id IS NULL
            AND executing_operation_id IS NULL
            AND intent_expires_at IS NULL
            AND next_attempt_at IS NULL)
    )
);

COMMENT ON TABLE media_auto_enhancement_recovery IS
    'Durable automatic enhancement recovery state, one row per Asset Version. The absence of a row means the Asset Version has never been automatically scheduled and is therefore eligible; a row is created only when an intent is committed, and deleted when the asset reaches READY. Redis is never the source of truth for whether automatic recovery is outstanding.';

COMMENT ON COLUMN media_auto_enhancement_recovery.current_intent_id IS
    'The outstanding automatic intent, which is also the id of the outbox event that carries it. Committed in the same transaction as that event, so the row can never name an intent that was not committed.';

COMMENT ON COLUMN media_auto_enhancement_recovery.executing_operation_id IS
    'The processing operation the worker minted for current_intent_id, written in the same transaction that took the media claim. This is the authoritative intent-to-execution link and the reason attribution is never heuristic.';

-- The scheduler selects on next_attempt_at, and only for rows that are not
-- outstanding. A partial index keeps the scan proportional to the work actually
-- due rather than to the number of assets ever recovered.
CREATE INDEX media_auto_enhancement_recovery_due
    ON media_auto_enhancement_recovery (next_attempt_at)
    WHERE state = 'BACKOFF';

-- Recovering an abandoned SCHEDULED intent reads the same way.
CREATE INDEX media_auto_enhancement_recovery_abandoned
    ON media_auto_enhancement_recovery (intent_expires_at)
    WHERE state = 'SCHEDULED';

-- Terminal attribution, nullable on purpose.
--
-- Every processing attempt that exists before schema 45 was either a manual
-- enhancement or a FULL transcode, and processing_attempts is append-only, so a
-- NOT NULL column would mean either refusing the migration or rewriting audit
-- evidence. NULL means "not an automatic recovery attempt", which is the truth
-- for all of them.
--
-- Deliberately NOT a foreign key to media_auto_enhancement_recovery: that row is
-- deleted when the asset reaches READY, and the attempt evidence must outlive the
-- scheduler state it was produced under. The audit value of the column is the
-- identity itself, not a live join.
ALTER TABLE processing_attempts
    ADD COLUMN auto_recovery_intent_id UUID;

COMMENT ON COLUMN processing_attempts.auto_recovery_intent_id IS
    'The automatic recovery intent this attempt executed, or NULL when the attempt was manual, automatic-transcode, or predates schema 45. Retained after the scheduler row is deleted, which is why it is not a foreign key.';
