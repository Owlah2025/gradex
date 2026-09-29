ALTER TABLE admin_notes
    DROP CONSTRAINT IF EXISTS admin_notes_subject_account_fk;

ALTER TABLE admin_notes
    ADD CONSTRAINT admin_notes_subject_account_id_fkey
        FOREIGN KEY (subject_account_id) REFERENCES accounts (id) ON DELETE CASCADE;
