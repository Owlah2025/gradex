# Hardening final closure — 2026-10-06

## Scope and authority

This record closes the 2026-10-04 hardening campaign after FINAL-REVIEW-1
(`VERDICT: REJECT` at `16aa3c2`) and the blocking-repair record
[2026-10-06-final-review-repairs.md](2026-10-06-final-review-repairs.md).
The campaign base is `45e66f0550e3d0c6ec436bd2e9357f2de28ee33c`.

- Gate-tested software tree: `683a67a92d065de200936f21102ad707bbed2ff6`.
- The closure documentation commit that follows it changes only Markdown
  under `docs/audits/`; its backend, frontend, deploy and CI trees are
  identical to `683a67a` (`git diff --stat 683a67a HEAD`).
- Every gate below ran in a clean detached worktree created from the exact
  commit, with no tracked changes before or after
  (`/home/owlah/worktrees/gradex-closure-683a67a`). Logs are under
  `/var/tmp/gradex-closure-20261006/`.

No deployment, push, production read or write, DNS/Hostinger/Cloudflare/email
change, or protected `gradex` database reset occurred. Only disposable
`gradex_*_test` and `gradex_playwright_e2e_*` databases on the local hardening
PostgreSQL were used.

## Corrections to earlier gate claims

The repair record and the interrupted GATES-2 narration overstated three
results. They are corrected here rather than carried forward.

| Earlier claim | What the exact checkout showed | Repair |
|---|---|---|
| `gofmt -l .` empty | The go1.26.5 toolchain `gofmt` (and the older `~/go/bin/gofmt` on PATH) flagged `internal/httpapi/authorization_test.go`, unchanged since before the base. CI's formatting step could not pass. | `f7e6171`, whitespace only (`git diff -w` empty). The gate script now runs both the toolchain and PATH `gofmt`. |
| "Full backend integration: 11 packages" | Integration-tagged tests also exist in `cmd/api`, `cmd/migrate`, `internal/academic/...`, `internal/email`, `internal/legacymigrate`, `internal/db/e2equery`, `internal/media`, `internal/playback` and `internal/storage`. `internal/media` had only been run with `-run Multipart`. A full `go test -tags=integration -p 1 ./...` failed in `cmd/api` (2 tests) and `internal/media` (5 tests). | `26f8aa7`, `b15027d`; `23c8637` adds the missing packages to CI. |
| GATES-2 narration "no failures" through test 266 | Its run log records test 207 (S6 Course Access Grant) failing at 05:27. | Classified below. |

## Failures found and repaired tonight

| Commit | Class | Finding | Repair and proof |
|---|---|---|---|
| `adac3ed` | Product (fail-closed hardening) | The last-active-Admin guard counted its `FOR UPDATE` rows without `rows.Err()`; an iteration error was only caught indirectly by PostgreSQL aborting the transaction. | Return the iteration error. Identity integration and suspension race regressions pass. |
| `2c4f04c` | Evidence collection defect | UX-J wrote its curated evidence to a fixed `frontend/test-results/uxj-release-evidence` no run owned or cleared, and proved completeness by counting every PNG there. UX-J and UX-G left capture contexts open when a capture failed; Playwright 1.62 only stops tracing for such contexts. | Run-owned output directory by default, stale target removed before each capture, every expected file verified by name and size with no strays; contexts closed in `finally`. Targeted: 75/75. |
| `f7e6171` | Gate defect | Formatting, above. | gofmt empty with both binaries. |
| `26f8aa7` | Test-environment regression in range | `4b67773` moved the T108 TLS Redis certificates from `/var/tmp` to the default temp directory. They are bind-mounted into Docker; a snap-packaged daemon has a private `/tmp`, so `redis-server` could not load them. | Restore `/var/tmp`. Both T108 tests pass. |
| `b15027d` | Fixture defect (same class as CAT-01) | One lesson-video fixture selected a nonexistent version, correctly refused by `course_lessons_video_asset_course_fk`; four D-096 trusted-preview tests inserted a second DRAFT revision 1 after `2ece442` began seeding a real lesson. | Real course-owned VIDEO version; bind previews to the existing draft revision. No constraint or assertion relaxed. Full `internal/media`: pass. |
| `23c8637` | CI coverage gap | The CI integration job omitted the packages above, which is how their fixtures drifted. | Added, each verified green locally; job timeout 35 → 50 minutes. Not yet observed in GitHub Actions (no push). |
| `b5a6eff` | Deterministic test race | D-102 "rejected order rolls back…" pressed Space/ArrowUp/ArrowUp/Space back to back; the keyboard sensor can drop the order (`reorderAttempts` 0). | Uses the synchronized pick-up/move/drop sequence of the passing keyboard test; the title edit still overlaps the held rejection. 28/28 across `--repeat-each=4`. |
| `82d23c5` | Intermittent test-harness hang | `completePurchaseConfirmation` closed the WhatsApp handoff popup while its stubbed navigation committed; `close()` never returned (trace: UNFINISHED Close page) and "Admin cancellation releases…" timed out at 150s. It had failed in 4 of about 14 earlier canonical runs. | Wait for the stub's `domcontentloaded` before closing; every caller stubs `wa.me`. 7/7 in five fresh-database runs. |
| `683a67a` | Harness/host-resource defect | One `next dev` served all 640 development tests and grew from ~2.7 GB to ~5.5 GB RSS. Swap reached 4.0/4.0 GiB and MemAvailable fell to ~0.5 GiB. | Sequential development shards with their own DB/API/worker/media server/`next dev`, process-reaping and memory logging between shards, and a coverage guard. See below. |

### Classification of the interrupted GATES-2 failures

Run `/var/tmp/gradex-current-head-canonical-2-20261006` (software identical
to `0235c43`):

- **207 S6 Course Access Grant** — the trace's two admin list requests failed in
  the browser with `net::ERR_INSUFFICIENT_RESOURCES` before reaching the API
  (510 requests in the trace; no request storm). The UI correctly showed its
  load error.
- **464/465 UX-G access evidence 1440px en** and **618 UX-J curated evidence** —
  `Page.captureScreenshot: Unable to capture screenshot`, on full-page captures
  of large pages. All three involve `/en/admin/course-access`.
- At the start of this session swap was 4.0/4.0 GiB and 4.5 GiB of RAM-backed
  `/tmp` held 139 stale campaign run directories (moved, not deleted, to
  `/var/tmp/gradex-tmp-archive-20261006`).

Classification: **transient browser failure caused by host memory exhaustion**,
driven by the long-lived development compiler, not a product defect. With
memory relieved, the three spec files passed 77/77 (`targeted-1`). The
monitored monolithic run at `23c8637` reproduced the memory trajectory
(`next dev` 2.7 → 5.5 GiB RSS, MemAvailable 0.5 GiB at test ~630); it was
interrupted at test 636 when the session ended and is not evidence. Its two
failures (D-102 and the purchase popup) occurred early with ample memory and
are the deterministic defects repaired above. The application has no
unbounded server-side cache (two bounded-key maps in `src/lib`).

## Canonical runner hardening (`683a67a`)

`frontend/scripts/e2e-canonical.mjs` runs the development lane as sequential
Playwright shards (default 4; `GRADEX_E2E_DEVELOPMENT_SHARDS` overrides), one
worker each. Each shard is a complete invocation whose `globalSetup` creates
its own database, Go API, worker and media server, whose `webServer` starts its
own `next dev`, and whose output/report/JSON paths are its own. A shard starts
only after no process remains in the frontend directory and the previous run
state's API/worker PIDs are gone; the runner waits up to 60s and refuses
rather than killing. MemAvailable and swap are logged around each shard and
written to `summary.json`.

Coverage guard: before running, each shard's `--list` must be disjoint from the
others and their union must equal the lane's `playwright test --list` (640).
After running, each shard must report exactly its assigned tests and outcomes.
Negative checks: listing shards as `i/5` (one test file group unlisted) and
listing shard 1 twice were both rejected with exit 1. A grep-selected four-shard
run of the runner itself (`runner-check-1`) produced five distinct run
databases, separate output directories, and flat memory. The production lane
is unchanged.

## Gate results on `683a67a`

| Gate | Command | Result |
|---|---|---|
| Formatting | `$(go env GOROOT)/bin/gofmt -l .` and PATH `gofmt -l .` in `backend/` | Empty, pass |
| Whitespace | `git diff --check 45e66f0 HEAD` | Empty, pass |
| Repository guards | `scripts/docs-guard.sh`, `scripts/expose-guard.sh` | Pass |
| Build / vet | `go build ./...`; `go vet ./...`; `go vet -tags=integration ./...` | Pass |
| Backend unit (race) | `go test -race ./...` | Pass |
| Backend integration, every package | `go test -json -tags=integration -p 1 -count=1 -timeout=40m ./...` | Pass: 1449 top-level tests, 0 failed; 2 pre-existing declared skips that need separately built schema-probe binaries (`TestSchema42RecoveryReadinessPredicates`, `TestSchema43RollbackApplicationAfterAutomaticRotation`). Includes catalog 311s, HTTP API 647s, identity 254s, media 508s, `cmd/api`, `cmd/migrate`. |
| Targeted race | `go test -race -tags=integration -p 1 -run 'Multipart\|TestAdminUser360UsesOneConnectionAndAuditsConcurrentReads\|TestD5Approval…\|TestLesson.*Binding\|TestLessonFileAttachmentRetry\|Suspend' ./internal/media ./internal/httpapi ./internal/catalog ./internal/identity ./internal/admin` | Pass |
| Migration CLI | run-owned `verify-migrations.sh` | Fresh → 57, idempotent up, down 1 / up, production-shaped down refused with schema unchanged, canary password redacted; database dropped |
| Deploy tooling | `verify-schema-41-rollback.sh`, `verify-schema-42-rollback.sh`, `verify-compose-render.sh` | Pass (local rendering only) |
| Frontend | `npm ci`; `npm run lint`; `npm run typecheck`; `npm test`; `npm run build` | Pass; 863 tests from 113 compiled files (= 113 source test files), 0 skipped |
| Shard partition | `node scripts/e2e-canonical.mjs --plan-only` | 164 + 179 + 150 + 147 = 640 |

## Authoritative canonical E2E on `683a67a`

`npm run test:e2e:canonical` from the clean checkout (`GRADEX_E2E_TMP_DIR=/var/tmp/gradex-closure-20261006/canonical-683a67a`,
`NODE_OPTIONS=--max-old-space-size=6144`, Mailpit at `127.0.0.1:8025`) rebuilt the frontend, then ran the
production lane and four sequential development shards, each with a fresh run database
(`gradex_playwright_e2e_muwfs8pl0ykiw9b2`, `…muwftflj7xati9qo`, `…muwg8qzlf0ab5bh4`, `…muwguhf9sm72vr7t`,
`…muwha34svxulekm0`). Exit 0. `identity.txt` records the SHA and zero tracked changes.

| Lane / shard | Assigned | Passed | Failed | Flaky | Skipped | MemAvailable before → after |
|---|---:|---:|---:|---:|---:|---|
| Production | 4 | 4 | 0 | 0 | 0 | 8985 → 9069 MiB |
| Development shard 1/4 | 164 | 162 | 0 | 0 | 2 | 9063 → 9407 MiB |
| Development shard 2/4 | 179 | 179 | 0 | 0 | 0 | 9414 → 9432 MiB |
| Development shard 3/4 | 150 | 150 | 0 | 0 | 0 | 9431 → 9318 MiB |
| Development shard 4/4 | 147 | 146 | 0 | 0 | 1 | 9255 → 9304 MiB |
| Development total | 640 listed, 640 reported | 637 | 0 | 0 | 3 | — |
| Aggregate | 644 | 641 | 0 | 0 | 3 | — |

The lowest MemAvailable sampled during the whole run was 4953 MiB (swap steady at ~1.4 GiB), against
~0.5 GiB with swap exhausted in the monolithic run. Memory returned to the same ~9.3 GiB baseline after
every shard.

Declared skips, each retained as a coverage boundary and not counted as a pass:

- `landing-study-plan-production.spec.ts` English and Arabic production smoke cases: guarded to the built
  frontend and excluded from the development lane by their mode check; the production lane runs only the
  T076 selection, so these two are not claimed.
- `uxi-global-sweep.spec.ts` "a screen issues each of its reads once": the pre-existing `test.fixme`
  tracked in the hardening plan (P1.5).

## Standalone browser lanes on `683a67a`

Sequential, each with its own `GRADEX_E2E_TMP_DIR`, output and report directories under
`/var/tmp/gradex-closure-20261006/`.

| Lane | Command from `frontend/` | Result |
|---|---|---|
| Playback reauthorization/recovery (A5-001) | `npx playwright test --config=playwright.playback-recovery.config.ts` | 3 passed (1.3m): real short expiry, storage 403s, exhaustion/retry, position and lease |
| S11 release acceptance | `npx playwright test e2e/s11-release-acceptance.spec.ts --workers=1` | 1 passed |
| Media authoring with real MinIO, worker and FFmpeg | `npx playwright test --config=playwright.media-authoring.config.ts` | 9 passed (6.2m), including the committed `resumable-upload.spec.ts`; the clean checkout has no untracked review spec |
| S3 public catalogue performance | `npm run build`, then `npx playwright test --config=playwright.s3-performance.config.ts` | 1 passed; synthetic local 4G list/detail p95 LCP 880/2468 ms. The first attempt ran after the development lanes had replaced `.next` and stopped at the missing production build; it is retained as `s3perf-683a67a-nobuild` and is not counted. |

## Remaining non-blocking risks and deferred work

Re-evaluated for this closure; none is a Sev1/Sev2 or P0 blocker for the
local hardening range.

| Risk | Severity / priority | Status and pre-production follow-up |
|---|---|---|
| Migration 0054 composite FKs on legacy `course_lessons.video_asset_version_id` values | Sev2 release risk if data is dirty | Unchanged. Read-only production preflight for dangling or cross-course IDs before any release that applies 0054. Production is at schema 44 and nothing was applied. |
| Strict JSON unknown-field rejection | Sev3 compatibility | Unchanged. The repository's browser client and E2E pass; confirm no external client sends extra fields before release. |
| Suspension lock order | Sev4 | `rows.Err()` now fails closed (`adac3ed`). Two Admins suspending each other can still deadlock (subject row then Admin set); PostgreSQL aborts one with a 500 and the invariant holds. |
| RU-02 cleanup fairness; provider calls under row locks; multipart verification throughput | P2 | Unchanged; measure before raising worker concurrency or upload volume. |
| Authorization-matrix coverage (A2-003/A2-004/A2-005), headers (A2-006) | P1 | Unchanged; tracked in the hardening plan. |
| Dependency triage | P3 | `npm ci` reports 12 vulnerabilities (11 high, 1 critical) and deprecations; not triaged here, counts are not severities. |
| C1 intermittency | Existing | Still `UNRESOLVED_INTERMITTENT_NONREPRODUCIBLE`; T035a retained. |
| E2E harness | P3 test infrastructure | Other specs still open contexts without `finally`; a failure can leak pages within a shard. Fixture e-mails are not repeat-safe within one database (`--repeat-each` fails registration by design). |
| CI additions | P3 | `23c8637` is verified locally only; the 50-minute job limit needs a first GitHub Actions observation. |
| External acceptance | Separate authority | Production R2 and scanner, actual Safari HLS, and manual acceptance are not covered by local Chromium/MinIO evidence. |
