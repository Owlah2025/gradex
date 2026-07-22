# SCREENS

> Status: Draft
> Last Updated: 2026-07-21

Source-of-truth screen inventory for the Gradex **MVP**. One entry per screen: what it's for, how you get in/out, what it's made of, what states it can be in, who may see it. This is the contract between product (what the screen is for), design (how it looks), and engineering (what to build).

**Source chain:** [PROJECT_VISION](PROJECT_VISION.md) → [PRD](PRD.md) → [BUSINESS_RULES](BUSINESS_RULES.md) → [USER_JOURNEYS](USER_JOURNEYS.md) → **SCREENS** → Navigation Map → Wireframes → UI Mockups.

Rule references: `BR-xxx` → [BUSINESS_RULES.md](BUSINESS_RULES.md), `D-xxx` → [DECISIONS.md](DECISIONS.md), `T#` → [USER_JOURNEYS.md](USER_JOURNEYS.md).

---

## Conventions

- **Permissions:** `Public` (no auth) · `Authenticated` (any signed-in role) · `Student` · `Instructor` · `Admin`.
- **Global chrome (not repeated per screen):** top nav (logo, role-aware links, Notification bell → Notification Center, account menu), footer (Legal links). Student surfaces are **mobile-first, single column**; instructor & admin surfaces are **desktop-first working views**. Per-screen chrome (header/bottom-nav/breadcrumb/sidebar/back), responsive targets, and behavior rules (guarded URLs, unsaved changes, back semantics) live in [NAVIGATION_RULES.md](NAVIGATION_RULES.md).
- **Every screen implicitly has** a `Loading` and an `Error` state; only screen-specific or notable states are re-listed below.
- **Sub-states & modals (NOT standalone screens)** — deliberately demoted to reduce navigation complexity:
  - **Lesson Preview** → modal/player on *Course Details*.
  - **Payment Processing** ("confirming payment…") & **Payment Failed / retry** → states of *Checkout* (BR-020, BR-022).
  - **Community link-out** → external action/card on *Course Home* / *Lesson Resources* (T7).
  - **Review Outcome** (approved/rejected + reason) → *Notification Center* entry + status on *Instructor Dashboard* (T6, BR-071/072).

**Count: 34 standalone screens** — 10 shared · 9 student · 7 instructor · 8 admin.

---

# Shared / System

## Landing

**Purpose**
Market Gradex, surface featured courses, route visitors to catalog or auth.

**Entry**
- Direct / marketing link (root URL)
- Logout redirect

**Exit**
- Catalog
- Login
- Register
- Course Details (featured card)

**Components**
- Hero + value proposition
- Featured / popular courses strip
- How-it-works / differentiators (labs, community, follow-up)
- Primary CTAs (Browse, Sign up)
- Footer (Legal)

**States**
- Default
- Loading (featured strip)
- Empty (thin catalog at launch, 8–12 courses)

**Permissions**
Public

---

## Login

**Purpose**
Authenticate an existing user; return them to where they were headed.

**Entry**
- Landing / global nav
- Register ("already have an account")
- Any auth-gated action (e.g. Buy → deep-link back to Checkout, T3)

**Exit**
- Role-based redirect: Student Dashboard / Instructor Dashboard / Admin Ops Landing
- Deep-linked return target (e.g. Checkout)
- Forgot Password
- Register

**Components**
- Email + password fields
- Submit
- "Forgot password?" link
- Link to Register
- Inline error area

**States**
- Default
- Invalid credentials — 401, no email-exists leak (BR-003)
- Account suspended — blocked (BR-007)
- Submitting

**Permissions**
Public

---

## Register

**Purpose**
Create a student account with minimum friction, without losing the chosen course.

**Entry**
- Landing / global nav
- Login ("create account")
- Buy action when logged out (T3)

**Exit**
- Role-based redirect / deep-linked return (e.g. Checkout)
- Login

**Components**
- Email + password (+ confirm) fields
- Submit
- Link to Login
- Inline error area
- Legal consent (Terms / Privacy)

**States**
- Default
- Duplicate email — 409 (BR-001)
- Submitting

**Notes**
No self-serve **instructor** signup in MVP — instructors are recruited manually (T1).

**Permissions**
Public

---

## Forgot Password

**Purpose**
Start password recovery by email.

**Entry**
- Login

**Exit**
- Login (after request sent)
- Reset Password (via emailed link)

**Components**
- Email field
- Submit
- Confirmation message

**States**
- Default
- Sent (generic confirmation — no email-exists leak)
- Submitting

**Permissions**
Public

---

## Reset Password

**Purpose**
Set a new password from an emailed one-time link.

**Entry**
- Password-reset email link

**Exit**
- Login

**Components**
- New password (+ confirm) fields
- Submit

**States**
- Default
- Invalid / expired token
- Success
- Submitting

**Permissions**
Public (token-scoped)

---

## Account / Settings

**Purpose**
Manage credentials and account-level preferences.

**Entry**
- Global account menu

**Exit**
- Back to originating dashboard

**Components**
- Change email
- Change password
- Notification preferences
- Session / logout

**States**
- Default
- Saving
- Save error

**Permissions**
Authenticated

---

## Profile

**Purpose**
View / edit personal display info.

**Entry**
- Global account menu
- Dashboard

**Exit**
- Account / Settings

**Components**
- Name / display fields
- Editable form
- Save

**States**
- View
- Edit
- Saving

**Permissions**
Authenticated

---

## Notification Center

**Purpose**
Central list of transactional notifications (payment receipt, video ready, review outcome, approvals) — email + in-app, best-effort (BR-120, PRD §Notifications).

**Entry**
- Global nav bell (unread badge)

**Exit**
- Deep-link to the related screen (receipt, course, lesson, moderation item)

**Components**
- Unread badge
- Notification list (read/unread)
- Item → deep link
- Mark-as-read

**States**
- Default
- Empty (no notifications)
- Loading

**Permissions**
Authenticated (content scoped per role)

---

## Legal (Terms / Privacy / Refund Policy)

**Purpose**
Static compliance content; refund policy referenced at checkout (Kuwait Consumer Protection Law, 14-day right + digital-once-accessed exemption, BR-044).

**Entry**
- Footer
- Checkout
- Register (consent)

**Exit**
- Back

**Components**
- Static content sections
- Section nav / anchors

**States**
- Default

**Permissions**
Public

---

## System Error & Empty States

**Purpose**
Standardized fallbacks: not-found, server error, and **access-denied / expired enrollment** (403 mid-watch, BR-023).

**Entry**
- Any failed route / expired-access authorization

**Exit**
- Home / Dashboard / Catalog
- Re-buy via Checkout (after expiry, BR-024/025)

**Components**
- Status message
- Cause-appropriate CTA (retry / go home / re-purchase)

**States**
- 404 Not Found
- 500 Server Error
- 403 Access Denied / Enrollment Expired
- Offline / network

**Permissions**
Public

---

# Student

> Mobile-first, single column.

## Catalog

**Purpose**
Browse published courses and narrow to a university subject/level (T1).

**Entry**
- Landing
- Global nav
- Student Dashboard

**Exit**
- Course Details
- Search Results

**Components**
- Search bar
- Filters (major, year/level)
- Course grid (thumbnail, title, instructor, price, access term)
- Pagination / infinite scroll

**States**
- Default
- Loading
- Empty (thin launch catalog / no results for filter)
- Error (slow 4G — target p95 < 2.5s LCP)

**Permissions**
Public

---

## Search Results

**Purpose**
Show courses matching a query (T1). Shares layout with Catalog.

**Entry**
- Catalog search bar
- Global nav search

**Exit**
- Course Details
- Catalog (clear search)

**Components**
- Query field (persisted term)
- Filters
- Result grid
- Pagination

**States**
- Results
- Empty ("subject not covered")
- Loading
- Error

**Permissions**
Public

---

## Course Details

**Purpose**
Let a student evaluate a course and decide to buy the whole course or a single chapter (T2).

**Entry**
- Catalog / Search Results
- Landing featured card
- Deep link / share

**Exit**
- Checkout (Buy course / Buy chapter)
- Login / Register (if logged out → return here)
- Course Home (if already owned → "Go to course", BR-024)

**Components**
- Title, instructor, price, **access-until** (concrete date, not "150 days")
- Outline (sections → lessons)
- **Preview** trigger → Lesson Preview modal
- Included labs / resources / community callout (value legibility, Risk 4)
- Buy CTA (course or chapter scope)
- Sample lab download

**States**
- Default (purchasable)
- Owned → "Go to course" instead of Buy (BR-024)
- Unpublished / unavailable
- Price mid-change → last-approved price shown (BR-017)
- Loading

**Notes**
**Lesson Preview** is a modal here, not a standalone screen.

**Permissions**
Public (Buy requires auth)

---

## Checkout

**Purpose**
Confirm the item and pay via hosted card/KNET; grant access only on webhook success (T4, BR-020).

**Entry**
- Course Details (Buy)
- Login / Register (deep-link return)

**Exit**
- Payment Success / Receipt (on webhook success)
- Course Details (cancel)
- Retry (stay, on failure)

**Components**
- Order summary (item, scope, price, access term)
- Payment method (card / KNET) → hosted gateway page/redirect
- Refund-policy link (Legal)
- Pay CTA
- Idempotency-keyed order (BR-020)

**States**
- Review / ready
- **Processing** — "confirming payment…" holding state, webhook may lag (BR-020) — never a false failure
- Success → redirect to Receipt
- **Failed / declined / cancelled / timeout** — no access, clear retry (BR-022)
- Already-enrolled block (re-buy active enrollment refused, BR-024)

**Notes**
Processing and Failed are **states here**, not separate screens.

**Permissions**
Student

---

## Payment Success / Receipt

**Purpose**
Confirm access is granted and give an in-app receipt; one-tap into first lesson (T4, BR-121).

**Entry**
- Checkout (webhook success)
- Notification Center (receipt notification)

**Exit**
- Course Home / first Lesson Player ("Start learning")
- Student Dashboard

**Components**
- Confirmation + receipt details (item, amount, date, txn ref)
- "Start first lesson" CTA
- Link to Dashboard

**States**
- Default
- Loading (post-redirect reconcile)

**Permissions**
Student

---

## Student Dashboard

**Purpose**
Home base for a signed-in student: what they own, where to resume (T5/T8).

**Entry**
- Post-login redirect
- Global nav
- Receipt

**Exit**
- Course Home
- Catalog (browse more)
- Profile

**Components**
- "Continue learning" (resume last lesson/position)
- My Courses list (progress %, access-until)
- Empty-state CTA → Catalog

**States**
- Default (has courses)
- Empty (no enrollments → browse)
- Near-expiry indicator (silent expiry in MVP, D-009)
- Loading

**Permissions**
Student

---

## Course Home

**Purpose**
Per-course map: understand what was bought and where to start (T5).

**Entry**
- Student Dashboard
- Receipt
- Course Details (owned)

**Exit**
- Lesson Player
- Lesson Resources & Labs
- Community link-out (external)

**Components**
- Course header (progress, access-until)
- Section → lesson list with per-lesson lock/complete markers
- "Start here" / resume CTA
- Estimated time-to-complete
- Resources / labs entry
- **Community link-out** card (external Discord, T7)

**States**
- Default
- Chapter-only purchase → non-owned lessons visibly **Locked** (BR-021/023)
- Progress load error
- Near / past expiry (access ends silently, D-009)

**Permissions**
Student (enrollment-scoped)

---

## Lesson Player

**Purpose**
Watch a lesson smoothly and never lose place (T6).

**Entry**
- Course Home
- Student Dashboard ("Continue")
- Receipt ("Start first lesson")

**Exit**
- Next / previous lesson (auto-advance)
- Course Home
- Lesson Resources & Labs

**Components**
- HLS adaptive video player (play/pause, seek, quality, fullscreen, speed) — keyboard-operable
- Resume banner ("Pick up at 12:04")
- Progress / auto-complete at ≥90% (BR-051)
- Lesson list / next-up
- Resources tab entry

**States**
- Playing / paused
- Resume prompt
- Buffering
- Signed-URL refresh (silent, no interruption, BR-053/100)
- **Access denied** — expired enrollment mid-watch (BR-023) → System 403
- Playback error (CDN/storage outage — distinguishable)

**Permissions**
Student (enrollment-scoped playback authz, BR-050)

---

## Lesson Resources & Labs

**Purpose**
Download lesson resources (slides/notes) and lab materials (project + guide) to practice (T7).

**Entry**
- Lesson Player (Resources tab)
- Course Home

**Exit**
- Back to Lesson Player / Course Home
- Community link-out (external)

**Components**
- Resources list (slides/notes) — download
- Lab materials list (project + guide) — download, buyer-tagged (BR-103, D-011)
- Per-item size/type
- "Mark lab done"
- Lab setup checklist

**States**
- Default
- Empty (no materials for lesson)
- Entitlement / expiry re-checked per download (BR-023)
- Download link expired → re-issue
- Error

**Notes**
May render as a **tab within Course Home / Lesson Player** rather than a distinct route — decide at wireframe stage.

**Permissions**
Student (entitlement-scoped)

---

# Instructor

> Desktop-first working views. Instructor accounts are provisioned manually (T1). **No earnings/payout figures anywhere instructor-facing** (BR-064/074, D-006).

## Instructor Dashboard

**Purpose**
Overview of the instructor's courses and their lifecycle status; entry to build, review outcomes, analytics (T1/T7).

**Entry**
- Post-login redirect
- Global nav

**Exit**
- Course Builder
- Course Analytics
- Payout Statements
- Notification Center (review outcome)

**Components**
- Course list with status (Draft / Pending Approval / Published / Pending-Revision)
- **Review outcome** surfacing (approved / rejected + reason) — status + notification, not its own screen (BR-071/072)
- "New course" CTA
- Per-course quick actions (edit, analytics)

**States**
- Default
- Empty (no courses yet)
- Rejected banner (reason shown, back to Draft, editable — BR-072/015)
- Read-only while Pending (BR-016)
- Loading

**Permissions**
Instructor (own courses only, BR-060)

---

## Course Builder

**Purpose**
Model and edit the course as Course → Section → Lesson; set price (T2).

**Entry**
- Instructor Dashboard (New / Edit course)

**Exit**
- Lesson Editor
- Resources & Labs Manager
- Submit for Review

**Components**
- Course metadata (title, description, price in 30–60 KWD band)
- Section list (add / reorder / delete)
- Lesson list per section (add / reorder / delete)
- Autosave indicator
- Live student-preview

**States**
- Draft (invisible until published, BR-011)
- Editing / autosaving
- Pending-revision (edits to a live course queue for re-approval; live stays unchanged, BR-017/090)
- Read-only while Pending Approval (BR-016)
- Save / reorder error

**Permissions**
Instructor (own courses, BR-060)

---

## Lesson Editor

**Purpose**
Get a watchable lesson: upload raw video, track transcode to READY (T3).

**Entry**
- Course Builder (open lesson)

**Exit**
- Course Builder
- Resources & Labs Manager

**Components**
- Lesson title / metadata
- Video upload (drag-drop, resumable)
- Per-stage status (Uploading → Processing → Ready)
- Replace / re-upload
- Preview

**States**
- Empty (no video)
- Uploading (progress)
- Processing / transcoding
- Ready
- **Failed** — auto-retry 3× then manual retry (BR-091)
- Over `MAX_UPLOAD_SIZE_BYTES` rejected
- Stuck-UPLOADING (reaper) surfaced

**Permissions**
Instructor (own courses)

---

## Resources & Labs Manager

**Purpose**
Attach downloadable materials to a lesson in two separate buckets (T4, D-011).

**Entry**
- Lesson Editor
- Course Builder

**Exit**
- Course Builder / Lesson Editor

**Components**
- Resources uploader (slides/notes — ≤50 MB/file, 200 MB/lesson)
- Lab materials uploader (project + guide — ≤250 MB/file, 1 GB/lesson)
- File list per bucket (replace in place, no versioning, BR-066)
- Instant size/type validation
- Per-lesson "materials complete" indicator

**States**
- Default
- Uploading
- Wrong type rejected
- Over-cap rejected with message (BR-068)
- Error

**Permissions**
Instructor (own courses)

---

## Submit for Review

**Purpose**
Submit a complete course for admin approval with a pre-submit checklist (T5).

**Entry**
- Course Builder

**Exit**
- Instructor Dashboard (course → Pending Approval, BR-070)

**Components**
- Pre-submit checklist (every lesson has a READY video; ≥1 section/lesson)
- Blocking-issues list naming what's missing (BR-012/013)
- "Fix" jump-links to gaps
- Submit CTA

**States**
- Ready to submit
- Blocked (missing content / video still transcoding)
- Submitting

**Permissions**
Instructor (own courses)

---

## Course Analytics

**Purpose**
See how a published course performs — engagement only, **no revenue** (T7, BR-064).

**Entry**
- Instructor Dashboard

**Exit**
- Course Builder (iterate content)

**Components**
- Per-course enrollments count
- Completion rate
- Per-lesson completion funnel (drop-off)
- Student roster (own students)

**States**
- Default
- Empty (no enrollments yet)
- Loading / analytics lag

**Permissions**
Instructor (own courses; **never** earnings, BR-074)

---

## Payout Statements

**Purpose**
Access periodic manual payout statements — informational, no live earnings dashboard in MVP (T8, D-006).

**Entry**
- Instructor Dashboard

**Exit**
- Download (PDF / CSV)

**Components**
- Statement list by cycle
- Download link per statement
- Payout cadence explainer (trust, Risk 6)

**States**
- Default
- Empty (no statements yet)

**Notes**
Displays issued statements only — **no per-course earnings figures** in the live UI (BR-074, D-006).

**Permissions**
Instructor (own statements)

---

# Admin

> Desktop-first. Only admins see student PII (BR-101). Privileged actions are audited.

## Admin Ops Landing

**Purpose**
"What needs attention today" — queues and exceptions at a glance (T1).

**Entry**
- Post-login redirect
- Global nav

**Exit**
- Moderation Queue
- Refunds
- Payouts
- User Management
- Revenue Dashboard

**Components**
- Moderation queue depth
- Pending refunds count
- Failed transcodes count
- Quick links to each ops area

**States**
- Default
- All-clear (empty queues)
- Loading

**Permissions**
Admin

---

## Moderation Queue

**Purpose**
Clear the review backlog of Pending Approval courses (T2, BR-070).

**Entry**
- Admin Ops Landing
- Global nav

**Exit**
- Content Review

**Components**
- Queue list (course, instructor, submitted-at, **age/SLA**)
- Priority sort
- Bulk triage
- Open → Content Review

**States**
- Default
- Empty (queue clear)
- Launch-week batch (8–12 at once, Risk 5)
- Loading

**Permissions**
Admin

---

## Content Review

**Purpose**
Preview any lesson (incl. Draft/Pending) and approve or reject with a reason (T3/T4).

**Entry**
- Moderation Queue

**Exit**
- Moderation Queue (after decision)

**Components**
- Course outline
- **Audited** admin lesson preview player (logged: admin ID, lesson, timestamp; no enrollment, BR-081)
- Reviewer checklist overlay
- Approve → Published + notify (BR-071)
- Reject → **required reason**, back to Draft (BR-072)
- Reason templates

**States**
- Reviewing
- Approving (atomic publish; must not end half-visible)
- Rejecting (reason required)
- Pending-revision review → applies changes atomically to live course (BR-017/090)

**Permissions**
Admin

---

## User Management

**Purpose**
Keep the platform safe — view users, suspend students/instructors (T5).

**Entry**
- Admin Ops Landing
- Global nav

**Exit**
- User detail / action confirm

**Components**
- User list / search (PII admin-only, BR-101)
- Role filter
- Suspend / reinstate (reason + audit trail)
- User detail

**States**
- Default
- Suspend confirm (reason required)
- Suspended view
- Loading

**Notes**
Suspending a **student** kills access despite prior purchases (BR-007); suspending an **instructor** does **not** revoke enrolled students' access (BR-065).

**Permissions**
Admin

---

## Revenue Dashboard

**Purpose**
Platform financial health — platform-wide revenue/payments (T6).

**Entry**
- Admin Ops Landing
- Global nav

**Exit**
- Refunds (drill-in)

**Components**
- Revenue over period
- Per-course revenue
- Refund / chargeback trend
- **Reconciliation view** (webhook vs record desync, Risk 1)

**States**
- Default
- Loading
- Reconciliation-flag / desync warning

**Permissions**
Admin

---

## Refunds

**Purpose**
Refund fairly and safely; revoke access only after gateway confirms (T7, BR-040/041).

**Entry**
- Admin Ops Landing
- Revenue Dashboard
- User / order lookup

**Exit**
- Refund confirm
- Payouts (clawback flag)

**Components**
- Order lookup
- Inline **policy check** (streamed? file opened? — 14-day right minus digital-once-accessed, BR-044)
- Full / partial refund (scoped to item, BR-043)
- Refund action → gateway call
- Audit log / export (BR-042)

**States**
- Eligible / ineligible (policy)
- **Pending-refund** (access retained until gateway confirms, BR-041)
- Confirmed → access revoked
- Payout-already-paid → **flag for clawback** (BR-043)
- Gateway refund failed → do not revoke; reconcile

**Permissions**
Admin (refunds are admin-only, BR-040)

---

## Payouts

**Purpose**
Pay instructors correctly — itemized, fees + refunds pre-deducted, approve → paid (T8, BR-073).

**Entry**
- Admin Ops Landing
- Global nav

**Exit**
- Payout statement generated (feeds instructor Payout Statements)

**Components**
- Payout run itemized by course/purchase
- Fees + refunds pre-deducted
- Reconciliation flags before "Paid"
- Approve → mark Paid with reference
- Auto-generated statement (PDF/CSV) per instructor

**States**
- Draft run
- Approved
- Paid (with reference)
- Clawback pending (refund after Paid → manual next cycle, BR-043)
- Mismatch vs gateway flagged

**Notes**
Earnings live here, in **admin** — never exposed to the instructor UI (BR-074).

**Permissions**
Admin

---

## Reported Content

**Purpose**
Moderate reported courses/materials (T8).

**Entry**
- Admin Ops Landing
- Global nav

**Exit**
- Content Review / action

**Components**
- Reports list (target, reporter, reason, date)
- Open reported item
- Action (dismiss / take down / warn)
- Audit trail

**States**
- Default
- Empty (no reports)
- Loading

**Permissions**
Admin

---

## Screen index

| # | Screen | Role | Permission |
|---|--------|------|------------|
| 1 | Landing | Shared | Public |
| 2 | Login | Shared | Public |
| 3 | Register | Shared | Public |
| 4 | Forgot Password | Shared | Public |
| 5 | Reset Password | Shared | Public |
| 6 | Account / Settings | Shared | Authenticated |
| 7 | Profile | Shared | Authenticated |
| 8 | Notification Center | Shared | Authenticated |
| 9 | Legal | Shared | Public |
| 10 | System Error & Empty States | Shared | Public |
| 11 | Catalog | Student | Public |
| 12 | Search Results | Student | Public |
| 13 | Course Details | Student | Public |
| 14 | Checkout | Student | Student |
| 15 | Payment Success / Receipt | Student | Student |
| 16 | Student Dashboard | Student | Student |
| 17 | Course Home | Student | Student |
| 18 | Lesson Player | Student | Student |
| 19 | Lesson Resources & Labs | Student | Student |
| 20 | Instructor Dashboard | Instructor | Instructor |
| 21 | Course Builder | Instructor | Instructor |
| 22 | Lesson Editor | Instructor | Instructor |
| 23 | Resources & Labs Manager | Instructor | Instructor |
| 24 | Submit for Review | Instructor | Instructor |
| 25 | Course Analytics | Instructor | Instructor |
| 26 | Payout Statements | Instructor | Instructor |
| 27 | Admin Ops Landing | Admin | Admin |
| 28 | Moderation Queue | Admin | Admin |
| 29 | Content Review | Admin | Admin |
| 30 | User Management | Admin | Admin |
| 31 | Revenue Dashboard | Admin | Admin |
| 32 | Refunds | Admin | Admin |
| 33 | Payouts | Admin | Admin |
| 34 | Reported Content | Admin | Admin |

**Demoted to states/modals (documented in-parent, not standalone):** Lesson Preview · Payment Processing · Payment Failed · Community link-out · Review Outcome.
