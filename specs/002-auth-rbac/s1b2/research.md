# S1B2 Research and Technical Decisions

## R1 — Credential model

**Decision:** Use one 32-byte opaque random session credential in
`__Host-gradex_session`; store only its SHA-256 digest. Return an independently generated CSRF
token in no-store JSON and store only its digest.

**Rationale:** D-034 is authoritative and keeps authentication credentials inaccessible to
JavaScript while preserving explicit server-side revocation and rotation.

**Alternatives rejected:** Dual access/refresh cookies and a Next.js token vault add state,
rotation paths, and failure modes without an approved launch requirement. Browser storage exposes
credentials to JavaScript.

## R2 — Existing database model

**Decision:** Reuse migration 0004's `sessions` family and immutable
`session_credentials` generations. Do not add a migration unless implementation proves a missing
database invariant.

**Rationale:** The schema already models current generation, ACTIVE/REVOKED/EXPIRED state,
role-independent expiry timestamps, supersession links, stale-use counters, and
`REUSE_DETECTED` family revocation.

**Alternatives rejected:** A second session table duplicates authority. Mutable credential rows
erase the evidence needed to classify stale use and detect reuse.

## R3 — HTTP resource shape

**Decision:** Provide `POST /api/v1/sessions`, `GET /api/v1/session`,
`POST /api/v1/session-renewals`, and `DELETE /api/v1/session`. Login creates a family; GET
rehydrates browser memory without rotating or extending idle expiry; renewal rotates deliberately;
logout revokes before clearing.

**Rationale:** Explicit resource semantics make credential-changing operations intentional and
auditable. Ordinary reads never cause surprise rotation or concurrent-tab churn.

**Alternatives rejected:** Refresh-on-every-request makes one-time generations unusable with
normal concurrency. Silent idle extension on every read defeats meaningful inactivity expiry.

## R4 — Authentication context

**Decision:** Resolve the cookie to a typed authenticated-session context containing only
non-secret account, family, generation, expiry, and role facts. Preserve the existing
`auth.Authenticator` user-ID seam for protected handlers while exposing typed session facts to
cookie-authenticated mutation middleware.

**Rationale:** Existing video authorization needs only the account ID. Renewal/logout/CSRF need
generation-specific facts and immediate authoritative rechecks. One resolver avoids divergent
cookie parsing.

**Alternatives rejected:** Putting the raw credential or CSRF token into Gin context increases
secret lifetime. Replacing every existing authorization interface would broaden S1B2 unnecessarily.

## R5 — Login indistinguishability

**Decision:** Normalize email, fetch the candidate Account, and run the configured Argon2id verify
path in all cases. Unknown email uses a startup-validated dummy hash. Wrong password, unknown,
unverified, and inactive Accounts return the same `401 AUTHENTICATION_FAILED` status, headers,
body, response padding class, and cookie behavior.

**Rationale:** BR-003 prohibits account and state enumeration. Dummy verification prevents the
largest computational timing split.

**Alternatives rejected:** Returning validation or state-specific messages leaks whether an Account
exists and why it cannot sign in.

## R6 — Role expiry configuration

**Decision:** Parse and validate separate idle/absolute windows for Student (7d/30d), Instructor
(1h/24h), and Admin (30m/12h), plus general recent-auth (10m), highest-risk recent-auth (5m), and a
short stale-use classification window. Every idle window must be positive and no greater than its
absolute window.

**Rationale:** D-034 requires server-authoritative role-specific expiry. Typed configuration makes
invalid launch settings fail closed during startup.

**Alternatives rejected:** One global session lifetime cannot meet the approved role risk profile.
Client-side expiry is advisory and can be modified.

## R7 — Rotation, concurrency, and stale reuse

**Decision:** Lock the family and presented generation in one PostgreSQL transaction. A renewal can
replace only the current ACTIVE generation; it supersedes the old row, inserts one new row, advances
the family generation, and appends evidence before commit. Immediate first stale use by a
non-sensitive request is rejected as `SESSION_REPLACED`; renewal, mutation, repeated, or late stale
use confirms reuse and revokes the family as `REUSE_DETECTED`.

**Rationale:** Row locking and database uniqueness give exactly one renewal winner while immutable
generations retain classification evidence. Confirmed reuse response protects the entire family.

**Alternatives rejected:** In-process locks fail across instances. Issuing credentials before
commit can create a browser credential with no authoritative row.

## R8 — CSRF and origin policy

**Decision:** Require a trusted first-party Origin, or same-origin Referer fallback where applicable,
plus a constant-time CSRF digest match on every state-changing cookie-authenticated request.
Login remains protected by S1B1 anonymous admission and origin policy.

**Rationale:** SameSite is defense in depth, not the sole CSRF control. Binding CSRF to the current
generation makes rotation invalidate the old request authority.

**Alternatives rejected:** SameSite-only policy is browser-policy dependent. A stable family-wide
CSRF token would not rotate at sensitive boundaries.

## R9 — Rate decisions and telemetry

**Decision:** Reuse the existing layered limiter. Login uses network, anonymous session, keyed
normalized-email HMAC, and global layers. Renewal/logout use network, authenticated family/account,
and global layers. Log only allowlisted outcome/stage codes, request IDs, and non-secret stable
identifiers or digests.

**Rationale:** Layering constrains distributed guessing and single-account attacks without exposing
raw identifiers. Safe stage telemetry closes the S1B1 diagnostic carryover.

**Alternatives rejected:** IP-only limiting penalizes shared networks and is easy to distribute
around. Logging emails, cookies, CSRF values, or passwords creates a credential/PII leak.

## R10 — Frontend state and navigation

**Decision:** Keep the CSRF token in a module-scoped memory store, rehydrate with
`GET /api/v1/session`, and clear it on failure/logout. Accept `returnTo` only when it is a normalized
same-origin absolute path without control characters or protocol-relative syntax; otherwise use the
role root.

**Rationale:** Reload can safely obtain a fresh representation of the current CSRF authority without
browser persistence. Strict internal redirects prevent open redirects and credential-bearing URLs.

**Alternatives rejected:** local/session storage and readable cookies violate D-034. Arbitrary
`returnTo` values enable phishing redirects.

## R11 — Readiness and S1B1 carryovers

**Decision:** Make schema readiness capability-aware: startup/readiness requires the highest
migration needed by enabled real capabilities. Reject deterministic password screening in every
environment except development, emit safe failure-stage telemetry, and set `Cache-Control:
no-store` before strict body binding can fail.

**Rationale:** A globally broad schema range can report healthy when an enabled route's tables are
absent. The other changes close explicit security findings before new credential-bearing routes.

**Alternatives rejected:** One static minimum version loses the relationship between enabled
features and required schema. Production-only deterministic-screen rejection leaves staging with a
non-production security behavior.
