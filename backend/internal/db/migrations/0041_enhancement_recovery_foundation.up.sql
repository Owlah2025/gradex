-- Phase 3C-A / enhancement recovery foundation.
--
-- This migration adds durable representation only. It produces no new attempt
-- kind, writes no new row shape by itself, and changes no runtime recovery,
-- publication, or delivery behaviour. The producers arrive in a later phase.
--
-- WHY THIS IS REQUIRED
--
-- There is a reachable crash window today. The last missing rendition commits,
-- the worker dies before the READY transaction, and the Asset Version stays
-- PLAYABLE holding a complete canonical ladder. Stale recovery then makes the
-- old operation terminal and clears the claim, so a later recovery must be able
-- to finalize that asset without re-encoding anything.
--
-- Schema 40 cannot represent that finalization honestly:
--
--   * `processing_attempt_result_coherent` requires rendition_count > 0 for
--     every SUCCEEDED attempt, and a finalization produces zero new renditions;
--   * processing_attempts is append-only, so the earlier FAILED attempt can
--     never be rewritten SUCCEEDED;
--   * `media_asset_versions_successful_processing_fk` and the immutability
--     trigger both require successful_processing_attempt_id to point at a
--     SUCCEEDED attempt of this exact version;
--   * recording rendition_count = <full ladder size> for an attempt that
--     produced nothing would be false audit evidence about storage output.
--
-- So the attempt kind becomes explicit and the result-coherence rule is
-- discriminated by it.

-- A NEW enum, deliberately separate from media_processing_state. The state enum
-- keeps exactly SUCCEEDED and FAILED: an attempt kind is not an attempt
-- outcome, and merging them would make every existing coherence rule ambiguous.
CREATE TYPE media_processing_attempt_kind AS ENUM ('FULL', 'ENHANCEMENT', 'FINALIZATION');

COMMENT ON TYPE media_processing_attempt_kind IS
    'What a processing attempt was asked to do. FULL encodes the whole expected ladder, which is every attempt that exists at schema 41. ENHANCEMENT encodes only the rungs a PLAYABLE asset is still missing. FINALIZATION encodes nothing and exists to prove an already-complete canonical ladder was verified and accepted.';

-- Every existing row is a whole-ladder attempt, so FULL is the only honest
-- value for history and the DEFAULT resolves them without a data rewrite.
ALTER TABLE processing_attempts
    ADD COLUMN attempt_kind media_processing_attempt_kind NOT NULL DEFAULT 'FULL';

-- `output_prefix` is already column-level nullable (0012), so FINALIZATION
-- needs no column alteration. Nullability there has always been governed by the
-- coherence CHECK rather than by the column, and it stays that way below.
ALTER TABLE processing_attempts
    DROP CONSTRAINT processing_attempt_result_coherent;

-- The schema-40 rule, split by attempt kind. The FAILED arm is unchanged
-- verbatim, and the FULL/ENHANCEMENT success arm is the unchanged schema-40
-- success rule: an attempt that really produced storage output must still prove
-- a positive rendition count and a non-empty output prefix. Only FINALIZATION
-- is new, and it is constrained in the opposite direction — it must have
-- produced nothing — so it can never impersonate an attempt that wrote objects.
ALTER TABLE processing_attempts
    ADD CONSTRAINT processing_attempt_result_coherent CHECK (
        (state = 'SUCCEEDED' AND attempt_kind IN ('FULL', 'ENHANCEMENT')
            AND rendition_count > 0 AND error_reason IS NULL
            AND output_prefix IS NOT NULL AND length(trim(output_prefix)) > 0
            AND trusted_duration_ms IS NOT NULL)
        OR (state = 'SUCCEEDED' AND attempt_kind = 'FINALIZATION'
            AND rendition_count = 0 AND error_reason IS NULL
            AND output_prefix IS NULL
            AND trusted_duration_ms IS NOT NULL)
        OR (state = 'FAILED' AND error_reason IS NOT NULL AND length(trim(error_reason)) > 0)
    );

-- Explicit provenance for each canonical rendition row.
--
-- Nullable on purpose. Every rendition row that exists at schema 41 predates
-- this column, and an append-only table cannot be backfilled: `UPDATE` on
-- video_renditions raises through `video_renditions_append_only`. Requiring a
-- value would therefore mean either destroying and rewriting canonical delivery
-- evidence or refusing the migration outright.
--
-- Deliberately NOT a foreign key to processing_attempts. The rendition row is
-- committed while the attempt is still running, so the terminal attempt row it
-- names does not exist yet — a reference would invert the real commit order and
-- make progressive persistence impossible.
ALTER TABLE video_renditions
    ADD COLUMN processing_operation_id TEXT;

ALTER TABLE video_renditions
    ADD CONSTRAINT video_rendition_processing_operation_present CHECK (
        processing_operation_id IS NULL OR length(trim(processing_operation_id)) > 0
    );

COMMENT ON COLUMN video_renditions.processing_operation_id IS
    'The processing operation whose transaction committed this canonical row. NULL means the row predates schema 41; storage_object_key still carries a one-way hash of the operation that wrote the objects, so a known operation can be verified against a legacy row even though it cannot be read back out of one. Future recovery code that needs to attribute a legacy row must define an explicit compatibility policy rather than assume NULL is impossible.';

-- FUTURE SEMANTICS, recorded here so the widening is never mistaken for a
-- silent behaviour change when it happens:
--
--   * `successful_processing_attempt_id` keeps its schema-40 meaning at this
--     version. Every READY asset points at its ordinary SUCCEEDED FULL attempt
--     because no other kind is produced yet.
--   * When finalization ships, that column is intended to point at the attempt
--     that COMPLETED the asset, which may be a FINALIZATION attempt that
--     produced no renditions of its own. The `state = 'SUCCEEDED'` requirement
--     in `media_asset_versions_enforce_immutability` is already satisfied by
--     such a row, so that widening needs no further trigger change — but it is
--     a semantic change to what the column asserts, and it belongs to the phase
--     that introduces the producer, not to this one.
