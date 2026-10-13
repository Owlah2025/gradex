-- Reverses 0058.
--
-- Dropping these columns destroys every contact number Students have provided
-- and every request contact snapshot. Schema 57 cannot represent them, so this
-- refuses while any exist, matching the guard 0021, 0036 and 0044 apply to
-- live data. A deliberate rollback first exports the numbers, then clears them
-- through an approved, audited operation.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM accounts WHERE contact_phone IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back 0058: Students have provided contact numbers; schema 57 cannot hold them';
    END IF;
    IF EXISTS (SELECT 1 FROM purchase_requests WHERE contact_phone IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back 0058: purchase requests carry contact snapshots; schema 57 cannot hold them';
    END IF;
END $$;

DROP TRIGGER IF EXISTS purchase_requests_contact_snapshot_immutable ON purchase_requests;
DROP FUNCTION IF EXISTS purchase_request_contact_snapshot_immutable();

ALTER TABLE purchase_requests
    DROP CONSTRAINT IF EXISTS purchase_requests_contact_phone_e164,
    DROP COLUMN IF EXISTS contact_phone;

ALTER TABLE accounts
    DROP CONSTRAINT IF EXISTS accounts_contact_phone_updated_coherent,
    DROP CONSTRAINT IF EXISTS accounts_contact_phone_student_only,
    DROP CONSTRAINT IF EXISTS accounts_contact_phone_e164,
    DROP COLUMN IF EXISTS contact_phone_updated_at,
    DROP COLUMN IF EXISTS contact_phone;
