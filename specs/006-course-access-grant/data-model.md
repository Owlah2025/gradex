# Phase 1 Data Model: S6 — Course Access Invitation and Entitlement Grant

**Date**: 2026-07-29 | **Plan**: [plan.md](plan.md) | **Migration**: `0015_course_access_grant` (recalculated 2026-08-06 from the committed schema)

Every invariant the specification states is expressed here as a **database constraint**, not as
guidance for a handler. Constitution VII and Principle IV both require it, and the S2 D5 work already
proved in this repository that a check without a constraint loses under concurrency.

> **Reconciled 2026-08-06 against the committed schema at the S5 closure head `d5ce557`.** Corrections
> are marked inline. In summary: the migration number is `0015`; `entitlements` already carries four of
> the five things §4 assigns to S6; `enrollments` matches §5 exactly and needs only re-verification;
> `courses.default_access_ends_at`, which §6 step 5 reads, **does not exist** and is now S6's under
> [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it);
> and §7's claim that the audit event types live in a closed enumeration is wrong for `audit_events`.
> **No invariant, constraint semantic, or business rule changed.**

---

## 1. Ownership and the S4 seam

| Table | Created by | This migration |
|---|---|---|
| `entitlements` | **S4**, in `0012_media_and_entitlement` | **Asserts** the four elements S4 already shipped; **adds** the `source_invitation_id` foreign key and `ent_manual_needs_invitation` — see §4 |
| **`enrollments`** | **S5**, in `0013_enrollments` | **Asserted, not created.** Verified 2026-08-06 to match §5 exactly |
| **`courses`** | **S1/S2**, `0001` + `0009` | **Adds `default_access_ends_at TIMESTAMPTZ` (nullable)** — the BR-025 instant that no closed slice created, assigned to S6 under [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it) |
| `identity_action_secrets` | **S1**, `0005`, widened by `0007`/`0008` | **Widens two closed `CHECK`s**: the purpose allowlist gains `COURSE_ACCESS_INVITATION`, and `identity_action_secrets_account_id_purpose` gains it on the null-`account_id`-permitted arm |
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

## 4. `entitlements` — S4 already shipped four of these five

> **Reconciled 2026-08-06.** This section was written expecting S6 to add the columns, the grant-source
> `CHECK`, and the active-uniqueness index. **S4's `0012_media_and_entitlement` already shipped all four**,
> under its own constraint names. Exactly **one** row below is genuinely S6's, plus the foreign key that
> could not exist before `course_access_invitations` did. A migration that re-created the four would fail
> on duplicate object, and one that dropped and re-added them would edit an applied shape — which D-031
> and `scripts/docs-guard.sh` §5 forbid.

| Column | Type | State at the S5 closure head |
|---|---|---|
| `grant_source` | `TEXT NOT NULL` | **Exists** (S4). The typed discriminator |
| `source_invitation_id` | `UUID` | **Exists** (S4), nullable, **with no foreign key** — the target table did not exist yet |

| Constraint | Rule | State |
|---|---|---|
| `entitlements_grant_source_implemented` | `CHECK (grant_source = 'MANUAL_INVITATION')` | **Exists** (S4). Planned here as `ent_grant_source_implemented`; the live name is authoritative. Reserved values are *not* permitted — a future source is added by amending this constraint in its own migration, which makes adding one a reviewable event rather than a silent insert. **FR-019, FR-020, BR-028** |
| `entitlements_grant_source_present` | `CHECK (length(trim(grant_source)) > 0)` | **Exists** (S4), not planned here. Retained; it forbids a whitespace grant source independently of the allowlist |
| `entitlements_one_active_student_course` | `UNIQUE (student_account_id, course_id) WHERE state = 'ACTIVE' AND scope_kind = 'COURSE'` | **Exists** (S4). Planned here as `ent_one_active_per_student_course` with no `scope_kind` predicate; the live predicate is **narrower** and is coextensive with S6's whole-Course-only writes. **FR-016, BR-024. Races 1 and 6** |
| `ent_manual_needs_invitation` | `CHECK (grant_source <> 'MANUAL_INVITATION' OR source_invitation_id IS NOT NULL) NOT VALID` | **Created by S6** in `0015_course_access_grant.up.sql`. Added `NOT VALID` so pre-0015 legacy `entitlements` rows survive without invalidation or fake invitation fabrication. See §4.1 |
| `fk_entitlements_source_invitation` | `FOREIGN KEY (source_invitation_id) REFERENCES course_access_invitations (id)` | **S6 creates it**, in the same migration that creates the referenced table. Not previously named here |

**S6's migration asserts the four existing elements and fails loudly if any is absent or has a different
shape**, the same treatment §5 gives `enrollments`. It does not drop, recreate, or redefine them.

> The `CHECK` permitting only `MANUAL_INVITATION` is deliberately strict. Declaring the reserved
> values now would let a future insert quietly use one; requiring a migration to widen it means adding
> `PAID_ORDER` is a diff someone reviews. The reserved names live in documentation, not in a
> permissive constraint.

### 4.1 Residual Migration Risk, Grandfathered Rows, and S8 Gate

Migration `0015_course_access_grant` adds constraint `ent_manual_needs_invitation` as `NOT VALID`:
```sql
ALTER TABLE entitlements ADD CONSTRAINT ent_manual_needs_invitation
  CHECK (grant_source <> 'MANUAL_INVITATION' OR source_invitation_id IS NOT NULL) NOT VALID;
```

**Trade-off & Consequence Analysis:**
- **Legacy Preservation:** Pre-0015 `entitlements` rows created with `grant_source = 'MANUAL_INVITATION'` and null `source_invitation_id` survive migration 0015 without invalidation or fake invitation fabrication because `NOT VALID` skips existing row verification.
- **Enforcement Scope:** PostgreSQL enforces `NOT VALID` constraints on all subsequent `INSERT` and `UPDATE` statements. Consequently, grandfathered legacy rows cannot undergo `UPDATE` unless their `source_invitation_id` provenance is reconciled first.
- **Rollback Provenance Destruction:** Migration `0015_course_access_grant.down.sql` clears `source_invitation_id` (`UPDATE entitlements SET source_invitation_id = NULL WHERE source_invitation_id IS NOT NULL`) before dropping `course_access_invitations`. A production rollback executed after real S6 grant invitations exist will destroy invitation references while preserving `entitlements` rows. Upon re-upgrade, those rows become grandfathered and un-updatable.
- **S8 Dependency Gate:** S8 implements Admin Entitlement expiry adjustments (BR-026). Before S8 ships any production Entitlement `UPDATE` path:
  1. The project must define an approved provenance reconciliation and backfill strategy for legacy and rolled-back rows.
  2. The constraint must be validated in PostgreSQL (`ALTER TABLE entitlements VALIDATE CONSTRAINT ent_manual_needs_invitation`).
  3. Production rollbacks after real S6 grants must be treated as provenance-destructive operational events requiring manual recovery.

---

## 5. `enrollments` — created by S5, written only by S6

The durable Student-to-Course learning relationship (DOMAIN_MODEL §4). **Created by S5** (§1), which
needs it first for Progress under BR-114 and BR-116; written only by this feature's grant
transaction; read by S8's Instructor roster and by S5's Progress.

The shape below is the contract S6 **asserts** before writing. If S5 shipped a divergent shape, this
migration fails loudly rather than altering the table into agreement.

> **Verified 2026-08-06: S5 shipped exactly this shape.** `0013_enrollments.up.sql` matches column for
> column, type for type, nullability for nullability, and **constraint name for constraint name** —
> `enr_one_per_student_course`. Both foreign keys are present (`accounts (id)`, `courses (id)`) and
> `created_at` carries `DEFAULT now()`. **No production `INSERT INTO enrollments` exists anywhere**: every
> insert in the repository is in a `_test.go` file or under `cmd/e2e-seed`, which is entirely
> `//go:build !production`. `internal/learning` only ever `SELECT`s the table and keys Progress by
> `enrollment_id`. S6 remains the only production writer. `T001a` is therefore a re-verification against
> the head being built, not an open question.

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

> **Step 5 reads a column that does not exist yet.** `courses.default_access_ends_at` is required by
> BR-025 and was created by no closed slice. Migration `0015` adds it under
> [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it),
> nullable, because BR-025 makes its absence a refusal condition rather than an invalid state. Until it
> exists, step 5 always fails and no grant can ever complete.

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
(the `0007` precedent), so the new purpose is added there rather than being free text.

> **Corrected 2026-08-06: `audit_events` is not a closed enumeration.** This paragraph read as though the
> audit event types also needed a migration. They do not. `audit_events.action` carries only a **format**
> constraint — `CHECK (action ~ '^[A-Z][A-Z0-9_]*$')` — plus an append-only trigger
> (`audit_events_append_only`) and the usual presence checks on actor, target, reason, and metadata. The
> seven S6 actions above therefore need no schema change.
>
> **Which is exactly why `T065` matters.** With no allowlist, nothing in the database prevents a new
> transition from shipping with no audit record at all; the enumeration test is the only enforcement. Two
> allowlists *are* closed and *do* need widening, and both are in `identity_action_secrets` rather than
> `audit_events`: `identity_action_secrets_purpose` gains `COURSE_ACCESS_INVITATION`, and
> `identity_action_secrets_account_id_purpose` must gain it on the arm that permits a null `account_id`,
> because the invited address may have no Account. `identity_security_events_type` is likewise a closed
> `CHECK` (widened by `0006`, `0007`, `0008`) and is widened only if S6 records a security event.

---

## 8. Invariant-to-constraint map

The completeness check. Every specification invariant must appear here with a database constraint, or
it is not enforced.

Constraint names below are the **live** ones, corrected 2026-08-06. Where this document originally
guessed a name for something S4 or S5 had already shipped, the shipped name wins — a test asserting the
planned name would pass vacuously against a table that does not have it.

| Spec | Invariant | Live constraint | Owner |
|---|---|---|---|
| FR-003 | One non-terminal invitation per email and Course | `cai_one_non_terminal_per_pair` | **S6 creates** |
| FR-016 | One active Entitlement per Student and Course | `entitlements_one_active_student_course` *(planned as `ent_one_active_per_student_course`)* | **S4 shipped**; S6 asserts |
| Principle IV | One Enrollment per Student and Course | `enr_one_per_student_course` | **S5 shipped**; S6 asserts |
| FR-019/020 | Only the implemented grant source | `entitlements_grant_source_implemented` *(planned as `ent_grant_source_implemented`)* | **S4 shipped**; S6 asserts |
| FR-021 | Manual grants reference their invitation | `ent_manual_needs_invitation` | **S6 creates — currently unenforced** |
| FR-021/BR-113 | The referenced invitation exists | `fk_entitlements_source_invitation` | **S6 creates** |
| FR-022 | Rejection carries a reason | `cai_rejection_needs_reason` | **S6 creates** |
| FR-042 | Both actors recorded | `cai_decided_has_actor`, `created_by_account_id NOT NULL` | **S6 creates** |
| BR-168 | Only defined states exist | `cai_state_valid` | **S6 creates** |
| BR-025 | The snapshotted instant has a source | `courses.default_access_ends_at` exists and is readable | **S6 creates** — see [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it) |

**Nothing in this table is enforced only in Go.** A task that implements one of these as a handler
check has not implemented it.

**The three rows marked "asserts" are not weaker for being inherited — they are stronger.** They were
created and independently reviewed by closed slices before the code that depends on them existed, which
is the property [SLICES.md §3.1](../../docs/launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation)
was arranged to produce. `T072`'s completeness test must assert them **by their live names**, and
`T052`'s index-drop mutation must drop `entitlements_one_active_student_course` — dropping a
non-existent `ent_one_active_per_student_course` would be a silent no-op and the mutation check would
pass while proving nothing.
