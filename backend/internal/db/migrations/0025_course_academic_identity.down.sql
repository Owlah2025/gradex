-- Reverses 0025 exactly. Only T4-A-owned schema is removed.
--
-- No legacy Course data was read, moved, or rewritten by the up migration, so
-- dropping the T4 columns restores the pre-T4 Course exactly: taxonomy_terms,
-- course_revisions.major_term_id, course_revisions.subject_term_id,
-- course_revisions.study_year, live_revision_id and every publication pointer
-- are untouched in both directions. The T1/T2 Academic Catalog and the T3
-- Student profile are likewise untouched.
--
-- DELIBERATE ASYMMETRY: rolling down restores 0023's coded-Subject uniqueness
-- rule, which is scoped to live rows and therefore REOPENS official-code reuse
-- after retirement. The permanence rule is owned by 0025 (D-093 §7), so it can
-- only exist while 0025 is applied. Rolling 0025 down on a database that has
-- since relied on code permanence is a decision, not a routine rollback.

DROP TABLE IF EXISTS subject_requests;
DROP TYPE IF EXISTS subject_request_status;

DROP TABLE IF EXISTS course_program_targets;

DROP TRIGGER IF EXISTS courses_subject_immutability_guard ON courses;
DROP FUNCTION IF EXISTS courses_reject_published_subject_change();

DROP INDEX IF EXISTS courses_institution_subject_idx;

ALTER TABLE courses
    DROP CONSTRAINT IF EXISTS courses_legacy_has_no_academic_identity,
    DROP CONSTRAINT IF EXISTS courses_academic_has_institution,
    DROP CONSTRAINT IF EXISTS courses_subject_same_institution,
    DROP CONSTRAINT IF EXISTS courses_id_institution_unique;

ALTER TABLE courses
    DROP COLUMN IF EXISTS subject_id,
    DROP COLUMN IF EXISTS institution_id,
    DROP COLUMN IF EXISTS classification_model;

DROP TYPE IF EXISTS course_classification_model;

-- Restore 0023's rule verbatim.
DROP INDEX IF EXISTS subjects_institution_code_unique;

CREATE UNIQUE INDEX subjects_institution_code_unique
    ON subjects (institution_id, code_normalized)
    WHERE code_normalized IS NOT NULL AND retired_at IS NULL;
