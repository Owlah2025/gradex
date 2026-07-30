-- S3 catalogue search storage: one normalized document per authored revision.
-- Visibility remains a query-time concern owned by courses and PublishedOnly.

-- Extensions are database capabilities, not S3-owned schema objects. Follow
-- 0001_init's IF NOT EXISTS convention and retain pg_trgm on rollback.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE courses
    ADD COLUMN slug text
    GENERATED ALWAYS AS ('course-' || replace(id::text, '-', '')) STORED;

CREATE UNIQUE INDEX courses_slug_unique_idx ON courses (slug);

CREATE FUNCTION catalog_normalize_ar(input text) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    RETURN trim(regexp_replace(
        lower(regexp_replace(
            translate(input, 'أإآٱىة٠١٢٣٤٥٦٧٨٩ـ', 'اااايه0123456789'),
            '[ً-ْٰٓ-ٕ]', '', 'g'
        )),
        '[[:space:]]+', ' ', 'g'
    ));

ALTER TABLE course_revisions
    ADD COLUMN search_text text
    GENERATED ALWAYS AS (
        catalog_normalize_ar(
            coalesce(title_ar, '') || ' ' ||
            coalesce(title_en, '') || ' ' ||
            coalesce(description_ar, '') || ' ' ||
            coalesce(description_en, '')
        )
    ) STORED;

CREATE INDEX course_revisions_search_text_trgm_idx
    ON course_revisions USING GIN (search_text gin_trgm_ops);
