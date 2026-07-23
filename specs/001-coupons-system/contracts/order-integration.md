# Contract: Order / Checkout Integration (Prerequisite Dependency)

> Status: Proposed feature seam; revalidate against the platform system design and final Order API.

> **This is the seam between the coupon domain and the orders/checkout/payments subsystem,
> which does not exist yet.** The coupon feature is built and unit-tested against this
> contract; the paid-checkout and refund paths are integration-tested only once the orders
> subsystem implements it. The coupon plan does NOT build the orders subsystem.

## What the orders subsystem must provide

- An `orders` table carrying the coupon columns from [data-model.md](../data-model.md)
  (`coupon_id`, `coupon_code`, `subtotal_amount`, `discount_amount`, `total_amount`,
  free-grant terminal status).
- A grant transaction fired on (a) gateway payment-success webhook and (b) free-grant, idempotent
  by stable Order identifier for both branches, that grants the Enrollment and Entitlement with
  the Order's disclosed Course-configured expiry snapshot (BR-020/021/025). The paid branch
  separately deduplicates callbacks by
  payment-attempt/gateway reference (BR-033); the free branch has no gateway key.
- An Enrollment/Entitlement service the free-grant path can call directly (no gateway).

## What the coupon domain exposes to the orders subsystem

The coupon `service.go` exposes (Go signatures illustrative):

```text
// PreviewAndPrice validates a code for (user, item) and computes amounts.
// No side effects. Used by both the preview endpoint and order creation.
func (s *Service) PreviewAndPrice(ctx, userID, code, itemType, itemID) (Priced, error)
// Priced = { CouponID, Code, Subtotal, Discount, Total int64 (fils), Free bool }

// CommitRedemption records a redemption inside the caller's grant transaction.
// MUST be called within the same DB tx that grants the enrollment, passing the tx handle.
// Locks the coupon row (FOR UPDATE), re-checks consuming eligibility, inserts the
// historical redemption row, and increments redemption_count. Idempotent per Order.
func (s *Service) CommitRedemption(ctx, tx, in CommitInput) error
// CommitInput = { CouponID, UserID, OrderID uuid, AmountDiscounted int64 }
```

## Interaction rules

1. **Order creation** calls `PreviewAndPrice`, then snapshots `Discount`/`Total`/`CouponID`/
   `Code` onto the order. If `Free`, route to the free-grant path; else open the gateway
   session for `Total`. (BR-128)
2. **On grant** (webhook success OR free-grant), the orders subsystem calls `CommitRedemption`
   **inside** the same Order-idempotent grant transaction. A duplicate/replayed paid callback is
   rejected or reconciled by its payment-attempt/gateway reference before re-entering the same
   Order grant, so `CommitRedemption` never double-counts. (BR-129/033)
3. **Soft cap**: `CommitRedemption` does not reject if the global cap filled after order
   creation — the order was already priced; it records the redemption (count may exceed cap).
   Student consuming-redemption uniqueness still rejects another purchase until a cumulative full
   refund releases eligibility. (BR-129/131)
4. **Refund** never returns more than the captured `total_amount`. Partial confirmed refunds retain
   the consuming Redemption. When cumulative confirmed refunds reach the captured amount, the same
   idempotent refund transaction calls the Coupon seam to set that Redemption's release state once.
   Historical rows and `redemption_count` remain unchanged. (BR-131)

## Test doubles until orders lands

The coupon integration tests stand up the 3 coupon tables plus a **minimal fake `orders`
table** (id + the coupon columns + status) to exercise `CommitRedemption`, full-refund release, and the free-grant
path. This fake is a test fixture only — it is replaced by the real orders table when that
subsystem is built. This keeps the coupon domain shippable and tested without pulling the
orders subsystem into scope.
