-- S6 Course Access Invitation and Entitlement Grant.

DO $$
BEGIN
    -- Assert S4 entitlement structures exist
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'entitlements' AND column_name = 'grant_source'
    ) THEN
        RAISE EXCEPTION 'entitlements table lacks grant_source column';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'entitlements' AND column_name = 'source_invitation_id'
    ) THEN
        RAISE EXCEPTION 'entitlements table lacks source_invitation_id column';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'entitlements_grant_source_implemented'
    ) THEN
        RAISE EXCEPTION 'entitlements_grant_source_implemented constraint is missing';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_class WHERE relname = 'entitlements_one_active_student_course'
    ) THEN
        RAISE EXCEPTION 'entitlements_one_active_student_course index is missing';
    END IF;

    -- Assert S5 enrollment structures exist
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables WHERE table_name = 'enrollments'
    ) THEN
        RAISE EXCEPTION 'enrollments table is missing';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'enrollments' AND column_name = 'student_account_id'
    ) THEN
        RAISE EXCEPTION 'enrollments table lacks student_account_id column';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'enrollments' AND column_name = 'course_id'
    ) THEN
        RAISE EXCEPTION 'enrollments table lacks course_id column';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'enr_one_per_student_course'
    ) THEN
        RAISE EXCEPTION 'enr_one_per_student_course constraint is missing';
    END IF;
END;
$$;

ALTER TABLE courses
    ADD COLUMN default_access_ends_at TIMESTAMPTZ;

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_purpose CHECK (
        purpose IN (
            'EMAIL_VERIFICATION',
            'PASSWORD_RESET',
            'STAFF_INVITATION',
            'COURSE_ACCESS_INVITATION'
        )
    );

ALTER TABLE identity_action_secrets
    DROP CONSTRAINT identity_action_secrets_account_id_purpose;

ALTER TABLE identity_action_secrets
    ADD CONSTRAINT identity_action_secrets_account_id_purpose CHECK (
        (purpose IN ('EMAIL_VERIFICATION', 'PASSWORD_RESET') AND account_id IS NOT NULL)
        OR (purpose IN ('STAFF_INVITATION', 'COURSE_ACCESS_INVITATION') AND account_id IS NULL)
    );

CREATE TABLE course_access_invitations (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    normalized_email       TEXT NOT NULL,
    email                  TEXT NOT NULL,
    course_id              UUID NOT NULL REFERENCES courses (id),
    created_by_account_id UUID NOT NULL REFERENCES accounts (id),
    decided_by_account_id UUID REFERENCES accounts (id),
    accepted_by_account_id UUID REFERENCES accounts (id),
    state                  TEXT NOT NULL,
    decision_reason        TEXT,
    admin_note             TEXT,
    external_reference     TEXT,
    action_secret_id       UUID REFERENCES identity_action_secrets (id),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at            TIMESTAMPTZ,
    decided_at             TIMESTAMPTZ,
    cancelled_at           TIMESTAMPTZ,

    CONSTRAINT cai_state_valid CHECK (state IN ('PENDING_STUDENT_ACCEPTANCE','PENDING_ADMIN_APPROVAL','APPROVED','REJECTED','CANCELLED')),
    CONSTRAINT cai_rejection_needs_reason CHECK (state <> 'REJECTED' OR decision_reason IS NOT NULL),
    CONSTRAINT cai_decided_has_actor CHECK (state NOT IN ('APPROVED','REJECTED') OR decided_by_account_id IS NOT NULL),
    CONSTRAINT cai_accepted_has_actor CHECK (state IN ('PENDING_STUDENT_ACCEPTANCE','CANCELLED') OR accepted_by_account_id IS NOT NULL),
    CONSTRAINT cai_email_present CHECK (length(trim(normalized_email)) BETWEEN 3 AND 320)
);

CREATE UNIQUE INDEX cai_one_non_terminal_per_pair
    ON course_access_invitations (normalized_email, course_id)
    WHERE state IN ('PENDING_STUDENT_ACCEPTANCE','PENDING_ADMIN_APPROVAL');

ALTER TABLE entitlements
    ADD CONSTRAINT fk_entitlements_source_invitation
        FOREIGN KEY (source_invitation_id) REFERENCES course_access_invitations (id);

ALTER TABLE entitlements
    ADD CONSTRAINT ent_manual_needs_invitation
        CHECK (grant_source <> 'MANUAL_INVITATION' OR source_invitation_id IS NOT NULL)
        NOT VALID;
