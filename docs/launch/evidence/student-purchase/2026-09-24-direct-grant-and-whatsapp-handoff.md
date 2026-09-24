# Direct Course access on payment confirmation, and the WhatsApp handoff

**Date:** 2026-09-24
**Branch:** `release/student-ui-email-polish-20260918`
**Starting HEAD:** `f60639b9754ec3e35a7b1e8a795bfcc43547e2f1` (clean tree)
**Decisions:** [D-113](../../../DECISIONS.md#d-113--admin-payment-confirmation-grants-course-access-directly),
[D-114](../../../DECISIONS.md#d-114--production-sales-whatsapp-handoff-number-and-separate-browsing-context)

## What changed

Two Student-journey changes, authorized together by the Product Owner on 2026-09-24.

1. The purchase handoff opens WhatsApp in a separate browsing context instead of replacing the
   Gradex page, and production moves to the real sales number.
2. Admin payment confirmation grants Course access directly. The invitation the Student used to
   have to accept is gone from the purchase flow.

## Production state observed before the work (read-only)

Observed on the live host, `docker inspect` and `SELECT` only. No mutation.

| Fact | Value |
| --- | --- |
| Running images | `gradex-backend:device43-bb9d71b645fc`, `gradex-frontend:device43-bb9d71b645fc` |
| Schema version | 43, clean |
| `SALES_WHATSAPP_NUMBER` | `201090358799` (developer test number) |
| COURSE purchase requests | 14, all `ACCESS_GRANTED` |
| Requests awaiting confirmation | 0 (`WAITING_PAYMENT` and `INVITATION_CREATED` both zero) |
| Course access invitations | 14, all `APPROVED`; zero pending |
| Entitlements | 11 `PURCHASE_REQUEST`/`ACTIVE`, 3 `PURCHASE_REQUEST`/`REVOKED` |
| Purchase requests with NULL `requester_account_id` | 0 |

Nothing was in flight, so no data migration is required and no Student is stranded mid-flow.
This must be re-observed immediately before any production change.

## Verification performed

| Check | Result |
| --- | --- |
| `backend: make ci` (gofmt, build, vet, vet -tags=integration, `go test -race ./...`, docs-guard, expose-guard) | PASS |
| `backend: go test -tags=integration ./...` | PASS, 0 failures |
| Migration 0044 up on a fresh database | version 44, dirty=false |
| Migration 0044 down with no direct-grant rows | returns to 43, schema-43 rule restored |
| Migration 0044 down with direct-grant rows | refused, `cannot roll back 0044` |
| Constraints after 0044 | all three present and `convalidated = true` |
| `frontend: tsc --noEmit` | PASS |
| `frontend: npm run lint` | PASS |
| `frontend: npm test` | 803 pass, 0 fail |
| `frontend: npm run build` | PASS |

## Zero-downtime deployment: mechanism proven, tooling absent

The Product Owner required zero user-visible downtime and instructed that deployment stop if it
cannot be proven. It cannot be proven with the tooling that exists today.

**Why the ordering is forced.** `db.CheckSchemaAtLeast` fails closed when
`state.Version > MaxSchemaVersion`. The schema-43 application in production therefore reports **not
ready** the instant schema 44 commits, and `/readyz` is publicly routed
(`@api path /api/* /healthz /readyz`). Migrating before the application is replaced would make a
public endpoint return 503, which is exactly what the availability condition forbids. So the
application accepting 43..44 must be serving *before* the migration runs.

**Why the application swap cannot be done safely today.** `host.sh apply_release` performs:

```
compose up --detach --no-deps --force-recreate api worker frontend
```

`--force-recreate` stops the only `api` and `frontend` containers and starts replacements. Caddy
proxies to the service names `api:8080` and `frontend:3000` with no health checking and no second
upstream, so between stop and healthy there is nothing to serve. That is a real outage window, not
a "brief restart". No blue/green, scaling, or alternate-upstream path exists in the tooling.

**What was proven.** The underlying failover mechanism does work. An isolated Caddy 2.10.2 stack was
built with two containers sharing one Compose service alias, driven by 6 concurrent keepalive
clients, with one container killed mid-run:

```
client1 OK=6695 FAIL=0
client2 OK=6700 FAIL=0
client3 OK=6705 FAIL=0
client4 OK=6705 FAIL=0
client5 OK=6695 FAIL=0
client6 OK=6705 FAIL=0
```

40,205 requests, zero failures. Docker DNS returns both A records and Go's dialer falls through to
the live address, so removing one of two containers sharing an alias loses no requests. A single
sequential run stopping the *other* container gave the same result (3,057 requests, 0 failures).

**What is still missing.** Compose cannot hold old and new images under one service name at the same
time — a changed image recreates every container of that service. Executing the proven mechanism
therefore needs a second aliased service and a corresponding change to `apply_release`, which is new
deployment tooling: unwritten, unrehearsed against the real stack, and guarded against by the
existing `assert_production_project_scope` and `require_no_local_production_workers` checks that
assert a single known topology. Deployment tooling is also outside the change scope the Product
Owner set.

**Recommended sequence, once that tooling exists and has been reviewed:**

1. Add an `api_next`/`frontend_next` service aliased to `api`/`frontend`, on the new images.
2. Bring them up; wait for health. Two instances now serve.
3. Remove the old containers. Proven above to lose no requests.
4. Migrate 43 → 44. The serving application already accepts 44, so `/readyz` never fails.
5. Rename `api_next` back to `api` at the next quiet moment.

Between steps 3 and 4 an Admin confirming a COURSE payment would receive a 500 and no state change,
because the new grant shape is not yet representable. The transaction rolls back whole, and
production currently has zero requests awaiting confirmation, so the window is empty in practice.

## Outcome

Implementation, migration, tests, and documentation are complete. Production deployment is stopped
pending a reviewed zero-downtime mechanism.
