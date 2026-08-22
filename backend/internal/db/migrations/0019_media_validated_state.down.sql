-- PostgreSQL cannot remove a value from an enum type, so this step is
-- deliberately empty rather than pretending to reverse. Rolling back 0020
-- removes every use of `VALIDATED`: the validation-attempt evidence, the
-- provenance column, and the transitions that reach the state. The unused
-- label left behind in `media_asset_version_state` is inert, and no row can
-- carry it once 0020 is reversed, because the trigger reinstated there rejects
-- the transition into it.
SELECT 1;
