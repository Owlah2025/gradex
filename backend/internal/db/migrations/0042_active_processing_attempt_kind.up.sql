-- A processing claim must carry its kind before its worker can disappear.
-- Migration requires quiesced media work: schema 41 cannot distinguish an
-- existing claimed PLAYABLE FULL operation from an ENHANCEMENT operation.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM media_asset_versions
        WHERE state IN ('SCANNING', 'PROCESSING') OR work_claim_token IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'cannot apply 0042 while media work is active; quiesce media producers and settle claims first';
    END IF;
END $$;

ALTER TABLE media_asset_versions
    ADD COLUMN active_processing_attempt_kind media_processing_attempt_kind;

-- processing_attempt_token is retained as terminal progress metadata, including
-- on READY, so it cannot be equivalent to the active kind. A processing claim
-- requires both, bound to the same operation; scan claims require neither.
ALTER TABLE media_asset_versions
    ADD CONSTRAINT media_asset_versions_active_processing_kind_coherent CHECK (
        (active_processing_attempt_kind IS NULL AND NOT (
            state IN ('PROCESSING', 'PLAYABLE') AND work_claim_token IS NOT NULL
        ))
        OR (
            active_processing_attempt_kind IS NOT NULL
            AND state IN ('PROCESSING', 'PLAYABLE')
            AND (state <> 'PROCESSING' OR active_processing_attempt_kind = 'FULL')
            AND work_claim_token IS NOT NULL
            AND processing_attempt_token IS NOT NULL
            AND processing_attempt_token = work_claim_token
        )
    );
