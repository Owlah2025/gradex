# Gradex Launch Status

> Current schedule date: 2026-07-29 — advanced by user
> Last repository reconciliation: 2026-07-29 — start-of-day, HEAD `f39257b`
> Scheduled day: Day 6 — Closed at `f39257b`; Day 7 (July 29) is active
> Target public go-live: 2026-08-15
> Days remaining after today: 17 calendar days
> Launch confidence: **Red**

Red means the full-MVP public-launch forecast is not yet credible. The documentation baseline,
platform architecture, and domain/data/state design are approved, but API/security design and
implementation are not complete, the operating envelope remains provisional, and the founder
deliberately deferred all external-owner outreach to August 6. All 20 required entries in
[LAUNCH_GATES.md](../LAUNCH_GATES.md) remain open, with several still required by August 9 or 12.
This compressed response window can move the readiness-gated August 15 launch. The complete
July 27 API/security/integration design is developer-approved, has passed independent read-only
review at exact range `1a388cb..d6b4991` with no critical or high finding, and Day 5 is closed.
Confidence stays Red because it is driven by the 20 open launch gates and the absence of any
production implementation, not by design-review state. Three consecutive design days have now closed
on schedule with no carryover, which is the evidence that would move confidence once implementation
starts landing.

## Current Phase

Day 5 is closed with no carryover. Day 6 (July 28) is open and `PLANNED` — see
[the July 28 record](daily/2026-07-28.md): approve the combined M1 architecture baseline, convert the
July 25/26/27 designs into dependency-ordered feature slices, and build the delivery foundation. It
is the last day before July 29 begins the Authentication/RBAC implementation, and the first day this
project produces production application code rather than design.

Every Day 6 gap is now closed. The delivery foundation exists and is verified on hosted
infrastructure rather than only on the developer's workstation: typed two-layer configuration with
fail-closed validation, structured logging behind a closed field allowlist, per-attempt trusted
request IDs, the RFC 9457 Problem Details envelope across all of `/api/v1`, liveness and readiness
probes, repository-owned migrations under `cmd/migrate`, and a four-job CI pipeline with a
documentation guard and a secret-exposure guard.

This is the first day the project produced production application code. Nine commits land it, from
`4d4bbe8` through `7bd4d84`.

Delivery roles changed on 2026-07-25 under
[D-032](../DECISIONS.md#d-032--claude-builds-agy-reviews): Codex exhausted its quota, Claude took
the builder/planner seat, and `agy` took the independent reviewer seat.

Repository evidence at the latest reconciliation:

- Current branch: `feature/002-authentication-rbac`.
- Approved documentation/specification baseline: `1f63a59`
  (`docs: reconcile authentication and coupon specs`), including the reviewed commit chain
  `7ebfcd3..1f63a59`.
- The frontend contains the landing-page implementation.
- The backend contains development scaffolding plus a video-processing/playback slice; the current
  auth seam is development-only.
- Authentication/RBAC is ready for planning, and Coupons have planning artifacts.

Re-evaluate these facts from Git and the repository at every `Start the day`; do not keep stale
claims merely because they appear here.

## Active Outcome

Complete the independent-review and written-artifact-review evidence for the approved MVP
API/security/integration design, then let July 28 create dependency-ordered implementation slices
without inventing an externally visible contract or trust-boundary decision.

## Milestones

| Milestone | Target | Status | Evidence |
|---|---|---|---|
| M0 — Launch control and approved baseline | July 23 | Completed | Baseline `1f63a59`; Claude verdict `APPROVE BASELINE`; zero critical/high findings |
| M1 — Platform architecture baseline | July 28 | **Completed** | [M1_ARCHITECTURE_BASELINE.md](M1_ARCHITECTURE_BASELINE.md) combines July 25 `c9c2238`, July 26 `2e4f3e1`, and July 27 `6862db5`; cross-design reconciliation found no conflicting authority; the focused §4.5/§7.1 implementation-readiness review passed all thirteen required properties with no amendment. Developer sign-off `APPROVED` at `4d4bbe8`, with four obligations carried into [SLICES.md](SLICES.md). Delivery foundation (S0) still in progress today |
| M2 — Authentication/RBAC vertical slice | July 29 | In progress | Acceptance tests and reviewed implementation |
| M3 — Product/revenue journey | August 5 | Not started | Authoring through verified entitlement |
| M4 — Complete MVP operations | August 9 | Not started | Admin/Instructor, office hours, notifications, payouts |
| M5 — Integrated production candidate | August 12 | Not started | E2E, infrastructure, security, load, accessibility |
| M6 — Staging acceptance | August 13 | Not started | UAT and all-gate audit |
| M7 — Production soft launch | August 14 | Not started | Smoke tests and rollback rehearsal |
| M8 — Public go/no-go | August 15 | Not started | Every criterion in PLAN.md §8 |

## Carryover

No incomplete July 26 or July 27 `Must` or `Should` work. The July 27 `Could` item — non-binding JSON
examples — was deferred by developer decision; the contracts are binding as written, so deferring
illustration removes no acceptance evidence and it is not carryover.

External-gate contact confirmation and outreach are deliberately scheduled for August 6 by founder
decision and remain tracked risks rather than hidden carryover. The untracked financial spreadsheet
and `.caveman.json` are user-owned, intentionally untouched, and outside the active slice.

## Current Blockers and Risks

| Item | Owner | Next action | Deadline | Required evidence |
|---|---|---|---|---|
| Required launch gates are all open | Role owners in LAUNCH_GATES.md | Replace placeholders and send the deferred outreach pack | August 6 | Named contacts plus acknowledged requests/delivery dates |
| S1 carries four bootstrap obligations that no earlier slice covers | Claude | Implement the fixed S1 bootstrap chain and its five required tests | July 29 | [SLICES.md §5](SLICES.md#5-s1--identity-sessions-and-rbac-july-29) chain complete with all five tests passing |
| Landing FAQ still promises fixed 150-day access | Developer + Claude | Replace the stale copy when implementing D-026 | Before public release | UI copy and tests reflect the snapshotted Course expiry |
| External lead times can outlast the remaining launch window | Developer/founder | Contact counsel, accounting, Tap, email, hosting, scanner, and content owners | August 6 | Acknowledged requests with delivery dates compatible with the August 9/12 gates |

## Required Launch Gates

| Status | Count |
|---|---:|
| Open | 20 |
| Resolved | 0 |
| Deferred | 0 |

Fast-follow gates are outside this count. Recalculate from
[LAUNCH_GATES.md](../LAUNCH_GATES.md) whenever it changes.

## Latest Verified Checks

- Hosted CI on `feature/002-authentication-rbac` demonstrated green → fail → green: run
  `30169408259` at `7f942cd` all green; run `30169530354` at `aae5039` failed **only** the Guards
  job at the Documentation guard step while the other three stayed green; run `30169635035` at
  `654e63b` green after the revert; run `30169979735` at `7bd4d84` green with the review fixes. CI
  is therefore proven to enforce, not merely to pass, and to isolate failures by area.
- Backend `gofmt`, `go build`, `go vet` on the default and `integration` tags, and `go test -race`
  all pass. Frontend `npm ci`, `lint`, `typecheck`, and `build` all pass.
- Migration lifecycle verified against real PostgreSQL: empty → `up` → expected tables, foreign keys
  and version 1 → repeated `up` is a no-op → `down` empties → `up` again. Dirty-state and
  unsupported-schema-version detection both verified. A production `down` migration is refused, and
  a canary password placed in the DSN never reaches failure output.
- `scripts/docs-guard.sh` passes across 106 Markdown files; `scripts/expose-guard.sh` passes with 5
  approved call sites. Both were negative-tested and fail as intended.
- `git diff --check` passed at Day 5 close.
- Documentation guard passed across 45 Markdown files at Day 5 close: zero missing local links, zero
  invalid JSON examples, and every `DECISIONS.md` anchor referenced by a changed document resolves —
  including the new D-032 anchor referenced from `CLAUDE.md`, `AGENTS.md`, `PLAN.md`, `STATUS.md`,
  the July 27 record, and the review brief template. The prior full-baseline screen-reference and
  SpecKit-manifest checks remain valid because those artifacts did not change.
- The Day 5 independent-review harness was verified end to end: `agy help` and `agy models` succeed,
  `gemini-3.1-pro-high` is available, the reviewer's `touchedFiles` was `[]`, the disposable worktree
  was removed, and the developer's `agy` settings file was restored byte-identical after the run.
- No frontend or backend source file changed on July 27, so the frontend and backend gates below
  remain the latest verified application state and were not rerun for a documentation-only day.
- SpecKit CLI reports `0.13.4`; all five Bash workflow scripts are executable (`755`).
- Frontend `typecheck`, `lint`, and production `build` passed.
- Backend `make build` and `make test` passed.
- The required gate register contains 20 entries and all 20 currently have status `OPEN`.
- The July 26 owner-approved design was self-reviewed against the current `0001_init` schema,
  direct-asynq video path, fake access seam, and current frontend access copy. Exact corrected
  commit `2e4f3e1` passed independent review with zero critical/high findings and every advisory
  disposition resolved.
- Start-of-day July 27 reconciliation confirmed no frontend/backend changes from `1f63a59` through
  `1a388cb`; application checks were not rerun for the documentation-only start. The current Go API
  remains a video-slice `/api/v1` surface with development-only fake identity/Entitlements.
- July 27 developer-approved API/security/integration design commit 6862db5 passed its
  documentation guard: no whitespace errors or placeholders, valid internal design links, planned
  contracts clearly separated from the current fake-auth/video seam, and source citations checked.
- July 24 closed with a clean worktree before its launch-control closeout and no application
  changes; no test or independent-review rerun was required.

## Latest Review

The Day 6 delivery foundation passed independent read-only review at exact range
`1cce2c4..654e63b`, reviewed by `agy` on `gemini-3.1-pro-high` under
[D-032](../DECISIONS.md#d-032--claude-builds-agy-reviews): **0 critical, 0 high, 0 medium, 2 low**,
verdict **APPROVE WITH FINDINGS**, all nine review dimensions reported verified. Read-only was proven
structurally: the reviewer ran in a disposable detached worktree at the frozen commit and its
workspace was confirmed unmodified afterwards.

Both low findings were confirmed empirically before being accepted, then fixed in `7bd4d84`: a
`Secret.LogValue` signature that did not actually satisfy `slog.LogValuer`, and a `truncate` that
could split a multi-byte character. Neither was a security regression — redaction held through a
fallback — but both weakened a guarantee the code claimed to make. No critical or high finding
required rechecking.

Earlier: the July 27 API/security/integration design passed independent read-only review at exact
range `1a388cb..d6b4991`, reviewed by `agy` on `gemini-3.1-pro-high` under
[D-032](../DECISIONS.md#d-032--claude-builds-agy-reviews): **0 critical, 0 high, 0 medium, 0 low**,
verdict **APPROVE**, with all nine review dimensions reported verified. The reviewer ran in a
disposable worktree at the frozen commit and its workspace was asserted unmodified afterwards.

One earlier dispatch returned a valid low finding (duplicate `### Session` heading in
`DOMAIN_MODEL.md`) but was discarded as evidence because the live repository changed mid-run. The
finding was confirmed against the file and fixed in `b4d101e` before the recorded review ran. A
review that cannot prove it was read-only is not downgraded to a weaker approval — it is discarded.

Earlier: Claude's independent review of domain-design commit `5ba126c` returned 0 critical, 0 high,
1 medium, and 4 low findings with verdict **APPROVE DOMAIN DESIGN**; exact corrected commit
`2e4f3e1` then passed final read-only verification with every disposition resolved.

## Decisions in Force

- Six workdays per week, with July 24, July 31, and August 7 protected for recovery/spillover.
- Daily capacity is 8–10 focused hours.
- The full current PRD is the release target.
- August 15 is readiness-gated.
- D-032: Claude is the primary builder and planner; `agy` (Antigravity CLI, `gemini-3.1-pro-high`) is
  the independent read-only reviewer. Codex held the builder seat until 2026-07-25 and was replaced
  when its quota was exhausted. Reviews run through `scripts/agy-review.sh`; a `TAINTED` or
  `UNAVAILABLE` run is never recorded as an approval.
- Missed work becomes visible carryover and cannot be marked complete without evidence.
- The approved documentation/specification baseline ends at commit `1f63a59`.
- Claude is the default SpecKit integration; the Codex integration remains installed but unused.
- Local `gradex-spec-review.zip` bundles are generated review artifacts and are ignored.
- The four consolidated external/operations messages are prepared as drafts in the
  [August 6 outreach pack](outreach/2026-08-06-launch-gate-outreach.md); `DRAFT` never counts as
  sent evidence.
- Founder decision on 2026-07-23: external/provider outreach is deferred to August 6. That
  scheduling decision did not change required gate statuses.
- D-025: use a split managed PaaS around the modular monolith; the edge frontend, Go API, Go worker,
  PostgreSQL, Redis, and object-storage/CDN boundaries scale independently without hard-coding
  providers.
- D-031: preserve authentic legacy identity/content/Media/Learning state through forward-only
  context cutovers; fake access never becomes commercial provenance and post-switch authority only
  moves forward.
- Production approval requires no unresolved critical defect. A high-severity defect requires
  documented risk acceptance, mitigation, and owner approval.

## Current Next Task

All Day 6 `Must`, `Should`, and `Could` work is complete with recorded evidence, and the independent
review is resolved. Formal closure awaits the developer's `Close the day` command.

Next: July 29 (Day 7) — the Authentication/RBAC vertical slice, S1 in
[SLICES.md](SLICES.md#5-s1--identity-sessions-and-rbac-july-29). First action is the fixed bootstrap
chain, which must be worked in order and cannot be started out of sequence: bootstrap schema/state →
controlled bootstrap command → restricted-session principal/policy → password-change completion →
session rotation and restriction removal → normal Admin authorization. Its five required tests are
close conditions, not optional coverage.
