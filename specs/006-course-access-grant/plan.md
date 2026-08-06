# Implementation Plan: S6 — Course Access Invitation and Entitlement Grant

**Branch**: `006-course-access-grant` | **Date**: 2026-07-29 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/006-course-access-grant/spec.md`

**Constitution**: v1.1.0 (amended 2026-07-29). Principle IV — Access-Grant Correctness — governs this
slice directly.

> **Reconciled 2026-08-06 against the implemented repository at the S5 closure head
> `d5ce557`.** This plan was written on 2026-07-29, before S4 and S5 were implemented, so several of
> its statements described an expected repository rather than the real one. Every correction is marked
> inline with what was assumed, what is actually there, and how it was verified. **No requirement, rule,
> or product decision changed** — only the paths, names, and numbers the plan cites, plus one
> previously unowned schema element now assigned under
> [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it).
>
> Summary of what moved: the package is `internal/access` **created by S6**, not extended from S4;
> S4's package is `internal/entitlement` and is out of scope; the migration is `0015` and
> `MaxSchemaVersion` becomes 15; `entitlements` already carries `grant_source`,
> `source_invitation_id`, the grant-source `CHECK`, and the active-uniqueness index; the role map is
> the `Authorize` switch in `policy.go`, not `policy_set.go`; the frontend route groups are
> unparenthesised; and `courses.default_access_ends_at` does not exist.

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
model, `internal/outbox` writer, `internal/catalog` repository, `internal/problem` RFC 9457 envelope,
and **`internal/entitlement`** — S4's evaluator package, consumed and never modified

**Storage**: PostgreSQL — one new migration, **`0015_course_access_grant`**, derived on 2026-08-06 from
the committed schema at the S5 closure head: the highest existing pair is `0014_protected_learning` and
`db.MaxSchemaVersion` is `ProtectedLearningSchemaVersion = 14`

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

**Decision: a new `backend/internal/access` package** owning Course Access Invitations, Enrollment
rows, and the grant transaction.

> **Reconciled 2026-08-06 against the implemented repository.** This section was written on 2026-07-29
> expecting S4 to create `internal/access` and S6 to extend it. **S4 did not.** It landed
> **`backend/internal/entitlement`** — `evaluate.go`, `repository.go`, `types.go`, `scope.go`,
> `seed_nonprod.go`, `production_exclusion_test.go` — and no `internal/access` directory exists at the
> S5 closure head. `internal/access` is therefore a **new package S6 creates**, not one it extends, and
> `internal/entitlement` stays S4's and is not modified by this slice.
>
> This is a better boundary than the one originally planned, and the correction is recorded rather than
> quietly adopted: the producer (`internal/access`) and the evaluator (`internal/entitlement`) are now
> in separate packages, so FR-027's "S6 implements no evaluation" is provable by package boundary
> instead of by reading one package's files. It also means the out-of-scope surface is the whole
> `internal/entitlement` package, not the single file `internal/access/entitlement.go` the tasks named.

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

**Seam with S4 — checked, and revised where it differed.** The precondition table in
[research.md §1](research.md#1-the-s4-seam) was verified against the committed schema and code on
2026-08-06. Two of its three rows hold; the third does not, and the plan is revised rather than the
mismatch absorbed:

| Precondition | Result |
|---|---|
| An `entitlements` table with scope, `original_access_ends_at`, effective `access_ends_at`, `retirement_eligibility_at`, and revocation state | **Holds.** `0012_media_and_entitlement.up.sql` delivers all of them, plus `grant_source`, `source_invitation_id`, `state`, and `revision` |
| An evaluator answering "may this Student reach this Lesson?" from the Entitlement alone | **Holds.** `internal/entitlement.Evaluator` — `Evaluate`, `EvaluateInTransaction`, `EvaluateTarget`, `EvaluateRead`, `EvaluateCourseReads` |
| A transaction helper on the package repository | **Does not hold.** `internal/entitlement.Repository` exposes only `readerFor(tx pgx.Tx)`; it opens no transaction. There is no package-level transaction helper anywhere in the backend |

**Resolution of the third row:** the repository house pattern is that each package's repository opens
its own transaction with `pool.Begin(ctx)` — `internal/catalog/repository.go:79`,
`internal/identity/staff.go:49`, `internal/learning/report.go:168`. S6's `internal/access` repository
follows that pattern and owns its own transaction, and calls
`entitlement.Evaluator.EvaluateInTransaction(ctx, tx, …)` where an in-transaction evaluation is needed.
Nothing is added to `internal/entitlement` to make this work, so the out-of-scope boundary holds.

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

**The inherited shape was verified on 2026-08-06 and matches exactly.**
`0013_enrollments.up.sql` creates `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`,
`student_account_id UUID NOT NULL REFERENCES accounts (id)`,
`course_id UUID NOT NULL REFERENCES courses (id)`,
`created_at TIMESTAMPTZ NOT NULL DEFAULT now()`, and
`CONSTRAINT enr_one_per_student_course UNIQUE (student_account_id, course_id)` — column for column and
constraint name for constraint name what [data-model.md §5](data-model.md#5-enrollments--created-by-s5-written-only-by-s6)
declares. **No production `INSERT INTO enrollments` exists**: every insert in the repository is in an
`_test.go` file or under `cmd/e2e-seed`, which is entirely `//go:build !production`. S6 is still the
only production writer, and `internal/learning` only ever `SELECT`s the table and keys Progress by
`enrollment_id`. `T001a` is therefore a re-verification, not an open question.

| File | Owner | Contents |
|---|---|---|
| `internal/entitlement/` (whole package) | **S4** | Entitlement record, scope evaluation, expiry, revocation. **Not modified by S6** |
| `internal/access/repository.go` | **S6 creates** | Own `pool.Begin` transaction and the lock primitives |
| `internal/access/enrollment.go` | **S6** | Enrollment **create-or-reuse** only. The table and schema are S5's |
| `internal/access/invitation.go` | **S6** | Invitation lifecycle and state machine |
| `internal/access/grant.go` | **S6** | The approval transaction |
| `internal/access/doc.go` | **S6** | The module boundary, following `internal/entitlement/doc.go` and `internal/learning/doc.go` |
| `internal/httpapi/access_foundation.go` | **S6** | Dependency validation and the Student/Admin guards, following `learning_foundation.go` and `media_foundation.go` |
| `internal/httpapi/access_routes.go` | **S6** | Admin and Student route mounting, following `catalog_routes.go` |

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

> **The index races 1 and 6 rely on already exists, under S4's name.** `0012_media_and_entitlement`
> created `entitlements_one_active_student_course` as
> `UNIQUE (student_account_id, course_id) WHERE state = 'ACTIVE' AND scope_kind = 'COURSE'`. S6 does
> **not** create it and must not create a second. Two consequences follow and both are deliberate:
>
> 1. The mutation check that drops the index must name `entitlements_one_active_student_course`, not the
>    planned `ent_one_active_per_student_course`, which does not exist.
> 2. The predicate is **narrower** than this plan assumed — it carries `AND scope_kind = 'COURSE'`, so a
>    `SECTION`-scope Entitlement is not covered. That is exactly coextensive with S6's writes, because
>    FR-015 creates only whole-Course grants and S6 creates no `SECTION` scope at all. It is recorded
>    because the invariant is narrower than its name suggests, and a later slice that grants
>    Section-scope access would inherit an uncovered duplicate path.
>
> Race 4's index, `cai_one_non_terminal_per_pair`, does not exist yet and is genuinely S6's to create.

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
│   ├── entitlement/                  # S4 — evaluation. NOT touched by S6
│   ├── access/                       # S6 creates the whole package
│   │   ├── doc.go                    # module boundary
│   │   ├── repository.go             # own pool.Begin transaction + lock primitives
│   │   ├── invitation.go             # lifecycle and state machine
│   │   ├── invitation_test.go
│   │   ├── enrollment.go             # Enrollment create-or-reuse (table is S5's)
│   │   ├── grant.go                  # the approval transaction
│   │   ├── grant_integration_test.go # includes the mutation checks
│   │   └── grant_concurrency_integration_test.go  # the six concurrency proofs
│   ├── httpapi/
│   │   ├── access_foundation.go      # S6 — dependency validation and guards
│   │   ├── access_routes.go          # S6 — Admin and Student routes
│   │   ├── access_routes_test.go     # S6
│   │   ├── access_invariants_test.go # S6 — the six contract-level invariants
│   │   └── authorization_test.go     # exists; S6 extends the derived sweep
│   ├── identity/
│   │   └── policy.go                 # S6 adds COURSE_ACCESS_GRANT to the const
│   │                                 #   block, AllCapabilities, and the RoleAdmin
│   │                                 #   arm of the Authorize switch
│   └── db/
│       ├── migrations/
│       │   ├── 0015_course_access_grant.up.sql
│       │   └── 0015_course_access_grant.down.sql
│       └── schema.go                 # CourseAccessGrantSchemaVersion = 15
└── ...

frontend/
└── src/app/[locale]/
    ├── access/                       # ST03 invitation, ST04 status, ST10 history
    └── admin/course-access/          # AD06 invitation queue, AD07 entitlement detail
```

> **Reconciled 2026-08-06.** Three path corrections, each verified against the repository:
>
> - **`policy_set.go` is not the role map.** It holds registration policy documents — `PolicyKind`,
>   `RegistrationPolicySet`, locales. Role-to-capability grants live in the `Authorize` switch in
>   `policy.go`, in the `case RoleAdmin:` arm alongside `CapCatalogPublish`, `CapCatalogPricing`, and
>   `CapCatalogTaxonomy`. Adding a capability to `policy_set.go` would compile and grant nothing.
> - **No `(student)` or `(admin)` route group exists under `[locale]`.** The live convention is
>   unparenthesised: `[locale]/admin/catalog`, `[locale]/catalog`, `[locale]/instructor`,
>   `[locale]/learn`. Parenthesised groups exist only at `src/app/(auth)/`. The corrected paths follow
>   the live convention.
> - **ST03/ST04/ST10 sit at `[locale]/access`, not under `[locale]/learn`.** `learn` is the entitled
>   area; an invited Student arriving from an emailed link holds no Entitlement yet, so placing the
>   acceptance surface inside the learning area would gate acceptance behind the access it grants.

**Structure Decision**: The existing web-application layout is used unchanged. The only structural
addition is the `internal/access` package described in §Module Placement, plus one migration pair and
two frontend route directories on the S3 shell. No new service, library, or infrastructure is
introduced, per Constitution VI.

## Migration and schema

**Number: `0015`, recalculated on 2026-08-06 from the committed schema rather than from the planned
guess.** The uncertainty this section was written to protect against is resolved. `0011_catalog_search`
**does** exist — S2 T062's reservation was honoured, so S3's "introduces no write path" caveat is moot
and no number in the sequence is missing. The committed migration directory at the S5 closure head runs
`0001`–`0014` with no gap, ending at `0013_enrollments` and `0014_protected_learning`, and
`backend/internal/db/schema.go` reads:

```go
EnrollmentSchemaVersion        = 13
ProtectedLearningSchemaVersion = 14
MaxSchemaVersion               = ProtectedLearningSchemaVersion
```

S6 therefore takes **`0015_course_access_grant`** and adds
`CourseAccessGrantSchemaVersion = 15`, repointing `MaxSchemaVersion` at it in the existing named-constant
style. The number is now a verified fact rather than an assumption, so `T003` becomes a confirmation
step: re-derive it at implementation time and halt if the highest committed migration is no longer
`0014`.

**CI already derives its assertion and needs no change.** `.github/workflows/ci.yml` runs
`expected="$(go run ./cmd/migrate max-version)"` rather than carrying a literal, with a comment naming
the S1B2 drift it exists to prevent. `T006`'s CI clause is satisfied by existing infrastructure and only
needs re-confirming, not building.

The `up`/`down`/`up` lifecycle is verified against real PostgreSQL. **Every migration applied before
this one — including S5's `enrollments` and Progress migrations — is never edited** (D-031,
Constitution VII). S6 adapts to the shape it inherits; it does not amend S5's migration to suit
itself.

Full table and constraint detail is in [data-model.md](data-model.md).

## Post-Design Constitution Re-Check

Re-evaluated after the Phase 1 artifacts were written:

- **Principle IV** — all five sub-rules land as concrete artifacts. `grant_source` is a `NOT NULL`
  typed column with a `CHECK` permitting only the implemented value; idempotency is by invitation ID
  with in-transaction state re-assertion; three unique indexes carry the duplicate rules — of which
  **two already exist** (`entitlements_one_active_student_course` from S4's `0012`,
  `enr_one_per_student_course` from S5's `0013`) and **one is S6's to create**
  (`cai_one_non_terminal_per_pair`); the approval guard refuses on missing capability or stale
  authentication; the grant-source column is the single extension point and no payment column exists
  anywhere. **PASS** — and stronger than planned, because two of the three invariants were already
  proven by closed slices rather than being introduced with the code that depends on them.
- **Principle VII** — every invariant in the specification maps to a named constraint in
  [data-model.md](data-model.md), not to a handler. **PASS.**
- **Principle V** — the concurrency proofs and their mutation checks are specified above and will be
  enumerated as tasks. **PASS.**
- No new violation was introduced by the design. **Complexity Tracking remains empty.**

## Risks carried into implementation

| Risk | Mitigation |
|---|---|
| ~~**S4 lands a different `internal/access` shape than assumed**~~ | **Materialised and resolved 2026-08-06.** S4 landed `internal/entitlement`, not `internal/access`, and shipped no transaction helper. §Module Placement and §Seam with S4 above record the actual shape and the revision; `internal/access` is now S6's to create and `internal/entitlement` is out of scope |
| **BR-025's Course access-expiry column does not exist** | **Unresolved blocker.** No migration `0001`–`0014` creates `courses.default_access_ends_at`, so FR-017 would refuse every approval and FR-015 would never run. Assigned to S6 under [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it), which also surfaces the effort consequence. **Implementation does not begin on the grant path until this is acknowledged** |
| The delisted-Course refusal strands a paid Student | Surfaced above for a product decision; not softened in code |
| The manual workflow does not scale past launch volume | Recorded in Technical Context; not an engineering fix |
| Approval is a single human control | Answered by capability, recent-auth, idempotency, constraints, and audit — which is the whole reason this slice is Tier 3 |
| The one-active-Entitlement index is narrower than its name | `entitlements_one_active_student_course` carries `AND scope_kind = 'COURSE'`. Coextensive with S6's writes, which are whole-Course only; recorded in §Race Resolution so a later Section-scope grant does not inherit an uncovered duplicate path |
| S6's new `internal/access` package lands outside hosted CI | Six of thirteen integration-tagged packages already run only locally (S5 `F-7`). `internal/access` is added to the hosted integration list in the same commit that creates the package, so the gap does not widen |
