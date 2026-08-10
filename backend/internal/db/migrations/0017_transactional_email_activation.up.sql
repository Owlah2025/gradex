-- S9 transactional email activation boundary.
--
-- The outbox is an immutable historical record that predates email delivery.
-- Without a boundary, the first poller on an existing database would discover
-- every historical intent ever written and mail stale credentials to real
-- people. This table records, exactly once and durably, the instant delivery
-- became active. Only intents that occurred at or after that instant are
-- eligible for automatic delivery.
--
-- The boundary is a creation-time cutoff, not a progress watermark: it is
-- written once and never advances, so it cannot move forward on restart and
-- cannot silently skip a post-activation intent that has not been discovered
-- yet. Historical rows stay in the outbox untouched as evidence; a historical
-- user who needs an action is issued a fresh one through the domain workflow.

CREATE TABLE transactional_email_activation (
    id            BOOLEAN PRIMARY KEY DEFAULT TRUE,
    activated_at  TIMESTAMPTZ NOT NULL,
    recorded_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- One row, forever. The primary key plus this check make a second
    -- activation boundary unrepresentable rather than merely discouraged.
    CONSTRAINT transactional_email_activation_singleton CHECK (id)
);

-- Stamped here, by the migration that introduces delivery, because this is the
-- instant delivery becomes possible for this database. Stamping it at first
-- poll instead would leave a gap: intents written between deploy and the first
-- worker tick are legitimately new, but a first-poll boundary would exclude
-- them as if they were historical. On an existing database this excludes every
-- pre-existing intent; on a fresh one no intent exists yet, so nothing is lost.
INSERT INTO transactional_email_activation (id, activated_at) VALUES (TRUE, now());
