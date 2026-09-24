-- Revision-scoped Lesson public preview intent.
--
-- Public preview stops being a separate uploaded MP4 on the Course revision and
-- becomes a permission on a Lesson: zero, one, or many Lessons in a Course may be
-- marked previewable, and an anonymous visitor watches the SAME Lesson video a
-- paying Student watches, over the same protected HLS renditions, from the same
-- storage objects. There is no second upload, no second transcode, and no second
-- media row.
--
-- WHY THE FLAG LIVES ON course_lessons
--
-- course_lessons is revision-scoped: a candidate revision holds its own lesson
-- rows, cloned from the revision it is based on, and only becomes public when an
-- Administrator approves it and it becomes the live revision. Preview intent has
-- exactly those semantics — it is edited on a candidate, reviewed as part of that
-- candidate, and takes effect on approval — so it belongs on the same row and is
-- carried by the same clone.
--
-- It deliberately does NOT live on media_assets or media_asset_versions. A media
-- asset is globally reusable; a permission on it would be a permission that
-- escapes the revision that granted it, and marking one Lesson previewable could
-- silently expose the same video wherever else it was used.
--
-- WHAT THIS MIGRATION DOES NOT TOUCH
--
-- course_revisions.preview_asset_version_id, the existing PREVIEW assets, their
-- versions and their renditions all stay exactly as they are. Production has real
-- legacy preview data in use. Nothing is backfilled onto Lessons, nothing is
-- mapped, nothing is cleared, and the legacy pointer keeps working for every
-- Course whose live revision marks no Lesson previewable. Removing it is a later,
-- separate tranche that requires production observation first.

ALTER TABLE course_lessons
    ADD COLUMN allow_public_preview BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN course_lessons.allow_public_preview IS
    'Whether this Lesson revision permits anonymous public preview of its video. Revision-scoped: edited on a candidate, cloned with revision data, reviewed as part of the submitted revision, and public only once that revision is live. It is authorization metadata only — it creates no media asset, version, processing attempt, rendition, or transcode intent. Resource and Lab Material attachments remain entitlement-protected regardless of its value.';

-- The public resolver asks one question: which Lessons of this Course's live
-- revision are previewable. Every previewable Lesson also carries a video, so the
-- index is partial on the flag and covers the course, which is the only entry
-- point the anonymous path has.
--
-- No uniqueness. Many previewable Lessons per Course is the intended model, not a
-- tolerated edge case, so nothing here may assume one.
CREATE INDEX course_lessons_public_preview
    ON course_lessons (course_id, section_id)
    WHERE allow_public_preview;
