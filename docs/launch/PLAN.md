# Gradex MVP Launch Workflow

> Status: Active
> Schedule: 2026-07-23 through 2026-08-15
> Public go-live target: 2026-08-15, readiness-gated
> Delivery team: Solo developer, Codex builder, Claude reviewer

This is the schedule of record for delivering the MVP in [PRD.md](../PRD.md). It supersedes any
standalone launch draft when its scope or dates disagree with the current canonical product
documents.

The full current PRD remains the target. A missed task becomes visible carryover; it does not
silently disappear from scope. Security, legal, payment, privacy, backup, and data-integrity gates
are never waived to preserve the target date.

## 1. Sources of Truth

Read these sources in this order when starting or closing a day:

1. [STATUS.md](STATUS.md) — current delivery state and tomorrow's first task.
2. The latest record under [`daily/`](daily/) — planned and actual daily work.
3. This plan — dates, milestones, completion rules, and launch policy.
4. [LAUNCH_GATES.md](../LAUNCH_GATES.md) — external and production-readiness evidence.
5. [PRD.md](../PRD.md), [DECISIONS.md](../DECISIONS.md), and
   [BUSINESS_RULES.md](../BUSINESS_RULES.md) — authoritative scope and behavior.
6. Approved platform system design and the relevant feature specification.
7. Git status, recent commits, and test output — implementation evidence.

When these sources disagree, stop the affected work, record the conflict, and reconcile it using
the source-of-truth order in [the documentation index](../README.md#source-of-truth-order).

## 2. Team Operating Model

### Responsibilities

- **Developer/product owner:** approves product decisions, external commitments, accepted risks,
  scope changes, and the public go/no-go decision.
- **Codex/builder:** plans and implements one bounded slice, runs its checks, documents evidence,
  and corrects review findings.
- **Claude/reviewer:** reviews the exact diff or commit without editing it, checking requirements,
  security, privacy, authorization, idempotency, concurrency, tests, observability, and scope.

The reviewer must not review a moving target. Record the reviewed commit or diff description in the
daily record. Critical and high findings return to the builder and must be rechecked before the
slice closes.

### Work-in-progress rule

Only one launch slice may be active. Finish or explicitly block the day's `Must` work before
starting `Should` work. `Could` work never displaces carryover, a gate action, testing, or review.

### Daily capacity

Use the following 8–10 hour envelope:

| Activity | Timebox |
|---|---:|
| Reconcile status and prepare the brief | 30 minutes |
| Clarify/specify today's bounded slice | 60–90 minutes |
| Implement the slice | 5–6 hours |
| Automated and manual verification | 60–90 minutes |
| Independent review and corrections | 45–60 minutes |
| Closeout and next-day handoff | 30 minutes |

If the slice cannot fit this envelope, split it before implementation. Do not compensate by
removing its failure paths or quality evidence.

## 3. Command Protocol

The quoted phrases below are the operational interface. They may be given to Codex in any
conversation that has access to this repository. Root-level `AGENTS.md` and `CLAUDE.md` route fresh
agent sessions to this canonical protocol; they do not duplicate the schedule.

### `Start the day`

Before presenting work:

1. Read the sources in §1.
2. Inspect the current branch, Git status, recent commits, and relevant test state.
3. Compare claimed completion with repository evidence.
4. Move incomplete `Must` work to the top of today's brief.
5. Recalculate launch confidence and days remaining.
6. Create or refresh `daily/YYYY-MM-DD.md` without overwriting an already closed record.

Return a brief containing:

- today's single outcome;
- `Must`, `Should`, and `Could` tasks in execution order;
- timebox and stop condition for each `Must`;
- Codex build assignment and Claude review assignment;
- exact completion evidence and checks;
- decisions or external blockers requiring the developer;
- launch confidence and the reason for it.

Starting on a protected recovery day shows carryover and urgent gate actions only. It does not pull
new feature work forward.

### `Close the day`

1. Run the checks required by today's acceptance evidence.
2. Give Claude the exact stable diff or commit for read-only review.
3. Correct and retest all critical/high findings.
4. Record completed, incomplete, blocked, and deliberately deferred work.
5. Update [STATUS.md](STATUS.md), gate evidence, confidence, and tomorrow's first task.
6. Close the daily record only after evidence is linked or summarized.

### `Launch status`

Report:

- completed and active milestones;
- carryover and critical-path variance;
- required gates by status and approaching deadline;
- critical defects and accepted risks;
- the next three dated outcomes;
- Green/Yellow/Red confidence with evidence.

### `Replan`

Reallocate unfinished work from the current date forward. Preserve every current PRD capability
unless the developer explicitly approves a canonical scope change. Record the reason, affected
dates, and launch forecast in the status file and the current daily log.

### `Go/no-go check`

Evaluate §8 in full. A target date, elapsed schedule, or mostly passing build is not sufficient
evidence for public launch.

## 4. Status and Daily-Record Contract

[STATUS.md](STATUS.md) must always contain:

- current date, phase, scheduled day, and days remaining;
- current branch and last verified baseline commit;
- completed milestones and active slice;
- carryover and tomorrow's first task;
- blockers with owner, next action, deadline, and required evidence;
- required launch-gate counts;
- latest verified checks and reviewer verdict;
- launch confidence with a concrete reason.

Each `daily/YYYY-MM-DD.md` record contains:

- state: `PLANNED`, `IN_PROGRESS`, or `CLOSED`;
- intended outcome and starting evidence;
- ordered `Must`, `Should`, and `Could` tasks;
- builder/reviewer assignments;
- acceptance evidence;
- closeout: completed, tests, review, blockers, decisions, carryover, and next task.

A closed record is historical evidence. Correct a factual error explicitly; do not rewrite its
planned or actual result to make the schedule look successful.

## 5. Launch Confidence

- **Green:** the critical path is current, required checks pass, and no required gate is forecast
  late.
- **Yellow:** there is at most one workday of critical carryover, or a gate is at risk but has an
  owner, dated action, and credible resolution path.
- **Red:** there are more than two workdays of critical carryover, a required gate lacks a credible
  resolution path, or any critical security, authorization, payment, privacy, data-loss, restore,
  or rollback defect exists.

Confidence describes the public launch forecast, not effort or morale.

## 6. Three-Week Delivery Calendar

### Week 1 — Baseline, System Design, and Foundation

#### Thursday, July 23 — Establish launch control

**Outcome:** Create a trustworthy baseline from which system design can start.

- Review the existing dirty documentation changes without overwriting user work.
- Reconcile previous 23-day launch assumptions with the current PRD and decisions.
- Map every MVP capability and launch gate to a scheduled outcome and owner.
- Initiate legal, accounting, Tap, email, hosting, malware-scanning, policy, content, and support
  actions.

**Exit evidence:** approved reconciliation baseline; every MVP capability mapped; every required
gate has an owner, next action, evidence requirement, and deadline.

#### Friday, July 24 — Protected recovery/spillover

No new feature scope. Resolve July 23 blockers or urgent provider outreach only. Preserve the day
for recovery if the baseline is complete.

#### Saturday, July 25 — Platform architecture

**Outcome:** Approve the modular-monolith and production topology.

- Define architectural drivers, launch load, availability target, recovery targets, and budget
  envelope.
- Define module boundaries, system context, containers, runtime topology, and deployment model.
- Confirm PostgreSQL authority and the Redis/worker, object-storage/CDN, email, and Tap boundaries.

**Exit evidence:** every component and module has one primary responsibility and an explicit
must-not-own boundary; dependencies are explicit; open policy/provider choices remain configurable
rather than hard-coded.

#### Sunday, July 26 — Domain, data, and state design

**Outcome:** Make the complete MVP domain implementable without inventing behavior in code.

- Finalize entities, ownership, invariants, transactions, database constraints, and indexes.
- Cover identity, Course content, media, commerce, coupons, payments, refunds, entitlements,
  learning, reports, office hours, notifications, payouts, and audit.
- Define lifecycle and failure states for transactional and asynchronous work.

**Exit evidence:** Course/Section entitlement and 150-day expiry are unambiguous; all critical state
transitions have actors and preconditions; idempotency constraints are defined.

#### Monday, July 27 — API, security, and integration design

**Outcome:** Freeze externally visible contracts and trust boundaries.

- Define API conventions, errors, pagination, idempotency, and concurrency behavior.
- Define session rotation, immediate suspension, authorization, signed media/download delivery,
  rate limits, webhook verification, reconciliation, audit, and secret handling.
- Define testable adapters for Tap, email, storage/CDN, malware scanning, and monitoring.

**Exit evidence:** every protected action has backend authorization; redirects cannot grant access;
duplicate, replayed, delayed, and reordered callbacks have defined behavior.

#### Tuesday, July 28 — Architecture review and delivery foundation

**Outcome:** Approve the architecture baseline and executable delivery path.

- Obtain independent Codex and Claude reviews and resolve critical findings.
- Convert the architecture into dependency-ordered feature slices.
- Complete configuration validation, migrations, structured logging, request IDs,
  health/readiness endpoints, and CI.

**Exit evidence:** architecture baseline approved; API, worker, frontend, PostgreSQL, and Redis run
in the supported environment; CI exercises backend tests, frontend lint/typecheck, and builds.

#### Wednesday, July 29 — Authentication and RBAC vertical slice

**Outcome:** Deliver production-shaped identity and backend access control.

- Implement Student registration/verification, login, refresh rotation, logout, recovery, and safe
  non-enumerating responses.
- Implement staff invitations and secure bootstrap Admin operation.
- Enforce role, ownership, account status, and immediate suspension.
- Complete responsive Arabic/English authentication screens.

**Exit evidence:** authentication acceptance tests pass end-to-end; direct API calls cannot bypass
role/ownership; suspended accounts immediately lose protected access; critical/high review findings
are resolved.

### Week 2 — Product and Revenue Journey

#### Thursday, July 30 — Course authoring and review

**Outcome:** Let an Instructor create a Course and an Admin safely publish it.

- Implement Course/Section/Lesson management, ownership, resources/labs metadata, preview metadata,
  taxonomy selection, and price visibility.
- Implement submission, changes requested, approval, publishing, unpublishing, archiving, and
  revision handling.
- Implement Admin taxonomy and audited Course/Section pricing.

**Exit evidence:** Instructors cannot publish or change prices; draft content does not leak; the
live revision remains stable until replacement approval.

#### Friday, July 31 — Protected recovery/spillover

No new feature scope. Finish incomplete Week 2 `Must` work only; otherwise preserve recovery.

#### Saturday, August 1 — Catalog, search, and public experience

**Outcome:** Let Students find and understand purchasable Courses and Sections.

- Implement catalog, filters, Arabic-normalized bilingual search, Course detail, curriculum,
  preview, and purchase choices.
- Complete the Arabic-default responsive shell, RTL/LTR behavior, metadata, and state handling.

**Exit evidence:** search and filters compose; only Published Courses appear; Course/Section prices
and purchase targets remain consistent in both languages.

#### Sunday, August 2 — Media, Resources, and Labs

**Outcome:** Produce and deliver safe protected learning assets.

- Complete direct upload, validation, quarantine/scanning, queues, HLS processing, retry, cleanup,
  and status UI.
- Implement entitlement-protected Resource and Lab Material downloads.
- Apply the opaque buyer tag to Lab Materials only.

**Exit evidence:** real media completes upload-to-HLS; failures are observable/retryable; unsafe or
unscanned downloads fail closed; duplicate completion calls are harmless.

#### Monday, August 3 — Protected learning

**Outcome:** Let entitled Students learn and resume safely.

- Implement Student dashboard, Course home, player, progress/resume, completion, and navigation.
- Implement Course/Section access boundaries, community-link authorization, public preview
  isolation, and content reporting.

**Exit evidence:** Course entitlements cover all Sections and Section entitlements only their
Section; revoked/expired/suspended/non-entitled access is denied; progress failure does not stop
playback.

#### Tuesday, August 4 — Orders, checkout, and coupons

**Outcome:** Create a server-priced, idempotent order for one Course or Section.

- Implement price snapshots, integer minor-unit money, order idempotency, and purchase eligibility.
- Implement fixed/percentage coupons, scopes, caps, one consuming redemption per Student, and free
  grants.
- Implement Tap-hosted checkout initiation plus failure/cancellation UI.

**Exit evidence:** the client cannot change price/discount; duplicate requests do not duplicate
orders; free orders do not call Tap; coupon consumption occurs only on payment success/free grant.

#### Wednesday, August 5 — Payments, entitlements, and refunds

**Outcome:** Turn verified payment state into recoverable access.

- Implement webhook authenticity, replay protection, deduplication, state transitions, and
  transactional entitlement creation.
- Handle delayed, duplicate, unknown, and out-of-order events; add reconciliation and manual repair.
- Implement full/partial refund records, provider processing, entitlement outcome, audit, and
  coupon-release behavior.

**Exit evidence:** redirects never grant access; paid orders are transactionally recoverable;
duplicate webhook/refund calls are harmless; LG-007 through LG-010 are resolved or confidence is
Red.

### Week 3 — Operations, Integration, and Production Readiness

#### Thursday, August 6 — Instructor and Admin operations

**Outcome:** Operate and support the launch without direct database editing.

- Complete Instructor analytics and the privacy-limited Course roster.
- Complete Admin identity, content, taxonomy, price, coupon, order, payment, refund, entitlement,
  report, and media-recovery operations.
- Audit every privileged operation.

**Exit evidence:** common support incidents have authorized recovery actions; Instructor views
contain no prohibited Student PII; privileged actions are traceable.

#### Friday, August 7 — Protected recovery/spillover

No new feature scope. Finish incomplete critical-path work only; otherwise preserve recovery.

#### Saturday, August 8 — Office hours and notifications

**Outcome:** Deliver the MVP follow-up loop without exposing private meeting links.

- Implement one-off Course office hours with Course-state, ownership, entitlement, reschedule, and
  cancellation rules.
- Implement the in-app notification center and required transactional email events.
- Isolate notification delivery from business transactions and add retry.

**Exit evidence:** unauthorized users never receive meeting links; required changes produce
notifications; delivery failure cannot roll back business/security actions.

#### Sunday, August 9 — Revenue, payouts, compliance, and recovery

**Outcome:** Make commercial and support operations auditable.

- Implement revenue reports, monthly payout calculations, adjustments, statement records, and
  manual transfer status.
- Complete legal-page delivery, accepted-version records, privacy-request operations, support
  routes, incident ownership, and recovery runbooks.

**Exit evidence:** revenue reconciles to payments, refunds, coupons, and fees; missing revenue-share
configuration fails closed; LG-001–LG-006, LG-011–LG-013, LG-016–LG-017, and LG-020 are resolved or
confidence is Red.

#### Monday, August 10 — End-to-end integration

**Outcome:** Make all primary Student, Instructor, and Admin journeys pass together.

- Exercise identity, authoring/review, Course/Section purchase, coupons/free grants, payment
  failures/retries, entitlements, learning, refunds, reporting, office hours, notifications, and
  Admin recovery.

**Exit evidence:** launch-critical journeys pass automatically where practical; every known failure
has an owner and deadline; no critical journey is verified only by a happy-path manual check.

#### Tuesday, August 11 — Production infrastructure and observability

**Outcome:** Run the release candidate in a production-like environment.

- Provision/configure the database, Redis, storage/CDN, API, worker, frontend, email, DNS/TLS,
  secrets, and Tap environment.
- Configure health, restarts, backups, restore, storage lifecycle, CORS, cookies, and caching.
- Add logs, metrics, error tracking, alerts, queue/payment/media visibility, and runbooks.

**Exit evidence:** staging mirrors intended production; restore is demonstrated; alerts reach the
owner; rollback is executable from its runbook.

#### Wednesday, August 12 — Security and quality gate

**Outcome:** Demonstrate that the release candidate is safe and usable at launch load.

- Test authorization, escalation, CSRF/XSS, webhook forgery/replay, unsafe uploads, secrets, rate
  limits, and error disclosure.
- Load-test catalog, login, signed playback, progress, webhook bursts, and media workers.
- Audit accessibility, responsive behavior, Arabic/English, RTL/LTR, and mixed-language content.

**Exit evidence:** no unresolved critical security or data-loss defect; every high-severity defect
has documented risk acceptance, mitigation, and owner approval; launch-load targets pass; LG-014,
LG-015, LG-018, and LG-019 are resolved; otherwise confidence is Red.

### Final Launch Runway

#### Thursday, August 13 — Staging acceptance and gate audit

**Outcome:** Produce the final release-candidate and go/no-go evidence.

- Load all 8–12 approved launch Courses and test representative mobile, tablet, and desktop
  journeys.
- Exercise successful/failed payments, slow networks, interrupted uploads, expired sessions,
  failed media, missing objects, and recovery.
- Audit all required launch gates.

**Exit evidence:** every launch-critical scenario passes; launch content is verified; every
required gate is `RESOLVED`; otherwise record a formal no-go forecast.

#### Friday, August 14 — Blocker-only soft launch

**Outcome:** Verify the production deployment without adding scope.

- Fix only blockers, security problems, payment/access failures, data-loss risks, and severe UX
  failures.
- Deploy, smoke test, rehearse rollback, and run a controlled soft launch.

**Exit evidence:** production health, monitoring, backups, commerce, access, learning, and support
are verified; release and rollback versions are recorded.

#### Saturday, August 15 — Public go/no-go

Launch only when every criterion in §8 passes. If any fails, keep public commerce disabled,
preserve the tested environment, record the blocker/owner/evidence required, and schedule the next
go/no-go within 48 hours.

## 7. Gate Deadlines

| Deadline | Required result |
|---|---|
| July 23 | All LG-001–LG-020 entries have a real owner, next action, evidence target, and deadline |
| July 29 | Every architecture-affecting open gate has a documented configurable/provider boundary |
| August 5 | LG-007–LG-010 payment/commercial evidence resolved |
| August 9 | LG-001–LG-006, LG-011–LG-013, LG-016–LG-017, and LG-020 resolved |
| August 12 | LG-014, LG-015, LG-018, and LG-019 resolved |
| August 13 | Every required gate is `RESOLVED` with linked or summarized evidence |

An unmet deadline makes launch confidence Red unless the gate is already resolved by equivalent
evidence recorded in [LAUNCH_GATES.md](../LAUNCH_GATES.md).

## 8. Public Launch Criteria

The developer may approve public go-live only when:

1. All required launch gates are `RESOLVED` with evidence.
2. The production critical journey passes:
   registration → discovery → Course/Section purchase → verified payment/free grant → entitlement
   → protected learning → progress.
3. Instructor authoring/Admin review, refunds, support repair, office hours, notifications,
   reporting, and payout records meet their MVP acceptance criteria.
4. No unresolved critical defect remains. Every high-severity security, authorization, privacy,
   payment, data-loss, or public/private media defect has documented risk acceptance, mitigation,
   and owner approval.
5. Production backup, restore, monitoring, alerting, support, incident response, and rollback have
   been demonstrated.
6. Prices, content, policies, consent versions, payment credentials, sender identity, domain, and
   TLS are production-approved.

Failure of any criterion is a no-go. It does not authorize a reduced public launch unless the
canonical MVP and gate register are explicitly revised and reapproved.

## 9. Workflow Validation

The launch-control workflow is working when:

- the first `Start the day` builds its brief from this plan and current repository evidence;
- unfinished `Must` work becomes next-day carryover;
- user-owned or uncommitted work is reported and never overwritten;
- a blocked gate changes confidence and priority;
- a critical/high review finding prevents closure;
- a recovery day introduces no new feature scope;
- a missed calendar day is reconciled from Git evidence before replanning;
- a no-go preserves a safe deployment and creates a dated next review.

The release acceptance suite must cover identity, staff invitation, Course review/publishing,
catalog/search, protected media/Resources/Labs, Course/Section orders, coupons/free grants, Tap
callbacks, refunds, entitlement expiry, playback/progress, reports, office hours, notifications,
revenue/payouts, audit, localization, accessibility, backup/restore, monitoring, and rollback.
