# Tasks: S12 — Staging + Production Infrastructure

**Input**: Design documents from `specs/008-staging-production-infrastructure/`

**Tests**: Required. Infrastructure behavior, restore, rollback, and deployed smoke cannot close from documentation alone.

## Standing Rules

- Base is exactly `dde093bc9f8e75b89cc96667c73a30fea5f8baee`; do not rewrite or reopen S6.
- Preserve unrelated/user-owned changes and commit each meaningful batch after validation.
- Never commit real secrets, destroy the source database for a restore drill, or run migration 0015 down after real S6 grants.
- Open launch gates are non-blocking engineering inputs. Stop only for the technical blockers listed in `plan.md`.
- A task is checked only when its named evidence exists.

## Phase 1 — Planning and authority reconciliation

- [x] T001 Record PO-1 through PO-7, S12 scope, success criteria, and exclusions in `specs/008-staging-production-infrastructure/spec.md` (FR-001–FR-021)
- [x] T002 Record provider-neutral topology, recovery, rollback, security, and evidence decisions in `specs/008-staging-production-infrastructure/plan.md`, `research.md`, `data-model.md`, and `contracts/deployment-contract.md`
- [x] T003 Validate requirements quality in `specs/008-staging-production-infrastructure/checklists/requirements.md` and `checklists/operations.md`
- [x] T004 Reconcile only the active S12 base/dependency, health endpoints, isolated restore, and application-rollback authority in `docs/launch/STATUS.md`, `docs/launch/SLICES.md`, `docs/launch/AUGUST_15_EXECUTION_PLAN.md`, `docs/launch/PLAN.md`, and `docs/launch/RUNBOOK.md`
- [x] T005 Run SpecKit consistency analysis across `spec.md`, `plan.md`, and `tasks.md`; correct every Critical/High planning defect before implementation
- [x] T006 Run docs guard, exposure guard, `git diff --check`, verify the exact base, and commit the planning package

## Phase 2 — Batch A / US1: Deployable production artifacts (P1)

**Goal**: Reproducible frontend/API/worker artifacts with fail-closed production startup and safe lifecycle.

**Independent Test**: Clean host and container builds; valid production-like startup; invalid configuration exits non-zero; API probes and API/worker termination behavior pass.

- [x] T007 [P] [US1] Add a multi-stage shared backend image containing API, worker, and migrate binaries plus FFmpeg runtime in `backend/Dockerfile` and `backend/.dockerignore` (FR-001, FR-003)
- [x] T008 [P] [US1] Add a standalone production frontend image in `frontend/Dockerfile`, `frontend/.dockerignore`, and `frontend/next.config.mjs` (FR-001, FR-004)
- [x] T009 [US1] Add PostgreSQL/Redis/storage startup preflight, signal-aware worker cancellation, and safe asynq drain in `backend/cmd/worker/main.go` and `backend/internal/storage/storage.go`; prove startup failure and termination behavior in T013 (FR-003)
- [x] T010 [US1] Remove production loopback fallback and test server/client environment separation in `frontend/src/lib/api/learning-server-request.ts` and `frontend/src/lib/api/learning-server.test.ts` (FR-004)
- [x] T011 [P] [US1] Define non-secret production and production-like environment examples in `deploy/env/production.env.example` and `deploy/env/production-like.env.example` (FR-005)
- [x] T012 [US1] Document exact artifact commands, process commands, configuration contract, and failure behavior in `deploy/README.md` (FR-001–FR-005)
- [x] T013 [US1] Run backend format/build/vet/unit/race checks, frontend install/typecheck/test/lint/build, container builds, config-negative checks, API probe checks, and API/worker termination checks; record exact evidence in `docs/launch/evidence/s12/batch-a.md`
- [x] T014 [US1] Run clean-code, test, docs, and exposure guards plus `git diff --check`; mark T007–T013 complete and commit Batch A

## Phase 3 — Batch B / US2: Production-like disposable deployment (P1)

**Goal**: Run all application processes and dependencies with production settings and isolated state.

**Independent Test**: One command provisions from zero, migrates to 15, reaches frontend/API readiness, runs worker, and keeps storage administration private.

- [x] T015 [P] [US2] Define isolated PostgreSQL, Redis, S3-compatible storage, migration, API, worker, frontend, and edge services in `deploy/compose/compose.production-like.yml` (FR-006–FR-010)
- [x] T016 [P] [US2] Define disposable TLS routing and private-service boundaries in `deploy/compose/Caddyfile` (FR-006, FR-011)
- [x] T017 [US2] Add fail-closed setup/start/status/stop commands in `deploy/scripts/environment.sh` without printing secret values (FR-006, FR-007)
- [x] T018 [US2] Provision from empty volumes, migrate zero-to-15, start all processes, and record exact health/readiness/schema/process evidence in `docs/launch/evidence/s12/batch-b.md` (SC-003)
- [x] T019 [US2] Prove one representative database operation, Redis connectivity, worker startup, and private storage access from the application network (FR-006–FR-010)
- [x] T020 [US2] Run guards and `git diff --check`; mark T015–T019 complete and commit Batch B

## Phase 4 — Batch C / US3: Migration, backup, and isolated restore (P1)

**Goal**: Demonstrate real recovery from a backup into a fresh target database.

**Independent Test**: Known records survive backup/restore and Gradex becomes ready against only the restored target.

- [x] T021 [US3] Add source-safe backup and fresh-target restore commands with source/target identity refusal in `deploy/scripts/database-recovery.sh` (FR-015, FR-018)
- [x] T022 [US3] Add schema and identity/access record assertions in `deploy/scripts/verify-restored-database.sh` (FR-016)
- [x] T023 [US3] Run zero-to-15 migration tests and upgrade-path policy tests against disposable PostgreSQL (FR-007)
- [x] T024 [US3] Create known records, create/checksum a real backup, restore into a fresh database, verify schema/data, and start API against the restored database; record evidence in `docs/launch/evidence/s12/restore-drill.md` (SC-006, SC-007)
- [x] T025 [US3] Update `docs/launch/RUNBOOK.md` with only commands proven by T024 and commit Batch C

## Phase 5 — Batch D / US4: HTTPS, proxy, cookie, and origin security (P1)

**Goal**: Prove the production browser/session boundary behind TLS termination.

**Independent Test**: Production-mode tests pass through the HTTPS edge for cookies, CORS, CSRF/origin, trusted forwarding, and secret absence.

- [x] T026 [US4] Add production-edge security checks in `deploy/scripts/verify-edge-security.sh` (FR-011, FR-021)
- [x] T027 [US4] Confirm focused automated coverage for trusted proxy and production-origin behavior in existing backend/frontend test files (FR-004, FR-011)
- [x] T028 [US4] Run HTTPS redirect, certificate, cookie, CORS, CSRF/origin, forwarded-header, fake-auth exclusion, bundle/log secret scans; record evidence in `docs/launch/evidence/s12/edge-security.md` (SC-009)
- [x] T029 [US4] Run guards and commit Batch D

## Phase 6 — Batch E / US2: Redis, worker, storage, and protected media (P1)

**Goal**: Prove durable queue recovery and preserve the private protected-media path.

**Independent Test**: Safe work enqueues/consumes, recovers after Redis/worker restart, and protected media remains private and authorized.

- [x] T030 [US2] Add a production-like queue/storage/media proof harness in `deploy/scripts/verify-worker-media.sh` reusing existing S4/S5 fixtures (FR-009, FR-010)
- [x] T031 [US2] Prove Redis unavailable failure, restart, PostgreSQL outbox reconciliation, idempotent worker consumption, and observable failure/recovery (SC-004)
- [x] T032 [US2] Prove source/derived objects are private and protected signed playback succeeds without API byte proxying (SC-005)
- [x] T033 [US2] Record evidence in `docs/launch/evidence/s12/queue-media.md`, run guards, and commit Batch E

## Phase 7 — Batch F / US5: Observability and alerts (P2)

**Goal**: Correlate operational failures and demonstrate alert capability without provider lock-in.

**Independent Test**: Induced API/dependency/media-job failures produce redacted correlated output and reach a disposable alert sink.

- [ ] T034 [US5] Use the existing backend logging vocabulary for structured worker lifecycle/job events in `backend/cmd/worker/main.go` and `backend/internal/logging/` with focused tests (FR-012)
- [ ] T035 [US5] Define provider-neutral health/error/backup monitoring and alert webhook configuration in `deploy/monitoring/` (FR-013, FR-014)
- [ ] T036 [US5] Add a disposable alert sink and safe failure scenarios in `deploy/scripts/verify-observability.sh` (FR-014)
- [ ] T037 [US5] Prove correlation and redaction plus disposable alert delivery in `docs/launch/evidence/s12/observability.md`; record external production alert delivery separately as pending when credentials are absent (SC-010, SC-011)
- [ ] T038 [US5] Run guards and commit Batch F

## Phase 8 — Batch G / US6: Application rollback (P2)

**Goal**: Roll application artifacts backward while preserving the forward-compatible database.

**Independent Test**: N → N+1 → N restores frontend/API/worker health and retains schema 15/provenance.

- [ ] T039 [US6] Add release selection and application-only rollback commands in `deploy/scripts/application-rollback.sh` with explicit schema-down refusal (FR-017, FR-018)
- [ ] T040 [US6] Exercise two compatible artifact versions, roll back application artifacts, verify frontend and both API probes, and assert unchanged schema/provenance (SC-008)
- [ ] T041 [US6] Record evidence in `docs/launch/evidence/s12/rollback.md`, update the runbook, run guards, and commit Batch G

## Phase 9 — Batch H / US6: Deployed MVP smoke and convergence (P2)

**Goal**: Exercise the current access-to-learning journey against the deployed production-like origin.

**Independent Test**: Existing S5/S6 automation passes through the deployed edge and proves unrelated-Student denial.

- [ ] T042 [US6] Parameterize the existing production-mode Playwright harness for an externally supplied staging origin without changing S5/S6 business assertions (FR-019)
- [ ] T043 [US6] Run the deployed journey through invitation acceptance with zero access, Admin Approval, exactly one Entitlement/Enrollment, protected playback, progress, and unrelated-Student denial (SC-012)
- [ ] T044 [US6] Run complete backend/frontend/CI-equivalent validation, exposure guard, documentation guard, `git diff --check`, and record results in `docs/launch/evidence/s12/convergence.md`
- [ ] T045 Run `speckit.converge`, reconcile remaining tasks/evidence, freeze the exact S12 implementation range, and dispatch independent review without self-approving closure

## Dependencies

```text
Planning T001–T006
  └─ Batch A T007–T014
       └─ Batch B T015–T020
            ├─ Batch C T021–T025 ─┐
            ├─ Batch D T026–T029 ─┼─ Batch G T039–T041
            ├─ Batch E T030–T033 ─┤
            └─ Batch F T034–T038 ─┘
                                   └─ Batch H T042–T045
```

- T007, T008, and T011 may run in parallel; T009/T010 follow their relevant artifact design.
- T015 and T016 may run in parallel after Batch A.
- Batches C–F may proceed independently after Batch B, subject to shared environment coordination.
- Live cloud deployment, public TLS, and external alert delivery are additional executions of the same
  contracts and require credentials only at that external boundary.

## Task Summary

- Total tasks: **45**
- Planning: 6
- Batch A / US1: 8
- Batch B / US2: 6
- Batch C / US3: 5
- Batch D / US4: 4
- Batch E / US2: 4
- Batch F / US5: 5
- Batch G / US6: 3
- Batch H / US6 and convergence: 4
