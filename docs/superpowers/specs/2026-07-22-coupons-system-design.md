# Coupons System — Design Spec

**Date:** 2026-07-22
**Status:** Approved feature design; capacity mechanics reconciled 2026-07-26 by D-028
**Scope:** MVP feature addition
**Related:** [PRD.md](../../PRD.md) §5 Payments, [BUSINESS_RULES.md](../../BUSINESS_RULES.md) BR-019/020/022/024/033/040–047/124–133, [DECISIONS.md](../../DECISIONS.md) D-012

---

## 1. Summary

Admin-managed discount codes applied at checkout. A coupon reduces the order amount
**before** the Tap payment session is created; a coupon that reduces the order to 0 KWD
grants enrollment directly without touching the gateway. Coupons never modify a course's
listed price (no BR-019 conflict) — the discount is per-order only.

## 2. Decisions locked in brainstorming

| # | Decision | Rationale |
|---|----------|-----------|
| Owner | **Admin-only** creation | Matches Admin-only price control (BR-019); no Instructor side door. |
| Type | **Percentage and fixed**, admin picks per coupon | Covers "20% off" and "5 KWD off" campaigns; cheap. |
| Free access | **Allowed** — 100% or fixed ≥ price → 0 KWD → direct enrollment grant, no gateway | Seeds influencers/beta testers at launch; reuses enrollment/idempotency machinery. |
| Scope | **Per-Coupon**: platform-wide by default, optionally restricted to specific Course(s)/Section(s) | Covers catalog purchase scopes without a separate “Chapter” entity. |
| Guardrails | Expiry window · optional global cap · exactly one consuming Redemption per Student · active toggle · **one Coupon per Order** | Per-user limits greater than one are not configurable in MVP. |
| Capacity | **Reserve paid capacity at Order acceptance; consume on timely capture; release unused on Order cancellation/expiry** | Prevents accepting discounted payments the configured cap cannot honor; exact globally and per Student. |

## 3. Dependency note

The backend currently has video-slice Course/Section/Lesson stubs, Video/Progress, and fake
per-Lesson entitlements. It has no real Accounts/auth, catalog pricing, Orders/payments,
Enrollment/Entitlement, Refund, or audit subsystem. A Coupon attaches to an Order, reserves at paid
acceptance, and consumes through the Entitlement-grant transaction. This is a feature-level contract, not an
independently shippable schema; platform system design must sequence all prerequisites without
temporary duplicate models.

## 4. Money representation

All money is integer **fils** (1 KWD = 1000 fils). No floating point on monetary values
anywhere. Percentage discounts round to the nearest fil. Every computed discount is clamped
to `0 ≤ discount ≤ subtotal` (never negative, never exceeds price).

## 5. Data model

### `coupons`

| column | type | notes |
|--------|------|-------|
| `id` | uuid | PK |
| `code` | text | stored UPPERCASE + trimmed; unique index; matched case-insensitively |
| `discount_type` | enum | `percentage` \| `fixed` |
| `discount_value` | int | percentage 1–100, or fixed amount in fils |
| `valid_from` | timestamptz null | null = open-ended start |
| `valid_until` | timestamptz null | null = open-ended end |
| `max_redemptions` | int null | global cap; null = unlimited |
| `reserved_count` | int | default 0 — live paid-Order reservations |
| `consumed_count` | int | default 0 — historical consumed count; never decremented |
| `is_active` | bool | default true — instant kill switch |
| `created_by` | uuid | admin user id |
| `created_at` / `updated_at` | timestamptz | audit |

### `coupon_course_targets` / `coupon_section_targets`

Relational `(coupon_id, course_id)` and `(coupon_id, section_id)` unique target rows. **No rows in
either table means platform-wide.**

### `coupon_redemptions`

`id`, `coupon_id`, `student_id`, `order_id`, `amount_discounted` (fils), `state`,
`reservation_expires_at`, `reserved_at`, `consumed_at`, `released_at`, `release_reason`. States are
`RESERVED`, `CONSUMED`, `RELEASED_UNUSED`, and `RELEASED_AFTER_FULL_REFUND`. Preserve every row.
Enforce at most one `RESERVED`/`CONSUMED` Redemption per `(coupon_id, student_id)`.

### Order-side contract (fields the future orders table must carry)

`coupon_id` (nullable) · `coupon_code` (denormalized snapshot) · `subtotal_amount` ·
`discount_amount` · `total_amount` (all fils). Order status set must include a **free-grant
terminal state** (comp order, no gateway; e.g. payment method = `comp`).

## 6. Checkout flow

1. **Preview** (read-only, no side effects) — Student enters code for a Course/Section.
   Server validates: exists · `is_active` · within `[valid_from, valid_until]` · scope
   matches item · global cap not reached · no consuming Student Redemption. Computes `discount_amount`
   + `total_amount`. Returns the preview, or a typed rejection reason.
2. **Create order** — hard-validate again, **snapshot** `discount_amount` / `total_amount`
   onto the order (this locks the price the gateway will charge). Two branches:
   - **`total_amount == 0` (free):** single transaction → lock Coupon capacity → re-check rules →
     create Order/item → insert `CONSUMED` Redemption and increment `consumed_count` → create/reuse
     Enrollment and Entitlement → mark Order `FREE_GRANTED`. **No gateway call.**
   - **`total_amount > 0`:** in the Order-acceptance transaction lock Coupon capacity, insert a
     deadline-matched `RESERVED` Redemption, and increment `reserved_count`; then open Tap. Verified
     timely capture atomically moves `RESERVED → CONSUMED`, transfers the count from reserved to
     historical consumed, and grants access. Cancellation/expiry moves it to `RELEASED_UNUSED` and
     decrements reserved capacity.
3. **Capacity** — accept only while `reserved_count + consumed_count < max_redemptions` under the
   Coupon row lock. Full Refund releases Student eligibility but does not decrement historical
   `consumed_count` or restore global quota.

## 7. API

### Admin (admin auth; all mutations audited)

- `POST /admin/coupons` — create
- `GET /admin/coupons` — list + reservation/consumption stats (`reserved + consumed / max`)
- `GET /admin/coupons/:id` — detail + redemption log
- `PATCH /admin/coupons/:id` — edit. **Once any reservation/use exists, freeze `discount_type` /
  `discount_value` / `code`** (integrity — cannot rewrite what people already redeemed);
  still editable: `is_active`, `valid_until`, `max_redemptions`, targets.
- `DELETE /admin/coupons/:id` — only if zero redemptions; otherwise deactivate (mirrors
  BR-018 "never delete what has history").

### Student

- `POST /checkout/coupons/preview` — `{code, item_type, item_id}` →
  `{valid, discount_amount, total_amount, free, reason?}`. Rejection `reason` is typed:
  `expired` · `inactive` · `not_applicable` · `cap_reached` · `already_used` · `unknown_code`.
- Coupon code rides into the existing create-order call — no separate commit endpoint.

## 8. Edge cases and governing business rules

- **Money math:** fils integers; percentage rounds to nearest fil; discount clamped to
  `[0, subtotal]`; fixed ≥ price → total 0 → free path.
- **One coupon per order** — no stacking.
- **Free-grant order is a real order** — same Enrollment record and Entitlement checks
  (BR-023/025), with the same disclosed Course-configured expiry snapshot as a paid Order;
  distinguished by payment method = comp.
- **BR-024 still applies** — a coupon cannot grant an item the user already actively holds.
- **BR-019 clean** — a Coupon never touches a Course/Section's listed price; discount is per-Order
  only. No pending-revision, no instructor involvement.
- **Refund (BR-040–047/131):** Refunds cannot exceed captured total. Historical Redemption/Refund
  rows and `consumed_count` remain. Partial Refund keeps `CONSUMED`; cumulative confirmed full
  Refund moves to `RELEASED_AFTER_FULL_REFUND`, permitting later Student use only if the Coupon
  remains valid and has global capacity. Historical quota is not restored.
- **Audit:** coupon create / edit / deactivate + every redemption is logged (same discipline
  as refunds BR-042 and admin preview BR-081).

## 9. Testing

- **Unit:** discount math — % rounding, fixed clamp-to-zero, negative guard, > subtotal
  guard; each validation predicate (window, cap, consuming eligibility, scope, active).
- **Integration**: paid Order acceptance reserves exact capacity; expiry/cancellation releases it;
  timely capture consumes it and grants once; free path consumes/grants in one transaction;
  duplicate webhook does not double-count; concurrent cap/Student races are rejected; partial
  Refund remains consuming; cumulative full Refund releases Student eligibility without restoring
  historical quota; frozen-field edit is rejected after first reservation/use.

## 10. Out of scope (MVP)

Instructor-created Coupons; Coupon stacking; configurable per-user limits greater than one;
minimum-purchase thresholds; releasing the global historical cap slot/count after Refund;
bundle-scoped Coupons.
