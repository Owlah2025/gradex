-- T2 Admin User 360: append-only internal notes and operator session evidence.

CREATE TABLE admin_notes (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_type       TEXT NOT NULL DEFAULT 'ACCOUNT',
    subject_account_id UUID NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    author_account_id  UUID NOT NULL REFERENCES accounts (id),
    body               TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT admin_notes_subject_type_valid CHECK (subject_type = 'ACCOUNT'),
    CONSTRAINT admin_notes_body_length CHECK (length(trim(body)) BETWEEN 1 AND 4000)
);

CREATE INDEX admin_notes_subject_created_idx
    ON admin_notes (subject_account_id, created_at DESC, id DESC);

CREATE TRIGGER admin_notes_append_only
    BEFORE UPDATE OR DELETE ON admin_notes
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();

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
            'ADMIN_DEVICE_COOLDOWN_RESET',
            'ADMIN_SESSIONS_REVOKED'
        )
    );
