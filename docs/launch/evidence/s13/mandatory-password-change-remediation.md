# Mandatory password change — founder manual-acceptance remediation

**Date:** 2026-08-10
**Branch:** `instructor-authoring-ui-20260810`
**Starting HEAD:** `2a4008cfbf14dffd863e5bc23940866242b49630`
**Status:** Evidence only. This does not close a slice or a gate, and the range remains subject to
independent review.

## The finding, as observed

A founder manual test showed the bootstrap Administrator could authenticate but could do nothing:

- `POST /api/v1/sessions` → `201`
- `GET /api/v1/session` → `200`
- `GET /api/v1/courses` → `403`, `deny_reason = PASSWORD_CHANGE_REQUIRED`
- `GET /api/v1/taxonomy/terms` → `403`, `deny_reason = PASSWORD_CHANGE_REQUIRED`

## Root cause

`cmd/bootstrap-admin` deliberately creates the Administrator with credential state
`CHANGE_REQUIRED` and prints "the credential is CHANGE_REQUIRED; the first sign-in must change it".
`identity.Authorize` implements the matching §4.5 rule: a restricted principal is granted
`PASSWORD_CHANGE` and `SESSION_TERMINATE` and refused everything else.

The domain work to clear that state existed and was fully tested —
`internal/identity/password_change.go` (`PreparePasswordChange`, `CompletePasswordChange`) — but
**nothing called it**:

- no mounted HTTP route,
- no session field telling the browser it was restricted,
- no frontend screen, route, or redirect.

So the state the bootstrap command instructed the operator to leave was unreachable. `CHANGE_REQUIRED`
was terminal, and the platform had no usable Administrator.

Two smaller defects sat behind it and are fixed rather than worked around:

1. `CompletePasswordChange` minted the replacement session credential with `NewSessionCredential()`,
   which generates an independent random CSRF token. `SessionRepository.Resolve` re-derives each
   generation's CSRF token by HMAC over `(key, session, generation, credential digest)` and refuses
   a session whose stored digest does not match. A password change would therefore have committed
   and then made the very session it re-established unresolvable on the next page load. The
   replacement is now minted with `NewSessionCredentialForGeneration`, the same way login and
   renewal mint theirs.
2. `NewSessionFoundation` had no compromised-password source, because no route under it screened a
   password. It is now required, so the route that installs a long-lived credential cannot be
   mounted with screening absent.

## What changed

### Backend

- **Route:** `POST /api/v1/password-changes`, mounted in `mountSessionRoutes`. Its chain is
  strict JSON binding (2 KiB) → same-origin + session-cookie + session-CSRF → rate limiter
  (`password-changes`) → `requireAuth` → `requireCapability(CapPasswordChange)` → handler.
  The capability gate is the ordinary one: the policy already grants `PASSWORD_CHANGE` to a
  restricted principal, so no gate is bypassed and no exemption is added.
- **Application command:** `SessionRepository.ChangePassword` resolves the presented cookie to a
  session generation, proves the session CSRF token, and calls the existing
  `CompletePasswordChange` once. It adds no domain rule and duplicates none.
- **Current-password proof:** the HTTP boundary always sends `VoluntaryChange`, which the domain
  explicitly permits on a `CHANGE_REQUIRED` credential and which requires the current password.
  That is strictly stronger than `BootstrapMandatoryChange`, whose weaker precondition exists for a
  caller that cannot ask for the old password. A browser form can, so the stronger one is mounted.
- **Session representation:** `password_change_required` (derived boolean) added to the
  authenticated session response. No credential state enum, hash, or internal is exposed.

### Frontend

- `/password-change` — mandatory change screen with current password, new password, confirmation,
  submit, and a safe error message.
- Login routes a restricted principal there instead of into the application, carrying `returnTo`.
- `PasswordChangeGuard` bounces a restricted principal off privileged surfaces
  (`/staff`, `/instructor/*`, `/:locale/{admin,instructor,learn,access}/*`) onto that screen.
  Public pages and the identity screens are untouched, so signing out stays reachable.
- After a successful change: the rotated session is installed, then redirect by role —
  `ADMIN` → `/staff`, `INSTRUCTOR` → `/:locale/instructor/courses`, or the interrupted destination.

## Evidence

| Property | Where proved |
| --- | --- |
| Unauthenticated request refused | `TestPasswordChangeRouteRefusesAnUnauthenticatedCaller` |
| Restricted principal reaches this route and only this route | `TestRestrictedPrincipalReachesOnlyThePasswordChangeRoute`, `TestRestrictedBootstrapAdminIsDeniedOnRealProtectedRoutes` |
| Wrong current password refused, nothing mutated | `TestRefusedPasswordChangeLeavesTheAccountUntouched` |
| Weak password and password reuse refused | `TestRefusedPasswordChangeLeavesTheAccountUntouched` |
| Compromised password refused by screening | `TestCompromisedReplacementPasswordIsRefused` |
| Origin, CSRF, cookie, and body admission run before the repository | `TestPasswordChangeRefusesBadAdmissionBeforeTheRepository`, `TestPasswordChangeBodyBoundaryIsStrictAndBounded` |
| Credential reaches `ACTIVE`; old password stops and new password starts authenticating | `TestRestrictedAdminChangesItsPasswordAndBecomesActive` |
| Session rotates and the rotated session resolves | `TestRestrictedAdminChangesItsPasswordAndBecomesActive` |
| Every other session family revoked | `TestPasswordChangeRevokesTheAccountsOtherSessions` |
| Instructor follows the same lifecycle | `TestRestrictedInstructorFollowsTheSamePasswordChangeLifecycle` |
| No password in any response, error, or log | `TestPasswordChangeNeverEchoesEitherPassword`, step 11 of the manual walkthrough |
| Whole journey in a real browser | `frontend/e2e/s13-mandatory-password-change.spec.ts` |

## Manual acceptance — exact steps

The Product Owner can reproduce this without modifying any database row.

### 1. Bootstrap the Administrator

On an empty database (the command refuses to create a second Admin):

```bash
cd backend
BOOTSTRAP_ADMIN_PASSWORD='<a temporary passphrase of at least 15 characters>' \
  go run ./cmd/bootstrap-admin \
    -email admin@example.test \
    -display-name "Platform Administrator" \
    -operation-id 2026-08-10-launch \
    -principal "founder@local"
```

It prints `the credential is CHANGE_REQUIRED; the first sign-in must change it`.

### 2. Start the stack

```bash
cd backend && make up && make migrate-up && make run-api
cd frontend && npm run dev
```

### 3. Walk the browser journey

1. Open `/login` and sign in with the email and the temporary password.
2. The browser lands on **`/password-change`**, not on an application screen.
   (Before this change it landed in the Admin UI and every panel failed.)
3. Optionally confirm the restriction is still enforced: open `/staff` directly — it bounces back
   to `/password-change`.
4. Enter the temporary password as **Current password**, choose a new password of at least 15
   characters, repeat it in **Confirm new password**, and submit.
5. The browser lands on **`/staff`** with full Administrator authority.
6. Invite an Instructor from that screen — enter an email, choose **Instructor**, send.
7. Sign out and sign in again: the temporary password is refused, the new one works, and no
   mandatory screen appears.

### 4. Or walk it at the API

Executed on 2026-08-10 against an isolated scratch database, with these results:

| Step | Result |
| --- | --- |
| `POST /api/v1/sessions` with the temporary password | `201`, body `{"role":"ADMIN","password_change_required":true}` |
| `GET /api/v1/courses` before the change | `403` |
| `GET /api/v1/taxonomy/terms` before the change | `403` |
| `POST /api/v1/password-changes` with current + new password | `200`, body `{"role":"ADMIN","password_change_required":false}` |
| `GET /api/v1/courses` after the change | `200` |
| `GET /api/v1/taxonomy/terms` after the change | `200` |
| `POST /api/v1/staff-invitations` (invite an Instructor) | created |
| Login with the old password | `401` |
| Login with the new password | `201` |
| Password plaintext in the API log | none |

## Boundary

No authorization rule changed. `CHANGE_REQUIRED` is not disabled, bypassed, or set directly, the
credential state is cleared only through the existing domain operation, and no second
password-change implementation exists. Identity was not redesigned, S6 semantics are untouched, and
no profile, settings, or MFA feature was added.
