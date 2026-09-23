# Schema-41 isolated PostgreSQL restore verification

Date: 2026-09-23 UTC

## Selected source snapshot

- Latest selected snapshot: `586f7beda487875fe201ae84b0dca8d46720943f862055f753ac880f30320f66`.
- Restic capture time: `2026-09-23T19:00:08.86608157Z`; completed backup marker:
  `2026-09-23T19:00:41Z`.
- At restore start (`2026-09-23T19:12:08Z`), this snapshot was about twelve minutes old.
- Host/tags: `srv1900125`, `gradex-production,postgresql-custom`.
- The snapshot contains the PostgreSQL custom dump and schema-state/checksum sidecars. Sidecar SHA-256
  `fb53da3c7844248860974bc0db3558045885ac0401ed516a858ce50bf9ec1476` matched the embedded checksum;
  its state was `41|false` and source counts were `21|10|14|10|14`.
- The previously verified 17:00 snapshot `b7c1089edac1078b80891be1147dbc658e47522942d9f432682e9a76f79eaf99`
  and earlier 16:00 snapshot `a6c3271ff0426b05da876c07e585d3380f45c4df062f3d61d5fdcd951e32e1fb`
  remain historical successful restores; the 19:00 snapshot is now the current verified recovery point.

## Isolated restore

- Restore target container: `gradex-restore-verify`; dedicated volume: `gradex-restore-verify-data`.
- Restore target container ID: `e083bfe8fff6f42501567bcfaa0528cb3d5f38403a21622f048724643a7e8362`; live PostgreSQL container ID: `048d55e61923a4c564fd6f154b1ceec21b28786c1500bba1da88d94aae625c72`.
- The restore target had no published ports and only the Docker default bridge network. Its ID differs from production PostgreSQL. Restore used the separate target and did not connect to or write the live database.
- Restore started at `2026-09-23T19:12:08Z`; the restored DB was available at
  `2026-09-23T19:12:31Z` and returned `41|false`. Restored record counts were
  `21|10|14|10|14`, matching the snapshot sidecar.

## Verification result and timing

- `host.sh verify-restore` completed at `2026-09-23T19:12:44Z`, exit code `0`.
- Restic deep check covered 62 snapshots and 11 packs and reported no errors.
- The verification confirmed snapshot identity, schema `41|false`, required tables, and exact restored record counts.
- End-to-end elapsed time from restore start through successful verification was 36 seconds, within the Founder-approved 4-hour operational RTO target.
- This measurement covers the current production snapshot and host; it demonstrates today's restore path, not a future full-launch data-volume guarantee or a contractual RTO.

## Retained transcript

The complete `host.sh restore` / `host.sh verify-restore` transcript for the latest snapshot is retained at:

`/home/deploy/gradex-production/backups/device43-isolated-restore-2026-09-23-1900.log`

Transcript SHA-256: `ca9ffaefd169a1c7a04f72f671ca6ff4c7065454a885d12eb611f42e2a6f74a1`.

The repository's restore provenance markers identify this same snapshot and schema. The dedicated isolated restore container and volume remain available for inspection; no application container, live database, or release selection was changed.
