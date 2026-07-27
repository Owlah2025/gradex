-- This contraction succeeds only after S1B3 recovery secrets and evidence have
-- been removed under an explicit rollback/retention procedure. It deliberately
-- fails rather than silently deleting security evidence or reset secrets.

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_purpose CHECK (purpose = 'EMAIL_VERIFICATION');

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
            'SESSION_LOGGED_OUT'
        )
    );
