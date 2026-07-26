# Gradex Launch Status

> Current schedule date: 2026-07-31 — advanced by user
> Last repository reconciliation: 2026-07-31 — synchronized start-of-day HEAD `d17a367`
> Scheduled day: Day 9 — Authentication and RBAC, S1B2 Authenticated sessions, `IN_PROGRESS`
> Target public go-live: 2026-08-15
> Days remaining after today: 15 calendar days
> Launch confidence: **Red**

Red means the full-MVP public-launch forecast is not yet credible. The documentation baseline,
platform architecture, and domain/data/state design are approved, but API/security design and
implementation are not complete, the operating envelope remains provisional, and the founder
deliberately deferred all external-owner outreach to August 6. All 21 required entries in
[LAUNCH_GATES.md](../LAUNCH_GATES.md) remain open, with several still required by August 9 or 12.
This compressed response window can move the readiness-gated August 15 launch. Confidence stays Red
because it is driven by the 21 open launch gates and by how little of the product is implemented —
not by design-review state, which is sound. S1B's approved three-part split adds two critical-path
days, moves S1C to August 2 and S2 to August 3, and leaves six downstream slices competing for four
dates before the fixed August 8 runway. `LG-021` also adds an unresolved production dependency for
compromised-password screening. The delivery foundation, S1A, and S1B1 are sound, but the remaining
full-MVP forecast is not yet credible.

## Current Phase

Day 7/S1A is `CLOSED` — see [the July 29 record](daily/2026-07-29.md). Day 8/S1B1 is also
`CLOSED` — see [the July 30 record](daily/2026-07-30.md). Student registration, verification
request/resend, exact-once verification consumption, policy retrieval, layered abuse controls,
durable protected delivery intent, and bilingual admission screens closed at reviewed
implementation head `ad1b8f6`. The final independent result was 0 critical, 0 high, 2 medium, and
7 low with verdict `APPROVE WITH FINDINGS`.

Day 9/S1B2 is `IN_PROGRESS` — see [the July 31 record](daily/2026-07-31.md). Its single outcome is
an Active Account signing in through the same-origin cookie boundary, safely rotating one
server-authoritative independently revocable family, and logging out. Implementation remains behind
the written-design review and executable-plan gates.

**S1B was split three ways on 2026-07-30 by developer decision.** Detailed reconciliation showed
that registration, rotating sessions, recovery, abuse controls, delivery intent, and bilingual UI
still did not fit one 8–10 hour envelope. [PLAN.md §2](PLAN.md#daily-capacity) required splitting
before implementation rather than compressing failure paths:

| Day | Slice | Contents |
|---|---|---|
| Jul 30 | S1B1 — Student admission | Registration, verification, privacy/abuse controls, durable delivery intent, admission screens |
| Jul 31 | S1B2 — authenticated sessions | Role-scoped windows, login, opaque cookie/CSRF rotation and reuse defense, logout, sign-in screens |
| Aug 1 | S1B3 — recovery and integration | Password recovery, all-family invalidation, Student journey, S1B review |
| Aug 2 | S1C — staff lifecycle and enforcement | Invitations, suspension enforcement, full authorization matrix, S1 integration review |
| Aug 3 | S2 — Course authoring and review | Starts only after S1C closes |

No MVP capability left the slice. **S1 does not close until S1C closes**, and no S2 work begins
before it does. **August 7 remains the next protected recovery point** and is not silently spent.
S3–S8 remain `TBD`, and the August 8–15 runway is forecast-at-risk until downstream dates are
reconciled.

The Day 6 delivery foundation is verified on hosted infrastructure rather than only on the
developer's workstation: typed two-layer configuration with fail-closed validation, structured
logging behind a closed field allowlist, per-attempt trusted request IDs, the RFC 9457 Problem
Details envelope across all of `/api/v1`, liveness and readiness probes, repository-owned migrations
under `cmd/migrate`, and a four-job CI pipeline with a documentation guard and a secret-exposure
guard. Nine commits landed it, from `4d4bbe8` through `7bd4d84`.

Day 7 landed all six ordered bootstrap links through `ec8af3b` and two review corrections through
`70b4809`. The one-off command, restricted principal, mandatory password preparation, atomic
password/session/CSRF rotation, other-family revocation, and normal Admin authority transition are
implemented. The complete local gate, hosted CI run `30180591201`, gate-boundary audit, and final
frozen-range independent review all passed S1A's acceptance contract.

Day 8 landed the admission foundation through `f4fb096`, the complete Student admission slice
through `3a9493f`, and independent-review remediation through `ad1b8f6`. The corrected exact range
`3af09bb..ad1b8f6` passed full local PostgreSQL/Redis/MinIO gates, the frontend production build,
documentation and exposure guards, hosted CI run `30210367125`, and clean detached-worktree review.

Delivery roles returned on 2026-07-25 under
[D-033](../DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review): Codex resumed the
builder/planner seat when its quota returned, Claude resumed independent read-only review, and `agy`
remains the approved fallback.

Repository evidence at the latest reconciliation:

- Current branch: `feature/002-authentication-rbac`.
- Current synchronized HEAD/upstream is `d17a367`; reviewed S1B1 implementation head is `ad1b8f6`.
- Final S1B1 independent review covered exact range `3af09bb..ad1b8f6`; hosted CI run
  `30210367125` passed Backend, Frontend, Migrations, Admission Integration, and Guards.
- The frontend contains the landing-page implementation.
- The backend contains the delivery foundation, the legacy video-processing/playback slice, and
  S1A's production Identity schema, bootstrap command, session/password-change core, principal
  resolver, and deny-by-default capability gate. S1B1 adds the public Student admission,
  verification, current-policy, and anonymous-bootstrap routes; the debug auth transport seam
  remains development-only while S1B2 builds authenticated browser sessions.
- The ignored local backend environment now enables development admission without committing local
  keys. Same-origin bootstrap, both localized policy reads, and synthetic registration passed.
- S1A and S1B1 are closed; S1B2 is active. Coupons have planning artifacts.

Re-evaluate these facts from Git and the repository at every `Start the day`; do not keep stale
claims merely because they appear here.

## Active Outcome

Deliver S1B2 authenticated sessions: first close S1B1's two medium review carryovers, then add
role-scoped access windows, generic login, browser session/CSRF rotation, credential-reuse
classification and family revocation, logout, and Arabic/English sign-in/session screens.

## Milestones

| Milestone | Target | Status | Evidence |
|---|---|---|---|
| M0 — Launch control and approved baseline | July 23 | Completed | Baseline `1f63a59`; Claude verdict `APPROVE BASELINE`; zero critical/high findings |
| M1 — Platform architecture baseline | July 28 | **Completed** | [M1_ARCHITECTURE_BASELINE.md](M1_ARCHITECTURE_BASELINE.md) combines July 25 `c9c2238`, July 26 `2e4f3e1`, and July 27 `6862db5`; cross-design reconciliation found no conflicting authority; the focused §4.5/§7.1 implementation-readiness review passed all thirteen required properties with no amendment. Developer sign-off `APPROVED` at `4d4bbe8`, with four obligations carried into [SLICES.md](SLICES.md). Delivery foundation (S0) closed at `f39257b` |
| M2 — Authentication/RBAC vertical slice | July 29–August 2 | In progress | S1A closed at reviewed head `70b4809`; S1B1 closed at reviewed head `ad1b8f6`. S1B2 (Jul 31), S1B3 (Aug 1), and S1C (Aug 2) remain |
| M3 — Product/revenue journey | **TBD — replan required** | Not started | Authoring through verified entitlement; S3–S8 dates are unresolved |
| M4 — Complete MVP operations | **TBD — replan required** | Not started | Admin/Instructor, office hours, notifications, payouts |
| M5 — Integrated production candidate | August 12 | **At risk** | E2E, infrastructure, security, load, accessibility depend on the unresolved downstream map |
| M6 — Staging acceptance | August 13 | **At risk** | UAT and all-gate audit |
| M7 — Production soft launch | August 14 | **At risk** | Smoke tests and rollback rehearsal |
| M8 — Public go/no-go | August 15 | **At risk, readiness-gated** | Every criterion in PLAN.md §8 |

## Carryover

No S1A or S1B1 acceptance blocker carries over; both final reviews approved their slices with no
critical/high finding. S1B1 completed bootstrap request fingerprinting, original-email
preservation, and mandatory compromised-password screening.

S1B2 starts with two explicit medium carryovers: add typed safe internal admission-failure stage
telemetry without exposing raw causes, and reject deterministic compromised-password screening
outside development before any staging bootstrap. S1B2 also owns capability-aware schema readiness
and transport-wide `no-store` on strict-binding errors. S1B3 owns a separate immutable bearer-attempt
evidence boundary; S1C reconciles the safe policy-read Origin wording; S9 retains outbox
dispatcher-health admission. These are scheduled work, not hidden acceptance blockers.

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
| S1B1 review carryovers precede staging/session expansion | Codex | Add typed safe failure-stage telemetry and reject deterministic password screening outside development before S1B2 session work | July 31 | Focused configuration/composition and telemetry tests plus no critical/high review finding |
| Compromised-password production source is unapproved (`LG-021`) | Engineering + security | Shortlist a privacy-preserving provider or licensed offline dataset | August 6/12 | Source/license/privacy/failure-policy evidence and staging validation |
| S1B split moves S1C and S2 two days | Codex | Close S1B1–S1B3 on their bounded evidence, then S1C before S2 | August 2 | Four sub-slices close with exact-range review evidence |
| S3–S8 cannot fit before the fixed August 8 runway | Developer + Codex | Reconcile the downstream calendar without silent compression or silently spending August 7 | July 31 | Dated S3–S8 and credible S9–S16 forecast |
| Landing FAQ still promises fixed 150-day access | Developer + Codex | Replace the stale copy when implementing D-026 | Before public release | UI copy and tests reflect the snapshotted Course expiry |
| External lead times can outlast the remaining launch window | Developer/founder | Contact counsel, accounting, Tap, email, hosting, scanner, and content owners | August 6 | Acknowledged requests with delivery dates compatible with the August 9/12 gates |

## Required Launch Gates

| Status | Count |
|---|---:|
| Open | 21 |
| Resolved | 0 |
| Deferred | 0 |

Fast-follow gates are outside this count. Recalculate from
[LAUNCH_GATES.md](../LAUNCH_GATES.md) whenever it changes.

## Latest Verified Checks

- Start-of-day Day 9 reconciliation at `d17a367`: backend formatting, build, default/integration
  vet, `go test -race ./...`, documentation guard, and exposure guard pass with the writable
  `GOCACHE=/tmp/gradex-go-cache`. Frontend typecheck, lint, and production build pass. The default
  Go-cache attempt was refused by the workspace sandbox before compilation; the supported
  writable-cache rerun passed. The tracked working tree was clean before the Day 9 record was
  created, with only the two user-owned untracked files present.
- Exact reviewed S1B1 head `ad1b8f6` is green locally with PostgreSQL, Redis, and MinIO:
  formatting, build, default/integration vet, `go test -race ./...`, and the complete integration
  suite pass. Frontend lint, typecheck, production build, and responsive Arabic-first visual checks
  pass. `scripts/docs-guard.sh` passes across 117 Markdown files; `scripts/expose-guard.sh` passes
  with 10 approved `Expose` call sites, one password-plaintext boundary, and two reviewed plaintext
  reads.
- Hosted CI
  [run 30210367125](https://github.com/Owlah2025/gradex/actions/runs/30210367125) completed
  successfully on exact reviewed S1B1 head `ad1b8f6`; Backend, Frontend, Migrations, Admission
  Integration, and Guards all passed.
- The complete development path was exercised from the Next frontend through its development-only
  same-origin rewrite to the Go API: anonymous bootstrap, Arabic policy retrieval, eligible
  registration, and a case-variant hidden duplicate passed; both registration outcomes returned
  byte-identical generic 202 responses.
- Exact reviewed head `70b4809` is green locally with PostgreSQL at schema version 4, Redis, MinIO,
  and the
  documented published-video fixture available: backend build, default/integration vet,
  `go test -race ./...`, and `go test -tags=integration ./...` all pass; the integration run includes
  the Identity transaction suite, real PostgreSQL migrations, real MinIO presigning, and the Redis
  video-redelivery case. Frontend clean install, lint, typecheck, and production build pass.
  `scripts/docs-guard.sh` passes across 107 Markdown files, and `scripts/expose-guard.sh` passes with
  9 approved `Expose` call sites, 1 password-plaintext boundary, and 2 reviewed plaintext reads.
- Hosted CI [run 30180591201](https://github.com/Owlah2025/gradex/actions/runs/30180591201) completed
  successfully on exact reviewed head `70b4809`; all four jobs passed.
- S1A closeout commit `f8a15f7` is synchronized with the upstream branch; hosted CI
  [run 30181079456](https://github.com/Owlah2025/gradex/actions/runs/30181079456) passed Frontend,
  Migrations, Guards, and Backend.
- Start-of-day July 29 reconciliation at `90f92ec`: `gofmt` clean, `go build ./...`, `go vet ./...`,
  and `go test ./...` all pass on the default tags. `scripts/docs-guard.sh` passes across 107
  Markdown files. The working tree holds only the two user-owned untracked files.
- Migration `0002_identity_bootstrap` was exercised against real PostgreSQL: every constraint refused
  what it claims to refuse, including a second bootstrap attempt, a non-Argon2id password hash, a
  role change, a mixed-case normalized email, and a verified timestamp on a `PENDING_VERIFICATION`
  Account. The `up`/`down` lifecycle covers all four migrations and CI verifies schema version 4;
  API readiness supports version 2 through 4 because protected routing requires the Identity
  principal tables introduced in version 2.
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
- The required gate register contained 20 entries through S1A; Day 8 adds `LG-021`, so 21 entries
  are now `OPEN`.
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

S1B1 passed final independent read-only review at exact range `3af09bb..ad1b8f6`, reviewed by Claude
Opus at high effort under [D-033](../DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review):
**0 critical, 0 high, 2 medium, 7 low**, verdict **APPROVE WITH FINDINGS**. All nine dimensions were
checked, and the disposable detached worktree remained clean.

The first full-range pass over `3af09bb..3a9493f` correctly rejected the slice with 0 critical,
1 high, 6 medium, and 6 low because production could disable Student registration while still using
the deterministic password-screening fixture for bootstrap Admin creation. `ad1b8f6` made
production screening validation independent of the registration flag and resolved the associated
outbox-contract, timeout, anonymous-bootstrap rate-limit, timing, purpose-binding, signing-key,
limiter-cardinality, policy-cache, Unicode-validation, logging-fixture, and composition-test
findings. The two remaining medium findings and every low disposition are recorded in the
[July 30 closeout](daily/2026-07-30.md#closeout).

Earlier:

S1A passed final independent read-only review at exact range `9bbdd49..70b4809`, reviewed by Claude
Opus at high effort under [D-033](../DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review):
**0 critical, 0 high, 5 medium, 6 low**, verdict **APPROVE WITH FINDINGS**. All nine dimensions were
checked, and the disposable detached worktree remained clean.

Two earlier full-range passes correctly rejected the slice. Range `9bbdd49..479b2e4` found the
mandatory-change recent-authentication bypass (0/1/4/5); `bf8e03a` fixed it. Range
`9bbdd49..bf8e03a` then found other-family revocation skipped for mandatory changes (0/1/5/5);
`70b4809` removed that exception and added integration proof for both password-change flows. An
interrupted run with no retrievable verdict was not counted. The final medium/low dispositions are
recorded in the [July 29 closeout](daily/2026-07-29.md#closeout) and scheduled in
[SLICES.md §5](SLICES.md#5-s1--identity-sessions-and-rbac).

Earlier:

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
  reassigned by the S1 split and now carries S1B2; August 7 remains the next protected recovery
  point.
- Daily capacity is 8–10 focused hours.
- The full current PRD is the release target.
- August 15 is readiness-gated.
- D-033: Codex is the primary builder and planner; Claude is the independent read-only reviewer.
  Review uses a disposable detached worktree and frozen exact commit range. `agy` remains the
  approved fallback under D-032. A `TAINTED` or `UNAVAILABLE` run is never recorded as approval.
- D-034: browser authentication uses one opaque server-managed credential in a `Secure`,
  `HttpOnly`, host-only, `SameSite=Strict` cookie. Controlled renewal rotates the credential and
  CSRF token; confirmed reuse revokes the family. Older dual-token wording is superseded.
- Missed work becomes visible carryover and cannot be marked complete without evidence.
- Developer-approved Day 8 replan: S1B1 July 30, S1B2 July 31, S1B3 August 1, S1C August 2, and S2
  August 3. S3–S8 remain `TBD`; no protected day or later runway date is silently reassigned.
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

Day 9/S1B2 is active in
[SLICES.md §5.3.2](SLICES.md#532-s1b2--authenticated-sessions) and the
[July 31 record](daily/2026-07-31.md).

Review the written D-034/S1B2 design reconciliation, then freeze its executable SpecKit plan and
tasks. Implementation starts with the two medium S1B1 review carryovers before role-scoped windows,
generic login, credential rotation/reuse classification, family revocation, logout, and bilingual
session UI.
