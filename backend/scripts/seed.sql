-- Minimal fixture data for manual/smoke testing of the video pipeline.
-- Fixed UUIDs so smoke_test.sh and manual curl testing can reference them directly.

BEGIN;

INSERT INTO courses (id, title, instructor_id) VALUES
    ('00000000-0000-0000-0000-000000000010', 'Smoke Test Course', '00000000-0000-0000-0000-000000000001')
ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, instructor_id = EXCLUDED.instructor_id;

INSERT INTO sections (id, course_id, title, "order") VALUES
    ('00000000-0000-0000-0000-000000000020', '00000000-0000-0000-0000-000000000010', 'Smoke Test Section', 1)
ON CONFLICT (id) DO UPDATE SET course_id = EXCLUDED.course_id, title = EXCLUDED.title, "order" = EXCLUDED."order";

INSERT INTO lessons (id, section_id, title, "order") VALUES
    ('00000000-0000-0000-0000-000000000030', '00000000-0000-0000-0000-000000000020', 'Smoke Test Lesson', 1)
ON CONFLICT (id) DO UPDATE SET section_id = EXCLUDED.section_id, title = EXCLUDED.title, "order" = EXCLUDED."order";

-- instructor = 00000000-0000-0000-0000-000000000001, student = ...002
INSERT INTO fake_entitlements (user_id, lesson_id, role) VALUES
    ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000030', 'instructor'),
    ('00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000030', 'student')
ON CONFLICT (user_id, lesson_id, role) DO NOTHING;

-- Real Accounts for the two smoke users.
--
-- Protected routes now run a capability policy that resolves a Principal from
-- the accounts table and denies when there is none. That check is deliberately
-- fail-closed, so a fake_entitlements row with no Account behind it is refused.
-- Correct, but it would break the smoke path for a reason unrelated to what the
-- smoke test checks.
--
-- The resolver reads a missing credential as CHANGE_REQUIRED — the restrictive
-- reading — so each Account gets an explicit ACTIVE credential. The hash is a
-- syntactically valid Argon2id encoding that no password produces; it satisfies
-- the schema's format constraint and is not something to authenticate against.
-- There is no login endpoint until S1B in any case.
INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at) VALUES
    ('00000000-0000-0000-0000-000000000001', 'smoke.instructor@gradex.invalid',
     'smoke.instructor@gradex.invalid', 'INSTRUCTOR', 'ACTIVE', 'Smoke Instructor', now()),
    ('00000000-0000-0000-0000-000000000002', 'smoke.student@gradex.invalid',
     'smoke.student@gradex.invalid', 'STUDENT', 'ACTIVE', 'Smoke Student', now())
ON CONFLICT (id) DO UPDATE SET
    status = EXCLUDED.status,
    display_name = EXCLUDED.display_name,
    email_verified_at = EXCLUDED.email_verified_at;

INSERT INTO password_credentials (account_id, password_hash, state) VALUES
    ('00000000-0000-0000-0000-000000000001',
     '$argon2id$v=19$m=65536,t=3,p=4$c21va2VzYWx0c21va2Vz$bm90LWEtcmVhbC1jcmVkZW50aWFsLXNlZWQwMQ', 'ACTIVE'),
    ('00000000-0000-0000-0000-000000000002',
     '$argon2id$v=19$m=65536,t=3,p=4$c21va2VzYWx0c21va2Vz$bm90LWEtcmVhbC1jcmVkZW50aWFsLXNlZWQwMg', 'ACTIVE')
ON CONFLICT (account_id) DO UPDATE SET state = EXCLUDED.state;

COMMIT;
