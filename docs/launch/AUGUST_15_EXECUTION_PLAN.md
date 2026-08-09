# August 15 Execution Plan

> Status: Active
> Authority: [D-040](../DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews),
> workflow reassigned by [D-042](../DECISIONS.md#d-042--codex-plans-antigravity-implements-and-claude-independently-reviews)
> Created: 2026-07-27 (real calendar) — 19 calendar days to launch
> Supersedes the calendar in [PLAN.md §6](PLAN.md#6-three-week-delivery-calendar) and the `TBD` rows in
> [SLICES.md §2](SLICES.md#2-slice-order)

This document holds the three things D-040 requires and PLAN.md does not: the classification of every
remaining requirement, the dated assignment of every remaining slice, and the record of what moves to
manual operation or post-launch and why.

Dependency ordering is **not** changed by this plan. [SLICES.md §3](SLICES.md#3-ordering-decisions-that-removed-forward-dependencies)
remains the ordering of record, including the load-bearing separation of Entitlement *evaluation*
(S4) from Entitlement *creation* (now S6). No slice is merged with another where the merge would
weaken authorization, entitlement, or media-access verification.

> **Amended 2026-07-28 by [D-045](../DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).**
> MVP ships no in-platform payments. S7 is removed, S6 becomes the Course Access Invitation and
> Entitlement grant slice, seven launch gates move to `DEFERRED`, and the Instructor Student roster
> returns to category A. The date, the three-category scope policy, and the quality boundaries in
> D-040 are unchanged.

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
Effort estimates are implementation hours. D1–D4 retain their historical D-040 assignment; from D5,
Codex specifies with SpecKit, Antigravity implements with SpecKit, and Claude independently reviews
under D-042.

### 2.1 Launch-critical slices

| Slice | Behaviour | Cat. | Depends on | Effort | Review tier | Launch blocker |
|---|---|---|---|---|---|---|
| S1C | Staff invitation and initial-password setup; Admin suspension and reinstatement; full role/ownership authorization matrix; final full-surface bootstrap denial proof | A | S1B3 (closed) | 8h | **3** | Yes — S1 does not close without it |
| S2 | Course/Section/Lesson authoring, submission and revision workflow, Admin review/publish/delist/retire, private-draft protection, Admin-only pricing with audit history, Asset Version *references* | A | S1C | 14h | **2** | Yes |
| S3 | Public catalogue list and Course detail, responsive bilingual shell, RTL/LTR, locale persistence | A | S2 | 8h | 1 | Yes |
| S4 | Upload, quarantine, malware-scan adapter, transcode pipeline, Asset Versions, short-lived signed media access, protected resource/lab downloads, **Entitlement evaluation** (grant record, scope, expiry, revocation) | A | S1C, S2 | 18h | **3** | Yes |
| S5 | Protected learning: HLS playback through signed access, per-Lesson completion, resume position. Also introduces the minimum `enrollments` table and creates **no** Enrollment row ([SLICES.md §3.4](SLICES.md#34-s5-introduces-the-enrollments-table-s6-owns-every-enrollment-write)) | A | S3, S4 | 10h | **3** | Yes |
| S6 | **Course Access Invitation and Entitlement grant**: Admin invitation creation, identity-bound Student acceptance, Admin Approval, rejection and cancellation, idempotent **Entitlement creation**, expiry snapshot, audit and notification intents, bilingual screens for both actors. Consumes S5's `enrollments` table and owns every Enrollment write; does not recreate it | A | S2, S4, **S5** | 9h | **3** | Yes |
| S12 | Production and staging environments, migrations in deploy, secrets handling, HTTPS, health/readiness, structured logging and request IDs in production, minimum monitoring and alerting, **database backups with a tested restore** | A | S6 implementation base `dde093b`; **not S11** | 10h | **2** | Yes |
| S11 | End-to-end critical-journey tests across the full path, error states and recovery paths | A | S1C–S6, **S12 staging** | 8h | **2** | Yes |
| S13 | Security and quality gate: negative-case sweep across every Tier-3 boundary, dependency and secret audit | A | S12 | 6h | **3** | Yes |
| S14 | Staging acceptance and launch-gate audit | A | S13 | 5h | 2 | Yes |
| S15 | Production rehearsal: deploy, smoke, restore test, rollback drill | A | S14 | 5h | 2 | Yes |
| S16 | Public go/no-go and cutover | A | S15 | 3h | 2 | Yes |

### 2.2 Reduced slices — launch-critical core retained, remainder reclassified

| Slice | Retained for August 15 | Cat. | Moved out | Cat. | Effort | Review tier |
|---|---|---|---|---|---|---|
| S3 | Catalogue listing, Course detail, taxonomy display, simple published-Course text search | A | Arabic-normalized ranked search, multi-dimension filtering, sort options | C | — | 1 |
| S8 | Admin support operations: account suspension/reinstatement (S1C), **audited entitlement expiry adjustment and revocation — the AD07 mutations, owned exclusively by S8**, failed-video retry, invitation queue administration; **Course-scoped Instructor Student roster (category A under D-045)**, which must carry the roster's own authorization assertion that it reads Entitlement and never Course Access Invitation state (deferred here from S6 L1, because the roster does not exist until S8) | **B**, roster **A** | Instructor analytics dashboard, vocabulary administration UI, reported-content moderation queue | C | 8h | **2** |
| S9 | Transactional email for verification, password reset, staff invitation, course-access invitation, and access-granted confirmation; durable in-app notification records | A | Office hours (scheduling, entitlement-protected meeting link), all non-transactional notification types | C | folded into S4/S6 delivery adapter, 3h | 2 |
| S10 | Bilingual Privacy and Terms, including Terms §8 consumer-rights and Privacy §4/Terms §4 course-access disclosures, plus support route | A (LG-011) | Revenue reporting screens, payout records, emailed Instructor statements | **C** | 5h | 2 |

**Instructor payout is category C under [D-045](../DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation),
not category B.** With no in-platform revenue record there is nothing to read, so there is no manual
in-product path either — compensation is arranged and paid entirely outside Gradex. Revenue-share
terms remain a required part of the Instructor agreement under `LG-020`.

**The Instructor roster moves back to category A.** Instructor visibility into Students enrolled in
their own Courses is part of the locked MVP scope, so its earlier post-launch deferral is
overturned.

### 2.3 Deferred to post-launch — recorded, not removed

Every item below has a reason and a destination. None is deleted from the PRD.

| Item | Reason | Destination |
|---|---|---|
| Office hours (S9) | Not on the critical access-to-playback journey; needs a verified meeting-link provider and `LG-018` email | First post-launch slice, S17 |
| Instructor analytics dashboard | Read-only reporting; no student-facing journey depends on it. **The Course-scoped Student roster is no longer deferred** — it is category A under D-045 | S18 (roster: S8) |
| Reported-content moderation queue | Content volume at launch is a hand-curated catalogue; reports route to the support address and are actioned manually | S18, manual until then |
| Catalogue vocabulary administration UI | Launch taxonomy is seeded through an audited migration and changes rarely | S18 |
| Arabic-normalized ranked search and filtering | Launch catalogue is small enough for listing plus simple text match | S18 |
| **External Discord/Telegram Course community link** | Deferred by [D-046](../DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch): no slice ever authored it, and closing the gap would reopen S2's frozen queue for a convenience that is not on the access-to-playback path | S18. The Discord community itself is unaffected; only the in-product link moves |
| Revenue reporting and payout screens | No in-platform revenue record exists to report; compensation is arranged entirely out of band under D-045 | S19, with in-platform payments |
| Automated Instructor settlement | `FF-004`, already fast-follow | Fast-follow register |
| Certificates, reviews and ratings, wishlist, gamification, recommendations | Already outside MVP | Future register |
| **In-platform checkout, Orders, Tap payment, payment webhooks, coupons, cart, automated refunds, invoices, reconciliation, Section/chapter/bundle/partial-course acquisition** | Deferred by D-045. Access converges on the same Entitlement whenever payment is added, through the `grant_source` seam | Post-launch payment programme |
| Deema BNPL, bundles, captions/transcripts, MFA/social login, lifecycle marketing notifications | Already `FF-001`–`FF-006` | Fast-follow register |
| Deep visual polish, UI animation, design-system project | Not a launch gate; the bilingual responsive shell in S3 is the bar | Post-launch |
| Native applications | Never in MVP | Future register |

**`LG-015` accessibility:** WCAG 2.2 AA validation of platform-owned UI and player controls stays
category A and runs inside S13. The caption gap (`FF-003`) is disclosed in the accessibility claim
copy rather than closed.

### 2.4 Launch gates on the critical path

The gates that can stop the August 15 cutover regardless of code, from
[LAUNCH_GATES.md](../LAUNCH_GATES.md):

**Seven gates left this table on 2026-07-28** — `LG-001`, `LG-002`, `LG-007`, `LG-008`, `LG-009`,
`LG-010`, and `LG-017` are `DEFERRED` with in-platform payments under D-045. **The legal and
accounting gates did not move**, and engineering may not move them.

| Gate | Blocks | Required by | If unresolved |
|---|---|---|---|
| `LG-005`, `LG-006`, `LG-011` | Legal launch | Aug 12 | **No launch.** Publishing without the required registration or bilingual policies is a legal blocker. Moving payment off-platform does not answer whether a Kuwait course platform must register |
| `LG-016` | Financial record-keeping for externally collected payment | Aug 12 | Unresolved treatment of how off-platform receipts must be recorded and retained. Off-platform collection may move this obligation rather than remove it |
| `LG-018` | Transactional email | Aug 7 | Degraded: verification and reset links become an authorised Admin-issued manual path (category B), which does not scale past a soft launch |
| `LG-021` | Compromised-password screening | Resolved Aug 9 | HIBP Pwned Passwords Range API is integrated under D-075; an offline dataset is not selected for launch. Credential admission fails closed if HIBP is unavailable |
| `LG-014` | Malware scanning | Aug 5 (S4) | Assets stay quarantined and unpublishable. Fallback: launch catalogue uploaded by the Admin only, scanned out of band, with public upload disabled |
| `LG-012` | Launch prices | Aug 11 | Catalogue shows no price, so a Student cannot know what to pay externally. Blocks a sellable catalogue |
| `LG-003`, `LG-004`, `LG-019`, `LG-020`, `LG-013` | Retention, privacy, operating envelope, Instructor agreement, support ownership | Aug 12 | Retention jobs stay disabled; the operating envelope is recorded as provisional; the launch catalogue ships only with signed Instructor evidence |

**The August 6 outreach is the single largest launch risk and it is now nine days out, not one.**
Sending it on July 28 instead of August 6 is the highest-value schedule action available and costs no
engineering time.

## 3. Nineteen-day execution plan

Six working days per week; Sundays August 2 and August 9 are rest days. D1–D4 are historical under
D-040. From D5, Codex owns specification, Antigravity owns implementation, and Claude reviews only
frozen exact ranges, so the never-self-approve rule continues to apply.

| Day | Date | Implementation | Planning / review |
|---|---|---|---|
| D1 | Jul 27 Mon | **S1C Musts 3–4**: staff invitation and initial-password setup; suspension and reinstatement with three independent proofs | Reality check, D-040, freeze S1C spec/plan, issue handoff prompt |
| D2 | Jul 28 Tue | **S1C Musts 5–6**: full authorization matrix mechanically tied to the router; bilingual staff screens | Freeze **S2** spec and plan. **Founder sends the entire August 6 outreach pack today** |
| D3 | Jul 29 Wed | S1C remediation; begin S2 | **Tier 3 review of S1C**; accept or reject; freeze **S3** plan |
| D4 | Jul 30 Thu | **S2**: authoring, submission/revision, Admin review lifecycle, private-draft protection, Admin-only pricing with audit | Freeze **S4** spec and plan (largest slice, plan it early) |
| D5 | Jul 31 Fri | **Antigravity:** S2 completion and remediation through `speckit.implement` | **Codex:** specify through `speckit.specify`; **Claude:** Tier 2 review of frozen S2 range; accept or reject |
| D6 | Aug 1 Sat | **Antigravity:** **S3**: public catalogue, Course detail, bilingual responsive shell, RTL/LTR, locale persistence | **Codex:** freeze **S5** specification; **Claude:** Tier 1 review of frozen S3 range |
| — | Aug 2 Sun | Rest | Rest |
| D7 | Aug 3 Mon | **Antigravity:** **S4 part 1**: upload, quarantine, scanner adapter, transcode pipeline, Asset Versions | **Codex:** freeze the **S6 Course Access Invitation and Entitlement grant** specification — the slice has none yet |
| D8 | Aug 4 Tue | **Antigravity:** **S4 part 2**: short-lived signed media access, protected downloads, **Entitlement evaluation** | **Codex:** freeze **S8-reduced** specification, including the Instructor roster restored to category A |
| D9 | Aug 5 Wed | **Antigravity:** S4 remediation; begin **S5** protected learning | **Claude:** Tier 3 review of frozen S4 range; accept or reject |
| D10 | Aug 6 Thu | **Antigravity:** **S5**: HLS playback through signed access, per-Lesson completion, resume position | **Claude:** Tier 3 review of frozen S5 range; **Codex:** freeze **S10-reduced** specification. Outreach responses chased |
| D11 | Aug 7 Fri | **Antigravity:** **S12**: staging and production environments, deploy pipeline with migrations, secrets, HTTPS, health checks, monitoring and alerting, **backups with a tested restore**. **First staging deploy of everything through S5** | **Claude:** Tier 2 review of frozen S12 range; verify the restore actually restored |
| D12 | Aug 8 Sat | **Antigravity:** **S6**: Course Access Invitation lifecycle, identity-bound acceptance, Admin Approval creating Enrollment + Entitlement idempotently, audit and notification intents, bilingual screens | **Claude:** freeze the S6 review brief; **float** — the freed S7 capacity absorbs D1–D11 overrun |
| — | Aug 9 Sun | Rest | Rest |
| D13 | Aug 10 Mon | **Antigravity:** S6 remediation; **S8-reduced** admin support operations and the **Instructor Student roster** | **Claude:** Tier 3 review of frozen S6 range — the deepest release review; accept or reject |
| D14 | Aug 11 Tue | **Antigravity:** **S10-reduced** bilingual legal and support pages; launch prices entered through the audited Admin path (`LG-012`). **Float day** — absorbs any overrun from D1–D13 | **Claude:** Tier 2 review of frozen S8 range |
| D15 | Aug 12 Wed | **Antigravity:** S8/S10 remediation. **Second float day**, freed by removing S7 | **Claude:** Tier 2 review of frozen S10 range. Launch-gate audit against every `LG-` row |
| D16 | Aug 13 Thu | **Antigravity:** **S11** end-to-end critical-journey tests, error states and recovery paths; **S13** hardening fixes | **Claude:** Tier 3 security and quality gate across every critical boundary: negative cases, replay, duplication, stale state, races, bypass attempts |
| D17 | Aug 14 Fri | **Antigravity:** **S14** staging acceptance; **S15** production rehearsal: full deploy, smoke, restore, rollback drill | **Claude:** gate audit sign-off; go/no-go criteria evaluated against evidence |
| D18 | **Aug 15 Sat** | **Antigravity:** **S16** production cutover, smoke tests, monitoring watch | **Claude:** go/no-go decision; accept the release or invoke rollback |

### 3.1 Rules this calendar obeys

- No day carries two incompatible critical tasks. Tier-3 reviews (D3, D9, D10, D13, D16) are never
  paired with another Tier-3 review on the same day.
- Every day ends with demonstrable user-visible or operational behaviour.
- Staging exists from **August 7**, eight days before cutover — not for the first time on launch eve.
- The production rehearsal on **August 14** is a full deploy plus rollback, not a checklist read.
- **D14 (August 11) and D15 (August 12) are float**, and D12 carries slack. Removing S7 under D-045
  returned roughly 14 hours of Tier-3 work to the runway, which is where the second float day came
  from. There is still no protected recovery week.

### 3.2 Manual operational paths in force at launch

Each is authorised, auditable, documented, and safe:

1. **External Payment confirmation and course-access granting** — the Admin confirms payment out of
   band, creates a Course Access Invitation, and approves it after the Student accepts. Capability-
   gated, recent-auth required, idempotent, fully audited. **This is the primary operating path of
   the MVP, not a fallback.**
2. **Entitlement expiry adjustment and revocation** — audited elevated-Admin adjustment. It may not
   mint an Entitlement, per the provenance invariant in
   [SLICES.md §3.1](SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation); creation is
   Admin Approval only.
3. **Failed-video retry** — Admin re-enqueues processing for a named Asset Version.
4. **Instructor compensation** — arranged and paid entirely outside Gradex; no in-product path.
5. **Refunds and payment reconciliation** — handled entirely outside Gradex. If access must end, the
   Admin uses the audited adjustment or revocation in item 2.
6. **Content moderation** — reports route to the support address; Admin actions Course suspension
   through the S2 emergency-suspension path.
7. **Support recovery** — Admin resends verification or reset intents; if `LG-018` is unresolved, an
   Admin issues them directly through an audited operation.
8. **Catalogue upload** — if `LG-014` is unresolved, public upload stays disabled and the Admin loads
   the launch catalogue with out-of-band scanning.

## 4. Go/no-go criteria for August 15

Launch proceeds only if **all** of these hold. Any one failing is a no-go, and the correct response
is a dated slip announced to nobody, not a waiver.

1. The full critical journey passes end to end on staging **and** on production smoke: register →
   invitation → accept → Admin Approval → playback.
2. No unresolved critical defect; every high-severity defect has documented risk acceptance,
   mitigation, and owner approval.
3. Admin Approval is proven idempotent under repetition and concurrency, capability-gated, and
   recent-authentication-bound, and is proven to **refuse** rather than degrade when either control
   is absent.
4. No Entitlement can be created except through recorded Admin Approval — proven by asserting the S4
   test seed is absent from a production build, not merely disabled. No production build contains a
   checkout, payment-callback, refund, or coupon path.
5. Media objects are private; every access is short-lived and signed.
6. Suspension, ownership, role, and identity-bound invitation acceptance are proven server-side by
   direct API call, including that a non-invited identity cannot accept.
7. A database restore has been performed successfully from a real backup.
8. `LG-005`, `LG-006`, `LG-011`, `LG-012`, and `LG-016` are `RESOLVED`. The seven payment gates are
   `DEFERRED` under D-045 and are therefore not evaluated here — deferral is not resolution, and none
   of them may be marked `RESOLVED` to satisfy this criterion.
9. Monitoring, alerting, and the rollback procedure are exercised, not merely configured.
