# Tasks: S1B2 Authenticated Sessions

**Input**: Design documents from `/specs/002-auth-rbac/s1b2/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`,
`contracts/session-api.md`, and D-034

**Tests**: Tests are mandatory because S1B2 changes authentication, credential rotation, and
server-side revocation. For each implementation group, write the named test first, observe the
expected failure, then implement.

**Organization**: One P1 vertical user story follows the S1B1 carryover foundation. BR-003–006 and
D-034 are cited at the behavior boundaries.

## Phase 1: Freeze and Setup

**Purpose**: Establish the approved contract and executable evidence boundary.

- [x] T001 Freeze D-034 and reconcile the single-cookie authority across
  `docs/DECISIONS.md`, `docs/BUSINESS_RULES.md`, `docs/PRD.md`,
  `docs/launch/daily/2026-07-31.md`, and the S1B2 design
- [x] T002 Create the focused specification, plan, research, data model, API contract, quickstart,
  and task graph in `specs/002-auth-rbac/s1b2/`
- [x] T003 Record the approved plan and next executable task in
  `docs/launch/daily/2026-07-31.md` and `docs/launch/STATUS.md`

---

## Phase 2: Foundational S1B1 Carryovers

**Purpose**: Close the explicit security and transport prerequisites before credential-bearing
routes. This phase blocks User Story 1.

- [x] T004 [P] Add failing environment-validation tests for deterministic password screening
  outside development in `backend/internal/config/config_test.go`
- [x] T005 [P] Add failing strict-binding no-store tests in
  `backend/internal/httpapi/binding_test.go`
- [x] T006 [P] Add failing capability-aware schema readiness tests in
  `backend/internal/db/migrate_integration_test.go` and `backend/internal/httpapi/health_test.go`
- [x] T007 Add failing safe admission-stage telemetry tests without hidden-state/PII leakage in
  `backend/internal/httpapi/observability_test.go` and `backend/internal/httpapi/admission_routes_test.go`
- [x] T008 Reject deterministic password screening outside development and validate real-screen
  composition in `backend/internal/config/config.go` and `backend/cmd/api/main.go`
- [x] T009 Set `Cache-Control: no-store` before strict body binding can fail in
  `backend/internal/httpapi/binding.go`
- [x] T010 Make schema checks accept the minimum required migration for each enabled capability in
  `backend/internal/db/schema.go`, `backend/internal/health/health.go`, and `backend/cmd/api/main.go`
- [x] T011 Emit allowlisted admission failure stages through
  `backend/internal/httpapi/observability.go`, `backend/internal/httpapi/admission_security.go`, and
  `backend/internal/logging/logging.go`
- [x] T012 Run the focused carryover tests and update evidence in
  `docs/launch/daily/2026-07-31.md`

**Checkpoint**: Staging/production fixture misuse fails closed, schema-4/schema-5 readiness follows
enabled capability, strict-binding errors are no-store, and public admission failures remain
generic while safe internal stages are visible.

---

## Phase 3: User Story 1 — Sign in, renew safely, and sign out (Priority: P1) MVP

**Goal**: An Active Student, Instructor, or Admin can use one server-managed opaque session family
without exposing credentials to JavaScript. *(BR-003–006, D-034)*

**Independent Test**: Against PostgreSQL, create one Active Account, login, prove digest-only
storage, resolve, race renewal, reject/confirm stale reuse, revoke the family, and prove logout
denial; compare every hidden login failure contract.

### Configuration and domain tests

- [x] T013 [P] [US1] Add failing role-window, recent-auth, stale-classification, cookie, and
  dummy-hash validation tests in `backend/internal/config/config_test.go`,
  `backend/internal/identity/session_test.go`, and `backend/internal/auth/session_test.go`
- [x] T014 [P] [US1] Add failing session resolution, expiry, and stale-use classification unit
  tests in `backend/internal/identity/session_test.go`
- [x] T015 [US1] Add failing PostgreSQL login/create, renewal race, reuse revocation, mutation
  recheck, and logout tests in `backend/internal/identity/session_flow_integration_test.go`
- [x] T016 [US1] Implement typed Student/Instructor/Admin session profiles and sensitive-window
  configuration in `backend/internal/config/config.go`
- [x] T017 [US1] Implement non-secret authenticated-session facts and state decisions in
  `backend/internal/identity/session.go`
- [x] T018 [US1] Implement PostgreSQL login, resolution, atomic rotation, stale evidence,
  family-revocation, and logout transactions in `backend/internal/identity/session_repository.go`,
  reusing the proven rotation boundary from `backend/internal/identity/password_change.go`

### HTTP contract tests

- [x] T019 [P] [US1] Add failing Problem Details and stable authentication-challenge tests in
  `backend/internal/problem/problem_test.go`
- [x] T020 [US1] Add failing hidden-state login equivalence and digest-only success contract tests
  in `backend/internal/httpapi/session_routes_integration_test.go`
- [x] T021 [US1] Add failing cookie, no-store, CSRF/origin, resolution, renewal-race, stale/reuse,
  and logout contract tests in `backend/internal/httpapi/session_routes_integration_test.go`
- [x] T022 [US1] Add failing layered login and authenticated-session rate-decision tests in
  `backend/internal/httpapi/session_routes_test.go`; the planned separate
  `session_rate_limit_test.go` was not created, and the rate-decision cases live beside the other
  route tests

### Backend implementation

- [x] T023 [US1] Add generic authentication, replacement, reuse, and CSRF Problem Details in
  `backend/internal/problem/problem.go`
- [x] T024 [US1] Implement session-cookie parsing and typed request authentication while preserving
  the existing user-ID authorization seam in the new `backend/internal/auth/session.go` and
  `backend/internal/auth/session_response.go`; `auth.go` needed no change because the seam is
  satisfied through `SessionAuthenticator.UserFromRequest`
- [x] T025 [US1] Implement trusted-origin and generation-bound CSRF middleware for state-changing
  cookie-authenticated requests in `backend/internal/httpapi/session_security.go`
- [x] T026 [US1] Implement layered login/session rate decisions with keyed identifier digests in
  `backend/internal/httpapi/session_rate_limit.go`
- [x] T027 [US1] Implement `POST /sessions`, `GET /session`, `POST /session-renewals`, and
  `DELETE /session` with commit-before-cookie behavior in
  `backend/internal/httpapi/session_handlers.go`
- [x] T028 [US1] Register real-session composition and routes without breaking the
  development-only fake-auth seam in `backend/internal/httpapi/router.go` and
  `backend/cmd/api/main.go`
- [x] T029 [US1] Append allowlisted login, rotation, stale-use, reuse, and logout evidence without
  credential, CSRF, password, email, or hidden-state leakage through
  `backend/internal/identity/session_repository.go` and `backend/internal/httpapi/observability.go`;
  the existing `appendIdentitySecurityEvent` helper in
  `backend/internal/identity/security_event.go` was reused unchanged, with the closed event
  allowlist widened by migration `0006_authenticated_sessions`

### Frontend tests and implementation

- [ ] T030 [P] [US1] Add safe internal `returnTo` and memory-only session-store unit tests in
  `frontend/src/lib/identity/session.test.ts` and `frontend/src/lib/identity/return-to.test.ts`,
  run through Node's built-in `node:test` runner under a new `npm run test` script
- [ ] T031 [US1] **Downgraded on 2026-07-31 by developer decision.** Bilingual login and
  session-state interaction coverage is manual RTL/LTR, keyboard, failure, return, and logout
  inspection per `quickstart.md`, recorded in `docs/launch/daily/2026-07-31.md`, instead of
  automated tests in `frontend/src/components/auth/login-form.test.tsx`

  The frontend has no component-test infrastructure: no runner, no test script, and no test file
  existed before this slice, and S1B1 shipped its admission screens on the same manual-inspection
  basis. Adding `vitest`, `@testing-library/react`, and `jsdom` would contradict this plan's
  recorded no-new-dependency constitution gate and put framework setup on the critical path of a
  Red-confidence day. T030's pure-TS logic — including the security-relevant `returnTo`
  open-redirect boundary — is still covered automatically, because `node:test` needs no new runtime
  dependency. Component-level test infrastructure is visible carryover, not a silent skip.
- [ ] T032 [US1] Implement session API calls with `credentials: include`, no persistence, safe
  Problem parsing, and in-memory CSRF rehydration in `frontend/src/lib/api/identity.ts` and
  `frontend/src/lib/identity/session.ts`
- [ ] T033 [US1] Implement safe internal `returnTo` validation and role-root selection in
  `frontend/src/lib/identity/return-to.ts`
- [ ] T034 [US1] Add Arabic/English login, expired/replaced/reuse/logout copy in
  `frontend/src/lib/i18n/dictionaries/ar.ts` and `frontend/src/lib/i18n/dictionaries/en.ts`
- [ ] T035 [US1] Implement accessible responsive sign-in and session-state UI in
  `frontend/src/components/auth/login-form.tsx`, `frontend/src/app/(auth)/login/page.tsx`, and
  existing navigation auth actions

**Checkpoint**: The complete S1B2 story is independently usable and verified against real
PostgreSQL; browser storage and database canaries contain no plaintext credential or CSRF value.

---

## Phase 4: Quality, Documentation, and Independent Review

**Purpose**: Prove the frozen slice and close it with repository evidence.

- [ ] T036 [P] Run frontend typecheck, lint, production build, and Arabic/English responsive,
  keyboard, RTL/LTR, failure, return, and logout inspection per `quickstart.md`
- [ ] T037 Run backend formatting, build, vet, race, PostgreSQL/Redis/MinIO integration, migration,
  docs, and exposure gates; run clean-code and test quality reviews over changed production/tests
- [ ] T038 Run database/browser/log canary sweeps for plaintext cookie, CSRF, password, raw email,
  and hidden-state leakage; record results in `docs/launch/daily/2026-07-31.md`
- [ ] T039 Synchronize implemented contracts and launch state in
  `specs/002-auth-rbac/s1b2/contracts/session-api.md`, `docs/launch/STATUS.md`,
  `docs/launch/SLICES.md`, and `docs/launch/daily/2026-07-31.md`
- [ ] T040 Freeze and push the exact implementation range, verify hosted CI, and dispatch `agy`
  read-only review from a disposable detached worktree under D-035. Claude cannot review this range
  because it authors part of it
- [ ] T041 Resolve every critical/high review finding, rerun affected/full gates, record the final
  reviewed head and verdict, and close Day 9 only when all acceptance evidence exists

---

## Dependencies and Execution Order

- Phase 1 is complete when T003 records the confirmed plan.
- Phase 2 depends on Phase 1 and blocks all credential-bearing routes.
- In Phase 3, T013–T015 fail first; T016–T018 make the domain tests pass. T019–T022 then fail before
  T023–T029 implement the HTTP boundary. T030–T031 fail before T032–T035 implement the UI.
- Phase 4 begins only after the independent story test passes.
- Tasks marked `[P]` touch independent files and can be reasoned about concurrently, but a single
  builder owns the slice. Codex held that seat through T029 under D-033; Claude holds it from T030
  under D-035, with `agy` as the later exact-range reviewer.

## Implementation Strategy

1. Land the four bounded S1B1 carryovers.
2. Build and prove the server-side family/generation core before exposing routes.
3. Add the generic login and authenticated HTTP contract.
4. Add browser-memory state and bilingual UI.
5. Run the full evidence gate, freeze the exact range, and obtain independent review.

No S1B3 recovery or S1C invitation/suspension/authorization-matrix work is pulled into this slice.
