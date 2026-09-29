-- T1 Admin Operations query-path indexes.
--
-- pg_trgm already belongs to the catalogue search migration. IF NOT EXISTS keeps
-- this migration safe when a PostgreSQL 16 database already has the extension,
-- and the down migration deliberately leaves the extension in place because
-- 0011 owns it.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX admin_audit_events_target_lookup_idx
    ON audit_events (target_type, target_id, occurred_at DESC);

CREATE INDEX admin_accounts_display_name_trgm_idx
    ON accounts USING GIN (lower(display_name) gin_trgm_ops);

CREATE INDEX admin_accounts_normalized_email_prefix_idx
    ON accounts (normalized_email text_pattern_ops);

CREATE INDEX admin_entitlements_student_state_idx
    ON entitlements (student_account_id, state, access_ends_at);
