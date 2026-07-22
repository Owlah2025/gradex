# Contract: Admin Coupon API

All endpoints require admin auth (`requireAuth` + admin role check, mirroring
`requireInstructor` in the existing router). All mutations write an audit-log entry (BR-133).
Money fields are integer fils. Base group: `/api/v1/admin/coupons`.

## POST /api/v1/admin/coupons — create

Request:
```json
{
  "code": "WELCOME20",
  "discount_type": "percentage",     // "percentage" | "fixed"
  "discount_value": 20,               // 1..100 for percentage; fils for fixed
  "valid_from": "2026-08-01T00:00:00Z",   // nullable
  "valid_until": "2026-08-31T23:59:59Z",  // nullable
  "max_redemptions": 100,             // nullable = unlimited
  "per_user_limit": 1,                // default 1
  "targets": [                        // omit/empty = platform-wide
    { "item_type": "course", "item_id": "<uuid>" }
  ]
}
```
Responses: `201` coupon object; `409` if code already exists; `400` on invalid
type/value/window.

## GET /api/v1/admin/coupons — list

Query: `?active=true|false`, pagination. Returns coupons with redemption stats:
```json
[{ "id": "...", "code": "WELCOME20", "discount_type": "percentage", "discount_value": 20,
   "redemption_count": 12, "max_redemptions": 100, "is_active": true, "valid_until": "..." }]
```

## GET /api/v1/admin/coupons/:id — detail

Returns the coupon + its targets + redemption log (user id, order id, amount, timestamp).

## PATCH /api/v1/admin/coupons/:id — edit

Editable always: `is_active`, `valid_until`, `max_redemptions`, `targets`.
Editable only while `redemption_count = 0`: `code`, `discount_type`, `discount_value`.
Responses: `200` updated; `409 frozen_field` if a frozen field is changed after redemptions.

## DELETE /api/v1/admin/coupons/:id — delete/deactivate

If `redemption_count = 0` → hard delete (`204`). Else → `409 has_redemptions` (client should
PATCH `is_active=false` instead). (BR-130)

## Errors (shape)

```json
{ "error": { "code": "frozen_field|has_redemptions|duplicate_code|invalid_value", "message": "..." } }
```
