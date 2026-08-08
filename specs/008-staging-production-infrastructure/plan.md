# Implementation Plan: S12 — Staging + Production Infrastructure

**Branch**: `s12-infrastructure-20260808` | **Date**: 2026-08-08 | **Spec**: [spec.md](spec.md)

**Input**: Product Owner decisions PO-1 through PO-7 and the S12 feature specification.

## Summary

Build three immutable application artifacts, prove them in a provider-neutral production-like Compose
topology, then add isolated database recovery, TLS/proxy verification, operational signals, application
rollback, and a deployed smoke harness. Preserve the approved split managed-PaaS architecture and keep
PostgreSQL authoritative, Redis disposable, storage private, and schema 15 forward-compatible.

## Technical Context

**Language/Version**: Go 1.26.5 from `backend/go.mod`; Node.js 22 from CI; Next.js 14.2.35 and TypeScript from the frontend lockfile

**Primary Dependencies**: Gin, pgx, go-redis, asynq, AWS S3 SDK, Next.js, PostgreSQL 16, Redis 7, S3-compatible storage, FFmpeg

**Storage**: PostgreSQL authoritative data, Redis queue/cache transport, private S3-compatible object storage

**Testing**: Go tests/vet/race/integration tags, Node test/typecheck/lint, Next production build, Playwright, shell deployment drills

**Target Platform**: OCI-compatible Linux containers behind an HTTPS-terminating reverse proxy

**Project Type**: Monorepo web application with separately deployed frontend, API, and worker

**Performance Goals**: Preserve PRD targets; S12 adds no new load target beyond the approved provisional architecture envelope

**Constraints**: One operator, August 15 target, no provider lock-in, no real secrets in source/images/logs, no destructive schema rollback, no media proxying through API

**Scale/Scope**: One production-like staging environment and one production deployment contract; single region; three application processes

## Constitution Check

- **I — Source authority**: PASS. PO-1 through PO-7 explicitly resolve the prior S12 stop, S11 dependency, health paths, restore isolation, and rollback model.
- **II — Deny by default**: PASS. Production configuration fails closed and S12 does not change authorization.
- **III — Traceability**: PASS. Tasks cite FR/SC identifiers and existing BR-163 where Redis recovery is involved.
- **IV — Access-grant correctness**: PASS. S12 reuses S6 behavior and never creates a second grant path.
- **V — Risk-proportionate testing**: PASS. Real infrastructure, restore, rollback, proxy, and deployed E2E proofs are required.
- **VI — Modular monolith/simplicity**: PASS. Three approved deployables; no new domain service or orchestration platform.
- **VII — Data integrity**: PASS. Migrations remain versioned; restore uses a fresh target; schema 15 is not downgraded.
- **VIII — Quality gate**: PASS. CI-equivalent validation and exposure guards remain required.
- **IX — Operational discipline**: PASS. Structured failure evidence, safe retries, backup/restore, and rollback are explicit.
- **X/XI — Experience/docs**: PASS. Existing UI behavior is reused and operational claims require evidence.

## Architecture and Batch Order

1. **Batch A — deployable artifacts**: shared backend image containing API, worker, and migrate binaries;
   separate Next.js standalone image; worker signal-aware drain; production origin validation; documented
   environment contract.
2. **Batch B — production-like topology**: Compose frontend/API/worker/PostgreSQL/Redis/MinIO and a
   disposable HTTPS edge, with isolated volumes and controlled migration job.
3. **Batch C — migration and restore**: zero-to-15 provisioning, known records, custom-format backup,
   fresh-target restore, schema/data checks, restored-app probes.
4. **Batch D — HTTPS/proxy security**: TLS, redirects, secure cookies, origin/CORS/CSRF, trusted
   forwarding, bundle/log secret scans.
5. **Batch E — queue/storage/media**: durable outbox enqueue, worker consume/restart, Redis recovery,
   private source/derived objects, signed protected delivery.
6. **Batch F — observability**: structured worker logs, health/monitor contract, failure injection,
   provider-neutral alert sink.
7. **Batch G — application rollback**: immutable N/N+1 deployment and application-only rollback.
8. **Batch H — deployed smoke**: point existing S5/S6 production-mode E2E at the environment.

Batch A blocks B. B blocks C–F. C and D block G. B–G block H. Missing cloud credentials block only
live cloud execution, never provider-neutral implementation or disposable proof.

## Process and Security Boundaries

- One backend image reduces build duplication; API and worker use distinct commands and lifecycle.
- Migrations run once as a release job before API/worker rollout.
- The frontend has no production loopback fallback. Browser API traffic uses the same-origin edge;
  server-side frontend calls use an explicit internal/public API origin.
- The API is the only public backend process. Worker, PostgreSQL, Redis, and storage administration
  have no public ingress in the deployment contract.
- TLS terminates at the trusted edge. The API trusts forwarding headers only from configured CIDRs.
- Environment/platform injection supplies secrets at runtime; images and committed examples contain names only.
- Liveness reports process state. Readiness reports dependencies without exposing diagnostic details.
- Redis transports work; PostgreSQL outbox/processing state reconstructs work after Redis loss.
- Backup reads the source. Restore refuses a non-empty target and never uses the active source target.
- Rollback selects earlier application artifacts while retaining the forward-compatible schema.

## Project Structure

### Documentation

```text
specs/008-staging-production-infrastructure/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/deployment-contract.md
├── checklists/requirements.md
├── checklists/operations.md
└── tasks.md
```

### Implementation

```text
backend/
├── Dockerfile
├── .dockerignore
└── cmd/worker/main.go
frontend/
├── Dockerfile
├── .dockerignore
├── next.config.mjs
└── src/lib/api/learning-server-request.ts
deploy/
├── compose/
│   ├── compose.production-like.yml
│   └── Caddyfile
├── env/production-like.env.example
├── scripts/
└── README.md
scripts/
└── guards/
```

**Structure Decision**: Add deployment-only files at repository root and make the smallest runtime
changes needed for production process behavior. Product modules and schema remain untouched.

## Evidence and Stop Conditions

Each task records a command, exact revision/artifact identity, result, and evidence path. A batch stops
only for a security/authorization regression, data-loss or migration corruption, secret exposure,
broken closed-slice contract, invalid production build, or a credential required for that exact live
external operation after independent work is exhausted. Open launch gates remain non-blocking engineering
inputs and are recorded separately.

## Post-Design Constitution Re-Check

PASS. The design introduces no domain entity, commerce behavior, service decomposition, destructive
schema operation, or alternate authorization path. Operational scripts are narrow, fail closed, and
are exercised against disposable targets before any live-provider use.
