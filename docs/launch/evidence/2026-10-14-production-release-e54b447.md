# 2026-10-14 production release — D-117 WhatsApp contact reliability `e54b447` (schema 57 → 58)

Status: **DEPLOYED AND HEALTHY.** Public, health, data-integrity and logged-out checks green.
Authenticated feature smokes **PENDING MANUAL VERIFICATION** (no approved production smoke identity;
no real student account was used).

| Item | Value |
|---|---|
| Production URL | https://gradexcourses.com |
| Released SHA | `e54b4473cdcc46fe066819d928dcf8d6ca8f899e` (branch `feature/whatsapp-contact-reliability-20261013`) |
| Previous production SHA | `a38dbd81283c47019d0fcf2190150fcd0a6d19c3` |
| Approval | Product Owner, explicit, 2026-10-14 |
| Schema | before `57\|false` → after `58\|false` |
| Images | backend `gradex-backend:hostinger-e54b4473cdcc` (`sha256:cd46af95…`), frontend `…` (`sha256:d20aec1c…`), proof (`sha256:6044022d…`); revision labels = released SHA; host IDs = builder IDs |
| Live binary proof | running `gradex-api` sha256 `d3548ff30784dec6…` = image |
| Release staging | transfer + `import-release.sh` 04:49 → 05:08Z (non-serving) |
| Cutover | 05:08:56Z start (API/worker stopped) → 05:09:24Z migration start (recorded cutover time) → 05:09:41Z schema 58 → 05:09:48Z `verify-core` + `verify` passed. API outage ≈45 s; frontend, edge, PostgreSQL, Redis never restarted |
| Readiness evidence | [readiness gates](2026-10-13-whatsapp-contact-reliability.md#2026-10-14-readiness-gates-on-the-final-candidate-e54b4473cdcc46fe066819d928dcf8d6ca8f899e), [agy review](2026-10-14-d117-agy-review-e54b447.md), CI run 38502585963 |

## Preflight (04:49Z, read-only)

`runtime.env` selected `a38dbd8`; schema `57|false`; no `contact_phone` column; 0 active media claims; all six
services healthy; counts identical to the readiness report (`33|39|11|32|31|32|0|0|144|181`, 28 active
entitlements; purchase requests 31 `ACCESS_GRANTED`, 1 `CANCELLED`); 51 GB disk free, ≈6 GB memory available;
backup service last result success; artifact checksums and image IDs matched `e54b447`.

## Backups and recovery references

- Local pre-migration dump (API/worker stopped): `/home/deploy/gradex-production/backups/pre-e54b4473cdcc46fe066819d928dcf8d6ca8f899e-20261014T050858Z.dump`, 1 238 562 bytes, sha256 `8b07236a6f6392e99205be43850a69ac1b083e1bc266bbe7415ca97cf826350f`, `pg_restore --list` OK (72 table-data entries).
- Offsite encrypted snapshot taken during cutover: `a4d36b7831d1382ebf40d17521170e03bf974dc6f3985736d332abad40032f52` — **restored after cutover into the isolated `gradex-restore-verify` container**: restic "no errors were found", `verify-restore` passed, `57|false`, counts match.
- Prior selection: `runtime.env.before-e54b4473cdcc` (non-selection keys proven unchanged). Cutover time file `backups/cutover-e54b4473cdcc46fe066819d928dcf8d6ca8f899e.at` = `2026-10-14T05:09:24Z`. Cutover log `backups/cutover-e54b447.log`.
- Rollback (not executed; requires Product Owner approval): `/var/tmp/gradex-release-20261014-d117/03-rollback-e54b447.sh` — exports every number, request and Entitlement touched since the cutover time, then restores the dump into a new database and rename-swaps (nothing dropped).

## Production verification

| Check | Result |
|---|---|
| Schema / objects | `58\|false`; 3 contact columns; 0 unvalidated constraints; 0 invalid indexes; 0 disabled triggers; snapshot trigger enabled |
| Data integrity | all counts unchanged; purchase requests `ACCESS_GRANTED:31, CANCELLED:1`; Entitlements `PURCHASE_REQUEST/ACTIVE:28, /REVOKED:3`; enrollments 32 |
| Health | `/healthz` 200; `/readyz` 200 `{"postgres":"ok","redis":"ok","schema":"ok"}`; `verify-public.sh --mode cloudflare` passed (DNS, edge, certificate, frontend, health, readiness, headers) |
| Pages | `/`, `/login`, `/en/catalog`, `/ar/catalog`, Course pages EN/AR, `/en/learn/dashboard`, `/en/admin` shells 200; catalogue API 8 courses; live AR Course page at 375 px renders, RTL, purchase CTA present, no horizontal overflow |
| Authorization (logged out) | `/api/v1/me/profile`, `/api/v1/me/purchase-requests`, `/api/v1/admin/accounts?whatsapp=MISSING`, `/api/v1/admin/purchase-requests` → 401 |
| Authenticated Student/Admin smokes | **PENDING MANUAL VERIFICATION** — checklist `/var/tmp/gradex-release-20261014-d117/04-smoke-checklist.md` |

## Monitoring (05:09:24Z → 05:30:30Z, three snapshots)

0 restarts and 0 OOM on all six containers; 0 ERROR/FATAL/panic lines in api, worker, frontend; 162 API requests,
**0 5xx**; 4xx = 12 × 401 (logged-out probes), 1 × 404, 2 × 403 (one POST, the known CSRF bootstrap retry
pattern), 1 × 401 DELETE; **0 422** (no WhatsApp validation failures); 0 PostgreSQL errors; 0 new purchase
requests (so 0 without contact); payment and Entitlement state unchanged; 3 sign-ins, 1 sign-out. One real
Student saved a WhatsApp number on their own (`ACCOUNT_PROFILE_UPDATED`), so the new profile path is live.
Memory ≈6 GB available, disk 49 %.

## Unresolved risks / follow-ups

- Authenticated smokes (reminder, add/edit number, request with contact, success link, Admin chat link,
  pending recovery) need a human or an approved smoke identity.
- Real WhatsApp and real in-app browsers not exercised; first real purchase request should be checked for a
  non-NULL `contact_phone`.
- Rollback is a database restore swap; it requires reconciliation of anything written after 05:09:24Z.
- Reviewer model was `gemini-3.8-flash-high` (the standing `gemini-3.1-pro-high` is no longer offered by agy).
