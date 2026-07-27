# Implementation Plan: S2 — Course Authoring and Review

**Branch**: `003-course-authoring` | **Date**: 2026-07-28 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/003-course-authoring/spec.md`

**Status**: Frozen for implementation on D3–D5 under
[D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews).
**Implementation does not begin until S1C closes on an independent verdict.**

## Summary

Deliver the authoring half of the catalogue: an Instructor builds a private Course graph referencing
already-processed media, submits it against a validation that names everything missing, and an Admin
publishes it, requests changes on it, prices it, or takes it out of circulation — with every
privileged action audited and every revision applied atomically or not at all.

The technical core is **one idea**: a Course has a *live approved revision pointer*, and every
mutation to a Published Course writes a new candidate revision rather than editing the live graph.
Publishing is a single-row pointer swap inside one transaction. That is what makes FR-020 provable
instead of aspirational, and it is what the rest of the design serves.

## Technical Context

**Language/Version**: Go 1.x backend (existing module), TypeScript/Next.js frontend (existing app)

**Primary Dependencies**: All existing. `gin` router, `pgx`/`database/sql` against PostgreSQL, the
`identity` package's `Authorize` gate and principal resolver, the `outbox` package's protected-payload
intent boundary, the `audit_events` table from migration `0003`, and the RFC 9457 problem envelope in
`internal/problem`. **No new infrastructure, no new service, no new queue.**

**Storage**: PostgreSQL as the durable authority. New migration `0009_course_authoring` — schema
moves 8 → 9. CI derives the expected version from `db.MaxSchemaVersion`, so no literal needs editing
(the `CARRYOVER-S1B2-CI-DRIFT` fix already handles this).

**Testing**: `go test -race` for logic; `-tags=integration` against real PostgreSQL for every
invariant, transition, and concurrency case; `node:test` for frontend logic; the derived-route
authorization sweep for FR-042.

**Target Platform**: Existing Linux API + worker + Next.js frontend.

**Project Type**: Web application — existing `backend/` + `frontend/` layout.

**Performance Goals**: No new targets. Authoring is a low-volume, staff-facing surface; the catalogue
read path it feeds belongs to S3.

**Constraints**: Bilingual Arabic/English with correct RTL/LTR (Constitution X). Complex authoring may
be optimized for tablet and larger. `LG-018` is unresolved, so notification is **intent only**.

**Scale/Scope**: 8–12 launch Courses, low tens of Sections/Lessons each, single-digit staff accounts.
This is deliberately not a scale problem, and no part of the design should be justified by future
volume (Constitution VI).

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

| Principle | Gate | Status |
|---|---|---|
| I — Source documents authoritative | Every behaviour traces to a BR or an approved design | **PASS** — 45 FRs, each cited |
| II — Deny by default, backend-enforced | Single decision point; ownership server-side; privileged actions audited | **PASS by design** — FR-041/042/043; see §Authorization below |
| III — BR traceability | Spec, plan, and tasks cite BR-xxx | **PASS** |
| IV — Payment correctness | S2 touches no payment path | **N/A** — and the plan must keep it that way: price *setting* only, never Order pricing |
| V — Testing commensurate with risk | Business logic unit, DB/API integration, critical journeys end-to-end | **PASS** — every acceptance item names its method in `tasks.md` |
| VI — Modular monolith, simplicity | No new service, library, or infrastructure without demonstrated need | **PASS** — zero new infrastructure |
| VII — Data integrity | DB constraints and transactions over application checks; versioned migrations; delete safeguards | **PASS** — see §Data integrity |
| VIII — Quality gate | Format, lint, typecheck, tests before merge | **PASS** — existing CI, unchanged |
| IX — Operational discipline | Structured logging and meaningful errors on publishing/admin operations | **PASS** — existing logging rails |
| X — Responsive, bilingual, accessible | Arabic/English, RTL/LTR from the start | **PASS** — SC-009 |
| XI — Docs stay in sync | Affected contracts and documents updated with the code | **PASS** — obligation carried into `tasks.md` |

**No violations. Complexity Tracking is therefore empty and stays empty.**

### Authorization — the part S1C got wrong, stated so it cannot recur

`identity.Authorize(principal, capability)` already decides **role → capability** over a closed set
that includes `CONTENT_MANAGEMENT` (held by Instructor and Admin) and `ADMIN_OPERATIONS`.

It deliberately does **not** decide ownership, because ownership is a fact about a *resource*, not
about a role. That gap is exactly where a second decision point gets invented. The plan closes it as
follows:

1. **Capabilities**: S2 adds `CATALOG_PUBLISH`, `CATALOG_PRICING`, and `CATALOG_TAXONOMY` to
   `AllCapabilities` and to `Authorize`'s switch. Admin-only. Instructor authoring continues to use
   the existing `CONTENT_MANAGEMENT`. **No capability check happens outside `Authorize`.**
2. **Ownership**: a single `RequireCourseOwnership` precondition, applied as route middleware, that
   loads the Course and compares its owner to the principal. One implementation, one place.
3. **Proof**: the S1C matrix harness — which derives the route table from `r.Routes()` — is extended
   to assert that **every** route under the owned-resource prefixes carries that middleware. A new
   route without it fails a test. This is FR-042, and it is the direct answer to S1C's
   hand-maintained matrix finding.

## The four concurrency cases, resolved

The spec named these; a plan that leaves them to the implementer produces four different answers.

| # | Race | Resolution |
|---|---|---|
| 1 | Two Admins act on one `PENDING_REVIEW` Course (publish vs. request changes) | Every review action takes `SELECT … FOR UPDATE` on the Course row and re-asserts the expected current state inside the transaction. The loser gets a `409` conflict naming the state it actually found. **No last-write-wins.** |
| 2 | Instructor submits the same Course twice concurrently | A partial unique index — at most one non-terminal revision per Course — makes the second submission a constraint violation mapped to an idempotent `409`, not a duplicate queue entry. The database refuses it, not the handler. |
| 3 | Revision approval races Instructor suspension | Approval re-reads the owner's account status inside the approving transaction. A suspended owner fails the approval closed. This mirrors the S1B2 session-repository precedent, which re-reads `accounts.status` live rather than trusting a cached claim. |
| 4 | Emergency suspension races revision approval | Both take the same Course row lock, so they serialize. Whichever commits second observes the first. Emergency suspension is orthogonal to lifecycle state — it is a separate column, not a lifecycle value — so a suspended Course can still have a revision approved, and it stays inaccessible until restored. |

Two further races the spec listed get the same treatment: an Asset Version disappearing between
submission and approval is caught by **revalidating references inside the approving transaction**
(FR-025), and taxonomy-term retirement is checked at assignment *and* at approval.

## Data integrity

Constraints, not intentions. Everything here belongs in migration `0009`:

- Lifecycle is a PostgreSQL `ENUM`; illegal values cannot be written at all.
- `courses.live_revision_id` is a nullable FK to `course_revisions`. **`NULL` means never published.**
  A Student-visible read joins through this pointer and can therefore never see a draft graph — the
  privacy property is structural rather than a `WHERE` clause someone can forget.
- Partial unique index: at most one `PENDING_REVIEW` revision per Course (race 2).
- Exactly one owning Instructor per Course: `NOT NULL` FK to `accounts`, plus a role check at write.
- Exactly one preview asset per Course: nullable single column, not a collection (BR-143).
- Taxonomy: exactly one Major, one Subject, one Study Year — three `NOT NULL` columns after
  assignment, validated at submission rather than at creation, since a Draft may be incomplete.
- Delete safeguard (BR-018): deletion is refused by an explicit enrollment count check in the same
  transaction, mirroring the existing archive-vs-delete gate. **Not** an `ON DELETE CASCADE`.
- Price change history is append-only. The current price is derived from the latest record rather
  than duplicated in a mutable column, so history and value cannot disagree.
- Every privileged action writes its `audit_events` row **in the same transaction** as the change,
  under the existing `CATALOG_AND_AUTHORING` module value — which already exists in the `0003` enum
  and needs no migration.

## Project Structure

### Documentation (this feature)

```text
specs/003-course-authoring/
├── spec.md              # Frozen
├── plan.md              # This file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/           # Phase 1
│   ├── authoring-api.md
│   ├── review-api.md
│   └── catalog-admin-api.md
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks, not this command
```

### Source Code

```text
backend/
├── internal/
│   ├── catalog/                  # NEW — the whole slice's domain logic
│   │   ├── course.go             # Entity, lifecycle transitions, invariants
│   │   ├── revision.go           # Candidate revisions and the atomic pointer swap
│   │   ├── validation.go         # Submission completeness — returns ALL failures
│   │   ├── pricing.go            # Admin-only price changes, append-only history
│   │   ├── taxonomy.go           # Term administration and assignment rules
│   │   ├── suspension.go         # Emergency access suspension and restoration
│   │   └── repository.go         # Transactional persistence
│   ├── identity/
│   │   ├── policy.go             # EDIT — three capabilities added to the closed set
│   │   └── policy_set.go         # EDIT — role grants
│   ├── httpapi/
│   │   ├── catalog_routes.go     # NEW — route registration
│   │   ├── authoring_handlers.go # NEW — Instructor surface
│   │   ├── review_handlers.go    # NEW — Admin review surface
│   │   ├── catalog_ownership.go  # NEW — the single ownership precondition
│   │   └── authorization_test.go # EDIT — derived sweep extended to S2 routes
│   └── db/migrations/
│       ├── 0009_course_authoring.up.sql
│       └── 0009_course_authoring.down.sql
frontend/
└── src/app/[locale]/
    ├── instructor/courses/       # NEW — builder, submission, validation display
    └── admin/catalog/            # NEW — review queue, pricing, lifecycle, taxonomy
```

**Structure Decision**: A new `internal/catalog` package, parallel to `internal/identity`, matching
the M1 module boundary `CATALOG_AND_AUTHORING` that the audit enum already names. Authorization stays
in `identity`; the catalogue never re-decides it. HTTP handling stays in `httpapi` as with every
prior slice. `internal/video` is untouched — it is S4 migration input, not a dependency
([SLICES.md §3.3](../../docs/launch/SLICES.md#33-the-legacy-video-path-is-migration-input-not-a-second-authority)).

## Standing implementation clause

Carried from the S1C closeout, where six instances of one defect class appeared in a single slice:

> **A required dependency is validated at construction and the component refuses to build without it.
> No security-relevant control may silently degrade, default, or become optional.**

Concretely for S2: no conditional CSRF, no defaulted authorization, no optional audit writer, no
optional outbox intent, no ownership check that is skipped when a lookup fails. A control that cannot
be satisfied **refuses the request** — it does not proceed with less.

## Complexity Tracking

No Constitution violations. This table is intentionally empty.
