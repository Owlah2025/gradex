# S1B2 Verification Quickstart

## Prerequisites

- Use the repository's local PostgreSQL and Redis services.
- Configure local values from `backend/.env.example`; never commit local secrets.
- Run real authentication with the development-only deterministic password screen disabled.
- Seed one Active Account per role and private fixtures for unknown, wrong-password, unverified, and
  inactive login cases.

## Automated baseline and gates

From the repository root:

```bash
env GOCACHE=/tmp/gradex-go-cache make ci
npm --prefix frontend run typecheck
npm --prefix frontend run lint
npm --prefix frontend run build
```

Run the focused PostgreSQL integration suite with the repository integration-test command documented
by the implementation task, then rerun the full race and exposure gates. A passing unit-only run is
not sufficient evidence for S1B2.

## Contract verification

Verify with a browser or secret-safe API harness:

1. Sign in each role and inspect the cookie attributes: `Secure`, `HttpOnly`, host-only, `Path=/`,
   and `SameSite=Strict`.
2. Confirm the JSON contains the current CSRF token but no session credential, email, or hidden
   Account state.
3. Confirm PostgreSQL contains only credential and CSRF digests.
4. Reload through `GET /api/v1/session`; the credential and CSRF values remain unchanged and idle
   expiry is not extended.
5. Renew once; both values change and the old generation becomes `SUPERSEDED`.
6. Race two renewals from the same generation; exactly one succeeds.
7. Present the old credential to renewal; the entire family becomes
   `REVOKED/REUSE_DETECTED`, and the winner credential can no longer authenticate.
8. Sign in again and log out; verify family revocation committed before the clearing cookie and the
   next resolution/renewal fails.
9. Compare the four hidden login failures. Status, public headers, body size class, cookie behavior,
   and verification-path telemetry must be indistinguishable.

## Frontend verification

Inspect Arabic and English at phone, tablet, laptop, and desktop widths:

- keyboard-only sign-in and visible focus;
- correct RTL/LTR layout;
- submitting and retry behavior;
- generic login failure;
- session expired, replaced, and confirmed-reuse guidance;
- safe internal `returnTo` and role-root fallback;
- logout clears in-memory CSRF state;
- browser storage, URLs, console, analytics, and retained errors contain no authentication or CSRF
  values.

## Launch evidence

Record focused tests, full local gates, hosted CI URL/run, exact commit review range, independent
review findings, canary sweeps, and responsive bilingual inspection in
`docs/launch/daily/2026-07-31.md`. Do not mark S1B2 complete without that evidence.
