-- Reverses 0026 exactly. Only the T4-A.1 guard is removed.
--
-- No Subject row is read or written in either direction, so rolling down
-- restores the 0025 database exactly. Rolling down does reopen active-Subject
-- renumbering, because that guard is owned by 0026 and can only exist while 0026
-- is applied — the same deliberate asymmetry 0025 records for code reservation.

DROP TRIGGER IF EXISTS subjects_code_identity_guard ON subjects;
DROP FUNCTION IF EXISTS subjects_reject_code_identity_change();
