# Schema-41 isolated PostgreSQL restore verification

Date: 2026-09-23 UTC

## Selected source snapshot

- Snapshot: `a6c3271ff0426b05da876c07e585d3380f45c4df062f3d61d5fdcd951e32e1fb`.
- Restic capture time: `2026-09-23T16:00:13Z`; completed backup marker: `2026-09-23T16:00:43Z`.
- At restore start (`16:02:10Z`), the snapshot was about two minutes old.
- Host/tags: `srv1900125`, `gradex-production,postgresql-custom`.
- The snapshot contains the PostgreSQL custom dump and schema-state/checksum sidecars. Sidecar SHA-256 `a91575fb5c5cac8bc39e2ed34bc3481293c6a79b9fde0ecf926dc99ca645f733` matched the embedded checksum; its state was `41|false`.
- Restored source counts matched the sidecar: accounts `20`, courses `10`, course-access invitations `13`, active invitation-sourced entitlements `10`, enrollments `14`.
- Earlier successful restores of the 14:00 and 15:00 schema-41 snapshots remain recorded separately on the host and are superseded for this release by the 16:00 snapshot.

## Isolated restore

- Restore target container: `gradex-restore-verify`; dedicated volume: `gradex-restore-verify-data`.
- Restore target container ID: `477753da24d133bee182fc217684a0035b705e2919a197e1c6b03dd3d682810d`; live PostgreSQL container ID: `048d55e61923a4c564fd6f154b1ceec21b28786c1500bba1da88d94aae625c72`.
- The restore target had no published ports and only the Docker default bridge network. Its ID differs from production PostgreSQL. Restore used the separate target and did not connect to or write the live database.
- Restore started at `2026-09-23T16:02:10Z`; the restored DB was available and returned `41|false` by `16:02:35Z`.

## Verification result and timing

- `host.sh verify-restore` ran from `2026-09-23T16:02:35Z` to `2026-09-23T16:02:47Z`, exit code `0`.
- Restic deep check covered 62 snapshots and 14 packs and reported no errors.
- The verification confirmed snapshot identity, schema `41|false`, required tables, and exact restored record counts.
- End-to-end elapsed time from restore start through successful verification was 37 seconds, within the Founder-approved 4-hour operational RTO target.
- This measurement covers the current production snapshot and host; it demonstrates today's restore path, not a future full-launch data-volume guarantee or a contractual RTO.

## Retained transcript

The complete `host.sh restore` / `host.sh verify-restore` transcript is retained at:

`/home/deploy/gradex-production/backups/device43-isolated-restore-2026-09-23-1600.log`

Transcript SHA-256: `a2edef272fb06d7cac7c409363eb4b3565966b7b9495fe4f4a03e4d6cc8ab5b0`.

The repository's restore provenance markers identify this same snapshot and schema. The dedicated isolated restore container and volume remain available for inspection; no application container, live database, or release selection was changed.
