-- Phase 3B1 / D-105: Introduce PLAYABLE state to media_asset_version_state enum.
--
-- The enum value is added alone in this migration before any runtime code
-- or future migration writes or transitions to it, because PostgreSQL refuses
-- to use a new enum value in the same transaction that created it.
--
-- In Phase 3B1, PLAYABLE is inert: no transitions into or out of PLAYABLE are
-- permitted by the application or the database immutability trigger, and
-- deliverability remains restricted to READY alone.

ALTER TYPE media_asset_version_state ADD VALUE IF NOT EXISTS 'PLAYABLE' AFTER 'PROCESSING';
