DROP TRIGGER IF EXISTS outbox_protected_payloads_append_only ON outbox_protected_payloads;
DROP TRIGGER IF EXISTS outbox_events_append_only ON outbox_events;
DROP TRIGGER IF EXISTS identity_security_events_append_only ON identity_security_events;
DROP TRIGGER IF EXISTS policy_acceptances_append_only ON policy_acceptances;
DROP FUNCTION IF EXISTS immutable_evidence_reject_mutation();

DROP TABLE IF EXISTS outbox_protected_payloads;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS identity_security_events;

DROP TRIGGER IF EXISTS identity_action_secrets_lifecycle ON identity_action_secrets;
DROP FUNCTION IF EXISTS identity_action_secrets_enforce_lifecycle();
DROP TABLE IF EXISTS identity_action_secrets;
DROP TABLE IF EXISTS policy_acceptances;

ALTER TABLE bootstrap_operations
    DROP CONSTRAINT IF EXISTS bootstrap_operations_fingerprint_pair,
    DROP COLUMN IF EXISTS request_fingerprint,
    DROP COLUMN IF EXISTS fingerprint_version;
