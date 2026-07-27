-- S2 Course authoring, review, pricing, taxonomy, and catalogue lifecycle.

CREATE TYPE course_lifecycle AS ENUM (
    'DRAFT',
    'PENDING_REVIEW',
    'CHANGES_REQUESTED',
    'PUBLISHED',
    'DELISTED',
    'ARCHIVED'
);

CREATE TYPE revision_state AS ENUM (
    'DRAFT',
    'PENDING_REVIEW',
    'CHANGES_REQUESTED',
    'APPROVED',
    'SUPERSEDED',
    'REJECTED'
);

CREATE TYPE study_year AS ENUM (
    'PREP',
    'YEAR_1',
    'YEAR_2',
    'YEAR_3',
    'YEAR_4'
);

CREATE TYPE taxonomy_kind AS ENUM (
    'MAJOR',
    'SUBJECT'
);

CREATE TYPE lesson_file_kind AS ENUM (
    'RESOURCE',
    'LAB_MATERIAL'
);

-- Allow CATALOG_AND_AUTHORING in outbox_events.
ALTER TABLE outbox_events
    DROP CONSTRAINT outbox_events_source_module;

ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_source_module CHECK (
        source_module IN ('IDENTITY_AND_ACCESS', 'CATALOG_AND_AUTHORING')
    );

-- Taxonomy terms (Admin-administered vocabulary).
CREATE TABLE taxonomy_terms (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind          taxonomy_kind NOT NULL,
    label_ar      TEXT NOT NULL,
    label_en      TEXT NOT NULL,
    academic_code TEXT,
    retired_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT taxonomy_terms_label_ar_non_empty CHECK (length(trim(label_ar)) > 0),
    CONSTRAINT taxonomy_terms_label_en_non_empty CHECK (length(trim(label_en)) > 0),
    CONSTRAINT taxonomy_terms_academic_code_check CHECK (academic_code IS NULL OR (kind = 'SUBJECT' AND length(trim(academic_code)) > 0))
);

-- Expand 0001_init's stub courses table to full S2 course domain table.
ALTER TABLE courses
    ADD COLUMN owner_account_id UUID REFERENCES accounts (id),
    ADD COLUMN lifecycle course_lifecycle NOT NULL DEFAULT 'DRAFT',
    ADD COLUMN live_revision_id UUID,
    ADD COLUMN access_suspended_at TIMESTAMPTZ,
    ADD COLUMN access_suspension_reason TEXT,
    ADD COLUMN retired_at TIMESTAMPTZ,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE courses SET owner_account_id = instructor_id WHERE owner_account_id IS NULL AND instructor_id IS NOT NULL;

ALTER TABLE courses
    DROP COLUMN title,
    DROP COLUMN instructor_id;

ALTER TABLE courses
    ALTER COLUMN owner_account_id SET NOT NULL;

ALTER TABLE courses
    ADD CONSTRAINT courses_suspension_check CHECK (
        (access_suspended_at IS NULL) = (access_suspension_reason IS NULL)
    ),
    ADD CONSTRAINT courses_suspension_reason_non_empty CHECK (
        access_suspension_reason IS NULL OR length(trim(access_suspension_reason)) > 0
    ),
    ADD CONSTRAINT courses_published_has_live_revision CHECK (
        lifecycle <> 'PUBLISHED' OR live_revision_id IS NOT NULL
    );

-- Candidate graph / unit of review.
CREATE TABLE course_revisions (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id                 UUID NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    state                     revision_state NOT NULL DEFAULT 'DRAFT',
    revision_number           INT NOT NULL,
    title_ar                  TEXT NOT NULL,
    title_en                  TEXT NOT NULL,
    description_ar            TEXT NOT NULL DEFAULT '',
    description_en            TEXT NOT NULL DEFAULT '',
    major_term_id             UUID REFERENCES taxonomy_terms (id),
    subject_term_id           UUID REFERENCES taxonomy_terms (id),
    study_year                study_year,
    preview_asset_version_id  UUID,
    submitted_at              TIMESTAMPTZ,
    reviewed_at               TIMESTAMPTZ,
    reviewed_by_account_id    UUID REFERENCES accounts (id),
    review_reason             TEXT,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT course_revisions_course_number_unique UNIQUE (course_id, revision_number),
    CONSTRAINT course_revisions_title_ar_non_empty CHECK (length(trim(title_ar)) > 0),
    CONSTRAINT course_revisions_title_en_non_empty CHECK (length(trim(title_en)) > 0),
    CONSTRAINT course_revisions_review_reason_check CHECK (state <> 'CHANGES_REQUESTED' OR (review_reason IS NOT NULL AND length(trim(review_reason)) > 0))
);

-- Concurrency Case 2: At most one revision in PENDING_REVIEW per course.
CREATE UNIQUE INDEX idx_course_revisions_pending_review
    ON course_revisions (course_id)
    WHERE state = 'PENDING_REVIEW';

-- Foreign key for live_revision_id after course_revisions is created.
ALTER TABLE courses
    ADD CONSTRAINT fk_courses_live_revision
    FOREIGN KEY (live_revision_id) REFERENCES course_revisions (id)
    DEFERRABLE INITIALLY DEFERRED;

-- Ordered sections belonging to a revision.
CREATE TABLE course_sections (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    revision_id        UUID NOT NULL REFERENCES course_revisions (id) ON DELETE CASCADE,
    title_ar           TEXT NOT NULL,
    title_en           TEXT NOT NULL,
    position           INT NOT NULL,
    price_minor_units  BIGINT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT course_sections_position_unique UNIQUE (revision_id, position),
    CONSTRAINT course_sections_title_ar_non_empty CHECK (length(trim(title_ar)) > 0),
    CONSTRAINT course_sections_title_en_non_empty CHECK (length(trim(title_en)) > 0),
    CONSTRAINT course_sections_position_non_negative CHECK (position >= 0),
    CONSTRAINT course_sections_price_non_negative CHECK (price_minor_units IS NULL OR price_minor_units >= 0)
);

-- Ordered lessons belonging to a section.
CREATE TABLE course_lessons (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    section_id              UUID NOT NULL REFERENCES course_sections (id) ON DELETE CASCADE,
    title_ar                TEXT NOT NULL,
    title_en                TEXT NOT NULL,
    position                INT NOT NULL,
    video_asset_version_id  UUID,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT course_lessons_position_unique UNIQUE (section_id, position),
    CONSTRAINT course_lessons_title_ar_non_empty CHECK (length(trim(title_ar)) > 0),
    CONSTRAINT course_lessons_title_en_non_empty CHECK (length(trim(title_en)) > 0),
    CONSTRAINT course_lessons_position_non_negative CHECK (position >= 0)
);

-- Resources and lab materials attached to a lesson.
CREATE TABLE lesson_files (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lesson_id         UUID NOT NULL REFERENCES course_lessons (id) ON DELETE CASCADE,
    kind              lesson_file_kind NOT NULL,
    asset_version_id  UUID NOT NULL,
    display_name_ar   TEXT NOT NULL,
    display_name_en   TEXT NOT NULL,
    position          INT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT lesson_files_position_unique UNIQUE (lesson_id, kind, position),
    CONSTRAINT lesson_files_display_name_ar_non_empty CHECK (length(trim(display_name_ar)) > 0),
    CONSTRAINT lesson_files_display_name_en_non_empty CHECK (length(trim(display_name_en)) > 0),
    CONSTRAINT lesson_files_position_non_negative CHECK (position >= 0)
);

-- Append-only price changes for courses and sections.
CREATE TABLE course_price_changes (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id              UUID NOT NULL REFERENCES courses (id),
    section_id             UUID REFERENCES course_sections (id),
    old_value_minor_units  BIGINT,
    new_value_minor_units  BIGINT NOT NULL,
    changed_by_account_id  UUID NOT NULL REFERENCES accounts (id),
    reason                 TEXT NOT NULL,
    changed_at             TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT course_price_changes_old_value_check CHECK (old_value_minor_units IS NULL OR old_value_minor_units >= 0),
    CONSTRAINT course_price_changes_new_value_check CHECK (new_value_minor_units >= 0),
    CONSTRAINT course_price_changes_reason_non_empty CHECK (length(trim(reason)) > 0)
);
