-- T4 Batch D: moderated instructor profiles and public snapshots.
-- Expertise deliberately reuses the canonical academic Subjects vocabulary so
-- public profile discovery and course academic identity speak one language.

CREATE TYPE instructor_profile_publication_state AS ENUM (
    'DRAFT',
    'PENDING_REVIEW',
    'PUBLISHED',
    'CHANGES_REQUESTED',
    'HIDDEN'
);

CREATE TABLE instructor_profiles (
    account_id                    UUID PRIMARY KEY REFERENCES accounts (id) ON DELETE CASCADE,
    public_slug                   TEXT UNIQUE,
    headline_ar                  TEXT NOT NULL DEFAULT '',
    headline_en                  TEXT NOT NULL DEFAULT '',
    bio_ar                       TEXT NOT NULL DEFAULT '',
    bio_en                       TEXT NOT NULL DEFAULT '',
    -- The existing media pipeline can safely reference a thumbnail version, but
    -- no profile-avatar upload command is opened in T4; the UI uses initials
    -- until that command can prove the same readiness and ownership invariants.
    avatar_asset_version_id      UUID REFERENCES media_asset_versions (id),
    publication_state            instructor_profile_publication_state NOT NULL DEFAULT 'DRAFT',
    published_snapshot           JSONB,
    submitted_at                 TIMESTAMPTZ,
    decided_at                   TIMESTAMPTZ,
    decided_by                   UUID REFERENCES accounts (id) ON DELETE SET NULL,
    decision_note                TEXT,
    revision                     INTEGER NOT NULL DEFAULT 1,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT instructor_profiles_slug_shape CHECK (
        public_slug IS NULL OR (
            char_length(public_slug) BETWEEN 3 AND 60
            AND public_slug = lower(public_slug)
            AND public_slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'
        )
    ),
    CONSTRAINT instructor_profiles_headline_ar_length CHECK (char_length(headline_ar) <= 120),
    CONSTRAINT instructor_profiles_headline_en_length CHECK (char_length(headline_en) <= 120),
    CONSTRAINT instructor_profiles_bio_ar_length CHECK (char_length(bio_ar) <= 4000),
    CONSTRAINT instructor_profiles_bio_en_length CHECK (char_length(bio_en) <= 4000),
    CONSTRAINT instructor_profiles_decision_note_length CHECK (
        decision_note IS NULL OR char_length(decision_note) <= 4000
    ),
    CONSTRAINT instructor_profiles_revision_positive CHECK (revision >= 1),
    CONSTRAINT instructor_profiles_published_snapshot_object CHECK (
        published_snapshot IS NULL OR jsonb_typeof(published_snapshot) = 'object'
    ),
    CONSTRAINT instructor_profiles_published_has_snapshot CHECK (
        publication_state <> 'PUBLISHED' OR published_snapshot IS NOT NULL
    )
);

CREATE INDEX instructor_profiles_state_submitted_idx
    ON instructor_profiles (publication_state, submitted_at DESC, updated_at DESC);

CREATE TABLE instructor_profile_expertise (
    account_id  UUID NOT NULL REFERENCES instructor_profiles (account_id) ON DELETE CASCADE,
    subject_id  UUID NOT NULL REFERENCES subjects (id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (account_id, subject_id)
);

CREATE INDEX instructor_profile_expertise_subject_idx
    ON instructor_profile_expertise (subject_id, account_id);
