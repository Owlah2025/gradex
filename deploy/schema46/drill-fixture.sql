-- Fixture for the schema46 application rollback drill.
--
-- It is seeded ONCE, into the one database the drill uses throughout, and is
-- never re-seeded between application switches — re-seeding would destroy the
-- thing the drill exists to prove.
--
-- The shapes are the ones the two applications actually read over HTTP:
--
--   * a PUBLISHED Course with a live APPROVED revision, so the anonymous
--     catalogue routes return it;
--   * a Lesson carrying a READY video with canonical renditions, so Student
--     playback and the new Lesson preview both have something to authorize;
--   * that Lesson marked allow_public_preview, which is the schema-46 column
--     the candidate reads and the rollback artifact must ignore;
--   * a second Lesson left NOT previewable, so the preview authorization can be
--     proven to refuse as well as to succeed;
--   * a legacy course-level PREVIEW asset on the revision, which is the preview
--     surface the rollback artifact still serves;
--   * a WAITING_PAYMENT purchase request, so Admin payment confirmation can
--     perform the direct Course access grant over real HTTP. That grant is the
--     behaviour whose schema floor made schema 43 unsafe, so both applications
--     are held to it.
--
-- The Accounts are created by the real gradex-bootstrap-admin binary before
-- this file runs; this only adjusts the second one's role, because the
-- bootstrap tool deliberately creates Administrators and the drill also needs a
-- Student with a real, loginable password credential.

\set ON_ERROR_STOP on

BEGIN;

-- gradex-bootstrap-admin deliberately creates the SINGLE Administrator, so the
-- Student cannot be made with it. The Student is created here and given the
-- Administrator's own password hash, which makes it a real, loginable
-- credential for the same disposable passphrase without this fixture having to
-- know how the product hashes passwords.
--
-- Both credentials are moved to ACTIVE. Bootstrap leaves its own at
-- CHANGE_REQUIRED on purpose, which is correct for a real first Administrator
-- and would turn every login in this drill into a password-change challenge.

CREATE TEMPORARY TABLE drill_ids (name text PRIMARY KEY, id uuid) ON COMMIT DROP;

INSERT INTO drill_ids (name, id)
SELECT 'admin', id FROM accounts WHERE normalized_email = :'admin_email';

INSERT INTO accounts (normalized_email, email, role, status, display_name, email_verified_at)
VALUES (:'student_email', :'student_email', 'STUDENT', 'ACTIVE', 'Rollback Drill Student', now())
RETURNING id \gset student_
INSERT INTO drill_ids (name, id) VALUES ('student', :'student_id');

INSERT INTO password_credentials (account_id, password_hash, state)
SELECT :'student_id', password_hash, 'ACTIVE'
  FROM password_credentials
 WHERE account_id = (SELECT id FROM drill_ids WHERE name = 'admin');

UPDATE password_credentials SET state = 'ACTIVE'
 WHERE account_id = (SELECT id FROM drill_ids WHERE name = 'admin');

INSERT INTO courses (owner_account_id, lifecycle, default_access_ends_at)
SELECT id, 'DRAFT', now() + interval '365 days' FROM drill_ids WHERE name = 'admin'
RETURNING id \gset course_
INSERT INTO drill_ids (name, id) VALUES ('course', :'course_id');

INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en)
VALUES (:'course_id', 'APPROVED', 1, 'مقرر التجربة', 'Rollback Drill Course')
RETURNING id \gset revision_
INSERT INTO drill_ids (name, id) VALUES ('revision', :'revision_id');

INSERT INTO course_section_identities (course_id) VALUES (:'course_id')
RETURNING id \gset section_identity_

INSERT INTO course_sections (revision_id, course_id, section_identity_id, title_ar, title_en, position)
VALUES (:'revision_id', :'course_id', :'section_identity_id', 'القسم الأول', 'Section One', 0)
RETURNING id \gset section_

-- The Lesson video.
--
-- It is walked through the REAL media state machine rather than inserted as
-- READY, because the database enforces that machine with triggers: UPLOADED ->
-- QUARANTINED -> SCANNING -> SCAN_PASSED -> PROCESSING -> READY, with exact
-- per-version scan evidence and trusted processing evidence bound to these
-- exact bytes. Nothing here is relaxed to make seeding easier; a fixture that
-- had to disable a safety trigger would not be evidence of anything.
INSERT INTO media_assets (kind, owner_account_id, course_id, visibility)
VALUES ('VIDEO', (SELECT id FROM drill_ids WHERE name = 'admin'), :'course_id', 'PROTECTED')
RETURNING id \gset video_asset_

INSERT INTO media_asset_versions
  (logical_asset_id, kind, state, storage_object_key, storage_object_version,
   content_type, size_bytes, sha256_hex, trusted_duration_ms)
VALUES (:'video_asset_id', 'VIDEO', 'UPLOADED', 'media/drill/source/lesson.mp4', 'v1',
        'video/mp4', 4096, repeat('a', 64), 480000)
RETURNING id \gset video_version_
INSERT INTO drill_ids (name, id) VALUES ('video_version', :'video_version_id');

UPDATE media_asset_versions SET state = 'QUARANTINED' WHERE id = :'video_version_id';
UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = :'video_version_id';

INSERT INTO scan_attempts
  (asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
VALUES (:'video_version_id', 1, 'drill-scan-video', 'v1', 'PASSED', 'schema46-drill')
RETURNING id \gset video_scan_

UPDATE media_asset_versions
   SET state = 'SCAN_PASSED', successful_scan_attempt_id = :'video_scan_id'
 WHERE id = :'video_version_id';
UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = :'video_version_id';

INSERT INTO processing_attempts
  (asset_version_id, operation_id, state, attempt_kind, rendition_count,
   trusted_duration_ms, output_prefix)
VALUES (:'video_version_id', 'drill-full-1', 'SUCCEEDED', 'FULL', 2, 480000, 'media/drill/hls/lesson')
RETURNING id \gset attempt_

INSERT INTO video_renditions
  (asset_version_id, name, storage_object_key, width, height, bitrate_kbps,
   duration_ms, processing_operation_id)
-- The width, height and video bitrate of every row MUST equal the compiled HLS
-- ladder rung of the same name (media.hlsLadder): 720p is 1280x720 at 2800
-- kbps and 480p is 854x480 at 1400 kbps.
--
-- This is not cosmetic. Master-manifest generation re-derives each rendition
-- through persistedVideoRendition, which refuses any row that does not match
-- the ladder, and the refusal surfaces as an inventory-safe 404. An earlier
-- version of this fixture wrote 480p at 1200 kbps; preview AUTHORIZATION still
-- succeeded, because issuance only asks whether renditions exist, and the
-- manifest GET then 404'd. The product is right and is not relaxed here.
VALUES
  (:'video_version_id', '480p', 'media/drill/hls/lesson/480p/playlist.m3u8', 854, 480, 1400, 480000, 'drill-full-1'),
  (:'video_version_id', '720p', 'media/drill/hls/lesson/720p/playlist.m3u8', 1280, 720, 2800, 480000, 'drill-full-1');

UPDATE media_asset_versions
   SET state = 'READY', successful_processing_attempt_id = :'attempt_id'
 WHERE id = :'video_version_id';

-- Lesson one: publicly previewable. This is the schema-46 authoring intent.
INSERT INTO course_lesson_identities (course_id, section_identity_id)
VALUES (:'course_id', :'section_identity_id')
RETURNING id \gset lesson_identity_
INSERT INTO drill_ids (name, id) VALUES ('lesson_identity', :'lesson_identity_id');

INSERT INTO course_lessons
  (section_id, course_id, section_identity_id, lesson_identity_id,
   title_ar, title_en, position, video_asset_version_id, allow_public_preview)
VALUES (:'section_id', :'course_id', :'section_identity_id', :'lesson_identity_id',
        'الدرس الأول', 'Lesson One', 0, :'video_version_id', true)
RETURNING id \gset lesson_

-- Lesson two: NOT previewable, sharing the same video. A preview authorization
-- for it must be refused, which is what proves the flag is actually consulted
-- rather than the route simply always succeeding.
INSERT INTO course_lesson_identities (course_id, section_identity_id)
VALUES (:'course_id', :'section_identity_id')
RETURNING id \gset private_lesson_identity_
INSERT INTO drill_ids (name, id) VALUES ('private_lesson_identity', :'private_lesson_identity_id');

INSERT INTO course_lessons
  (section_id, course_id, section_identity_id, lesson_identity_id,
   title_ar, title_en, position, video_asset_version_id)
VALUES (:'section_id', :'course_id', :'section_identity_id', :'private_lesson_identity_id',
        'الدرس الثاني', 'Lesson Two', 1, :'video_version_id');

-- The legacy course-level preview. Untouched by 0046 and still the preview the
-- rollback artifact serves.
INSERT INTO media_assets (kind, owner_account_id, course_id, visibility, preview_origin_revision_id)
VALUES ('PREVIEW', (SELECT id FROM drill_ids WHERE name = 'admin'), :'course_id', 'PUBLIC_PREVIEW', :'revision_id')
RETURNING id \gset preview_asset_

INSERT INTO media_asset_versions
  (logical_asset_id, kind, state, storage_object_key, storage_object_version,
   content_type, size_bytes, sha256_hex, trusted_duration_ms)
VALUES (:'preview_asset_id', 'PREVIEW', 'UPLOADED', 'media/drill/preview/course.mp4', 'v1',
        'video/mp4', 2048, repeat('b', 64), 60000)
RETURNING id \gset preview_version_

UPDATE media_asset_versions SET state = 'QUARANTINED' WHERE id = :'preview_version_id';
UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = :'preview_version_id';

INSERT INTO scan_attempts
  (asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
VALUES (:'preview_version_id', 1, 'drill-scan-preview', 'v1', 'PASSED', 'schema46-drill')
RETURNING id \gset preview_scan_

UPDATE media_asset_versions
   SET state = 'SCAN_PASSED', successful_scan_attempt_id = :'preview_scan_id'
 WHERE id = :'preview_version_id';

-- PREVIEW is not VIDEO, so the machine allows SCAN_PASSED -> READY without
-- transcode evidence. That is the legacy preview's real shape: a single MP4.
UPDATE media_asset_versions SET state = 'READY' WHERE id = :'preview_version_id';

UPDATE course_revisions SET preview_asset_version_id = :'preview_version_id' WHERE id = :'revision_id';
UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = :'revision_id' WHERE id = :'course_id';

-- No purchase request is seeded here.
--
-- purchase_requests_one_active_course_email allows only ONE active request per
-- Course and email, which is correct product behaviour, so the drill cannot
-- pre-seed one per application turn. Each turn mints its own immediately before
-- it confirms payment, once the previous turn's request has left the active
-- set by reaching ACCESS_GRANTED.

COMMIT;

-- The identifiers the drill script needs, on one line, in a stable order.
SELECT :'course_id' || ' ' || :'lesson_identity_id' || ' ' || :'private_lesson_identity_id'
    || ' ' || :'video_version_id' || ' ' || :'preview_version_id' || ' ' || :'student_id';
