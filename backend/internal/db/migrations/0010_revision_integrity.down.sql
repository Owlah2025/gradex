-- Revert S2 Revision integrity schema changes.

ALTER TABLE course_price_changes
    DROP CONSTRAINT IF EXISTS fk_course_price_changes_section;

UPDATE course_price_changes cpc
SET section_id = (
    SELECT cs.id
    FROM course_sections cs
    WHERE cs.course_id = cpc.course_id
      AND cs.section_identity_id = cpc.section_id
    ORDER BY cs.created_at, cs.id
    LIMIT 1
)
WHERE cpc.section_id IS NOT NULL;

ALTER TABLE course_price_changes
    ADD CONSTRAINT course_price_changes_section_id_fkey
    FOREIGN KEY (section_id) REFERENCES course_sections (id);

ALTER TABLE course_lessons
    DROP CONSTRAINT IF EXISTS course_lessons_section_identity_unique,
    DROP CONSTRAINT IF EXISTS fk_course_lessons_identity,
    DROP CONSTRAINT IF EXISTS fk_course_lessons_section;

ALTER TABLE course_lessons
    DROP COLUMN IF EXISTS lesson_identity_id,
    DROP COLUMN IF EXISTS section_identity_id,
    DROP COLUMN IF EXISTS course_id;

ALTER TABLE course_sections
    DROP CONSTRAINT IF EXISTS course_sections_id_course_section_unique,
    DROP CONSTRAINT IF EXISTS course_sections_revision_identity_unique,
    DROP CONSTRAINT IF EXISTS fk_course_sections_identity,
    DROP CONSTRAINT IF EXISTS fk_course_sections_revision;

ALTER TABLE course_sections
    DROP COLUMN IF EXISTS section_identity_id,
    DROP COLUMN IF EXISTS course_id;

DROP TABLE IF EXISTS course_lesson_identities;
DROP TABLE IF EXISTS course_section_identities;

ALTER TABLE courses
    DROP CONSTRAINT IF EXISTS fk_courses_live_revision;

ALTER TABLE courses
    ADD CONSTRAINT fk_courses_live_revision
    FOREIGN KEY (live_revision_id) REFERENCES course_revisions (id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE course_revisions
    DROP CONSTRAINT IF EXISTS course_revisions_review_reason_check;

ALTER TABLE course_revisions
    ADD CONSTRAINT course_revisions_review_reason_check
    CHECK (state <> 'CHANGES_REQUESTED' OR (review_reason IS NOT NULL AND length(trim(review_reason)) > 0));

ALTER TABLE course_revisions
    DROP CONSTRAINT IF EXISTS course_revisions_based_on_check,
    DROP CONSTRAINT IF EXISTS fk_course_revisions_based_on,
    DROP CONSTRAINT IF EXISTS course_revisions_course_id_id_unique;

ALTER TABLE course_revisions
    DROP COLUMN IF EXISTS based_on_revision_id;

DROP INDEX IF EXISTS course_revisions_one_active_candidate;

CREATE UNIQUE INDEX idx_course_revisions_pending_review
    ON course_revisions (course_id)
    WHERE state = 'PENDING_REVIEW';
