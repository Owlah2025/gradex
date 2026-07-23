# Feature Specification: Authentication and Role-Based Access Control

**Feature Branch**: `002-auth-rbac`

**Created**: 2026-07-22

**Reconciled**: 2026-07-23

**Status**: Ready for planning

**Input**: Student registration and verification; Admin-invited staff; email/password
authentication; revocable rotating sessions; password recovery; role, ownership, status, and
suspension enforcement.

## User Scenarios and Testing

### User Story 1 — Student creates and verifies an account (Priority: P1)

A visitor creates a Student account, verifies ownership of the email address, and then signs in.
Public catalog browsing does not require an account.

**Why this priority**: Purchase and protected learning require an authenticated Student identity.

**Independent test**: Register a new address and valid display name, prove that login is blocked
before verification, consume the verification link, and then sign in with the display name stored
as the Account's editable, non-unique profile label.

**Acceptance scenarios**:

1. **Given** a visitor submits a valid display name, normalized email, valid password, and required
   policy acceptance, **when** registration is accepted, **then** Gradex creates a
   `PENDING_VERIFICATION` Student with that display name, stores only an Argon2id password hash,
   sends an expiring single-use verification link, and issues no session.
   *(BR-001, BR-002, BR-008, BR-105)*
2. **Given** an address is already registered, **when** registration/recovery/verification is
   requested, **then** the public response does not reveal whether an Account exists. *(BR-001,
   BR-003)*
3. **Given** a pending Student supplies correct credentials before verification, **when** login is
   attempted, **then** no authenticated session is created and the safe response directs the user
   to verification without leaking additional Account state. *(BR-003, BR-008)*
4. **Given** a valid unused verification link, **when** it is consumed, **then** the Student becomes
   `ACTIVE`; a repeated or expired link cannot activate the Account again. *(BR-008)*
5. **Given** a public registrant, **when** they submit registration, **then** they cannot choose or
   acquire Instructor/Admin role. *(BR-001, BR-009)*

### User Story 2 — Staff accepts an Admin invitation (Priority: P1)

An existing Admin invites an Instructor or additional Admin. The recipient verifies the invited
address by consuming the invitation and establishes an initial password for the assigned role.

**Why this priority**: The MVP needs controlled Course supply and platform administration without
public privileged-role creation.

**Independent test**: Send each supported role invitation, accept it once, sign in with the assigned
role, and prove public registration cannot produce that role.

**Acceptance scenarios**:

1. **Given** an authorized Admin sends an invitation for Instructor/Admin role, **when** the
   recipient consumes a valid unused invitation and sets a valid display name and password,
   **then** Gradex creates the Account with that display name, exactly the assigned
   role, and verified email. *(BR-002, BR-009, BR-105)*
2. **Given** an invitation is expired, revoked, or already used, **when** it is submitted, **then**
   Gradex creates no privileged access and returns a safe actionable status. *(BR-009)*
3. **Given** a non-Admin or an unapproved role value, **when** invitation creation is attempted,
   **then** the backend denies it. *(BR-080)*
4. **Given** the invited email already belongs to any Account, **when** invitation creation or
   acceptance is attempted, **then** Gradex rejects it without changing role or merging identity.
   *(BR-009)*
5. **Given** the platform has no Admin, **when** the secure deployment bootstrap runs once, **then**
   it creates the bootstrap Admin without repository-stored credentials and requires an initial
   password change; it cannot be used as a public endpoint or repeated to mint Admins. *(BR-009)*

### User Story 3 — User signs in, refreshes, recovers, and signs out (Priority: P1)

An active Student, Instructor, or Admin signs in, remains authenticated through token rotation,
recovers a forgotten password safely, and can end the current session.

**Independent test**: Sign in, refresh through access-token expiry, reject reuse of the rotated
refresh token, log out, and confirm that the session cannot refresh again; separately complete a
single-use password reset.

**Acceptance scenarios**:

1. **Given** an Active user submits correct credentials, **when** login succeeds, **then** Gradex
   issues a short-lived access token and rotating refresh token tied to an independently revocable
   session. *(BR-004)*
2. **Given** an invalid email/password combination, **when** login is attempted, **then** Gradex
   returns the same generic failure without confirming Account existence. *(BR-003)*
3. **Given** a valid unrevoked refresh token, **when** refresh succeeds, **then** Gradex issues a new
   access token and rotates the refresh token. *(BR-004, BR-005)*
4. **Given** an expired, revoked, or previously used refresh token, **when** refresh is attempted,
   **then** no token is issued. *(BR-005)*
5. **Given** a user logs out, **when** logout completes, **then** the current refresh session is
   invalidated; treatment of the already-issued access token follows the system design while still
   satisfying immediate suspension separately. *(BR-006, BR-007)*
6. **Given** any password-reset request, **when** it is submitted, **then** the response is
   non-enumerating; a valid expiring single-use reset link accepts only a password satisfying the
   approved policy and cannot be reused. *(BR-001, BR-002, PRD §5 Authentication)*

### User Story 4 — Backend enforces role and ownership boundaries (Priority: P1)

Every protected request is authorized by the backend using Account role, status, resource ownership,
and the requested action. UI visibility is only a convenience.

**Independent test**: Call protected capabilities directly as every role and as a different owning
Instructor, bypassing the frontend.

**Acceptance scenarios**:

1. **Given** a caller lacks the required role, **when** they call a restricted capability directly,
   **then** the backend denies it regardless of the client UI. *(BR-080)*
2. **Given** Instructor A, **when** they act on Instructor B's Course, **then** the backend denies the
   request without exposing unauthorized Course data. *(BR-060)*
3. **Given** an Instructor, **when** they attempt direct Course publication or a Course/Section price
   mutation, **then** the backend denies it; those are Admin actions. *(BR-019, BR-061)*
4. **Given** a Student/Instructor, **when** they request another Student's direct account/contact/
   payment PII, **then** the backend denies it; an Instructor can receive only BR-064's minimal
   Course-scoped roster fields for an owned Course. *(BR-064, BR-101)*

### User Story 5 — Suspension takes effect immediately (Priority: P1)

An Admin suspends an Account and every existing or new session immediately loses protected access,
regardless of previous purchases or ownership.

**Independent test**: Suspend an Account while it has a valid access/refresh session, then prove its
next protected request, refresh, fresh login, playback, and download are denied.

**Acceptance scenarios**:

1. **Given** an Active Account with existing sessions, **when** an authorized Admin suspends it,
   **then** every subsequent protected action is denied immediately; no access-token TTL grace
   period is allowed. *(BR-007)*
2. **Given** a suspended Student with an active Entitlement, **when** playback or protected download
   is requested, **then** access is denied without deleting the underlying purchase/history. *(BR-007)*
3. **Given** a suspended Instructor, **when** they attempt Course changes/submission, **then** access
   is denied while already-enrolled Students retain access to the Instructor's Published Courses.
   *(BR-007, BR-065)*
4. **Given** an Admin reactivates an Account under the approved Admin workflow, **when** the user
   authenticates again, **then** access is evaluated normally; old revoked sessions need not be
   restored. *(PRD §5 Admin User Operations)*

## Edge Cases

- Normalize email consistently before uniqueness and lookup; preserve the display/address needed for
  delivery without creating alias-based Account enumeration.
- Verification, invitation, and reset tokens are expiring, single-use, stored non-reversibly, and
  safe under concurrent repeated consumption.
- Resend/reset/login/refresh/invitation endpoints are rate-limited and monitored without revealing
  whether an Account exists.
- Passwords accept 15–128 Unicode characters including spaces; Unicode length semantics must be
  consistent across client/backend, and common/known-compromised values are rejected.
- Registration and invitation acceptance require a display name. Validate the 2–50-character
  Arabic/Latin-script, no-URL/control-character/markup contract consistently, without treating the
  non-unique value as an Account lookup key.
- Refresh-token reuse is rejected. Whether it revokes the remaining session family is a system-design
  security decision and must be documented before implementation.
- Multiple concurrent sessions are allowed unless system design documents a justified limit; each
  must be independently revocable.
- Concurrent suspension and a protected request must fail closed at the defined authorization
  boundary.
- An invitation to an already-registered address is rejected and never changes role, merges
  identity, or creates a second role. A future conversion/multi-role workflow is outside MVP.
- Creating an invitation creates no placeholder Account. The Account and credential are created
  atomically only after a valid invitation/token is locked, validated, and consumed.
- `accounts.role` is assigned at Account creation and is not an editable MVP field.

## Requirements

### Functional Requirements

- **FR-001**: Public self-registration MUST create Student Accounts only and MUST require email
  verification before sign-in. *(BR-001, BR-008)*
- **FR-002**: Email addresses MUST be normalized and unique, while public auth/verification/recovery
  responses MUST not reveal Account existence. *(BR-001, BR-003)*
- **FR-003**: Passwords MUST allow 15–128 Unicode characters including spaces, reject common or
  known-compromised values, have no composition/periodic-rotation rule, and be stored using Argon2id;
  plaintext/hash values MUST never be returned or logged. *(BR-002)*
- **FR-004**: Verification, invitation, and reset tokens MUST expire, be single-use, and be stored in
  a form that does not expose the bearer secret. *(BR-008, BR-009, PRD §6 Security)*
- **FR-005**: Existing Admins alone MUST invite Instructor/additional Admin Accounts; the recipient
  MUST receive exactly the assigned immutable role and MUST NOT self-select a role. Invitation
  creation MUST NOT create a placeholder Account. An email already attached to any Account MUST be
  rejected without role change or identity merge. *(BR-009, BR-080)*
- **FR-006**: The bootstrap Admin MUST be created exactly once through a secure out-of-band deployment
  operation, MUST have no repository credential, and MUST change the initial password. *(BR-009)*
- **FR-006A**: Only Student Accounts MAY place Orders, receive ordinary Entitlements, create
  Enrollments, or record Progress. Instructor Accounts have no Student consumption capability;
  Admin protected-content access MUST use the separate audited preview path. *(BR-081/082)*
- **FR-007**: Successful login MUST create an independently revocable session with a short-lived
  access token and rotating refresh token. *(BR-004)*
- **FR-008**: Expired, revoked, or reused refresh tokens MUST be rejected without new credentials.
  *(BR-005)*
- **FR-009**: Logout MUST invalidate the current refresh session; refresh after logout MUST fail.
  *(BR-006)*
- **FR-010**: A suspended Account MUST immediately fail every new/existing-session protected action,
  refresh, and login independent of prior Entitlement. *(BR-007)*
- **FR-011**: The backend MUST enforce the three roles—Student, Instructor, Admin—plus ownership and
  resource/action status for every protected request. *(BR-060, BR-080)*
- **FR-012**: Instructors MUST be limited to their own Course-management scope and MUST NOT publish
  or mutate Course/Section prices. *(BR-019, BR-060, BR-061)*
- **FR-013**: Direct Student account/contact/payment PII MUST be visible only to authorized Admin
  operations. Instructor roster access MUST be limited to BR-064's fields for an owned Course;
  authentication and authorization failures MUST not leak private/internal state. *(BR-003,
  BR-064, BR-101)*
- **FR-014**: Auth, verification, invitation, reset, and refresh endpoints MUST be rate-limited,
  monitored, and audited in proportion to security risk. *(PRD §6 Security)*
- **FR-015**: Security-sensitive events MUST generate the fixed required notification/audit events
  without allowing notification failure to roll back the auth state change. *(BR-120–BR-123)*
- **FR-016**: Student registration and staff invitation acceptance MUST collect a display name that
  satisfies BR-105. It MUST be stored as an editable, non-unique profile label and MUST NOT replace
  normalized email/internal ID as Account identity. *(BR-105)*

### Key Entities

- **Account**: Unique normalized email, internal identifier, password hash, exactly one immutable
  MVP role, verification/status fields, non-unique BR-105 display name, language/profile metadata,
  and security timestamps.
- **Session**: Independently revocable login session associated with one Account and refresh-token
  rotation state.
- **Email Verification**: Expiring single-use proof that activates a pending Student or verifies a
  changed address.
- **Staff Invitation**: Admin-created expiring single-use assignment for Instructor/Admin role.
- **Password Reset**: Non-enumerating expiring single-use credential for establishing a new password.
- **Role**: Student, Instructor, or Admin.
- **Course Ownership**: Relationship used with role/status checks to authorize an Instructor's own
  Course operations.
- **Audit Event**: Actor, target, action, outcome, reason/context, and timestamp for security-sensitive
  operations without secret values.

## Success Criteria

- **SC-001**: In acceptance testing, 100% of new public Accounts are Students and none can sign in
  before successful email verification.
- **SC-002**: In acceptance testing, 100% of valid staff invitations assign only the Admin-selected
  role; expired/revoked/reused invitations grant no access.
- **SC-003**: 100% of tested role, ownership, status, and PII boundaries are enforced by direct backend
  calls even when the client is bypassed.
- **SC-004**: A logged-out/revoked refresh session cannot obtain a token on its next attempt, and a
  rotated refresh token cannot be reused.
- **SC-005**: A suspended Account's next protected request is denied even when it presents a
  previously valid active-session credential.
- **SC-006**: Registration, login, verification, recovery, and invitation responses do not allow an
  external caller to enumerate Accounts.
- **SC-007**: Password-policy tests accept valid 15–128-character Unicode/spaced values and reject
  out-of-range, common, and known-compromised values without composition rules.

## Assumptions and Boundaries

- Social login, MFA, passwordless login, public Instructor/Admin registration, role conversion,
  multi-role/organization Accounts, and Admin impersonation are outside MVP.
- Admin is one flat MVP role; future sub-roles require a new approved decision/specification.
- The exact token format, TTLs, cookie/storage strategy, session-family reuse response, email vendor,
  and immediate-suspension mechanism are system-design decisions. They must preserve the outcomes in
  this specification.
- Public catalog browsing remains available without authentication; purchase and protected learning
  require an Active Student Account.

## Clarifications

### Session 2026-07-23

- Administrator provisioning: resolved—one out-of-band bootstrap Admin; subsequent Instructors and
  Admins are invited by an existing Admin.
- Password policy: resolved—15–128 Unicode characters including spaces; common/compromised rejection;
  Argon2id; no composition or periodic rotation.
- Suspension timing: resolved—immediate for all protected actions, including existing sessions; the
  system-design enforcement mechanism remains open.
- Public registration: resolved—Student only, with email verification required before sign-in.
