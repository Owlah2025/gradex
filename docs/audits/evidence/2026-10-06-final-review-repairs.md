# Final-review blocking repairs — 2026-10-06

## Scope and authority

The input is `.hardening-campaign/reports/FINAL-REVIEW-1.md`, which rejected
`45e66f0..16aa3c2`. This task repairs its four blocking findings. It grants no
production access, deployment, push, or independent approval. Optional P2/P3
redesigns remain deferred. A fresh independent frozen-range review is required.

The original `GATES-1.md` assertion that all gates passed on committed
`7e0c328` is withdrawn: the independent reviewer reproduced failures in the
committed tree. Working-tree passes were incorrectly attributed to that SHA.
New closure evidence must identify the committed checkout actually tested.

## Repairs and invariants

| Review finding | Repair | Regression contract |
|---|---|---|
| A4-001, Sev2, admin/identity | `3ddcbf6`: User 360 passes its transaction through device history and cooldown reads. | One pool connection suffices; two concurrent requests saturate a two-connection pool without nested acquisition. Active/revoked history and privileged-read audit are preserved; failed audit returns no view and rolls back. |
| A5-001, Sev2, Student playback | `2edd386` and the follow-up recorded in the frozen ledger: HLS/native error handling, source fencing, two automatic reauthorizations per lesson, explicit retry, and teardown. Persistent non-fatal authorization errors have a five-second recovery deadline canceled by successful fragments. | Normal authorization evaluates session/device/entitlement/lease; recovery restores position and play state. Exhaustion shows a recoverable error. Stale requests release their own leases. |
| False-green gate evidence | `2edd386` enumerates compiled unit test files; `1b87319` commits inspected prior formatting, strict payload, owned-media fixtures, and constant-time secret comparison. | Actual tests execute. Forged unknown fields fail with exact MALFORMED_JSON and no course persistence. Privileged mutation/audit assertions remain behavioral. |
| CAT-01, Sev2, catalog/media | `a227226`: real course-owned asset versions, trusted scan/processing fixtures, catalog/admin CI coverage, and logical asset validation/locking. | Wrong owner/course/lesson/kind denials preserve selected video and audit count; composite FK rejects direct cross-course writes. Publication refuses retired assets and serializes concurrent retirement. Legacy-only validation remains supported. |

Correcting catalog fixtures also reproduced a real publication defect: its
READY branch ignored logical asset retirement and locked only the version.
`validateLessonVideoAsset` checks both kinds and retirement and share-locks
both rows when the validator is transactional. The existing dependency
revalidation and controlled concurrency tests exposed the failure before this
repair; their assertions were retained.

The first frozen browser run exposed a conflict with the S5 no-protected-mocks
guard: the initial exhaustion test intercepted the application manifest.
That run failed and was interrupted after 205 passes; it is not a green gate.
The guard is unchanged. The replacement injects real segment 403s in the
run-owned local storage fixture while API authorization/manifests remain live.
It then reproduced a second A5-001 failure: hls.js kept reporting non-fatal
denials and never reached the unavailable UI during 90 seconds of active
playback. The five-second deadline bounds that retry behavior, does not restart
on repeated failures, and is canceled by a successful fragment or teardown.

The stronger outage test then reproduced position loss across sources that
never loaded metadata: an explicit retry resumed at zero rather than the saved
12 seconds (`/var/tmp/gradex-repair-storage-position-before.log`). The player
now retains its last saved position and intended play state until metadata
allows a replacement source to restore them. The assertion remains 12 seconds
after repeated real storage failures and explicit retry.

Inspected prior browser/seeder repairs were continued in `6f4b7fc` and
`7909730`. Course selection uses the existing server search contract; resolved
academic context updates an already-mounted study-plan section. Dynamically
created courses receive their own test media through the protected isolated
E2E seeder. Arabic headings, real audit/security-event reasons, and denied
mutation persistence retain explicit assertions. Unrelated logs, a temporary
review spec, and generated reports were preserved outside the commits.

## Targeted repair evidence

Local PostgreSQL/Redis/MinIO only; backend fixtures use their disposable
`gradex_*_test` databases and browser runs use the isolated E2E database safety
system. No protected application database was reset.

- User 360 `MaxConns=1/2` regression passed, including saturated concurrency,
  history, exact audits, and rollback (`/var/tmp/gradex-repair-user360.log`).
- Full HTTP API integration passed in 687.442s after the fixture repairs
  (`/var/tmp/gradex-repair-httpapi.log`).
- Full catalog integration passed in 300.451s after real-media and publication
  repairs (`/var/tmp/gradex-repair-catalog-complete.jsonl`).
- Catalog ownership/publication concurrency tests passed with `-race`,
  including a READY resource refusal (`/var/tmp/gradex-repair-catalog-race.log`).
- Backend build, ordinary/integration vet, and race-enabled unit tests passed
  (`/var/tmp/gradex-repair-backend-unit.log`).
- Initial frontend lint/typecheck passed and `npm test` executed 861 passing
  tests; with deadline/cancellation regressions, 863 unit tests pass.
- The final short-expiry Chromium playback suite passed 3/3
  (`/var/tmp/gradex-repair-playback-strong-progress.log`, run `muvw6kiy52hfu7cc`). The first
  test proves an old capability returns exact 404 NOT_FOUND, a fresh authorized
  source plays past the saved position, the replacement reporter persists new
  progress at 10 seconds and retains it after navigation, the old lease is gone,
  and SPA navigation releases the current lease. Further tests prove bounded
  real storage-segment 403 failures/user retry with the saved 12-second paused
  position preserved, and terminal media-element recovery. The earlier manifest-
  intercepted exhaustion proof is superseded by this stronger real-storage test.

These were targeted working-tree repair checks. The completed frozen-checkout
ledger below supplies the committed-software closure evidence.

## Frozen-checkout gates

The software checkout is frozen at
`0235c43dc79f486981eac20744c89bc88090aed4`, detached at
`/home/owlah/worktrees/gradex-final-repair-3`. Its tracked working tree was clean
before verification. Closing documentation will be committed separately;
the results below apply to this exact software SHA, not an uncommitted diff.
Logs are under `/var/tmp/gradex-final-repair-gates-3`.

| Gate | Command / proof | Result |
|---|---|---|
| Formatting and whitespace | `gofmt -l .` in backend; `git diff --check 45e66f0 HEAD` | Empty output, pass |
| Repository guards | `./scripts/docs-guard.sh`; `./scripts/expose-guard.sh` | Pass: 288 Markdown files, 21 approved exposure sites |
| Local deployment tooling | `verify-schema-41-rollback.sh`, `verify-schema-42-rollback.sh`, `verify-compose-render.sh` under `deploy/scripts/` | Pass; local rendering and disposable database checks only |
| Backend canonical checks | `go build ./...`; `go vet ./...`; `go vet -tags=integration ./...`; `go test -race -count=1 ./...` | Pass (`backend-unit.log`) |
| Frontend canonical checks | `npm ci`; `npm run lint`; `npm run typecheck`; `npm test`; `npm run build` | Pass; 863 actual unit tests, zero failures/skips (`frontend-*.log`; build in `canonical-e2e.log`) |
| Backend integration | Command below | All 11 packages pass (`backend-integration.jsonl`, no failure actions) |
| Focused concurrency/retry races | Command below | Media, HTTP API, and catalog pass (`targeted-race.log`) |
| Migration CLI | Run-owned `verify-migrations.sh` in the log directory | Fresh/up/idempotent-up/down-one/up reaches clean 57; production-shaped local down refuses and leaves 57; connection-error output redacts the canary password; owned database dropped (`migration-cli.log`) |
| S3 performance | `npx playwright test --config=playwright.s3-performance.config.ts` at the same SHA in `/home/owlah/worktrees/gradex-final-render-673a31c` | 1 passed; synthetic local 4G list/detail p95 LCP 900/2416ms (`s3-performance.log`); this is not an account-capacity benchmark |

All Go commands use `GOTMPDIR=/var/tmp` to avoid the local `/tmp` quota.
The integration and race commands, run from `backend/`, were:

```sh
go test -json -tags=integration -p 1 -count=1 -timeout=20m \
  ./internal/db ./internal/identity ./internal/outbox ./internal/httpapi \
  ./internal/catalog ./internal/catalogpublic ./internal/admin ./internal/ratelimit \
  ./internal/learning ./internal/access ./internal/entitlement

go test -race -tags=integration -p 1 -count=1 -timeout=10m \
  -run 'Multipart|TestAdminUser360UsesOneConnectionAndAuditsConcurrentReads|TestD5ApprovalDependencyLocksSerializeConflictingWrites|TestD5ApprovalRevalidatesEveryDependencyClass|TestLesson.*Binding|TestLessonFileAttachmentRetry' \
  ./internal/media ./internal/httpapi ./internal/catalog
```

The exact User 360 regression is selected by name in this final race command;
an earlier trial using a `.*Pool` expression selected no such test and is not
evidence for that regression. The selected catalog tests cover real dependency
retirement, kind/binding refusals, and concurrent publication writes.

`npm run test:e2e:canonical` rebuilt the frontend from this checkout, then ran
the production lane followed by the development lane. Both exited zero:

| Lane | Passed | Failed | Flaky | Skipped |
|---|---:|---:|---:|---:|
| Production | 4 | 0 | 0 | 0 |
| Development | 637 | 0 | 0 | 3 |
| Aggregate | 641 | 0 | 0 | 3 |

The production lane took 36.2s and development 3735.5s. The owned databases
were `gradex_playwright_e2e_muvwd0e79s7kogjf` and
`gradex_playwright_e2e_muvwe2a96mcnbnfd`, respectively, under
`/var/tmp/gradex-final-repair-e2e-3`. The canonical runner's JSON summaries
were copied to `canonical-report/` in the log directory. The S5 no-protected-
mocks guard, real progress/journey tests, Arabic heading assertions, privileged
mutation/audit assertions, and S11 release acceptance all passed in this run.

The three declared skips are the English and Arabic production landing smoke
cases (excluded by their mode guard in the development lane) and the existing
UX-I read-count `fixme`. The production canonical selection runs T076, so it
does not establish a pass for those two landing cases. No skipped case is
counted as a pass; these are retained coverage boundaries.

The standalone lanes then ran sequentially in the same frozen checkout, with
`GOTMPDIR=/var/tmp`, `NODE_OPTIONS=--max-old-space-size=6144`, and
`NEXT_TELEMETRY_DISABLED=1`. Each had a distinct run-owned
`GRADEX_E2E_TMP_DIR` and external HTML report directory so canonical evidence
was preserved:

| Command from `frontend/` | Result | Run / evidence |
|---|---|---|
| `npm run test:e2e:playback-recovery` | 3 passed (1.4m) | `muvyn1hnq0kzzshq`, `/var/tmp/gradex-final-repair-playback-3`; `playback-recovery.log`, `playback.json`, `playback-report/` |
| `npm run test:e2e:release` | 1 passed (50.9s) | `muvyotyvudmq09v4`, `/var/tmp/gradex-final-repair-release-3`; `release.log`, `release.json`, `release-report/` |
| `npm run test:e2e:media-authoring` | 9 passed (6.2m) | `muvypy7cvguq0837`, `/var/tmp/gradex-final-repair-media-3`; `media-authoring.log`, `media-report/` |

Playback repeats the real expiry/new progress proof and the saved-position
storage outage regressions described above. Media authoring uses local MinIO
and the real worker/FFmpeg. It includes the committed
`resumable-upload.spec.ts`: part interruption, reload/reselection recovery,
ownership refusal, and provider cancellation. The backend multipart race lane
separately proves lost creation/provider responses and concurrent completion
retry convergence. The unrelated untracked review duplicate is absent from this
clean checkout and does not inflate the count. Worker READY processing,
attachment persistence, Admin candidate preview, and protected Resource/Lab
Material bytes with revision isolation also pass.

All listed gates have completed with zero failures on the frozen software
SHA. No test assertion or security control was weakened. The earlier failed
or interrupted trials are retained as defect evidence, not green gates.
This closes the assigned blocking-repair task locally; it does not grant the
independent APPROVE verdict required for overall campaign completion.

## Remaining boundaries and risks

Independent final approval is pending. Actual Safari playback and production
R2/scanner acceptance are separate from local Chromium/MinIO evidence. Before
a future release, preflight legacy lesson-video IDs for migration 0054 and
check strict-JSON client compatibility. No production read was made here.
RU-02 cleanup fairness, provider calls holding locks, unbounded histories,
remaining authorization-matrix gaps, the recorded suspension lock-order/
iteration risks, existing C1 intermittency, and dependency/deprecation triage
remain non-blocking follow-ups for this repair task. No capacity benchmark or
overall campaign completion is claimed.
