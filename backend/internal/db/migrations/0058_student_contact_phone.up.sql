-- Student contact phone and the per-request contact snapshot (D-117).
--
-- The mobile number the Gradex team uses to reach a Student about a Course
-- purchase request. The schema names the stored fact, a mobile contact
-- number, not the messaging channel the product presents it through (D-117).
--
-- Expand-only. Two nullable columns on accounts and one on purchase_requests.
-- No row is rewritten and nothing is backfilled: Gradex has never stored a
-- phone number anywhere, so there is no trustworthy existing number to copy,
-- and inventing one is exactly what this change must never do. Every row legal
-- at schema 57 stays legal here, and a missing number is NULL.
--
-- Adding a nullable column without a default is a catalogue-only change in
-- PostgreSQL. The CHECK constraints are validated against existing rows, all
-- of which are NULL in the new columns, so validation is a short scan.

ALTER TABLE accounts
    ADD COLUMN contact_phone            TEXT,
    ADD COLUMN contact_phone_updated_at TIMESTAMPTZ;

-- The application normalizes to E.164 before writing. This is the backstop
-- that keeps any other writer from storing a value a chat link cannot use.
ALTER TABLE accounts
    ADD CONSTRAINT accounts_contact_phone_e164 CHECK (
        contact_phone IS NULL OR contact_phone ~ '^\+[1-9][0-9]{6,14}$'
    ),
    -- Contact for purchase follow-up is a Student concern. Staff accounts
    -- have no reason to hold one, so the column cannot be used to hoard it.
    ADD CONSTRAINT accounts_contact_phone_student_only CHECK (
        contact_phone IS NULL OR role = 'STUDENT'
    ),
    ADD CONSTRAINT accounts_contact_phone_updated_coherent CHECK (
        (contact_phone IS NULL) = (contact_phone_updated_at IS NULL)
    );

-- The number the Student gave for this request, captured when the request was
-- created. Historical requests keep NULL: the Admin surface falls back to the
-- Student's current profile number for them, and says so.
ALTER TABLE purchase_requests
    ADD COLUMN contact_phone TEXT;

ALTER TABLE purchase_requests
    ADD CONSTRAINT purchase_requests_contact_phone_e164 CHECK (
        contact_phone IS NULL OR contact_phone ~ '^\+[1-9][0-9]{6,14}$'
    );

-- A snapshot is history. It may be filled once where it is missing — a Student
-- re-confirming an older request that predates this column — but a recorded
-- value is never replaced or erased.
CREATE FUNCTION purchase_request_contact_snapshot_immutable() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.contact_phone IS NOT NULL
       AND NEW.contact_phone IS DISTINCT FROM OLD.contact_phone
    THEN
        RAISE EXCEPTION 'purchase request contact snapshot is immutable once recorded'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER purchase_requests_contact_snapshot_immutable
    BEFORE UPDATE OF contact_phone ON purchase_requests
    FOR EACH ROW EXECUTE FUNCTION purchase_request_contact_snapshot_immutable();
