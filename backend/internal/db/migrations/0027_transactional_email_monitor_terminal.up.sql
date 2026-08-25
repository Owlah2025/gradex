-- Keep the monitor's terminal-state EXISTS probe index-backed as the ledger grows.
CREATE INDEX transactional_email_monitor_terminal_idx
    ON transactional_email_deliveries (terminal_at, event_id)
    WHERE status IN ('PERMANENT_FAILED', 'EXHAUSTED');
