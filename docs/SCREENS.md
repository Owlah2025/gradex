# Screens

> Status: Canonical MVP screen contract
> Last Updated: 2026-07-28

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
- No MVP screen exists for checkout, cart, orders, receipts, refunds, coupons, Instructor pricing,
  Instructor earnings/payouts, notification preferences, platform-wide office hours, reviews/ratings,
  recommendations, bundles, or BNPL.

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
- **Events:** Course access granted, invitation issued/rejected, security, staff invitations, Course/revision submission and
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

- **Entry:** Footer, Registration, Course Access Invitation, Profile.
- **Content:** Effective version/date and approved text.
- **Actions:** Switch legal document/language; return to source.
- **Constraints:** No unapproved claim that streaming automatically removes refund rights.

## S10 — System States

**Purpose:** Shared 401/403/404/expired/offline/5xx and empty states.

- **Actions:** Retry, Login, role root, Catalog, Course Details.
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
- **Rules:** Only `PUBLISHED` Courses appear (BR-161). **Under
  [D-091](DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy)
  the filter set becomes University / Program / academic level / Subject; the Major/Subject/Study
  Year set below is the legacy model and is retained until cutover.** Filters are exact-match on one
  value per dimension; taxonomy labels render in the selected language while search matches Arabic and English
  at once with diacritic/alef/digit normalization (BR-162). **Ranking is relevance only, and may use
  the signed-in Student's own academic profile to order otherwise-visible Courses** under
  [D-092](DECISIONS.md#d-092--the-student-academic-profile-persists-academic-unit-context-for-program-less-states-and-records-onboarding-as-an-explicit-three-state-decision)
  §1: the ordering is request-scoped, reads only that Student's own profile, and is deterministic —
  explicit Program target, then inferred Curriculum match, then same Institution, then every other
  public Course. It reorders results and nothing else. **It never changes which Courses are eligible,
  visible, published, purchasable, invitable, entitled, or reachable for learning; it never hides a
  Course whose Program does not match; and anonymous catalogue browsing is never personalized.**
  There is no paid promotion, no sponsored ranking, and no recommendation presented as relevance.
- **Responsive:** Filter sheet on small screens; rail where space allows.

## ST02 — Course Details

**Purpose:** Evaluate a Published Course and learn how to obtain access.

- **Content:** Title, Instructor, authored description/language, outline, Resources/Labs summary,
  office-hours support, community, Course price, access term, and how-to-get-access guidance.
- **Actions:** Play optional Public Preview, open how-to-get-access guidance, submit only an email
  to **I want to buy this Course**, then hand the Student to WhatsApp only after Gradex has persisted
  the Purchase Request; Go to Course if access is active, Login/Register when required.
- **Public Preview state:** Separate validated asset; absent preview removes the control.
- **Locked content:** Lesson titles may be visible, but protected media/files are not public.
- **Constraints:** No checkout, cart, coupon field, Section purchase control, Sample Lab download,
  ratings/reviews, recommendations, bundle, or BNPL CTA. The price is informational — Gradex charges
  nothing. The manual purchase request snapshots it server-side before the external handoff. Section
  prices are not displayed.

## ST03 — Course Access Invitation

**Purpose:** Let the invited Student review and accept an invitation to one Course.

- **Content:** Course identity, inviting message, the invited email address, access term that would
  apply, accepted policy versions, and the correct conditional statement: standard acceptance awaits
  Admin approval; a payment-confirmed purchase-backed invitation activates access on matching
  Student acceptance.
- **Actions:** Accept, decline to act, sign in or register with the invited email.
- **States:** Awaiting acceptance; accepted and awaiting Admin approval; approved and active;
  rejected with reason; cancelled; wrong identity signed in; acceptance link expired with resend.
- **Constraints:** Only an Account whose normalized email matches may accept; any other signed-in
  identity sees a refusal, not the accept control. No payment field appears anywhere on this screen.

## ST04 — Access Status

**Purpose:** Show the Student where a request stands without implying access exists.

- **States:** Awaiting your acceptance, awaiting Admin approval, access active, rejected with reason,
  cancelled, expired.
- **Content:** Course, current state, relevant timestamps, access-until instant when active.
- **Actions:** Go to Course when active; return to catalogue otherwise.
- **Constraints:** No access is offered until an Entitlement exists. Admin notes and any External
  Payment reference are never shown to the Student.

## ST05 — Student Dashboard

**Purpose:** Resume learning and see owned/expired Courses, upcoming office hours, and recent status.

- **Content:** Continue Learning, My Courses, progress, expiry, upcoming sessions, recent
  notifications, and any invitation awaiting acceptance or approval.
- **Actions:** Resume Lesson, open Course Home, Browse, Access History, act on a pending invitation.
- **States:** No access yet/empty, awaiting approval, active, near expiry (without scheduled
  reminder), expired.

## ST06 — Course Home

**Purpose:** Navigate the Course within the Student's exact Entitlement scope.

- **Content:** Course progress, access-until, ordered Sections/Lessons, locked markers, Resources/Labs,
  upcoming office hours. **No community link** — deferred to S18 on 2026-07-29 by
  [D-046](DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch).
- **Actions:** Start/resume Lesson, open allowed material, join authorized office hours, report Course.
- **States:** Active Course Entitlement, expired, Delisted but accessible, emergency access
  suspended.
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
- **Constraints:** No public link; per-Entitlement Lab buyer-identification may be applied
  server-side and must not encode Student PII.

## ST09 — Office Hours

**Purpose:** View Course-scoped sessions the Student is entitled to join.

- **Content:** Course, title, description, localized start/end, status.
- **Actions:** Open Course, join external meeting after authorization.
- **States:** Upcoming, rescheduled, cancelled, completed, empty, entitlement expired.
- **Constraints:** Link is never present in public/unauthorized payloads.

## ST10 — Access History

**Purpose:** View the Student's Course Access Invitations and current entitlements.

- **Content:** Per Course — invitation state, acceptance and decision timestamps, Entitlement term
  and access-until instant, accepted policy versions.
- **Actions:** Open an active Course; contact support about a rejected or missing grant.
- **States:** Empty, awaiting acceptance, awaiting approval, active, rejected, cancelled, expired.
- **Constraints:** No payment, order, receipt, or refund data exists to show. Admin notes, External
  Payment references, and approval evidence are never exposed to the Student.

### Report Content Modal/Drawer

Owned by Course Details only when entitled, Course Home, Lesson Player, and Materials screens.

- **Fields:** Target, fixed reason, explanation (required for “other”).
- **States:** Submitting, accepted, duplicate/rate-limited, error.
- **Effect:** Creates a report; never auto-hides content.

---

# Instructor

## IN01 — Instructor Dashboard and Courses

**Purpose:** See owned Courses and their lifecycle/review state.

- **Content:** Course cards/table, Draft/Pending Review/Changes Requested/Published/Delisted/
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

- **Content:** Pending Course reviews, open reports, Course Access Invitations awaiting approval,
  failed content processing/scans, staff invitation status.
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
- **Constraint:** Price is displayed to Students as what to pay externally; Gradex charges nothing.
  Section prices are maintained here but not displayed in the public catalogue.

## AD04 — Course Review Queue

**Purpose:** Triage `PENDING_REVIEW` Courses/revisions.

- **Content:** Course, Instructor, first publication/revision, submitted time, readiness summary.
- **Actions:** Open Content Review, filter/sort.
- **States:** Loading, empty, stale/conflict.

## AD05 — Content Review

**Purpose:** Review Course content and apply an audited publication/moderation transition.

- **Content:** Metadata, outline, Resources/Labs/Preview, audited video player, revision diff, history.
- **Actions:** Publish, request changes with reason, delist/relist, retire, archive, or invoke/resolve
  constrained emergency access suspension.
- **States:** Reviewing, applying, Published, Changes Requested, Delisted, access suspended,
  conflict/failure.
- **Constraints:** No partial publish; Admin preview never creates Student Entitlement.

## AD06 — Course Access Invitations

**Purpose:** Create standard Course Access Invitations after confirming External Payment, work the
approval queue, and manage automatic Purchase Requests.

- **Content:** Standard-invitation queue filtered by state; per invitation the Student email, Course, creating Admin,
  current state, and timestamps; optional Admin note and opaque external reference.
- **Fields (create):** Student email, one Course, optional Admin note, optional external reference.
- **Actions:** Create, approve, reject with a required reason, cancel before a decision, resend the
  acceptance link.
- **States:** Awaiting student acceptance, awaiting admin approval, approved, rejected, cancelled;
  duplicate non-terminal invitation refused; Course missing a future access-expiry instant blocks
  approval.
- **Constraints:** **Approval is the only action that grants access.** It requires the course-access
  capability and valid recent authentication, and is refused — not degraded — without them. The
  Purchase Requests panel displays request reference, email, Course title, historical price snapshot,
  requested time and factual state, and can only confirm payment/send the linked invitation. Every
  transition is audited.

## AD07 — Entitlement Detail

**Purpose:** Inspect a granted Entitlement. **Read-only.**

- **Owning slice:** S6 builds the read surface. **Every mutation on this screen belongs to S8 Admin
  Operations** — expiry extension, expiry shortening, and revocation are S8's alone, and S6 ships
  none of them. One owner per mutation.
- **Content:** Student, Course, grant source, originating invitation, `original_access_ends_at`,
  current effective `access_ends_at`, adjustment history, revocation state.
- **Actions (S6):** View; open the originating invitation.
- **Actions (S8):** Extend or shorten expiry with a required reason; revoke with a required reason —
  the audited elevated adjustment under BR-026.
- **States:** Active, expired, revoked.
- **Constraints:** No screen may create an Entitlement directly — creation is the authorised standard
  approval or the server-side purchase-backed acceptance transaction only.
  `original_access_ends_at` is never editable by any actor in any slice.

## AD08, AD09 — Refunds and Payouts — deferred out of MVP

These screens described in-platform refund processing and monthly payout statements. Both are
deferred with in-platform payments under
[D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
The identifiers are reserved rather than reused so existing references keep their meaning. Coupons
previously occupied AD06 and are deferred with them; AD06 and AD07 above are new screens, not
renamed ones.

## AD10 — Reported Content

**Purpose:** Resolve Student reports without automatic takedown.

- **Content:** Target, reporter, reason/note, Course/Instructor, related reports, current state/history.
- **Actions:** Start review, dismiss, request changes, delist, retire, emergency-access-suspend, or
  suspend Account; record exact reason/action.
- **States:** Open/Under Review/Resolved Dismissed/Resolved Actioned.

## AD11 — Office-Hours Moderation

**Purpose:** Inspect and cancel inappropriate/invalid Course sessions.

- **Content:** Course/Instructor/session/link/schedule/status and audit history.
- **Action:** Cancel with required reason.
- **Constraints:** No Admin create/platform-wide session action.

## AD12 — Catalog Taxonomy *(legacy; superseded by AD13 on cutover)*

> **[D-091](DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy) supersedes this screen's model.**
> It remains operational until the Academic Catalog migration is proven on a dual path. The
> canonical replacement is AD13 below.

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

## AD13 — Academic Catalog / الكتالوج الأكاديمي

**Purpose:** Maintain the canonical Institution-scoped Academic Catalog that Courses classify
against and that Students are personalised by. Separate from Course review (AD10).

- **Content:** Institution list; per Institution an Academic Unit tree (Colleges/Departments),
  Programs with their owning unit, each Program's active Curriculum and its Subject mappings, and
  the Institution's Subject list with official code, bilingual titles, active/retired state, and
  usage counts.
- **Actions:** Create/edit/retire an Institution, Academic Unit (including safe re-parenting),
  Program, Curriculum, and Subject; map a Subject into a Curriculum with requirement kind and
  recommended level/semester; unmap.
- **States:** Empty catalog (the launch state — must render cleanly with a clear first action),
  loading, saving, duplicate-Subject conflict naming the existing Subject, invalid hierarchy
  rejection, retired-entity display, error.
- **Rules:** Admin only (BR-180). Every mutation is audited. Cross-Institution relationships are
  refused server-side, not merely hidden in the UI (BR-173–BR-176). A duplicate Subject is refused
  deterministically by the database (BR-175). Retirement is soft (BR-180).
- **Vocabulary:** الجامعة · الكلية · القسم · التخصص · الخطة الدراسية · المادة. **Never** display a
  UUID, a revision identifier, or the word "taxonomy"/"تصنيف" as user workflow.
- **Constraints:** No Kuwait University data is hardcoded; launch catalog data is loaded separately.
  No Course read or write path depends on this screen until the migration cutover.

---

# Screen Index

| ID | Screen | Audience |
|---|---|---|
| S01–S10 | Shared/auth/legal/system screens | Public or role-aware |
| ST01–ST10 | Catalog, Course details, access invitation and status, learning, office hours, access history | Student/public where stated |
| IN01–IN08 | Course operations, analytics, office hours | Instructor |
| AD01–AD07, AD10–AD13 | Users, pricing, course review, course-access invitations, entitlements, moderation, legacy catalog taxonomy, Academic Catalog | Admin |

Modal/drawer states: Public Preview · Report Content · invitation accept/reject confirmation ·
confirmation dialogs · unsaved/upload warning. External destination: meeting link. **There is no
external checkout destination in MVP, and no screen links to a Discord/Telegram Course community** —
that link is deferred to S18 by
[D-046](DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch).
