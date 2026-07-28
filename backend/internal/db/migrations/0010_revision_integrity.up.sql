-- S2 Revision integrity: candidate invariants, pointer constraints, and stable Section/Lesson identities.

-- 1. Active-candidate unique index (D5-C04 backstop): replaces narrower PENDING_REVIEW index.
DROP INDEX IF EXISTS idx_course_revisions_pending_review;

CREATE UNIQUE INDEX course_revisions_one_active_candidate
    ON course_revisions (course_id)
    WHERE state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW');

-- 2. Add based_on_revision_id and composite uniqueness/FK for base tracking on course_revisions.
ALTER TABLE course_revisions
    ADD COLUMN based_on_revision_id UUID,
    ADD CONSTRAINT course_revisions_course_id_id_unique UNIQUE (course_id, id);

ALTER TABLE course_revisions
    ADD CONSTRAINT fk_course_revisions_based_on
    FOREIGN KEY (course_id, based_on_revision_id) REFERENCES course_revisions (course_id, id)
    ON DELETE NO ACTION DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT course_revisions_based_on_check
    CHECK (based_on_revision_id IS NULL OR based_on_revision_id <> id);

-- 3. Review reason constraint: mandatory non-empty review_reason for both CHANGES_REQUESTED and REJECTED.
ALTER TABLE course_revisions
    DROP CONSTRAINT IF EXISTS course_revisions_review_reason_check;

ALTER TABLE course_revisions
    ADD CONSTRAINT course_revisions_review_reason_check
    CHECK (state NOT IN ('CHANGES_REQUESTED', 'REJECTED') OR (review_reason IS NOT NULL AND length(trim(review_reason)) > 0));

-- 4. Same-Course live revision pointer constraint on courses table.
ALTER TABLE courses
    DROP CONSTRAINT IF EXISTS fk_courses_live_revision;

ALTER TABLE courses
    ADD CONSTRAINT fk_courses_live_revision
    FOREIGN KEY (id, live_revision_id) REFERENCES course_revisions (course_id, id)
    DEFERRABLE INITIALLY DEFERRED;

-- 5. Stable Section/Lesson identity registries.
CREATE TABLE course_section_identities (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id   UUID NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT course_section_identities_id_course_unique UNIQUE (id, course_id)
);

CREATE TABLE course_lesson_identities (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id           UUID NOT NULL,
    section_identity_id UUID NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT fk_course_lesson_identities_section
        FOREIGN KEY (section_identity_id, course_id) REFERENCES course_section_identities (id, course_id) ON DELETE CASCADE,
    CONSTRAINT course_lesson_identities_id_course_section_unique UNIQUE (id, course_id, section_identity_id)
);

-- 6. Add course_id and section_identity_id to course_sections and backfill.
ALTER TABLE course_sections
    ADD COLUMN course_id UUID,
    ADD COLUMN section_identity_id UUID;

UPDATE course_sections cs
SET course_id = cr.course_id
FROM course_revisions cr
WHERE cs.revision_id = cr.id;

UPDATE course_sections
SET section_identity_id = id
WHERE section_identity_id IS NULL;

INSERT INTO course_section_identities (id, course_id, created_at)
SELECT DISTINCT section_identity_id, course_id, created_at
FROM course_sections
ON CONFLICT (id) DO NOTHING;

ALTER TABLE course_sections
    ALTER COLUMN course_id SET NOT NULL,
    ALTER COLUMN section_identity_id SET NOT NULL;

ALTER TABLE course_sections
    ADD CONSTRAINT fk_course_sections_revision
        FOREIGN KEY (course_id, revision_id) REFERENCES course_revisions (course_id, id) ON DELETE CASCADE,
    ADD CONSTRAINT fk_course_sections_identity
        FOREIGN KEY (section_identity_id, course_id) REFERENCES course_section_identities (id, course_id) ON DELETE CASCADE,
    ADD CONSTRAINT course_sections_revision_identity_unique UNIQUE (revision_id, section_identity_id),
    ADD CONSTRAINT course_sections_id_course_section_unique UNIQUE (id, course_id, section_identity_id);

-- 7. Add course_id, section_identity_id, and lesson_identity_id to course_lessons and backfill.
ALTER TABLE course_lessons
    ADD COLUMN course_id UUID,
    ADD COLUMN section_identity_id UUID,
    ADD COLUMN lesson_identity_id UUID;

UPDATE course_lessons cl
SET course_id = cs.course_id,
    section_identity_id = cs.section_identity_id
FROM course_sections cs
WHERE cl.section_id = cs.id;

UPDATE course_lessons
SET lesson_identity_id = id
WHERE lesson_identity_id IS NULL;

INSERT INTO course_lesson_identities (id, course_id, section_identity_id, created_at)
SELECT DISTINCT lesson_identity_id, course_id, section_identity_id, created_at
FROM course_lessons
ON CONFLICT (id) DO NOTHING;

ALTER TABLE course_lessons
    ALTER COLUMN course_id SET NOT NULL,
    ALTER COLUMN section_identity_id SET NOT NULL,
    ALTER COLUMN lesson_identity_id SET NOT NULL;

ALTER TABLE course_lessons
    ADD CONSTRAINT fk_course_lessons_section
        FOREIGN KEY (section_id, course_id, section_identity_id) REFERENCES course_sections (id, course_id, section_identity_id) ON DELETE CASCADE,
    ADD CONSTRAINT fk_course_lessons_identity
        FOREIGN KEY (lesson_identity_id, course_id, section_identity_id) REFERENCES course_lesson_identities (id, course_id, section_identity_id) ON DELETE CASCADE,
    ADD CONSTRAINT course_lessons_section_identity_unique UNIQUE (section_id, lesson_identity_id);

-- 8. Remap course_price_changes dormant section_id FK from version row to stable section identity.
UPDATE course_price_changes cpc
SET section_id = cs.section_identity_id
FROM course_sections cs
WHERE cpc.section_id = cs.id;

ALTER TABLE course_price_changes
    DROP CONSTRAINT IF EXISTS course_price_changes_section_id_fkey;

ALTER TABLE course_price_changes
    ADD CONSTRAINT fk_course_price_changes_section
    FOREIGN KEY (section_id, course_id) REFERENCES course_section_identities (id, course_id);
