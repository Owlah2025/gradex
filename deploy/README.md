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
./deploy/scripts/environment.sh status
```

The verified local origin is `https://gradex.localhost:18443`; Caddy uses an environment-local CA.
The `verify` command extracts that CA certificate into ignored state and uses it to validate the
frontend plus `/healthz` and `/readyz`. `data-plane` verifies schema 15, Redis, an application-credential
object write/read/delete, and the bucket's private policy. Use `logs [service]`, `stop`, or `reset` for
the corresponding lifecycle operation. `stop` preserves volumes; `reset` removes them.

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

## Configuration

- `env/production.env.example` lists the production contract.
- `env/production-like.env.example` lists non-secret disposable settings.
- `backend/.env.example` remains the authoritative exhaustive backend key reference.
- `GRADEX_API_ORIGIN` is server-only and required by protected server-rendered requests in production.
  It must identify the API origin reachable from the frontend server.
- Browser API calls use the public same-origin edge; no `NEXT_PUBLIC_*` secret is required.

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
production CORS, fake authentication, and the example playback-secret prefix. The frontend refuses a
protected server request in production when `GRADEX_API_ORIGIN` is absent. The API exits on PostgreSQL
startup failure and reports Redis failure through `/readyz`; the worker exits on PostgreSQL, Redis, or
storage preflight failure.

## Rollback boundary

Select earlier immutable frontend and backend application images while retaining the forward-compatible
database schema. Never use migration `0015_course_access_grant.down.sql` as the normal rollback after real
S6 grants because it clears `source_invitation_id` provenance.
