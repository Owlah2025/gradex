# Product Requirements Document (PRD)

> Version: 1.0
> Status: Approved baseline for system design
> Last Updated: 2026-07-23

This document owns Gradex product scope and acceptance criteria. Product decisions are recorded
in [DECISIONS.md](DECISIONS.md), invariants in [BUSINESS_RULES.md](BUSINESS_RULES.md), canonical
entities and states in [DOMAIN_MODEL.md](DOMAIN_MODEL.md), and unresolved production dependencies
in [LAUNCH_GATES.md](LAUNCH_GATES.md).

---

# 1. Introduction

## Product Name

Gradex

## Purpose

Gradex is a responsive bilingual course platform for Gulf university students, initially focused
on Kuwait. It combines structured video lessons, protected resources, hands-on lab materials,
external community, and lightweight live office hours. It also gives subject-matter experts an
Admin-governed way to publish courses and earn revenue from their expertise.

---

# 2. Goals

## Business Goals

- Launch with 8–12 approved Courses and reach 100–500 paid Students in the first six months.
- Build sustainable revenue through full-Course and single-Section purchases.
- Expand toward 100+ Courses and 50,000–200,000 registered users within three years.
- Validate Student outcomes, Instructor supply, and Kuwait-first operations before adding
  bundles, BNPL, native applications, or automated marketplace settlement.

## User Goals

- **Students:** understand university coursework, practise with real materials, receive
  follow-up after purchase, and learn on phones, tablets, laptops, or desktops at a fair price.
- **Instructors:** publish high-quality Courses, reach Students, track learning outcomes, and
  receive transparent monthly payout statements without operating payment infrastructure.
- **Admins:** control access, pricing, publishing, moderation, refunds, coupons, and payout
  operations with complete auditability.

---

# 3. Target Users

## Student

Gulf university Students, initially in Kuwait, seeking structured academic support and practical
skills. Public self-registration is limited to this role.

## Instructor

Subject-matter experts invited by an Admin to create and maintain their own Course content.
Instructors do not control catalog prices, refunds, or payouts.

## Administrator

Gradex operators responsible for staff provisioning, pricing, publishing, user management,
payment/refund oversight, content moderation, coupons, and Instructor payouts.

---

# 4. Scope

This register is the authoritative release boundary. A downstream document cannot move an item
between columns without updating this section and [DECISIONS.md](DECISIONS.md).

## MVP

### Identity and access

- Student-only public registration with mandatory email verification.
- Email/password login, rotating refresh sessions, password reset, and logout.
- Admin invitation and initial-password setup for Instructors and additional Admins.
- One out-of-band bootstrap Admin created during secure deployment.
- Immediate enforcement of account suspension across protected actions.

### Catalog, learning, and content

- Public Course catalog and Course detail pages.
- Course classification on Major, Subject, and Study Year, with Admin-managed vocabulary, catalog
  filtering, and bilingual Arabic-normalized search.
- Canonical `Course → Section → Lesson` structure; “Chapter” may be a UI label for Section only.
- Adaptive HLS video playback, resume position, and per-Lesson completion tracking.
- Entitlement-protected Lesson resources and downloadable lab materials.
- Optional separate public Course preview asset.
- External Discord/Telegram Course community link.
- Student content reporting and Admin resolution.

### Instructor and Admin operations

- Instructor-owned Course/Section/Lesson builder, video/resource/lab upload, submission, and
  revision workflow.
- Instructor Course analytics and a minimal Course-scoped Student roster; price visibility is
  read-only.
- Admin-only Course/Section pricing with audit history.
- Admin Course review, publish, delist/relist, retire, archive, emergency access suspension, and
  reported-content moderation.
- Admin user provisioning, suspension, coupons, refunds, revenue reporting, and payout records.

### Commerce and communication

- One full Course or one Section per order.
- Tap-hosted card/KNET checkout; webhook/API confirmation controls successful payment.
- Admin coupons: percentage/fixed, optional Course/Section scope, global cap, one consuming
  redemption per Student, and zero-value grants.
- Full and partial refunds, subject to gateway capability and the counsel-approved policy.
- Course-configured semester expiry disclosed at checkout and snapshotted onto the Order and
  Entitlement, with audited Admin extension/shortening of individual Entitlements.
- Manual monthly Instructor payout process with system-recorded accounting and emailed statement.
- Course-scoped one-off live office hours using an entitlement-protected external meeting link.
- Fixed transactional in-app/email notifications.

### Experience and compliance baseline

- Responsive website supporting the complete Student experience on phones, tablets/iPads,
  laptops, and desktops.
- Arabic and English UI for every role; Arabic default, persistent preference, full RTL/LTR.
- Platform-owned UI/player controls target WCAG 2.2 AA within the boundary in §6.
- Bilingual Privacy Notice, Terms, Refund Policy, and checkout disclosures before production.

## Fast-Follow

- Bundle browsing, pricing, checkout, and cross-Course entitlement.
- Deema BNPL after written digital-goods approval and a separate entitlement/payment-state review.
- Captions/subtitles/transcripts and a complete product-level accessibility claim.
- Instructor earnings dashboard and automated settlement.
- MFA and social login.
- Lifecycle/marketing notifications with consent, preferences, and unsubscribe management.

## Out of MVP

- Native iOS/Android applications.
- In-platform conferencing/live streaming, recordings, attendance, recurring office hours,
  RSVP/capacity, timed reminders, and calendar integration.
- In-platform community/forum.
- Sandboxed code execution for labs.
- Course certificates.
- Reviews, ratings, recommendation engines, and fabricated testimonials.
- Marketing/broadcast, SMS, WhatsApp, and push notifications.
- Instructor-controlled prices, coupons, refunds, or withdrawals.

---

# 5. Functional Requirements

## Authentication and Accounts

- Public signup creates only a `PENDING_VERIFICATION` Student account.
- Email verification, invitations, and password-reset tokens are expiring, single-use, stored
  securely, and rate-limited.
- Verification must succeed before Student sign-in. Changing an email requires verifying the
  new address.
- Existing Admins invite Instructors/Admins; public privileged-role registration does not exist.
  Sending an invitation does not create an Account; acceptance creates it with the assigned role.
  An address already attached to an Account cannot be invited or converted to another role.
- Every Account has exactly one role assigned at creation and immutable during MVP. Students alone
  may purchase, receive Entitlements, and record Progress. Instructors author assigned content
  without Student learning capability; Admins use the separate audited preview path. A person
  needing another capability uses a separate Account with another normalized email.
- The bootstrap Admin has no credential in the repository and must change the initial password.
- Passwords accept 15–128 Unicode characters, reject common/compromised values, use Argon2id,
  and do not require character-class composition or scheduled rotation.
- Authentication, signup, verification, and recovery responses do not reveal account existence.
- Suspension immediately blocks all protected actions, including actions from existing sessions.
- Every Account has a self-chosen, non-unique display name of 2–50 characters in either script,
  defaulting to the name given at registration or invitation acceptance and editable by its owner.
  It rejects URLs, control characters, and markup, is never required to carry legal identity, and is
  the only identity field an Instructor roster exposes. An Admin may reset an abusive value through
  the audited moderation path.

## Student Features

- Browse published Courses and filter them by Major, Subject, and Study Year.
- Search published Courses by title, description, Instructor display name, and taxonomy label/code.
  Search matches Arabic and English at once regardless of interface language, ignores diacritics and
  alef/taa-marbuta/digit variants, ranks by relevance only, and never returns delisted Courses or
  protected Lesson content.
- Evaluate a Course using its authored details and optional public preview.
- Purchase one Course or one Section and view order/payment/refund history.
- Apply one coupon at checkout and see the original price, discount, and final KWD total.
- Watch entitled Lessons, resume playback, track completion, and retain progress after expiry.
- Download entitled resources and labs through short-lived signed access.
- View/join entitled upcoming office hours and receive transactional notifications.
- Report entitled content using a fixed reason plus an optional/required explanation.
- Manage profile, display name, and persistent interface language.

## Instructor Features

- Create and edit only owned Course, Section, and Lesson content.
- Classify an owned Course by selecting one Major, one Subject, and one Study Year from the
  Admin-managed vocabulary; Instructors cannot create, rename, or retire vocabulary terms.
- Upload videos through the existing processing pipeline; upload resources and lab materials as
  distinct categories.
- Upload at most one separate public Course preview and confirm publication permission.
- Submit a Course/revision, view review status and Admin change requests, and resubmit.
- View own Course analytics, a roster limited to Student-chosen display name/alias and
  Course-scoped enrollment/progress, and catalog prices read-only; no direct Student PII.
- Schedule/reschedule/cancel one-off office hours for an owned Published Course.
- Receive Course-review, video-processing, change-request, and office-hours notifications.

## Admin Features

- Invite Instructors/additional Admins and suspend/reactivate users.
- Create, rename, retire, and delete Major/Subject taxonomy terms with bilingual labels and audit
  history, and override any Course's classification.
- Set/change Course and Section prices with reason and audit history.
- Review Course content, publish/request changes, delist/relist, retire, archive, and invoke or
  resolve emergency access suspension when authorized.
- Preview Course media through a separate audited authorization path.
- Manage coupons and view historical redemption data.
- Process full/partial refund requests and monitor gateway status.
- Review content reports, dismiss/request changes/delist/retire/access-suspend, and record resolution.
- Reconcile monthly Instructor payouts, record adjustments/transfer reference, and email statement.
- Cancel any office-hours session for moderation; Admins do not create platform-wide sessions.

## Course and Content Management

- Course hierarchy and ordering use `Course → Section → Lesson` only.
- Course state follows the lifecycle in [DOMAIN_MODEL.md](DOMAIN_MODEL.md): Draft, Pending Review,
  Changes Requested, Published, Delisted, and Archived. Retirement and emergency access suspension
  are separate.
- A Course cannot enter review without at least one Section, one Lesson, required READY video
  content, and an assigned Major, Subject, and Study Year; validation identifies missing
  requirements.
- Each Course carries exactly one value per classification dimension. A retired term stays on the
  Courses already using it until an Admin reassigns them, and a term still referenced by a Course
  cannot be deleted.
- Published content changes use a pending revision and never silently mutate the approved version.
- Protected files remain inaccessible without entitlement. A public preview is a distinct asset.
- Uploads are type/size validated and quarantined; public/downloadable assets require a successful
  malware scan before availability.

## Payments, Orders, Coupons, and Refunds

- Monetary values are integer fils and displayed as KWD with three decimal places.
- Percentage discounts round to the nearest fil and are clamped to `[0, subtotal]`.
- The server creates an Order and stable Payment Attempt reference before Tap redirect.
- Tap webhook/API verification—not browser redirect—controls capture and entitlement grant.
- Payment/refund callbacks and transactional state changes are signature-verified and idempotent.
- Orders explicitly distinguish Pending Payment, Paid, Free Granted, Cancelled, Expired,
  Reconciliation Required, Partially Refunded, and Refunded, with separate creation, acceptance,
  payment-deadline, completion, expiry, and cancellation timestamps.
- One active `CREATED`/`PENDING`/`UNKNOWN` Payment Attempt exists per Order. Success means verified
  capture; timeout remains reconcilable; provider occurrence—not arrival—controls deadline.
- A zero-value coupon order creates a real Order and entitlement without a gateway call.
- Every ordinary Entitlement originates from one paid or zero-value Coupon Order. Admins may adjust
  an existing Entitlement's expiry but cannot create access through a separate manual-grant path.
- A Course Entitlement blocks repurchase of that Course or any contained Section. A Section
  Entitlement blocks that Section only; another Section or the full Course remains purchasable at
  current catalog price, with no MVP upgrade credit/proration/refund/expiry combination. Both
  Entitlements remain independent after a Section-to-Course purchase.
- A Course must have a future Admin-configured access-expiry instant before checkout. Sections have
  no independent expiry override. The Order preserves the disclosed instant; runtime access uses
  the Entitlement's current effective expiry, which an elevated Admin may extend or shorten through
  an audited adjustment.
- Paid Coupon Order acceptance reserves exact Coupon capacity until its payment deadline. Timely
  capture consumes the reservation; cancellation/expiry releases it unused. Zero-value Orders
  consume immediately. Full Refund releases Student eligibility but never restores historical
  global quota; partial Refund does not release it.
- Only Admins initiate refunds. One or more refund amounts may not exceed the remaining captured
  balance and require a reason.
- Partial Refund keeps access active; cumulative full Refund revokes only that Order's Entitlement
  after confirmed gateway success. Unsupported/failed requests have no access effect.
- Checkout records the accepted bilingual refund-policy version.

## Payouts

- One platform-wide Instructor revenue-share percentage is configurable but has no default.
- Share uses net collected revenue after coupons, confirmed refunds, and gateway/payment fees.
- Each paid Order snapshots the effective share version and owning Instructor. Course reassignment
  affects later Orders only; Refund/chargeback adjustments remain with the original earning.
- Earnings, fees, Refunds, chargebacks, payout adjustments, carry-forwards, and corrections are
  immutable source-linked ledger entries; corrections use compensating entries.
- One monthly Statement per Instructor/currency/period freezes its items and totals on approval.
- Payment initiation snapshots the destination; `PAID` requires verified full-payment evidence.
  Partial Statement payments and negative transfers are not supported; negative balances carry.
- Late refunds/chargebacks adjust a later Statement without rewriting an approved/paid one.
- Instructor receives the statement by email; no in-app payout dashboard/withdrawal exists.

## Live Office Hours

- A session belongs to one Published Course and contains title, description, UTC start/end, and
  an external link.
- Only the owning Instructor creates/reschedules/cancels; Admin may cancel for moderation.
- An uncancelled Session is derived as Upcoming, Live, or Ended from its scheduled instants; Ended
  does not imply delivery or attendance.
- The join link is returned only during the authorized Live window. Existing entitled Students may
  retain historical Session/material access after delisting/retirement/archival.
- Cancellation blocks joining but preserves Session, notification, delivery, and Audit history.
- Times display in the user's local timezone/language, defaulting to Kuwait time.

## Content Reporting

- Entitled Students can report a Course, Lesson, video, resource, lab material, or Office-Hours
  Session. The stable target and exact visible revision/version are preserved.
- Reasons are broken/unavailable, inaccurate, inappropriate, suspected copyright violation, or
  other; “other” requires an explanation.
- Reports are rate-limited and never auto-hide content. Automated findings also cannot perform
  moderation actions; Media quarantine and emergency security suspension remain separate.
- Admin resolution, exact resulting action, and any Instructor notice are immutably audited.

## Notifications

- Required in-app + email events: purchase receipt, refund/reconciliation status, password/security,
  invitation, Course approval/change request, office-hours cancellation/material reschedule, Admin
  Entitlement expiry adjustment, and emergency Course access suspension/restoration.
- New office-hours sessions and new Instructor Course/revision submissions are in-app and may also
  use email when operationally appropriate.
- Video-processing completion is an Instructor event.
- Notification Events relationally snapshot exact Account/channel recipients at source-event time.
  Delivery attempts are idempotent; email failure never changes the source transaction or durable
  in-app record.
- Mandatory transactional/security events cannot be disabled. Operational events follow fixed
  channel policy; optional marketing/preferences remain outside MVP.

---

# 6. Non-Functional Requirements

## Performance

- Public catalog/Course pages target p95 LCP under 2.5 seconds on representative Kuwait 4G.
- Entitled video targets p95 time-to-first-frame under 3 seconds when the selected media delivery
  path is healthy.
- Read API endpoints target p95 under 300 ms and transactional writes under 800 ms, excluding
  third-party gateway latency.
- Progress-write failure must not interrupt playback.

## Security

- Deny by default; role, ownership, status, and entitlement authorization is server-side.
- Secrets never enter the repository; credentials/tokens/personal data are excluded from logs.
- Sensitive data is encrypted in transit and at rest according to classification.
- Gradex never collects, transmits, or stores full card/PAN data; payment entry is Tap-hosted.
- Webhooks are signature-verified, replay-safe, and idempotent.
- Auth, verification, reset, checkout, reporting, signed-URL, and download endpoints are
  rate-limited based on abuse risk.
- Privileged identity, pricing, publishing, preview, refund, payout, and moderation actions are
  auditable.

## Reliability

- Core catalog, purchase, and playback paths target 99.5% monthly availability for MVP.
- Payment and entitlement writes are transactional and safely recoverable after duplicate,
  delayed, or out-of-order callbacks.
- Backup/restore procedures must be automated and restore-tested before production; system design
  selects RPO/RTO consistent with the business target and operating budget.
- External notification failure is isolated from the business transaction.

## Responsive Web and Localization

- Complete Student capability works on supported phones, tablets/iPads, laptops, and desktops.
- Instructor/Admin portals remain responsive; complex operational workflows may be desktop/tablet
  optimized.
- Arabic is the initial default and English is available everywhere; selection persists.
- UI direction, navigation, icons, tables, forms, validation, dates, and mixed-language text work
  correctly in RTL and LTR.
- Instructor-authored Course content is not automatically translated.

## Accessibility

- Platform-owned UI/player controls target WCAG 2.2 Level AA: keyboard operation, visible focus,
  accessible authentication, semantic structure, labels, announced errors, contrast, and target
  sizes.
- Hosted checkout accessibility is evaluated and documented but not claimed as Gradex-controlled.
- Captions/transcripts are outside MVP. Gradex therefore does not claim complete learning-product
  WCAG conformance until that fast-follow gap is closed.

## Privacy

- Collect only data required for identity, learning, commerce, support, security, and law.
- Instructor rosters expose only the minimum Course-scoped display identity and learning fields;
  direct Student account/contact/payment PII remains Admin-only.
- Record the version of any accepted legal/privacy/refund text.
- Support access, correction, deactivation, and deletion requests, subject to documented retention.
- Anonymize personal data where practical while preserving necessary financial/audit referential
  integrity.
- Exact retention periods require counsel/accounting approval before production.

---

# 7. Constraints

## Budget and Team

- Bootstrapped/self-funded.
- One developer owns the full stack; Tohamy owns founder/logistics work and Mokhtar owns
  marketing/social/advertising.
- Prefer a modular monolith and avoid new infrastructure without a current MVP requirement.

## Timeline

- The previously stated target launch is 2026-08-15. Readiness, external approval, security,
  testing, and legal gates take precedence over presenting that date as guaranteed.

## Technology

- Current repository stack: Next.js/React/TypeScript/Tailwind, Go/Gin, PostgreSQL, Redis,
  S3-compatible storage, and HLS processing/playback. A production CDN and real token/session
  authentication are system-design/implementation work; the current backend auth seam is fake
  development-only.
- This PRD defines outcomes, not final system-design mechanisms.

## Legal and External Dependencies

This is a product-readiness summary, not legal advice. Production requires resolution of
[LAUNCH_GATES.md](LAUNCH_GATES.md).

- Use the correct reference: [CITRA Decision No. 26 of 2024 issuing the Data Privacy Protection
  Regulation](https://www.citra.gov.kw/sites/ar/Pages/DecisionsDetails.aspx?id=6).
- Kuwait's official announcement states that Digital Commerce Law No. 10 of 2026 applies six
  months after Gazette publication and describes registration/disclosure/consumer obligations;
  counsel must confirm operative dates and applicability.
  [Official announcement](https://e.gov.kw/sites/KGOArabic/Pages/ApplicationPages/NewsDetail.aspx?nid=64409149)
- Counsel must approve refund eligibility for streamed/downloaded education, privacy scope,
  retention, consumer disclosures, and any education-sector licensing requirement.
- Tap production onboarding, digital-course merchant approval, payment methods, webhook contract,
  and partial-refund support must be verified in production configuration.

---

# 8. Assumptions

- External Discord/Telegram can provide initial community/follow-up if an owner and moderation
  process are assigned before launch.
- Students can use downloadable labs without a managed execution environment; setup friction must
  be tested with pilot Students.
- Instructor supply and Course production can proceed in parallel with platform delivery.
- One global revenue-share formula is sufficient for MVP; its numeric percentage remains a
  pre-launch commercial decision.
- Tap can activate an appropriate digital-course merchant account; this is an external launch
  gate, not a system-design assumption to hard-code.

---

# 9. Risks

## Payment Provider and Reconciliation

**Impact:** provider approval, outages, unsupported refund methods, or callback drift can block
revenue/access correctness.

**Mitigation:** hosted Tap integration behind an internal boundary, signature verification,
idempotent states, reconciliation, and production onboarding verification before launch.

## External Community Ownership

**Impact:** an unstaffed link-out undermines the follow-up promise.

**Mitigation:** assign moderation/response ownership and pilot expectations before launch.

## Downloadable Content Leakage

**Impact:** protected labs/resources can be redistributed.

**Mitigation:** entitlement checks, short-lived URLs, download logging/rate limits, lab buyer tag,
and enforceable Terms. MVP does not claim DRM.

## Accessibility Gap in Course Media

**Impact:** captions/transcripts are outside MVP, so some Students cannot fully access media and
Gradex cannot claim complete WCAG conformance.

**Mitigation:** keep claims scoped and prioritize manual/automated caption support fast-follow.

## Solo-Developer and External-Lead-Time Risk

**Impact:** gateway approval, legal review, security validation, video load testing, and end-to-end
QA do not compress at the same rate as code generation.

**Mitigation:** keep the authoritative MVP boundary fixed, track launch gates separately, and move
the date rather than silently pulling fast-follow features into MVP or skipping quality gates.

---

# 10. Success Metrics

Business and product outcome targets are owned by [PROJECT_VISION.md §11](PROJECT_VISION.md).
Metrics that require an out-of-MVP feature (for example public ratings) must not be treated as
instrumentable MVP metrics until that feature is approved.

---

# 11. Acceptance Criteria

Each criterion names its governing business rules and primary verification method.

## Authentication

- Given a new Student submits a valid display name/email/password, when signup is accepted, then a
  `PENDING_VERIFICATION` account is created with that non-unique display name, no session is
  issued, and a single-use verification link is sent with a generic anti-enumeration response.
  *(BR-001/002/008/105; integration + E2E)*
- Given a valid unused verification link, when it is consumed, then the Student becomes Active and
  can sign in; expired/reused links fail safely and resend is rate-limited. *(BR-008; integration)*
- Given an Admin invites an Instructor/Admin, when the recipient consumes the invitation, then the
  address is verified, a valid display name and initial password are established, and the assigned
  role—not a public choice—is activated. *(BR-009/105; integration + E2E)*
- Given a bad login/recovery/signup probe, when processed, then the response does not reveal whether
  the address exists. *(BR-001/003; security integration)*
- Given an Admin suspends an account, when any existing/new session attempts a protected action,
  then access is denied immediately and the suspension is audited. *(BR-007; E2E)*
- Given a Student sets a display name, when an Instructor opens the roster for a Course that Student
  is enrolled in, then only that display name and Course-scoped learning fields appear, two Students
  may hold the same display name without conflict, and an Admin reset is audited. *(BR-064/101/105;
  integration + E2E)*

## Catalog, Checkout, and Entitlement

- Given a Student opens a published Course, then full-Course and individually priced Section
  purchase options use the same Section entities shown in its outline; no Chapter entity exists.
  *(BR-010/021; E2E)*
- Given the catalog, when the Student filters by Major, Subject, and Study Year, then only published
  Courses carrying those exact values are returned, and a delisted or archived Course never
  appears in any filter combination. *(BR-157/161; integration + E2E)*
- Given an Arabic query typed with different hamza forms, diacritics, or Arabic-Indic digits, when
  it is searched under either interface language, then matching Courses are returned from both
  Arabic and English fields, ranked by relevance, and no Lesson title or protected file is exposed.
  *(BR-161/162; integration)*
- Given an Instructor edits an owned Course, then they may only select existing taxonomy terms;
  attempts to create, rename, or retire a term are denied server-side, and every Admin vocabulary
  change is audited. *(BR-158; integration)*
- Given a Course missing any classification dimension, then submission for review is blocked and
  names the missing dimension; a retired term stays on already-assigned Courses and a referenced
  term cannot be deleted. *(BR-159/160; integration + E2E)*
- Given a valid paid checkout, when Tap confirms capture through a verified callback/API result,
  then the Order becomes Paid and one scoped Entitlement with the disclosed Course-configured
  expiry is granted exactly once. *(BR-020/021/025/031/033; integration + E2E)*
- Given a redirect without confirmed capture or a failed/ambiguous/late Attempt, then no Entitlement
  is granted automatically and preserved evidence drives safe reconciliation. *(BR-020/022/034)*
- Given a valid coupon, when preview/order creation occurs, then integer-fils discount/total are
  snapshotted and paid capacity is reserved until the Order deadline; zero total consumes and grants
  once without Tap. *(BR-124–129; unit + integration)*
- Given the Student has a consuming redemption, a second use is denied; after cumulative full
  refund it is eligible again while history remains. *(BR-128/129/131; integration)*
- Given an active entitlement already covers the chosen scope, checkout blocks repurchase. A
  Section-entitled Student may buy another Section or the full Course without automatic credit,
  expiry combination, or modification of the original Section Entitlement; after expiry the
  standard purchase path is available. *(BR-024/025; E2E)*

## Refunds and Payouts

- Given an Admin requests a supported partial refund within the remaining balance, when the gateway
  confirms success, then the refunded balance/revenue/payout adjustment update and entitlement
  remains active. *(BR-040–047; integration + E2E)*
- Given cumulative confirmed refunds equal captured amount, then entitlement is revoked; a failed
  or pending refund does not revoke it. *(BR-041/046/047; integration)*
- Given a monthly payout run, then immutable source-linked ledger entries calculate one Statement
  per Instructor/currency/period; approval freezes items/totals, and only verified full-transfer
  evidence marks it Paid. Later adjustments carry forward without rewriting it. *(BR-073/074;
  integration + E2E)*

## Course Building and Moderation

- Given an Instructor edits an owned Draft Course, then Course/Section/Lesson ordering persists;
  editing another Instructor's Course or any price is denied server-side. *(BR-010/019/060; integration)*
- Given a Course does not meet readiness, submission is blocked with specific missing items; when
  ready, submission moves to Pending Review and locks concurrent editing. *(BR-012/013/016/070; E2E)*
- Given an Admin requests changes, the reason is required and visible; resubmission returns to
  review. Approval publishes and notifies. *(BR-071/072/090/122; E2E)*
- Given a Published Course revision, the live approved version remains unchanged until Admin
  approval applies the revision atomically. *(BR-017/090; integration)*
- Given an Admin delists a Course, it leaves catalog/checkout but qualifying existing access
  continues. Given an elevated Admin invokes emergency access suspension with a constrained reason,
  existing Student access stops without mutating Entitlements/Progress and Audit/notifications are
  recorded; restoration re-enables otherwise-valid access. *(BR-090; integration + E2E)*
- Given an Admin changes a Course/Section price, the change is audited and affects future Orders
  only; existing transaction snapshots remain unchanged. *(BR-019; integration)*

## Learning, Preview, Reporting, and Office Hours

- Given a Student requests playback/download or writes Progress, runtime access and qualifying graph
  reachability are required. Completion uses the trusted exact Asset Version duration, ignores
  client percentages, and never regresses across Video replacement. *(BR-023/050–053/116)*
- Given an Instructor uploads a public preview, it remains unavailable until validation,
  quarantine, scan, and permission confirmation succeed; protected Lesson files remain private.
  *(BR-104/143/144; integration)*
- Given an entitled Student reports content, it enters the Admin queue without auto-hiding; Admin
  resolution and any content/account action are audited. *(BR-145/146; E2E)*
- Given an Instructor creates Course office hours, only active Course/Section-entitled Students see
  the join link; an unauthorized/public request never receives it. *(BR-134–136; security E2E)*
- Given a session is materially rescheduled/cancelled, deduplicated notifications are recorded and
  email failure does not undo the schedule change. *(BR-120/122/140; integration)*

## Responsive, Bilingual, and Accessible Experience

- Given each core Student journey, it completes on representative phone, tablet/iPad, laptop, and
  desktop viewports without missing functionality. *(BR-147; responsive E2E)*
- Given Arabic/English selection, direction/layout/forms/tables/date display switch correctly and
  the preference persists without translating Course-authored content. *(BR-149/150; E2E + visual)*
- Given platform-owned UI, automated and manual accessibility checks cover WCAG 2.2 AA within the
  scoped boundary; no complete-product claim is published while captions remain absent.
  *(BR-151; automated accessibility + manual keyboard/screen-reader review)*

---

# 12. Launch Gates

Unresolved production decisions and external dependencies are maintained in
[LAUNCH_GATES.md](LAUNCH_GATES.md), with owner, evidence, deadline, and blocking point. They do not
block system design unless that document explicitly says otherwise; they do block production when
marked required-before-launch.
