-- T1 (MVP-F17) Academic Catalog Foundation.
--
-- D-091 replaces the flat MAJOR/SUBJECT/study_year classification with an
-- Institution-scoped academic catalog. This migration is strictly ADDITIVE:
-- taxonomy_terms, taxonomy_kind, study_year, course_revisions.major_term_id and
-- course_revisions.subject_term_id are untouched and remain authoritative for
-- Courses. Nothing in this migration is read or written by any existing Course,
-- catalogue, review, entitlement, or media path.
--
-- courses.subject_id is deliberately NOT added here. D-D places Subject on the
-- Course, but that column belongs to T4/T5 cutover work; adding it now would
-- create an unused column inside the Course write path for no sequencing gain.

CREATE TYPE academic_unit_kind AS ENUM (
    'COLLEGE',
    'DEPARTMENT',
    'SERVICE_UNIT'
);

CREATE TYPE curriculum_status AS ENUM (
    'ACTIVE',
    'SUPERSEDED'
);

-- Requirement categories observed across Kuwait University, AUK, and AUM
-- academic plans. Metadata only: no degree-audit, prerequisite, or credit
-- accumulation logic reads these.
CREATE TYPE curriculum_requirement_kind AS ENUM (
    'UNIVERSITY_REQUIREMENT',
    'COLLEGE_REQUIREMENT',
    'MAJOR_CORE',
    'MAJOR_ELECTIVE',
    'SUPPORTING',
    'FREE_ELECTIVE'
);

-- Format-agnostic academic code normalization. Uppercases and strips every
-- character that is not A-Z0-9, so all observed schemes collapse to one
-- comparable form:
--   '0410-101' -> '0410101'   (Kuwait University numeric)
--   'CS 490'   -> 'CS490'     (Kuwait University alphabetic)
--   'ELEG 220' -> 'ELEG220'   (AUK/AUM alphanumeric)
-- Deliberately distinct from catalog_normalize_ar, which owns *title* folding
-- and must not fork (D-023, amended by D-091).
CREATE FUNCTION academic_normalize_code(input text) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    RETURN upper(regexp_replace(input, '[^A-Za-z0-9]', '', 'g'));

-- Degree-granting institution. Country lives here rather than in its own table:
-- a market is an attribute of an institution, not an entity Gradex operates on.
CREATE TABLE institutions (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    country_code         CHAR(2) NOT NULL,
    slug                 TEXT NOT NULL,
    name_ar              TEXT NOT NULL,
    name_en              TEXT NOT NULL,

    -- Institution-owned level bounds. Kuwait University defines five
    -- credit-derived levels; no fixed four-year assumption exists anywhere.
    max_academic_level   SMALLINT NOT NULL DEFAULT 4,
    has_foundation_stage BOOLEAN NOT NULL DEFAULT FALSE,

    retired_at           TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT institutions_country_code_format CHECK (country_code ~ '^[A-Z]{2}$'),
    CONSTRAINT institutions_slug_format CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT institutions_slug_length CHECK (length(slug) BETWEEN 2 AND 80),
    CONSTRAINT institutions_name_ar_non_empty CHECK (length(trim(name_ar)) > 0),
    CONSTRAINT institutions_name_en_non_empty CHECK (length(trim(name_en)) > 0),
    CONSTRAINT institutions_max_level_range CHECK (max_academic_level BETWEEN 1 AND 12)
);

-- Institution slug is a global identifier because it addresses a real-world
-- institution. Retired institutions release their slug for reuse.
CREATE UNIQUE INDEX institutions_slug_unique
    ON institutions (slug)
    WHERE retired_at IS NULL;

-- Self-referencing academic hierarchy. One table rather than separate colleges
-- and departments tables, because the observed shapes differ: Kuwait University
-- nests College -> Department, Abdullah Al Salem University has no department
-- layer, and the American University of the Middle East has departments that
-- hang directly off the institution. Depth is data.
CREATE TABLE academic_units (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    institution_id  UUID NOT NULL REFERENCES institutions (id),
    parent_unit_id  UUID,
    kind            academic_unit_kind NOT NULL,
    slug            TEXT NOT NULL,
    name_ar         TEXT NOT NULL,
    name_en         TEXT NOT NULL,
    retired_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Target for the composite parent foreign key below.
    CONSTRAINT academic_units_id_institution_unique UNIQUE (id, institution_id),

    -- Structural, not advisory: a parent must be in the same Institution.
    -- Enforced by the database so a cross-Institution parent is impossible even
    -- if application validation is bypassed.
    CONSTRAINT academic_units_parent_same_institution
        FOREIGN KEY (parent_unit_id, institution_id)
        REFERENCES academic_units (id, institution_id),

    CONSTRAINT academic_units_no_self_parent CHECK (parent_unit_id IS NULL OR parent_unit_id <> id),
    CONSTRAINT academic_units_slug_format CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT academic_units_slug_length CHECK (length(slug) BETWEEN 2 AND 80),
    CONSTRAINT academic_units_name_ar_non_empty CHECK (length(trim(name_ar)) > 0),
    CONSTRAINT academic_units_name_en_non_empty CHECK (length(trim(name_en)) > 0)
);

-- Unit slug is scoped to the Institution, never global: two universities must
-- both be able to own a unit called "engineering".
CREATE UNIQUE INDEX academic_units_institution_slug_unique
    ON academic_units (institution_id, slug)
    WHERE retired_at IS NULL;

CREATE INDEX academic_units_institution_parent_idx
    ON academic_units (institution_id, parent_unit_id);

-- Multi-node cycle protection. The self-parent CHECK above stops A -> A; only a
-- walk can stop A -> B -> C -> A. UI nesting is not a control.
CREATE FUNCTION academic_units_reject_cycle() RETURNS TRIGGER AS $$
DECLARE
    ancestor UUID := NEW.parent_unit_id;
    hops     INTEGER := 0;
BEGIN
    WHILE ancestor IS NOT NULL LOOP
        IF ancestor = NEW.id THEN
            RAISE EXCEPTION 'academic unit % would create a hierarchy cycle', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        hops := hops + 1;
        IF hops > 32 THEN
            RAISE EXCEPTION 'academic unit hierarchy for % exceeds the supported depth', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT parent_unit_id INTO ancestor FROM academic_units WHERE id = ancestor;
    END LOOP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER academic_units_cycle_guard
    BEFORE INSERT OR UPDATE OF parent_unit_id ON academic_units
    FOR EACH ROW
    WHEN (NEW.parent_unit_id IS NOT NULL)
    EXECUTE FUNCTION academic_units_reject_cycle();

-- The degree specialisation a Student follows. Deliberately separate from
-- academic_units: Kuwait University's Mathematics Department owns both the
-- Mathematics and the Financial Mathematics programs, so Department is not
-- Major.
CREATE TABLE programs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    institution_id  UUID NOT NULL REFERENCES institutions (id),
    owning_unit_id  UUID,
    slug            TEXT NOT NULL,
    name_ar         TEXT NOT NULL,
    name_en         TEXT NOT NULL,
    degree_kind     TEXT NOT NULL,
    retired_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT programs_id_institution_unique UNIQUE (id, institution_id),
    CONSTRAINT programs_owning_unit_same_institution
        FOREIGN KEY (owning_unit_id, institution_id)
        REFERENCES academic_units (id, institution_id),

    CONSTRAINT programs_slug_format CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT programs_slug_length CHECK (length(slug) BETWEEN 2 AND 80),
    CONSTRAINT programs_name_ar_non_empty CHECK (length(trim(name_ar)) > 0),
    CONSTRAINT programs_name_en_non_empty CHECK (length(trim(name_en)) > 0),
    CONSTRAINT programs_degree_kind_format CHECK (degree_kind ~ '^[A-Z][A-Z0-9_]*$')
);

CREATE UNIQUE INDEX programs_institution_slug_unique
    ON programs (institution_id, slug)
    WHERE retired_at IS NULL;

CREATE INDEX programs_institution_unit_idx ON programs (institution_id, owning_unit_id);

-- Canonical Institution-owned academic identity. Belongs to the Institution and
-- never to one Program: Kuwait University's 0410-101 Calculus I is required
-- verbatim by both Electrical and Computer Engineering, and must exist once.
CREATE TABLE subjects (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    institution_id   UUID NOT NULL REFERENCES institutions (id),
    owning_unit_id   UUID,
    official_code    TEXT,
    title_ar         TEXT NOT NULL,
    title_en         TEXT NOT NULL,

    code_normalized  TEXT GENERATED ALWAYS AS (academic_normalize_code(official_code)) STORED,
    -- Each title is normalized independently rather than as one concatenation.
    -- Concatenation is order-sensitive and would let a duplicate slip through by
    -- varying only one language; per-title uniqueness blocks both directions.
    title_ar_normalized TEXT GENERATED ALWAYS AS (catalog_normalize_ar(title_ar)) STORED,
    title_en_normalized TEXT GENERATED ALWAYS AS (catalog_normalize_ar(title_en)) STORED,

    retired_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT subjects_id_institution_unique UNIQUE (id, institution_id),
    CONSTRAINT subjects_owning_unit_same_institution
        FOREIGN KEY (owning_unit_id, institution_id)
        REFERENCES academic_units (id, institution_id),

    CONSTRAINT subjects_title_ar_non_empty CHECK (length(trim(title_ar)) > 0),
    CONSTRAINT subjects_title_en_non_empty CHECK (length(trim(title_en)) > 0),
    -- A code of '---' would normalize to '' and silently defeat the code index.
    CONSTRAINT subjects_official_code_meaningful CHECK (
        official_code IS NULL OR length(academic_normalize_code(official_code)) > 0
    ),
    CONSTRAINT subjects_official_code_length CHECK (
        official_code IS NULL OR length(official_code) <= 40
    )
);

-- Subject identity, database-enforced so a race cannot create a duplicate.
-- A coded Subject is identified by its normalized code within the Institution.
CREATE UNIQUE INDEX subjects_institution_code_unique
    ON subjects (institution_id, code_normalized)
    WHERE code_normalized IS NOT NULL AND retired_at IS NULL;

-- A code-less Subject is identified by each of its normalized titles. Coded
-- Subjects are excluded because a university legitimately reuses a title such
-- as "Special Topics" across departments under different codes.
CREATE UNIQUE INDEX subjects_institution_title_ar_unique
    ON subjects (institution_id, title_ar_normalized)
    WHERE code_normalized IS NULL AND retired_at IS NULL;

CREATE UNIQUE INDEX subjects_institution_title_en_unique
    ON subjects (institution_id, title_en_normalized)
    WHERE code_normalized IS NULL AND retired_at IS NULL;

CREATE INDEX subjects_institution_title_trgm_idx
    ON subjects USING GIN ((title_ar_normalized || ' ' || title_en_normalized) gin_trgm_ops);

-- Versioned academic plan (major sheet). institution_id is carried here so
-- curriculum_subjects can enforce same-Institution mapping with a composite
-- foreign key rather than a trigger.
CREATE TABLE curricula (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    program_id           UUID NOT NULL,
    institution_id       UUID NOT NULL REFERENCES institutions (id),
    version_label        TEXT NOT NULL,
    effective_from_year  SMALLINT,
    status               curriculum_status NOT NULL DEFAULT 'ACTIVE',
    retired_at           TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT curricula_id_institution_unique UNIQUE (id, institution_id),
    CONSTRAINT curricula_program_same_institution
        FOREIGN KEY (program_id, institution_id)
        REFERENCES programs (id, institution_id),

    CONSTRAINT curricula_version_label_non_empty CHECK (length(trim(version_label)) > 0),
    CONSTRAINT curricula_version_label_length CHECK (length(version_label) <= 40),
    CONSTRAINT curricula_effective_year_range CHECK (
        effective_from_year IS NULL OR effective_from_year BETWEEN 1900 AND 2200
    )
);

CREATE UNIQUE INDEX curricula_program_version_unique
    ON curricula (program_id, version_label);

-- Exactly one ACTIVE Curriculum per Program (D-091 §5).
CREATE UNIQUE INDEX curricula_one_active_per_program
    ON curricula (program_id)
    WHERE status = 'ACTIVE' AND retired_at IS NULL;

-- The many-to-many that lets one canonical Subject serve many Programs without
-- duplication. Metadata only.
CREATE TABLE curriculum_subjects (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    curriculum_id         UUID NOT NULL,
    subject_id            UUID NOT NULL,
    institution_id        UUID NOT NULL REFERENCES institutions (id),
    requirement_kind      curriculum_requirement_kind NOT NULL,
    recommended_level     SMALLINT,
    recommended_semester  SMALLINT,
    credits               NUMERIC(4, 1),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Both sides are pinned to the same Institution by composite foreign key,
    -- so a cross-Institution mapping cannot be written at all.
    CONSTRAINT curriculum_subjects_curriculum_same_institution
        FOREIGN KEY (curriculum_id, institution_id)
        REFERENCES curricula (id, institution_id) ON DELETE CASCADE,
    CONSTRAINT curriculum_subjects_subject_same_institution
        FOREIGN KEY (subject_id, institution_id)
        REFERENCES subjects (id, institution_id),

    CONSTRAINT curriculum_subjects_unique UNIQUE (curriculum_id, subject_id),
    CONSTRAINT curriculum_subjects_level_positive CHECK (
        recommended_level IS NULL OR recommended_level >= 1
    ),
    CONSTRAINT curriculum_subjects_semester_range CHECK (
        recommended_semester IS NULL OR recommended_semester BETWEEN 1 AND 3
    ),
    CONSTRAINT curriculum_subjects_credits_range CHECK (
        credits IS NULL OR (credits >= 0 AND credits <= 30)
    )
);

CREATE INDEX curriculum_subjects_subject_idx ON curriculum_subjects (subject_id);

-- recommended_level is bounded by the owning Institution's declared maximum.
-- A CHECK cannot read another row, so this is the smallest robust guard.
CREATE FUNCTION curriculum_subjects_enforce_level_bound() RETURNS TRIGGER AS $$
DECLARE
    institution_max SMALLINT;
BEGIN
    IF NEW.recommended_level IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT max_academic_level INTO institution_max
    FROM institutions WHERE id = NEW.institution_id;
    IF institution_max IS NULL THEN
        RAISE EXCEPTION 'institution % is unknown', NEW.institution_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    IF NEW.recommended_level > institution_max THEN
        RAISE EXCEPTION 'recommended level % exceeds the maximum academic level % for institution %',
            NEW.recommended_level, institution_max, NEW.institution_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER curriculum_subjects_level_bound_guard
    BEFORE INSERT OR UPDATE OF recommended_level, institution_id ON curriculum_subjects
    FOR EACH ROW
    EXECUTE FUNCTION curriculum_subjects_enforce_level_bound();
