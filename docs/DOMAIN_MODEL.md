# Gradex Domain Model

> Status: Approved conceptual baseline for system design
> Last Updated: 2026-07-23

This document defines product-level entities, ownership, relationships, invariants, and lifecycle
states. It is not a database schema or API design. System design may choose storage/mechanisms but
must preserve these meanings and the rules in [BUSINESS_RULES.md](BUSINESS_RULES.md).

## 1. Canonical Language

- The content hierarchy is `Course → Section → Lesson`.
- `Section` is the domain term. “Chapter” may be a localized Student-facing label for the same
  object; it is never a separate entity, table, purchase type, or API target.
- `Enrollment` is the durable Student-to-Course learning relationship used for roster/progress.
- `Entitlement` is the time-bounded authorization to access a Course or one Section.
- A purchase/grant may create or reuse an Enrollment and creates one Entitlement.
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
- Only Student Accounts can place Orders, receive ordinary Entitlements, create Enrollments, and
  record Progress. Instructors author assigned content without Student consumption capability.
  Admin content access uses the distinct audited preview path and creates no Entitlement or Progress.
- A person needing separate role capabilities uses a separate Account with a different normalized
  email during MVP.
- One bootstrap Admin is created through secure deployment.
- Suspension is an Account restriction; it does not delete purchases, Course ownership, or audit
  history.

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

### Session

Represents a refreshable authenticated session that can be revoked. System design chooses access
token/session mechanics but must satisfy immediate suspension and safe logout/rotation rules.

## 3. Course and Learning Content

### Course

Top-level catalog/learning product. Exactly one Instructor owns a Course; an Admin may reassign
ownership. A Course has authored details, one current Admin-controlled price, an Admin-configured
`default_access_ends_at` for future purchases, its catalog classification, Sections, an optional
public preview, community link, publication state, and history.

Lifecycle:

```text
DRAFT → PENDING_REVIEW → PUBLISHED
           ↓                 ↕
     CHANGES_REQUESTED    UNPUBLISHED
           └─→ PENDING_REVIEW  └─→ ARCHIVED
PUBLISHED ──────────────────────→ ARCHIVED
```

- `CHANGES_REQUESTED` requires an Admin reason and permits Instructor revision/resubmission.
- `UNPUBLISHED` is a reversible Admin moderation state; it removes the Course from catalog/new
  purchase and temporarily blocks Student access to protected Course content while the issue is
  resolved, without deleting Entitlements or progress.
- `ARCHIVED` is terminal for catalog/new purchases. A Course with enrollment history is archived,
  not hard-deleted; Students retain access while their existing Entitlement remains active.
- Changing `default_access_ends_at` affects future Orders only. A Course may remain Published and
  available to existing entitled Students without a future default, but checkout is disabled until
  an Admin configures one.

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
catalog price, making it an MVP purchasable scope. It has no independent access-period override;
Section checkout snapshots the containing Course's configured expiry.

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

Student/Lesson learning state: last position, maximum position, completion, and timestamps. It
belongs to a durable Enrollment and remains after Entitlement expiry. Completion never regresses.

## 4. Catalog Price and Commerce

### Catalog Price / Price Change

Current integer-fils price for a Course or Section, controlled only by Admin. Each change records
old/new value, reason, Admin, and timestamp. Orders snapshot their own commercial values and never
change when the catalog price changes.

### Order

Commercial intent for exactly one Student and one purchasable item (Course or Section). Its item
snapshot preserves identity, catalog subtotal, coupon details, discount, total, currency,
`access_ends_at`, accepted policy version, and identifiers.

Lifecycle:

```text
PENDING_PAYMENT → PAID ───────────────→ REFUNDED
       │             └→ PARTIALLY_REFUNDED ─→ REFUNDED
       ├────────→ CANCELLED
       └────────→ EXPIRED

PENDING_PAYMENT → FREE_GRANTED
```

`FREE_GRANTED` is a successful zero-value terminal payment outcome and still creates normal
Enrollment/Entitlement records. Order refund state is derived from confirmed Refund totals; it does
not replace Refund records.

### Payment Attempt

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
access, and it grants once.

### Refund

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

Durable Student-to-Course relationship used for roster, progress, and learning history. A Section
purchase still enrolls the Student in the containing Course but does not grant other Sections. An
Instructor roster is a least-privilege projection of Course-scoped display identity/enrollment/
progress, not access to the Student's direct account/contact/payment PII.

### Entitlement

Authorization scope tied to a Student, Course or Section, source Order/grant, effective commercial
grant time, `original_access_ends_at`, current authoritative `access_ends_at`, and revocation
details. Access is allowed only while `current_timestamp < access_ends_at`.

```text
ACTIVE → EXPIRED
   └──→ REVOKED
```

Account suspension and Course unpublishing can block access without mutating Entitlement state.
A Course Entitlement authorizes every Section in its Course. A Section Entitlement authorizes only
that Section; it creates no upgrade credit against a later Course purchase.

An elevated Admin may extend or shorten `access_ends_at`. Each change creates an immutable
Entitlement Adjustment recording old/new instants, reason, actor, timestamp, and an optional
support/refund reference; `original_access_ends_at` never changes.

Retirement blocks future acquisition. Retired Course/Section/Lesson content remains accessible only
when the Entitlement's effective payment-success or free/manual-grant timestamp is earlier than the
relevant `retired_at` instant and the Entitlement remains otherwise active. A delayed webhook's
database insertion time cannot turn a post-retirement grant into an eligible one.

## 5. Coupons

### Coupon / Coupon Target

Admin-managed percentage/fixed discount with validity window, active flag, optional Course/Section
targets, and optional global cap. No targets means platform-wide applicability.

### Coupon Redemption

Historical link between Coupon, Student, Order, and committed discount.

```text
COMMITTED → RELEASED_AFTER_FULL_REFUND
```

Only `COMMITTED` consumes the Student's one-use eligibility. Release permits a future Order but does
not delete history or reduce the historical global redemption count.

## 6. Office Hours and Notifications

### Office-Hours Session

One Course-scoped external meeting created by the owning Instructor. It records schedule, link,
status, and cancellation/moderation history. Student discovery/join additionally requires the Course
to remain `PUBLISHED`.

```text
SCHEDULED → COMPLETED
     └────→ CANCELLED
```

`COMPLETED` may be derived after end time. Join-link disclosure requires authorization at request
time and is not embedded in public/catalog data.

### Notification / Delivery Attempt

Durable per-recipient in-app event with read state, deduplication key, and optional channel delivery
attempts.

```text
Notification: RECORDED → READ
Email attempt: PENDING → SENT | FAILED
```

Delivery state never controls the source business transaction.

## 7. Moderation and Audit

### Content Report

Student-submitted report against a Course/Lesson/video/resource/lab with a reason and optional note.

```text
OPEN → UNDER_REVIEW → RESOLVED_DISMISSED | RESOLVED_ACTIONED
```

Content is not hidden automatically. Resolution records the Admin and action.

### Audit Event

Immutable record of a privileged action: actor, role, action, target, before/after or relevant
metadata, reason, timestamp, and correlation/reference identifiers. Audit Events cover account/role,
pricing, publishing/preview, refunds, payouts, coupons, reports, office-hours moderation, taxonomy
vocabulary changes, and Admin resets of an Account display name.

## 8. Instructor Earnings and Payouts

### Earning / Adjustment

Derived accounting line from a paid Order or later refund/chargeback. Zero-value grants create no
positive earning. Amount uses the configured global share of net collected revenue and snapshots
the formula inputs used for the statement.

### Payout Statement

Monthly Instructor record containing eligible Orders, fees/refunds/chargeback adjustments, share
configuration, and payable total.

```text
DRAFT → APPROVED → PAID
  └──────────────→ VOID (before payment, with reason)
```

`PAID` requires a manual bank-transfer reference. Later adjustments appear in a future statement.
The statement is emailed; there is no Instructor dashboard in MVP.

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
Student Account 1 ── has ── * Entitlement (Course or Section)

Student 1 ── places ── * Order 1 ── targets ── 1 Course or Section
Order 1 ── has ── * Payment Attempt, * Refund, 0..1 committed Coupon Redemption

Instructor 1 ── receives ── * Payout Statement 1 ── contains ── * Earning/Adjustment
Course 1 ── has ── * Office-Hours Session
Student 1 ── submits ── * Content Report ── resolved by ── Admin
```

## 10. Cross-Cutting Invariants

- Role, ownership, status, and entitlement authorization is always server-side.
- Monetary values are integer fils; KWD display uses three decimal places.
- Gateway success/refund confirmation is authoritative; browser redirect is not.
- Idempotency prevents duplicate charge-state application, grant, refund, coupon commit, or notice.
- Catalog price changes never rewrite historical Order/refund/payout values.
- Suspension, unpublishing, entitlement expiry/revocation, and deletion are separate concepts.
- Records with financial, access, moderation, or audit history are not silently hard-deleted.
- Catalog discovery, filtering, and search expose only `PUBLISHED` Courses and never index Lesson
  titles or protected Resource/Lab content.
- Exact storage, table, API, token, and queue designs belong to the system-design phase.
- The commerce design must accommodate required receipt/invoice fields and immutable
  dispute/chargeback events without assuming the unresolved tax or Entitlement policies in
  [LAUNCH_GATES.md](LAUNCH_GATES.md) LG-016/LG-017.
