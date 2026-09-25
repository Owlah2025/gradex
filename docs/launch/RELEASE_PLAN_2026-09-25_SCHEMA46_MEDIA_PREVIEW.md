# Schema 44 to 46 release boundary

This is a future production procedure, not evidence of deployment.

The named command is `up-core-schema-46-media-preview`. It accepts only the
`SCHEMA46_CAPABILITY=auto-enhancement-lesson-preview-v1` bundle and starts only
from `44|false`. It verifies immutable candidate images and bundle contents,
their compiled schema range `44..46`, migration hashes for 0045 and 0046, fresh
schema-44 backup/restore evidence, the schema-46 old-behaviour rollback artifact,
removed API/worker producers, zero active media claims, PostgreSQL/Redis health,
and `MEDIA_AUTO_ENHANCEMENT_RECOVERY_ENABLED=false`.

The command applies 0045, verifies `45|false`, then applies 0046 and verifies
`46|false` before starting the candidate. It never uses generic production down,
force, or marker repair. A dirty marker after either step means no application is
selected automatically; preserve the actual marker and make a recovery decision.

If the candidate cannot start or a post-start smoke fails on clean schema 46,
select the separately imported schema46 rollback-compatible old-behaviour
artifact through application release selection. Its compiled range is `44..46`;
no database down runs. It retains legacy previews, direct Course grants, device
behaviour and protected playback, and has no automatic recovery scheduler or
Lesson-preview route.

After candidate start, validate health/readiness, legacy Course preview, Lesson
preview HLS, Student protected playback, and purchase/access. 3C-C remains off.
Enabling it later requires one legitimate operational manual 3C-B observation;
test or release smoke evidence is not that observation.
