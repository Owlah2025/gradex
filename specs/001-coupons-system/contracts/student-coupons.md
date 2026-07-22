# Contract: Student Coupon API

Requires student auth (`requireAuth`). Money fields are integer fils.

## POST /api/v1/checkout/coupons/preview — validate + compute (no side effects)

Request:
```json
{ "code": "WELCOME20", "item_type": "course", "item_id": "<uuid>" }
```

Response `200` (valid):
```json
{
  "valid": true,
  "code": "WELCOME20",
  "subtotal_amount": 40000,   // fils (course price, resolved server-side)
  "discount_amount": 8000,    // fils
  "total_amount": 32000,      // fils; 0 => free
  "free": false
}
```

Response `200` (invalid) — HTTP 200 with `valid:false` so the checkout UI can show a message
inline:
```json
{ "valid": false, "reason": "expired" }
```
`reason` ∈ `expired | inactive | not_applicable | cap_reached | already_used | unknown_code`.

### Validation performed (FR-015, BR-128)
1. code exists (normalized match) — else `unknown_code`
2. `is_active` — else `inactive`
3. within `[valid_from, valid_until]` — else `expired`
4. applicable to `item_id` (platform-wide or targeted) — else `not_applicable`
5. global cap not reached — else `cap_reached`
6. user has not already redeemed / already holds the item (BR-024) — else `already_used`
7. compute discount (fils, clamp `[0, subtotal]`); set `free = total==0`

**Preview is read-only** — it never inserts a redemption or mutates `redemption_count`.

## Apply at checkout

There is **no separate apply/commit endpoint**. The validated `code` is passed into the
existing create-order call (orders subsystem). Order creation re-runs the same hard validation
(step 1–7) and snapshots `discount_amount`/`total_amount` onto the order. Redemption is
committed later by the order grant transaction (see order-integration contract).
