-- S1B3 password recovery.
--
-- Recovery reuses the identity_action_secrets machinery that migration 0005
-- introduced for email verification rather than adding a parallel table: the
-- digest-only storage, expiry, single-live-per-purpose index, terminal-state
-- exclusivity, and attempt evidence are exactly the properties a reset secret
-- needs. Only the closed purpose and event-type allowlists have to expand.
--
-- The one-live-per-purpose unique index is already scoped by purpose, so an
-- Account may hold a live verification secret and a live reset secret at the
-- same time without either superseding the other.

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_purpose CHECK (
        purpose IN (
            'EMAIL_VERIFICATION',
            'PASSWORD_RESET'
        )
    );

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
