# Feature Specification: S1C — Staff lifecycle, enforcement, and authorization matrix

**Branch**: `feature/002-authentication-rbac`
**Date**: 2026-07-27 (real calendar; the repository's schedule calendar labelled this Day 11 /
August 2 — see [the execution plan §1](../../../docs/launch/AUGUST_15_EXECUTION_PLAN.md#1-calendar-reconciliation))
**Parent spec**: [Authentication and RBAC](../spec.md), narrowed to FR-005, FR-010, FR-011, FR-012,
FR-014, FR-015, FR-016
**Slice authority**: [SLICES.md §5.4](../../../docs/launch/SLICES.md#54-staff-lifecycle-enforcement-and-authorization-matrix-s1c)
**Workflow**: [D-040](../../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews)
— Claude plans, Antigravity implements, Claude reviews
**Review tier**: **3 (Critical)** — authentication, account suspension, and authorization boundaries

## 1. Goal

Close S1 by delivering the staff and enforcement half of Identity: an Admin brings an Instructor or
Admin into existence through an invitation the invitee completes themselves, a suspended Account
loses protected access on its **next request** rather than at expiry, and **every** protected route
that exists refuses the wrong role and the wrong owner when called directly.

S1 does not close until S1C closes. No S2 work begins before it does.

**Musts 1 and 2 of this slice are already complete** at `c65cd53` — the two gate-fidelity carryovers
are fixed and the three inherited policy dispositions are substantiated. This spec covers the
remaining work: Musts 3 through 7.

## 2. Included user journeys

1. **Admin invites staff.** An Admin holding `ADMIN_OPERATIONS` with a fresh recent-authentication
   window invites an Instructor or Admin by email address and role. A durable notification intent is
   written; no credential and no session exist yet.
2. **Invitee completes.** The invitee opens the invitation bearer, chooses a display name and their
   own initial password, and the Account is created. They are **not** signed in; they log in
   normally afterwards.
3. **Admin suspends an Account.** An Admin holding `SECURITY_OPERATIONS` with a fresh
   recent-authentication window suspends an Account. Every session family dies immediately.
4. **Admin reinstates an Account.** A separately audited operation restores `ACTIVE`. Reinstatement
   does not restore revoked sessions; the user logs in again.

## 3. Included business rules

- **FR-005** — only existing Admins invite Instructor/additional Admin Accounts; the recipient sets
  their own password.
- **FR-004** — invitation secrets expire, are single-use, and are stored only as digests.
- **FR-010** — a suspended Account immediately fails every protected action on new *and* existing
  sessions.
- **FR-011** — the backend enforces the three roles plus ownership; deny by default.
- **FR-012** — Instructors are limited to their own Course-management scope.
- **FR-014** — invitation endpoints are rate-limited and non-enumerating.
- **FR-015** — security-sensitive events generate the fixed audit/notification events.
- **FR-016** — invitation acceptance collects a display name.
- Constitution principle II — deny by default, enforce in the backend; Admin actions touching
  account suspension are both authorized server-side and auditable.

## 4. Explicit exclusions

- No S2 capability. No Course, Section, Lesson, catalogue, or pricing surface.
- No live email delivery. `LG-018` is open, so invitations produce durable outbox **intent** and
  evidence only.
- No self-service staff registration. No role change of an existing Account.
- No new session mechanism. S1B2's session core is reused unchanged.
- No Admin user-listing or search screen beyond what invitation and suspension require.
- No MFA (`FF-005`).

## 5. Database changes

One migration, `0008_staff_lifecycle`, raising the schema to version 8.

- **Reuse `identity_action_secrets`** for invitations rather than creating a parallel table. Add the
  `STAFF_INVITATION` purpose to the existing closed purpose allowlist. Digest-only, expiring,
  supersedable, single-use under a row lock, one live per purpose — the existing boundary already
  provides all of it.
- **New table `staff_invitations`** holding the authoritative invited attributes the secret must not
  carry: invited email (normalized key plus preserved correspondence form), **invited role**,
  inviter account id, state (`PENDING` / `CONSUMED` / `SUPERSEDED` / `EXPIRED`), the action-secret
  reference, and timestamps. Unique partial index guaranteeing at most one `PENDING` invitation per
  normalized email.
- **Account suspension** needs no new column — `accounts.status` already carries `SUSPENDED` and
  `session_epoch` already exists. Add the `ACCOUNT_SUSPENDED` and `ACCOUNT_REINSTATED` values to the
  closed security-event allowlist if not already present.
- CI derives the expected schema version from `db.MaxSchemaVersion`; do not hardcode 8 anywhere.

## 6. State transitions

**Invitation**

```
(none) --create--> PENDING --complete--> CONSUMED
                     |  \--supersede--> SUPERSEDED
                     \-----expire-----> EXPIRED
```

`PENDING → CONSUMED` is the only transition that creates an Account, and it is atomic with credential
creation. Completion is refused if the invited email now belongs to an existing Account, or if the
invitation target was suspended before completion.

**Account status**

```
ACTIVE --suspend--> SUSPENDED --reinstate--> ACTIVE
```

Suspension in one transaction: set `SUSPENDED`, revoke every session family with reason
`ACCOUNT_SUSPENDED`, advance `session_epoch`, write Identity security evidence. Suspension is
idempotent — suspending an already-suspended Account succeeds without writing a second revocation
sweep or a second epoch advance.

## 7. API contracts

All under `/api/v1`, RFC 9457 Problem Details on failure, existing uniform authorization refusal for
every denial.

| Method | Path | Caller | Requires |
|---|---|---|---|
| `POST` | `/staff-invitations` | Admin | `ADMIN_OPERATIONS` + fresh recent-auth |
| `GET` | `/staff-invitations` | Admin | `ADMIN_OPERATIONS` |
| `DELETE` | `/staff-invitations/{id}` | Admin | `ADMIN_OPERATIONS` + fresh recent-auth |
| `GET` | `/staff-invitations/preview` | Anonymous, bearer in body/header | — (returns invited role and display state only; **never** the email) |
| `POST` | `/staff-invitation-completions` | Anonymous, bearer | — |
| `POST` | `/accounts/{id}/suspension` | Admin | `SECURITY_OPERATIONS` + fresh recent-auth |
| `DELETE` | `/accounts/{id}/suspension` | Admin | `SECURITY_OPERATIONS` + fresh recent-auth |

`POST /staff-invitation-completions` accepts the bearer, a display name, and a password. It accepts
**no role field.** It returns `201` with no session cookie.

`GET /staff-invitations/preview` carries the bearer in the `X-Gradex-Invitation-Bearer` header. The
method is `GET`, so a request body is not reliably carried, and a query parameter would place a
one-time secret into access logs, referrer headers, and browser history. The response sets
`Cache-Control: no-store`. *(Transport detail recorded 2026-07-28 while closing the review finding
that the mounted routes diverged from this section. The contract specified `GET`, so the routes moved
to match it rather than the method being rewritten to suit what had shipped.)*

Reinstatement requires a non-empty reason, exactly as suspension does. *(Tightened 2026-07-28: it was
optional, which allowed a privileged account-status change to be recorded with no explanation.)*

## 8. Authorization matrix

The matrix is **mechanically derived from the mounted router**, not hand-maintained. Each mounted
route carries one class:

| Class | Matrix asserts |
|---|---|
| Anonymous | reachable without a session; asserts **no** capability |
| Authenticated session-lifecycle | requires a usable session; no capability decision |
| Capability-protected | the named capability, per role |
| Ownership-protected | correct capability **and** resource ownership |
| Recent-auth-required | a fresh Admin window in addition to the capability |

The proof **must fail** in all six of these cases:

1. a protected route is mounted **without** a matrix row;
2. a matrix row references a route **no longer mounted**;
3. an authenticated caller has the **wrong capability**;
4. the caller has the correct capability but **does not own** the resource;
5. the Account is **suspended**;
6. an Admin's **recent-auth window has expired** on a sensitive operation.

Cases 1 and 2 are what make this a gate on future slices rather than a snapshot of today.

## 9. Frontend screens and states

Arabic and English, RTL and LTR, phone and desktop, keyboard reachable.

| Screen | States |
|---|---|
| Admin — invite staff | form, submitting, success, duplicate-pending, denied, stale-recent-auth |
| Admin — pending invitations | list, empty, revoke confirm, revoked |
| Admin — account suspension | confirm, suspended, reinstate confirm, reinstated, denied |
| Invitation acceptance | loading, valid (shows invited role), expired, already used, invalid |
| Initial password | form with policy hints, submitting, success → "now sign in", failure |

The invitation bearer is handled exactly as S1B3 handles recovery bearers: fragment-carried,
purpose-namespaced capture, monotonic, released only on a terminal outcome. **No bearer in the DOM,
in storage, or in the address bar after load** — including after hydration, which is the specific
defect S1B3 found and fixed.

## 10. Background jobs

None new. Invitation notification uses the existing outbox intent path. No delivery worker ships
while `LG-018` is open.

## 11. External integrations

None new. Compromised-password screening at invitation completion uses the existing provider-neutral
checker and **fails closed** when unconfigured (`LG-021`).

## 12. Failure and retry behaviour

- Two concurrent completions of one invitation produce **exactly one** winner; the loser receives the
  already-used outcome, not a 500.
- A failed completion leaves no partial Account, no credential, and a still-`PENDING` invitation.
- Re-inviting an email with a live `PENDING` invitation supersedes the old secret rather than
  creating a second live one.
- Suspension is idempotent and safe to retry.
- Rate limits on invitation creation and completion reuse the existing layered limiter.

## 13. Audit requirements

Co-committed Identity security evidence, in the same transaction as its subject, for: invitation
created, invitation superseded, invitation revoked, invitation completed (with the created account
id), account suspended (actor, subject, reason, families revoked), account reinstated. No plaintext
password, no bearer, and no secret digest reaches logs or telemetry.

## 14. Tests

**Nine invitation invariants**, each with its own assertion:

| # | Invariant |
|---|---|
| I1 | The **invitation row is authoritative for the invited role.** The client cannot submit or modify the role during completion |
| I2 | The secret is purpose-bound, digest-only, expiring, supersedable, single-use |
| I3 | The inviter must possess the capability to invite **that exact role** |
| I4 | An Admin must not invite a role above their permitted ceiling (under the Gradex three-role model `STUDENT` / `INSTRUCTOR` / `ADMIN`, `ADMIN` is the top ceiling so I4 collapses into I3) |
| I5 | No password credential and no session exists before successful completion |
| I6 | Completion atomically consumes the invitation and creates the credential |
| I7 | Two concurrent completions produce **exactly one** winner — under real PostgreSQL contention |
| I8 | Completion does **not** authenticate the new staff member |
| I9 | Suspending the invitation target before completion **prevents** completion |

**Three independent suspension proofs**, each mutation-checked against **its own** mechanism. One
test cannot prove three mechanisms when any one of them is sufficient for its assertion — the live
`ACTIVE` check in `sessionRecord.usable` already denies on its own.

| Proof | Asserts | Mutation that must break it |
|---|---|---|
| 4a | Suspend an authenticated Account, issue the next protected request, assert denial | Remove the **live `ACTIVE`-account check** in `sessionRecord.usable` |
| 4b | Every existing family is **persisted** as revoked with reason `ACCOUNT_SUSPENDED`, with evidence | Remove the **family-revocation update** |
| 4c | `session_epoch` advances **atomically** inside the suspension transaction; a family admitted concurrently against the old epoch cannot survive | Remove the **epoch advance** |

**A proof whose mutation check does not fail is not evidence.**

**Full-surface authorization matrix** per §8, plus the rerun of bootstrap test 3 across the complete
protected surface. **This run is recorded as the final full-surface proof** that S1A deliberately
staged to S1C.

## 15. Acceptance criteria

1. All nine invitation invariants asserted, I7 under real PostgreSQL contention.
2. The plaintext initial password never reaches storage, logs, argv, or telemetry.
3. A non-Admin caller is refused by **capability**, not by a handler-local role string.
4. A stale-recent-auth Admin is refused at the **backend policy boundary** on both invitation
   creation and suspension, verified by **direct API call**, returning the existing uniform refusal
   with the typed `DenyReason` confined to monitoring.
5. Proofs 4a, 4b, 4c each pass and each fails under its own mutation, against real PostgreSQL.
6. Suspension is idempotent; reinstatement is separately audited.
7. The authorization matrix fails in all six cases of §8.
8. Bootstrap test 3 denies the restricted bootstrap Admin across the whole surface.
9. Screens pass RTL/LTR, phone/desktop, keyboard, expired-secret and reused-secret paths; no bearer
   in DOM, storage, or address bar after load; production build passes **with `.next` removed
   first**.
10. Full local gates green with real PostgreSQL, Redis, MinIO; hosted CI green on the exact head.

## 16. Manual operational path

If `LG-018` remains open at launch, staff invitation delivery is manual: the Admin reads the
invitation bearer from the audited Admin screen and transmits it out of band. The bearer is still
digest-stored, expiring, and single-use — the manual step changes the delivery channel, not the
security boundary. This path is category **B** in
[the scope matrix](../../../docs/launch/AUGUST_15_EXECUTION_PLAN.md#2-scope-matrix).

## 17. Launch-gate dependencies

- `LG-021` — compromised-password screening. Invitation completion **fails closed** when unconfigured.
- `LG-018` — transactional email. Ships as intent plus evidence; §16 is the manual path.

Neither gate blocks building this slice. Both block declaring it production-ready.

## 18. Antigravity implementation order

1. Migration `0008_staff_lifecycle` + the `STAFF_INVITATION` purpose and security-event allowlist
   entries. Verify `up`/`down` against real PostgreSQL before writing any handler.
2. Suspension and reinstatement domain operations with their transaction, plus proofs 4a/4b/4c and
   their mutation checks. **This first**, because I9 depends on it.
3. Invitation domain: create, supersede, revoke, complete. Invariants I1–I9.
4. HTTP boundary: the seven routes in §7, capability gating, recent-auth enforcement, rate limits.
5. The mechanically derived authorization matrix and the six failure cases, plus the bootstrap
   test 3 full-surface rerun.
6. Bilingual screens.
7. Full local gates, clean frontend build, push, hosted CI on the exact head, evidence package.

---

## 19. Amendment — 2026-08-11: production staff onboarding is a launch requirement

**Added after S1C closed**, on the **first valid independent review** of the integrated launch range
`18fb7e0..48e1f3f`, which returned `VERDICT: REJECT` with Critical finding C4: production staff
onboarding is blocked, because the staff foundation composes only under `EnvDevelopment`. Sections 1–18
are the original slice and are not rewritten. Authority:
[D-084](../../../docs/DECISIONS.md#d-084--the-independent-review-of-the-integrated-launch-range-returned-reject-and-bounded-remediation-of-its-seven-findings-is-authorized);
the development-only composition it supersedes was recorded in
[D-081](../../../docs/DECISIONS.md#d-081--staff-lifecycle-composition-is-decoupled-from-student-registration-and-production-staff-onboarding-stays-unapproved).

### 19.1 Requirement

**Production Gradex MUST support Admin-controlled Instructor and staff onboarding at launch.** A
launch in which no Instructor can be brought into existence in production is not a launch: every
Course, every submission and every approval in the founder journey begins with an invited Instructor.

The requirement is that production **composes the staff foundation when it is safe to do so** — not
that the environment check is deleted. Removing the gate without preconditions would compose staff
identity on top of whatever happens to be configured, which is the failure mode the gate was
protecting against.

### 19.2 Production composition preconditions

The staff lifecycle routes mount in production **only** when all of the following hold. Each is an
existing capability of this architecture; none is new machinery.

1. **Real session foundation** — the S1B2 session core, unchanged and fully configured.
2. **Capability gating** — `CapAdminOperations` for invitation operations and `CapSecurityOperations`
   for suspension and reinstatement, enforced at the policy boundary, never by a handler-local role
   string, with the recent-authentication window of §7 and §8.
3. **Production origin and CSRF enforcement** — production values, not development admission policy.
4. **Production-safe password screening** — compromised-password screening configured and failing
   closed, per `LG-021` (§17).
5. **Production-safe rate limiting** — the invitation and completion limiters configured with real
   keys, non-enumerating as FR-014 requires.
6. **Audit evidence** — the §13 audit events committed for every invitation, completion, suspension
   and reinstatement.
7. **Durable Staff Invitation** — digest-only, expiring, single-use, supersedable secrets in
   `identity_action_secrets` under the `STAFF_INVITATION` purpose. No secret is stored or logged in
   plaintext, and the invitation bearer never appears in logs, argv, telemetry, DOM, storage or the
   address bar.
8. **Transactional email outbox** — the durable intent boundary present and writable.
9. **Production email provider** — a configured provider under the provider-neutral adapter of
   [D-077](../../../docs/DECISIONS.md#d-077--resend-delivers-launch-transactional-email-behind-a-provider-neutral-durable-boundary).
   §16's manual out-of-band delivery path remains the fallback and is unchanged.
10. **No fake authentication** — no development bootstrap identity, no test-only login, no fixture
    Account participates in production composition.
11. **No development-only seams in identity composition** — the development scanner mode of
    [D-079](../../../docs/DECISIONS.md#d-079--the-instructor-authoring-ui-is-wired-to-the-existing-authoring-and-media-apis-and-a-development-only-scanner-mode-makes-the-whole-path-testable)
    and the development admission policy must not be reachable from the production staff path.
12. **Existing suspension and reinstatement authorization** — §6 and §8 unchanged, including immediate
    session-family death on suspension.

**Fail closed.** If any precondition is unmet, production **refuses to compose** the staff routes and
says which precondition failed, in the existing startup-validation style. It does not mount a degraded
variant, and it does not fall back to the development composition.

**Student registration is not a prerequisite.** Staff onboarding composes independently of the Student
admission path, as decoupled in D-081.

### 19.3 Acceptance criteria for this amendment

1. With `APP_ENV=production` and a valid production-safe configuration, the staff invitation and
   lifecycle routes are mounted.
2. An Admin holding the required capability and a fresh recent-authentication window can create an
   invitation; the invitee can complete it and log in normally afterwards.
3. Instructor, Student and unauthenticated callers are refused on the staff routes exactly as §8's
   matrix requires.
4. Suspension and reinstatement work in production composition under the existing policy, including
   immediate session revocation.
5. With any precondition unmet, startup fails closed and the routes are not mounted.
6. Router composition is provable **without live email delivery**. Live provider sending remains an
   external production step tracked by `LG-018`; it is not a prerequisite for testing composition.
