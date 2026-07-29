# Phase 1 Data Model: S6 — Course Access Invitation and Entitlement Grant

**Date**: 2026-07-29 | **Plan**: [plan.md](plan.md) | **Migration**: `NNNN_course_access_grant` (number derived at implementation)

Every invariant the specification states is expressed here as a **database constraint**, not as
guidance for a handler. Constitution VII and Principle IV both require it, and the S2 D5 work already
proved in this repository that a check without a constraint loses under concurrency.

---

## 1. Ownership and the S4 seam

| Table | Created by | This migration |
|---|---|---|
| `entitlements` | **S4** | Adds `grant_source`, `source_invitation_id`, and the active-uniqueness index if S4 did not |
| **`enrollments`** | **S5** | **Asserted, not created** — see the correction below |
| `course_access_invitations` | **S6** | Created here |

**Enrollment ownership was reassigned to S6 on 2026-07-29** after cross-artifact analysis found that
S4's specification never claimed the table — it names Enrollment only as something that *survives*
revocation, and omits it from its Key Entities. The table therefore had no owner. S6 was chosen
because S6 is the only slice that *writes* to it: the grant transaction creates or reuses an
Enrollment and nothing else in the product does. **S4 remains responsible only for Entitlement and
access evaluation.**

**Corrected to S5 later the same day**, when
[S5's specification](../007-protected-learning/spec.md#c1--s5-needs-the-enrollment-record-and-s6-creates-it)
found that the reassignment created a forward dependency: BR-114 and BR-116 key Progress by
`enrollment_id`, S5 writes Progress on **August 5–6**, and S6 runs on **August 8**. The table Progress
hangs from would not exist when S5 first needed it, which
[SLICES.md §2 rule 2](../../docs/launch/SLICES.md) forbids.

The correction applies the same consumer-defines / producer-populates split this feature already uses
for `entitlements`: **S5 creates `enrollments`; S6 writes to it.** The reasoning that moved the table
off S4 still holds — S6 remains the only writer — but "only writer" is not the same as "first
needer", and the schema must exist for whichever slice needs it first. S6 therefore **asserts** the
expected `enrollments` shape before using it and fails loudly if it diverges, exactly as it already
does for `entitlements`. Migration numbers in this feature are derived at implementation time (M1),
so no number is invalidated.

The migration **asserts** the expected `entitlements` columns exist before altering them, and fails
loudly rather than silently creating a divergent shape. See
[research.md §1](research.md#1-the-s4-seam).

---

## 2. `course_access_invitations`

The workflow record. **Never the access record.**

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID` PK | `gen_random_uuid()` |
| `normalized_email` | `TEXT NOT NULL` | Same normalization as Identity |
| `email` | `TEXT NOT NULL` | Original correspondence form, preserved separately (the S1B1 correction) |
| `course_id` | `UUID NOT NULL` | FK → `courses(id)` |
| `created_by_account_id` | `UUID NOT NULL` | FK → `accounts(id)`, the creating Admin |
| `decided_by_account_id` | `UUID` | FK → `accounts(id)`, the approving or rejecting Admin. Null until decided |
| `accepted_by_account_id` | `UUID` | FK → `accounts(id)`, set at acceptance |
| `state` | `TEXT NOT NULL` | See §3 |
| `decision_reason` | `TEXT` | Required on rejection |
| `admin_note` | `TEXT` | Optional, Admin-only |
| `external_reference` | `TEXT` | Optional, opaque, Admin-only |
| `action_secret_id` | `UUID` | FK → `identity_action_secrets(id)`, current acceptance link |
| `created_at` | `TIMESTAMPTZ NOT NULL` | |
| `accepted_at` | `TIMESTAMPTZ` | |
| `decided_at` | `TIMESTAMPTZ` | |
| `cancelled_at` | `TIMESTAMPTZ` | |

**There is deliberately no amount, currency, payment status, gateway identifier, or payer instrument
column.** SC-012 asserts this against the schema.

### Constraints

| Name | Rule | Enforces |
|---|---|---|
| `cai_state_valid` | `CHECK (state IN ('PENDING_STUDENT_ACCEPTANCE','PENDING_ADMIN_APPROVAL','APPROVED','REJECTED','CANCELLED'))` | BR-168 |
| `cai_one_non_terminal_per_pair` | `UNIQUE (normalized_email, course_id) WHERE state IN ('PENDING_STUDENT_ACCEPTANCE','PENDING_ADMIN_APPROVAL')` — partial index | **FR-003, BR-165.** Race 4 |
| `cai_rejection_needs_reason` | `CHECK (state <> 'REJECTED' OR decision_reason IS NOT NULL)` | FR-022, BR-168 |
| `cai_decided_has_actor` | `CHECK (state NOT IN ('APPROVED','REJECTED') OR decided_by_account_id IS NOT NULL)` | FR-042 |
| `cai_accepted_has_actor` | `CHECK (state IN ('PENDING_STUDENT_ACCEPTANCE','CANCELLED') OR accepted_by_account_id IS NOT NULL)` | FR-010 |
| `cai_email_present` | `CHECK (length(trim(normalized_email)) BETWEEN 3 AND 320)` | Mirrors `staff_invitations` |

**No expiry column and no `EXPIRED` state** (BR-169). The acceptance link expires; the invitation does
not.

**This is a separate table from `staff_invitations`** (BR-171). It shares no state machine, no
uniqueness rule — `staff_invitations` is one-pending-per-email *globally*, this is
one-non-terminal-per-email-**and-Course** — and no account-creation semantics.

---

## 3. Invitation state machine

```text
PENDING_STUDENT_ACCEPTANCE ──accept──→ PENDING_ADMIN_APPROVAL ──approve──→ APPROVED
            │                                    │
            │                                    └──reject────────────────→ REJECTED
            └──cancel──→ CANCELLED               └──cancel───────────────→ CANCELLED
```

| From | To | Actor | Guard |
|---|---|---|---|
| `PENDING_STUDENT_ACCEPTANCE` | `PENDING_ADMIN_APPROVAL` | Student | Normalized email matches; Account is Active and a Student |
| `PENDING_STUDENT_ACCEPTANCE` | `CANCELLED` | Admin | Capability |
| `PENDING_ADMIN_APPROVAL` | `APPROVED` | Admin | Capability, recent auth, Course grantable, future expiry configured |
| `PENDING_ADMIN_APPROVAL` | `REJECTED` | Admin | Capability, reason present |
| `PENDING_ADMIN_APPROVAL` | `CANCELLED` | Admin | Capability |

`APPROVED`, `REJECTED`, and `CANCELLED` are **terminal**. Every other transition is refused with
`409 invitation-state-conflict`. A new invitation may follow a terminal one for the same pair
(FR-023), which the partial index permits because terminal states are excluded from it.

---

## 4. `entitlements` — S6's additions

| Column | Type | Notes |
|---|---|---|
| `grant_source` | `TEXT NOT NULL` | The typed discriminator |
| `source_invitation_id` | `UUID` | FK → `course_access_invitations(id)`. Required when `grant_source = 'MANUAL_INVITATION'` |

| Constraint | Rule | Enforces |
|---|---|---|
| `ent_grant_source_implemented` | `CHECK (grant_source = 'MANUAL_INVITATION')` | **FR-019, FR-020, BR-028.** Reserved values are *not* permitted — a future source is added by amending this constraint in its own migration, which makes adding one a reviewable event rather than a silent insert |
| `ent_manual_needs_invitation` | `CHECK (grant_source <> 'MANUAL_INVITATION' OR source_invitation_id IS NOT NULL)` | FR-021, BR-113 |
| `ent_one_active_per_student_course` | `UNIQUE (student_account_id, course_id) WHERE state = 'ACTIVE'` — partial index | **FR-016, BR-024.** Races 1 and 6 |

> The `CHECK` permitting only `MANUAL_INVITATION` is deliberately strict. Declaring the reserved
> values now would let a future insert quietly use one; requiring a migration to widen it means adding
> `PAID_ORDER` is a diff someone reviews. The reserved names live in documentation, not in a
> permissive constraint.

---

## 5. `enrollments` — created by S5, written only by S6

The durable Student-to-Course learning relationship (DOMAIN_MODEL §4). **Created by S5** (§1), which
needs it first for Progress under BR-114 and BR-116; written only by this feature's grant
transaction; read by S8's Instructor roster and by S5's Progress.

The shape below is the contract S6 **asserts** before writing. If S5 shipped a divergent shape, this
migration fails loudly rather than altering the table into agreement.

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID` PK | `gen_random_uuid()` |
| `student_account_id` | `UUID NOT NULL` | FK → `accounts(id)` |
| `course_id` | `UUID NOT NULL` | FK → `courses(id)` |
| `created_at` | `TIMESTAMPTZ NOT NULL` | First grant for this pair |

| Constraint | Rule | Enforces |
|---|---|---|
| `enr_one_per_student_course` | `UNIQUE (student_account_id, course_id)` | **Principle IV**, and it keeps Progress single-homed under BR-116's `UNIQUE(enrollment_id, lesson_id)` identity |

Enrollment survives Entitlement expiry and revocation (BR-026, DOMAIN_MODEL §4). Nothing in this
feature deletes one.

Approval **reuses** an existing Enrollment and never creates a second (BR-167).

---

## 6. The grant transaction

One transaction. Partial completion is impossible (FR-015).

```text
BEGIN
  1. SELECT course              FOR SHARE     -- lifecycle state + default_access_ends_at
  2. SELECT invitation          FOR UPDATE    -- re-assert state HERE, not before BEGIN
  3. if state = APPROVED        -> return the existing grant (idempotent success, race 1)
     if state <> PENDING_ADMIN_APPROVAL -> 409 invitation-state-conflict
  4. assert course grantable    -> else 409 course-not-grantable
  5. assert default_access_ends_at is present and in the future -> else 422 validation-failed
  6. SELECT enrollment          FOR UPDATE, or INSERT
  7. INSERT entitlement:
       grant_source              = 'MANUAL_INVITATION'
       source_invitation_id      = invitation.id
       original_access_ends_at   = course.default_access_ends_at   (snapshot, BR-025)
       access_ends_at            = same value initially            (BR-026)
       retirement_eligibility_at = now()                           (approval instant, BR-027)
       state                     = 'ACTIVE'
  8. UPDATE invitation -> APPROVED, decided_by, decided_at
  9. INSERT audit events: invitation approved + entitlement granted
 10. Append outbox intent: access granted (NoticeDelivery)
COMMIT
```

Steps 9 and 10 are **inside** the transaction. An audit record or delivery intent written outside it
can be lost while the grant stands, which is the failure the staff-invitation code's own comment warns
about.

A unique violation at step 7 means a concurrent grant won (race 6) → `409 already-has-active-access`,
never a 500.

---

## 7. Audit events

Appended to the existing `audit_events` table. Each records actor, target, reason where applicable,
and timestamp (FR-031).

`COURSE_ACCESS_INVITATION_CREATED` · `_ACCEPTED` · `_APPROVED` · `_REJECTED` · `_CANCELLED` ·
`_LINK_REISSUED` · `ENTITLEMENT_GRANTED`

The action-secret purpose and security-event allowlists are closed enumerations widened by migration
(the `0007` precedent), so the new purpose and event types are added there rather than being free
text.

---

## 8. Invariant-to-constraint map

The completeness check. Every specification invariant must appear here with a database constraint, or
it is not enforced.

| Spec | Invariant | Constraint |
|---|---|---|
| FR-003 | One non-terminal invitation per email and Course | `cai_one_non_terminal_per_pair` |
| FR-016 | One active Entitlement per Student and Course | `ent_one_active_per_student_course` |
| Principle IV | One Enrollment per Student and Course | `enr_one_per_student_course` |
| FR-019/020 | Only the implemented grant source | `ent_grant_source_implemented` |
| FR-021 | Manual grants reference their invitation | `ent_manual_needs_invitation` |
| FR-022 | Rejection carries a reason | `cai_rejection_needs_reason` |
| FR-042 | Both actors recorded | `cai_decided_has_actor`, `created_by_account_id NOT NULL` |
| BR-168 | Only defined states exist | `cai_state_valid` |

**Nothing in this table is enforced only in Go.** A task that implements one of these as a handler
check has not implemented it.
