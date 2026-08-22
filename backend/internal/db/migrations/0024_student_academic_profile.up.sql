-- T3 (MVP-F19) Student Academic Profile.
--
-- D-092: discovery-only personalisation data. No entitlement, access, purchase,
-- invitation, enrollment, progress, or playback decision may read this table.
-- It is deliberately additive and is referenced by nothing that already exists:
-- dropping it restores the pre-T3 database exactly.

CREATE TYPE academic_setup_state AS ENUM (
    -- NOT_STARTED is the absence of a row, so it is deliberately not a value
    -- here. Encoding it would allow two representations of the same state.
    'SKIPPED',
    'COMPLETED'
);

-- The Student's own academic standing. Deliberately a closed set: an undeclared
-- or non-degree Student is a state of the Student, never a placeholder Program.
CREATE TYPE academic_enrollment_status AS ENUM (
    'ENROLLED',
    'UNDECLARED',
    'FOUNDATION',
    'NON_DEGREE'
);

CREATE TABLE student_academic_profiles (
    -- One profile per Account, enforced by the primary key rather than by a
    -- unique index on a surrogate, so a concurrent upsert cannot create two.
    account_id        UUID PRIMARY KEY REFERENCES accounts (id) ON DELETE CASCADE,

    setup_state       academic_setup_state NOT NULL,
    enrollment_status academic_enrollment_status,

    institution_id    UUID REFERENCES institutions (id),

    -- D-092 §2: retains the Student's College when no Program is selected. It
    -- must never duplicate the College an enrolled Student's Program already
    -- determines (§3), which the CHECK below enforces.
    academic_unit_id  UUID,

    program_id        UUID,

    -- D-092 §6: resolved server-side from the Program's ACTIVE curriculum and
    -- then held, so a Student stays on the plan they enrolled under until they
    -- change Program.
    curriculum_id     UUID,

    current_level     SMALLINT,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Composite foreign keys pin every academic reference to the Student's own
    -- Institution. A cross-institution profile is unwritable, not merely
    -- rejected by application code.
    CONSTRAINT student_profiles_unit_same_institution
        FOREIGN KEY (academic_unit_id, institution_id)
        REFERENCES academic_units (id, institution_id),
    CONSTRAINT student_profiles_program_same_institution
        FOREIGN KEY (program_id, institution_id)
        REFERENCES programs (id, institution_id),
    CONSTRAINT student_profiles_curriculum_same_institution
        FOREIGN KEY (curriculum_id, institution_id)
        REFERENCES curricula (id, institution_id),

    -- A skipped profile is a deferral, not a half-filled one. Keeping it empty
    -- means SKIPPED can never be mistaken for a real academic profile.
    CONSTRAINT student_profiles_skipped_is_empty CHECK (
        setup_state <> 'SKIPPED' OR (
            enrollment_status IS NULL
            AND institution_id IS NULL
            AND academic_unit_id IS NULL
            AND program_id IS NULL
            AND curriculum_id IS NULL
            AND current_level IS NULL
        )
    ),

    -- A completed profile always names an Institution and a status.
    CONSTRAINT student_profiles_completed_has_context CHECK (
        setup_state <> 'COMPLETED' OR (
            institution_id IS NOT NULL AND enrollment_status IS NOT NULL
        )
    ),

    -- ENROLLED means a real Program on a real plan; every other status means no
    -- Program and no plan at all. Both halves are enforced, so neither an
    -- enrolled Student without a plan nor an undeclared Student holding one can
    -- be written.
    CONSTRAINT student_profiles_enrolled_shape CHECK (
        enrollment_status IS DISTINCT FROM 'ENROLLED'
        OR (program_id IS NOT NULL AND curriculum_id IS NOT NULL)
    ),
    CONSTRAINT student_profiles_non_enrolled_shape CHECK (
        enrollment_status IS NULL
        OR enrollment_status = 'ENROLLED'
        OR (program_id IS NULL AND curriculum_id IS NULL)
    ),

    -- D-092 §3: College is derived from the Program, never stored twice.
    CONSTRAINT student_profiles_no_redundant_unit CHECK (
        program_id IS NULL OR academic_unit_id IS NULL
    ),

    -- The Institution's own maximum is checked in the domain, which can read
    -- it; the column only refuses values no institution could ever have.
    CONSTRAINT student_profiles_level_positive CHECK (
        current_level IS NULL OR current_level BETWEEN 1 AND 12
    ),

    -- A curriculum without its program, or a level without an institution,
    -- would be uninterpretable.
    CONSTRAINT student_profiles_curriculum_needs_program CHECK (
        curriculum_id IS NULL OR program_id IS NOT NULL
    ),
    CONSTRAINT student_profiles_level_needs_institution CHECK (
        current_level IS NULL OR institution_id IS NOT NULL
    )
);

-- Discovery will filter by these once T6 exists. They are cheap now and avoid a
-- later migration on a table that will already hold Student data.
CREATE INDEX student_academic_profiles_institution_program_idx
    ON student_academic_profiles (institution_id, program_id)
    WHERE setup_state = 'COMPLETED';

CREATE INDEX student_academic_profiles_curriculum_level_idx
    ON student_academic_profiles (curriculum_id, current_level)
    WHERE curriculum_id IS NOT NULL;

-- The curriculum a profile points at must belong to the profile's own Program.
-- A CHECK cannot read another row, so this is the smallest robust guard, and it
-- is the constraint that makes "which plan is this Student on" answerable.
CREATE FUNCTION student_profiles_enforce_curriculum_program() RETURNS TRIGGER AS $$
DECLARE
    owning_program UUID;
BEGIN
    IF NEW.curriculum_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT program_id INTO owning_program FROM curricula WHERE id = NEW.curriculum_id;
    IF owning_program IS NULL OR owning_program IS DISTINCT FROM NEW.program_id THEN
        RAISE EXCEPTION 'curriculum % does not belong to program %', NEW.curriculum_id, NEW.program_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER student_profiles_curriculum_program_guard
    BEFORE INSERT OR UPDATE OF curriculum_id, program_id ON student_academic_profiles
    FOR EACH ROW
    EXECUTE FUNCTION student_profiles_enforce_curriculum_program();
