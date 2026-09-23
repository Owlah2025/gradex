-- PostgreSQL enum values cannot be dropped in place. Refuse downgrade while
-- rows retain the new reason; rewriting that evidence would falsify the audit.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM identity_trusted_devices WHERE revocation_reason = 'AUTO_REPLACED')
       OR EXISTS (SELECT 1 FROM sessions WHERE revocation_reason = 'AUTO_REPLACED') THEN
        RAISE EXCEPTION 'cannot roll back 0043 while AUTO_REPLACED device evidence exists';
    END IF;
END $$;

CREATE TYPE trusted_device_revocation_reason_previous AS ENUM (
    'STUDENT_REMOVED', 'STUDENT_REPLACED', 'ADMIN_REVOKED',
    'ADMIN_REVOKED_ALL', 'PASSWORD_RESET', 'SECURITY_RECOVERY',
    'ACCOUNT_SUSPENDED'
);
ALTER TABLE identity_trusted_devices
    ALTER COLUMN revocation_reason TYPE trusted_device_revocation_reason_previous
    USING revocation_reason::text::trusted_device_revocation_reason_previous;
DROP TYPE trusted_device_revocation_reason;
ALTER TYPE trusted_device_revocation_reason_previous RENAME TO trusted_device_revocation_reason;

CREATE TYPE session_revocation_reason_previous AS ENUM (
    'LOGOUT', 'LOGOUT_ALL', 'PASSWORD_CHANGE', 'PASSWORD_RESET',
    'ACCOUNT_SUSPENDED', 'REUSE_DETECTED', 'ADMIN_REVOKED'
);
ALTER TABLE sessions
    ALTER COLUMN revocation_reason TYPE session_revocation_reason_previous
    USING revocation_reason::text::session_revocation_reason_previous;
DROP TYPE session_revocation_reason;
ALTER TYPE session_revocation_reason_previous RENAME TO session_revocation_reason;
