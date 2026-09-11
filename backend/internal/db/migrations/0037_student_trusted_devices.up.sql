-- Student trusted devices and device-bound session families.
--
-- Everything here is additive. No existing table is rebuilt, no existing row is
-- rewritten, and no existing column changes type or nullability. The two
-- session columns are nullable precisely so the deployment does not log every
-- live Student out: an existing family arrives with a NULL device and is
-- adopted on its next protected-learning request rather than revoked.

-- Why a device record ends. A closed set because these drive security response
-- and the replacement cooldown, not display: STUDENT_REPLACED starts a cooldown
-- while ADMIN_REVOKED_ALL deliberately does not.
CREATE TYPE trusted_device_revocation_reason AS ENUM (
    'STUDENT_REMOVED',
    'STUDENT_REPLACED',
    'ADMIN_REVOKED',
    'ADMIN_REVOKED_ALL',
    'PASSWORD_RESET',
    'SECURITY_RECOVERY',
    'ACCOUNT_SUSPENDED'
);

-- How one login family relates to device policy.
--
-- LEGACY_UNBOUND exists only for families that predate this migration. It is
-- not a state any new session may be created in, and the policy treats it as
-- "must adopt a device before protected learning" rather than as trusted.
CREATE TYPE session_device_trust_state AS ENUM (
    'NOT_APPLICABLE',
    'PENDING_DEVICE_TRUST',
    'TRUSTED',
    'LEGACY_UNBOUND'
);

CREATE TABLE identity_trusted_devices (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id         UUID NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,

    -- SHA-256 of the opaque browser device credential, never the credential.
    -- A leak of this table must not yield a cookie that any browser can
    -- present, exactly as session_credentials already guarantees.
    --
    -- Deliberately NOT globally unique. One browser holds one origin-scoped
    -- device cookie, so a shared household browser presents the same digest to
    -- two different Student Accounts. Those are two independent trusted-device
    -- records consuming one slot each, which is the honest model; a global
    -- unique index would instead make the second Account's login fail.
    credential_digest  TEXT NOT NULL,

    -- Presentation only, and deliberately coarse. Derived server-side from the
    -- User-Agent so the Student can tell their two devices apart. It is not
    -- device identity: the credential digest is, and a changed label never
    -- changes which record a browser matches.
    label              TEXT NOT NULL,
    browser_family     TEXT NOT NULL,
    platform_family    TEXT NOT NULL,

    first_seen_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- NULL while the device exists but has not completed its email OTP
    -- challenge. A pending record occupies no slot.
    trusted_at         TIMESTAMPTZ,

    revoked_at         TIMESTAMPTZ,
    revocation_reason  trusted_device_revocation_reason,

    -- Security forensics only. Never read as identity, never compared to decide
    -- whether a browser matches a record, and never a reason to refuse a
    -- Student whose network changed.
    last_ip_address    INET,

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT identity_trusted_devices_digest_present
        CHECK (length(credential_digest) > 0),
    CONSTRAINT identity_trusted_devices_label_present
        CHECK (length(trim(label)) > 0),
    CONSTRAINT identity_trusted_devices_revocation_coherent CHECK (
        (revoked_at IS NULL AND revocation_reason IS NULL)
        OR (revoked_at IS NOT NULL AND revocation_reason IS NOT NULL)
    ),
    CONSTRAINT identity_trusted_devices_trusted_after_first_seen
        CHECK (trusted_at IS NULL OR trusted_at >= first_seen_at),
    CONSTRAINT identity_trusted_devices_revoked_after_first_seen
        CHECK (revoked_at IS NULL OR revoked_at >= first_seen_at)
);

-- One live record per browser per Account. This is what makes a returning
-- trusted device reuse its record instead of accumulating a new row on every
-- login, and it is enforced by the database rather than by the repository
-- remembering to look first.
CREATE UNIQUE INDEX identity_trusted_devices_live_credential_idx
    ON identity_trusted_devices (account_id, credential_digest)
    WHERE revoked_at IS NULL;

-- The slot count read on every new-device decision.
CREATE INDEX identity_trusted_devices_account_trusted_idx
    ON identity_trusted_devices (account_id)
    WHERE trusted_at IS NOT NULL AND revoked_at IS NULL;

CREATE INDEX identity_trusted_devices_account_recent_idx
    ON identity_trusted_devices (account_id, last_seen_at DESC);

-- Replacement cooldown state.
--
-- A separate row rather than a column on accounts: this is device-policy
-- bookkeeping with its own admin override, and accounts is read on every single
-- authorization. Absence of a row means no cooldown has ever started, which is
-- the correct state for every existing Account at deployment.
CREATE TABLE identity_device_replacement_state (
    account_id             UUID PRIMARY KEY REFERENCES accounts (id) ON DELETE CASCADE,

    -- When the Student last removed or replaced a trusted device. The cooldown
    -- is computed from this against the configured window, so changing the
    -- window changes live behavior without a data migration.
    last_replacement_at    TIMESTAMPTZ,

    -- An override clears the cooldown by moving this forward. The cooldown is
    -- in force only while last_replacement_at is more recent.
    --
    -- cooldown_cleared_by is the operator who did it, and is deliberately
    -- nullable: password reset and security recovery clear the cooldown too,
    -- and there is no person to name for those. A Student who has just proven
    -- control of their mailbox must not then be made to wait a day, so the
    -- clearance is real and its actor is genuinely absent rather than invented.
    cooldown_cleared_at    TIMESTAMPTZ,
    cooldown_cleared_by    UUID REFERENCES accounts (id),

    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- An actor without a clearance is incoherent; a clearance without an actor
    -- is a recovery flow acting on the Account's behalf.
    CONSTRAINT identity_device_replacement_clear_coherent CHECK (
        cooldown_cleared_by IS NULL OR cooldown_cleared_at IS NOT NULL
    )
);

-- Session families gain their device binding.
--
-- device_trust_state defaults to LEGACY_UNBOUND so every family that already
-- exists is marked as predating device policy instead of silently appearing
-- trusted. New families always write the column explicitly.
ALTER TABLE sessions
    ADD COLUMN trusted_device_id UUID REFERENCES identity_trusted_devices (id),
    ADD COLUMN device_trust_state session_device_trust_state NOT NULL DEFAULT 'LEGACY_UNBOUND',

    ADD CONSTRAINT sessions_device_trust_coherent CHECK (
        (device_trust_state = 'TRUSTED' AND trusted_device_id IS NOT NULL)
        OR (device_trust_state <> 'TRUSTED' AND trusted_device_id IS NULL)
    );

-- Device revocation revokes exactly the families bound to that device, so the
-- lookup is by device rather than by Account.
CREATE INDEX sessions_trusted_device_idx
    ON sessions (trusted_device_id)
    WHERE trusted_device_id IS NOT NULL AND state = 'ACTIVE';

-- The device-trust OTP joins the existing action-secret chain under a new
-- purpose, for the reasons 0029 already recorded: the supersession chain, the
-- one-live-per-purpose index, the attempt budget, and the terminal-state
-- exclusion are the exact invariants an OTP challenge needs and are proven
-- here. A second OTP table would restate them and drift.
ALTER TABLE identity_action_secrets
    ADD COLUMN trusted_device_id UUID REFERENCES identity_trusted_devices (id);

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_purpose CHECK (
        purpose IN (
            'EMAIL_VERIFICATION',
            'EMAIL_VERIFICATION_OTP',
            'DEVICE_TRUST_OTP',
            'PASSWORD_RESET',
            'STAFF_INVITATION',
            'COURSE_ACCESS_INVITATION'
        )
    );

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_account_id_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_account_id_purpose CHECK (
        (purpose IN ('EMAIL_VERIFICATION', 'EMAIL_VERIFICATION_OTP',
                     'DEVICE_TRUST_OTP', 'PASSWORD_RESET')
            AND account_id IS NOT NULL)
        OR (purpose IN ('STAFF_INVITATION', 'COURSE_ACCESS_INVITATION')
            AND account_id IS NULL)
    );

-- A device-trust challenge is meaningless without the device it would trust,
-- and no other purpose may carry one.
ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_device_purpose CHECK (
        (purpose = 'DEVICE_TRUST_OTP' AND trusted_device_id IS NOT NULL)
        OR (purpose <> 'DEVICE_TRUST_OTP' AND trusted_device_id IS NULL)
    );

CREATE INDEX IF NOT EXISTS identity_action_secrets_live_device_otp_idx
    ON identity_action_secrets (account_id, issued_at DESC)
    WHERE purpose = 'DEVICE_TRUST_OTP'
      AND consumed_at IS NULL
      AND superseded_at IS NULL;

-- Device lifecycle produces its own evidence. None of these are describable by
-- an existing event type: trusting a device is not a session creation, and a
-- blocked replacement is not a denied login.
--
-- Playback heartbeats deliberately write nothing: a heartbeat every 25 seconds
-- per watching Student would bury every other security event in the table.
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
