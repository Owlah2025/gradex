# Tasks: S6 — Course Access Invitation and Entitlement Grant

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-29

**Constitution**: v1.1.0. Principle IV — Access-Grant Correctness — governs this slice directly.
**Review Tier 3.** A builder never closes its own slice.

**Revised 2026-07-29** after `/speckit-analyze`: Enrollment ownership moved to S6 (C2) — **superseded
the same day: the `enrollments` table is S5's, and only its rows and lifecycle are S6's** (see the
banner below) — the migration
number is derived rather than assumed (M1), three uncovered requirements gained tasks (H2),
self-approval auditability gained a test (H1), three concurrency proofs gained mutation checks (M2),
and **every task now carries its FR/SC citations** (H3).

**Depends on**: S2, S4, **and S5**, all closed on independent verdicts. **T001 is an
interface-compatibility stop condition** on S4's Entitlement record and evaluator — see
[research.md §1](research.md#1-the-s4-seam).

> ## The enrollment ownership boundary
>
> **S5 owns physical schema introduction required by protected learning. S6 owns enrollment lifecycle
> semantics and production mutations.**
>
> S5 creates the `enrollments` table and creates **no row**. This slice **asserts** the inherited
> shape and fails loudly on divergence; it does **not** create, alter, or re-create the table. S6 is
> the only production writer of Enrollment rows, and its grant transaction **reuses** an existing row
> rather than creating a second (BR-167, Principle IV).
>
> S6 also owns every invitation, approval, rejection, cancellation, reissue, and revocation workflow —
> none of which exists in S5.

---

## Standing clause — applies to every task below

> **A required dependency is validated at construction and the component refuses to build without it.
> No security-relevant control may silently degrade, default, or become optional.**

Carried from the S1C closeout, where six instances of this defect class appeared in one slice:
conditional CSRF, a defaulted recent-auth window, an optional outbox intent, a hand-maintained matrix,
a context key nobody set, and an unvalidated outbox writer. If a control cannot be satisfied, the
request is **refused** — it does not proceed with less.

## Tests are required, not optional

Constitution V scales rigor to risk, and this slice contains the only access-granting path in the
product. **Every acceptance proof must fail under a deliberate mutation.** A test that passes against
broken code is not evidence — S1C proved that twice in this repository.

## Out of scope — do not touch

`backend/internal/access/entitlement.go` belongs to S4. This slice **creates** Entitlements and
**consumes** S4's evaluator. Any task that modifies evaluation, scope resolution, expiry checking, or
revocation is out of scope and is a finding, not a contribution.

**AD07 mutations belong to S8 Admin Operations, exclusively.** S6 ships the read surface only.

**SC-010 is deferred out of this slice.** Its two-minute interface time and
"Student determines state without support" are UX outcome metrics measurable only against real
operators after launch. No task claims them; this is an explicit exclusion, not an omission.

---

## Phase 1 — Setup and the S4 interface check

- [ ] T001 **STOP CONDITION.** Verify S4's interface against
      [research.md §1](research.md#1-the-s4-seam): the `entitlements` table carries scope,
      `original_access_ends_at`, `access_ends_at`, `retirement_eligibility_at`, and revocation state;
      `backend/internal/access/` exposes an evaluator and a transaction helper. Record the actual
      shape. **If it differs, halt and revise plan.md before T002.** *(The `enrollments` table is not
      part of this check — it is S5's, and T004a asserts it separately)*
- [ ] T001a **STOP CONDITION.** Verify **S5's** `enrollments` table exists with the shape
      [data-model.md §5](data-model.md#5-enrollments--created-by-s5-written-only-by-s6) declares —
      `id`, `student_account_id`, `course_id`, `created_at`, and
      `UNIQUE (student_account_id, course_id)`. **If it differs, halt and revise plan.md before
      T002.** S6 adapts to the inherited shape; it never alters S5's migration to suit itself
      *(D-031, Constitution VII)*
- [ ] T002 Add the S6 files to `backend/internal/access/` with doc comments stating the module
      boundary, that `entitlement.go` is S4-owned, and that **the `enrollments` table is S5-owned
      while its rows are S6's**: `invitation.go`, `enrollment.go`, `grant.go`
- [ ] T003 **Derive the migration number** from the highest existing file in
      `backend/internal/db/migrations/` and name the pair `NNNN_course_access_grant`. Do **not** assume
      `0015`: S5 takes two migrations ahead of this slice, but S3's specification states it introduces
      no write path, so `0011` may never exist *(M1)*
- [ ] T004 Write `backend/internal/db/migrations/NNNN_course_access_grant.up.sql` creating
      `course_access_invitations` and altering `entitlements`, with every column and constraint in
      [data-model.md](data-model.md) §2 and §4. **This migration does not create `enrollments`** —
      S5 created it *(FR-002, FR-003, FR-016, FR-019, FR-021, FR-022, FR-042)*
- [ ] T004a In the same migration, **assert** the inherited `enrollments` columns, types, nullability,
      and unique constraint exist as [data-model.md §5](data-model.md#5-enrollments--created-by-s5-written-only-by-s6)
      declares, and **fail loudly** if they diverge rather than altering the table into agreement —
      the same treatment this migration gives `entitlements` *(Principle VII)*
- [ ] T005 Write the matching `.down.sql` and verify the full `up`/`down`/`up` lifecycle against real
      PostgreSQL in `backend/internal/db/migrate_integration_test.go`
- [ ] T006 Raise `MaxSchemaVersion` to the **derived** number in `backend/internal/db/schema.go` using
      a named constant in the existing style, and confirm CI derives its assertion through
      `migrate max-version` rather than carrying a literal — the drift that failed S1B2's hosted CI
      *(M1)*
- [ ] T007 Add the new action-secret purpose and the seven audit/security event types to their closed
      allowlists by migration, following the `0007` precedent *(FR-031)*

## Phase 2 — Foundational (blocks every user story)

- [ ] T008 Add `CapCourseAccessGrant Capability = "COURSE_ACCESS_GRANT"` to
      `backend/internal/identity/policy.go`, registering it in `AllCapabilities` and the `Authorize`
      switch *(FR-001, FR-014)*
- [ ] T009 Grant `COURSE_ACCESS_GRANT` to the **Admin role only** in
      `backend/internal/identity/policy_set.go`. No Instructor and no Student grant — this is what
      makes FR-001 a property of the capability set rather than of a handler *(FR-001)*
- [ ] T010 Implement the repository with the canonical lock order from
      [plan.md](plan.md#canonical-lock-order) — Course `FOR SHARE` → Invitation `FOR UPDATE` →
      Enrollment `FOR UPDATE` → Entitlement insert — in `backend/internal/access/repository.go`. Every
      precondition is re-asserted **inside** the transaction *(FR-015, FR-016)*
- [ ] T011 Implement the invitation state machine and its guards in
      `backend/internal/access/invitation.go`, per [data-model.md §3](data-model.md#3-invitation-state-machine)
      *(FR-002, FR-024, BR-168)*
- [ ] T012 [P] Unit-test every legal and illegal transition in
      `backend/internal/access/invitation_test.go`, including that all three terminal states refuse
      every further transition *(FR-024, BR-168)*
- [ ] T013 Implement Enrollment create-or-reuse in `backend/internal/access/enrollment.go` — **S6 owns
      the rows and the lifecycle; S5 owns the table.** Reuse an existing row, never create a second
      *(FR-015, BR-167, Principle IV)*
- [ ] T014 Map PostgreSQL unique violations to their distinguishable conflict classes —
      `duplicate-invitation`, `already-has-active-access` — in
      `backend/internal/access/repository.go`. **A unique violation must never surface as a 500**
      *(FR-003, FR-016)*
- [ ] T015 Extend the derived authorization sweep in
      `backend/internal/httpapi/authorization_test.go` so every route under the new prefixes is
      asserted to carry its capability guard, deriving the route list from `r.Routes()`. A new
      unguarded route must **fail** this test *(FR-001, FR-014)*
- [ ] T016 Mutation check for T015: mount a route without its guard and confirm the sweep fails

## Phase 3 — US1: An Admin invites a Student to one Course (P1)

**Goal**: an invitation exists, is audited, is delivered — and grants nothing.
**Independent test**: create an invitation; the queue shows it, the address receives a link, and no
Enrollment or Entitlement row exists.

- [ ] T017 [US1] Implement invitation creation in `backend/internal/access/invitation.go`: bind
      normalized email, Course, creating Admin, state, and timestamps; preserve the original
      correspondence email separately (the S1B1 correction) *(FR-002, FR-005)*
- [ ] T018 [US1] Refuse creation when the target email is attached to a non-Student Account, in
      `backend/internal/access/invitation.go` *(FR-004, BR-082)*
- [ ] T019 [US1] Issue the acceptance link as an expiring, single-use, purpose-bound
      `identity_action_secrets` row, following `backend/internal/identity/invitation.go`, in
      `backend/internal/access/invitation.go` *(FR-007, BR-169)*
- [ ] T020 [US1] Co-commit the invitation-issued outbox intent using `outbox.VerificationDelivery`
      inside the creation transaction, in `backend/internal/access/invitation.go`. The intent is part
      of the invariant, not an optional extra *(FR-032)*
- [ ] T021 [US1] Mount `POST` and `GET /admin/course-access-invitations` in
      `backend/internal/httpapi/access_routes.go` with capability guard, CSRF, and strict body-limit
      binding — the S1C correction where two routes bypassed strict binding with their declared limits
      unreferenced *(FR-001, FR-038)*
- [ ] T022 [US1] Integration test in `backend/internal/httpapi/access_routes_integration_test.go`:
      creation returns `201`, writes audit and outbox rows, and creates **zero** Enrollment and
      Entitlement rows *(FR-006, FR-031, FR-032)*
- [ ] T023 [US1] Integration test: a second creation for the same pair returns
      `409 duplicate-invitation`, and the acceptance secret appears in no response body and no log
      *(FR-003)*
- [ ] T024 [US1] Mutation check for T022: remove the outbox intent and confirm the test fails
      *(FR-032)*
- [ ] T025 [P] [US1] Build the AD06 invitation queue and creation form in
      `frontend/src/app/[locale]/(admin)/course-access/`, Arabic and English *(FR-038, FR-039)*

## Phase 4 — US2: The Student accepts, and still has no access (P1)

**Goal**: prove acceptance is not a grant.
**Independent test**: accept, then request playback — still denied, denial byte-identical to a Course
never invited to.

- [ ] T026 [US2] Implement acceptance in `backend/internal/access/invitation.go`: permit only an
      authenticated Account whose normalized email equals the invitation's; refuse every other
      identity server-side *(FR-008, FR-010)*
- [ ] T027 [US2] Preserve the validated return destination across sign-in, registration, and email
      verification so an invited Student without an Account returns to acceptance, reusing the S1B3
      `returnTo` mechanism, in `backend/internal/httpapi/access_routes.go` *(FR-011)*
- [ ] T028 [US2] Mount `GET`/`POST /me/course-access-invitations` and `…/{id}/accept` in
      `backend/internal/httpapi/access_routes.go`, returning **404 for a wrong identity — never 403**
      *(FR-008, FR-009)*
- [ ] T029 [US2] Integration test: acceptance moves state to `PENDING_ADMIN_APPROVAL`, writes audit,
      and creates **zero** Enrollment and Entitlement rows *(FR-010, SC-002)*
- [ ] T030 [US2] Security integration test: a different Student, an Instructor, an Admin, and an
      unauthenticated visitor each fail to accept a valid link; every authenticated wrong-identity
      response is byte-identical to not-found *(FR-008, FR-009, SC-004)*
- [ ] T031 [US2] Integration test: an expired acceptance token returns `410` and leaves the invitation
      **unchanged and unexpired** *(FR-012, BR-169)*
- [ ] T032 [US2] Mutation check for T029: make acceptance create an Entitlement and confirm the test
      fails. **This is the single most important mutation check in the slice** *(FR-010, SC-002)*
- [ ] T033 [P] [US2] Build the ST03 acceptance screen in
      `frontend/src/app/[locale]/(student)/access/`, stating explicitly that acceptance does not grant
      access *(FR-037, FR-039)*

## Phase 5 — US3: Admin Approval creates access (P1)

**Goal**: the only grant path in the product.
**Independent test**: approve; exactly one Entitlement exists, playback succeeds, a second approval
changes nothing.

- [ ] T034 [US3] Implement the grant transaction in `backend/internal/access/grant.go` exactly as
      specified in [data-model.md §6](data-model.md#6-the-grant-transaction): canonical lock order,
      in-transaction state re-assertion, Enrollment create-or-reuse, one Entitlement with
      `grant_source`, snapshotted `original_access_ends_at`, `retirement_eligibility_at` from the
      approval instant, audit, and outbox — all in one transaction. The access-granted intent is
      raised **only after** the Entitlement row exists, never on creation, acceptance, rejection, or
      cancellation *(FR-013, FR-015, FR-019, FR-021, FR-034)*
- [ ] T035 [US3] Enforce capability **and** recent authentication on approval using
      `identity.CheckRecentAuthentication` with the configured security window, in
      `backend/internal/httpapi/access_routes.go`. Absent either, **refuse** — no default, no
      fallback, no conditional *(FR-014)*
- [ ] T036 [US3] Implement the Course-state gate from
      [plan.md](plan.md#course-state-outcomes-at-approval): refuse on archived, delisted, and retired;
      **permit** under emergency access suspension, in `backend/internal/access/grant.go`
      *(FR-018, BR-018, BR-090 as amended 2026-07-29)*
- [ ] T037 [US3] Refuse approval when the Course has no configured expiry instant or it is not in the
      future, naming the missing configuration, in `backend/internal/access/grant.go` *(FR-017)*
- [ ] T038 [US3] Mount `POST …/{id}/approve` returning **`200` with the existing grant** on a repeat,
      not `409`, in `backend/internal/httpapi/access_routes.go` *(FR-016)*
- [ ] T039 [US3] Integration test in `backend/internal/access/grant_integration_test.go`: approval
      creates exactly one Enrollment and one `ACTIVE` Entitlement with correct `grant_source`,
      `source_invitation_id`, snapshotted expiry, and approval-instant retirement eligibility
      *(FR-015, FR-019, FR-021)*
- [ ] T040 [US3] Integration test: sequential repeat approval returns `200` with the same Entitlement;
      still exactly one row *(FR-016)*
- [ ] T041 [US3] Integration test: approval without capability, and with stale authentication, each
      return `403` and leave **no** Enrollment, Entitlement, grant audit record, or notification intent
      *(FR-014, SC-005)*
- [ ] T042 [US3] Integration test: each Course state produces its outcome from T036, and a missing or
      past expiry instant returns `422` *(FR-017, FR-018)*
- [ ] T043 [US3] E2E test: after approval the Student plays a Lesson, and the authorization path reads
      the Entitlement and **never** the invitation *(FR-026, SC-007)*
- [ ] T044 [US3] Mutation check for T039: break the transaction boundary so audit commits separately,
      and confirm the test fails *(FR-015)*
- [ ] T045 [P] [US3] Build the AD07 entitlement detail **read-only** view in
      `frontend/src/app/[locale]/(admin)/course-access/`. **No expiry-adjustment or revocation
      control** — those are S8's *(FR-039)*

## Phase 6 — Concurrency proofs (mandatory, Constitution V)

Each runs under `-race` against real PostgreSQL in
`backend/internal/access/grant_concurrency_integration_test.go`. **A sequential repeat is not a
substitute for a concurrent one.** *(SC-003)*

- [ ] T046 Race 1: N concurrent approvals of one invitation → exactly one Entitlement, one Enrollment
      *(FR-016, SC-003)*
- [ ] T047 Race 2: concurrent approve and cancel → one wins, no partial state, loser returns `409`
      *(FR-024, SC-003)*
- [ ] T048 Race 3: concurrent accept and cancel → one wins, loser returns `409` *(FR-024, SC-003)*
- [ ] T049 Race 4: concurrent creation of the same pair → one row, loser returns `409` **not 500**
      *(FR-003, SC-003)*
- [ ] T050 Race 5: approval concurrent with a Course expiry change → the snapshot equals exactly one
      committed value, never torn and never rolled back *(FR-015, SC-003)*
- [ ] T051 Race 6 (**not named in the spec**, added by the plan): concurrent approval of two different
      invitations for the same Student and Course → exactly one Entitlement, loser returns
      `409 already-has-active-access` *(FR-016, SC-003)*
- [ ] T052 **Index-drop mutation check**: drop `cai_one_non_terminal_per_pair` and
      `ent_one_active_per_student_course`, then confirm **T046, T049, and T051 fail**. If they still
      pass they were testing the handler, not the invariant, and they are not evidence
- [ ] T053 **Mutation checks for the non-index-backed races** *(M2)*. T047, T048, and T050 are not
      protected by a unique index, so the index-drop mutation cannot prove them. Each carries its own
      instead, and the reason is recorded rather than assumed:
      - **T047, T048** — replace `SELECT … FOR UPDATE` on the invitation with a plain `SELECT`. Both
        proofs must fail, because the state re-assertion is then no longer serialized and both
        transitions can commit. This is the mutation that distinguishes a row lock from a read.
      - **T050** — replace the Course `FOR SHARE` lock with a plain `SELECT`. The proof must fail,
        because the expiry snapshot can then observe a value the writer rolls back.
      - Why an index cannot cover these three: they are **ordering** invariants over one row, not
        **uniqueness** invariants over a set. PostgreSQL has no constraint that expresses "these two
        transitions must not interleave", so the lock is the mechanism and removing the lock is the
        only mutation that tests it.

## Phase 7 — US4, US5, US6: rejection, status, reissue (P2, P3)

- [ ] T054 [US4] Implement rejection requiring a reason, and cancellation from either non-terminal
      state invalidating any outstanding acceptance secret, in
      `backend/internal/access/invitation.go` *(FR-022, FR-024)*
- [ ] T055 [US4] Mount `POST …/{id}/reject` and `…/{id}/cancel` with their response classes in
      `backend/internal/httpapi/access_routes.go` *(FR-022, FR-024)*
- [ ] T056 [US4] Integration test: rejection without a reason returns `422`; a new invitation for a
      previously rejected pair succeeds and the earlier record is unchanged *(FR-022, FR-023)*
- [ ] T057 [US5] Implement `GET /me/course-access` returning per-Course invitation state, timestamps,
      and access-until, in `backend/internal/httpapi/access_routes.go` *(FR-035)*
- [ ] T058 [US5] Integration test: the Student projection **excludes** `admin_note`,
      `external_reference`, `decided_by_account_id`, and all approval evidence *(FR-036)*
- [ ] T059 [P] [US5] Build the ST04 access-status and ST10 access-history screens in
      `frontend/src/app/[locale]/(student)/access/` *(FR-035, FR-039)*
- [ ] T060 [US6] Implement acceptance-link reissue superseding every prior secret and leaving state
      unchanged, refusing for an accepted or terminal invitation, in
      `backend/internal/access/invitation.go` *(FR-025)*
- [ ] T061 [US6] Integration test: the reissued link works, every prior link fails, invitation state
      and history are unchanged, and the reissue is audited *(FR-025, FR-031)*

## Phase 8 — Contract-level invariants and requirement coverage

- [ ] T062 Invariant 1: enumerate the live route table and assert **no route creates an Entitlement
      except approve**, in `backend/internal/httpapi/access_invariants_test.go`. Proven by
      enumeration, not inspection *(FR-013, FR-020, SC-006)*
- [ ] T063 Invariant 2: assert no authorization decision **implemented by S6** reads Course Access
      Invitation state — playback, protected download, and Progress write — in
      `backend/internal/httpapi/access_invariants_test.go`. **The Instructor roster is deliberately
      out of range: it ships in S8 and carries its own assertion there** *(FR-026, SC-007; L1)*
- [ ] T064 Invariant 3: assert no request or response body carries an amount, currency, payment
      status, gateway identifier, or payer instrument *(FR-005, SC-012)*
- [ ] T065 Invariant 4: enumerate every mutation and assert each writes audit evidence before its
      transaction commits, with the enumeration **failing if a new transition ships without one**;
      additionally assert the access-granted notification intent exists for approval and for **no other**
      transition *(FR-031, FR-034, SC-008)*
- [ ] T066 Invariant 5: assert every mutation carries CSRF and a referenced strict body limit
- [ ] T067 Invariant 6: assert wrong-identity access returns `404` and never `403` *(FR-009, SC-004)*
- [ ] T068 **Self-approval auditability** *(H1)*. Integration test in
      `backend/internal/access/grant_integration_test.go` proving `created_by_account_id` and
      `decided_by_account_id` are persisted as **separate** values, and that when one Admin both
      creates and approves an invitation the two columns hold the **same** account id — so
      self-approval is reconstructable after the fact even though FR-041 permits it *(FR-042, SC-013)*
- [ ] T069 **Registration grants nothing** *(H2)*. Integration test: a Student who registers, verifies
      email, and signs in reaches **zero** protected content, enumerated across every S6-reachable
      protected operation, with each denial byte-identical to a non-existent Course *(FR-028, SC-001)*
- [ ] T070 **Separation from staff invitations** *(H2)*. Test in
      `backend/internal/access/invitation_test.go` asserting `course_access_invitations` and
      `staff_invitations` share no state machine, no uniqueness rule, and no account-creation path:
      creating a course-access invitation creates no Account and assigns no role, and two concurrent
      course-access invitations for the same email on **different** Courses both succeed — which the
      global one-pending-per-email staff rule would have refused *(FR-030, BR-171)*
- [ ] T071 **Notification failure never rolls back** *(H2)*. Integration test: force outbox delivery
      to fail after commit and assert the invitation transition, the Enrollment, and the Entitlement
      all stand unchanged, and that the durable in-app record survives *(FR-033, BR-120)*
- [ ] T072 Assert every constraint in [data-model.md §8](data-model.md#8-invariant-to-constraint-map)
      exists **in the database** and that none of the eight is implemented only as a Go handler check,
      in `backend/internal/db/migrate_integration_test.go` *(FR-003, FR-016, FR-019, FR-021, FR-022,
      FR-042)*
- [ ] T073 Schema assertion: no payment-shaped column exists on `course_access_invitations`,
      `enrollments`, or `entitlements`, asserted against the live schema per quickstart Scenario 10
      *(FR-005, SC-012)*
- [ ] T074 **S6 implements no Entitlement evaluation** *(H2 follow-on)*. Assert in
      `backend/internal/httpapi/access_invariants_test.go` that no S6-authored file performs scope
      resolution, expiry comparison, or revocation checking — S6 calls S4's evaluator and duplicates
      none of it — and that `backend/internal/access/entitlement.go` is unmodified by this slice
      *(FR-027, SLICES §3.1)*

## Phase 9 — Cross-cutting and convergence

- [ ] T075 [P] Verify Arabic/English and RTL/LTR across ST03, ST04, ST10, AD06, and AD07 at phone,
      tablet, laptop, and desktop widths *(FR-039, SC-011)*
- [ ] T076 Integration test: a suspended Account holding an active Entitlement is denied every
      protected action, the Entitlement is byte-identical before, during, and after, and approval for
      a suspended Student still returns `200` *(FR-029, FR-040, SC-009)*
- [ ] T077 Update `docs/BUSINESS_RULES.md` cross-references, the API contract documents, and
      `docs/launch/STATUS.md`; record the S4 interface as verified or revised (Constitution XI — a
      behaviour change without its document update is incomplete, not done)
- [ ] T078 Run the complete gate suite from [quickstart.md](quickstart.md), including a **clean**
      frontend build with `.next` removed first, and both repository guards
- [ ] T079 Run `speckit.converge`; complete any appended work through another `speckit.implement`
      pass until convergence is clean, then push the exact head and verify hosted CI passes every job.
      Only then freeze the range for independent Tier 3 review

---

## Dependencies

```text
T001 (STOP CONDITION — S4 interface)
  └─→ Phase 1 setup (T002–T007)
        └─→ Phase 2 foundational (T008–T016)   ← blocks every user story
              ├─→ Phase 3  US1  (T017–T025)
              │     └─→ Phase 4  US2  (T026–T033)
              │           └─→ Phase 5  US3  (T034–T045)
              │                 ├─→ Phase 6  concurrency (T046–T053)
              │                 └─→ Phase 7  US4/US5/US6 (T054–T061)
              └─────────────────→ Phase 8  invariants + coverage (T062–T073)
                                    └─→ Phase 9 convergence (T075–T079)
```

US1 → US2 → US3 are genuinely sequential: each builds the state the next transitions from. US4, US5,
and US6 depend only on US3 and are independent of one another.

## Parallel opportunities

| Tasks | Why parallel |
|---|---|
| T012, T025 | Unit tests and the AD06 screen touch different files from the backend work |
| T033, T045, T059 | Three separate frontend route groups, no shared file |
| T046–T051 | Six independent concurrency tests in one file, written separately, run together |
| T062–T067 | Six invariant assertions, each independently authored |
| T069, T070, T071 | Three independent coverage tests in three different files |
| T075 | Responsive verification is independent of backend convergence |

**T052 and T053 are not parallel** — each mutates schema or lock state the other proofs depend on, and
they must run alone.

## MVP scope

**US1 + US2 + US3** (T001–T045) plus **Phase 6** (T046–T053) is the minimum that delivers a working
product: an Admin can grant access and a Student can learn. Phase 6 is **not** optional in that MVP —
an idempotency guarantee never exercised concurrently is an assumption, and Constitution V now requires
the proof.

US4, US5, and US6 make the workflow operable and supportable, but no Student is blocked from learning
without them.

## Requirement coverage

Every FR-001…FR-042 and every SC except the explicitly deferred SC-010 is cited by at least one task
above. The map is verifiable by grepping FR/SC identifiers out of this file and diffing against
[spec.md](spec.md); `/speckit-analyze` performs exactly that check.

**SC-010 is deferred by decision**, not missed — see §Out of scope.
