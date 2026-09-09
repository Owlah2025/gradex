-- D-103: durable ownership for scan and processing work.
--
-- Asset state alone cannot distinguish a live worker from one that died after
-- committing SCANNING or PROCESSING.  These additive columns bind an in-flight
-- state to one opaque work identity and a bounded lease.  Existing rows remain
-- NULL and compatible; the recovery pass treats a legacy in-flight NULL lease
-- as stale, while every other existing state (especially READY) is unchanged.

ALTER TABLE media_asset_versions
    ADD COLUMN work_claim_token TEXT,
    ADD COLUMN work_claimed_at TIMESTAMPTZ,
    ADD COLUMN work_lease_expires_at TIMESTAMPTZ,
    ADD COLUMN scan_attempt_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN processing_attempt_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN last_failure_category TEXT,

    ADD CONSTRAINT media_asset_versions_work_claim_coherent CHECK (
        (work_claim_token IS NULL AND work_claimed_at IS NULL AND work_lease_expires_at IS NULL)
        OR (
            work_claim_token IS NOT NULL AND length(trim(work_claim_token)) > 0
            AND work_claimed_at IS NOT NULL
            AND work_lease_expires_at IS NOT NULL
            AND work_lease_expires_at > work_claimed_at
            AND state IN ('SCANNING', 'PROCESSING')
        )
    ),
    ADD CONSTRAINT media_asset_versions_scan_attempt_count_non_negative
        CHECK (scan_attempt_count >= 0),
    ADD CONSTRAINT media_asset_versions_processing_attempt_count_non_negative
        CHECK (processing_attempt_count >= 0),
    ADD CONSTRAINT media_asset_versions_failure_category_known CHECK (
        last_failure_category IS NULL OR last_failure_category IN (
            'INVALID_MEDIA', 'STORAGE_UNAVAILABLE',
            'PROCESS_TIMEOUT', 'TRANSCODE_FAILED', 'SCAN_REJECTED',
            'SCAN_UNAVAILABLE', 'WORKER_INTERRUPTED'
        )
    );

CREATE INDEX media_asset_versions_expired_work_lease_idx
    ON media_asset_versions (work_lease_expires_at, id)
    WHERE state IN ('SCANNING', 'PROCESSING');
