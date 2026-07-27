# Tasks: S2 — Course Authoring and Review

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-28

**Builder**: Antigravity, under [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews).
**Reviewer**: Claude, Tier 2. **A builder never closes its own slice** — the self-review in S1C was
inadmissible and this rule is not waived by convenience.

**Blocked until S1C closes.** These tasks are frozen and ready; none of them starts before S1 closes
on an independent verdict.

**Tests are required, not optional.** Constitution V scales rigor to risk, and this slice contains
authorization, private-content protection, and an atomicity guarantee. Every acceptance proof must
**fail under a deliberate mutation** — a test that passes against broken code is not evidence.

---

## Standing clause — applies to every task below

> **A required dependency is validated at construction and the component refuses to build without
> it. No security-relevant control may silently degrade, default, or become optional.**

Carried from the S1C closeout, where six instances of this defect class appeared in one slice:
conditional CSRF, a defaulted recent-auth window, an optional outbox intent, a hand-maintained
matrix, a context key nobody set, and an unvalidated outbox writer. If a control cannot be satisfied,
the request is **refused** — it does not proceed with less.

---

## Phase 1 — Setup

- [ ] T001 Create the `internal/catalog` package skeleton with its doc comment stating the module
      boundary in `backend/internal/catalog/doc.go`
- [ ] T002 Write migration `backend/internal/db/migrations/0009_course_authoring.up.sql` with all
      enums, tables, and constraints from [data-model.md](data-model.md)
- [ ] T003 Write the matching `backend/internal/db/migrations/0009_course_authoring.down.sql` and
      verify the full `up`/`down`/`up` lifecycle against real PostgreSQL
- [ ] T004 Raise `db.MaxSchemaVersion` to 9 in `backend/internal/db` and confirm CI derives the
      assertion rather than carrying a literal (`CARRYOVER-S1B2-CI-DRIFT` is already fixed; verify it)

## Phase 2 — Foundational (blocks every user story)

- [ ] T005 Add `CATALOG_PUBLISH`, `CATALOG_PRICING`, and `CATALOG_TAXONOMY` to `AllCapabilities` and
      to the `Authorize` switch in `backend/internal/identity/policy.go` — Admin only
- [ ] T006 Grant the three capabilities to the Admin role only in
      `backend/internal/identity/policy_set.go`; **no Instructor grant** (this is what makes FR-013 a
      property of the capability set rather than of a handler)
- [ ] T007 Implement the single `RequireCourseOwnership` precondition in
      `backend/internal/httpapi/catalog_ownership.go` — one implementation, applied as middleware
- [ ] T008 Extend the derived authorization sweep in `backend/internal/httpapi/authorization_test.go`
      to assert **every** route under the owned-resource prefixes carries T007's middleware, deriving
      the route list from `r.Routes()` — a new unguarded route must fail this test (FR-042)
- [ ] T009 Implement the transactional repository with row-level locking in
      `backend/internal/catalog/repository.go`; every lifecycle mutation takes `SELECT … FOR UPDATE`
      on the Course row and re-asserts expected state inside the transaction
- [ ] T010 [P] Implement audit writing for the slice in `backend/internal/catalog/audit.go` using the
      existing `audit_events` table and `module = CATALOG_AND_AUTHORING` — **same transaction as the
      change**, no separate writer, no optional path
- [ ] T011 [P] Implement notification intent writing in `backend/internal/catalog/notify.go` through
      the existing outbox protected-payload boundary — **mandatory, never optional** (S1C round-two
      finding)

**Checkpoint**: capabilities exist, ownership is enforced uniformly and provably, and every write path
can produce audit and intent. No user story starts before this holds.

## Phase 3 — User Story 1: Private authoring (P1)

**Goal**: an Instructor builds a Course that nobody else can see.
**Independent test**: fill a Course completely, then fail to reach it as a Student, a second
Instructor, and anonymously — by exact identifier, on every read route.

- [ ] T012 [P] [US1] Implement the Course entity, lifecycle type, and invariants in
      `backend/internal/catalog/course.go`
- [ ] T013 [P] [US1] Implement revision, Section, and Lesson structures with explicit ordering in
      `backend/internal/catalog/revision.go`
- [ ] T014 [US1] Implement Asset Version **reference** validation in
      `backend/internal/catalog/revision.go` — refuse absent or unprocessed versions; **no upload,
      scan, or transcode path may be added anywhere in this slice** (SLICES §3.2)
- [ ] T015 [US1] Implement lesson resource and lab-material references as two distinct kinds in
      `backend/internal/catalog/revision.go` (BR-067)
- [ ] T016 [US1] Implement the single optional preview asset in `backend/internal/catalog/course.go`
      (BR-143)
- [ ] T017 [US1] Implement authoring routes and handlers per
      [contracts/authoring-api.md](contracts/authoring-api.md) in
      `backend/internal/httpapi/authoring_handlers.go` and `catalog_routes.go`
- [ ] T018 [US1] Make every non-Published read non-enumerating — a non-owner cannot distinguish "does
      not exist" from "not yours" (FR-002)
- [ ] T019 [US1] Refuse all editing by a suspended Instructor while leaving enrolled Students' access
      intact (BR-065)
- [ ] T020 [US1] Integration test: private-draft protection across **every** read route enumerated
      from the live route table, as Student, non-owning Instructor, and anonymous — quickstart
      Scenario 1
- [ ] T021 [US1] Mutation check for T020: remove the ownership precondition from one route and
      confirm the sweep fails
- [ ] T022 [P] [US1] Bilingual Instructor course-builder screens in
      `frontend/src/app/[locale]/instructor/courses/` with RTL/LTR

**Checkpoint**: authoring works and is private. This alone is a demonstrable increment.

## Phase 4 — User Story 2: Submission and review (P1)

**Goal**: only an Admin publishes, and incomplete submissions say everything that is wrong.

- [ ] T023 [US2] Implement submission validation collecting **all** failures in
      `backend/internal/catalog/validation.go` — empty Course, empty Section, missing video, and each
      unassigned taxonomy dimension, in one response (FR-009, FR-010)
- [ ] T024 [US2] Implement submission with the partial unique index making a concurrent second
      submission a constraint violation mapped to `409` — concurrency case 2
- [ ] T025 [US2] Make `PENDING_REVIEW` read-only to the Instructor via in-transaction state assertion
      (BR-016)
- [ ] T026 [US2] Implement the Admin review queue and approve/request-changes handlers per
      [contracts/review-api.md](contracts/review-api.md) in
      `backend/internal/httpapi/review_handlers.go`
- [ ] T027 [US2] Implement approval revalidation: Asset Versions present and processed **now**, owner
      not suspended, no assigned term retired — all inside the approving transaction (FR-025,
      concurrency case 3)
- [ ] T028 [US2] Implement audited Admin content preview creating **no** enrollment and **no**
      Entitlement (BR-081, FR-016)
- [ ] T029 [US2] Integration test: validation reports all three defects in one response — quickstart
      Scenario 2, with the fail-fast mutation
- [ ] T030 [US2] Integration test: Instructor cannot publish through any review route; mutation grants
      `CATALOG_PUBLISH` to Instructor and the sweep must fail — quickstart Scenario 3
- [ ] T031 [P] [US2] Bilingual Admin review-queue screens in `frontend/src/app/[locale]/admin/catalog/`

## Phase 5 — User Story 3: Revision integrity (P1)

**Goal**: the live graph is untouchable until an atomic swap.

- [ ] T032 [US3] Route every edit to a `PUBLISHED` Course into a new candidate revision; **no route
      may edit a live revision** (FR-018, BR-017)
- [ ] T033 [US3] Implement the atomic pointer swap — `live_revision_id` update, previous revision
      `SUPERSEDED`, lifecycle, audit, and intent in **one** transaction (FR-020)
- [ ] T034 [US3] Keep the approved live Resource or Lab Material serving until its replacement is
      approved (BR-066, FR-022)
- [ ] T035 [US3] Ensure rejection leaves the live Published version unchanged, distinctly from a
      first-publication change request (FR-021)
- [ ] T036 [US3] Integration test with **concurrent readers during approval**: every read returns the
      complete old graph or the complete new one, never a mixture — quickstart Scenario 4
- [ ] T037 [US3] Mutation check for T036: apply the revision outside a transaction and confirm the
      concurrent-reader assertion fails
- [ ] T038 [US3] Integration test for all four concurrency cases under `-race` against real
      PostgreSQL with genuine parallel transactions — quickstart Scenario 5

## Phase 6 — User Story 4: Admin pricing (P2)

- [ ] T039 [P] [US4] Implement append-only price changes in `backend/internal/catalog/pricing.go`;
      current price derived from the latest record, never a mutable duplicate
- [ ] T040 [US4] Implement Course and Section price routes per
      [contracts/catalog-admin-api.md](contracts/catalog-admin-api.md); Instructor has **no** write
      route
- [ ] T041 [US4] Integration test: Instructor price change refused by direct API call; no existing
      Order, Entitlement, Refund, or payout snapshot is mutated (FR-029)
- [ ] T042 [P] [US4] Admin pricing screens with audit history display

## Phase 7 — User Story 5: Lifecycle and emergency control (P2)

- [ ] T043 [US5] Implement delist, relist, retire, and archive transitions in
      `backend/internal/catalog/course.go`, refusing every transition outside BR-090's graph
- [ ] T044 [US5] Implement the deletion safeguard: refused at ≥1 enrollment with archiving offered,
      checked inside the deleting transaction — **not** an `ON DELETE CASCADE` (BR-018)
- [ ] T045 [US5] Implement retirement eligibility per BR-027 so retried or delayed payment delivery
      cannot bypass it
- [ ] T046 [US5] Implement emergency access suspension and restoration in
      `backend/internal/catalog/suspension.go` — orthogonal to lifecycle, reason mandatory, audit and
      intent in the same transaction, **no Entitlement mutated** (FR-034–FR-036)
- [ ] T047 [US5] Record the S4/S5 read obligation for live suspension checking in the S4 plan inputs —
      this is a cross-slice dependency and must not be discovered late ([research R4](research.md))
- [ ] T048 [US5] Integration test: an entitled Student keeps access through delist, relist, retire,
      and archive, and loses it **on the next request** under emergency suspension — quickstart
      Scenario 6
- [ ] T049 [US5] Mutation check for T048: make delisting deny access and confirm the delist assertion
      fails — if it passes, delisting and suspension have been conflated
- [ ] T050 [P] [US5] Admin lifecycle and emergency-suspension screens with mandatory reason capture

## Phase 8 — User Story 6: Taxonomy administration (P3)

- [ ] T051 [P] [US6] Implement term creation, rename, retirement, and deletion in
      `backend/internal/catalog/taxonomy.go` with the reference-count refusal (BR-158–BR-160)
- [ ] T052 [US6] Implement Instructor selection and Admin override routes; Instructors hold no
      mutation capability
- [ ] T053 [US6] Integration test: retired terms unassignable but preserved on Courses carrying them;
      referenced terms refuse deletion — quickstart Scenario 7
- [ ] T054 [P] [US6] Bilingual taxonomy administration screens

## Phase 9 — Carryover with a named slot

**`CARRYOVER-S1B3-VOLUNTARY-CHANGE-EVIDENCE` — undelivered for three consecutive slices.** It has
lost its place in three cut orders, which is a scheduling failure rather than a priority judgement.
It is scheduled here **before** polish, not after it, and it is not cuttable to make room for polish.

- [ ] T055 Integration test in `backend/internal/identity/password_change_integration_test.go`
      producing **observed evidence** that a voluntary password change revokes another session
      family — the recovery path is already proven, the voluntary-change path is a different code
      path and is not
- [ ] T056 Mutation check for T055: remove family revocation from the voluntary-change path and
      confirm the test fails. Without this, the test is a restatement of policy rather than evidence
- [ ] T057 Close `CARRYOVER-S1B3-VOLUNTARY-CHANGE-EVIDENCE` in
      [STATUS.md](../../docs/launch/STATUS.md) with a link to the passing test

## Phase 10 — Polish and cross-cutting

- [ ] T058 Enumerate every privileged route from the live route table and assert each writes its
      audit row — enumeration, not sampling (FR-043, quickstart Scenario 8)
- [ ] T059 Mutation check for T058: remove one audit write and confirm enumeration fails
- [ ] T060 [P] Verify bilingual Arabic/English and RTL/LTR across every new screen at tablet, laptop,
      and desktop widths (SC-009, quickstart Scenario 9)
- [ ] T061 [P] Address the S1C low finding pattern: every new assertion states the reason it fails,
      rather than failing through an incidental scan error
- [ ] T062 Update `docs/BUSINESS_RULES.md` cross-references, the API contract documents, and any
      affected decision record (Constitution XI — a behaviour change without its document update is
      incomplete, not done)
- [ ] T063 Run the complete gate suite from [quickstart.md](quickstart.md), including a **clean**
      frontend build with `.next` removed first
- [ ] T064 Push the exact head and verify hosted CI passes all five jobs **before** offering the range
      for review — S1B2 proved a green local suite is not evidence of a green CI

---

## Dependencies

```text
Setup (T001–T004)
   └─▶ Foundational (T005–T011)   ← blocks everything
          ├─▶ US1 (T012–T022)     ← MVP
          ├─▶ US2 (T023–T031)     depends on US1
          ├─▶ US3 (T032–T038)     depends on US2
          ├─▶ US4 (T039–T042)     independent of US2/US3
          ├─▶ US5 (T043–T050)     depends on US1
          └─▶ US6 (T051–T054)     independent; US2 submission needs assignable terms
   Carryover (T055–T057)          independent of all of S2 — do not let it slip again
   Polish (T058–T064)
```

## Parallel opportunities

- T010 and T011 after T009.
- Frontend tasks T022, T031, T042, T050, and T054 run alongside their backend phases.
- US4 and US6 run alongside US2/US3 — different files, no shared state.
- T055–T057 touch only the identity package and are parallel to all S2 work.

## MVP scope

**User Story 1 plus Foundational.** An Instructor can author a complete Course that provably nobody
else can see. That is a demonstrable, independently valuable increment, and it carries the slice's
security property.

## Task count

64 tasks — 4 setup, 7 foundational, 43 across six user stories, 3 carryover, 7 polish.
