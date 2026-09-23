-- Distinguish automatic slot rotation from Student-initiated replacement.
ALTER TYPE trusted_device_revocation_reason ADD VALUE IF NOT EXISTS 'AUTO_REPLACED';
ALTER TYPE session_revocation_reason ADD VALUE IF NOT EXISTS 'AUTO_REPLACED';
