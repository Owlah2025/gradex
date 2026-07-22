# Phase 1 Data Model: Coupons System

All money is BIGINT **fils**. Timestamps are `timestamptz` (UTC). Migration `0002_coupons`.

## Enums

- `coupon_discount_type`: `percentage` | `fixed`

## Table: `coupons`

| column | type | constraints | notes |
|--------|------|-------------|-------|
| `id` | uuid | PK, default gen | |
| `code` | text | NOT NULL, `UNIQUE` (on normalized value) | stored UPPERCASE + trimmed |
| `discount_type` | `coupon_discount_type` | NOT NULL | |
| `discount_value` | int | NOT NULL, CHECK (`> 0`); when `percentage`, CHECK (`<= 100`) | fils when `fixed` |
| `valid_from` | timestamptz | NULL | null = open start |
| `valid_until` | timestamptz | NULL, CHECK (`valid_until IS NULL OR valid_until >= valid_from`) | null = open end |
| `max_redemptions` | int | NULL, CHECK (`> 0`) | null = unlimited (global cap) |
| `per_user_limit` | int | NOT NULL, default 1, CHECK (`>= 1`) | |
| `redemption_count` | int | NOT NULL, default 0, CHECK (`>= 0`) | committed count |
| `is_active` | bool | NOT NULL, default true | instant kill switch |
| `created_by` | uuid | NOT NULL, FK → users(id) | admin |
| `created_at` | timestamptz | NOT NULL, default now() | |
| `updated_at` | timestamptz | NOT NULL, default now() | |

Indexes: unique on `code`; index on `is_active` (list filtering).

## Table: `coupon_targets`

Absence of any row for a coupon = **platform-wide**. Presence restricts to the listed items.

| column | type | constraints |
|--------|------|-------------|
| `coupon_id` | uuid | NOT NULL, FK → coupons(id) ON DELETE CASCADE |
| `item_type` | text | NOT NULL, CHECK in (`course`,`chapter`) |
| `item_id` | uuid | NOT NULL |
| | | `UNIQUE (coupon_id, item_type, item_id)` |

Index on `coupon_id`.

## Table: `coupon_redemptions`

One row per committed use.

| column | type | constraints |
|--------|------|-------------|
| `id` | uuid | PK |
| `coupon_id` | uuid | NOT NULL, FK → coupons(id) |
| `user_id` | uuid | NOT NULL, FK → users(id) |
| `order_id` | uuid | NOT NULL, FK → orders(id) *(external table, see contract)* |
| `amount_discounted` | bigint | NOT NULL, CHECK (`>= 0`) | fils |
| `redeemed_at` | timestamptz | NOT NULL, default now() |
| | | **`UNIQUE (coupon_id, user_id)`** — hard per-user guard |

Indexes: unique `(coupon_id, user_id)`; index on `coupon_id` (stats); index on `order_id`.

## External contract: order-side columns

Owned by the orders/checkout subsystem (added by its migration, NOT `0002`). Listed here as
the interface this feature reads/writes. See [contracts/order-integration.md](contracts/order-integration.md).

| column (on `orders`) | type | notes |
|----------------------|------|-------|
| `coupon_id` | uuid NULL | applied coupon |
| `coupon_code` | text NULL | denormalized snapshot at apply time |
| `subtotal_amount` | bigint | original price, fils |
| `discount_amount` | bigint | fils, ≥ 0, ≤ subtotal |
| `total_amount` | bigint | subtotal − discount, fils; `0` → free-grant path |
| `status` | enum | must include a free-grant terminal state (comp order) |

## Relationships

- `coupons` 1—* `coupon_targets` (scope)
- `coupons` 1—* `coupon_redemptions`
- `coupon_redemptions` *—1 `orders` (external) and *—1 `users`
- A coupon references only its own targets/redemptions; no coupling to course pricing rows
  (BR-017 stays clean — discount is per-order).

## Validation & state rules (enforced in service + DB)

1. `discount_value` bounds per type (DB CHECK + service). (BR-125)
2. Computed discount clamped `[0, subtotal]`; `total = 0` ⇒ free path. (BR-125/126)
3. Applicability: platform-wide (no targets) OR item ∈ targets. (FR-006)
4. Window: `now ∈ [valid_from, valid_until]` (nulls open-ended). (FR-007)
5. Global cap: `redemption_count < max_redemptions` at order creation (soft thereafter). (BR-129)
6. Per-user: no existing redemption for `(coupon,user)` beyond `per_user_limit`. (BR-129)
7. Active: `is_active = true`. (FR-007)
8. Immutability after first redemption: `code`/`discount_type`/`discount_value` frozen. (BR-130)
9. Delete only when `redemption_count = 0`; else deactivate. (BR-130)
10. One coupon per order. (BR-127)

## Audit

`audit_log` entries (existing audit facility) for: coupon create, edit, deactivate, delete,
and each redemption (coupon id, user id, order id, amount, timestamp). (BR-133)
