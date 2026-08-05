CREATE TABLE enrollments (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    student_account_id UUID NOT NULL REFERENCES accounts (id),
    course_id          UUID NOT NULL REFERENCES courses (id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT enr_one_per_student_course UNIQUE (student_account_id, course_id)
);
