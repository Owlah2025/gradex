-- Schema 41 cannot retain the kind of an in-flight processing claim. Terminal
-- attempts remain in processing_attempts; this guard protects active work only.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM media_asset_versions
        WHERE state = 'PROCESSING'
           OR (state = 'PLAYABLE' AND work_claim_token IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'cannot roll back 0042 while a processing operation is active';
    END IF;
END $$;

ALTER TABLE media_asset_versions
    DROP CONSTRAINT media_asset_versions_active_processing_kind_coherent,
    DROP COLUMN active_processing_attempt_kind;
