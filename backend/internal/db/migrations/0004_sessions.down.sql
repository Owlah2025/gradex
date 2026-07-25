DROP TRIGGER IF EXISTS session_credentials_immutable ON session_credentials;
DROP FUNCTION IF EXISTS session_credentials_enforce_immutability();

DROP TABLE IF EXISTS session_credentials;
DROP TABLE IF EXISTS sessions;

DROP TYPE IF EXISTS session_credential_state;
DROP TYPE IF EXISTS session_revocation_reason;
DROP TYPE IF EXISTS session_state;
