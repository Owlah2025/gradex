-- Revert the Phase 3C-A enhancement recovery foundation.
--
-- ROLLBACK FLOOR
--
-- Schema 41 can be downgraded to schema 40 only while all processing_attempts
-- remain attempt_kind = FULL. Once any ENHANCEMENT or FINALIZATION attempt row
-- is persisted, regardless of outcome, the schema-40 rollback floor is closed
-- and the floor becomes a schema-41-compatible application revision instead.
--
-- The boundary is the FIRST non-FULL row, not the first successful one. A
-- FAILED ENHANCEMENT is refused too, even though schema 40's restored
-- `processing_attempt_result_coherent` would accept its remaining columns:
-- schema 40 has no attempt_kind at all, so dropping the column would leave the
-- row in place while silently reinterpreting it as a legacy whole-ladder
-- attempt. Losing what the worker was asked to do is not a lesser failure than
-- losing the row, so the refusal below is deliberately broader than the
-- coherence constraint requires.

-- Refuse before any destructive DDL, in the style of 0021. Nothing below runs
-- if the data cannot be honestly represented by the restored schema.
DO $$
BEGIN
    -- Hard unrepresentability: a successful attempt that schema 40's restored
    -- `processing_attempt_result_coherent` would reject. Rewriting
    -- rendition_count or fabricating an output_prefix to make it fit would be
    -- inventing audit evidence about storage output that was never produced.
    IF EXISTS (
        SELECT 1 FROM processing_attempts
        WHERE state = 'SUCCEEDED'
          AND NOT (rendition_count > 0 AND error_reason IS NULL
                   AND output_prefix IS NOT NULL AND length(trim(output_prefix)) > 0
                   AND trusted_duration_ms IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'cannot roll back 0041: successful processing attempts exist that schema 40 cannot represent (zero-rendition finalization); retain schema 41 or remove that evidence through an approved migration';
    END IF;
    -- Evidence loss: a non-FULL attempt whose outcome schema 40 happens to
    -- accept, such as a FAILED enhancement. Dropping attempt_kind would keep
    -- the row but silently relabel it a whole-ladder attempt, which is a
    -- different claim about what the worker was asked to do.
    IF EXISTS (SELECT 1 FROM processing_attempts WHERE attempt_kind <> 'FULL') THEN
        RAISE EXCEPTION 'cannot roll back 0041: enhancement or finalization processing attempts exist and schema 40 would silently record them as whole-ladder attempts; retain schema 41 or archive that evidence through an approved migration';
    END IF;
END;
$$;

-- video_renditions.processing_operation_id is dropped without a refusal on
-- purpose. Unlike an attempt row it is not the only record of its fact: the
-- rendition's storage_object_key still embeds a one-way hash of the operation
-- that wrote the objects, so the association remains verifiable against a known
-- operation ID. Refusing here would instead make rollback impossible from the
-- moment the first rendition is written after 0041 — which is exactly the
-- window this floor exists to keep open.
ALTER TABLE video_renditions
    DROP CONSTRAINT video_rendition_processing_operation_present;

ALTER TABLE video_renditions
    DROP COLUMN processing_operation_id;

ALTER TABLE processing_attempts
    DROP CONSTRAINT processing_attempt_result_coherent;

ALTER TABLE processing_attempts
    DROP COLUMN attempt_kind;

DROP TYPE media_processing_attempt_kind;

-- The authoritative schema-40 definition, restored exactly as 0012 declared it.
ALTER TABLE processing_attempts
    ADD CONSTRAINT processing_attempt_result_coherent CHECK (
        (state = 'SUCCEEDED' AND rendition_count > 0 AND error_reason IS NULL
            AND output_prefix IS NOT NULL AND length(trim(output_prefix)) > 0
            AND trusted_duration_ms IS NOT NULL)
        OR (state = 'FAILED' AND error_reason IS NOT NULL AND length(trim(error_reason)) > 0)
    );
