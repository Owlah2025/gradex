# 2026-10-13 — WhatsApp contact and course-request reliability (D-117), release candidate

Status: **IMPLEMENTED AND TESTED LOCALLY — NOT DEPLOYED.** Production is untouched. Independent review
is required before deployment ([D-117](../../DECISIONS.md#d-117--every-purchase-request-carries-a-reachable-student-whatsapp-number-and-the-handoff-is-an-explicit-link)).

| Item | Value |
|---|---|
| Branch | `feature/whatsapp-contact-reliability-20261013` |
| Base | `a38dbd81283c47019d0fcf2190150fcd0a6d19c3` (production, schema 57, verified read-only 2026-10-13) |
| Schema | 57 → 58 (expand-only migration `0058_student_contact_phone`) |
| API schema range | 58..58 (was 57..57) |
| New environment variables | none |
| `deploy/` changes | none (`git diff a38dbd8 -- deploy` empty) |
| New dependencies | `github.com/nyaruka/phonenumbers` v1.8.1 (Go), `libphonenumber-js` 1.13.15 (frontend, `mobile` metadata) |

## Root cause

**Confirmed, from code:**

1. **Gradex never stored a phone number.** No column, API field, or UI anywhere held one. The only way the
   sales team could reach a Student about a persisted request was for the Student to send the WhatsApp
   message themselves. Any failure between "request saved" and "message sent" left a request nobody could
   follow up.
2. **The handoff depended on a scripted window.** `purchase-action.tsx` / `bundle-detail.tsx` (D-114) called
   `window.open("", "_blank")` inside the click and pointed it at `wa.me` after the request resolved. When
   the browser returned a handle, the panel showed **nothing at all** afterwards — no success message, no
   reference, no link — so if that window did not actually reach WhatsApp (in-app webviews such as
   Instagram/Snapchat/TikTok, iOS app-switching from a script-opened tab, a window closed by the Student),
   the Student had no confirmation and no way back. Only a `null` handle produced a fallback link.
3. **No recovery path.** A pending Course request was not shown anywhere: the Course page offered a fresh
   confirmation, the Student access page listed invitations and entitlements only, and the Bundle page showed
   "pending" without a WhatsApp link.
4. **`reused` was always `true`** in `POST /me/purchase-requests`: the handler compared the nanosecond clock
   with the microsecond `requested_at` PostgreSQL returned. Fixed by returning the insert's own `xmax = 0`
   answer.

**Plausible, not provable from repository or production data:** which specific browsers failed. There is no
client telemetry of handoff outcomes. Read-only production aggregates (2026-10-13): 33 Students, 32 purchase
requests (31 `ACCESS_GRANTED`, 1 `CANCELLED` after 24.4 h, **0 `WAITING_PAYMENT`**); request → payment
confirmation p50 0.7 h, p90 10 h, max 224.9 h. The long tail and the cancelled request are consistent with
Students who could not be reached, but the data cannot attribute them.

**Ruled out:** a malformed URL (the backend builds `https://wa.me/<digits>?text=…` from configuration; the
URL decodes correctly). Minor hardening: spaces are now `%20` rather than `+`, which some WhatsApp clients
render literally.

## What changed

- **Backend:** `identity.NormalizeWhatsAppNumber` (E.164, mobile-capable, Kuwait default, Arabic-Indic digits,
  `00` prefix); `GET/PUT /me/profile` carry `whatsapp_number`; the purchase route requires a number (profile or
  body), saves and snapshots it atomically, and refuses with `WHATSAPP_NUMBER_REQUIRED` /
  `INVALID_WHATSAPP_NUMBER` without echoing the value; Student `GET /me/purchase-requests` returns
  `whatsapp_url` for waiting requests; Admin purchase list adds `contact_whatsapp_number`,
  `requester_whatsapp_number` and `contact=AVAILABLE|MISSING`; Admin directory adds `whatsapp_number`,
  `pending_payment_requests` and `whatsapp=PROVIDED|MISSING`, `pendingPayment=true`; User 360 adds the number
  and per-request snapshot; three overview metrics; `migrate down` preflight for 58 → 57.
- **Student UI:** profile card (Provided / Not provided, add/replace), derived reminder card on dashboard and
  Course access page, contact step inside the Course and Bundle confirmations, success state with an explicit
  **Continue to WhatsApp** link, pending-request recovery on Course, Bundle and Course access pages. Arabic and
  English, RTL/LTR, phone digits always LTR.
- **Admin UI:** User 360 contact card (chat link, copy, status), directory filters and contact cell, request
  queue contact column and filter, analytics metrics.
- **Removed:** `lib/browser/external-handoff.ts` and the automatic window.

## Tests (commands executed; results as observed)

| Gate | Command | Result |
|---|---|---|
| Go format/vet | `gofmt -l .`; `go vet ./...`; `go vet -tags=integration ./...` | clean |
| Go unit (race) | `go test -race ./...` | pass |
| Go integration — new | `go test -tags=integration ./internal/httpapi -run WhatsAppContact`; `./internal/db -run StudentContactPhone` | pass |
| Go integration — full | `go test -tags=integration -p 1 -count=1 ./...` (+ `-timeout 60m` for `./internal/httpapi`, which needs 622 s and exceeds Go's 10 m default) | all 36 packages pass |
| Frontend typecheck/lint | `npx tsc --noEmit`; `npm run lint` | clean |
| Frontend unit | `npm test` | 954/954 pass |
| Production build | `npm run build` | success |
| E2E — D-117 journeys | `npx playwright test e2e/whatsapp-contact-reliability.spec.ts` | 2/2 pass (EN desktop with scripted popups disabled; AR RTL mobile 390×844) |
| E2E — canonical | `node scripts/e2e-canonical.mjs` | **643 passed, 0 failed, 0 flaky, 3 skipped** (production lane 4; development lane 639 in 4 shards, 642/642 listed tests reported) |
| Docs/expose guards | `scripts/docs-guard.sh`; `scripts/expose-guard.sh` | ok |

The final two UI commits (handoff button containment, digits-only placeholder) were re-verified with
the D-117, Bundle and Course-detail specs (49/49) and the unit suite; the canonical lane ran on the
preceding commit.

Infrastructure used: disposable local containers for PostgreSQL 16, Redis 7, Mailpit and MinIO on
loopback. No external service was contacted; `wa.me` was stubbed in every browser run.

## Deployment plan (not executed)

The candidate changes the schema range to 58..58, so `apply-release` (which requires the current schema to be
inside the target range) cannot select it, exactly as on 2026-10-07. Use the same forward-schema path:

1. **Review:** independent `agy` review of `a38dbd8..<candidate>` with a recorded verdict; hosted CI green on
   the candidate SHA.
2. **Build and transfer:** from a clean checkout of the candidate, `./deploy/hostinger/release.sh build` and
   `export`; transfer the five artifacts to `/home/deploy/gradex-production/incoming/<sha>/`; run
   `import-release.sh` (installs `releases/<sha>/tooling/`); `docker load` the images if import does not.
3. **Preflight (read-only):** `schema_migrations` = `57|false`; `GRADEX_RELEASE_SHA=a38dbd8…`; no
   `contact_phone` column and no `purchase_request_contact_snapshot_immutable` function; `deploy/` identical;
   no new env keys.
4. **Rehearse on restored production:** `host.sh restore` + `verify-restore`, then the candidate
   `gradex-migrate up` against `gradex-restore-verify` (pattern: `/var/tmp/gradex-release-20261007/rehearse-d082726.sh`
   with `REL`/`IMG` changed). Expect `58|false`, 0 unvalidated constraints, unchanged row counts, idempotent rerun,
   production still `57|false`.
5. **Cutover (≈1 minute API/worker downtime):** the `cutover-d082726.sh` pattern with `57|false` preflight and the
   new image tags — stop api/worker, verify 0 active media claims, local `pg_dump -Fc` + `pg_restore --list`,
   `host.sh backup` (offsite), atomic `runtime.env` selection edit with `runtime.env.before-<sha12>` backup,
   `host.sh up-core` (migrates 57 → 58, recreates api/worker/frontend), `host.sh verify-core`, `host.sh verify`.
6. **Smoke:** `/healthz`, `/readyz`; `verify-public.sh --mode cloudflare`; logged-out `GET /api/v1/me/profile` → 401;
   catalogue and Course pages EN/AR; with a human or approved smoke identity — Student reminder → add number →
   request → success link → `/access` recovery; Admin User 360 number and chat link; request queue contact column.
7. **Observe** 20 minutes: restarts, 5xx, ERROR lines, `purchase_requests` created with `contact_phone` NULL
   (want 0 for new rows).

## Rollback

Old application (`a38dbd8`, range 57..57) refuses schema 58, so rollback is the established database-restore
path, not a container swap: stop api/worker → restore the pre-migration dump into
`gradex_production_rollback` → rename-swap databases (nothing dropped) → `apply-release` the
`a38dbd8` `release.env` → `verify`. Numbers Students provide after cutover remain in the swapped-out database.
`migrate down` is refused under `APP_ENV=production` and, separately, 0058 down and its preflight refuse while
any number or snapshot exists.

## User-facing impact

- All 33 existing Students see a non-blocking reminder until they add a number; purchased Courses stay open.
- New purchase requests require a number (asked once inside the confirmation).
- WhatsApp opens from a button on the success screen instead of automatically — one extra tap, reliable in
  in-app browsers and with popup blockers.
- During the deploy window, a browser still running the old frontend that submits a request for a Student with
  no number receives a generic failure until reload (the old UI does not know the new requirement).
