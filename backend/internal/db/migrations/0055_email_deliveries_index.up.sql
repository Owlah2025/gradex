CREATE INDEX CONCURRENTLY transactional_email_deliveries_updated_event_idx
    ON transactional_email_deliveries (updated_at DESC, event_id DESC);
