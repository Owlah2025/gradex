-- Bundles V1 and Catalog Offers V1.
-- Money remains integer KWD fils. Existing Course commerce rows retain their values.

ALTER TABLE course_price_changes
    ADD COLUMN old_offer_price_minor_units BIGINT,
    ADD COLUMN offer_price_minor_units BIGINT,
    ADD CONSTRAINT course_price_changes_old_offer_valid CHECK (
        old_offer_price_minor_units IS NULL OR old_offer_price_minor_units > 0
    ),
    ADD CONSTRAINT course_price_changes_offer_valid CHECK (
        offer_price_minor_units IS NULL
        OR (offer_price_minor_units > 0 AND offer_price_minor_units < new_value_minor_units)
    );

CREATE TYPE bundle_lifecycle AS ENUM ('DRAFT', 'PUBLISHED', 'DELISTED', 'ARCHIVED');

CREATE TABLE bundles (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                  TEXT GENERATED ALWAYS AS ('bundle-' || replace(id::text, '-', '')) STORED,
    lifecycle             bundle_lifecycle NOT NULL DEFAULT 'DRAFT',
    title_ar              TEXT NOT NULL,
    title_en              TEXT NOT NULL,
    description_ar        TEXT NOT NULL DEFAULT '',
    description_en        TEXT NOT NULL DEFAULT '',
    revision              BIGINT NOT NULL DEFAULT 1,
    created_by_account_id UUID NOT NULL REFERENCES accounts (id),
    updated_by_account_id UUID NOT NULL REFERENCES accounts (id),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT bundles_slug_unique UNIQUE (slug),
    CONSTRAINT bundles_title_ar_present CHECK (length(trim(title_ar)) > 0),
    CONSTRAINT bundles_title_en_present CHECK (length(trim(title_en)) > 0),
    CONSTRAINT bundles_revision_positive CHECK (revision >= 1)
);

CREATE INDEX bundles_public_list_idx
    ON bundles (updated_at DESC, id DESC)
    WHERE lifecycle = 'PUBLISHED';

CREATE TABLE bundle_courses (
    bundle_id UUID NOT NULL REFERENCES bundles (id),
    course_id UUID NOT NULL REFERENCES courses (id),
    position  INTEGER NOT NULL,

    PRIMARY KEY (bundle_id, course_id),
    CONSTRAINT bundle_courses_position_unique UNIQUE (bundle_id, position),
    CONSTRAINT bundle_courses_position_non_negative CHECK (position >= 0)
);

CREATE INDEX bundle_courses_order_idx ON bundle_courses (bundle_id, position, course_id);
CREATE INDEX bundle_courses_course_idx ON bundle_courses (course_id, bundle_id);

CREATE TABLE bundle_price_changes (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bundle_id                   UUID NOT NULL REFERENCES bundles (id),
    old_value_minor_units       BIGINT,
    new_value_minor_units       BIGINT NOT NULL,
    old_offer_price_minor_units BIGINT,
    offer_price_minor_units     BIGINT,
    changed_by_account_id       UUID NOT NULL REFERENCES accounts (id),
    reason                      TEXT NOT NULL,
    changed_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT bundle_price_old_value_valid CHECK (
        old_value_minor_units IS NULL OR old_value_minor_units >= 0
    ),
    CONSTRAINT bundle_price_value_valid CHECK (new_value_minor_units >= 0),
    CONSTRAINT bundle_price_old_offer_valid CHECK (
        old_offer_price_minor_units IS NULL OR old_offer_price_minor_units > 0
    ),
    CONSTRAINT bundle_price_offer_valid CHECK (
        offer_price_minor_units IS NULL
        OR (offer_price_minor_units > 0 AND offer_price_minor_units < new_value_minor_units)
    ),
    CONSTRAINT bundle_price_reason_present CHECK (length(trim(reason)) > 0)
);

CREATE INDEX bundle_price_changes_latest_idx
    ON bundle_price_changes (bundle_id, changed_at DESC, id DESC);

ALTER TABLE purchase_requests
    ADD COLUMN target_kind TEXT NOT NULL DEFAULT 'COURSE',
    ADD COLUMN bundle_id UUID REFERENCES bundles (id),
    ADD COLUMN bundle_revision BIGINT,
    ADD COLUMN bundle_title_ar TEXT,
    ADD COLUMN bundle_title_en TEXT,
    ADD COLUMN regular_price_minor_units BIGINT,
    ALTER COLUMN course_id DROP NOT NULL,
    ALTER COLUMN course_title_ar DROP NOT NULL,
    ALTER COLUMN course_title_en DROP NOT NULL,
    DROP CONSTRAINT purchase_requests_transition_coherent,
    ADD CONSTRAINT purchase_requests_target_valid CHECK (
        (target_kind = 'COURSE'
            AND course_id IS NOT NULL AND bundle_id IS NULL
            AND bundle_revision IS NULL AND bundle_title_ar IS NULL AND bundle_title_en IS NULL)
        OR
        (target_kind = 'BUNDLE'
            AND course_id IS NULL AND bundle_id IS NOT NULL
            AND bundle_revision IS NOT NULL AND bundle_revision >= 1
            AND bundle_title_ar IS NOT NULL AND length(trim(bundle_title_ar)) > 0
            AND bundle_title_en IS NOT NULL AND length(trim(bundle_title_en)) > 0)
    ),
    ADD CONSTRAINT purchase_requests_regular_price_valid CHECK (
        regular_price_minor_units IS NULL OR regular_price_minor_units >= price_minor_units
    ),
    ADD CONSTRAINT purchase_requests_offer_quote_valid CHECK (
        regular_price_minor_units IS NULL
        OR regular_price_minor_units = price_minor_units
        OR (price_minor_units > 0 AND price_minor_units < regular_price_minor_units)
    ),
    ADD CONSTRAINT purchase_requests_bundle_identity_unique UNIQUE (id, bundle_id),
    ADD CONSTRAINT purchase_requests_transition_coherent CHECK (
        (target_kind = 'COURSE' AND (
            (state = 'WAITING_PAYMENT'
                AND payment_confirmed_by_account_id IS NULL AND payment_confirmed_at IS NULL
                AND invitation_id IS NULL AND invitation_created_at IS NULL
                AND access_ends_at_snapshot IS NULL AND access_granted_at IS NULL AND cancelled_at IS NULL)
            OR (state = 'INVITATION_CREATED'
                AND payment_confirmed_by_account_id IS NOT NULL AND payment_confirmed_at IS NOT NULL
                AND invitation_id IS NOT NULL AND invitation_created_at IS NOT NULL
                AND access_ends_at_snapshot IS NOT NULL AND access_granted_at IS NULL AND cancelled_at IS NULL)
            OR (state = 'ACCESS_GRANTED'
                AND payment_confirmed_by_account_id IS NOT NULL AND payment_confirmed_at IS NOT NULL
                AND invitation_id IS NOT NULL AND invitation_created_at IS NOT NULL
                AND access_ends_at_snapshot IS NOT NULL AND access_granted_at IS NOT NULL AND cancelled_at IS NULL)
            OR (state = 'CANCELLED' AND cancelled_at IS NOT NULL AND access_granted_at IS NULL)
        ))
        OR
        (target_kind = 'BUNDLE' AND invitation_id IS NULL AND invitation_created_at IS NULL
            AND access_ends_at_snapshot IS NULL AND (
            (state = 'WAITING_PAYMENT'
                AND payment_confirmed_by_account_id IS NULL AND payment_confirmed_at IS NULL
                AND access_granted_at IS NULL AND cancelled_at IS NULL)
            OR (state = 'ACCESS_GRANTED'
                AND payment_confirmed_by_account_id IS NOT NULL AND payment_confirmed_at IS NOT NULL
                AND access_granted_at IS NOT NULL AND cancelled_at IS NULL)
            OR (state = 'CANCELLED' AND cancelled_at IS NOT NULL AND access_granted_at IS NULL)
        ))
    );

CREATE FUNCTION purchase_request_bundle_snapshot_immutable() RETURNS TRIGGER AS $$
BEGIN
    IF (OLD.target_kind = 'BUNDLE' OR NEW.target_kind = 'BUNDLE')
       AND (
           OLD.reference_code IS DISTINCT FROM NEW.reference_code
           OR OLD.email IS DISTINCT FROM NEW.email
           OR OLD.normalized_email IS DISTINCT FROM NEW.normalized_email
           OR OLD.requester_account_id IS DISTINCT FROM NEW.requester_account_id
           OR OLD.target_kind IS DISTINCT FROM NEW.target_kind
           OR OLD.course_id IS DISTINCT FROM NEW.course_id
           OR OLD.bundle_id IS DISTINCT FROM NEW.bundle_id
           OR OLD.bundle_revision IS DISTINCT FROM NEW.bundle_revision
           OR OLD.bundle_title_ar IS DISTINCT FROM NEW.bundle_title_ar
           OR OLD.bundle_title_en IS DISTINCT FROM NEW.bundle_title_en
           OR OLD.regular_price_minor_units IS DISTINCT FROM NEW.regular_price_minor_units
           OR OLD.price_minor_units IS DISTINCT FROM NEW.price_minor_units
           OR OLD.currency IS DISTINCT FROM NEW.currency
           OR OLD.requested_at IS DISTINCT FROM NEW.requested_at
       )
    THEN
        RAISE EXCEPTION 'Bundle purchase request commercial snapshot is immutable'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER purchase_requests_bundle_snapshot_immutable
    BEFORE UPDATE ON purchase_requests
    FOR EACH ROW EXECUTE FUNCTION purchase_request_bundle_snapshot_immutable();

CREATE UNIQUE INDEX purchase_requests_one_active_bundle_student
    ON purchase_requests (bundle_id, requester_account_id)
    WHERE target_kind = 'BUNDLE' AND requester_account_id IS NOT NULL
      AND state = 'WAITING_PAYMENT';

CREATE UNIQUE INDEX purchase_requests_one_active_bundle_email
    ON purchase_requests (bundle_id, normalized_email)
    WHERE target_kind = 'BUNDLE' AND state = 'WAITING_PAYMENT';

CREATE TABLE purchase_request_bundle_items (
    purchase_request_id UUID NOT NULL REFERENCES purchase_requests (id),
    course_id           UUID NOT NULL REFERENCES courses (id),
    position            INTEGER NOT NULL,
    course_title_ar     TEXT NOT NULL,
    course_title_en     TEXT NOT NULL,

    PRIMARY KEY (purchase_request_id, course_id),
    CONSTRAINT purchase_bundle_items_position_unique UNIQUE (purchase_request_id, position),
    CONSTRAINT purchase_bundle_items_position_non_negative CHECK (position >= 0),
    CONSTRAINT purchase_bundle_items_title_ar_present CHECK (length(trim(course_title_ar)) > 0),
    CONSTRAINT purchase_bundle_items_title_en_present CHECK (length(trim(course_title_en)) > 0)
);

CREATE INDEX purchase_bundle_items_order_idx
    ON purchase_request_bundle_items (purchase_request_id, position, course_id);

CREATE FUNCTION purchase_bundle_item_enforce_target() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM purchase_requests pr
        WHERE pr.id = NEW.purchase_request_id AND pr.target_kind = 'BUNDLE'
    ) THEN
        RAISE EXCEPTION 'bundle snapshot item requires a Bundle purchase request'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER purchase_bundle_item_target
    BEFORE INSERT ON purchase_request_bundle_items
    FOR EACH ROW EXECUTE FUNCTION purchase_bundle_item_enforce_target();

CREATE FUNCTION purchase_bundle_item_reject_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Bundle purchase snapshot items are immutable'
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER purchase_bundle_items_immutable
    BEFORE UPDATE OR DELETE ON purchase_request_bundle_items
    FOR EACH ROW EXECUTE FUNCTION purchase_bundle_item_reject_mutation();

ALTER TABLE entitlements
    DROP CONSTRAINT entitlements_grant_source_implemented,
    ADD COLUMN source_purchase_request_id UUID REFERENCES purchase_requests (id),
    ADD CONSTRAINT entitlements_grant_source_implemented CHECK (
        grant_source IN ('MANUAL_INVITATION', 'PURCHASE_REQUEST', 'BUNDLE_PURCHASE')
    ),
    ADD CONSTRAINT ent_bundle_purchase_source_valid CHECK (
        (grant_source = 'BUNDLE_PURCHASE'
            AND source_invitation_id IS NULL AND source_purchase_request_id IS NOT NULL)
        OR
        (grant_source <> 'BUNDLE_PURCHASE' AND source_purchase_request_id IS NULL)
    );

CREATE TABLE bundle_purchase_grants (
    purchase_request_id             UUID NOT NULL REFERENCES purchase_requests (id),
    bundle_id                       UUID NOT NULL REFERENCES bundles (id),
    course_id                       UUID NOT NULL REFERENCES courses (id),
    entitlement_id                  UUID NOT NULL REFERENCES entitlements (id),
    disposition                     TEXT NOT NULL,
    previous_access_ends_at         TIMESTAMPTZ,
    resulting_access_ends_at        TIMESTAMPTZ NOT NULL,
    granted_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (purchase_request_id, course_id),
    CONSTRAINT bundle_purchase_grants_disposition_valid CHECK (
        disposition IN ('GRANTED', 'PRESERVED', 'EXTENDED')
    ),
    CONSTRAINT bundle_purchase_grants_previous_coherent CHECK (
        (disposition = 'GRANTED' AND previous_access_ends_at IS NULL)
        OR (disposition IN ('PRESERVED', 'EXTENDED') AND previous_access_ends_at IS NOT NULL)
    ),
    CONSTRAINT bundle_purchase_grants_snapshot_fk
        FOREIGN KEY (purchase_request_id, course_id)
        REFERENCES purchase_request_bundle_items (purchase_request_id, course_id),
    CONSTRAINT bundle_purchase_grants_bundle_fk
        FOREIGN KEY (purchase_request_id, bundle_id)
        REFERENCES purchase_requests (id, bundle_id)
);

CREATE INDEX bundle_purchase_grants_entitlement_idx
    ON bundle_purchase_grants (entitlement_id, granted_at DESC);
CREATE INDEX bundle_purchase_grants_bundle_idx
    ON bundle_purchase_grants (bundle_id, granted_at DESC);
