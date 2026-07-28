# Implementation Plan: S2 — Course Authoring and Review

**Branch**: `003-course-authoring` | **Date**: 2026-07-28 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/003-course-authoring/spec.md`

**Status**: T001–T031 closed; D5 is frozen to T032–T038 under
[D-042](../../docs/DECISIONS.md#d-042--codex-plans-antigravity-implements-and-claude-independently-reviews).
Codex specifies, Antigravity implements through `speckit.implement`, and Claude independently reviews
the frozen implementation range.

## Summary

Deliver the authoring half of the catalogue: an Instructor builds a private Course graph referencing
already-processed media, submits it against a validation that names everything missing, and an Admin
publishes it, requests changes on it, prices it, or takes it out of circulation — with every
privileged action audited and every revision applied atomically or not at all.

The technical core is **one idea**: a Course has a *live approved revision pointer*, and every
mutation to a Published Course names one database-unique active candidate rather than editing or
implicitly resolving the live graph. Approval swaps the pointer and its evidence in one transaction;
the production read path captures that pointer once per graph. Both halves are required to make
FR-020 provable instead of aspirational.

## Technical Context

**Language/Version**: Go 1.x backend (existing module), TypeScript/Next.js frontend (existing app)

**Primary Dependencies**: All existing. `gin` router, `pgx`/`database/sql` against PostgreSQL, the
`identity` package's `Authorize` gate and principal resolver, the `outbox` package's protected-payload
intent boundary, the `audit_events` table from migration `0003`, and the RFC 9457 problem envelope in
`internal/problem`. **No new infrastructure, no new service, no new queue.**

**Storage**: PostgreSQL as the durable authority. T001–T031 landed
`0009_course_authoring` (schema 8 → 9). D5 adds the minimal
`0010_revision_integrity` migration (schema 9 → 10) for candidate/pointer invariants and stable
Section/Lesson identities. CI derives the expected version from `db.MaxSchemaVersion`; no hardcoded
CI literal is permitted.

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
| I — Source documents authoritative | Every behaviour traces to a BR or an approved design | **PASS** — FR-001–FR-045 plus owner-approved D5 refinements FR-046–FR-055; FR-047 carries BR-019/BR-059 stable identity |
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

## D5 concurrency contract

T038 means these exact four races. It does not refer implicitly to every race discussed elsewhere in
S2:

| # | D5 race | Required outcome |
|---|---|---|
| 1 | Two first edits of one Published Course | Both serialize on the Course lock and return the same candidate identity; the partial unique index independently prevents two active candidates. |
| 2 | Two approvals of the same candidate | Exactly one commits. The loser locks the Course then finds the named candidate terminal or replaced and returns `409`; only one live pointer and one approval evidence set exist. |
| 3 | Approval races an Instructor mutation of the same `PENDING_REVIEW` candidate | The mutation returns `409` regardless of lock order because a pending candidate is read-only. Approval either waits for that refusal or commits first; the mutation never enters or changes the approved graph. |
| 4 | Approval races rejection of the same candidate | Exactly one terminal action commits. The loser returns `409`; `APPROVED` and `REJECTED` cannot both be evidenced. |

The earlier plan also identified cross-phase races. Their obligations remain, but they are not the
ambiguous referent of T038:

The spec named these; a plan that leaves them to the implementer produces four different answers.

| # | Race | Resolution |
|---|---|---|
| 1 | Two Admins act on one `PENDING_REVIEW` Course (publish vs. request changes) | This is D5 race 4, made candidate-specific above. |
| 2 | Instructor submits the same candidate twice concurrently | The Course/candidate locks and active-candidate index serialize the exact transition. One submission commits; the request that observes `PENDING_REVIEW` returns `409`, and no duplicate queue entry is possible. |
| 3 | Revision approval races Instructor suspension | Approval re-reads the owner's account status inside the approving transaction. A suspended owner fails the approval closed. This mirrors the S1B2 session-repository precedent, which re-reads `accounts.status` live rather than trusting a cached claim. |
| 4 | Emergency suspension races revision approval | Both take the same Course row lock, so they serialize. Whichever commits second observes the first. Emergency suspension is orthogonal to lifecycle state — it is a separate column, not a lifecycle value — so a suspended Course can still have a revision approved, and it stays inaccessible until restored. |

Two dependency races get the same treatment: an Asset Version changing between submission and
approval and taxonomy retirement are caught by transaction-aware revalidation against the same
approving transaction, with referenced rows protected until commit.

## Candidate creation and explicit mutation identity

`PUT /api/v1/courses/{courseId}/candidate` is the sole create-or-return operation. It locks the Course
first. For a never-published Course it returns the existing initial Draft. For a Published Course it
returns the existing active candidate or clones the exact graph named by the captured
`live_revision_id`. A second concurrent request returns the same candidate. Migration `0010` is the
database backstop:

```sql
CREATE UNIQUE INDEX course_revisions_one_active_candidate
ON course_revisions (course_id)
WHERE state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW');
```

Migration `0010` also adds nullable `based_on_revision_id` to `course_revisions`, a same-Course
composite foreign key from `(course_id, based_on_revision_id)` to `(course_id, id)`, and a self-base
refusal. It is null for the initial first-publication Draft and set to the captured
`live_revision_id` for a Published-Course candidate. Approval compares it with the locked Course
pointer; a mismatch means the candidate is stale and returns `409`.

Cloning copies revision-owned rows and foreign-key references in one transaction. It does not copy
the referenced media objects or any commerce, enrollment, Entitlement, or learning record. New
version-row IDs are allocated for Sections, Lessons, and files, but unchanged Sections and Lessons
retain their stable identity IDs. A newly authored entity receives a new stable identity; deleting
and recreating one also creates a new identity. This is required so a video replacement preserves
Lesson progress (BR-059) and a Section remains the same purchasable scope (BR-019).

Every content mutation and submission route contains `{revisionId}`. Repository commands accept that
identity directly and verify that it belongs to the named Course, is editable (`DRAFT` or
`CHANGES_REQUESTED`), and is not `live_revision_id`; submission additionally performs the exact
editable-to-pending transition. `PENDING_REVIEW` remains an active candidate for uniqueness but is
read-only. Queries such as `ORDER BY revision_number DESC LIMIT 1` are forbidden on mutation paths:
chronology is not authority. Section and Lesson path IDs are their stable identities; the repository
resolves the matching version row only inside the named candidate. A wrong-Course candidate or child
retains the existing concealed-resource denial rather than being reported as a state conflict.

## Read consistency

The production live-graph loader first captures `courses.live_revision_id`, then passes that immutable
identity to every query that assembles Course fields, Sections, Lessons, taxonomy, and Asset Version
references. No child loader may query the Course pointer again or substitute the latest revision.
Multi-query assembly is valid only with the captured identity; concurrent approval therefore yields a
complete old graph or a complete new graph, never a mixture.

The already mounted `GET /api/v1/courses/{id}` owner response uses this loader for its
`live_revision` field and loads the active candidate separately by explicit identity. This makes the
loader reachable through `buildProductionFoundations` and the production router during D5 without
adding a Student catalogue, learning, search, or playback route. S3 and S5 consume this same loader
rather than constructing a second Student-visible query path.

## Approval and rejection transactions

Both commands use the exact `{courseId, revisionId}` from the route and one PostgreSQL transaction.
The documented lock order is:

1. lock the Course row `FOR UPDATE`;
2. lock the named candidate revision `FOR UPDATE`;
3. verify Course ownership relationship, candidate membership, `PENDING_REVIEW`, and
   `candidate.based_on_revision_id == course.live_revision_id`;
4. lock the owner Account `FOR SHARE`, then referenced taxonomy rows and video/Asset Version rows
   `FOR SHARE` in ascending identifier order; every dependency mutation takes a conflicting row lock.
   Re-read owner eligibility and revalidate the complete revision graph, required processed assets,
   and taxonomy availability through readers bound to this same transaction;
5. for approval, mark the previous live revision `SUPERSEDED`, mark the candidate `APPROVED`, swap
   `live_revision_id`, keep lifecycle `PUBLISHED`, and write audit plus outbox intent;
6. for rejection, mark only the candidate `REJECTED`, preserve the mandatory reason and review
   evidence, and write the rejection audit plus outbox intent without touching Course lifecycle or
   pointer;
7. commit.

No notification delivery or other external call occurs inside the transaction. Only durable outbox
intent is written. Any error rolls back pointer, revision states, audit, and outbox together.
Required validators and the outbox writer are construction-time dependencies; nil checks, ignored
writer errors, or optional notification paths are fail-open defects.

`409` is reserved for stale or competing state: terminal/replaced candidate, competing approval or
rejection, or a mutation losing to approval. Incomplete graphs, invalid or unavailable dependencies,
and an ineligible owning Instructor at approval use the existing `422` validation envelope. Caller
authorization, ownership concealment, and mutation by an acting suspended Instructor retain the
existing `401`/`403` denial semantics.

## D5 mutation evidence

Each mutation is run independently, restored afterward, and reported with both its proof and limit:

| Mutation | Test that must fail | Proves | Does not prove |
|---|---|---|---|
| Move the `courses.live_revision_id`/lifecycle update to an auto-commit write after the approval transaction, then inject failure at that write | Approval rollback-state snapshot | Pointer, revision states, audit, and outbox share one rollback boundary | Read-path consistency |
| Remove the active-candidate partial unique index | Direct concurrent two-`DRAFT` insert invariant test | The database independently refuses duplicates when the application lock is bypassed | Route-level idempotent create-or-return behavior |
| Make one live child loader select the latest revision instead of the captured `live_revision_id` | Pending-visibility and concurrent-reader tests | Candidate content cannot leak into the live graph | The approval transaction boundary |
| Bypass the shared approval-time validation call, then invalidate completeness, owner eligibility, taxonomy, and assets in separate subcases | Approval response/state matrix | Submission-time validation is not trusted for any required dependency | Mid-validation dependency locking |
| Let rejection alter lifecycle, pointer, or live revision state | Rejection preservation test | Rejection cannot disturb the live graph or access state | Approval rollback |
| Regenerate stable Section/Lesson identities while cloning | Clone-lineage and video-replacement tests | Unchanged logical entities survive a revision and BR-059 progress identity is preserved | External-resource clone counts |

The production-wiring proof extends `TestProductionRouterWiringHasNoMissingSurfaces` through
`buildProductionFoundations` and asserts the exact D5 method/path surface, not merely the already
present `/api/v1/courses` prefix. The ownership/authorization sweep continues to derive from that
production router.

## Data integrity

Constraints, not intentions. The base schema belongs to migration `0009`; D5 adds the bounded
`0010` revision-integrity changes:

- Lifecycle is a PostgreSQL `ENUM`; illegal values cannot be written at all.
- `courses.live_revision_id` is a nullable same-Course composite FK to `course_revisions`. **`NULL`
  means never published.** A Student-visible read joins through this pointer and can therefore never
  see a draft graph — the privacy property is structural rather than a `WHERE` clause someone can
  forget.
- D5 replaces the narrower `PENDING_REVIEW` partial unique index with one active-candidate index: at
  most one `DRAFT`, `CHANGES_REQUESTED`, or `PENDING_REVIEW` revision per Course. This is the
  database backstop for concurrent first edits; the down migration restores the narrower index.
- D5 `based_on_revision_id`: nullable same-Course FK to the exact revision cloned; null only for a
  first-publication Draft, never inferred from revision number.
- D5 stable identity registries: `course_section_identities` belong to a Course and
  `course_lesson_identities` belong to a Section identity. Revision-owned Section/Lesson rows
  reference them; cloning preserves the references while creating new version-row IDs. Uniqueness
  prevents one identity from appearing twice in one candidate graph.
- D5 rejection reason constraint: `review_reason` is non-empty for both `CHANGES_REQUESTED` and
  `REJECTED`, so a rejected candidate without preserved evidence is unrepresentable.
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
│       ├── 0009_course_authoring.down.sql
│       ├── 0010_revision_integrity.up.sql
│       └── 0010_revision_integrity.down.sql
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
optional outbox intent, no ownership check that is skipped when a lookup fails. The production
catalogue foundation requires the real repository and session-mutation foundation; every authoring
or review mutation runs origin/CSRF enforcement before authorization and ownership. A control that
cannot be satisfied **refuses the request** — it does not proceed with less.

## D5 scope boundary

D5 stops after T038. It does not pull forward Admin pricing, lifecycle or emergency controls,
taxonomy administration, search, unrelated frontend work, or unrelated refactoring. The additive
schema invariant, transaction-aware dependency checks, required outbox construction, explicit
candidate routes, stable Section/Lesson identity, same-Course pointer integrity, catalog mutation
security, and the production live-graph loader are admitted because T032–T038 cannot meet
FR-046–FR-055 and BR-019/BR-059 without them. Any other prerequisite stops for a recorded scope
decision.

## Complexity Tracking

No Constitution violations. This table is intentionally empty.
