# Tasks: S2 — Course Authoring and Review

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-28

**Planner/orchestrator**: Codex through `speckit.specify`, `speckit.plan`, `speckit.tasks`, and
`speckit.analyze`.
**Builder for T039–T064**: Antigravity on `gemini-3.6-flash-high`, through the repository
`speckit.implement` skill, under
[D-044](../../docs/DECISIONS.md#d-044--antigravity-completes-s2-and-claude-reviews-the-whole-feature-once).
**Reviewer**: Claude, Tier 2, once only after the whole S2 feature converges and hosted CI passes.
**A builder never closes its own slice.**

> **D-045 scope note (2026-07-28).** The MVP ships no in-platform payments; course access is granted
> by an Admin-approved Course Access Invitation. **No S2 task is cancelled or rewritten by this.**
> Course authoring, review, revision integrity, lifecycle, taxonomy, and Admin pricing are all
> unaffected — pricing is retained because the displayed Course price tells a Student what to pay
> externally. T044's enrollment-based deletion safeguard still holds, and T045's
> `retirement_eligibility_at` input changes source only. The active T043–T064 queue continues.

**Whole-S2 completion freeze**: T001–T038 and their evidence remain completed. The only unchecked
implementation program is T039–T064. Antigravity processes it sequentially as T039–T042,
T043–T050, T051–T054, T055–T057, and T058–T064. There is no interim Claude review.

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

- [x] T001 Create the `internal/catalog` package skeleton with its doc comment stating the module
      boundary in `backend/internal/catalog/doc.go`
- [x] T002 Write migration `backend/internal/db/migrations/0009_course_authoring.up.sql` with all
      enums, tables, and constraints from [data-model.md](data-model.md)
- [x] T003 Write the matching `backend/internal/db/migrations/0009_course_authoring.down.sql` and
      verify the full `up`/`down`/`up` lifecycle against real PostgreSQL
- [x] T004 Raise `db.MaxSchemaVersion` to 9 in `backend/internal/db` and confirm CI derives the
      assertion rather than carrying a literal (`CARRYOVER-S1B2-CI-DRIFT` is already fixed; verify it)

## Phase 2 — Foundational (blocks every user story)

- [x] T005 Add `CATALOG_PUBLISH`, `CATALOG_PRICING`, and `CATALOG_TAXONOMY` to `AllCapabilities` and
      to the `Authorize` switch in `backend/internal/identity/policy.go` — Admin only
- [x] T006 Grant the three capabilities to the Admin role only in
      `backend/internal/identity/policy_set.go`; **no Instructor grant** (this is what makes FR-013 a
      property of the capability set rather than of a handler)
- [x] T007 Implement the single `RequireCourseOwnership` precondition in
      `backend/internal/httpapi/catalog_ownership.go` — one implementation, applied as middleware
- [x] T008 Extend the derived authorization sweep in `backend/internal/httpapi/authorization_test.go`
      to assert **every** route under the owned-resource prefixes carries T007's middleware, deriving
      the route list from `r.Routes()` — a new unguarded route must fail this test (FR-042)
- [x] T009 Implement the transactional repository with row-level locking in
      `backend/internal/catalog/repository.go`; every lifecycle mutation takes `SELECT … FOR UPDATE`
      on the Course row and re-asserts expected state inside the transaction
- [x] T010 [P] Implement audit writing for the slice in `backend/internal/catalog/audit.go` using the
      existing `audit_events` table and `module = CATALOG_AND_AUTHORING` — **same transaction as the
      change**, no separate writer, no optional path
- [x] T011 [P] Implement notification intent writing in `backend/internal/catalog/notify.go` through
      the existing outbox protected-payload boundary — **mandatory, never optional** (S1C round-two
      finding)

**Checkpoint**: capabilities exist, ownership is enforced uniformly and provably, and every write path
can produce audit and intent. No user story starts before this holds.

## Phase 3 — User Story 1: Private authoring (P1)

**Goal**: an Instructor builds a Course that nobody else can see.
**Independent test**: fill a Course completely, then fail to reach it as a Student, a second
Instructor, and anonymously — by exact identifier, on every read route.

- [x] T012 [P] [US1] Implement the Course entity, lifecycle type, and invariants in
      `backend/internal/catalog/course.go`
- [x] T013 [P] [US1] Implement revision, Section, and Lesson structures with explicit ordering in
      `backend/internal/catalog/revision.go`
- [x] T014 [US1] Implement Asset Version **reference** validation in
      `backend/internal/catalog/revision.go` — refuse absent or unprocessed versions; **no upload,
      scan, or transcode path may be added anywhere in this slice** (SLICES §3.2)
- [x] T015 [US1] Implement lesson resource and lab-material references as two distinct kinds in
      `backend/internal/catalog/revision.go` (BR-067)
- [x] T016 [US1] Implement the single optional preview asset in `backend/internal/catalog/course.go`
      (BR-143)
- [x] T017 [US1] Implement authoring routes and handlers per
      [contracts/authoring-api.md](contracts/authoring-api.md) in
      `backend/internal/httpapi/authoring_handlers.go` and `catalog_routes.go`
- [x] T018 [US1] Make every non-Published read non-enumerating — a non-owner cannot distinguish "does
      not exist" from "not yours" (FR-002)
- [x] T019 [US1] Refuse all editing by a suspended Instructor while leaving enrolled Students' access
      intact (BR-065)
- [x] T020 [US1] Integration test: private-draft protection across **every** read route enumerated
      from the live route table, as Student, non-owning Instructor, and anonymous — quickstart
      Scenario 1
- [x] T021 [US1] Mutation check for T020: remove the ownership precondition from one route and
      confirm the sweep fails
- [x] T022 [P] [US1] Bilingual Instructor course-builder screens in
      `frontend/src/app/[locale]/instructor/courses/` with RTL/LTR

**Checkpoint**: authoring works and is private. This alone is a demonstrable increment.

## Phase 4 — User Story 2: Submission and review (P1)

**Goal**: only an Admin publishes, and incomplete submissions say everything that is wrong.

- [x] T023 [US2] Implement submission validation collecting **all** failures in
      `backend/internal/catalog/validation.go` — empty Course, empty Section, missing video, and each
      unassigned taxonomy dimension, in one response (FR-009, FR-010)
- [x] T024 [US2] Implement submission with the partial unique index making a concurrent second
      submission a constraint violation mapped to `409` — concurrency case 2
- [x] T025 [US2] Make `PENDING_REVIEW` read-only to the Instructor via in-transaction state assertion
      (BR-016)
- [x] T026 [US2] Implement the Admin review queue and approve/request-changes handlers per
      [contracts/review-api.md](contracts/review-api.md) in
      `backend/internal/httpapi/review_handlers.go`
- [x] T027 [US2] Implement approval revalidation: Asset Versions present and processed **now**, owner
      not suspended, no assigned term retired — all inside the approving transaction (FR-025,
      concurrency case 3)
- [x] T028 [US2] Implement audited Admin content preview creating **no** enrollment and **no**
      Entitlement (BR-081, FR-016)
- [x] T029 [US2] Integration test: validation reports all three defects in one response — quickstart
      Scenario 2, with the fail-fast mutation
- [x] T030 [US2] Integration test: Instructor cannot publish through any review route; mutation grants
      `CATALOG_PUBLISH` to Instructor and the sweep must fail — quickstart Scenario 3
- [x] T031 [P] [US2] Bilingual Admin review-queue screens in `frontend/src/app/[locale]/admin/catalog/`

### Completion evidence for T001–T031

| Tasks | Exact implementation range | Closure evidence |
|---|---|---|
| T001–T011 | `3d9604e..71ad368` | D3 closed after one rejection; hosted CI green at code-identical `b8b2ccf` |
| T012–T022 | `a3a1126..ae638c0` | D4 closed after two rejections; production wiring and ownership mutations reproduced independently |
| T023–T031 | `8487f93..08b8857` | D4 closed after one rejection; hosted CI run `30351429941` green on the exact head |

The checkmarks reconcile the closed historical phases; they do not erase defects proven afterward.
T002, T017, and T026 close the pre-D5 schema and contracts at their exact ranges; T032–T035 own the
explicit D5 corrections below.
D5 admits only the following corrections because T032–T038 cannot be correct while they remain:

- **D5-C01**: `SubmitCourse` currently contains an enabled `23505 → success` mutation. Restore the
  frozen `409` behavior and add a genuine parallel PostgreSQL test; the existing Course lock means
  the prior test never exercised the unique-violation branch.
- **D5-C02**: repository construction and mutation paths currently permit nil Asset Version
  validation, optional outbox writing, and ignored intent-construction/write errors. Make the
  dependencies mandatory and propagate errors on candidate submission, approval, and rejection.
- **D5-C03**: current authoring mutations resolve an editable revision implicitly. Replace that
  authority with the explicit candidate identity required by FR-048.
- **D5-C04**: cloning every Section/Lesson with a new public identity would violate BR-059 and
  destabilize BR-019's purchasable Section scope. Add stable Section/Lesson identities; clone their
  version rows while preserving identity, and allocate a new identity only for a new or explicitly
  deleted-and-recreated entity.
- **D5-C05**: the production catalogue router receives but does not apply the session-mutation
  foundation, so current authoring/review mutations omit the established origin/CSRF boundary.
  Require that foundation and the real repository at composition, then enforce mutation security
  before authorization/ownership on the exact D5 surface.

## Phase 5 — User Story 3: Revision integrity (P1)

**Goal**: the live graph is untouchable until an atomic swap.

- [x] T032 [US3] Implement explicit, atomic, idempotent candidate creation and mutation targeting
      (BR-017, BR-019, BR-059, BR-066, BR-091, BR-120, BR-122; FR-018, FR-046–FR-048):
      - add migration `0010_revision_integrity` and raise `db.MaxSchemaVersion` to 10;
      - replace the narrower pending-review index with one active-candidate index for `DRAFT`,
        `CHANGES_REQUESTED`, or `PENDING_REVIEW`, and verify the complete `up`/`down`/`up` lifecycle;
      - add same-Course `live_revision_id` and `based_on_revision_id` constraints and require a
        non-empty review reason for `REJECTED`;
      - add and backfill stable Section/Lesson identity registries and their composite ancestry
        constraints; remap the dormant Section price-history FK to stable Section identity without
        implementing pricing;
      - clone the captured live revision's version rows and references only, preserving stable
        Section/Lesson IDs while allocating new version-row/file IDs;
      - add `PUT /courses/:id/candidate`, make every mutation and submission route carry
        `:revisionId`, and remove the implicit/latest-revision mutation surface;
      - return the existing uniform denial for a cross-Course candidate/child and `409` only for an
        exact stale, terminal, live, or replaced candidate;
      - keep a Published Course lifecycle `PUBLISHED` when its candidate is submitted;
      - close D5-C01 through D5-C05 minimally;
      - add real-PostgreSQL deep-clone tests proving authored graph equality, stable identity
        preservation, new identity on delete/recreate, video replacement retaining Lesson identity,
        and zero new Asset Version, upload/object, commerce, Enrollment, Entitlement, or Progress
        rows;
      - add candidate/Section/Lesson/file membership-negative tests and the complete `401`/`403`/
        `409`/`422` response matrix;
      - extend the production composition-root route/dependency/security sweep for every exact D5
        method/path.
- [x] T033 [US3] Implement exact-candidate approval in one PostgreSQL transaction with lock order
      Course → candidate; transaction-aware owner, completeness, processed-asset, and taxonomy
      revalidation; previous revision `SUPERSEDED`; candidate `APPROVED`; `live_revision_id` swap;
      lifecycle `PUBLISHED`; mandatory audit and outbox intent; no external delivery call
      (BR-017, BR-070–BR-071, BR-090, BR-091, BR-120, BR-122; FR-020, FR-025,
      FR-050–FR-051). Lock owner, taxonomy, and Asset Version rows `FOR SHARE` in ascending ID order,
      require conflicting locks on their mutation paths, and prove serialization with real
      PostgreSQL. Bind every validator to the approving transaction. Add post-submission
      invalidation subcases for completeness, owner eligibility, taxonomy, and every asset kind;
      assert `422` with zero state/evidence change rather than `409`.
- [x] T034 [US3] Implement the production live-graph loader that captures `live_revision_id` once and
      loads Course, Section, Lesson, taxonomy, and Asset Version references using that same identity.
      Prove the approved live Resource, Lab Material, and video reference remain selected until
      approval. Wire it into the existing production-mounted owned-Course read for `live_revision`
      while loading the active candidate separately; add no Student catalogue, search, learning, or
      playback route. Record that S3/S5 must prove their future Student routes consume this loader;
      D5 proves the production repository seam and existing owner-route consumer, not those future
      routes (BR-017, BR-059, BR-066, BR-090, BR-091; FR-019, FR-022–FR-023, FR-049, FR-054).
- [x] T035 [US3] Implement exact-candidate rejection with Course → candidate lock order, mandatory
      preserved reason, audit and outbox intent in the same transaction, and no change to Course
      lifecycle, `live_revision_id`, live revision state, enrollments, Entitlements, or Student
      access. Use the canonical `COURSE_REVISION_REJECTED` audit action for a Published-Course
      candidate (BR-072, BR-090, BR-120, BR-122; FR-021, FR-052–FR-053).
- [x] T036 [US3] Add real PostgreSQL integration evidence for read and rollback atomicity:
      - concurrent readers receive a complete old graph or complete new graph, never a mixture;
      - forced failure after each load-bearing approval stage leaves pointer, revision states, audit,
        and outbox unchanged;
      - concurrent approvals cannot produce two live revisions;
      - dependency locks prevent owner/taxonomy/asset state from changing between approval
        revalidation and commit
      (BR-017, BR-090, BR-120, BR-122; FR-020, FR-025, FR-049–FR-051).
- [x] T037 [US3] Run and restore these independent mutations, recording for each what it proves and
      does not prove (BR-017, BR-019, BR-059, BR-072, BR-090, BR-120; FR-055):
      - move the `courses.live_revision_id`/lifecycle update to an auto-commit write after the
        approval transaction and inject failure at that write;
      - remove active-candidate uniqueness and prove it through a direct concurrent-insert invariant
        test using two `DRAFT` rows that does not rely on the Course lock;
      - make one live child loader select the latest revision instead of the captured
        `live_revision_id`;
      - bypass the shared approval-time revalidation call, exercising completeness, owner,
        taxonomy, and asset subcases;
      - let rejection alter the live revision, pointer, or Course lifecycle;
      - regenerate stable Section/Lesson identities during cloning.
- [x] T038 [US3] Run the exact four D5 races under `-race` against real PostgreSQL with genuine
      parallel transactions: (1) concurrent first edits, (2) concurrent approvals, (3) approval
      versus Instructor mutation, and (4) approval versus rejection. In race 1 both calls succeed
      with the same candidate and no `409`. In race 2 one approval commits and the other returns
      `409`. In race 3 approval commits and the submitted-candidate mutation returns `409` regardless
      of lock order. In race 4 exactly one terminal action commits and the other returns `409`.
      Assert no duplicate active candidate, no second approved live revision, and no contradictory
      audit/outbox evidence. Run the complete local gates and stop: do not begin T039
      (BR-016–BR-017, BR-059, BR-070–BR-072, BR-090, BR-120, BR-122; FR-046,
      FR-050–FR-053).

**D5 checkpoint**: T032–T038 complete, production-wired, mutation-proven, and offered to Claude as
one frozen exact range. No T039+ file or behavior may enter the range.

### Completion evidence for T032–T038

- Migration `0010_revision_integrity` passed the real-PostgreSQL up/down/up migration suite.
- `TestD5CandidateCloneIsDeepAtomicAndIdentityStable`,
  `TestD5ActiveCandidateUniqueIndexRejectsDirectConcurrentInserts`,
  `TestD5MutationsRefuseLiveRowsAndCrossCandidateChildren`,
  `TestD5ApprovalRollbackIsAtomicAfterEveryLoadBearingStage`,
  `TestD5ApprovalRevalidatesEveryDependencyClass`,
  `TestD5LiveReadersObserveCompleteOldOrNewGraph`,
  `TestD5ApprovalDependencyLocksSerializeConflictingWrites`,
  `TestD5PublishedCandidateRejectionPreservesLiveStateAndAccess`, and
  `TestD5ExactFourRaces` passed against real PostgreSQL under the integration race suite.
- All six independent mutations failed their named proof and were restored; see
  [d5-mutation-report.md](d5-mutation-report.md).
- Backend build, vet, unit race, and full integration race gates passed. Frontend typecheck, lint,
  tests, and clean production build passed. Documentation and exposure guards remain the final
  pre-commit gate.
- Claude Opus accepted exact range `0811ca5..3b6d752` with 0 critical/high findings after one
  rejected round and one Codex-authored security-evidence correction. Hosted CI run
  [30370633192](https://github.com/Owlah2025/gradex/actions/runs/30370633192) passed all five jobs on
  exact reviewed head `3b6d752`.
- At D5 closure, T039–T064 remained unchecked.

## Phase 6 — User Story 4: Admin pricing (P2)

- [x] T039 [P] [US4] Implement append-only price changes in `backend/internal/catalog/pricing.go`;
      current Course or stable-Section price is derived from the latest record, never a mutable
      duplicate; lock the Course, verify same-Course stable Section membership, derive `old` inside
      the transaction, append the price and mandatory audit evidence atomically, and expose a
      read-only current-price/history query
- [x] T040 [US4] Implement Course and Section price routes per
      [contracts/catalog-admin-api.md](contracts/catalog-admin-api.md) through the production
      composition root, with Origin/CSRF before `CATALOG_PRICING`; Instructor has **no** write route
      and sees current prices read-only on owned-Course reads
- [x] T041 [US4] Real-PostgreSQL/API evidence: concurrent changes serialize with an unbroken
      old→new chain; cross-Course Section IDs are refused; Instructor direct writes are refused; and
      counts/content of every existing commerce or access fixture remain unchanged (FR-026–FR-029)
- [x] T042 [P] [US4] Add bilingual Admin pricing controls and audit history to the existing Admin
      catalogue surface, plus read-only Course/Section price visibility in the Instructor builder

## Phase 7 — User Story 5: Lifecycle and emergency control (P2)

- [X] T043 [US5] Implement delist, relist, retire, archive, and Admin-only owner reassignment in
      `backend/internal/catalog/course.go`, refusing transitions outside BR-090's graph and
      revalidating an active Instructor owner under the Course lock. Reassignment preserves every
      revision, price, enrollment/access fixture, and audit history; a pending candidate remains
      explicit and cannot silently change author (FR-003, FR-030, FR-043)
- [X] T044 [US5] Implement the deletion safeguard: refused at ≥1 enrollment with archiving offered,
      checked through the existing access compatibility records inside the deleting transaction —
      **not** an `ON DELETE CASCADE`; zero-access deletion must also respect revision-owned and
      stable-identity FK order (BR-018)
- [X] T045 [US5] Persist retirement as a locked, audited `retired_at` transition and expose that
      timestamp through the production Course access-state reader. S4 remains the sole owner of
      comparing an Entitlement's `retirement_eligibility_at`, which under
      [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)
      is set from the Admin Approval instant rather than derived from an Order; update its frozen
      input so a delayed approval cannot bypass BR-027
- [X] T046 [US5] Implement emergency access suspension and restoration in
      `backend/internal/catalog/suspension.go` — orthogonal to lifecycle, reason mandatory, audit and
      intent in the same transaction, **no Entitlement mutated**; constrain suspension causes to
      legal, security, malware, or severe moderation, and require a reason for restoration
      (FR-034–FR-036)
- [X] T047 [US5] Add a mandatory production-wired Course access-state reader that resolves
      lifecycle, `retired_at`, and `access_suspended_at` live for a Lesson/Course without creating
      or mutating Entitlements. Record the S4/S5 obligation to consume this state inside their single
      entitlement decision; do not add a second permanent evaluator ([research R4](research.md))
- ~~T048~~ **[REMOVED 2026-07-29 — duplicate cross-slice obligation. Not executable in S2.]**
      Originally asked for real-PostgreSQL/API evidence that an existing Student access fixture
      survives delist/relist/retire/archive but is denied during emergency suspension. **That proof
      requires an access-decision point S2 does not own and is forbidden to build**: T047 states
      "do not add a second permanent evaluator", and
      [research R4](research.md) already assigned enforcement to S4/S5, recording the obligation as
      one that "belongs in the S4 plan". The "production compatibility access seam" this task named
      is defined in no artifact; the only access seam that exists is `AUTH_FAKE_MODE`, which
      configuration validation refuses in production and which therefore cannot yield production
      evidence.
      **Authoritative implementation and proof: S4 `T016`** — the single
      `Evaluate(student, lesson, now) → Decision`, which answers emergency suspension among its
      ordered checks. The ID is retained as a tombstone so no task is renumbered.
- ~~T049~~ **[REMOVED 2026-07-29 — duplicate cross-slice obligation. Not executable in S2.]**
      Originally the mutation check for T048. **Authoritative equivalent: S4 mutation check #7**,
      "Skip the emergency-suspension check → T016", already recorded in
      [the S4 task list](../005-media-and-entitlement-evaluation/tasks.md). No coverage is lost by
      this removal, because S4 never stopped owning it.

> **Ownership recorded, so this does not get rediscovered.** **S2 owns publishing the live Course
> suspension state** — the `access_suspended_at` transition, its audit and notification evidence, and
> the production-wired access-state reader delivered by T047. **S4 owns enforcing that state at the
> access-decision boundary**, inside its single evaluator. These are two different responsibilities in
> two different slices, and S2 must not acquire the second one. **No temporary or compatibility
> entitlement evaluator may be added to S2 to close these two IDs.**
- [X] T050 [P] [US5] Add bilingual Admin lifecycle, owner-reassignment, and
      emergency-suspension/restoration controls to the existing catalogue surface with mandatory
      reason/cause capture and explicit conflict/error display

## Phase 8 — User Story 6: Taxonomy administration (P3)

- [X] T051 [P] [US6] Implement term creation, rename, retirement, and deletion in
      `backend/internal/catalog/taxonomy.go` with locked reference-count refusal, kind/academic-code
      validation, immutable stable IDs, atomic audit evidence, and no assignment rewrite on rename
      (BR-158–BR-160)
- [X] T052 [US6] Production-wire Instructor selection on an explicit owned candidate revision and
      Admin override on any exact candidate/live Course per the frozen contracts; refuse retired or
      wrong-kind terms, preserve existing assignments to newly retired terms, and grant Instructors
      no taxonomy-mutation capability
- [X] T053 [US6] Real-PostgreSQL/API evidence: retired terms are unassignable but preserved and
      displayable on Courses carrying them; rename changes display without rewriting assignments;
      referenced deletion returns the frozen conflict/refusal semantics; unreferenced deletion
      succeeds; every mutation is authorized and audited — quickstart Scenario 7
- [X] T054 [P] [US6] Add bilingual taxonomy administration and Admin override controls to the
      existing catalogue surface, reusing the Instructor builder's localized selection vocabulary

## Phase 9 — Carryover with a named slot

**`CARRYOVER-S1B3-VOLUNTARY-CHANGE-EVIDENCE` — undelivered for three consecutive slices.** It has
lost its place in three cut orders, which is a scheduling failure rather than a priority judgement.
It is scheduled here **before** polish, not after it, and it is not cuttable to make room for polish.

- [X] T055 Integration test in `backend/internal/identity/password_change_integration_test.go`
      producing **observed evidence** that a voluntary password change revokes another session
      family — the recovery path is already proven, the voluntary-change path is a different code
      path and is not
- [X] T056 Mutation check for T055: remove family revocation from the voluntary-change path and
      confirm the test fails. Without this, the test is a restatement of policy rather than evidence
- [X] T057 Close `CARRYOVER-S1B3-VOLUNTARY-CHANGE-EVIDENCE` in
      [STATUS.md](../../docs/launch/STATUS.md) with a link to the passing test

## Phase 10 — Polish and cross-cutting

- [X] T058 Enumerate every privileged route from the live production route table and assert its
      capability, Origin/CSRF mutation boundary, required dependency wiring, and audit row —
      enumeration, not sampling. Include the pre-D5 Course-creation mutation and every pricing,
      lifecycle, ownership, suspension, taxonomy, submission, review, and preview route
      (FR-041–FR-044, quickstart Scenario 8)
- [X] T059 Mutation check for T058: remove one audit write and confirm enumeration fails
- [X] T060 [P] Verify bilingual Arabic/English and RTL/LTR across every new screen at tablet, laptop,
      and desktop widths (SC-009, quickstart Scenario 9)
- [X] T061 [P] Address the S1C low finding pattern and the accepted D5 carryovers in code touched by
      S2 completion: every new assertion states why it fails; use canonical Problem type URIs and
      `errors.Is`; remove unnecessary test assertions/hooks where a safer seam exists; and ensure
      route helpers substitute every parameter. Do not refactor unrelated code
- [X] T062 Update `docs/BUSINESS_RULES.md` cross-references, the API contract documents, and any
      affected decision record. Reconcile the Problem Details `errors`/`violations` terminology,
      record the canonical live-graph consumer obligation for S3/S5, and renumber the still-unbuilt
      downstream migrations to `0011_catalog_search` and `0012_media_and_entitlement`
      (Constitution XI — a behaviour change without its document update is incomplete, not done).
      **The D-045 scope reconciliation already updated the canonical documents on 2026-07-28**, so
      this task covers only what S2 implementation itself changes — do not re-reconcile the payment
      scope here
- [X] T063 Run the complete gate suite from [quickstart.md](quickstart.md), including a **clean**
      frontend build with `.next` removed first
- [X] T064 Run `speckit.converge`; complete any appended work through another
      `speckit.implement` pass until convergence is clean, then push the exact head and verify hosted
      CI passes all five jobs. Only then freeze `3d9604e..<final-head>` for the single whole-S2
      Claude review — S1B2 proved a green local suite is not evidence of green CI

## Phase 11: Convergence

- [X] T065 Exercise every S2 privileged mutation derived from the live production route table and
      assert its committed `audit_events` row (actor, target, action, required reason, timestamp,
      and `CATALOG_AND_AUTHORING` module) per FR-043 and SC-006 (partial).
- [X] T066 Add repeatable tablet, laptop, and desktop viewport evidence for every new taxonomy
      screen in Arabic RTL and English LTR per SC-009 (partial).

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

---

## Amendment — 2026-08-11, post-independent-review remediation (T067–T072)

**These tasks did not exist during the original S2 review and are not part of its completion record.**
Everything above closed on its own evidence and is neither reopened nor unchecked here. These six were
added on 2026-08-11 after the **first valid independent review** of the integrated launch range
`18fb7e0..48e1f3f` returned `VERDICT: REJECT`. Two of its Critical findings are owned by this spec:

- **C2 — the Admin submitted-revision inspector is absent.** The review queue is real
  ([D-082](../../docs/DECISIONS.md#d-082--the-admin-catalog-review-surface-is-backed-by-the-real-review-api-and-submitted-revision-inspection-remains-unbuilt)),
  but no component renders the submitted revision, so an Admin approves blind.
- **C3 — the Admin Lesson video preview is absent.** The backend preview route is served; no frontend
  client or UI reaches it.

Authority:
[D-084](../../docs/DECISIONS.md#d-084--the-independent-review-of-the-integrated-launch-range-returned-reject-and-bounded-remediation-of-its-seven-findings-is-authorized).
These two are implemented as one Admin journey: an Admin cannot responsibly approve what they cannot
read and cannot watch.

**The backend is not redesigned.** Both routes already exist:

```text
GET  /api/v1/admin/review/courses/:id/revisions/:revisionId
POST /api/v1/admin/review/courses/:id/revisions/:revisionId/preview/:lessonId
```

A backend change is authorized only if inspection proves an existing contract is genuinely
insufficient for the workflow below, and that proof is recorded before the change. No parallel preview
architecture is authorized.

- [ ] T067 Add the frontend Admin review-detail client call for
      `GET /api/v1/admin/review/courses/:id/revisions/:revisionId`, keyed on the exact `course_id` and
      `revision_id` carried from the queue row, with typed errors and **no demo, local or fixture
      fallback**. A detail load failure surfaces as a failure, never as empty or placeholder content.
- [ ] T068 Add the frontend Admin preview client call for
      `POST /api/v1/admin/review/courses/:id/revisions/:revisionId/preview/:lessonId`, bound to the
      inspected revision and the selected Lesson. The client must not expose storage object keys,
      bucket names, permanent S3/R2 URLs, credentials, internal signatures or upload URLs to the
      browser surface, and must not persist the returned playback material beyond the view.
- [ ] T069 Build the Admin submitted-revision inspector screen, opened from the queue with exactly
      `course_id` + `revision_id`, rendering the authoritative submitted data: Arabic Course title,
      English Course title, Arabic description, English description, study year, Major, Subject,
      revision ID and revision state, ordered Sections, ordered Lessons within each Section, and each
      Lesson's attached media and version state. Bilingual RTL/LTR as the surrounding Admin surface.
- [ ] T070 Add the protected Lesson video player to the inspector, playing the **submitted** Lesson
      media for the inspected revision through T068. It renders the media state honestly when a Lesson
      has no attached, ready media, rather than implying playable content.
- [ ] T071 Bind the decision controls to the exact inspected revision: Approve & Publish and its
      reject/return counterpart act on the `revision_id` that was inspected, and **fail closed** when
      the review detail cannot be loaded, or when the loaded detail's Course or revision does not match
      the one opened. An Admin cannot approve a revision the screen did not successfully render.
- [ ] T072 Prove the journey end to end: an Instructor creates and submits a real Course; the Admin
      queue shows it; the Admin opens it; the submitted metadata, taxonomy, Sections, Lessons and
      attached media state render; the submitted Lesson video plays; Approve & Publish publishes
      **that exact revision**. In the same suite, prove Instructor and Student roles cannot reach the
      Admin review detail or the Admin preview route.

**Amended task count:** 70 tasks — the 64 recorded above, complete, plus 6 post-review remediation
tasks, all open.
