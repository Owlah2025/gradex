# S1B Student Authentication Delivery Design

> Status: Written-spec review pending
> Schedule decision approved by the developer: 2026-07-26
> Scheduled implementation window: 2026-07-30 through 2026-08-01
> Governing feature: [Authentication and RBAC](../../../specs/002-auth-rbac/spec.md)

## 1. Purpose

S1B delivers the public Student identity journey without compressing registration, session
rotation, recovery, privacy, abuse controls, notification intent, and bilingual UI evidence into
one implementation day.

This document changes delivery boundaries only. Product behavior remains governed by the PRD,
Business Rules, the Authentication/RBAC feature specification, and the approved API/security and
domain/data designs.

## 2. Approved split

| Date | Slice | Closed outcome |
|---|---|---|
| July 30 | **S1B1 — Student admission** | A visitor can register only as a pending Student, receive a durable verification intent, and verify once without Account enumeration |
| July 31 | **S1B2 — Authenticated sessions** | An Active Account can sign in through the same-origin cookie boundary, rotate its session safely, and log out |
| August 1 | **S1B3 — Recovery and integration** | Password recovery is single-use and non-enumerating; the complete Student authentication journey and staged authorization evidence pass review |
| August 2 | **S1C — Staff lifecycle and enforcement** | Staff invitations, immediate suspension, full authorization matrix, and S1 integration review |
| August 3 | **S2 — Course authoring and review** | Course authoring remains blocked until S1 closes |

S3–S8 remain `TBD`. The fixed August 8–15 runway is now forecast-at-risk and must be reconciled
before it is represented as achievable. August 7 remains protected unless the developer explicitly
spends it.

## 3. Invariants across all three slices

- Public registration creates `STUDENT` only and issues no authenticated session.
- Normalized email is the uniqueness/lookup key; the supplied correspondence/display address is
  preserved separately.
- Public registration, verification, login, and recovery outcomes do not reveal Account existence,
  lifecycle, delivery, or limiter dimensions.
- Passwords are 15–128 Unicode characters, retain spaces, use Argon2id, and are checked against both
  the common-password floor and an approved compromised-password adapter.
- Verification and reset bearer values are random, purpose-bound, expiring, single-use, and stored
  only as digests.
- Browser JavaScript never receives session bearer credentials. Authentication uses the host-only,
  `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/` cookie contract.
- PostgreSQL remains authoritative for Accounts, action secrets, sessions, and durable outbox
  intent. Redis holds only disposable rate-limit state.
- Source mutation, immutable Identity security evidence, and required notification outbox intent
  commit atomically. Provider delivery is asynchronous and cannot roll back Identity state.
- Every public Identity endpoint applies structural and Origin checks before layered rate limiting;
  no limiter key contains plaintext email.
- S1B does not implement staff invitation, Account suspension commands, email change, MFA, social
  login, notification delivery workers, or notification-center UI.

## 4. S1B1 — Student admission

### 4.1 Security prerequisites

Before the public registration route is mounted:

1. Bind the bootstrap operation ID to a canonical request fingerprint. An identical retry may read
   the recorded result; the same ID with changed email, display name, deployment principal, or
   other semantic input fails closed.
2. Preserve the originally supplied email for correspondence while keeping normalized email as the
   unique comparison value.
3. Make compromised-password screening a required credential-creation dependency. The adapter
   receives only its documented privacy-preserving representation; plaintext never crosses the
   password boundary. Unavailable or unconfigured screening fails credential creation closed.

Provider/data-source selection is tracked as `LG-021`. Tests use a deterministic local checker;
production activation remains blocked until the source, license, privacy boundary, failure policy,
test vectors, and monitoring are approved.

### 4.2 Persistence

The S1B1 migration introduces only the durable structures needed by the admission transaction:

- purpose-bound Identity action-secret records with digest, Account, issue/expiry, consumption,
  supersession, and attempt evidence;
- append-only Identity registration/security evidence distinct from routine logs and privileged
  Audit;
- the minimum shared `outbox_events` contract required to co-commit a versioned verification
  notification intent.

S1B1 does not implement provider delivery attempts or the notification center; those remain owned by
S9. The outbox row is durable source intent, not a claim that email was accepted or delivered.

### 4.3 Registration transaction

A valid first registration performs one PostgreSQL transaction:

1. normalize and validate email and the BR-105 display name;
2. prove current policy-version acceptance is available;
3. apply the complete password policy and Argon2id hashing;
4. create one `PENDING_VERIFICATION`, immutable-role Student Account while preserving original
   email;
5. create its password credential;
6. create a hashed, expiring verification secret;
7. append Identity registration/security evidence;
8. create the stable verification notification outbox intent.

No session is issued. If any required write fails, all state rolls back. An existing normalized
email performs no Account, credential, secret, resend, or delivery mutation and returns the same
public acceptance response class.

### 4.4 Verification and resend

- Verification request/resend returns the same public result for eligible, ineligible, and unknown
  identifiers.
- An eligible resend atomically supersedes the prior unused secret, stores a new digest, and creates
  one linked delivery intent.
- Verification consumption locks the exact secret and Account. Only the correct purpose, unexpired,
  unused, unsuperseded secret may transition the pending Student to `ACTIVE`.
- Concurrent duplicate consumption produces one activation; later attempts share the safe invalid
  token result and create no further mutation.

Bearer secrets are submitted in the request body, not ordinary paths or query strings.

### 4.5 Abuse and privacy controls

Registration and verification use versioned rate-limit policies with identifier HMAC, trusted client
network, anonymous client/session, endpoint, and global dimensions. Development and tests receive
conservative deterministic settings; staging and production require explicit configuration.

Redis outage uses only the approved strict local fallback. If neither Redis nor the bounded fallback
can make a safe decision, admission returns a generic unavailable response rather than allowing the
request or fabricating `429`.

### 4.6 Frontend

S1B1 adds responsive Arabic/English registration, verify-email, and verification-result screens.
They:

- never infer Account existence from response, timing, delivery, or navigation;
- apply the same Unicode length and display-name rules as the backend for immediate feedback while
  treating backend validation as authoritative;
- store no password, action secret, or future session credential in browser persistence;
- expose retryable delivery guidance without claiming a message was sent.

## 5. S1B2 — Authenticated sessions

S1B2 supplies role-specific typed configuration for Student, Instructor, and Admin idle/absolute
expiries plus the ten-minute general and five-minute highest-risk recent-authentication classes.

It then implements:

- generic email/password login with production-comparable dummy Argon2id work for unknown email;
- verified/active status admission and session-family creation;
- the same-origin host-only cookie and session-bound CSRF credential;
- atomic generation rotation;
- stale-generation classification and confirmed family-reuse revocation;
- current-family logout and cookie clearing;
- sign-in and session-expired/replaced UI states.

S1B2 closes only when replay of a rotated credential cannot obtain a new session and logout prevents
the current family from authenticating again.

## 6. S1B3 — Recovery and integrated proof

S1B3 adds non-enumerating reset request, purpose-bound reset-secret consumption, password-policy
validation, atomic password replacement, Account revision/epoch advancement, revocation of every
authenticated family, Identity security evidence, and notification outbox intent. Recovery issues
no authenticated session; the user signs in normally afterward.

It also completes the forgot/reset-password screens and runs the complete Student journey:

`register → verify → login → rotate → reject reuse → logout → recover → login`

The staged bootstrap-Admin denial proof is expanded across every protected Identity route delivered
by S1B. Final full-surface authorization proof still belongs to S1C.

## 7. Error and failure contract

- Malformed structure and ordinary field validation use normal Problem Details.
- Hidden Account outcomes retain the canonical uniform response classes from API design §5.
- Invalid, expired, consumed, superseded, and wrong-purpose action secrets share `TOKEN_INVALID`.
- Transaction failure returns no credential, cookie, delivery claim, or partial Identity state.
- Notification provider failure after durable outbox admission does not roll back source state.
- Unsafe outbox admission, missing required policy versions, unavailable password screening, or no
  safe rate-limit decision fails the affected surface closed.

## 8. Verification and review

Each sub-slice must pass:

- focused unit and PostgreSQL integration tests for its success, duplicate, concurrency, rollback,
  enumeration, expiry, and abuse paths;
- the full backend build/vet/race/integration suite and frontend lint/typecheck/build;
- documentation and secret-exposure guards;
- hosted CI on the frozen head;
- independent Claude review of one exact range with no critical or high finding.

S1B3 additionally runs the complete Student authentication journey and records the S1B-wide review.

## 9. Rejected alternatives

- **Keep S1B on one day:** violates the delivery-capacity rule and makes security evidence the only
  available compression target.
- **Use two days:** leaves login, refresh-reuse defense, logout, recovery, integration, UI, and
  review in one overloaded day.
- **Defer screens or recovery outside S1:** removes approved MVP scope from the slice rather than
  planning it honestly.
