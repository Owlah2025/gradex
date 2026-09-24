-- Reverses the revision-scoped Lesson public preview intent.
--
-- This is safe to run unconditionally, which is unusual here and worth stating.
-- The column is pure authorization metadata: it owns no media, no storage object,
-- no rendition, and no Student access. Dropping it withdraws public preview from
-- every Lesson that had it, which is a fail-closed loss of exposure rather than a
-- loss of anything a Student, an Instructor, or an Administrator paid for or
-- created. Nothing becomes unrepresentable and no live entitlement is affected.
--
-- The legacy preview pointer and every PREVIEW asset are untouched, so a Course
-- that also has a legacy course-level preview simply continues serving it — which
-- is precisely why the legacy path is retained during the transition rather than
-- cleared when a Lesson preview goes live.

DROP INDEX course_lessons_public_preview;

ALTER TABLE course_lessons
    DROP COLUMN allow_public_preview;
