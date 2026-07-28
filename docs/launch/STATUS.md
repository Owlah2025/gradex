# Gradex Launch Status

> Current date: **2026-07-28 (real calendar).** The schedule-day numbering ended at Day 11; from now on
> there is one calendar and it is the real one — see [the execution plan §1](AUGUST_15_EXECUTION_PLAN.md#1-calendar-reconciliation)
> Last repository reconciliation: **2026-07-28 (D5 implemented locally, awaiting frozen review range)**; D5 planning remains frozen at `3e1809d`; application baseline remains `08b8857`
> Active slice: **S2 — Course authoring and review, D5 Phase 5 revision integrity `IN_PROGRESS` (implemented locally, awaiting review).** T001–T031 retain their reviewed closure evidence; T032–T038 are implemented and locally green under D-043. Claude review and hosted CI remain closure gates. Day record: [2026-07-28-d5.md](daily/2026-07-28-d5.md), `IN_PROGRESS`; D4 is `CLOSED` at [2026-07-28-d4.md](daily/2026-07-28-d4.md)
> Plan-day note: **D3 runs one day early** — the execution plan dates it July 29, and D2's work ran on the evening of July 27. The `-dN` suffix tracks the plan day, not the date
> Target public go-live: **2026-08-15 — hard product-owner decision**, restored under [D-040](../DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews). Supersedes [D-039](../DECISIONS.md#d-039--remedy-a-adopted-scope-preserved-public-target-moves-to-september) on the date only
> Days remaining: **18**
> Workflow for S2 D5: Codex specifies and implements with SpecKit → Claude independently reviews and accepts (D-043)
> Launch confidence: **RED** — reverted from Amber on 2026-07-28

## Legal and accounting exposure is accepted, not resolved — D-041

On 2026-07-28 the developer deferred sourcing Kuwaiti counsel and an accountant to the final days and
accepted the resulting exposure, recorded as
[D-041](../DECISIONS.md#d-041--legal-and-accounting-outreach-deferred-to-the-final-days-the-resulting-exposure-is-accepted-rather-than-resolved).

**This resolves nothing.** All 21 gates stay `OPEN` with the same owners, evidence requirements, and
deadlines. Confidence stays **Red**: an accepted risk is not a resolution path.

**Under [PLAN.md §8](PLAN.md#8-public-launch-criteria), criteria 1 and 6 will fail on August 15** —
required gates are not `RESOLVED`, and policies and consent versions are not production-approved. §8
says failure of any criterion is a no-go and does not authorise a reduced public launch unless the
canonical MVP and gate register are explicitly revised and reapproved. **No such revision has been
made.** The launch therefore proceeds against its own stated criteria, knowingly, and that is recorded
rather than smoothed over.

Technical gates are untouched: security, authorization, payment correctness, privacy, data integrity,
and protected-media controls remain non-negotiable and are not part of this acceptance.

**The Tap outreach is not blocked by D-041** — no named contact is required, it carries `LG-007`,
`LG-008`, `LG-010`, and its webhook test vectors are a direct input to S7 on August 10.

## Why Red, as of 2026-07-28

**There is no Kuwaiti counsel and no accountant engaged.** The founder confirmed it at D3 closeout.
Every version of the outreach plan since 2026-07-23 assumed both existed and were merely uncontacted,
so the task was recorded for five days as *send the messages* when it was always *source and engage*.

[PLAN.md §5](PLAN.md#5-launch-confidence) makes confidence Red when **a required gate lacks a credible
resolution path.** `LG-005`, `LG-006`, and `LG-011` are legal cutover-blockers due **August 12** whose
path today begins with finding a lawyer: no owner, no candidate, no dated action, no known sourcing
lead time. Fifteen days out, against one day of float.

`LG-007` is in the same position behind the missing accountant, and `LG-012` launch prices are due
August 11 computed against a revenue share only an accountant can approve.

**This is not the July 29 outreach trigger firing.** That trigger measured a *delay* in sending
messages that had recipients. This is a missing precondition, which is a different and worse fact.

**Engineering is not the cause and cannot be the fix.** D3 closed three of four Musts with independent
verification and hosted CI green. The forecast still got worse, which is exactly why confidence is
driven by the gate register rather than by delivery velocity.

The historical Amber assessment below is retained for the record.

Confidence moved from Red to Amber on 2026-07-27, and the reason was arithmetic rather than optimism.
[D-038](../DECISIONS.md#d-038--august-8-is-no-longer-a-credible-runway-start-s3s8-remain-undated-pending-a-developer-remedy)
measured the remaining runway in *schedule* days and treated them as real days; the repository's
schedule calendar ran six days ahead of the real one, and eleven schedule days (S0 through the S1C
plan) were produced across five real days. Nineteen real days remain, not seven, and the delivery
workflow changed under D-040 so that planning and implementation now run concurrently.

Amber, not Green, because **the 21 open launch gates are unchanged and are now the binding
constraint** — not engineering capacity. `LG-005`, `LG-006`, `LG-007`, `LG-008`, `LG-010`, `LG-011`,
and `LG-012` can each stop the cutover regardless of how much code is finished, and no manual path
substitutes for any of them. The outreach that closes them was scheduled for August 6 and is now due
**July 28**; it is the highest-value schedule action available and it costs no engineering time.

Scope is classified rather than cut blind: every remaining requirement is Launch Critical, Manual but
Supported, or Post-Launch in
[the August 15 scope matrix](AUGUST_15_EXECUTION_PLAN.md#2-scope-matrix). Nothing is silently
dropped, and no security, payment-correctness, authorization, data-integrity, or protected-media
control is traded for the date.

The historical Red assessment below is retained for the record.

Red means the full-MVP public-launch forecast is not yet credible. The documentation baseline,
platform architecture, and domain/data/state design are approved, but API/security design and
implementation are not complete, the operating envelope remains provisional, and the founder
deliberately deferred all external-owner outreach to August 6. All 21 required entries in
[LAUNCH_GATES.md](../LAUNCH_GATES.md) remain open, and their August 9 and 12 deadlines are now
themselves subject to the rebaseline below rather than being fixed. Confidence stays Red
because it is driven by the 21 open launch gates and by how little of the product is implemented —
not by design-review state, which is sound. `LG-021` also adds an unresolved production dependency for
compromised-password screening. The delivery foundation, S1A, and S1B are sound, but the remaining
full-MVP forecast is not credible.

**The downstream calendar was reconciled on 2026-08-02 and the answer is negative
([D-038](../DECISIONS.md#d-038--august-8-is-no-longer-a-credible-runway-start-s3s8-remain-undated-pending-a-developer-remedy)).**
Six slices (S3–S8) have three available dates (August 4–6), and nine feature slices (S2–S10) have six
dates before the August 10 integration runway. **August 8 is no longer a credible runway start, and a
full-PRD August 15 launch is not forecastable.** That result needs no velocity assumption — it is
arithmetic on dates — and the observed velocity makes it worse: S1 was scoped as one day and took five.
**The developer adopted Remedy A the same day
([D-039](../DECISIONS.md#d-039--remedy-a-adopted-scope-preserved-public-target-moves-to-september)):
August 8 and the August 15 full-PRD target are retired as non-credible, full PRD scope is preserved, and
the public target moves into September.** No exact September date is set, and none may be recorded until
the August 6 outreach results exist and S2–S16 are rebaselined against them — "early-to-mid September"
was a forecast hypothesis behind 21 open gates and four uncontacted external dependencies, and replacing
one uncredible date with another would repeat the error being corrected. Remedy B (scope reduction) stays
available afterwards as an optimization of the new plan, not as a rescue of August 15. Remedy C
(compressing the envelope or spending August 7) is **rejected**.

Red therefore no longer means "the August 15 forecast is failing" — that target is gone. It means the
21 open gates and the unimplemented majority of the product still stand between here and any date, and
no credible date exists yet to measure against.

## Current Phase

Day 7/S1A is `CLOSED` — see [the July 29 record](daily/2026-07-29.md). Day 8/S1B1 is also
`CLOSED` — see [the July 30 record](daily/2026-07-30.md). Student registration, verification
request/resend, exact-once verification consumption, policy retrieval, layered abuse controls,
durable protected delivery intent, and bilingual admission screens closed at reviewed
implementation head `ad1b8f6`. The final independent result was 0 critical, 0 high, 2 medium, and
7 low with verdict `APPROVE WITH FINDINGS`.

Day 9/S1B2 is `CLOSED` at reviewed implementation head `7d8710e` — see
[the July 31 record](daily/2026-07-31.md). An Active Account signs in through the same-origin cookie
boundary, rotates one server-authoritative independently revocable family, and logs out. Role-scoped
windows, generic login, generation-bound CSRF, superseded-use classification, family revocation on
confirmed reuse, logout, and bilingual sign-in and session-state screens are implemented. Two
consecutive frozen ranges were independently reviewed by `agy` and both returned `APPROVE` with
0 critical, 0 high, 0 medium, and 0 low.

The slice needed a second range because the first did not pass hosted CI. Migration `0006` raised the
schema to version 6, but the Migrations job still asserted version 5; `7d8710e` corrects it. Two
process facts came out of that failure and are carried into S1B3: the full local gate suite was green
while the range was red on CI, because no local script asserts schema version at all, and an
independent reviewer returned 0/0/0/0 on a range that did not build, because the review dimensions
cover the diff rather than gate execution. **A verdict alone is not evidence that a range passes.**

Day 10/S1B3 is `CLOSED` at reviewed implementation head `9d3db91` — see
[the August 1 record](daily/2026-08-01.md#closeout). **This completes S1B.** Password recovery is
non-enumerating at the transport boundary, reset secrets are digest-only, purpose-bound, expiring,
and single-use under contention, completion atomically replaces the credential while revoking every
family and advancing the session epoch, and the complete Student journey passes end to end through
one cookie jar. The single frozen range `3b2f7a8..9d3db91` returned `APPROVE` with 0 critical,
0 high, 0 medium, and 0 low.

Two defects in already-shipped S1B1 code surfaced while building this slice and were fixed: a
supersession timestamp that inverted under concurrency and returned a 500 on an ordinary second
request, and a one-time-token fragment scrub that hydration silently undid, leaving the secret in the
address bar. Neither was introduced by S1B3.

S1B3 also closed both S1B2 carryovers and opened two of its own, both instances of the same class —
a local gate that reads green while testing less than the hosted one. See Carryover below.

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
before it does. **August 7 remains the next protected recovery point** and is not silently spent —
under D-039 spending it was explicitly rejected. S3–S8 remain `TBD`, and the August 8–15 runway is
retired rather than merely at risk.

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

**Roles moved again mid-S1B2 on 2026-07-31 under
[D-035](../DECISIONS.md#d-035--claude-builds-s1b2-and-agy-reviews), were reassigned for S1B3 under
[D-036](../DECISIONS.md#d-036--claude-builds-s1b3-and-agy-reviews), and are assigned for S1C under
[D-037](../DECISIONS.md#d-037--claude-builds-s1c-and-agy-reviews): Claude builds, `agy` on
`gemini-3.1-pro-high` reviews through `scripts/agy-review.sh`.** Each is scoped to a single slice and
expires at that slice's frozen reviewed head; D-036 expired at the S1B3 closeout and D-037 replaced it
rather than extending it. Seats never renew implicitly, so S2 needs its own dated assignment.

**D-033 remains paused, and its restoration now requires explicit reverification.** Its precondition is
returned Codex quota; no report of quota is not a return of quota, and work must not begin under D-033
until availability is verified rather than assumed.

Under D-037 the **S1 integration review** spanning S1A, S1B, and S1C is also dispatched to `agy`, not
self-checked: its scope contains Claude-authored commits.

The S1B2 history below is retained for the record.

 Codex exhausted its quota with
the S1B2 backend complete but the frontend, verification, and review work outstanding, so Claude
takes the builder/planner seat for the remainder of this slice and `agy` takes the independent
read-only reviewer seat under D-032's containment harness. Codex's S1B2 work is inherited unchanged
from implementation head `24b0d21` plus its uncommitted T013/T019–T029 backend tree. Claude may not
review the S1B2 range it authors. The handoff is temporary: when Codex quota returns, the developer
may explicitly restore D-033's assignment.

Repository evidence at the latest reconciliation:

- Current branch: `feature/002-authentication-rbac`, synchronized with upstream at `881639d`, the S1B3
  closeout commit.
- The latest reviewed implementation head is `9d3db91` (S1B3). Earlier reviewed heads: S1A `70b4809`,
  S1B1 `ad1b8f6`, S1B2 `7d8710e`.
- Start-of-day Day 11 gates pass at `881639d`: backend `gofmt`, `go build ./...`, `go vet ./...`,
  `go vet -tags=integration ./...`, and `go test ./...`; frontend `typecheck`, `lint`, 21 of 21
  `node:test` cases, and a **clean** production build with `.next` removed first; documentation and
  exposure guards.
- The only untracked file is the user-owned `.caveman.json`.
  **`Gradex_Financial_Model_v1.xlsx` is no longer present in the working tree**, contrary to the
  inventory carried in earlier records. It was never tracked and no launch work touched it; the absence
  is recorded as a correction, not acted on.
- The database schema is at version 7. The build supports 2 through 7, and CI derives the expected
  version from `db.MaxSchemaVersion` rather than a hardcoded literal.
- Final S1B1 independent review covered exact range `3af09bb..ad1b8f6`; hosted CI run
  `30210367125` passed Backend, Frontend, Migrations, Admission Integration, and Guards.
- The frontend contains the landing-page implementation.
- The backend contains the delivery foundation, the legacy video-processing/playback slice, and
  S1A's production Identity schema, bootstrap command, session/password-change core, principal
  resolver, and deny-by-default capability gate. S1B1 adds the public Student admission,
  verification, current-policy, and anonymous-bootstrap routes; the debug auth transport seam
  remains development-only. S1B2 added the authenticated session boundary, and S1B3 added password
  recovery request and completion plus the bilingual recovery screens.
- The frontend now contains the complete S1B admission and session journey: registration,
  verification, sign-in, session state, and recovery, in Arabic and English.
- The ignored local backend environment now enables development admission without committing local
  keys. Same-origin bootstrap, both localized policy reads, and synthetic registration passed.
- S1A, S1B1, S1B2, and S1B3 are closed, so S1B is complete. S1C is next and unstarted. Coupons have
  planning artifacts.

Re-evaluate these facts from Git and the repository at every `Start the day`; do not keep stale
claims merely because they appear here.

## Active Outcome

**S1C is `CLOSED` at reviewed head `edd6508`. S1 is complete.** All eleven acceptance items are
satisfied, the full authorization matrix is derived from the live router rather than hand-maintained,
and the staff surface now matches its frozen §7 contract exactly. **S2 implementation is unblocked**
and starts from the frozen plan in
[specs/003-course-authoring/](../../specs/003-course-authoring/plan.md).

**S2 planning is complete and frozen** — spec, plan, research, data model, three API contracts, 64
tasks, and the Antigravity handoff brief. No S2 implementation commit exists yet.

Historical, retained for the record: S1C was rejected three times before closing. The first rejection
followed Antigravity reviewing its own range and returning `APPROVE — 0 critical` while missing three
criticals; that self-review is recorded as a process violation rather than absorbed.

Seats are **[D-040](../DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews)**,
standing and not per-slice: Claude plans with SpecKit, Antigravity implements, Claude reviews and
accepts. D-037's per-slice assignment is superseded; `agy` is the dispatch mechanism for Antigravity
rather than a reviewer seat. The frozen plan is
[specs/002-auth-rbac/s1c/](../../specs/002-auth-rbac/s1c/plan.md).

**A process violation is recorded rather than absorbed.** After implementing `6a9e2da`, Antigravity
reviewed its own range and returned `APPROVE — 0 critical`. That is a self-check, it cannot close a
slice, and it missed every finding below. The rule it broke is the one this project has held since
D-032: a slice never closes on its builder's own assessment.

Two remediation rounds have run. **Round two was killed by an Antigravity individual quota limit
(`RESOURCE_EXHAUSTED`) six minutes in**, after closing two of its four findings, and left `router.go`
uncompilable. Claude restored the deleted `v1 := r.Group("/api/v1")` declaration and the unimported
`errors` reference — **reviewer-authored repair inside a range Claude reviews, recorded here because
it must be visible at closeout.** No behaviour beyond that repair is Claude-authored.

S1C's inherited inputs are separated in the daily record into three kinds with different obligations —
**functional** work to build, **policy** calls to confirm or overturn, and **gate-fidelity** carryovers
to fix before their own evidence is trusted. The gate-fidelity fixes run first, because one of them
already misreported at start of day.

Earlier: S1B3 recovery and Student integration is delivered and closed, completing S1B.

All local gates are green: backend formatting, build, vet on both tag sets, `go test -race ./...`,
and the complete integration suite under race against real PostgreSQL at schema 7, Redis, and MinIO;
frontend typecheck, lint, 21 `node:test` logic cases, and a clean production build with `.next`
removed first; documentation and exposure guards. Hosted CI passed all five jobs on the exact
reviewed head `9d3db91`.

Migration `0007` widened the closed action-secret purpose and security-event allowlists. The CI
schema assertion now derives its expected version from `db.MaxSchemaVersion` through a
`migrate max-version` subcommand, and it tracked to schema 7 with no manual edit — the exact drift
that failed S1B2's hosted CI.

Two defects in already-shipped S1B1 code surfaced and were fixed while building this slice: a
supersession timestamp that inverted under concurrency and returned a 500 on an ordinary second
request, and a one-time-token fragment scrub that hydration silently undid, leaving the secret in the
address bar. Neither was introduced by S1B3; both were found because this slice exercised those paths
harder.

Next is Day 11/S1C — staff lifecycle, enforcement, and the full authorization matrix, scheduled for
August 2. S1C inherits four carryovers and three unexamined judgement calls, all recorded below.

## Milestones

| Milestone | Target | Status | Evidence |
|---|---|---|---|
| M0 — Launch control and approved baseline | July 23 | Completed | Baseline `1f63a59`; Claude verdict `APPROVE BASELINE`; zero critical/high findings |
| M1 — Platform architecture baseline | July 28 | **Completed** | [M1_ARCHITECTURE_BASELINE.md](M1_ARCHITECTURE_BASELINE.md) combines July 25 `c9c2238`, July 26 `2e4f3e1`, and July 27 `6862db5`; cross-design reconciliation found no conflicting authority; the focused §4.5/§7.1 implementation-readiness review passed all thirteen required properties with no amendment. Developer sign-off `APPROVED` at `4d4bbe8`, with four obligations carried into [SLICES.md](SLICES.md). Delivery foundation (S0) closed at `f39257b` |
| M2 — Authentication/RBAC vertical slice | July 29–August 2 | **Completed** | S1A `70b4809`; S1B1 `ad1b8f6`; S1B2 `7d8710e`; S1B3 `9d3db91`; **S1C `edd6508`**. All five slices closed on independent verdicts with hosted CI green on each exact reviewed head. **S1 is complete** |
| M3 — Product/revenue journey | **TBD — developer remedy required** | Not started | Authoring through verified entitlement. S3–S8 undated on a recorded verdict, not pending analysis: [D-038](../DECISIONS.md#d-038--august-8-is-no-longer-a-credible-runway-start-s3s8-remain-undated-pending-a-developer-remedy) |
| M4 — Complete MVP operations | **TBD — developer remedy required** | Not started | Admin/Instructor, office hours, notifications, payouts — all downstream of the undated S3–S8 block |
| M5 — Integrated production candidate | August 12 | **Not forecastable** | Depends on S1A–S10; the feature slices feeding it have no credible dates (D-038) |
| M6 — Staging acceptance | August 13 | **Not forecastable** | UAT and all-gate audit, downstream of M5 |
| M7 — Production soft launch | August 14 | **Not forecastable** | Smoke tests and rollback rehearsal, downstream of M6 |
| M8 — Public go/no-go | **September, date unset** | **Retired and awaiting rebaseline** | Every criterion in PLAN.md §8. August 15 retired under [D-039](../DECISIONS.md#d-039--remedy-a-adopted-scope-preserved-public-target-moves-to-september); scope preserved, exact date set only after the August 6 outreach and an S2–S16 rebaseline |

## Carryover

No S1A or S1B1 acceptance blocker carries over; both final reviews approved their slices with no
critical/high finding. S1B1 completed bootstrap request fingerprinting, original-email
preservation, and mandatory compromised-password screening.

S1B2's two inherited medium carryovers are closed in `916bc52`: typed safe internal admission-failure
stage telemetry, and rejection of deterministic compromised-password screening outside development.
Capability-aware schema readiness and transport-wide `no-store` on strict-binding errors are closed
in the same commit.

S1B2's two carryovers are both **closed** by S1B3: `CARRYOVER-S1B2-RETURNTO` by Must 5, which carries
the validated destination across every admission hop, and `CARRYOVER-S1B2-CI-DRIFT` by Must 1, which
derives the CI schema assertion from `db.MaxSchemaVersion`.

S1B3 hands four carryovers and three unexamined judgement calls to S1C, all recorded in the
[August 1 closeout](daily/2026-08-01.md#closeout). **They are accepted into S1C separated by the kind
of obligation they carry** — functional work to build, policy calls to confirm or overturn, and
gate-fidelity gates to fix before their own evidence is trusted — in
[the August 2 record](daily/2026-08-02.md#inherited-inputs). Collapsing the three kinds into one list is
how a policy question gets closed by an implementation that assumed it.

- `CARRYOVER-DOCS-GUARD-UNTRACKED`: **closed** by `8b016d5`, which made the guard enumerate untracked
  Markdown too.
- **`CARRYOVER-DOCS-GUARD-IGNORED-TARGETS` (new, 2026-07-28)**: `scripts/docs-guard.sh` resolves link
  targets against the filesystem rather than against tracked files, so a link into a git-ignored but
  locally present directory passes here and fails in CI. It did exactly that on `5f1188a`. **Fourth
  observed instance of the class — a local gate reading green while testing less than the hosted
  one.** Reproduce with a shallow clone into a temp directory, which is what `actions/checkout` does.
- `CARRYOVER-LOCAL-BUILD-CACHE`: `npm run build` reuses `.next`, so prerender-time failures stay
  invisible locally. This let a `Suspense` defect reach hosted CI. Until fixed, a frontend build
  offered as pre-push evidence must clear `.next` first.
- `CARRYOVER-S1B3-VOLUNTARY-CHANGE-EVIDENCE`: the undelivered S1B3 `Should` — observed evidence that
  voluntary password change revokes another family, rather than restated policy. The recovery path is
  proven; the voluntary-change path is a different code path and is not.
- `CARRYOVER-S1B3-DENIAL-VOCABULARY`: the undelivered S1B3 `Could` — reconcile the S1B-wide
  policy-denial vocabulary against API design §6.1.
- **Three judgement calls the S1B3 review did not address by name**: that `CHANGE_REQUIRED` stays
  recovery-eligible and is cleared by recovery, that role is deliberately absent from recovery
  eligibility so staff retain a self-service path, and the `recovery.go` exposure-guard allowlist
  entry. All three were raised before dispatch and the report returned `OPEN QUESTIONS: none`. S1C
  should confirm or overturn them rather than inherit them as settled.

The first two are the same class — **a local gate that reads green while testing less than the hosted
one** — and two instances surfaced in a single day, which is worth treating as a pattern.

S1C reconciles the safe policy-read Origin wording; S9 retains outbox dispatcher-health admission.
These are scheduled work, not hidden acceptance blockers.

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
| **No Kuwaiti counsel is engaged — blocks LG-005, LG-006, LG-011 (cutover-blocking) plus LG-002, LG-004, LG-020** | Developer/founder | **Source and engage.** Ask the Digital Commerce Law registration/lead-time question first, to several candidates at once — it is the one answer that can move the date rather than the content | **Immediately** | An engaged firm, then acknowledged receipt of the Message 1 brief |
| **No accountant is engaged — blocks LG-007 (cutover-blocking) plus LG-001, LG-016, LG-017** | Developer/founder | **Source and engage.** LG-012 launch prices are due August 11 and are computed against the revenue share only they approve | **Immediately** | An engaged adviser, then an approved revenue-share percentage with an effective date |
| Tap message is sendable today and is not blocked | Developer/founder | Send to Tap's merchant-onboarding intake; it needs no named contact and carries LG-007, LG-008, LG-010 | **2026-07-29** | A `SENT` row with a real timestamp |
| Required launch gates are all open | Role owners in LAUNCH_GATES.md | 21 of 21 `OPEN`, zero `RESOLVED`. Unchanged on 2026-07-28: no gate weakened, merged, or completed on engineering progress | August 6 | Named contacts plus acknowledged requests/delivery dates |
| **Critical-path rebaseline of S2–S16 is owed; no public date exists** | Developer + builder seat | Remedy A is adopted (D-039). Rebaseline S2–S16 against the August 6 outreach results, then set the September date. Do not publish a date before both | **After August 6 outreach returns** | Dated S2–S16 with acknowledged external delivery dates, and a recorded public target |
| Compromised-password production source is unapproved (`LG-021`) | Engineering + security | Shortlist a privacy-preserving provider or licensed offline dataset | August 6/12 | Source/license/privacy/failure-policy evidence and staging validation |
| S1 does not close until S1C closes | Claude, builder under D-037 | Deliver S1C's eleven acceptance items, then the S1 integration review by `agy` | August 2 | S1C closes on a frozen exact range with no critical or high finding |
| Three slices are blocked on external parties not yet contacted | Developer/founder | S4 needs a malware scanner, S6/S7 need live Tap, S9 needs a verified sender, S10 needs counsel and accounting | August 6 | Acknowledged requests with delivery dates; no engineering rate substitutes for these |
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

- **Start-of-day D3 reconciliation at `93eb745`, 2026-07-28.** Backend `gofmt` clean, `go build ./...`,
  `go vet ./...`, `go vet -tags=integration ./...`, and `go test ./...` all pass with
  `GOCACHE=/tmp/gradex-go-cache`. Frontend `typecheck`, `lint`, and 21 of 21 `node:test` cases pass.
  `scripts/docs-guard.sh` ok across 94 Markdown files; `scripts/expose-guard.sh` ok with 13 approved
  `Expose` call sites, 1 password-plaintext boundary, and 2 reviewed plaintext reads. Integration
  suites and the production build were **not** rerun: no application code has changed since hosted CI
  run 30299076346 passed all five jobs on `edd6508`, and the only commit since is documentation
  (`93eb745`). Branch synchronized with upstream; the only untracked file is the user-owned
  `.caveman.json`.
- **Start-of-day Day 11 reconciliation at `881639d`.** Backend `gofmt` clean, `go build ./...`,
  `go vet ./...`, `go vet -tags=integration ./...`, and `go test ./...` all pass with
  `GOCACHE=/tmp/gradex-go-cache`. Frontend `typecheck`, `lint`, and 21 of 21 `node:test` cases pass,
  and the production build passes **with `.next` removed first** — twelve routes, all static except the
  OpenGraph image. `scripts/docs-guard.sh` passes across 129 Markdown files and
  `scripts/expose-guard.sh` passes with 12 approved `Expose` call sites, 1 password-plaintext boundary,
  and 2 reviewed plaintext reads. Integration suites were not rerun: no application code changed since
  the S1B3 evidence below.
- **`CARRYOVER-DOCS-GUARD-UNTRACKED` misreported at start of day, as predicted.** The first guard run
  returned `ok (128 Markdown files checked)` while never opening the newly written August 2 record or
  the reconciliation document, because both were untracked. Staging them produced 129 files and a real
  check. This is the second observed instance of the defect and it is now scheduled as S1C Must 1.
- **Hosted CI [run 30290157849](https://github.com/Owlah2025/gradex/actions/runs/30290157849) passed
  all five jobs — Backend, Migrations, Admission Integration, Frontend, and Guards — on exact head
  `3c16122`, the S1C closeout head.** The complete local suite was re-run by the reviewer on the same
  tree rather than accepted from the builder's report: backend `gofmt`, build, `vet` on both tag sets,
  `go test -count=1 -race ./...`, and the full integration suite under race against real PostgreSQL at
  schema 8, Redis, and MinIO; frontend `typecheck`, `lint`, 21 of 21 `node:test` cases, and a clean
  production build with `.next` removed first; both guards.
- Hosted CI [run 30265328569](https://github.com/Owlah2025/gradex/actions/runs/30265328569) passed
  all five jobs — Backend, Frontend, Migrations, Admission Integration, and Guards — on exact
  reviewed S1B3 head `9d3db91`.
- Every S1B3 checkpoint carried its own CI evidence as it landed: `a75662a`, `a79fe0b`, `0be4878`,
  and `e0e4ea0` green; `d79cbf9` **red** on the Frontend build and corrected by `c39a650`.
- Run `30264250133` on `c39a650` failed Migrations at "Initialize containers", a runner step that
  precedes any repository code. The preceding run passed Migrations and `c39a650` changed only
  frontend pages and one document, so it was treated as infrastructure flake; run `30265328569`
  confirmed that with no migration change in between.
- The complete S1B3 local suite is green with PostgreSQL at schema 7, Redis, and MinIO: backend
  formatting, build, vet on default and `integration` tags, `go test -race ./...`, and the full
  integration suite under race. Frontend typecheck, lint, 21 `node:test` logic cases, and a
  production build run with `.next` removed first.
- Hosted CI [run 30251188682](https://github.com/Owlah2025/gradex/actions/runs/30251188682) passed
  all five jobs — Backend, Frontend, Migrations, Admission Integration, and Guards — on exact
  reviewed S1B2 head `7d8710e`.
- Hosted CI [run 30250723457](https://github.com/Owlah2025/gradex/actions/runs/30250723457) **failed**
  on `e21d0e4`: Backend, Frontend, Admission Integration, and Guards passed, and Migrations failed at
  "Verify schema version and expected objects" because the job asserted schema 5 after migration
  `0006` raised the schema to 6. Recorded rather than discarded — it is the evidence that CI enforces
  the migration contract, and that the local suite does not.
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

**S1C is CLOSED. S1 is complete.** Exact range `c65cd53..edd6508` passed independent read-only review
by `agy` on `gemini-3.1-pro-high`: **0 critical, 0 high, 0 medium, 0 low, verdict `APPROVE`**, with
`touched files: 0` and a clean disposable worktree. Hosted CI
[run 30299076346](https://github.com/Owlah2025/gradex/actions/runs/30299076346) passed all five jobs
on that same exact head.

It took six dispatches:

| # | Range | Result |
|---|---|---|
| 1–2 | `c65cd53..506e0b4` | `UNAVAILABLE` — account-wide quota, recorded as such and never as a pass |
| 3 | `c65cd53..506e0b4` | **`REJECT`** — 0/2/0/1 |
| 4 | `c65cd53..41f50aa` | **`REJECT`** — 0/1/1/0 |
| 5 | `c65cd53..5f1188a` | `APPROVE` — 0/0/0/0 |
| 6 | `c65cd53..edd6508` | **`APPROVE` — 0/0/0/0**, the closing verdict |

Round 3 found that every mounted staff route diverged from the frozen
[S1C spec §7](../../specs/002-auth-rbac/s1c/spec.md) on path, method, or both, that suspension was
gated on `ADMIN_OPERATIONS` where §7 requires `SECURITY_OPERATIONS`, and that both suspension routes
bypassed strict binding with their declared body limits unreferenced. Round 4 then caught a
regression the fix itself introduced — a required backend field still labelled "(optional)" in both
dictionaries. Every finding was reproduced against the code before being accepted.

**Claude reviewed the same range at Tier 3 and found none of the round-3 findings.** That is the
strongest evidence this project has produced for its own never-self-approve rule, and it is recorded
plainly: the range was twenty minutes from closing on a developer risk acceptance instead.

Round 5 raised, then round 6 dispositioned, a medium about S2 specification files appearing in the
range. **Disposition: not a defect.** The S2 planning commits sit between the review points on a
linear history, so no range excludes them; planning commits no behaviour. Recorded by name rather
than allowed to vanish between rounds.

Earlier: **S1C was REJECTED at exact range `c65cd53..4cf3e6e`, reviewed by Claude at Tier 3 under
[D-040](../DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews):
3 critical, 3 high, 4 medium, 2 low, verdict `REJECT WITH FINDINGS`.**

The three critical findings all meant the same thing: the slice shipped no working behaviour.
`WithStaffFoundation` was never wired into `cmd/api`, no production implementation of `staffService`
existed anywhere (the only one was a test fake), `sessionFromContext` read a gin context key nothing
ever set — so an active Admin received `401` on every staff mutation, proven by direct probe — and
the mutations were mounted behind the legacy video-slice authenticator with no CSRF. The high
findings were a hand-maintained authorization matrix that could not detect drift, four hardcoded
recent-auth windows beside a typed configuration value built for exactly that purpose, and a missing
outbox intent whose absence was contradicted by the expose-guard comment justifying its own
allowlist entry.

Remediation round one closed all nine. Verified by Claude rather than accepted from the report: both
matrix drift cases were reproduced independently, and every gate was re-run. Round one then
introduced **three fail-open constructions of its own** — conditional CSRF, a silent recent-auth
default, and an optional outbox intent — so the range was rejected a second time at
0 critical, 1 high, 3 medium.

Round two closed the conditional CSRF and the silent default before hitting the quota limit. The
last two findings — the optional invitation outbox intent and the hardcoded `"en"` locale — were
closed at `506e0b4` **by Claude, on the developer's explicit instruction**, because Antigravity's
quota did not return inside the working session. Both new assertions were mutation-checked.

**That creates a review boundary the slice must not be closed across.** `506e0b4` is
Claude-authored implementation inside a range Claude reviews, so **S1C cannot close on Claude's
review alone.** It needs an independent pass over `c65cd53..506e0b4` once Antigravity's quota
returns, or a recorded developer risk acceptance naming the exposure. Never-self-approve is not
waived by the developer having asked for the code — the same rule that made Antigravity's
self-review inadmissible applies here symmetrically.

The pattern across both rounds is one class — **a control that silently degrades instead of
refusing** — and it is worth naming because five separate instances appeared in a slice whose whole
subject is deny-by-default enforcement.

Earlier:

S1B3 passed independent read-only review at exact range `3b2f7a8..9d3db91`, reviewed by `agy` on
`gemini-3.1-pro-high` through `scripts/agy-review.sh` under
[D-036](../DECISIONS.md#d-036--claude-builds-s1b3-and-agy-reviews): **0 critical, 0 high, 0 medium,
0 low**, verdict **APPROVE**. All nine dimensions were reported verified, the run reported
`touched files: 0`, and the disposable detached worktree was confirmed unmodified on exit.

One frozen range sufficed, unlike S1B2's two, because every checkpoint was pushed and CI-verified as
it landed rather than at the end. Claude authored all eight commits and reviewed none.

Three judgement calls were raised for the reviewer before dispatch — `CHANGE_REQUIRED` remaining
recovery-eligible, role being absent from recovery eligibility, and the `recovery.go` exposure-guard
allowlist entry. The report returned `OPEN QUESTIONS: none` and addressed none of them by name. A
clean verdict is not the same as independent examination of those three, so they are carried to S1C
as open judgement rather than settled precedent.

Earlier:

S1B2 passed independent read-only review under
[D-035](../DECISIONS.md#d-035--claude-builds-s1b2-and-agy-reviews) across two consecutive frozen
ranges, both reviewed by `agy` on `gemini-3.1-pro-high` through `scripts/agy-review.sh`:

| Range | Contents | Counts (C/H/M/L) | Verdict |
|---|---|---:|---|
| `ad1b8f6..e21d0e4` | S1B2 implementation | 0/0/0/0 | `APPROVE` |
| `e21d0e4..7d8710e` | CI schema-assertion correction | 0/0/0/0 | `APPROVE` |

Both runs reported `touched files: 0`, both disposable detached worktrees were confirmed unmodified
on exit, and the `agy` settings file was restored. No run was `TAINTED`, `UNAVAILABLE`, or
`INCONCLUSIVE`. Claude authored part of both ranges and reviewed neither, so the slice did not close
on its builder's own assessment.

The correction was split into its own reviewed range deliberately. Folding a Claude-authored fix into
an already-approved range would have shipped an unreviewed change under an earlier verdict.

Earlier:

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
- The public target is readiness-gated and, under D-039, is **September with no exact date**. August 15
  is retired. A forecast range is not a target and is not to be published as one.
- D-033: Codex is the standing builder and planner; Claude is the standing independent read-only
  reviewer. Review uses a disposable detached worktree and frozen exact commit range. A `TAINTED` or
  `UNAVAILABLE` run is never recorded as approval.
- D-037 (active, scoped to S1C): Claude holds the builder/planner seat and `agy` on
  `gemini-3.1-pro-high` holds the independent read-only reviewer seat, dispatched through
  `scripts/agy-review.sh`. It **expires at S1C's frozen reviewed head**; S2 needs its own dated
  assignment. The S1 integration review also goes to `agy`, because its scope contains Claude-authored
  commits. D-033's containment and never-self-approve rules are unchanged, and D-033 stays paused until
  Codex availability is **explicitly reverified** — silence is not a return of quota. D-035 (S1B2) and
  D-036 (S1B3) are expired and historical.
- D-038 (active): August 8 is recorded as no longer a credible runway start and a full-PRD August 15
  launch as not forecastable. S3–S8 stay `TBD`.
- D-039 (active): **Remedy A adopted.** August 8 and the August 15 full-PRD target are retired as
  non-credible, full PRD scope is preserved, and the public target moves into September with **no exact
  date set** until the August 6 outreach results and an S2–S16 rebaseline exist. Remedy B stays
  available afterwards as an optimization; Remedy C — compressing the envelope or spending August 7 —
  is **rejected**. A September range must not be committed to publicly as a target.
- **A frontend production build is not local build evidence unless `.next` was removed first.** In
  force from August 2 regardless of whether `CARRYOVER-LOCAL-BUILD-CACHE` is fixed. A build claim that
  does not say "clean" is to be read as not having been made.
- D-034: browser authentication uses one opaque server-managed credential in a `Secure`,
  `HttpOnly`, host-only, `SameSite=Strict` cookie. Controlled renewal rotates the credential and
  CSRF token; confirmed reuse revokes the family. Older dual-token wording is superseded.
- Missed work becomes visible carryover and cannot be marked complete without evidence.
- Developer-approved Day 8 replan: S1B1 July 30, S1B2 July 31, S1B3 August 1, S1C August 2, and S2
  August 3. S3–S8 remain `TBD`; no protected day or later runway date is silently reassigned.
- The approved documentation/specification baseline ends at commit `1f63a59`.
- Claude is the default SpecKit integration; the Codex integration remains installed but unused.
- Local `gradex-spec-review.zip` bundles are generated review artifacts and are ignored.
- **Three** consolidated external messages are prepared as drafts in the
  [outreach pack](outreach/2026-08-06-launch-gate-outreach.md); `DRAFT` never counts as sent
  evidence. It held **four** until `caf301b`, when the Operations message was reclassified into
  founder task work because its recipient was the founder — nothing sent, merged, or dropped, and its
  six gates (`LG-013`–`LG-015`, `LG-018`, `LG-019`, `LG-021`) moved with it. Every reply date was
  rebased for a July 28 send; the file name still carries the retired August 6 date because two
  **closed** day records link to it.
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

**Freeze Codex's completed S2 D5 T032–T038 implementation, then have Claude independently review
that one exact range under D-043; hosted CI must pass on the reviewed head before closure.**

Codex's specification and implementation seats are complete for this boundary. The existing
[S2 tasks](../../specs/003-course-authoring/tasks.md) reconcile T001–T031 with exact evidence and
record T032–T038 complete for D5. The bounded
[D5 handoff](../../specs/003-course-authoring/handoff.md) prohibits T039+, separate feature
creation, S2 regeneration, pricing, lifecycle/emergency controls, taxonomy administration, search,
frontend work, and unrelated refactoring.

The non-engineering item below is **deferred under D-041**, not forgotten:

**Source Kuwaiti counsel.** Not send — source. Two of the three messages have **no recipient**, which
is a `BLOCKED` state distinct from `DRAFT`, and it is the highest-priority item on the project ahead
of all engineering work. Ask the Digital Commerce Law registration question first and to several
candidates at once: a blocking registration discovered on August 10 does not move August 15, it moves
the launch.

**Then send the Tap message**, which needs no named contact and carries three of the seven
cutover-blockers.

The implementation stop condition is a buildable, uncommitted T032–T038 result with all named real
PostgreSQL races, rollback/dependency/clone/identity proofs, six restored mutations, production
wiring/security sweep, and complete local gates green. Claude reviews the Codex-authored range from
a disposable detached worktree and does not implement.

**S1B and S1C are not reopened** unless a concrete defect surfaces in them. A suspicion is not a
defect, and reopening a reviewed slice on suspicion discards the frozen-range evidence that closed
it.

The August 6 outreach is now the largest launch risk and is due **July 28** under
[D-040](../DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews).
It costs no engineering time and it gates seven launch-blocking items that no amount of code can
close.
