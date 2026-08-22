-- Automated manual-payment purchase request flow.
--
-- This expands the existing Course Access Invitation lifecycle; it does not
-- replace its standard two-step approval path.

ALTER TABLE entitlements
    DROP CONSTRAINT entitlements_grant_source_implemented;

ALTER TABLE entitlements
    ADD CONSTRAINT entitlements_grant_source_implemented
        CHECK (grant_source IN ('MANUAL_INVITATION', 'PURCHASE_REQUEST'));

ALTER TABLE entitlements
    ADD CONSTRAINT ent_purchase_needs_invitation
        CHECK (grant_source <> 'PURCHASE_REQUEST' OR source_invitation_id IS NOT NULL)
        NOT VALID;

CREATE TABLE purchase_requests (
    id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reference_code                  TEXT NOT NULL UNIQUE,
    course_id                       UUID NOT NULL REFERENCES courses (id),
    email                           TEXT NOT NULL,
    normalized_email                TEXT NOT NULL,
    requester_account_id            UUID REFERENCES accounts (id) ON DELETE SET NULL,
    course_title_ar                 TEXT NOT NULL,
    course_title_en                 TEXT NOT NULL,
    price_minor_units               BIGINT NOT NULL CHECK (price_minor_units >= 0),
    currency                        TEXT NOT NULL CHECK (currency = 'KWD'),
    state                           TEXT NOT NULL,
    payment_confirmed_by_account_id UUID REFERENCES accounts (id),
    invitation_id                   UUID UNIQUE REFERENCES course_access_invitations (id),
    access_ends_at_snapshot         TIMESTAMPTZ,
    requested_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    payment_confirmed_at            TIMESTAMPTZ,
    invitation_created_at           TIMESTAMPTZ,
    access_granted_at               TIMESTAMPTZ,
    cancelled_at                    TIMESTAMPTZ,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT purchase_requests_state_valid CHECK (
        state IN ('WAITING_PAYMENT', 'INVITATION_CREATED', 'ACCESS_GRANTED', 'CANCELLED')
    ),
    CONSTRAINT purchase_requests_email_present CHECK (
        length(trim(email)) > 0 AND length(trim(normalized_email)) > 0
    ),
    CONSTRAINT purchase_requests_transition_coherent CHECK (
        (state = 'WAITING_PAYMENT'
            AND payment_confirmed_by_account_id IS NULL
            AND payment_confirmed_at IS NULL
            AND invitation_id IS NULL
            AND invitation_created_at IS NULL
            AND access_ends_at_snapshot IS NULL
            AND access_granted_at IS NULL
            AND cancelled_at IS NULL)
        OR (state = 'INVITATION_CREATED'
            AND payment_confirmed_by_account_id IS NOT NULL
            AND payment_confirmed_at IS NOT NULL
            AND invitation_id IS NOT NULL
            AND invitation_created_at IS NOT NULL
            AND access_ends_at_snapshot IS NOT NULL
            AND access_granted_at IS NULL
            AND cancelled_at IS NULL)
        OR (state = 'ACCESS_GRANTED'
            AND payment_confirmed_by_account_id IS NOT NULL
            AND payment_confirmed_at IS NOT NULL
            AND invitation_id IS NOT NULL
            AND invitation_created_at IS NOT NULL
            AND access_ends_at_snapshot IS NOT NULL
            AND access_granted_at IS NOT NULL
            AND cancelled_at IS NULL)
        OR (state = 'CANCELLED'
            AND cancelled_at IS NOT NULL
            AND access_granted_at IS NULL)
    )
);

CREATE UNIQUE INDEX purchase_requests_one_active_course_email
    ON purchase_requests (course_id, normalized_email)
    WHERE state IN ('WAITING_PAYMENT', 'INVITATION_CREATED');

CREATE INDEX purchase_requests_admin_queue
    ON purchase_requests (state, requested_at DESC);

CREATE INDEX purchase_requests_admin_email
    ON purchase_requests (normalized_email, requested_at DESC);

CREATE INDEX purchase_requests_admin_reference
    ON purchase_requests (reference_code);
