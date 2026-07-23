# Implementation Plan: Coupons System

**Branch**: `001-coupons-system` | **Date**: 2026-07-22 | **Reconciled**: 2026-07-23 | **Spec**: [spec.md](spec.md)

> Status: Draft implementation input. Revalidate package boundaries, API conventions, migration
> numbering, and transaction ownership after platform system design.

**Input**: Feature specification from `/specs/001-coupons-system/spec.md`

## Summary

The proposed implementation adds an Admin-managed Coupon subsystem to the Go backend: a
provisional `coupon` domain with types, repository, and service; Admin + Student HTTP capabilities;
and a schema migration whose sequence number is assigned when tasks are ordered. Discounts are
computed in integer fils in the service layer **before** a payment session is created;
a 0-KWD result routes to a direct enrollment grant. Redemption commit is deferred to the
order's payment-success / free-grant transaction, keyed by the stable Order identifier. The paid
branch separately deduplicates callbacks by payment-attempt/gateway reference.

**Critical sequencing:** real Accounts/auth, catalog prices, Orders/checkout/payments,
Enrollment/Entitlement, Refunds, and audit do not exist yet. Only pure discount/validation logic can
be built independently. Admin CRUD needs real Admin authorization/audit/targets; paid and free
Redemption/grant paths both need the Order and Enrollment/Entitlement transaction boundary.
Platform system design must sequence these foundations without duplicate temporary models.

## Technical Context

**Language/Version**: Go (module `github.com/Owlah2025/gradex/backend`), matching existing backend

**Primary Dependencies**: Gin (HTTP) and pgx v5/PostgreSQL. Redis exists for the video queue;
whether commerce/Coupon processing uses it is a platform system-design decision.

**Storage**: Proposed PostgreSQL tables (`coupons`, `coupon_targets`, `coupon_redemptions`) +
coupon-related columns on the future `orders` table (defined as contract, added by the orders
migration, not this one). Money as integer **fils** (BIGINT).

**Testing**: Go `testing` + existing integration-test pattern
(`internal/*/**_integration_test.go`, Postgres-backed)

**Target Platform**: Web backend; deployment topology/region is selected during platform system
design.

**Project Type**: Web service (Go/Gin backend + Next.js frontend)

**Performance Goals**: coupon preview + apply are write-path-adjacent; stay within PRD p95
< 800ms for write/transactional endpoints. Preview is a read (< 300ms target).

**Constraints**: no floats on money; discount clamped `[0, subtotal]`; one active consuming
Redemption per Coupon/Student with full-refund release; global cap intentionally soft (design
approach 1); all Coupon mutations and Redemption releases audit-logged.

**Scale/Scope**: Initial business target is 100–500 paid Students and 8–12 Courses. Correctness
under retries/concurrency is mandatory; system design validates performance/capacity assumptions.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

The project constitution (`.specify/memory/constitution.md`) is ratified and provides binding
engineering gates. This draft honors its MVP, authorization, transactional, testing, localization,
accessibility, security, and documentation requirements:

- **Domain layout** mirrors `internal/video` (`types.go`, `repo.go`, `service.go`, handlers in
  `internal/httpapi`). ✅
- **Migrations** follow the numbered `NNNN_name.up/down.sql` convention; the sequence is assigned
  after prerequisites are planned. ✅
- **Testing** follows the existing Postgres-backed `*_integration_test.go` pattern + unit tests
  for pure logic. ✅
- **Money** is integer fils end-to-end (no float), per BR-125. ✅
- **No silent scope creep**: this plan does not invent temporary Account/Order/Enrollment/audit
  implementations; it depends on their platform contracts. ✅

Re-check the final implementation against `docs/CODING_STANDARDS.md` and the future platform system
design before task generation.

**Result: PASS as a feature-level proposal; the technical structure is not approved until platform
system design revalidates it.**

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
│   │   ├── validate.go             # pure validation (window/scope/cap/consuming-use/active)
│   │   ├── service.go              # orchestration: preview, apply-at-order-creation, commit-on-grant
│   │   ├── repo.go                 # SQL: CRUD, row-locked commit, redemption insert, stats
│   │   ├── audit.go                # audit-log writes for create/edit/deactivate/redeem
│   │   ├── discount_test.go        # unit
│   │   ├── validate_test.go        # unit
│   │   └── coupon_integration_test.go  # free-grant, commit/release, uniqueness, soft-cap
│   ├── httpapi/
│   │   ├── coupon_handlers.go      # NEW: admin + student endpoints
│   │   └── router.go               # MODIFIED: wire admin + checkout coupon groups
│   └── db/migrations/
│       ├── NNNN_coupons.up.sql     # sequence assigned during task planning
│       └── NNNN_coupons.down.sql
└── ...

frontend/                            # follow-on (not this plan's core; contract-ready)
└── src/ ...                          # admin coupon mgmt screens + checkout coupon field
```

**Provisional Structure**: A self-contained `internal/coupon` domain following the established
`internal/video` layering (pure logic split into `discount.go`/`validate.go` so the risky money
math and predicates are unit-testable without a DB). HTTP stays in `internal/httpapi`. One
migration adds Coupon tables; Order-side columns are owned by the Order migration per the
dependency contract. Frontend work is contract-ready but sequenced after the backend + orders
subsystem.

## Complexity Tracking

> No constitution violations. Table intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|--------------------------------------|
| — | — | — |

## Phase Gate Notes

- **Blocked on platform foundations:** Admin CRUD/preview/commit/refund release require Account
  authorization, Course/Section prices, Orders, Enrollment/Entitlement, Refunds, and audit; paid
  paths additionally require the gateway adapter.
- **Independent before those foundations:** integer-fils discount math and validation rules can be
  unit-tested against domain values. They are not a shippable Coupon feature by themselves.
