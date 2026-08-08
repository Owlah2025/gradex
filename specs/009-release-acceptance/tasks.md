---

description: "Implementation tasks for S11 release acceptance"
---

# Tasks: S11 — Release Acceptance

**Input**: Design documents from `specs/009-release-acceptance/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`

**Tests**: Required. S11 is an acceptance-coverage slice and ships no product behavior.

## Phase 1: Setup — Portable release entry point

**Purpose**: Add an S11 selection around existing S12 infrastructure while preserving S12 defaults.

- [ ] T001 Add S11 suite selection while preserving the production registration safety gate and default S12 behavior in `deploy/scripts/verify-staging-smoke.sh`
- [ ] T002 Add a thin production-like S11 entry point that delegates to the existing verifier in `deploy/scripts/verify-s11-release-acceptance.sh`
- [ ] T003 Add the local release-acceptance Playwright command in `frontend/package.json`

---

## Phase 2: Foundational — Authoritative test evidence

**Purpose**: Expose only the test-side identity, provenance, cardinality, and Progress evidence required by every S11 story.

**Critical**: No browser story begins until these helpers fail closed on missing or malformed evidence.

- [ ] T004 Add a safety-gated email-verification-token query to the existing test binary in `backend/cmd/e2e-seed/seed_test.go`
- [ ] T005 [P] Add query flag, input-validation, protected-payload, and failure tests in `backend/cmd/e2e-seed/invocation_test.go` and `backend/cmd/e2e-seed/seed_test.go`
- [ ] T006 Add email-verification-token query support for Playwright in `frontend/e2e/rotating-students.ts`
- [ ] T007 [P] Add malformed/absent token helper tests in `frontend/src/lib/api/e2e-progress.test.ts` or a focused adjacent test file
- [ ] T008 Add Entitlement ID, grant source, source Invitation, Enrollment ID, and exact Progress evidence to `backend/cmd/e2e-seed/seed_test.go` and `frontend/src/lib/api/e2e-progress.ts`
- [ ] T009 [P] Add parsing and missing-row fail-closed tests for the expanded state evidence in `backend/cmd/e2e-seed/seed_test.go` and `frontend/src/lib/api/e2e-progress.test.ts`

**Checkpoint**: The browser suite can observe every invariant without ad hoc SQL.

---

## Phase 3: User Story 1 — Launch-critical journey (Priority: P1)

**Goal**: Prove one Student path from registration through persisted protected learning.

**Independent Test**: Run `frontend/e2e/s11-release-acceptance.spec.ts` against a fresh isolated environment and observe registration, verification, login, access grant, media, and Progress succeed in order.

- [ ] T010 [US1] Add browser registration, delivered-token verification, and real password login in `frontend/e2e/s11-release-acceptance.spec.ts`
- [ ] T011 [US1] Add Admin login, Course expiry configuration, Invitation creation, identity-bound acceptance, exact zero grant counts, and pre-approval Course denial in `frontend/e2e/s11-release-acceptance.spec.ts`
- [ ] T012 [US1] Add Admin Approval and exact active provenance-bearing Entitlement plus Enrollment assertions in `frontend/e2e/s11-release-acceptance.spec.ts`
- [ ] T013 [US1] Add protected Course, Lesson, playback, signed-media issuance, and persisted Progress assertions in `frontend/e2e/s11-release-acceptance.spec.ts`

**Checkpoint**: SC-001–SC-005 pass without manual state edits.

---

## Phase 4: User Story 2 — Negative authorization and access (Priority: P1)

**Goal**: Prove premature, unrelated, and anonymous requesters receive no protected capability or side effect.

**Independent Test**: Run the S11 negative cases and selected S4/S5 checks, then compare authoritative state before and after every denial.

- [ ] T014 [US2] Add pre-approval Lesson, playback, and Progress denials with unchanged state in `frontend/e2e/s11-release-acceptance.spec.ts`
- [ ] T015 [US2] Add unrelated-Student Course, Lesson, playback, and Progress denials with unchanged intended-Student state in `frontend/e2e/s11-release-acceptance.spec.ts`
- [ ] T016 [US2] Select existing protected-media retrieval and anonymous/unrelated media denial evidence in `deploy/scripts/verify-staging-smoke.sh` without copying S4/S5 test logic

**Checkpoint**: Every launch-critical protected boundary is deny-by-default before or outside the grant.

---

## Phase 5: User Story 3 — Failure and recovery (Priority: P2)

**Goal**: Prove invalid secrets and safe retries do not wedge or duplicate the journey.

**Independent Test**: Refuse an invalid secret, complete the valid retry, repeat authorized approval, and run existing concurrent grant coverage; counts and provenance stay exact.

- [ ] T017 [US3] Add invalid-email-verification-secret refusal followed by successful valid verification in `frontend/e2e/s11-release-acceptance.spec.ts`
- [ ] T018 [US3] Add invalid-Invitation-secret refusal followed by successful valid acceptance and zero premature grant state in `frontend/e2e/s11-release-acceptance.spec.ts`
- [ ] T019 [US3] Replace weak replay evidence with valid Admin CSRF, exact `200`, identical Entitlement identity, and one-to-one cardinality in `frontend/e2e/s11-release-acceptance.spec.ts` and `frontend/e2e/s6-course-access-grant-launch.spec.ts`
- [ ] T020 [US3] Select the existing identity recovery, action-secret replay, rejection/cancellation, and concurrent approval integration tests in `deploy/scripts/verify-s11-release-acceptance.sh`

**Checkpoint**: SC-006 passes through both browser and integration evidence.

---

## Phase 6: User Story 4 — Disposable and public staging portability (Priority: P2)

**Goal**: Run the same release suite through existing external-origin and isolated-database configuration.

**Independent Test**: Run the thin S11 entry point against `https://gradex.localhost:18443`, then run origin-validation unit tests for a representative public HTTPS URL.

- [ ] T021 [US4] Wire S11 browser and integration selections into `deploy/scripts/verify-staging-smoke.sh` while retaining the existing external-origin, CA/SPKI, database tunnel, and cleanup contract
- [ ] T022 [US4] Add verifier mode/default and shell-syntax regression coverage in `deploy/scripts/verify-s11-release-acceptance.sh` and existing shell validation commands
- [ ] T023 [US4] Record exact-head redacted results, schema, reused coverage, findings, and provider boundary in `docs/launch/evidence/s11/release-acceptance.md`

**Checkpoint**: The disposable HTTPS run passes; T047 needs configuration plus the separately tracked production registration capability, not S11 test-source changes.

---

## Phase 7: Polish and closure candidate

**Purpose**: Run the complete quality gate, audit scope, and freeze a clean review range.

- [ ] T024 Run formatting and focused/full backend tests from `backend/` and record exact results in `docs/launch/evidence/s11/release-acceptance.md`
- [ ] T025 Run frontend typecheck, unit tests, lint, and production build from `frontend/` and record exact results in `docs/launch/evidence/s11/release-acceptance.md`
- [ ] T026 Run the local isolated S11 Chromium journey from `frontend/` and record exact results in `docs/launch/evidence/s11/release-acceptance.md`
- [ ] T027 Run the S11 production-like HTTPS verifier from `deploy/scripts/verify-s11-release-acceptance.sh` and record exact results in `docs/launch/evidence/s11/release-acceptance.md`
- [ ] T028 Audit migration/schema version, changed paths, commerce/S8/Entitlement-update/provider exclusions, and secret-free evidence in `docs/launch/evidence/s11/release-acceptance.md`
- [ ] T029 Mark completed tasks, commit final evidence, verify a clean final HEAD, and record the frozen range in `specs/009-release-acceptance/tasks.md` and `docs/launch/evidence/s11/release-acceptance.md`

---

## Dependencies & Execution Order

- Phase 1 preserves the current S12 default and creates the release entry point.
- Phase 2 blocks browser implementation because S11 assertions require trustworthy evidence.
- User Story 1 establishes the positive journey; User Story 2 and User Story 3 then extend the same isolated state sequentially.
- User Story 4 depends on the complete browser story and delegates to existing S12 infrastructure.
- Phase 7 starts only after all story tasks pass independently.

## Parallel Opportunities

- T005, T007, and T009 touch separate Go/TypeScript test files after their corresponding helper shapes are fixed.
- Existing S4/S5 coverage inventory for T016 can be checked while the S11 browser story is being authored.
- Static backend and frontend gates in T024–T025 may run independently once implementation is frozen; browser runs stay serial because they own shared environment state.

## Implementation Strategy

1. Preserve default S12 behavior and add the narrow S11 entry point.
2. Make test evidence trustworthy before writing browser assertions.
3. Complete the positive journey, then denial and recovery cases in the same serial isolated run.
4. Run locally, then through the deployed HTTPS topology.
5. Record honest findings and freeze one clean exact range for independent review.

## Format Validation

All 29 tasks use the required checkbox, sequential task ID, optional parallel marker, story label where applicable, and concrete file path.
