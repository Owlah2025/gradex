# Contract: Student Coupon API

> Status: Proposed feature contract; confirm path/error envelope during system design.

Requires an authenticated Active Student. Money fields are integer fils and the server resolves the
Admin-controlled catalog subtotal.

## Preview Coupon (No Side Effects)

Proposed endpoint: `POST /api/v1/checkout/coupons/preview`

```json
{
  "code": "WELCOME20",
  "item_type": "section",
  "item_id": "00000000-0000-0000-0000-000000000000"
}
```

Valid response example:

```json
{
  "valid": true,
  "code": "WELCOME20",
  "subtotal_amount": 40000,
  "discount_amount": 8000,
  "total_amount": 32000,
  "free": false
}
```

Invalid response example:

```json
{ "valid": false, "reason": "expired" }
```

Stable rejection reasons may include `expired`, `inactive`, `not_applicable`, `cap_reached`,
`already_used`, and `unknown_code`. Final HTTP status/error-envelope conventions belong to system
design.

Validation checks normalized code, active/window, Course/Section target, global cap, no active
consuming Redemption, and no active Entitlement for the item; then it computes/clamps integer-fils
amounts. Preview never creates a Redemption or changes the historical count.

## Apply at Checkout

There is no separate commit endpoint. The create-Order request carries the code, and the server
revalidates it against current authoritative price and eligibility before snapshotting amounts.
Paid Order acceptance reserves capacity through its payment deadline; verified timely capture
consumes it, while cancellation/expiry releases it unused. Zero-value Orders consume immediately.
See [order-integration.md](order-integration.md).
