-- Student email verification moves from an emailed bearer link to an emailed
-- one-time code.
--
-- The code lives in the existing identity_action_secrets table under a new
-- purpose rather than in a table of its own. That is deliberate: the
-- supersession chain, the one-live-secret-per-purpose index, the attempt
-- counters, the terminal-state exclusion, and the security-event foreign key
-- are the exact invariants an OTP challenge needs, and they are already proven
-- here. A parallel table would restate all of them and could drift.
--
-- secret_digest holds an HMAC-SHA256 over a server-held pepper, so the stored
-- value is still exactly 32 bytes and the existing size and uniqueness
-- constraints continue to apply unchanged.
--
-- EMAIL_VERIFICATION is deliberately retained. Gradex is live and pending
-- Accounts may hold verification links that were already delivered; dropping
-- the purpose would strand them mid-journey. Both purposes are live during the
-- legacy window, and the one-live-per-purpose index keeps each chain single.

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
    DROP CONSTRAINT identity_action_secrets_account_id_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_account_id_purpose CHECK (
        (purpose IN ('EMAIL_VERIFICATION', 'EMAIL_VERIFICATION_OTP', 'PASSWORD_RESET')
            AND account_id IS NOT NULL)
        OR (purpose IN ('STAFF_INVITATION', 'COURSE_ACCESS_INVITATION')
            AND account_id IS NULL)
    );

-- Resend cooldown and attempt budget are both read by challenge id on the hot
-- path. The primary key already serves the lookup; this index serves the
-- "current live challenge for this Account" read that resend performs.
CREATE INDEX IF NOT EXISTS identity_action_secrets_live_otp_idx
    ON identity_action_secrets (account_id, issued_at DESC)
    WHERE purpose = 'EMAIL_VERIFICATION_OTP'
      AND consumed_at IS NULL
      AND superseded_at IS NULL;

-- The attempt budget produces evidence of its own. Exhausting a challenge is a
-- security-relevant outcome that no existing event type describes: it is not a
-- reissue and it is not a verification, and recording it as either would make
-- the trail say something untrue about what happened.
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
            'ACCOUNT_REINSTATED'
        )
    );
