-- Reverses the direct-grant widening.
--
-- Narrowing is only safe while nothing has used the widened shape. A direct
-- purchase Entitlement, or a COURSE request granted without an invitation,
-- cannot be expressed at schema 43, and rolling back underneath one would make
-- live Student access unrepresentable. This refuses instead, matching the
-- guard 0021 and 0036 already apply to live purchase data.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM entitlements
         WHERE grant_source = 'PURCHASE_REQUEST' AND source_invitation_id IS NULL
    ) THEN
        RAISE EXCEPTION 'cannot roll back 0044: direct purchase-backed entitlements exist; they cannot be represented before this migration';
    END IF;
    IF EXISTS (
        SELECT 1 FROM purchase_requests
         WHERE target_kind = 'COURSE' AND state = 'ACCESS_GRANTED' AND invitation_id IS NULL
    ) THEN
        RAISE EXCEPTION 'cannot roll back 0044: directly granted COURSE purchase requests exist; they cannot be represented before this migration';
    END IF;
END $$;

ALTER TABLE purchase_requests
    DROP CONSTRAINT purchase_requests_transition_coherent;

ALTER TABLE purchase_requests
    ADD CONSTRAINT purchase_requests_transition_coherent
        CHECK (
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

ALTER TABLE entitlements
    DROP CONSTRAINT ent_bundle_purchase_source_valid;

ALTER TABLE entitlements
    ADD CONSTRAINT ent_bundle_purchase_source_valid
        CHECK (
            (grant_source = 'BUNDLE_PURCHASE'
                AND source_invitation_id IS NULL AND source_purchase_request_id IS NOT NULL)
            OR
            (grant_source <> 'BUNDLE_PURCHASE' AND source_purchase_request_id IS NULL)
        );

ALTER TABLE entitlements
    DROP CONSTRAINT ent_purchase_source_valid;

ALTER TABLE entitlements
    ADD CONSTRAINT ent_purchase_needs_invitation
        CHECK (grant_source <> 'PURCHASE_REQUEST' OR source_invitation_id IS NOT NULL);
