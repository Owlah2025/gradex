# Schema-41 isolated PostgreSQL restore verification

Date: 2026-09-23 UTC

## Selected source snapshot

- Snapshot: `be759dc254199749ac8ae3a22d47a4f604abea83d50d41db64c576c0797d7222`.
- Restic capture time: `2026-09-23T15:00:09Z`; completion marker: `2026-09-23T15:00:38Z`.
- At restore start (`15:26:18Z`), the snapshot was about 26 minutes old.
- Host/tags: `srv1900125`, `gradex-production,postgresql-custom`.
- The snapshot contains the PostgreSQL custom dump and schema-state/checksum sidecars. Sidecar SHA-256 `a91575fb5c5cac8bc39e2ed34bc3481293c6a79b9fde0ecf926dc99ca645f733` matched the embedded checksum; its state was `41|false`.
- Restored source counts matched the sidecar: accounts `20`, courses `10`, course-access invitations `13`, active invitation-sourced entitlements `10`, enrollments `14`.
- This is the newest valid completed schema-41 snapshot observed before artifact staging. A prior successful restore of the 14:00 snapshot `47eb759b6ff110e380baaf899d2f70a53b8e81dd3f7d3af8b754974309b324fd` remains recorded separately on the host and is superseded for this release.

## Isolated restore

- Restore target container: `gradex-restore-verify`; dedicated volume: `gradex-restore-verify-data`.
- Restore target container ID: `57a2df9066e55ceccbc3d15de6f9782bbc273703e43b4e5881606cddb9623e59`; live PostgreSQL container ID: `048d55e61923a4c564fd6f154b1ceec21b28786c1500bba1da88d94aae625c72`.
- The restore target had no published ports and only the Docker default bridge network. Its ID differs from the production PostgreSQL container. The restore used only the separate target and did not connect to or write the live database.
- Restore started at `2026-09-23T15:26:18Z`; the restored DB was available and returned `41|false` by `15:26:40Z`.

## Verification result and timing

- `host.sh verify-restore` ran from `2026-09-23T15:26:40Z` to `2026-09-23T15:26:51Z`, exit code `0`.
- Restic deep check covered 62 snapshots and 12 packs and reported no errors.
- The verification confirmed snapshot identity, schema `41|false`, required tables, and exact restored record counts.
- End-to-end elapsed time from restore start through successful verification was 33 seconds, within the Founder-approved 4-hour operational RTO target.
- This measurement covers the current production snapshot and host; it demonstrates today's restore path, not a future full-launch data-volume guarantee or a contractual RTO.

## Retained transcript

The complete `host.sh restore` / `host.sh verify-restore` transcript is retained at:

`/home/deploy/gradex-production/backups/device43-isolated-restore-2026-09-23-1500.log`

Transcript SHA-256: `b6a9c5942f7d8f6a0119dad667f4cce3fde510323fa479414b3bb1b49e401711`.

The repository's restore provenance markers identify this same snapshot and schema. The dedicated isolated restore container and volume remain available for inspection; no application container, live database, or release selection was changed.
