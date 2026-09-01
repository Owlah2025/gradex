-- A Course purchase request now belongs to an authenticated, verified Student.
--
-- purchase_requests.requester_account_id already exists (0021) and has never
-- been written. This migration adds no column: it makes the existing one
-- meaningful, indexes it for the reads the Student surface performs, and adds
-- the active-request uniqueness that ownership makes possible.
--
-- Historical rows stay NULL on purpose. They were created anonymously before
-- authentication was required and there is no sound way to attribute them
-- after the fact; inventing an owner would be worse than recording that they
-- had none. NULLs do not participate in the unique index, so those rows never
-- collide with a new one.
--
-- Both statements are additive and neither rewrites the table. CONCURRENTLY is
-- deliberately not used: golang-migrate runs each file in one transaction, and
-- purchase_requests is a small operational queue where a brief ACCESS
-- EXCLUSIVE lock during index creation is cheaper than splitting the change
-- across a non-transactional migration.

CREATE INDEX IF NOT EXISTS purchase_requests_requester_idx
    ON purchase_requests (requester_account_id, requested_at DESC)
    WHERE requester_account_id IS NOT NULL;

-- One active request per Student per Course. This complements, and does not
-- replace, purchase_requests_one_active_course_email: the email index still
-- covers the historical anonymous rows, and the server derives the email from
-- the authenticated Account, so the two can never disagree for a new request.
CREATE UNIQUE INDEX IF NOT EXISTS purchase_requests_one_active_course_student
    ON purchase_requests (course_id, requester_account_id)
    WHERE requester_account_id IS NOT NULL
      AND state IN ('WAITING_PAYMENT', 'INVITATION_CREATED');
