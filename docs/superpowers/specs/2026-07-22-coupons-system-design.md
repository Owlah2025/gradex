# Coupons System — Design Spec

**Date:** 2026-07-22
**Status:** Approved (brainstorming) — ready for implementation plan
**Scope:** MVP feature addition
**Related:** [PRD.md](../../PRD.md) §5 Payments, [BUSINESS_RULES.md](../../BUSINESS_RULES.md) BR-017/020/022/024/033/040–045, [DECISIONS.md](../../DECISIONS.md) D-002

---

## 1. Summary

Admin-managed discount codes applied at checkout. A coupon reduces the order amount
**before** the Tap payment session is created; a coupon that reduces the order to 0 KWD
grants enrollment directly without touching the gateway. Coupons never modify a course's
listed price (no BR-017 conflict) — the discount is per-order only.

## 2. Decisions locked in brainstorming

| # | Decision | Rationale |
|---|----------|-----------|
| Owner | **Admin-only** creation | Matches who controls pricing/revenue today (BR-064); no BR-017 side door. Instructor coupons → V1. |
| Type | **Percentage and fixed**, admin picks per coupon | Covers "20% off" and "5 KWD off" campaigns; cheap. |
| Free access | **Allowed** — 100% or fixed ≥ price → 0 KWD → direct enrollment grant, no gateway | Seeds influencers/beta testers at launch; reuses enrollment/idempotency machinery. |
| Scope | **Per-coupon**: platform-wide by default, optionally restricted to specific course(s)/chapter(s) | One nullable target link; covers both patterns. |
| Guardrails | Expiry window · global cap · per-user limit (default 1) · active toggle · **one coupon per order** (no stacking) | Each is one field + one check; caps/toggle are real abuse controls given free-access codes exist. |
| Redemption commit | **Commit on payment success / free-grant** (soft global cap, exact per-user) | No phantom redemptions from abandoned checkouts; zero new infra; cap softness is non-critical at launch scale (100–500 students). |

## 3. Dependency note

The backend currently has only `courses / sections / lessons / videos / progress /
fake_entitlements` — **there is no orders/payments subsystem yet**. A coupon attaches to
an order. This spec therefore defines its own tables **plus the coupon-relevant hooks into
the order lifecycle**, stated as a contract the payments/checkout build must satisfy.
Coupons and payments are co-dependent and should be sequenced together.

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
| `per_user_limit` | int | default 1 |
| `redemption_count` | int | default 0 — committed count |
| `is_active` | bool | default true — instant kill switch |
| `created_by` | uuid | admin user id |
| `created_at` / `updated_at` | timestamptz | audit |

### `coupon_targets` (only rows when scope = targeted)
`coupon_id`, `item_type` (`course` \| `chapter`), `item_id`; `unique(coupon_id, item_type, item_id)`.
**No rows for a coupon = platform-wide.**

### `coupon_redemptions`
`id`, `coupon_id`, `user_id`, `order_id`, `amount_discounted` (fils), `redeemed_at`;
**`unique(coupon_id, user_id)`** — hard per-user guard (works for the default limit of 1;
for a configured limit > 1, enforcement falls back to a transactional count under the coupon
row lock).

### Order-side contract (fields the future orders table must carry)
`coupon_id` (nullable) · `coupon_code` (denormalized snapshot) · `subtotal_amount` ·
`discount_amount` · `total_amount` (all fils). Order status set must include a **free-grant
terminal state** (comp order, no gateway; e.g. payment method = `comp`).

## 6. Checkout flow

1. **Preview** (read-only, no side effects) — student enters code for a course/chapter.
   Server validates: exists · `is_active` · within `[valid_from, valid_until]` · scope
   matches item · global cap not reached · per-user not exceeded. Computes `discount_amount`
   + `total_amount`. Returns the preview, or a typed rejection reason.
2. **Create order** — hard-validate again, **snapshot** `discount_amount` / `total_amount`
   onto the order (this locks the price the gateway will charge). Two branches:
   - **`total_amount == 0` (free):** single transaction → lock coupon row → re-check cap +
     per-user → insert `coupon_redemptions` row → `redemption_count++` → create enrollment
     → mark order free-granted. **No gateway call.** Idempotent per BR-024.
   - **`total_amount > 0`:** open Tap session for `total_amount`. On the success webhook
     (BR-020 / BR-033), within the same idempotent grant-transaction → lock coupon → insert
     redemption → `redemption_count++` → grant enrollment.
3. **Authority split** — validity is enforced **hard at order creation**; the redemption
   **count commits only at payment-success / free-grant**. If the global cap fills from other
   orders between order creation and webhook, this already-priced order is still honored (the
   accepted soft cap). Per-user stays exact via the unique constraint + BR-024.

## 7. API

### Admin (admin auth; all mutations audited)
- `POST /admin/coupons` — create
- `GET /admin/coupons` — list + redemption stats (`count / max`)
- `GET /admin/coupons/:id` — detail + redemption log
- `PATCH /admin/coupons/:id` — edit. **Once `redemption_count > 0`, freeze `discount_type` /
  `discount_value` / `code`** (integrity — cannot rewrite what people already redeemed);
  still editable: `is_active`, `valid_until`, `max_redemptions`, targets.
- `DELETE /admin/coupons/:id` — only if zero redemptions; otherwise deactivate (mirrors
  BR-018 "never delete what has history").

### Student
- `POST /checkout/coupons/preview` — `{code, item_type, item_id}` →
  `{valid, discount_amount, total_amount, free, reason?}`. Rejection `reason` is typed:
  `expired` · `inactive` · `not_applicable` · `cap_reached` · `already_used` · `unknown_code`.
- Coupon code rides into the existing create-order call — no separate commit endpoint.

## 8. Edge cases → new business rules (to slot into BUSINESS_RULES.md)

- **Money math:** fils integers; percentage rounds to nearest fil; discount clamped to
  `[0, subtotal]`; fixed ≥ price → total 0 → free path.
- **One coupon per order** — no stacking.
- **Free-grant order is a real order** — same enrollment record, same entitlement checks
  (BR-023), same **150-day term** (BR-025) from grant time; distinguished by payment method
  = comp.
- **BR-024 still applies** — a coupon cannot grant an item the user already actively holds.
- **BR-017 clean** — a coupon never touches a course's listed price; discount is per-order
  only. No pending-revision, no instructor involvement.
- **Refund (BR-040–045):** refund returns the actually-charged `total_amount`, not subtotal.
  The redemption record **stays** and the count is **not** decremented — consistent with
  BR-045 (analytics reflect what happened).
  - *Open question (deferred to V1):* whether a refunded capped-promo slot should be freed
    back to the pool.
- **Audit:** coupon create / edit / deactivate + every redemption is logged (same discipline
  as refunds BR-042 and admin preview BR-081).

## 9. Testing

- **Unit:** discount math — % rounding, fixed clamp-to-zero, negative guard, > subtotal
  guard; each validation predicate (window, cap, per-user, scope, active).
- **Integration** (follows existing `*_integration_test.go` patterns): free path grants
  enrollment + redemption in one transaction; paid path records redemption on webhook;
  **duplicate webhook does not double-count** (BR-033); per-user unique enforced; soft-cap
  race honored (already-priced order still granted); refund keeps redemption; frozen-field
  edit rejected after first redemption.

## 10. Out of scope (MVP)

Instructor-created coupons; coupon stacking; minimum-purchase-amount thresholds;
refund-releases-slot behavior; bundle-scoped coupons (bundles themselves are V1).
