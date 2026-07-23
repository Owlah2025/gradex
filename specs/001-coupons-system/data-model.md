# Phase 1 Data Model: Coupons System

> Status: Design input; migration numbering and final schema must be revalidated during platform
> system design.

All money is BIGINT **fils**. Timestamps are `timestamptz` instants stored in UTC.

## Enums

- `coupon_discount_type`: `percentage` | `fixed`
- `coupon_target_type`: `course` | `section`

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
| `redemption_count` | bigint | NOT NULL, default 0, `>= 0` | historical committed count; never decremented |
| `is_active` | bool | NOT NULL, default true | immediate validation kill switch |
| `created_by` | uuid | NOT NULL, FK → Accounts | Admin |
| `created_at` / `updated_at` | timestamptz | NOT NULL | audit timestamps |

There is no configurable `per_user_limit`: MVP always permits one consuming redemption per
Coupon/Student at a time.

## Table: `coupon_targets`

No rows for a Coupon means platform-wide applicability. Rows restrict it to the listed items.

| column | type | constraints |
|---|---|---|
| `coupon_id` | uuid | NOT NULL, FK → coupons(id) ON DELETE CASCADE |
| `item_type` | `coupon_target_type` | NOT NULL (`course` or `section`) |
| `item_id` | uuid | NOT NULL; must resolve to the matching canonical entity |
| composite | | UNIQUE (`coupon_id`, `item_type`, `item_id`) |

System design must choose how polymorphic target integrity is enforced; it cannot accept a missing
or mismatched Course/Section.

## Table: `coupon_redemptions`

One row per committed historical use. Full refund releases consuming status without deleting or
rewriting the historical event.

| column | type | constraints | notes |
|---|---|---|---|
| `id` | uuid | PK | |
| `coupon_id` | uuid | NOT NULL, FK → coupons(id) | |
| `student_id` | uuid | NOT NULL, FK → Accounts | Student role |
| `order_id` | uuid | NOT NULL, FK → Orders, UNIQUE | idempotent order association |
| `amount_discounted` | bigint | NOT NULL, `>= 0` | fils snapshot |
| `redeemed_at` | timestamptz | NOT NULL | historical commit time |
| `released_at` | timestamptz | NULL | set only after cumulative confirmed full refund |
| `release_reason` | text/enum | NULL with `released_at` | `FULL_REFUND` in MVP |

Use a partial unique constraint (or an equivalent transactionally enforced invariant) so there is
at most one row for `(coupon_id, student_id)` where `released_at IS NULL`. This retains earlier
history while allowing a later redemption after full refund.

Indexes support Coupon history, Student eligibility, and Order lookup. `redemption_count` remains a
global historical committed count and is not decremented when `released_at` is set.

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
5. Commit Redemption only with payment success or free grant in the entitlement-grant transaction.
   Failed/abandoned attempts create no Redemption. *(BR-126, BR-129)*
6. Keep the global cap soft only for an already-priced Order as approved in approach 1. *(BR-129)*
7. A partial Refund leaves `released_at` null. Cumulative confirmed full Refund sets it once without
   deleting history or decrementing `redemption_count`. *(BR-131)*
8. Freeze code/type/value after first historical Redemption; preserve delete/deactivate behavior.
   *(BR-130)*
9. Apply no more than one Coupon per Order. *(BR-127)*

## Audit

Record Coupon create/edit/deactivate/delete, Redemption commit, and full-refund eligibility release
with actor/system source, target, Order, outcome, and timestamp. Never log a secret or unnecessary
Student PII. *(BR-133, BR-156)*
