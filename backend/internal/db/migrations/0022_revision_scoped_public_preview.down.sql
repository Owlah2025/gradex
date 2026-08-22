-- Dropping the origin binding while live rows depend on it would weaken the
-- public-media boundary.  Operators must keep the expanded schema until those
-- preview assets have been retired through an approved migration.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM media_assets
        WHERE preview_origin_revision_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'cannot roll back 0022: revision-scoped public preview assets exist';
    END IF;
END $$;

DROP INDEX IF EXISTS media_assets_preview_origin_revision_idx;

ALTER TABLE media_assets
    DROP CONSTRAINT IF EXISTS media_assets_preview_origin_revision_fk,
    DROP CONSTRAINT IF EXISTS media_assets_preview_origin_revision_kind_check,
    DROP COLUMN IF EXISTS preview_origin_revision_id;

ALTER TABLE course_revisions
    DROP CONSTRAINT IF EXISTS course_revisions_id_course_id_unique;
