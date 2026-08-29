# Hostinger public staging deployment

This directory maps the frozen S12 artifacts onto one Hostinger KVM 2 VPS. Docker Compose keeps the
frontend, API, worker, PostgreSQL, Redis, and edge as separate processes. Only TCP 80/443 and UDP 443
are published by Compose; PostgreSQL, Redis, and application ports remain private. Cloudflare R2 is
the only external data-plane service.

These files are a deployment mechanism, not evidence that a public environment exists. Record T047
evidence only after every command below succeeds against the real hostname.

## Required external inputs

Create these outside Git and do not paste credentials into chat or evidence:

- Hostinger VPS IP, non-root SSH user, and SSH public-key access;
- a staging hostname in a Cloudflare zone;
- a private R2 bucket and a bucket-scoped API token with object read/write/delete access;
- an alert webhook URL/token when external alert delivery is being proved.

The R2 S3 endpoint is `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`, with region `auto` and virtual-host
addressing. The bucket must remain private. Cloudflare's [presigned URL documentation](https://developers.cloudflare.com/r2/api/s3/presigned-urls/)
requires the S3 API domain rather than a custom domain, and its [CORS documentation](https://developers.cloudflare.com/r2/buckets/cors/)
requires bucket CORS for browser presigned requests. Configure the contents rendered by
`host.sh prepare` at `/var/lib/gradex/r2-cors.json`; it permits only the exact `PUBLIC_ORIGIN` and the
`GET`, `HEAD`, and `PUT` methods.

## 1. Host baseline

Use supported Ubuntu 22.04 or 24.04 LTS, install current OS security updates plus Docker Engine with
Compose, and create a non-root operational user with SSH-key access and Docker access. Keep a second
working SSH session open before changing SSH or firewall rules. At both the Hostinger firewall and
UFW layers, allow the confirmed SSH port plus HTTP/HTTPS and deny other inbound traffic. Do not expose
3000, 5432, 6379, or 8080.

From the repository checkout on the VPS, run the read-only baseline check with privilege:

```bash
sudo ./deploy/hostinger/audit-host.sh
```

The check requires time synchronization, key-only SSH posture, default-deny UFW, Docker, and no
database/Redis/application listener on a host interface. Correct a failure deliberately; the script
does not mutate SSH or firewall configuration.

## 2. Build and transfer immutable releases

Build on a clean trusted machine at the exact revision to deploy:

```bash
./deploy/hostinger/release.sh build
./deploy/hostinger/release.sh export
```

Transfer the repository checkout, ignored `release.env`, image archive, and checksum over SSH. Verify
the checksum on the VPS before `docker load`. The image tags, image IDs, and OCI revision labels in
the ignored manifest bind the release to one full Git SHA. Do not use `latest`.

Create two releases at two T046-compatible commits before the provider rollback drill. A pre-T046
backend is not an acceptable rollback target once Redis is TLS/auth-only. If a compatible release was
built before a later tooling commit, run `release.sh record <full-sha>` and
`release.sh export <full-sha>` while its three labeled images remain local.

## 3. Protected runtime configuration

On the VPS, copy `runtime.env.example` to `/var/lib/gradex/runtime.env`, populate it locally, and set
mode 0600. Generate independent random values for every blank secret. The database URLs use private
Compose hostnames; `GRADEX_E2E_ADMIN_DB_URL` connects to the `postgres` maintenance database and is
accepted only by the safety-gated proof image. Never print or commit the populated file.

```bash
sudo install -d -m 0700 -o "$USER" -g "$USER" /var/lib/gradex
install -m 0600 deploy/hostinger/runtime.env.example /var/lib/gradex/runtime.env
./deploy/hostinger/host.sh prepare
```

The four `LOGIN_*` values are non-secret capacity controls. The current KVM2 baseline is one active
password verification, 500 waiting requests, a 45-second queue wait, and a login-only 60-second
request deadline. Do not raise concurrency from raw-hash benchmarks alone; run the external
browser-equivalent LG-019 scenario and capture API, PostgreSQL, Redis, CPU, RAM, and swap evidence.
Caddy has no shorter response timeout configured, while unrelated API requests retain the ordinary
30-second server write timeout.

`prepare` validates the release labels and configuration, creates a private 90-day Redis CA/server
certificate with the `redis` DNS identity, and renders the narrow R2 CORS policy. Apply that policy
through the R2 dashboard or API without changing public bucket access.

Before deploying application traffic, run the explicit provider contract test from a protected shell:

```bash
cd backend
go test -tags=provider -run '^TestCloudflareR2PreservesPrivateImmutableObjectIdentityContract$' ./internal/storage
```

Export only `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, and `PUBLIC_ORIGIN` into that
shell. `S3_PRESIGN_ENDPOINT` is optional and defaults to `S3_ENDPOINT`, so the existing R2 deployment
continues to sign with its configured R2 endpoint. The test uploads disposable objects, proves
private/CORS behavior, records the returned ETag identity, reads exact bytes with `If-Match`, proves
that an overwrite at the same key fails the old identity with `PreconditionFailed`, and removes its
prefix. R2 historical `VersionId` retrieval is not part of this contract; an ETag identity is required
when no usable VersionId is returned. A missing ETag identity or silent current-object substitution is
a hard S4 compatibility failure; do not work around it by weakening media provenance.

## 4. DNS, origin TLS, and Cloudflare

Create an `A`/`AAAA` record for the staging hostname. Keep it DNS-only until Caddy obtains a publicly
valid origin certificate and HTTPS succeeds directly, then enable the Cloudflare proxy and select
[Full (strict) TLS mode](https://developers.cloudflare.com/ssl/origin-configuration/ssl-modes/full-strict/).
Do not use Flexible TLS. Do not add cache-everything rules: authentication,
API, health/readiness, manifests, signed tokens, and personalized protected responses must not be
cached. The Caddy edge explicitly emits `private, no-store` for API/probe routes.

The application trusts only its private Docker proxy network. It does not trust arbitrary Internet
forwarding headers; Cloudflare-specific client-IP restoration is intentionally unnecessary for MVP
correctness.

## 4a. Declaring the environment

This topology runs both staging and production. Which one it is comes from the protected runtime
environment, never from the hostname, and there is no default — a host that does not declare itself
fails preflight instead of quietly running as staging.

`validateStaffComposition` in `cmd/api` exempts **development alone**, and a managed host is never
development. Staging and production therefore share one composition contract; only the LG-021
approval flag differs.

| | staging | production |
|---|---|---|
| `APP_ENV` | `staging` | `production` |
| `PASSWORD_SCREEN_MODE` | `adapter` | `adapter` |
| `COMPROMISED_PASSWORD_ADAPTER_APPROVED` | may stay `false` | `true` |
| `EMAIL_ENABLED` | `true` | `true` |
| `EMAIL_PROVIDER` | `resend` | `resend` |
| `EMAIL_API_KEY` | required | required |
| `SESSION_CSRF_KEY` | required | required |

`STUDENT_REGISTRATION_ENABLED=false` and `AUTH_FAKE_MODE=false` are fixed in the Compose file for
both environments and are not operator-settable.

Both environments require real compromised-password screening **even though public student
registration stays closed**. Staff invitation and onboarding set passwords, so the API's staff
composition refuses to build without `PasswordScreenMode == adapter`. The adapter is the fixed HIBP
Pwned Passwords range endpoint over HTTPS; it needs no API key and no additional provider
credential, only outbound internet, which the API, worker, and bootstrap-admin services already have
through the edge network.

The approval flag is the single production-only rule: `config.go` gates LG-021 on
`environment.IsProduction()`, so staging runs the same real adapter with the flag unset.

`host.sh prepare` and `host.sh up` both refuse an invalid composition before any container starts:
an undeclared or unknown `APP_ENV`, `deterministic` screening on a managed host, production without
the adapter, the adapter without its approval, approval without the adapter, disabled or non-Resend
production email, a missing production email key, fake authentication, or public registration.

## 5. Deploy and verify

```bash
./deploy/hostinger/host.sh up
./deploy/hostinger/host.sh verify
./deploy/hostinger/verify-public.sh --mode direct "https://staging.example.com"
```

For a production host, use the production hostname and `--mode cloudflare` once the origin holds a
certificate and the proxy is enabled.

Replace the example hostname. `verify` reads the selected backend image's maximum supported schema
with `gradex-migrate max-version`, requires the live database to be clean at exactly that version, and
checks worker lifecycle, plaintext Redis refusal, unauthenticated TLS refusal, authenticated
verified-TLS Redis, and public health/readiness.

`verify-public.sh` independently checks public DNS, HTTP-to-HTTPS redirect, certificate validity and
hostname, frontend, `/healthz`, `/readyz`, API security headers, and that no internal service port
answers on the public hostname. Its edge policy is chosen by an explicit `--mode`, never guessed:

- `--mode direct` verifies an origin served straight from Caddy, as staging is today. It makes no
  claim about Cloudflare and says so in its output.
- `--mode cloudflare` additionally requires Cloudflare proxy evidence and the conservative
  expectation that the frontend is not being served from the Cloudflare cache. Production hostnames
  are refused in `direct` mode outright, so omitting a variable can never downgrade production to the
  weaker policy.

## 5a. Live release-acceptance smoke

A deployed environment is verified where it runs, without owning it:

```bash
./deploy/scripts/verify-live-staging-acceptance.sh "https://staging.example.com"
```

The smoke reads the public frontend, locale, privacy and terms routes, `/healthz`, `/readyz`, the
public catalogue and academic options, and a course detail page addressed by a slug it discovers
through the public API rather than a fixture identifier. It then asserts the boundaries: admin,
authoring, and account routes reject anonymous callers, protected media is not served anonymously,
and the anonymous session/admission boundary refuses a foreign origin and an invalid CSRF token
before failing authentication closed.

It never resets, seeds, or repoints a database, never brings a Compose project up or down, and never
mutates an existing record. Optional runtime and provider checks are opt-in and additive:

```bash
GRADEX_LIVE_SMOKE_COMPOSE_PROJECT=gradex-founder-beta \
GRADEX_LIVE_SMOKE_POSTGRES_DB=gradex_founder_beta \
GRADEX_LIVE_SMOKE_PROVIDER_IMAGE=gradex-backend-proof:hostinger-<short-sha> \
GRADEX_LIVE_SMOKE_PROVIDER_ENV_FILE=/home/deploy/r2-staging.env \
  ./deploy/scripts/verify-live-staging-acceptance.sh "https://staging.example.com"
```

The runtime checks observe the operator-named Compose project only. The provider check writes one
object under `capacity/live-smoke-<run-id>/`, confirms its ETag against the written content, and
removes it again before exit. Running against a production hostname additionally requires
`GRADEX_LIVE_SMOKE_ALLOW_PRODUCTION=i-have-authorized-production-smoke`.

Authenticated student, instructor, and admin journeys stay with the isolated E2E suite, which owns
disposable data. The live smoke proves the deployed composition and its critical boundaries, not the
whole product.

## 6. Provider backup and isolated restore

After the controlled public S5/S6 smoke has created provenance-bearing records:

```bash
./deploy/hostinger/host.sh backup
./deploy/hostinger/host.sh restore
./deploy/hostinger/host.sh verify-restore
```

The source database is never destroyed. Backup accepts only a clean schema, records its
`version|false` state before the dump, rejects a schema change during the dump, and checksum-protects
both the temporary custom-format dump and schema metadata before restic encrypts the snapshot. Restore
selects the latest or an explicit snapshot ID, verifies the encrypted snapshot files and both checksums,
invalidates stale restored evidence before replacing the isolated target, provisions a fresh separate
volume/database, and restores without `--clean`. Only a successful restore records the exact restic
snapshot ID in `restored-source`; verification follows that remote identity, performs a deep repository
check, requires the restored schema to match the remote schema metadata, checks Account, Course,
invitation, ACTIVE Entitlement with `source_invitation_id`, and Enrollment records, and then requires
the separate restore API to become healthy.

### Encrypted offsite backup setup

Install the pinned restic binary once on the VPS; this command verifies the architecture-specific SHA-256 before installing a root-owned binary:

```bash
sudo ./deploy/hostinger/install-restic.sh
```

Create a private, backup-only S3-compatible bucket or prefix separate from the application media bucket. Grant the backup credential only list access to that prefix plus object read, write, delete, and multipart-upload permissions required by the provider. Do not reuse the application media credential.

Create a strong restic password, copy it to `/var/lib/gradex/backup-restic-password` with mode `0600`, and store a recovery copy in an operator-controlled off-host password manager or offline encrypted store. The password is not stored in the repository.

Populate the `GRADEX_BACKUP_*` entries in `/var/lib/gradex/runtime.env` with the HTTPS endpoint, dedicated bucket/prefix, region, backup credential, password-file path, and retention values. Production rejects missing values and non-HTTPS endpoints; HTTP is accepted only for an explicit localhost disposable proof.

The canonical Hostinger deployment leaves `GRADEX_BACKUP_POSTGRES_CONTAINER` empty and resolves the
Compose `postgres` service. A backup-only checkout for an existing deployment, such as Founder Beta,
sets it to the exact running PostgreSQL container name. The override is validated and inspected as a
running container; it does not replace, restart, or recreate that container. Keep the backup runtime
file and restic password owned by the scheduled non-root operator with mode `0600`, and keep the
state and backup directories mode `0700`.

Initialize the repository once, then exercise the same command used by systemd:

```bash
./deploy/hostinger/host.sh backup-init
./deploy/hostinger/host.sh backup
./deploy/hostinger/host.sh restore              # latest encrypted snapshot
./deploy/hostinger/host.sh restore <snapshot-id>
./deploy/hostinger/host.sh verify-restore
```

The launch policy keeps at least two latest snapshots, 48 hourly generations, 14 daily generations, and 8 weekly generations. `latest.completed-at` is written only after the encrypted snapshot is visible, repository verification and retention succeed, and the temporary plaintext staging is removed. A retention failure returns non-zero and is journaled even when the newly-created encrypted snapshot remains available remotely.
If encryption or remote access fails, the uniquely named staging directory remains mode 0700 with dump files mode 0600 for retry/diagnosis; the next successful encrypted run removes stale pipeline staging. This is lifecycle control, not a claim of cryptographic SSD erasure.

Retention groups snapshots by stable host and tags rather than the unique plaintext staging path.
The completion marker and plaintext cleanup occur only after retention succeeds; a retention failure
leaves the previous success markers and failed staging unchanged for diagnosis. Inspect retained
staging by name, ownership, mode, type, and size without reading dump contents. Do not delete wildcard
paths or unknown directories; after inspection, prefer the next successful backup's guarded cleanup.

List snapshots and run structural or deep repository checks from a protected shell without printing
the runtime file or password:

```bash
(
  set -a
  . /var/lib/gradex/runtime.env
  set +a
  . ./deploy/hostinger/backup-restic.sh
  backup_validate_configuration
  backup_restic snapshots --tag "$GRADEX_BACKUP_SNAPSHOT_TAG"
  backup_check_repository
  backup_deep_check_repository                 # reads every encrypted pack
)
```

Run repository maintenance only when no backup or restore is active. The host entrypoints share
`/var/lib/gradex/backups/.backup.lock`; do not run `restic unlock` until every local and remote client
using the repository is proven stopped.

For the canonical topology, `host.sh restore` and `verify-restore` use the isolated restore Compose
profile. For a backup-only Founder Beta checkout, perform a separate exact-equivalence proof instead:

1. Select a full 64-hex snapshot carrying both the configured snapshot and `postgresql-custom` tags.
2. Download its dump, schema metadata, and checksum files from restic into a new protected directory;
   verify both checksums and run `pg_restore --list`.
3. Start a disposable PostgreSQL container from the live image digest with no application network and
   no persistent production volume, then restore with `--exit-on-error --single-transaction`.
4. Compare the remote schema state and every current public table name/count against the live source.
   Do not require a positive invitation-provenance Entitlement count when the live source legitimately
   has zero; require exact source-to-restore equality instead.
5. Run `backup_deep_check_repository`, then remove the disposable container and protected plaintext
   restore directory. Never attach the proof container to the live database volume.

The installer does not enable or start timers. After configuration, manual backup, service, and restore proof, enable the backup timer explicitly; enable monitoring separately only after its own configuration and proof. Existing historical plaintext files in `/var/lib/gradex/backups` are not removed by installation; retire them only after Founder/operator approval and a successful encrypted restore drill.
The restic command timeout defaults to 300 seconds and is bounded by the backup service's 360-second start timeout; a timed-out remote operation is a failed run and does not refresh freshness.

## 7. Public smoke, rollback, and alerts

Reset the designated staging proof database only when it is safe to discard the existing smoke data:

```bash
./deploy/hostinger/host.sh seed-smoke
```

Use an SSH local forward to the private PostgreSQL service only for the duration of the existing
external Playwright assertions, then point the existing S5/S6 production-mode suite at the public
origin. Do not expose PostgreSQL in Compose or the firewall. Capture the full 30-step result, browser
console/CORS result, private R2 media retrieval, and unrelated-Student denial without credentials.

The bounded tunnel sequence is:

```bash
# VPS: loopback only
./deploy/hostinger/host.sh open-db-tunnel

# trusted test runner: keep this foreground SSH session open
ssh -N -L 15432:127.0.0.1:15432 <operator>@<vps-address>

# trusted test runner, in a separate protected shell
./deploy/hostinger/verify-provider-smoke.sh

# VPS: always remove the temporary listener
./deploy/hostinger/host.sh close-db-tunnel
```

The runner script requires `GRADEX_PROVIDER_ORIGIN`, the three existing `GRADEX_E2E_*_DB_URL`
values pointing through loopback port 15432, and `GRADEX_PROVIDER_DB_HOST`, `_PORT`, `_USER`, and
`_PASSWORD` for the post-journey SQL assertion. Keep them in the runner environment only. Before the
runner starts, `seed-smoke` must have reset the guarded database, installed the private R2 HLS fixture,
and restarted API/worker. The script uses the existing S5/S6 tests unchanged, verifies provenance and
cardinality directly, issues protected playback, and downloads a signed R2 segment without printing
the session cookie, database password, or signed URL.

For rollback, load both compatible release archives and run:

```bash
./deploy/hostinger/host.sh apply-release /protected/path/release-N-plus-1.env
./deploy/hostinger/host.sh verify
./deploy/hostinger/host.sh apply-release /protected/path/release-N.env
./deploy/hostinger/host.sh verify
```

Application rollback selects application images only and never performs a schema-down migration. It
requires a clean live schema, reads the target backend image's maximum supported schema, and fails
closed before replacing application processes if that image cannot run the retained forward schema.
A compatible selection leaves the schema unchanged and verifies the Entitlement provenance count.
In particular, never run migration `0015_course_access_grant.down.sql` as an application rollback
after real Course Access grants because it clears `source_invitation_id` provenance.

Configure a protected alert webhook, then install the systemd scheduler described below. Prove real
delivery by causing a short controlled readiness failure, invoking `monitor`, confirming delivery at
the external destination, restoring the service, and rerunning `verify`. Record timestamps and
correlation IDs, never webhook credentials.

## 8. Systemd monitoring and backup schedule

The production unit sources and installer live in `deploy/hostinger/systemd/`. Installation renders
the repository checkout and an explicitly supplied non-root operator into the services; the
repository does not invent a VPS username or checkout path. The services execute only the supported
`host.sh monitor` and `host.sh backup` entrypoints. `host.sh` reads the mode-0400/0600
`/var/lib/gradex/runtime.env`, so webhook and provider credentials do not enter unit files or command
arguments.

Before installation, verify the chosen operator can read the protected state and reach Docker:

```bash
sudo -u <operator> test -r /var/lib/gradex/runtime.env
sudo -u <operator> test -r /var/lib/gradex/backup-restic-password
sudo -u <operator> docker info >/dev/null
```

From the intended repository checkout, render, validate, copy, and reload the units without starting
any job:

```bash
sudo ./deploy/hostinger/systemd/install.sh install \
  --user <operator> \
  --repo "$(pwd)"
```

The installer requires the protected runtime file to be owned by that non-root operator. It validates
the rendered units with `systemd-analyze verify`, installs them under `/etc/systemd/system`, and runs
`daemon-reload`. It deliberately leaves both timers disabled so installation cannot immediately run a
monitor or provider backup. After the application, Docker access, runtime configuration, and external
destinations are ready, first validate and run the backup service itself:

```bash
systemd-analyze verify \
  /etc/systemd/system/gradex-backup.service \
  /etc/systemd/system/gradex-backup.timer
sudo systemctl start gradex-backup.service
systemctl show gradex-backup.service \
  --property=Result --property=ExecMainStatus --property=ActiveState --property=SubState
journalctl -u gradex-backup.service --since '10 minutes ago' --no-pager -o cat \
  | grep --extended-regexp \
    'no errors were found|created and retention applied|successful offsite backup marker updated'
```

Do not use `set -x`, print the protected files, inspect a service's environment, or paste an unfiltered
journal. Before enabling the timer, confirm the journal does not contain credential-variable names or
the restic password. Then enable only the configured scheduler:

```bash
sudo systemctl enable --now gradex-backup.timer
systemctl is-enabled gradex-backup.timer
systemctl is-active gradex-backup.timer
systemctl list-timers --all gradex-backup.timer
```

Enable `gradex-monitor.timer` separately only after its Founder/deployment topology, runtime metrics,
health targets, and alert destination have been configured and proved.

`gradex-monitor.timer` runs at exact five-minute calendar boundaries.
`gradex-backup.timer` runs hourly. Both use `Persistent=true`, so a missed event is run after the VPS
returns. A oneshot service cannot overlap another invocation of the same unit; the backup command's
existing `flock` remains the authoritative guard against overlap with manual or other invocations.
Failures propagate to systemd and journald.

Inspect schedules, results, and bounded logs with:

```bash
systemctl status gradex-monitor.timer gradex-backup.timer
systemctl status gradex-monitor.service gradex-backup.service
journalctl -u gradex-monitor.service -u gradex-backup.service --since today
```

Manual starts execute the real configured operation. Use them deliberately for an operator check:

```bash
sudo systemctl start gradex-monitor.service
sudo systemctl start gradex-backup.service
```

`gradex-monitor.service` is a oneshot: after a successful run, `systemctl status` can show
`inactive (dead)` with `Result=success`. That is healthy completion, not a failed monitor. Inspect the
most recent report in `journalctl -u gradex-monitor.service --since '15 minutes ago' --no-pager`.

Disable future scheduling without stopping Gradex application containers:

```bash
sudo systemctl disable --now gradex-monitor.timer gradex-backup.timer
```

The protected runtime file supplies `GRADEX_ALERT_WEBHOOK_URL`, optional
`GRADEX_ALERT_WEBHOOK_TOKEN`, optional `GRADEX_ALERT_RESEND_API_KEY`, optional
`GRADEX_ALERT_EMAIL_TO`, optional `GRADEX_MONITOR_CA_FILE`, and the configurable
`GRADEX_MONITOR_EMAIL_STALE_SECONDS`, `GRADEX_MONITOR_DISK_WARN_PERCENT`,
`GRADEX_MONITOR_DISK_CRITICAL_PERCENT`, `GRADEX_MONITOR_DISK_MIN_FREE_BYTES`, and optional
`GRADEX_MONITOR_DISK_PATHS`. `host.sh monitor` derives root, health, and readiness URLs from
`PUBLIC_ORIGIN`, resolves the exact Compose worker/PostgreSQL services, derives Docker's data-root when
disk paths are not explicit, and points freshness monitoring at
`/var/lib/gradex/backups/latest.completed-at`.

Founder Beta is a non-canonical Compose topology. Configure its monitor-only source contract in the protected
runtime file with `GRADEX_MONITOR_COMPOSE_PROJECT=gradex-founder-beta`,
`GRADEX_MONITOR_WORKER_CONTAINER=gradex-founder-beta-worker-1`, and
`GRADEX_MONITOR_POSTGRES_CONTAINER=gradex-founder-beta-postgres-1`. When the protected tooling runtime does
not hold `GRADEX_BACKEND_IMAGE`, also set
`GRADEX_MONITOR_API_CONTAINER=gradex-founder-beta-api-1`. This does not change application Compose
configuration. The monitor validates each selected container's project/service labels before querying it;
invalid, missing, stopped, or mismatched containers fail the check.

The monitor requires HTTPS for public probes and configured webhooks. Leave the optional external webhook
configuration blank when no destination exists; health monitoring still runs and reports check failures locally.
Configure an optional custom CA only through `GRADEX_MONITOR_CA_FILE`. Use the repository's disposable
monitor tests to test alert behavior; do not create a staging outage to test a destination. Disk warning begins
at 85% used; critical is 95% used or under 5368709120 free bytes. Email outbox failures cover terminal
delivery states, queued messages due for over 3600 seconds, and expired sending leases.
Resend alert delivery is independent of the application email outbox. Configure its dedicated monitoring key in
`GRADEX_ALERT_RESEND_API_KEY` and its recipient in `GRADEX_ALERT_EMAIL_TO`; it reuses the existing
`EMAIL_FROM_NAME` and `EMAIL_FROM_ADDRESS` sender identity. A failed monitor delivers to every configured
destination. For a safe live proof without a service outage, run `./deploy/hostinger/host.sh monitor-alert-test`;
its email subject identifies the message as a synthetic delivery test.
The hourly schedule uses a two-hour freshness threshold: this allows one delayed or missed run and
detects stale backup state before a third hourly opportunity. It does not prove an RPO. Successful
scheduled backups, real external alert delivery, and an isolated provider restore drill remain
required evidence.

Hourly dumps are backup-based recovery, not WAL archiving or point-in-time recovery. A practical
approximately one-hour backup-based RPO requires every scheduled backup to complete and still needs
production evidence. The older provisional 15-minute PostgreSQL RPO is unsupported without an
implemented and tested WAL/PITR mechanism; PostgreSQL RPO and the provisional four-hour RTO still
require Founder approval under `LG-019`.

## 9. Evidence and operations

`host.sh status` and `host.sh logs [SERVICE]` expose bounded container status/logs. Containers restart
unless stopped, except migration/proof/restore jobs. Persistent state is in named PostgreSQL and Caddy
volumes plus protected `/var/lib/gradex` backup/TLS/configuration files. Record release SHA, image IDs,
public results, R2 proof, backup/restore, rollback, alert delivery, host scan, and Playwright output in
sanitized T047 evidence before checking the task.
