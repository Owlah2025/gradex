# 2026-10-07 production release — hardening candidate `d082726` (schema 46 → 57)

Status: **DEPLOYED. Public, health, and logged-out checks green. Logged-in role smokes NOT AVAILABLE**
(production has no approved smoke identity — see "Smoke coverage").

| Item | Value |
|---|---|
| Production URL | https://gradexcourses.com |
| Released SHA | `d0827261e5e756d196fc80af3ff5a23dcc079bef` (branch `hardening/2026-10-04-autonomous`, pushed) |
| Previous production SHA | `0c5c67e20f2bbf7312e403889aa1f021adb8748a` |
| Schema | before `46\|false` → after `57\|false` |
| Images | backend `gradex-backend:hostinger-d0827261e5e7` (`sha256:b905bcaa…`), frontend `gradex-frontend:hostinger-d0827261e5e7` (`sha256:c2d165b6…`), revision labels = released SHA |
| Live binary proof | `sha256sum /usr/local/bin/gradex-api` in the running container = locally built image (`4d05e7c9d53a5d5a…`) |
| Maintenance window | API/worker stopped 19:32:36Z → candidate healthy 19:33:16Z (≈50 s). Frontend, edge, PostgreSQL, Redis never restarted |
| Hosted CI | run 37671963646 on `d082726`: **success**, all 6 jobs incl. S5 T075 |

## Why the candidate is `d082726`, not the approved `19ecab3`

The independently approved candidate `19ecab3` could not be deployed. Rehearsing 46 → 57 with the
`19ecab3` migrator against a restored copy of the latest production backup failed at 0054:

```
migration failed: a READY media asset version is immutable (version 32f9f461-…)
```

0054 backfills `media_asset_versions.course_id` with an UPDATE; `media_asset_versions_immutable` rejects
any UPDATE of a READY version. Production held 105 READY versions; every test database was migrated empty
and seeded afterwards, so no gate saw it. Production was untouched (rehearsal ran only in the isolated
`gradex-restore-verify` container).

With Product Owner authorization, `d082726` disables exactly that trigger around the single backfill
statement and re-enables it immediately; the file runs as one implicit transaction (golang-migrate v4.19.1,
`x-multi-statement=false`, lib/pq simple query), so a failure also rolls back the disable. Production had
never applied 0054. Regression tests in `backend/internal/db/migrate_0054_ready_media_integration_test.go`
start at schema 46 with READY VIDEO/RESOURCE/PREVIEW versions bound to a lesson, lesson file and revision
preview, fail against the old 0054 with the production error, and on the fix prove: clean 46 → 57, trigger
enabled afterwards, READY mutation still refused, exact non-NULL `course_id`, cross-course binding refused,
and that a failed 0054 leaves nothing behind with the trigger enabled.

Independent review of `19ecab3..d082726`:
- agy (required reviewer seat): **VERDICT: APPROVE**, no findings.
- Codex: **VERDICT: APPROVE**; one Sev4 — the failure-atomicity test accepts any migration error rather than
  asserting SQLSTATE 23503 / `course_lessons_video_asset_course_fk`. Not fixed, to keep the reviewed SHA.

Local gates on `d082726`: gofmt clean, `go build`, `go vet`, `go vet -tags=integration`, integration
`./internal/db` and `./cmd/migrate` pass. Everything outside those two files is identical to gate-tested
`683a67a`/`19ecab3`.

Hosted CI on `19ecab3` (run 37649741301) was **cancelled**: S5 T075 hit its 25-minute limit inside
`apt-get install ffmpeg`. It is not counted; the green run is 37671963646 on `d082726`.

## Production preflight (read-only, 2026-10-07 ~16:11Z)

- Production was at schema **46**, not ≤ 44 as FINAL-REVIEW-2 assumed. `0c5c67e` is an ancestor of the
  candidate; migrations 0001–0046 are byte-identical. Production never applied the old 0051/0053 bodies.
- Every 0047–0057 constraint checked against live rows: 0 security-event types outside the 0048 list; 0 versions
  without a logical asset/course; 0 cross-course lesson videos (146 bound), lesson files (145) or revision
  previews; 0 kind/retired/owner mismatches; 0 name collisions; `pg_trgm` present.
- Configuration: no new environment variable read by api/worker/config; `compose.yml` byte-identical to
  `0c5c67e`; postgres/redis compose config hashes identical, so `up-core` did not recreate them.
- `ent_manual_needs_invitation` is `NOT VALID` before and after — pre-existing, not from this release.

## Rehearsal on restored production (d082726)

Fresh restore of offsite snapshot `8144471a…` into the isolated container, `verify-restore` passed, then the
`d082726` migrator: `46|false` → `57|false` in 2 s; trigger `O`; 0 disabled user triggers; 0 NULL/mismatched
`course_id` on versions and lesson files; 0 invalid indexes; 6 completions backfilled; READY mutation probe
refused; idempotent re-run clean; production still `46|false`.

## Backup and rollback anchor

- Local pre-migration dump: `/home/deploy/gradex-production/backups/pre-d0827261e5e756d196fc80af3ff5a23dcc079bef-20261007T193236Z.dump`
  (962 282 bytes, sha256 `8cebeece20715c48…`, `pg_restore --list` OK), taken with API/worker stopped.
- Offsite encrypted snapshot: `f5e585884a1f39da5db3533499702811239988a7de3a4578463d8c338b2672a1` (19:33:02Z,
  restic check "no errors were found").
- Prior runtime selection: `runtime.env.before-d0827261e5e7`. Old images `*:hostinger-0c5c67e20f2b` and
  release bundle `releases/0c5c67e…` remain on the host.

**Rollback is a database restore plus old application, never a container swap.** The candidate supports
schema 57..57 and `0c5c67e` supports ≤ 46. Drilled before cutover (dump → scratch DB → identical counts
`32|10|23|24|113|91` → rename swap):

1. `docker stop gradex-production-api-1 gradex-production-worker-1`.
2. In the production PostgreSQL container: `CREATE DATABASE gradex_production_rollback`, then
   `pg_restore --exit-on-error --single-transaction --no-owner --no-acl -d gradex_production_rollback` from the
   pre-migration dump; confirm `46|false` and counts.
3. Terminate remaining sessions on `gradex_production`, then
   `ALTER DATABASE gradex_production RENAME TO gradex_production_failed_57` and
   `ALTER DATABASE gradex_production_rollback RENAME TO gradex_production`. Nothing is dropped.
4. From `releases/0c5c67e…/tooling`: `host.sh apply-release releases/0c5c67e…/release.env`, then `host.sh verify`.
5. Writes made between 19:33:16Z and the rollback are lost from the restored database (they remain in
   `gradex_production_failed_57`).

## Smoke coverage

| Area | Result |
|---|---|
| Health | `/healthz` 200, `/readyz` 200 (postgres/redis/schema ok); `host.sh verify-core` and `verify` passed |
| Public edge | `verify-public.sh --mode cloudflare`: DNS, Cloudflare edge, certificate (GTS WE1, to 2026-11-27), frontend, health, readiness, security headers passed; HTTP→HTTPS 301 |
| Visitor | Landing, EN/AR catalogue, EN/AR Course Details, subjects, privacy/terms 200; 7 catalogue courses; static assets 200; light/dark landing renders; access/purchase messaging correct |
| Logged-out authorization | Protected API routes return 401 `AUTHENTICATION_REQUIRED`; staff pages render data-less shells |
| Media / storage | All 7 public course previews issue signed R2 URLs that serve `video/mp4` (206); worker READY; 0 active claims; immutability trigger enabled |
| Student / Instructor / Admin logged-in | **NOT AVAILABLE** — no approved production smoke identity exists (see `RELEASE_PLAN_2026-09-25_SCHEMA46_MEDIA_PREVIEW.md`), and real user credentials were not used. Needs a human or an approved fixture |
| Resumable upload / playback renewal in production | **NOT AVAILABLE** for the same reason |

## Post-deploy observation (19:33Z → 19:53Z, three snapshots)

185 API requests; 0 restarts, 0 OOM, 0 api/worker/frontend ERROR/FATAL/panic lines, 0 API 5xx, PostgreSQL errors only from
the operator's own mistyped diagnostic query, Redis TLS "wrong version number" lines only at the exact
times `verify-core`/`verify` deliberately probe plaintext refusal. Host memory ≈6.1 GiB available, swap
133 MiB, disk 46 %.

## Follow-ups

- Logged-in role smokes (Student, Instructor, Admin), production resumable upload and playback renewal: human
  check or an approved smoke-fixture policy.
- Codex Sev4: tighten the 0054 failure test to assert the exact FK violation.
- CI: S5 T075 `apt-get install ffmpeg` can hang to the job limit; add a step timeout or cached binary.
- Migration gates should include an upgrade from a populated production-shaped schema, not only fresh → max.
- `select-release`/`apply-release` cannot select a schema-changing release (bundle always carries the schema46
  capability; apply-release requires the current schema in range), so `runtime.env` selection keys were set by
  a scripted atomic edit with a backup copy. Tooling should gain a selection path for forward schema releases.
- Pre-existing hardening follow-ups remain (FINAL-REVIEW-2 Sev3/Sev4, A5-002, npm advisories, C1, R2 scanner,
  Safari HLS, manual acceptance).

Run artifacts (scripts, preflight output, rehearsal logs, reviews): `/var/tmp/gradex-release-20261007/` on the
builder workstation; cutover log `/home/deploy/gradex-production/backups/cutover-d082726.log` on the host.
