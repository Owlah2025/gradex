-- Dropping the indexes leaves every purchase request row intact, including the
-- ownership already recorded on it. The column predates this migration and is
-- not removed here.
DROP INDEX IF EXISTS purchase_requests_one_active_course_student;
DROP INDEX IF EXISTS purchase_requests_requester_idx;
