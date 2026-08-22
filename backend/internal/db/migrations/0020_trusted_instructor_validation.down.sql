-- Reverses D-088 trusted validation to the 0012 scanner-only enforcement. Any
-- Asset Version that reached PROCESSING or READY on validation provenance is
-- refused rather than silently relabelled as scanned: reversing this migration
-- means the deployment no longer accepts that evidence, so those bytes must
-- not stay deliverable on a claim the schema can no longer support.
DO $$
DECLARE
    validated_count BIGINT;
BEGIN
    SELECT count(*) INTO validated_count
    FROM media_asset_versions
    WHERE successful_validation_attempt_id IS NOT NULL
       OR state = 'VALIDATED';
    IF validated_count > 0 THEN
        RAISE EXCEPTION
            'cannot reverse D-088 trusted validation: % asset version(s) carry validation provenance', validated_count
            USING ERRCODE = 'restrict_violation';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION media_asset_versions_enforce_immutability() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.logical_asset_id IS DISTINCT FROM OLD.logical_asset_id
        OR NEW.kind IS DISTINCT FROM OLD.kind
        OR NEW.storage_object_key IS DISTINCT FROM OLD.storage_object_key
        OR NEW.content_type IS DISTINCT FROM OLD.content_type
        OR NEW.size_bytes IS DISTINCT FROM OLD.size_bytes
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION 'media asset version identity is immutable (version %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF OLD.state = 'READY' THEN
        RAISE EXCEPTION 'a READY media asset version is immutable (version %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF OLD.state IS DISTINCT FROM NEW.state AND NOT (
        (OLD.state = 'UPLOADED' AND NEW.state = 'QUARANTINED')
        OR (OLD.state = 'QUARANTINED' AND NEW.state = 'SCANNING')
        OR (OLD.state = 'SCANNING' AND NEW.state IN ('SCAN_PASSED', 'SCAN_FAILED', 'SCAN_ERROR'))
        OR (OLD.state = 'SCAN_FAILED' AND NEW.state = 'QUARANTINED')
        OR (OLD.state = 'SCAN_ERROR' AND NEW.state = 'QUARANTINED')
        OR (OLD.state = 'SCAN_PASSED' AND NEW.state = 'PROCESSING')
        OR (OLD.state = 'SCAN_PASSED' AND NEW.state = 'READY' AND NEW.kind <> 'VIDEO')
        OR (OLD.state = 'PROCESSING' AND NEW.state IN ('READY', 'PROCESS_FAILED'))
        OR (OLD.state = 'PROCESS_FAILED' AND NEW.state = 'QUARANTINED')
    ) THEN
        RAISE EXCEPTION 'invalid media asset version state transition % -> %', OLD.state, NEW.state
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.state IN ('SCAN_PASSED', 'PROCESSING', 'READY')
        AND NEW.successful_scan_attempt_id IS NULL
    THEN
        RAISE EXCEPTION 'media version % lacks successful exact-version scan evidence', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.successful_scan_attempt_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM scan_attempts sa
        WHERE sa.id = NEW.successful_scan_attempt_id
          AND sa.asset_version_id = NEW.id
          AND sa.storage_object_version = NEW.storage_object_version
          AND sa.outcome = 'PASSED'
    ) THEN
        RAISE EXCEPTION 'media version % lacks a matching successful exact-version scan attempt', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.state = 'READY' AND NEW.kind = 'VIDEO'
        AND (NEW.successful_processing_attempt_id IS NULL OR NEW.trusted_duration_ms IS NULL)
    THEN
        RAISE EXCEPTION 'video version % lacks successful trusted processing evidence', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.successful_processing_attempt_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM processing_attempts pa
        WHERE pa.id = NEW.successful_processing_attempt_id
          AND pa.asset_version_id = NEW.id
          AND pa.state = 'SUCCEEDED'
    ) THEN
        RAISE EXCEPTION 'media version % lacks a matching successful processing attempt', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    IF OLD.state <> 'UPLOADED'
        AND NEW.storage_object_version IS DISTINCT FROM OLD.storage_object_version
    THEN
        RAISE EXCEPTION 'media object version is immutable after upload completion (version %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.state <> 'UPLOADED'
        AND OLD.sha256_hex IS DISTINCT FROM NEW.sha256_hex
    THEN
        RAISE EXCEPTION 'media checksum is immutable after upload completion (version %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS validation_attempts_append_only ON validation_attempts;

DROP TRIGGER IF EXISTS upload_intents_immutable ON upload_intents;

DROP FUNCTION IF EXISTS upload_intents_enforce_immutability();

ALTER TABLE media_asset_versions
    DROP CONSTRAINT IF EXISTS media_asset_versions_successful_validation_fk;

ALTER TABLE media_asset_versions
    DROP COLUMN IF EXISTS successful_validation_attempt_id;

DROP TABLE IF EXISTS validation_attempts;

DROP TYPE IF EXISTS media_validation_outcome;
