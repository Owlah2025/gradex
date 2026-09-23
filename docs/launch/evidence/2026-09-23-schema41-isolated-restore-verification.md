# Schema-41 isolated PostgreSQL restore verification

Date: 2026-09-23 UTC

## Selected source snapshot

- Latest selected snapshot: `b7c1089edac1078b80891be1147dbc658e47522942d9f432682e9a76f79eaf99`.
- Restic capture time: `2026-09-23T17:00:10.200565317Z`; completed backup marker:
  `2026-09-23T17:00:40Z`.
- At restore start (`2026-09-23T17:04:25Z`), this snapshot was about four minutes old.
- Host/tags: `srv1900125`, `gradex-production,postgresql-custom`.
- The snapshot contains the PostgreSQL custom dump and schema-state/checksum sidecars. Sidecar SHA-256
  `a91575fb5c5cac8bc39e2ed34bc3481293c6a79b9fde0ecf926dc99ca645f733` matched the embedded checksum;
  its state was `41|false` and source counts were `20|10|13|10|14`.
- The earlier 16:00 snapshot `a6c3271ff0426b05da876c07e585d3380f45c4df062f3d61d5fdcd951e32e1fb`
  was also restored successfully, but this later completed schema-41 snapshot supersedes it for
  maintenance planning.

## Isolated restore

- Restore target container: `gradex-restore-verify`; dedicated volume: `gradex-restore-verify-data`.
- Restore target container ID: `dc09f2102375a6e7717887ea11e29b9de9f64515489ec8225f59b77ba43f1dd7`; live PostgreSQL container ID: `048d55e61923a4c564fd6f154b1ceec21b28786c1500bba1da88d94aae625c72`.
- The restore target had no published ports and only the Docker default bridge network. Its ID differs from production PostgreSQL. Restore used the separate target and did not connect to or write the live database.
- Restore started at `2026-09-23T17:04:25Z`; the restored DB was available at
  `2026-09-23T17:04:49Z` and returned `41|false`. Restored record counts were
  `20|10|13|10|14`, matching the snapshot sidecar.

## Verification result and timing

- `host.sh verify-restore` completed at `2026-09-23T17:05:04Z`, exit code `0`.
- Restic deep check covered 62 snapshots and 7 packs and reported no errors.
- The verification confirmed snapshot identity, schema `41|false`, required tables, and exact restored record counts.
- End-to-end elapsed time from restore start through successful verification was 39 seconds, within the Founder-approved 4-hour operational RTO target.
- This measurement covers the current production snapshot and host; it demonstrates today's restore path, not a future full-launch data-volume guarantee or a contractual RTO.

## Retained transcript

The complete `host.sh restore` / `host.sh verify-restore` transcript for the latest snapshot is retained at:

`/home/deploy/gradex-production/backups/device43-isolated-restore-2026-09-23-1700.log`

Transcript SHA-256: `58616cb794d8d399c8c02922e36b52f6636a1fce9c353a5b3b78fd184e3ba52d`.

The repository's restore provenance markers identify this same snapshot and schema. The dedicated isolated restore container and volume remain available for inspection; no application container, live database, or release selection was changed.
