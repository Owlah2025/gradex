-- PostgreSQL cannot remove a value from an enum type, so this step is
-- deliberately empty rather than pretending to reverse. The unused label
-- left behind in `media_asset_version_state` is inert because no production
-- code or transition reaches it in Phase 3B1.
SELECT 1;
