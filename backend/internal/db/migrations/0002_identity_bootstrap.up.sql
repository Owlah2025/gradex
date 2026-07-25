-- Identity core and the secure bootstrap Administrator's state.
--
-- Scope is the bootstrap chain's first link only: Accounts, password
-- credentials, and the bootstrap operation record. Sessions, tokens, and
-- invitations arrive with the links that need them.

-- Every Account has exactly one role, assigned at creation and immutable during
-- MVP. There is deliberately no account_roles table and no role-update command
-- (domain design §4.1); a person needing separate capabilities uses a separate
-- Account with another normalized email.
CREATE TYPE account_role AS ENUM ('STUDENT', 'INSTRUCTOR', 'ADMIN');

CREATE TYPE account_status AS ENUM ('PENDING_VERIFICATION', 'ACTIVE', 'SUSPENDED');

-- The credential's own state, kept separate from account_status on purpose.
--
-- §4.5 creates the bootstrap Administrator as ACTIVE *and* required to change
-- its password, so these are two independent facts about one Account. Folding
-- "must change password" into account_status would make an Account that is
-- both active and not-yet-usable inexpressible, and would put a credential
-- concern into the Account's lifecycle.
CREATE TYPE credential_state AS ENUM ('ACTIVE', 'CHANGE_REQUIRED');

CREATE TABLE accounts (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Normalized form is what uniqueness is enforced on, so two addresses
    -- differing only by case or surrounding whitespace cannot both register.
    -- The address as the user typed it is kept for display and correspondence.
    normalized_email       TEXT NOT NULL,
    email                  TEXT NOT NULL,

    role                   account_role NOT NULL,
    status                 account_status NOT NULL DEFAULT 'PENDING_VERIFICATION',

    display_name           TEXT NOT NULL,
    locale                 TEXT NOT NULL DEFAULT 'ar',

    email_verified_at      TIMESTAMPTZ,

    -- Incrementing the epoch invalidates every credential generation issued
    -- before it, which is how suspension takes effect immediately rather than
    -- at the next token expiry.
    session_epoch          INTEGER NOT NULL DEFAULT 1,

    -- Optimistic concurrency for Account mutations.
    revision               INTEGER NOT NULL DEFAULT 1,

    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT accounts_normalized_email_key UNIQUE (normalized_email),
    CONSTRAINT accounts_normalized_email_lowercase CHECK (normalized_email = lower(normalized_email)),
    CONSTRAINT accounts_session_epoch_positive CHECK (session_epoch >= 1),
    CONSTRAINT accounts_revision_positive CHECK (revision >= 1),
    -- A verified timestamp and an unverified status contradict each other.
    CONSTRAINT accounts_verified_status CHECK (
        email_verified_at IS NULL OR status <> 'PENDING_VERIFICATION'
    )
);

CREATE INDEX accounts_status_idx ON accounts (status);
CREATE INDEX accounts_role_status_idx ON accounts (role, status);

-- The role is immutable during MVP. The domain design excludes it from ordinary
-- update statements; this trigger makes that a database guarantee rather than a
-- convention every future writer has to remember.
CREATE FUNCTION accounts_reject_role_change() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.role IS DISTINCT FROM OLD.role THEN
        RAISE EXCEPTION 'accounts.role is immutable during MVP (account %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER accounts_role_immutable
    BEFORE UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION accounts_reject_role_change();

-- One password credential per Account. Holds no profile data: this table is the
-- most sensitive in the schema and should stay the smallest.
CREATE TABLE password_credentials (
    account_id        UUID PRIMARY KEY REFERENCES accounts (id) ON DELETE CASCADE,

    -- Argon2id encoded hash. The plaintext never reaches this database.
    password_hash     TEXT NOT NULL,

    state             credential_state NOT NULL DEFAULT 'ACTIVE',

    password_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Argon2id encoded hashes are self-describing and start with the variant.
    -- A hash that does not is a plaintext or a foreign format, and storing one
    -- would be silent, so it is rejected loudly instead.
    CONSTRAINT password_credentials_hash_is_argon2id CHECK (password_hash LIKE '$argon2id$%')
);

CREATE INDEX password_credentials_state_idx ON password_credentials (state)
    WHERE state = 'CHANGE_REQUIRED';

-- The bootstrap Administrator operation, recorded exactly once.
--
-- This is domain uniqueness, not request deduplication, so it does not reuse
-- idempotency_records — the domain design defines that table as "shared
-- infrastructure, not a substitute for domain uniqueness" (§2.2).
--
-- The singleton column is the enforcement: it is the primary key, defaults to
-- TRUE, and is constrained to TRUE, so the table admits at most one row no
-- matter how many concurrent bootstrap attempts race. A second attempt fails on
-- the primary key rather than on application logic.
CREATE TABLE bootstrap_operations (
    singleton        BOOLEAN PRIMARY KEY DEFAULT TRUE,

    -- The caller-supplied stable operation ID. A retry carrying the same ID
    -- reads back the recorded result; a different ID cannot mint another Admin
    -- because the singleton row already exists.
    operation_id     TEXT NOT NULL,

    -- The Account this operation created. Kept as evidence of what the
    -- completed operation actually did.
    account_id       UUID NOT NULL REFERENCES accounts (id),

    completed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT bootstrap_operations_singleton CHECK (singleton),
    CONSTRAINT bootstrap_operations_operation_id_present CHECK (length(trim(operation_id)) > 0)
);
