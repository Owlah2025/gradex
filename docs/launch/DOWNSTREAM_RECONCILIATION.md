# Downstream Calendar Reconciliation — S3 through S8

> **HISTORICAL — superseded twice.** The calendar analysis here was corrected by
> [D-040](../DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews)
> (it counted schedule days as real days), and its slice inventory was superseded by
> [D-045](../DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)
> on 2026-07-28, which removed S7 and rescoped S6 to the Course Access Invitation and Entitlement
> grant. The S6/S7/S10 rows below describe payment work that is no longer in the MVP. Retained
> unedited as the evidence behind D-038 and D-039.


> Produced: 2026-08-02, before S1C planning, at developer instruction
> Bounded scope: the calendar and dependency forecast for S3–S8, with the S2, S9, and S10 dates they
> collide with
> Authority: forecast only. This document records a verdict and offers remedies; it does not change
> PRD scope, does not move the public target, and does not spend a protected day
> Recorded as: [D-038](../DECISIONS.md#d-038--august-8-is-no-longer-a-credible-runway-start-s3s8-remain-undated-pending-a-developer-remedy)

This reconciliation was **not** taken out of S1C engineering time. It is start-of-day planning work,
performed before the S1C implementation plan was written, and it starts no S2 work — no schema, no
route, no screen, and no specification for any downstream slice was authored here.

The conflict being reconciled has carried the label "unresolved" in
[SLICES.md §2](SLICES.md#2-slice-order) since 2026-07-30 under a July 31 deadline that has now passed.
This is its third appearance. It is resolved here into a recorded verdict rather than carried a fourth
time.

## 1. Method and its limits

Three things are evidence, and everything else in this document is derived from them:

1. **Available dates.** Counted from [PLAN.md §6](PLAN.md#6-three-week-delivery-calendar), with
   August 7 protected and August 2 already committed to S1C.
2. **Dependency order.** Taken unchanged from [SLICES.md §2](SLICES.md#2-slice-order). No dependency
   is re-derived here; the order is already proven acyclic and is not the problem.
3. **Observed velocity.** One product slice has been delivered end to end. S1 was scoped as a single
   day on July 29 and took five — S1A, S1B1, S1B2, S1B3, and S1C. S0, the delivery foundation, was
   scoped as one day and took one.

The per-slice sizes in §3 are **derived estimates, not validated measurements.** They are produced by
counting the independent boundaries inside each slice's PLAN.md §6 bullets and assigning one envelope
day per boundary — the same decomposition that, applied to S1 in hindsight, yields its five actual
days rather than its planned one. That method has exactly one calibration point, so treat the totals
as a magnitude, not a schedule. The verdict in §4 does not depend on their precision, and §4.1 shows
why.

## 2. The arithmetic

Workdays remaining after S1C closes, from [PLAN.md §6](PLAN.md#6-three-week-delivery-calendar):

| Date | Day | Current assignment | Can absorb feature work? |
|---|---|---|---|
| Aug 3 | Mon | S2 — Course authoring and review | Committed |
| Aug 4 | Tue | **unassigned** | Yes |
| Aug 5 | Wed | **unassigned** | Yes — but LG-007–LG-010 fall due |
| Aug 6 | Thu | **unassigned** | Partly — the deferred external outreach pack is due here |
| Aug 7 | Fri | Protected recovery | **No** — protected, and not to be spent silently |
| Aug 8 | Sat | S9 — Office hours and notifications | Committed |
| Aug 9 | Sun | S10 — Revenue, payouts, compliance, recovery | Committed |
| Aug 10 | Mon | S11 — End-to-end integration | No — depends on S1A–S10 |
| Aug 11 | Tue | S12 — Production infrastructure | No |
| Aug 12 | Wed | S13 — Security and quality gate | No |
| Aug 13 | Thu | S14 — Staging acceptance and gate audit | No |
| Aug 14 | Fri | S15 — Blocker-only soft launch | No |
| Aug 15 | Sat | S16 — Public go/no-go | No |

**Six slices (S3–S8) have three available dates (August 4, 5, 6).** That is the whole conflict stated
without any velocity assumption at all: even if every downstream slice took exactly one envelope day —
a rate no product slice has ever achieved here — the calendar is short by three days before August 8.

Two further facts make the table worse than it reads:

- **August 6 is not a clear feature day.** The four consolidated external-outreach messages in the
  [August 6 outreach pack](outreach/2026-08-06-launch-gate-outreach.md) are due that day, and all 21
  required entries in [LAUNCH_GATES.md](../LAUNCH_GATES.md) are still `OPEN`. Outreach is not
  optional work that yields to feature pressure; several gates have August 9 and August 12 deadlines
  that depend on a reply arriving.
- **August 9–15 already violates the plan's own rule.** [PLAN.md §6](PLAN.md#6-three-week-delivery-calendar)
  assigns work to all seven of those dates while
  [STATUS.md](STATUS.md#decisions-in-force) records six workdays per week as a decision in force. One
  of those seven dates is an unrecorded rest day or an unrecorded overrun; either way the final week
  has no absorbed slack to lend backwards.

## 3. Dated dependency map — what *can* be assigned credibly

Dependencies are assignable with confidence. Dates are not. The distinction matters: the ordering
below is a real deliverable of this reconciliation and is unchanged from
[SLICES.md §2](SLICES.md#2-slice-order), while the date column is the thing the evidence refuses.

| ID | Slice | Depends on | Independent boundaries | Derived size | Earliest credible start | External gate dependency |
|---|---|---|---|---:|---|---|
| S2 | Course authoring and review | S1C | authoring CRUD + ownership; review/publish/retire/revision state machine; Admin taxonomy and audited pricing | 3 | Aug 3 | — |
| S3 | Public catalog, search, and shell | S2 | catalog + filters + Arabic-normalized bilingual search; Course detail/curriculum/preview/purchase choices; Arabic-default RTL/LTR responsive shell | 3 | after S2 | — |
| S4 | Media pipeline, delivery, Entitlement evaluation | S1C, S2 | upload + validation + quarantine + scanning; HLS queue/processing/retry/cleanup/status UI; Entitlement grant record, scope evaluation, expiry, revocation; protected Resource/Lab download with buyer tag | 4 | after S2 | **Malware scanning provider** |
| S5 | Protected learning | S3, S4 | dashboard/Course home/player/progress/resume/completion; access boundaries + community-link authorization + preview isolation + reporting | 2 | after S3 and S4 | — |
| S6 | Orders, checkout, and coupons | S2, S4 | price snapshot + minor-unit money + order idempotency + eligibility; coupon scopes/caps/redemption; Tap-hosted checkout initiation and failure UI | 3 | after S4 | **Tap activation** |
| S7 | Payments, entitlement grants, refunds | S6 | webhook authenticity/replay/dedup/state machine; transactional grant creation + out-of-order handling + reconciliation + manual repair; refunds and coupon release | 3 | after S6 | **LG-007–LG-010, Tap** |
| S8 | Instructor and Admin operations | S5, S7 | Instructor analytics + privacy-limited roster; the Admin identity/content/price/coupon/order/payment/refund/entitlement/report/media-recovery surface; audit of every privileged operation | 3 | after S5 and S7 | — |
| S9 | Office hours and notifications | S4, S5 | office-hours lifecycle with Course-state/ownership/entitlement rules; in-app notification centre + transactional email; delivery isolation and retry | 2 | after S5 | **LG-018 email sender** |
| S10 | Revenue, payouts, compliance, recovery | S7, S8 | revenue reports + payout calculation + adjustments + statements; legal pages, consent versions, privacy-request operations, support routes, runbooks | 3 | after S8 | **Counsel, accounting** |

**Derived total for S2–S10: 26 envelope days.** Available feature dates before the August 10
integration runway: **6**. The S11–S16 dates cannot absorb any of it — S11 depends on S1A–S10 by
definition, and S13's security gate and S14's gate audit are the checks that make the launch decision
meaningful rather than ceremonial.

Three of these slices are additionally blocked on external parties who have **not yet been contacted**,
by the founder's own July 23 decision to defer outreach to August 6. S4 needs a malware scanner, S6
and S7 need a live Tap merchant account, S9 needs a verified email sender, and S10 needs counsel and
accounting. No amount of engineering rate closes those.

## 4. Verdict

**August 8 is no longer a credible runway start**, and a **full-PRD public launch on August 15 is not
forecastable** from this calendar.

Stated precisely, so it can be checked rather than believed:

- At the *most favourable* assumption available — one envelope day per slice, a rate never yet
  achieved on a product slice — S3–S8 need six dates and have three. **August 8 fails by three days.**
- At the *derived* sizing in §3, S2–S10 need 26 days and have six. **August 15 fails by roughly 20
  workdays**, which at six workdays per week places a full-PRD launch in **early-to-mid September**.
- Three slices are gated on external parties not yet contacted, so even a correct engineering forecast
  cannot be converted into a launch date until the August 6 outreach produces acknowledged delivery
  dates.

### 4.1 Why the verdict does not rest on the estimates

The §3 sizes have one calibration point and could be wrong in either direction. The verdict survives
that, because the August 8 failure is proven by counting dates alone — six slices, three dates — with
no velocity assumption of any kind. The estimates affect *how far* past August 15 a full-PRD launch
lands, not *whether* it lands past it.

The one way the arithmetic could be wrong in the optimistic direction is if several downstream slices
are materially smaller than S1. That is possible for S5 and S9 and implausible for S4, S6, S7, and
S8, which carry the payment, media-safety, and Admin-authority surfaces — historically the boundaries
that expanded, not the ones that held.

## 5. Remedies requiring developer approval

Exactly three remedies exist. Each is a canonical change reserved to the developer under
[PLAN.md §3](PLAN.md#replan), and none was adopted by this document as first written. They are mutually
combinable.

> **Resolved the same day.** The developer adopted **Remedy A** on 2026-08-02, recorded as
> [D-039](../DECISIONS.md#d-039--remedy-a-adopted-scope-preserved-public-target-moves-to-september).
> August 8 and the August 15 full-PRD target are retired as non-credible, full scope is preserved, and
> the public target moves into September with **no exact date set** — see §6. **Remedy B is not
> adopted and not rejected**, and remains available after the August 6 outreach as an optimization of
> the new plan. **Remedy C is rejected.**

### Remedy A — Move the public launch date, full PRD preserved

Keep every PRD capability and re-date the runway. On the §3 sizing this places S11 integration around
**September 1**, the security and staging gates around **September 3–5**, and a public go/no-go around
**September 7–12**. August 7 stays protected and a second recovery day is added in the extended weeks,
because 26 consecutive envelope days without slack is not a plan.

**Cost:** roughly three to four weeks of runway. **Preserves:** all scope, all gate evidence, the
8–10 hour envelope, and the six-workdays-per-week rule.

### Remedy B — Reduce launch scope, August 15 preserved

Cut capability from the *public launch* set — deferring to fast-follow, not deleting from the PRD —
until the remainder fits six feature days. To close a 20-day gap the cut has to be structural, not
cosmetic. The candidates, in the order they cost the least revenue and safety:

| Candidate | Slice | Recovers | What launch loses |
|---|---|---:|---|
| Office hours | S9 | ~2 days | The MVP follow-up loop — the differentiator named in the PRD problem statement |
| Revenue reports and payout calculation | S10 | ~2 days | Instructor payouts become manual with recorded statements only |
| Instructor analytics | S8 | ~1 day | Instructors see enrolment but not analytics at launch |
| Coupons and free grants | S6 | ~1 day | No launch promotions |
| Section-level purchase | S2, S6 | ~2 days | Course-level purchase only |
| Labs and downloadable Resources | S4 | ~2 days | Video-only launch |

That is roughly 10 days against a 20-day gap, and it already reaches the follow-up loop the product
exists to deliver. **A scope reduction alone does not close this.** It is a partial remedy that must
be combined with Remedy A, and it forces a product judgement about what Gradex *is* at launch, which
is why it is not a decision Claude can take.

**Cost:** launch capability, and a fast-follow backlog carried into revenue operations.
**Preserves:** the August 15 date, only in combination with A.

### Remedy C — Change the operating envelope

Raise daily capacity, add working days, or compress slices below the
[PLAN.md §2](PLAN.md#daily-capacity) envelope.

**This is recorded for completeness and recommended against.** The evidence in this repository is
already that compression does not work: S1 was split three ways precisely because compressing it
would have removed failure paths, and the two most recent carryovers —
`CARRYOVER-DOCS-GUARD-UNTRACKED` and `CARRYOVER-LOCAL-BUILD-CACHE` — are both instances of a check
that read green while testing less than it claimed. Compressing an identity, payment, or media slice
buys days by removing exactly the evidence that would catch that class of defect, on a solo-developer
project with no second reader.

Spending August 7 belongs here too, and is worth one day against a twenty-day gap while removing the
only recovery point before the runway.

## 6. Recommendation, and the decision taken

**Decided 2026-08-02: Remedy A, recorded as
[D-039](../DECISIONS.md#d-039--remedy-a-adopted-scope-preserved-public-target-moves-to-september).**
August 8 and the August 15 full-PRD target are retired. Full PRD scope is preserved. The public target
moves into September.

**One important amendment to what §4 said.** The "early-to-mid September" range in §4 must **not** be
recorded as a target or committed to publicly. It is a forecast hypothesis derived from §3's estimates,
which have a single calibration point, and it sits behind 21 open launch gates and four external
dependencies nobody has contacted. Replacing one uncredible date with another would repeat the exact
error being corrected here. **No exact public date is set until two inputs exist:**

1. the August 6 outreach results — acknowledged requests with delivery dates from counsel, accounting,
   Tap, email, hosting, and malware scanning; and
2. a critical-path rebaseline of S2–S16 against those results.

**Remedy B remains available afterwards**, as an optimization of the new plan rather than a rescue of
August 15 — deciding it now would be deciding it blind to the outreach. **Remedy C is rejected**: it
attacks slice quality and evidence integrity, which is the part of this delivery that is currently
working.

The original recommendation, retained for the record:

**Remedy A, with Remedy B held in reserve.**

Move the public target and preserve scope and evidence. The reasoning is that the two failures this
calendar produces are not symmetric. Missing a self-imposed date on a pre-launch product with no
external commitments, no paying customers, and no announced launch costs credibility with nobody —
[LAUNCH_GATES.md](../LAUNCH_GATES.md) records no counterparty who has been promised August 15.
Launching a payment and media platform on a compressed security gate costs money, student trust, and
possibly a Kuwait Digital Commerce Law exposure that cannot be rolled back.

Remedy B should be decided *after* the August 6 outreach returns, not before: if Tap activation or
counsel lead times independently push past August 15, a scope cut made now would have bought nothing
and lost capability permanently.

## 7. What this document deliberately does not do

- **It does not date S3–S8.** [SLICES.md §2](SLICES.md#2-slice-order) keeps them `TBD`. Assigning
  dates the evidence contradicts would replace a visible problem with an invisible one.
- **It does not start S2 or any later slice.** No downstream schema, route, screen, or specification
  was authored here.
- **It does not reduce PRD scope or move the public target.** Both are developer decisions.
- **It does not spend August 7**, and it does not treat the protected day as available slack.
- **It does not touch S1C's plan or evidence.** S1C's contents are unaffected by which remedy is
  chosen; only what follows S1C changes.

## 8. Effect on launch confidence

Confidence stays **Red**, and this reconciliation makes the reason more precise rather than changing
it. Red was previously driven by 21 open launch gates and by how little of the product is implemented.
It is now additionally driven by a *recorded* forecast failure with a named remedy set awaiting
developer decision — which under [PLAN.md §5](PLAN.md#5-launch-confidence) is the difference between a
gate at risk with a credible resolution path and one without.

The next dated action this document creates is a developer decision on §5, and it should be taken
before August 4 — the first unassigned date — so that the day is spent against a chosen plan rather
than against a conflict.
