# Product Requirements Document (PRD)

> Version: 1.1
> Status: Approved baseline for system design
> Last Updated: 2026-07-28

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

- Launch with 8–12 approved Courses and reach 100–500 Students with granted access in the first six
  months. Payment is collected outside the platform and confirmed by an Admin before access is
  granted ([D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)).
- Build sustainable revenue through full-Course access, collected as External Payment at launch and
  through in-platform checkout once a gateway is integrated.
- Expand toward 100+ Courses and 50,000–200,000 registered users within three years.
- Validate Student outcomes, Instructor supply, and Kuwait-first operations before adding in-platform
  checkout, Section purchases, bundles, BNPL, native applications, or automated marketplace
  settlement.

## User Goals

- **Students:** understand university coursework, practise with real materials, receive
  follow-up after gaining access, and learn on phones, tablets, laptops, or desktops at a fair price.
- **Instructors:** publish high-quality Courses, reach Students, track learning outcomes, and see who
  is enrolled in their own Courses, without operating payment infrastructure.
- **Admins:** control account provisioning, course access granting, pricing, publishing, and
  moderation with complete auditability.

---

# 3. Target Users

## Student

Gulf university Students, initially in Kuwait, seeking structured academic support and practical
skills. Public self-registration is limited to this role.

## Instructor

Subject-matter experts invited by an Admin to create and maintain their own Course content.
Instructors do not control catalog prices or who is granted access to their Courses.

## Administrator

Gradex operators responsible for staff provisioning, pricing, publishing, user management,
confirming External Payment and granting Course access, and content moderation.

---

# 4. Scope

This register is the authoritative release boundary. A downstream document cannot move an item
between columns without updating this section and [DECISIONS.md](DECISIONS.md).

> **MVP has no in-platform payments** ([D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)).
> Payment is External Payment, confirmed by an Admin outside Gradex. Course access is granted by an
> Admin-approved Course Access Invitation. Checkout, cart, coupons, refunds, invoices, payouts, BNPL,
> payment webhooks, gateway integration, reconciliation, and chapter/bundle/partial-course purchases
> are **deferred, not rejected**, and appear under Fast-Follow below.

## MVP

### Identity and access

- Student-only public registration with mandatory email verification. **Registration grants no
  course access.**
- Email/password login, one opaque rotating server-managed cookie session, password reset, and
  logout. Older access/refresh-token wording is superseded by D-034.
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
- ~~External Discord/Telegram Course community link.~~ **Deferred to post-launch (S18) on 2026-07-29
  by [D-046](DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch).** The
  external Discord community itself is unaffected; only the in-product link moves out of MVP.
- Student content reporting and Admin resolution.

### Instructor and Admin operations

- Instructor-owned Course/Section/Lesson builder, video/resource/lab upload, submission, and
  revision workflow.
- Instructor Course analytics and a minimal Course-scoped Student roster; price visibility is
  read-only.
- Admin-only Course/Section pricing with audit history.
- Admin Course review, publish, delist/relist, retire, archive, emergency access suspension, and
  reported-content moderation.
- Admin user provisioning and suspension; Admin creation, approval, rejection, and cancellation of
  Course Access Invitations.

### Course access and communication

- Admin-created Course Access Invitation bound to one Student email and one complete Course.
- Student acceptance from the invited email identity only; acceptance grants no access.
- Admin Approval as the sole grant trigger, creating or reusing an Enrollment and creating exactly
  one Entitlement, idempotently and audited.
- Admin rejection with a required reason, and Admin cancellation before a decision.
- Course-configured semester expiry snapshotted onto the Entitlement at approval, with audited Admin
  extension/shortening of individual Entitlements.
- Displayed Course prices so a Student knows what to pay through External Payment; Admin-only pricing
  with audit history. Section prices are retained but not displayed, because Section is not an
  acquirable scope.
- Course-scoped one-off live office hours using an entitlement-protected external meeting link.
- Fixed transactional in-app/email notifications.

### Experience and compliance baseline

- Responsive website supporting the complete Student experience on phones, tablets/iPads,
  laptops, and desktops.
- Arabic and English UI for every role; Arabic default, persistent preference, full RTL/LTR.
- Platform-owned UI/player controls target WCAG 2.2 AA within the boundary in §6.
- Bilingual Privacy Notice, Terms, Refund Policy, and course-access terms disclosed before a Student
  accepts an invitation, all approved before production. The Refund Policy remains required even
  though Gradex processes no refunds.

## Fast-Follow

Deferred by [D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation),
each retaining its approved design and its extension point in the access model:

- In-platform checkout and Orders, Tap-hosted card/KNET payment, payment webhooks and callback
  verification, and automated reconciliation. A successful payment must converge on the same
  Entitlement the manual flow produces.
- Shopping cart, Admin coupons and zero-value grants, and Section, chapter, bundle, or
  partial-course acquisition.
- Automated refunds and their entitlement effect; invoice and receipt generation.
- Instructor payout processing, revenue reporting, and emailed statements.

Previously recorded fast-follow items, unchanged:

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
  may receive Course Access Invitations, Entitlements, and Progress. Instructors author assigned content
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
- Evaluate a Course using its authored details, displayed price, and optional public preview.
- Receive a Course Access Invitation at the invited email, accept it from that identity only, see
  that acceptance awaits Admin Approval, and view current access status and invitation history.
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

- Invite Instructors/additional Admins and suspend/reactivate users. Suspension is the mechanism for
  disabling a Student account and immediately blocks access even where an Entitlement is active.
- Confirm External Payment out of band, then create a Course Access Invitation for one Student email
  and one Course; approve, reject with a reason, or cancel it. Approval is the only action that
  grants access, and every transition is audited.
- Create, rename, retire, and delete Major/Subject taxonomy terms with bilingual labels and audit
  history, and override any Course's classification.
- Set/change Course and Section prices with reason and audit history.
- Review Course content, publish/request changes, delist/relist, retire, archive, and invoke or
  resolve emergency access suspension when authorized.
- Preview Course media through a separate audited authorization path.
- Extend or shorten an individual Entitlement's expiry through the audited elevated adjustment.
- Review content reports, dismiss/request changes/delist/retire/access-suspend, and record resolution.
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

## Course Access Invitation and Entitlement

- All payment activity is **External Payment**, performed and verified outside Gradex. Gradex stores
  no payment transaction, amount, currency, gateway reference, or payment status, and receives no
  payment callback. *(BR-020)*
- Displayed monetary values are integer fils rendered as KWD with three decimal places. They tell a
  Student what to pay externally; Gradex charges nothing.
- An Admin confirms External Payment out of band, then creates a Course Access Invitation bound to
  one normalized Student email, one complete Course, the creating Admin, its state, and separate
  creation, acceptance, decision, and cancellation timestamps. *(BR-165)*
- Creating an Invitation grants nothing, and it is never evidence that payment occurred inside
  Gradex. An optional Admin note and opaque external reference may be recorded on the audit record;
  no amount, currency, or payment status is stored anywhere. *(BR-020, BR-170)*
- A Student who already has an Account still requires an Invitation. A Student without one may
  register normally, but the Account alone grants no course access. *(BR-029)*
- Only an authenticated Account whose normalized email matches may accept; any other identity is
  refused server-side regardless of how the link was obtained. *(BR-166)*
- Acceptance moves the Invitation to pending Admin approval and **grants no access**. *(BR-029)*
- **Admin Approval is the sole grant trigger.** It atomically creates or reuses the Enrollment and
  creates exactly one active Entitlement scoped to the whole Course, is idempotent under repetition
  and concurrency, and requires the Admin course-access capability plus a valid recent
  authentication — absent either, the request is refused rather than degraded. *(BR-167)*
- The Invitation lifecycle is pending student acceptance, pending admin approval, approved, rejected,
  and cancelled. Rejection requires a reason; an accepted Invitation may still be rejected, and a new
  Invitation may afterwards be created for the same email and Course. Every transition is audited.
  *(BR-168)*
- An Invitation does not expire. The acceptance link is a separate expiring, single-use, purpose-bound
  secret that is reissued when it lapses. *(BR-169)*
- A Course must have a future Admin-configured access-expiry instant before an Invitation for it can
  be approved. Approval snapshots that instant; runtime access uses the Entitlement's current
  effective expiry, which an elevated Admin may extend or shorten through an audited adjustment.
  *(BR-025, BR-026)*
- Every Entitlement carries a typed grant source and none may exist without one. MVP implements
  `MANUAL_INVITATION` only; no production build may create an Entitlement by any other route,
  command, screen, fixture, or configuration flag. *(BR-028)*
- At most one active Entitlement exists per Student and Course, enforced by database constraint.
  *(BR-024)*
- Instructor and Course Access invitations are separate workflows and neither is implemented in terms
  of the other. *(BR-171)*
- A future online payment flow must converge on this same Entitlement rather than introducing a
  second access model. Playback, downloads, progress, and rosters must never read payment-provider
  state or Invitation state.

## Payouts — deferred out of MVP

Instructor payout processing, revenue reporting, earnings ledgers, and emailed statements are
deferred with in-platform payments. With no in-platform revenue record there is no earning to
calculate, and Instructors are paid entirely out of band at launch. The Instructor agreement's
revenue-share terms remain required under `LG-020`, and the approved payout design is retained in
[BUSINESS_RULES.md](BUSINESS_RULES.md) BR-073/074 and
[DOMAIN_MODEL.md §8](DOMAIN_MODEL.md#8-instructor-earnings-and-payouts--deferred-out-of-mvp).

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

- Required in-app + email events: Course Access Invitation issued, access granted, invitation
  rejected, invitation cancelled after the Student was notified, password/security, Account
  invitation, Course approval/change request, office-hours cancellation/material reschedule, Admin
  Entitlement expiry adjustment, and emergency Course access suspension/restoration.
- Invitation accepted targets Admin operations. New office-hours sessions and new Instructor
  Course/revision submissions are in-app and may also use email when operationally appropriate.
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
- Gradex never collects, transmits, or stores full card/PAN data. In MVP no payment is entered
  anywhere in Gradex; the rule remains binding on any future hosted checkout.
- Course access is granted only by an authorised, capability-gated, recent-authentication-bound,
  idempotent, audited Admin Approval. It replaces gateway verification as the sole control between a
  registered account and paid content and carries the same review depth.
- Auth, verification, reset, invitation acceptance, reporting, signed-URL, and download endpoints are
  rate-limited based on abuse risk.
- Privileged identity, pricing, publishing, preview, course-access invitation, entitlement-grant, and
  moderation actions are auditable.

## Reliability

- Core catalog, course-access, and playback paths target 99.5% monthly availability for MVP.
- Entitlement writes are transactional and idempotent: a repeated or concurrent Admin Approval
  produces exactly one Entitlement.
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
- MVP has no hosted checkout to assess. Whenever one is added, its accessibility is evaluated and
  documented but not claimed as Gradex-controlled.
- Captions/transcripts are outside MVP. Gradex therefore does not claim complete learning-product
  WCAG conformance until that fast-follow gap is closed.

## Privacy

- Collect only data required for identity, learning, commerce, support, security, and law.
- Instructor rosters expose only the minimum Course-scoped display identity and learning fields;
  direct Student account/contact/payment PII remains Admin-only.
- Record the version of any accepted legal/privacy/refund text.
- Support access, correction, deactivation, and deletion requests, subject to documented retention.
- Anonymize eligible personal data where practical while preserving stable surrogate identity and
  necessary financial/action provenance. Financial ledger entries, Statements, payout evidence,
  and privileged-action Audit records are append-only and are never rewritten or hard-deleted;
  policy may restrict access, archive, minimize separable payloads, or anonymize eligible personal
  references.
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
  retention, consumer disclosures, and any education-sector licensing requirement. **Moving payment
  outside Gradex does not answer any of these.** Whether a Kuwait-based course platform must register
  under the Digital Commerce Law, and how off-platform collection is treated for tax, invoicing, and
  record retention, remain counsel and accounting questions (`LG-005`, `LG-006`, `LG-011`, `LG-016`).
- Tap production onboarding and merchant approval are deferred with in-platform payments and are no
  longer MVP launch dependencies.

---

# 8. Assumptions

- External Discord/Telegram can provide initial community/follow-up if an owner and moderation
  process are assigned before launch.
- Students can use downloadable labs without a managed execution environment; setup friction must
  be tested with pilot Students.
- Instructor supply and Course production can proceed in parallel with platform delivery.
- Instructor compensation is handled entirely out of band at launch; the revenue-share percentage
  remains a pre-launch commercial decision and a required term of the Instructor agreement.
- The admin team can confirm External Payment reliably and at launch volume through its own
  operational process, and can carry the manual invitation workload. This is an operating assumption,
  not an engineering control, and it does not scale past a modest launch.

---

# 9. Risks

## Manual Access Granting Is a Single Human Control

**Impact:** with no gateway, an Admin's approval is the only thing between a registered account and
paid content. A mistaken, coerced, or compromised approval grants access that no automated check
would have caught, and the process does not scale past a modest launch volume.

**Mitigation:** a distinct Admin capability rather than a broad one, required recent authentication,
immutable audit on every transition, idempotent grants, an identity-bound acceptance step that an
Admin cannot perform on the Student's behalf, and Tier-3 review of the grant path.

## External Payment Reconciliation Is Off-Platform

**Impact:** Gradex holds no payment record, so payment-to-access reconciliation, disputes, and
refunds depend entirely on the founder's out-of-band process and on `LG-016`'s unresolved accounting
treatment. Errors surface as a Student who paid and has no access, or access with no payment.

**Mitigation:** an optional Admin note and opaque external reference on the audit record, `LG-016`
kept open rather than assumed closed, and a documented manual operating procedure before launch.

## Payment Provider Dependency — deferred, not resolved

**Impact:** whenever in-platform checkout is taken up, provider approval, outages, unsupported refund
methods, or callback drift can block revenue/access correctness.

**Mitigation:** the deferred design keeps hosted integration behind an internal boundary with
signature verification, idempotent states, and reconciliation. The Entitlement grant-source seam
means adding it does not redesign access.

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

## Catalog, Course Access, and Entitlement

- Given a Student opens a published Course, then the full-Course price and the Section outline are
  shown using the same Section entities in that outline; no Chapter entity exists and no Section is
  offered as an acquirable scope. *(BR-010/021; E2E)*
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
- Given a Student registers and verifies their email, when they request any protected Course
  content, then access is denied, because registration creates no Entitlement. *(BR-029; E2E)*
- Given an Admin confirms External Payment out of band, when they create a Course Access Invitation
  for one email and one Course, then the Invitation records email, Course, creating Admin, state, and
  timestamps and is audited, and no Entitlement exists. A second non-terminal Invitation for the same
  pair is refused. *(BR-020/165; integration + E2E)*
- Given the invited Student signs in with the invited email and accepts, then the Invitation moves to
  pending admin approval and **no access is granted**. Given any other Account attempts to accept,
  acceptance is refused server-side regardless of how the link was obtained. *(BR-029/166; security
  integration + E2E)*
- Given an accepted Invitation, when an authorised Admin with valid recent authentication approves
  it, then one transaction creates or reuses the Enrollment, creates exactly one active Entitlement
  scoped to that Course with the snapshotted expiry and approval-derived retirement eligibility,
  writes audit evidence, and notifies the Student. *(BR-025/027/167; integration + E2E)*
- Given the same approval is submitted twice, sequentially or concurrently, then exactly one
  Entitlement exists. *(BR-024/167; integration under race)*
- Given approval is attempted without the course-access capability or without valid recent
  authentication, then it is refused — not degraded, not defaulted. *(BR-167; security integration)*
- Given a Course with no future configured access-expiry instant, then an Invitation for it cannot be
  approved. *(BR-025; integration)*
- Given an Admin rejects an accepted Invitation, then a reason is required, the Student is notified,
  no Entitlement is created, and a new Invitation may afterwards be created for the same email and
  Course. *(BR-168; integration + E2E)*
- Given an active Entitlement, then playback, protected downloads, Progress writes, and the
  Instructor roster all authorise against it, and none reads Course Access Invitation state.
  *(BR-023/029; integration)*
- Given the Account is suspended, then every protected action is denied immediately even though the
  Entitlement remains active, and the Entitlement is not mutated. *(BR-007; E2E)*
- Given a production build, then no route, command, screen, fixture, or configuration flag performs
  checkout, accepts a payment callback, issues a refund, applies a coupon, or creates an Entitlement
  by any path other than recorded Admin Approval. *(BR-020/028; build-level assertion)*

## Instructor Roster

- Given an Instructor opens the roster for an owned Course, then only Students with a qualifying
  Enrollment for that Course appear, showing display name and Course-scoped enrollment/progress only,
  and no Admin note, External Payment reference, approval evidence, direct contact PII, or Student
  from another Instructor's Course is exposed. *(BR-064/101/170; integration + E2E)*
- Given a Course the Instructor does not own, then the roster request is refused server-side.
  *(BR-060/064; security integration)*


## Course Building and Moderation

- Given an Instructor edits an owned Draft Course, then Course/Section/Lesson ordering persists;
  editing another Instructor's Course or any price is denied server-side. *(BR-010/019/060; integration)*
- Given a Course does not meet readiness, submission is blocked with specific missing items; when
  ready, submission moves to Pending Review and locks concurrent editing. *(BR-012/013/016/070; E2E)*
- Given an Admin requests changes, the reason is required and visible; resubmission returns to
  review. Approval publishes and notifies. *(BR-071/072/090/122; E2E)*
- Given a Published Course revision, the live approved version remains unchanged until Admin
  approval applies the revision atomically. *(BR-017/090; integration)*
- Given an Admin delists a Course, it leaves catalog discovery and new access grants but qualifying existing access
  continues. Given an elevated Admin invokes emergency access suspension with a constrained reason,
  existing Student access stops without mutating Entitlements/Progress and Audit/notifications are
  recorded; restoration re-enables otherwise-valid access. *(BR-090; integration + E2E)*
- Given an Admin changes a Course/Section price, the change is audited and affects future access
  grants only; the expiry snapshot on an existing Entitlement remains unchanged. *(BR-019; integration)*

## Learning, Preview, Reporting, and Office Hours

- Given a Student requests playback/download or writes Progress, runtime access and qualifying graph
  reachability are required. Completion uses the trusted exact Asset Version duration, ignores
  client percentages, and never regresses across Video replacement. *(BR-023/050–053/116)*
- Given an Instructor uploads a public preview, it remains unavailable until validation,
  quarantine, scan, and permission confirmation succeed; protected Lesson files remain private.
  *(BR-104/143/144; integration)*
- Given an entitled Student reports content, it enters the Admin queue without auto-hiding; Admin
  resolution and any content/account action are audited. *(BR-145/146; E2E)*
- Given an Instructor creates Course office hours, only Students holding an active Course Entitlement
  see the join link; an unauthorized/public request never receives it. *(BR-134–136; security E2E)*
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
