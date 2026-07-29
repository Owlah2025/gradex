# Implementation Plan: S6 — Course Access Invitation and Entitlement Grant

**Branch**: `006-course-access-grant` | **Date**: 2026-07-29 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/006-course-access-grant/spec.md`

**Constitution**: v1.1.0 (amended 2026-07-29). Principle IV — Access-Grant Correctness — governs this
slice directly.

## Summary

Build the only path by which course access exists in the MVP: an Admin creates a Course Access
Invitation after confirming External Payment off-platform, the invited Student accepts from that
identity alone, and an authorised Admin approves — which is the single transaction that creates the
Enrollment and the Entitlement.

The technical core is one transaction and four invariants around it. Everything else in this slice is
CRUD and screens. The transaction takes a fixed lock order, re-asserts every precondition inside the
transaction rather than before it, and is idempotent by the invitation identifier. The invariants —
one non-terminal invitation per email and Course, one active Entitlement per Student and Course, one
Enrollment per Student and Course, and no Entitlement without a typed grant source — are database
constraints, not handler checks, because a handler check loses under concurrency and this is the
highest-risk path in the release.

## Technical Context

**Language/Version**: Go 1.x (backend, existing `go.mod`), TypeScript with Next.js/React (frontend)

**Primary Dependencies**: Gin, pgx/pgxpool, the existing `internal/identity` capability and session
model, `internal/outbox` writer, `internal/catalog` repository, `internal/problem` RFC 9457 envelope

**Storage**: PostgreSQL — one new migration, `NNNN_course_access_grant`, whose number is derived from repository state at implementation time

**Testing**: `go test` unit; `go test -tags=integration -race` against real PostgreSQL, Redis, MinIO;
`node:test` frontend logic; concurrency tests are mandatory on the grant path per Constitution V

**Target Platform**: Linux server on the existing split managed PaaS (D-025)

**Project Type**: Web application — Go API and worker, Next.js frontend, modular monolith

**Performance Goals**: Transactional writes p95 under 800 ms (PRD §6). The grant transaction is
low-volume and correctness-dominated; no throughput target applies to it.

**Constraints**: Deny by default; refuse rather than degrade; every privileged transition audited;
bilingual Arabic/English with RTL/LTR at four viewport classes

**Scale/Scope**: Launch catalogue of 8–12 Courses and 100–500 Students. Invitation volume is
hand-operated — hundreds, not thousands. This is why a manual workflow is viable at all, and it is
recorded here because it stops being viable an order of magnitude up.

## Constitution Check

*GATE: evaluated against v1.1.0 before Phase 0, re-evaluated after Phase 1.*

| Principle | Gate | Status |
|---|---|---|
| I — Source documents authoritative | Spec cites PRD §11, BR-165–171, D-045, DOMAIN_MODEL §2/§4; no silent contradiction | **PASS** |
| II — Deny by default, backend-enforced | New `COURSE_ACCESS_GRANT` capability, Admin-only, added to `AllCapabilities` and the `Authorize` switch; every route enters the derived authorization sweep | **PASS** |
| III — Business-rule traceability | All 42 FRs cite BR-xxx; BR-165–171 were written before this plan | **PASS** |
| **IV — Access-Grant Correctness** | Typed `grant_source` plus audit on every grant; idempotent by invitation ID; duplicate active Entitlement **and** Enrollment blocked by database constraint; refuse-never-degrade in the approval guard; the grant-source discriminator is the single boundary | **PASS** — see §Race Resolution |
| V — Testing commensurate with risk | Tier 3. Unit, integration, and E2E, **plus mandatory concurrency tests** on every race | **PASS** |
| VI — Modular monolith, simplicity | One new package, no new infrastructure, no new service | **PASS** — see §Module Placement |
| VII — Data integrity | Four invariants as constraints; versioned migration; no destructive operation | **PASS** |
| VIII — Quality gate | Existing CI: fmt, vet on both tag sets, race tests, typecheck, lint, clean build, both guards | **PASS** |
| IX — Operational discipline | Structured logging on every transition; outbox retries already idempotent | **PASS** |
| X — Responsive, bilingual, accessible | Five screens, Arabic default, RTL/LTR, four viewport classes | **PASS** |
| XI — Documentation in sync | D-045 already reconciled the canonical docs; this slice updates contracts and the S4 spec seam | **PASS** |

**No violations. Complexity Tracking is therefore empty and that section is removed.**

## Module Placement

**Decision: a new `backend/internal/access` package** owning Course Access Invitations, Enrollments,
Entitlements, and the grant transaction.

Justified against the modular-monolith boundary rather than defaulted:

- **Not `internal/catalog`** — catalog owns the Course graph and its authoring lifecycle. Course
  access is a different aggregate with a different actor, and folding it in would make one package the
  owner of both what a Course *is* and who may *see* it.
- **Not `internal/identity`** — identity owns accounts, sessions, capabilities, and staff invitations.
  Putting course-access invitations beside staff invitations is the exact adjacency BR-171 forbids:
  two similarly named records in one package is how they get generalised into one abstraction by a
  later, well-meaning refactor.
- **`internal/access` matches the architecture's existing Entitlements module boundary**, which
  [SLICES.md §3.1](../../docs/launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation)
  already describes as owning "grant records, validity, scope evaluation, expiry, and revocation" with
  creation triggered from outside.

**Seam with S4.** S4 implements this package first and owns Entitlement evaluation. This plan states
the exact shape S6 expects S4 to have delivered, in [research.md §1](research.md#1-the-s4-seam), as a
checkable precondition rather than an assumption. **If S4 lands a different shape, this plan needs
revision before implementation** — recorded as a risk, not absorbed silently.

**Enrollment rows belong to S6; the Enrollment table does not.** Cross-artifact analysis on
2026-07-29 found S4's specification never claimed the `enrollments` table, leaving it unowned. It was
briefly assigned to S6 and **reassigned to S5** the same day: BR-116 keys Progress by `enrollment_id`
and S5 writes Progress on D5–D6, so the table must exist before S6 runs on D8.

> **S5 owns physical schema introduction required by protected learning. S6 owns enrollment lifecycle
> semantics and production mutations.**

S6 therefore **asserts** the inherited shape before writing and fails loudly if it diverges — it does
**not** create the table. S6 remains the only production writer of Enrollment rows, and its grant
transaction reuses an existing row rather than creating a second (BR-167). S4's responsibility is
Entitlement and access evaluation only.

| File | Owner | Contents |
|---|---|---|
| `internal/access/entitlement.go` | **S4** | Grant record, scope evaluation, expiry, revocation |
| `internal/access/enrollment.go` | **S6** | Enrollment **create-or-reuse** only. The table and schema are S5's |
| `internal/access/repository.go` | S4 creates, **S6 extends** | Transaction helper and lock primitives |
| `internal/access/invitation.go` | **S6** | Invitation lifecycle and state machine |
| `internal/access/grant.go` | **S6** | The approval transaction |
| `internal/httpapi/access_routes.go` | **S6** | Admin and Student routes |

## Race Resolution

The five races the specification named, each with a lock order, a designed outcome, and a
distinguishable response class. This follows the S2 D5 house pattern of naming exact races rather than
describing a locking strategy.

### Canonical lock order

```text
Course (FOR SHARE) → Course Access Invitation (FOR UPDATE) → Enrollment (FOR UPDATE / insert) → Entitlement (insert)
```

**Every transaction in this feature takes locks in this order and no other.** The Course lock is
`FOR SHARE` because no path here writes a Course; it exists to hold the Course's lifecycle state and
configured expiry instant stable for the transaction's duration. Creation validates the Course before
inserting the invitation, so it takes the same order — which is what removes the deadlock cycle a
naive invitation-then-Course order in approval would have created against it.

Every precondition is re-asserted **inside** the transaction once locks are held. A precondition
checked before `BEGIN` is a suggestion.

### The five races

| # | Race | Lock contention | Designed outcome | Response class |
|---|---|---|---|---|
| 1 | Two Admins approve the same invitation | Both on Invitation `FOR UPDATE` | First commits the grant. Second re-asserts state, sees `APPROVED`, returns the **existing** grant. Exactly one Entitlement | **200**, idempotent success — *not* 409. FR-016 requires the repeat to report the existing grant rather than fail confusingly |
| 2 | Approve races cancel | Both on Invitation `FOR UPDATE` | Whichever commits first wins. The loser re-asserts state, finds it terminal or changed, and aborts with no partial work | **409** `invitation-state-conflict`, naming the state actually found |
| 3 | Student accepts while an Admin cancels | Both on Invitation `FOR UPDATE` | Same rule as race 2. If cancel wins, acceptance is refused and the acceptance secret is already invalidated | **409** `invitation-state-conflict` |
| 4 | Two creations for the same email and Course | Partial unique index on `(normalized_email, course_id)` where state is non-terminal | The database raises a unique violation. The loser maps it to a conflict — **never** a 500 | **409** `duplicate-invitation` |
| 5 | Approval races an Admin changing the Course expiry | Approval holds Course `FOR SHARE`; the expiry writer needs `FOR UPDATE` and blocks | Approval snapshots exactly one committed value. It can never observe a torn or uncommitted instant, nor one that was rolled back | **200** — resolved to a correct snapshot, not to an error |

**A sixth race exists and the specification did not name it.** Two *different* invitations for the
same Student and Course — legal, because a rejected invitation may be followed by a new one — could
both reach approval concurrently. The partial unique index on `(student_account_id, course_id)` where
the Entitlement is active catches it, and the loser maps to **409** `already-has-active-access`.
Recorded because it is exactly the case a handler-level "does this Student already have access?"
check misses under concurrency.

### Mandatory concurrency proofs

Constitution V now requires these. Each must fail under deliberate mutation:

1. N concurrent approvals of one invitation → exactly one Entitlement, exactly one Enrollment.
2. Concurrent approve and cancel → one wins, no partial state, the loser returns 409.
3. Concurrent creation of the same pair → one row, the loser returns 409 and not 500.
4. Concurrent approvals of two invitations for the same Student and Course → exactly one Entitlement.
5. Removing the partial unique indexes must make proofs 1, 3, and 4 fail. **If they still pass, they
   were testing the handler, not the invariant.**

## Course-state outcomes at approval

The specification deferred these to planning. Each is resolved from an existing rule, not invented.

| Course state at approval | Outcome | Source |
|---|---|---|
| `ARCHIVED` | **Refuse** | Archived is terminal for catalogue discovery and new access grants (BR-018, Domain Model §3). Already fixed as FR-018 |
| `DELISTED` | **Refuse** | **BR-090 states it directly**: delisting blocks new access grants without denying existing access, and an invitation for a delisted Course cannot be approved. Derived from the rule, not from secondary wording |
| Retired | **Refuse** | Retirement blocks future acquisition (BR-027). Also self-consistent: `retirement_eligibility_at` comes from the approval instant, so a post-retirement grant would be born unable to reach retired content |
| Emergency access suspension active | **Permit** | Suspension is an access-denial overlay that never mutates Entitlements (BR-090). A grant made during it is valid and simply unusable until it lifts — symmetric with FR-040's decision for a suspended Account |
| `PUBLISHED` | Permit | The normal path |

> **Operational consequence worth the product owner's attention.** Refusing on `DELISTED` means a
> Student who paid, was invited, and accepted becomes un-grantable if an Admin delists the Course in
> between. The remedy is relist → approve → delist, which is auditable but clumsy. This follows the
> documents as they now read; changing it would be an amendment to BR-090, not a planning decision, so
> it is surfaced rather than quietly softened.

## Project Structure

### Documentation (this feature)

```text
specs/006-course-access-grant/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── course-access-api.md
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
backend/
├── internal/
│   ├── access/                       # S4 creates; S6 adds the producer
│   │   ├── entitlement.go            # S4 — evaluation, NOT touched here
│   │   ├── repository.go             # S4 creates; S6 adds lock primitives
│   │   ├── invitation.go             # S6 — lifecycle and state machine
│   │   ├── invitation_test.go        # S6
│   │   ├── enrollment.go             # S6 — Enrollment create-or-reuse (table is S5's)
│   │   ├── grant.go                  # S6 — the approval transaction
│   │   └── grant_integration_test.go # S6 — includes the concurrency proofs
│   ├── httpapi/
│   │   ├── access_routes.go          # S6 — Admin and Student routes
│   │   ├── access_routes_test.go     # S6
│   │   └── authorization_test.go     # S6 extends the derived sweep
│   ├── identity/
│   │   ├── policy.go                 # S6 adds COURSE_ACCESS_GRANT
│   │   └── policy_set.go             # S6 grants it to Admin only
│   └── db/
│       ├── migrations/
│       │   ├── NNNN_course_access_grant.up.sql   # NNNN derived at implementation
│       │   └── NNNN_course_access_grant.down.sql
│       └── schema.go                 # S6 raises MaxSchemaVersion
└── ...

frontend/
└── src/app/[locale]/
    ├── (student)/access/             # ST03 invitation, ST04 status, ST10 history
    └── (admin)/course-access/        # AD06 invitation queue, AD07 entitlement detail
```

**Structure Decision**: The existing web-application layout is used unchanged. The only structural
addition is the `internal/access` package described in §Module Placement, plus one migration pair and
two frontend route groups on the S3 shell. No new service, library, or infrastructure is introduced,
per Constitution VI.

## Migration and schema

**Number: derived from repository state at implementation time, not assumed here.** S2 T062 reserved
`0011_catalog_search` and `0012_media_and_entitlement`, and **S5 takes two migrations ahead of this
slice** — `0013_enrollments` and `0014_protected_learning`
([S5 plan](../007-protected-learning/plan.md#migration-sequence)) — which puts S6 at an expected
`0015`. But **S3's own specification states it "introduces no write path"**, so `0011` may never
exist. Hard-coding `0015` and `MaxSchemaVersion = 15` would bake in an assumption about migrations
that may not be created.

The implementation task therefore reads the highest existing migration number and takes the next one,
naming the file `NNNN_course_access_grant`. `db.MaxSchemaVersion` is raised to that derived number
through a named constant in the existing style, and CI continues to derive its assertion from
`db.MaxSchemaVersion` via the `migrate max-version` subcommand rather than carrying a literal — the
exact drift that failed S1B2's hosted CI.

The `up`/`down`/`up` lifecycle is verified against real PostgreSQL. **Every migration applied before
this one — including S5's `enrollments` and Progress migrations — is never edited** (D-031,
Constitution VII). S6 adapts to the shape it inherits; it does not amend S5's migration to suit
itself.

Full table and constraint detail is in [data-model.md](data-model.md).

## Post-Design Constitution Re-Check

Re-evaluated after the Phase 1 artifacts were written:

- **Principle IV** — all five sub-rules land as concrete artifacts. `grant_source` is a `NOT NULL`
  typed column with a `CHECK` permitting only the implemented value; idempotency is by invitation ID
  with in-transaction state re-assertion; three partial unique indexes carry the duplicate rules; the
  approval guard refuses on missing capability or stale authentication; the grant-source column is the
  single extension point and no payment column exists anywhere. **PASS.**
- **Principle VII** — every invariant in the specification maps to a named constraint in
  [data-model.md](data-model.md), not to a handler. **PASS.**
- **Principle V** — the concurrency proofs and their mutation checks are specified above and will be
  enumerated as tasks. **PASS.**
- No new violation was introduced by the design. **Complexity Tracking remains empty.**

## Risks carried into implementation

| Risk | Mitigation |
|---|---|
| **S4 lands a different `internal/access` shape than assumed** | [research.md §1](research.md#1-the-s4-seam) states the expected shape — Entitlement record, evaluator, transaction helper — as a checkable precondition. Implementation verifies it before starting and revises this plan if it differs. The Enrollment table is a separate precondition: **S5 creates it**, and S6 asserts the inherited shape rather than altering it into agreement |
| The delisted-Course refusal strands a paid Student | Surfaced above for a product decision; not softened in code |
| The manual workflow does not scale past launch volume | Recorded in Technical Context; not an engineering fix |
| Approval is a single human control | Answered by capability, recent-auth, idempotency, constraints, and audit — which is the whole reason this slice is Tier 3 |
