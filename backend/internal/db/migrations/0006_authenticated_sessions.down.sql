-- This contraction succeeds only after S1B2 session evidence has been removed
-- under an explicit rollback/retention procedure. It deliberately fails rather
-- than silently deleting security evidence.

ALTER TABLE identity_security_events
    DROP CONSTRAINT identity_security_events_type;

ALTER TABLE identity_security_events
    ADD CONSTRAINT identity_security_events_type CHECK (
        event_type IN (
            'BOOTSTRAP_ADMIN_CREATED',
            'STUDENT_REGISTRATION_ACCEPTED',
            'EMAIL_VERIFICATION_REISSUED',
            'STUDENT_EMAIL_VERIFIED'
        )
    );
