DROP TRIGGER IF EXISTS admin_notes_append_only ON admin_notes;
DROP INDEX IF EXISTS admin_notes_subject_created_idx;
DROP TABLE IF EXISTS admin_notes;

ALTER TABLE identity_security_events
    DROP CONSTRAINT identity_security_events_type;

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
            'ACCOUNT_REINSTATED',
            'DEVICE_TRUST_CHALLENGED',
            'DEVICE_TRUST_ATTEMPTS_EXHAUSTED',
            'DEVICE_TRUSTED',
            'DEVICE_ADOPTED_LEGACY_SESSION',
            'DEVICE_REVOKED',
            'DEVICE_LIMIT_REACHED',
            'DEVICE_REPLACEMENT_BLOCKED',
            'ADMIN_DEVICE_REVOKED',
            'ADMIN_DEVICE_COOLDOWN_RESET'
        )
    );
