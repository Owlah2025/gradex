ALTER TABLE transactional_email_deliveries
    DROP CONSTRAINT transactional_email_provider,
    ADD CONSTRAINT transactional_email_provider CHECK (provider IN ('fake', 'mailpit', 'resend'));
