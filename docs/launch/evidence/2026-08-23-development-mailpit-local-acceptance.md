# Development Mailpit local acceptance

Date: 2026-08-23

## Scope and isolation

This is a separate development-only acceptance environment. It uses the existing
`backend/docker-compose.yml` dependencies, a new `gradex_mailpit_acceptance`
database, host-run API/worker, and the existing Mailpit service. The s12
production-like stack was not changed: it remains `APP_ENV=production` with
`EMAIL_PROVIDER=resend`.

## Runtime topology

- API: `http://127.0.0.1:18080`
- Frontend: `http://127.0.0.1:13000`
- Mailpit SMTP: `127.0.0.1:1025`
- Mailpit UI: `http://127.0.0.1:8025`
- Development configuration: `APP_ENV=development`, `AUTH_FAKE_MODE=false`,
  `EMAIL_PROVIDER=mailpit`, and `EMAIL_SMTP_ADDR=127.0.0.1:1025`.

The API `/readyz` returned 200 with PostgreSQL, Redis, and schema checks all
`ok`; the real worker reached `READY`.

## Admin recovery

`admin@gradex.local` was created only through `cmd/bootstrap-admin`. A real
forgot-password request returned 202, the worker delivered the reset message to
Mailpit, and the rendered Mailpit link was used to complete reset. Completion
returned 200, issued no session cookie, and a subsequent ordinary login returned
an ADMIN session. The protected Academic Catalog API and
`/en/admin/academic-catalog` frontend route both returned 200.

## Instructor lifecycle

The Admin created a real staff invitation for `instructor@gradex.local`. The
worker delivered it to Mailpit. Its rendered invitation link was previewed and
completed through the public invitation endpoint; normal Instructor login and
the protected courses API/frontend route returned 201/200 and 200 respectively.

## Student lifecycle

Development registration was explicitly enabled only for this separate
acceptance runtime. `student@gradex.local` registered, received a real Mailpit
verification message, consumed its rendered verification link, then logged in
normally as STUDENT. The protected dashboard API/frontend route returned 200.

## Email delivery ledger

Without querying protected payloads or reset/invitation bearers, the database
delivery ledger recorded these accepted Mailpit messages, each after one worker
attempt:

- `account-password-reset-v1`
- `account-password-reset-completed-v1`
- `staff-invitation-v1`
- `student-email-verification-v1`

No password, token, API key, or protected payload is retained in this record.

## Canonical Academic Catalog import

The same development acceptance database received the embedded manifest
`backend/internal/academic/manifest/data/kuwait-university/manifest.yaml`,
selected only by `kuwait-university-launch-v1`. Validation reported manifest
version `1.2.0`: 11 units, 5 Programs, 5 curricula, 84 Subjects, 112 mappings,
and 20 cited sources. Dry-run planned 218 creates with no update, noop, or
drift; apply produced those 218 creates.

The canonical `kuwait-university` institution now has 11 units, 5 Programs, 5
curricula, 84 Subjects, and 112 curriculum-Subject mappings. A repeated apply
produced `create=0 update=0 noop=218 drift=0`.

The Admin Academic Catalog API returned 200 and included Kuwait University.
The frontend `/en/admin/academic-catalog` route returned 200. The database also
contains one pre-existing empty institution row with slug `ku`; it predates this
import and has no units. It was preserved because the importer is slug-identity
based and this acceptance task authorized no manual deletion or merge.
