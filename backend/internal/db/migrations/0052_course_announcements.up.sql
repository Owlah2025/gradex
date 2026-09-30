-- T6 Batch F: immutable, immediately-published course announcements.
--
-- Announcements are an in-app course communication surface. They are written
-- only by the owning Instructor while the Course is published, and there is
-- deliberately no edit path: an announcement is a durable statement of what
-- was published at this time.

CREATE TABLE course_announcements (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Course deletion checks this relation explicitly and returns a lifecycle
    -- conflict while announcements exist; cascade would fire the immutable
    -- announcement trigger and turn the same request into a 500.
    course_id          UUID NOT NULL REFERENCES courses (id) ON DELETE RESTRICT,
    author_account_id  UUID NOT NULL REFERENCES accounts (id),
    title              TEXT NOT NULL,
    body               TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT course_announcements_title_non_empty CHECK (char_length(btrim(title)) BETWEEN 1 AND 140),
    CONSTRAINT course_announcements_body_non_empty CHECK (char_length(btrim(body)) BETWEEN 1 AND 4000),
    CONSTRAINT course_announcements_published_after_created CHECK (published_at >= created_at)
);

CREATE INDEX course_announcements_course_published_idx
    ON course_announcements (course_id, published_at DESC, id DESC);

CREATE FUNCTION course_announcements_immutable() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'published course announcements are immutable (announcement %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.course_id IS DISTINCT FROM OLD.course_id
        OR NEW.author_account_id IS DISTINCT FROM OLD.author_account_id
        OR NEW.title IS DISTINCT FROM OLD.title
        OR NEW.body IS DISTINCT FROM OLD.body
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
        OR NEW.published_at IS DISTINCT FROM OLD.published_at
    THEN
        RAISE EXCEPTION 'published course announcements are immutable (announcement %)', OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER course_announcements_append_only
    BEFORE UPDATE OR DELETE ON course_announcements
    FOR EACH ROW EXECUTE FUNCTION course_announcements_immutable();
