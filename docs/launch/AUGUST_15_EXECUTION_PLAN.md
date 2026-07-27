# August 15 Execution Plan

> Status: Active
> Authority: [D-040](../DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews)
> Created: 2026-07-27 (real calendar) — 19 calendar days to launch
> Supersedes the calendar in [PLAN.md §6](PLAN.md#6-three-week-delivery-calendar) and the `TBD` rows in
> [SLICES.md §2](SLICES.md#2-slice-order)

This document holds the three things D-040 requires and PLAN.md does not: the classification of every
remaining requirement, the dated assignment of every remaining slice, and the record of what moves to
manual operation or post-launch and why.

Dependency ordering is **not** changed by this plan. [SLICES.md §3](SLICES.md#3-ordering-decisions-that-removed-forward-dependencies)
remains the ordering of record, including the load-bearing separation of Entitlement *evaluation*
(S4) from Entitlement *creation* (S7). No slice is merged with another where the merge would weaken
authorization, payment, entitlement, or media-access verification.

## 1. Calendar reconciliation

The repository ran two calendars and read the wrong one when forecasting.

| | Schedule calendar | Real calendar |
|---|---|---|
| Day 6 / S0 | July 28 | July 23 |
| Day 11 / S1C plan | August 2 | July 27 |
| Elapsed to reach S1C | 11 schedule days | 5 real days |

Commit dates confirm it: 28 commits on 2026-07-23, 6 on 07-24, 34 on 07-25, 18 on 07-26, 21 on
07-27. Observed throughput is **≈2.2 schedule-days per real day**.

[D-038](../DECISIONS.md#d-038--august-8-is-no-longer-a-credible-runway-start-s3s8-remain-undated-pending-a-developer-remedy)
counted remaining *schedule* days (August 3–9) as if they were real days and found a deficit. On the
real calendar, 19 days remain. The deficit was an artifact of double-counting elapsed time.

**This corrects an arithmetic error only.** D-038's qualitative risks are unaffected and remain the
live risk register: 21 open launch gates, four uncontacted external dependencies, and S1 expanding
from one estimated day to five schedule days. Those are handled by scope classification in §2, not by
the calendar correction.

From this document forward there is **one calendar: the real one.** Day records are dated by real
date. The schedule-day numbering ends at Day 11.

## 2. Scope matrix

Category **A** = Launch Critical, **B** = Manual but Supported, **C** = Post-Launch.
Effort is focused Antigravity implementation hours under the D-040 workflow, excluding Claude
planning and review.

### 2.1 Launch-critical slices

| Slice | Behaviour | Cat. | Depends on | Effort | Review tier | Launch blocker |
|---|---|---|---|---|---|---|
| S1C | Staff invitation and initial-password setup; Admin suspension and reinstatement; full role/ownership authorization matrix; final full-surface bootstrap denial proof | A | S1B3 (closed) | 8h | **3** | Yes — S1 does not close without it |
| S2 | Course/Section/Lesson authoring, submission and revision workflow, Admin review/publish/delist/retire, private-draft protection, Admin-only pricing with audit history, Asset Version *references* | A | S1C | 14h | **2** | Yes |
| S3 | Public catalogue list and Course detail, responsive bilingual shell, RTL/LTR, locale persistence | A | S2 | 8h | 1 | Yes |
| S4 | Upload, quarantine, malware-scan adapter, transcode pipeline, Asset Versions, short-lived signed media access, protected resource/lab downloads, **Entitlement evaluation** (grant record, scope, expiry, revocation) | A | S1C, S2 | 18h | **3** | Yes |
| S5 | Protected learning: HLS playback through signed access, per-Lesson completion, resume position | A | S3, S4 | 10h | **3** | Yes |
| S6 | Order creation (one Course or one Section), checkout initiation against Tap hosted payment, coupon redemption and capacity reservation, expiry snapshot onto the Order | A | S2, S4 | 12h | **3** | Yes |
| S7 | Payment callback verification, idempotency and replay handling, order/payment state integrity, **Entitlement creation** from verified payment, refunds and their entitlement effect | A | S6 | 14h | **3** | Yes |
| S12 | Production and staging environments, migrations in deploy, secrets handling, HTTPS, health/readiness, structured logging and request IDs in production, minimum monitoring and alerting, **database backups with a tested restore** | A | S11 inputs | 10h | **2** | Yes |
| S11 | End-to-end critical-journey tests across the full path, error states and recovery paths | A | S1C–S7 | 8h | **2** | Yes |
| S13 | Security and quality gate: negative-case sweep across every Tier-3 boundary, dependency and secret audit | A | S12 | 6h | **3** | Yes |
| S14 | Staging acceptance and launch-gate audit | A | S13 | 5h | 2 | Yes |
| S15 | Production rehearsal: deploy, smoke, restore test, rollback drill | A | S14 | 5h | 2 | Yes |
| S16 | Public go/no-go and cutover | A | S15 | 3h | 2 | Yes |

### 2.2 Reduced slices — launch-critical core retained, remainder reclassified

| Slice | Retained for August 15 | Cat. | Moved out | Cat. | Effort | Review tier |
|---|---|---|---|---|---|---|
| S3 | Catalogue listing, Course detail, taxonomy display, simple published-Course text search | A | Arabic-normalized ranked search, multi-dimension filtering, sort options | C | — | 1 |
| S8 | Admin support operations: account suspension/reinstatement (S1C), entitlement correction, failed-video retry, manual refund initiation, order lookup | **B** | Instructor analytics dashboard, Course-scoped Student roster, vocabulary administration UI, reported-content moderation queue | C | 8h | **2** |
| S9 | Transactional email for verification, password reset, staff invitation, and purchase confirmation; durable in-app notification records | A | Office hours (scheduling, entitlement-protected meeting link), all non-transactional notification types | C | folded into S4/S7 delivery adapter, 3h | 2 |
| S10 | Bilingual Privacy Notice, Terms, Refund Policy, checkout disclosures, support route | A (LG-011) | Revenue reporting screens, payout records, emailed Instructor statements | **B** | 5h | 2 |

**Instructor payout is category B by design, not by compression.** The PRD already specifies a manual
monthly payout. At launch it is: Admin reads completed Orders through a read-only query, computes the
share against the approved `LG-001` percentage, pays out of band, and records the payout with an
audit entry. No automated settlement ships.

### 2.3 Deferred to post-launch — recorded, not removed

Every item below has a reason and a destination. None is deleted from the PRD.

| Item | Reason | Destination |
|---|---|---|
| Office hours (S9) | Not on the critical purchase-to-playback journey; needs a verified meeting-link provider and `LG-018` email | First post-launch slice, S17 |
| Instructor analytics and Student roster | Read-only reporting; no student-facing journey depends on it | S18 |
| Reported-content moderation queue | Content volume at launch is a hand-curated catalogue; reports route to the support address and are actioned manually | S18, manual until then |
| Catalogue vocabulary administration UI | Launch taxonomy is seeded through an audited migration and changes rarely | S18 |
| Arabic-normalized ranked search and filtering | Launch catalogue is small enough for listing plus simple text match | S18 |
| Revenue reporting and payout screens | Manual path in §2.2 covers the operation | S19 |
| Automated Instructor settlement | `FF-004`, already fast-follow | Fast-follow register |
| Certificates, reviews and ratings, wishlist, gamification, recommendations | Already outside MVP | Future register |
| Deema BNPL, bundles, captions/transcripts, MFA/social login, lifecycle marketing notifications | Already `FF-001`–`FF-006` | Fast-follow register |
| Deep visual polish, UI animation, design-system project | Not a launch gate; the bilingual responsive shell in S3 is the bar | Post-launch |
| Native applications | Never in MVP | Future register |

**`LG-015` accessibility:** WCAG 2.2 AA validation of platform-owned UI and player controls stays
category A and runs inside S13. The caption gap (`FF-003`) is disclosed in the accessibility claim
copy rather than closed.

### 2.4 Launch gates on the critical path

The gates that can stop the August 15 cutover regardless of code, from
[LAUNCH_GATES.md](../LAUNCH_GATES.md):

| Gate | Blocks | Required by | If unresolved |
|---|---|---|---|
| `LG-007`, `LG-008` | Production payment activation | Aug 10 (S7 review) | **No launch.** No manual path exists for accepting money |
| `LG-010` | Payment callback authenticity | Aug 10 | **No launch.** Callback verification may not be skipped |
| `LG-005`, `LG-006`, `LG-011` | Legal launch | Aug 12 | **No launch.** Publishing without the required registration or bilingual policies is a legal blocker |
| `LG-018` | Transactional email | Aug 7 | Degraded: verification and reset links become an authorised Admin-issued manual path (category B), which does not scale past a soft launch |
| `LG-021` | Compromised-password screening | Aug 7 | Fails closed at credential creation, which blocks registration. A licensed offline dataset is the fallback |
| `LG-014` | Malware scanning | Aug 5 (S4) | Assets stay quarantined and unpublishable. Fallback: launch catalogue uploaded by the Admin only, scanned out of band, with public upload disabled |
| `LG-001` | Payout configuration | Aug 12 | Earnings are not calculated; payouts wait. Does not block launch |
| `LG-012` | Launch prices | Aug 11 | **No launch** of a purchasable catalogue |
| `LG-002`, `LG-009`, `LG-016`, `LG-017` | Refund/tax/dispute policy | Aug 12 | Refunds run entirely as an authorised manual Admin operation with recorded reason (category B) |
| `LG-003`, `LG-004`, `LG-019`, `LG-020`, `LG-013` | Retention, privacy, operating envelope, Instructor agreement, support ownership | Aug 12 | Retention jobs stay disabled; the operating envelope is recorded as provisional; the launch catalogue ships only with signed Instructor evidence |

**The August 6 outreach is the single largest launch risk and it is now nine days out, not one.**
Sending it on July 28 instead of August 6 is the highest-value schedule action available and costs no
engineering time.

## 3. Nineteen-day execution plan

Six working days per week; Sundays August 2 and August 9 are rest days. Claude and Antigravity work
concurrently: Claude never plans a slice whose contracts overlap the slice Antigravity is currently
implementing.

| Day | Date | Antigravity implements | Claude plans / reviews |
|---|---|---|---|
| D1 | Jul 27 Mon | **S1C Musts 3–4**: staff invitation and initial-password setup; suspension and reinstatement with three independent proofs | Reality check, D-040, freeze S1C spec/plan, issue handoff prompt |
| D2 | Jul 28 Tue | **S1C Musts 5–6**: full authorization matrix mechanically tied to the router; bilingual staff screens | Freeze **S2** spec and plan. **Founder sends the entire August 6 outreach pack today** |
| D3 | Jul 29 Wed | S1C remediation; begin S2 | **Tier 3 review of S1C**; accept or reject; freeze **S3** plan |
| D4 | Jul 30 Thu | **S2**: authoring, submission/revision, Admin review lifecycle, private-draft protection, Admin-only pricing with audit | Freeze **S4** spec and plan (largest slice, plan it early) |
| D5 | Jul 31 Fri | S2 completion and remediation | **Tier 2 review of S2**; accept |
| D6 | Aug 1 Sat | **S3**: public catalogue, Course detail, bilingual responsive shell, RTL/LTR, locale persistence | **Tier 1 review of S3** at end of day; freeze **S5** plan |
| — | Aug 2 Sun | Rest | Rest |
| D7 | Aug 3 Mon | **S4 part 1**: upload, quarantine, scanner adapter, transcode pipeline, Asset Versions | Freeze **S6** spec and plan |
| D8 | Aug 4 Tue | **S4 part 2**: short-lived signed media access, protected downloads, **Entitlement evaluation** | Freeze **S7** spec and plan |
| D9 | Aug 5 Wed | S4 remediation; begin **S5** protected learning | **Tier 3 review of S4** — signed access and entitlement evaluation; accept |
| D10 | Aug 6 Thu | **S5**: HLS playback through signed access, per-Lesson completion, resume position | **Tier 3 review of S5**; freeze **S8-reduced** and **S10-reduced** plans. Outreach responses chased |
| D11 | Aug 7 Fri | **S12**: staging and production environments, deploy pipeline with migrations, secrets, HTTPS, health checks, monitoring and alerting, **backups with a tested restore**. **First staging deploy of everything through S5** | **Tier 2 review of S12**; verify the restore actually restored |
| D12 | Aug 8 Sat | **S6**: Orders, checkout initiation against Tap, coupons with capacity reservation, expiry snapshot | Review S6 contracts against `LG-008`/`LG-010` responses |
| — | Aug 9 Sun | Rest | Rest |
| D13 | Aug 10 Mon | **S7**: callback verification, idempotency and replay, order/payment state integrity, **Entitlement creation**, refunds | **Tier 3 review of S6**; accept |
| D14 | Aug 11 Tue | S7 remediation. **Float day** — absorbs any overrun from D1–D13 | **Tier 3 review of S7** — the deepest review of the release; accept |
| D15 | Aug 12 Wed | **S8-reduced** admin support operations; **S10-reduced** bilingual legal and support pages; launch prices entered through the audited Admin path (`LG-012`) | **Tier 2 review of S8/S10**; accept. Launch-gate audit against every `LG-` row |
| D16 | Aug 13 Thu | **S11**: end-to-end critical-journey tests, error states and recovery paths; **S13** hardening fixes | **Tier 3 security and quality gate** across every critical boundary: negative cases, replay, duplication, stale state, races, bypass attempts |
| D17 | Aug 14 Fri | **S14** staging acceptance; **S15 production rehearsal**: full deploy, smoke, restore, rollback drill | Gate audit sign-off; go/no-go criteria evaluated against evidence |
| D18 | **Aug 15 Sat** | **S16**: production cutover, smoke tests, monitoring watch | **Go/no-go decision**; accept the release or invoke rollback |

### 3.1 Rules this calendar obeys

- No day carries two incompatible critical tasks. Tier-3 reviews (D3, D9, D10, D13, D14, D16) are
  never paired with another Tier-3 review on the same day.
- Every day ends with demonstrable user-visible or operational behaviour.
- Staging exists from **August 7**, eight days before cutover — not for the first time on launch eve.
- The production rehearsal on **August 14** is a full deploy plus rollback, not a checklist read.
- **D14 (August 11) is the only float.** There is no protected recovery week. That is the accepted
  consequence of the hard date, and it is stated rather than hidden.

### 3.2 Manual operational paths in force at launch

Each is authorised, auditable, documented, and safe:

1. **Refund review and initiation** — Admin-only, capability-gated, recent-auth required, reason
   recorded, entitlement effect applied by the same transaction.
2. **Entitlement correction** — restores an Entitlement only from an already-valid completed Order
   Item. It may not mint one, per the provenance invariant in
   [SLICES.md §3.1](SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation).
3. **Failed-video retry** — Admin re-enqueues processing for a named Asset Version.
4. **Instructor payout** — computed and paid out of band, recorded with an audit entry.
5. **Reconciliation** — Admin reads provider transactions against Orders and records discrepancies.
6. **Content moderation** — reports route to the support address; Admin actions Course suspension
   through the S2 emergency-suspension path.
7. **Support recovery** — Admin resends verification or reset intents; if `LG-018` is unresolved, an
   Admin issues them directly through an audited operation.
8. **Catalogue upload** — if `LG-014` is unresolved, public upload stays disabled and the Admin loads
   the launch catalogue with out-of-band scanning.

## 4. Go/no-go criteria for August 15

Launch proceeds only if **all** of these hold. Any one failing is a no-go, and the correct response
is a dated slip announced to nobody, not a waiver.

1. The full critical journey passes end to end on staging **and** on production smoke.
2. No unresolved critical defect; every high-severity defect has documented risk acceptance,
   mitigation, and owner approval.
3. Payment callback verification, idempotency, and replay handling are proven against `LG-010`
   vectors.
4. No Entitlement can be created except from a verified payment or a zero-value Coupon Order Item.
5. Media objects are private; every access is short-lived and signed.
6. Suspension, ownership, and role enforcement are proven server-side by direct API call.
7. A database restore has been performed successfully from a real backup.
8. `LG-005`, `LG-006`, `LG-007`, `LG-008`, `LG-010`, `LG-011`, and `LG-012` are `RESOLVED`.
9. Monitoring, alerting, and the rollback procedure are exercised, not merely configured.
