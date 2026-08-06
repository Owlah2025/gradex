# Phase 0 Research: S6 — Course Access Invitation and Entitlement Grant

**Date**: 2026-07-29 | **Plan**: [plan.md](plan.md) | **Spec**: [spec.md](spec.md)

Every unknown in the plan's Technical Context is resolved here. Nothing is left as
NEEDS CLARIFICATION. Where a decision reuses an existing repository mechanism, the mechanism is named
and its precedent cited, so implementation extends proven code rather than re-deriving it.

---

## 1. The S4 seam

**Decision**: S6 assumes S4 has delivered `backend/internal/access` containing the Entitlement record
and its evaluator. S6 adds the producer and **must verify the shape before starting**.

> **Verified 2026-08-06 against the S5 closure head `d5ce557`. Writing this as a checkable precondition
> was the right call, because the check caught a real mismatch.**
>
> **The package name is wrong.** S4 landed **`backend/internal/entitlement`** and there is no
> `internal/access` directory. `internal/access` is therefore **new and S6's to create**, and
> `internal/entitlement` is S4's and out of scope for this slice. Two of the three rows below hold; the
> third does not. Results are per row, and [plan.md](plan.md#module-placement) carries the resolution.

**Expected precondition — checkable, not assumed:**

| S4 must have delivered | Why S6 needs it | Verified result |
|---|---|---|
| An `entitlements` table with scope, `original_access_ends_at`, effective `access_ends_at`, `retirement_eligibility_at`, revocation state | S6 writes every one of these fields in the grant transaction | **Holds.** `0012_media_and_entitlement.up.sql` delivers all five, plus `grant_source`, `source_invitation_id`, `state`, `revision`, and an append-only `entitlement_adjustments` table |
| An evaluator that answers "may this Student reach this Lesson?" from the Entitlement alone | S6's E2E proof drives playback after approval and must not write its own evaluator | **Holds.** `internal/entitlement.Evaluator` with `Evaluate`, `EvaluateInTransaction(ctx, tx, …)`, `EvaluateTarget`, `EvaluateRead`, `EvaluateCourseReads`. `EvaluateInTransaction` is exactly what the grant transaction needs |
| A transaction helper on the package repository | S6's grant transaction extends it rather than opening its own pool | **Does not hold.** `internal/entitlement.Repository` exposes only the unexported `readerFor(tx pgx.Tx)` and opens no transaction. No backend package has a shared transaction helper; each repository calls `pool.Begin(ctx)` itself — `internal/catalog/repository.go:79`, `internal/identity/staff.go:49`, `internal/learning/report.go:168`. S6 follows that house pattern |

**Three further facts the verification turned up. Each changes a task, not a requirement:**

1. **S4 already implemented most of what [data-model.md §4](data-model.md#4-entitlements--s4-already-shipped-four-of-these-five)
   assigns to S6.** `entitlements` already carries `grant_source TEXT NOT NULL`,
   `source_invitation_id UUID`, `CONSTRAINT entitlements_grant_source_implemented CHECK (grant_source = 'MANUAL_INVITATION')`,
   and `CREATE UNIQUE INDEX entitlements_one_active_student_course … WHERE state = 'ACTIVE' AND scope_kind = 'COURSE'`.
   S6's migration **asserts** these rather than creating them. What genuinely remains is the
   `source_invitation_id` foreign key — impossible before `course_access_invitations` exists — and the
   `ent_manual_needs_invitation` CHECK, which S4 did **not** ship: `source_invitation_id` is nullable
   with nothing requiring it, so FR-021 and BR-113 are not yet enforced by the database.
2. **The Go type vocabulary already exists.** `internal/entitlement/types.go` exports `GrantSource`,
   `const GrantSourceManualInvitation GrantSource = "MANUAL_INVITATION"`, `ScopeKind`, `ScopeCourse`,
   `State`, `StateActive`, and a `Record` struct covering every column S6 writes. S6 consumes these and
   defines no parallel set — a second `GrantSource` enum would be the duplication FR-027 forbids, in Go
   rather than in SQL.
3. **The production-exclusion precedent already exists.** `internal/entitlement/production_exclusion_test.go`
   proves `seed_nonprod.go` is absent from a `-tags=production` build and that the package still builds
   under that tag; every `cmd/e2e-seed` file is `//go:build !production`. SC-006 and FR-020 extend this
   proven pattern instead of inventing one, and must also assert the `cmd/e2e-seed` entitlement inserts
   stay production-excluded.

**`enrollments` is deliberately absent from this table.** *(Verified separately on 2026-08-06:
`0013_enrollments.up.sql` matches [data-model.md §5](data-model.md#5-enrollments--created-by-s5-written-only-by-s6)
column for column and constraint name for constraint name, and no production `INSERT INTO enrollments`
exists anywhere — every insert is in a `_test.go` file or under the `!production`-tagged
`cmd/e2e-seed`. `internal/learning` only `SELECT`s it.)* Cross-artifact analysis on 2026-07-29 found
S4 never claimed it, so it had no owner. It was briefly assigned to S6 and then **reassigned to S5**
the same day, because S5 writes Progress on D5–D6 and BR-116 keys Progress by `enrollment_id` — the
table must exist before S6 runs on D8. **S5 creates the table; S6 asserts the inherited shape and is
its only production writer** (see [data-model.md §5](data-model.md#5-enrollments--created-by-s5-written-only-by-s6)
and [S5's C1](../007-protected-learning/spec.md#c1--s5-needs-the-enrollment-record-and-s6-creates-it)).
S4's responsibility is Entitlement and access evaluation only.

**If S4 lands a different shape for the three rows above, this plan is revised before
implementation.** That is a genuine interface-compatibility stop condition — but it is now about the
Entitlement record and the evaluator, not about a missing table.

**Rationale**: S4 is specified but unimplemented, so this plan is necessarily written against an
expected interface. Stating it as a precondition table makes the assumption falsifiable in one check
instead of discovering the mismatch halfway through the grant transaction.

**Alternatives considered**: Having S6 create the entitlement tables itself — rejected, because S4's
FR-011 already claims that record and duplicating it would produce two owners for one invariant.
Deferring S6 planning until S4 ships — rejected, because planning and implementation run concurrently
under D-040 and the seam is narrow enough to state precisely.

---

## 2. Capability model

**Decision**: add `CapCourseAccessGrant Capability = "COURSE_ACCESS_GRANT"` to
`backend/internal/identity/policy.go`, register it in `AllCapabilities` and the `Authorize` switch,
and grant it to the **Admin role only**.

> **Corrected 2026-08-06: `policy_set.go` is not the role map.** This section said the Admin grant goes
> in `policy_set.go`. That file holds *registration policy documents* — `PolicyKind`,
> `PolicyPrivacyNotice`, `PolicyTermsOfService`, `RegistrationPolicySet`, `Locale`,
> `PolicySetResolver` — and contains no capability reference at all. Role-to-capability grants live
> **entirely inside the `Authorize` switch in `policy.go`**, in the `case RoleAdmin:` arm next to
> `CapCatalogPublish`, `CapCatalogPricing`, and `CapCatalogTaxonomy`. All three edits — the `const`
> block, `AllCapabilities`, and the `RoleAdmin` arm — are in `policy.go`. Adding a capability to
> `policy_set.go` would compile and grant nothing, and the deny-by-default `Authorize` fallthrough means
> the failure would be a silent refusal rather than a build error.
>
> The verified live set is twelve capabilities: `CapPasswordChange`, `CapSessionTerminate`,
> `CapAdminOperations`, `CapFinancialOperations`, `CapSecurityOperations`, `CapRetentionOperations`,
> `CapProviderOperations`, `CapContentManagement`, `CapLearningAccess`, `CapCatalogPublish`,
> `CapCatalogPricing`, `CapCatalogTaxonomy`. `COURSE_ACCESS_GRANT` makes thirteen. The rationale below
> holds unchanged: `CapFinancialOperations` exists and is deliberately **not** reused.

**Rationale**: the existing model already has six privileged capabilities plus three catalog ones
(`CATALOG_PUBLISH`, `CATALOG_PRICING`, `CATALOG_TAXONOMY`), and the S2 pattern of one capability per
privileged concern is proven. Granting paid access is the money-equivalent action in a platform with
no money in it, so it earns its own subject rather than riding on `ADMIN_OPERATIONS`.

This is also the exact correction S1C's round-3 rejection produced: suspension had been gated on
`ADMIN_OPERATIONS` where the frozen spec required `SECURITY_OPERATIONS`. A capability chosen too
broadly is a finding in this project's history, not a hypothetical.

**Alternatives considered**: reusing `ADMIN_OPERATIONS` — rejected as too broad, and it would make the
grant power inseparable from ordinary admin work. Reusing `FINANCIAL_OPERATIONS` — rejected because no
money moves through Gradex, so the name would mislead every future reader.

---

## 3. Recent authentication

**Decision**: reuse `identity.CheckRecentAuthentication(session, window, now)` with the **Admin
financial/security window** from typed configuration, and let it refuse rather than default.

**Rationale**: `backend/internal/identity/session.go` already implements this and already returns
`ErrRecentAuthRequired` when **no window is configured** — it fails closed rather than falling back to
a permissive default. That behaviour was itself an S1C remediation finding: round one introduced "a
silent recent-auth default" and it was rejected. Reusing the hardened function inherits that
correction; hand-rolling a window would re-introduce the defect.

**Alternatives considered**: a dedicated grant-specific window — rejected as an unnecessary
configuration knob (Constitution VI) when the security window already exists and fits. Skipping
recent-auth — rejected outright; FR-014 requires it.

---

## 4. The acceptance link

**Decision**: reuse `identity_action_secrets` with a new purpose value, following
`backend/internal/identity/invitation.go` exactly, including its co-committed outbox delivery intent.

> **Two constraints must be widened, not one — verified 2026-08-06.** `identity_action_secrets` carries
> a closed purpose allowlist that `0007` and `0008` widened by migration, currently
> `CHECK (purpose IN ('EMAIL_VERIFICATION', 'PASSWORD_RESET', 'STAFF_INVITATION'))`. Migration `0015`
> adds `'COURSE_ACCESS_INVITATION'` to it.
>
> **It must also widen `identity_action_secrets_account_id_purpose`**, which `0008` added to couple
> nullability to purpose — `EMAIL_VERIFICATION` and `PASSWORD_RESET` require `account_id`, and
> `STAFF_INVITATION` permits it to be null because the address may have no Account. A Course Access
> Invitation targets exactly that case (FR-007, and the assumption in `spec.md` that the notice reaches
> an address that may have no Account), so the new purpose must be added to the null-permitted arm. This
> was uncovered by `T007`, which named only the purpose allowlist, and is now explicit.
>
> **`audit_events` needs no migration.** `data-model.md` §7 stated that the audit event types are added
> to a closed enumeration by migration on the `0007` precedent. That is true of the action-secret purpose
> and of `identity_security_events_type`, both of which are closed `CHECK`s. It is **not** true of
> `audit_events`: its `action` column carries only a *format* constraint,
> `CHECK (action ~ '^[A-Z][A-Z0-9_]*$')`, plus an append-only trigger. The seven S6 audit actions
> therefore need no schema change — which is why `T065`'s enumeration test is the only thing standing
> between a new transition and an unaudited one.

**Rationale**: the staff invitation path is the working precedent for a link delivered to an email
address that may have no Account. It already provides expiry, single use, purpose binding, digest-only
storage, and — critically — an outbox `VerificationDelivery` carrying a **destination address rather
than an Account reference**. That last property is exactly what BR-123's relational recipient snapshot
cannot supply for a not-yet-registered Student, and it is why no new mechanism is needed.

Reissue invalidates prior secrets for the same invitation by superseding them, which the action-secret
model already supports.

**Alternatives considered**: a bespoke token table for course-access invitations — rejected as
duplicated security-sensitive machinery (Constitution VI); every property needed already exists.
Making the invitation itself expire — rejected by BR-169 and would invent a duration.

---

## 5. Notification intents

**Decision**: reuse `outbox.Writer.Append` with `outbox.VerificationDelivery` for the invitation
notice (it carries an actionable secret) and `outbox.NoticeDelivery` for access-granted, rejected, and
cancelled notices (they carry none). All are co-committed inside the transaction that causes them.

**Rationale**: the two delivery types already exist and are deliberately distinct — the doc comment on
`NoticeDelivery` states the separation exists so ciphertext cannot be read as carrying a usable token
and a consumer cannot mistake an absent token for a blank one. Using the right one per event preserves
that property. Co-committing matches the staff-invitation comment: an intent written outside the
transaction can be lost, and nothing downstream would report it.

**Alternatives considered**: a single delivery type with an optional token — explicitly rejected by the
existing code's own rationale. Best-effort send outside the transaction — rejected by BR-120 and by the
outbox's whole reason for existing.

---

## 6. Response classes

**Decision**: RFC 9457 Problem Details through the existing `internal/problem` envelope, with these
distinguishable types:

| Condition | Status | Type suffix |
|---|---|---|
| Missing capability or stale recent authentication | 403 | `insufficient-capability` / `recent-authentication-required` |
| Invitation not found, or not addressed to this identity | 404 | `not-found` |
| Invitation state does not permit the transition | 409 | `invitation-state-conflict` |
| A non-terminal invitation already exists for this pair | 409 | `duplicate-invitation` |
| The Student already holds active access to this Course | 409 | `already-has-active-access` |
| Course lifecycle forbids granting | 409 | `course-not-grantable` |
| **Target email belongs to a non-Student Account** | **409** | **`ineligible-recipient`** *(added 2026-08-06: the contract already specified this class for FR-004/BR-082 and this table omitted it)* |
| Acceptance link expired, consumed, or superseded | 410 | `acceptance-link-expired` *(added 2026-08-06: specified in the contract and asserted by `T031`, omitted here)* |
| Missing reason, or Course has no future expiry instant | 422 | `validation-failed` |

> **The Student routes must not reuse `requireProtectedLearningAccess`.** That middleware
> (`internal/httpapi/media_delivery_handlers.go:112`) is the guard on `/learn/*` and the protected media
> paths. It authenticates, resolves the principal, checks `CapLearningAccess`, and on any failure calls
> `writeProtectedUnavailable` — a deliberately **uniform** refusal that discloses nothing. That is
> correct for protected content and wrong here: reusing it would collapse this table's 403/404/409/410/422
> classes into one indistinguishable response and silently defeat FR-009's byte-identical-404 requirement
> by making *everything* byte-identical. S6's Student guard is its own, in `access_foundation.go`, and
> emits the `internal/problem` envelope.
>
> `CapLearningAccess` is still the right capability class for the Student surfaces: `Authorize` grants it
> to every Active Student regardless of Entitlement, and the doc comment states that whether this Student
> may reach this Course is a separate Entitlement decision. An invited Student holding no Entitlement
> therefore passes the capability check and can reach their own acceptance screen.

**Rationale**: the envelope is already applied across `/api/v1` from S0, and S1C's round-3 finding was
partly that distinct failures had collapsed into indistinguishable responses. Keeping conflict classes
separate is what lets the Admin UI say what actually happened, and what lets a test assert the right
refusal rather than merely a refusal.

**Anti-enumeration note**: an invitation addressed to a different identity returns **404, not 403**, so
holding a link reveals nothing about whether it exists. This matches the BR-001/BR-003 posture applied
throughout admission.

**Alternatives considered**: a single 409 for all conflicts — rejected; it makes the four cases
untestable apart. 403 for wrong-identity — rejected as an enumeration leak.

---

## 7. Idempotency key

**Decision**: the grant transaction is idempotent **by the invitation identifier**, with state
re-asserted inside the transaction under `FOR UPDATE`.

**Rationale**: Constitution IV requires idempotency by a stable identifier. The invitation ID is the
natural one here — it is stable, unique, already the subject of the request, and one-to-one with the
grant it produces. The deferred payment rules use the Order identifier for the same reason; when
payment returns, that becomes a second key against the same boundary rather than a second model.

The database is the backstop, not the handler: the partial unique index on active Entitlements makes a
double grant impossible even if the state re-assertion were wrong.

**Alternatives considered**: a client-supplied idempotency key — rejected as unnecessary state for an
operation whose natural key already exists. Relying on the state check alone — rejected; a check
without a constraint loses under concurrency, which is exactly the failure the S2 D5 work was built to
prevent.

---

## 8. Frontend placement

**Decision**: two route directories under the existing localized shell — `[locale]/access/*` for ST03,
ST04, and ST10, and `[locale]/admin/course-access/*` for AD06 and AD07 — reusing the S3 bilingual shell,
locale persistence, and RTL/LTR handling.

> **Corrected 2026-08-06.** This section originally said `(student)/access/*` and
> `(admin)/course-access/*`. **Neither route group exists.** The live tree under
> `frontend/src/app/[locale]/` is unparenthesised — `admin/catalog`, `catalog/[idOrSlug]`,
> `instructor/courses`, `learn/courses`, `learn/dashboard` — and parenthesised groups appear only at
> `frontend/src/app/(auth)/` (login, register, recover, verify-email, onboard), outside `[locale]`.
> Inventing `(student)` and `(admin)` groups would add a layout convention this codebase does not use.
>
> The Student surfaces sit at `[locale]/access`, **not** under `[locale]/learn`. `learn` is the entitled
> area; an invited Student arriving from an emailed link holds no Entitlement yet, so nesting acceptance
> inside the learning area would gate acceptance behind the very access it exists to obtain. Shared
> components go under `frontend/src/components/access/`, following the existing `components/learning/`
> and `components/admin/` split.

**Rationale**: S3 owns the shell and each later slice ships its own screens on it, per the SLICES
coverage map. No new layout primitive is required.

**Build-evidence note**: a frontend production build offered as evidence must clear `.next` first
(`CARRYOVER-LOCAL-BUILD-CACHE`, in force since August 2). A build claim that does not say "clean"
reads as not having been made.

**Alternatives considered**: folding the Student access screens into the dashboard route — rejected
because ST03's acceptance surface is entered from an emailed link and needs its own addressable route
with a preserved return destination.

---

## 9. Return-destination preservation

**Decision**: reuse the validated `returnTo` mechanism closed by S1B3 Must 5
(`CARRYOVER-S1B2-RETURNTO`), which already carries a validated destination across every admission hop.

**Rationale**: FR-011 requires an invited Student with no Account to register, verify, and arrive back
at acceptance. That is precisely the multi-hop journey the carryover fixed, and the destination
validation is what stops the invitation link becoming an open redirect.

**Alternatives considered**: storing the pending invitation in the session — rejected; it would create
a second source of truth about which invitation is being accepted, and the URL-carried destination is
already validated and proven.

---

## 10. The Course access-expiry instant does not exist *(added 2026-08-06)*

**Finding**: BR-025 requires that "before a Course Access Invitation for a Course can be **approved**, an
Admin must have configured a future Course `default_access_ends_at` instant," which approval then
snapshots onto the Entitlement. `spec.md` lists the Course as read-only for "lifecycle state and
configured access-expiry instant," and [data-model.md §6](data-model.md#6-the-grant-transaction) step 5
asserts the value is present and in the future.

**No migration creates it.** Verified across `0001`–`0014`: `courses` carries `id`, `title`,
`instructor_id`, `created_at`, and — added by `0009` — `owner_account_id`, `lifecycle`,
`live_revision_id`, `access_suspended_at`, `access_suspension_reason`, `retired_at`, `updated_at`, plus
`0011`'s search columns. There is no `default_access_ends_at`, and no access-duration column on
`courses` or `course_revisions` under any other name. The only expiry columns in the schema are
`entitlements.original_access_ends_at` and `entitlements.access_ends_at`, which are the snapshot
*targets*. `course_price_changes` and `course_sections.price_minor_units` carry price, not duration.

**Decision**: S6 owns the column and its Admin configuration surface, under
[D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it).
Migration `0015` adds `courses.default_access_ends_at TIMESTAMPTZ`, **nullable** — BR-025 makes its
absence a refusal condition rather than an invalid state, and `NOT NULL` would require inventing a
default duration no approved rule supplies.

**Rationale**: the owner is derived, not chosen. S2 is closed and frozen at `785d71c`; S4 and S5 are
closed and did not create it; [SLICES.md §2 rule 2](../../docs/launch/SLICES.md#1-rules) forbids a
forward dependency onto S8. S6 is the first and only slice that needs it, which is the same
consumer-before-producer test that assigned `enrollments` to S5 and `entitlements` to S4. Leaving it
unowned is not an option: FR-017 would refuse every approval, FR-015 would never execute, and the
product's only grant path would be unreachable.

**BR-025's local-date conversion comes with it.** The rule states that when an Admin enters a
Kuwait-local calendar date, the platform persists the exclusive boundary as the first instant of the
following local day converted to UTC. That is a stated rule, so implementing it invents nothing — but no
S6 task covered it, and it is now `T003a` and `T007a`. The write path is audited like any other
privileged Course mutation, and BR-025's "changing the Course default afterwards affects only future
approvals" is already guaranteed by the Entitlement snapshot, so no backfill is implied.

**Alternatives considered**: deriving expiry from a duration configured elsewhere — rejected, because
BR-025 names an *instant*, and converting a duration to an instant at approval time would make two
approvals of the same Course produce different expiries, which the "changing the default affects only
future approvals" clause presumes cannot happen. Reopening S2 to add the column — rejected; S2 closed on
independent verdict and reopening it discards the frozen-range evidence that closed it. Hard-coding a
launch-wide expiry — rejected as invented policy.
