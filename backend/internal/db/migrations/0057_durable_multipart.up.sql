ALTER TABLE upload_intents
 ADD COLUMN multipart_status text NOT NULL DEFAULT 'ACTIVE'
   CHECK (multipart_status IN ('ACTIVE','COMPLETING','ASSEMBLED','ABORTING','ABORTED')),
 ADD COLUMN multipart_object_version text,
 ADD COLUMN multipart_manifest text,
 ADD COLUMN multipart_sha256 text,
 ADD COLUMN multipart_verification_claim uuid,
 ADD COLUMN multipart_verification_lease timestamptz,
 ADD COLUMN multipart_verification_retry_at timestamptz NOT NULL DEFAULT now(),
 ADD COLUMN multipart_verification_attempts integer NOT NULL DEFAULT 0,
 ADD COLUMN multipart_verification_error text NOT NULL DEFAULT '';
ALTER TABLE upload_intents ADD COLUMN multipart_client_request_id uuid;
CREATE UNIQUE INDEX upload_intents_multipart_client_request_idx ON upload_intents (multipart_client_request_id)
 WHERE multipart_client_request_id IS NOT NULL;

CREATE INDEX upload_intents_multipart_recovery_idx ON upload_intents (expires_at, id)
 WHERE is_multipart AND completed_at IS NULL AND multipart_status <> 'ABORTED';
CREATE INDEX upload_intents_multipart_verification_idx ON upload_intents (multipart_verification_retry_at, created_at, id)
 WHERE is_multipart AND completed_at IS NULL AND multipart_status='ASSEMBLED' AND multipart_verification_error='';
CREATE INDEX upload_intents_multipart_abort_idx ON upload_intents (id)
 WHERE is_multipart AND completed_at IS NULL AND multipart_status='ABORTING';

-- 0054 introduced required denormalized course keys without deriving them for new writes.
ALTER TABLE media_assets ADD CONSTRAINT media_assets_id_course_unique UNIQUE (id, course_id);
ALTER TABLE media_asset_versions ADD CONSTRAINT media_version_logical_course_fk
 FOREIGN KEY (logical_asset_id, course_id) REFERENCES media_assets(id, course_id);

CREATE FUNCTION media_version_bind_course() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE bound_course uuid;
BEGIN
 SELECT course_id INTO STRICT bound_course FROM media_assets WHERE id=NEW.logical_asset_id;
 IF NEW.course_id IS NOT NULL AND NEW.course_id <> bound_course THEN
  RAISE EXCEPTION 'media version course mismatch' USING ERRCODE='23514';
 END IF;
 NEW.course_id := bound_course;
 RETURN NEW;
END $$;
CREATE TRIGGER media_version_bind_course BEFORE INSERT OR UPDATE OF logical_asset_id, course_id
 ON media_asset_versions FOR EACH ROW EXECUTE FUNCTION media_version_bind_course();

CREATE FUNCTION lesson_file_bind_course() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE bound_course uuid;
BEGIN
 SELECT course_id INTO STRICT bound_course FROM course_lessons WHERE id=NEW.lesson_id;
 IF NEW.course_id IS NOT NULL AND NEW.course_id <> bound_course THEN
  RAISE EXCEPTION 'lesson file course mismatch' USING ERRCODE='23514';
 END IF;
 NEW.course_id := bound_course;
 RETURN NEW;
END $$;
CREATE TRIGGER lesson_file_bind_course BEFORE INSERT OR UPDATE OF lesson_id, course_id
 ON lesson_files FOR EACH ROW EXECUTE FUNCTION lesson_file_bind_course();
