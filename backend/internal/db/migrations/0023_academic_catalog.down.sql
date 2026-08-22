-- Reverses 0023 exactly. Only T1-owned additive schema is removed.
--
-- pg_trgm and catalog_normalize_ar are NOT dropped: they are owned by 0011 and
-- are still used by the existing catalogue search path. academic_normalize_code
-- is T1-owned and is dropped.
--
-- No legacy taxonomy object and no Course row is touched, because 0023 never
-- created, altered, or wrote one.

DROP TRIGGER IF EXISTS curriculum_subjects_level_bound_guard ON curriculum_subjects;
DROP FUNCTION IF EXISTS curriculum_subjects_enforce_level_bound();

DROP TABLE IF EXISTS curriculum_subjects;
DROP TABLE IF EXISTS curricula;
DROP TABLE IF EXISTS subjects;
DROP TABLE IF EXISTS programs;

DROP TRIGGER IF EXISTS academic_units_cycle_guard ON academic_units;
DROP FUNCTION IF EXISTS academic_units_reject_cycle();

DROP TABLE IF EXISTS academic_units;
DROP TABLE IF EXISTS institutions;

DROP FUNCTION IF EXISTS academic_normalize_code(text);

DROP TYPE IF EXISTS curriculum_requirement_kind;
DROP TYPE IF EXISTS curriculum_status;
DROP TYPE IF EXISTS academic_unit_kind;
