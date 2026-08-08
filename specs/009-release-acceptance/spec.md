# Feature Specification: S11 — Release Acceptance

**Feature Branch**: `s11-release-e2e-20260808`

**Created**: 2026-08-08

**Status**: Implementation-ready

**Input**: End-to-end critical journey, authorization failures, recovery behavior, and release acceptance for the current no-commerce Gradex MVP.

## User Scenarios & Testing

### User Story 1 - Complete the launch-critical learning journey (Priority: P1)

A Student can create and verify an account, sign in, accept an Admin-created Course Access Invitation, remain unable to learn before approval, receive access only after Admin Approval, and then open protected learning content, retrieve media, and persist progress.

**Why this priority**: This is the sole MVP path from identity creation to usable Course access and is the central August 15 launch promise.

**Independent Test**: Run the release acceptance journey against an isolated environment and verify every transition from registration through persisted Progress without manual database mutation.

**Acceptance Scenarios**:

1. **Given** registration is enabled, **when** a Student registers, verifies the delivered email action, and signs in, **then** the authenticated Student identity is available for the access journey.
2. **Given** a future Course default access end and an authenticated Admin, **when** the Admin creates an Invitation and the intended Student accepts it, **then** the Student has zero Entitlements and zero Enrollments and protected Course access is denied.
3. **Given** an accepted Invitation, **when** an authorized recently authenticated Admin approves it, **then** exactly one active invitation-sourced Entitlement and one Enrollment exist for that Student and Course.
4. **Given** the resulting active Entitlement, **when** the Student opens the protected Course and Lesson, **then** playback authorization, signed media retrieval, and a Progress write succeed and the Progress is durably observable.

---

### User Story 2 - Deny unrelated and premature access (Priority: P1)

Students who have not received the authoritative grant cannot use Course, Lesson, playback, protected media, or Progress operations, and denials do not mutate access or learning records.

**Why this priority**: A false allow is a launch-blocking authorization defect even when the happy path works.

**Independent Test**: Exercise protected operations before approval and as an unrelated Student, then compare authoritative record counts and Progress state before and after each denial.

**Acceptance Scenarios**:

1. **Given** an Invitation that is pending Admin Approval, **when** the intended Student requests protected Course or Lesson access, **then** access is denied and no Entitlement, Enrollment, or Progress is created.
2. **Given** an unrelated authenticated Student, **when** that Student requests the Course, Lesson, playback, media, or Progress paths, **then** every operation is denied without revealing protected content or mutating records.
3. **Given** an anonymous requester, **when** protected media is requested directly, **then** the object remains unavailable.

---

### User Story 3 - Recover safely from retryable and invalid actions (Priority: P2)

Students and Admins can retry safe actions after an invalid secret or repeated request without duplicate access, stale partial state, or a permanently wedged journey.

**Why this priority**: Email links, browser requests, and Admin actions are routinely retried in real operation; safe recovery is required for a one-person launch team.

**Independent Test**: Submit an invalid action secret before the valid one, repeat approval through the authorized route, and verify the journey recovers while cardinalities and provenance remain unchanged.

**Acceptance Scenarios**:

1. **Given** a live verification or invitation action, **when** an invalid secret is refused and the valid secret is then submitted, **then** the valid action can still complete and no premature grant exists.
2. **Given** an already approved Invitation, **when** the same authorized approval is repeated, **then** the request returns the existing grant and total Entitlement and Enrollment counts remain one.
3. **Given** concurrent or replayed grant work, **when** the approved integration coverage runs, **then** database uniqueness and transactional behavior prevent duplicate active access.

---

### User Story 4 - Run one acceptance contract in disposable and public staging (Priority: P2)

The release owner can run the same acceptance suite against the existing disposable HTTPS deployment now and a future public T047 staging URL later by changing configuration only.

**Why this priority**: Acceptance evidence must describe the deployed topology and remain reusable when the externally blocked provider environment becomes available.

**Independent Test**: Run the suite first with the disposable HTTPS origin and then validate configuration parsing for a different credential-free HTTPS origin without changing test source.

**Acceptance Scenarios**:

1. **Given** an isolated safety-gated acceptance database and HTTPS origin, **when** the suite runs, **then** it uses the configured origin and database and does not start or attach to an unrelated local server.
2. **Given** a future public staging URL, **when** the origin and environment credentials are supplied, **then** the same suite selection runs without source changes.
3. **Given** an acceptance run, **when** evidence is retained, **then** it identifies the exact commit, origin class, schema version, commands, outcomes, and any unproven provider-only boundary without storing secrets.

### Edge Cases

- An invalid email-verification or invitation secret is refused without consuming the corresponding valid secret.
- An acceptance attempt by an Account whose normalized email does not match the Invitation is refused.
- An Invitation that is accepted but not approved never grants access.
- A repeat approval must prove an authorized `200` idempotent response; a `401`, `403`, or generic conflict is not acceptable replay evidence.
- An existing Enrollment is reused and Progress remains single-homed.
- A playback response that merely contains a non-empty URL is insufficient; the protected manifest and at least one non-empty signed media object must be retrieved.
- Registration may be disabled in a prelaunch environment, but positive registration acceptance is mandatory before public launch and the suite must fail closed when enabled behavior is expected.
- The suite must not reset, migrate down, or otherwise mutate the active application database.

## Requirements

### Functional Requirements

- **FR-001**: The release suite MUST prove Student registration, email verification, and password login through production routes and user-facing screens when registration is configured as enabled.
- **FR-002**: The suite MUST prove real HTTP login against the deployed origin even when a prelaunch environment intentionally disables new registration.
- **FR-003**: The suite MUST prove that only an Admin can configure the Course default access end and create a Course Access Invitation (BR-025, BR-165).
- **FR-004**: The suite MUST prove identity-bound Student acceptance and refusal of a mismatched or invalid acceptance secret (BR-166, BR-169).
- **FR-005**: The suite MUST observe exactly zero Entitlements and zero Enrollments after acceptance and before approval (BR-029).
- **FR-006**: The suite MUST prove protected Course and Lesson denial before approval without side effects (BR-023, BR-029).
- **FR-007**: The suite MUST prove Admin Approval creates exactly one `ACTIVE` Entitlement with `grant_source = MANUAL_INVITATION`, the approving Invitation as immutable provenance, and exactly one Enrollment (BR-024, BR-028, BR-167).
- **FR-008**: The suite MUST prove an authorized repeated approval returns the existing grant and leaves both cardinalities at one; refusal caused by invalid authentication or CSRF is not evidence of idempotency (BR-167).
- **FR-009**: Existing concurrent approval integration coverage MUST be included in the release command (BR-167; Constitution Principle V).
- **FR-010**: The suite MUST open the protected Course and Lesson through the deployed application after approval (BR-021, BR-024).
- **FR-011**: The suite MUST request playback authorization, retrieve the protected manifest, and retrieve at least one non-empty signed media object (BR-023).
- **FR-012**: The suite MUST persist Progress and verify the authoritative row records at least the submitted position for the stable Lesson identity (BR-116).
- **FR-013**: The suite MUST prove an unrelated Student cannot access the protected Course, Lesson, playback, media, or Progress paths and cannot mutate the intended Student's records (BR-023, BR-116).
- **FR-014**: Existing S4 protected-delivery and S5 protected-learning denial, lifecycle, and side-effect coverage MUST be selected or cited rather than duplicated.
- **FR-015**: Invalid action-secret coverage MUST prove a later valid retry succeeds and no partial access state was created.
- **FR-016**: The suite MUST run against a credential-free HTTPS origin supplied by configuration and reject origins containing credentials, paths, queries, fragments, or non-HTTPS schemes.
- **FR-017**: External runs MUST use an explicitly safety-gated isolated acceptance database and MUST NOT reset or downgrade the active application database.
- **FR-018**: The suite MUST retain redacted evidence containing exact revision, environment class, schema version, command results, and coverage mapping.
- **FR-019**: The suite MUST leave the existing S12 default production-like behavior unchanged when S11 mode is not selected.
- **FR-020**: The suite MUST introduce no payment, checkout, KNET, Apple Pay, coupon, refund, invoice, BNPL, payout, S8 support, Entitlement-update, provider-deployment, or other product behavior.
- **FR-021**: The suite MUST use schema version 15 and MUST introduce no migration.
- **FR-022**: All launch-blocking defects and all Critical, High, Medium, and Low findings discovered during acceptance MUST be recorded honestly; Critical or unresolved High findings prevent S11 closure.

### Key Entities

- **Acceptance Run**: Exact revision, environment class, origin, isolated database, schema version, selected checks, results, and retained evidence paths for one run.
- **Student Identity**: The verified Student Account and authenticated session used in the launch-critical journey.
- **Course Access Invitation**: The identity-bound workflow record whose acceptance grants nothing and whose Admin Approval is the sole MVP grant trigger.
- **Entitlement**: The authoritative Course access record, including active state, expiry snapshot, typed grant source, and approving-Invitation provenance.
- **Enrollment**: The unique Student-and-Course learning home reused by replayed approval and referenced by Progress.
- **Progress**: The durable Student-and-stable-Lesson learning state whose persisted position proves the playback journey reached storage.

## Success Criteria

### Measurable Outcomes

- **SC-001**: One automated run completes the enabled-registration-to-persisted-progress journey with no manual data edits between steps.
- **SC-002**: Before approval, observed Entitlement and Enrollment counts are both exactly zero; after approval and after replay, both counts are exactly one.
- **SC-003**: The single resulting Entitlement is active and carries both the invitation grant type and the exact approving Invitation provenance.
- **SC-004**: The intended Student retrieves protected Course, Lesson, manifest, and a non-empty signed media object, while the unrelated Student succeeds at none of those protected operations.
- **SC-005**: The persisted Progress row reaches at least the position submitted by the acceptance journey and remains attached to the unique Enrollment.
- **SC-006**: Invalid-secret recovery and authorized repeated approval both complete without partial or duplicate access state.
- **SC-007**: The release suite runs against the disposable HTTPS environment and accepts a future public staging origin solely through configuration.
- **SC-008**: All selected browser, integration, type, lint, build, schema, and safety checks pass at one clean exact HEAD with zero unresolved Critical or High finding.
- **SC-009**: The acceptance package adds zero schema migrations and zero commerce, S8, Entitlement-update, or provider-deployment behavior.

## Assumptions

- S1 identity, S4 protected delivery, S5 protected learning, S6 access granting, and S12 disposable staging behavior at the starting revision are inputs; S11 composes and strengthens their acceptance evidence rather than reopening their product scopes.
- The disposable environment may enable registration only for its isolated S11 acceptance database; its default S12 prelaunch policy remains unchanged.
- T047 public infrastructure remains externally blocked and is not required to complete the local production-like S11 run.
- Existing seeded Admin and unrelated Student fixtures remain test-only identities and are never production grant paths.
- S8 and Entitlement correction/update behavior remain explicitly out of scope because migration 0015 provenance reconciliation is unresolved.
