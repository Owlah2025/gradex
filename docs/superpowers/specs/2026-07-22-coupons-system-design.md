# Coupons System — Design Spec

**Date:** 2026-07-22
**Status:** Approved feature design; reconciled 2026-07-23; revalidate technical mechanics during platform system design
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
| Redemption commit | **Commit on payment success / free-grant** (soft global cap, exact per-user) | No phantom redemptions from abandoned checkouts; zero new infra; cap softness is non-critical at launch scale (100–500 students). |

## 3. Dependency note

The backend currently has video-slice Course/Section/Lesson stubs, Video/Progress, and fake
per-Lesson entitlements. It has no real Accounts/auth, catalog pricing, Orders/payments,
Enrollment/Entitlement, Refund, or audit subsystem. A Coupon attaches to an Order and commits
through the same Entitlement-grant transaction. This is a feature-level contract, not an
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
| `redemption_count` | int | default 0 — committed count |
| `is_active` | bool | default true — instant kill switch |
| `created_by` | uuid | admin user id |
| `created_at` / `updated_at` | timestamptz | audit |

### `coupon_targets` (only rows when scope = targeted)

`coupon_id`, `item_type` (`course` \| `section`), `item_id`; `unique(coupon_id, item_type, item_id)`.
**No rows for a coupon = platform-wide.**

### `coupon_redemptions`

`id`, `coupon_id`, `student_id`, `order_id`, `amount_discounted` (fils), `redeemed_at`,
`released_at`, `release_reason`. Preserve every row historically. Enforce at most one unreleased
Redemption per `(coupon_id, student_id)` using a partial unique constraint or equivalent
transactional invariant. Cumulative confirmed full Refund releases it; partial Refund does not.

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
   - **`total_amount == 0` (free):** single transaction → lock coupon row → re-check cap +
     Student eligibility → insert `coupon_redemptions` row → `redemption_count++` → create enrollment
     → mark order free-granted. **No gateway call.** The stable Order identifier makes the entire
     grant transaction idempotent under BR-129.
   - **`total_amount > 0`:** open Tap session for `total_amount`. On the success webhook
     (BR-020 / BR-033), deduplicate by payment-attempt/gateway reference, then within the same
     Order-idempotent grant transaction → lock coupon → insert redemption →
     `redemption_count++` → grant enrollment.
3. **Authority split** — validity is enforced **hard at order creation**; the redemption
   **count commits only at payment-success / free-grant**. If the global cap fills from other
   orders between order creation and webhook, this already-priced order is still honored (the
   accepted soft cap). Student consuming eligibility stays exact via the unreleased-row invariant.

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
- **Refund (BR-040–047/131):** Refunds cannot exceed the actually charged total. Historical
  Redemption/Refund rows and global `redemption_count` remain. Partial Refund keeps the Redemption
  consuming; cumulative confirmed full Refund marks it released so that Student can use the Coupon
  again on a future eligible purchase. The global historical cap slot is not released.
- **Audit:** coupon create / edit / deactivate + every redemption is logged (same discipline
  as refunds BR-042 and admin preview BR-081).

## 9. Testing

- **Unit:** discount math — % rounding, fixed clamp-to-zero, negative guard, > subtotal
  guard; each validation predicate (window, cap, consuming eligibility, scope, active).
- **Integration** (follows existing `*_integration_test.go` patterns): free path grants
  enrollment + redemption in one transaction; paid path records redemption on webhook;
  **duplicate webhook does not double-count** (BR-033); consuming uniqueness enforced; soft-cap
  race honored; partial Refund retains eligibility use; cumulative full Refund releases once while
  retaining history/count; frozen-field edit rejected after first Redemption.

## 10. Out of scope (MVP)

Instructor-created Coupons; Coupon stacking; configurable per-user limits greater than one;
minimum-purchase thresholds; releasing the global historical cap slot/count after Refund;
bundle-scoped Coupons.
