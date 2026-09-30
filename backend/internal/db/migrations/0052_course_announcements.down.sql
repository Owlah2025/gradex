DROP TRIGGER IF EXISTS course_announcements_append_only ON course_announcements;
DROP FUNCTION IF EXISTS course_announcements_immutable();
DROP INDEX IF EXISTS course_announcements_course_published_idx;
DROP TABLE IF EXISTS course_announcements;
