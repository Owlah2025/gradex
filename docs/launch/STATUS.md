# Gradex Launch Status

> Current schedule date: 2026-07-26 — advanced by user
> Last repository reconciliation: 2026-07-26 — user-advanced schedule
> Scheduled day: Day 4 — In progress
> Target public go-live: 2026-08-15
> Days remaining after today: 20 calendar days
> Launch confidence: **Red**

Red means the full-MVP public-launch forecast is not yet credible. The documentation baseline and
platform architecture are approved, but detailed domain/API design and implementation are not
complete, the operating envelope remains provisional, and the founder deliberately deferred all
external-owner outreach to August 6. All 20 required entries in
[LAUNCH_GATES.md](../LAUNCH_GATES.md) remain open, with several still required by August 9 or 12.
This compressed response window can move the readiness-gated August 15 launch.

## Current Phase

Day 4 is in progress. July 26 is defining the complete MVP domain/data/state model from the approved
July 25 architecture, canonical rules, existing feature designs, and current video-slice schema.
Design Sections 1–6 are owner-approved: shared foundations; Identity/Catalog/Media;
Commerce/Coupons/Entitlements/Learning; operational modules/Audit/outbox/retention; preservation-
safe migration; and final ownership/transition/protection/retention matrices. Documentation
self-review and the first independent read-only review are complete with zero critical/high
findings; advisory precision corrections are being finalized. There is no incomplete July 25
`Must` work.

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

Approve the July 26 domain/data/state design: entities, ownership, invariants, PostgreSQL
transactions/constraints/indexes, lifecycles, failure states, and retention boundaries.

## Milestones

| Milestone | Target | Status | Evidence |
|---|---|---|---|
| M0 — Launch control and approved baseline | July 23 | Completed | Baseline `1f63a59`; Claude verdict `APPROVE BASELINE`; zero critical/high findings |
| M1 — Platform architecture baseline | July 28 | In progress | [July 25 platform architecture](../superpowers/specs/2026-07-25-platform-architecture-design.md); owner accepted; Claude approved exact commit `c9c2238` with zero critical/high findings |
| M2 — Authentication/RBAC vertical slice | July 29 | Not started | Acceptance tests and reviewed implementation |
| M3 — Product/revenue journey | August 5 | Not started | Authoring through verified entitlement |
| M4 — Complete MVP operations | August 9 | Not started | Admin/Instructor, office hours, notifications, payouts |
| M5 — Integrated production candidate | August 12 | Not started | E2E, infrastructure, security, load, accessibility |
| M6 — Staging acceptance | August 13 | Not started | UAT and all-gate audit |
| M7 — Production soft launch | August 14 | Not started | Smoke tests and rollback rehearsal |
| M8 — Public go/no-go | August 15 | Not started | Every criterion in PLAN.md §8 |

## Carryover

No incomplete July 25 `Must` work. External-gate contact confirmation and outreach are deliberately
scheduled for August 6 by founder decision and are tracked risks rather than hidden carryover.

## Current Blockers and Risks

| Item | Owner | Next action | Deadline | Required evidence |
|---|---|---|---|---|
| Required launch gates are all open | Role owners in LAUNCH_GATES.md | Replace placeholders and send the deferred outreach pack | August 6 | Named contacts plus acknowledged requests/delivery dates |
| Detailed MVP data/API behavior is not yet frozen | Developer + Codex | Complete the July 26–28 system-design schedule | July 28 | Approved domain/data/state and API/security designs plus dependency-ordered delivery slices |
| Landing FAQ still promises fixed 150-day access | Developer + Codex | Replace the stale copy when implementing D-026 | Before public release | UI copy and tests reflect the snapshotted Course expiry |
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
- Documentation guard passed across 43 Markdown files: zero missing local links or invalid JSON
  examples; changed-document BR, D, and LG references are all defined. The prior full-baseline
  screen-reference and SpecKit-manifest checks remain valid because those artifacts did not change.
- SpecKit CLI reports `0.13.4`; all five Bash workflow scripts are executable (`755`).
- Frontend `typecheck`, `lint`, and production `build` passed.
- Backend `make build` and `make test` passed.
- The required gate register contains 20 entries and all 20 currently have status `OPEN`.
- The July 26 owner-approved design was self-reviewed against the current `0001_init` schema,
  direct-asynq video path, fake access seam, and current frontend access copy. Independent review
  remains required.
- July 24 closed with a clean worktree before its launch-control closeout and no application
  changes; no test or independent-review rerun was required.

## Latest Review

Claude independently reviewed exact domain-design commit `5ba126c` read-only against the canonical
rules, launch gates, architecture, and current schema/code seams. Counts: 0 critical, 0 high,
1 medium, and 4 low; verdict: **APPROVE DOMAIN DESIGN**. The advisory findings clarified the
pending earning-snapshot carrier, direct-full-Refund diagram, greenfield Identity migration,
uncapped Coupon capacity, and free-grant payment-deadline nullability. A final exact-commit
verification will confirm those dispositions.

## Decisions in Force

- Six workdays per week, with July 24, July 31, and August 7 protected for recovery/spillover.
- Daily capacity is 8–10 focused hours.
- The full current PRD is the release target.
- August 15 is readiness-gated.
- Codex is the primary builder; Claude is the independent read-only reviewer.
- Missed work becomes visible carryover and cannot be marked complete without evidence.
- The approved documentation/specification baseline ends at commit `1f63a59`.
- Codex is the default SpecKit integration; both Codex and Claude integrations remain installed.
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

Commit the independent-review dispositions, run their documentation guard, and obtain final
exact-commit verification before implementation planning.
