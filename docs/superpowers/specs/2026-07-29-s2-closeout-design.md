# S2 Closeout Design

## Scope

Complete only `specs/003-course-authoring` tasks T054–T064. The work remains inside S2: it adds
taxonomy administration UI and final evidence, but no Order, payment, checkout, refund, payout,
Entitlement evaluation, S4, or S6 behavior.

## Queue A — Taxonomy UI

Extend the existing Admin catalogue page with bilingual taxonomy administration and exact-revision
override controls. Reuse the Instructor builder's existing localized term vocabulary and its explicit
candidate-revision identity. The Admin control must require the revision identifier already required
by the API; it never infers a current or latest revision. Validate with the existing frontend test,
typecheck, lint, and production build rails before marking T054.

## Queue B — Voluntary password-change evidence

Add a real-PostgreSQL integration test for the voluntary password-change path. It creates two
session families, changes the password through the voluntary flow, and observes that the other family
is revoked while the rotated current session remains usable. The test asserts the durable session and
audit evidence. T056 records and runs the precise mutation that removes the family revocation so the
test demonstrably fails; T057 records the passing evidence in the launch status without weakening or
deferring the carryover.

## Queue C — Whole-S2 convergence

Derive privileged-route coverage from the live production route table rather than maintaining a
sample list. The test verifies the capability, mutation security boundary, dependency wiring, and
same-transaction audit evidence for each privileged S2 route. A mutation proof removes an audit write
and must break enumeration. Verify bilingual RTL/LTR screens at the stated viewport classes, correct
only S2-affected documentation, run the complete backend/frontend/guard suite including a clean
frontend build, and run convergence until no additional S2 work remains.

## Delivery discipline

Preserve the existing mixed working tree. Stage and commit only files attributable to S2 closeout,
leaving reconciliation and user-owned files untouched. T064's frozen range begins at `3d9604e` and
ends at the selectively committed S2 head; pushing and hosted-CI verification occur only after the
full local gate passes. Independent review is deliberately excluded: it begins after convergence on
that exact range.

## Acceptance boundary

Tasks are checked individually only after their stated proof passes. A missing source rule, unsafe
dependency, or hosted-CI inability is reported as a blocker; no fallback behavior or replacement
scope is invented.
