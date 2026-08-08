# S12 Batch A evidence — deployable production artifacts

Date: 2026-08-08

Planning base: `322538c`

Authoritative S12 base: `dde093bc9f8e75b89cc96667c73a30fea5f8baee`

This record covers artifact and process proof only. It does not claim a cloud staging deployment,
backup/restore, rollback, alert delivery, or the deployed MVP smoke.

## Artifacts

- `gradex-backend:s12-batch-a` built successfully from `backend/Dockerfile` with Go 1.26.5,
  API/worker/migrate binaries, migration SQL, CA roots, FFmpeg, and an unprivileged `gradex` user.
  Local image ID: `sha256:817da3f2ee54099fe7d5e223f4735b76b16e1b77b84460183554675ad53510d0`.
- `gradex-frontend:s12-batch-a` built successfully from `frontend/Dockerfile` as a Next.js standalone
  server running as the unprivileged `node` user. Local image ID:
  `sha256:2139a46fdec6d97302715fb74f8b66325fa91b8a5ab58e6500abd56131818468`.
- The host Docker package is Snap-confined from reading `/var/tmp`; the same repository contexts were
  streamed with `tar ... | docker build ... -` for this local proof.
- The frontend image returned HTTP 200 from `/` with an explicit server-only `GRADEX_API_ORIGIN`.
  Starting it without that setting exited 2 before Next.js startup.
- A static-client scan found none of `DATABASE_URL`, `S3_SECRET_KEY`, `PLAYBACK_TOKEN_SECRET`, or
  `SESSION_CSRF_KEY` in `.next/static`.

## Backend process proof

A fresh `s12proof` PostgreSQL/Redis/MinIO dependency stack was used. The image migration command
reported:

```text
migrate up: version=15 dirty=false (supported; this build supports 2..15)
migrate version: version=15 dirty=false (supported; this build supports 2..15)
```

The production-mode API then reported:

```json
{"status":"ok"}
{"status":"ok","checks":{"postgres":"ok","redis":"ok","schema":"ok"}}
```

Production startup emitted no Gin debug route dump. `docker stop --timeout 15 s12-batch-a-api`
recorded `received terminated, draining` followed by `gradex API stopped`.

The worker passed PostgreSQL, private-bucket, and Redis preflights, then started asynq. A SIGTERM drill
recorded dispatcher cancellation, asynq graceful shutdown, all workers finished, and
`gradex media worker stopped`. Negative process checks also passed:

- missing required API configuration exited 1 and listed missing keys without values;
- `SERVICE_ROLE=api` refused the worker command and exited 1;
- an unreachable Redis address caused worker preflight to exit 1;
- a missing private bucket caused worker preflight to exit 1.

## Validation

- `gofmt` on changed Go files: pass.
- `go build ./...`: pass.
- three `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"` production binaries: pass.
- `go vet ./...`: pass.
- `go test ./...`: pass.
- `go test -race ./...`: pass.
- `npm ci`: pass; 435 packages installed from the lockfile.
- `npm run typecheck`: pass.
- `npm test`: pass, 161/161.
- `npm run lint`: pass, no warnings or errors.
- `npm run build`: pass, Next.js 14.2.35 production build.
- backend and frontend container builds: pass.
- `./scripts/docs-guard.sh`: pass.
- `./scripts/expose-guard.sh`: pass.
- `git diff --check`: pass.
- Clean-code review: missing runtime migration SQL and production Gin release mode were found and fixed;
  final process proof passed.
- Test review: the focused frontend environment test is behavior-based, restores process state, and
  adds no duplicate fixture layer.
- Documentation review: endpoint, failure-mode, process, and rollback claims were checked against the
  implementation; an overbroad dependency-startup claim was corrected.

## Open security finding

`npm audit --omit=dev` reports three High production dependency findings affecting the pinned Next.js
14.2.35 tree (`next`, `postcss`, and `nanoid`). npm's offered complete fix upgrades Next.js to 16.3.0,
which is a breaking major upgrade. This is not hidden by the successful build: cloud exposure and S12
technical closure remain blocked until a tested dependency upgrade or an independently approved,
evidence-backed mitigation resolves the production advisories. Provider-neutral Batch B work may
continue on the loopback-only disposable environment.
