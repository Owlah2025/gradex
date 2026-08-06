ALTER TABLE entitlements
    DROP CONSTRAINT IF EXISTS ent_manual_needs_invitation;

ALTER TABLE entitlements
    DROP CONSTRAINT IF EXISTS fk_entitlements_source_invitation;

DROP TABLE IF EXISTS course_access_invitations;

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT IF EXISTS identity_action_secrets_account_id_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_account_id_purpose CHECK (
        (purpose IN ('EMAIL_VERIFICATION', 'PASSWORD_RESET') AND account_id IS NOT NULL)
        OR (purpose = 'STAFF_INVITATION' AND account_id IS NULL)
    );

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT IF EXISTS identity_action_secrets_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_purpose CHECK (
        purpose IN (
            'EMAIL_VERIFICATION',
            'PASSWORD_RESET',
            'STAFF_INVITATION'
        )
    );

ALTER TABLE courses
    DROP COLUMN IF EXISTS default_access_ends_at;
