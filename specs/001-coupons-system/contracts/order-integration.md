# Contract: Order / Checkout Integration (Prerequisite Dependency)

> Status: Reconciled with the 2026-07-26 domain/data/state design; final API remains July 27 work.

> **This is the seam between the coupon domain and the orders/checkout/payments subsystem,
> which does not exist yet.** The coupon feature is built and unit-tested against this
> contract; the paid-checkout and refund paths are integration-tested only once the orders
> subsystem implements it. The coupon plan does NOT build the orders subsystem.

## What the orders subsystem must provide

- An `orders` table carrying the coupon columns from [data-model.md](../data-model.md)
  (`coupon_id`, `coupon_code`, `subtotal_amount`, `discount_amount`, `total_amount`,
  free-grant terminal status).
- An Order-acceptance transaction that reserves Coupon capacity through the payment deadline.
- A verified paid-success transaction that consumes the reservation and grants Enrollment/
  Entitlement; cancellation/expiry releases unused reservation.
- A zero-value transaction that consumes/grants immediately with no gateway.
- An Enrollment/Entitlement service the free-grant path can call directly (no gateway).

## What the coupon domain exposes to the orders subsystem

The coupon `service.go` exposes (Go signatures illustrative):

```text
// PreviewAndPrice validates a code for (user, item) and computes amounts.
// No side effects. Used by both the preview endpoint and order creation.
func (s *Service) PreviewAndPrice(ctx, userID, code, itemType, itemID) (Priced, error)
// Priced = { CouponID, Code, Subtotal, Discount, Total int64 (fils), Free bool }

// Reserve records exact capacity in the Order-acceptance transaction.
func (s *Service) Reserve(ctx, tx, in ReserveInput) error
// Consume moves RESERVED to CONSUMED in the paid grant transaction.
func (s *Service) Consume(ctx, tx, orderID uuid) error
// ReleaseUnused moves RESERVED to RELEASED_UNUSED on Order cancellation/expiry.
func (s *Service) ReleaseUnused(ctx, tx, orderID uuid, reason Reason) error
```

## Interaction rules

1. **Paid Order acceptance** revalidates under locks, snapshots amounts/terms, calls `Reserve`
   with the Order deadline, and only after commit opens the provider session. (BR-128)
2. **Verified timely capture** calls `Consume` inside the source-unique Entitlement transaction.
   Provider/Order idempotency prevents double consumption/grant. (BR-129/033)
3. **Cancellation/expiry** calls `ReleaseUnused`; a zero-value Order inserts directly as
   `CONSUMED` in its no-gateway grant transaction.
4. **Refund** never returns more than the captured `total_amount`. Partial confirmed refunds retain
   `CONSUMED`. Cumulative full Refund moves to `RELEASED_AFTER_FULL_REFUND` once. Historical rows,
   consumed count, and global quota remain unchanged. (BR-131)

## Test doubles until orders lands

The coupon integration tests stand up Coupon/target/Redemption tables plus a **minimal fake `orders`
table** to exercise Reserve/Consume/Release, full-refund release, and the free-grant
path. This fake is a test fixture only — it is replaced by the real orders table when that
subsystem is built. This keeps the coupon domain shippable and tested without pulling the
orders subsystem into scope.
