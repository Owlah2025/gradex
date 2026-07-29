# Gradex Domain Model

> Status: Approved conceptual baseline for system design
> Last Updated: 2026-07-28

This document defines product-level entities, ownership, relationships, invariants, and lifecycle
states. It is not a database schema or API design. System design may choose storage/mechanisms but
must preserve these meanings and the rules in [BUSINESS_RULES.md](BUSINESS_RULES.md).

## 1. Canonical Language

> **MVP scope note (2026-07-28, [D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)).**
> Gradex launches with no in-platform payments. `Order`, `Payment Attempt`, `Refund`, `Coupon`,
> `Coupon Redemption`, `Financial Ledger Entry`, and `Payout Statement` are **deferred out of MVP**
> and their definitions are retained below, clearly marked, as the design of record for whenever
> those features are taken up. Access is granted by the `Course Access Invitation` workflow.

- The content hierarchy is `Course → Section → Lesson`.
- `Section` is the domain term. “Chapter” may be a localized Student-facing label for the same
  object; it is never a separate entity, table, acquirable scope, or API target.
- `Course Access Invitation` is the Admin-created **workflow** record through which a Student
  requests access to one specific Course. It is never the authoritative access record.
- `External Payment` is payment performed and verified **outside** Gradex. Gradex stores no payment
  transaction, amount, currency, or status.
- `Admin Approval` is the final Admin action on an accepted Invitation, and it is the authoritative
  trigger that activates access.
- `Enrollment` is the durable Student-to-Course learning relationship used for roster/progress.
- `Entitlement` is the time-bounded **authorization** to access a Course, and it is the authoritative
  access record every protected operation checks. Enrollment and Entitlement are distinct and are not
  merged.
- Admin Approval creates or reuses an Enrollment and creates exactly one Entitlement, idempotently.
- Expiry/revocation ends access but does not delete Enrollment or progress history.

## 2. Identity and Access

### Account

Represents one person and normalized unique email, authentication identity, role, status, language,
display name, and profile. MVP roles are `STUDENT`, `INSTRUCTOR`, and `ADMIN`.

Ownership and rules:

- Identity is the normalized email and the internal identifier. The display name is a self-chosen,
  non-unique label defaulting to the name supplied at registration or invitation acceptance; the
  owner may change it at any time and it never has to carry legal identity. It is the only identity
  field an Instructor roster exposes, and an Admin may reset an abusive value through the audited
  moderation path.
- Public registration can assign only `STUDENT`.
- `INSTRUCTOR` and additional `ADMIN` roles are assigned when an Admin invitation is accepted;
  sending an invitation does not create an Account.
- An email already attached to an Account cannot be invited into a different role; MVP Accounts
  have exactly one role assigned at creation and immutable during MVP, with no role conversion,
  multi-role membership, or identity merge.
- Only Student Accounts can receive Course Access Invitations, receive Entitlements, create
  Enrollments, and record Progress. Instructors author assigned content without Student consumption
  capability. Admin content access uses the distinct audited preview path and creates no Entitlement
  or Progress.
- A person needing separate role capabilities uses a separate Account with a different normalized
  email during MVP.
- One bootstrap Admin is created through secure deployment.
- Suspension is an Account restriction. It immediately blocks every protected action, including for
  an Account holding an active Entitlement, but it does not mutate that Entitlement and does not
  delete course access, Course ownership, or audit history. Reactivation restores otherwise-valid
  access. "Disabling" an account is this same `ACTIVE ↔ SUSPENDED` transition, not a separate state.

Lifecycle:

```text
Student:    PENDING_VERIFICATION → ACTIVE ↔ SUSPENDED → DEACTIVATED
Staff:                             ACTIVE ↔ SUSPENDED → DEACTIVATED
Bootstrap:                         ACTIVE ↔ SUSPENDED → DEACTIVATED

Invitation: PENDING → ACCEPTED
                  ├→ EXPIRED
                  └→ REVOKED
```

`SUSPENDED` immediately blocks protected actions. Reactivation restores role-authorized access
subject to independent Course/Entitlement states.

### Verification Token / Invitation / Password Reset Token

Single-purpose, expiring, single-use credential associated with an Account/email. Only the secure
token representation is stored. Resend/attempt behavior is rate-limited and audited as appropriate.

### Course Access Invitation

The Admin-created workflow record granting one Student the ability to request access to one Course.
It is **not** an access record and **not** an account invitation, and it must not be implemented in
terms of the staff Invitation above: it creates no Account and assigns no role.

It binds one normalized Student email, one Course, the creating Admin, its current state, and
separate creation, acceptance, decision, and cancellation timestamps. It may carry an Admin-only
free-text note and an opaque External Payment reference; it never carries an amount, currency, or
payment status.

```text
PENDING_STUDENT_ACCEPTANCE ──accept──→ PENDING_ADMIN_APPROVAL ──approve──→ APPROVED
            │                                    │
            │                                    └──reject──→ REJECTED
            └──cancel──→ CANCELLED               (Admin may cancel before a decision)
```

`APPROVED`, `REJECTED`, and `CANCELLED` are terminal. There is **no expiry state**: the acceptance
link is a separate expiring action secret that is reissued, while the Invitation itself does not
expire. Only an Account whose normalized email matches may accept. At most one non-terminal
Invitation exists per `(normalized email, Course)`. An Admin may reject an already-accepted
Invitation, and a new Invitation may afterwards be created for the same pair.

Acceptance grants nothing. Only Admin Approval creates access, and it does so idempotently.

### Session

An authenticated browser Session is one stable, independently revocable family associated with an
Account and the admitted `session_epoch`. Each opaque cookie rotation creates an immutable credential
generation with separate credential and CSRF digests; it never overwrites the earlier generation.
Only the current unsuperseded generation authenticates. The stable family holds authentication,
activity, reauthentication, idle/absolute expiry, revocation, and reuse status, while immutable
generation ancestry supports concurrency-safe stale-presentation classification and confirmed reuse
response.

## 3. Course and Learning Content

### Course

Top-level catalog/learning product. Exactly one Instructor owns a Course; an Admin may reassign
ownership. A Course has authored details, one current Admin-controlled price, an Admin-configured
`default_access_ends_at` applied at future approvals, its catalog classification, Sections, an optional
public preview, publication state, and history.

The **external community link is a post-launch attribute of the Course revision**, deferred to S18 on
2026-07-29 by [D-046](DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch).
It is retained here as the design of record: no MVP slice authors, persists, serves, or renders it,
and no MVP migration defines a column for it.

Lifecycle:

```text
DRAFT → PENDING_REVIEW → PUBLISHED
           ↓                 ↕
     CHANGES_REQUESTED      DELISTED
           └─→ PENDING_REVIEW  └─→ ARCHIVED
PUBLISHED ──────────────────────→ ARCHIVED
```

- `CHANGES_REQUESTED` requires an Admin reason and permits Instructor revision/resubmission.
- `DELISTED` is reversible and removes catalog discovery **and blocks new access grants**, without
  denying qualifying existing Student access: an active Entitlement is untouched (BR-090).
- `ARCHIVED` is terminal for catalog discovery and new access grants. A Course with enrollment history is archived,
  not hard-deleted; Students retain access while their existing Entitlement remains active.
- Retirement is a separate explicit future-acquisition/inclusion block. Emergency Course access
  suspension is an orthogonal elevated state that immediately blocks existing Student access for a
  constrained legal, security, malware, or severe-moderation reason without mutating Entitlements.
  Suspension and restoration preserve immutable reason/actor/Audit/notification evidence.
- Changing `default_access_ends_at` affects future approvals only. A Course may remain Published and
  available to existing entitled Students without a future default, but a Course Access Invitation
  for it **cannot be approved** until an Admin configures one.

### Taxonomy Term and Course Classification

Catalog classification uses exactly three dimensions, and a Course carries exactly one value on
each: **Major**, **Subject**, and **Study Year**.

- Major and Subject are Taxonomy Terms: Admin-managed vocabulary entries with a stable identity,
  Arabic and English labels, and — for Subject only — an optional academic code such as `CS 101`.
- Study Year is a fixed enumeration: `PREP`, `YEAR_1`, `YEAR_2`, `YEAR_3`, `YEAR_4`.

Ownership and rules:

- Only Admins create, rename, retire, or delete Taxonomy Terms, and each action is audited.
- An Instructor selects among existing terms for an owned Course and cannot add to the vocabulary;
  an Admin may override any Course's assignment.
- All three dimensions must be assigned before a Course can enter review.
- Renaming a term changes its displayed label everywhere and never rewrites assigned Courses.

```text
Term:  ACTIVE → RETIRED        (deletable only while unreferenced)
```

A `RETIRED` term cannot be newly assigned; Courses already carrying it keep it and stay filterable
until an Admin reassigns them.

### Course Revision

Pending proposed changes to a Published Course. It is separate from the approved live version.
Approval atomically makes the revision current; a change request leaves the live version unchanged.
Catalog pricing is not part of an Instructor revision.

### Section

Ordered grouping within exactly one Course. A Section contains Lessons and may have an Admin-set
catalog price. **Section is not an acquirable access scope in MVP** — access is granted for a
complete Course only — so Section prices are retained in the model and the Admin surface but are not
displayed in the student-facing catalogue. A Section has no independent access-period override.

### Lesson

Ordered learning unit within exactly one Section. It may have one current video, optional Resources,
optional Lab Materials, and per-Student progress.

### Video Asset

One Lesson's source/processed playback asset. Its processing milestone is independent from Course
publication and Course Revision approval:

```text
NOT_UPLOADED → UPLOADING → PROCESSING → READY ── Admin approval → APPROVED_CURRENT
                              ↓   ↑
                            FAILED ── retry

APPROVED_CURRENT ── staged replacement → PROCESSING → READY
                                                    └─ Admin revision approval → APPROVED_CURRENT
```

`READY` means technically processable/playable, not approved for Students. Exact queue/transcode
states, retry counts, and TTLs remain system-design/implementation concerns. A replacement is staged
separately so the current approved Asset remains playable until Admin approval.

### Lesson Resource / Lab Material

Protected downloadable Lesson attachments. Resources are reference material; Lab Materials are
hands-on files/guides. Both require entitlement. Lab Materials may carry a buyer identifier;
Resources do not.

### Public Preview Asset

At most one optional public Course evaluation asset, separate from Lessons and protected files. It
is published only after validation, quarantine, successful malware scan, and permission confirmation.

### Progress

Student/Lesson learning state: last/max position in milliseconds, permanent completion,
`completed_at`, the exact completing Media Asset Version, last-watched time, and revision. It belongs
to a durable Enrollment and remains after Entitlement expiry. Writes require runtime Lesson access;
the server validates/bounds positions and calculates completion from the trusted duration of the
exact played Asset Version. Completion never regresses.

## 4. Catalog Price and Course Access

### Catalog Price / Price Change

Current integer-fils price for a Course or Section, controlled only by Admin. Each change records
old/new value, reason, Admin, and timestamp.

In MVP the Course price is **displayed** so a Student knows what to pay through External Payment; it
is not charged by Gradex and nothing in Gradex snapshots it. Section prices are retained but not
displayed, because Section is not an acquirable scope.

### Order — *deferred out of MVP*

*No Order entity exists in MVP. Retained as the design of record for a future checkout.*

Commercial intent for exactly one Student and one purchasable item (Course or Section). Its item
snapshot preserves identity, catalog subtotal, coupon details, discount, total, currency,
`access_ends_at`, accepted policy version, approved revision, and identifiers. Order timestamps
separate creation, commercial acceptance, payment deadline, completion, expiry, and cancellation.

Lifecycle:

```text
PENDING_PAYMENT → PAID ───────────────→ REFUNDED
       │             └→ PARTIALLY_REFUNDED ─→ REFUNDED
       ├────────→ CANCELLED
       └────────→ EXPIRED
       └────────→ RECONCILIATION_REQUIRED

PENDING_PAYMENT → FREE_GRANTED
```

`FREE_GRANTED` is a successful zero-value terminal payment outcome and still creates normal
Enrollment/Entitlement records. Order refund state is derived from confirmed Refund totals; it does
not replace Refund records. `CANCELLED` and `EXPIRED` are separate outcomes, not sequential.
`RECONCILIATION_REQUIRED` is a visible exception when money may have moved but automatic completion
is unsafe; resolution preserves all evidence.

### Payment Attempt — *deferred out of MVP*

*No Payment Attempt entity exists in MVP.*

One attempt to pay an Order, with a stable idempotency reference and gateway reference.

```text
CREATED → PENDING ─→ SUCCEEDED
             ├────→ FAILED
             ├────→ CANCELLED
             ├────→ TIMED_OUT
             └────→ UNKNOWN → reconciled to a definitive state
```

An Order may have multiple Attempts while it remains payable. A failed/cancelled/timed-out Attempt
does not itself grant access; Order cancellation/expiry is separate. Only verified success can grant
access, and it grants once. `SUCCEEDED` means verified capture, not authorization. `FAILED` and
`CANCELLED` are provider-confirmed outcomes. `TIMED_OUT` is locally observed and may later reconcile;
`UNKNOWN` blocks another Attempt. Provider occurrence time—not arrival time—controls deadline
eligibility, and immutable event/transition history remains even when Attempt state changes.

### Refund — *deferred out of MVP*

*Gradex processes no refunds. Money returned to a Student is an External Payment matter handled
outside the platform; ending that Student's access uses the audited Entitlement adjustment or
revocation, never an unrecorded deletion.*

One Admin-requested gateway refund amount/reason against a captured Order.

```text
REQUESTED → PENDING ─→ SUCCEEDED
             ├─────→ FAILED
             └─────→ CANCELLED
```

- Multiple Refunds may exist up to remaining captured balance.
- Partial success keeps Entitlement active.
- Cumulative full success revokes Entitlement and releases per-Student coupon eligibility.
- Failed/pending requests do not change access or collected revenue.

### Enrollment

Durable Student-to-Course relationship used for roster, progress, and learning history, created or
reused by Admin Approval. An Instructor roster is a least-privilege projection of Course-scoped
display identity, enrollment, and progress — never the Student's direct account/contact PII, the
Admin note, the External Payment reference, or the approval evidence.

### Entitlement

**The authoritative access record.** Authorization tied to one Student and one Course, carrying a
typed `grant_source` that records how access was granted. It preserves `acquired_at`,
`retirement_eligibility_at` set from the Admin Approval instant, `original_access_ends_at`
snapshotted from the Course's configured expiry at approval, the current authoritative
`access_ends_at`, and revocation details. Access is allowed only while
`current_timestamp < access_ends_at`.

```text
ACTIVE → EXPIRED
   └──→ REVOKED
```

**Grant sources.** MVP implements exactly one: `MANUAL_INVITATION`, produced by Admin Approval of an
accepted Course Access Invitation, which the Entitlement references. `PAID_ORDER`, `PROMOTIONAL`, and
`DIRECT_ADMIN_GRANT` are reserved names that are **not implemented**; no route, command, screen,
fixture, or configuration flag in a production build may create an Entitlement by any other path.
This discriminator is the extension point for a future payment gateway — no speculative
payment-provider, checkout-session, or webhook-event entity is introduced.

**Scope.** MVP grants whole-Course scope only, authorizing every Section and Lesson in that Course.
The scope concept remains expressive enough for a narrower future scope, but Section-, Lesson-,
bundle-, and partial-course access are not acquirable at launch. At most one `ACTIVE` Entitlement
exists per `(Student, Course)`.

Account suspension and explicit emergency Course access suspension can block access without mutating
Entitlement state. Delisting, retirement, and archival alone do not deny qualifying existing access.

An elevated Admin may extend or shorten `access_ends_at`. Each change creates an immutable
Entitlement Adjustment recording old/new instants, reason, actor, timestamp, and an optional support
reference; `original_access_ends_at` never changes.

Retirement blocks future acquisition. Retired Course/Section/Lesson content remains accessible only
when `retirement_eligibility_at < retired_at`. Invitation creation time, acceptance time, and
database insertion time are never the eligibility timestamp.

## 5. Coupons — *deferred out of MVP*

*No Coupon entity exists in MVP; a coupon discounts a checkout and there is no checkout. Free or
promotional access is granted through the same audited Course Access Invitation path as any other
access. Retained as the design of record.*

### Coupon / Coupon Target

Admin-managed percentage/fixed discount with validity window, active flag, optional Course/Section
targets, and optional global cap. No targets means platform-wide applicability.

### Coupon Redemption

Historical capacity reservation/use between Coupon, Student, Order, and discount.

```text
RESERVED → CONSUMED → RELEASED_AFTER_FULL_REFUND
    └────→ RELEASED_UNUSED
```

Paid Order acceptance creates `RESERVED` until its payment deadline. Timely success consumes it;
Order expiry/cancellation releases it unused. A zero-value Coupon Order consumes immediately.
Reserved plus historically consumed uses count against global capacity. Full Refund releases the
Student's eligibility but does not delete history, reduce historical consumed count, or restore
global quota.

## 6. Office Hours and Notifications

### Office-Hours Session

One Course-scoped external meeting created by the owning Instructor. It records schedule, link,
status, and cancellation/moderation history. Student discovery/join requires runtime access and no
active emergency Course access suspension. Delisting, retirement, or archival alone does not hide
the Session from otherwise-qualified existing Students. A material reschedule creates an immutable
Session Version and moves the stable Session's current pointer, so reports retain the exact schedule
and content they observed.

```text
Derived time phase: UPCOMING → LIVE → ENDED
Explicit mutation: ACTIVE → CANCELLED
```

`ENDED` is derived after the end time and proves no delivery or attendance. Join-link disclosure is
limited to the authorized live window. Session history and separately authorized materials or
recordings may remain visible afterward; cancellation blocks joining but deletes no history.
Attendance, Instructor check-in, provider, recording, no-show, and dispute evidence stay separate.

### Notification Event / Recipient / Delivery Attempt

One source Event has relationally snapshotted Account/channel Recipients. In-app recipient state is
durable; optional email Attempts are best-effort and retain immutable delivery evidence.

```text
Notification: RECORDED → READ
Email attempt: PENDING → SENT | FAILED
```

Mandatory transactional/security notices cannot be suppressed. Operational notices follow fixed
product channel rules. Marketing is optional and outside MVP. Email state never controls the source
business transaction or invalidates the in-app record.

## 7. Moderation and Audit

### Content Report

Student- or automation-originated report/finding against a stable logical target and the exact
Course Revision, authored version, Media Asset Version, or Office-Hours Session revision visible
when reported, with a constrained reason and optional note.

```text
OPEN → UNDER_REVIEW → RESOLVED_DISMISSED | RESOLVED_ACTIONED
```

Content is not hidden automatically. Media quarantine/rejection and emergency security suspension
remain separate safety workflows. Every resolution and resulting Admin action is appended
immutably.

### Audit Event

Immutable record of a privileged action: actor, role, action, target, before/after or relevant
metadata, reason, timestamp, and correlation/reference identifiers. Audit Events cover account/role,
account suspension and reinstatement, pricing, publishing/preview, Course Access Invitation
creation/acceptance/approval/rejection/cancellation, Entitlement grant and expiry adjustment,
reports, office-hours moderation, taxonomy vocabulary changes, and Admin resets of an Account display
name.

## 8. Instructor Earnings and Payouts — *deferred out of MVP*

*With no in-platform revenue record there is no earning to calculate, so no ledger, Statement, or
transfer entity exists in MVP. Instructors are paid entirely out of band at launch. The Instructor
agreement's revenue-share terms remain required under `LG-020`. Retained as the design of record.*

### Financial Ledger Entry

Immutable accounting line for an earning, payment fee, Refund, chargeback, payout adjustment,
carry-forward, or approved correction. Zero-value grants create no positive earning. An earning
snapshots formula inputs, effective configuration version, and owning Instructor at Order
completion. Every adjustment links its exact source; corrections append compensating entries.
Course reassignment never rewrites earlier lines, and later Refund/chargeback adjustments remain
tied to the original earning, Instructor, and snapshotted policy.

### Payout Statement

Monthly Instructor record containing eligible Orders, fees/refunds/chargeback adjustments, share
configuration, and payable total.

```text
DRAFT → READY_FOR_REVIEW → APPROVED → PAYMENT_PENDING → PAID
DRAFT / READY_FOR_REVIEW → BLOCKED → DRAFT
PAYMENT_PENDING → PAYMENT_FAILED → PAYMENT_PENDING
```

Approval immutably freezes items, totals, and the approved payout destination. Transfer initiation
creates an immutable attempt using that destination; `PAID` requires verified full-payment evidence.
Partial statement payment is prohibited. Later adjustments appear in a future statement, and
negative payable balances carry forward without a negative bank transfer. The statement is emailed;
there is no Instructor dashboard in MVP.

## 9. Core Relationship Summary

```text
Instructor Account 1 ── owns ── * Course
Course 1 ── contains ── * Section 1 ── contains ── * Lesson
Lesson 1 ── has ── 0..1 Video, * Resources, * Lab Materials
Course 1 ── has ── 0..1 Public Preview
Course 1 ── classified by ── 1 Major, 1 Subject, 1 Study Year
Taxonomy Term 1 ── classifies ── * Course

Student Account 1 ── has ── * Enrollment (Course)
Enrollment 1 ── has ── * Progress (Lesson)
Student Account 1 ── has ── * Entitlement (Course)

Admin 1 ── creates ── * Course Access Invitation ── targets ── 1 Student email + 1 Course
Course Access Invitation 1 ── when APPROVED produces ── 1 Entitlement + 0..1 new Enrollment
Entitlement 1 ── records ── 1 grant_source (MVP: MANUAL_INVITATION only)

Course 1 ── has ── * Office-Hours Session
Student 1 ── submits ── * Content Report ── resolved by ── Admin
```

Deferred out of MVP with in-platform payments: `Order`, `Payment Attempt`, `Refund`,
`Coupon Redemption`, `Payout Statement`, and `Earning/Adjustment`.

## 10. Cross-Cutting Invariants

- Role, ownership, status, and entitlement authorization is always server-side.
- Monetary values are integer fils; KWD display uses three decimal places. In MVP these are display
  values only — Gradex charges nothing and records no payment.
- **Admin Approval is the authoritative grant trigger.** Registration, email verification, External
  Payment, and invitation acceptance each grant nothing on their own.
- **The Entitlement is the authoritative access record.** No protected operation reads Course Access
  Invitation state, and none may ever read payment-provider state.
- Idempotency prevents a duplicate grant or a duplicate notice; repeating an approval returns the
  existing Entitlement.
- Account suspension, catalog delisting, retirement, emergency Course access suspension,
  Entitlement expiry/revocation, archival, and deletion are separate concepts.
- Privileged-action Audit records — including every Course Access Invitation transition and every
  Entitlement grant and adjustment — are append-only and never rewritten or hard-deleted. Other
  records with access or moderation history require an explicit approved retention workflow rather
  than silent deletion. The same rule applies to financial ledger entries, Statements, and payout
  evidence whenever those deferred entities are built.
- Catalog discovery, filtering, and search expose only `PUBLISHED` Courses and never index Lesson
  titles or protected Resource/Lab content.
- Exact storage, table, API, token, and queue designs belong to the system-design phase.
- MVP records no receipt, invoice, tax, or dispute data, because it handles no money. `LG-016`
  remains open: where payment is captured does not decide whether records are required, and
  off-platform collection may move that obligation rather than remove it. A future commerce design
  must still accommodate receipt/invoice fields and immutable dispute/chargeback events without
  assuming the unresolved policies in [LAUNCH_GATES.md](LAUNCH_GATES.md) LG-016/LG-017.
