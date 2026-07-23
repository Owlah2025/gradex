# Contract: Admin Coupon API

> Status: Proposed feature contract; confirm path/error envelope during system design.

Every endpoint requires authenticated Admin authorization and every mutation writes an audit event.
Money fields are integer fils. Proposed base group: `/api/v1/admin/coupons`.

## Create Coupon

`POST /api/v1/admin/coupons`

```json
{
  "code": "WELCOME20",
  "discount_type": "percentage",
  "discount_value": 20,
  "valid_from": "2026-08-01T00:00:00Z",
  "valid_until": "2026-08-31T23:59:59Z",
  "max_redemptions": 100,
  "targets": [
    { "item_type": "course", "item_id": "00000000-0000-0000-0000-000000000000" }
  ]
}
```

`valid_from`, `valid_until`, and `max_redemptions` may be null/omitted according to the final API
serialization. Empty/omitted targets means platform-wide. `discount_value` is 1–100 for percentage
and positive integer fils for fixed discounts. There is no `per_user_limit` field in MVP.

Expected outcomes: created; duplicate normalized code; invalid type/value/window/target; denied role.

## List and Detail

- `GET /api/v1/admin/coupons` — paginated/filterable list with historical committed count and global
  cap.
- `GET /api/v1/admin/coupons/:id` — Coupon, Course/Section targets, Redemption history, releases, and
  relevant audit data.

Example list item:

```json
{
  "id": "00000000-0000-0000-0000-000000000000",
  "code": "WELCOME20",
  "discount_type": "percentage",
  "discount_value": 20,
  "redemption_count": 12,
  "max_redemptions": 100,
  "is_active": true,
  "valid_until": "2026-08-31T23:59:59Z"
}
```

## Edit and Delete

- `PATCH /api/v1/admin/coupons/:id` — after any historical Redemption, code/type/value are frozen;
  active state, validity end, global cap, and targets remain editable subject to validation.
- `DELETE /api/v1/admin/coupons/:id` — allowed only with zero historical Redemptions; otherwise
  deactivate.

The system-design error envelope must provide stable machine codes such as `duplicate_code`,
`invalid_value`, `invalid_target`, `frozen_field`, and `has_redemptions` without leaking internals.
