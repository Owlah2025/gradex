# T9 / MVP-F25 — SY-07 Whole-suite E2E runnability

**Date:** 2026-08-24
**Branch:** `ui-antigravity-20260817`
**Tranche:** T9 / MVP-F25 — `SY-07 — Whole-suite E2E runnability`
**Verdict:** `PROVEN` — the canonical suite runs **168 / 168 green** from one command, twice.

## 1. Authority

- Founder authorization, 2026-08-24: open T9 / MVP-F25 against SY-07 as a reliability/infrastructure
  tranche. Explicitly prohibited: weakening the performance assertion, skipping the production-only
  spec, or accepting `164 / 1 / 3` as green.
- [D-089](../../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time) —
  gap-driven work only, one tranche at a time.
- [`docs/mvp/FUNCTIONAL_COMPLETION.md`](../../mvp/FUNCTIONAL_COMPLETION.md) §4.4 — the SY-07 row,
  `INTEGRATION_BROKEN`, severity HIGH, recorded evidence *"4 specs pass alone, fail together"*.
- [D-069](../../DECISIONS.md#d-069--sc-001-is-measured-against-the-built-frontend-behind-a-run-owned-same-origin-proxy) —
  the production-mode measurement contract for T076/SC-001:
  production mode is opt-in through `GRADEX_E2E_FRONTEND_MODE=production`, a missing build fails
  closed, and **neither the profile nor the threshold may be tuned after observing a result**.
- The open governance question this tranche closes was recorded by T7 in
  [FUNCTIONAL_COMPLETION.md §25](../../mvp/FUNCTIONAL_COMPLETION.md): *"Whether the canonical suite
  should measure the production build is a governance question beyond T7's authority."* The Founder
  authorization for T9 answers it: **yes, in the mode its contract requires.**

## 2. Pre-T9 state

Canonical `npx playwright test` (one worker, local `backend/docker-compose.yml` stack):

| | count |
|---|---:|
| passed | 164 |
| failed | 1 |
| did not run | 3 |
| **discovered total** | **168** |

The single failing identity was `e2e/s5-playback-performance.spec.ts:157` — the `phone` viewport of
T076/SC-001. The 3 "did not run" were the remaining three viewports (`tablet`, `laptop`, `desktop`)
of the same spec.

## 3. Root cause — a harness execution-mode defect, not a product failure

`s5-playback-performance.spec.ts` asserts its own precondition in `test.beforeAll`:

```ts
expect(
  process.env.GRADEX_E2E_FRONTEND_MODE,
  "T076 must measure the built frontend: run with GRADEX_E2E_FRONTEND_MODE=production after npm run build",
).toBe("production");
```

SC-001 is a claim about the **shipped** application. Measured against `next dev` the figure is
dominated by on-demand compilation and unoptimized assets, so the spec fails closed rather than
publishing a misleading number — that refusal is the contract, recorded in D-069.

The canonical command launched only the development frontend. So:

1. the first viewport failed the `beforeAll` precondition — **1 failed**;
2. Playwright does not run the remaining tests of a describe whose `beforeAll` threw — **3 did not run**.

Both classes of the canonical suite were valid, and the harness treated them as one. `164 / 1 / 3`
was produced entirely by launching the wrong frontend mode. No product behaviour was involved.

`playwright.config.ts` already knew how to run *either* mode — production mode, the production-origin
proxy, the internal/public port split, and the fail-closed build check all shipped with T076. What did
not exist was a supported path that runs **both**, each in the mode its contract requires, and returns
one verdict. That orchestration is the whole of T9.

## 4. Harness architecture

```
npm run test:e2e:canonical
        │
        ├─ pre-flight: refuse to start if a prior E2E run still owns live processes
        ├─ next build              (production build from the current worktree)
        │
        ├─ PRODUCTION LANE   GRADEX_E2E_FRONTEND_MODE=production
        │     production-origin-proxy → next start (internal port) + /api/* → run-owned Go API
        │     testMatch: PRODUCTION_MODE_SPECS                         → 4 tests
        │
        ├─ DEVELOPMENT LANE  (no mode variable)
        │     next dev on the run-allocated port
        │     testIgnore: SEPARATE_CONFIG_SPECS + PRODUCTION_MODE_SPECS → 164 tests
        │
        └─ aggregate → summary.json + console table → exit 0 only if both lanes pass
```

**Lane classification is machine-readable and lives in exactly one place.**
`frontend/playwright.config.ts` declares:

```ts
const PRODUCTION_MODE_SPECS  = ["**/s5-playback-performance.spec.ts"];
const SEPARATE_CONFIG_SPECS  = ["**/s3-public-catalogue-performance.spec.ts", "**/media-authoring/**"];
```

and derives both lanes from the *same* list, as exact complements:

- production mode → `testMatch: PRODUCTION_MODE_SPECS`
- development mode → `testIgnore: [...SEPARATE_CONFIG_SPECS, ...PRODUCTION_MODE_SPECS]`

The runner script never restates a spec name or greps a filename. Moving a spec between lanes is a
one-line change in the config, and no spec can be claimed by both lanes or dropped by neither.

**Why production runs first.** `next dev` writes into the same `.next` directory `next build`
produces. Building and then running the development lane first would leave the production lane
measuring a directory the dev server had since rewritten. Production-first removes that hazard
without needing a second `distDir`.

**Lanes are separate Playwright invocations, run sequentially.** Each has its own
`globalSetup`/`globalTeardown`, so each creates, owns and disposes of its own database, Go API,
worker, media server and frontend. The existing `/var/tmp` singletons (`LOCK_FILE_PATH`,
`RUN_STATE_FILE_PATH`, the binary paths) are honoured unchanged, because sequential lanes never
overlap. `workers: 1` and the `assertSingleWorker` gate from MVP-F06 are untouched.

## 5. Canonical command

**From the repository root:**

```bash
cd frontend && npm run test:e2e:canonical
```

It (1) refuses to start if a previous E2E run still owns live processes, (2) runs `next build` from
the current worktree, (3) runs the production lane, (4) runs the development lane, (5) prints a
per-lane and aggregate result, writes `playwright-report/canonical/summary.json`, and (6) exits `0`
only if both lanes pass.

Sub-entrypoints, for iterating on one lane only — neither is the authoritative verdict:

| script | effect |
|---|---|
| `npm run test:e2e:development` | development lane alone (164 tests) |
| `npm run test:e2e:production` | production lane alone (4 tests); requires an existing `npm run build` |

`npm run test:e2e` is unchanged and still means "the development lane", so every existing habit and
document that names it keeps working — it simply no longer picks up a spec it cannot satisfy.

### Prerequisites

Genuine prerequisites only. Everything else the run needs, it starts and disposes of itself.

| prerequisite | why |
|---|---|
| Docker, with `backend/docker-compose.yml` up | PostgreSQL 16 on 5432, Redis 7 on 6379, MinIO on 9000, Mailpit on 1025/8025 |
| Go toolchain | `globalSetup` compiles the API, worker and seeder binaries per run |
| Node + npm, with `npm ci` done in `frontend/` | Next.js, Playwright 1.62.0 and Chromium |

Nothing must be started by hand: the per-run database, Go API, Go worker, media fixture server and
frontend are all created and destroyed by the run. There are no interactive prompts, no fixed ports,
no machine-specific paths, and no cloud or provider dependency, so the same command is usable from CI
unchanged.

## 6. Test classification

| lane | tests | files |
|---|---:|---:|
| development | 164 | 25 |
| production | 4 | 1 |
| **union** | **168** | **26** |

Measured with `npx playwright test --list` in each mode. 168 is exactly the pre-T9 discovered total,
so **no test was lost and none is executed twice**. Not in either canonical lane, unchanged:
`playwright.media-authoring.config.ts` and `playwright.s3-performance.config.ts` own their own specs
and their own environments.

**Skips.** Zero. Both full canonical runs reported `skipped 0` in both lanes. The pre-T9 "3 did not
run" were purely downstream of the production-precondition failure and disappear once the spec runs
in its own lane. The runner enumerates any skipped test title in its output, so a future skip cannot
hide inside a count.

**External-deployment note.** With `GRADEX_E2E_EXTERNAL_ORIGIN` set (staging/provider smoke), the
production-mode specs are now excluded rather than run-and-failed. That path runs
`s11-release-acceptance.spec.ts` by explicit file argument and does not own a local production build,
so T076 could never have passed there. This removes a guaranteed failure; it removes no coverage.

## 7. Production build

Created by `next build` inside the canonical command, from the current worktree, on every
invocation. Nothing reuses a previous `.next`:

- the build runs before either lane, so the artifacts always correspond to the source under test;
- `playwright.config.ts` additionally fails closed if `.next/BUILD_ID` is absent in production mode;
- `production-origin-proxy.mjs` repeats that check before it will spawn anything;
- `--skip-build` exists only for iterating on the harness itself and prints a loud warning plus its
  own `BUILD_ID` existence check. It was **not** used for either recorded canonical run.

The build succeeds from the protected dirty worktree with no `git clean`, `reset`, `stash`,
`restore`, or checkout. No production compilation error was exposed, so no `T9-REMEDIATION-xx` was
opened.

The production frontend is real: `next build` output served by `next start` on an internal loopback
port, fronted by the run-owned origin proxy. `NODE_ENV=production` over `next dev` is not used
anywhere, and no marker is fabricated to satisfy the spec — the spec reads the same
`GRADEX_E2E_FRONTEND_MODE` value that selects the actual build/serve path, and the recorded evidence
JSON carries `"frontend_mode": "production"`.

> Observed and benign: `next start` prints `⚠ "next start" does not work with "output: standalone"
> configuration.` The built server nevertheless serves correctly and all four viewports measured
> below their threshold through it. This is a Next.js advisory about the deployed topology, not a
> harness defect, and changing `next.config.mjs` is out of T9's scope.

## 8. Process ownership

Every process is owned by the lane that started it and disposed of by that lane's teardown.

| process | started by | identified by | terminated by |
|---|---|---|---|
| Go API | `global-setup.ts` | recorded PID + `apiExecPath` + process start time in `RUN_STATE_FILE_PATH` | `cleanupRunResources` in `global-teardown.ts` |
| Go worker | `global-setup.ts` (T8B) | recorded `workerPid` + `workerExecPath` | same |
| media fixture server | `global-setup.ts` | in-process | run exit |
| frontend (`next dev`) | Playwright `webServer` | Playwright child | Playwright |
| frontend (proxy + `next start`) | Playwright `webServer` | proxy child; proxy kills `next start` on SIGINT/SIGTERM/exit | Playwright |
| Playwright itself | `scripts/e2e-canonical.mjs` | its own child handle | signal forwarding |

`e2e-canonical.mjs` **never kills by name**. It holds exactly one child at a time and forwards
`SIGINT`/`SIGTERM` to it, so Playwright's own teardown disposes of the database, API, worker and
servers on interrupt as well as on failure. Its pre-flight reads `RUN_STATE_FILE_PATH` and probes
only the PIDs that file records; if either is alive it **reports and exits non-zero** rather than
terminating a process it does not own. No `pkill node`, no `pkill go`, no process-name pattern
anywhere.

Ports remain per-run kernel-allocated (`allocateEphemeralPort` / `runPort`), never 3000, with
`assertPortIsFree` before binding and `reuseExistingServer: false`. The unrelated stacks running on
this host throughout — `gradex-s12-*`, `compose-*`, `gradex-founder-acceptance-postgres-1` — were
untouched and still running afterwards.

## 9. Database ownership

Unchanged from MVP-F06/T8B: one disposable database per lane invocation, named
`gradex_playwright_e2e_<runId>`, created and seeded by `globalSetup` and dropped by
`globalTeardown` by that exact name.

Two lanes therefore mean two disposable databases per canonical run, each fully torn down. No
retained database was dropped or truncated, no Docker volume was removed, and `docker compose down`
was never run.

## 10. Mailpit / worker regression

Preserved exactly. The development lane still starts the T8B Go worker with
`EMAIL_PROVIDER=mailpit` against `127.0.0.1:1025`, and the real
API → outbox → worker → SMTP → Mailpit path is still asserted end to end:

- `t8b-staff-invitation-lifecycle.spec.ts` A/B/C — 3/3 green in the focused run and in both canonical runs;
- teardown still reports `Worker process exited with code 0`.

No email test was weakened. The production lane starts the same infrastructure through the same
`globalSetup`; T076 does not exercise email, but sharing one setup is simpler and safer than
introducing a second, divergent one, so no separate harness was built.

`MEDIA_SCANNER_MODE=DEVELOPMENT_NO_OP` remains scoped to the worker environment in `globalSetup`
exactly as before. No production media security setting was changed.

## 11. Performance spec — measurements

**Threshold unchanged at 5000 ms.** The profile `gradex-sc001-deterministic-4g` is unchanged. No
viewport was removed, no timeout inflated, no assertion relaxed, nothing mocked.

Independent re-proof before any harness change (`GRADEX_E2E_FRONTEND_MODE=production`, run
`mt6ohrwwkeuko3wv`):

| viewport | elapsed | signal | verdict |
|---|---:|---|---|
| phone | 3431 ms | `timeupdate` | PASS |
| tablet | 3373 ms | `totalVideoFrames` | PASS |
| laptop | 3351 ms | `totalVideoFrames` | PASS |
| desktop | 3361 ms | `totalVideoFrames` | PASS |

Inside canonical run 1 (`t076-playback-performance.json`, `"frontend_mode": "production"`,
`"all_loopback": true`, public dependency count 0):

| viewport | elapsed | verdict |
|---|---:|---|
| phone | 3597 ms | PASS |
| tablet | 3381 ms | PASS |
| laptop | 3386 ms | PASS |
| desktop | 3421 ms | PASS |

All four are consistent with the historical production figures (3382/3374/3338/3344 ms) and well
below 5000 ms. `4/4` measurements recorded, `passed: true`.

## 12. Harness defects found

### T9-HARNESS-01 — the canonical command ran a production-required spec in development mode

- **Observed.** `npx playwright test` → `164 passed / 1 failed / 3 did not run`; the failure is the
  T076 `beforeAll` precondition, and the 3 "did not run" are the remaining viewports of that describe.
- **Root cause.** The suite contained two valid runtime-mode classes; the harness exposed one
  execution path, which launched `next dev` only. The classification existed solely as prose in the
  spec's own precondition — nothing in the config or in any script assigned the spec to a lane.
- **Fix.** `PRODUCTION_MODE_SPECS` in `playwright.config.ts` drives both lanes as exact complements
  (`testMatch` in production, `testIgnore` in development); `scripts/e2e-canonical.mjs` runs both.
- **Proof.** `--list` reports 164 and 4; two full canonical runs report `168 passed / 0 failed /
  0 skipped` and exit `0`.

### T9-HARNESS-02 — a shared `outputDir` would have destroyed the first lane's failure artifacts

- **Observed.** Found while designing the runner. Playwright clears `outputDir` when a run starts, so
  the development lane would delete every trace, screenshot and error-context the production lane had
  just written — exactly the diagnostics a failing lane needs.
- **Root cause.** `outputDir` was a hard-coded default, adequate for a single invocation per suite.
- **Fix.** `outputDir: process.env.GRADEX_PLAYWRIGHT_OUTPUT_DIR || "test-results"`. The runner sets
  `test-results/<lane>`.
- **Proof.** `frontend/test-results/` contains `development/` and `production/` after a canonical run.

### T9-HARNESS-03 — a shared HTML report would have been overwritten lane-by-lane

- **Observed.** Same class as 02, for the retained HTML report.
- **Root cause.** `GRADEX_PLAYWRIGHT_HTML_DIR` already existed but defaulted to one shared folder,
  and there was no machine-readable per-lane result at all, so an aggregate verdict could only have
  come from scraping console text.
- **Fix.** The runner sets `GRADEX_PLAYWRIGHT_HTML_DIR=playwright-report/<lane>`, and the config gained
  an opt-in `json` reporter keyed on `GRADEX_PLAYWRIGHT_JSON_FILE`. The runner reads the two JSON
  reports for its aggregate and enumerates skipped titles from them.
- **Proof.** `playwright-report/{production,development}` plus `playwright-report/canonical/{production,development,summary}.json`.

### T9-HARNESS-04 — building then running the development lane first would invalidate the production build

- **Observed.** Design-time. `next dev` and `next build` share `.next`.
- **Root cause.** Single build directory for both modes.
- **Fix.** The runner orders the lanes production-first, immediately after the build. Documented
  inline in the script so the order is not reshuffled as cosmetic.
- **Proof.** Both canonical runs measured a build produced moments earlier in the same invocation, with
  `"frontend_mode": "production"` recorded in the evidence JSON.

### T9-HARNESS-05 — no clean-start guarantee

- **Observed.** Nothing checked, before a run, whether a previous run still owned live processes.
  `acquireEnvironmentLock` catches a concurrent run, but a crashed run that left a live API or worker
  behind would have been reclaimed silently.
- **Root cause.** No pre-flight in any entrypoint; there was no top-level entrypoint to put one in.
- **Fix.** `assertCleanStart()` reads `RUN_STATE_FILE_PATH`, probes only the PIDs it records, and
  exits non-zero with the exact PIDs and file path if any is alive. It never terminates anything.
- **Proof.** Both canonical runs were started from a verified clean state — no run-state file, no lock
  file, no E2E-owned process — and the runner logged `clean start`.

## 13. Product defects found

**NONE.** No production code was changed and no product failure was observed. T9 changed test
infrastructure only.

## 14. Files changed

| file | change |
|---|---|
| `frontend/playwright.config.ts` | `PRODUCTION_MODE_SPECS` / `SEPARATE_CONFIG_SPECS` lane classification; mode-derived `testMatch`/`testIgnore`; overridable `outputDir`; opt-in `json` reporter |
| `frontend/scripts/e2e-canonical.mjs` | **new** — the canonical two-lane runner: pre-flight, build, both lanes, aggregate, exit status |
| `frontend/package.json` | `test:e2e:canonical`, `test:e2e:development`, `test:e2e:production` |

Nothing else. No production source file, no spec, no `global-setup.ts`, no `global-teardown.ts`, no
`e2e-infrastructure.ts`, no `next.config.mjs`, no dependency and no lockfile.

## 15. Gates

**Backend** — no production backend change expected or made:

| gate | result |
|---|---|
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `go vet -tags=integration ./...` | clean |
| `go test ./...` | 28 packages `ok`, 0 fail |

**Frontend:**

| gate | result |
|---|---|
| `npm run typecheck` (`tsc --noEmit`) | clean |
| `npm test` | **379 passed / 0 failed** (unchanged from the T8C baseline) |
| `npm run build` | succeeds from the dirty worktree |

**On unit tests for the classification.** None added, deliberately. The only behaviour a unit test
could assert about `PRODUCTION_MODE_SPECS` is that Playwright's own glob matching separates the two
lanes — which the Founder authorization explicitly names as a test not worth writing. The invariant
that *does* matter — `164 + 4 = 168 = the whole discovered suite` — is proved by `--list` in both modes
and re-proved by every canonical run's `summary.json`, against the real matcher rather than a
reimplementation of it.

## 16. Focused validation

Run before the whole suite, per the authorization:

| # | check | result |
|---|---|---|
| A | development lane, representative specs — `s5-infrastructure-smoke` | 3/3 green |
| B | production lane — `s5-playback-performance`, all four viewports | 4/4 green, 3431/3373/3351/3361 ms |
| C | T8B Staff invitation with worker + Mailpit — `t8b-staff-invitation-lifecycle` | 3/3 green |
| D | T8A entitlement lifecycle — `t8a-entitlement-lifecycle` | 4/4 green |

A, C and D ran in one development-lane invocation: **10 passed (1.7m)**, clean teardown, worker exit 0.

## 17. Canonical runs

### Run 1 — 2026-08-24

Started from a verified clean state (no run-state file, no lock file, no E2E-owned process).

```
production   passed 4    failed 0  flaky 0  skipped 0  (25.8s, exit 0)
development  passed 164  failed 0  flaky 0  skipped 0  (844.8s, exit 0)
aggregate    passed 168  failed 0  flaky 0  skipped 0
```

Command exit status: **0**.

### Run 2 — repeatability

A second complete canonical invocation, sequentially after run 1 (never concurrently), again from a
verified clean state — run 1's teardown had already removed the run-state and lock files:

```
production   passed 4    failed 0  flaky 0  skipped 0  (24.6s, exit 0)
development  passed 164  failed 0  flaky 0  skipped 0  (824.9s, exit 0)
aggregate    passed 168  failed 0  flaky 0  skipped 0
```

Command exit status: **0**. Identical counts, identical lane assignment, no flake, no skip.

Run 2 production measurements: phone 3412 ms, tablet 3365 ms, laptop 3353 ms, desktop 3356 ms — all
below the unchanged 5000 ms threshold.

### Cleanup verification, after both runs

- `/var/tmp/gradex-s5-e2e-run-state.json` — absent.
- `/var/tmp/gradex-s5-e2e-environment.lock` — absent.
- No E2E-owned API, worker or frontend process left running.
- `pg_database` contains **none** of the four run IDs this session created; each disposable
  `gradex_playwright_e2e_<runId>` was dropped by its own teardown. Pre-existing databases from
  historical runs were left exactly as found — cleanup targets only the exact name a run created.
- Per-run API logs archived to `/var/tmp/gradex-s5-e2e-evidence/` as before; HTML reports, traces and
  screenshots retained per lane.

## 18. Regression

| tranche | canonical evidence | result |
|---|---|---|
| T5 (MVP-F21) legacy migration | `t2-launch-catalog-data` A–E | green |
| T6 (MVP-F22) academic discovery | `t6-academic-discovery` A–F | green |
| T7 (MVP-F23) learning payload contract | `s5-viewport-evidence` ×4, `s5-expired-entitlement` | green |
| T8A (AD-09) entitlement lifecycle | `t8a-entitlement-lifecycle` A–D | green |
| T8B (AD-13) staff invitation | `t8b-staff-invitation-lifecycle` A–C | green |
| T8C (AD-12) course lifecycle | `t8c-course-lifecycle` A–D | green |

All within the canonical development lane, unmodified. Every previously accepted failure identity is
gone and no new identity appeared.

## 19. Known unrelated bugs — not fixed here

1. **`courses.default_access_ends_at IS NULL` → `ConfirmPurchaseRequest` returns 500 instead of
   `ErrExpiryRequired` / 409.** Product defect, out of T9 scope. It does not affect any canonical E2E
   test: the full suite is green.
2. **Admin Staff page has no rendered pending-invitation list / revocation UI** despite the API and
   the strings existing (T8B observation). Product/UI gap, out of T9 scope.

Neither was touched. GAP-06 (Notifications, Profile, Office Hours, Analytics, Reported Content,
Entitlement Detail, Public Preview Manager) was not touched.

## 20. Repository safety

- `git diff --check` — clean, exit 0.
- No `git reset`, `clean`, `stash`, `restore`, broad `checkout`, repo-wide format, or package
  normalization was run at any point.
- No retained database dropped or truncated; no Docker volume removed; `docker compose down` never
  run. Only the two disposable `gradex_playwright_e2e_<runId>` databases each canonical run creates
  were dropped, by exact name, by their own teardown.
- Unrelated stacks (`gradex-s12-*`, `compose-*`, `gradex-founder-acceptance-postgres-1`,
  `backend-mailpit-1`) untouched and still running.
- All pre-existing dirty working-tree changes preserved; the T9 diff is the three files in §14.
- No production provider credential was added, read, or changed. Mailpit remains the development/test
  email provider; no Resend credential is required by canonical E2E.
