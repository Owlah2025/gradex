# Documentation Reconciliation — Design Spec

**Date:** 2026-07-23

**Status:** Approved design record; baseline validation completed after follow-up remediation on
2026-07-23

**Scope:** Documentation baseline required before platform system design

**Change boundary:** Documentation only; no production code or schema changes

---

## 1. Purpose

Gradex has substantial product, experience, feature, and technical documentation, but the
documents do not yet form one reliable system-design input. MVP features differ between
the product baseline and downstream UX documents, several policy decisions are missing or
contradictory, and some technical/legal statements are placeholders or are not supported by
the referenced source.

This reconciliation will establish one canonical MVP baseline and propagate it through all
downstream Markdown documentation. The selected approach preserves useful existing work,
makes conflicts explicit, and avoids redesigning the product or adding post-MVP features.

## 2. Selected approach

Use a **canonical baseline and cascade**:

1. Correct the durable principles and authoritative product documents.
2. Record the approved decisions and make their business rules enforceable.
3. Align user journeys, screens, navigation, wireframes, feature specs, and technical docs.
4. Verify every downstream statement against its authoritative source and current code where
   the document describes implementation.

Rejected alternatives:

- **Surgical corrections only:** faster, but leaves authority unclear and makes renewed drift
  likely during system design.
- **Rewrite and consolidate:** creates excessive churn, loses useful history, and increases the
  risk of omitting already documented behavior.

## 3. Documentation authority and governance

Documentation authority, from highest to lowest, is:

1. **Constitution** — durable product and engineering principles.
2. **Product baseline** — Project Vision, PRD, Decisions, Business Rules, and Glossary.
3. **Experience documentation** — User Journeys, Screens, Navigation Rules, Wireframes, and
   design-system guidance.
4. **Feature specifications and technical designs** — detailed behavior constrained by the
   product baseline.
5. **READMEs and implementation notes** — descriptions of the code as it currently exists.

Governance rules:

- The PRD owns one explicit MVP, fast-follow, and out-of-scope register.
- Decisions are appended and traceable. Superseded statements are marked rather than silently
  overwritten.
- Feature specs reference their governing decisions and business rules.
- Legal interpretations, provider capabilities, and unresolved commercial values are labeled
  as launch gates instead of confirmed facts.
- Existing implementation terminology is respected when it matches the approved product model.
- Downstream documents may add detail but may not expand scope or contradict an upstream rule.

## 4. Approved MVP baseline

### 4.1 Scope boundary

MVP includes:

- Public catalog and course evaluation.
- Student registration, verification, authentication, purchase, learning, and progress.
- Admin-provisioned instructor and administrator accounts.
- Instructor-owned course creation and content management with admin moderation.
- Course and single-Section purchase through card/KNET checkout.
- Admin-managed coupons, including zero-value grants.
- Full and partial refunds.
- Entitlement-protected video, resources, and lab materials.
- Optional public course preview assets.
- Student content reporting and admin resolution.
- External-link live office-hours scheduling.
- Fixed transactional in-app/email notifications.
- Manual instructor payouts with system-recorded accounting.
- Arabic/English responsive web experiences.

Fast-follow or out of MVP:

- Bundles and BNPL installments.
- Native iOS or Android applications.
- Built-in conferencing, recordings, attendance tracking, recurring-session engines, and
  calendar integration.
- Instructor payout dashboards and withdrawal controls.
- Notification preferences, marketing messages, SMS, and push notifications.
- Reviews, ratings, recommendation engines, and fabricated testimonials.
- Captions, subtitles, and transcripts.
- Social login and MFA.

### 4.2 Identity and access

- Public self-registration creates Student accounts only.
- A Student remains pending until an expiring, single-use email verification succeeds.
- Existing Admins invite Instructors and additional Admins. The invitation verifies the email
  address and lets the recipient establish their initial password.
- The first bootstrap Admin is created once through a secure deployment/seed operation. No
  bootstrap credentials are committed, and the account must change its initial password.
- Canonical account states are `pending_verification`, `pending_invitation`, `active`,
  `suspended`, and `deactivated`.
- Suspension immediately blocks all protected actions, including actions attempted through an
  existing session. System design selects the revocation/status-check mechanism.
- Deactivation blocks access while preserving only records required for finance, audit,
  security, or legal obligations. Privacy and retention policy govern deletion/anonymization.
- Passwords accept 15–128 Unicode characters, including spaces. The product does not impose
  composition rules or periodic rotation; it rejects common/known-compromised passwords.
  Passwords use Argon2id and are forced to change only after bootstrap, reset, or compromise.
- Verification, invitation, and reset tokens are expiring, single-use, stored securely, and
  rate-limited.
- Authentication responses do not expose whether an email address is registered.
- Role, ownership, and entitlement checks occur server-side on every protected operation.
- Privileged identity, role, moderation, refund, pricing, and payout actions are audited.

Password choices follow current OWASP authentication and password-storage guidance and NIST
SP 800-63B:

- <https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html>
- <https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html>
- <https://pages.nist.gov/800-63-4/sp800-63b.html>

### 4.3 Course model, ownership, and pricing

- The canonical hierarchy is `Course -> Section -> Lesson`.
- `Section` is the only domain entity at the middle level. “Chapter” may appear as a localized
  or student-facing label, but it maps one-to-one to a Section and is not a separate entity.
- Instructors manage only their own course content, hierarchy, videos, downloadable resources,
  lab materials, public previews, and office-hours sessions.
- Instructors do not create or modify Course or Section prices. They may view current prices as
  read-only.
- Admins exclusively set and change Course and Section prices.
- Admin price changes apply only to future orders. Existing orders, entitlements, refunds, and
  payouts retain the transaction snapshots created at purchase.
- Each price change records the previous and new value, acting Admin, reason, and timestamp.
- Coupons change an order total but never mutate a catalog price.

### 4.4 Course and video lifecycles

- The Course lifecycle supports `draft`, `pending_review`, `changes_requested`, `published`,
  `unpublished`, and `archived`, plus resubmission transitions. A Course with enrollment
  history is archived rather than permanently deleted.
- A Course needs at least one Section, one Lesson, and a READY Lesson video before submission.
- Published content cannot bypass Admin review. Material Instructor changes return through the
  documented review path.
- Video processing state is independent from Course publication state and includes uploaded,
  processing, ready, and failed behavior aligned with the video design.
- Playback and protected downloads require an active entitlement covering the Lesson's Course
  or Section.
- Learning progress remains when an entitlement expires, but protected access stops until the
  Student purchases access again.

### 4.5 Commerce and entitlement

- One MVP order purchases either one complete Course or one Section.
- Money is stored and calculated as integer fils and displayed as KWD with three decimal places.
- Percentage discounts use one documented integer-fils rounding rule and are clamped so an
  order total never becomes negative.
- Tap-hosted card/KNET checkout is the MVP payment path.
- The server creates stable order and payment-attempt references before gateway redirect.
- Gateway webhook/API confirmation, not browser redirect, controls payment success, refunds,
  and entitlement changes.
- Callbacks, retries, and grants are idempotent.
- Successful payment or a valid zero-value coupon grant creates an Entitlement using the exact
  expiry disclosed and snapshotted on the Order. *(Reconciled 2026-07-26 by D-026, which supersedes
  the fixed 150-day boundary.)*
- A Student cannot repurchase a scope for which they already have an active entitlement.
- Order, payment attempt, refund, entitlement, and payout states remain separate.

### 4.6 Coupons

- Coupons are created and managed only by Admins.
- A Student may successfully redeem a given coupon only once.
- Failed or abandoned attempts do not consume the redemption.
- A fully refunded purchase releases the Student's per-coupon redemption for future use while
  preserving historical records and auditability.
- Per-user coupon limits greater than one are not configurable in MVP.
- Global redemption caps remain configurable.
- Redemption commits only after confirmed payment or a successful zero-value grant.
- One coupon may be applied to an order; stacking is out of scope.

### 4.7 Refunds

- MVP supports full and partial refunds.
- Admin enters an amount in integer fils and a mandatory reason.
- One or more refunds may be issued up to the order's remaining refundable balance.
- The system checks whether the original payment method supports partial refunds.
- A successful partial refund leaves the entitlement active.
- When cumulative successful refunds equal the captured amount, the entitlement is revoked.
- Refund state changes and entitlement effects occur only after confirmed gateway success,
  including asynchronous confirmation.
- Refund requests are idempotent and retain gateway reference, acting Admin, reason, amount,
  status, timestamps, and audit history.
- Instructor earnings and payout adjustments use the amount actually refunded.
- Checkout records the exact bilingual refund-policy version accepted by the Student.
- Eligibility remains configurable until Kuwaiti counsel approves the launch policy.

Tap currently documents amount-controlled full/partial refunds, while also documenting a
possible `Partial Refund not Supported` response for some transactions:

- <https://developers.tap.company/reference/create-a-refund>
- <https://developers.tap.company/reference/charge-response-codes>

### 4.8 Instructor earnings and payouts

- MVP uses one platform-wide Instructor revenue-share percentage.
- The percentage has no assumed value. It is required commercial configuration before
  production launch and can be set without code changes.
- The Instructor share is calculated from net collected revenue after coupons, confirmed
  refunds, and gateway/payment fees.
- Payouts are reconciled monthly and transferred manually by bank transfer.
- The system records covered orders, calculations, adjustments, status, transfer reference,
  acting Admin, and audit history.
- Late refunds and chargebacks become adjustments in the next statement.
- Instructors receive monthly statements by email.
- MVP has no Instructor payout dashboard, withdrawal controls, or automated settlement.
- Admin tooling is limited to preparing, reviewing, recording, and marking payouts paid.

### 4.9 Public previews and protected materials

- Actual Lesson resources and lab materials always require entitlement.
- An Instructor may optionally upload one separate Course preview asset: a short video, PDF,
  image, or deliberately prepared sample file.
- The preview is explicitly public, is not attached to a protected Lesson, and is omitted from
  the public page when absent.
- Public previews and downloadable assets receive file-type/size validation, quarantine, and
  malware scanning before availability.
- The Instructor confirms that they have permission to publish the preview.
- The platform never exposes a protected lab automatically as a sample.

### 4.10 Content reporting

- Entitled Students can report a Course, Lesson, video, Resource, or Lab Material.
- Reasons include broken/unavailable, inaccurate, inappropriate, suspected copyright
  violation, and other. “Other” requires a short explanation.
- A report never hides content automatically.
- Admins may dismiss a report, request Instructor changes, unpublish affected content, or use
  existing account-suspension powers.
- Resolution records the acting Admin, reason, action, timestamp, and audit history.
- Duplicate/spam reports are rate-limited.
- Instructors are notified only when an Admin requests changes or performs a relevant content
  action.

### 4.11 Live office hours

- Instructors create or materially reschedule office-hours sessions only for their own
  `PUBLISHED` Courses. An owner may still cancel an existing scheduled Session after the Course is
  Unpublished or Archived; they cannot create or reschedule in those states.
- A session contains a title, description, start/end time, and external meeting link.
- Students may discover and open the link only while the Course remains `PUBLISHED` and they have
  an active Course entitlement or an active Section entitlement for that Course. Unpublishing or
  archiving hides Student discovery/join without deleting the Session. Admins retain moderation
  access.
- Times are stored consistently and displayed in the user's local timezone, defaulting to
  Kuwait time, using the selected interface language.
- Meeting links remain hidden until authentication and entitlement checks pass.
- MVP does not provide conferencing, recordings, attendance, recurring sessions, or calendar
  integration.

### 4.12 Notifications

The MVP uses a fixed transactional policy rather than notification preferences:

- In-app and email: purchase receipt, refund updates, password/security events, account
  invitation, Course approval/changes requested, and office-hours cancellation or material
  rescheduling.
- In-app by default, with email where operationally appropriate: new office-hours session and new
  Instructor Course/revision submissions to Admin operations.
- Video-processing completion is an Instructor event, not a Student event.
- Required security and transactional messages cannot be disabled.
- Each event is deduplicated and stores delivery status. Delivery failure never rolls back the
  underlying business transaction.
- No marketing, SMS, push, or granular preference system is in MVP.

### 4.13 Responsive web and localization

- Gradex MVP is one responsive website, not a native mobile application.
- Students receive the complete learning experience on phones, iPads/tablets, laptops, and
  desktops. Larger screens may improve layout density but do not unlock exclusive Student
  functions.
- Video supports responsive sizing, landscape viewing, accessible controls, and browser
  fullscreen where available.
- Instructor/Admin portals remain responsive, while complex build, upload, moderation,
  refund, reporting, and payout work is optimized for tablets/laptops/desktops.
- Arabic and English are supported across every role. Arabic is the initial default, and the
  user's language choice persists.
- Layout, navigation, tables, icons, validation, and mixed-language content support RTL and
  LTR correctly.
- Course content remains in its authored language; Gradex does not translate it automatically.

### 4.14 Accessibility

- Platform-owned interfaces and player controls target WCAG 2.2 Level AA, including keyboard
  operation, visible focus, accessible authentication, labels, announced errors, contrast,
  touch targets, and screen-reader semantics.
- Third-party hosted checkout accessibility is evaluated and documented but is not directly
  controlled by Gradex.
- Captions and transcripts remain outside MVP. The documentation therefore must not claim that
  the complete learning product conforms to WCAG 2.2 AA. It may state only the scoped target
  for platform-owned interfaces and controls and must identify media accessibility as a known
  fast-follow gap.

W3C recommends using WCAG 2.2 as the current WCAG 2 version:

- <https://www.w3.org/WAI/standards-guidelines/wcag/>
- <https://www.w3.org/TR/WCAG22/>

### 4.15 Privacy, security, and legal readiness

- Documentation uses the correct title: **CITRA Decision No. 26 of 2024 issuing the Data
  Privacy Protection Regulation**, rather than calling it a separate Law No. 26/2024.
- Gradex provides bilingual Privacy Notice, Terms, Refund Policy, and checkout disclosures
  before launch.
- The platform collects only data needed for identity, learning, commerce, support, security,
  and legal operations.
- Full card details are never stored; payment entry remains with the hosted gateway.
- Sensitive data is encrypted in transit and at rest. Secrets remain outside the repository,
  and credentials, tokens, and personal data are excluded from logs.
- Policy/consent versions are stored where acceptance is required.
- The product supports requests to access, correct, deactivate, or delete personal data,
  subject to records that must remain for finance, audit, security, or law.
- Data is anonymized where possible while preserving necessary referential commercial records.
- Financial, refund, payout, security, and privileged audit records use a documented retention
  schedule. Exact retention periods are a pre-launch counsel/accounting decision.
- Security-sensitive operations use rate limits, audit trails, generic failures, server-side
  role/ownership checks, and least privilege.
- Documentation states that the official Kuwait announcement describes Digital Commerce Law
  No. 10 of 2026 as applying six months after Gazette publication. It does not repeat the
  unsupported “one month after implementing regulations” claim.
- Counsel verifies refund exceptions for streamed education, privacy-regulation applicability,
  retention, consumer disclosures, and policy wording before production.
- Tap production onboarding, digital-goods approval, supported methods, webhook behavior, and
  partial-refund capability are external launch dependencies.

Official sources:

- CITRA Decision No. 26 of 2024: <https://www.citra.gov.kw/sites/ar/Pages/DecisionsDetails.aspx?id=6>
- Kuwait Digital Commerce Law announcement: <https://e.gov.kw/sites/KGOArabic/Pages/ApplicationPages/NewsDetail.aspx?nid=64409149>

These gates do not prevent system design, but they prevent production launch until resolved.

## 5. State-model requirement

The reconciliation will make the following lifecycles explicit enough to serve as system-design
inputs:

- User/account and invitation/verification.
- Course moderation/publication.
- Video processing/publication.
- Order and payment attempt.
- Refund, including cumulative partial refunds.
- Enrollment/entitlement and expiry/revocation.
- Content report and resolution.
- Office-hours session scheduling/cancellation.
- Coupon and redemption.
- Instructor earning, statement, and payout.

`docs/DOMAIN_MODEL.md` will own canonical entities, relationships, ownership, invariants, and
state definitions. Feature designs may add implementation detail but may not redefine them.

## 6. Cross-document propagation

| Document group | Required change |
|---|---|
| Constitution | Preserve existing uncommitted work; replace ambiguous device wording and align durable MVP principles |
| Project Vision / PRD | Establish the definitive scope register and approved policy baseline |
| Decisions | Append traceable decisions, mark superseded statements, and repair incorrect/nonportable references |
| Business Rules | Add enforceable identity, pricing, coupon, refund, payout, preview, reporting, office-hours, and notification rules |
| Glossary | Replace the placeholder with canonical domain and lifecycle terms |
| User Journeys | Add missing MVP flows and remove Instructor pricing control |
| Screens / Navigation / Wireframes | Align screen inventory and remove unsupported screens/features |
| Design guidance | Align responsive bilingual behavior, repair references, and distinguish requirements from examples |
| Feature specs | Resolve auth clarifications, coupon limits, pricing/refund behavior, and office-hours references |
| Technical docs | Replace the Coding Standards placeholder and correct invalid or misleading examples against source |
| Root README | Report the project's actual pre-system-design status |

Create two focused baseline documents:

- `docs/DOMAIN_MODEL.md` — canonical entities, ownership, relationships, invariants, and states.
- `docs/LAUNCH_GATES.md` — unresolved commercial, legal, compliance, and provider items, with
  owner and required resolution point.

No production implementation or schema changes are part of this documentation task.

## 7. Specific contradictions to remove

- Coupons and external-link office hours appear consistently as MVP.
- Bundles, BNPL, native apps, built-in conferencing, notification preferences, Instructor
  payout dashboards, reviews, recommendations, captions, and transcripts are not shown as MVP.
- Instructor course-building flows never allow price mutation.
- `Chapter` is not a separate domain entity or API/data-model target.
- Public previews are separate from protected Lesson resources/labs.
- The Reported Content queue has a valid Student-originating report flow.
- Coupon per-user behavior matches its uniqueness constraint.
- Auth feature clarifications are replaced by the approved provisioning, password, and
  suspension policies.
- Office-hours decisions reference the correct community/office-hours decision records.
- Notification events match the approved fixed policy.
- Digital-commerce and data-privacy references use the official wording and retain counsel
  gates where interpretation remains unresolved.
- Missing design-system references are corrected, removed, or explicitly identified as future
  artifacts.
- JSON examples are valid JSON. Illustrative source snippets are verified or clearly labeled
  pseudocode.
- Root/project phase wording reflects that design and implementation artifacts already exist
  while platform system design has not started.

## 8. Verification and acceptance

The documentation is ready for system design when all of the following pass:

1. Every Markdown file is inventoried and classified as authoritative, downstream,
   implementation-facing, historical, or generated.
2. The PRD has one unambiguous MVP/fast-follow/out-of-scope register.
3. Every approved policy has a decision and corresponding business rule.
4. Downstream journeys, screens, navigation, wireframes, and feature specs agree with the
   baseline.
5. No unresolved clarification tag, placeholder, task marker, or consolidation note
   remains unless intentionally recorded in `LAUNCH_GATES.md` with an owner and resolution
   point.
6. Relative links and referenced local files resolve.
7. JSON examples parse, and technical examples match source or are labeled pseudocode.
8. Coding standards reflect actual repository conventions.
9. Legal claims cite official sources, and unresolved interpretations remain explicit gates.
10. State definitions and transitions are sufficient inputs for system design.
11. Existing unrelated/uncommitted workspace work remains intact.
12. A final docs review verifies accuracy, traceability, readability, link integrity,
    docs-versus-code consistency, and strict MVP scope.

The final handoff may contain unresolved production launch gates. It must not contain hidden
product ambiguity or contradictory system-design inputs.

## 9. Implementation sequence

The approved reconciliation was completed in this sequence:

1. Inventory and classify all Markdown documents.
2. Patch Constitution and canonical product baseline while preserving unrelated edits.
3. Create Domain Model and Launch Gates documents.
4. Propagate rules into journeys and experience artifacts.
5. Reconcile feature specs and technical documentation against source.
6. Run automated searches, link/example checks, and traceability checks.
7. Run the documentation quality guard, address findings, and report remaining launch gates.
