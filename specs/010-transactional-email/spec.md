# Feature Specification: S9 — Transactional Email Delivery

**Feature Branch**: `s9-transactional-email-20260809`

**Created**: 2026-08-09

**Status**: Approved for implementation by the Product Owner

**Input**: Deliver the launch-required no-commerce transactional messages through Resend behind a provider-neutral boundary, using the existing durable domain intents and credentials.

## Scope Reconciliation

S9 delivers existing fixed transactional intents. It does not create new account, password, invitation, Enrollment, Entitlement, commerce, marketing, office-hours, or lifecycle-notification products. PostgreSQL domain state and existing single-use credentials remain authoritative; email is delivery only.

The launch-required contracts are the six already emitted by the repository plus two Course invitation terminal notices required by BR-122 and the closed S6 FR-032 but missing from the current producers:

| Intent | Producer | Durable contract | Recipient/locale | Credential/link | Template |
|---|---|---|---|---|---|
| Verify Student email | Student registration or verification resend | `identity.email_verification_requested` | Account email and locale | Existing `EMAIL_VERIFICATION` secret; 24-hour configured expiry | `student-email-verification-v1` |
| Reset password | Password-reset request | `identity.password_reset_requested` | Account email and locale | Existing `PASSWORD_RESET` secret; one-hour configured expiry | `account-password-reset-v1` |
| Confirm password reset | Successful reset transaction | `identity.password_reset_completed` | Account email and locale | No credential | `account-password-reset-completed-v1` |
| Invite staff Account | Admin staff invitation | `identity.staff_invitation_created` | Invitation email and requested locale | Existing `STAFF_INVITATION` secret and invitation expiry | `staff-invitation-v1` |
| Invite Student to Course access workflow | Admin Course Access Invitation | `access.invitation_issued` | Existing Account locale when present, otherwise request locale | Existing `COURSE_ACCESS_INVITATION` secret and invitation expiry | `course-access-invitation-v1` |
| Confirm Course access grant | Admin Approval transaction | `access.granted` | Student Account email and locale | No credential; Entitlement already exists | `course-access-granted-v1` |
| Notify Course invitation rejection | Admin rejection transaction | `access.invitation_rejected` | Student Account email and locale | No credential; rejection is already terminal | `course-access-invitation-rejected-v1` |
| Notify Course invitation cancellation | Admin cancellation after invitation issue | `access.invitation_cancelled` | Account locale when present, otherwise invitation locale | No credential; cancellation is already terminal | `course-access-invitation-cancelled-v1` |

Course review, Entitlement adjustment, emergency suspension, office hours, and other BR-122 messages are not added here because the Product Owner scoped S9 to the current account/access path and no current launch S9 email producer contract exists for them. Their domain workflows are not silently changed in this slice.

## User Scenarios & Testing

### User Story 1 - Complete account recovery from email (Priority: P1)

A Student can register, receive a verification message, verify the same existing credential, request a password reset, and complete recovery without database or test-helper access.

**Why this priority**: Verification and recovery are mandatory normal-user account paths and launch blockers.

**Independent Test**: Use deterministic delivery against the real dispatcher boundary and prove valid, wrong-purpose, replayed, and expired credentials preserve existing identity semantics.

**Acceptance Scenarios**:

1. **Given** a new Student registration, **when** the worker claims its durable intent, **then** Arabic or English plain-text and HTML verification content contains the canonical public fragment link and no operational log contains the credential.
2. **Given** the delivered valid link, **when** the Student opens it, **then** existing verification succeeds exactly once and replay, wrong-purpose, expired, or superseded credentials are refused.
3. **Given** a password-reset request, **when** dispatch succeeds and the Student follows the link, **then** the existing credential changes the password once, revokes existing sessions, and emits the reset-completed security notice.

---

### User Story 2 - Complete invitation workflows from email (Priority: P1)

Invited staff and Students can open usable bilingual links. Course invitation acceptance still grants zero Course access until an authorized Admin approves it.

**Why this priority**: Staff provisioning and Course access are launch-critical and currently require manual secret extraction.

**Independent Test**: Drive both invitation producers through delivery, consume the exact delivered links, and assert the closed S6 grant invariants.

**Acceptance Scenarios**:

1. **Given** an Admin staff invitation, **when** its email link is opened, **then** the invitee can preview the role and complete the existing invitation with a compliant password.
2. **Given** a Course Access Invitation, **when** the intended Student accepts the delivered link, **then** the invitation becomes pending Admin approval and there are zero Entitlements and zero Enrollments.
3. **Given** that accepted invitation, **when** an authorized recently authenticated Admin approves it, **then** exactly one provenance-bearing Entitlement and exactly one Enrollment exist and a grant-confirmation intent is dispatched.

---

### User Story 3 - Operate delivery safely through provider failures (Priority: P1)

An operator can determine why a message did not arrive without seeing its address body, action credential, or API key, while transient failures retry and permanent failures stop.

**Why this priority**: A launch-critical external dependency must fail independently of domain transactions and provide truthful evidence.

**Independent Test**: Exercise accepted, timeout, transport, rate-limit, malformed-response, invalid-recipient/configuration, retry exhaustion, and worker restart cases against deterministic HTTPS endpoints.

**Acceptance Scenarios**:

1. **Given** Resend is unavailable, **when** an intent is processed, **then** the domain action remains committed, the attempt is durably classified, and bounded retry is scheduled only for a transient failure.
2. **Given** an ambiguous retry for the same durable message, **when** Resend receives it again within its supported window, **then** the stable provider idempotency key is reused.
3. **Given** a permanent provider or recipient failure, **when** it is classified, **then** no unbounded retry occurs and safe operational evidence records the reason class.

### Edge Cases

- A worker crash after provider acceptance but before the database update retries the same immutable message identity and stable provider idempotency key.
- A protected outbox payload that cannot authenticate, decrypt, or match its template contract is marked as a permanent internal/configuration failure without exposing ciphertext or plaintext.
- An action credential may expire while queued; the email truthfully displays its original expiry and existing domain validation still decides whether it is usable.
- A locale outside `ar` or `en`, an unknown event/template contract, a missing public origin, fake mode in production, or a non-HTTPS production origin fails closed.
- Resend success means provider acceptance, not inbox placement or exactly-once delivery.

## Requirements

### Functional Requirements

- **FR-001**: S9 MUST consume only the eight fixed durable contracts listed above and MUST NOT create a second source of truth for any credential or domain state. The two missing Course terminal producers MUST co-commit notification intents without changing invitation transitions. *(BR-001, BR-008, BR-009, BR-029, BR-120, BR-121, BR-122, BR-167, BR-168, BR-171)*
- **FR-002**: Domain/application producers MUST depend only on a narrow provider-neutral transactional delivery contract; Resend types, IDs, HTTP responses, and errors MUST remain in infrastructure and attempt evidence. *(Constitution VI, IX)*
- **FR-003**: Each durable message MUST have one PostgreSQL-authoritative delivery record with durable attempts, status, next-attempt time, safe failure class, and optional provider acceptance ID. *(BR-120)*
- **FR-004**: Dispatch MUST claim work safely across concurrent workers and worker restarts without making Redis authoritative. *(Constitution VII, IX)*
- **FR-005**: Development/test MUST support deterministic fake delivery; production MUST require the approved Resend mode, API key, sender, public HTTPS origin, and bounded timeout and MUST reject fake/disabled delivery. *(LG-018)*
- **FR-006**: The Resend adapter MUST use HTTPS, one bounded request, bearer authentication, the official send-email payload, and a stable `Idempotency-Key` derived from immutable message identity. *(LG-018)*
- **FR-007**: Provider acceptance MUST capture its provider ID; secrets, bodies, recipient addresses, and action links MUST NOT appear in logs or operator evidence. *(Constitution IX)*
- **FR-008**: Transient network, timeout, rate-limit, concurrent-idempotency, and provider 5xx failures MUST use bounded asynchronous retry; validation, authentication, sender/domain, invalid-recipient, payload-conflict, and malformed-success failures MUST stop. *(BR-120)*
- **FR-009**: Retry MUST be bounded to five total attempts with persisted exponential delays of 30 seconds, 2 minutes, 10 minutes, and 30 minutes; a valid `Retry-After` may lengthen but not shorten a delay. *(BR-120)*
- **FR-010**: All eight listed templates MUST provide Arabic and English subjects, plain text, and mobile-friendly HTML; Arabic HTML MUST declare RTL and templates MUST contain no marketing or tracking pixels. *(Constitution X, D-010)*
- **FR-011**: Action links MUST use `PUBLIC_ORIGIN`, existing frontend destinations, and URL fragments for credentials; production templates MUST never use localhost. *(BR-008, Constitution IX)*
- **FR-012**: Course invitation locale MUST use the target Account locale when the Account exists and otherwise the validated request locale, defaulting to Arabic; access-granted confirmation MUST use the Student Account locale. *(Constitution X)*
- **FR-013**: Staff invitation email MUST lead to a bilingual frontend screen that previews and completes the existing invitation contract without exposing the credential in the query string or DOM. *(BR-009)*
- **FR-014**: Course invitation copy MUST state that acceptance grants no Course access and that Admin Approval is required. *(BR-029, BR-167)*
- **FR-015**: Observability MUST distinguish queued, attempt-started, provider-accepted, transient failure, permanent failure, retry scheduled, and exhausted using only safe message/event identifiers and classifications. *(Constitution IX)*
- **FR-016**: Automated acceptance MUST reuse S11/S6 journeys and prove exact existing verification, reset, invitation, Entitlement, Enrollment, and provenance behavior without reopening S11. *(Constitution V)*
- **FR-017**: Provider-domain verification and public sender proof MAY remain external only when real domain/DNS or safe credentials are unavailable; repository implementation and deterministic HTTPS compatibility proof MUST still complete. *(LG-018, T047)*

### Key Entities

- **Transactional Delivery**: One durable, provider-neutral email message derived one-to-one from an immutable outbox event.
- **Delivery Attempt**: One claimed send attempt with safe timing, classification, retry decision, and optional provider acceptance ID.
- **Rendered Message**: Ephemeral subject, plain text, and HTML derived from a known contract, locale, and canonical public link; never persisted or logged.
- **Provider Result**: Provider-neutral acceptance or typed transient/permanent failure returned by an adapter.

## Success Criteria

### Measurable Outcomes

- **SC-001**: All eight launch-required contracts render and dispatch in both Arabic and English, for 16 deterministic content cases with text and HTML.
- **SC-002**: Verification, password reset, staff invitation, and Course invitation can each be completed from the captured delivered message with zero database/test-helper token extraction.
- **SC-003**: Course invitation acceptance produces zero Entitlements and zero Enrollments before approval; repeated approval converges on exactly one of each with `MANUAL_INVITATION` provenance.
- **SC-004**: Every tested transient failure is retried within the bounded schedule and every tested permanent failure receives zero scheduled retries.
- **SC-005**: Concurrent claims produce at most one active provider call per delivery, and crash/restart retry reuses the same provider idempotency key.
- **SC-006**: Automated log/evidence scans find zero API keys, raw credentials, secret-bearing links, message bodies, or plaintext passwords.
- **SC-007**: Production startup rejects fake/disabled delivery and every missing or malformed required setting before the worker becomes ready.
- **SC-008**: The full required backend validation and every touched frontend validation command pass at the final S9 head.

## Assumptions

- Resend's `POST /emails` contract and 24-hour idempotency retention are the current official provider behavior as of 2026-08-09.
- Provider acceptance is the strongest synchronous claim; bounce/delivery webhooks and marketing suppression are post-launch unless a later explicit decision adds them.
- The real sender domain and DNS may remain unavailable until T047; no repository evidence will claim they were verified.
- Existing expiry durations and wrong-purpose/replay semantics are unchanged.
