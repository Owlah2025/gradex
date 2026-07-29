# Phase 0 Research: S6 — Course Access Invitation and Entitlement Grant

**Date**: 2026-07-29 | **Plan**: [plan.md](plan.md) | **Spec**: [spec.md](spec.md)

Every unknown in the plan's Technical Context is resolved here. Nothing is left as
NEEDS CLARIFICATION. Where a decision reuses an existing repository mechanism, the mechanism is named
and its precedent cited, so implementation extends proven code rather than re-deriving it.

---

## 1. The S4 seam

**Decision**: S6 assumes S4 has delivered `backend/internal/access` containing the Entitlement record
and its evaluator. S6 adds the producer and **must verify the shape before starting**.

**Expected precondition — checkable, not assumed:**

| S4 must have delivered | Why S6 needs it |
|---|---|
| An `entitlements` table with scope, `original_access_ends_at`, effective `access_ends_at`, `retirement_eligibility_at`, revocation state | S6 writes every one of these fields in the grant transaction |
| An evaluator that answers "may this Student reach this Lesson?" from the Entitlement alone | S6's E2E proof drives playback after approval and must not write its own evaluator |
| A transaction helper on the package repository | S6's grant transaction extends it rather than opening its own pool |

**`enrollments` is deliberately absent from this table.** Cross-artifact analysis on 2026-07-29 found
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
and grant it to the **Admin role only** in `policy_set.go`.

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
| Missing reason, or Course has no future expiry instant | 422 | `validation-failed` |

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

**Decision**: two route groups under the existing localized shell — `(student)/access/*` for ST03,
ST04, and ST10, and `(admin)/course-access/*` for AD06 and AD07 — reusing the S3 bilingual shell,
locale persistence, and RTL/LTR handling.

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
