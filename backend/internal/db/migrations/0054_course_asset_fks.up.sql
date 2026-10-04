-- 1. Add course_id to media_asset_versions
ALTER TABLE media_asset_versions ADD COLUMN course_id UUID;

UPDATE media_asset_versions mav
SET course_id = ma.course_id
FROM media_assets ma
WHERE ma.id = mav.logical_asset_id;

ALTER TABLE media_asset_versions ALTER COLUMN course_id SET NOT NULL;
ALTER TABLE media_asset_versions ADD CONSTRAINT media_asset_versions_id_course_id_key UNIQUE (id, course_id);

-- 2. Add composite FKs for course_lessons
ALTER TABLE course_lessons
    ADD CONSTRAINT course_lessons_video_asset_course_fk
    FOREIGN KEY (video_asset_version_id, course_id)
    REFERENCES media_asset_versions (id, course_id)
    ON DELETE RESTRICT;

-- 3. Add composite FKs for lesson_files
ALTER TABLE lesson_files ADD COLUMN course_id UUID;

UPDATE lesson_files lf
SET course_id = cl.course_id
FROM course_lessons cl
WHERE cl.id = lf.lesson_id;

ALTER TABLE lesson_files ALTER COLUMN course_id SET NOT NULL;
ALTER TABLE lesson_files
    ADD CONSTRAINT lesson_files_asset_course_fk
    FOREIGN KEY (asset_version_id, course_id)
    REFERENCES media_asset_versions (id, course_id)
    ON DELETE RESTRICT;

-- 4. Add composite FKs for course_revisions
ALTER TABLE course_revisions
    ADD CONSTRAINT course_revisions_preview_asset_course_fk
    FOREIGN KEY (preview_asset_version_id, course_id)
    REFERENCES media_asset_versions (id, course_id)
    ON DELETE RESTRICT;
