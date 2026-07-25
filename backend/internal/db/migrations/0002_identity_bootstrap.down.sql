-- Reverses 0002. Dropped in dependency order: the bootstrap record references
-- accounts, and the trigger's function cannot be dropped while the trigger on
-- accounts still uses it.
DROP TABLE IF EXISTS bootstrap_operations;
DROP TABLE IF EXISTS password_credentials;

DROP TRIGGER IF EXISTS accounts_role_immutable ON accounts;
DROP FUNCTION IF EXISTS accounts_reject_role_change();

DROP TABLE IF EXISTS accounts;

DROP TYPE IF EXISTS credential_state;
DROP TYPE IF EXISTS account_status;
DROP TYPE IF EXISTS account_role;
