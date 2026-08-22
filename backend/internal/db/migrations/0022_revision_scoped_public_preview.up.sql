-- MVP-F14: a public-preview Asset is created for one Course revision.  The
-- public pointer itself remains on course_revisions so changing live_revision_id
-- remains the one atomic publication switch.  The origin column prevents an
-- Instructor from attaching a preview uploaded for an unrelated candidate.
--
-- It is intentionally nullable: historical PREVIEW rows predate this contract
-- and are not rewritten by a migration.  New production writes must carry the
-- binding and the application rejects unbound rows for publication.
ALTER TABLE course_revisions
    ADD CONSTRAINT course_revisions_id_course_id_unique UNIQUE (id, course_id);

ALTER TABLE media_assets
    ADD COLUMN preview_origin_revision_id UUID;

ALTER TABLE media_assets
    ADD CONSTRAINT media_assets_preview_origin_revision_kind_check CHECK (
        (preview_origin_revision_id IS NULL OR kind = 'PREVIEW')
        AND (kind <> 'PREVIEW' OR lesson_id IS NULL)
    ),
    ADD CONSTRAINT media_assets_preview_origin_revision_fk
        FOREIGN KEY (preview_origin_revision_id, course_id)
        REFERENCES course_revisions (id, course_id)
        ON DELETE RESTRICT;

CREATE INDEX media_assets_preview_origin_revision_idx
    ON media_assets (preview_origin_revision_id)
    WHERE preview_origin_revision_id IS NOT NULL;
