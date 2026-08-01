DROP TRIGGER IF EXISTS entitlement_adjustments_append_only ON entitlement_adjustments;
DROP TABLE IF EXISTS entitlement_adjustments;
DROP INDEX IF EXISTS entitlements_student_course_expiry_idx;
DROP INDEX IF EXISTS entitlements_one_active_student_course;
DROP TABLE IF EXISTS entitlements;

DROP TABLE IF EXISTS media_outbox_dispatches;

DROP TRIGGER IF EXISTS video_renditions_append_only ON video_renditions;
DROP TRIGGER IF EXISTS processing_attempts_append_only ON processing_attempts;
DROP TRIGGER IF EXISTS scan_attempts_append_only ON scan_attempts;
DROP TRIGGER IF EXISTS media_asset_versions_immutable ON media_asset_versions;
DROP FUNCTION IF EXISTS media_asset_versions_enforce_immutability();

DROP TABLE IF EXISTS legacy_media_mappings;
DROP INDEX IF EXISTS video_renditions_asset_idx;
DROP TABLE IF EXISTS video_renditions;
ALTER TABLE IF EXISTS media_asset_versions
    DROP CONSTRAINT IF EXISTS media_asset_versions_successful_scan_fk,
    DROP CONSTRAINT IF EXISTS media_asset_versions_successful_processing_fk;
DROP INDEX IF EXISTS processing_attempts_asset_idx;
DROP TABLE IF EXISTS processing_attempts;
DROP INDEX IF EXISTS scan_attempts_asset_idx;
DROP TABLE IF EXISTS scan_attempts;
DROP INDEX IF EXISTS media_callback_asset_idx;
DROP TABLE IF EXISTS media_callback_receipts;
DROP TABLE IF EXISTS upload_intents;
DROP INDEX IF EXISTS media_asset_versions_state_idx;
DROP INDEX IF EXISTS media_asset_versions_logical_idx;
DROP TABLE IF EXISTS media_asset_versions;
DROP INDEX IF EXISTS media_assets_lesson_idx;
DROP INDEX IF EXISTS media_assets_course_idx;
DROP INDEX IF EXISTS media_assets_owner_idx;
DROP TABLE IF EXISTS media_assets;

ALTER TABLE outbox_events
    DROP CONSTRAINT outbox_events_source_module;

ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_source_module CHECK (
        source_module IN ('IDENTITY_AND_ACCESS', 'CATALOG_AND_AUTHORING')
    );

DROP TYPE IF EXISTS media_processing_state;
DROP TYPE IF EXISTS media_scan_outcome;
DROP TYPE IF EXISTS media_asset_version_state;
DROP TYPE IF EXISTS media_asset_visibility;
DROP TYPE IF EXISTS media_asset_kind;
