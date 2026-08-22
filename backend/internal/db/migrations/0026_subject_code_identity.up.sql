-- T4-A.1 (MVP-F20) Subject official-code identity hardening.
--
-- D-093 §7 established that an official Subject code stays reserved after
-- retirement. Migration 0025 closed reuse from two directions: a second Subject
-- cannot take a retired Subject's code, and a retired Subject cannot release its
-- own by clearing or changing it.
--
-- A third path remained open. An ACTIVE coded Subject could renumber itself:
--
--     Subject A: KU / 0418-320  ->  0418-999      (frees 0418320)
--     Subject B: KU / 0418-320                    (claims it)
--
-- which performs academic renumbering through an ordinary Admin edit and leaves
-- a published Gradex Course pointing at a Subject whose canonical identity has
-- silently changed. The amended D-093 §7 makes the normalized code part of
-- canonical Subject identity and immutable once established, so this migration
-- closes that path in the database.
--
-- Display formatting stays editable, because it is not identity:
--     '0418 320' -> '0418-320'     both normalize to 0418320   ALLOWED
--     '0418-320' -> '0418-321'     identity changes            REFUSED
--     '0418-320' -> NULL           identity withdrawn          REFUSED
--
-- Genuine university renumbering is deliberately NOT supported here. It needs
-- supersession, aliases, lineage, or effective dates, and an ordinary Admin edit
-- must not be able to approximate it by accident.

-- The guard recomputes the normalized form from NEW.official_code rather than
-- reading NEW.code_normalized. code_normalized is a STORED generated column, and
-- PostgreSQL computes generated columns AFTER BEFORE-triggers run, so
-- NEW.code_normalized is NULL here regardless of what is being written.
-- OLD.code_normalized is a committed value and is reliable.
--
-- academic_normalize_code is STRICT, so a NULL official_code normalizes to NULL,
-- which IS DISTINCT FROM an established code and is therefore refused — that is
-- the coded-to-codeless case.
CREATE FUNCTION subjects_reject_code_identity_change() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.code_normalized IS NOT NULL
       AND academic_normalize_code(NEW.official_code) IS DISTINCT FROM OLD.code_normalized THEN
        RAISE EXCEPTION
            'subject % already has the canonical official code %; a normalized code is immutable once established',
            OLD.id, OLD.code_normalized
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Scoped to UPDATE OF official_code, so retirement, renaming, and owning-unit
-- changes never enter the guard at all.
CREATE TRIGGER subjects_code_identity_guard
    BEFORE UPDATE OF official_code ON subjects
    FOR EACH ROW
    EXECUTE FUNCTION subjects_reject_code_identity_change();
