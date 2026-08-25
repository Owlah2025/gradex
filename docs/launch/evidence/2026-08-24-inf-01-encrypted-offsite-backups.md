# INF-01 — Encrypted Offsite Backups + Restore Proof

Date: 2026-08-24
Repository: `gradex-ui-antigravity`
Branch: `ui-antigravity-20260817`
Authorization: Founder-authorized INF-01 remediation tranche in the task request.

## 1. Overall verdict

**PARTIAL — implementation and disposable S3-compatible proof complete; real offsite provider proof pending.**

Implementation is fail-closed and ready for a controlled provider configuration. A real provider smoke backup/remote restore was not run because `/var/lib/gradex/runtime.env` is absent in this workspace and no approved backup-provider credential set is available. No production or retained Gradex data was uploaded.

## 2. Finding and previous risk

Ox Alpha finding INF-01 identified unencrypted PostgreSQL dumps retained only beside the production database. Host loss could remove the database and every recovery copy together; operator/host compromise could expose retained PII, access, and payment state.

Historical audit evidence was not edited: `docs/reviews/2026-08-24-ox-alpha-full-repository-review.md` remained part of the pre-existing protected dirty/untracked baseline.

## 3. Audit matrix

| Existing property | Current implementation before INF-01 | Keep? | Required change |
|---|---|---:|---|
| Dump format | `host.sh` used `docker exec ... pg_dump --format=custom --no-owner --no-acl` | Yes | Same command now writes protected temporary staging |
| Schema guard | Clean `schema_migrations` state captured before and after dump; changes abort | Yes | Same guard preserved |
| Serialization | Non-blocking `flock` on `/var/lib/gradex/backups/.backup.lock` | Yes | Shared by backup/init/restore/verify-restore |
| Local permissions | State/backup directory mode 0700; artifacts mode 0600; systemd `UMask=0077` | Yes | New staging and secret validation retain/tighten this |
| Checksums | SHA-256 sidecars for dump and schema metadata | Yes | Sidecars are included in the encrypted snapshot and rechecked after remote extraction |
| Restore | Separate `restore-postgres` service and exact `restore-data` volume; `pg_restore --exit-on-error --single-transaction`; API restore health check | Yes | Restore input now comes from restic, never `latest.dump` |
| Retention | Local timestamped dumps plus mutable `latest.dump`; no offsite generations | No | Native restic `forget --prune` policy |
| Freshness | `latest.completed-at` was written after local dump creation | No | Same marker path is written only after remote success/check/cleanup |
| Scheduling | Hourly `Persistent=true` systemd timer; installer does not enable/start timers | Yes | Existing timer retained; service gets bounded timeout/network access |
| Monitoring | Existing monitor reads the completion marker | Yes | Marker semantics change to successful offsite backup; no general monitoring tranche added |

## 4. Selected backup tool

Selected: **restic 0.19.1**, installed by `deploy/hostinger/install-restic.sh`.

Restic was selected because one mature client owns authenticated client-side encryption, S3-compatible repositories, snapshot generations, integrity checks, restore, and native retention/prune. The official documentation supports `RESTIC_PASSWORD_FILE`, S3-compatible repository URLs, `check`, `restore`, and native `forget/prune` workflows: [restic repository preparation](https://restic.readthedocs.io/en/stable/030_preparing_new_repo.html), [backup](https://restic.readthedocs.io/en/stable/040_backup.html), [repository checks](https://restic.readthedocs.io/en/stable/045_working_with_repos.html), and [restore](https://restic.readthedocs.io/en/stable/050_restore.html).

Rejected alternatives:

- `age` plus a custom uploader would require new upload, repository, retention, remote-integrity, and restore glue.
- `rclone`, `s3cmd`, and provider-specific scripts were not present in the repository and would not own encrypted snapshot semantics.
- Custom cryptography was rejected.

Version/provenance: the installer pins v0.19.1, supports Linux amd64 and arm64, downloads only over HTTPS, verifies the pinned release archive SHA-256, installs root-owned mode 0755, and never self-updates during a backup. The official release page identifies v0.19.1 and reproducible official binaries: [restic v0.19.1 release](https://github.com/restic/restic/releases/tag/v0.19.1).

## 5. Final architecture

```text
PostgreSQL
  -> pg_dump --format=custom into mode-0600 temporary staging
  -> schema and SHA-256 validation
  -> restic client-side authenticated encryption
  -> dedicated S3-compatible bucket/prefix over HTTPS
  -> remote snapshot visibility check
  -> restic structural check
  -> native forget/prune retention
  -> successful-offsite marker

Encrypted repository
  -> restic snapshot selection
  -> remote dump/schema/checksum extraction
  -> checksum verification
  -> pg_restore into exact disposable restore-postgres target
  -> schema/data/API invariants
```

The backup repository is configured separately from the application media bucket through `GRADEX_BACKUP_S3_*` variables. The production endpoint must be HTTPS; HTTP is accepted only for explicit localhost disposable proof mode.

## 6. Encryption and key handling

Restic repository data uses restic's authenticated encryption; the official design documents AES-256-CTR with Poly1305-AES authentication and scrypt-based password derivation: [restic encryption design](https://restic.readthedocs.io/en/latest/100_references.html). No encryption primitive was implemented in Gradex.

The runtime passes the password through `RESTIC_PASSWORD_FILE`, never `RESTIC_PASSWORD` or a command-line argument. The password file must be non-empty, readable by the backup user, and mode 0400 or 0600. The password file is never included in the staging directory or remote repository; the repository's own encrypted key envelope is not the operator password.

## 7. Key custody

- Runtime copy: `GRADEX_BACKUP_PASSWORD_FILE`, default `/var/lib/gradex/backup-restic-password`, mode 0600.
- Recovery copy: Founder/operator-controlled password manager or offline encrypted store outside the VPS.
- The code validates the local protected copy but cannot prove Founder custody; this remains an operational prerequisite.
- A host compromise with remote delete authority can still delete remote snapshots; provider versioning/object lock or a separate immutable copy is recommended.

## 8. Remote object storage and least privilege

Configuration supports generic S3-compatible providers through endpoint, bucket, prefix, region, access key, secret key, and bucket-lookup mode. The backup destination must be a dedicated bucket or dedicated prefix, not the application media bucket.

Expected provider policy is limited to listing the backup prefix and getting, putting, deleting, and completing multipart objects within that backup destination. No account-global administrator credential and no media-bucket authority is required. Separate backup credentials must not be shared with the Gradex application runtime.

TLS certificate verification is left enabled by restic; there is no `--insecure`, skip-verify, or custom signed-URL path.

## 9. Plaintext lifecycle

During a successful run, plaintext exists only in a newly-created `.offsite-staging.*` directory under the protected backup directory. The custom dump, schema metadata, and sidecars are mode 0600. Restic encrypts the complete staging directory before remote storage.

After remote snapshot visibility, repository check, retention attempt, and snapshot-presence assertion, the current staging is removed and stale pipeline staging from earlier failed runs is removed on the next successful run. Ordinary filesystem removal is not claimed to be cryptographic SSD erasure.

On encryption or remote failure, the current protected staging remains for retry/diagnosis; no successful-offsite marker is written. Prior remote generations are not removed by a failed upload.

## 10. Retention

Chosen launch policy, encoded in `runtime.env.example` and validated at runtime:

- `keep-last=2`
- `keep-hourly=48`
- `keep-daily=14`
- `keep-weekly=8`

Retention uses restic `forget --prune`, never manual bulk object deletion. All retention values must be positive integers. After prune, the repository must still contain at least one tagged snapshot and the newly-created snapshot must still exist; otherwise the service fails before marker update.

## 11. Failure semantics

- Missing endpoint, bucket, access key, secret key, password file, executable restic binary, invalid endpoint/TLS mode, invalid retention, or missing timeout fails before `pg_dump`.
- Encryption/repository initialization/upload failure returns non-zero and leaves protected staging; plaintext is never sent to the remote repository as a fallback.
- Remote-unavailable failure returns non-zero, leaves prior snapshots intact, leaves `latest.completed-at` unchanged, and retries on the next scheduled/manual run.
- Restic commands are bounded by `GRADEX_BACKUP_TIMEOUT_SECONDS` (default 300 seconds); the systemd service has `TimeoutStartSec=360s`.
- Structural repository check is required on every backup. `verify-restore` additionally runs `restic check --read-data` as the deep check.
- Retention failure does not erase a successfully-created offsite snapshot: the marker is written after remote success and cleanup, then the service returns non-zero and journals the retention problem.
- Successful marker files: `latest.offsite.snapshot` and the existing `latest.completed-at`; the latter is the only freshness input required by the existing monitor.

## 12. Systemd integration

Preserved units:

- `deploy/hostinger/systemd/gradex-backup.service.in` -> `host.sh backup`
- `deploy/hostinger/systemd/gradex-backup.timer` -> `OnCalendar=hourly`, `Persistent=true`

Hardening retained and extended minimally: non-root supplied user/group, `UMask=0077`, `NoNewPrivileges=true`, `PrivateTmp=true`, `ProtectSystem=full`, explicit `ReadWritePaths=/var/lib/gradex`, and `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6` for Docker plus outbound S3 TLS. The installer still only renders/validates/installs units and daemon-reloads; it does not enable or start timers.

## 13. Local S3-compatible proof

Command:

```bash
GRADEX_BACKUP_RESTIC_BINARY=/tmp/gradex-inf01-restic.zF48da/restic ./deploy/scripts/verify-inf-01-backup.sh
```

Result: PASS. The harness used only synthetic PostgreSQL records, an isolated pinned MinIO container, and an isolated pinned PostgreSQL source/restore container. It created three distinct snapshots across initial, repeated, and post-outage retry runs; the final repository retained 3 snapshots (minimum required: 2). It verified remote snapshot presence, structural/deep integrity, correct-key extraction, wrong-key rejection, restore checksums, schema/data invariants, no plaintext synthetic sentinel in mirrored remote objects, missing credential/key rejection, production HTTP rejection, unchanged marker/prior snapshots during outage, and retry success.

Observed local proof snapshot IDs (non-secret disposable identifiers):

- `381d9ab4ac9a1b505d93870cf5cde47888fddc9cefcac51083d9f6d0d4e86c51`
- `800341b2401f696f7bc7ec06f942b1a3c7794622fed0658d5f3c99243c9c2e51`
- `e0f82f6c47468ce215fd1c48c83dd50934e15d8089d8b82fcf63d908a224e47a`

Cleanup verification found no `gradex-inf01-*` containers and no `/tmp/gradex-inf01-proof.*` directory after the run.

## 14. Actual offsite proof

**NOT RUN.** Exact dependency missing: an approved real S3-compatible off-host endpoint/bucket and scoped access key/secret are not present in this environment; `/var/lib/gradex/runtime.env` is absent. Existing application media credentials were not reused, and no production data was uploaded to a new provider.

Required final smoke, using synthetic disposable PostgreSQL data through the same path:

1. Configure the dedicated provider endpoint/bucket/prefix, region, scoped credential, and runtime restic password file securely on the VPS.
2. Run `host.sh backup-init` once, then `host.sh backup`.
3. Confirm the remote snapshot exists, remove any local restored copy, run `host.sh restore <full-snapshot-id>`, and run `host.sh verify-restore`.
4. Record provider/object-store evidence without recording credentials or password material.

## 15. Wrong-key proof

PASS in local S3 proof: replacing the correct password file with a separate wrong password caused restic to fail with `Fatal: wrong password or no key found`; no correct password appeared in the captured log.

## 16. Restore proof

PASS in local S3 proof: the harness listed and extracted files with `restic dump` from the encrypted repository snapshot, verified the remote sidecars, and fed the extracted dump to `pg_restore`. It did not use the original local staging dump for restore.

Production `host.sh restore` follows the same contract and records only the exact restic snapshot ID in `restored-source`; its fixed target is the separate `restore-postgres` service and exact `${GRADEX_HOST_PROJECT}_restore-data` volume.

## 17. Restore invariants

Local synthetic restore result:

- schema `15|false`
- Accounts: `3`
- Courses: `1`
- approved Course Access Invitations: `1`
- ACTIVE Entitlements with non-null invitation provenance: `1`
- Enrollments: `1`
- no `pg_restore` errors ignored

Production restore additionally requires the isolated API restore service to become healthy, preserving the prior Gradex restore contract.

## 18. Repeatability

PASS: initial snapshot restored, second snapshot restored, remote endpoint outage returned failure without changing the marker or prior generations, then a retry created and restored a third distinct snapshot. All proof infrastructure was isolated and cleaned.

## 19. Tests and verification

Exact commands/results:

- `bash -n deploy/hostinger/host.sh deploy/hostinger/backup-restic.sh deploy/hostinger/install-restic.sh deploy/scripts/verify-inf-01-backup.sh deploy/scripts/verify-hostinger-systemd.sh` — PASS.
- `./deploy/scripts/verify-inf-01-backup.sh` with the verified local restic binary override — PASS.
- `./deploy/scripts/verify-hostinger-systemd.sh` — PASS; rendering, cadence, entrypoints, persistence, secret isolation, freshness behavior, hardening assertions, and `systemd-analyze verify` passed.
- `git diff --check` — PASS.
- Official amd64 archive verification: SHA-256 `f415415624dcc452f2a02b8c33641791a8c6d6d3b65bbb3543fcf9a25151585c2`; verified binary output `restic 0.19.1 compiled with go1.26.4 on linux/amd64`; extracted binary mode `755`.
- Pinned arm64 archive SHA-256 in installer: `a5f64aaab53d51e311fa3829124c5b703f2d14cf187d8640b6be3b2b49376465`.
- Current shell UID was 1000, so the root-only installer was not executed against the shared host; it was syntax-checked and its downloaded official archive checksum was verified.

Tests intentionally not run: canonical 168-case application E2E, frontend/backend application suites, production deployment, and general MED-04/MED-05 monitoring work; this tranche changed only deployment/backup surfaces.

## 20. Files changed

- `deploy/hostinger/backup-restic.sh` — sourced restic/S3 validation, encrypted snapshot, check, restore-file, retention, and marker primitives.
- `deploy/hostinger/host.sh` — preserved dump/locking/isolated restore path; added encrypted remote backup, explicit repository initialization, remote restore selection, timeout, marker semantics, and stale staging cleanup.
- `deploy/hostinger/install-restic.sh` — pinned, architecture-aware, checksum-verified root installer.
- `deploy/hostinger/runtime.env.example` — backup endpoint/credential/password/retention/timeout placeholders.
- `deploy/hostinger/systemd/gradex-backup.service.in` — bounded timeout, writable state path, and narrow address-family allowance.
- `deploy/scripts/verify-inf-01-backup.sh` — disposable MinIO/PostgreSQL integration proof.
- `deploy/scripts/verify-hostinger-systemd.sh` — assertions for new service hardening/timeout.
- `deploy/monitoring/README.md` — marker semantics documented; no general monitoring implementation.
- `deploy/hostinger/README.md` — operator setup, retention, failure, restore, key-custody, and timer procedure.
- `docs/launch/evidence/2026-08-24-inf-01-encrypted-offsite-backups.md` — this evidence.

## 21. Evidence and tracker impact

Evidence path: `docs/launch/evidence/2026-08-24-inf-01-encrypted-offsite-backups.md`.

Canonical MVP tracker remains unchanged: **45 / 53 = 84.9%** before and after this tranche. `SY-08` remains `BLOCKED` because its row covers both backup and monitoring timers and the installer deliberately did not enable/start production timers. Local MinIO proof does not satisfy the actual offsite acceptance boundary, so no tracker row is promoted.

Paid-beta P0 list after this tranche: INF-01 is implemented with real offsite proof pending; MED-04 worker/email monitoring and MED-05 disk monitoring remain. No GAP-06, Resend, Course Access, or application feature work was performed.

## 22. Ox Alpha status

**INF-01: OPEN -> IMPLEMENTED, REAL OFFSITE PROOF PENDING.**

Do not change the immutable Ox Alpha review to CLOSED until a real off-host repository has been tested end-to-end.

## 23. Historical plaintext backups

Existing retained plaintext backups and the old `latest.dump` pointer were not deleted, rewritten, or migrated. Post-cutover retirement plan:

1. Complete and record the real encrypted offsite smoke/restore proof.
2. Run a new production backup successfully and verify the remote marker.
3. Retain historical local plaintext temporarily while recovery acceptance is reviewed.
4. Founder/operator explicitly approves retirement.
5. Remove only the exact historical generations under the approved policy; do not claim cryptographic SSD erasure.

## 24. Repository safety

No `git reset`, `git clean`, `git stash`, `git restore`, broad checkout, repo-wide formatting, application deployment, retained database drop, retained volume removal, `docker compose down -v`, or volume prune was performed. Disposable proof used only exact uniquely named MinIO/PostgreSQL containers with no retained Gradex volumes; cleanup was verified.

## 25. Required Founder/operator action

Configure these categories through the protected VPS/runtime mechanism, never in chat or Git:

- dedicated backup bucket or prefix separate from media;
- HTTPS S3-compatible endpoint and region;
- scoped backup access key ID and secret with only backup-destination list/get/put/delete/multipart authority;
- restic password file on the VPS plus an independent off-host recovery copy;
- root-run `install-restic.sh`, secure runtime configuration, one `backup-init`, and the real synthetic smoke/restore proof.

Do not paste the access secret or restic password into chat.

## 26. Recommended next step

Perform the real synthetic remote smoke/restore proof first. Once it passes and key custody is recorded, close INF-01; then proceed to the separately authorized MED-04/MED-05 monitoring tranche.

## 27. Final status line

**INF-01 PARTIAL — IMPLEMENTATION PROVEN, REAL OFFSITE RESTORE PROOF REQUIRED**