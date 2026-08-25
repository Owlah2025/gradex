DROP INDEX IF EXISTS content_reports_open_queue_idx;

ALTER TABLE content_reports
    DROP CONSTRAINT IF EXISTS rep_resolution_consistency_check,
    DROP CONSTRAINT IF EXISTS rep_resolution_reason_check,
    DROP CONSTRAINT IF EXISTS rep_resolution_action_check,
    DROP COLUMN IF EXISTS resolution_reason,
    DROP COLUMN IF EXISTS resolution_action,
    DROP COLUMN IF EXISTS resolved_by_account_id;
