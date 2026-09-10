-- Refuse destructive rollback once 0036 commerce data exists.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM bundles)
        OR EXISTS (SELECT 1 FROM bundle_price_changes)
        OR EXISTS (SELECT 1 FROM purchase_requests WHERE target_kind = 'BUNDLE')
        OR EXISTS (SELECT 1 FROM entitlements WHERE grant_source = 'BUNDLE_PURCHASE')
        OR EXISTS (SELECT 1 FROM course_price_changes WHERE offer_price_minor_units IS NOT NULL)
        -- Every Course purchase request created on 0036 records the regular
        -- price its quote was taken against. Dropping the column would destroy
        -- the only evidence of what a Student was actually offered, so a
        -- data-bearing rollback refuses rather than silently discarding it.
        OR EXISTS (SELECT 1 FROM purchase_requests WHERE regular_price_minor_units IS NOT NULL)
    THEN
        RAISE EXCEPTION 'cannot roll back 0036: Bundle or offer commerce data exists';
    END IF;
END;
$$;

DROP INDEX IF EXISTS bundle_purchase_grants_bundle_idx;
DROP INDEX IF EXISTS bundle_purchase_grants_entitlement_idx;
DROP TABLE IF EXISTS bundle_purchase_grants;

ALTER TABLE entitlements
    DROP CONSTRAINT ent_bundle_purchase_source_valid,
    DROP CONSTRAINT entitlements_grant_source_implemented,
    DROP COLUMN source_purchase_request_id,
    ADD CONSTRAINT entitlements_grant_source_implemented
        CHECK (grant_source IN ('MANUAL_INVITATION', 'PURCHASE_REQUEST'));

DROP TRIGGER IF EXISTS purchase_bundle_items_immutable ON purchase_request_bundle_items;
DROP FUNCTION IF EXISTS purchase_bundle_item_reject_mutation();
DROP TRIGGER IF EXISTS purchase_bundle_item_target ON purchase_request_bundle_items;
DROP FUNCTION IF EXISTS purchase_bundle_item_enforce_target();
DROP INDEX IF EXISTS purchase_bundle_items_order_idx;
DROP TABLE IF EXISTS purchase_request_bundle_items;

DROP INDEX IF EXISTS purchase_requests_one_active_bundle_email;
DROP INDEX IF EXISTS purchase_requests_one_active_bundle_student;

ALTER TABLE purchase_requests
    DROP CONSTRAINT purchase_requests_transition_coherent,
    DROP CONSTRAINT purchase_requests_bundle_identity_unique,
    DROP CONSTRAINT purchase_requests_offer_quote_valid,
    DROP CONSTRAINT purchase_requests_regular_price_valid,
    DROP CONSTRAINT purchase_requests_target_valid,
    DROP COLUMN regular_price_minor_units,
    DROP COLUMN bundle_title_en,
    DROP COLUMN bundle_title_ar,
    DROP COLUMN bundle_revision,
    DROP COLUMN bundle_id,
    DROP COLUMN target_kind,
    ALTER COLUMN course_title_en SET NOT NULL,
    ALTER COLUMN course_title_ar SET NOT NULL,
    ALTER COLUMN course_id SET NOT NULL,
    ADD CONSTRAINT purchase_requests_transition_coherent CHECK (
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
    );

DROP INDEX IF EXISTS bundle_price_changes_latest_idx;
DROP TABLE IF EXISTS bundle_price_changes;
DROP INDEX IF EXISTS bundle_courses_course_idx;
DROP INDEX IF EXISTS bundle_courses_order_idx;
DROP TABLE IF EXISTS bundle_courses;
DROP INDEX IF EXISTS bundles_public_list_idx;
DROP TABLE IF EXISTS bundles;
DROP TYPE IF EXISTS bundle_lifecycle;

ALTER TABLE course_price_changes
    DROP CONSTRAINT course_price_changes_offer_valid,
    DROP CONSTRAINT course_price_changes_old_offer_valid,
    DROP COLUMN offer_price_minor_units,
    DROP COLUMN old_offer_price_minor_units;
