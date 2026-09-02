# Gradex API, Security, and Integration Design

> Status: Developer-approved design
> Date: 2026-07-27
> Scope: MVP transport, browser security, authorization, delivery, provider, asynchronous-work,
> observability, configuration, and schema-evolution contracts
> Change boundary: Design only; this record implements no production API, session, provider adapter,
> or migration

## 1. Purpose and authority

This record turns the approved [platform architecture](2026-07-25-platform-architecture-design.md)
and [domain/data/state design](2026-07-26-domain-data-state-design.md) into the implementation-ready
MVP security and integration contract.

`/api/v1` is a private, versioned JSON API for Gradex-owned Student, Instructor, and Admin web
clients. It is not a public developer API: MVP supplies no public keys, client registration,
developer portal, third-party OAuth scope, or compatibility promise for undocumented endpoints and
fields. Coordinated first-party releases may make documented breaking changes. Internal versioned
contracts, generated types, integration tests, stable opaque identifiers, pagination, concurrency,
and idempotency remain required. A future mobile/public API is a separate product decision.

CORS permits only explicitly configured Gradex-owned origins; wildcard origins are prohibited.
Gradex treats every browser request as untrusted even when it is first party, and browser clients
never receive provider secrets, storage credentials, internal object keys, or privileged
infrastructure detail.

The existing Go video routes, `X-Debug-User-ID`, and `fake_entitlements` are development-only
seams, not compatibility commitments or production authority.

This design resolves no launch gate. `LG-003`, `LG-004`, `LG-009`, `LG-010`, `LG-014`,
`LG-018`, and `LG-019` remain configurable gated boundaries. Implementations fail closed at the
smallest safe scope; they do not invent legal, provider, accounting, scanning, email, or recovery
policy.

## 2. Common HTTP and representation contract

### 2.1 Negotiation, JSON, and correlation

| Concern | Locked rule |
|---|---|
| JSON input | JSON bodies use `Content-Type: application/json` and UTF-8. A no-content request needs no content type. Malformed JSON, duplicate object-member names, or structural ambiguity returns `400 MALFORMED_JSON`. |
| Schema | Unknown request members are rejected unless a documented extension object permits them. Semantic schema violations return `422`. |
| Negotiation | Gradex clients send `Accept: application/json, application/problem+json`. Success is `application/json`; Problem Details is `application/problem+json`; unsupported response negotiation is `406`; unsupported request media type/encoding is `415`. |
| Additive evolution | Clients ignore unknown response members. |
| Correlation | Gradex creates a fresh opaque `X-Request-ID` for every HTTP attempt. It contains no Account, email, token, or business meaning and equals Problem Details `request_id`. Client IDs are untrusted and are validated/replaced or retained only as a separate parent correlation value. |

Approved correlation identifiers can flow to protected logs, Audit, outbox, and external calls but
never expose internal traces to clients. Idempotency replays get a new request ID but remain linked
to the original operation internally.

### 2.2 Success shapes and metadata

A single successful resource is direct, without a universal `data` wrapper:

```json
{ "id": "0194...", "title": "Operating Systems", "status": "PUBLISHED", "revision": 7 }
```

Collections use one envelope:

```json
{ "items": [], "page": { "limit": 20, "next_cursor": null, "has_more": false } }
```

Cursor collections use `limit`, `next_cursor`, and `has_more`; they do not add totals/page
numbers unless those values are stable and inexpensive. An explicitly offset-paginated finite Admin
report instead uses `number`, `size`, `total_items`, and `total_pages`. One endpoint never
mixes cursor and offset semantics.

| Status | Command result |
|---|---|
| `201 Created` | A resource was created; normally send `Location`. |
| `200 OK` | Return the useful updated/resulting resource. |
| `202 Accepted` | Durable asynchronous work accepted; normally return an operation resource and `Location`, with `Retry-After` when useful. |
| `204 No Content` | Succeeded without useful representation; no body. A new `ETag` may still be sent. |

Privacy-sensitive public Identity admission is the narrow `202` exception: it returns a generic
acknowledgment without operation resource or `Location`, because neither may reveal Account,
secret, or delivery existence. Operation resources never expose queue, Redis, provider, stack, or
infrastructure detail.

Fields are `snake_case`; IDs are opaque UUID strings; timestamps are UTC RFC 3339; money is
integer fils plus explicit currency; optionality is documented consistently as omission or `null`;
and empty collections use `items: []`. Shared protocol metadata stays in headers. Success shapes
never use Problem Details and errors never use success shapes.

### 2.3 Problem Details

All errors use RFC 9457 Problem Details with `application/problem+json`. A Gradex-controlled
absolute `type` URI is the canonical machine identifier. Uppercase `code` is a one-to-one
generated helper and never contradicts `type`. `about:blank` is allowed only when HTTP status
alone is sufficient.

```json
{
  "type": "https://api.gradex.com/problems/validation-failed",
  "title": "Request validation failed",
  "status": 422,
  "detail": "One or more fields are invalid.",
  "instance": "urn:gradex:problem:01JXYZ...",
  "code": "VALIDATION_FAILED",
  "request_id": "01JXYZ...",
  "errors": [{ "code": "REQUIRED", "detail": "Email is required.", "location": "body", "pointer": "#/email" }]
}
```

Every problem includes `status`, equal to the HTTP response status; the HTTP status line remains
authoritative. `title` is a short type summary, `detail` is safe occurrence guidance, and
`instance` is opaque. `errors` exists only for multiple violations of one general validation
problem. Body errors use JSON Pointer fragments and escape `/` as `~1` and `~` as `~0`;
path/query/header/cookie errors use approved `location` and `parameter`.

Human-readable title/detail/violation text may use `Accept-Language`; MVP supports English and
Arabic where translated, otherwise a configured default, and returns `Content-Language`.
Machine fields are never localized. Problems never reveal stack traces, SQL/constraint/table/column
names, object keys, provider credentials/payloads, internal addresses, password/token verification
detail, another Account's email state, or unauthorized moderation evidence. Unexpected failure is
generic `500 INTERNAL_ERROR`; diagnostics remain protected and correlated by request ID.

| Status | Common Gradex use | Companion |
|---|---|---|
| `400` | malformed/structural request; required idempotency key missing | — |
| `401` | protected-resource authentication missing, expired, invalid, or replaced | fixed `WWW-Authenticate: GradexSession realm="gradex-web"` |
| `403` | authenticated but unauthorized; CSRF/origin failure | — |
| `404` | unavailable or concealed resource | — |
| `405` | method unavailable | `Allow` |
| `406` | acceptable response representation unavailable | — |
| `408` | timeout before command admission | — |
| `409` | domain conflict or command in progress | safe `Retry-After` where known |
| `412` | failed `If-Match` | — |
| `413` | content exceeds endpoint limit | — |
| `415` | unsupported media type or encoding | — |
| `422` | semantic validation, cursor mismatch/expiry, key reuse with changed semantics | — |
| `429` | evaluated quota exceeded | `Cache-Control: no-store`; safe `Retry-After` |
| `500` | unexpected failure | — |
| `503` | unavailable service/dependency or no safe limiter fallback | safe `Retry-After` |

This mapping is common, not exhaustive. Every browser-facing `401`, including the generic login
`401 AUTHENTICATION_FAILED`, returns the same
`WWW-Authenticate: GradexSession realm="gradex-web"` challenge. `GradexSession` is a
first-party opaque-session challenge, not Basic or Bearer authentication, so it does not ask the
browser to surface a native credential prompt or expose a token to JavaScript. The challenge never
varies by hidden Account state. A future mobile, service, or public bearer surface defines its own
challenge as part of that separate product contract.

### 2.4 Caching, validators, and URIs

Credential, security-token, payment, Refund, private-evidence, and similarly sensitive responses use
`Cache-Control: no-store`. Personalized revalidatable GET uses `Cache-Control: private, no-cache`.
Public Catalog caching needs an explicit reviewed shared-cache policy. Localized cacheable output uses
`Vary: Accept-Language`; origin-selected output uses `Vary: Origin`.

Revision-protected aggregates return strong `ETag` values derived from authoritative revision and
any representation variant necessary for strong validation. Their documented mutations require strong
`If-Match`, evaluated immediately before action; weak validators are rejected and mismatch is
`412`. A replay returns its original business ETag even after later change.

URI segments are lowercase plural nouns, hyphenated when multiword; JSON/path/query names are
`snake_case`, for example `/api/v1/course-revisions/{course_revision_id}`. Nest only where parent
scope is semantic; retain direct canonical child URIs; avoid depth above roughly two ownership levels.
Role namespaces describe a real operational surface but never grant authority. Use subordinate
resources for consequential commands, e.g. `POST /orders/{order_id}/refunds`, and explicit command
paths only where no durable domain resource exists. GET retrieves; POST creates/executes
non-idempotent command; PUT replaces/client-selects identity; PATCH partially mutates; DELETE requests
allowed deletion/cancellation.

No trailing slash is canonical except `/`; segments are lowercase and case-sensitive; IDs are never
business-parsed; filters/search/sort/page are query parameters; sensitive values do not enter ordinary
paths/query. Reject or redirect alternate forms consistently. Existing video handlers are temporary
compatibility adapters to Media ownership, never a second source of truth.

## 3. Idempotency and signed pagination

### 3.1 Idempotency

`Idempotency-Key` is Gradex's contract informed by IETF HTTPAPI work, not a claim of published-RFC
compliance. Required sensitive commands uniquely scope:

```text
(scope_type, scope_id, command_type, idempotency_key)
```

Account, Service, and Provider Account are example scopes. Provider webhooks use verified provider
observation deduplication instead. Anonymous Identity commands use a privacy-safe scope derived from
command, Gradex client, and HMAC-normalized destination.

Fingerprint method, route template, normalized path, semantic query, canonical body, and material
preconditions. Exclude credentials, cookies, request/trace IDs, user agent, and transport metadata.
Upload completion includes Intent, expected provider object/version, size, checksum, and command
fields—not bytes. `Accept-Language` does not alter the fingerprint.

| Condition | Result |
|---|---|
| required key missing | `400 IDEMPOTENCY_KEY_REQUIRED` |
| same scoped key is processing | `409 IDEMPOTENCY_IN_PROGRESS` |
| same key has different fingerprint | `422 IDEMPOTENCY_KEY_REUSED` |
| identical completed retry | original status/body/Problem instance and replay-safe `Location`, `Content-Type`, `Content-Language`, `ETag`; add `Idempotency-Replayed: true` |

Replays refresh request/date/trace/rate/cookie transport values. A replayed Problem retains original
`instance` but has the retry's `request_id`. Records are `PROCESSING`, `COMPLETED`,
`RETRYABLE_FAILED`, or `RECONCILIATION_REQUIRED`; leases and reconciliation prevent permanent
blocking/duplicate execution. Pre-admission failures are not finalized. Deterministic post-claim
validation/conflict may be retained; transient uncommitted `5xx`/timeouts are never permanently
replayed. `If-Match` remains independent and is part of the fingerprint. Sensitive financial,
access, and security evidence retains idempotency records with authority; no sensitive automatic
purge precedes approved retention. Keys are high-entropy opaque values with no personal data.

### 3.2 Cursors

Cursor endpoints use deterministic keyset ordering: business sort fields plus globally unique stable
tie-breaker, normally UUID `id`. A URL-safe Base64 HMAC-SHA-256-protected cursor contains schema
version, endpoint ID, full sort/direction/null order, last values, filter hash, audience hash where
authorization changes results, optional issue/expiry, and key ID. It is opaque but not confidential:
no secrets, SQL/table names, object keys, email, protected business data, or internal topology. Use
encryption/server state where sensitive state is unavoidable.

Use a dedicated rotated cursor MAC key and constant-time verification; never reuse JWT, password,
webhook, or encryption keys. Canonical filter hashing uses effective semantics: parameter order,
defaults, repeated filters, normalized search, selected sort, and visibility scope. Page size may
stay outside if it changes batch size only. Each endpoint documents direction, null order, text
collation, matching index, and lexicographic continuation predicate.

Ordinary collections are live views; inserts/deletes/sort changes can affect later pages. Stable
reports use immutable `as_of`, materialized report/export, or persisted snapshot. Malformed
encoding/schema/signature is `400 CURSOR_INVALID`; endpoint/sort/filter/audience mismatch is
`422 CURSOR_CONTEXT_MISMATCH`; elapsed validity is `422 CURSOR_EXPIRED`. No response reveals
which signature/key/database condition failed. The API is forward-only; clients may retain earlier
cursors locally.

## 4. Browser Identity and session security

### 4.1 Same-origin BFF

The browser uses a single Gradex public origin, for example `https://app.gradex.com`, where
Next.js assets/pages and `/api/v1/*` share the browser-facing origin and a gateway reverse-proxies
to Go. Deployments may remain separate behind that boundary.

The browser receives only an opaque server-managed session cookie:

```text
Set-Cookie: __Host-gradex_session=<opaque-random-value>;
            Secure; HttpOnly; SameSite=Strict; Path=/
```

It has no `Domain` attribute. Browser JavaScript never receives access or refresh bearer tokens;
they are prohibited from localStorage, sessionStorage, IndexedDB, JavaScript-readable cookies, and
persisted state. Ordinary API calls need no credentialed cross-origin CORS.

Cookie `Max-Age`/expiry never exceeds server absolute expiry. Server state remains authoritative
for idle/absolute expiry, Account status, revocation, and epoch. Clearing uses the same name,
host-only scope, Path, Secure, and SameSite. The gateway does not forward this cookie to static,
CDN, or unrelated upstream services, and logs never retain its value.

`SameSite=Strict` is intentional for MVP. A cross-site return navigation from Tap may omit the
session cookie on the first HTML request, so the checkout-return page is public, renders no private
Order/Account state, and treats `tap_id` only as an untrusted discovery hint. After that page loads,
a same-origin API request re-establishes the browser-session view and retrieves authoritative payment
status; if the session is unavailable, the user signs in again. Provider webhook/retrieval evidence,
not the redirect or initial SSR state, completes payment.

### 4.2 Stable sessions and immutable generations

`sessions` is the stable server-authoritative family record: Account/family IDs, session epoch,
authentication/activity/reauthentication times, idle/absolute expiry, current credential generation,
revocation reason/time, and reuse status. `session_credentials` has immutable
session/generation rows: credential and CSRF digests, issue/supersession times, replacement
generation, and stale-use/reuse evidence. Digests have appropriate uniqueness constraints. Only the
current unsuperseded generation authenticates.

Credentials are random opaque values stored only as digests. Login creates a new authenticated family.
The anonymous pre-authentication session becomes invalid and is never authenticated-family ancestry.

| Role | Idle expiry | Absolute expiry |
|---|---:|---:|
| Student | 7 days | 30 days |
| Instructor | 1 hour | 24 hours |
| Admin | 30 minutes | 12 hours |

These configurable production defaults require a security decision to change. Only meaningful
authenticated activity resets idle expiry. Polling, bootstrap, notification refresh, static assets,
failed/unauthenticated traffic do not. Explicit validated Student playback may count, but an
abandoned tab cannot preserve a session indefinitely.

Admin financial/security commands require primary credential authentication within five minutes;
other sensitive Account changes require it within ten. This includes Refund/payout initiation or
approval, payout destination, Account suspension/reactivation, emergency Course suspension,
email/password, authenticator/session, and sensitive reconciliation/retention actions. Recent primary
authentication is not MFA. MFA is deferred for MVP; enabling highest-risk payout/recovery functions
requires separate production-security acceptance.

### 4.3 Rotation, revocation, and race safety

Ordinary requests never rotate the session. Rotate only after login, anonymous-to-authenticated
transition, successful reauthentication, password change, security-sensitive recovery, and future
authenticator/privilege boundaries. In one transaction: lock session, verify active generation,
create new credential/CSRF digests, mark prior immutable generation `SUPERSEDED`, link
replacement ancestry, increment generation, commit required evidence, and issue a no-store cookie.

A superseded credential is invalid immediately and has no authentication grace. Its immediate
presentation returns `401 SESSION_REPLACED`, does no work, and is recorded. A configurable
seconds-scale classification window avoids falsely revoking a new session for normal concurrent
in-flight browser requests. Delayed, repeated, sensitive, materially context-inconsistent, or
rotation/recovery-endpoint use confirms probable reuse and revokes the affected family.

Every authoritative mutation retains admitted session ID, credential generation, and session epoch.
Immediately before commit, its transaction rechecks these with Account status and mutable policy
facts. Rotation, logout, suspension, recovery, or epoch invalidation therefore blocks an
already-admitted mutation. Ordinary reads may finish unless especially sensitive but do not extend a
superseded generation.

| Event | Required result |
|---|---|
| Logout | Revoke current session, clear cookie, issue no replacement. |
| Logout all devices | Revoke all families or increment `session_epoch`. |
| Password change | After fresh authentication, revoke other sessions and re-establish current session. |
| Successful password recovery | After secret consumption and password commit, atomically revoke/invalidate all authenticated families; require normal login. A request alone never changes security state. |
| Account suspension/deactivation | Deny every session immediately. |
| Confirmed credential reuse | Revoke affected family and require authentication. |
| Future role conversion | Invalidate all sessions. |

### 4.4 CSRF, Origin, and bootstrap

Each cookie-authenticated `POST`, `PUT`, `PATCH`, or `DELETE` requires structural/media
checks; exact trusted Origin or controlled Referer fallback; valid session-bound
`X-CSRF-Token`; authentication sufficient to establish the trusted rate-limit scope; application
rate-limit decision; idempotency admission; and then full resource authorization, concurrency
preconditions, and the domain command. A CSRF rejection never claims idempotency or creates domain,
Audit, or outbox records. A rate-limit rejection never claims idempotency, and full authorization is
re-evaluated inside every authoritative mutation transaction as required by §6.1.

The synchronizer token is random, server-session-bound, browser-memory-only, sent only in
`X-CSRF-Token`, compared safely, rotated with session credential, and invalidated on logout,
revocation, expiry, or epoch invalidation. It never enters URL, cookie, persistent browser storage,
logs, traces, analytics, or errors. Failure is generic `403 CSRF_VALIDATION_FAILED`.

When present, Origin exactly matches configured scheme/host/port. Wildcard, suffix, substring,
sibling-domain, `null`, and dynamically Host/forwarded-header-derived matching are rejected. When
Origin is absent, parsed HTTPS Referer must exactly match configured origin; path/query are ignored.
If neither trusted value is usable, reject. SameSite and JSON content type are defense in depth, not
substitutes. GET, HEAD, and OPTIONS never change domain/business state.

`GET /api/v1/session/bootstrap` is the safe-method exception for ephemeral anonymous security state:
it may create/reuse anonymous host-only cookie and return CSRF token with `Cache-Control: no-store`.
It never creates Account, credential delivery, Order, Entitlement, or other business record and does
not extend authenticated idle expiry. Login requires its Origin/CSRF defense, then creates a wholly
new authenticated family and CSRF token.

### 4.5 Secure bootstrap Administrator

The one bootstrap Admin is created by a restricted, out-of-band deployment operation, never by an
HTTP endpoint or ordinary application worker. The operation requires the dedicated deployment
principal, an explicit production configuration gate, a stable operation ID, the normalized Admin
email, and an initial password supplied only through the approved secret manager or equivalent
secure one-time injection. The plaintext password never enters Git, a migration, process arguments,
logs, telemetry, Audit, support tooling, or the database; Identity stores only its Argon2id hash.

One PostgreSQL transaction locks a singleton bootstrap marker, proves no bootstrap operation has
completed, proves no Admin Account exists, validates the password policy, creates the verified
`ACTIVE` Admin with immutable `ADMIN` role and `PASSWORD_CHANGE_REQUIRED`, writes immutable Identity
security evidence and required Audit, and marks the stable operation complete. Failure rolls the
entire transaction back. A retry with the same operation ID returns the recorded result; any later
or differently fingerprinted attempt fails closed and cannot mint another Admin.

The temporary credential can authenticate only into a restricted password-change session. Until the
password is successfully changed, the bootstrap Account cannot use Admin, financial, security,
retention, provider, or content-management capabilities. Successful change uses the normal recent-
authentication, credential/session rotation, Audit, and notification rules, clears
`PASSWORD_CHANGE_REQUIRED`, and creates the first ordinary Admin session. Additional Admins are then
created only through the approved invitation workflow.

## 5. Public Identity privacy boundary

Public endpoint responses are uniform for hidden Account-state outcomes after ordinary input and abuse
admission. Malformed JSON, invalid email syntax, missing fields, media/size, CSRF/origin, and
rate-limit failures retain ordinary errors; only Account existence, lifecycle, eligibility, queue,
and delivery are concealed.

| Endpoint | Uniform external result |
|---|---|
| Registration | `202 REGISTRATION_REQUEST_ACCEPTED` |
| Verification request/resend | `202 VERIFICATION_REQUEST_ACCEPTED` |
| Password-reset request | `202 PASSWORD_RESET_REQUEST_ACCEPTED` |
| Login failure | `401 AUTHENTICATION_FAILED` |
| Invalid secret consumption | `400 TOKEN_INVALID` |
| Authenticated email-change initiation | `202 EMAIL_CHANGE_REQUEST_ACCEPTED` |

Hidden outcomes use the same status, body, meaningful headers, timing class, redirects, cookies,
response-size class, and externally observable delivery behavior. A generic `202` does not assert
Account existence or delivery. Unknown-email login performs a dummy password-hash verification with
production-comparable parameters. Internal causes belong in protected security monitoring.

A valid first registration atomically creates one `PENDING_VERIFICATION` Student Account, password
credential, verification secret digest, immutable Identity registration/security evidence, and
notification outbox intent. Existing email causes no Account, role, status, credential, or
verification mutation and does not become resend/recovery. Eligible reset/verification requests
create/rotate purpose-bound, hashed, single-use, time-limited secret plus delivery intent; ineligible
or nonexistent cases are privacy-safe no-ops.

Email-change initiation requires active session, recent authentication, CSRF/origin, and current
Account as fixed subject. It creates a pending Email Change Request; email does not change until the
new address verifies and availability remains concealed. Completion atomically changes normalized and
original email, consumes secret, invalidates pending changes and relevant verification/reset secrets,
increments Account revision and session epoch or revokes other sessions, and writes protected old/new
evidence plus former-address notification intent. An Admin change requires confirmation from both old
and new addresses until a future MFA-backed approved recovery/change route exists.

Malformed, unknown, wrong-purpose, expired, consumed, revoked, superseded, or conflicting secrets
share `TOKEN_INVALID`. Layered Identity limits use endpoint + HMAC(normalized identifier), trusted
client IP/network, anonymous session/device, and global budget—never plaintext email or disclosed
limiting dimension.

## 6. Authorization and abuse controls

### 6.1 Module-owned typed policies

Authorization is deny-by-default. Every protected action maps to explicit typed policy owned by the
module that owns relevant business state: for example `Catalog.AuthorCourse`,
`Catalog.ApproveRevision`, `Commerce.InitiateRefund`, `Entitlements.PlayLesson`,
`Media.DeliverAssetVersion`, `Learning.ViewCourseRoster`, and
`Reporting.ApprovePayoutStatement`. A role makes a request eligible; it never automatically grants
authority over every module resource.

Identity supplies trusted principal facts—Account ID, immutable role, Account status, session ID,
credential generation, epoch, authentication and reauthentication time—but does not decide
ownership, Entitlement coverage, exact Media version graph access, or Refund eligibility. Modules
own those policies/data. A small coordinator composes owner-defined policy/query contracts for
cross-module decisions but owns no copied authorization state and never mutates module tables
directly.

Policy results are internal typed `ALLOW` or `NOT_AUTHENTICATED`, `ACCOUNT_INACTIVE`,
`ROLE_NOT_ALLOWED`, `NOT_OWNER`, `ACCESS_NOT_COVERED`, `RECENT_AUTH_REQUIRED`,
`RESOURCE_SUSPENDED`, and `STATE_NOT_ALLOWED`. Reasons guide tests/security monitoring but do not
automatically enter public errors. A concealed resource may yield the same `404` for absence and
invisibility.

Authorization-sensitive queries select only rows the principal may examine; frontend/post-query
filtering is never a boundary. Mutations conduct early checks then, inside transaction, lock owner
and guard records, recheck session generation/epoch plus mutable facts, and mutate only if still
allowed. Workers use explicit User, Service, Migration, or Scheduled-Job principals, not a generic
unrestricted system flag; they validate durable event/command identity, authority epoch, operation
ID, expected state, and limited capability.

Routine policy checks are not Audit rows. Security logs/metrics handle routine denial; consequential
privileged decisions co-commit Audit. PostgreSQL RLS is not MVP's business-authorization engine.
Database roles, grants, schema ownership, and restricted updates provide accidental-write defense;
selective RLS may later add defense in depth without duplicating policy.

### 6.2 Layered, risk-tiered rate limits

Edge/gateway controls absorb broad connection, body, and volumetric traffic. Redis provides atomic
distributed API quotas; CDN/provider controls protect delivery and external services. Redis is
disposable operational state, not business, authorization, idempotency, webhook-deduplication, or
commercial authority.

Each endpoint category has a versioned policy: ID, category, dimensions, algorithm, capacity/refill/
burst, operation cost, concurrency limit, outage mode, local fallback budget, and retry behavior.
Only four modes exist: `FAIL_CLOSED`, `STRICT_LOCAL_FALLBACK`,
`CONSERVATIVE_LOCAL_FALLBACK`, and `WEBHOOK_DURABLE_ADMISSION`. Payout, Refund, suspension,
retention, highest-risk security commands, and production password login fail closed. Login requires
Redis-coordinated admission before PostgreSQL lookup or Argon2id because a process-local fallback
cannot enforce the shared expensive-work budget. Explicitly approved recovery, checkout, upload,
report, and playback-grant paths use strict local fallback; low-cost reads use
conservative local fallback; valid provider webhooks use durable admission.

Return `429` only after a policy has been evaluated and quota exceeded; it is Problem Details with
`Cache-Control: no-store`. Redis unavailability with no safe fallback is
`503 RATE_LIMITING_UNAVAILABLE`, never `429`. Include `Retry-After` only when safe. Local
fallback is deliberately stricter, short-windowed, memory/key-bounded, sized for maximum replicas,
and never merged into Redis.

Production browser bootstrap is cheap and permits 600 attempts/minute endpoint-wide, globally, and
per exact IPv4 source (IPv6 is aggregated to `/64`). Production login atomically checks then commits
five one-minute buckets: normalized identifier 6, anonymous browser 10, exact IPv4 source or IPv6
`/64` 600, endpoint 600, and global expensive-work budget 600. A denial commits none of them, so an
attack against one identifier cannot drain the shared legitimate budget. Trusted-proxy parsing is
the only source of forwarding-header authority.

After distributed admission, one process-local bounded gate wraps the actual stored or dummy
Argon2id verification. The current KVM2 default is one active verification, 500 waiting requests,
and a 45-second queue wait inside a login-only 60-second request/write deadline. Cancellation frees
the queue slot; saturation and timeout fail authentication safely. Unknown Accounts still execute
the current-strength dummy hash and receive the same generic authentication response.

Public Identity keys combine endpoint + HMAC-normalized identifier using dedicated versioned secret,
trusted gateway-sanitized IP, network prefix, anonymous session/device, and global budget. Normalize
and bound identifier before HMAC, but never query Account existence first. Unverified webhook fields
are never authoritative limiter keys. Until provider verification, webhook ingress uses strict
body/method/content/concurrency/edge control; a new valid durably admissible observation is never
dropped just because Redis is unavailable.

Atomic token-bucket/sliding-window decisions use Redis Lua or equivalent primitives, per-key TTL,
bounded cardinality, dedicated prefixes, timeout/circuit-breaker, and allow/deny/fallback/
unavailable/latency metrics. Each endpoint also defines body/upload size, page/batch cardinality,
expensive search, concurrent upload/processing, active delivery grants, external sends, spending,
storage/bandwidth, and execution-time limits.

Admission order is edge limits; method/media/structure; Origin/CSRF; authentication sufficient for
trusted limit scope; application rate decision; idempotency; authorization/preconditions/domain
command. Completed replays remain subject to edge/replay-abuse limits. Rate limit playback-session
creation, manifests/renewals, download initiation, repeated capability issuance, and media processing
requests—not every CDN segment. CDN/provider independently controls bytes/connections/bandwidth/path
access/capability expiry/WAF.

## 7. Protected Media delivery

Media authorizes exact `media_asset_version`, not merely Course. A cookie-authenticated,
CSRF-protected `POST /api/v1/media-asset-versions/{version_id}/playback-sessions` creates a
short-lived server-side playback/delivery grant. It records Account and authenticated Session
reference, exact Asset Version, acquired Course revision/authorization evidence, purpose
(`PLAYBACK` or `DOWNLOAD`), issue/expiry/revocation, stable grant ID, and policy evidence.

`GET /api/v1/playback-sessions/{playback_session_id}/manifest` is a safe GET that returns only
rewritten HLS playlist text. For every playlist layer it revalidates session state, grant validity,
exact Asset Version, Entitlement, acquired/historical graph, emergency Course suspension, Media
readiness, and expiry/revocation. It rewrites/protects multivariant/rendition playlist,
initialization, audio, subtitle, and segment references. Go never serves .ts, .m4s, .mp4,
subtitle packages, or other large media bytes. It returns
`Content-Type: application/vnd.apple.mpegurl`, `Cache-Control: private, no-store`, and
`Referrer-Policy: no-referrer`.

CDN/object storage serves media bytes from a private, non-bypassable origin. Provider configuration
requires HTTPS, protected exact paths, signing-key rotation, compatible cache behavior, GET/HEAD/
Range support, exact limited CORS when needed, and capability-query redaction/restricted logging.

Provider capabilities are bearer secrets, not Gradex-session-bound credentials. Scope exact immutable
object/rendition, GET/HEAD as needed, short expiry, purpose, provider key ID, and optional opaque
playback-session reference. They contain no Account/email/Entitlement/Course/internal object/
infrastructure data. IP binding is not default. Application logs, traces, Audit, errors, analytics,
and support tooling never intentionally record them; CDN/gateway logs redact or restrict them.

Download initiation may consume once, but the resulting capability supports HEAD, Range, retries,
and parallel requests required to complete a valid download. Playback session, manifest,
segment/rendition, and download lifetimes are separate configuration. Logout, suspension, or
Entitlement revocation block new grants/manifest renewal immediately; already-issued provider
capabilities can persist only through short expiry unless provider supports immediate revocation.
This is access control, not DRM.

Provider-neutral adapters are equivalent to:

```text
CreatePlaybackCapabilities(asset_version, rendition_scope, playback_session, expires_at)
CreateDownloadCapability(exact_object, download_grant, expires_at)
```

They may use signed URLs, path tokens, edge validation, or future signed cookies without changing
the Gradex domain model.

### 7.1 Malware-scanning adapter

Upload completion always establishes the exact provider object/version, size, type, and checksum and
leaves the Asset Version in private quarantine. From quarantine, Gradex selects the authorized
safety path for that upload. Under the bounded trusted-Instructor launch profile in D-088, an
`ACTIVE` vetted Instructor's approved MP4 Lesson video or PDF/DOCX Lesson Resource may progress
without malware scanning only after exact-version validation succeeds. That validation verifies the
configured size bound, actual stored size, declared type against the actual file format/signature,
and SHA-256 checksum. The system records this path as validation rather than fabricating
`SCAN_PASSED` or a successful scan attempt.

A validated D-088 Lesson video still requires successful trusted FFmpeg processing before `READY`
and protected HLS delivery. A validated D-088 PDF/DOCX Lesson Resource may become `READY` after the
required exact-version validation and remains available only through the existing authenticated,
Entitlement-checked protected-download boundary. Validation failure leaves the Asset Version
non-deliverable.

Every upload outside the D-088 trusted-Instructor profile remains scanner-gated. D-096 admits the MP4 public preview to that profile, where it must additionally complete trusted video processing before it is deliverable; every non-MP4 preview stays scanner-gated.
For those assets, quarantine atomically appends one stable scanning operation to the transactional
outbox, and no public preview, protected delivery, Course approval dependent on that asset, or
`READY` transition is possible until the exact quarantined object has verified clean scan evidence.

The scanner adapter remains provider-neutral until `LG-014` is reopened and approves a service,
supported limits, authenticity mechanism, and test vectors. Its operation binds provider
account/environment, exact object identity/version/checksum, scan-policy version, and Gradex
operation ID. It uses that operation ID as provider idempotency key where supported. A callback is
external ingress authenticated by the approved scanner-specific procedure; polling retrieves the
authoritative provider result. Source IP, filename, content type, and caller-supplied result fields
never prove scan success.

Immutable scan attempts distinguish queued/submitted, provider-accepted, clean, rejected,
retryable-failed, unknown, and exhausted outcomes. A timeout or ambiguous provider response is
`UNKNOWN` and is reconciled before another provider submission. Verified callbacks/results are
deduplicated by the strongest provider event or observation identity. Duplicate, delayed, or
reordered observations cannot regress terminal rejection, replace the exact object under review, or
create a second `READY` transition. A clean result applies only when object identity, checksum,
policy, current Asset Version state, and authority epoch still match.

Transient failures retry with bounded backoff, jitter, concurrency, cost, and elapsed-time limits.
Known malicious content, unsupported permanent conditions, or authenticity failure never retries as
clean. For an asset that requires scanning, scanner outage, unknown result, exhausted retry, or
missing scanner configuration fails closed: the Asset Version remains quarantined and unavailable,
the exhausted operation stays visible, and any manual retry is an authorized idempotent command
with immutable attempt evidence. No feature flag, Admin action, or direct storage URL may bypass
required scan evidence. The only launch exception is the explicit D-088 trusted-Instructor
validation path; it must never be represented as malware-scanned or malware-free.

## 8. Provider ingress and reconciliation

Provider webhooks are external ingress outside first-party `/api/v1`, for example:

```text
POST /provider-webhooks/tap/payments
POST /provider-webhooks/tap/refunds
```

They have no browser Session, CSRF, CORS, localization, or first-party compatibility promise. They
are HTTPS-only, do not redirect, enforce strict method/content/body limits, capture bounded raw bytes
before parse, and apply only the exact approved provider authenticity procedure.

For Tap, LG-010 must freeze the documented `hash`/`hashstring` scheme: signed parsed fields and
order; amount/currency formatting; headers; merchant-secret selection/rotation; constant-time
comparison; test/live separation; official vectors; retry/response behavior. Gradex does not invent
timestamp, nonce, replay-window, or key-ID validation a provider does not document. Production Tap
processing stays disabled until that adapter contract is tested and approved.

After authenticity, one PostgreSQL transaction creates/deduplicates immutable provider observation,
preserves raw-body digest and protected verification evidence, resolves provider account/environment,
links an unambiguous Payment/Refund/Dispute Attempt or creates reconciliation, and appends durable
processing outbox intent. Commit precedes minimal provider-compatible acknowledgment. An asynchronous
command separately validates expected merchant/environment/object/reference, amount/currency
precision, Attempt, allowed transition, deadline, cumulative capture/refund, and occurrence time
where available. Browser redirect and `tap_id` are discovery hints, never payment proof. Ambiguous/
sensitive results retrieve authoritative provider state before business completion.

Use strongest provider observation identity:
`provider + provider_account + provider_event_id`. Without event ID, derive approved stable identity
from provider object ID, kind, status, occurrence time, and verified payload fingerprint. Retries
deduplicate; materially different verified observations remain. New/duplicate/unmatched verified
observations safely acknowledge after durable admission. Invalid authenticity receives minimal
provider-compatible non-2xx; PostgreSQL admission failure receives retryable failure. No response
reveals Order, Student, Payment Attempt, or Refund existence.

Raw payload/signature evidence is encrypted or access-restricted financial evidence. Logs, traces,
analytics, errors, and support views never contain raw payloads, signatures, secrets, or full
capability values. Source IP and User-Agent are weak monitoring signals, not authenticity.

## 9. Transactional outbox and email

### 9.1 At-least-once work contract

Gradex makes no exactly-once external delivery claim. The owner module atomically commits
authoritative mutation, required Audit evidence, and immutable asynchronous intent in PostgreSQL.
Redis may wake workers, but PostgreSQL alone reconstructs pending work. Retries retain their stable
logical ID and add immutable attempts.

| Identity | Meaning |
|---|---|
| `event_id` | immutable domain fact or notification intent |
| `operation_id` | logical asynchronous command |
| `attempt_id` | one worker or provider attempt |
| consumer receipt | evidence a named consumer applied event/operation |

Receipt uniqueness is `UNIQUE(event_id, consumer_name)` or
`UNIQUE(operation_id, consumer_name)`. An immutable `outbox_events` event holds type/schema
version, source module/aggregate/revision, payload or source reference, occurrence/availability,
authority epoch, and correlation ID. `outbox_deliveries` holds consumer, delivery state, attempts,
availability, lease owner/expiry, safe last error, and completion. Delivery is
`PENDING → LEASED → COMPLETED`, with retry-pending, unknown, and exhausted branches. Failure never
rewrites the event.

For PostgreSQL-only effects, domain/projection effect and consumer receipt commit in one transaction.
For external effects, send `operation_id` as provider idempotency key where supported; retain
attempt, provider reference, and request fingerprint; classify timeout as ambiguous and reconcile
before another submission. Webhook deduplication remains its independently verified observation
contract and can arrive duplicate, before worker response, after timeout, reordered, or later.

No global order is promised. Where necessary, include aggregate revision/sequence. Consumers enforce
predecessor, read current authority, or safely ignore stale events; timestamps are not ordering.
Events have stable type/schema version; consumers explicitly support versions, ignore additive
unknown fields, visibly reject unsupported semantic versions, and preserve historical snapshots such
as Instructor/revenue-share version rather than querying mutable current owner.

Workers claim with guarded PostgreSQL updates or `FOR UPDATE SKIP LOCKED`, lease expiry recovery,
exponential backoff/jitter, authority-epoch guard, and visible exhaustion. Manual retry is a new
audited attempt. Ambiguous external effects reconcile before automatic or manual retry. Payload
minimization waits for all required receipt/disposition, no ambiguity, adequate source/provider
reconciliation references, and retention permission. Financial/access/security/privileged evidence
uses its stricter retention policy.

### 9.2 Transactional email

Provider outage does not normally turn registration, verification resend, password reset, or
email-change admission into `503`. Once authoritative Identity state and PostgreSQL notification
outbox intent commit, Gradex returns normal privacy-safe response. Provider delivery is bounded,
observable, idempotent, and asynchronous; it never rolls back Identity or exposes Account state.

Generic `503 TRANSACTIONAL_DELIVERY_UNAVAILABLE` is allowed only when durable delivery admission is
unsafe: source/outbox transaction unavailable, hard backlog safety limit reached, required
sender/configuration invalid/disabled, admission intentionally closed, or required intent cannot
commit. Public outcomes retain uniform status/header/timing/cookie behavior whether or not the
identifier matches an Account.

Immutable attempts use `PENDING`, `SENDING`, `ACCEPTED`, `DELIVERED`,
`RETRYABLE_FAILED`, `PERMANENTLY_FAILED`, and `UNKNOWN`. Accepted means provider responsibility;
delivered requires provider evidence. A timeout reconciles before another provider call. Provider
acceptance/delivery events deduplicate. Retries use exponential backoff/jitter, bounded count or
elapsed horizon, provider retry guidance, concurrency/spend limits, and circuit breaker. Permanent
syntax/bounce/suppression/sender/credential/content failures do not retry. Exhausted work stays
visible and manually retriable only through audited command.

Before first provider acceptance of a message containing verification/reset/invitation/email-change
secret, worker checks configured minimum remaining validity. If insufficient and unaccepted, it
atomically supersedes old secret, makes idempotently linked replacement secret/protected payload, then
sends. Accepted-message secrets are never silently replaced. Superseded secrets remain invalid. Raw
secrets appear only in protected delivery payload; token tables hold digests.

Monitor pending age/count by purpose, failure class, provider acceptance latency, delivery/bounce/
complaint, near-expiry secret, exhaustion, and circuit state. Operational escalation occurs before
hard admission closure. Secondary-provider failover retains notification event and operation ID to
prevent duplicate sends. Security mutation notification failure never rolls back completed mutation;
exhausted mandatory notification is an operational security incident.

## 10. Audit and observability

### 10.1 Audit ledger

MVP Audit is an append-only PostgreSQL ledger with strong operational integrity, not a claim of
cryptographic tamper proof. It uses dedicated schema, restricted roles, and an Audit-owned
transaction-aware contract. Ordinary repositories cannot write directly; application roles have no
UPDATE, DELETE, or TRUNCATE permission. A trigger may add defense in depth. Restricted migration
credentials, privileged database-access monitoring, backups, and point-in-time recovery preserve it.

Required evidence co-commits with privileged state mutation and outbox intent; Audit write failure
rolls back that mutation. Examples: Account suspension/reactivation; Course approval, retirement,
reassignment, emergency suspension; Refund; Entitlement adjustment; payout approval; moderation;
manual outbox retry; retention/deletion authorization; migration authority cutover. Routine reads,
checks, playback, and non-consequential denials stay in operational/security telemetry.

Records include stable event ID, authoritative UTC occurrence, actor and effective principal, action,
target/revision, outcome, structured reason, request/correlation/idempotency references, source
operation, authority epoch, safe before/after reference or minimized data, and metadata schema
version. They never contain password, session/CSRF/reset secret, provider secret, signed Media URL,
or unrestricted provider payload. Corrections append linked `CORRECTION` or
`SUPERSEDING_EVIDENCE`; originals never update. Hash chains, notarization, WORM export, and
independent replication remain deferred until an approved compliance threat model covers custody,
verification, recovery, partitioning, and administrator access.

### 10.2 Strict telemetry data boundary

Operational telemetry, security events, Audit ledger, and domain/financial evidence are distinct.
Logs, metrics, and traces are never authority for payment, Refund, Entitlement, Audit, provider
dedupe, payout, session validity, or migration evidence. Their loss cannot change domain correctness.

General telemetry uses structured JSON/schema versions, UTC/synchronized clocks, typed allowlisted
attributes, size limits, log-injection sanitization, TLS, bounded local buffers, and dedicated
exporter credentials. A normal API signal may include service/environment/deployment, request/trace
IDs, route template, method/status, duration, response-size class, module/operation, safe error
code, and authority epoch. It never contains request/response body, raw query, cookie,
Authorization/CSRF header, idempotency value, signed capability, email/display name, password/token,
bank data, provider signature/payload, or raw SQL parameter.

Direct personal data is excluded by default. Approved restricted security event may use minimized
pseudonymous Account reference or truncated network address for detection/investigation. Metric labels
are low-cardinality aggregate values only: service, module, route template, method, status class,
safe error, worker/operation/provider type, environment—not IDs, email, IP, URL, request ID,
user agent, or raw error text.

Automatic instrumentation disables/sanitizes body, arbitrary header, query/URL credentials, SQL bind,
GraphQL argument, provider/email payload, file content, and exception value capture. Exception
telemetry keeps class, safe error, sanitized stack, module/service, and correlation. Application
instrumentation rejects unknown attributes; middleware plus OpenTelemetry Collector filtering/
redaction are defense in depth. High-risk exporters drop unknown attributes by default.

Security, Audit, provider, and financial stores have separate destinations/schemas, least privilege,
access logging/review, encryption, masked default view, controlled support tooling, approved
retention, and explicit export authorization. Critical security evidence has independent always-record
routing; trace sampling never determines authoritative evidence existence. Export failure is detected
and alerted. Development/staging never copies production personal data.

## 11. Schema evolution and configuration

### 11.1 Expand–migrate–cutover–observe–contract

Production schema change first expands with structures compatible with old, new, and mixed-version
rolling instances: tables, columns, indexes, constraints, nullable/safe defaults, NOT VALID
constraints, and compatibility adapters. Avoid unrehearsed large table rewrites, blocking locks, and
volatile defaults.

Backfills are resumable operational jobs, not opaque application-start migrations. They are
idempotent, restartable, batch/transaction-bounded, observable, concurrent-write-safe, and
reconciled. They retain source version/fingerprint, target mapping, progress boundary, attempts,
unresolved review items, and live delta capture until authority converges.

Cut over only after backfill/final-delta completion, validated constraints/indexes, deployed new
paths, smoke/authorization proof, and committed cutover/authority-epoch evidence. New reads/writes
then use new authority and old writes reject. Rollback deploys compatible code against new schema;
legacy authority is never silently re-enabled.

Contract removal waits through a documented observation window: no legacy writes/divergence or
old-version dependency; no core-workflow failure or critical unresolved item; acceptable query
performance; tested backup/restore; and completed queue/external reconciliation. A later separately
approved release may remove legacy schema/routes/adapters/workers/flags only after compatibility,
retention, conversion/disposition, restore rehearsal, lock/disk/failure analysis, and forward recovery
evidence. Routine startup never performs destructive schema/data removal.

Schema migration uses a dedicated restricted identity, one runner, immutable applied files, and
forward-only fixes. DDL has explicit statement/lock timeout; long indexes use nonblocking PostgreSQL
techniques where available; slow data conversion remains outside transactional DDL; readiness requires
compatible schema. Deployment records schema version, application compatibility range, config revision,
and authority epochs. Emergency destructive migration is reserved for active security/legal/data-
integrity incident with explicit incident authorization, safe preservation/backup evidence, blast
radius, restricted credentials, Audit, immediate reconciliation, and forward repair plan.

### 11.2 Typed configuration and external secrets

Runtime non-secret configuration is typed, schema-versioned, and checksum-identified. It may hold
secret reference but never secret value. Secrets include database/Redis credentials, payment/email/
webhook keys, HMAC/cursor/signing/encryption keys, CDN/storage credentials, TLS private keys, and
telemetry exporter credentials. Resolve them through approved secret manager, workload identity, or
secure injection—not Git, image, normal manifest, configuration API, logs, crash reports, or support
tooling. Development, staging, Tap test, and production secrets are separated.

Startup parses manifest version, rejects unknown production fields/types, validates ranges and
cross-field rules, resolves required secret references, verifies provider/environment consistency,
builds immutable runtime configuration, then becomes ready. It never silently chooses permissive
default. Invalid examples include wildcard credentialed CORS, HTTP production origin, idle expiry
above absolute expiry, live Tap without approved LG-010 adapter, webhook without authenticity
material, payout without security acceptance, missing Audit/outbox, invalid currency/provider
environment, or unbounded request limit.

Fail closed at smallest safe scope:

| Condition | Outcome |
|---|---|
| database credential absent | `STARTUP_BLOCKED` |
| session/CSRF/Audit/origin/authorization configuration invalid | affected API service does not become ready |
| Tap secret absent | payment initiation and webhook disabled; unrelated learning may continue |
| email credentials absent | durable delivery admission follows its approved outage/backlog policy; Catalog may continue |
| CDN signing key absent | new protected grants reject; unrelated API may continue |
| retention policy absent | destructive retention jobs disabled |

Ordinary product flags may control optional features; operational rollout flags may control reviewed
safe paths. Security/financial gates, such as live Tap, payout transfer, retention deletion, or
high-risk Admin actions, need secure default, restricted owner, documented approval, and Audit. No
flag weakens authentication, Account status, authorization, CSRF/origin, TLS, webhook verification,
idempotency/deduplication, Audit co-commit, financial reconciliation, legal hold, Entitlement
provenance, Media scanning/readiness, session generation/epoch, privacy, or anti-enumeration.

Every deployment records manifest schema/config revision/checksum, app version, environment,
deployment ID/time, secret-reference versions, and authority epochs; diagnostics expose safe
identifiers only. Low-risk settings dynamically reload only when designed. Origin/proxy, session/
CSRF, provider environment, verification procedure, signing/encryption, payout/retention gate,
database connectivity, and authorization settings require controlled deployment or secure explicit
reload. Dynamic updates are schema-validated, versioned, atomic, observable, reversible, and wholly
rejected when invalid. Configuration never stores business authority such as price, Coupon, role/
status, revenue share, Course lifecycle, payout, moderation, or legal hold.

## 12. Capability and critical-flow matrix

| Surface | Session/CSRF | Primary decision | Idempotency | Evidence |
|---|---|---|---|---|
| Public Identity admission | anonymous bootstrap and exact Origin/CSRF where browser initiated | input/abuse; never Account disclosure | privacy-safe scope where required | security events; registration Identity evidence only when real |
| Bootstrap Admin deployment | no browser or public endpoint | dedicated deployment principal + singleton/no-Admin guard | stable operation ID and fingerprint | Identity security evidence + Audit co-commit |
| Student/Instructor/Admin mutation | required | module policy + transaction recheck | documented sensitive command | privileged/consequential Audit co-commit |
| Personalized GET | session; no mutation CSRF | scoped query/module policy | no | telemetry/security only |
| Playback session | authenticated mutation + CSRF | exact version, Entitlement/history, state | documented grant policy | protected policy evidence |
| Manifest | authenticated safe GET | revalidate session/grant/version/access/suspension/readiness | no | telemetry/security only |
| CDN byte delivery | provider capability | exact short-lived protected path | provider semantics | restricted delivery logs |
| Tap webhook | no browser/CSRF | authenticity then semantic reconciliation | provider observation dedupe | immutable observation + outbox |
| Worker/job | typed trusted execution principal | durable limited intent + epoch/state guard | event/operation/receipt | Audit when consequential |
| Schema/config operation | no public API | restricted deployment identity | migration/config version | cutover/config Audit |

```text
Rotation/reuse
browser → API: authenticated security boundary
API transaction: lock current generation → replacement digest + CSRF → supersede old → commit
old credential → API: reject 401 SESSION_REPLACED; record stale presentation
suspicious reuse → API transaction: revoke affected family; later mutation epoch/generation check fails
```

```text
Immediate suspension
Admin transaction: policy + recent auth → account status/epoch change → Audit + outbox → commit
admitted mutation: recheck account/session generation/epoch immediately before commit → reject
future request: session/status check → deny
```

```text
Protected playback
browser → API: create session (cookie, Origin, CSRF, exact policy)
browser → API: manifest; revalidate; rewrite every protected HLS reference
browser → CDN: short-lived exact-path capability → private-origin media bytes
revocation: new grant/renewal blocked; already issued capability expires shortly
```

```text
Verified payment
Tap → ingress: bounded bytes → approved authenticity
PostgreSQL transaction: observation/dedupe + link/reconciliation + outbox → commit → minimal ack
worker: reconcile provider/reference/amount/state → domain transition + Audit + outbox
duplicate/reordered callback: no duplicate Entitlement or financial effect
```

## 13. Required implementation verification

| Area | Minimum proof |
|---|---|
| HTTP/API | negotiation; UTF-8/duplicate/unknown JSON; problem status; localization; fixed browser-session challenge; headers/cache; direct/collection shape |
| Idempotency/cursor | same/different fingerprint; in-flight/stale/reconcile; replay headers/request ID; cursor tamper/expiry/context/order/filter |
| Session/CSRF | host-only cookie; no JS credentials; rotation boundaries; concurrent stale classification; logout/epoch/suspension commit-time race; Tap return after Strict-cookie omission; CSRF/rate decision before idempotency/full authorization |
| Bootstrap Admin | deployment-only singleton; no Admin pre-exists; secret-manager injection; no plaintext persistence/telemetry; password-change-only session; retry and concurrent execution |
| Privacy | hidden-account response status/body/header/cookie/timing/size/delivery equivalence |
| Authorization | deny map; scoped query; concealment; ownership/access/state/recent-auth/epoch rechecks |
| Rate control | quota 429 vs unavailable 503; outage modes; fallback bounds; private keys; atomic spend; webhook durable admission |
| Media | exact version; every HLS URI; no byte proxy; expiry/renewal/revocation; Range/HEAD; capability log/referrer protection; scanning clean/reject/timeout/duplicate/reorder/unknown/exhaustion |
| Providers | Tap approved vectors; invalid/duplicate/reordered/ambiguous callbacks; test/live isolation; semantic mismatch/reconciliation; DB retry |
| Outbox/email | source+intent atomicity; duplicate receipt; lease recovery; external unknown; accepted vs delivered; secret validity replacement; exhaustion/manual retry |
| Audit/telemetry | Audit write rollback; no update/delete grants; correction link; allowlist/redaction leakage; sampling independence; export-failure alert |
| Schema/config | mixed-version expansion; resumable concurrent backfill; authority cutover; destructive-contract evidence; manifest/secret validation; scoped fail closed; flag non-bypass |

## 14. Primary references

This document records Gradex decisions; implementation must consult the primary source and approved
adapter contract rather than treating this design as a substitute for a protocol specification.

- [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457.html), [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html), [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259.html), [RFC 4648](https://www.rfc-editor.org/rfc/rfc4648.html), and [RFC 6585](https://www.rfc-editor.org/rfc/rfc6585.html).
- [OWASP CSRF prevention](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html), [session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html), [authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html), [logging](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html), and [secrets management](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html).
- [NIST SP 800-63B-4](https://pages.nist.gov/800-63-4/sp800-63b.html) and [RFC-to-be 10017 final review](https://queue.rfc-editor.org/final-review/rfc10017/).
- [Apple HLS guidance](https://developer.apple.com/documentation/http-live-streaming/using-http-live-streaming), [Google Cloud signed URLs](https://cloud.google.com/storage/docs/access-control/signed-urls), and [AWS CloudFront private content](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/private-content-restricting-access-to-s3.html).
- [Tap webhook documentation](https://developers.tap.company/docs/webhook); LG-010 must create a versioned tested Gradex adapter before production enablement.
- [AWS transactional outbox guidance](https://docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/transactional-outbox.html), [Kafka delivery semantics](https://kafka.apache.org/documentation/#semantics), and [AWS SES send/delivery behavior](https://docs.aws.amazon.com/ses/latest/dg/send-email-concepts-process.html).
- [OpenTelemetry sensitive-data guidance](https://opentelemetry.io/docs/security/handling-sensitive-data/).
