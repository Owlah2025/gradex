-- Student demand for an unserved Subject (D-106 §6).
--
-- Everything here is additive: one table, no change to any existing table,
-- column, type, or index.
--
-- WHAT THIS TABLE IS NOT. It is not an entitlement, a reservation, a waitlist
-- position, a purchase intent, or a promise that a Course will exist. Nothing
-- in the access, entitlement, enrollment, purchase, or media-playback path
-- reads it, and nothing here may ever be given that authority: a row means one
-- Student said they want this Subject taught, and the only thing it drives is
-- which Course Gradex produces next.
--
-- Deliberately NOT reusing subject_requests. That table is an Instructor asking
-- Admin to add a Subject that is missing from the catalog, and it resolves to a
-- catalog mutation. This is a Student asking for teaching of a Subject that
-- already exists. Same word, different actor, different resolution, different
-- lifetime -- merging them would make "pending" mean two incompatible things.
CREATE TABLE subject_demand_signals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The Student who asked. Demand is attributable on purpose: an anonymous
    -- counter cannot be deduplicated, cannot be followed up, and is trivially
    -- inflated by the only people motivated to inflate it.
    account_id      UUID NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,

    subject_id      UUID NOT NULL,

    -- Carried, not derived, so the composite foreign key below can pin the
    -- Subject to its Institution in the database rather than in Go.
    institution_id  UUID NOT NULL REFERENCES institutions (id),

    -- Free-text context the Student optionally supplies ("I take this in the
    -- fall"). Never rendered as catalog copy and never used as a title.
    note            TEXT,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    withdrawn_at    TIMESTAMPTZ,

    -- A Subject from another Institution is unwritable, not merely rejected by
    -- application validation. Matches the pattern courses and subject_requests
    -- already use.
    CONSTRAINT subject_demand_signals_subject_same_institution
        FOREIGN KEY (subject_id, institution_id)
        REFERENCES subjects (id, institution_id),

    CONSTRAINT subject_demand_signals_note_length CHECK (
        note IS NULL OR length(note) <= 500
    ),

    -- Withdrawal cannot predate the signal.
    CONSTRAINT subject_demand_signals_withdrawn_after_created CHECK (
        withdrawn_at IS NULL OR withdrawn_at >= created_at
    )
);

-- One live signal per Student per Subject. Partial, so a Student who withdraws
-- and later asks again creates a new row rather than being permanently barred,
-- and the withdrawn row survives as history. This is what makes a demand count
-- a count of Students rather than a count of clicks.
CREATE UNIQUE INDEX subject_demand_signals_live_unique
    ON subject_demand_signals (account_id, subject_id)
    WHERE withdrawn_at IS NULL;

-- Admin reads demand ordered by Subject within an Institution.
CREATE INDEX subject_demand_signals_institution_subject_idx
    ON subject_demand_signals (institution_id, subject_id)
    WHERE withdrawn_at IS NULL;

-- A Student reads their own signals on their profile.
CREATE INDEX subject_demand_signals_account_idx
    ON subject_demand_signals (account_id)
    WHERE withdrawn_at IS NULL;
