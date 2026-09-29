-- T2 hardening: append-only notes must not be removed by a future account delete.

ALTER TABLE admin_notes
    DROP CONSTRAINT IF EXISTS admin_notes_subject_account_id_fkey;

ALTER TABLE admin_notes
    ADD CONSTRAINT admin_notes_subject_account_fk
        FOREIGN KEY (subject_account_id) REFERENCES accounts (id) ON DELETE RESTRICT;
