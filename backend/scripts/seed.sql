-- Minimal fixture data for manual/smoke testing of the video pipeline.
-- Fixed UUIDs so smoke_test.sh and manual curl testing can reference them directly.

INSERT INTO courses (id, title, instructor_id) VALUES
    ('00000000-0000-0000-0000-000000000010', 'Smoke Test Course', '00000000-0000-0000-0000-000000000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO sections (id, course_id, title, "order") VALUES
    ('00000000-0000-0000-0000-000000000020', '00000000-0000-0000-0000-000000000010', 'Smoke Test Section', 1)
ON CONFLICT (id) DO NOTHING;

INSERT INTO lessons (id, section_id, title, "order") VALUES
    ('00000000-0000-0000-0000-000000000030', '00000000-0000-0000-0000-000000000020', 'Smoke Test Lesson', 1)
ON CONFLICT (id) DO NOTHING;

-- instructor = 00000000-0000-0000-0000-000000000001, student = ...002
INSERT INTO fake_entitlements (user_id, lesson_id, role) VALUES
    ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000030', 'instructor'),
    ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000030', 'student')
ON CONFLICT DO NOTHING;
