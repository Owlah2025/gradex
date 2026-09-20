-- D-105: Prepare Schema 40 playable state foundation

ALTER TABLE media_asset_versions
    DROP CONSTRAINT media_asset_versions_work_claim_coherent;

ALTER TABLE media_asset_versions
    ADD CONSTRAINT media_asset_versions_work_claim_coherent CHECK (
        (work_claim_token IS NULL AND work_claimed_at IS NULL AND work_lease_expires_at IS NULL)
        OR (
            work_claim_token IS NOT NULL AND length(trim(work_claim_token)) > 0
            AND work_claimed_at IS NOT NULL
            AND work_lease_expires_at IS NOT NULL
            AND work_lease_expires_at > work_claimed_at
            AND state IN ('SCANNING', 'PROCESSING', 'PLAYABLE')
        )
    );

DROP INDEX IF EXISTS media_asset_versions_expired_work_lease_idx;

CREATE INDEX media_asset_versions_expired_work_lease_idx
    ON media_asset_versions (work_lease_expires_at, id)
    WHERE state IN ('SCANNING', 'PROCESSING', 'PLAYABLE');

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
        OR (OLD.state = 'QUARANTINED' AND NEW.state = 'READY' AND NEW.kind = 'THUMBNAIL')
        OR (OLD.state = 'QUARANTINED' AND NEW.state = 'SCANNING')
        OR (OLD.state = 'SCANNING' AND NEW.state IN ('SCAN_PASSED', 'SCAN_FAILED', 'SCAN_ERROR'))
        OR (OLD.state = 'SCAN_FAILED' AND NEW.state = 'QUARANTINED')
        OR (OLD.state = 'SCAN_ERROR' AND NEW.state = 'QUARANTINED')
        OR (OLD.state = 'SCAN_PASSED' AND NEW.state = 'PROCESSING')
        OR (OLD.state = 'SCAN_PASSED' AND NEW.state = 'READY' AND NEW.kind <> 'VIDEO')
        OR (OLD.state = 'QUARANTINED' AND NEW.state = 'VALIDATED')
        OR (OLD.state = 'VALIDATED' AND NEW.state = 'PROCESSING')
        -- A validated PREVIEW owes the same FFmpeg evidence a Lesson video
        -- owes, so it may not take the direct VALIDATED -> READY edge that a
        -- validated PDF or DOCX Lesson Resource takes.
        OR (OLD.state = 'VALIDATED' AND NEW.state = 'READY' AND NEW.kind NOT IN ('VIDEO', 'PREVIEW'))
        OR (OLD.state = 'VALIDATED' AND NEW.state = 'PROCESS_FAILED')
        OR (OLD.state = 'PROCESSING' AND NEW.state IN ('PLAYABLE', 'READY', 'PROCESS_FAILED'))
        OR (OLD.state = 'PLAYABLE' AND NEW.state = 'READY')
        OR (OLD.state = 'PROCESS_FAILED' AND NEW.state = 'QUARANTINED')
    ) THEN
        RAISE EXCEPTION 'invalid media asset version state transition % -> %', OLD.state, NEW.state
            USING ERRCODE = 'check_violation';
    END IF;

    -- SCAN_PASSED remains scan-only. Validation evidence never satisfies it,
    -- so no query or operator reading that state can be misled about what was
    -- actually performed.
    IF NEW.state = 'SCAN_PASSED' AND NEW.successful_scan_attempt_id IS NULL THEN
        RAISE EXCEPTION 'media version % lacks successful exact-version scan evidence', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.state = 'VALIDATED' AND NEW.successful_validation_attempt_id IS NULL THEN
        RAISE EXCEPTION 'media version % lacks successful exact-version validation evidence', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    -- Deliverability requires one legitimate provenance or the other. Neither
    -- an Admin action, a mode switch, nor a direct UPDATE can reach these
    -- states without evidence bound to these exact bytes.
    IF NEW.kind <> 'THUMBNAIL' AND NEW.state IN ('PROCESSING', 'PLAYABLE', 'READY')
        AND NEW.successful_scan_attempt_id IS NULL
        AND NEW.successful_validation_attempt_id IS NULL
    THEN
        RAISE EXCEPTION 'media version % lacks successful exact-version scan or validation evidence', OLD.id
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
    -- A PASSED attempt is not automatically evidence about *these* bytes. The
    -- attempt records its own account of what it inspected — checksum, actual
    -- size, declared type, the profile it was performed under, and the
    -- configured bound it was checked against — so provenance requires every
    -- one of those to agree with the Asset Version it is attached to, and the
    -- bound to agree with the upload intent this upload was admitted under.
    IF NEW.successful_validation_attempt_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM validation_attempts va
        JOIN upload_intents ui ON ui.asset_version_id = va.asset_version_id
        WHERE va.id = NEW.successful_validation_attempt_id
          AND va.asset_version_id = NEW.id
          AND va.storage_object_version = NEW.storage_object_version
          AND va.outcome = 'PASSED'
          AND va.profile = 'D-088-TRUSTED-INSTRUCTOR'
          AND va.sha256_hex = NEW.sha256_hex
          AND va.verified_size_bytes = NEW.size_bytes
          AND lower(va.declared_content_type) = lower(NEW.content_type)
          AND va.max_size_bytes = ui.max_size_bytes
    ) THEN
        RAISE EXCEPTION 'media version % lacks a matching successful exact-version validation attempt: the asset, object version, PASSED outcome, D-088 profile, checksum, verified size, declared type, and configured bound must all agree', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.state = 'READY'
        AND (
            NEW.kind = 'VIDEO'
            OR (NEW.kind = 'PREVIEW' AND NEW.successful_validation_attempt_id IS NOT NULL)
        )
        AND (NEW.successful_processing_attempt_id IS NULL OR NEW.trusted_duration_ms IS NULL)
    THEN
        RAISE EXCEPTION 'media version % (kind %) lacks successful trusted processing evidence', OLD.id, NEW.kind
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.successful_processing_attempt_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM processing_attempts pa
        WHERE pa.id = NEW.successful_processing_attempt_id
          AND pa.asset_version_id = NEW.id
          AND pa.state = 'SUCCEEDED'
          AND pa.trusted_duration_ms = NEW.trusted_duration_ms
    ) THEN
        RAISE EXCEPTION 'media version % lacks matching successful transcode evidence', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
