# Gradex Launch Status

> Last reconciled: 2026-07-23 18:28 EEST
> Scheduled day: Day 1 — Establish launch control
> Target public go-live: 2026-08-15
> Days remaining after today: 23 calendar days
> Launch confidence: **Red**

Red means the full-MVP public-launch forecast is not yet credible. The Day 1 documentation baseline
is approved, but system design has not started, all 20 required entries in
[LAUNCH_GATES.md](../LAUNCH_GATES.md) remain open, and external-owner outreach has not yet been
confirmed. This is a forecast, not a rejection of the approved baseline.

## Current Phase

Product, experience, and feature documentation is reconciled, independently reviewed, committed,
and approved for platform system design. The July 24 recovery day is reserved for external-gate
outreach; platform architecture starts July 25.

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

Confirm the named external contacts and initiate the July 24 actions in
[LAUNCH_GATES.md](../LAUNCH_GATES.md), while preserving the approved baseline for the July 25
architecture session.

## Milestones

| Milestone | Target | Status | Evidence |
|---|---|---|---|
| M0 — Launch control and approved baseline | July 23 | Completed | Baseline `1f63a59`; Claude verdict `APPROVE BASELINE`; zero critical/high findings |
| M1 — Platform architecture baseline | July 28 | Not started | Required system-design artifacts and reviews |
| M2 — Authentication/RBAC vertical slice | July 29 | Not started | Acceptance tests and reviewed implementation |
| M3 — Product/revenue journey | August 5 | Not started | Authoring through verified entitlement |
| M4 — Complete MVP operations | August 9 | Not started | Admin/Instructor, office hours, notifications, payouts |
| M5 — Integrated production candidate | August 12 | Not started | E2E, infrastructure, security, load, accessibility |
| M6 — Staging acceptance | August 13 | Not started | UAT and all-gate audit |
| M7 — Production soft launch | August 14 | Not started | Smoke tests and rollback rehearsal |
| M8 — Public go/no-go | August 15 | Not started | Every criterion in PLAN.md §8 |

## Carryover

No documentation-remediation carryover. Founder confirmation of named external contacts and
outreach moves to the July 24 recovery day.

## Current Blockers and Risks

| Item | Owner | Next action | Deadline | Required evidence |
|---|---|---|---|---|
| Required launch gates are all open | Role owners in LAUNCH_GATES.md | Founder confirms named contacts and sends the dated July 24 outreach | July 24 | Named contacts plus acknowledged requests/delivery dates |
| Full MVP is not yet decomposed behind an architecture baseline | Developer + Codex | Complete Days 2–5 of the system-design schedule | July 28 | Approved architecture and dependency-ordered delivery slices |
| External lead times can outlast the build | Developer/founder | Contact counsel, accounting, Tap, email, hosting, scanner, and content owners on the recovery day | July 24 | Acknowledged requests with delivery dates |

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
- Documentation guard passed across 38 Markdown files: zero missing local links, invalid JSON
  examples, undefined BR/D/screen references, or SpecKit manifest mismatches.
- SpecKit CLI reports `0.13.4`; all five Bash workflow scripts are executable (`755`).
- Frontend `typecheck`, `lint`, and production `build` passed.
- Backend `make build` and `make test` passed.
- The required gate register contains 20 entries and all 20 currently have status `OPEN`.

## Latest Review

Claude completed an independent read-only review of the full tracked diff and relevant untracked
baseline files. Verdict: **APPROVE BASELINE**, with zero critical/high findings. It explicitly
verified display-name propagation, paid/free-grant idempotency, office-hours lifecycle, report
targets, public FAQ promises, SpecKit provenance, constitution history/links, and launch-gate
actionability.

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

## Tomorrow's First Task

On July 24, confirm named contacts and send the due external-gate requests. If those actions are
already acknowledged, prepare the load, budget, availability, and recovery inputs for the July 25
platform-architecture session; do not pull new feature scope into the recovery day.
