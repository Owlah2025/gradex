DROP TRIGGER IF EXISTS entitlement_adjustments_append_only ON entitlement_adjustments;
DROP TABLE IF EXISTS entitlement_adjustments;
DROP INDEX IF EXISTS entitlements_student_course_expiry_idx;
DROP INDEX IF EXISTS entitlements_one_active_student_course;
DROP TABLE IF EXISTS entitlements;

ALTER TABLE IF EXISTS course_lesson_identities
    DROP COLUMN IF EXISTS retired_at;
ALTER TABLE IF EXISTS course_section_identities
    DROP COLUMN IF EXISTS retired_at;

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

-- D7 outbox rows are not valid under the pre-D7 source-module constraint.
-- Outbox evidence is append-only while D7 is active, so temporarily remove
-- those append-only triggers only to delete D7-owned rows during rollback,
-- then restore the pre-D7 append-only protections before returning control.
DROP TRIGGER IF EXISTS outbox_protected_payloads_append_only ON outbox_protected_payloads;
DROP TRIGGER IF EXISTS outbox_events_append_only ON outbox_events;
DELETE FROM outbox_protected_payloads
WHERE event_id IN (
    SELECT id FROM outbox_events WHERE source_module = 'MEDIA_AND_ASSETS'
);
DELETE FROM outbox_events WHERE source_module = 'MEDIA_AND_ASSETS';
CREATE TRIGGER outbox_events_append_only
    BEFORE UPDATE OR DELETE ON outbox_events
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();
CREATE TRIGGER outbox_protected_payloads_append_only
    BEFORE UPDATE OR DELETE ON outbox_protected_payloads
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();

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
