# Gradex deployment contract

S12 produces one shared backend image with three commands and one standalone frontend image. Images
contain no environment file or secret value.

## Build artifacts

From the repository root:

```bash
docker build --tag gradex-backend:local ./backend
docker build --tag gradex-frontend:local ./frontend
```

The backend image commands are:

```text
gradex-api
gradex-worker
gradex-migrate up
gradex-migrate version
```

The frontend image starts `node server.js`. Set `PORT` for each runtime. The API binds `PORT`; the
frontend standalone server uses its own `PORT` and `HOSTNAME=0.0.0.0`.

## Disposable production-like environment

The local S12 topology keeps PostgreSQL, Redis, MinIO, API, worker, and frontend ports private and
publishes only a Caddy TLS edge on loopback. It generates secrets into the ignored, mode-0700
`deploy/.state/` directory and never prints their values.

```bash
./deploy/scripts/environment.sh build
./deploy/scripts/environment.sh reset  # removes only the gradex-s12 project and its disposable volumes
./deploy/scripts/environment.sh up
./deploy/scripts/environment.sh verify
./deploy/scripts/environment.sh data-plane
./deploy/scripts/environment.sh redis-security
./deploy/scripts/environment.sh status
./deploy/scripts/verify-edge-security.sh
./deploy/scripts/verify-worker-media.sh
./deploy/scripts/verify-observability.sh
./deploy/scripts/verify-application-rollback.sh
./deploy/scripts/verify-staging-smoke.sh
```

The verified local origin is `https://gradex.localhost:18443`; Caddy uses an environment-local CA.
The `verify` command extracts that CA certificate into ignored state and uses it to validate the
frontend plus `/healthz` and `/readyz`. `data-plane` verifies schema 15, Redis, an application-credential
object write/read/delete, and the bucket's private policy. Use `logs [service]`, `stop`, or `reset` for
the corresponding lifecycle operation. `stop` preserves volumes; `reset` removes them.
`redis-security` verifies the generated certificate chain, proves the Redis port refuses plaintext
and unauthenticated TLS, then proves authenticated verified TLS without printing the credential.

`verify-edge-security.sh` verifies the HTTP redirect and TLS hostname, probes through the HTTPS edge,
checks the host-only Secure/HttpOnly/SameSite=Strict anonymous cookie, exercises trusted and hostile
Origin/CSRF requests, refuses cross-origin preflight, verifies trusted request-ID replacement, confirms
fake authentication is off, and scans service logs plus frontend static assets for the generated
runtime secrets. Temporary response bodies, the cookie jar, and the local CA remain under ignored
mode-0700 `deploy/.state/` and are removed when the check exits.

`verify-worker-media.sh` resets only a safety-gated `gradex_playwright_e2e_*` proof database, then
uses separate proof API/worker/Redis processes with the production configuration contract. It proves
PostgreSQL outbox retention during Redis failure, worker restart/recovery and idempotency, a real
FFmpeg HLS transcode into private storage, same-origin protected manifest rewriting, direct signed
segment delivery, and refusal for an unrelated Student. The default backend image excludes the
E2E seed binary; that binary exists only in the explicitly selected `proof` image target.

`verify-observability.sh` induces a safe Redis readiness failure, verifies a structured redacted
worker event and correlated API request log, delivers an authenticated alert to a disposable sink,
then restores Redis and proves healthy monitoring emits no second alert. The provider-neutral rule
and webhook contract live in `deploy/monitoring/`; a real external alert destination remains a
separate staging/production configuration action.

For the isolated database recovery drill after the environment is up:

```bash
./deploy/scripts/database-recovery.sh seed
./deploy/scripts/database-recovery.sh backup
./deploy/scripts/database-recovery.sh restore
./deploy/scripts/verify-restored-database.sh
```

The restore command checksum-verifies the custom-format backup, removes only the prior disposable
restore target, provisions a fresh `restore-postgres` container/database, and restores without
`--clean`. The verifier asserts schema 15 plus fixed identity, invitation-provenance, Entitlement, and
Enrollment records before starting `api-restore` against the restored database.
Each successful backup also writes an ignored completion timestamp used by the freshness monitor.

`application-rollback.sh apply RELEASE_MANIFEST` changes only the API, worker, and frontend image
selection. A release manifest contains an immutable release ID plus backend/frontend image references.
The command verifies clean forward schema 15, recreates only those application processes, and runs the
TLS-edge probes. It intentionally has no schema-down operation; `migrate`, `downgrade`, `schema`, and
`database` commands are refused. `verify-application-rollback.sh` builds two real compatible revisions
and proves N → N+1 → N while comparing Entitlement invitation-provenance counts.

`verify-staging-smoke.sh` resets only `gradex_playwright_e2e_s12smoke01`, exposes PostgreSQL through
a pinned disposable tunnel bound to loopback for the safety-gated seed/query tool, and deploys the
production API/worker/frontend against that database. It points the existing S5 login smoke and S6
Course Access Grant Playwright journey at the HTTPS edge, then independently verifies exact grant,
Enrollment, Progress, and protected-media results. The tunnel is removed on exit; the production-like
services remain available at the verified local origin for inspection.

## Configuration

- `env/production.env.example` lists the production contract.
- `env/production-like.env.example` lists non-secret disposable settings.
- `backend/.env.example` remains the authoritative exhaustive backend key reference.
- `REDIS_ADDR` is a credential-free `host:port`. Development may omit authentication and TLS.
  Staging and production require `REDIS_PASSWORD` plus `REDIS_TLS_ENABLED=true`; `REDIS_USERNAME`
  enables ACL authentication. `REDIS_TLS_CA_CERT_FILE` optionally mounts a private CA and
  `REDIS_TLS_SERVER_NAME` overrides certificate name derivation. Certificate verification cannot be
  disabled, and API, readiness, rate-limiter, worker, and asynq clients share this contract.
- `GRADEX_API_ORIGIN` is server-only and required by protected server-rendered requests in production.
  It must identify the API origin reachable from the frontend server.
- Browser API calls use the public same-origin edge; no `NEXT_PUBLIC_*` secret is required.
- The public browser surface does not grant cross-origin CORS access. Browser API calls are same-origin;
  the edge check proves a foreign preflight receives no allow-origin or allow-credentials header.
- `S3_ENDPOINT` is also the origin embedded into signed segment URLs. In staging/production it must
  be browser-reachable over HTTPS, and the private bucket must allow credential-free CORS `GET`/`HEAD`
  from only `PUBLIC_ORIGIN` for presigned requests. CORS does not make objects public; every object
  request still requires a valid signature.

Every secret entry in the committed examples is blank. Supply real values through the deployment
platform's managed secret/environment facility. Do not pass secrets as image build arguments, bake them
into layers, or print a populated environment.

## Process lifecycle

- Run `gradex-migrate up` as a controlled one-off release job before starting API or worker replicas.
- The API exposes `/healthz` for liveness and `/readyz` for PostgreSQL/schema/Redis readiness. It marks
  itself unready before draining active requests on `SIGTERM` or `SIGINT`.
- The worker verifies PostgreSQL, the configured storage bucket, and Redis before consuming work. It
  stops the dispatcher, drains active asynq jobs, and returns unfinished jobs to Redis on termination.
- The worker has no public ingress. Its process exit and lifecycle/job logs are the liveness signal.

## Failure behavior

Production configuration rejects missing required secrets, non-HTTPS public/CORS origins, wildcard
production CORS, unauthenticated/plaintext Redis, fake authentication, and the example playback-secret
prefix. The frontend refuses a
protected server request in production when `GRADEX_API_ORIGIN` is absent. The API exits on PostgreSQL
startup failure and reports Redis failure through `/readyz`; the worker exits on PostgreSQL, Redis, or
storage preflight failure.

## Rollback boundary

Select earlier immutable frontend and backend application images while retaining the forward-compatible
database schema. Never use migration `0015_course_access_grant.down.sql` as the normal rollback after real
S6 grants because it clears `source_invitation_id` provenance.

Enabling mandatory Redis TLS/authentication is a deployment compatibility boundary: do not select a
pre-T046 backend image after the Redis service has been hardened. Establish the first T046-capable
release as the new known-good rollback floor, and exercise subsequent N → N+1 → N application drills
only between artifacts that implement this Redis contract.
