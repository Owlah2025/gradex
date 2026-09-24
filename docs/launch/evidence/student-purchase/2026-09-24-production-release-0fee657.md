# Production release 0fee657 — direct Course access grant and WhatsApp handoff

**Date:** 2026-09-24
**Release SHA:** `0fee657897c939cb679c9d804d184542bb2f692f`
**Previous release:** `bb9d71b645fc1afbcf3666c5035c6b8396536ff2`
**Schema:** 43 → 44
**Decisions:** [D-113](../../../DECISIONS.md#d-113--admin-payment-confirmation-grants-course-access-directly),
[D-114](../../../DECISIONS.md#d-114--production-sales-whatsapp-handoff-number-and-separate-browsing-context)

A short planned maintenance window was authorized by the Product Owner; the earlier zero-downtime
requirement was withdrawn. No blue/green tooling, parallel services, Caddy changes, or compatibility
release were introduced.

## Maintenance window

| Event | UTC |
| --- | --- |
| Window start | 2026-09-24T06:12:13Z |
| Edge stopped | 2026-09-24T06:12:14Z |
| Application writers removed | 2026-09-24T06:12:41Z |
| Migration 0044 applied | 2026-09-24T06:13:14Z – 06:13:15Z |
| Release applied, API healthy | 2026-09-24T06:14:18Z – 06:14:33Z |
| Edge started | 2026-09-24T06:15:43Z |

**User-visible downtime: 3 minutes 29 seconds** (06:12:14Z → 06:15:43Z).

## Gates

| Gate | Result |
| --- | --- |
| Builder HEAD clean at `0fee657` | PASS |
| Preflight: runtime `bb9d71b`, schema `43|false` | PASS |
| Preflight: all services healthy, restart counts 0 | PASS |
| Fresh encrypted offsite backup `23a3a61c…` | PASS |
| Backup schema sidecar | `43|false` |
| Deep repository integrity (63 snapshots, 15 packs) | no errors |
| Isolated restore of `23a3a61c…` + invariant verification | PASS |
| Artifact image IDs match manifest, revision labels `0fee657…` | PASS |
| Backend schema ceiling | 44 |
| Pre-migration predicates (existing rows satisfy widened constraints) | 0 violations |
| Post-migration marker | `44|false` |
| Constraints installed and validated | 3 of 3, `convalidated = true` |
| Internal smoke: healthz, readyz, schema | ok |
| Public smoke: `/`, `/healthz`, `/readyz` | 200 / 200 / 200 |
| Packaged `host.sh verify` | passed on clean schema 44 |

## Row-level outcome

Counts were identical before and after the release; nothing was rewritten, backfilled, or deleted.

```
purchase_requests    COURSE / ACCESS_GRANTED   14
course_access_invitations  APPROVED            14
entitlements  PURCHASE_REQUEST / ACTIVE        11
entitlements  PURCHASE_REQUEST / REVOKED        3
historical invitation-backed entitlements      14
direct-grant entitlements                       0
invalid provenance                              0
duplicate active Student/Course entitlements    0
```

All 14 historical invitation-backed Entitlements remain valid under the widened constraints, which
is the backward-compatibility claim this release had to hold.

## Feature verification

**WhatsApp.** Running API config carries `SALES_WHATSAPP_NUMBER=201503260733`; the prior developer
number `201090358799` is absent from the container environment and from the persisted runtime file.
Only that one line changed in `runtime.env` (verified by diff against `runtime.env.before-0fee657897c9`).
Both shipped purchase chunks — the Course detail page and the Bundle detail page — contain the
blank-context open, the severed opener, the server `whatsapp_url`, and the `noopener noreferrer`
fallback, and contain zero occurrences of `location.assign`.

**Admin copy.** The shipped bundle contains "Confirm payment & grant access" and
"تأكيد الدفع ومنح الوصول". The previous "Confirm payment & send invitation" and
"تأكيد الدفع وإرسال الدعوة" are absent from `/app/.next` entirely.

**Direct grant.** `POST /api/v1/admin/purchase-requests/:id/confirm-payment` answers 403 to an
unauthenticated probe, proving the route is mounted and still capability-guarded. The installed
`ent_purchase_source_valid` definition permits the direct shape. No end-to-end grant was executed in
production: there were zero requests awaiting confirmation, no designated production smoke fixture
exists, and manufacturing a customer transaction to prove a deployment is not acceptable. The
behaviour rests on the independently approved integration and Playwright evidence recorded with the
implementation. Row counts confirm the smoke probes mutated nothing.

## Stabilization

Ten minutes after edge reopen: zero ERROR/FATAL in API and worker logs, zero 5xx at the edge,
restart counts 0 across all six containers, 71 × 200 responses observed.

## Rollback

None performed. The recovery point is snapshot `23a3a61c…` at `43|false`. Note that `bb9d71b` has a
schema ceiling of 43 and will refuse schema 44; it is not a valid rollback application while the
database is at 44.
