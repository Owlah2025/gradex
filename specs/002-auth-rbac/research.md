# S1B1 Student Admission Research

**Date**: 2026-07-30
**Scope**: User Story 1 and FR-001–FR-004, FR-014–FR-016 only
**Primary inputs**: [feature spec](spec.md),
[S1B delivery design](../../docs/superpowers/specs/2026-07-30-s1b-delivery-design.md),
[API/security design](../../docs/superpowers/specs/2026-07-27-api-security-integration-design.md),
[domain/data design](../../docs/superpowers/specs/2026-07-26-domain-data-state-design.md)

## R1. Admission remains inside the existing modular monolith

**Decision**: Add S1B1 commands to `backend/internal/identity`, transport to
`backend/internal/httpapi`, disposable quota decisions to `backend/internal/ratelimit`, and
presentation to the existing Next.js application. PostgreSQL remains authoritative; Redis remains
disposable.

**Rationale**: This follows constitution VI and the approved architecture. Identity owns Accounts,
credentials, action secrets, and their state transitions; HTTP owns representation and middleware
order. A separate authentication service or provider-specific email implementation would add an
unapproved operational boundary.

**Alternatives considered**:

- A separate Identity service: rejected because no current isolation or scale requirement justifies
  it.
- SQL in Gin handlers: rejected because it would mix transport, privacy mapping, transactions, and
  domain rules.

## R2. Bootstrap retries bind every semantic input without a fast password digest

**Decision**: Migration `0005_student_admission` adds a fingerprint version and digest to
`bootstrap_operations`. The fingerprint uses a canonical, length-delimited encoding of the command
version, normalized and preserved email, normalized display name, and deployment principal. On an
operation-ID retry, compare that digest in constant time and verify the supplied password against
the recorded Argon2id credential. Any changed semantic input fails closed. Bootstrap writes the
trimmed supplied email to `accounts.email` and the normalized comparison form to
`accounts.normalized_email`.

**Rationale**: Current code treats any repeated operation ID as identical and writes normalized
email into both Account columns. Binding non-secret fields plus Argon2id verification closes the
retry gap without persisting a fast offline-guessable password fingerprint. It also preserves the
correspondence address the schema already intended to retain.

**Alternatives considered**:

- Include plaintext or an unkeyed password digest in the fingerprint: rejected as credential
  exposure and an offline-guessing aid.
- Exclude the password from retry equivalence: rejected because the same operation ID with a changed
  password would still be semantically different.
- Store only normalized email: rejected because delivery/display must preserve the supplied address.

## R3. Password screening is required and privacy-preserving

**Decision**: Replace the optional plaintext `CompromisedChecker` seam with a required
`CompromisedRangeSource`-style dependency. `prepareCredential` remains the only plaintext exposure
boundary: it derives a versioned cryptographic lookup representation, sends only a bounded prefix to
the adapter, and compares returned candidates locally. A deterministic local source supports tests;
an unconfigured or unavailable source returns an infrastructure error and fails credential creation
closed.

**Rationale**: BR-002 requires known-compromised rejection. The provider-neutral boundary keeps
plaintext and the complete derived digest away from adapters while allowing deterministic fixtures.
On 2026-08-09, D-075 selected HIBP Pwned Passwords Range API for production using SHA-1 prefix-5
queries and local suffix comparison.

**Alternatives considered**:

- Keep `IsCompromised(password string)`: rejected because plaintext crosses the boundary.
- Allow `nil` to skip screening: rejected because public credential creation would silently weaken
  policy.
- Select a vendor during S1B1: deferred then because source, license, test vectors, and privacy
  approval were open. D-075 later selected HIBP for production.

## R4. Current policy acceptance is an explicit fail-closed dependency

**Decision**: Add an immutable `policy_acceptances` table and a public safe-method current-policy
descriptor. Registration submits the opaque policy-set ID it displayed; Identity resolves that set
to exact required policy kind/version/link records and writes one acceptance per required policy in
the Account transaction. A stale/missing client acceptance is `422`; an absent or unapproved server
policy set makes registration unavailable.

**Rationale**: Registration requires policy acceptance, and the domain design explicitly owns
`policy_acceptances`. Encoding it only in generic event metadata would contradict that model.
LG-011 means test/development may use explicit fixtures, but production public registration cannot
activate until approved bilingual policies and versions exist.

**Alternatives considered**:

- A boolean `accepted_terms`: rejected because it cannot prove which version or language was shown.
- Frontend-only version constants: rejected because the backend could accept a stale or invented
  policy.
- Generic security-event metadata only: rejected because it loses the canonical entity and queryable
  immutable evidence.

## R5. Public browser commands use anonymous CSRF bootstrap

**Decision**: Implement the already-frozen `GET /api/v1/session/bootstrap` safe-method exception.
It creates or reuses an anonymous, host-only, `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`
cookie and returns a CSRF token with `Cache-Control: no-store`. The browser keeps the CSRF token in
memory only. Browser-initiated admission POSTs require exact Origin/controlled Referer and that
session-bound token before rate limiting or domain work.

**Rationale**: The API/security capability matrix requires anonymous bootstrap and exact
Origin/CSRF for public Identity admission. The anonymous identifier also supplies a privacy-safe,
bounded limiter dimension. It creates no Account, credential, delivery, or authenticated family and
does not pull S1B2 login/session behavior into scope.

**Alternatives considered**:

- Origin check only: rejected because it weakens the approved browser-admission matrix.
- Build authenticated sessions now: rejected as S1B2 scope.
- Store the CSRF token in browser persistence: rejected because the frozen contract requires
  browser memory only.

## R6. Admission uses three command resources and one policy read

**Decision**: Freeze:

- `GET /api/v1/registration-policy-set`
- `POST /api/v1/student-registrations`
- `POST /api/v1/email-verification-requests`
- `POST /api/v1/email-verifications`

Registration and verification-request hidden outcomes use their canonical generic `202`
acknowledgments. Verification success is `200 {"status":"VERIFIED"}` with no session; malformed,
unknown, wrong-purpose, expired, consumed, revoked, superseded, or conflicting secrets all return
`400 TOKEN_INVALID`. The bearer is sent in the request body.

**Rationale**: Noun resources match the API URI rules, and the shapes separate ordinary input
validation from hidden Account state. Verification replay must become invalid rather than replaying a
prior success, so the token command does not use generic HTTP idempotency replay.

**Alternatives considered**:

- Token in path or query: rejected because ordinary URLs reach history, referrers, logs, and
  analytics.
- Distinct expired/consumed/unknown errors: rejected as secret-state disclosure.
- Claim that a `202` means email was sent: rejected because it means only privacy-safe command
  acknowledgment and, where eligible, durable admission.

## R7. JSON decoding is strict before semantic validation

**Decision**: Extend the shared binder to reject duplicate JSON object members, unknown request
members, trailing documents, and structural ambiguity. Enforce bounded bodies and
`application/json`; structural failure is `400 MALFORMED_JSON`, while known-field semantic
violations are `422 VALIDATION_FAILED`.

**Rationale**: Gin's current binder distinguishes validator failures from other decoding errors but
does not yet prove the duplicate/unknown-member rules frozen by API design §2.1. Security-sensitive
commands need one unambiguous interpretation.

**Alternatives considered**:

- Accept unknown members: rejected because an ignored `role` or misspelled acceptance field creates
  unsafe client/server drift.
- Report decoder text: rejected because it may quote credentials or identifiers.

## R8. Registration is atomic and duplicate email is a true no-op

**Decision**: Perform bounded validation, password screening, and Argon2id hashing before opening the
transaction. Within one PostgreSQL transaction, create the pending immutable-role Student Account,
credential, policy acceptances, verification-secret digest, protected delivery payload, Identity
security evidence, and outbox event. A normalized-email uniqueness conflict becomes the same public
`202` but creates no credential, secret, evidence, resend, or outbox row. No session is issued.

**Rationale**: This is the exact BR-001/002/008/105 and API/privacy boundary. Expensive password work
outside the transaction reduces lock duration while uniform work classes resist Account-existence
timing disclosure.

**Alternatives considered**:

- Look up email before password work: rejected because it widens timing differences and invites a
  check-then-insert race.
- Treat registration of an existing email as resend: rejected because the approved contract says it
  is a no-op.
- Publish to Redis or a provider inside the transaction: rejected because neither is authoritative
  and external work cannot be atomic with PostgreSQL.

## R9. Action secrets are digest-only, purpose-bound, and lock-safe

**Decision**: Generate 32 random bytes, encode the bearer for transport, and store only its SHA-256
digest in `identity_action_secrets`. Constrain purpose, issue/expiry, consumption, supersession, and
attempt evidence; permit only one live secret per Account/purpose. Resend locks Account then current
secret, supersedes it, and creates its replacement plus outbox intent atomically. Consumption
resolves by digest, then locks Account followed by the exact secret and rechecks all state before one
pending-to-active transition.

**Rationale**: Stable Account→secret lock ordering prevents deadlocks. Row locks and a partial unique
index make concurrent resend/consumption safe; exactly one consumer activates and every later
consumer receives the same invalid-token result.

**Alternatives considered**:

- Reusable or overwrite-in-place token rows: rejected because they destroy issuance and
  supersession evidence.
- Application mutexes: rejected because they do not coordinate multiple API replicas.
- Plaintext bearer storage: rejected by FR-004 and the approved design.

## R10. Durable delivery uses a protected payload beside an immutable outbox event

**Decision**: The immutable `outbox_events` row contains only stable type/schema, source reference,
safe payload, timestamps, and correlation. A protected one-to-one payload stores key version, nonce,
and authenticated ciphertext containing the minimum delivery snapshot, including the one-time
bearer. Token tables, ordinary outbox JSON, logs, and evidence never contain a raw or reversible
bearer. S9 will be the only decrypting consumer and will add dispatch attempts/receipts without
changing source-event identity.

**Rationale**: A future asynchronous worker cannot reconstruct a bearer from its digest. The
approved API design explicitly allows the raw secret only in a protected delivery payload. Separating
protected ciphertext from ordinary JSON keeps operational tools and routine queries secret-safe.

**Alternatives considered**:

- Store the raw link in JSON: rejected because routine outbox/log tooling could expose it.
- Store only the digest: rejected because delivery would be impossible.
- Send synchronously before commit: rejected because provider behavior cannot be atomic with source
  state and would make provider failure roll back registration.

## R11. Layered rate limiting fails safely

**Decision**: Every endpoint uses a versioned policy and combines endpoint, HMAC-normalized
identifier or token, trusted client IP/network, anonymous client, and global dimensions. Redis makes
atomic distributed decisions. Its only outage mode for these commands is bounded
`STRICT_LOCAL_FALLBACK`; exhausted quota is `429 RATE_LIMITED`, while no safe decision is
`503 RATE_LIMITING_UNAVAILABLE`. Keys and telemetry contain no plaintext identifier or bearer.

**Rationale**: This is the approved API/security §6.2 and S1B1 abuse boundary. Returning `429` during
infrastructure failure would falsely claim a quota decision.

**Alternatives considered**:

- Allow on Redis failure: rejected because attackers could trigger or exploit the outage.
- Store rate state in PostgreSQL: rejected as unnecessary authoritative write load.
- Publish the limiting dimension: rejected because it can disclose Account or network state.

## R12. Frontend routes scrub the verification fragment and remain bilingual

**Decision**: Add `/register`, `/verify-email`, and `/verify-email/result` under an App Router auth
layout. Email links land at `/verify-email/result#token=...`; the client copies the token to memory,
immediately removes the fragment with `history.replaceState`, and POSTs the bearer in the body.
Admission strings live in paired typed dictionaries; Arabic is the initial locale, and document
language/direction are correct from first render. Forms use native labels/autocomplete, described
guidance, error summary/focus, live result announcements, logical-direction CSS, and no credential
persistence.

**Rationale**: The fragment is not sent to the server and can be scrubbed before API submission.
This preserves body-only token consumption while still supporting an email deep link. The existing
frontend has a bilingual design system but no auth/form/API layer; reusing it is smaller than a new
UI stack.

**Alternatives considered**:

- Query-string token: rejected because it reaches server/access logs and browser history.
- Session/local storage: rejected because action secrets and future session credentials cannot be
  persisted there.
- Inline English/Arabic copies: rejected because it bypasses the typed dictionary and RTL contract.

## R13. Verification is risk-proportional and hosted

**Decision**: Add unit tests for validators, fingerprints, token primitives, and password-screening
privacy; PostgreSQL integration tests for atomicity, no-op duplicates, expiry, supersession,
concurrency, and rollback; HTTP tests for strict JSON, CSRF/Origin order, uniform outcomes, and
rate-limit outage behavior; frontend lint/typecheck/build plus browser evidence for responsive
Arabic/English flows. Add the critical admission integration suite to hosted CI with PostgreSQL and
Redis. Retain the docs and plaintext-exposure guards.

**Rationale**: Current hosted CI runs migration integration but not Identity integration. S1B1 close
requires concurrency, rollback, privacy, and leak evidence on the frozen head, not only locally.

**Alternatives considered**:

- Unit tests alone: rejected because transaction, constraints, locks, HTTP equivalence, and Redis
  behavior are integration concerns.
- Add a broad frontend test framework solely for S1B1: deferred unless implementation evidence
  shows the manual browser check is insufficient; S1B3 owns the full automated Student journey.

## Open gates and bounded claims

- **LG-003 / LG-004**: do not activate deletion or invent privacy/retention conclusions.
- **LG-011**: production registration remains unavailable until approved bilingual policies and
  version identifiers exist.
- **LG-015**: test accessibility behavior but do not claim audited product-level WCAG conformance.
- **LG-018**: claim durable notification intent only, never provider acceptance or delivery.
- **LG-019**: use explicit development/test quota fixtures; make no production SLO, capacity, or
  numeric rate-limit claim.
- **LG-021**: resolved on 2026-08-09 by D-075 and the HIBP production adapter evidence. Production
  registration remains independently blocked by LG-011.

These are resolved planning boundaries, not unresolved implementation questions.
