\set ON_ERROR_STOP on

INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
VALUES (
  '90000000-0000-0000-0000-00000000e001',
  'VIDEO',
  'a0000000-0000-0000-0000-000000000003',
  'c0000000-0000-0000-0000-000000000001',
  'PROTECTED'
);

INSERT INTO media_asset_versions (
  id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
  content_type, size_bytes
) VALUES (
  '90000000-0000-0000-0000-00000000e002',
  '90000000-0000-0000-0000-00000000e001',
  'VIDEO',
  'SCANNING',
  'quarantine/s12-worker/source.mp4',
  :'object_version',
  'video/mp4',
  :object_size
);

INSERT INTO scan_attempts (
  id, asset_version_id, attempt_number, work_id, storage_object_version, outcome,
  scanner_identity
) VALUES (
  '90000000-0000-0000-0000-00000000e003',
  '90000000-0000-0000-0000-00000000e002',
  1,
  's12-recorded-scan-evidence',
  :'object_version',
  'PASSED',
  'out-of-band:s12-disposable-proof'
);

UPDATE media_asset_versions
SET state = 'SCAN_PASSED',
    successful_scan_attempt_id = '90000000-0000-0000-0000-00000000e003'
WHERE id = '90000000-0000-0000-0000-00000000e002';
