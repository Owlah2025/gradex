# Tasks: S9 — Transactional Email Delivery

**Input**: Design documents from `specs/010-transactional-email/`

**Tests**: Required by the feature specification and constitution.

## Phase 1: Planning and setup

- [x] T001 Reconcile repository authority and fixed eight-intent inventory in `specs/010-transactional-email/spec.md`
- [x] T002 Record Resend/provider-boundary decision in `docs/DECISIONS.md` and active S9 state in `docs/launch/STATUS.md`
- [x] T003 Validate `spec.md`, `plan.md`, and `tasks.md` consistency and commit the planning package

## Phase 2: Foundational delivery infrastructure

- [x] T004 [P] Add delivery/attempt migration and rollback in `backend/internal/db/migrations/0016_transactional_email.*.sql`
- [x] T005 [P] Add provider-neutral message/result/error types and fake in `backend/internal/email/message.go` and `backend/internal/email/fake.go`
- [x] T006 [P] Add protected outbox payload decryption/read contract and privacy tests in `backend/internal/outbox/protected_payload.go` and `backend/internal/outbox/*_test.go`
- [x] T007 Add durable claim/complete/retry repository and integration tests in `backend/internal/email/repository.go` and `backend/internal/email/repository_integration_test.go`
- [x] T008 Add typed email mode/sender/timeout/retry configuration and fail-closed tests in `backend/internal/config/config.go` and `backend/internal/config/config_test.go`

## Phase 3: User Story 1 — Account verification and recovery

- [x] T009 [P] [US1] Write Arabic/English renderer contract tests for verification, reset, and reset-completed in `backend/internal/email/renderer_test.go`
- [x] T010 [US1] Implement fixed bilingual text/HTML templates and fragment link builder in `backend/internal/email/renderer.go`
- [x] T011 [P] [US1] Write Resend TLS/idempotency/timeout/error tests in `backend/internal/email/resend_test.go`
- [x] T012 [US1] Implement one-call Resend HTTPS adapter in `backend/internal/email/resend.go`
- [x] T013 [US1] Implement dispatcher validation, decrypt/render/send, bounded retry, and privacy behavior in `backend/internal/email/dispatcher.go`
- [x] T014 [US1] Add verification/reset deterministic delivery acceptance without helper extraction in `backend/internal/httpapi/journey_integration_test.go`

## Phase 4: User Story 2 — Staff and Course invitations

- [x] T015 [P] [US2] Add recipient Account-locale selection plus co-committed rejection/cancellation intents and tests in `backend/internal/access/repository.go` and `backend/internal/httpapi/access_routes_integration_test.go`
- [x] T016 [P] [US2] Add staff preview/completion frontend API functions and unit tests in `frontend/src/lib/api/identity.ts` and `frontend/src/lib/api/identity.test.ts`
- [x] T017 [P] [US2] Extend credential-fragment capture for Course invitations and tests in `frontend/src/lib/identity/validation.ts` and `frontend/src/lib/identity/fragment-token.test.ts`
- [x] T018 [US2] Add bilingual staff invitation acceptance component/route in `frontend/src/components/staff/staff-invitation-acceptance.tsx` and `frontend/src/app/staff/accept/page.tsx`
- [x] T019 [US2] Update Course access page to consume fragment credentials while preserving S6 authorization in `frontend/src/app/[locale]/access/page.tsx`
- [x] T020 [US2] Add Course invitation/grant and staff invitation renderer and acceptance cases in `backend/internal/email/renderer_test.go` and `backend/internal/httpapi/*integration_test.go`
- [x] T021 [US2] Prove delivered Course link creates zero access before approval and exact grant after approval by reusing S6/S11 backend and browser coverage

## Phase 5: User Story 3 — Operations and composition

- [x] T022 [P] [US3] Add safe transactional-email lifecycle logging and exposure tests in `backend/internal/logging/logging.go` and `backend/internal/logging/logging_test.go`
- [x] T023 [US3] Compose fake/Resend sender and dispatcher in `backend/cmd/worker/main.go` with worker tests in `backend/cmd/worker/main_test.go`
- [x] T024 [US3] Prove retry schedule, permanent failure, exhaustion, stale lease recovery, concurrency, and stable idempotency in `backend/internal/email/*_test.go`
- [x] T025 [US3] Update backend/deployment environment examples, Compose wiring, and configuration validation tests
- [x] T026 [US3] Add operational delivery diagnosis/runbook content in `docs/launch/RUNBOOK.md`

## Phase 6: Validation and evidence

- [x] T027 Run gofmt, Go build/vet/unit/integration/race gates, frontend install/lint/typecheck/unit/build/audit, and `git diff --check`
- [x] T028 Run clean-code, test, documentation, and exposure guards; remediate all Critical/High findings
- [x] T029 Record redacted repository and optional live-provider evidence in `docs/launch/evidence/s9/transactional-email.md`
- [x] T030 Mark completed tasks truthfully, commit coherent batches, verify clean final HEAD, and freeze the exact independent review range

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
