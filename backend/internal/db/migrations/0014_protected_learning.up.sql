DO $$
DECLARE
    legacy_progress_rows BIGINT;
BEGIN
    SELECT count(*) INTO legacy_progress_rows FROM progress;
    IF legacy_progress_rows <> 0 THEN
        RAISE EXCEPTION 'legacy progress table contains % row(s); refusing protected-learning cutover', legacy_progress_rows;
    END IF;
END;
$$;

DROP TABLE progress;

CREATE TABLE progress (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    enrollment_id               UUID NOT NULL REFERENCES enrollments (id),
    course_lesson_identity_id   UUID NOT NULL REFERENCES course_lesson_identities (id),
    max_position_seconds        NUMERIC(10, 3) NOT NULL DEFAULT 0,
    last_position_seconds       NUMERIC(10, 3) NOT NULL DEFAULT 0,
    completed_at                TIMESTAMPTZ,
    completing_asset_version_id UUID REFERENCES media_asset_versions (id),
    last_watched_at             TIMESTAMPTZ,
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT prog_identity UNIQUE (enrollment_id, course_lesson_identity_id),
    CONSTRAINT prog_max_non_negative CHECK (max_position_seconds >= 0),
    CONSTRAINT prog_last_non_negative CHECK (last_position_seconds >= 0),
    CONSTRAINT prog_max_ge_last CHECK (max_position_seconds >= last_position_seconds),
    CONSTRAINT prog_completion_pair CHECK ((completed_at IS NULL) = (completing_asset_version_id IS NULL))
);

CREATE INDEX idx_progress_enrollment ON progress (enrollment_id);

CREATE TABLE content_reports (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_account_id UUID NOT NULL REFERENCES accounts (id),
    target_kind         TEXT NOT NULL,
    target_id           UUID NOT NULL,
    target_revision_ref UUID,
    reason              TEXT NOT NULL,
    explanation         TEXT,
    resolved_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rep_target_kind CHECK (target_kind IN ('COURSE', 'LESSON', 'VIDEO', 'RESOURCE', 'LAB_MATERIAL')),
    CONSTRAINT rep_reason CHECK (reason IN ('broken_unavailable', 'inaccurate', 'inappropriate', 'suspected_copyright_violation', 'other')),
    CONSTRAINT rep_other_needs_explanation CHECK (reason <> 'other' OR (explanation IS NOT NULL AND length(btrim(explanation)) > 0))
);

CREATE UNIQUE INDEX rep_no_duplicate_open
    ON content_reports (reporter_account_id, target_kind, target_id)
    WHERE resolved_at IS NULL;
