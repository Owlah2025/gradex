-- Stable session families and their immutable credential generations.
--
-- Two tables, because they answer two different questions and have opposite
-- mutability. `sessions` is the mutable family record — what is currently true
-- about this login. `session_credentials` is the append-only history of the
-- credentials that family has issued — what was true at each point, kept so
-- that presenting a superseded credential is detectable rather than merely
-- invalid (API/security design §4.2).
--
-- Link 4 creates the schema and the password-change internals that read it.
-- Nothing here is reachable over HTTP until link 5 can complete the whole
-- rotation atomically.

CREATE TYPE session_state AS ENUM ('ACTIVE', 'REVOKED', 'EXPIRED');

-- Why a family was revoked. Kept as a closed set because these values drive
-- security response, not just display: REUSE_DETECTED is the one that means an
-- attacker may hold a credential.
CREATE TYPE session_revocation_reason AS ENUM (
    'LOGOUT',
    'LOGOUT_ALL',
    'PASSWORD_CHANGE',
    'PASSWORD_RESET',
    'ACCOUNT_SUSPENDED',
    'REUSE_DETECTED',
    'ADMIN_REVOKED'
);

CREATE TYPE session_credential_state AS ENUM ('CURRENT', 'SUPERSEDED');

CREATE TABLE sessions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id              UUID NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,

    -- The Account epoch this family was admitted under. Incrementing
    -- accounts.session_epoch invalidates every family admitted before it, which
    -- is how suspension and logout-all take effect immediately rather than at
    -- the next expiry. Comparison is against the Account's current epoch, so no
    -- row here needs rewriting to revoke.
    admitted_epoch          INTEGER NOT NULL,

    state                   session_state NOT NULL DEFAULT 'ACTIVE',

    -- The generation number of the credential that currently authenticates.
    -- Monotonic within the family; the matching session_credentials row is the
    -- only one that may authenticate.
    current_generation      INTEGER NOT NULL DEFAULT 1,

    -- Primary authentication is when a password was last proven. It is distinct
    -- from last_activity_at, which any request updates: a session that has been
    -- clicking around for hours has recent activity and stale authentication,
    -- and only the latter may satisfy a recent-authentication requirement.
    authenticated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    reauthenticated_at      TIMESTAMPTZ,
    last_activity_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    idle_expires_at         TIMESTAMPTZ NOT NULL,
    absolute_expires_at     TIMESTAMPTZ NOT NULL,

    revoked_at              TIMESTAMPTZ,
    revocation_reason       session_revocation_reason,

    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT sessions_generation_positive CHECK (current_generation >= 1),
    CONSTRAINT sessions_epoch_positive CHECK (admitted_epoch >= 1),
    CONSTRAINT sessions_idle_before_absolute CHECK (idle_expires_at <= absolute_expires_at),
    -- A revoked family carries both its time and its reason, and a live one
    -- carries neither. Half-recorded revocation is how a security event becomes
    -- unexplainable after the fact.
    CONSTRAINT sessions_revocation_complete CHECK (
        (state = 'REVOKED' AND revoked_at IS NOT NULL AND revocation_reason IS NOT NULL)
        OR (state <> 'REVOKED' AND revoked_at IS NULL AND revocation_reason IS NULL)
    )
);

CREATE INDEX sessions_account_state_idx ON sessions (account_id, state);
CREATE INDEX sessions_account_epoch_idx ON sessions (account_id, admitted_epoch);
-- Supports expiry sweeps without scanning revoked families.
CREATE INDEX sessions_active_expiry_idx ON sessions (idle_expires_at)
    WHERE state = 'ACTIVE';

-- Immutable credential generations.
--
-- Only the row matching sessions.current_generation with state CURRENT may
-- authenticate. A SUPERSEDED row is not deleted: presenting one is the signal
-- that distinguishes a normal in-flight request from credential reuse, and a
-- deleted row would make that indistinguishable from an unknown credential.
CREATE TABLE session_credentials (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id            UUID NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    generation            INTEGER NOT NULL,

    -- Digests, never the credentials themselves. A database leak must not hand
    -- over usable session cookies, so what is stored cannot be replayed.
    credential_digest     TEXT NOT NULL,
    csrf_digest           TEXT NOT NULL,

    state                 session_credential_state NOT NULL DEFAULT 'CURRENT',

    issued_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    superseded_at         TIMESTAMPTZ,

    -- Ancestry: which generation replaced this one. Makes a rotation chain
    -- reconstructable during an incident without inferring it from numbering.
    replaced_by_generation INTEGER,

    -- Evidence of a superseded credential being presented. Recorded rather than
    -- only rejected, because the pattern of stale use is what separates a
    -- concurrent in-flight request from a stolen credential.
    stale_use_count       INTEGER NOT NULL DEFAULT 0,
    first_stale_use_at    TIMESTAMPTZ,
    last_stale_use_at     TIMESTAMPTZ,

    CONSTRAINT session_credentials_generation_positive CHECK (generation >= 1),
    CONSTRAINT session_credentials_unique_generation UNIQUE (session_id, generation),
    CONSTRAINT session_credentials_digest_present CHECK (length(credential_digest) > 0),
    CONSTRAINT session_credentials_csrf_present CHECK (length(csrf_digest) > 0),
    CONSTRAINT session_credentials_supersession_complete CHECK (
        (state = 'SUPERSEDED' AND superseded_at IS NOT NULL)
        OR (state = 'CURRENT' AND superseded_at IS NULL AND replaced_by_generation IS NULL)
    ),
    CONSTRAINT session_credentials_stale_use_coherent CHECK (
        (stale_use_count = 0 AND first_stale_use_at IS NULL AND last_stale_use_at IS NULL)
        OR (stale_use_count > 0 AND first_stale_use_at IS NOT NULL AND last_stale_use_at IS NOT NULL)
    )
);

-- Global uniqueness on the digests. A collision across sessions would let one
-- family's credential authenticate another's, so this is enforced by the
-- database rather than by trusting the generator's entropy.
CREATE UNIQUE INDEX session_credentials_credential_digest_key
    ON session_credentials (credential_digest);
CREATE UNIQUE INDEX session_credentials_csrf_digest_key
    ON session_credentials (csrf_digest);

-- At most one CURRENT generation per family. Two current credentials would mean
-- a rotation that did not supersede its predecessor — exactly the half-completed
-- state links 4 and 5 exist to prevent — and it is cheaper to make that
-- unrepresentable than to detect it later.
CREATE UNIQUE INDEX session_credentials_one_current_per_session
    ON session_credentials (session_id)
    WHERE state = 'CURRENT';

CREATE INDEX session_credentials_session_generation_idx
    ON session_credentials (session_id, generation DESC);

-- Immutability, with a deliberate exception.
--
-- A generation's identity — which session, which generation, which digests,
-- when issued — can never change. Supersession and stale-use evidence are
-- append-forward: they may be set once and then only accumulate. Enforcing this
-- in a trigger rather than by convention means a future writer cannot quietly
-- rewrite security history through an UPDATE that looks routine.
CREATE FUNCTION session_credentials_enforce_immutability() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.session_id IS DISTINCT FROM OLD.session_id
        OR NEW.generation IS DISTINCT FROM OLD.generation
        OR NEW.credential_digest IS DISTINCT FROM OLD.credential_digest
        OR NEW.csrf_digest IS DISTINCT FROM OLD.csrf_digest
        OR NEW.issued_at IS DISTINCT FROM OLD.issued_at
    THEN
        RAISE EXCEPTION 'session_credentials identity is immutable (session %, generation %)',
            OLD.session_id, OLD.generation
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- A superseded generation never returns to CURRENT.
    IF OLD.state = 'SUPERSEDED' AND NEW.state = 'CURRENT' THEN
        RAISE EXCEPTION 'a superseded session credential cannot become current (session %, generation %)',
            OLD.session_id, OLD.generation
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- Supersession evidence is written once.
    IF OLD.superseded_at IS NOT NULL AND NEW.superseded_at IS DISTINCT FROM OLD.superseded_at THEN
        RAISE EXCEPTION 'supersession evidence is immutable (session %, generation %)',
            OLD.session_id, OLD.generation
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- Stale-use evidence only accumulates.
    IF NEW.stale_use_count < OLD.stale_use_count THEN
        RAISE EXCEPTION 'stale-use evidence cannot decrease (session %, generation %)',
            OLD.session_id, OLD.generation
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER session_credentials_immutable
    BEFORE UPDATE ON session_credentials
    FOR EACH ROW EXECUTE FUNCTION session_credentials_enforce_immutability();

-- Deliberately no DELETE trigger. Immutable content is not the same as an
-- undeletable row: expiry and revocation are states rather than removals, but
-- Account deletion cascades here, and retention under LG-003 will eventually
-- need to remove old families. A blanket refusal would make both impossible and
-- would deadlock against the ON DELETE CASCADE above. What must not happen is a
-- generation being rewritten, which the UPDATE trigger prevents.
