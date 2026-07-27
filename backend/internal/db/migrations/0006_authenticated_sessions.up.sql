-- S1B2 authenticated-session security evidence.
--
-- The session family/generation schema arrived in migration 0004, but the
-- closed Identity security-event type set from migration 0005 admits only
-- Student admission events. Expanding that allowlist is required before login,
-- rotation, stale-use, reuse revocation, and logout can co-commit evidence.

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
