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
-- applies to active media work. That guard now also covers MANUAL_PENDING: an
-- accepted Admin RetryEnhancements request that has not yet executed is
-- outstanding work, and dropping its suppression would let automatic scheduling
-- resume behind an operator's back.
--
-- THIS DOWN IS NOT UNCONDITIONALLY SAFE, AND MUST NOT BE DESCRIBED AS SUCH.
--
-- The UP is additive, but that says nothing about the DOWN. Whenever scheduler
-- rows exist, running this migration DESTROYS operational state that exists
-- nowhere else:
--
--   * BACKOFF deadlines — every asset waiting to retry forgets that it was
--     waiting, and its consecutive-failure budget resets to nothing.
--   * NEEDS_OPERATOR verdicts — an asset an operator was told to look at becomes
--     indistinguishable from one that was never attempted.
--   * automatic scheduling decisions and attempt attribution — dropping
--     processing_attempts.auto_recovery_intent_id loses which attempts were
--     automatic and which intent produced them.
--
-- BACKOFF and NEEDS_OPERATOR rows are not refused, because nothing is queued for
-- them and refusing would make the migration unrunnable rather than safe. They
-- are discarded, and that discard is a real loss of evidence, not a no-op.
--
-- PRODUCTION ROLLBACK POSTURE
--
-- This is NOT the production rollback path for the schema 44 -> 46 release. The
-- immediate rollback is to keep the database at clean schema 46 and roll the
-- APPLICATION back to the schema-46-compatible old-behaviour artifact, whose
-- truthful supported range is 44..46 (see deploy/schema46/). No database
-- downgrade is involved, so none of the state above is lost.
--
-- Database restore, or an explicitly authorized future downgrade, is a separate
-- recovery decision made with its own evidence. There is deliberately no generic
-- production DOWN command and no automatic 46 -> 44 downgrade.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM media_auto_enhancement_recovery
         WHERE state IN ('SCHEDULED', 'EXECUTING', 'MANUAL_PENDING')
    ) THEN
        RAISE EXCEPTION 'cannot roll back 0045: automatic enhancement recovery intents or accepted manual recovery requests are outstanding; let them settle or clear them deliberately before rolling back';
    END IF;
END $$;

-- processing_attempts is append-only, so the attribution column is dropped
-- rather than cleared. Dropping it loses which attempts were automatic; the
-- attempts themselves, their kinds, their outcomes and their operation identities
-- are untouched.
ALTER TABLE processing_attempts
    DROP COLUMN auto_recovery_intent_id;

DROP TABLE media_auto_enhancement_recovery;
