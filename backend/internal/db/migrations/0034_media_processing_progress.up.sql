-- Durable, honest video processing progress.
--
-- Before this migration the only thing an Instructor could learn after their
-- bytes finished uploading was the coarse Asset Version state: PROCESSING, and
-- then eventually READY. A long transcode was therefore indistinguishable from
-- a stuck one, and a page reload lost even the fact that a run was underway.
--
-- Progress is recorded on the Asset Version itself rather than in a side table
-- because it is a property of exactly one in-flight processing attempt over
-- exactly these immutable bytes, and it is read on the same status query the
-- authoring client already polls. A separate table would add a join to the hot
-- read and a second lifetime to reason about, for no additional fact.
--
-- Nothing here is a source of truth about deliverability. READY still requires
-- the successful processing attempt and trusted duration the lifecycle trigger
-- already demands; these columns only describe how far an attempt has got.

ALTER TABLE media_asset_versions
    -- The named phase of the current attempt. NULL means no attempt has
    -- reported. Free text is deliberately not allowed: an unknown stage would
    -- reach the API and the UI with nothing able to render it.
    ADD COLUMN processing_stage TEXT,
    -- 0..100 over the whole attempt, derived from measured work (FFmpeg's own
    -- structured progress against the ffprobe duration), never from elapsed
    -- wall-clock time.
    ADD COLUMN processing_progress_percent SMALLINT,
    ADD COLUMN processing_updated_at TIMESTAMPTZ,
    -- The operation identity of the attempt these numbers describe. A retry
    -- carries a new identity, so a late write from an abandoned attempt cannot
    -- be mistaken for progress on the current one.
    ADD COLUMN processing_attempt_token TEXT,

    ADD CONSTRAINT media_asset_versions_processing_stage_known CHECK (
        processing_stage IS NULL
        OR processing_stage IN ('TRANSCODING', 'PACKAGING')
    ),
    ADD CONSTRAINT media_asset_versions_processing_percent_bounded CHECK (
        processing_progress_percent IS NULL
        OR processing_progress_percent BETWEEN 0 AND 100
    ),
    -- A stage, a percentage, and a timestamp are one observation. Half of one
    -- is not representable.
    ADD CONSTRAINT media_asset_versions_processing_observation_coherent CHECK (
        (processing_stage IS NULL
            AND processing_progress_percent IS NULL
            AND processing_updated_at IS NULL
            AND processing_attempt_token IS NULL)
        OR (processing_stage IS NOT NULL
            AND processing_progress_percent IS NOT NULL
            AND processing_updated_at IS NOT NULL
            AND processing_attempt_token IS NOT NULL)
    );

-- Existing rows are left NULL on purpose. A version that is already READY has
-- nothing to report, and a version that was mid-flight when this migration ran
-- reports nothing until its next attempt writes an observation — which is the
-- truth, rather than a fabricated zero.
