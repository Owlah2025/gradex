# Gradex Launch Status

> Current schedule date: 2026-07-29 — advanced by user
> Last repository reconciliation: 2026-07-29 — bootstrap link 6, HEAD `ec8af3b`
> Scheduled day: Day 7 — Authentication and RBAC, S1A bootstrap/Admin security core, `IN_PROGRESS`
> Target public go-live: 2026-08-15
> Days remaining after today: 17 calendar days
> Launch confidence: **Red**

Red means the full-MVP public-launch forecast is not yet credible. The documentation baseline,
platform architecture, and domain/data/state design are approved, but API/security design and
implementation are not complete, the operating envelope remains provisional, and the founder
deliberately deferred all external-owner outreach to August 6. All 20 required entries in
[LAUNCH_GATES.md](../LAUNCH_GATES.md) remain open, with several still required by August 9 or 12.
This compressed response window can move the readiness-gated August 15 launch. Confidence stays Red
because it is driven by the 20 open launch gates and by how little of the product is implemented —
not by design-review state, which is sound. Four consecutive days have now closed on schedule with no
carryover, and the delivery foundation plus the first identity schema have landed as production code.
That is the evidence that moves confidence, but it needs several more days of accumulation before the
forecast changes.

## Current Phase

Day 6 (July 28) is `CLOSED` at `f39257b` with no carryover; closure was recorded in `9bbdd49`. Day 7
(July 29) is open and `IN_PROGRESS` — see [the July 29 record](daily/2026-07-29.md): S1A, the
six-link bootstrap chain, which must be worked in fixed order.

**S1 was split three ways on 2026-07-29 by developer decision.** It did not fit one 8–10 hour
envelope, so [PLAN.md §2](PLAN.md#daily-capacity) required splitting before implementation rather
than compressing its failure paths:

| Day | Slice | Contents |
|---|---|---|
| Jul 29 | S1A — bootstrap and Admin security core | Six-link chain, five close conditions, gate audit |
| Jul 30 | S1B — Student authentication and session lifecycle | Registration, verification, login, refresh rotation, recovery, Student auth screens |
| Jul 31 | S1C — staff lifecycle and enforcement | Invitations, suspension enforcement, full authorization matrix, S1 integration review |
| Aug 1 | S2 — Course authoring and review | Moved from July 30 |

No MVP capability left the slice. **S1 does not close until S1C closes**, and no S2 work begins
before it does. July 31 was a protected recovery day and now carries S1C, so **August 7 is the next
protected recovery point**.

The Day 6 delivery foundation is verified on hosted infrastructure rather than only on the
developer's workstation: typed two-layer configuration with fail-closed validation, structured
logging behind a closed field allowlist, per-attempt trusted request IDs, the RFC 9457 Problem
Details envelope across all of `/api/v1`, liveness and readiness probes, repository-owned migrations
under `cmd/migrate`, and a four-job CI pipeline with a documentation guard and a secret-exposure
guard. Nine commits landed it, from `4d4bbe8` through `7bd4d84`.

Day 7 has landed all six ordered bootstrap links through `ec8af3b`. The one-off command, restricted
principal, mandatory password preparation, atomic password/session/CSRF rotation, and normal Admin
authority transition are implemented. The complete backend gate and Identity PostgreSQL integration
suite are green locally. The gate-boundary audit, hosted CI, frozen-range independent review, and
remaining close-condition reconciliation still stand between S1A and closure.

Delivery roles returned on 2026-07-25 under
[D-033](../DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review): Codex resumed the
builder/planner seat when its quota returned, Claude resumed independent read-only review, and `agy`
remains the approved fallback.

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

Deliver the secure bootstrap Administrator through its complete six-link chain, with deny-by-default
backend authorization proven against real protected endpoints rather than against a policy function
in isolation.

## Milestones

| Milestone | Target | Status | Evidence |
|---|---|---|---|
| M0 — Launch control and approved baseline | July 23 | Completed | Baseline `1f63a59`; Claude verdict `APPROVE BASELINE`; zero critical/high findings |
| M1 — Platform architecture baseline | July 28 | **Completed** | [M1_ARCHITECTURE_BASELINE.md](M1_ARCHITECTURE_BASELINE.md) combines July 25 `c9c2238`, July 26 `2e4f3e1`, and July 27 `6862db5`; cross-design reconciliation found no conflicting authority; the focused §4.5/§7.1 implementation-readiness review passed all thirteen required properties with no amendment. Developer sign-off `APPROVED` at `4d4bbe8`, with four obligations carried into [SLICES.md](SLICES.md). Delivery foundation (S0) closed at `f39257b` |
| M2 — Authentication/RBAC vertical slice | July 29–31 | In progress | Split into S1A (Jul 29, bootstrap/Admin security core), S1B (Jul 30, Student authentication), and S1C (Jul 31, staff lifecycle and enforcement). All six S1A links are implemented through `ec8af3b`; local/hosted verification and the frozen-range independent review remain before S1A closes. M2 completes at S1C close |
| M3 — Product/revenue journey | August 5 | Not started | Authoring through verified entitlement |
| M4 — Complete MVP operations | August 9 | Not started | Admin/Instructor, office hours, notifications, payouts |
| M5 — Integrated production candidate | August 12 | Not started | E2E, infrastructure, security, load, accessibility |
| M6 — Staging acceptance | August 13 | Not started | UAT and all-gate audit |
| M7 — Production soft launch | August 14 | Not started | Smoke tests and rollback rehearsal |
| M8 — Public go/no-go | August 15 | Not started | Every criterion in PLAN.md §8 |

## Carryover

No incomplete July 28 `Must`, `Should`, or `Could` work; Day 6 closed complete. No incomplete July 26
or July 27 work either. The July 27 `Could` item — non-binding JSON examples — was deferred by
developer decision; the contracts are binding as written, so deferring illustration removes no
acceptance evidence and it is not carryover.

External-gate contact confirmation and outreach are deliberately scheduled for August 6 by founder
decision and remain tracked risks rather than hidden carryover. The untracked financial spreadsheet
and `.caveman.json` are user-owned, intentionally untouched, and outside the active slice.

## Current Blockers and Risks

| Item | Owner | Next action | Deadline | Required evidence |
|---|---|---|---|---|
| Required launch gates are all open | Role owners in LAUNCH_GATES.md | Replace placeholders and send the deferred outreach pack | August 6 | Named contacts plus acknowledged requests/delivery dates |
| S1 carries four bootstrap obligations that no earlier slice covers | Codex | Finish S1A verification and freeze the implemented chain for independent review | July 29 | [SLICES.md §5](SLICES.md#5-s1--identity-sessions-and-rbac) chain complete, all five tests passing, hosted CI green, and reviewer verdict recorded |
| Splitting S1 consumes the July 31 protected recovery day | Developer + Codex | Hold S1A, S1B, and S1C to their days; August 7 is the next protected recovery point and any slip must travel there to find slack | July 31 | All three sub-slices close on their scheduled day with reviewer verdicts |
| S2 moving to August 1 displaces S3, cascading through S8 | Developer | Decide the downstream calendar; S3–S8 are `TBD` in SLICES.md until then | July 30 | Dated S3–S8 assignments that do not silently consume August 7 |
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

- S1A pre-review local gate is green with PostgreSQL at schema version 4, Redis, MinIO, and the
  documented published-video fixture available: backend build, default/integration vet,
  `go test -race ./...`, and `go test -tags=integration ./...` all pass; the integration run includes
  the Identity transaction suite, real PostgreSQL migrations, real MinIO presigning, and the Redis
  video-redelivery case. Frontend clean install, lint, typecheck, and production build pass.
  `scripts/docs-guard.sh` passes across 107 Markdown files, and `scripts/expose-guard.sh` passes with
  9 approved `Expose` call sites, 1 password-plaintext boundary, and 2 reviewed plaintext reads.
- Start-of-day July 29 reconciliation at `90f92ec`: `gofmt` clean, `go build ./...`, `go vet ./...`,
  and `go test ./...` all pass on the default tags. `scripts/docs-guard.sh` passes across 107
  Markdown files. The working tree holds only the two user-owned untracked files.
- Migration `0002_identity_bootstrap` was exercised against real PostgreSQL: every constraint refused
  what it claims to refuse, including a second bootstrap attempt, a non-Argon2id password hash, a
  role change, a mixed-case normalized email, and a verified timestamp on a `PENDING_VERIFICATION`
  Account. The `up`/`down` lifecycle covers both migrations and CI's schema verification moved to
  version 2.
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

- Six workdays per week. July 24 and August 7 are protected for recovery/spillover. July 31 was
  protected until the 2026-07-29 S1 split reassigned it to S1C, leaving August 7 as the next
  protected recovery point.
- Daily capacity is 8–10 focused hours.
- The full current PRD is the release target.
- August 15 is readiness-gated.
- D-033: Codex is the primary builder and planner; Claude is the independent read-only reviewer.
  Review uses a disposable detached worktree and frozen exact commit range. `agy` remains the
  approved fallback under D-032. A `TAINTED` or `UNAVAILABLE` run is never recorded as approval.
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

Day 7 remains active on S1A in [SLICES.md](SLICES.md#5-s1--identity-sessions-and-rbac). All six
ordered bootstrap links are implemented through `ec8af3b`, and the five close conditions have
targeted local evidence. The July 29 architecture-boundary deadline is audited and linked from
[LAUNCH_GATES.md](../LAUNCH_GATES.md#july-29-architecture-boundary-audit).

The complete local gate is green with PostgreSQL, Redis, and MinIO available. Next action is to
freeze and push the exact S1A range for hosted CI and independent Claude review. S1A stays open
until those results are recorded and no critical/high reviewer finding remains.
