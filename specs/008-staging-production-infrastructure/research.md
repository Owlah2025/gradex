# Phase 0 Research: S12 — Staging + Production Infrastructure

## 1. Feature numbering

**Decision**: Use `specs/008-staging-production-infrastructure/`.

**Rationale**: `.specify/init-options.json` selects sequential numbering and the existing top-level
feature directories occupy `001` through `007`. Slice identifiers and feature-directory numbers are
independent; the directory title retains S12 explicitly.

**Alternatives considered**: Reusing `007` would collide with protected learning. Naming the directory
`012-*` would confuse slice numbering with the repository's sequential feature numbering.

## 2. Artifact topology

**Decision**: Build one backend image containing API, worker, and migration binaries with distinct
commands, plus one standalone frontend image.

**Rationale**: This implements the approved independent process topology with one backend build chain
and permits independent rollout and rollback.

**Alternatives considered**: Separate API/worker images duplicate FFmpeg and build logic. One combined
runtime process prevents independent scaling and safe worker draining.

## 3. Provider-neutral environment proof

**Decision**: Use an isolated Compose topology with production application settings, PostgreSQL,
Redis, S3-compatible storage, and an HTTPS edge as the disposable proof contract.

**Rationale**: OCI images, environment injection, standard service protocols, and edge TLS map to the
approved managed-PaaS architecture without selecting a vendor.

**Alternatives considered**: Kubernetes is explicitly excluded. A development-mode Compose stack does
not prove production validation or secure-origin behavior.

## 4. Frontend API boundary

**Decision**: Require an explicit server-side API origin in production and route browser API traffic
through the same-origin edge.

**Rationale**: This removes implicit loopback while keeping secrets server-only and avoiding browser CORS
fragility. Development retains the existing loopback convenience.

**Alternatives considered**: A public browser environment variable would freeze configuration into the
bundle and widen exposure. Production Next rewrites would couple the app server to proxy behavior already
owned by the edge.

## 5. Secret injection

**Decision**: Retain the existing typed configuration and `SecretResolver` boundary; deployment supplies
secret values as platform-managed process environment variables.

**Rationale**: Managed platforms already inject secret values into process environments. No new provider
SDK or secret-storage abstraction is needed for the current deployment contract.

**Alternatives considered**: Committed env files are unsafe. Provider SDK integration creates lock-in
without an approved provider.

## 6. Database backup and restore

**Decision**: Use a custom-format logical backup, create a fresh target database, restore without
`--clean`, verify schema and selected records, then start the application against the restored target.

**Rationale**: This proves recoverability without mutating or destroying the source database.

**Alternatives considered**: Restoring over the active database is unsafe. Provider snapshots alone do
not provide runnable evidence until a separate restore is performed.

## 7. Rollback

**Decision**: Roll back immutable frontend/API/worker artifacts independently while retaining schema 15.

**Rationale**: Migration 0015's down path clears invitation provenance; application rollback and database
recovery are separate operations.

**Alternatives considered**: Automated schema down migration is provenance-destructive after real grants.

## 8. Observability and alerts

**Decision**: Extend existing structured/request-ID logging to the worker, expose process/dependency
health through the deployment contract, and use a generic alert webhook boundary proven against a
disposable sink.

**Rationale**: It supplies actionable launch-grade signals without building a monitoring platform or
choosing a vendor prematurely.
**Alternatives considered**: A full metrics stack exceeds launch need. Log-only operation cannot
demonstrate that someone is notified.
