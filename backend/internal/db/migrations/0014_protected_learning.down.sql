DROP INDEX IF EXISTS rep_no_duplicate_open;
DROP TABLE IF EXISTS content_reports;
DROP INDEX IF EXISTS idx_progress_enrollment;
DROP TABLE IF EXISTS progress;

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
    CHECK (max_position_seconds >= 0),
    CHECK (last_position_seconds >= 0),
    CHECK (max_position_seconds >= last_position_seconds),
    UNIQUE (user_id, lesson_id)
);
