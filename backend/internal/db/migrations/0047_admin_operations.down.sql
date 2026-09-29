DROP INDEX IF EXISTS admin_entitlements_student_state_idx;
DROP INDEX IF EXISTS admin_accounts_normalized_email_prefix_idx;
DROP INDEX IF EXISTS admin_accounts_display_name_trgm_idx;
DROP INDEX IF EXISTS admin_audit_events_target_lookup_idx;

-- pg_trgm is owned by 0011_catalog_search and may be used by other objects.
