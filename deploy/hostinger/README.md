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

`prepare` validates the release labels and configuration, creates a private 90-day Redis CA/server
certificate with the `redis` DNS identity, and renders the narrow R2 CORS policy. Apply that policy
through the R2 dashboard or API without changing public bucket access.

Before deploying application traffic, run the explicit provider contract test from a protected shell:

```bash
cd backend
go test -tags=provider -run '^TestCloudflareR2PreservesPrivateVersionBoundMediaContract$' ./internal/storage
```

Export only `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, and `PUBLIC_ORIGIN` into that
shell. The test uploads disposable objects, proves private/CORS behavior and exact version-bound reads,
then removes its prefix. A missing `x-amz-version-id` or silent current-object substitution is a hard
S4 compatibility failure; do not work around it by weakening media provenance.

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

## 5. Deploy and verify

```bash
./deploy/hostinger/host.sh up
./deploy/hostinger/host.sh verify
./deploy/hostinger/verify-public.sh "https://staging.example.com"
```

Replace the example hostname. `verify` asserts schema `15|false`, worker lifecycle, plaintext Redis
refusal, unauthenticated TLS refusal, authenticated verified-TLS Redis, and public health/readiness.
`verify-public.sh` independently checks public DNS, HTTP-to-HTTPS redirect, certificate validity,
Cloudflare presence, conservative cache behavior, frontend, `/healthz`, and `/readyz`.

## 6. Provider backup and isolated restore

After the controlled public S5/S6 smoke has created provenance-bearing records:

```bash
./deploy/hostinger/host.sh backup
./deploy/hostinger/host.sh restore
./deploy/hostinger/host.sh verify-restore
```

The source database is never destroyed. The restore command verifies the backup checksum, provisions
a fresh separate volume/database, restores without `--clean`, and starts a separate API against it.
Verification requires schema 15 plus Account, Course, invitation, ACTIVE Entitlement with
`source_invitation_id`, and Enrollment records.

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

This selects application images only. It keeps schema 15 and verifies Entitlement provenance count;
never run migration 0015 down as an application rollback.

Configure a protected alert webhook, then schedule `host.sh monitor` at least every five minutes and
`host.sh backup` on the selected backup schedule. Prove real delivery by causing a short controlled
readiness failure, invoking `monitor`, confirming delivery at the external destination, restoring the
service, and rerunning `verify`. Record timestamps and correlation IDs, never webhook credentials.

## 8. Evidence and operations

`host.sh status` and `host.sh logs [SERVICE]` expose bounded container status/logs. Containers restart
unless stopped, except migration/proof/restore jobs. Persistent state is in named PostgreSQL and Caddy
volumes plus protected `/var/lib/gradex` backup/TLS/configuration files. Record release SHA, image IDs,
public results, R2 proof, backup/restore, rollback, alert delivery, host scan, and Playwright output in
sanitized T047 evidence before checking the task.
