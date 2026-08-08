\set ON_ERROR_STOP on

INSERT INTO outbox_events (
  id, event_type, schema_version, source_module, aggregate_type, aggregate_id,
  aggregate_revision, safe_payload, correlation_id
) VALUES (
  '90000000-0000-0000-0000-00000000e005',
  'media.transcode_requested',
  1,
  'MEDIA_AND_ASSETS',
  'MEDIA_ASSET_VERSION',
  '90000000-0000-0000-0000-00000000e002',
  2,
  '{"asset_version_id":"90000000-0000-0000-0000-00000000e002","operation_id":"90000000-0000-0000-0000-00000000e005"}',
  's12-worker-media-idempotency-proof'
);
