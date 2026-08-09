# Tasks: S9 — Transactional Email Delivery

**Input**: Design documents from `specs/010-transactional-email/`

**Tests**: Required by the feature specification and constitution.

## Phase 1: Planning and setup

- [x] T001 Reconcile repository authority and fixed eight-intent inventory in `specs/010-transactional-email/spec.md`
- [x] T002 Record Resend/provider-boundary decision in `docs/DECISIONS.md` and active S9 state in `docs/launch/STATUS.md`
- [x] T003 Validate `spec.md`, `plan.md`, and `tasks.md` consistency and commit the planning package

## Phase 2: Foundational delivery infrastructure

- [ ] T004 [P] Add delivery/attempt migration and rollback in `backend/internal/db/migrations/0016_transactional_email.*.sql`
- [ ] T005 [P] Add provider-neutral message/result/error types and fake in `backend/internal/email/message.go` and `backend/internal/email/fake.go`
- [ ] T006 [P] Add protected outbox payload decryption/read contract and privacy tests in `backend/internal/outbox/protected_payload.go` and `backend/internal/outbox/*_test.go`
- [ ] T007 Add durable claim/complete/retry repository and integration tests in `backend/internal/email/repository.go` and `backend/internal/email/repository_integration_test.go`
- [ ] T008 Add typed email mode/sender/timeout/retry configuration and fail-closed tests in `backend/internal/config/config.go` and `backend/internal/config/config_test.go`

## Phase 3: User Story 1 — Account verification and recovery

- [ ] T009 [P] [US1] Write Arabic/English renderer contract tests for verification, reset, and reset-completed in `backend/internal/email/renderer_test.go`
- [ ] T010 [US1] Implement fixed bilingual text/HTML templates and fragment link builder in `backend/internal/email/renderer.go`
- [ ] T011 [P] [US1] Write Resend TLS/idempotency/timeout/error tests in `backend/internal/email/resend_test.go`
- [ ] T012 [US1] Implement one-call Resend HTTPS adapter in `backend/internal/email/resend.go`
- [ ] T013 [US1] Implement dispatcher validation, decrypt/render/send, bounded retry, and privacy behavior in `backend/internal/email/dispatcher.go`
- [ ] T014 [US1] Add verification/reset deterministic delivery acceptance without helper extraction in `backend/internal/email/acceptance_integration_test.go`

## Phase 4: User Story 2 — Staff and Course invitations

- [ ] T015 [P] [US2] Add recipient Account-locale selection plus co-committed rejection/cancellation intents and tests in `backend/internal/access/repository.go` and `backend/internal/access/*_test.go`
- [ ] T016 [P] [US2] Add staff preview/completion frontend API functions and unit tests in `frontend/src/lib/api/identity.ts` and `frontend/src/lib/api/identity.test.ts`
- [ ] T017 [P] [US2] Extend credential-fragment capture for Course invitations and tests in `frontend/src/lib/identity/validation.ts` and `frontend/src/lib/identity/validation.test.ts`
- [ ] T018 [US2] Add bilingual staff invitation acceptance component/route in `frontend/src/components/staff/staff-invitation-acceptance.tsx` and `frontend/src/app/staff/accept/page.tsx`
- [ ] T019 [US2] Update Course access page to consume fragment credentials while preserving S6 authorization in `frontend/src/app/[locale]/access/page.tsx`
- [ ] T020 [US2] Add Course invitation/grant and staff invitation renderer and acceptance cases in `backend/internal/email/renderer_test.go` and `backend/internal/email/acceptance_integration_test.go`
- [ ] T021 [US2] Prove delivered Course link creates zero access before approval and exact grant after approval by reusing S6/S11 coverage in `frontend/e2e/s9-transactional-email.spec.ts`

## Phase 5: User Story 3 — Operations and composition

- [ ] T022 [P] [US3] Add safe transactional-email lifecycle logging and exposure tests in `backend/internal/logging/logging.go` and `backend/internal/logging/logging_test.go`
- [ ] T023 [US3] Compose fake/Resend sender and dispatcher in `backend/cmd/worker/main.go` with worker tests in `backend/cmd/worker/main_test.go`
- [ ] T024 [US3] Prove retry schedule, permanent failure, exhaustion, stale lease recovery, concurrency, and stable idempotency in `backend/internal/email/*_test.go`
- [ ] T025 [US3] Update deployment configuration examples and validation scripts in `.env.example`, `deploy/env/*.example`, and `deploy/scripts/validate-compose-config.sh`
- [ ] T026 [US3] Add operational delivery diagnosis/runbook content in `docs/launch/RUNBOOK.md`

## Phase 6: Validation and evidence

- [ ] T027 Run gofmt, Go build/vet/unit/integration/race gates, frontend install/lint/typecheck/unit/build/audit, and `git diff --check`
- [ ] T028 Run clean-code, test, documentation, and exposure guards; remediate all Critical/High findings
- [ ] T029 Record redacted repository and optional live-provider evidence in `docs/launch/evidence/s9/transactional-email.md`
- [ ] T030 Mark completed tasks truthfully, commit coherent batches, verify clean final HEAD, and freeze the exact independent review range

## Dependencies and execution order

- T001–T003 planning precedes implementation.
- T004–T008 are foundational and block dispatcher composition.
- T009/T011 may run independently; T010/T012 precede T013.
- T015–T021 depend on renderer/dispatcher foundations and preserve closed S6/S11 semantics.
- T022–T026 depend on the stable lifecycle.
- T027–T030 run only after all implementation tasks are complete.

## Independent story tests

- **US1**: verification/reset complete from captured delivered links with replay/wrong/expired refusal.
- **US2**: staff invitation completes; Course invitation acceptance creates zero access until exact Admin Approval grant.
- **US3**: provider failures produce safe durable diagnosis, correct bounded retry, and production fail-closed composition.
