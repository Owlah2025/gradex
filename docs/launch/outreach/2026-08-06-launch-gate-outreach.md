# Launch-Gate Outreach Pack

> Status: **READY TO SEND** — recipient placeholders outstanding; every reply date rebased for a
> **2026-07-28** send
> Public go-live target: 2026-08-15
> Prepared: 2026-07-23 · Rebased: 2026-07-28
> Send date: **2026-07-28**, pulled forward nine days from the 2026-08-06 date the founder set on
> 2026-07-23. The deferral is superseded, not overridden silently: it cost nothing to reverse and it
> converts nine days of other people's lead time into schedule slack.

> **Filename note.** This file is still named `2026-08-06-launch-gate-outreach.md`. Six documents link
> to it, two of which are **closed** day records that [PLAN.md §4](../PLAN.md#4-status-and-daily-record-contract)
> treats as historical evidence. Renaming the file to match the new send date would edit those records
> to make the schedule read better, which is the specific thing that rule forbids. The stale filename
> is the cheaper inaccuracy and it is recorded here rather than left to be discovered.

These messages consolidate the external requests that can block the Gradex launch. They ask
recipients to verify unresolved policy/provider questions; they do not present an unapproved legal,
accounting, payment, or infrastructure position as settled.

## What changed on 2026-07-28

1. **Every internal date was rebased.** The messages previously asked recipients to acknowledge on
   6 August and reply by 8–9 August. Sent on 28 July, those dates would have handed back the entire
   nine days of lead time the early send exists to capture — a recipient reading "please acknowledge
   in nine days" files the mail and forgets it. The pack's own rule permits the change: *adjust
   response dates only if the replacement date still protects the blocking point.* Every new date is
   earlier, so every blocking point is better protected, and each one is now tied to the slice that
   consumes it rather than to a round number.
2. **Message 4 is no longer a message.** It was addressed to an "operations owner" who is the founder.
   Six research tasks addressed to yourself are not outreach, and tracking them as an awaited reply
   made six gates look like they were blocked on somebody else when nobody was coming. They are now
   [§Founder Operational Decisions](#founder-operational-decisions) with owners and dates.

## Before Sending

1. Replace every bracketed placeholder.
2. Keep credentials, tokens, customer data, and unpublished commercial documents out of ordinary
   email. Use an approved secure channel when a recipient needs sensitive material.
3. Adjust response dates only if the replacement date still protects the blocking point in
   [LAUNCH_GATES.md](../../LAUNCH_GATES.md).
4. After sending, update the tracking table and the corresponding launch-gate evidence.

## Placeholder Key

| Placeholder | Replace with |
|---|---|
| `[FOUNDER_NAME]` | Founder/sender name |
| `[COMPANY_LEGAL_NAME]` | Registered entity name, or "entity formation in progress" if accurate |
| `[COUNSEL_NAME]` | Kuwaiti counsel contact |
| `[ACCOUNTING_CONTACT_NAME]` | Accountant/finance adviser contact |
| `[TAP_CONTACT_NAME]` | Tap account manager, or the Tap merchant-onboarding intake channel |
| `[SENDER_EMAIL]` | Sender's business email |
| `[SENDER_PHONE]` | Sender's business phone, if appropriate |

## Tracking

**Do not change a message to `SENT` until it has actually left the sender's account.** This table is
read directly by the 12 August launch-gate audit and by the go/no-go evaluation in
[PLAN.md §8](../PLAN.md#8-public-launch-criteria). A `SENT` row with no message behind it is not a
placeholder — it is false evidence in the one register those decisions are made from.

| Request | Gates | Recipient | Status | Sent at | Ack due | Substantive reply due | Follow-up if silent |
|---|---|---|---|---|---|---|---|
| Legal and policy | LG-002, LG-004–006, LG-011, LG-020 | `[COUNSEL_NAME]` | DRAFT | — | 2026-07-30 | 2026-08-02 initial, 2026-08-06 final | 2026-07-31 |
| Finance and accounting | LG-001, LG-007, LG-016–017 | `[ACCOUNTING_CONTACT_NAME]` | DRAFT | — | 2026-07-30 | 2026-08-02 initial, 2026-08-06 final | 2026-07-31 |
| Tap activation and technical contract | LG-007–010, LG-017 | `[TAP_CONTACT_NAME]` | DRAFT | — | 2026-07-30 | 2026-08-02 docs and test vectors, 2026-08-07 activation | 2026-07-31 |

Message 4's gates — LG-013–015, LG-018–019, LG-021 — are tracked in
[§Founder Operational Decisions](#founder-operational-decisions) instead. They are founder work, not
awaited replies.

### Why these dates

| New date | Protects |
|---|---|
| Ack 2026-07-30 | Two days. A silent recipient is detected on 31 July with 15 days left, not on 7 August with 8 |
| Tap vectors 2026-08-02 | S6 checkout starts D12 (8 Aug) and S7 callback verification D13 (10 Aug). Webhook signature vectors are an **input** to S7, not a review artifact |
| Initial legal/accounting 2026-08-02 | Leaves 10 days before the 12 August legal gate instead of 4 |
| Tap activation 2026-08-07 | `LG-007`/`LG-008`/`LG-010` are required by 10 August, and production activation has a lead time nobody here controls |
| Final legal/accounting 2026-08-06 | `LG-012` launch prices are due 11 August and depend on the approved revenue share (`LG-001`) |

## Message 1 — Kuwaiti Counsel

**To:** `[COUNSEL_NAME]`

**Subject:** Gradex legal and policy review needed for 15 August launch

Hello `[COUNSEL_NAME]`,

I am preparing Gradex, a Kuwait-focused online learning platform, for a readiness-gated public
launch on 15 August 2026 under `[COMPANY_LEGAL_NAME]`.

Please provide written guidance on the following launch items:

1. The permitted and required eligibility, process, exceptions, timing, disclosures, and
   entitlement effect for full and partial refunds on streamed digital Courses.
2. The applicability of Kuwait's privacy requirements to Gradex, including controller/provider
   responsibilities, cross-border processing, data-subject requests, notice wording, and the legal
   inputs needed for a retention schedule.
3. The operative date, registration requirements, and required disclosures under the Digital
   Commerce Law and any applicable Gazette publication or implementing regulation.
4. Whether Gradex requires an education-sector license or registration, or written confirmation
   that none applies to the proposed operating model.
5. Review requirements for bilingual Privacy, Terms, Refund, and checkout disclosures, including
   how accepted policy versions should be evidenced.
6. An Instructor agreement/content-rights process covering ownership and permissions, revenue
   share, payout/tax responsibilities, warranties, takedown/moderation, termination, and Course
   asset handoff.

I am sending this earlier than the launch schedule requires so that lead time is yours rather than
mine. Please acknowledge receipt by **30 July**, identify any missing information immediately, and
provide initial risk guidance by **2 August**. I am targeting final approved policies and agreements
by **6 August** so they can be validated before production release.

If items 3 or 4 turn out to be a blocking registration with its own lead time, please tell me that
first and separately — it changes the launch date rather than the launch content.

Please cite the official authority or contract source behind each conclusion and distinguish a
confirmed requirement from a recommendation.

Regards,

`[FOUNDER_NAME]` | `[SENDER_EMAIL]` | `[SENDER_PHONE]`

## Message 2 — Accounting and Finance

**To:** `[ACCOUNTING_CONTACT_NAME]`

**Subject:** Gradex launch accounting, payout, and commerce decisions

Hello `[ACCOUNTING_CONTACT_NAME]`,

Gradex is targeting a readiness-gated public launch on 15 August 2026. I need written accounting
and finance decisions for the following:

1. Approve one platform-wide Instructor revenue-share percentage and its effective date. The
   system will require versioned configuration and will not assume a default percentage.
2. Confirm the commercial-registration, business-bank, and payment-account prerequisites needed
   before production transactions.
3. Define the tax, payment-fee, invoice, receipt, numbering, KWD rounding, refund-document, and
   financial-record-retention treatment.
4. Confirm how discounts, confirmed refunds, payment fees, late refunds, and chargebacks affect
   net revenue, monthly Instructor statements, payouts, and later-period adjustments.
5. Identify the financial records that must be retained and the required retention period, subject
   to reconciliation with counsel's legal guidance.

Please acknowledge receipt by **30 July**, list any required source documents, and provide initial
decisions by **2 August**. I am targeting final accounting sign-off by **6 August**.

Item 1 is the one with a downstream dependency: launch prices must be entered and approved by
11 August, and they are computed against the approved share. Item 2 may have a lead time I cannot
compress — please flag that immediately if so.

Please separate mandatory accounting/legal treatment from internal operational recommendations.

Regards,

`[FOUNDER_NAME]` | `[SENDER_EMAIL]` | `[SENDER_PHONE]`

## Message 3 — Tap Payments

**To:** `[TAP_CONTACT_NAME]`

**Subject:** Gradex production activation and payment-contract confirmation

Hello `[TAP_CONTACT_NAME]`,

Gradex is a Kuwait-focused platform selling time-limited access to online university Courses and
Sections. We are targeting a readiness-gated public launch on 15 August 2026 and need written
confirmation of the production and technical requirements below:

1. Approval of the Gradex merchant category/use case for digital Course access.
2. Production onboarding prerequisites and the card/KNET payment sources that can be enabled.
3. Full and partial refund support for each enabled payment source, including asynchronous states,
   unsupported cases, reconciliation behavior, and sandbox evidence.
4. The official webhook-authenticity procedure, required headers/signature algorithm, replay
   handling, event identifiers, retry behavior, and test vectors or sandbox events.
5. Chargeback/dispute notifications, evidence requirements, status lifecycle, and reconciliation
   data available to the merchant.
6. The expected activation timeline and any dependency that could prevent production payments by
   14 August.

Please acknowledge receipt by **30 July**. **Item 4 is the most time-critical**: the webhook
signature procedure and test vectors are a direct input to our payment-callback implementation, which
begins **8 August**, so I need those by **2 August** even if the rest follows later. Please confirm
production activation by **7 August**, and identify immediately if it cannot complete by 12 August.

Do not request secrets or credentials by email; please direct us to the approved secure channel for
any account-specific material.

Regards,

`[FOUNDER_NAME]` | `[SENDER_EMAIL]` | `[SENDER_PHONE]`

## Founder Operational Decisions

**Formerly "Message 4 — Operations and Infrastructure."** It was addressed to an operations owner who
is the founder. Six research tasks addressed to yourself are not outreach, and tracking them as an
awaited reply made `LG-013`–`LG-015`, `LG-018`, `LG-019`, and `LG-021` look blocked on a third party
who was never coming. They are scheduled work.

Vendor-facing items still generate real outreach — to a vendor's sales or support intake — but the
decision and the evidence are the founder's.

| # | Decision | Gates | Needed by | Why that date |
|---|---|---|---|---|
| 1 | Name the support/community owner; document the support route, moderation rules, escalation path, response expectation, and active Discord/Telegram links | LG-013 | **2026-08-12** | S10-reduced support route ships D15 |
| 2 | Shortlist and select a malware-scanning service: supported types/sizes, quarantine, fail-closed behavior, alerts, test evidence | LG-014 | **shortlist 2026-08-01, decision 2026-08-03** | S4 part 1 builds the scanner adapter on D7 (3 Aug). The adapter can be built against an interface; the provider cannot be chosen after the fact |
| 3 | Select transactional email delivery: processing boundary, sender domain, SPF/DKIM/DMARC, templates, bounce/suppression, rate limits, monitoring, deliverability evidence | LG-018 | **2026-08-05** | Required by 7 Aug. Unresolved degrades verification and reset to an Admin-issued manual path that does not scale past a soft launch |
| 4 | Supply hosting budget, launch load/storage/egress, availability target, RPO/RTO, secrets approach, monitoring/alerting, incident ownership, backup/restore evidence, load-test plan | LG-019 | **2026-08-01** | S12 provisions staging on D11 (7 Aug). These are its inputs, and the table below is where they go |
| 5 | Schedule the platform-owned UI/player accessibility audit and hosted-checkout assessment, without claiming product-level conformance while captions/transcripts stay outside MVP | LG-015 | **book by 2026-08-05** | Runs inside S13 on D16 (13 Aug); auditors need booking lead time |
| 6 | Shortlist a privacy-preserving compromised-password screening provider or licensed offline dataset: source/license, no-plaintext query/storage boundary, fail-closed outage behavior, deterministic test vectors, latency/error monitoring, staging validation | LG-021 | **2026-08-05** | Required by 7 Aug. It **fails closed at credential creation**, so an unresolved outcome blocks registration outright. A licensed offline dataset is the fallback |

Item 2 and item 6 are the two that can stop a working build rather than merely degrade it.

### Founder Architecture Inputs

Needed by **2026-08-01**, not 6 August: S12 provisions staging on D11 (7 August) and these are its
inputs. Until they exist, system design must label its operating-envelope values provisional, keep
them configurable, and replace them before production architecture sign-off.

| Input | Value |
|---|---|
| Monthly infrastructure budget | `[MONTHLY_BUDGET_KWD]` |
| Expected registered Students at launch | `[EXPECTED_REGISTERED_STUDENTS]` |
| Expected peak concurrent viewers | `[EXPECTED_CONCURRENT_VIEWERS]` |
| Expected initial Course count and video hours | `[EXPECTED_COURSES_AND_VIDEO_HOURS]` |
| Availability target | `[AVAILABILITY_TARGET]` |
| Maximum acceptable data loss (RPO) | `[RPO_TARGET]` |
| Maximum acceptable recovery time (RTO) | `[RTO_TARGET]` |
| Primary support hours/timezone | `[SUPPORT_HOURS_AND_TIMEZONE]` |
| Preferred hosting region/provider constraints | `[HOSTING_CONSTRAINTS]` |
