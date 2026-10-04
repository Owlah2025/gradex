ALTER TABLE upload_intents
ADD COLUMN provider_upload_id TEXT,
ADD COLUMN is_multipart BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX upload_intents_abandoned_multipart_idx
ON upload_intents (id)
WHERE is_multipart = true AND completed_at IS NULL AND expires_at < now();
