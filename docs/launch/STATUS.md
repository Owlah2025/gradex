# Gradex Launch Status

> Current schedule date: 2026-07-27 — advanced by user
> Last repository reconciliation: 2026-07-27 — user-advanced schedule
> Scheduled day: Day 5 — In progress
> Target public go-live: 2026-08-15
> Days remaining after today: 19 calendar days
> Launch confidence: **Red**

Red means the full-MVP public-launch forecast is not yet credible. The documentation baseline,
platform architecture, and domain/data/state design are approved, but API/security design and
implementation are not complete, the operating envelope remains provisional, and the founder
deliberately deferred all external-owner outreach to August 6. All 20 required entries in
[LAUNCH_GATES.md](../LAUNCH_GATES.md) remain open, with several still required by August 9 or 12.
This compressed response window can move the readiness-gated August 15 launch. The complete
July 27 API/security/integration design is now developer-approved at commit 6862db5 and awaits
independent read-only review plus final written-artifact review.

## Current Phase

Day 5 is in progress. The common HTTP/API/error/event, session, authorization, delivery, provider,
abuse, reconciliation, and security-verification design is written and developer-approved. The
remaining Day 5 evidence is independent read-only review of exact commit 6862db5 and developer
review of the written specification. There is no incomplete July 26 Must or Should work.

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
| M1 — Platform architecture baseline | July 28 | In progress | July 25 architecture approved at c9c2238; July 26 domain/data/state design approved at 2e4f3e1; July 27 API/security design developer-approved at 6862db5, independent review pending |
| M2 — Authentication/RBAC vertical slice | July 29 | Not started | Acceptance tests and reviewed implementation |
| M3 — Product/revenue journey | August 5 | Not started | Authoring through verified entitlement |
| M4 — Complete MVP operations | August 9 | Not started | Admin/Instructor, office hours, notifications, payouts |
| M5 — Integrated production candidate | August 12 | Not started | E2E, infrastructure, security, load, accessibility |
| M6 — Staging acceptance | August 13 | Not started | UAT and all-gate audit |
| M7 — Production soft launch | August 14 | Not started | Smoke tests and rollback rehearsal |
| M8 — Public go/no-go | August 15 | Not started | Every criterion in PLAN.md §8 |

## Carryover

No incomplete July 26 `Must` or `Should` work. External-gate contact confirmation and outreach are
deliberately scheduled for August 6 by founder decision and remain tracked risks rather than hidden
carryover. The untracked financial spreadsheet is user-owned, intentionally untouched, and outside
the active design slice.

## Current Blockers and Risks

| Item | Owner | Next action | Deadline | Required evidence |
|---|---|---|---|---|
| Required launch gates are all open | Role owners in LAUNCH_GATES.md | Replace placeholders and send the deferred outreach pack | August 6 | Named contacts plus acknowledged requests/delivery dates |
| Independent review of July 27 design is pending | Developer + agy | Review the exact July 27 design range read-only; resolve critical/high findings before Day 5 close | July 28 | Reviewed exact commit range, finding dispositions, no unresolved critical/high finding |
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

- `git diff --check` passed.
- Documentation guard passed across 44 Markdown files: zero missing local links or invalid JSON
  examples; July 27 changed-document BR, D, and LG references are all defined. The prior full-baseline
  screen-reference and SpecKit-manifest checks remain valid because those artifacts did not change.
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

Claude's initial independent review of exact domain-design commit `5ba126c` returned 0 critical,
0 high, 1 medium, and 4 low findings with verdict **APPROVE DOMAIN DESIGN**. Exact corrected commit
`2e4f3e1` then passed final read-only verification: every disposition resolved, no new finding at
any severity, no tracked worktree changes, and final verdict **APPROVE DOMAIN DESIGN**.

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

Obtain Claude's independent read-only review of exact commit 6862db5, resolve any critical/high
finding, then have the developer review the written design before Day 5 close and July 28 planning.
