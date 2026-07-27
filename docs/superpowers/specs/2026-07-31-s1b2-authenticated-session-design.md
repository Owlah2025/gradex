# S1B2 Authenticated Session Design

**Date:** 2026-07-31 scheduled launch day  
**Decision approved:** 2026-07-26  
**Status:** Approved for implementation

## 1. Outcome and Boundary

S1B2 lets an Active Student, Instructor, or Admin sign in, use and deliberately renew one
server-authoritative independently revocable browser session, recover safely from replaced or
expired session state, and log out.

This slice includes:

- the S1B1 safe-failure telemetry and environment-validation carryovers;
- role-specific idle, absolute, and recent-authentication configuration;
- generic email/password login and production-comparable dummy Argon2id work;
- one opaque cookie credential, session-bound CSRF, deliberate renewal, stale/reuse classification,
  family revocation, and logout;
- responsive Arabic/English login and session-state UI.

Password recovery remains S1B3. Staff invitation, suspension administration, and the full
authorization matrix remain S1C. Transactional email-provider selection remains behind `LG-018`;
the compromised-password production adapter remains behind `LG-021`.

## 2. Authoritative Credential Decision

Gradex uses one opaque, server-managed session credential stored in an `HttpOnly`, `Secure`,
host-only cookie. References to separate access and refresh tokens in older requirements are
superseded. Session renewal is implemented through controlled session-credential and CSRF rotation,
not through a client-visible refresh-token flow.

The browser receives:

- an opaque `__Host-gradex_session` cookie inaccessible to JavaScript; and
- the matching CSRF token in a no-store JSON response, held only in memory.

The cookie has `Path=/`, `Secure`, `HttpOnly`, and `SameSite=Strict`, has no `Domain` attribute, and
never outlives the server-side absolute expiry. Authentication or renewal credentials never enter
local storage, session storage, IndexedDB, URLs, analytics, logs, or JavaScript-readable cookies.

This decision follows the later approved same-origin system design and the existing migration-4
session-generation schema. Dual access/refresh cookies and a Next.js token vault are explicitly out
of scope.

## 3. Server-Authoritative Model

The existing `sessions` row is the mutable family record. It owns Account, admitted epoch, state,
current generation, authentication/activity times, role-derived idle and absolute expiry, and
revocation evidence.

Each `session_credentials` row is one immutable credential generation. PostgreSQL stores only the
SHA-256 digest of the 32-byte random credential and its independently keyed pseudorandom CSRF-token
digest. The CSRF token is derived with a separate server HMAC key from immutable family/generation
facts, so `GET /api/v1/session` can restore browser-memory CSRF state after reload without storing
the token plaintext or rotating on an ordinary read.
Only the current generation of an Active, unexpired, epoch-current family authenticates.

Login creates a new family and generation 1. It does not promote or descend from the anonymous
pre-authentication cookie. Renewal locks the family, proves the presented current generation,
inserts generation `n+1`, marks generation `n` superseded with replacement ancestry, advances the
family generation, rotates CSRF, and commits security evidence before exposing the new cookie.

Role defaults are the approved system-design values:

| Role | Idle expiry | Absolute expiry |
|---|---:|---:|
| Student | 7 days | 30 days |
| Instructor | 1 hour | 24 hours |
| Admin | 30 minutes | 12 hours |

The general recent-authentication class is ten minutes; the highest-risk class is five minutes.
These are typed, fail-closed configuration values. Meaningful authenticated activity may advance
idle expiry but never absolute expiry. Polling, session status reads, failed requests, and static
traffic do not count.

## 4. HTTP and Browser Contract

S1B2 adds four first-party same-origin resources:

| Method and path | Purpose |
|---|---|
| `POST /api/v1/sessions` | Validate anonymous Origin/CSRF and credentials; create an authenticated family |
| `GET /api/v1/session` | Resolve current session state and return its memory-only CSRF token without rotation or idle extension |
| `POST /api/v1/session-renewals` | Validate authenticated Origin/CSRF; deliberately rotate credential and CSRF |
| `DELETE /api/v1/session` | Validate authenticated Origin/CSRF; revoke the family, then clear the cookie |

Login failure is always `401 AUTHENTICATION_FAILED` for unknown email, wrong password,
unverified/inactive Account, or otherwise ineligible hidden state. Unknown email performs one dummy
Argon2id verification with production-comparable parameters. Hidden outcomes match status, body,
meaningful headers, cookie behavior, response-size class, and bounded timing class.

Successful login and renewal return a no-store authenticated-session descriptor containing only
safe Account/session display state and the CSRF token. They set the opaque cookie only after the
transaction commits. Login invalidates the anonymous cookie. No response returns the session
credential.

`GET /api/v1/session` is a safe, no-store rehydration endpoint. It does not rotate credentials or
extend idle expiry. Every cookie-authenticated `POST`, `PUT`, `PATCH`, and `DELETE` requires exact
trusted Origin or controlled Referer fallback plus the current session-bound `X-CSRF-Token` before
domain mutation.

Logout is ordered: lock and revoke the server-side family with `LOGOUT`, commit its evidence, and
only then clear the browser cookie with the same host-only path/security attributes. An already
invalid cookie receives a generic unauthenticated outcome and is cleared without inventing a
successful revocation.

## 5. Renewal, Stale Use, and Confirmed Reuse

Ordinary requests never rotate the credential. Rotation occurs after:

- login;
- explicit session renewal;
- successful primary reauthentication;
- password or privilege/security-boundary changes;
- recovery and future authenticator changes.

Only login and explicit renewal are implemented in S1B2; the later events must call the same
rotation primitive when their owning slices land.

A superseded credential authenticates no request. Its first immediate, non-sensitive presentation
inside the configured seconds-scale classification window returns `401 SESSION_REPLACED`, records
stale-use evidence, and does no application work. This avoids treating one in-flight request from
the old browser generation as proven theft.

Probable reuse is confirmed when a superseded credential is:

- presented to the renewal endpoint;
- presented again after one stale presentation;
- presented after the classification window; or
- used for a state-changing or security-sensitive request.

Confirmed reuse revokes the entire family with `REUSE_DETECTED`, clears the cookie where possible,
records safe security evidence, and requires normal authentication. A credential from an already
revoked family never authenticates; its family remains revoked and the attempt is recorded without
creating replacement credentials.

Concurrent renewal has exactly one winner. The losing request presents a now-superseded generation
and follows the same stale/reuse rules; it never receives another current credential. Authoritative
mutations recheck Account status, epoch, family state, and generation immediately before commit.

## 6. Configuration, Readiness, and Telemetry

Configuration supplies:

- Student, Instructor, and Admin idle/absolute windows;
- general and highest-risk recent-authentication windows;
- stale-use classification window;
- explicit same-origin and cookie policy.

Invalid role windows, nonpositive security windows, or missing production secrets fail startup.
Deterministic compromised-password screening is accepted only in development; staging and
production require the approved adapter boundary.

Readiness is capability-aware. S1A-only composition may remain ready at its supported schema floor;
enabled Student admission requires migration 5; enabled authenticated sessions require the complete
session/admission schema they use.

Admission failures emit one allowlisted internal stage—policy resolution, password screening,
outbox admission, transaction, or persistence—without raw causes, email, password, credential,
bearer-derived value, or database detail. Public Problem Details remain unchanged. Strict JSON
binding stamps `Cache-Control: no-store` before it can return credential-adjacent 400/413/415/422
responses.

## 7. Frontend Flow

The Arabic-first `/login` screen provides email, password, generic failure, retry, registration,
and future recovery navigation. A successful login goes to a validated internal `returnTo` or the
role root.

The API helper keeps authenticated CSRF only in module memory. On a full reload it rehydrates from
`GET /api/v1/session`. It never reads the session cookie. A controlled renewal replaces the in-memory
CSRF value only after the API confirms rotation.

Session-expired, replaced, confirmed-reuse, and logout states use explicit bilingual guidance:

- expired or revoked: sign in again;
- replaced: rehydrate once, then sign in if the current family cannot be recovered;
- confirmed reuse: explain that the session ended for security and require sign-in;
- logout: clear in-memory CSRF and navigate to the public/login state only after the server response.

All states support RTL/LTR, phone through desktop layouts, keyboard operation, focus management, and
no credential persistence.

## 8. Verification and Stop Conditions

S1B2 closes only with:

1. unit tests for typed windows, cookie attributes, dummy password work, error classification, and
   stale/reuse rules;
2. real PostgreSQL tests for login atomicity, digest-only storage, concurrent renewal, one winner,
   immediate stale rejection, confirmed reuse revocation, logout, expiry, epoch, and mutation
   recheck;
3. HTTP tests for generic login outcomes, exact Origin/CSRF ordering, no-store responses, cookie
   issue/rotation/clearing, and no secret leakage;
4. frontend lint, typecheck, production build, responsive bilingual inspection, safe `returnTo`,
   and no browser persistence;
5. full local PostgreSQL/Redis/MinIO gates, hosted CI, and independent read-only review of one frozen
   exact commit range with no critical/high finding.
