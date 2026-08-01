-- S4 D7 media authority and the D8 entitlement record shape.
--
-- The legacy `videos` table is migration input only. New uploads, scan
-- evidence, processing evidence, and readiness decisions use the immutable
-- media tables below. No legacy row is promoted to READY without fresh scan
-- evidence; an old READY/PUBLISHED row is mapped as QUARANTINED instead.

CREATE TYPE media_asset_kind AS ENUM (
    'VIDEO', 'RESOURCE', 'LAB_MATERIAL', 'PREVIEW'
);

CREATE TYPE media_asset_visibility AS ENUM (
    'PROTECTED', 'PUBLIC_PREVIEW'
);

CREATE TYPE media_asset_version_state AS ENUM (
    'UPLOADED', 'QUARANTINED', 'SCANNING', 'SCAN_PASSED', 'SCAN_FAILED',
    'SCAN_ERROR', 'PROCESSING', 'READY', 'PROCESS_FAILED'
);

CREATE TYPE media_scan_outcome AS ENUM ('PASSED', 'FAILED', 'ERROR');

CREATE TYPE media_processing_state AS ENUM ('SUCCEEDED', 'FAILED');

ALTER TABLE outbox_events
    DROP CONSTRAINT outbox_events_source_module;

ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_source_module CHECK (
        source_module IN ('IDENTITY_AND_ACCESS', 'CATALOG_AND_AUTHORING', 'MEDIA_AND_ASSETS')
    );

CREATE TABLE media_outbox_dispatches (
    event_id      UUID PRIMARY KEY REFERENCES outbox_events (id),
    dispatched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE media_assets (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind               media_asset_kind NOT NULL,
    owner_account_id   UUID NOT NULL REFERENCES accounts (id),
    course_id          UUID NOT NULL REFERENCES courses (id),
    lesson_id          UUID,
    visibility         media_asset_visibility NOT NULL DEFAULT 'PROTECTED',
    retired_at         TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT media_assets_preview_visibility CHECK (
        (kind = 'PREVIEW' AND visibility = 'PUBLIC_PREVIEW')
        OR (kind <> 'PREVIEW' AND visibility = 'PROTECTED')
    ),
    CONSTRAINT media_assets_id_kind_unique UNIQUE (id, kind)
);

CREATE INDEX media_assets_owner_idx ON media_assets (owner_account_id, created_at DESC);
CREATE INDEX media_assets_course_idx ON media_assets (course_id, created_at DESC);
CREATE INDEX media_assets_lesson_idx ON media_assets (lesson_id, created_at DESC)
    WHERE lesson_id IS NOT NULL;

CREATE TABLE media_asset_versions (
    id                            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    logical_asset_id              UUID NOT NULL REFERENCES media_assets (id),
    kind                          media_asset_kind NOT NULL,
    state                         media_asset_version_state NOT NULL DEFAULT 'UPLOADED',
    storage_object_key            TEXT NOT NULL,
    storage_object_version        TEXT NOT NULL,
    content_type                  TEXT NOT NULL,
    size_bytes                    BIGINT NOT NULL,
    sha256_hex                    TEXT,
    trusted_duration_ms           BIGINT,
    successful_scan_attempt_id    UUID,
    successful_processing_attempt_id UUID,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT media_asset_versions_object_key_present CHECK (length(trim(storage_object_key)) > 0),
    CONSTRAINT media_asset_versions_object_version_present CHECK (length(trim(storage_object_version)) > 0),
    CONSTRAINT media_asset_versions_content_type_present CHECK (length(trim(content_type)) > 0),
    CONSTRAINT media_asset_versions_size_non_negative CHECK (size_bytes >= 0),
    CONSTRAINT media_asset_versions_duration_non_negative CHECK (
        trusted_duration_ms IS NULL OR trusted_duration_ms >= 0
    ),
    CONSTRAINT media_asset_versions_sha256_format CHECK (
        sha256_hex IS NULL OR sha256_hex ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT media_asset_versions_object_identity_unique
        UNIQUE (storage_object_key, storage_object_version),
    CONSTRAINT media_asset_versions_id_object_version_unique
        UNIQUE (id, storage_object_version),
    CONSTRAINT media_asset_versions_kind_matches_asset
        FOREIGN KEY (logical_asset_id, kind) REFERENCES media_assets (id, kind)
);

CREATE INDEX media_asset_versions_logical_idx
    ON media_asset_versions (logical_asset_id, created_at DESC);
CREATE INDEX media_asset_versions_state_idx
    ON media_asset_versions (state, created_at DESC);

CREATE TABLE upload_intents (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_version_id      UUID NOT NULL UNIQUE REFERENCES media_asset_versions (id),
    expected_object_key   TEXT NOT NULL,
    expected_content_type TEXT NOT NULL,
    expected_size_bytes   BIGINT NOT NULL,
    max_size_bytes        BIGINT NOT NULL,
    expires_at             TIMESTAMPTZ NOT NULL,
    completed_at           TIMESTAMPTZ,
    completion_fingerprint TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT upload_intents_expected_size_non_negative CHECK (expected_size_bytes >= 0),
    CONSTRAINT upload_intents_max_size_positive CHECK (max_size_bytes > 0),
    CONSTRAINT upload_intents_expiry_after_creation CHECK (expires_at > created_at),
    CONSTRAINT upload_intents_completion_pair CHECK (
        (completed_at IS NULL AND completion_fingerprint IS NULL)
        OR (completed_at IS NOT NULL AND completion_fingerprint IS NOT NULL)
    )
);

CREATE TABLE media_callback_receipts (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_event_id     TEXT NOT NULL,
    callback_kind         TEXT NOT NULL,
    asset_version_id      UUID NOT NULL REFERENCES media_asset_versions (id),
    object_version        TEXT,
    request_fingerprint   TEXT NOT NULL,
    received_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT media_callback_kind_valid CHECK (
        callback_kind IN ('UPLOAD_COMPLETED', 'TRANSCODE_COMPLETED')
    ),
    CONSTRAINT media_callback_event_id_present CHECK (length(trim(provider_event_id)) BETWEEN 1 AND 200),
    CONSTRAINT media_callback_fingerprint_present CHECK (length(trim(request_fingerprint)) > 0),
    CONSTRAINT media_callback_event_unique UNIQUE (callback_kind, provider_event_id)
);

CREATE INDEX media_callback_asset_idx
    ON media_callback_receipts (asset_version_id, callback_kind, received_at DESC);

CREATE TABLE scan_attempts (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_version_id      UUID NOT NULL REFERENCES media_asset_versions (id),
    attempt_number        INTEGER NOT NULL,
    work_id               TEXT NOT NULL,
    storage_object_version TEXT NOT NULL,
    outcome               media_scan_outcome NOT NULL,
    scanner_identity      TEXT NOT NULL,
    reason                TEXT,
    scanned_at            TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT scan_attempt_number_positive CHECK (attempt_number >= 1),
    CONSTRAINT scan_attempt_work_present CHECK (length(trim(work_id)) > 0),
    CONSTRAINT scan_attempt_scanner_present CHECK (length(trim(scanner_identity)) > 0),
    CONSTRAINT scan_attempt_reason_for_failure CHECK (
        outcome = 'PASSED' OR (reason IS NOT NULL AND length(trim(reason)) > 0)
    ),
    CONSTRAINT scan_attempt_exact_version_attempt_unique
        UNIQUE (asset_version_id, storage_object_version, attempt_number),
    CONSTRAINT scan_attempt_work_unique UNIQUE (work_id),
    CONSTRAINT scan_attempt_id_asset_version_unique UNIQUE (id, asset_version_id)
);

ALTER TABLE scan_attempts
    ADD CONSTRAINT scan_attempt_exact_version_fk
        FOREIGN KEY (asset_version_id, storage_object_version)
        REFERENCES media_asset_versions (id, storage_object_version);

CREATE INDEX scan_attempts_asset_idx
    ON scan_attempts (asset_version_id, scanned_at DESC);

CREATE TABLE processing_attempts (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_version_id      UUID NOT NULL REFERENCES media_asset_versions (id),
    operation_id          TEXT NOT NULL,
    state                 media_processing_state NOT NULL,
    output_prefix         TEXT,
    rendition_count       INTEGER NOT NULL DEFAULT 0,
    trusted_duration_ms   BIGINT,
    error_reason          TEXT,
    completed_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT processing_attempt_operation_present CHECK (length(trim(operation_id)) > 0),
    CONSTRAINT processing_attempt_rendition_count_non_negative CHECK (rendition_count >= 0),
    CONSTRAINT processing_attempt_duration_non_negative CHECK (
        trusted_duration_ms IS NULL OR trusted_duration_ms >= 0
    ),
    CONSTRAINT processing_attempt_result_coherent CHECK (
        (state = 'SUCCEEDED' AND rendition_count > 0 AND error_reason IS NULL
            AND output_prefix IS NOT NULL AND length(trim(output_prefix)) > 0
            AND trusted_duration_ms IS NOT NULL)
        OR (state = 'FAILED' AND error_reason IS NOT NULL AND length(trim(error_reason)) > 0)
    ),
    CONSTRAINT processing_attempt_operation_unique UNIQUE (asset_version_id, operation_id),
    CONSTRAINT processing_attempt_id_asset_version_unique UNIQUE (id, asset_version_id)
);

CREATE INDEX processing_attempts_asset_idx
    ON processing_attempts (asset_version_id, completed_at DESC);

ALTER TABLE media_asset_versions
    ADD CONSTRAINT media_asset_versions_successful_scan_fk
        FOREIGN KEY (successful_scan_attempt_id, id)
        REFERENCES scan_attempts (id, asset_version_id),
    ADD CONSTRAINT media_asset_versions_successful_processing_fk
        FOREIGN KEY (successful_processing_attempt_id, id)
        REFERENCES processing_attempts (id, asset_version_id);

CREATE TABLE video_renditions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_version_id      UUID NOT NULL REFERENCES media_asset_versions (id),
    name                  TEXT NOT NULL,
    storage_object_key    TEXT NOT NULL,
    width                 INTEGER,
    height                INTEGER,
    bitrate_kbps          INTEGER,
    duration_ms           BIGINT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT video_rendition_name_present CHECK (length(trim(name)) > 0),
    CONSTRAINT video_rendition_key_present CHECK (length(trim(storage_object_key)) > 0),
    CONSTRAINT video_rendition_dimensions_positive CHECK (
        (width IS NULL AND height IS NULL) OR (width > 0 AND height > 0)
    ),
    CONSTRAINT video_rendition_bitrate_positive CHECK (bitrate_kbps IS NULL OR bitrate_kbps > 0),
    CONSTRAINT video_rendition_duration_non_negative CHECK (duration_ms IS NULL OR duration_ms >= 0),
    CONSTRAINT video_rendition_name_unique UNIQUE (asset_version_id, name)
);

CREATE INDEX video_renditions_asset_idx ON video_renditions (asset_version_id, name);

CREATE TABLE legacy_media_mappings (
    legacy_video_id       UUID PRIMARY KEY REFERENCES videos (id),
    media_asset_id        UUID NOT NULL REFERENCES media_assets (id),
    media_asset_version_id UUID NOT NULL UNIQUE REFERENCES media_asset_versions (id),
    source_status         TEXT NOT NULL,
    mapped_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A completed version is history. Before READY, only lifecycle/evidence fields
-- may move forward; after READY no UPDATE is permitted at all.
CREATE FUNCTION media_asset_versions_enforce_immutability() RETURNS TRIGGER AS $$
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

CREATE TRIGGER media_asset_versions_immutable
    BEFORE UPDATE ON media_asset_versions
    FOR EACH ROW EXECUTE FUNCTION media_asset_versions_enforce_immutability();

CREATE TRIGGER scan_attempts_append_only
    BEFORE UPDATE OR DELETE ON scan_attempts
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();

CREATE TRIGGER processing_attempts_append_only
    BEFORE UPDATE OR DELETE ON processing_attempts
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();

CREATE TRIGGER video_renditions_append_only
    BEFORE UPDATE OR DELETE ON video_renditions
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();

-- Preserve authentic legacy identifiers as migration input. Historical bytes
-- are deliberately fail-closed: no automatic scan work is manufactured and no
-- historical status is readiness evidence. An intentional re-upload or a
-- separately approved reprocessing procedure is required before delivery.
INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility, created_at)
SELECT gen_random_uuid(), 'VIDEO', c.owner_account_id, c.id, l.id, 'PROTECTED', v.created_at
FROM videos v
JOIN lessons l ON l.id = v.lesson_id
JOIN sections s ON s.id = l.section_id
JOIN courses c ON c.id = s.course_id
ON CONFLICT DO NOTHING;

INSERT INTO media_asset_versions (
    id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
    content_type, size_bytes, created_at
)
SELECT
    v.id,
    ma.id,
    'VIDEO',
    'QUARANTINED'::media_asset_version_state,
    COALESCE(NULLIF(v.raw_key, ''), 'legacy/' || v.id::text),
    'legacy:' || v.id::text,
    'video/mp4',
    GREATEST(COALESCE(v.file_size_bytes, 0), 0),
    v.created_at
FROM videos v
JOIN lessons l ON l.id = v.lesson_id
JOIN media_assets ma ON ma.lesson_id = l.id
WHERE NOT EXISTS (SELECT 1 FROM media_asset_versions mav WHERE mav.id = v.id);

INSERT INTO legacy_media_mappings (
    legacy_video_id, media_asset_id, media_asset_version_id, source_status
)
SELECT v.id, mav.logical_asset_id, mav.id, v.status::text
FROM videos v
JOIN media_asset_versions mav ON mav.id = v.id
ON CONFLICT DO NOTHING;

CREATE TABLE entitlements (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    student_account_id        UUID NOT NULL REFERENCES accounts (id),
    scope_kind                TEXT NOT NULL,
    scope_id                  UUID NOT NULL,
    course_id                 UUID NOT NULL REFERENCES courses (id),
    grant_source              TEXT NOT NULL,
    source_invitation_id      UUID,
    original_access_ends_at   TIMESTAMPTZ NOT NULL,
    access_ends_at            TIMESTAMPTZ NOT NULL,
    revoked_at                TIMESTAMPTZ,
    retirement_eligibility_at TIMESTAMPTZ NOT NULL,
    state                     TEXT NOT NULL DEFAULT 'ACTIVE',
    revision                  BIGINT NOT NULL DEFAULT 1,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT entitlements_scope_kind_valid CHECK (scope_kind IN ('COURSE', 'SECTION')),
    CONSTRAINT entitlements_grant_source_implemented CHECK (grant_source = 'MANUAL_INVITATION'),
    CONSTRAINT entitlements_grant_source_present CHECK (length(trim(grant_source)) > 0),
    CONSTRAINT entitlements_state_valid CHECK (state IN ('ACTIVE', 'REVOKED')),
    CONSTRAINT entitlements_revocation_coherent CHECK (
        (state = 'REVOKED' AND revoked_at IS NOT NULL)
        OR (state = 'ACTIVE' AND revoked_at IS NULL)
    ),
    CONSTRAINT entitlements_revision_positive CHECK (revision >= 1),
    CONSTRAINT entitlements_expiry_pair_valid CHECK (access_ends_at IS NOT NULL)
);

CREATE UNIQUE INDEX entitlements_one_active_student_course
    ON entitlements (student_account_id, course_id)
    WHERE state = 'ACTIVE';
CREATE INDEX entitlements_student_course_expiry_idx
    ON entitlements (student_account_id, course_id, access_ends_at);

CREATE TABLE entitlement_adjustments (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entitlement_id        UUID NOT NULL REFERENCES entitlements (id),
    old_access_ends_at    TIMESTAMPTZ NOT NULL,
    new_access_ends_at    TIMESTAMPTZ NOT NULL,
    reason                TEXT NOT NULL,
    actor_account_id      UUID NOT NULL REFERENCES accounts (id),
    support_reference     TEXT,
    adjusted_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT entitlement_adjustment_reason_present CHECK (length(trim(reason)) > 0)
);

CREATE TRIGGER entitlement_adjustments_append_only
    BEFORE UPDATE OR DELETE ON entitlement_adjustments
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();
