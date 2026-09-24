-- Direct Course access on Admin payment confirmation.
--
-- Externally paid COURSE purchases previously reached an Entitlement only
-- through an invitation the Student had to accept. Admin confirmation is now
-- the authoritative grant, so an Entitlement must be representable with a
-- purchase request as its provenance and no invitation at all.
--
-- This migration only WIDENS what is representable. Every shape that was legal
-- at schema 43 stays legal here, because historical invitation-backed purchases
-- and their Entitlements must continue to read and validate unchanged. Nothing
-- is rewritten, backfilled, or deleted.
--
-- Each constraint is replaced as ADD ... NOT VALID followed by a separate
-- VALIDATE CONSTRAINT, which is the pattern 0015 and 0021 already use here: the
-- brief ACCESS EXCLUSIVE lock covers a catalog update only, and the row scan
-- runs under SHARE UPDATE EXCLUSIVE, which does not block readers or writers.

-- 1. Entitlement provenance for grant_source = 'PURCHASE_REQUEST'.
--
-- Replaces ent_purchase_needs_invitation, which required an invitation
-- unconditionally. A purchase-backed Entitlement now carries exactly one
-- provenance: the invitation that produced it historically, or the purchase
-- request that produced it directly. Requiring exactly one rather than at least
-- one keeps the lineage unambiguous.
ALTER TABLE entitlements
    DROP CONSTRAINT ent_purchase_needs_invitation;

ALTER TABLE entitlements
    ADD CONSTRAINT ent_purchase_source_valid
        CHECK (
            grant_source <> 'PURCHASE_REQUEST'
            OR (source_invitation_id IS NOT NULL AND source_purchase_request_id IS NULL)
            OR (source_invitation_id IS NULL AND source_purchase_request_id IS NOT NULL)
        )
        NOT VALID;

ALTER TABLE entitlements
    VALIDATE CONSTRAINT ent_purchase_source_valid;

-- 2. Permit source_purchase_request_id on a direct purchase grant.
--
-- The 0036 form allowed that column for BUNDLE_PURCHASE only and forced it NULL
-- everywhere else, which is precisely what a direct COURSE grant needs to
-- record. BUNDLE_PURCHASE keeps its exact original requirement, and every
-- grant source other than the two purchase kinds is still forbidden from
-- carrying a purchase request.
ALTER TABLE entitlements
    DROP CONSTRAINT ent_bundle_purchase_source_valid;

ALTER TABLE entitlements
    ADD CONSTRAINT ent_bundle_purchase_source_valid
        CHECK (
            (grant_source = 'BUNDLE_PURCHASE'
                AND source_invitation_id IS NULL AND source_purchase_request_id IS NOT NULL)
            OR
            (grant_source = 'PURCHASE_REQUEST')
            OR
            (grant_source NOT IN ('BUNDLE_PURCHASE', 'PURCHASE_REQUEST')
                AND source_purchase_request_id IS NULL)
        )
        NOT VALID;

ALTER TABLE entitlements
    VALIDATE CONSTRAINT ent_bundle_purchase_source_valid;

-- 3. A COURSE purchase request may reach ACCESS_GRANTED without an invitation.
--
-- Only the COURSE/ACCESS_GRANTED branch changes, and it changes by adding an
-- alternative: an invitation-backed grant (every historical row) or a direct
-- grant with no invitation. The payment-confirmation and expiry-snapshot
-- evidence stays mandatory in both, so a granted request can never be missing
-- the facts that justify it. WAITING_PAYMENT, INVITATION_CREATED, CANCELLED and
-- the whole BUNDLE branch are reproduced exactly as 0036 wrote them.
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
                    AND access_ends_at_snapshot IS NOT NULL AND access_granted_at IS NOT NULL
                    AND cancelled_at IS NULL
                    AND (
                        (invitation_id IS NOT NULL AND invitation_created_at IS NOT NULL)
                        OR
                        (invitation_id IS NULL AND invitation_created_at IS NULL)
                    ))
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
        )
        NOT VALID;

ALTER TABLE purchase_requests
    VALIDATE CONSTRAINT purchase_requests_transition_coherent;
