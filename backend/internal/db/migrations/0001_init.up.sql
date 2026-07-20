CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE video_status AS ENUM (
    'DRAFT', 'UPLOADING', 'UPLOADED', 'QUEUED', 'PROCESSING', 'READY', 'PUBLISHED', 'FAILED'
);

-- Minimal course/section/lesson stubs: just enough to satisfy FKs for the video
-- pipeline. Real course-catalog CRUD (and a users table) is a separate, later task.
CREATE TABLE courses (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title         TEXT NOT NULL,
    instructor_id UUID NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sections (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id  UUID NOT NULL REFERENCES courses(id),
    title      TEXT NOT NULL,
    "order"    INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE lessons (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    section_id        UUID NOT NULL REFERENCES sections(id),
    title             TEXT NOT NULL,
    "order"           INT NOT NULL,
    status            video_status NOT NULL DEFAULT 'DRAFT',
    duration_seconds  NUMERIC(10, 3),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- videos: 1:1 with lessons. status is the Gradex-owned source of truth for
-- lifecycle; provider_* fields are a cache/mirror of the transcoding provider's
-- own state, never authoritative (see docs/superpowers/specs/2026-07-20-mediakit-spike-plan.md §7).
CREATE TABLE videos (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lesson_id          UUID NOT NULL UNIQUE REFERENCES lessons(id),

    status             video_status NOT NULL DEFAULT 'DRAFT',
    failed_reason      TEXT,
    retry_count        INT NOT NULL DEFAULT 0,

    raw_key            TEXT,
    hls_master_key     TEXT,

    resolution         TEXT,
    bitrate            INT,
    codec              TEXT,
    fps                NUMERIC(6, 3),
    file_size_bytes    BIGINT,

    provider           TEXT NOT NULL DEFAULT 's3',
    provider_asset_id  TEXT,
    provider_status    TEXT,
    provider_metadata  JSONB NOT NULL DEFAULT '{}',
    last_sync_at       TIMESTAMPTZ,
    sync_version       INT NOT NULL DEFAULT 0,

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_videos_status ON videos(status);

CREATE TABLE progress (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               UUID NOT NULL,
    lesson_id             UUID NOT NULL REFERENCES lessons(id),
    max_position_seconds  NUMERIC(10, 3) NOT NULL DEFAULT 0,
    last_position_seconds NUMERIC(10, 3) NOT NULL DEFAULT 0,
    completed             BOOLEAN NOT NULL DEFAULT false,
    completed_at          TIMESTAMPTZ,
    last_watched_at       TIMESTAMPTZ,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, lesson_id)
);

-- Dev-only seam: backs the fake Authenticator/EntitlementChecker until real
-- JWT auth + real purchase/enrollment records exist (separate, later task).
CREATE TABLE fake_entitlements (
    user_id   UUID NOT NULL,
    lesson_id UUID NOT NULL REFERENCES lessons(id),
    role      TEXT NOT NULL CHECK (role IN ('student', 'instructor')),
    PRIMARY KEY (user_id, lesson_id, role)
);
