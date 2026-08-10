-- S9 transactional email delivery lifecycle. The source outbox remains
-- immutable; this ledger owns mutable provider-attempt state.

CREATE TABLE transactional_email_deliveries (
    event_id              UUID PRIMARY KEY REFERENCES outbox_events (id) ON DELETE RESTRICT,
    template_contract     TEXT NOT NULL,
    locale                TEXT NOT NULL,
    status                TEXT NOT NULL DEFAULT 'QUEUED',
    attempt_count         SMALLINT NOT NULL DEFAULT 0,
    next_attempt_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_token           UUID,
    lease_expires_at      TIMESTAMPTZ,
    provider              TEXT NOT NULL,
    provider_message_id   TEXT,
    last_failure_class    TEXT,
    last_provider_code    TEXT,
    queued_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at           TIMESTAMPTZ,
    terminal_at           TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT transactional_email_template_present CHECK (
        length(trim(template_contract)) BETWEEN 1 AND 120
    ),
    CONSTRAINT transactional_email_locale CHECK (locale IN ('ar', 'en')),
    CONSTRAINT transactional_email_status CHECK (
        status IN ('QUEUED', 'SENDING', 'ACCEPTED', 'PERMANENT_FAILED', 'EXHAUSTED')
    ),
    CONSTRAINT transactional_email_attempt_count CHECK (attempt_count BETWEEN 0 AND 5),
    CONSTRAINT transactional_email_provider CHECK (provider IN ('fake', 'resend')),
    CONSTRAINT transactional_email_lease_pair CHECK (
        (lease_token IS NULL AND lease_expires_at IS NULL)
        OR (lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)
    ),
    CONSTRAINT transactional_email_sending_lease CHECK (
        (status = 'SENDING' AND lease_token IS NOT NULL)
        OR (status <> 'SENDING' AND lease_token IS NULL)
    ),
    CONSTRAINT transactional_email_acceptance CHECK (
        (status = 'ACCEPTED' AND accepted_at IS NOT NULL AND provider_message_id IS NOT NULL)
        OR (status <> 'ACCEPTED' AND accepted_at IS NULL)
    ),
    CONSTRAINT transactional_email_terminal_time CHECK (
        (status IN ('PERMANENT_FAILED', 'EXHAUSTED') AND terminal_at IS NOT NULL)
        OR (status NOT IN ('PERMANENT_FAILED', 'EXHAUSTED') AND terminal_at IS NULL)
    ),
    CONSTRAINT transactional_email_provider_message_id_length CHECK (
        provider_message_id IS NULL OR length(provider_message_id) BETWEEN 1 AND 200
    ),
    CONSTRAINT transactional_email_failure_class_length CHECK (
        last_failure_class IS NULL OR length(last_failure_class) BETWEEN 1 AND 80
    ),
    CONSTRAINT transactional_email_provider_code_length CHECK (
        last_provider_code IS NULL OR length(last_provider_code) BETWEEN 1 AND 80
    )
);

CREATE INDEX transactional_email_dispatch_idx
    ON transactional_email_deliveries (next_attempt_at, event_id)
    WHERE status = 'QUEUED';

CREATE INDEX transactional_email_stale_lease_idx
    ON transactional_email_deliveries (lease_expires_at, event_id)
    WHERE status = 'SENDING';

CREATE TABLE transactional_email_attempts (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id              UUID NOT NULL REFERENCES transactional_email_deliveries (event_id) ON DELETE RESTRICT,
    attempt_number        SMALLINT NOT NULL,
    lease_token           UUID NOT NULL,
    outcome               TEXT NOT NULL DEFAULT 'STARTED',
    failure_class         TEXT,
    provider_code         TEXT,
    provider_message_id   TEXT,
    retry_at              TIMESTAMPTZ,
    started_at            TIMESTAMPTZ NOT NULL,
    finished_at           TIMESTAMPTZ,

    CONSTRAINT transactional_email_attempt_identity UNIQUE (event_id, attempt_number),
    CONSTRAINT transactional_email_attempt_number CHECK (attempt_number BETWEEN 1 AND 5),
    CONSTRAINT transactional_email_attempt_outcome CHECK (
        outcome IN ('STARTED', 'ACCEPTED', 'TRANSIENT_FAILURE', 'PERMANENT_FAILURE', 'EXHAUSTED')
    ),
    CONSTRAINT transactional_email_attempt_finished CHECK (
        (outcome = 'STARTED' AND finished_at IS NULL)
        OR (outcome <> 'STARTED' AND finished_at IS NOT NULL)
    ),
    CONSTRAINT transactional_email_attempt_retry CHECK (
        (outcome = 'TRANSIENT_FAILURE' AND retry_at IS NOT NULL)
        OR (outcome <> 'TRANSIENT_FAILURE' AND retry_at IS NULL)
    ),
    CONSTRAINT transactional_email_attempt_provider_message_length CHECK (
        provider_message_id IS NULL OR length(provider_message_id) BETWEEN 1 AND 200
    ),
    CONSTRAINT transactional_email_attempt_failure_length CHECK (
        failure_class IS NULL OR length(failure_class) BETWEEN 1 AND 80
    ),
    CONSTRAINT transactional_email_attempt_provider_code_length CHECK (
        provider_code IS NULL OR length(provider_code) BETWEEN 1 AND 80
    )
);

CREATE INDEX transactional_email_attempt_event_idx
    ON transactional_email_attempts (event_id, attempt_number DESC);
