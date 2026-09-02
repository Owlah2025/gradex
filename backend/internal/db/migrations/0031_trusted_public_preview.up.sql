-- D-096 extends the bounded D-088 trusted-Instructor profile to the one media
-- kind the production authoring journey still could not reach: an MP4 public
-- Course preview.
--
-- Nothing about the scanner path changes. A PREVIEW that holds scan provenance
-- behaves exactly as it did before this migration, including becoming READY
-- straight from SCAN_PASSED with no processing evidence.
--
-- Three things change, all inside the one lifecycle trigger:
--
--   1. `PREVIEW` + `video/mp4` may carry trusted-validation provenance. Every
--      other preview content type still cannot, and neither can a Lab Material.
--   2. A validated PREVIEW loses the direct VALIDATED -> READY edge. It must go
--      through PROCESSING, exactly as a validated Lesson video does.
--   3. A READY PREVIEW that reached deliverability on validation provenance
--      must hold successful processing evidence and a trusted duration.
--
-- The net effect is that a trusted public preview cannot become READY — and so
-- cannot become publicly readable — until real FFmpeg processing has succeeded
-- over the exact stored object version that validation proved.

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
        OR (OLD.state = 'QUARANTINED' AND NEW.state = 'VALIDATED')
        OR (OLD.state = 'VALIDATED' AND NEW.state = 'PROCESSING')
        -- A validated PREVIEW owes the same FFmpeg evidence a Lesson video
        -- owes, so it may not take the direct VALIDATED -> READY edge that a
        -- validated PDF or DOCX Lesson Resource takes.
        OR (OLD.state = 'VALIDATED' AND NEW.state = 'READY' AND NEW.kind NOT IN ('VIDEO', 'PREVIEW'))
        OR (OLD.state = 'VALIDATED' AND NEW.state = 'PROCESS_FAILED')
        OR (OLD.state = 'PROCESSING' AND NEW.state IN ('READY', 'PROCESS_FAILED'))
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
    IF NEW.state IN ('PROCESSING', 'READY')
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
    -- Without that, a forged or stale row whose columns all read as a
    -- legitimate pass could make an object deliverable that nothing verified.
    --
    -- `NEW.sha256_hex` is NULL until upload completion records it, so an Asset
    -- Version with no recorded checksum can never carry validation provenance:
    -- the comparison is NULL, the row does not match, and the state is refused.
    IF NEW.successful_validation_attempt_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM validation_attempts va
        JOIN upload_intents ui ON ui.asset_version_id = va.asset_version_id
        WHERE va.id = NEW.successful_validation_attempt_id
          AND va.asset_version_id = NEW.id
          AND va.storage_object_version = NEW.storage_object_version
          AND va.outcome = 'PASSED'
          -- The one canonical D-088 profile identifier, written by
          -- media.TrustedValidationProfile. Compared exactly: a near-miss is a
          -- different profile, not this one.
          AND va.profile = 'D-088-TRUSTED-INSTRUCTOR'
          AND va.sha256_hex = NEW.sha256_hex
          AND va.verified_size_bytes = NEW.size_bytes
          AND lower(va.declared_content_type) = lower(NEW.content_type)
          AND va.max_size_bytes = ui.max_size_bytes
    ) THEN
        RAISE EXCEPTION 'media version % lacks a matching successful exact-version validation attempt: the asset, object version, PASSED outcome, D-088 profile, checksum, verified size, declared type, and configured bound must all agree', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    -- The D-088 allowlist as amended by D-096, enforced where it cannot be
    -- edited around. An MP4 public preview now carries validation provenance
    -- on the same terms as MP4 Lesson video. A Lab Material, a non-MP4
    -- preview, and every content type outside the approved set still cannot,
    -- so they remain scanner-gated no matter what the application asks for.
    IF NEW.successful_validation_attempt_id IS NOT NULL AND NOT (
        (NEW.kind = 'VIDEO' AND lower(NEW.content_type) = 'video/mp4')
        OR (NEW.kind = 'PREVIEW' AND lower(NEW.content_type) = 'video/mp4')
        OR (NEW.kind = 'RESOURCE' AND lower(NEW.content_type) IN (
            'application/pdf',
            'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
        ))
    ) THEN
        RAISE EXCEPTION 'media version % (kind %, type %) is outside the D-088 trusted-validation profile',
            OLD.id, NEW.kind, NEW.content_type
            USING ERRCODE = 'check_violation';
    END IF;

    -- Trusted processing evidence is required for every READY Lesson video and
    -- for a READY PREVIEW that reached deliverability on validation provenance.
    -- A scanner-mode PREVIEW is unchanged: it holds scan provenance, owes no
    -- FFmpeg evidence, and still becomes READY straight from SCAN_PASSED.
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
