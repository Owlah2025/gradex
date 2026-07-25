-- Append-only privileged-action evidence.
--
-- Domain design §17: "Every privileged change commits its required Audit row in
-- the same PostgreSQL transaction as the source change." The bootstrap
-- Administrator operation is the first such change, so Audit has to exist
-- before link 2 of the bootstrap chain can be written — a bootstrap that
-- created an Admin without its evidence row would not be the operation §4.5
-- specifies.

-- The module set comes from the M1 architecture's module boundaries. It is a
-- closed set: a new module is an architecture change, not a value someone can
-- introduce by writing a row.
CREATE TYPE audit_module AS ENUM (
    'IDENTITY_AND_ACCESS',
    'CATALOG_AND_AUTHORING',
    'MEDIA_AND_ASSETS',
    'ENTITLEMENTS',
    'COMMERCE',
    'LEARNING',
    'OFFICE_HOURS',
    'NOTIFICATIONS',
    'REPORTING_AND_PAYOUTS',
    'MODERATION',
    'AUDIT'
);

CREATE TABLE audit_events (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The authoritative occurrence timestamp. Set by the database rather than
    -- accepted from a caller so that evidence cannot be backdated.
    occurred_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Actor. NULL account_id means the actor is not an Account: the bootstrap
    -- operation runs as a deployment principal, before any Admin exists.
    -- actor_descriptor names that principal either way, so an investigator
    -- never has to infer who acted from an absent foreign key.
    actor_account_id   UUID REFERENCES accounts (id),
    actor_role         TEXT NOT NULL,
    actor_descriptor   TEXT NOT NULL,

    -- Action is open-ended by format rather than by enumeration: every slice
    -- adds actions, and a CHECK list would make Audit a migration bottleneck on
    -- ordinary feature work. The module beside it is closed, which is the part
    -- that must not drift.
    action             TEXT NOT NULL,
    module             audit_module NOT NULL,

    target_type        TEXT NOT NULL,
    target_id          TEXT NOT NULL,
    target_revision    INTEGER,

    -- §17 requires a reason on every privileged action. Enforced as NOT NULL
    -- plus non-empty so "" cannot stand in for one.
    reason             TEXT NOT NULL,

    -- Safe before/after or immutable evidence metadata. §17 forbids
    -- credentials, bearer secrets, protected links, full provider payloads, and
    -- unnecessary personal data here; that is a review and code obligation, not
    -- something the column type can enforce.
    metadata           JSONB NOT NULL DEFAULT '{}'::jsonb,

    correlation_id     TEXT,
    operation_id       TEXT,

    CONSTRAINT audit_events_action_format CHECK (action ~ '^[A-Z][A-Z0-9_]*$'),
    CONSTRAINT audit_events_actor_role_present CHECK (length(trim(actor_role)) > 0),
    CONSTRAINT audit_events_actor_descriptor_present CHECK (length(trim(actor_descriptor)) > 0),
    CONSTRAINT audit_events_target_type_present CHECK (length(trim(target_type)) > 0),
    CONSTRAINT audit_events_target_id_present CHECK (length(trim(target_id)) > 0),
    CONSTRAINT audit_events_reason_present CHECK (length(trim(reason)) > 0),
    CONSTRAINT audit_events_metadata_is_object CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT audit_events_target_revision_positive CHECK (
        target_revision IS NULL OR target_revision >= 1
    )
);

-- §17: indexes support time, actor, module/target, action, and correlation
-- investigations.
CREATE INDEX audit_events_occurred_at_idx ON audit_events (occurred_at DESC);
CREATE INDEX audit_events_actor_idx ON audit_events (actor_account_id, occurred_at DESC);
CREATE INDEX audit_events_module_target_idx ON audit_events (module, target_type, target_id);
CREATE INDEX audit_events_action_idx ON audit_events (action, occurred_at DESC);
CREATE INDEX audit_events_correlation_idx ON audit_events (correlation_id)
    WHERE correlation_id IS NOT NULL;
CREATE INDEX audit_events_operation_idx ON audit_events (operation_id)
    WHERE operation_id IS NOT NULL;

-- Append-only.
--
-- §17 states the application database role cannot update or delete Audit rows.
-- Role separation is a deployment concern that does not exist yet and would not
-- protect against a migration or a superuser session in the meantime. This
-- trigger makes append-only a property of the table itself, so it holds for
-- every connection regardless of which role opened it. When the restricted
-- application role does arrive, it becomes a second, independent layer rather
-- than a replacement for this one.
CREATE FUNCTION audit_events_reject_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only (attempted %)', TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_events_append_only
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_reject_mutation();
