# Feature Specification: S12 — Staging + Production Infrastructure

**Feature Branch**: `s12-infrastructure-20260808`

**Created**: 2026-08-08

**Status**: Implementation authorized by Product Owner decisions PO-1 through PO-7

**Input**: Make the existing Gradex MVP deployable, recoverable, observable, and verifiable in a production-like staging environment without adding product functionality.

## Scope Boundaries

S12 owns deployment artifacts, environment contracts, production-like staging, operational security,
health, worker lifecycle, monitoring capability, backup/restore, application rollback, and deployment
smoke evidence. It does not change the closed product behavior in S2, S4, S5, or S6.

Checkout, payments, KNET, Apple Pay, coupons, payment callbacks, refunds, invoices, BNPL, payouts,
Kubernetes, Kafka, service mesh, sharding, speculative multi-region work, and unrelated UI work are
outside S12.

## User Scenarios & Testing

### User Story 1 — Operator deploys immutable application processes (Priority: P1)

An operator can build and run independently deployable frontend, API, and worker artifacts from one
exact source revision with production configuration validation.

**Why this priority**: Every later staging, recovery, and rollback proof depends on repeatable artifacts.

**Independent Test**: Build all three artifacts from a clean checkout, start each with production-like
configuration, and verify startup success or a clear non-zero failure for invalid configuration.

**Acceptance Scenarios**:

1. **Given** a clean checkout, **when** the production build runs, **then** immutable frontend, API,
   and worker artifacts are produced without embedding secrets.
2. **Given** valid production configuration, **when** the API starts, **then** it binds the configured
   port, answers `/healthz` and `/readyz`, and drains on termination.
3. **Given** valid production configuration, **when** the worker starts and receives termination,
   **then** it stops accepting work and drains or safely retries in-flight work.
4. **Given** a missing required production value, **when** a process starts, **then** it exits visibly
   rather than using a development fallback.

---

### User Story 2 — Operator runs a production-like isolated environment (Priority: P1)

An operator can start the existing MVP with separate frontend, API, worker, PostgreSQL, Redis, and
private object-storage processes using the same configuration boundaries intended for staging.

**Why this priority**: It proves the topology before provider credentials are available.

**Independent Test**: Start the disposable environment from zero, migrate it, and verify all processes,
dependency-aware readiness, one durable queued job, and private storage.

**Acceptance Scenarios**:

1. **Given** no prior environment, **when** the deployment is started, **then** PostgreSQL and Redis
   become reachable, storage remains private, migrations reach the current maximum version, and the
   application processes become healthy.
2. **Given** Redis is restarted, **when** the environment recovers, **then** PostgreSQL-held work can
   be republished and Redis is never treated as durable business authority.

---

### User Story 3 — Operator restores authoritative data safely (Priority: P1)

An operator can back up an existing database and restore it into a fresh separate target without
modifying the source database.

**Why this priority**: A backup is not useful until a restore has succeeded.

**Independent Test**: Seed known schema and identity/access records, back them up, restore into a newly
created database, start Gradex against the restored target, and verify records and readiness.

**Acceptance Scenarios**:

1. **Given** known records in a migrated source, **when** a backup is restored to a fresh database,
   **then** schema version, identity, invitation, Enrollment, Entitlement, and representative reads match.
2. **Given** real S6 grants, **when** an application release is rolled back, **then** schema version 15
   remains and `source_invitation_id` provenance is preserved.

---

### User Story 4 — User reaches Gradex through a secure edge (Priority: P1)

A browser reaches Gradex over HTTPS through a documented reverse-proxy boundary while authentication,
origin enforcement, and protected-media authorization retain their existing guarantees.

**Why this priority**: Production session and media security depend on the real TLS/origin boundary.

**Independent Test**: Exercise production-mode sessions through TLS termination and verify secure
cookies, strict SameSite behavior, CORS, CSRF/origin refusal, trusted forwarding, and secret absence.

**Acceptance Scenarios**:

1. **Given** the trusted HTTPS origin, **when** a user authenticates, **then** cookies are Secure,
   HttpOnly, host-only, and SameSite Strict.
2. **Given** an untrusted origin or forwarding header from an untrusted peer, **when** a protected
   request is made, **then** the request is refused or the forwarding header is ignored.

---

### User Story 5 — Operator detects and reconstructs failures (Priority: P2)

An operator can tell whether the API is healthy and ready, whether the worker and dependencies are
available, and why an unexpected request or background job failed without exposing secrets.

**Why this priority**: A solo operator needs actionable signals rather than a large monitoring platform.

**Independent Test**: Induce a safe dependency/job failure, correlate structured events, and deliver an
alert to a disposable sink; external provider delivery remains separately evidenced.

**Acceptance Scenarios**:

1. **Given** a request or job failure, **when** logs are inspected, **then** an operator can correlate
   the failure using stable identifiers without seeing credentials, cookies, tokens, or signed URLs.
2. **Given** a health or background-work threshold breach, **when** the monitor runs, **then** it emits
   an alert through the configured sink and records delivery success or failure.

---

### User Story 6 — Operator rolls back and verifies the MVP (Priority: P2)

An operator can return from release N+1 to known-good application release N without destructive schema
downgrade, then run the current MVP smoke journey against the deployed environment.

**Why this priority**: Rollback and deployed acceptance are release controls, not runbook prose.

**Independent Test**: Deploy two compatible application revisions, roll back only application
artifacts, verify probes, then run the existing access/protected-learning smoke harness.

**Acceptance Scenarios**:

1. **Given** releases N and N+1 share a forward-compatible schema, **when** N+1 is rolled back, **then**
   N answers health/readiness and the schema is not downgraded.
2. **Given** a deployed production-like environment, **when** the MVP smoke runs, **then** acceptance
   alone grants no access, Admin Approval creates exactly one Entitlement and Enrollment, protected
   learning works, progress persists, and an unrelated Student is denied.

### Edge Cases

- A required secret is absent, is an example placeholder, or accidentally appears in build output.
- PostgreSQL is reachable but the schema is below or above the supported range.
- Redis becomes unavailable before enqueue, after durable intent commit, or during worker consumption.
- Worker termination occurs while a transcode or outbox dispatch is active.
- The storage endpoint is reachable but the bucket is absent, public, or credentials are invalid.
- A backup is incomplete, restored into a non-empty target, or reports a different schema version.
- Release N cannot run against the forward schema used by N+1.
- TLS terminates at an untrusted proxy or spoofed forwarded headers arrive directly from a client.
- Alert delivery fails or no external alert destination is configured.

## Requirements

### Functional Requirements

- **FR-001**: S12 MUST produce reproducible, separately runnable frontend, API, and worker artifacts from one revision.
- **FR-002**: The API artifact MUST honor the configured port, expose `/healthz` and `/readyz`, validate production configuration at startup, and drain on termination.
- **FR-003**: The worker artifact MUST validate PostgreSQL, Redis, storage, and media dependencies, expose observable startup failure, and terminate with safe drain/retry behavior.
- **FR-004**: Production frontend server-side API origin configuration MUST fail closed rather than defaulting to loopback; no server secret may enter browser output.
- **FR-005**: Production configuration MUST cover environment, origins, CORS, trusted proxies, PostgreSQL, Redis, sessions, playback signing, storage, media tools, logging, and process role without committed real secrets.
- **FR-006**: A disposable production-like topology MUST run the frontend, API, worker, PostgreSQL, Redis, and private S3-compatible storage with isolated state.
- **FR-007**: A controlled one-off migration MUST provision an empty database from version zero to the repository maximum and prove API/worker compatibility.
- **FR-008**: API readiness MUST include PostgreSQL, schema compatibility, and role-required Redis; liveness MUST not depend on external services.
- **FR-009**: PostgreSQL MUST remain authoritative for business and queued-work intent; Redis loss MUST be recoverable without loss of authoritative work.
- **FR-010**: Object storage MUST remain private, version-aware where supported, and compatible with direct signed upload/protected delivery without proxying media bytes through the API.
- **FR-011**: The deployment contract MUST define HTTPS termination, HTTP-to-HTTPS behavior, trusted proxy boundaries, secure cookies, SameSite, CORS, CSRF/origin enforcement, and forwarded-header handling.
- **FR-012**: API and worker logs MUST be structured, correlation-capable, and redact credentials, cookies, action tokens, playback tokens, signed URLs, and unnecessary PII.
- **FR-013**: Monitoring MUST cover API health/readiness, worker/process failure, PostgreSQL, Redis, failed media work, unexpected server errors, and backup status.
- **FR-014**: Alert capability MUST be demonstrable through a disposable sink and distinguish configured capability from externally delivered production alerts.
- **FR-015**: Database backup MUST be reproducible and restoration MUST target a fresh separate database without altering the source.
- **FR-016**: Restore verification MUST prove exact schema version, known records, identity/access-critical records, API startup, `/healthz`, `/readyz`, and representative reads.
- **FR-017**: Application rollback MUST restore known-good frontend/API/worker artifacts without running destructive database down migrations.
- **FR-018**: S12 MUST preserve migration 0015 invitation provenance; rollback after real grants MUST NOT clear `source_invitation_id`.
- **FR-019**: The deployed smoke harness MUST reuse existing S5/S6 coverage for registration/login, invitation acceptance with zero access, Admin Approval, one Entitlement, Enrollment, protected playback, progress, and unrelated-Student denial.
- **FR-020**: Runbooks MUST record exact commands, versions, evidence locations, failure outcomes, and external actions still required; written configuration alone is not completion evidence.
- **FR-021**: Production artifacts MUST exclude checkout, payment, callback, coupon, refund, invoice, BNPL, payout, fake-auth, and test-seed production paths.

### Key Entities

- **Application Release**: One immutable revision and its frontend, API, and worker artifact identities.
- **Deployment Environment**: Isolated configuration and service endpoints for staging or production.
- **Backup Artifact**: Timestamped database backup tied to a source database and schema version.
- **Restore Drill**: Evidence linking a backup to a fresh target and verified application behavior.
- **Rollback Drill**: Evidence linking release N, N+1, rollback action, schema posture, and probe results.
- **Operational Signal**: Redacted structured event, health result, metric, or alert delivery attempt.

## Success Criteria

### Measurable Outcomes

- **SC-001**: All three application artifacts build from a clean checkout and start with valid production-like configuration.
- **SC-002**: Invalid or incomplete production configuration causes the affected process to exit non-zero before serving work.
- **SC-003**: A new disposable environment reaches repository schema version 15 and returns `200` from `/healthz` and `/readyz`.
- **SC-004**: A representative durable job survives a Redis restart by reconciliation from PostgreSQL and is consumed exactly as allowed by its idempotent contract.
- **SC-005**: Private source and derived media cannot be fetched anonymously; authorized protected playback succeeds through signed access.
- **SC-006**: A real backup restores successfully into a fresh target with schema version 15 and all selected identity/access records intact.
- **SC-007**: Gradex starts against the restored target and passes health, readiness, and representative read verification.
- **SC-008**: A release N → N+1 → N rollback completes without schema downgrade and returns both probes to `200`.
- **SC-009**: Production-mode TLS testing proves secure cookies, intended SameSite behavior, trusted-origin enforcement, CORS, CSRF, and proxy-header handling.
- **SC-010**: A safe induced failure is correlated across operational output without exposing any injected secret.
- **SC-011**: Alert capability delivers to a disposable sink; external production delivery is reported separately and never inferred.
- **SC-012**: The production-like MVP smoke completes the invitation-to-protected-learning journey and denies an unrelated Student.

## Assumptions

- Product Owner decision PO-1 fixes `dde093bc9f8e75b89cc96667c73a30fea5f8baee` as the S12 base.
- Provider-neutral artifacts precede final provider selection; missing cloud credentials defer only the live external operation.
- PostgreSQL schema version 15 is forward-preserved during application rollback.
- TLS may be proven with a disposable local certificate authority before a public domain is available.
- Final production alert delivery remains pending until an external destination is supplied, but local alert capability is required now.
- S12 staging infrastructure precedes and enables S11 deployed E2E/acceptance testing.
## Dependencies

- Closed implementation behavior through the Product Owner-authorized S12 base.
- Existing PostgreSQL migrations, Redis/asynq queue, S3-compatible storage, and S5/S6 E2E harnesses.
- Docker-compatible local/disposable runtime for provider-neutral topology proof.
- Cloud accounts, DNS, public certificates, and final alert credentials only for live external deployment evidence.
