-- The append-only trigger blocks DELETE, not DROP TABLE. Dropping the trigger
-- first is still explicit about the order so a partial run cannot leave a
-- table whose protection was removed while its rows remain.
DROP TRIGGER IF EXISTS audit_events_append_only ON audit_events;
DROP FUNCTION IF EXISTS audit_events_reject_mutation();

DROP TABLE IF EXISTS audit_events;

DROP TYPE IF EXISTS audit_module;
