-- Reverses 0024 exactly. Only T3-owned schema is removed.
--
-- Nothing outside this migration references student_academic_profiles, so the
-- drop restores the pre-T3 database: no Account, entitlement, invitation,
-- progress, or Academic Catalog row is touched.

DROP TRIGGER IF EXISTS student_profiles_curriculum_program_guard ON student_academic_profiles;
DROP FUNCTION IF EXISTS student_profiles_enforce_curriculum_program();

DROP TABLE IF EXISTS student_academic_profiles;

DROP TYPE IF EXISTS academic_enrollment_status;
DROP TYPE IF EXISTS academic_setup_state;
