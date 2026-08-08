# Quickstart: S12 Production-Like Validation

This guide is the planned execution order. Commands are marked complete in `tasks.md` only after their
implementation exists and the command succeeds.

## Prerequisites

- Docker with Compose support
- Go and Node versions used by CI for host validation
- `curl`, `openssl`, PostgreSQL client tools, and sufficient local disk for images/media
- Clean worktree at the exact revision being evidenced

## 1. Build gate

```bash
cd backend
gofmt -w ./cmd/worker
go build ./...
go vet ./...
go test ./...

cd ../frontend
npm ci
npm run typecheck
npm test
npm run lint
npm run build
```

Build the frontend and shared backend container artifacts using the deployment commands documented in
`deploy/README.md` once Batch A lands. Record their immutable identifiers.

## 2. Production-like topology

Copy only the non-secret example settings to an ignored local environment file, inject fresh independent
secret values, start the deployment, and run the controlled migration job. Do not commit the populated
file or print it in evidence.

Expected outcomes:

- frontend reachable through the disposable edge;
- `GET /healthz` returns `200`;
- `GET /readyz` returns `200` with PostgreSQL, schema, and Redis checks healthy;
- schema version equals `15`;
- worker remains running and consumes a safe representative job;
- storage administrative endpoint is not exposed through the public edge.

## 3. Restore drill

Use the scripts delivered by Batch C:

1. insert/identify known identity and access records in the disposable source;
2. create a custom-format backup and checksum;
3. create a fresh separate restore database;
4. restore without cleaning or altering the source;
5. verify schema version and selected records;
6. start an API instance whose database URL targets only the restored database;
7. verify `/healthz`, `/readyz`, and representative reads.

## 4. TLS and security

Through the HTTPS edge, verify redirect behavior, secure host-only SameSite Strict cookies, trusted
origin/CORS/CSRF handling, forwarding-header trust, and absence of injected sentinel secrets from logs
and frontend output.

## 5. Redis, worker, and media

Commit durable work, prove enqueue and consumption, restart Redis/worker, reconcile pending work from
PostgreSQL, and exercise the existing private upload/processing/protected-playback path. Anonymous object
access must fail.

## 6. Rollback and smoke

Deploy release N, deploy compatible N+1, then select N's application artifacts without changing schema.
Verify both probes before running the existing S5/S6 production-mode smoke journey against the deployed
origin.

## Evidence rule

Store only redacted command output and identifiers in the S12 evidence location defined by the task that
runs each drill. Never claim live cloud, public TLS, external alert delivery, restore, or rollback evidence
from this guide alone.

For the selected Hostinger/Cloudflare provider execution, follow `deploy/hostinger/README.md`. The
provider path adds immutable image transfer, a non-mutating host baseline audit, TLS-only authenticated
Redis on the private network, a credential-gated R2 compatibility proof, public edge verification,
provider-hosted isolated restore, and T046-compatible application rollback.
