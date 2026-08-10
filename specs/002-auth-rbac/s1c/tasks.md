# Tasks: S1C — post-independent-review remediation (production staff onboarding)

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-08-11
**Authority**: [D-084](../../../docs/DECISIONS.md#d-084--the-independent-review-of-the-integrated-launch-range-returned-reject-and-bounded-remediation-of-its-seven-findings-is-authorized)
**Governing spec section**: [spec.md §19](spec.md#19-amendment--2026-08-11-production-staff-onboarding-is-a-launch-requirement)

## Why this file exists

S1C shipped without a `tasks.md`; its work was ordered by [spec.md §18](spec.md#18-antigravity-implementation-order)
and that record stands untouched. This file holds **only** the remediation of Critical finding C4 from
the **first valid independent review** of the integrated launch range `18fb7e0..48e1f3f`, which
returned `VERDICT: REJECT`: production staff onboarding is blocked because the staff foundation
composes only under `EnvDevelopment`, so no Instructor can be invited, onboarded, suspended or
reinstated in production.

**Task IDs start at T101** so they can never be confused with the parent
[`specs/002-auth-rbac/tasks.md`](../tasks.md) list (T001–T038, complete) or with S1C's §18 ordering.
Nothing in either record is reopened, reinterpreted or unchecked here, and none of these tasks existed
during any earlier review.

## Preconditions

**T101–T105 are blocked until the §19 spec amendment is committed.** Authority precedes code: the
production composition requirement is a specification statement, not something an implementation may
decide for itself. The remediation is **not** deleting the `EnvDevelopment` check — it is composing
production staff identity when, and only when, §19.2's preconditions hold, and failing closed
otherwise.

No commerce, no new session mechanism, no self-service staff registration, no role change of an
existing Account, and no MFA enter this scope. §4's exclusions still hold.

## Tasks

- [ ] T101 Add typed production-composition preconditions for the staff foundation covering every item
      in §19.2 — real session foundation, `CapAdminOperations` / `CapSecurityOperations` policy
      gating, production origin/CSRF enforcement, production-safe compromised-password screening,
      production-safe rate limiting, audit sink, durable Staff Invitation storage, transactional email
      outbox, and a configured production email provider — evaluated at startup and reported
      individually. Development-only admission policy and the development scanner seam must be
      structurally unreachable from this path.
- [ ] T102 Replace the `EnvDevelopment` hard gate on staff lifecycle composition in
      `backend/cmd/api/main.go` with the T101 precondition evaluation: mount the staff routes when all
      preconditions hold, and **fail closed** — refusing to start with a message naming the unmet
      precondition — when any does not. No degraded variant, and no fallback to the development
      composition. Staff composition stays independent of Student registration.
- [ ] T103 Prove the production path with `APP_ENV=production` and a valid production-safe
      configuration: the staff invitation and lifecycle routes are mounted; an Admin with the required
      capability and a fresh recent-authentication window creates an invitation; the invitee completes
      it and logs in normally afterwards; suspension and reinstatement behave under the existing
      policy, including immediate session-family death. The test must not require live email delivery
      — the durable outbox intent is the boundary it asserts.
- [ ] T104 Prove the denial matrix under production composition: Instructor, Student and
      unauthenticated callers are refused on every staff route exactly as §8 requires, by capability
      rather than by a handler-local role string, and a stale-recent-auth Admin is refused at the
      backend policy boundary.
- [ ] T105 Prove fail-closed startup: for each §19.2 precondition, an otherwise-valid production
      configuration with that one precondition unmet refuses to compose the staff routes and names the
      failure. Include the invitation-secret confidentiality assertion — no bearer in logs, argv,
      telemetry, DOM, storage or the address bar.

## Task count

5 tasks, all open. None is complete, and none may be marked complete in the authority pass that
created this file.

## Out of scope

Live Resend sender-domain delivery remains external and is tracked by `LG-018` and
[spec.md §16](spec.md#16-manual-operational-path)'s manual path; it is not a prerequisite for proving
router composition.
