# S12 Batch H evidence — production-like staging smoke

Date: 2026-08-08

Batch base: `3ef16b6`

## Environment

The smoke ran against the separately deployed production frontend, API, and worker through the Caddy
HTTPS edge at `https://gradex.localhost:18443`. PostgreSQL, Redis, and private MinIO remained on the
internal application network. Test authority lived in the separate safety-gated database
`gradex_playwright_e2e_s12smoke01`; the active `gradex` database was neither reset nor downgraded.

The existing seed/query utility accepts only local/test database hosts. Rather than weakening that
guard, the harness used pinned `alpine/socat:1.8.0.3` image digest
`sha256:beb4a68d9e4fe6b0f21ea774a0fde6c31f580dde6368939ed70100c5385b015e` as a disposable PostgreSQL
tunnel bound only to `127.0.0.1:15432`. It was removed on exit. Chromium trusted only the exact live
leaf-certificate SPKI; Node's Playwright request client used the extracted internal CA. The separate
TLS/security proof remained authoritative for certificate, redirect, cookie, proxy, CORS, and CSRF
behavior.

## Deployed journey

The Playwright harness now supports an explicit credential-free HTTPS `GRADEX_E2E_EXTERNAL_ORIGIN`.
In this mode it starts no development server, no local API, and no local media server. The existing
S5/S6 specs ran unchanged at their UI/API boundaries, with added database counts that prove the grant
cardinality instead of inferring it from a found row.

```text
Running 2 tests using 1 worker
✓ S5 Production-like Playwright Infrastructure Smoke Test › authenticates real Student via Go API session and renders Course Home from real PostgreSQL
✓ S6 Course Access Grant — Real Production Launch Journey › Complete 30-Step End-to-End Course Access Grant & Protected Learning Journey
2 passed (4.6s)
```

The exercised path was:

```text
real HTTP login
→ Admin Course access-expiry configuration
→ Admin invitation creation
→ intended Student acceptance
→ database count: 0 Entitlements, 0 Enrollments
→ protected Course denial before approval
→ Admin Approval
→ database count: exactly 1 ACTIVE invitation-sourced Entitlement and 1 Enrollment
→ protected Course and Lesson
→ playback issuance
→ Progress write
→ unrelated/expired Student denial
→ repeated approval remains cardinality-safe
```

After Playwright, a separate database assertion returned `1|1|1|1` for approved target invitation,
ACTIVE provenance-bearing Entitlement, Enrollment, and Progress at or beyond 15 seconds. The harness
then issued another real production session, fetched the protected same-origin manifest through the
HTTPS edge, and fetched its non-empty signed HLS segment directly from private MinIO. Anonymous
private-object refusal and unrelated-Student refusal were already re-proven by Batch E's same image
path.

The final command result was:

```text
./deploy/scripts/verify-staging-smoke.sh
s12-staging-smoke: HTTPS S6 invitation-to-learning journey, zero-before-approval, exact grant counts, progress, unrelated denial, and protected media passed
s12-staging-smoke: production-like origin=https://gradex.localhost:18443 database=gradex_playwright_e2e_s12smoke01 state=1|1|1|1
```

## Evidence boundary

This is a real production-configuration disposable deployment on loopback, not a public cloud staging
URL. Public DNS, a publicly trusted certificate, provider-managed PostgreSQL/Redis/object storage,
browser storage CORS, and execution from an external network remain unproven until provider access is
supplied. Student registration is disabled by the current production-like policy contract; the real
HTTP login path and the full current invitation-to-protected-learning MVP path are proven.
