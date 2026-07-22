# Contract: Order / Checkout Integration (PREREQUISITE dependency)

> **This is the seam between the coupon domain and the orders/checkout/payments subsystem,
> which does not exist yet.** The coupon feature is built and unit-tested against this
> contract; the paid-checkout and refund paths are integration-tested only once the orders
> subsystem implements it. The coupon plan does NOT build the orders subsystem.

## What the orders subsystem must provide

- An `orders` table carrying the coupon columns from [data-model.md](../data-model.md)
  (`coupon_id`, `coupon_code`, `subtotal_amount`, `discount_amount`, `total_amount`,
  free-grant terminal status).
- A grant transaction fired on (a) gateway payment-success webhook and (b) free-grant, that is
  idempotent and keyed by the gateway idempotency/transaction id (BR-033), and that grants the
  enrollment (BR-020/021) and 150-day term (BR-025).
- An enrollment/entitlement service the free-grant path can call directly (no gateway).

## What the coupon domain exposes to the orders subsystem

The coupon `service.go` exposes (Go signatures illustrative):

```go
// PreviewAndPrice validates a code for (user, item) and computes amounts.
// No side effects. Used by both the preview endpoint and order creation.
func (s *Service) PreviewAndPrice(ctx, userID, code, itemType, itemID) (Priced, error)
// Priced = { CouponID, Code, Subtotal, Discount, Total int64 (fils), Free bool }

// CommitRedemption records a redemption inside the caller's grant transaction.
// MUST be called within the same DB tx that grants the enrollment, passing the tx handle.
// Locks the coupon row (FOR UPDATE), re-checks per-user, inserts the redemption row,
// increments redemption_count. Idempotent w.r.t. (coupon,user) via the unique constraint.
func (s *Service) CommitRedemption(ctx, tx, in CommitInput) error
// CommitInput = { CouponID, UserID, OrderID uuid, AmountDiscounted int64 }
```

## Interaction rules

1. **Order creation** calls `PreviewAndPrice`, then snapshots `Discount`/`Total`/`CouponID`/
   `Code` onto the order. If `Free`, route to the free-grant path; else open the gateway
   session for `Total`. (BR-128)
2. **On grant** (webhook success OR free-grant), the orders subsystem calls `CommitRedemption`
   **inside** the same idempotent grant transaction. A duplicate/replayed webhook re-enters the
   same idempotency-keyed tx, so `CommitRedemption` never double-counts. (BR-129/033)
3. **Soft cap**: `CommitRedemption` does not reject if the global cap filled after order
   creation — the order was already priced; it records the redemption (count may exceed cap).
   Per-user uniqueness still rejects a true duplicate (surfaces as the BR-024 double-purchase
   block upstream). (BR-129)
4. **Refund** (orders/admin subsystem) returns `total_amount`; it does **not** call any coupon
   API to decrement — the redemption is retained. (BR-131)

## Test doubles until orders lands

The coupon integration tests stand up the 3 coupon tables plus a **minimal fake `orders`
table** (id + the coupon columns + status) to exercise `CommitRedemption` and the free-grant
path. This fake is a test fixture only — it is replaced by the real orders table when that
subsystem is built. This keeps the coupon domain shippable and tested without pulling the
orders subsystem into scope.
