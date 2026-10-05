-- T5 Batch E: durable, append-only Course completion facts.
--
-- The completion row records the live approved revision and the lesson counts
-- that made it true. Progress remains the source of current percentage; this
-- table is the historical fact that a Course was completed once.

CREATE TYPE course_completion_source AS ENUM (
    'PROGRESS',
    'BACKFILL'
);

CREATE TABLE course_completions (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    enrollment_id              UUID NOT NULL REFERENCES enrollments (id),
    student_account_id        UUID NOT NULL REFERENCES accounts (id),
    course_id                 UUID NOT NULL REFERENCES courses (id),
    completed_at               TIMESTAMPTZ NOT NULL,
    course_revision_id         UUID NOT NULL REFERENCES course_revisions (id),
    course_revision_number    INTEGER NOT NULL,
    required_lesson_count     INTEGER NOT NULL,
    completed_lesson_count    INTEGER NOT NULL,
    source                     course_completion_source NOT NULL,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT course_completions_enrollment_unique UNIQUE (enrollment_id),
    CONSTRAINT course_completions_revision_number_positive CHECK (course_revision_number >= 1),
    CONSTRAINT course_completions_required_count_positive CHECK (required_lesson_count > 0),
    CONSTRAINT course_completions_completed_count_valid CHECK (
        completed_lesson_count >= required_lesson_count
        AND completed_lesson_count >= 0
    )
);

CREATE INDEX course_completions_student_idx
    ON course_completions (student_account_id, completed_at DESC, id DESC);

CREATE INDEX course_completions_course_idx
    ON course_completions (course_id, completed_at DESC, id DESC);

CREATE TRIGGER course_completions_append_only
    BEFORE UPDATE OR DELETE ON course_completions
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();

-- Deterministic, idempotent backfill for already-complete current live graphs.
-- Re-running this statement is safe because enrollment_id is the domain key.
DO $$
DECLARE
    batch_size INT := 1000;
    inserted INT := 1;
BEGIN
    WHILE inserted > 0 LOOP
        WITH current_course_lessons AS (
            SELECT e.id AS enrollment_id,
                   e.student_account_id,
                   e.course_id,
                   cr.id AS course_revision_id,
                   cr.revision_number,
                   cli.id AS lesson_identity_id
            FROM enrollments e
            JOIN courses c ON c.id = e.course_id
            JOIN course_revisions cr
              ON cr.id = c.live_revision_id
             AND cr.course_id = c.id
             AND cr.state = 'APPROVED'
            JOIN course_sections cs
              ON cs.revision_id = cr.id
             AND cs.course_id = c.id
            JOIN course_lessons cl
              ON cl.section_id = cs.id
             AND cl.course_id = c.id
            JOIN course_lesson_identities cli
              ON cli.id = cl.lesson_identity_id
             AND cli.course_id = c.id
             AND cli.section_identity_id = cl.section_identity_id
        ), eligible AS (
            SELECT current_course_lessons.enrollment_id,
                   current_course_lessons.student_account_id,
                   current_course_lessons.course_id,
                   current_course_lessons.course_revision_id,
                   current_course_lessons.revision_number,
                   count(DISTINCT current_course_lessons.lesson_identity_id)::INTEGER AS required_lesson_count,
                   count(DISTINCT progress.course_lesson_identity_id)
                       FILTER (WHERE progress.completed_at IS NOT NULL)::INTEGER AS completed_lesson_count,
                   max(progress.completed_at) AS completed_at
            FROM current_course_lessons
            LEFT JOIN progress
              ON progress.enrollment_id = current_course_lessons.enrollment_id
             AND progress.course_lesson_identity_id = current_course_lessons.lesson_identity_id
            GROUP BY current_course_lessons.enrollment_id,
                     current_course_lessons.student_account_id,
                     current_course_lessons.course_id,
                     current_course_lessons.course_revision_id,
                     current_course_lessons.revision_number
        ), batch AS (
            SELECT enrollment_id
            FROM eligible
            WHERE required_lesson_count > 0
              AND completed_lesson_count = required_lesson_count
              AND completed_at IS NOT NULL
              AND NOT EXISTS (
                  SELECT 1 FROM course_completions cc
                  WHERE cc.enrollment_id = eligible.enrollment_id
              )
            LIMIT batch_size
        )
        INSERT INTO course_completions (
            enrollment_id,
            student_account_id,
            course_id,
            completed_at,
            course_revision_id,
            course_revision_number,
            required_lesson_count,
            completed_lesson_count,
            source
        )
        SELECT e.enrollment_id,
               e.student_account_id,
               e.course_id,
               e.completed_at,
               e.course_revision_id,
               e.revision_number,
               e.required_lesson_count,
               e.completed_lesson_count,
               'BACKFILL'::course_completion_source
        FROM eligible e
        JOIN batch b ON b.enrollment_id = e.enrollment_id;

        GET DIAGNOSTICS inserted = ROW_COUNT;
    END LOOP;
END;
$$;
