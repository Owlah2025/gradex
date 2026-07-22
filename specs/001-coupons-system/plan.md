# Implementation Plan: Coupons System

**Branch**: `001-coupons-system` | **Date**: 2026-07-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/001-coupons-system/spec.md`

## Summary

Add an admin-managed coupon subsystem to the Go backend: a new `coupon` domain
(`backend/internal/coupon/`) with types, repository, and service; admin + student HTTP
handlers wired into the existing Gin router; and one new migration (`0002`). Discounts are
computed in integer fils in the service layer **before** a payment session is created;
a 0-KWD result routes to a direct enrollment grant. Redemption commit is deferred to the
order's payment-success / free-grant transaction, keyed by the gateway idempotency id.

**Critical sequencing:** the orders/checkout/payments subsystem does not exist yet. This plan
builds the coupon domain and its validation/discount logic against the **order-side interface
contract** ([contracts/order-integration.md](contracts/order-integration.md)) and is
**gated** on that subsystem. Coupon redemption-commit and the paid-checkout path cannot be
integration-tested end-to-end until the orders subsystem lands; the coupon domain, its unit
tests, admin CRUD, and the free-grant path (which needs only enrollment, not the gateway) can
proceed independently against the contract.

## Technical Context

**Language/Version**: Go (module `github.com/Owlah2025/gradex/backend`), matching existing backend

**Primary Dependencies**: Gin (HTTP), database/sql + PostgreSQL driver, Redis (existing; used
by payments/webhook idempotency, not by coupon core)

**Storage**: PostgreSQL — 3 new tables (`coupons`, `coupon_targets`, `coupon_redemptions`) +
coupon-related columns on the future `orders` table (defined as contract, added by the orders
migration, not this one). Money as integer **fils** (BIGINT).

**Testing**: Go `testing` + existing integration-test pattern
(`internal/*/**_integration_test.go`, Postgres-backed)

**Target Platform**: Linux server (single-region, per PRD §6)

**Project Type**: Web service (Go/Gin backend + Next.js frontend)

**Performance Goals**: coupon preview + apply are write-path-adjacent; stay within PRD p95
< 800ms for write/transactional endpoints. Preview is a read (< 300ms target).

**Constraints**: no floats on money; discount clamped `[0, subtotal]`; per-user exactness via
DB unique constraint; global cap intentionally soft (design approach 1); all coupon mutations
audit-logged.

**Scale/Scope**: launch scale 100–500 students, 8–12 courses; coupon volume tiny. Concurrency
correctness matters only at the row level (coupon row lock on commit), not throughput.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

The project constitution (`.specify/memory/constitution.md`) is an unfilled template — no
ratified principles, so there are **no binding constitutional gates**. In its place this plan
honors the repository's actual conventions:

- **Domain layout** mirrors `internal/video` (`types.go`, `repo.go`, `service.go`, handlers in
  `internal/httpapi`). ✅
- **Migrations** follow the numbered `NNNN_name.up/down.sql` convention (this feature = `0002`).
  ✅
- **Testing** follows the existing Postgres-backed `*_integration_test.go` pattern + unit tests
  for pure logic. ✅
- **Money** is integer fils end-to-end (no float), per BR-125. ✅
- **No silent scope creep**: this plan does not build the orders subsystem; it depends on it via
  a written contract. ✅

`docs/CODING_STANDARDS.md` is itself a placeholder pending the pre-build architecture pass; when
it is filled, re-check lint/format/error-handling conventions against this plan.

**Result: PASS** (no violations; Complexity Tracking empty).

## Project Structure

### Documentation (this feature)

```text
specs/001-coupons-system/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
└── contracts/           # Phase 1 output
    ├── admin-coupons.md
    ├── student-coupons.md
    └── order-integration.md   # the prerequisite dependency contract
```

### Source Code (repository root)

```text
backend/
├── internal/
│   ├── coupon/                     # NEW domain
│   │   ├── types.go                # Coupon, CouponTarget, CouponRedemption, enums, DTOs
│   │   ├── discount.go             # pure discount math (fils, %/fixed, clamp) — unit-tested
│   │   ├── validate.go             # pure validation predicates (window/scope/cap/per-user/active)
│   │   ├── service.go              # orchestration: preview, apply-at-order-creation, commit-on-grant
│   │   ├── repo.go                 # SQL: CRUD, row-locked commit, redemption insert, stats
│   │   ├── audit.go                # audit-log writes for create/edit/deactivate/redeem
│   │   ├── discount_test.go        # unit
│   │   ├── validate_test.go        # unit
│   │   └── coupon_integration_test.go  # Postgres-backed: free-grant, commit, uniqueness, soft-cap
│   ├── httpapi/
│   │   ├── coupon_handlers.go      # NEW: admin + student endpoints
│   │   └── router.go               # MODIFIED: wire admin + checkout coupon groups
│   └── db/migrations/
│       ├── 0002_coupons.up.sql     # NEW: 3 tables + indexes + enums
│       └── 0002_coupons.down.sql
└── ...

frontend/                            # follow-on (not this plan's core; contract-ready)
└── src/ ...                          # admin coupon mgmt screens + checkout coupon field
```

**Structure Decision**: New self-contained `internal/coupon` domain following the established
`internal/video` layering (pure logic split into `discount.go`/`validate.go` so the risky money
math and predicates are unit-testable without a DB). HTTP stays in `internal/httpapi`. One
migration adds coupon tables; the order-side columns are owned by the orders migration per the
dependency contract. Frontend work is contract-ready but sequenced after the backend + orders
subsystem.

## Complexity Tracking

> No constitution violations. Table intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|--------------------------------------|
| — | — | — |

## Phase Gate Notes

- **Blocked-until-orders:** paid-path redemption commit, order-creation snapshot, and refund
  interaction require the orders/checkout subsystem. Track as an explicit dependency; do not
  fold orders into this feature.
- **Independently shippable now:** coupon tables + admin CRUD + preview endpoint + discount/
  validation logic (unit-tested) + free-grant path (needs only enrollment).
