# Implementation Plan: S1B2 Authenticated Sessions

**Branch**: `feature/002-authentication-rbac` | **Date**: 2026-07-31 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/002-auth-rbac/s1b2/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

Implement first-party browser login, session resolution, controlled renewal, and logout for Active
Student, Instructor, and Admin Accounts. The backend will issue one opaque
`__Host-gradex_session` cookie, persist only SHA-256 credential and CSRF digests in the existing
session-family schema, enforce role-specific server-side expiry, and atomically rotate immutable
credential generations with stale-use classification and family revocation on confirmed reuse.
The bilingual Next.js UI will keep the CSRF token in memory only and use a validated internal
`returnTo`. S1B1 security carryovers land before the session routes.

## Technical Context

**Language/Version**: Go 1.26.5; TypeScript 5.5; React 18

**Primary Dependencies**: Gin 1.12, pgx 5.10, go-redis 9.21, Next.js 14.2

**Storage**: PostgreSQL for Accounts, families, immutable credential generations, and security
evidence; Redis for layered rate decisions

**Testing**: Go unit, race, and PostgreSQL integration tests; frontend TypeScript checking,
ESLint, production build, and focused component/browser inspection

**Target Platform**: Linux API server and modern desktop/mobile browsers on the first-party web
origin

**Project Type**: Modular-monolith web application (Go API plus Next.js frontend)

**Performance Goals**: Login failures remain in one production-comparable response-time class;
session lookup and rotation stay within existing API/database timeouts; the login UI remains
responsive at the PRD viewport targets

**Constraints**: One opaque HttpOnly cookie; no browser-persisted authentication or CSRF secrets;
generic hidden-state login failure; deny by default; one renewal winner; server revocation commits
before browser credential changes; no new service or token vault

**Scale/Scope**: Four session endpoints, three role-specific expiry profiles, one bilingual login
screen, and the existing `sessions`/`session_credentials` tables

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

### Pre-research gate

| Principle | Result | Evidence |
|---|---|---|
| I. Source Documents | PASS | D-034 and BR-003–006 are reconciled before code. |
| II. Deny by Default | PASS | Every cookie request resolves current server state; mutations recheck it. |
| III. Traceability | PASS | Spec, contract, tests, and tasks cite BR-003–006 and D-034. |
| IV. Payment | N/A | No payment behavior changes. |
| V. Risk-based Testing | PASS | Unit, contract, PostgreSQL integration, concurrency, and UI gates are planned. |
| VI. Modular Monolith | PASS | Existing API/frontend/database/Redis components only; no token vault or service. |
| VII. Data Integrity | PASS | Existing constraints and transactions enforce one current generation. |
| VIII. Quality Gate | PASS | Format, lint, race, integration, build, exposure, CI, and review gates are required. |
| IX. Operations | PASS | Allowlisted session/security evidence avoids secret or hidden-state leakage. |
| X. Responsive/Bilingual | PASS | Arabic/English, RTL/LTR, keyboard, and responsive states are specified. |
| XI. Documentation | PASS | Contract, decision, daily record, and source documents remain synchronized. |

### Post-design re-check

PASS. Phase 1 introduces no new service, dependency, migration, business behavior, or source-document
conflict. The selected contracts preserve all pre-research gates.

## Project Structure

### Documentation (this feature)

```text
specs/002-auth-rbac/s1b2/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/           # Phase 1 output (/speckit-plan command)
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)
```text
backend/
├── cmd/api/
└── internal/
    ├── auth/
    ├── config/
    ├── db/
    ├── health/
    ├── httpapi/
    ├── identity/
    ├── problem/
    └── ratelimit/

frontend/
├── src/
│   ├── app/(auth)/
│   ├── components/auth/
│   └── lib/
│       ├── api/
│       ├── i18n/
│       └── identity/
└── public/

specs/002-auth-rbac/s1b2/
├── contracts/
├── data-model.md
├── plan.md
├── quickstart.md
├── research.md
├── spec.md
└── tasks.md
```

**Structure Decision**: Extend the existing modular Go API and Next.js application in place. Keep
session domain/repository behavior in `backend/internal/identity`, HTTP policy in
`backend/internal/httpapi`, authentication context integration in `backend/internal/auth`, and
browser-memory state in `frontend/src/lib/identity`. No additional project, service, or persistence
layer is introduced.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No constitution violations.
