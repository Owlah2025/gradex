# Feature Specification: S1B2 Authenticated Sessions

**Feature branch:** `feature/002-authentication-rbac`  
**Scheduled slice:** Day 9 / S1B2  
**Status:** Approved for planning  
**Canonical design:** [S1B2 Authenticated Session Design](../../../docs/superpowers/specs/2026-07-31-s1b2-authenticated-session-design.md)

## Scope

S1B2 implements the authenticated browser-session portion of
[Authentication and Role-Based Access Control](../spec.md). D-034 is authoritative: Gradex uses one
opaque server-managed session credential in a `Secure`, `HttpOnly`, host-only,
`SameSite=Strict` cookie. Separate access/refresh tokens and a Next.js token vault are out of scope.

Password recovery remains S1B3. Staff invitation, suspension administration, and the complete
authorization matrix remain S1C.

## User Story 1 — Active Account signs in, renews safely, and signs out (Priority: P1)

As an Active Student, Instructor, or Admin, I want to sign in through the Gradex first-party browser
origin, renew my session safely, and sign out so that I can use my role-appropriate experience
without exposing authentication credentials to JavaScript.

**Independent test:** Against real PostgreSQL, sign in one Active Account, prove only a credential
digest is stored, resolve the session through its cookie, rotate the cookie credential and CSRF
token with one concurrency winner, reject the superseded credential, confirm reuse revokes the
family, and prove logout prevents every family credential from authenticating. Repeat the public
login-failure contract for unknown, wrong-password, unverified, and inactive states.

### Acceptance Scenarios

1. **Given** an Active Account submits correct credentials through an admitted anonymous browser,
   **when** login succeeds, **then** one independently revocable family and generation 1 commit,
   the anonymous cookie is invalidated, the opaque authenticated cookie is set, and only the
   session-bound CSRF token is returned to browser memory. *(BR-004, D-034)*
2. **Given** an unknown email, wrong password, unverified Account, or inactive Account, **when**
   login is attempted, **then** each outcome returns the same generic `401 AUTHENTICATION_FAILED`
   status/body/header/cookie/response-size/timing class and creates no session. *(BR-003)*
3. **Given** a current usable family and matching CSRF token, **when** controlled renewal succeeds,
   **then** the prior immutable generation is superseded and linked to exactly one new current
   credential/CSRF generation. *(BR-004, BR-005, D-034)*
4. **Given** two concurrent renewal attempts use the same generation, **when** they race, **then**
   exactly one receives the new generation and the other performs no protected work.
5. **Given** a superseded credential is presented once immediately by a non-sensitive in-flight
   request, **when** it falls inside the configured classification window, **then** it is rejected
   as `SESSION_REPLACED` and recorded without granting access.
6. **Given** a superseded credential is presented to renewal, repeatedly, after the classification
   window, or to a state-changing/security-sensitive request, **when** reuse is confirmed, **then**
   the whole family is revoked with `REUSE_DETECTED` and normal authentication is required.
   *(BR-005)*
7. **Given** a current authenticated family, **when** logout is admitted, **then** the server-side
   family is revoked and committed before the cookie is cleared; subsequent authentication or
   renewal fails. *(BR-006)*
8. **Given** a cookie-authenticated state-changing request, **when** Origin/Referer, CSRF, Account
   status, epoch, family state, expiry, or generation is invalid, **then** it fails before domain
   mutation.
9. **Given** a successful login, session reload, replacement, expiry/reuse, or logout in Arabic or
   English, **when** the user navigates the responsive UI, **then** credentials remain outside
   browser persistence and the flow returns only to a validated internal destination or role root.

## Requirements

- **S2-FR-001:** S1B1 carryovers MUST land before session routes: typed safe admission-failure stage
  telemetry, deterministic password-screen rejection outside development, strict-binding
  `no-store`, and capability-aware schema readiness.
- **S2-FR-002:** Configuration MUST provide validated Student, Instructor, and Admin idle/absolute
  windows plus general/highest-risk recent-authentication and stale-use classification windows.
- **S2-FR-003:** Login MUST use production-comparable dummy Argon2id verification for unknown email
  and MUST reveal no hidden Account state. *(BR-003)*
- **S2-FR-004:** Login MUST create a new family rather than promote or derive from anonymous state.
- **S2-FR-005:** The browser MUST receive only `__Host-gradex_session` with `Secure`, `HttpOnly`,
  host-only, `Path=/`, and `SameSite=Strict`; PostgreSQL MUST store only its digest. *(BR-004,
  D-034)*
- **S2-FR-006:** The CSRF token MUST be independently keyed and pseudorandom, stored only as a
  digest, reconstructable only by the server from immutable generation facts and a separate HMAC
  key, returned only in no-store JSON, held only in browser memory, and required with trusted
  Origin/Referer for every state-changing cookie-authenticated request.
- **S2-FR-007:** Only the current credential generation of an Active, epoch-current, unexpired
  family MAY authenticate; reads MUST NOT extend idle expiry unless explicitly classified as
  meaningful activity.
- **S2-FR-008:** Renewal MUST atomically supersede the prior generation, insert and select one new
  generation, rotate CSRF, and commit safe security evidence before issuing the cookie.
- **S2-FR-009:** Confirmed superseded-credential reuse MUST revoke the whole family and require
  normal authentication. *(BR-005)*
- **S2-FR-010:** Logout MUST revoke and commit server-side state before clearing the browser cookie.
  *(BR-006)*
- **S2-FR-011:** Authoritative mutations MUST recheck Account status, epoch, family state, and
  generation immediately before commit.
- **S2-FR-012:** Login, session resolution, renewal, and logout MUST use the planned contracts in
  `contracts/session-api.md`, Problem Details, `Cache-Control: no-store`, and risk-appropriate
  layered rate decisions.
- **S2-FR-013:** Arabic/English UI MUST implement login, safe `returnTo`, submitting, generic
  failure, expired, replaced, confirmed-reuse, retry, and logout states with RTL/LTR, keyboard, and
  responsive behavior.
- **S2-FR-014:** No authentication credential or CSRF value MAY enter local storage, session
  storage, IndexedDB, URL, analytics, console, retained error, or JavaScript-readable cookie.

## Success Criteria

- **S2-SC-001:** Every hidden login state has the same observable public failure class.
- **S2-SC-002:** Database/browser canary sweeps find no plaintext session credential or CSRF token.
- **S2-SC-003:** One hundred percent of tested concurrent renewal races have exactly one winner.
- **S2-SC-004:** Every confirmed-reuse case revokes the family; no superseded/revoked credential
  obtains replacement credentials.
- **S2-SC-005:** Logout prevents the next authentication and renewal attempt from the family.
- **S2-SC-006:** Every role uses its configured server-authoritative idle/absolute window.
- **S2-SC-007:** Frontend lint, typecheck, production build, responsive bilingual inspection, full
  local gates, hosted CI, and independent review pass with no critical/high finding.
