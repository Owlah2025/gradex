-- Purchase history and purchase-sourced grants cannot be represented by the
-- pre-0021 access schema. Refuse before any destructive DDL rather than
-- silently deleting paid-access evidence during an operator rollback.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM entitlements WHERE grant_source = 'PURCHASE_REQUEST') THEN
        RAISE EXCEPTION 'cannot roll back 0021: PURCHASE_REQUEST entitlements exist; retain schema 0021 or remove the live purchase data through an approved migration';
    END IF;
    IF EXISTS (SELECT 1 FROM purchase_requests) THEN
        RAISE EXCEPTION 'cannot roll back 0021: purchase request records exist; retain schema 0021 or archive them through an approved migration';
    END IF;
END $$;

DROP INDEX IF EXISTS purchase_requests_admin_reference;
DROP INDEX IF EXISTS purchase_requests_admin_email;
DROP INDEX IF EXISTS purchase_requests_admin_queue;
DROP INDEX IF EXISTS purchase_requests_one_active_course_email;
DROP TABLE IF EXISTS purchase_requests;
DROP SEQUENCE IF EXISTS purchase_request_reference_sequence;

ALTER TABLE entitlements
    DROP CONSTRAINT IF EXISTS ent_purchase_needs_invitation;

ALTER TABLE entitlements
    DROP CONSTRAINT IF EXISTS entitlements_grant_source_implemented;

ALTER TABLE entitlements
    ADD CONSTRAINT entitlements_grant_source_implemented
        CHECK (grant_source = 'MANUAL_INVITATION');
