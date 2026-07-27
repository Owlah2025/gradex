DROP TABLE IF EXISTS staff_invitations;

ALTER TABLE identity_security_events
    DROP CONSTRAINT identity_security_events_type;

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
            'PASSWORD_RESET_COMPLETED'
        )
    );

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_account_id_purpose;

DELETE FROM identity_action_secrets WHERE purpose = 'STAFF_INVITATION';

ALTER TABLE identity_action_secrets
    ALTER COLUMN account_id SET NOT NULL;

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_purpose CHECK (
        purpose IN (
            'EMAIL_VERIFICATION',
            'PASSWORD_RESET'
        )
    );
