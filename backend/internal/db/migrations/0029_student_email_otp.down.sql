-- Evidence of exhausted challenges is removed before the type allowlist
-- narrows again, or the CHECK would refuse to validate against existing rows
-- and leave the migration marker dirty partway through.
DELETE FROM identity_security_events
 WHERE event_type = 'EMAIL_VERIFICATION_ATTEMPTS_EXHAUSTED';

ALTER TABLE identity_security_events
    DROP CONSTRAINT IF EXISTS identity_security_events_type;

ALTER TABLE identity_security_events
    ADD CONSTRAINT identity_security_events_type CHECK (
        event_type IN (
            'BOOTSTRAP_ADMIN_CREATED',
            'STUDENT_REGISTRATION_ACCEPTED',
            'EMAIL_VERIFICATION_REISSUED',
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

-- Reversing the OTP purpose is only safe once no challenge rows remain: the
-- CHECK constraint would refuse to validate against them and the migration
-- would fail partway, leaving the marker dirty. Superseding them is the
-- correct rollback — a pending Student re-requests verification — and it is
-- done before the constraint narrows.
DROP INDEX IF EXISTS identity_action_secrets_live_otp_idx;

DELETE FROM identity_security_events
 WHERE action_secret_id IN (
     SELECT id FROM identity_action_secrets WHERE purpose = 'EMAIL_VERIFICATION_OTP'
 );

DELETE FROM identity_action_secrets WHERE purpose = 'EMAIL_VERIFICATION_OTP';

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT IF EXISTS identity_action_secrets_account_id_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_account_id_purpose CHECK (
        (purpose IN ('EMAIL_VERIFICATION', 'PASSWORD_RESET') AND account_id IS NOT NULL)
        OR (purpose IN ('STAFF_INVITATION', 'COURSE_ACCESS_INVITATION') AND account_id IS NULL)
    );

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT IF EXISTS identity_action_secrets_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_purpose CHECK (
        purpose IN (
            'EMAIL_VERIFICATION',
            'PASSWORD_RESET',
            'STAFF_INVITATION',
            'COURSE_ACCESS_INVITATION'
        )
    );
