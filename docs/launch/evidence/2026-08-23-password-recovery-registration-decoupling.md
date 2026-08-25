# Password recovery / Student registration decoupling

Date: 2026-08-23

## Founder contract

Student self-registration, its policy route, and registration email-verification routes are closed when `STUDENT_REGISTRATION_ENABLED=false`. Password recovery remains available to eligible existing ADMIN, INSTRUCTOR, and STUDENT Accounts.

## Root cause and remediation

The disabled-registration composition selected `NewPurchaseAdmissionFoundation` plus `WithAdmissionSecurityFoundation`; its route option deliberately mounted no admission routes. `mountAdmissionRoutesWithBootstrap` coupled registration and recovery in one route block.

`RecoveryFoundation` now owns only `RecoveryService` and mounts `/password-reset-requests` plus `/password-resets` through the exact `AdmissionFoundation` anonymous security boundary. Student registration is mounted separately. Disabled registration composes the purchase boundary, real recovery dependencies, and `WithRecoveryFoundation`, without `WithAdmissionFoundation`.

## Security preserved

Recovery keeps anonymous cookie/CSRF middleware, endpoint limiting, privacy-equivalent reset acknowledgements, token validation and expiry, password screening, all-family session revocation, outbox events, and no automatic authenticated session.

## Focused proof

- `go test -tags=integration ./cmd/api -run TestDevelopmentStaffLifecycleMountsWithoutStudentRegistration -count=1` — PASS.
- `go test -tags=integration ./internal/httpapi -run TestRecoveryFoundationWorksForEligibleRolesWithRegistrationDisabled -count=1` — PASS.
- The latter uses migrated PostgreSQL; sends real recovery requests for ADMIN, INSTRUCTOR, and STUDENT; completes an ADMIN reset with a real token; verifies no session cookie; and proves Student registration returns 404.
- `TestPasswordResetRequestIsHTTPEquivalentAcrossAccountStates` — PASS, preserving enumeration resistance.
- `TestCompleteStudentAuthenticationJourney` — PASS, preserving the existing reset-completion journey.
- `TestProductionRouterWiringAndMutationSecurity` — PASS, including recovery mounted with registration enabled.

## Quality gates

- `go build ./...` — PASS.
- `go vet ./...` — PASS.
- `go vet -tags=integration ./...` — PASS.
- `go test ./...` — PASS.
- `npm run typecheck` — PASS.
- `npm test` — PASS (347 tests).

## Scope and safety

Frontend production code changes: NONE. No T5/T6 work was started. The pre-existing `deploy/scripts/environment.sh` change was preserved and untouched. `git diff --check` passed after the code changes.

## Production-like evidence

`./deploy/scripts/environment.sh build` and then `./deploy/scripts/environment.sh up` completed without a volume reset. `environment.sh status` reported healthy PostgreSQL, Redis, MinIO, API, and frontend; migrate exited successfully; worker and Edge were running.

Through `https://gradex.localhost:18443`:

- `GET /healthz` — `200 {"status":"ok"}`.
- `GET /readyz` — `200` with PostgreSQL, Redis, and schema checks all `ok`.
- Anonymous bootstrap — `200`.
- `POST /api/v1/password-reset-requests` for `you@example.com`, using that anonymous cookie and CSRF token — `202 PASSWORD_RESET_REQUEST_ACCEPTED`, not 404.
- `POST /api/v1/student-registrations` — `404 NOT_FOUND`; public Student registration remains closed.

The request created `identity.password_reset_requested` and its encrypted protected payload. The worker claimed the delivery and made one Resend attempt. The resulting delivery state is `PERMANENT_FAILED`, provider `resend`, failure class `provider_rejected`, provider code `validation_error`. Worker telemetry records the attempt and permanent failure without a reset bearer or protected payload. The configured local Resend credential/provider therefore cannot deliver to a local mailbox; no repository-supported safe recovery-link proof mechanism was available, so no reset completion, Admin login, or Academic Catalog browser proof was attempted through an unsupported workaround.

### Local-delivery configuration audit

The repository has a real `mailpit` sender, selected by the worker through the normal transactional-email dispatcher. It uses `EMAIL_SMTP_ADDR` and is restricted to a loopback IP:port. The development Compose topology owns Mailpit (`127.0.0.1:1025` SMTP and `http://127.0.0.1:8025` UI), and its documented use is development-only.

The s12 production-like topology intentionally sets `APP_ENV=production`, requires `EMAIL_PROVIDER=resend`, and config validation rejects `mailpit` outside development. Its default sender is `no-reply@gradex.localhost`; the production-like compose file documents that no provider verifies this disposable domain. Resend returns only the sanitised `validation_error` code, not a response body. Changing s12 to Mailpit would weaken the repository's explicit production-like provider guard, so it was not performed. A real mailbox completion therefore requires either the documented LG-018 verified-domain Resend configuration or an explicitly authorised development-Mailpit acceptance run; neither is an application-code change.

The recovery UI is unchanged: `RecoveryRequestForm` calls `requestPasswordReset`, and a successful `202` sets its accepted state rather than its generic error state. Browser automation was unavailable in this environment, but the actual Edge `202` exercised the exact API call the form makes.

## Canonical regression

`cd frontend && npx playwright test --workers=1` ran uncontended against its isolated database. Result: 142 passed, 3 did not run, 6 failed in 10.0 minutes. The failures match the accepted baseline exactly:

- `s5-expired-entitlement.spec.ts:712`
- `s5-playback-performance.spec.ts:157` (the remaining viewport cases were not run after the first failure)
- `s5-viewport-evidence.spec.ts:223` at phone, tablet, laptop, and desktop

No additional deterministic failure identity appeared.
