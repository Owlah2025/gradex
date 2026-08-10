# Staff invitation composition — founder manual-acceptance remediation

**Date:** 2026-08-10
**Branch:** `instructor-authoring-ui-20260810`
**Starting HEAD:** `5f21c6185fb5ca1f3c6fd33f423e0636c96995bc`
**Status:** Evidence only. This does not close a slice or a gate, and the range remains subject to
independent review. It also does **not** unblock production staff onboarding — see §6.

## The finding, as observed

The Admin UI's "Invite Staff Member" action calls `POST /api/v1/staff-invitations`. The frontend call
was correct. The running development API answered:

```
route_template="unmatched"
status=404
safe_error_code="NOT_FOUND"
```

Every part of the backend existed: `identity.StaffService` with `CreateStaffInvitation`,
`PreviewStaffInvitation`, `CompleteStaffInvitation`, `RevokeStaffInvitation`,
`ListPendingInvitations`; the staff HTTP handlers; `mountStaffRoutes`; `WithStaffFoundation`; and
authorization tests for the exact routes. Nothing composed them at runtime.

## Root cause

`backend/cmd/api/main.go`, in `buildProductionFoundations`:

```go
if cfg.Admission().Enabled() && cfg.Environment() == config.EnvDevelopment {
    foundation, limiterClient, err := buildStaffFoundation(...)
    ...
    pf.Options = append(pf.Options, httpapi.WithStaffFoundation(foundation))
}
```

`cfg.Admission().Enabled()` is `STUDENT_REGISTRATION_ENABLED`. Staff lifecycle — an Admin
capability — was therefore coupled to public Student registration. This environment intentionally
runs `STUDENT_REGISTRATION_ENABLED=false`, so no staff foundation was composed, `routerConfig.staff`
was `nil`, `mountStaffRoutes` never ran, and every `/api/v1/staff-invitations*` route was absent from
the router. The 404 was accurate: the route did not exist.

The two admissions are independent. Closing public Student registration must not remove the Admin
surface that creates Instructors.

## The composition change

One condition, in `buildProductionFoundations`:

```go
-if cfg.Admission().Enabled() && cfg.Environment() == config.EnvDevelopment {
+if cfg.Environment() == config.EnvDevelopment && cfg.Sessions().Enabled() {
```

- The Student-registration term is gone. That was the defect.
- The environment term is kept: production composition remains refused (§6).
- A sessions term replaces it, because it is a real dependency rather than an unrelated one. Staff
  mutations carry the S1B2 session and CSRF boundary, and `httpapi.NewRouter` already refuses to
  build a staff surface without a session foundation
  (`internal/httpapi/router.go`: "staff foundation requires a session foundation"). Composing staff
  with sessions off would stop the API from starting rather than mount anything.

Nothing else moved. No domain semantics, no route chain, no capability, no rate-limit policy, no
outbox usage, and no auth, recent-auth, CSRF, or password-screening behaviour changed.

`buildStaffFoundation` gained no logic. Its error is now wrapped as `composing staff lifecycle: %w`
so a misconfigured dependency is diagnosable at startup instead of reappearing as a mystery 404.
Because enabled sessions already require `ADMISSION_LIMITER_HMAC_KEY` and the anonymous keys
(`internal/config/config.go`), and every composition already requires the protected outbox key, the
only staff dependency such an environment can still be missing is `PASSWORD_SCREEN_MODE`. It fails
closed: staff invitation completion sets a password, and screening it is not optional.

`backend/.env.example` is corrected accordingly — it claimed `ANONYMOUS_*`,
`ADMISSION_LIMITER_HMAC_KEY`, and `OUTBOX_PROTECTED_PAYLOAD_KEY` were needed only for Student
registration, which was already untrue for any environment with sessions enabled.

## Proof

`backend/cmd/api/main_test.go` (`-tags=integration`):

- **`TestDevelopmentStaffLifecycleMountsWithoutStudentRegistration`** — `APP_ENV=development`,
  sessions enabled, `STUDENT_REGISTRATION_ENABLED` absent (defaults false, asserted via
  `cfg.Admission().Enabled() == false`), staff dependencies configured. Asserts:
  - `pf.StaffRedis != nil` and `pf.AdmissionRedis == nil` — staff composed, Student admission not;
  - all five routes mounted: `GET`/`POST /api/v1/staff-invitations`,
    `DELETE /api/v1/staff-invitations/:id`, `GET /api/v1/staff-invitations/preview`,
    `POST /api/v1/staff-invitation-completions`;
  - Student admission routes still absent: `POST /api/v1/student-registrations`,
    `POST /api/v1/email-verification-requests`, `POST /api/v1/email-verifications`,
    `POST /api/v1/password-reset-requests`, `POST /api/v1/password-resets`,
    `GET /api/v1/registration-policy-set`;
  - `POST /api/v1/staff-invitations` and `DELETE /api/v1/staff-invitations/:id` still answer
    `403 ORIGIN_NOT_ALLOWED` with a missing or foreign origin, `403 CSRF_FAILED` with a missing or
    malformed CSRF token, and `401 AUTHENTICATION_REQUIRED` anonymously — and never `404`.
- **`TestDevelopmentStaffCompositionFailsClosedOnScreeningMode`** — with `PASSWORD_SCREEN_MODE`
  unavailable, `buildStaffFoundation` refuses rather than mounting a surface that cannot screen.
- **`TestDevelopmentStaffCompositionRequiresSessions`** — sessions disabled composes no staff
  foundation.
- **`TestProductionRegistrationFoundationsStartWithHIBPAndApprovedPolicySet`** (unchanged) still
  asserts `foundations.StaffRedis == nil` in production.

`backend/internal/httpapi/authorization_test.go`:

- **`TestStaffInvitationRoutesDenyInstructorAndStudent`** (new) — Instructor and Student are refused
  `403` on list, create, and revoke.
- `TestFreshAdminSucceedsAndStaleAdminIsRefusedOnStaffEndpoints` (existing) — recent-auth still
  enforced: a stale Admin session gets `403 NOT_AUTHORIZED` on create and revoke.
- `TestAuthorizationMatrixMatchesMountedRouter` (existing) — the matrix still classifies
  `POST`/`DELETE` staff invitations as `RECENT_AUTH_REQUIRED` and `GET` as `CAPABILITY_PROTECTED`,
  and no route was added or removed.

No new anonymous capability is introduced: the only anonymous staff routes remain the two the
contract already specified (`GET /api/v1/staff-invitations/preview` and
`POST /api/v1/staff-invitation-completions`), both bearer-token and rate-limited, unchanged.

## Commands run

```bash
cd backend
gofmt -l .
go build ./...
go vet ./...
go vet -tags=integration ./...
go test ./... -count=1
go test -tags=integration ./cmd/api/ -run 'TestDevelopmentStaff|TestProduction|TestBuildLearning' -count=1
git diff --check
```

All clean; all pass.

## 6. Production staff onboarding remains blocked

`buildStaffFoundation` still contains, deliberately untouched by this remediation:

```go
if cfg.Environment() != config.EnvDevelopment {
    return nil, nil, errors.New(
        "production staff admission composition remains unavailable pending launch approval",
    )
}
```

and `buildProductionFoundations` still composes staff lifecycle only in development.

**Consequence:** in staging and production there is no staff invitation, no staff onboarding, and no
Instructor invitation path. An Administrator cannot create an Instructor outside development. This is
a launch blocker in its own right and is untouched by this fix, which addresses only the development
composition defect that produced the 404.

Resolving it is a separate authorization and composition decision — approving production staff
admission, choosing the production compromised-password screening posture for staff completion, and
deciding the production limiter and outbox posture for those endpoints. It must be taken explicitly,
not by deleting the hard stop as a side effect of a bug fix.
