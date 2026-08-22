-- D-088 trusted-Instructor exact-version validation evidence and enforcement.
--
-- D-088 defers production malware scanning for one narrow launch profile: MP4
-- Lesson video and PDF/DOCX Lesson Resources uploaded by an ACTIVE vetted
-- Instructor. Those objects still enter private quarantine and must pass
-- exact-version validation before they may progress.
--
-- The requirement this migration exists to satisfy is that the *database*, not
-- only the service, still refuses a READY Asset Version with no legitimate
-- provenance. It therefore does not relax the existing scan requirement. It
-- adds a second, separately evidenced path and keeps both exact-version bound:
--
--   scanner path   QUARANTINED -> SCANNING  -> SCAN_PASSED -> PROCESSING/READY
--   D-088 path     QUARANTINED -> VALIDATED               -> PROCESSING/READY
--
-- Nothing here records that malware inspection occurred. Validation evidence
-- has its own table, its own outcome type, and its own provenance column, so a
-- reader can always tell the two paths apart, and no query that asks for scan
-- evidence can be satisfied by validation evidence.

CREATE TYPE media_validation_outcome AS ENUM ('PASSED', 'FAILED');

-- Immutable, append-only evidence that one exact stored object version passed
-- the D-088 validation. `storage_object_version` participates in the foreign
-- key, so a pass can never transfer to replacement bytes.
CREATE TABLE validation_attempts (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_version_id       UUID NOT NULL REFERENCES media_asset_versions (id),
    attempt_number         INTEGER NOT NULL,
    work_id                TEXT NOT NULL,
    storage_object_version TEXT NOT NULL,
    outcome                media_validation_outcome NOT NULL,
    validator_identity     TEXT NOT NULL,
    profile                TEXT NOT NULL,
    declared_content_type  TEXT NOT NULL,
    verified_size_bytes    BIGINT NOT NULL,
    max_size_bytes         BIGINT NOT NULL,
    sha256_hex             TEXT NOT NULL,
    reason                 TEXT,
    validated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT validation_attempt_number_positive CHECK (attempt_number >= 1),
    CONSTRAINT validation_attempt_work_present CHECK (length(trim(work_id)) > 0),
    CONSTRAINT validation_attempt_validator_present CHECK (length(trim(validator_identity)) > 0),
    CONSTRAINT validation_attempt_profile_present CHECK (length(trim(profile)) > 0),
    CONSTRAINT validation_attempt_declared_type_present CHECK (length(trim(declared_content_type)) > 0),
    CONSTRAINT validation_attempt_size_positive CHECK (verified_size_bytes > 0),
    CONSTRAINT validation_attempt_within_bound CHECK (
        max_size_bytes > 0 AND verified_size_bytes <= max_size_bytes
    ),
    CONSTRAINT validation_attempt_sha256_format CHECK (sha256_hex ~ '^[0-9a-f]{64}$'),
    CONSTRAINT validation_attempt_reason_for_failure CHECK (
        outcome = 'PASSED' OR (reason IS NOT NULL AND length(trim(reason)) > 0)
    ),
    CONSTRAINT validation_attempt_exact_version_attempt_unique
        UNIQUE (asset_version_id, storage_object_version, attempt_number),
    CONSTRAINT validation_attempt_work_unique UNIQUE (work_id),
    CONSTRAINT validation_attempt_id_asset_version_unique UNIQUE (id, asset_version_id)
);

ALTER TABLE validation_attempts
    ADD CONSTRAINT validation_attempt_exact_version_fk
        FOREIGN KEY (asset_version_id, storage_object_version)
        REFERENCES media_asset_versions (id, storage_object_version);

CREATE INDEX validation_attempts_asset_idx
    ON validation_attempts (asset_version_id, validated_at DESC);

ALTER TABLE media_asset_versions
    ADD COLUMN successful_validation_attempt_id UUID;

ALTER TABLE media_asset_versions
    ADD CONSTRAINT media_asset_versions_successful_validation_fk
        FOREIGN KEY (successful_validation_attempt_id, id)
        REFERENCES validation_attempts (id, asset_version_id);

CREATE TRIGGER validation_attempts_append_only
    BEFORE UPDATE OR DELETE ON validation_attempts
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();

-- The upload intent is the only durable record of the configured size bound an
-- upload was admitted against, and D-088 validation evidence is bound to it
-- below. That binding proves nothing if the intent can be edited afterwards to
-- agree with whatever the evidence claims, so the intent's terms are fixed at
-- creation. Completion is the single field that legitimately moves, and only
-- once: it is the record that this exact upload finished, not a mutable status.
CREATE FUNCTION upload_intents_enforce_immutability() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'upload intent % is immutable and cannot be deleted', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.asset_version_id IS DISTINCT FROM OLD.asset_version_id
        OR NEW.expected_object_key IS DISTINCT FROM OLD.expected_object_key
        OR NEW.expected_content_type IS DISTINCT FROM OLD.expected_content_type
        OR NEW.expected_size_bytes IS DISTINCT FROM OLD.expected_size_bytes
        OR NEW.max_size_bytes IS DISTINCT FROM OLD.max_size_bytes
        OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION 'upload intent % terms are immutable after creation', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF OLD.completed_at IS NOT NULL AND (
        NEW.completed_at IS DISTINCT FROM OLD.completed_at
        OR NEW.completion_fingerprint IS DISTINCT FROM OLD.completion_fingerprint
    ) THEN
        RAISE EXCEPTION 'upload intent % is already completed and its completion evidence is immutable', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER upload_intents_immutable
    BEFORE UPDATE OR DELETE ON upload_intents
    FOR EACH ROW EXECUTE FUNCTION upload_intents_enforce_immutability();

-- The 0012 lifecycle enforcement, extended with the D-088 path. Everything the
-- scanner path required is unchanged and still required; the additions are the
-- new transitions, the new provenance alternative, and the bounds that keep
-- the new alternative from being usable outside the D-088 profile.
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
        OR (OLD.state = 'VALIDATED' AND NEW.state = 'READY' AND NEW.kind <> 'VIDEO')
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

    -- The D-088 allowlist, enforced where it cannot be edited around. A public
    -- preview and a Lab Material can never carry validation provenance, and no
    -- content type outside the approved MP4/PDF/DOCX set can either, so both
    -- remain scanner-gated no matter what the application asks for.
    IF NEW.successful_validation_attempt_id IS NOT NULL AND NOT (
        (NEW.kind = 'VIDEO' AND lower(NEW.content_type) = 'video/mp4')
        OR (NEW.kind = 'RESOURCE' AND lower(NEW.content_type) IN (
            'application/pdf',
            'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
        ))
    ) THEN
        RAISE EXCEPTION 'media version % (kind %, type %) is outside the D-088 trusted-validation profile',
            OLD.id, NEW.kind, NEW.content_type
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
