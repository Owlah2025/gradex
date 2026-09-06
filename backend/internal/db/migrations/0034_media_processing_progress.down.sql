-- Progress observations are derived, not evidence: dropping them loses no
-- record of what was scanned, validated, or processed.
ALTER TABLE media_asset_versions
    DROP CONSTRAINT media_asset_versions_processing_observation_coherent,
    DROP CONSTRAINT media_asset_versions_processing_percent_bounded,
    DROP CONSTRAINT media_asset_versions_processing_stage_known,
    DROP COLUMN processing_attempt_token,
    DROP COLUMN processing_updated_at,
    DROP COLUMN processing_progress_percent,
    DROP COLUMN processing_stage;
