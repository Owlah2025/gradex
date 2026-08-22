-- T4-A (MVP-F20) Course Academic Identity Foundation.
--
-- D-093 places academic identity on the Course: an explicit classification
-- model, an owning Institution, and a canonical Subject. This migration is
-- ADDITIVE to the Course write path and DESTRUCTIVE TO NOTHING. taxonomy_terms,
-- course_revisions.major_term_id, course_revisions.subject_term_id and
-- course_revisions.study_year are untouched and remain authoritative for every
-- Course that has not been migrated. T5 owns their removal.
--
-- Every existing Course becomes LEGACY_TAXONOMY by column default, so no
-- existing row changes meaning and no existing read or write path observes a
-- difference. The new Academic path is unreachable until a caller supplies
-- academic context explicitly.
--
-- The one non-additive change is the coded-Subject uniqueness rule, which is
-- TIGHTENED (see section 1). That is a D-093 §7 Founder decision, not an
-- incidental cleanup, and rolling this migration down deliberately restores the
-- looser 0023 rule.

-- =========================================================================
-- 1. Subject official-code permanence (D-093 §7).
-- =========================================================================
--
-- 0023 scoped coded-Subject uniqueness to live rows:
--     UNIQUE (institution_id, code_normalized) WHERE ... retired_at IS NULL
-- which frees an official code for reuse as soon as its Subject is retired.
-- A published Gradex Course keeps pointing at the retired Subject, so a second
-- Subject taking the same Institution+code makes academic identity ambiguous
-- with no temporal or supersession semantics to resolve it.
--
-- The Founder decision is that an official code, once used, permanently
-- identifies that Subject within its Institution. Uniqueness therefore spans
-- active AND retired rows. Codeless Subjects are deliberately NOT affected:
-- their identity is their title, titles are Gradex-authored and editable, and
-- reserving editable prose forever has none of the same justification.
--
-- Preflight. A tightened index cannot be created over conflicting data, and a
-- bare index failure would report only "could not create unique index" with a
-- duplicate key. This block fails first with the actual conflicting rows named,
-- so an operator sees which Subjects need a Founder resolution. It never
-- deletes, merges, or rewrites a Subject.
DO $$
DECLARE
    conflict_report TEXT;
BEGIN
    SELECT string_agg(
               format('institution %s code %s: %s subjects (%s)',
                      institution_id, code_normalized, row_count, ids),
               E'\n')
      INTO conflict_report
      FROM (
          SELECT institution_id,
                 code_normalized,
                 count(*)                         AS row_count,
                 string_agg(id::text, ', ' ORDER BY id) AS ids
            FROM subjects
           WHERE code_normalized IS NOT NULL
           GROUP BY institution_id, code_normalized
          HAVING count(*) > 1
      ) conflicts;

    IF conflict_report IS NOT NULL THEN
        RAISE EXCEPTION
            'FOUNDER_DATA_RESOLUTION_REQUIRED: official Subject codes are reused across active and retired rows, so D-093 §7 permanence cannot be applied without a Founder decision on each pair:%s%s',
            E'\n', conflict_report
            USING ERRCODE = 'unique_violation';
    END IF;
END $$;

DROP INDEX subjects_institution_code_unique;

CREATE UNIQUE INDEX subjects_institution_code_unique
    ON subjects (institution_id, code_normalized)
    WHERE code_normalized IS NOT NULL;

-- =========================================================================
-- 2. Course classification model (D-093 §1).
-- =========================================================================
--
-- An explicit discriminator rather than an inference. Nullability cannot carry
-- this fact: a Course with no classification data is the normal initial state
-- of the existing create path, and a subject-less Academic draft is a
-- legitimate T4 state, so `subject_id IS NULL` describes both. Nothing else on
-- courses is both immutable and classification-bearing: owner_account_id is
-- reassignable, lifecycle is mutable, and slug derives from id.
CREATE TYPE course_classification_model AS ENUM (
    'LEGACY_TAXONOMY',
    'ACADEMIC_CATALOG'
);

ALTER TABLE courses
    -- The default is what makes this migration invisible to existing rows and
    -- to the existing create path. It is a transition device, not a product
    -- choice: T4-B replaces the normal Instructor create experience so that
    -- ordinary new Courses are ACADEMIC_CATALOG, and T5 migrates the rest.
    ADD COLUMN classification_model course_classification_model
        NOT NULL DEFAULT 'LEGACY_TAXONOMY',

    -- D-093 §3. The Course's own Institution, required by an Academic Course
    -- from creation. It exists because the Instructor flow begins at the
    -- University and a Course may legitimately hold no Subject yet, so
    -- Institution cannot be derived from Subject during drafting.
    ADD COLUMN institution_id UUID REFERENCES institutions (id),

    -- D-093 §4. Subject is Course-level stable identity, never revision-level:
    -- a Course that changes what it teaches is a different Course.
    ADD COLUMN subject_id UUID;

-- Target for the composite foreign keys below, which is how the Course's
-- Institution becomes the single authority every academic reference is pinned
-- to. Rows with a NULL institution_id (every legacy Course) are all distinct
-- under a unique constraint, so this costs legacy Courses nothing.
ALTER TABLE courses
    ADD CONSTRAINT courses_id_institution_unique UNIQUE (id, institution_id);

ALTER TABLE courses
    -- The invariant that makes institution_id and subject_id one authority
    -- rather than two that can disagree: a Subject from another Institution is
    -- unwritable, not merely rejected by Go. MATCH SIMPLE is deliberate — a
    -- NULL subject_id satisfies this trivially, which is exactly the
    -- subject-less Academic draft state.
    ADD CONSTRAINT courses_subject_same_institution
        FOREIGN KEY (subject_id, institution_id)
        REFERENCES subjects (id, institution_id),

    -- An Academic Course always knows its University.
    ADD CONSTRAINT courses_academic_has_institution CHECK (
        classification_model <> 'ACADEMIC_CATALOG' OR institution_id IS NOT NULL
    ),

    -- And a legacy Course holds no academic identity at all. Together these two
    -- make the hybrid state — Academic Subject plus legacy classification —
    -- impossible to write rather than merely discouraged. T5 therefore has to
    -- flip classification_model and assign academic identity in one statement,
    -- which is the intended atomic migration shape.
    ADD CONSTRAINT courses_legacy_has_no_academic_identity CHECK (
        classification_model <> 'LEGACY_TAXONOMY'
        OR (institution_id IS NULL AND subject_id IS NULL)
    );

CREATE INDEX courses_institution_subject_idx
    ON courses (institution_id, subject_id)
    WHERE classification_model = 'ACADEMIC_CATALOG';

-- =========================================================================
-- 3. Post-publication Subject immutability (D-093 §5).
-- =========================================================================
--
-- courses.live_revision_id is already the canonical publication-history fact:
-- it is set only when a revision goes live and is never cleared, and the review
-- queue already projects `live_revision_id IS NULL` as is_first_publish. No
-- redundant has_been_published or subject_locked flag is introduced.
--
-- A CHECK constraint cannot express this because the rule compares NEW to OLD,
-- so a trigger is the smallest correct guard — the same reasoning 0023 and 0024
-- already apply for their cross-row invariants. This is the last line of
-- defence beneath the domain command, so that no direct SQL, no bypassed
-- handler, and no last-write race can change what a published Course teaches.
--
-- The guard fires on a Course that already HAS a Subject. Assigning the first
-- Subject to an already-published Course stays possible, which is precisely
-- what T5 must do when it migrates a published legacy Course; an Academic
-- Course cannot reach publication without a Subject, so for the Academic path
-- the two conditions coincide.
CREATE FUNCTION courses_reject_published_subject_change() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.live_revision_id IS NOT NULL
       AND OLD.subject_id IS NOT NULL
       AND NEW.subject_id IS DISTINCT FROM OLD.subject_id THEN
        RAISE EXCEPTION
            'course % has published history; its Subject is immutable', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER courses_subject_immutability_guard
    BEFORE UPDATE OF subject_id ON courses
    FOR EACH ROW
    EXECUTE FUNCTION courses_reject_published_subject_change();

-- =========================================================================
-- 4. Revision-scoped Program audience targets (D-093 §8).
-- =========================================================================
--
-- SCHEMA ONLY in T4-A. No inference, customization, reset, clone, or validation
-- behaviour is implemented here; that is T4-C. The table ships now so the T4
-- schema is designed and proven once instead of accreting a second migration
-- over a table that will by then hold data.
--
-- Audience is revision-scoped because it is publishable metadata that follows
-- the ordinary review lifecycle, while Subject is Course-scoped because it is
-- stable product identity.
--
-- Zero rows means "use the audience inferred from the Subject's curriculum
-- mappings". There is deliberately no mode column: an explicit empty audience
-- must not be representable, and adding a boolean beside the rows would create
-- exactly the contradictory states it is supposed to prevent.
CREATE TABLE course_program_targets (
    revision_id     UUID NOT NULL,
    course_id       UUID NOT NULL,
    program_id      UUID NOT NULL,
    institution_id  UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Duplicate targets are unrepresentable rather than deduplicated in Go.
    CONSTRAINT course_program_targets_pkey PRIMARY KEY (revision_id, program_id),

    -- The target revision must belong to the named Course. This is the same
    -- composite device course_sections and course_lessons already use.
    CONSTRAINT course_program_targets_revision_same_course
        FOREIGN KEY (course_id, revision_id)
        REFERENCES course_revisions (course_id, id) ON DELETE CASCADE,

    -- And these two together make a cross-Institution target unwritable: the
    -- row's Institution must be the Course's Institution, and the Program must
    -- belong to that same Institution.
    CONSTRAINT course_program_targets_course_same_institution
        FOREIGN KEY (course_id, institution_id)
        REFERENCES courses (id, institution_id),
    CONSTRAINT course_program_targets_program_same_institution
        FOREIGN KEY (program_id, institution_id)
        REFERENCES programs (id, institution_id)
);

CREATE INDEX course_program_targets_program_idx
    ON course_program_targets (program_id);

-- =========================================================================
-- 5. Subject requests (D-093 §9 boundary).
-- =========================================================================
--
-- SCHEMA ONLY in T4-A. The Instructor request flow, the Admin queue, and
-- approve / link-to-existing / reject resolution are T4-D.
--
-- An Instructor never creates canonical Subjects. When the catalog is missing
-- one, the Instructor raises a request an Admin resolves through the existing
-- Academic Catalog domain, which is what keeps the T1 dedupe path the only way
-- a Subject is ever created.
CREATE TYPE subject_request_status AS ENUM (
    'PENDING',
    'APPROVED_NEW',
    'LINKED_EXISTING',
    'REJECTED',
    'CANCELLED'
);

CREATE TABLE subject_requests (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_account_id    UUID NOT NULL REFERENCES accounts (id),
    institution_id          UUID NOT NULL REFERENCES institutions (id),

    -- Nullable: a request may exist before any Course, and an attached request
    -- is an explicit relationship so resolution never has to match on title
    -- text to find the draft that should receive the Subject.
    course_id               UUID,

    proposed_title_ar       TEXT NOT NULL,
    proposed_title_en       TEXT NOT NULL,
    proposed_official_code  TEXT,
    academic_context        TEXT,
    note                    TEXT,

    status                  subject_request_status NOT NULL DEFAULT 'PENDING',
    resolved_subject_id     UUID,
    resolution_reason       TEXT,
    resolved_by_account_id  UUID REFERENCES accounts (id),
    resolved_at             TIMESTAMPTZ,

    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- The attached Course must be in the request's own Institution, structurally.
    -- Ownership — that the requester owns the Course — is a domain rule T4-D
    -- enforces, because it involves the session principal and is not a property
    -- of these rows.
    CONSTRAINT subject_requests_course_same_institution
        FOREIGN KEY (course_id, institution_id)
        REFERENCES courses (id, institution_id),

    -- A resolved Subject must belong to the request's Institution.
    CONSTRAINT subject_requests_subject_same_institution
        FOREIGN KEY (resolved_subject_id, institution_id)
        REFERENCES subjects (id, institution_id),

    CONSTRAINT subject_requests_title_ar_non_empty CHECK (length(trim(proposed_title_ar)) > 0),
    CONSTRAINT subject_requests_title_en_non_empty CHECK (length(trim(proposed_title_en)) > 0),
    CONSTRAINT subject_requests_code_meaningful CHECK (
        proposed_official_code IS NULL
        OR (length(proposed_official_code) <= 40
            AND length(academic_normalize_code(proposed_official_code)) > 0)
    ),

    -- A resolution that names a Subject and a resolution that does not are the
    -- two halves of the same fact, so neither can be written without the other.
    CONSTRAINT subject_requests_resolved_subject_shape CHECK (
        (status IN ('APPROVED_NEW', 'LINKED_EXISTING')) = (resolved_subject_id IS NOT NULL)
    ),
    CONSTRAINT subject_requests_rejection_needs_reason CHECK (
        status <> 'REJECTED'
        OR (resolution_reason IS NOT NULL AND length(trim(resolution_reason)) > 0)
    ),
    CONSTRAINT subject_requests_resolution_timestamp_shape CHECK (
        (status = 'PENDING') = (resolved_at IS NULL)
    )
);

-- At most one PENDING request per Course. Two open requests against one draft
-- would make "which resolution assigns the Subject" undecidable, and the race
-- would surface as a silent overwrite rather than an error.
CREATE UNIQUE INDEX subject_requests_one_pending_per_course
    ON subject_requests (course_id)
    WHERE status = 'PENDING' AND course_id IS NOT NULL;

CREATE INDEX subject_requests_pending_queue_idx
    ON subject_requests (institution_id, created_at)
    WHERE status = 'PENDING';

CREATE INDEX subject_requests_requester_idx
    ON subject_requests (requester_account_id, created_at DESC);
