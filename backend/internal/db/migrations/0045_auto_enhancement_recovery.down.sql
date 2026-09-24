-- Reverses the automatic enhancement recovery state.
--
-- Rolling back while an automatic intent is outstanding would drop the only
-- record of work the queue may still be about to execute. The worker would then
-- receive a task carrying an auto_recovery_intent_id it cannot resolve; the
-- schema-44 worker does not read the field at all and would execute the
-- enhancement as if it were manual, so the operation itself is safe — but the
-- scheduler's budget, its backoff deadline, and its NEEDS_OPERATOR verdict would
-- all be destroyed silently, and an asset an operator had been told to look at
-- would look untouched.
--
-- So this refuses while any intent is outstanding, matching the guard 0042
-- applies to active media work. BACKOFF and NEEDS_OPERATOR rows are not
-- outstanding work — nothing is queued for them — and they are discarded with the
-- table, which is honest: without the producer there is no automatic recovery to
-- schedule.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM media_auto_enhancement_recovery
         WHERE state IN ('SCHEDULED', 'EXECUTING')
    ) THEN
        RAISE EXCEPTION 'cannot roll back 0045: automatic enhancement recovery intents are outstanding; let them settle or clear them deliberately before rolling back';
    END IF;
END $$;

-- processing_attempts is append-only, so the attribution column is dropped
-- rather than cleared. Dropping it loses which attempts were automatic; the
-- attempts themselves, their kinds, their outcomes and their operation identities
-- are untouched.
ALTER TABLE processing_attempts
    DROP COLUMN auto_recovery_intent_id;

DROP TABLE media_auto_enhancement_recovery;
