# Implementation Plan: S11 — Release Acceptance

**Branch**: `s11-release-e2e-20260808` | **Date**: 2026-08-08 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/009-release-acceptance/spec.md`

## Summary

Compose the existing S1 identity, S4 protected-delivery, S5 protected-learning, S6 access-grant, and S12 disposable HTTPS assets into one release-acceptance contract. Add the missing browser registration/verification/login leg, replace weak replay evidence with an authorized idempotent replay, expose exact grant provenance and durable Progress in test-only evidence, and select existing negative/concurrency checks instead of recreating them. No production business behavior or schema changes.

## Technical Context

**Language/Version**: Go 1.24 backend test utility; TypeScript 5.5 and Node 22 frontend test harness; POSIX Bash deployment verifier

**Primary Dependencies**: Existing Gin/PostgreSQL services, Playwright 1.62, Next.js 15.5, Docker Compose, Caddy, Redis, MinIO

**Storage**: Existing PostgreSQL schema version 15; isolated `gradex_playwright_e2e_*` acceptance database only; no migration

**Testing**: Go unit/integration tests, Node test runner, TypeScript typecheck, Playwright Chromium, existing S12 HTTPS smoke and media retrieval verifier

**Target Platform**: Linux development/CI host and the existing disposable production-like HTTPS topology; later T047 public Linux staging

**Project Type**: Modular-monolith web application with deployment verification scripts

**Performance Goals**: Preserve existing launch thresholds; S11 adds correctness acceptance and does not define a new load target

**Constraints**: Hard launch target 2026-08-15; no S8, Entitlement updates, commerce, provider deployment, new service, or schema change; default S12 behavior must remain unchanged

**Scale/Scope**: One launch-critical journey, its authorization/failure/recovery boundaries, and one portable release command

## Constitution Check

*GATE: Passed before Phase 0 and passed again after Phase 1 design.*

| Principle | Evidence |
|---|---|
| I. Sources authoritative | S11 follows D-045, BR-023/024/025/028/029/116/165–169, the user-supplied S8 freeze, and S12's external-infrastructure boundary. |
| II. Deny by default | Pre-approval, unrelated-Student, anonymous-media, playback, and Progress denials are explicit acceptance outcomes. |
| III. Traceability | Every access requirement and task cites the governing BR identifiers; the traceability contract maps every FR and SC to evidence. |
| IV. Access-grant correctness | The suite proves typed Invitation provenance, exact cardinality, authorized sequential replay, and existing concurrent replay. |
| V. Risk-proportionate testing | Browser E2E covers the critical journey; Go integration covers concurrency and identity recovery; deployed HTTPS verifies signed media. |
| VI. Simplicity | Existing S5/S6 Playwright and S12 deployment infrastructure are reused; no service or product abstraction is added. |
| VII. Data integrity | No migration; isolated databases only; exact database constraints and counts are observed. |
| VIII. Quality gate | Formatting, typecheck, unit/integration, browser, build, schema, and clean-worktree checks are tasks. |
| IX. Operational discipline | Retry/replay and recovery are exercised, and evidence records exact failure boundaries without secrets. |
| X. Web experience | Existing bilingual/responsive/accessibility S5 coverage is selected; S11's critical journey uses the shipped web UI. |
| XI. Documentation sync | Spec, plan, tasks, quickstart, contract, traceability, and retained launch evidence are updated together. |

No constitutional violation requires a Complexity Tracking entry.

## Project Structure

### Documentation (this feature)

```text
specs/009-release-acceptance/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── release-suite.md
│   └── traceability.md
├── checklists/
│   └── requirements.md
└── tasks.md
```

### Source Code (repository root)

```text
backend/cmd/e2e-seed/                 # test-only token and state evidence utility
backend/internal/httpapi/             # selected identity/access/authorization integration coverage
frontend/e2e/                         # existing S5/S6 journeys plus S11 critical journey
frontend/src/lib/api/                 # test-only state/query helpers and their unit tests
frontend/playwright.config.ts         # existing local/external origin contract
deploy/compose/                        # existing disposable production-like topology
deploy/scripts/                       # existing S12 verifier and thin S11 entry point
docs/launch/evidence/s11/             # redacted exact-head acceptance record
```

**Structure Decision**: Keep S11 entirely in existing test, deployment-verification, and documentation boundaries. Production packages and database migrations remain untouched.

## Complexity Tracking

No violations.
