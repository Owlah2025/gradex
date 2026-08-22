-- D-088 adds one honest lifecycle state to the media state machine.
--
-- `VALIDATED` records that the exact stored object version passed the
-- trusted-Instructor validation in D-088 §4 — configured size bound, actual
-- stored size, declared type against the real file format, and SHA-256 over
-- that exact version. It deliberately does not claim, imply, or substitute for
-- malware scanning; D-088 §5 requires that distinction to be visible in the
-- record rather than hidden behind a fabricated `SCAN_PASSED`.
--
-- The enum value is added alone, in its own migration, because PostgreSQL
-- refuses to use a new enum value in the transaction that created it.
-- 0020 installs the table, provenance column, and state-transition
-- enforcement that actually use it.

ALTER TYPE media_asset_version_state ADD VALUE IF NOT EXISTS 'VALIDATED' AFTER 'SCAN_ERROR';
