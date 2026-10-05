DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM upload_intents WHERE is_multipart AND multipart_status <> 'ABORTED') THEN
  RAISE EXCEPTION 'cannot roll back durable multipart while sessions exist';
 END IF;
END $$;
DROP INDEX upload_intents_multipart_recovery_idx;
DROP INDEX upload_intents_multipart_verification_idx;
DROP INDEX upload_intents_multipart_abort_idx;
DROP INDEX upload_intents_multipart_client_request_idx;
DROP TRIGGER lesson_file_bind_course ON lesson_files;
DROP FUNCTION lesson_file_bind_course();
DROP TRIGGER media_version_bind_course ON media_asset_versions;
DROP FUNCTION media_version_bind_course();
ALTER TABLE media_asset_versions DROP CONSTRAINT media_version_logical_course_fk;
ALTER TABLE media_assets DROP CONSTRAINT media_assets_id_course_unique;
ALTER TABLE upload_intents DROP COLUMN multipart_status, DROP COLUMN multipart_object_version,
 DROP COLUMN multipart_manifest, DROP COLUMN multipart_sha256,
 DROP COLUMN multipart_verification_claim, DROP COLUMN multipart_verification_lease,
 DROP COLUMN multipart_verification_retry_at, DROP COLUMN multipart_verification_attempts,
 DROP COLUMN multipart_verification_error, DROP COLUMN multipart_client_request_id;
