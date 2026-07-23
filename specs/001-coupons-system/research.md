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
- **Alternatives considered**: gateway-side promo codes (rejected because Gradex owns the Order
  amount and the Coupon rules must remain gateway-independent).

## R-03 — Reserve exact capacity at Order acceptance

- **Decision**: Paid Order acceptance locks the Coupon and creates a deadline-matched `RESERVED`
  Redemption. Timely verified capture consumes it in the Enrollment/Entitlement transaction;
  cancellation/expiry releases it unused. A zero-value Order consumes immediately. Capacity counts
  reservations plus historical consumed uses.
- **Rationale**: Prevents accepting discounted payments that cannot be honored after another Order
  exhausts the cap. Stable Order/provider keys keep each transition idempotent. D-028/BR-128/129.
- **Alternatives considered**: soft capacity until capture (superseded—can accept money against
  unavailable quota); commit-at-checkout-never-release (abandoned Orders burn capacity).

## R-04 — Student consuming-redemption enforcement

- **Decision**: MVP has no configurable per-user limit. It permits one `RESERVED`/`CONSUMED`
  Redemption per Coupon/Student through a partial unique constraint. Unused reservation or
  cumulative full Refund releases Student eligibility; partial Refund does not.
- **Rationale**: This preserves exact eligibility and complete history without a hard
  `unique(coupon_id, student_id)` constraint that would make the approved full-refund release
  impossible. BR-128/129/131.
- **Alternatives considered**: deleting the Redemption or decrementing the global historical count
  (rejected because both erase what happened); configurable limits above one (out of MVP).

## R-05 — Concurrency on reservation and consumption

- **Decision**: At Order acceptance, lock the Coupon, verify
  `reserved_count + consumed_count < max_redemptions`, insert `RESERVED`, and increment reserved
  count. At paid success, lock again and atomically transfer reserved capacity to historical
  consumed capacity. Release decrements only reserved capacity; full Refund never decrements
  historical consumed count.
- **Rationale**: Serializes capacity before payment while preserving exact history. PostgreSQL row
  locking is sufficient at launch scale.
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
  transaction, idempotent by stable Order identifier under BR-129. This is an explicit, audited
  exception to BR-020's
  "access only after payment webhook".
- **Rationale**: Free-seeding (influencers/testers) is a real launch need; reuses the same
  enrollment/idempotency machinery. BR-126.
- **Alternatives considered**: disallow free codes (rejected in brainstorming, Q3=A).

## R-08 — Rejection reason typing

- **Decision**: Preview/apply return a typed reason: `expired`, `inactive`, `not_applicable`,
  `cap_reached`, `already_used`, `unknown_code`.
- **Rationale**: Frontend shows the correct message; avoids leaking which codes exist beyond
  what's necessary. FR-015.

## R-09 — Refund release behavior

- **Decision**: Partial Refund leaves `CONSUMED`. Cumulative full Refund moves to
  `RELEASED_AFTER_FULL_REFUND` once. Keep history and `consumed_count`; global quota is not restored.
- **Rationale**: Implements BR-131 while making duplicate/out-of-order refund confirmation safe.
- **Alternatives considered**: release on any partial Refund (rejected); release the global cap slot
  or delete history (rejected).

## Resolved unknowns

All prior Technical Context clarification questions are resolved above. No open product
clarifications remain for the Coupon domain itself. The external design dependency—the shape of the Order/
checkout subsystem — is captured as a contract dependency in
[contracts/order-integration.md](contracts/order-integration.md), not a clarification on this
feature.
