-- Optional reference on the approved revision graph; no legacy backfill.
ALTER TABLE course_revisions ADD COLUMN thumbnail_asset_version_id UUID
    REFERENCES media_asset_versions(id);
CREATE INDEX course_revisions_thumbnail_idx ON course_revisions(thumbnail_asset_version_id)
    WHERE thumbnail_asset_version_id IS NOT NULL;

ALTER TABLE media_assets DROP CONSTRAINT media_assets_preview_origin_revision_kind_check;
ALTER TABLE media_assets ADD CONSTRAINT media_assets_preview_origin_revision_kind_check CHECK (
    (preview_origin_revision_id IS NULL OR kind IN ('PREVIEW', 'THUMBNAIL'))
    AND (kind NOT IN ('PREVIEW', 'THUMBNAIL') OR lesson_id IS NULL)
    AND (kind <> 'THUMBNAIL' OR preview_origin_revision_id IS NOT NULL)
);

CREATE UNIQUE INDEX media_thumbnail_single_version ON media_asset_versions(logical_asset_id) WHERE kind='THUMBNAIL';

CREATE TABLE media_thumbnail_variants (
    asset_version_id UUID PRIMARY KEY REFERENCES media_asset_versions(id),
    source_object_version TEXT NOT NULL,
    source_sha256 TEXT NOT NULL CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
    width INTEGER NOT NULL CHECK (width BETWEEN 800 AND 8192),
    height INTEGER NOT NULL CHECK (height BETWEEN 450 AND 8192),
    card_key TEXT NOT NULL UNIQUE,
    large_key TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (width::bigint * height <= 16000000)
);
CREATE FUNCTION thumbnail_variants_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'thumbnail processing evidence is immutable' USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER thumbnail_variants_immutable BEFORE UPDATE OR DELETE ON media_thumbnail_variants
    FOR EACH ROW EXECUTE FUNCTION thumbnail_variants_immutable();

-- Tombstones survive deletion failures and block later attachment. Evidence remains.
CREATE TABLE media_thumbnail_cleanup (
    asset_version_id UUID PRIMARY KEY REFERENCES media_asset_versions(id),
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE FUNCTION revision_thumbnail_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.thumbnail_asset_version_id IS DISTINCT FROM OLD.thumbnail_asset_version_id
        AND OLD.state NOT IN ('DRAFT', 'CHANGES_REQUESTED') THEN
        RAISE EXCEPTION 'only editable candidate thumbnails may change' USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.thumbnail_asset_version_id IS NOT NULL THEN
        PERFORM 1 FROM media_assets ma
        JOIN media_asset_versions v ON v.logical_asset_id=ma.id
        JOIN media_thumbnail_variants t ON t.asset_version_id=v.id
        WHERE v.id=NEW.thumbnail_asset_version_id AND ma.course_id=NEW.course_id
            AND ma.kind='THUMBNAIL' AND v.kind='THUMBNAIL' AND v.state='READY'
            AND ma.retired_at IS NULL
        FOR SHARE OF ma;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'invalid thumbnail reference' USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER revision_thumbnail_guard BEFORE INSERT OR UPDATE OF thumbnail_asset_version_id ON course_revisions
    FOR EACH ROW EXECUTE FUNCTION revision_thumbnail_guard();

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
    IF NEW.kind <> 'THUMBNAIL' AND NEW.state IN ('PROCESSING', 'READY')
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

    IF NEW.kind = 'THUMBNAIL' AND NEW.state = 'READY' AND NOT EXISTS (
        SELECT 1 FROM media_thumbnail_variants t
        WHERE t.asset_version_id = NEW.id
          AND t.source_object_version = NEW.storage_object_version
          AND t.source_sha256 = NEW.sha256_hex
          AND NEW.size_bytes BETWEEN 1 AND 5242880
          AND NEW.content_type IN ('image/jpeg', 'image/png', 'image/webp')
    ) THEN
        RAISE EXCEPTION 'thumbnail requires exact-source raster processing evidence'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
