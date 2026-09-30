DROP TRIGGER IF EXISTS course_completions_append_only ON course_completions;
DROP INDEX IF EXISTS course_completions_student_idx;
DROP INDEX IF EXISTS course_completions_course_idx;
DROP TABLE IF EXISTS course_completions;
DROP TYPE IF EXISTS course_completion_source;
