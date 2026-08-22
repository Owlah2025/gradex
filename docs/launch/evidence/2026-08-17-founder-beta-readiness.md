# Founder Beta readiness — 2026-08-17

## Current public state

- `https://staging.gradex.network/` is publicly reachable and returned HTTP 200 during the
  read-only probe on 2026-08-17.
- `https://gradex.network/` returned Cloudflare HTTP 525 during the same probe; it is not a
  usable Founder-Beta URL.
- The reachable staging edge routes `/api/*`, `/healthz`, and `/readyz` to the isolated
  `gradex-founder-beta` Compose project. `/healthz` and `/readyz` returned HTTP 200 after the
  cutover.

## Founder-Beta cutover

The former LG-019 edge was stopped, but its API, worker, PostgreSQL, Redis, MinIO, fixtures, and
evidence remain preserved under the `gradex-lg019-target` project. Founder staging now uses a
separate project, volumes, containers, and PostgreSQL database:

- database: `gradex_founder_beta` (schema version 20, clean); the former
  `gradex_playwright_e2e_founder_beta_20260817` database remains present but unreferenced;
- initial account count: `0` (no acceptance accounts or load-test sessions copied); and
- registration policy endpoint: `GET /api/v1/registration-policy-set` returned HTTP 200 with
  `Accept-Language: en` after anonymous bootstrap.

## Registration-policy root cause and remediation

Before the cutover, the browser's `GET /api/v1/registration-policy-set` returned HTTP 404 with
problem code `NOT_FOUND`. `STUDENT_REGISTRATION_ENABLED=false` prevented the admission foundation
and its routes from being composed; the request never reached legal-policy resolution. The
frontend therefore displayed “The current terms could not be loaded.”

The isolated Beta configuration enables registration and selects the repository's approved
2026-08-09 policy-set identifier while retaining controlled-staging legal sentinels and normal
CSRF/session/password-screening configuration.

## Why the former target is not a Founder-Beta environment

The live Compose configuration identifies itself as an LG-019 KVM2 acceptance target and states
that it is not a Founder Acceptance or release topology. Its current settings include:

- `EMAIL_ENABLED=false`;
- `STUDENT_REGISTRATION_ENABLED=false`;
- controlled-staging legal identity values; and
- acceptance-only runtime fixture/session retention.

The former acceptance target must not be relabelled or populated with real catalogue data.

The protected prior Gradex runtime contains no Resend API key, sender address, or reply-to setting.
The Beta runtime therefore has no approved Resend credential/sender to provision. No initial
Admin/Instructor credentials have been created because the supported bootstrap command requires a
human-supplied bootstrap password. Normal email-backed registration and the remaining role journey
stop at those provider/credential boundaries; no fake email or acceptance identity is substituted.

## Capacity evidence retained

The original canonical results remain unchanged:

- canonical 250 application RPS: FAIL;
- previous 100-RPS run: FAIL because of transport failures;
- 75, 50, 40, 30, 20, and 10 RPS public characterization runs remain retained as evidence;
- no tested rate satisfied the complete zero-failure and p95-under-300-ms contract twice;
- 180 real student logins per minute remains valid PASS evidence;
- 500 real logins per minute remains failed canonical evidence and a future-scale target.

No numeric First-Year public API RPS envelope is asserted by this record. LG-019 remains OPEN.

## Operational state

The VPS stack was healthy during inspection (API, frontend, edge, PostgreSQL, Redis, MinIO, and
worker containers running). The expected `gradex-backup.timer` and `gradex-monitor.timer` units
were not installed or active at inspection time. Installing them requires the authorized
interactive root step; no credentials are recorded here.

## Minimum work remaining

1. An approved Resend staging credential/sender must be supplied and configured without exposing
   the API key; normal email-backed Student registration remains blocked until then.
2. Initial Founder-Beta Admin and Instructor credentials must be established through approved
   bootstrap/invitation mechanisms.
3. A beta-safe runtime configuration must provide approved transactional email and normal Student
   registration/invitation journeys without exposing acceptance fixtures.
4. Founder-approved non-commercial beta catalogue data and test identities must be created through
   the human-facing Admin/Instructor workflows.
5. Backup and monitoring units must be installed, enabled, and freshness/alert checks retained.
6. LG-019, commercial catalogue/pricing, legal/provider, email, monitoring, and off-site recovery
   gates remain visible and unresolved; Founder Beta is not commercial launch readiness.
