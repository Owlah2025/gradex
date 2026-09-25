-- Reverses the revision-scoped Lesson public preview intent.
--
-- This runs unconditionally, but it is NOT free, and must not be described as
-- unconditionally safe merely because the UP was additive.
--
-- The column is pure authorization metadata: it owns no media, no storage object,
-- no rendition, and no Student access. Dropping it withdraws public preview from
-- every Lesson that had it, which is a fail-closed loss of exposure rather than a
-- loss of anything a Student, an Instructor, or an Administrator paid for or
-- created. No live entitlement is affected.
--
-- What IS lost is authoring intent: which Lessons an Instructor deliberately
-- chose to make publicly previewable. That choice exists nowhere else, so it
-- cannot be reconstructed after the drop and would have to be made again by hand.
--
-- PRODUCTION ROLLBACK POSTURE
--
-- This is not the production rollback path for the schema 44 -> 46 release. The
-- immediate rollback keeps the database at clean schema 46 and rolls the
-- APPLICATION back to the schema-46-compatible old-behaviour artifact, which
-- simply does not read this column. Nothing is dropped and no authoring intent is
-- lost. Database restore, or an explicitly authorized future downgrade, is a
-- separate recovery decision.
--
-- The legacy preview pointer and every PREVIEW asset are untouched, so a Course
-- that also has a legacy course-level preview simply continues serving it — which
-- is precisely why the legacy path is retained during the transition rather than
-- cleared when a Lesson preview goes live.

DROP INDEX course_lessons_public_preview;

ALTER TABLE course_lessons
    DROP COLUMN allow_public_preview;
