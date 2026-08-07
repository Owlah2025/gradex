# Tasks: S6 — Course Access Invitation and Entitlement Grant

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-29

**Constitution**: v1.1.0. Principle IV — Access-Grant Correctness — governs this slice directly.
**Review Tier 3.** A builder never closes its own slice.

**Revised 2026-07-29** after `/speckit-analyze`: Enrollment ownership moved to S6 (C2) — **superseded
the same day: the `enrollments` table is S5's, and only its rows and lifecycle are S6's** (see the
banner below) — the migration
number is derived rather than assumed (M1), three uncovered requirements gained tasks (H2),
self-approval auditability gained a test (H1), three concurrency proofs gained mutation checks (M2),
and **every requirement-bearing task carries its FR/SC citations** (H3).

> **H3's claim was overstated and is corrected 2026-08-06.** It read "every task now carries its FR/SC
> citations." Seven do not, and correctly so: `T002` creates a package, `T005` writes a down migration,
> `T016` is a mutation check on `T015`, `T077` syncs documents, `T078` runs the gate suite, `T079` runs
> convergence, and `T079a` adds a CI entry. None implements a requirement, so a citation would be
> invented rather than traced. The corrected wording is "every **requirement-bearing** task," which is
> verifiable; the original was not.

**Depends on**: S2, S4, **and S5**, all closed on independent verdicts. **T001 is an
interface-compatibility stop condition** on S4's Entitlement record and evaluator — see
[research.md §1](research.md#1-the-s4-seam).

> ## Reconciled 2026-08-06 against the implemented repository
>
> **All three dependencies are now closed**: S2 at `785d71c`, S4 at `944c0a7`, S5 at `d5ce557` on a
> Tier 3 `APPROVE`. This task list was written on 2026-07-29 against an expected repository, so its
> paths, file names, constraint names, and migration number were assumptions. They have been checked
> and corrected inline. **No task was deleted, no requirement dropped, and no proof weakened.**
>
> What changed, and why each matters to the implementer:
>
> | Was | Is | Consequence if not corrected |
> |---|---|---|
> | S4 created `internal/access`; S6 extends it | S4 created **`internal/entitlement`**; **S6 creates `internal/access`** | `T002` would edit a non-existent package |
> | Out of scope: `internal/access/entitlement.go` | Out of scope: **all of `internal/entitlement/`** | `T074` would assert a file that does not exist is unmodified — vacuously true |
> | Migration number derived, possibly not `0015` | **`0015`**, `MaxSchemaVersion` **15**. `0011_catalog_search` exists; the sequence has no gap | `T003`'s hedge is resolved; it becomes a confirmation |
> | `T004` alters `entitlements` with every §4 column and constraint | S4 **already shipped four of five**. `T004` asserts them and adds only the FK and `ent_manual_needs_invitation` | The migration would fail on duplicate object, or would edit an applied shape |
> | `T009` grants the capability in `policy_set.go` | The role map is the **`Authorize` switch in `policy.go`** | Would compile and grant nothing — a silent deny-by-default refusal, not a build error |
> | `T052` drops `ent_one_active_per_student_course` | The live index is **`entitlements_one_active_student_course`** | Dropping a non-existent index is a no-op; the mutation check would pass while proving nothing |
> | Frontend `(student)`/`(admin)` route groups | Unparenthesised `[locale]/access` and `[locale]/admin/course-access` | Would introduce a layout convention this codebase does not use |
> | `T007` widens one closed allowlist | **Two**: the purpose allowlist **and** `identity_action_secrets_account_id_purpose` | An invitation to an address with no Account would violate a CHECK at insert |
>
> **Four tasks are added**, in the existing suffixed style: `T003a` and `T007a` for the missing Course
> access-expiry column under [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it),
> `T014a` for the Student/Admin guards that must not reuse the uniform protected-learning refusal, and
> `T079a` so the new package does not join the six integration-tagged packages already outside hosted CI.
> **Total: 85 tasks.**
>
> **`T001a` is now a re-verification rather than an open question.** `0013_enrollments` matches
> [data-model.md §5](data-model.md#5-enrollments--created-by-s5-written-only-by-s6) column for column and
> constraint name for constraint name, and no production `INSERT INTO enrollments` exists. It is still run
> against the head being built, because a stop condition that is only ever satisfied on paper is not a
> stop condition.

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

**`backend/internal/entitlement/` — the whole package — belongs to S4.** This slice **creates**
Entitlements and **consumes** S4's evaluator. Any task that modifies evaluation, scope resolution,
expiry checking, or revocation is out of scope and is a finding, not a contribution.

> **Corrected 2026-08-06.** This read `backend/internal/access/entitlement.go`, which does not exist —
> S4 landed `internal/entitlement`, and `internal/access` is the package **S6 creates**. The corrected
> boundary is stronger: the producer and the evaluator are in **separate packages**, so FR-027 is provable
> by package boundary rather than by reading one package's files. Concretely, out of scope are
> `evaluate.go`, `repository.go`, `types.go`, `scope.go`, `seed_nonprod.go`,
> `production_exclusion_test.go`, and `doc.go` under `internal/entitlement/`.
>
> S6 **imports** `internal/entitlement` for its exported vocabulary — `GrantSourceManualInvitation`,
> `ScopeCourse`, `StateActive`, `Record`, and `Evaluator.EvaluateInTransaction` — and defines no parallel
> type set. A second `GrantSource` enum in `internal/access` would be the duplication FR-027 forbids, in
> Go rather than in SQL.

**Migrations `0001`–`0014` are frozen.** S6 adds `0015` and edits none of them (D-031, Constitution VII).
`scripts/docs-guard.sh` §5 enforces this against recorded checksums.

**AD07 mutations belong to S8 Admin Operations, exclusively.** S6 ships the read surface only.

**SC-010 is deferred out of this slice.** Its two-minute interface time and
"Student determines state without support" are UX outcome metrics measurable only against real
operators after launch. No task claims them; this is an explicit exclusion, not an omission.

---

## Phase 1 — Setup and the S4 interface check

- [x] T001 **STOP CONDITION.** Verify S4's interface against
      [research.md §1](research.md#1-the-s4-seam): the `entitlements` table carries scope,
      `original_access_ends_at`, `access_ends_at`, `retirement_eligibility_at`, and revocation state,
      and **`backend/internal/entitlement/`** exposes the `Evaluator`. **Reconciled 2026-08-06:** two of
      the three precondition rows hold and the third does not — there is **no transaction helper** on
      `entitlement.Repository` and none anywhere in the backend, so `internal/access` opens its own
      `pool.Begin` transaction per the house pattern. Re-run the check against the head being built and
      **halt if the recorded shape has changed since 2026-08-06** *(The `enrollments` table is not part
      of this check — it is S5's, and T001a asserts it separately)*
- [x] T001a **STOP CONDITION.** Verify **S5's** `enrollments` table exists with the shape
      [data-model.md §5](data-model.md#5-enrollments--created-by-s5-written-only-by-s6) declares —
      `id`, `student_account_id`, `course_id`, `created_at`, and
      `CONSTRAINT enr_one_per_student_course UNIQUE (student_account_id, course_id)`. **Verified
      matching on 2026-08-06**, constraint name included; re-verify against the head being built and
      **halt on divergence.** Also assert **no production `INSERT INTO enrollments` exists** — every
      one must be in a `_test.go` file or under the `!production`-tagged `cmd/e2e-seed`, which is what
      makes S6 the only production writer. S6 adapts to the inherited shape; it never alters S5's
      migration to suit itself *(D-031, Constitution VII)*
- [x] T002 Create the `backend/internal/access/` package — `doc.go`, `invitation.go`, `enrollment.go`,
      `grant.go`, `repository.go` — with a `doc.go` boundary comment stating that **evaluation lives in
      `internal/entitlement` and is not touched here**, and that **the `enrollments` table is S5-owned
      while its rows are S6's**. Follow `internal/entitlement/doc.go` and `internal/learning/doc.go`.
      **The package does not exist yet: S4 landed `internal/entitlement`, not `internal/access`**
- [x] T003 **Confirm the migration number is `0015`** and name the pair `0015_course_access_grant`.
      Recalculated 2026-08-06 from the committed schema: `0001`–`0014` with no gap,
      `0011_catalog_search` present so S3's no-write-path caveat is moot, highest pair
      `0014_protected_learning`, and `db.MaxSchemaVersion = ProtectedLearningSchemaVersion = 14`.
      Re-derive from the highest existing file and **halt if it is no longer `0014`** *(M1)*
- [x] T003a **The BR-025 Course access-expiry column does not exist.** In the same migration, add
      `courses.default_access_ends_at TIMESTAMPTZ` — **nullable**, because BR-025 makes its absence a
      refusal condition rather than an invalid state, and `NOT NULL` would require inventing a default
      duration no rule supplies. Verified 2026-08-06: no migration `0001`–`0014` creates it under this
      or any other name, so [data-model.md §6](data-model.md#6-the-grant-transaction) step 5 currently
      reads a column that is not there and **no grant could ever complete**. Assigned to S6 under
      [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it).
      Add no other column to `courses` *(BR-025, FR-015, FR-017)*
- [x] T004 Write `backend/internal/db/migrations/0015_course_access_grant.up.sql` creating
      `course_access_invitations` with every column and constraint in
      [data-model.md §2](data-model.md#2-courseaccessinvitations). On `entitlements`, **assert** the
      four elements S4 already shipped — `grant_source`, `source_invitation_id`,
      `entitlements_grant_source_implemented`, `entitlements_one_active_student_course` — and **add only**
      `fk_entitlements_source_invitation` and `ent_manual_needs_invitation`, which S4 did **not** ship
      and without which FR-021 and BR-113 are unenforced. **Do not drop, recreate, or redefine an
      applied constraint**, and **do not create `enrollments`** — S5 did *(FR-002, FR-003, FR-016,
      FR-019, FR-021, FR-022, FR-042)*
- [x] T004a In the same migration, **assert** the inherited `enrollments` columns, types, nullability,
      and unique constraint exist as [data-model.md §5](data-model.md#5-enrollments--created-by-s5-written-only-by-s6)
      declares, and **fail loudly** if they diverge rather than altering the table into agreement —
      the same treatment this migration gives `entitlements` *(Principle VII)*
- [x] T005 Write the matching `.down.sql` and verify the full `up`/`down`/`up` lifecycle against real
      PostgreSQL in `backend/internal/db/migrate_integration_test.go`. The `down` must drop only what
      `0015` created — it must **not** drop `grant_source`, `source_invitation_id`, or either S4
      constraint, and must not drop `enrollments`
- [x] T006 Add `CourseAccessGrantSchemaVersion = 15` to `backend/internal/db/schema.go` and repoint
      `MaxSchemaVersion` at it, in the existing named-constant style alongside
      `EnrollmentSchemaVersion = 13` and `ProtectedLearningSchemaVersion = 14`. **CI already derives its
      assertion** through `expected="$(go run ./cmd/migrate max-version)"` in
      `.github/workflows/ci.yml` — confirm that, do not rebuild it, and do not introduce a literal. This
      is the drift that failed S1B2's hosted CI *(M1)*
- [x] T007 Widen **two** closed allowlists by migration, following the `0007` precedent
      *(FR-007, FR-031)*:
      - `identity_action_secrets_purpose` gains `'COURSE_ACCESS_INVITATION'` alongside
        `'EMAIL_VERIFICATION'`, `'PASSWORD_RESET'`, `'STAFF_INVITATION'`.
      - **`identity_action_secrets_account_id_purpose` must gain it on the arm that permits a null
        `account_id`**, because the invited address may have no Account. Missed by the original task,
        which named only the purpose allowlist; without it every invitation to an unregistered address
        violates a CHECK at insert.
      - **The seven audit actions need no migration.** `audit_events.action` carries a *format* check,
        `CHECK (action ~ '^[A-Z][A-Z0-9_]*$')`, not a closed enumeration — which is why `T065`'s
        enumeration test is the only thing preventing an unaudited transition. Widen
        `identity_security_events_type` only if S6 records a security event.
- [x] T007a **Implement the Admin write path for the expiry instant `T003a` adds**, in
      `backend/internal/httpapi/access_routes.go` and `backend/internal/access/`, gated on
      `COURSE_ACCESS_GRANT`, with CSRF, strict body-limit binding, and an audit record like any other
      privileged Course mutation. **BR-025's conversion is part of the rule, not a choice**: when an
      Admin supplies a Kuwait-local calendar date, persist the exclusive boundary as the first instant
      of the following local day converted to UTC. Unit-test the conversion at a DST-free offset
      boundary and at month and year rollover. No backfill is implied — BR-025's "changing the Course
      default afterwards affects only future approvals" is already guaranteed by the Entitlement
      snapshot *(BR-025, FR-017; [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it))*

## Phase 2 — Foundational (blocks every user story)

- [x] T008 Add `CapCourseAccessGrant Capability = "COURSE_ACCESS_GRANT"` to the capability `const` block
      in `backend/internal/identity/policy.go` and register it in `AllCapabilities`, which currently
      holds twelve entries *(FR-001, FR-014)*. **Completed & consumed as dependency of T007a.**
- [x] T009 Grant `COURSE_ACCESS_GRANT` to the **Admin role only**, in the **`case RoleAdmin:` arm of the
      `Authorize` switch in `backend/internal/identity/policy.go`**, alongside `CapCatalogPublish`,
      `CapCatalogPricing`, and `CapCatalogTaxonomy`. No Instructor and no Student grant — this is what
      makes FR-001 a property of the capability set rather than of a handler. **Completed & consumed as dependency of T007a.**
      **Corrected 2026-08-06: not `policy_set.go`.** That file holds registration policy documents
      (`PolicyKind`, `RegistrationPolicySet`, `Locale`, `PolicySetResolver`) and contains no capability
      reference at all. An entry added there would compile and grant nothing, and `Authorize`'s
      deny-by-default fallthrough would turn the mistake into a silent refusal rather than a build
      failure *(FR-001)*
- [x] T010 Implement the repository in `backend/internal/access/repository.go` with the canonical lock
      order from [plan.md](plan.md#canonical-lock-order) — Course `FOR SHARE` → Invitation `FOR UPDATE`
      → Enrollment `FOR UPDATE` → Entitlement insert. **It owns its own transaction via
      `pool.Begin(ctx)`**, following `internal/catalog/repository.go:79`,
      `internal/identity/staff.go:49`, and `internal/learning/report.go:168`, because
      `internal/entitlement.Repository` exposes no transaction helper and nothing is added to it. Where
      in-transaction evaluation is needed, call
      `entitlement.Evaluator.EvaluateInTransaction(ctx, tx, …)`. Every precondition is re-asserted
      **inside** the transaction *(FR-015, FR-016)*
- [x] T011 Implement the invitation state machine and its guards in
      `backend/internal/access/invitation.go`, per [data-model.md §3](data-model.md#3-invitation-state-machine)
      *(FR-002, FR-024, BR-168)*
- [x] T012 [P] Unit-test every legal and illegal transition in
      `backend/internal/access/invitation_test.go`, including that all three terminal states refuse
      every further transition *(FR-024, BR-168)*
- [ ] T013 Implement Enrollment create-or-reuse in `backend/internal/access/enrollment.go` — **S6 owns
      the rows and the lifecycle; S5 owns the table.** Reuse an existing row, never create a second
      *(FR-015, BR-167, Principle IV)*
- [x] T014 Map PostgreSQL unique violations to their distinguishable conflict classes —
      `cai_one_non_terminal_per_pair` → `duplicate-invitation`,
      `entitlements_one_active_student_course` → `already-has-active-access` — in
      `backend/internal/access/repository.go`, keying on the live constraint name rather than on the
      planned one. **A unique violation must never surface as a 500** *(FR-003, FR-016)*
- [x] T014a Build the Student and Admin guards in a new
      `backend/internal/httpapi/access_foundation.go`, following `learning_foundation.go` and
      `media_foundation.go` for dependency validation. **The Student guard must not reuse
      `requireProtectedLearningAccess`** (`media_delivery_handlers.go:112`): it writes a uniform
      `writeProtectedUnavailable` refusal on every failure, which would collapse the 403/404/409/410/422
      classes in [contracts/course-access-api.md](contracts/course-access-api.md) into one response and
      defeat FR-009's byte-identical-404 by making everything byte-identical. Emit the
      `internal/problem` envelope instead. `CapLearningAccess` is still the right capability class — an
      Active Student holds it independently of any Entitlement, so an invited Student with no access
      reaches their own acceptance screen *(FR-008, FR-009, FR-014)*
- [x] T015 Extend the derived authorization sweep in the existing
      `backend/internal/httpapi/authorization_test.go` so every route under the new prefixes is
      asserted to carry its capability guard, deriving the route list from `r.Routes()`. A new
      unguarded route must **fail** this test. Note `/me` is a **new top-level prefix** — no `/me` route
      exists in the router today — so the sweep must cover it explicitly and not assume it inherits
      `/learn`'s guard *(FR-001, FR-014)*
- [ ] T016 Mutation check for T015: mount a route without its guard and confirm the sweep fails

## Phase 3 — US1: An Admin invites a Student to one Course (P1)

**Goal**: an invitation exists, is audited, is delivered — and grants nothing.
**Independent test**: create an invitation; the queue shows it, the address receives a link, and no
Enrollment or Entitlement row exists.

- [x] T017 [US1] Implement invitation creation in `backend/internal/access/invitation.go`: bind
      normalized email, Course, creating Admin, state, and timestamps; preserve the original
      correspondence email separately (the S1B1 correction) *(FR-002, FR-005)*
- [x] T018 [US1] Refuse creation when the target email is attached to a non-Student Account, in
      `backend/internal/access/invitation.go` *(FR-004, BR-082)*
- [x] T019 [US1] Issue the acceptance link as an expiring, single-use, purpose-bound
      `identity_action_secrets` row, following `backend/internal/identity/invitation.go`, in
      `backend/internal/access/invitation.go` *(FR-007, BR-169)*
- [x] T020 [US1] Co-commit the invitation-issued outbox intent using `outbox.VerificationDelivery`
      inside the creation transaction, in `backend/internal/access/invitation.go`. The intent is part
      of the invariant, not an optional extra *(FR-032)*
- [x] T021 [US1] Mount `POST` and `GET /admin/course-access-invitations` in
      `backend/internal/httpapi/access_routes.go` with capability guard, CSRF, and strict body-limit
      binding — the S1C correction where two routes bypassed strict binding with their declared limits
      unreferenced *(FR-001, FR-038)*
- [x] T022 [US1] Integration test in `backend/internal/httpapi/access_routes_integration_test.go`:
      creation returns `201`, writes audit and outbox rows, and creates **zero** Enrollment and
      Entitlement rows *(FR-006, FR-031, FR-032)*
- [x] T023 [US1] Integration test: a second creation for the same pair returns
      `409 duplicate-invitation`, and the acceptance secret appears in no response body and no log
      *(FR-003)*
- [ ] T024 [US1] Mutation check for T022: remove the outbox intent and confirm the test fails
      *(FR-032)*
- [x] T025 [P] [US1] Build the AD06 invitation queue and creation form in
      `frontend/src/app/[locale]/admin/course-access/`, Arabic and English, with shared components under
      `frontend/src/components/access/`. **Include the Course access-expiry configuration control** for
      the instant `T003a` and `T007a` introduce — without it an Admin cannot satisfy BR-025 and no
      approval can succeed. **Corrected 2026-08-06: no `(admin)` route group exists under `[locale]`**;
      the live convention is unparenthesised, as in `[locale]/admin/catalog` *(FR-038, FR-039; BR-025)*

## Phase 4 — US2: The Student accepts, and still has no access (P1)

**Goal**: prove acceptance is not a grant.
**Independent test**: accept, then request playback — still denied, denial byte-identical to a Course
never invited to.

- [x] T026 [US2] Implement acceptance in `backend/internal/access/invitation.go`: permit only an
      authenticated Account whose normalized email equals the invitation's; refuse every other
      identity server-side *(FR-008, FR-010)*
- [x] T027 [US2] Preserve the validated return destination across sign-in, registration, and email
      verification so an invited Student without an Account returns to acceptance, reusing the S1B3
      `returnTo` mechanism, in `backend/internal/httpapi/access_routes.go` *(FR-011)*
- [x] T028 [US2] Mount `GET`/`POST /me/course-access-invitations` and `…/{id}/accept` in
      `backend/internal/httpapi/access_routes.go`, returning **404 for a wrong identity — never 403**
      *(FR-008, FR-009)*
- [x] T029 [US2] Integration test: acceptance moves state to `PENDING_ADMIN_APPROVAL`, writes audit,
      and creates **zero** Enrollment and Entitlement rows *(FR-010, SC-002)*
- [x] T030 [US2] Security integration test: a different Student, an Instructor, an Admin, and an
      unauthenticated visitor each fail to accept a valid link; every authenticated wrong-identity
      response is byte-identical to not-found *(FR-008, FR-009, SC-004)*
- [x] T031 [US2] Integration test: an expired acceptance token returns `410` and leaves the invitation
      **unchanged and unexpired** *(FR-012, BR-169)*
- [ ] T032 [US2] Mutation check for T029: make acceptance create an Entitlement and confirm the test
      fails. **This is the single most important mutation check in the slice** *(FR-010, SC-002)*
- [x] T033 [P] [US2] Build the ST03 acceptance screen in
      `frontend/src/app/[locale]/access/`, stating explicitly that acceptance does not grant
      access. **Corrected 2026-08-06: no `(student)` route group exists, and this must not sit under
      `[locale]/learn/`** — `learn` is the entitled area, and an invited Student arriving from an emailed
      link holds no Entitlement yet, so nesting acceptance there would gate acceptance behind the access
      it exists to obtain *(FR-037, FR-039)*

## Phase 5 — US3: Admin Approval creates access (P1)

**Goal**: the only grant path in the product.
**Independent test**: approve; exactly one Entitlement exists, playback succeeds, a second approval
changes nothing.

- [x] T034 [US3] Implement the grant transaction in `backend/internal/access/grant.go` exactly as
      specified in [data-model.md §6](data-model.md#6-the-grant-transaction): canonical lock order,
      in-transaction state re-assertion, Enrollment create-or-reuse, one Entitlement with
      `grant_source`, snapshotted `original_access_ends_at`, `retirement_eligibility_at` from the
      approval instant, audit, and outbox — all in one transaction. The access-granted intent is
      raised **only after** the Entitlement row exists, never on creation, acceptance, rejection, or
      cancellation *(FR-013, FR-015, FR-019, FR-021, FR-034)*
- [x] T035 [US3] Enforce capability **and** recent authentication on approval using
      `identity.CheckRecentAuthentication` with the configured security window, in
      `backend/internal/httpapi/access_routes.go`. Absent either, **refuse** — no default, no
      fallback, no conditional *(FR-014)*
- [x] T036 [US3] Implement the Course-state gate from
      [plan.md](plan.md#course-state-outcomes-at-approval): refuse on archived, delisted, and retired;
      **permit** under emergency access suspension, in `backend/internal/access/grant.go`
      *(FR-018, BR-018, BR-090 as amended 2026-07-29)*
- [x] T037 [US3] Refuse approval when the Course has no configured expiry instant or it is not in the
      future, naming the missing configuration, in `backend/internal/access/grant.go`. Reads
      `courses.default_access_ends_at`, which **`T003a` creates** — this task is unimplementable before
      it, and until then every approval refuses here *(FR-017, BR-025)*
- [x] T038 [US3] Mount `POST …/{id}/approve` returning **`200` with the existing grant** on a repeat,
      not `409`, in `backend/internal/httpapi/access_routes.go` *(FR-016)*
- [x] T039 [US3] Integration test in `backend/internal/access/grant_integration_test.go`: approval
      creates exactly one Enrollment and one `ACTIVE` Entitlement with correct `grant_source`,
      `source_invitation_id`, snapshotted expiry, and approval-instant retirement eligibility
      *(FR-015, FR-019, FR-021)*
- [x] T040 [US3] Integration test: sequential repeat approval returns `200` with the same Entitlement;
      still exactly one row *(FR-016)*
- [x] T041 [US3] Integration test: approval without capability, and with stale authentication, each
      return `403` and leave **no** Enrollment, Entitlement, grant audit record, or notification intent
      *(FR-014, SC-005)*
- [x] T042 [US3] Integration test: each Course state produces its outcome from T036, and a missing or
      past expiry instant returns `422` *(FR-017, FR-018)*
- [x] T043 [US3] E2E test: after approval the Student plays a Lesson, and the authorization path reads
      the Entitlement and **never** the invitation *(FR-026, SC-007)*
- [x] T044 [US3] Mutation check for T039: break the transaction boundary so audit commits separately,
      and confirm the test fails *(FR-015)*
- [x] T045 [P] [US3] Build the AD07 entitlement detail **read-only** view in
      `frontend/src/app/[locale]/admin/course-access/`. **No expiry-adjustment or revocation
      control** — those are S8's. Adjustment history comes from `entitlement_adjustments`, which S4's
      `0012` already created as an append-only table, so the read model has a real source *(FR-039)*

## Phase 6 — Concurrency proofs (mandatory, Constitution V)

Each runs under `-race` against real PostgreSQL in
`backend/internal/access/grant_concurrency_integration_test.go`. **A sequential repeat is not a
substitute for a concurrent one.** *(SC-003)*

- [x] T046 Race 1: N concurrent approvals of one invitation → exactly one Entitlement, one Enrollment
      *(FR-016, SC-003)*
- [x] T047 Race 2: concurrent approve and cancel → one wins, no partial state, loser returns `409`
      *(FR-024, SC-003)*
- [x] T048 Race 3: concurrent accept and cancel → one wins, loser returns `409` *(FR-024, SC-003)*
- [x] T049 Race 4: concurrent creation of the same pair → one row, loser returns `409` **not 500**
      *(FR-003, SC-003)*
- [x] T050 Race 5: approval concurrent with a Course expiry change → the snapshot equals exactly one
      committed value, never torn and never rolled back *(FR-015, SC-003)*
- [x] T051 Race 6 (**not named in the spec**, added by the plan): concurrent approval of two different
      invitations for the same Student and Course → exactly one Entitlement, loser returns
      `409 already-has-active-access` *(FR-016, SC-003)*
- [x] T052 **Index-drop mutation check**: drop `cai_one_non_terminal_per_pair` and
      **`entitlements_one_active_student_course`**, then confirm **T046, T049, and T051 fail**. If they
      still pass they were testing the handler, not the invariant, and they are not evidence.
      **Corrected 2026-08-06: the second index is S4's, under S4's name.** It was planned here as
      `ent_one_active_per_student_course`, which does not exist — dropping it is a silent no-op and this
      mutation check would pass while proving nothing. The live definition is
      `UNIQUE (student_account_id, course_id) WHERE state = 'ACTIVE' AND scope_kind = 'COURSE'`; the
      `scope_kind` predicate is coextensive with S6's whole-Course-only writes
- [x] T053 **Mutation checks for the non-index-backed races** *(M2)*. T047, T048, and T050 are not
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

- [x] T054 [US4] Implement rejection requiring a reason, and cancellation from either non-terminal
      state invalidating any outstanding acceptance secret, in
      `backend/internal/access/invitation.go` *(FR-022, FR-024)*
- [x] T055 [US4] Mount `POST …/{id}/reject` and `…/{id}/cancel` with their response classes in
      `backend/internal/httpapi/access_routes.go` *(FR-022, FR-024)*
- [x] T056 [US4] Integration test: rejection without a reason returns `422`; a new invitation for a
      previously rejected pair succeeds and the earlier record is unchanged *(FR-022, FR-023)*
- [x] T057 [US5] Implement `GET /me/course-access` returning per-Course invitation state, timestamps,
      and access-until, in `backend/internal/httpapi/access_routes.go` *(FR-035)*
- [x] T058 [US5] Integration test: the Student projection **excludes** `admin_note`,
      `external_reference`, `decided_by_account_id`, and all approval evidence *(FR-036)*
- [x] T059 [P] [US5] Build the ST04 access-status and ST10 access-history screens in
      `frontend/src/app/[locale]/access/` *(FR-035, FR-039)*
- [x] T060 [US6] Implement acceptance-link reissue superseding every prior secret and leaving state
      unchanged, refusing for an accepted or terminal invitation, in
      `backend/internal/access/invitation.go` *(FR-025)*
- [x] T061 [US6] Integration test: the reissued link works, every prior link fails, invitation state
      and history are unchanged, and the reissue is audited *(FR-025, FR-031)*

## Phase 8 — Contract-level invariants and requirement coverage

- [x] T062 Invariant 1: enumerate the live route table and assert **no route creates an Entitlement
      except approve**, in `backend/internal/httpapi/access_invariants_test.go`. Proven by
      enumeration, not inspection. **Extend the existing production-exclusion precedent rather than
      inventing one**: `internal/entitlement/production_exclusion_test.go` already proves `seed_nonprod.go`
      is absent from a `-tags=production` build and that the package still builds under it, and every
      `cmd/e2e-seed` file is `//go:build !production`. This task must also assert those `cmd/e2e-seed`
      entitlement inserts stay production-excluded, so the only production creation path really is
      approval *(FR-013, FR-020, SC-006)*
- [x] T063 Invariant 2: assert no authorization decision **implemented by S6** reads Course Access
      Invitation state — playback, protected download, and Progress write — in
      `backend/internal/httpapi/access_invariants_test.go`. **The Instructor roster is deliberately
      out of range: it ships in S8 and carries its own assertion there** *(FR-026, SC-007; L1)*
- [x] T064 Invariant 3: assert no request or response body carries an amount, currency, payment
      status, gateway identifier, or payer instrument *(FR-005, SC-012)*
- [x] T065 Invariant 4: enumerate every mutation and assert each writes audit evidence before its
      transaction commits, with the enumeration **failing if a new transition ships without one**;
      additionally assert the access-granted notification intent exists for approval and for **no other**
      transition *(FR-031, FR-034, SC-008)*
- [x] T066 Invariant 5: assert every mutation carries CSRF and a referenced strict body limit
- [x] T067 Invariant 6: assert wrong-identity access returns `404` and never `403` *(FR-009, SC-004)*
- [x] T068 **Self-approval auditability** *(H1)*. Integration test in
      `backend/internal/access/grant_integration_test.go` proving `created_by_account_id` and
      `decided_by_account_id` are persisted as **separate** values, and that when one Admin both
      creates and approves an invitation the two columns hold the **same** account id — so
      self-approval is reconstructable after the fact even though FR-041 permits it *(FR-042, SC-013)*
- [x] T069 **Registration grants nothing** *(H2)*. Integration test: a Student who registers, verifies
      email, and signs in reaches **zero** protected content, enumerated across every S6-reachable
      protected operation, with each denial byte-identical to a non-existent Course *(FR-028, SC-001)*
- [x] T070 **Separation from staff invitations** *(H2)*. Test in
      `backend/internal/access/invitation_test.go` asserting `course_access_invitations` and
      `staff_invitations` share no state machine, no uniqueness rule, and no account-creation path:
      creating a course-access invitation creates no Account and assigns no role, and two concurrent
      course-access invitations for the same email on **different** Courses both succeed — which the
      global one-pending-per-email staff rule would have refused *(FR-030, BR-171)*
- [x] T071 **Notification failure never rolls back** *(H2)*. Integration test: force outbox delivery
      to fail after commit and assert the invitation transition, the Enrollment, and the Entitlement
      all stand unchanged, and that the durable in-app record survives *(FR-033, BR-120)*
- [x] T072 Assert every constraint in [data-model.md §8](data-model.md#8-invariant-to-constraint-map)
      exists **in the database** and that none of the eight is implemented only as a Go handler check,
      in `backend/internal/db/migrate_integration_test.go` *(FR-003, FR-016, FR-019, FR-021, FR-022,
      FR-042)*
- [x] T073 Schema assertion: no payment-shaped column exists on `course_access_invitations`,
      `enrollments`, or `entitlements`, asserted against the live schema per quickstart Scenario 10
      *(FR-005, SC-012)*
- [x] T074 **S6 implements no Entitlement evaluation** *(H2 follow-on)*. Assert in
      `backend/internal/httpapi/access_invariants_test.go` that no S6-authored file performs scope
      resolution, expiry comparison, or revocation checking — S6 calls S4's evaluator and duplicates
      none of it — and that **every file under `backend/internal/entitlement/` is unmodified by this
      slice**, verified by diff against the S5 closure head `d5ce557`. Additionally assert
      `internal/access` declares no `GrantSource`, `ScopeKind`, or `State` type of its own and imports
      S4's. **Corrected 2026-08-06: the path was `backend/internal/access/entitlement.go`, which does
      not exist** — asserting a non-existent file is unmodified is vacuously true and proves nothing
      *(FR-027, SLICES §3.1)*

## Phase 9 — Cross-cutting and convergence

- [ ] T075 [P] Verify Arabic/English and RTL/LTR across ST03, ST04, ST10, AD06, and AD07 at phone,
      tablet, laptop, and desktop widths *(FR-039, SC-011)*
- [x] T076 Integration test: a suspended Account holding an active Entitlement is denied every
      protected action, the Entitlement is byte-identical before, during, and after, and approval for
      a suspended Student still returns `200` *(FR-029, FR-040, SC-009)*
- [x] T077 Update `docs/BUSINESS_RULES.md` cross-references, the API contract documents, and
      `docs/launch/STATUS.md` (Constitution XI — a behaviour change without its document update is
      incomplete, not done). **The S4 interface is already recorded as verified-and-revised** by the
      2026-08-06 reconciliation in [research.md §1](research.md#1-the-s4-seam) and
      [plan.md](plan.md#module-placement); this task records the *implementation* outcome against that
      record, and must state whether the shape still held at the head that was built
- [x] T078 Run the complete gate suite from [quickstart.md](quickstart.md), including a **clean**
      frontend build with `.next` removed first, and both repository guards. `scripts/docs-guard.sh` §5
      also proves no migration `0001`–`0014` was edited
- [x] T079 Run `speckit.converge`; complete any appended work through another `speckit.implement`
      pass until convergence is clean, then push the exact head and verify hosted CI passes every job.
      Only then freeze the range for independent Tier 3 review
- [x] T079a **Add `./internal/access` to the hosted integration list** in `.github/workflows/ci.yml`, in
      the same commit that creates the package. Hosted CI currently runs `-tags=integration` against
      seven packages — `./internal/db`, `./internal/identity`, `./internal/outbox`, `./internal/httpapi`,
      `./internal/catalogpublic`, `./internal/ratelimit`, `./internal/learning` — while **thirteen**
      carry integration-tagged tests. The six already outside it are `cmd/api`, `internal/catalog`,
      `internal/db/e2equery`, `internal/entitlement`, `internal/media`, and `internal/storage`, retained
      as S5 follow-up `F-7`. S6 **does not close that gap and does not widen it**: the concurrency proofs
      in `internal/access` are the highest-risk evidence in the slice, and a proof that runs only on the
      builder's machine is the exact failure mode S5's `M-1` finding recorded when hosted CI omitted
      `./internal/learning`

---

## Dependencies

```text
D-073 acknowledged  ← product-owner gate, not a task. T003a/T007a/T037 are blocked on it
  │
T001, T001a (STOP CONDITIONS — S4 interface, S5 enrollments shape)
  └─→ Phase 1 setup (T002–T007a)
        │     migration 0015 + MaxSchemaVersion 15 land here; nothing later works without them
        └─→ Phase 2 foundational (T008–T016)   ← blocks every user story
              ├─→ Phase 3  US1  (T017–T025)
              │     └─→ Phase 4  US2  (T026–T033)
              │           └─→ Phase 5  US3  (T034–T045)
              │                 ├─→ Phase 6  concurrency (T046–T053)
              │                 └─→ Phase 7  US4/US5/US6 (T054–T061)
              └─────────────────→ Phase 8  invariants + coverage (T062–T074)
                                    └─→ Phase 9 convergence (T075–T079a)
```

US1 → US2 → US3 are genuinely sequential: each builds the state the next transitions from. US4, US5,
and US6 depend only on US3 and are independent of one another.

**Two dependency edges added by the 2026-08-06 reconciliation:**

- **`T037`, `T042`, and every approval-path proof depend on `T003a`.** They read
  `courses.default_access_ends_at`, which does not exist in the committed schema. Until `T003a` adds it,
  approval refuses at grant-transaction step 5 and no proof past `T039` can pass.
- **`T007a` should land with `T025`, not after it.** An Admin who cannot configure the expiry instant
  cannot approve anything, so a queue screen without the configuration control is a queue that can only
  reject.

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

**US1 + US2 + US3** (T001–T045, including `T001a`, `T003a`, `T004a`, `T007a`, `T014a`) plus **Phase 6**
(T046–T053) is the minimum that delivers a working product: an Admin can grant access and a Student can
learn. Phase 6 is **not** optional in that MVP — an idempotency guarantee never exercised concurrently
is an assumption, and Constitution V now requires the proof.

`T003a` and `T007a` are inside this MVP, not adjacent to it: without the Course expiry instant an Admin
cannot grant access at all, so the minimum product does not exist without them.

US4, US5, and US6 make the workflow operable and supportable, but no Student is blocked from learning
without them.

## Requirement coverage

Every FR-001…FR-042 and every SC except the explicitly deferred SC-010 is cited by at least one task
above — **42 of 42 functional requirements, 12 of 13 success criteria**. The map is verifiable by
grepping FR/SC identifiers out of this file and diffing against [spec.md](spec.md);
`/speckit-analyze` performs exactly that check.

**SC-010 is deferred by decision**, not missed — see §Out of scope.

**Task count: 85** — `T001`–`T079` plus the six suffixed tasks `T001a`, `T003a`, `T004a`, `T007a`,
`T014a`, `T079a`. The 2026-08-06 reconciliation added four of those six (`T003a`, `T007a`, `T014a`,
`T079a`); `T001a` and `T004a` came from the 2026-07-29 `/speckit-analyze` pass. **No task was removed and
no requirement lost coverage.**

**One requirement gained a task it never had.** BR-025's Kuwait-local-date to UTC exclusive-boundary
conversion is cited by FR-017 through the "configured access-expiry instant" it depends on, but no task
implemented the conversion or the column it writes to. `T003a` and `T007a` close that, under
[D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it).
It was invisible to the coverage grep because the FR was cited — by tasks that read a column nothing
created.
