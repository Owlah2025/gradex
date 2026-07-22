# Phase 0 Research: Coupons System

Most decisions were resolved during brainstorming (see canonical design + D-012). This file
records them in decision/rationale/alternatives form and resolves the remaining
implementation-level unknowns.

## R-01 — Money representation

- **Decision**: All monetary values are integer **fils** (1 KWD = 1000 fils), stored as BIGINT.
  Percentage discounts round to nearest fil; discount clamped to `[0, subtotal]`.
- **Rationale**: KWD has 3 decimal places; floats introduce rounding drift on money. BR-125.
- **Alternatives considered**: decimal/numeric SQL type (heavier, still needs app-side care);
  float (rejected outright).

## R-02 — Where the discount is computed

- **Decision**: In the coupon **service layer**, at order creation, before any payment session
  is created. The service returns `{discount_amount, total_amount, free}`; the caller (orders/
  checkout) snapshots these onto the order.
- **Rationale**: Gradex already sets the order amount before delegating checkout to Tap
  (hosted page / tokenized SDK). Discount must land ahead of that. Keeps Tap integration
  unaware of coupons. BR-128.
- **Alternatives considered**: gateway-side promo codes (rejected — not portable across Tap's
  KNET/card/Deema sources, and Gradex owns the amount anyway).

## R-03 — Redemption commit timing (soft global cap, exact per-user)

- **Decision**: Validity enforced hard at order creation; redemption **count** committed only
  on payment-success / free-grant, inside the same enrollment-grant transaction, keyed by the
  gateway idempotency id (BR-033). Global cap is soft; per-user is exact.
- **Rationale**: No phantom redemptions from abandoned checkouts; reuses order idempotency so a
  duplicate webhook can't double-count; zero new infrastructure. Cap softness is a marketing
  nicety, not a money-loss risk, at launch scale. Design approach 1 / BR-129.
- **Alternatives considered**: reserve-at-checkout-with-expiry-job (rejected — real infra for a
  soft benefit); commit-at-checkout-never-release (rejected — abandoned checkouts burn the cap).

## R-04 — Per-user limit enforcement

- **Decision**: Unique constraint `unique(coupon_id, user_id)` on `coupon_redemptions` is the
  hard guard for the default limit of 1. For a configured limit > 1, enforce via a `SELECT
  count(*) ... FOR UPDATE`-style check under the coupon row lock in the commit transaction.
  BR-024 (no double-purchase of the same item) further constrains repeat use.
- **Rationale**: DB-level exactness for the common case; transactional counting for the rare
  configurable case. No race window.
- **Alternatives considered**: application-only counting (rejected — racy without the lock).

## R-05 — Concurrency on commit

- **Decision**: On commit, `SELECT ... FOR UPDATE` the coupon row, re-read `redemption_count`
  and per-user count, insert the redemption, increment the count — all in the grant transaction.
- **Rationale**: Serializes concurrent commits on the same coupon; keeps count consistent with
  redemption rows. Postgres row lock is sufficient at this scale.
- **Alternatives considered**: advisory locks / serializable isolation (unnecessary heavier
  machinery here).

## R-06 — Code normalization & matching

- **Decision**: Store `code` UPPERCASE + trimmed; unique index; match case-insensitively by
  upshifting input. BR-132.
- **Rationale**: Users type codes inconsistently; a single canonical form avoids collisions and
  confusing "code not found" from casing.
- **Alternatives considered**: case-sensitive codes (rejected — poor UX, collision-prone).

## R-07 — Free-grant path vs BR-020

- **Decision**: A 0-KWD order runs a direct enrollment grant with no gateway, but still creates
  a real order (payment method = comp), inserts the redemption, and grants enrollment in one
  transaction, idempotent per BR-024. This is an explicit, audited exception to BR-020's
  "access only after payment webhook".
- **Rationale**: Free-seeding (influencers/testers) is a real launch need; reuses the same
  enrollment/idempotency machinery. BR-126.
- **Alternatives considered**: disallow free codes (rejected in brainstorming, Q3=A).

## R-08 — Rejection reason typing

- **Decision**: Preview/apply return a typed reason: `expired`, `inactive`, `not_applicable`,
  `cap_reached`, `already_used`, `unknown_code`.
- **Rationale**: Frontend shows the correct message; avoids leaking which codes exist beyond
  what's necessary. FR-015.

## Resolved unknowns

All `NEEDS CLARIFICATION` from Technical Context are resolved above. No open clarifications
remain for the coupon domain itself. The one external open item — the shape of the orders/
checkout subsystem — is captured as a contract dependency in
[contracts/order-integration.md](contracts/order-integration.md), not a clarification on this
feature.
