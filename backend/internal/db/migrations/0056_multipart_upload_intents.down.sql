DROP INDEX upload_intents_abandoned_multipart_idx;

ALTER TABLE upload_intents
DROP COLUMN provider_upload_id,
DROP COLUMN is_multipart;
