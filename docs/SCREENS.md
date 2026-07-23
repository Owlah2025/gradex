# Screens

> Status: Canonical MVP screen contract
> Last Updated: 2026-07-23

This document defines the purpose, content, actions, states, and permissions of Gradex screens.
Route hierarchy is in [NAVIGATION_MAP.md](NAVIGATION_MAP.md), navigation behavior in
[NAVIGATION_RULES.md](NAVIGATION_RULES.md), and governing rules in
[BUSINESS_RULES.md](BUSINESS_RULES.md).

## Conventions

- One responsive web screen may render differently across viewports without changing capability.
- `Section` is canonical; UI copy may localize it as “Chapter.”
- Modal/drawer states are documented under their owning screen, not counted separately.
- Every mutation validates role/ownership/status server-side and exposes pending/success/failure.
- Arabic/English and RTL/LTR apply to every screen, including tables, dialogs, errors, and email
  deep-link destinations.
- No MVP screen exists for Instructor pricing, Instructor earnings/payouts, notification
  preferences, platform-wide office hours, reviews/ratings, recommendations, bundles, or BNPL.

---

# Shared and Authentication

## S01 — Landing

**Purpose:** Explain Gradex's Kuwait-first value and move visitors into Course discovery.

- **Entry:** Public root, logo/home links.
- **Core content:** Value proposition, how learning/follow-up works, featured published Courses,
  Instructor value, FAQ, legal/footer links.
- **Primary actions:** Browse Courses, open Course Details, Login, Register.
- **States:** Published Course list/empty; authenticated header variant.
- **Constraints:** No fabricated testimonials, public ratings, recommendation claims, or protected
  Lesson/Lab previews.

## S02 — Login

**Purpose:** Authenticate an Active Account and return safely to its intended route.

- **Entry:** Header, guarded-route redirect, Register/Reset completion.
- **Fields:** Email, password.
- **Actions:** Sign in, Forgot Password, Register (Students only).
- **States:** Submitting, generic invalid credentials, suspended/deactivated, retryable failure.
- **Exit:** Validated `returnTo` or role root.

## S03 — Student Registration

**Purpose:** Create a `PENDING_VERIFICATION` Student Account.

- **Fields:** Display name, email, password, required policy acceptance.
- **Display-name guidance:** 2–50 Arabic/Latin-script characters; no URLs, control characters, or
  markup. It is a non-unique profile label, not the Account identity.
- **Password guidance:** 15–128 characters; spaces/Unicode allowed; no composition checklist.
- **Actions:** Submit, Login.
- **States:** Submitting; generic accepted response; breached/common-password validation; rate limit.
- **Exit:** Verify Email screen. No session is issued before verification.
- **Permissions:** Public; role cannot be chosen.

## S04 — Verify Email

**Purpose:** Complete Student activation or explain why the link cannot be used.

- **Entry:** Registration and email deep link.
- **Actions:** Consume link, resend, return to Login.
- **States:** Pending, verified, expired, already used, invalid, resend throttled.
- **Exit:** Login or validated preserved intent.

## S05 — Forgot / Reset Password

**Purpose:** Request and complete a non-enumerating password reset.

- **Request fields:** Email.
- **Reset fields:** New password, confirmation.
- **States:** Generic request accepted, expired/reused token, invalid password, success.
- **Exit:** Login.

## S06 — Accept Staff Invitation

**Purpose:** Verify and activate an Admin-assigned Instructor/Admin role.

- **Entry:** Expiring email link.
- **Content:** Inviting organization, assigned role, email, policy links.
- **Fields:** Display name, initial password, and confirmation.
- **Display-name guidance:** Same BR-105 validation as Student registration; it defaults the
  newly created Account profile and remains editable after activation.
- **States:** Valid, expired, revoked, already used, conflicting registered address.
- **Exit:** Login; no public role selector or role upgrade.

## S07 — Notification Center

**Purpose:** Durable per-recipient record of fixed transactional events.

- **Content:** Unread/read list, event type, timestamp, safe deep link.
- **Actions:** Open event, mark read/all read.
- **Events:** Purchases, refunds, security, invitations, Course/revision submission and
  review/change request, video-processing (Instructor), and office-hours changes per BR-122.
- **States:** Loading, empty, delivery-channel failure metadata hidden from ordinary user.
- **Constraints:** No preferences, marketing, SMS/WhatsApp, or push controls.

## S08 — Profile and Account

**Purpose:** Manage personal profile, language, email/password, and Account/data requests.

- **Content:** Role/status, profile fields, display name, language, Account/security links.
- **Actions:** Edit profile and display name, switch Arabic/English, change email (verify new
  address), change password, request data access/correction/deactivation/deletion, logout.
- **Rules:** Display name is 2–50 characters in either script, is not unique, rejects URLs/control
  characters/markup, and is what an Instructor roster shows (BR-105).
- **States:** Dirty form, verification pending, request acknowledged, suspended/deactivated read-only
  handling as policy permits.
- **Permissions:** Authenticated owner; role mutation is absent.

## S09 — Legal

**Purpose:** Show versioned bilingual Terms, Privacy Notice, and Refund Policy.

- **Entry:** Footer, Registration, Checkout, Profile.
- **Content:** Effective version/date and approved text.
- **Actions:** Switch legal document/language; return to source.
- **Constraints:** No unapproved claim that streaming automatically removes refund rights.

## S10 — System States

**Purpose:** Shared 401/403/404/expired/offline/5xx and empty states.

- **Actions:** Retry, Login, role root, Catalog, Course Details/repurchase when allowed.
- **Constraints:** Do not reveal entity/account existence; do not advertise out-of-scope actions.

---

# Student

## ST01 — Catalog and Search

**Purpose:** Find published Courses by Major, Subject, Study Year, and search.

- **Content:** Search field, the three taxonomy filters, active-filter chips, result count, Course
  cards with Instructor, price, term, and practical material/office-hours indicators grounded in
  Course data.
- **Actions:** Search, filter by Major/Subject/Study Year, clear filters, sort, open Course Details.
- **States:** Loading, no Courses, no matches, error.
- **Rules:** Only `PUBLISHED` Courses appear (BR-161). Filters are exact-match on one value per
  dimension; taxonomy labels render in the selected language while search matches Arabic and English
  at once with diacritic/alef/digit normalization (BR-162). Ranking is relevance only — no
  recommendations, promotion, or personalization.
- **Responsive:** Filter sheet on small screens; rail where space allows.

## ST02 — Course Details

**Purpose:** Evaluate a Published Course and choose Course or Section access.

- **Content:** Title, Instructor, authored description/language, outline, Resources/Labs summary,
  office-hours support, community, Course price, individually priced Sections, access term.
- **Actions:** Play optional Public Preview, choose Course/Section, Checkout, Go to Course if active,
  Login/Register when required.
- **Public Preview state:** Separate validated asset; absent preview removes the control.
- **Locked content:** Lesson titles may be visible, but protected media/files are not public.
- **Constraints:** No Sample Lab download, ratings/reviews, recommendations, bundle, or BNPL CTA.

## ST03 — Checkout

**Purpose:** Confirm one Course/Section Order, apply one coupon, accept policy, and open Tap.

- **Content:** Item/scope, catalog subtotal, coupon/discount, total KWD, exact access-expiry instant,
  accepted Refund Policy version, payment-method handoff.
- **Actions:** Apply/remove coupon, continue to Tap, cancel.
- **States:** Coupon valid/invalid/inactive/expired/wrong-scope/cap/already-used; zero-value grant;
  creating Order; gateway unavailable.
- **Rules:** Integer fils; server recalculates; active duplicate Entitlement blocked.

## ST04 — Payment Confirmation and Receipt

**Purpose:** Represent gateway-confirmed status without trusting the redirect.

- **States:** Confirming/pending, paid, free-granted, failed/cancelled/timed-out, reconciliation
  needed.
- **Receipt content:** Order/item snapshot, subtotal/discount/paid amount, payment reference, date,
  access expiry, Refund Policy version.
- **Actions:** Start/Go to Course, Orders & Refunds, retry a definitive failure safely.
- **Constraints:** No access/receipt success until verified backend confirmation.

## ST05 — Student Dashboard

**Purpose:** Resume learning and see owned/expired Courses, upcoming office hours, and recent status.

- **Content:** Continue Learning, My Courses/Sections, progress, expiry, upcoming sessions, recent
  notifications/order status.
- **Actions:** Resume Lesson, open Course Home, Browse, Orders & Refunds.
- **States:** First purchase/empty, active, near expiry (without scheduled reminder), expired.

## ST06 — Course Home

**Purpose:** Navigate the Course within the Student's exact Entitlement scope.

- **Content:** Course progress, access-until, ordered Sections/Lessons, locked markers, Resources/Labs,
  upcoming office hours, community link.
- **Actions:** Start/resume Lesson, open allowed material, join authorized office hours, report Course.
- **States:** Course Entitlement, Section-only Entitlement, expired, temporarily Unpublished.
- **Constraints:** Locked Lessons never expose signed URLs.

## ST07 — Lesson Player

**Purpose:** Watch an entitled Lesson and retain progress.

- **Content:** Responsive HLS player, Lesson title/context, progress, Lesson outline/rail, material and
  report actions.
- **Actions:** Play/pause/seek/volume/quality/fullscreen, Previous/Next, Course Home, Resources/Labs,
  Report.
- **States:** Loading, playing, resuming, completed, video unavailable, transient retry,
  access denied/expired.
- **Accessibility:** Keyboard and screen-reader-labelled controls; captions are not an MVP control.

## ST08 — Lesson Resources and Labs

**Purpose:** Download entitled reference/hands-on files.

- **Content:** Separate Resource and Lab lists with type/size/description.
- **Actions:** Download via newly authorized short-lived link; report a file; return to Lesson/Course.
- **States:** Empty per category, generating link, expired/retry, denied, unavailable after moderation.
- **Constraints:** No public link; Lab buyer-identification may be applied server-side.

## ST09 — Office Hours

**Purpose:** View Course-scoped sessions the Student is entitled to join.

- **Content:** Course, title, description, localized start/end, status.
- **Actions:** Open Course, join external meeting after authorization.
- **States:** Upcoming, rescheduled, cancelled, completed, empty, entitlement expired.
- **Constraints:** Link is never present in public/unauthorized payloads.

## ST10 — Orders and Refunds

**Purpose:** View Order, Payment Attempt, Receipt, and Refund history/status.

- **Content:** Item snapshot, paid/discount amounts, payment status/reference, Entitlement term,
  accepted policy version, Refund list and remaining refundable balance where appropriate.
- **Actions:** View receipt/policy; follow support refund-request instructions; return to Course.
- **States:** Paid/free-granted/failed; refund pending/partially refunded/refunded/failed.
- **Constraints:** Student does not directly call refund mutation; Admin applies approved process.

### Report Content Modal/Drawer

Owned by Course Details only when entitled, Course Home, Lesson Player, and Materials screens.

- **Fields:** Target, fixed reason, explanation (required for “other”).
- **States:** Submitting, accepted, duplicate/rate-limited, error.
- **Effect:** Creates a report; never auto-hides content.

---

# Instructor

## IN01 — Instructor Dashboard and Courses

**Purpose:** See owned Courses and their lifecycle/review state.

- **Content:** Course cards/table, Draft/Pending Review/Changes Requested/Published/Unpublished/
  Archived, video failures, upcoming office hours.
- **Actions:** Create Course, open Builder/Analytics/Office Hours, respond to change request.
- **Constraints:** No earnings/payout or price-edit controls.

## IN02 — Course Builder

**Purpose:** Manage owned Course metadata and ordered Sections/Lessons.

- **Content:** Authored metadata/language, Major/Subject/Study Year selectors, Course outline,
  current Course/Section prices read-only, state/sync indicator.
- **Actions:** Edit metadata, select classification, add/reorder/delete Sections/Lessons, open
  Lesson/Preview, submit.
- **States:** Autosaving/synced/error, read-only Pending Review, Published live + pending revision,
  missing-classification validation blocking submit.
- **Permissions:** Own Course only; all price fields non-editable; taxonomy selectors offer existing
  Admin-managed terms only — no create/rename/retire control, and retired terms are unselectable.

## IN03 — Lesson Editor

**Purpose:** Edit Lesson metadata and manage its video.

- **Content:** Title/order, video upload/processing state, existing live-vs-replacement state.
- **Actions:** Save, upload/replace/retry video, open Materials.
- **States:** Uploading/processing/ready/failed/published; unsaved/upload-leave guard.

## IN04 — Resources and Labs Manager

**Purpose:** Manage protected files as two distinct categories.

- **Content:** Lists, type/size limits, scan/availability state.
- **Actions:** Upload/replace/delete allowed file, edit description.
- **States:** Validating/uploading/quarantined/scanning/available/failed/over-cap.

## IN05 — Public Preview Manager

**Purpose:** Manage the Course's optional separate public preview.

- **Content:** Current asset, allowed formats/limits, permission confirmation, scan state.
- **Actions:** Upload/replace/remove, confirm permission, preview public rendering.
- **States:** None, validating/quarantined/scanning/available/failed.
- **Constraints:** Cannot select a protected Lesson/Lab as public.

## IN06 — Submit and Review Status

**Purpose:** Validate readiness, submit, and respond to Admin review.

- **Content:** Checklist, missing-item links, current status, Admin reason/history.
- **Actions:** Submit, return to fix, revise/resubmit after Changes Requested.
- **States:** Not ready, ready, submitting, Pending Review read-only, Changes Requested, Published.

## IN07 — Course Analytics

**Purpose:** View owned Course learning activity.

- **Content:** Enrollment count, completion/progress aggregates, Lesson drop-off, own Course roster
  with Student-chosen display name/alias and Course-scoped enrollment/progress fields only.
- **Actions:** Filter/export only if approved by privacy/access policy; open Course/Lesson.
- **Constraints:** No earnings/payout numbers; no Student email, phone, payment/legal identity, or
  cross-Course activity.

## IN08 — Instructor Office Hours

**Purpose:** Schedule one-off external-link sessions for an owned Published Course.

- **Fields:** Course, title, description, start/end, external link.
- **Actions:** Create/reschedule/cancel.
- **States:** Scheduled/completed/cancelled, validation, suspended/read-only.
- **Constraints:** No platform-wide/recurring session, RSVP, attendance, recording, reminder, or
  calendar controls.

---

# Admin

## AD01 — Admin Ops

**Purpose:** Surface actionable operational state.

- **Content:** Pending Course reviews, open reports, pending/failed refunds, payout run status,
  failed content processing/scans, invitation status.
- **Actions:** Open the relevant queue/detail.
- **Constraints:** Metrics are operational, not recommendations/marketing analytics.

## AD02 — Users and Invitations

**Purpose:** Provision staff and manage Account status.

- **Content:** Search/filter Accounts, role/status, invitation history, relevant audit events.
- **Actions:** Invite Instructor/Admin, resend/revoke invitation, suspend/reactivate with reason.
- **States:** Pending/accepted/expired/revoked/conflicting-address invitations;
  Active/Suspended/Deactivated Accounts.
- **Constraints:** No password display/reset to known value; no public role assignment; an email
  already attached to an Account cannot be invited or converted to another role in MVP.

## AD03 — Pricing

**Purpose:** Set and audit Course/Section catalog prices.

- **Content:** Course outline, current prices, price history.
- **Actions:** Set/change integer-fils price with required reason.
- **States:** Unsaved validation, saving, success, conflict, audit view.
- **Constraint:** Change affects future Orders only.

## AD04 — Course Review Queue

**Purpose:** Triage `PENDING_REVIEW` Courses/revisions.

- **Content:** Course, Instructor, first publication/revision, submitted time, readiness summary.
- **Actions:** Open Content Review, filter/sort.
- **States:** Loading, empty, stale/conflict.

## AD05 — Content Review

**Purpose:** Review Course content and apply an audited publication/moderation transition.

- **Content:** Metadata, outline, Resources/Labs/Preview, audited video player, revision diff, history.
- **Actions:** Publish, request changes with reason, unpublish/republish, archive when allowed.
- **States:** Reviewing, applying, Published, Changes Requested, Unpublished, conflict/failure.
- **Constraints:** No partial publish; Admin preview never creates Student Entitlement.

## AD06 — Coupons

**Purpose:** Create/manage discounts and inspect history.

- **Fields:** Code, percentage/fixed value, validity, Course/Section targets, global cap, active.
- **Actions:** Create/edit/deactivate; delete only with no redemption; view redemption/refund history.
- **States:** Draft validation, active/inactive/expired, cap reached, frozen redeemed value fields.
- **Constraints:** No configurable per-user limit; one consuming redemption per Student.

## AD07 — Revenue and Order Detail

**Purpose:** Reconcile Orders, Payment Attempts, Entitlements, Refunds, and Instructor accounting.

- **Content:** Amount snapshots, gateway references/status, coupon, Entitlement, refund/earning lines,
  reconciliation warning.
- **Actions:** Search/filter/export as permitted, open Refund/Payout context.
- **States:** Pending/paid/free-granted/failed/unknown/partially refunded/refunded.

## AD08 — Refunds

**Purpose:** Request and track policy-eligible full/partial refunds.

- **Content:** Order/captured/refunded/remaining balance, method capability, accepted policy version,
  existing Refunds, payout impact.
- **Fields:** Integer-fils amount, required reason.
- **Actions:** Submit idempotently, refresh/reconcile status.
- **States:** Requested/pending/succeeded/failed/cancelled; partial/full cumulative outcome.
- **Constraints:** No Entitlement/revenue effect before confirmed gateway success.

## AD09 — Payouts

**Purpose:** Prepare monthly statements and record manual bank transfers.

- **Content:** Configured global share (or blocking unconfigured state), eligible Orders, fees,
  refunds/chargebacks, prior adjustments, payable total, statement history.
- **Actions:** Generate/review, approve, mark Paid with required reference, email statement, void an
  unpaid statement with reason.
- **States:** Draft/Approved/Paid/Void; failed email does not undo Paid state.
- **Constraints:** No automated transfer or Instructor UI.

## AD10 — Reported Content

**Purpose:** Resolve Student reports without automatic takedown.

- **Content:** Target, reporter, reason/note, Course/Instructor, related reports, current state/history.
- **Actions:** Start review, dismiss, request changes, unpublish, suspend Account; record reason.
- **States:** Open/Under Review/Resolved Dismissed/Resolved Actioned.

## AD11 — Office-Hours Moderation

**Purpose:** Inspect and cancel inappropriate/invalid Course sessions.

- **Content:** Course/Instructor/session/link/schedule/status and audit history.
- **Action:** Cancel with required reason.
- **Constraints:** No Admin create/platform-wide session action.

## AD12 — Catalog Taxonomy

**Purpose:** Maintain the Major and Subject vocabularies that classify and filter the catalog.

- **Content:** Major and Subject term lists with Arabic/English labels, Subject academic code,
  active/retired state, referencing-Course count, and audit history.
- **Actions:** Create term, edit bilingual labels/code, retire/restore, delete an unreferenced term,
  open referencing Courses, override a Course's classification.
- **States:** Saving, duplicate-label validation, retire confirmation, delete blocked while
  referenced, empty vocabulary.
- **Rules:** Admin only (BR-158). Renaming changes labels everywhere and never rewrites assigned
  Courses; retiring blocks new assignment but leaves existing Courses filterable; a referenced term
  cannot be deleted (BR-159/160). Every change is audited.
- **Constraints:** Study Year is a fixed enumeration and is not editable here. No fourth
  classification dimension, no free-text tags, and no Instructor access.

---

# Screen Index

| ID | Screen | Audience |
|---|---|---|
| S01–S10 | Shared/auth/legal/system screens | Public or role-aware |
| ST01–ST10 | Catalog through Orders/Refunds | Student/public where stated |
| IN01–IN08 | Course operations, analytics, office hours | Instructor |
| AD01–AD12 | Users, pricing, moderation, commerce, payouts, catalog taxonomy | Admin |

Modal/drawer states: Public Preview · Report Content · coupon result · confirmation dialogs ·
unsaved/upload warning. External destinations: Tap hosted checkout · Discord/Telegram · meeting link.
