ALTER TABLE content_reports
    ADD COLUMN resolved_by_account_id UUID REFERENCES accounts (id),
    ADD COLUMN resolution_action TEXT,
    ADD COLUMN resolution_reason TEXT;

ALTER TABLE content_reports
    ADD CONSTRAINT rep_resolution_action_check CHECK (
        resolution_action IS NULL OR resolution_action IN ('DISMISSED', 'DELISTED')
    ),
    ADD CONSTRAINT rep_resolution_reason_check CHECK (
        resolution_reason IS NULL OR (length(btrim(resolution_reason)) > 0 AND length(resolution_reason) <= 2000)
    ),
    ADD CONSTRAINT rep_resolution_consistency_check CHECK (
        (resolved_at IS NULL AND resolved_by_account_id IS NULL AND resolution_action IS NULL AND resolution_reason IS NULL)
        OR
        (resolved_at IS NOT NULL AND resolved_by_account_id IS NOT NULL AND resolution_action IS NOT NULL AND resolution_reason IS NOT NULL)
    );

CREATE INDEX content_reports_open_queue_idx
    ON content_reports (created_at ASC, id ASC)
    WHERE resolved_at IS NULL;
