-- S1B1 Student admission: bootstrap retry fingerprinting, immutable policy and
-- Identity evidence, purpose-bound action secrets, and the minimal
-- transactional outbox/protected-delivery boundary.

ALTER TABLE bootstrap_operations
    ADD COLUMN fingerprint_version SMALLINT,
    ADD COLUMN request_fingerprint BYTEA,
    ADD CONSTRAINT bootstrap_operations_fingerprint_pair CHECK (
        (fingerprint_version IS NULL AND request_fingerprint IS NULL)
        OR (
            fingerprint_version IS NOT NULL
            AND fingerprint_version > 0
            AND request_fingerprint IS NOT NULL
            AND octet_length(request_fingerprint) = 32
        )
    );

-- Existing bootstrap markers deliberately remain NULL/NULL. Their original
-- semantic request cannot be reconstructed safely, so application retries
-- against them fail closed. Every post-0005 bootstrap writes both fields.

CREATE TABLE policy_acceptances (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id     UUID NOT NULL REFERENCES accounts (id),
    policy_set_id  TEXT NOT NULL,
    policy_kind    TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    locale         TEXT NOT NULL,
    accepted_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    request_id     TEXT NOT NULL,

    CONSTRAINT policy_acceptances_set_present CHECK (length(trim(policy_set_id)) BETWEEN 1 AND 200),
    CONSTRAINT policy_acceptances_kind_format CHECK (policy_kind ~ '^[A-Z][A-Z0-9_]*$'),
    CONSTRAINT policy_acceptances_version_present CHECK (length(trim(policy_version)) BETWEEN 1 AND 200),
    CONSTRAINT policy_acceptances_locale CHECK (locale IN ('ar', 'en')),
    CONSTRAINT policy_acceptances_request_present CHECK (length(trim(request_id)) BETWEEN 1 AND 200),
    CONSTRAINT policy_acceptances_exact_version UNIQUE (account_id, policy_kind, policy_version)
);

CREATE INDEX policy_acceptances_version_time_idx
    ON policy_acceptances (policy_kind, policy_version, accepted_at DESC);

CREATE TABLE identity_action_secrets (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id        UUID NOT NULL REFERENCES accounts (id),
    purpose           TEXT NOT NULL,
    secret_digest     BYTEA NOT NULL,
    issued_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL,
    consumed_at       TIMESTAMPTZ,
    superseded_at     TIMESTAMPTZ,
    superseded_by_id  UUID,
    attempt_count     INTEGER NOT NULL DEFAULT 0,
    first_attempt_at  TIMESTAMPTZ,
    last_attempt_at   TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT identity_action_secrets_purpose CHECK (purpose = 'EMAIL_VERIFICATION'),
    CONSTRAINT identity_action_secrets_digest_size CHECK (octet_length(secret_digest) = 32),
    CONSTRAINT identity_action_secrets_digest_unique UNIQUE (secret_digest),
    CONSTRAINT identity_action_secrets_expiry CHECK (expires_at > issued_at),
    CONSTRAINT identity_action_secrets_terminal_exclusive CHECK (
        consumed_at IS NULL OR superseded_at IS NULL
    ),
    CONSTRAINT identity_action_secrets_consumed_after_issue CHECK (
        consumed_at IS NULL OR consumed_at >= issued_at
    ),
    CONSTRAINT identity_action_secrets_superseded_after_issue CHECK (
        superseded_at IS NULL OR superseded_at >= issued_at
    ),
    CONSTRAINT identity_action_secrets_supersession_link CHECK (
        (superseded_at IS NULL AND superseded_by_id IS NULL)
        OR (superseded_at IS NOT NULL AND superseded_by_id IS NOT NULL)
    ),
    CONSTRAINT identity_action_secrets_attempt_count CHECK (
        attempt_count BETWEEN 0 AND 1000000
    ),
    CONSTRAINT identity_action_secrets_attempt_evidence CHECK (
        (
            attempt_count = 0
            AND first_attempt_at IS NULL
            AND last_attempt_at IS NULL
        )
        OR (
            attempt_count > 0
            AND first_attempt_at IS NOT NULL
            AND last_attempt_at IS NOT NULL
            AND first_attempt_at >= issued_at
            AND last_attempt_at >= first_attempt_at
        )
    ),
    CONSTRAINT identity_action_secrets_superseded_by_fk
        FOREIGN KEY (superseded_by_id)
        REFERENCES identity_action_secrets (id)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE UNIQUE INDEX identity_action_secrets_one_live_per_purpose
    ON identity_action_secrets (account_id, purpose)
    WHERE consumed_at IS NULL AND superseded_at IS NULL;
CREATE INDEX identity_action_secrets_digest_purpose_idx
    ON identity_action_secrets (secret_digest, purpose);
CREATE INDEX identity_action_secrets_live_expiry_idx
    ON identity_action_secrets (expires_at)
    WHERE consumed_at IS NULL AND superseded_at IS NULL;

CREATE FUNCTION identity_action_secrets_enforce_lifecycle() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.account_id IS DISTINCT FROM OLD.account_id
        OR NEW.purpose IS DISTINCT FROM OLD.purpose
        OR NEW.secret_digest IS DISTINCT FROM OLD.secret_digest
        OR NEW.issued_at IS DISTINCT FROM OLD.issued_at
        OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION 'identity action-secret issuance is immutable (secret %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF OLD.consumed_at IS NOT NULL AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'a consumed identity action secret is terminal (secret %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.superseded_at IS NOT NULL AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'a superseded identity action secret is terminal (secret %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.consumed_at IS NULL AND NEW.consumed_at IS NOT NULL
        AND (NEW.superseded_at IS NOT NULL OR NEW.superseded_by_id IS NOT NULL)
    THEN
        RAISE EXCEPTION 'a consumed identity action secret cannot be superseded (secret %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.superseded_at IS NULL AND NEW.superseded_at IS NOT NULL
        AND NEW.consumed_at IS NOT NULL
    THEN
        RAISE EXCEPTION 'a superseded identity action secret cannot be consumed (secret %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.attempt_count < OLD.attempt_count
        OR (
            OLD.first_attempt_at IS NOT NULL
            AND NEW.first_attempt_at IS DISTINCT FROM OLD.first_attempt_at
        )
        OR (
            OLD.last_attempt_at IS NOT NULL
            AND NEW.last_attempt_at < OLD.last_attempt_at
        )
    THEN
        RAISE EXCEPTION 'identity action-secret attempt evidence cannot regress (secret %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER identity_action_secrets_lifecycle
    BEFORE UPDATE ON identity_action_secrets
    FOR EACH ROW EXECUTE FUNCTION identity_action_secrets_enforce_lifecycle();

CREATE TABLE identity_security_events (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    event_type        TEXT NOT NULL,
    account_id        UUID REFERENCES accounts (id),
    action_secret_id  UUID REFERENCES identity_action_secrets (id),
    account_revision  INTEGER,
    request_id        TEXT NOT NULL,
    evidence          JSONB NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT identity_security_events_type CHECK (
        event_type IN (
            'BOOTSTRAP_ADMIN_CREATED',
            'STUDENT_REGISTRATION_ACCEPTED',
            'EMAIL_VERIFICATION_REISSUED',
            'STUDENT_EMAIL_VERIFIED'
        )
    ),
    CONSTRAINT identity_security_events_revision CHECK (
        account_revision IS NULL OR account_revision >= 1
    ),
    CONSTRAINT identity_security_events_request_present CHECK (
        length(trim(request_id)) BETWEEN 1 AND 200
    ),
    CONSTRAINT identity_security_events_evidence_object CHECK (
        jsonb_typeof(evidence) = 'object'
    )
);

CREATE INDEX identity_security_events_account_time_idx
    ON identity_security_events (account_id, occurred_at DESC)
    WHERE account_id IS NOT NULL;
CREATE INDEX identity_security_events_type_time_idx
    ON identity_security_events (event_type, occurred_at DESC);
CREATE INDEX identity_security_events_request_idx
    ON identity_security_events (request_id);

CREATE TABLE outbox_events (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type          TEXT NOT NULL,
    schema_version      INTEGER NOT NULL,
    source_module       TEXT NOT NULL,
    aggregate_type      TEXT NOT NULL,
    aggregate_id        UUID NOT NULL,
    aggregate_revision  INTEGER NOT NULL,
    safe_payload        JSONB NOT NULL,
    occurred_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    available_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    correlation_id      TEXT NOT NULL,

    CONSTRAINT outbox_events_type_format CHECK (
        event_type ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'
    ),
    CONSTRAINT outbox_events_schema_version CHECK (schema_version >= 1),
    CONSTRAINT outbox_events_source_module CHECK (source_module = 'IDENTITY_AND_ACCESS'),
    CONSTRAINT outbox_events_aggregate_type CHECK (
        aggregate_type ~ '^[A-Z][A-Z0-9_]*$'
    ),
    CONSTRAINT outbox_events_aggregate_revision CHECK (aggregate_revision >= 1),
    CONSTRAINT outbox_events_safe_payload_object CHECK (
        jsonb_typeof(safe_payload) = 'object'
    ),
    CONSTRAINT outbox_events_availability CHECK (available_at >= occurred_at),
    CONSTRAINT outbox_events_correlation_present CHECK (
        length(trim(correlation_id)) BETWEEN 1 AND 200
    )
);

CREATE INDEX outbox_events_available_idx
    ON outbox_events (available_at, occurred_at);
CREATE INDEX outbox_events_aggregate_idx
    ON outbox_events (aggregate_type, aggregate_id, aggregate_revision);
CREATE INDEX outbox_events_type_idx
    ON outbox_events (event_type, occurred_at DESC);

CREATE TABLE outbox_protected_payloads (
    event_id     UUID PRIMARY KEY REFERENCES outbox_events (id),
    key_version  TEXT NOT NULL,
    nonce        BYTEA NOT NULL,
    ciphertext   BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT outbox_protected_payloads_key_version_present CHECK (
        length(trim(key_version)) BETWEEN 1 AND 200
    ),
    CONSTRAINT outbox_protected_payloads_nonce_size CHECK (
        octet_length(nonce) = 12
    ),
    CONSTRAINT outbox_protected_payloads_ciphertext_size CHECK (
        octet_length(ciphertext) >= 16
    ),
    CONSTRAINT outbox_protected_payloads_nonce_unique UNIQUE (key_version, nonce)
);

CREATE FUNCTION immutable_evidence_reject_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only (attempted %)', TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER policy_acceptances_append_only
    BEFORE UPDATE OR DELETE ON policy_acceptances
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();
CREATE TRIGGER identity_security_events_append_only
    BEFORE UPDATE OR DELETE ON identity_security_events
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();
CREATE TRIGGER outbox_events_append_only
    BEFORE UPDATE OR DELETE ON outbox_events
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();
CREATE TRIGGER outbox_protected_payloads_append_only
    BEFORE UPDATE OR DELETE ON outbox_protected_payloads
    FOR EACH ROW EXECUTE FUNCTION immutable_evidence_reject_mutation();
