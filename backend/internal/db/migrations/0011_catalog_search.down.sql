DROP INDEX IF EXISTS course_revisions_search_text_trgm_idx;

DROP INDEX IF EXISTS courses_slug_unique_idx;

ALTER TABLE courses
    DROP COLUMN IF EXISTS slug;

ALTER TABLE course_revisions
    DROP COLUMN IF EXISTS search_text;

DROP FUNCTION IF EXISTS catalog_normalize_ar(text);
