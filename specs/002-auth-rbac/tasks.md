# Tasks: S1B1 Student Admission

**Input**: Design documents from `specs/002-auth-rbac/`
**Scope**: User Story 1 only, as narrowed by the developer-approved S1B delivery design
**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`,
`quickstart.md`

**Tests**: Required by the feature specification, launch close conditions, and constitution V.
Write each named test first and observe the relevant failure before production implementation.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: May run in parallel with other marked tasks in the same phase because it uses distinct
  files and has no incomplete dependency.
- **[US1]**: Feature-spec User Story 1, “Student creates and verifies an account.”
- Every implementation/test must cite its governing BR identifiers in a nearby test name or comment.

## Phase 1: Setup and contract freeze

**Purpose**: Make the approved S1B1 boundaries executable without introducing provider or legal
defaults.

- [ ] T001 Freeze the S1B1 plan/task artifact set and verify every relative link and documented command in `specs/002-auth-rbac/` and `docs/superpowers/specs/2026-07-30-s1b-delivery-design.md`
- [ ] T002 [P] Add typed development/test admission policy, anonymous-CSRF key, limiter-HMAC key, protected-payload key reference, and timeout settings with fail-closed production validation in `backend/internal/config/config.go`, `backend/internal/config/config_test.go`, and `backend/.env.example`
- [ ] T003 [P] Add the S1B1 Problem Details constructors and fixed safe messages for `MALFORMED_JSON`, `TOKEN_INVALID`, `RATE_LIMITED`, `RATE_LIMITING_UNAVAILABLE`, `REGISTRATION_UNAVAILABLE`, and `TRANSACTIONAL_DELIVERY_UNAVAILABLE` in `backend/internal/problem/problem.go` and `backend/internal/problem/problem_test.go`

**Checkpoint**: The contract has stable configuration and public error types, with no invented
production provider/policy values.

---

## Phase 2: Foundational security and persistence

**Purpose**: Close S1A advisories and land the shared schema/security boundaries before mounting any
public admission route.

**Critical**: No Phase 3 route may be mounted until T004–T017 pass.

### Bootstrap retry and correspondence-email hardening

- [ ] T004 [P] Write failing BR-002/bootstrap tests for canonical fingerprint equivalence/mismatch, password equivalence, legacy fingerprint refusal, correspondence-email preservation, and concurrent identical retries in `backend/internal/identity/bootstrap_integration_test.go` and `backend/internal/identity/normalize_test.go`
- [ ] T005 Implement the versioned canonical bootstrap fingerprint, constant-time comparison, recorded Argon2id password check, and preserved correspondence email in `backend/internal/identity/bootstrap.go`, `backend/cmd/bootstrap-admin/main.go`, and `backend/internal/db/migrations/0005_student_admission.up.sql`

### Required compromised-password boundary

- [ ] T006 [P] Write failing BR-002 unit tests proving adapter plaintext/full-digest isolation, deterministic compromised/clear results, bounded/malformed response handling, and nil/unavailable fail-closed behavior in `backend/internal/identity/password_test.go` and `backend/internal/identity/credential_test.go`
- [ ] T007 Replace the optional plaintext checker with the required provider-neutral range source inside the sole plaintext boundary and wire deterministic development/test plus explicit unavailable production behavior in `backend/internal/identity/password.go`, `backend/internal/identity/credential.go`, `backend/internal/identity/compromised.go`, `backend/internal/identity/bootstrap.go`, and `backend/cmd/bootstrap-admin/main.go`

### Migration and data invariants

- [ ] T008 Write failing migration/invariant tests for bootstrap fingerprint columns, immutable policy acceptances, one-live-purpose-bound secret, append-only Identity events, immutable safe outbox event, and one protected payload in `backend/internal/db/migrate_integration_test.go`
- [ ] T009 Implement and reverse migration 0005 with all constraints, partial indexes, one-way/append-only triggers, and safe legacy bootstrap behavior in `backend/internal/db/migrations/0005_student_admission.up.sql` and `backend/internal/db/migrations/0005_student_admission.down.sql`
- [ ] T010 Update schema-version enforcement and hosted migration object assertions from 4 to 5 in `backend/internal/db/schema.go`, `backend/internal/db/migrate_integration_test.go`, and `.github/workflows/ci.yml`

### Protected outbox intent

- [ ] T011 [P] Write failing BR-120/122 unit/integration tests for authenticated encryption, associated-data binding, ciphertext canary absence, wrong-key rejection, atomic event/payload insertion, and rollback in `backend/internal/outbox/outbox_test.go` and `backend/internal/outbox/outbox_integration_test.go`
- [ ] T012 Implement the transaction-scoped immutable event/protected-payload writer and required encryption adapter in `backend/internal/outbox/outbox.go`, `backend/internal/outbox/protected_payload.go`, and `backend/internal/outbox/types.go`

### Anonymous browser admission and strict JSON

- [ ] T013 [P] Write failing HTTP tests for signed host-only anonymous cookie, browser-memory CSRF, exact Origin/Referer, method/media/body bounds, duplicate/unknown/trailing JSON rejection, and middleware order in `backend/internal/httpapi/admission_security_test.go` and `backend/internal/httpapi/binding_test.go`
- [ ] T014 Implement anonymous security bootstrap, admission Origin/CSRF middleware, strict bounded JSON decoding, and no-store cookie/response behavior in `backend/internal/httpapi/anonymous_session.go`, `backend/internal/httpapi/admission_security.go`, `backend/internal/httpapi/binding.go`, and `backend/internal/httpapi/router.go`

### Layered limiter

- [ ] T015 [P] Write failing FR-014 tests for versioned endpoint/identifier/network/anonymous/global dimensions, opaque HMAC keys, Redis atomic allow/deny, bounded strict-local fallback, true `429`, and unavailable `503` in `backend/internal/ratelimit/limiter_test.go` and `backend/internal/ratelimit/limiter_integration_test.go`
- [ ] T016 Implement Redis-backed layered admission decisions, dedicated key derivation, circuit timeout, bounded strict-local fallback, and safe metrics outcome types in `backend/internal/ratelimit/limiter.go`, `backend/internal/ratelimit/redis.go`, `backend/internal/ratelimit/local.go`, and `backend/internal/ratelimit/policy.go`
- [ ] T017 Compose the required policy resolver, credential screen, protected outbox writer, anonymous security keys, and rate limiter without mounting Student command routes yet in `backend/cmd/api/main.go` and `backend/internal/httpapi/router.go`

**Checkpoint**: Bootstrap hardening, migration 0005, required password screening, protected outbox,
strict anonymous browser admission, and rate-limit failure modes pass before User Story 1 work.

---

## Phase 3: User Story 1 — Student creates and verifies an account (Priority: P1) MVP

**Goal**: A visitor registers only as a pending Student, requests privacy-safe verification
delivery, and activates exactly once without Account enumeration or session issuance.

**Independent Test**: Run the focused unit/PostgreSQL/HTTP suites to prove a new Account is
`STUDENT/PENDING_VERIFICATION`, an existing email is a complete hidden no-op, eligible resend
supersedes atomically, one concurrent consumer activates, all unusable tokens are `TOKEN_INVALID`,
and the three Arabic/English screens expose no credential or Account state.

### Tests for User Story 1

- [ ] T018 [P] [US1] Write failing BR-001/002/008/105 unit tests for display-name/email/locale/policy validation, secret generation/digest, expiry, and error classification in `backend/internal/identity/admission_test.go`, `backend/internal/identity/action_secret_test.go`, and `backend/internal/identity/display_name_test.go`
- [ ] T019 [P] [US1] Write failing BR-001/002/008/105 PostgreSQL tests for pending-Student registration, no session, original email, exact policy acceptances, digest-only secret, duplicate-email no-op, forced final-write rollback, resend supersession, expiry/wrong-purpose/reuse, and concurrent resend/consumption in `backend/internal/identity/admission_integration_test.go`
- [ ] T020 [P] [US1] Write failing BR-001/003/008 and FR-014 HTTP contract/privacy tests for all four public endpoints, generic `202` equivalence across status/body/headers/cookies/size/timing/delivery class, `TOKEN_INVALID`, and limiter/delivery failure behavior in `backend/internal/httpapi/identity_handlers_test.go` and `backend/internal/httpapi/admission_privacy_integration_test.go`

### Identity domain implementation

- [ ] T021 [P] [US1] Implement the BR-105 display-name validator, normalized/preserved email value, locale value, current-policy resolver contract, and registration input/result types in `backend/internal/identity/admission_types.go`, `backend/internal/identity/display_name.go`, and `backend/internal/identity/policy_set.go`
- [ ] T022 [P] [US1] Implement 32-byte bearer generation, URL-safe encoding, SHA-256 digest lookup, expiry clock abstraction, and purpose/lifecycle types in `backend/internal/identity/action_secret.go`
- [ ] T023 [US1] Implement atomic Student registration with required policy/screen/hash work, pending immutable role, policy/evidence/secret/protected-outbox co-commit, and duplicate-email hidden no-op in `backend/internal/identity/admission.go` and `backend/internal/identity/security_event.go`
- [ ] T024 [US1] Implement privacy-safe verification request/resend with Account→secret lock order, eligible supersession/replacement, hidden no-op outcomes, and linked evidence/outbox intent in `backend/internal/identity/admission.go`
- [ ] T025 [US1] Implement body-token verification with digest resolution, Account→exact-secret locking/recheck, one pending→active transition/revision increment, single consumption, and uniform invalid result in `backend/internal/identity/admission.go`

### HTTP contract and composition

- [ ] T026 [US1] Implement the policy-set, registration, verification-request, and verification-consumption handlers with fixed success bodies and safe domain/error mapping in `backend/internal/httpapi/identity_handlers.go`
- [ ] T027 [US1] Mount `GET /api/v1/registration-policy-set`, `POST /api/v1/student-registrations`, `POST /api/v1/email-verification-requests`, and `POST /api/v1/email-verifications` behind the approved middleware order in `backend/internal/httpapi/router.go` and compose the Identity admission service in `backend/cmd/api/main.go`

### Responsive bilingual frontend

- [ ] T028 [P] [US1] Implement the relative same-origin admission client, RFC 9457 parsing, anonymous bootstrap/CSRF memory state, code-point validation, and fragment scrub helper in `frontend/src/lib/api/http.ts`, `frontend/src/lib/api/problem.ts`, `frontend/src/lib/api/identity.ts`, and `frontend/src/lib/identity/validation.ts`
- [ ] T029 [P] [US1] Add accessible Input/Field/Alert primitives, the responsive auth shell, paired typed admission dictionaries, localized skip/logo labels, and Arabic-first initial document direction in `frontend/src/components/ui/input.tsx`, `frontend/src/components/ui/field.tsx`, `frontend/src/components/ui/alert.tsx`, `frontend/src/components/auth/auth-shell.tsx`, `frontend/src/lib/i18n/dictionaries/en.ts`, `frontend/src/lib/i18n/dictionaries/ar.ts`, `frontend/src/lib/i18n/config.ts`, and `frontend/src/app/layout.tsx`
- [ ] T030 [US1] Build `/register` with current-policy loading, explicit acceptance, BR-105/password guidance, backend field-error focus, generic accepted navigation, and no role/session/credential persistence in `frontend/src/app/(auth)/register/page.tsx` and `frontend/src/components/auth/registration-form.tsx`
- [ ] T031 [US1] Build `/verify-email` with re-entered email, generic eligible/ineligible/unknown guidance, retryable `429`/`503` states, and no identifier persistence in `frontend/src/app/(auth)/verify-email/page.tsx` and `frontend/src/components/auth/verification-request-form.tsx`
- [ ] T032 [US1] Build `/verify-email/result` with immediate fragment copy/scrub, body-only consumption, success without session, one combined invalid-link state, disabled analytics, and future `/login` navigation in `frontend/src/app/(auth)/verify-email/result/page.tsx` and `frontend/src/components/auth/verification-consumer.tsx`

**Checkpoint**: User Story 1 is independently functional and satisfies every S1B1 close condition;
S1B2 login is still absent.

---

## Phase 4: Polish, verification, and launch evidence

**Purpose**: Close cross-cutting leakage, hosted-CI, documentation, and independent-review gates.

- [ ] T033 [P] Add password/token/identifier canary sweeps and logging allowlist assertions across success, duplicate, rollback, resend, invalid, and limiter paths in `backend/internal/identity/admission_integration_test.go`, `backend/internal/httpapi/admission_privacy_integration_test.go`, and `backend/internal/logging/logging_test.go`
- [ ] T034 [P] Add a PostgreSQL/Redis-backed hosted Admission Integration job that runs S1B1 concurrency, rollback, privacy, and migration evidence in `.github/workflows/ci.yml`
- [ ] T035 Run every command and browser scenario in `specs/002-auth-rbac/quickstart.md`, fix failures in their owning files, and record the exact evidence in `docs/launch/daily/2026-07-30.md`
- [ ] T036 Apply `clean-code-guard`, `test-guard`, and `docs-guard` to the complete S1B1 diff and resolve every critical/high issue in the affected production, test, and documentation files
- [ ] T037 Freeze and push the exact implementation commit range, record green hosted CI, and obtain independent read-only Claude review from a disposable detached worktree in `docs/launch/daily/2026-07-30.md`
- [ ] T038 Reconcile completed tasks, actual commit/CI/review evidence, unresolved lower-severity dispositions, and the next S1B2 handoff in `specs/002-auth-rbac/tasks.md`, `docs/launch/STATUS.md`, and `docs/launch/daily/2026-07-30.md`

---

## Dependencies and execution order

### Phase dependencies

```text
Phase 1 contract/config freeze
    ↓
Phase 2 security + persistence foundation
    ↓
Phase 3 User Story 1 backend/API/frontend
    ↓
Phase 4 full verification + independent review
```

- T002 and T003 may run in parallel after T001.
- T004 and T006 write overlapping Bootstrap/password tests only at separate named files, then T005
  depends on T004 and T007 depends on T006.
- T008 precedes T009; T010 follows the completed migration.
- T011 precedes T012; T013 precedes T014; T015 precedes T016.
- T017 depends on T002, T005, T007, T009, T012, T014, and T016.
- No User Story 1 implementation starts until T017 passes.
- T018–T020 may be authored in parallel and must fail for the intended missing behavior.
- T023 depends on T018, T019, T021, T022, and the complete foundation.
- T024 and T025 follow the shared transaction helpers in T023.
- T026 depends on T020 and T023–T025; T027 depends on T026.
- T028 and T029 may run in parallel after the API contract is frozen; T030–T032 depend on both.
- Phase 4 begins only when T018–T032 pass as one independent story.

### User-story dependency

- **US1 (P1)** is the only story in this S1B1 plan. It depends on the shared S1A/security/persistence
  foundation, not on feature-spec US2–US5.
- Staff invitation, login/session rotation, recovery, suspension, and the final authorization matrix
  remain in S1B2/S1B3/S1C and must not be pulled into these tasks.

## Parallel execution examples

After T001:

```text
T002 configuration boundary
T003 Problem Details catalog
```

Within the foundation:

```text
T004 bootstrap failing tests
T006 credential-boundary failing tests
T011 outbox failing tests
T013 anonymous security/strict-JSON failing tests
T015 limiter failing tests
```

After T017:

```text
T018 domain unit tests
T019 PostgreSQL transaction/concurrency tests
T020 HTTP contract/privacy tests
```

After the backend contract is stable:

```text
T028 frontend API/security helpers
T029 frontend auth shell/i18n/primitives
```

## Implementation strategy

### First executable increment: prerequisites

1. Complete T001–T017.
2. Run focused bootstrap, migration, outbox, anonymous-CSRF, strict-JSON, and limiter suites.
3. Do not mount public Student command routes if any prerequisite is incomplete.

### User Story 1 increment

1. Write and observe T018–T020 failures.
2. Implement domain primitives and transactions T021–T025.
3. Implement and mount transport T026–T027.
4. Implement bilingual responsive UI T028–T032.
5. Stop and run the independent User Story 1 test from the Phase 3 checkpoint.

### Delivery close

1. Complete leakage/hosted-CI work T033–T034.
2. Run the full quickstart and record actual evidence T035.
3. Apply the production/test/docs review guards T036.
4. Freeze one exact range for independent review T037.
5. Reconcile launch state and hand off to S1B2 T038.

## Task summary

- Total: 38 tasks
- Setup/contract freeze: 3
- Foundational security/persistence: 14
- User Story 1: 15
- Polish/verification/review: 6
- Parallel opportunities: 16 tasks marked `[P]`
- Suggested MVP scope: all of this task list; it is already the narrowed S1B1/US1 vertical

Every task uses the required checkbox, sequential ID, optional parallel marker, required User Story
label inside the story phase, and explicit repository file path.
