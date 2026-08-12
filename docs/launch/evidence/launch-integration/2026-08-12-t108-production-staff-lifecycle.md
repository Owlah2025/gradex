# T108 production-composed staff lifecycle evidence — 2026-08-12

## Scope

This evidence closes only S1C remediation task T108 for C4. It exercises the real production router,
not development staff composition or fake authentication.

## Deterministic production dependencies

- PostgreSQL uses the established repository integration service and fresh run schema.
- Redis uses the run-owned authenticated TLS fixture committed in `5455adb`, with an ephemeral CA,
  localhost SAN, password, TLS 1.2+, normal CA verification through `REDIS_TLS_CA_CERT_FILE`, and
  deterministic cleanup.
- `APP_ENV=production`, `AUTH_FAKE_MODE=false`, and `PASSWORD_SCREEN_MODE=adapter` are loaded through
  the normal configuration path.
- The only explicit test seam is the S1C-authorized Go-level staff compromised-password source factory.
  Normal production still selects `identity.NewRuntimeCompromisedSource`; no environment value selects
  the injected deterministic source.
- The production-valid Resend configuration constructs the ordinary durable transactional-email boundary;
  the test never delivers to Resend.

## HTTP journey and evidence

`TestT108ProductionStaffLifecycle` authenticates seeded Admin, Student, and stale-Admin identities
through `/api/v1/session/bootstrap` and `/api/v1/sessions`, preserving session cookies, Origin, and CSRF.
It then proves:

1. Only a fresh Admin creates an Instructor invitation; anonymous, Student, Instructor, and stale Admin
   callers are refused.
2. The safe invitation response has no bearer, token, secret, or secret-bearing URL. The database holds
   exactly one `identity.staff_invitation_created` outbox event and corresponding
   `STAFF_INVITATION_CREATED` identity-security event.
3. The test claims and decrypts the action only through the authorized transactional-email outbox seam,
   previews it through the public route, completes it once, and proves replay and malformed actions fail.
4. The injected adapter accepts the strong onboarding password and rejects a non-common compromised
   canary through the same invitation-completion endpoint while retaining the pending invitation.
5. The newly created account remains `INSTRUCTOR`; the real authoring `GET /api/v1/courses` route allows
   it and refuses Student and anonymous callers.
6. The Admin-only `GET /api/v1/staff-invitations/instructors` safe operational projection contains the
   account and refuses anonymous, Student, and Instructor callers.
7. Admin suspension invalidates the already-open Instructor session. Reinstatement does not revive that
   old session; a new real login restores permitted Instructor access without role mutation.

The test also asserts real production session/staff Redis foundations and observes rate-limit decisions
from the mounted router. The committed TLS fixture separately proves trusted TLS/auth success and
untrusted-certificate refusal.

## Focused commands

```text
cd backend && go test ./cmd/api -run 'Test(ProductionStaffCompositionFailsClosedOnNamedPrerequisites|ProductionCompositionSelectsHIBPAndApprovedPolicySet)' -count=1
cd backend && go test -tags=integration ./cmd/api -run 'Test(T108ProductionStaffLifecycle|TLSRedisFixtureUsesVerifiedTLSAndAuthentication)' -count=1
```

Both commands passed on 2026-08-12.

## Batch C regression results

The following commands passed:

```text
cd backend && go test ./...
cd backend && go vet ./...
cd backend && go test -tags=integration ./cmd/api -count=1
cd backend && go test -tags=integration ./internal/httpapi -run 'Test(SessionHTTPLifecycleStoresOnlyDigestsAndRevokesReuse|SessionLogoutRevokesBeforeCookieClear|PasswordChangeRevokesTheAccountsOtherSessions|FreshAdminSucceedsAndStaleAdminIsRefusedOnStaffEndpoints|StaffInvitationRoutesDenyInstructorAndStudent|InvitationCreateDoesNotReturnActionBearer|SuspensionRoutesBoundTheirRequestBodies)$' -count=1
cd backend && go build -tags=production ./cmd/api ./cmd/worker
cd frontend && npm run typecheck
cd frontend && npm run lint
cd frontend && npm run test
cd frontend && npm run build:clean
cd frontend && npx playwright test e2e/s13-mandatory-password-change.spec.ts --workers=1
scripts/docs-guard.sh
git diff --check
```

The mandatory-password Playwright journey passed both its bootstrap-Admin and restricted-Instructor
cases and removed its isolated database during global teardown.

One broader, unrelated command was also attempted:

```text
cd backend && go test -tags=integration ./internal/identity ./internal/httpapi -count=1
cd backend && go test -tags=integration ./internal/httpapi -count=1
```

The combined command proved the identity integration package green; before the conventional integration
Redis was started, the HTTP API package also reported its expected missing-Redis failures. With Redis
available, the standalone HTTP API package's staff/session and Redis-dependent tests passed, but it still
failed at the pre-existing, out-of-scope
`TestProductionPrivilegedMutationRoutesCommitAuditEvidence` Admin review-preview case: that case expected
HTTP 200 and received HTTP 404. T108 changes no Admin review or media-preview production behavior; the
focused Batch C HTTP API integration set above passed.
