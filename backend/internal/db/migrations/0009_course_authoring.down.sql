-- Revert S2 Course Authoring schema changes.

DROP TABLE IF EXISTS course_price_changes;
DROP TABLE IF EXISTS lesson_files;
DROP TABLE IF EXISTS course_lessons;
DROP TABLE IF EXISTS course_sections;

ALTER TABLE courses DROP CONSTRAINT IF EXISTS fk_courses_live_revision;

DROP TABLE IF EXISTS course_revisions;
DROP TABLE IF EXISTS taxonomy_terms;

ALTER TABLE courses DROP CONSTRAINT IF EXISTS courses_suspension_check;
ALTER TABLE courses DROP CONSTRAINT IF EXISTS courses_suspension_reason_non_empty;
ALTER TABLE courses DROP CONSTRAINT IF EXISTS courses_published_has_live_revision;

ALTER TABLE courses
    ADD COLUMN title TEXT,
    ADD COLUMN instructor_id UUID;

UPDATE courses SET instructor_id = owner_account_id WHERE instructor_id IS NULL;
UPDATE courses SET title = 'Smoke Test Course' WHERE title IS NULL;

ALTER TABLE courses
    ALTER COLUMN title SET NOT NULL,
    ALTER COLUMN instructor_id SET NOT NULL;

ALTER TABLE courses
    DROP COLUMN IF EXISTS owner_account_id,
    DROP COLUMN IF EXISTS lifecycle,
    DROP COLUMN IF EXISTS live_revision_id,
    DROP COLUMN IF EXISTS access_suspended_at,
    DROP COLUMN IF EXISTS access_suspension_reason,
    DROP COLUMN IF EXISTS retired_at,
    DROP COLUMN IF EXISTS updated_at;

ALTER TABLE outbox_events
    DROP CONSTRAINT outbox_events_source_module;

ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_source_module CHECK (
        source_module = 'IDENTITY_AND_ACCESS'
    );

DROP TYPE IF EXISTS lesson_file_kind;
DROP TYPE IF EXISTS taxonomy_kind;
DROP TYPE IF EXISTS study_year;
DROP TYPE IF EXISTS revision_state;
DROP TYPE IF EXISTS course_lifecycle;
