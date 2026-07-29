# Feature Spec: Coupons System

> **STATUS: DEFERRED — post-MVP. Not implementation scope.**
>
> Coupons were deferred out of MVP on 2026-07-28 by
> [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation):
> a coupon discounts a checkout, and MVP has no checkout. Free and promotional access is granted
> through the same audited Course Access Invitation path as any other access — there is no second
> grant mechanism.
>
> This document is **retained unchanged as the design of record** for whenever in-platform payments
> are taken up. Nothing in it is current implementation scope.

**Feature branch**: `001-coupons-system`
**Date**: 2026-07-22; reconciled 2026-07-23
**Status**: Ready for planning
**Canonical design**: [docs/superpowers/specs/2026-07-22-coupons-system-design.md](../../docs/superpowers/specs/2026-07-22-coupons-system-design.md)
**Governed by**: BUSINESS_RULES BR-124–BR-133, DECISIONS D-012

> This is the spec-kit feature spec. The narrative design and rationale live in the
> canonical design doc above; this file states the testable requirements and entities so
> `/speckit-plan` and `/speckit-tasks` can operate. Where the two overlap, the canonical
> design doc + business rules win.

## Overview

Admin-managed discount codes applied at checkout. A coupon reduces the order amount (integer
fils) before the payment gateway session is created; a code that reduces the order to 0 KWD
grants enrollment directly with no gateway call. Coupons never modify a course's listed price
— the discount is per-order only.

## User Scenarios

### Admin creates a coupon

Given an authenticated admin, when they submit a code, discount type/value, optional scope,
window, global cap and active state, then a coupon is created (code stored uppercase, unique) and
becomes redeemable per its window and `is_active`.

### Student redeems a paid coupon

Given a student at checkout for a Course/Section and a valid applicable code, when they apply
it, then the order total is reduced by the computed discount, the gateway charges the reduced
total, and on the success webhook the enrollment is granted and the redemption recorded.

### Student redeems a free (100%) coupon

Given a code that reduces the order to 0 KWD, when the student confirms, then enrollment is
granted directly with no gateway call, and the redemption is recorded — all in one
transaction.

### Coupon rejected

Given an expired / inactive / non-applicable / cap-reached / already-used / unknown code,
when previewed or applied, then the system returns the specific typed rejection reason and no
discount is applied.

## Functional Requirements

- **FR-001** Only admins can create/list/edit/deactivate coupons. (BR-124)
- **FR-002** A coupon has a discount type of `percentage` (1–100) or `fixed` (fils). (BR-125)
- **FR-003** The computed discount is integer fils, percentages rounded to nearest fil,
  clamped to `[0, subtotal]`. (BR-125)
- **FR-004** A coupon reducing the order to 0 grants Enrollment directly, no gateway; the
  free-grant Order is a real Order + Enrollment with the normal Entitlement checks and disclosed
  Course-configured expiry snapshot (BR-023/025/126).
- **FR-005** At most one coupon per order; no stacking. (BR-127)
- **FR-006** A coupon may be platform-wide (no targets) or restricted to specific
  Course(s)/Section(s). (BR-128)
- **FR-007** Coupon guardrails: `valid_from`/`valid_until` window, global `max_redemptions`
  cap, one consuming redemption per Student, and `is_active` toggle. Per-user limits greater
  than one are not configurable. (BR-128)
- **FR-008** Validity and exact capacity are enforced under locks at Order acceptance; amounts are
  snapshotted and a paid Order creates a deadline-matched `RESERVED` Redemption. (BR-128)
- **FR-009** Timely verified capture moves `RESERVED → CONSUMED` in the Entitlement transaction;
  cancellation/expiry releases unused capacity, and zero-value Orders consume immediately.
  Provider/Order idempotency prevents double transitions. Reserved plus historical consumed uses
  count against the global cap; one `RESERVED`/`CONSUMED` use per `(Coupon, Student)` is exact.
  (BR-129/033)
- **FR-010** After any redemption, `code`/`discount_type`/`discount_value` are immutable;
  only `is_active`/`valid_until`/`max_redemptions`/targets remain editable. Zero-redemption
  coupons may be deleted; redeemed coupons only deactivated. (BR-130)
- **FR-011** Refunds on a coupon order cannot exceed the amount charged. Historical redemption
  and refund records and the global historical count remain. A cumulative full refund releases
  that Student's eligibility to consume the coupon again; a partial refund does not. (BR-131)
- **FR-012** Codes stored uppercase + trimmed, matched case-insensitively, unique. (BR-132)
- **FR-013** Coupon create/edit/deactivate and every redemption are audit-logged. (BR-133)
- **FR-014** A student cannot redeem a coupon toward an item they already actively hold
  (BR-024).
- **FR-015** Preview endpoint validates and computes discount with no side effects; returns a
  typed rejection reason on failure.

## Key Entities

- **Coupon** — the code/rules plus exact `reserved_count` and historical `consumed_count`.
- **CouponTarget** — optional scope link to a Course/Section (absence = platform-wide).
- **CouponRedemption** — a historical `RESERVED`, `CONSUMED`, or released use with Order deadline,
  discount, transition timestamps, and reason evidence.
- **Order (external)** — owned by the orders/checkout subsystem; carries coupon_id,
  coupon_code snapshot, subtotal/discount/total amounts, and a free-grant terminal state.

## Dependencies

- **Orders/checkout/payments subsystem (PREREQUISITE, not built here).** Coupons reserve at Order
  acceptance and consume/release inside Order lifecycle transactions. This feature builds
  against the order-side **interface contract** in
  [contracts/order-integration.md](contracts/order-integration.md) and is gated on that
  subsystem existing. The coupon plan does NOT build a full orders subsystem.
- Enrollment + entitlement (BR-023/024/025), gateway webhook idempotency (BR-033), audit
  logging, notification center (purchase receipt) — all consumed, not built here.

## Out of Scope (MVP)

Instructor-created coupons; coupon stacking; configurable per-user limits greater than one;
minimum-purchase thresholds; releasing the global historical cap/count after refund; bundle-scoped
coupons.

## Success Criteria

- Every FR above is covered by an automated test (unit for math/validation; integration for
  reservation/expiry release, exact capacity races, free grant, paid consume, duplicate-webhook
  dedupe, Student uniqueness, frozen-field rejection, partial-Refund retention, and full-Refund
  Student release without quota restoration).
- No monetary value is represented as a float anywhere in the feature.
