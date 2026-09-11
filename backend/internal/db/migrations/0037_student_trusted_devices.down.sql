-- Reverses 0037 exactly, in dependency order.
--
-- Rolling back returns every Student to the pre-device world: session families
-- survive with their credentials intact and simply stop carrying a device
-- binding, which is the same state they were in before the up migration. No
-- Student is logged out by rolling back, and no session row is deleted.

ALTER TABLE identity_security_events
    DROP CONSTRAINT identity_security_events_type;

-- The pre-0037 vocabulary. Any device event already written is
-- removed first, because the restored constraint would otherwise be violated by
-- history this feature produced. Deleting them is correct on rollback: the
-- feature that gave them meaning no longer exists.
DELETE FROM identity_security_events
    WHERE event_type IN (
        'DEVICE_TRUST_CHALLENGED',
        'DEVICE_TRUST_ATTEMPTS_EXHAUSTED',
        'DEVICE_TRUSTED',
        'DEVICE_ADOPTED_LEGACY_SESSION',
        'DEVICE_REVOKED',
        'DEVICE_LIMIT_REACHED',
        'DEVICE_REPLACEMENT_BLOCKED',
        'ADMIN_DEVICE_REVOKED',
        'ADMIN_DEVICE_COOLDOWN_RESET'
    );

ALTER TABLE identity_security_events
    ADD CONSTRAINT identity_security_events_type CHECK (
        event_type IN (
            'BOOTSTRAP_ADMIN_CREATED',
            'STUDENT_REGISTRATION_ACCEPTED',
            'EMAIL_VERIFICATION_REISSUED',
            'EMAIL_VERIFICATION_ATTEMPTS_EXHAUSTED',
            'STUDENT_EMAIL_VERIFIED',
            'SESSION_CREATED',
            'SESSION_RENEWED',
            'SESSION_REPLACED_PRESENTED',
            'SESSION_REUSE_DETECTED',
            'SESSION_LOGGED_OUT',
            'PASSWORD_RESET_REQUESTED',
            'PASSWORD_RESET_COMPLETED',
            'STAFF_INVITATION_CREATED',
            'STAFF_INVITATION_SUPERSEDED',
            'STAFF_INVITATION_REVOKED',
            'STAFF_INVITATION_COMPLETED',
            'ACCOUNT_SUSPENDED',
            'ACCOUNT_REINSTATED'
        )
    );

DROP INDEX IF EXISTS identity_action_secrets_live_device_otp_idx;

-- Live device-trust challenges cannot survive a rollback: the purpose they
-- carry is about to stop being permitted, and a half-finished device challenge
-- authorizes nothing on its own.
DELETE FROM identity_action_secrets WHERE purpose = 'DEVICE_TRUST_OTP';

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_device_purpose;

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_account_id_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_account_id_purpose CHECK (
        (purpose IN ('EMAIL_VERIFICATION', 'EMAIL_VERIFICATION_OTP', 'PASSWORD_RESET')
            AND account_id IS NOT NULL)
        OR (purpose IN ('STAFF_INVITATION', 'COURSE_ACCESS_INVITATION')
            AND account_id IS NULL)
    );

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_purpose CHECK (
        purpose IN (
            'EMAIL_VERIFICATION',
            'EMAIL_VERIFICATION_OTP',
            'PASSWORD_RESET',
            'STAFF_INVITATION',
            'COURSE_ACCESS_INVITATION'
        )
    );

ALTER TABLE identity_action_secrets
    DROP COLUMN trusted_device_id;

DROP INDEX IF EXISTS sessions_trusted_device_idx;

ALTER TABLE sessions
    DROP CONSTRAINT sessions_device_trust_coherent;

ALTER TABLE sessions
    DROP COLUMN device_trust_state,
    DROP COLUMN trusted_device_id;

DROP TABLE identity_device_replacement_state;

DROP INDEX IF EXISTS identity_trusted_devices_account_recent_idx;
DROP INDEX IF EXISTS identity_trusted_devices_account_trusted_idx;
DROP INDEX IF EXISTS identity_trusted_devices_live_credential_idx;

DROP TABLE identity_trusted_devices;

DROP TYPE session_device_trust_state;
DROP TYPE trusted_device_revocation_reason;
