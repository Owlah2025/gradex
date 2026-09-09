DROP INDEX IF EXISTS media_asset_versions_expired_work_lease_idx;

ALTER TABLE media_asset_versions
    DROP CONSTRAINT IF EXISTS media_asset_versions_failure_category_known,
    DROP CONSTRAINT IF EXISTS media_asset_versions_processing_attempt_count_non_negative,
    DROP CONSTRAINT IF EXISTS media_asset_versions_scan_attempt_count_non_negative,
    DROP CONSTRAINT IF EXISTS media_asset_versions_work_claim_coherent,
    DROP COLUMN IF EXISTS last_failure_category,
    DROP COLUMN IF EXISTS processing_attempt_count,
    DROP COLUMN IF EXISTS scan_attempt_count,
    DROP COLUMN IF EXISTS work_lease_expires_at,
    DROP COLUMN IF EXISTS work_claimed_at,
    DROP COLUMN IF EXISTS work_claim_token;
