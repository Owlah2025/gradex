# Implementation Plan: S1B1 Student Admission

**Branch**: `feature/002-authentication-rbac` | **Date**: 2026-07-30 |
**Spec**: [Authentication and RBAC](spec.md)

**Input**: The Authentication/RBAC feature specification, narrowed to User Story 1 and
FR-001–FR-004, FR-014–FR-016 by the developer-approved
[S1B delivery design](../../docs/superpowers/specs/2026-07-30-s1b-delivery-design.md).

## Summary

Deliver the first public Identity vertical: a visitor can register only as a
`PENDING_VERIFICATION` Student, receive a durable verification-notification intent, request a
privacy-safe resend, and activate exactly once with an expiring digest-only secret. Close the three
S1A admission advisories before mounting routes: bind bootstrap retries to a canonical request
fingerprint, preserve the supplied correspondence email separately from its normalized comparison
key, and make compromised-password screening a required fail-closed credential-boundary dependency.

The implementation extends the existing Go modular monolith and PostgreSQL Identity schema,
introduces a Redis-backed layered limiter with a bounded strict local fallback, exposes the
anonymous security bootstrap, current-policy read, and three public JSON command resources, and adds
responsive Arabic/English Next.js admission screens. Account,
credential, verification secret, policy/evidence, and outbox intent writes remain atomic; no email
delivery worker or authenticated session is in S1B1.

## Technical Context

**Language/Version**: Go 1.26.5; TypeScript 5.5; React 18.3; Next.js 14.2

**Primary Dependencies**: Gin 1.12, pgx/v5, go-redis/v9, `golang.org/x/crypto/argon2`, Next.js App
Router, Tailwind CSS, existing Radix UI primitives

**Storage**: PostgreSQL 16 is authoritative for Identity, evidence, and outbox intent; Redis 7 holds
disposable distributed rate-limit state; browser persistence stores locale only, never credentials
or action secrets

**Testing**: Go unit and race tests, PostgreSQL integration tests behind the `integration` build tag,
HTTP handler/security integration tests, frontend ESLint/TypeScript/production build, and a
scripted end-to-end admission quickstart

**Target Platform**: Linux containers for Go/PostgreSQL/Redis; responsive evergreen phone, tablet,
laptop, and desktop browsers

**Project Type**: Modular-monolith web application with Go API and Next.js frontend

**Performance Goals**: No production throughput or latency claim while LG-019 is open. Bound request
bodies and limiter key cardinality, keep Argon2id and compromised-password work outside database
transactions, and verify that duplicate/rollback/concurrency paths terminate within configured test
timeouts.

**Constraints**: Deny by default; Student-only public registration; no session before verification;
uniform hidden-Account outcomes; password plaintext remains inside one reviewed credential boundary;
provider-neutral compromised screening fails credential creation closed; bearer secrets are
digest-only and body-submitted; source/evidence/outbox admission is one PostgreSQL transaction;
Redis loss may use only bounded strict fallback and otherwise returns generic `503`; API errors use
RFC 9457; Arabic/English and RTL/LTR behavior ship together; LG-011, LG-018, LG-019, and LG-021 block
production claims or activation at their named boundaries.

**Scale/Scope**: One migration boundary, the anonymous security bootstrap and current-policy read,
three public backend command routes, one deployment-bootstrap hardening change, one
credential-screening adapter seam, one shared limiter component, and three frontend admission
states/screens. Login, authenticated sessions, recovery, staff invitation, provider delivery, and
notification-center UI remain outside S1B1.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Pre-research result | Post-design result |
|---|---|---|
| I. Source documents are authoritative | PASS — scope is the approved S1B1 subset; open gates remain explicit boundaries. | PASS — research, data model, contracts, and quickstart cite the governing designs and do not resolve open gates. |
| II. Deny by default, enforce in backend | PASS — public commands grant only pending Student admission; no role input or session issuance exists. | PASS — contracts omit role/session fields and backend commands own status, purpose, expiry, and mutation checks. |
| III. Business-rule traceability | PASS — BR-001/002/003/008/105/120/122 govern the slice. | PASS — entities, contracts, and validation scenarios cite their BR identifiers. |
| IV. Payment correctness | PASS — payment behavior is outside this slice. | PASS — no payment or Entitlement behavior is introduced. |
| V. Testing commensurate with risk | PASS — unit, database/API integration, concurrency, rollback, privacy, and end-to-end evidence are planned. | PASS — `quickstart.md` names the verification method for each S1B1 close condition. |
| VI. Modular monolith, simplicity by default | PASS — work stays in the existing Go API and Next.js app; PostgreSQL/Redis are already approved dependencies. | PASS — minimal shared outbox intent and limiter interfaces add no service or speculative provider implementation. |
| VII. Data integrity | PASS — schema changes use a versioned migration and transactions/constraints own structural invariants. | PASS — `data-model.md` defines uniqueness, checks, lock order, append-only evidence, and rollback behavior. |
| VIII. Quality gate | PASS — formatting, build, vet, race, integration, frontend, migration, docs, and exposure guards are required. | PASS — `quickstart.md` provides the repository commands and expected evidence. |
| IX. Operational discipline | PASS — request IDs, typed safe outcomes, immutable security evidence, stable outbox IDs, and limiter metrics are in scope. | PASS — contracts distinguish durable intent from provider delivery and retain fail-closed dependency behavior. |
| X. Responsive, bilingual, accessible web | PASS — Arabic/English registration and verification are explicit slice output. | PASS — UI contract covers responsive layout, RTL/LTR, labels, focus, live status, and no credential persistence. |
| XI. Documentation stays in sync | PASS — feature, launch record, delivery design, and planning artifacts are the implementation inputs. | PASS — implementation tasks must update contracts/config/schema/launch evidence together. |

No constitution violation or unresolved clarification remains.

## Project Structure

### Documentation (this feature)

```text
specs/002-auth-rbac/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── admission-api.md
│   ├── credential-screening.md
│   └── verification-outbox.md
└── tasks.md
```

### Source Code (repository root)

```text
backend/
├── cmd/
│   ├── api/main.go                    # compose admission dependencies/routes
│   └── bootstrap-admin/main.go        # required checker + fingerprinted retry
├── internal/
│   ├── config/                        # typed admission/limiter/policy settings
│   ├── db/
│   │   ├── migrations/                # next versioned Identity/outbox migration
│   │   └── schema.go
│   ├── httpapi/                       # public command handlers and admission middleware
│   ├── identity/                      # validators, registration, resend, consumption
│   ├── problem/                       # stable admission Problem Details
│   └── ratelimit/                     # Redis decision + bounded strict fallback
└── scripts/                           # existing local/CI verification inputs

frontend/
├── src/
│   ├── app/
│   │   ├── register/
│   │   └── verify-email/
│   ├── components/auth/               # admission forms/result presentation
│   └── lib/
│       ├── api/                        # typed admission client
│       └── i18n/dictionaries/          # paired English/Arabic strings
└── package.json

scripts/
├── docs-guard.sh
└── expose-guard.sh
```

**Structure Decision**: Extend the existing two-project web application. Identity owns Accounts,
credentials, action secrets, registration/security evidence, and admission commands. The HTTP layer
owns transport and middleware ordering; `ratelimit` owns only disposable quota decisions; the
frontend owns presentation and calls the private same-origin API. The shared outbox table carries
durable intent, while provider delivery remains a later Notifications/S9 adapter.

## Complexity Tracking

No constitution violations require justification.
