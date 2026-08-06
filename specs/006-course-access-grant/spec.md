# Feature Specification: S6 — Course Access Invitation and Entitlement Grant

**Feature Branch**: `feature/006-course-access-grant`

**Created**: 2026-07-29

**Status**: Draft — clarifications resolved and `/speckit-analyze` remediations applied 2026-07-29; planned and tasked

**Input**: S6 — the complete manual course-access workflow created by
[D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation):
Admin-created Course Access Invitation, identity-bound Student acceptance, Admin Approval as the sole
grant trigger, idempotent Enrollment and Entitlement creation, rejection and cancellation, audit and
notification evidence, and bilingual screens for both actors.

**Depends on**: S2 (Course graph and lifecycle), S4 (the Entitlement record and its evaluation), and
**S5** (the physical `enrollments` table). **S6 implementation does not begin until all three close** on
independent verdicts. **All three are now closed** — S2 at `785d71c`, S4 at `944c0a7`, S5 at `d5ce557`.

> **Corrected 2026-08-06 on two points, neither of which changes a requirement.**
>
> **S5 was missing from this line.** It read "S2 … and S4 … until **both** close," while
> [SLICES.md §2](../../docs/launch/SLICES.md#2-slice-order) records S6's dependencies as `S2, S4, S5`
> and `tasks.md` already said all three. S5 introduces the `enrollments` table S6 writes to
> ([§3.4](../../docs/launch/SLICES.md#34-s5-introduces-the-enrollments-table-s6-owns-every-enrollment-write)),
> so it was always a dependency; only this line omitted it.
>
> **S2 does not supply the configured access-expiry instant.** This line credited S2 with it. Verified
> against the committed schema: no migration `0001`–`0014` creates `courses.default_access_ends_at`, and
> S2 closed without it. BR-025 requires it before any approval, so it is now S6's under
> [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it).
> What S6 reads from S2 is the Course graph and lifecycle state only.

**Effort**: 9h **as originally scoped**. **Review Tier 3** — shared only with S1C, S4, and S5.

> D-073 adds a column, a validated Admin write path carrying BR-025's Kuwait-local-date to UTC
> exclusive-boundary conversion, its audit evidence, and an Admin configuration control. The 9h figure
> assumed the expiry instant was inherited and does not include that work. The revised figure is for the
> product owner to set, not for this document to assume.

**Governing rules**: BR-007, BR-018, BR-020, BR-021, BR-023, BR-024, BR-025, BR-026, BR-027, BR-028,
BR-029, BR-082, BR-090, BR-113, BR-120, BR-121, BR-122, BR-123, BR-165, BR-166, BR-167, BR-168,
BR-169, BR-170, BR-171. Traceability is carried per requirement, per Constitution Principle III.

---

## The three boundaries this slice exists to hold

Everything else in S6 is workflow plumbing. These three are why it is Tier 3.

### 1. Admin Approval is the only thing between a registered account and paid content

MVP has no payment gateway, so there is no cryptographic verification anywhere in the access path.
The control that used to be *"a verified capture callback grants access"* is now *"an authorised Admin
approved a request."* **That is a human control, and it is the whole control.**

It therefore carries the depth the payment slice would have carried: a distinct capability, required
recent authentication, idempotency under repetition and concurrency, immutable audit on every
transition, and refusal — never degradation — when a precondition is absent.

Removing in-platform payments reduced scope. It did not reduce risk in this slice, and the plan must
not treat a smaller slice as a lower-assurance one.

### 2. The Invitation is a workflow record. The Entitlement is the access record.

These are two different objects with two different jobs, and **no protected operation may ever read
the Invitation.** Playback authorization, protected downloads, Progress writes, and the Instructor
roster authorise against the Entitlement alone *(BR-029)*.

The failure this prevents is specific: if any authorization path learns to ask "is there an accepted
invitation?", then acceptance silently becomes a grant and the approval step becomes decorative.

### 3. S6 creates Entitlements. It must never evaluate them.

The mirror of S4's boundary.
[SLICES.md §3.1](../../docs/launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation)
split evaluation from creation precisely so the consumer could be proven before the producer existed.
S4 delivers scope evaluation, expiry, and revocation. **S6 adds exactly one thing: the transaction
that creates the record.** It introduces no second evaluator, no parallel access check, and no
"can this Student watch?" helper of its own.

That split is also why D-045 was cheap — only the producer changed. Re-implementing evaluation here
would destroy the property that made the scope change survivable.

---

## Scope Boundaries

| S6 owns | S6 must not acquire |
|---|---|
| The Course Access Invitation record and its full lifecycle | Account-creation or role-assignment semantics — that is the staff Invitation, S1C |
| The transaction that creates Enrollment and Entitlement | Entitlement **evaluation**, scope resolution, expiry checking, revocation — S4 |
| Admin invitation administration and the approval queue | Course lifecycle, pricing, publication — S2 |
| Student acceptance and access-status surfaces | The player, downloads, progress — S4/S5 |
| Audit and notification intents for its own transitions | The email delivery adapter itself — S9 |
| Bilingual screens for both actors on the S3 shell | The shell, catalogue, or Course detail page — S3 |

**Course Access Invitations are a separate workflow from staff invitations** *(BR-171)*. The existing
staff invitation creates an Account and assigns a role, enforces one pending invitation per email
**globally**, and uses consumed/superseded semantics. A Course Access Invitation creates no Account,
assigns no role, and must permit concurrent invitations for the same person across different Courses.
Generalising the two into one abstraction is a defect, not a simplification.

**No payment surface is introduced anywhere.** No provider, checkout-session, or callback record, and
no amount, currency, payment status, gateway identifier, or payer instrument field on any entity
*(BR-020)*.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 — An Admin invites a Student to one Course (Priority: P1)

An Admin has confirmed, entirely outside Gradex, that a Student paid for a Course. In the admin area
they create a Course Access Invitation for that Student's email address and that one Course,
optionally recording a private note and an external reference for their own reconciliation. The
Student receives an email inviting them to claim access.

**Why this priority**: it is the entry point of the only access path in the product. Without it no
Student can reach paid content by any means.

**Independent Test**: create an invitation and verify it appears in the awaiting-acceptance queue,
the invited address received an acceptance link, an audit record was written, and — the point of the
story — **no Enrollment and no Entitlement exist** and the Student can reach nothing.

**Acceptance Scenarios**:

1. **Given** an Admin holding the course-access capability with valid recent authentication,
   **when** they create an invitation for one email and one published Course, **then** the invitation
   records the email, Course, creating Admin, state, and creation timestamp, an audit record is
   written, an acceptance link is issued to that address, and **no access record of any kind is
   created**.
2. **Given** the same email and Course already have a non-terminal invitation, **when** a second
   creation is attempted, **then** it is refused and no duplicate exists.
3. **Given** an Admin without the course-access capability, or with stale authentication, **when**
   creation is attempted, **then** it is refused — not queued, not partially applied.
4. **Given** an email attached to an Instructor or Admin Account, **when** creation is attempted,
   **then** it is refused, because only a Student Account may hold course access.
5. **Given** an invitation carrying a note and an external reference, **when** any Student-facing or
   Instructor-facing surface is inspected, **then** neither value appears anywhere in it.

---

### User Story 2 — The invited Student accepts, and still has no access (Priority: P1)

The Student opens the acceptance link, signs in or registers with the invited address, sees which
Course the invitation is for and the access period that would apply, and accepts. The screen states
plainly that access is not yet active and is awaiting review.

**Why this priority**: this is the step most likely to be implemented as an accidental grant. Proving
it grants nothing is the point of the story.

**Independent Test**: accept an invitation and verify the state moved to awaiting-approval, the Admin
queue shows it, and the Student still reaches no protected content — with a denial byte-identical to
the denial for a Course they were never invited to.

**Acceptance Scenarios**:

1. **Given** an authenticated Student whose normalized email matches the invitation, **when** they
   accept, **then** the invitation moves to awaiting-admin-approval, an audit record is written, and
   **no Enrollment or Entitlement is created**.
2. **Given** any other authenticated identity — a different Student, an Instructor, or an Admin —
   **when** acceptance is attempted with a valid link, **then** it is refused server-side, and holding
   the link confers nothing.
3. **Given** an unauthenticated visitor holding the link, **when** they open it, **then** they are
   asked to authenticate and **no Course, invitation, or email detail is disclosed** beforehand.
4. **Given** a Student with no Account, **when** they follow the link, register with the invited
   address, and verify their email, **then** they arrive back at the acceptance step with the
   invitation intact.
5. **Given** an invitation already accepted, cancelled, rejected, or approved, **when** acceptance is
   attempted again, **then** it is refused without changing state.
6. **Given** an expired acceptance link, **when** it is presented, **then** acceptance fails safely,
   the invitation itself remains valid and unexpired, and a new link can be issued.

---

### User Story 3 — An Admin approves, and access becomes active (Priority: P1)

An Admin reviews the accepted invitation and approves it. Access becomes active: the Student is
enrolled, holds an active Entitlement for the whole Course, and is notified.

**Why this priority**: it is the only grant path in the product. Nothing downstream — playback,
downloads, progress, roster — is reachable without it.

**Independent Test**: approve an accepted invitation and verify exactly one active Entitlement exists,
the Student can play a Lesson, the expiry matches the Course's configured instant, and approving a
second time changes nothing.

**Acceptance Scenarios**:

1. **Given** an accepted invitation for a Course carrying a future configured access-expiry instant,
   **when** an authorised Admin with valid recent authentication approves it, **then** one transaction
   creates or reuses the Enrollment, creates exactly one active Entitlement scoped to the whole
   Course, snapshots that expiry instant, sets retirement eligibility from the approval instant,
   records the grant source, writes audit evidence, and raises the Student notification intent.
2. **Given** the same approval is submitted twice in sequence, **then** exactly one Entitlement exists
   and the second attempt reports the existing grant rather than failing confusingly.
3. **Given** two approvals of the same invitation are submitted **concurrently**, **then** exactly one
   Entitlement exists and no partial state survives.
4. **Given** a Course with no configured access-expiry instant, or one already in the past, **when**
   approval is attempted, **then** it is refused and the response names the missing configuration.
5. **Given** an archived Course, **when** approval is attempted, **then** it is refused, because an
   archived Course accepts no new access grants.
6. **Given** approval succeeded, **when** the Student requests playback of a Lesson in that Course,
   **then** access is authorised through the Entitlement, and **no authorization path consults the
   invitation**.
7. **Given** approval is attempted without the course-access capability or without valid recent
   authentication, **then** it is refused, and no Enrollment, Entitlement, audit grant record, or
   notification intent exists afterwards.
8. **Given** the grant transaction fails at any point, **then** it rolls back whole — there is no
   state in which an Enrollment exists without its Entitlement, or audit evidence exists without its
   grant.

---

### User Story 4 — An Admin rejects or cancels (Priority: P2)

An Admin declines a request with a stated reason, or withdraws an invitation issued in error before
deciding on it. The Student is told, and no access results.

**Why this priority**: it completes the workflow and lets the operator correct mistakes, but the
product is demonstrable without it.

**Independent Test**: reject an accepted invitation and verify the reason is required and recorded,
the Student is notified, no access exists, and a fresh invitation can afterwards be created for the
same person and Course.

**Acceptance Scenarios**:

1. **Given** an accepted invitation, **when** an Admin rejects it, **then** a reason is required, the
   invitation reaches its terminal rejected state, audit evidence and a Student notification intent
   are recorded, and no Enrollment or Entitlement is created.
2. **Given** a rejection is attempted without a reason, **then** it is refused.
3. **Given** an invitation awaiting acceptance or awaiting approval, **when** an Admin cancels it,
   **then** it reaches its terminal cancelled state and any outstanding acceptance link stops working.
4. **Given** an invitation in a terminal state, **when** approval, rejection, or cancellation is
   attempted, **then** it is refused without changing state.
5. **Given** a previously rejected or cancelled pair of email and Course, **when** a new invitation is
   created for them, **then** it succeeds and the earlier record is preserved unchanged.

---

### User Story 5 — A Student sees where their request stands (Priority: P2)

The Student can see, per Course, whether an invitation awaits their acceptance, awaits Admin approval,
is active with an access-until date, was rejected with the reason, was cancelled, or has expired.

**Why this priority**: without it, a Student who paid externally and accepted cannot tell whether
silence means "pending" or "lost" — which is the exact support burden a manual workflow creates.

**Independent Test**: drive one invitation through every state and verify the Student-facing status is
correct at each step and never implies access that does not exist.

**Acceptance Scenarios**:

1. **Given** invitations in each state, **when** the Student opens their access history, **then** each
   appears with its correct state and relevant timestamps, and an active one shows its access-until
   instant.
2. **Given** any state, **when** the surface is rendered, **then** the Admin note, the external
   reference, and the identity of the deciding Admin are absent.
3. **Given** an invitation awaiting the Student's acceptance, **when** they open their dashboard,
   **then** the pending action is visible and reachable.

---

### User Story 6 — A Student needs a new acceptance link (Priority: P3)

The acceptance link expired or never arrived. An Admin reissues it without recreating the invitation.

**Why this priority**: a recovery path, not a primary flow — but without it an expired link forces a
cancel-and-recreate cycle that pollutes the audit trail.

**Independent Test**: expire a link, reissue it, and verify the new link works, the old one does not,
and the invitation's state and history are unchanged.

**Acceptance Scenarios**:

1. **Given** an invitation awaiting acceptance, **when** an Admin reissues the link, **then** a new
   single-use link is delivered, every previously issued link for that invitation stops working, and
   the invitation state is unchanged.
2. **Given** an invitation already accepted or terminal, **when** reissue is attempted, **then** it is
   refused.
3. **Given** a reissue occurs, **then** it is audited like any other privileged action.

---

### Edge Cases

**Concurrency** — each needs a designed outcome in `plan.md`, not merely a named risk:

- Two Admins approve the same invitation simultaneously.
- An Admin approves while another cancels the same invitation.
- A Student accepts while an Admin cancels.
- Two invitations for the same email and Course are created simultaneously — the uniqueness invariant
  must hold under race, not only under sequential checks.
- Approval races an Admin changing the Course's configured access-expiry instant.

**Access-model boundaries**:

- A Student already holding an active Entitlement for the Course is invited again and approved — the
  one-active-Entitlement invariant must hold, and the operator must receive a comprehensible outcome
  rather than a raw constraint violation.
- A Student whose Entitlement has expired is invited again — a new grant must be possible.
- The Course is delisted between acceptance and approval. Delisting removes catalogue discovery and
  new grants but does not deny existing access *(BR-090)*; the designed outcome for a *pending*
  approval must be stated.
- The Course enters emergency access suspension between acceptance and approval.
- The Course is retired between acceptance and approval — retirement eligibility comes from the
  approval instant, so a late approval must not silently bypass retirement *(BR-027)*.

**Identity**:

- The invited address later becomes attached to a non-Student Account.
- The Student changes their account email after accepting but before approval.
- Two different raw addresses that normalize to the same value.

**Operational**:

- An invitation sits awaiting approval indefinitely — invitations do not expire *(BR-169)*, so this is
  a supported steady state, not an error.
- Notification delivery fails — the grant stands regardless *(BR-120)*.

---

## Requirements *(mandatory)*

### Functional Requirements

**Invitation creation**

- **FR-001**: System MUST allow only an Admin holding the course-access capability to create a Course
  Access Invitation. *(BR-165)*
- **FR-002**: An invitation MUST bind exactly one normalized Student email, exactly one Course, the
  creating Admin, its current state, and separate creation, acceptance, decision, and cancellation
  timestamps. *(BR-165)*
- **FR-003**: System MUST enforce at most one non-terminal invitation per normalized email and Course
  as a database uniqueness invariant, not an application-layer check. *(BR-165; Constitution VII)*
- **FR-004**: System MUST refuse creation when the target email is attached to a non-Student Account.
  *(BR-082)*
- **FR-005**: System MUST permit an optional free-text Admin note and an optional opaque external
  reference, and MUST NOT store any amount, currency, payment status, gateway identifier, or payer
  instrument on any entity in this feature. *(BR-020, BR-170)*
- **FR-006**: Creating an invitation MUST NOT create, modify, or activate any Enrollment or
  Entitlement. *(BR-029)*

**Acceptance**

- **FR-007**: System MUST issue the acceptance link as an expiring, single-use, purpose-bound action
  secret, distinct from the invitation record itself. *(BR-169)*
- **FR-008**: System MUST permit acceptance only by an authenticated Account whose normalized email
  equals the invitation's, and MUST refuse every other identity server-side regardless of how the
  link was obtained. *(BR-166; Constitution II)*
- **FR-009**: System MUST NOT disclose the Course, the invited address, or the existence of the
  invitation to an unauthenticated request. *(BR-166; consistent with the BR-001/BR-003
  anti-enumeration posture)*
- **FR-010**: Acceptance MUST move the invitation to awaiting-admin-approval and MUST NOT create,
  modify, or activate any Enrollment or Entitlement. *(BR-029, BR-166)*
- **FR-011**: System MUST preserve a validated return destination across sign-in, registration, and
  email verification, so an invited Student without an Account arrives back at the acceptance step.
- **FR-012**: An expired acceptance link MUST fail safely without expiring, cancelling, or otherwise
  altering the invitation. *(BR-169)*

**Admin Approval and the grant**

- **FR-013**: System MUST treat Admin Approval as the sole trigger that creates course access. No
  other action in the product may create an Entitlement. *(BR-167, BR-028)*
- **FR-014**: Approval MUST require the course-access capability **and** valid recent authentication,
  and MUST refuse the request when either is absent. It MUST NOT proceed with a default, a fallback,
  a conditional check, or reduced authority. *(BR-167; standing clause carried from the S1C closeout,
  where six fail-open constructions appeared in one slice)*
- **FR-015**: Approval MUST, in one transaction, create or reuse the Student's Enrollment for the
  Course, create exactly one active Entitlement scoped to the whole Course, snapshot the Course's
  configured access-expiry instant, set retirement eligibility from the approval instant, record the
  grant source, write audit evidence, and raise the notification intent. Partial completion MUST be
  impossible. *(BR-024, BR-025, BR-026, BR-027, BR-167)*
- **FR-016**: Approval MUST be idempotent under both repetition and concurrency: exactly one
  Entitlement results, and the invariant MUST be enforced by a database constraint permitting at most
  one active Entitlement per Student and Course. *(BR-024, BR-167; Constitution VII)*
- **FR-017**: System MUST refuse approval when the Course has no configured access-expiry instant, or
  when that instant is not in the future, and MUST name the missing configuration. *(BR-025)*
- **FR-018**: System MUST refuse approval for an archived Course. *(BR-018)*
- **FR-019**: System MUST record a typed grant source on every Entitlement, and MUST implement exactly
  one value in this feature, denoting the manual invitation path. *(BR-028, BR-113)*
- **FR-020**: System MUST NOT provide any route, command, screen, fixture, or configuration flag in a
  production build that creates an Entitlement outside Admin Approval, and MUST NOT implement the
  reserved future grant sources. *(BR-028)*
- **FR-021**: An Entitlement created here MUST reference the exact approved invitation it came from.
  *(BR-113)*

**Rejection, cancellation, reissue**

- **FR-022**: System MUST require a reason to reject, and MUST refuse rejection without one.
  *(BR-168)*
- **FR-023**: System MUST allow an Admin to reject an invitation the Student has already accepted, and
  MUST allow a new invitation for the same email and Course afterwards. *(BR-168)*
- **FR-024**: System MUST allow an Admin to cancel an invitation before a decision, MUST invalidate any
  outstanding acceptance link on cancellation, and MUST refuse any transition out of a terminal state.
  *(BR-168)*
- **FR-025**: System MUST allow an Admin to reissue the acceptance link for an invitation awaiting
  acceptance, MUST invalidate every previously issued link for that invitation, MUST leave the
  invitation state unchanged, and MUST refuse reissue for an accepted or terminal invitation.
  *(BR-169)*

**The access boundary**

- **FR-026**: No authorization decision — playback, protected download, Progress write, Instructor
  roster, or any other protected Course operation — may read Course Access Invitation state. All
  authorise against the Entitlement. *(BR-023, BR-029)*
- **FR-027**: This feature MUST NOT implement Entitlement evaluation, scope resolution, expiry
  checking, or revocation. Those belong to S4 and MUST be consumed, not duplicated. *(SLICES §3.1)*
- **FR-028**: Account registration and email verification MUST NOT create or activate any course
  access. *(BR-029)*
- **FR-029**: A suspended Account MUST be denied every protected action even while holding an active
  Entitlement, and the Entitlement MUST NOT be mutated by suspension or reinstatement. *(BR-007)*
- **FR-030**: Course Access Invitations MUST be a separate record and workflow from staff invitations,
  sharing no state machine, no uniqueness rule, and no account-creation semantics. *(BR-171)*

**Audit and notification**

- **FR-031**: System MUST write immutable audit evidence for invitation creation, acceptance,
  approval, rejection, cancellation, link reissue, and the Entitlement grant, each recording actor,
  target, reason where applicable, and timestamp. *(BR-165, BR-167, BR-168; Constitution II)*
- **FR-032**: System MUST raise durable notification intents, co-committed with the transaction that
  causes them, for invitation issued, access granted, invitation rejected, and invitation cancelled
  after the Student was notified. Acceptance MUST notify Admin operations. *(BR-121, BR-122, BR-123)*
- **FR-033**: Notification delivery failure MUST NOT roll back, block, or alter any invitation
  transition or the grant. *(BR-120)*
- **FR-034**: The access-granted notification MUST be raised only after the Entitlement exists — never
  on creation, acceptance, rejection, or cancellation. *(BR-121)*

**Student and Admin surfaces**

- **FR-035**: System MUST present the Student their invitations and current access per Course, with
  state, relevant timestamps, and the access-until instant where active. *(BR-029)*
- **FR-036**: Student-facing and Instructor-facing surfaces MUST NOT expose the Admin note, the
  external reference, approval evidence, or the identity of the deciding Admin. *(BR-064, BR-101,
  BR-170)*
- **FR-037**: The Student acceptance surface MUST state explicitly that acceptance does not grant
  access and that approval is required. *(BR-029)*
- **FR-038**: System MUST present Admins a queue filtered by invitation state, exposing only the
  actions permitted from each state.
- **FR-039**: Every screen in this feature MUST work in Arabic and English with correct RTL and LTR
  layout, at phone, tablet, laptop, and desktop widths. *(BR-147, BR-149; Constitution X)*

**Account status and actor policy**

- **FR-040**: System MUST permit approval when the Student's Account is suspended. The grant is
  created normally and the suspended Student can use none of it; reinstatement restores otherwise-valid
  access without the Entitlement ever being mutated. Approval MUST NOT be refused on account status
  alone. *(BR-007; resolved by product owner 2026-07-29)*
- **FR-041**: System MUST allow any Admin holding the course-access capability to approve an
  invitation, **including the Admin who created it**. Separation of duties is not enforced, because a
  launch operations team may be a single person and a four-eyes rule would make granting access
  impossible. The controls that remain are the distinct capability, required recent authentication,
  and the audit record naming both the creating and the approving Admin. *(BR-167; resolved by product
  owner 2026-07-29)*
- **FR-042**: Audit evidence MUST record the creating Admin and the deciding Admin separately on every
  invitation, so that self-approval is reconstructable after the fact even though it is permitted.
  *(BR-165, BR-167, BR-168)*

### Key Entities

| Entity | Owner | This feature's relationship |
|---|---|---|
| **Course Access Invitation** | **S6** | Created, transitioned, and terminated here. A workflow record, never an access record |
| **Acceptance link** | **S6**, reusing the existing expiring single-use action-secret mechanism | Issued, consumed, reissued, invalidated here |
| **Enrollment** | **Table and schema: S5.** **Rows and lifecycle: S6 owns** — creation and reuse. S8 reads it for the roster | Created or reused by Admin Approval. S4 never claimed the table; it was assigned to S5 on 2026-07-29 because S5 needs it first for Progress under BR-114/BR-116 ([S5 C1](../007-protected-learning/spec.md#c1--s5-needs-the-enrollment-record-and-s6-creates-it)) |
| **Entitlement** | **S6 creates**, S4 evaluates | Created by Admin Approval, carrying a typed grant source and the approved invitation reference |
| Course | S2 | Read only — lifecycle state and configured access-expiry instant |
| Account | S1 | Read only — identity, role, status |
| Audit Event | S1A | Appended for every transition |
| Notification intent | S9 delivers | Raised here, co-committed with its transaction |

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A Student who registers, verifies their email, and does nothing else reaches **zero**
  protected Course content — enumerated across every protected operation, not sampled.
- **SC-002**: A Student who has accepted but is not yet approved reaches **zero** protected Course
  content, and the denial is indistinguishable from the denial for a Course they were never invited
  to.
- **SC-003**: Across repeated and concurrent approvals of the same invitation, **exactly one** active
  access grant exists — proven under real concurrent execution, not by sequential repetition alone.
- **SC-004**: No identity other than the invited one can accept an invitation — proven for a different
  Student, an Instructor, an Admin, and an unauthenticated visitor holding a valid link.
- **SC-005**: Approval attempted without the required capability or without valid recent
  authentication is refused in **100%** of cases and leaves no partial record: no enrollment, no
  grant, no audit grant entry, no notification intent.
- **SC-006**: In a production build, **no** route, command, screen, fixture, or configuration flag
  creates course access except recorded Admin Approval — proven by asserting absence from the build,
  not by asserting a flag is off.
- **SC-007**: No authorization decision anywhere in the product reads Course Access Invitation state,
  verified by inspecting the inputs of every protected operation.
- **SC-008**: Every invitation transition and every grant produces immutable audit evidence naming
  actor, target, and time — verified by enumerating transitions and asserting a record for each, with
  the enumeration failing if a new transition ships without one.
- **SC-009**: A suspended Student holding an active grant is denied every protected action, and the
  grant is byte-identical before suspension, during it, and after reinstatement — including a grant
  created *while* the Account was suspended.
- **SC-013**: Every invitation's audit trail names the creating Admin and the deciding Admin
  separately, so an approval by the creating Admin is identifiable after the fact.
- **SC-010**: An Admin can take a Student from confirmed external payment to active access in under
  two minutes of interface time, and a Student can determine their current access state without
  contacting support.
- **SC-011**: Every screen in the feature is complete and correct in Arabic and English, RTL and LTR,
  at phone, tablet, laptop, and desktop widths.
- **SC-012**: No entity introduced by this feature carries an amount, currency, payment status,
  gateway identifier, or payer instrument — asserted against the schema, not by reading code.

---

## Assumptions

Recorded rather than raised as questions, because each is resolved by an authoritative source document
under Constitution Principle I, or has an unambiguous reasonable default.

- **A Student cannot decline an invitation.** No source rule defines a declined state. A Student who
  does not want access simply does not accept, and an Admin can cancel. Adding a declined state would
  be new policy.
- **An invitation for an archived Course cannot be approved**, derived from archived being terminal
  for new access grants (BR-018, Domain Model §3).
- **Delisting and retirement follow BR-090 and BR-027 as written**: delisting does not deny existing
  access, and retirement eligibility comes from the approval instant, so a late approval cannot bypass
  retirement. The exact designed outcome per Course state belongs in `plan.md`.
- **Acceptance requires an Active Account**, because acceptance requires an authenticated session and
  an unverified Student cannot sign in (BR-008).
- **The invitation notice is delivered to an address that may have no Account**, using the existing
  outbox delivery-intent mechanism already proven for staff invitations, which carries a destination
  address rather than an Account reference. The durable in-app record begins once an Account exists
  (BR-123).
- **There is no bulk invitation creation** in MVP; invitations are created one at a time.
- **There is no limit** on how many invitations an Admin may create, nor on concurrent invitations for
  the same Student across different Courses.
- **Terminal invitations are retained**, not deleted. Their retention period is governed by the open
  `LG-003` gate and is not decided here.
- **An invitation awaiting approval indefinitely is a supported steady state**, not an error, because
  invitations do not expire (BR-169).
- **The Instructor roster is out of scope** for this slice; it consumes the Enrollment this slice
  creates, and ships in S8 with its own authorization assertion.
- **AD07 is read-only in this slice.** Entitlement expiry adjustment and revocation are **S8 Admin
  Operations, exclusively** — one owner per mutation. S6 ships no write path to an Entitlement other
  than the approval transaction that creates it.
- **SC-010 is deferred out of this slice's acceptance evidence.** The two-minute interface time and
  "Student determines state without contacting support" are UX outcome metrics, measurable only against
  real operators and real Students after launch. They remain the design intent for the screens; they
  are not a buildable proof S6 can produce, and no task claims them.
- **A suspended Student may still be granted access** (FR-040), so the approval queue does not need to
  filter or reorder by account status. Surfacing status in the queue is a usability improvement, not a
  requirement of this slice.
- **Self-approval is permitted but reconstructable** (FR-041, FR-042). If operations later grow past a
  single Admin, adding a separation-of-duties rule is a business-rule change, not a schema change,
  because both actors are already recorded.

## Dependencies

| Depends on | For | State |
|---|---|---|
| S2 — Course authoring | Course graph, lifecycle state, configured access-expiry instant | **Blocking.** In implementation |
| S4 — Media and Entitlement evaluation | The Entitlement grant record, scope evaluation, expiry, revocation. **Not** the Enrollment table | **Blocking.** Specified, not implemented |
| S5 — Protected Learning | The **`enrollments` table** this slice writes to. S5 creates it and creates no row; S6 asserts the inherited shape before writing ([S5 C1](../007-protected-learning/spec.md#c1--s5-needs-the-enrollment-record-and-s6-creates-it)) | **Blocking.** Specified and planned, not implemented |
| S1C — Staff lifecycle and authorization | Capability gate, session, recent authentication, suspension enforcement, audit, action secrets, outbox | Closed |
| S3 — Public catalogue and shell | The bilingual responsive shell these screens render on | **Blocking** for the screens only |
| S9 — Transactional email | Delivery of the invitation and access-granted messages | **Not blocking.** Intents are raised here; delivery is adapter work and `LG-018` may be unresolved at launch |

## Constitution Alignment Note

**Resolved on 2026-07-29.** This specification originally recorded a live conflict: Principle IV,
*Payment Correctness*, assumed every Entitlement originates from a verified payment, which described
a feature D-045 had removed from the MVP. Surfacing it rather than working around it is what
Principle I requires.

The constitution was amended to **v1.1.0** before planning. Principle IV is now **Access-Grant
Correctness** and governs authoritative course-access grants whatever triggers them. Every guarantee
this specification depends on is now backed by a principle rather than merely asserted here:

| This spec | Principle IV v1.1.0 |
|---|---|
| FR-013, FR-019, FR-021, FR-031 | Authorized, audited, typed grant source on every Entitlement |
| FR-015, FR-016 | Idempotent by stable identifier; no double-grant under retry or concurrency |
| FR-003, FR-016 | No duplicate active access, enforced by database constraint |
| FR-014 | Refuse, never degrade |
| FR-019, FR-020 | One boundary, many sources — the grant-source discriminator |
| FR-013 | Admin Approval named as the sole MVP grant source |
| FR-005, FR-020 | Payment rules retained as deferred and conditional, not repealed |

The amendment strengthened two things rather than relaxing them: duplicate prevention is now
explicitly a database constraint at principle level, and Principle V now requires a grant path to
carry a **concurrency** test, not only a sequential one — which this specification's SC-003 already
demanded.
