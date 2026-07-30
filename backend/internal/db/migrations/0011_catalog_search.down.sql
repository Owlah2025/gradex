DROP INDEX IF EXISTS course_revisions_search_text_idx;

ALTER TABLE course_revisions
    DROP COLUMN IF EXISTS search_text;

DROP FUNCTION IF EXISTS catalog_normalize_ar(text);
