-- S1C Staff lifecycle, account suspension enforcement, and staff invitations.

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_purpose CHECK (
        purpose IN (
            'EMAIL_VERIFICATION',
            'PASSWORD_RESET',
            'STAFF_INVITATION'
        )
    );

ALTER TABLE identity_action_secrets
    ALTER COLUMN account_id DROP NOT NULL;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_account_id_purpose CHECK (
        (purpose IN ('EMAIL_VERIFICATION', 'PASSWORD_RESET') AND account_id IS NOT NULL)
        OR (purpose = 'STAFF_INVITATION' AND account_id IS NULL)
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
            'PASSWORD_RESET_COMPLETED',
            'STAFF_INVITATION_CREATED',
            'STAFF_INVITATION_SUPERSEDED',
            'STAFF_INVITATION_REVOKED',
            'STAFF_INVITATION_COMPLETED',
            'ACCOUNT_SUSPENDED',
            'ACCOUNT_REINSTATED'
        )
    );

CREATE TABLE staff_invitations (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    normalized_email   TEXT NOT NULL,
    email              TEXT NOT NULL,
    invited_role       TEXT NOT NULL,
    inviter_account_id UUID NOT NULL REFERENCES accounts (id),
    state              TEXT NOT NULL,
    action_secret_id   UUID NOT NULL REFERENCES identity_action_secrets (id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT staff_invitations_role CHECK (invited_role IN ('INSTRUCTOR', 'ADMIN')),
    CONSTRAINT staff_invitations_state CHECK (state IN ('PENDING', 'CONSUMED', 'SUPERSEDED', 'EXPIRED', 'REVOKED')),
    CONSTRAINT staff_invitations_email_present CHECK (length(trim(normalized_email)) BETWEEN 3 AND 320)
);

CREATE UNIQUE INDEX staff_invitations_one_pending_per_email
    ON staff_invitations (normalized_email)
    WHERE state = 'PENDING';

CREATE INDEX staff_invitations_secret_idx
    ON staff_invitations (action_secret_id);

CREATE INDEX staff_invitations_inviter_idx
    ON staff_invitations (inviter_account_id);
