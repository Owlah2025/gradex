# Phase 1 Data Model: Coupons System

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

> Status: Design input; capacity mechanics reconciled 2026-07-26 by D-028.

All money is BIGINT **fils**. Timestamps are `timestamptz` instants stored in UTC.

## Enums

- `coupon_discount_type`: `percentage` | `fixed`

## Table: `coupons`

| column | type | constraints | notes |
|---|---|---|---|
| `id` | uuid | PK | |
| `code` | text | NOT NULL, unique normalized value | stored uppercase and trimmed |
| `discount_type` | `coupon_discount_type` | NOT NULL | |
| `discount_value` | bigint | NOT NULL, `> 0`; percentage also `<= 100` | fils when fixed |
| `valid_from` | timestamptz | NULL | null = open start |
| `valid_until` | timestamptz | NULL; not before `valid_from` | null = open end |
| `max_redemptions` | bigint | NULL, `> 0` | null = unlimited global historical cap |
| `reserved_count` | bigint | NOT NULL, default 0, `>= 0` | live paid-Order reservations |
| `consumed_count` | bigint | NOT NULL, default 0, `>= 0` | historical consumed count; never decremented |
| `is_active` | bool | NOT NULL, default true | immediate validation kill switch |
| `created_by` | uuid | NOT NULL, FK → Accounts | Admin |
| `created_at` / `updated_at` | timestamptz | NOT NULL | audit timestamps |

There is no configurable `per_user_limit`: MVP always permits one consuming redemption per
Coupon/Student at a time.

## Tables: `coupon_course_targets` and `coupon_section_targets`

No rows in either table means platform-wide. Each table uses real relational foreign keys and
`UNIQUE(coupon_id, course_id)` or `UNIQUE(coupon_id, section_id)`; no polymorphic target ID exists.

## Table: `coupon_redemptions`

One row per reservation/use. History remains through release.

| column | type | constraints | notes |
|---|---|---|---|
| `id` | uuid | PK | |
| `coupon_id` | uuid | NOT NULL, FK → coupons(id) | |
| `student_id` | uuid | NOT NULL, FK → Accounts | Student role |
| `order_id` | uuid | NOT NULL, FK → Orders, UNIQUE | idempotent order association |
| `amount_discounted` | bigint | NOT NULL, `>= 0` | fils snapshot |
| `state` | constrained text | `RESERVED`, `CONSUMED`, `RELEASED_UNUSED`, `RELEASED_AFTER_FULL_REFUND` | |
| `reservation_expires_at` | timestamptz | required only while reserved | equals Order payment deadline |
| `reserved_at` / `consumed_at` | timestamptz | state-consistent | |
| `released_at` / `release_reason` | timestamp/state reason | state-consistent | |

Use a partial unique constraint so there is at most one row per `(coupon_id, student_id)` in
`RESERVED`/`CONSUMED`. This retains history while allowing later use after release.

Indexes support Coupon history, reservation expiry, Student eligibility, and Order lookup.

## External Order Contract

Owned by the Order/checkout subsystem. See
[contracts/order-integration.md](contracts/order-integration.md).

| field on Order | type | meaning |
|---|---|---|
| `coupon_id` | uuid nullable | applied Coupon |
| `coupon_code` | text nullable | immutable normalized snapshot |
| `subtotal_amount` | bigint | Admin-controlled catalog price snapshot, fils |
| `discount_amount` | bigint | `0..subtotal_amount`, fils |
| `total_amount` | bigint | subtotal minus discount; zero uses `FREE_GRANTED` |
| `status` | state | includes the Domain Model's `FREE_GRANTED` outcome |

## Validation and State Rules

1. Validate discount type/value and compute/clamp integer-fils amounts. *(BR-125)*
2. No targets means platform-wide; otherwise the Course/Section target must match. *(BR-128)*
3. Validate active state, time window, global historical cap, Student consuming eligibility, and
   absence of an active Entitlement before order creation. *(BR-024, BR-128)*
4. Snapshot the Coupon and monetary values on the Order. *(BR-128)*
5. Paid Order acceptance reserves capacity through its payment deadline; cancellation/expiry
   releases unused capacity. *(BR-128)*
6. Timely verified capture consumes the reservation in the Entitlement transaction; a zero-value
   Order consumes directly. Capacity counts reserved plus historical consumed uses. *(BR-129)*
7. Partial Refund stays consumed. Cumulative full Refund releases Student eligibility without
   deleting history, decrementing `consumed_count`, or restoring global quota. *(BR-131)*
8. Freeze code/type/value after first historical Redemption; preserve delete/deactivate behavior.
   *(BR-130)*
9. Apply no more than one Coupon per Order. *(BR-127)*

## Audit

Record Coupon create/edit/deactivate/delete, reservation, consumption/release, and full-refund release
with actor/system source, target, Order, outcome, and timestamp. Never log a secret or unnecessary
Student PII. *(BR-133, BR-156)*
