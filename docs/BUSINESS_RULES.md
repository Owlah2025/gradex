# Business Rules

> Status: Active
> Last Updated: 2026-08-05

This document is the single source of truth for Gradex's business logic — the rules governing users, courses, course access and entitlement, video/progress, instructors, admin actions, access control, content lifecycle, and data integrity.

> **MVP scope note (2026-07-28, [D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)).** Gradex launches with **no in-platform payments**. Rules covering checkout, orders, payment attempts, refunds, coupons, and Instructor payouts are marked *(Deferred, outside MVP)* and retained as the design of record for when those features are taken up — they are deferred, not rejected. Course access is granted by the Course Access Invitation workflow in [§21](#21-course-access-invitation-rules) and authorised through the Entitlement rules in [§3](#3-course-access-and-entitlement-rules).

Most rules below are extracted from [PRD.md](PRD.md) (and, where PRD.md explicitly defers to it, [video-streaming-design.md](superpowers/specs/2026-07-17-video-streaming-design.md)). A smaller set fill real gaps the PRD was silent on — those are tagged "Decision" or "new," dated, and cross-referenced to [DECISIONS.md](DECISIONS.md) where significant enough to warrant a log entry. Either way, nothing here is silently assumed — each rule cites where it comes from.

Business rules state *what must always be true*; they intentionally omit tunable technical parameters (TTLs, retry counts, backoff schedules, cleanup windows) — those belong to the implementation spec that owns them, since changing a number there doesn't change the business. Acceptance Criteria in [PRD.md §11](PRD.md) are the testable Given/When/Then specs for these same rules — each AC bullet is tagged with the BR(s) it implements, so the two stay linked instead of silently drifting apart.

---

## 1. User & Auth Rules

- **BR-001** — Public self-registration creates Student accounts only. Email addresses are unique after normalization, but signup, verification, and recovery responses do not reveal whether an address is already registered. *(D-014; PRD §11 Authentication)*
- **BR-002** — Passwords accept 15–128 Unicode characters (including spaces), have no composition or periodic-rotation rule, and are rejected when common or known-compromised. They are hashed with Argon2id before storage; neither plaintext nor a hash is returned by any API. *(D-014; PRD §6 Security, §11 Authentication)*
- **BR-003** — Authentication failure returns a generic unauthorized response without revealing whether the email exists. *(PRD §11 Authentication)*
- **BR-004** — Successful login creates an independently revocable server-authoritative session and issues one opaque credential in a `Secure`, `HttpOnly`, host-only cookie. Controlled renewal rotates that credential and its session-bound CSRF token; no authentication bearer is exposed to browser JavaScript or browser persistence. *(D-034; PRD §11 Authentication, §6 Security)*
- **BR-005** — An expired, revoked, or superseded session credential cannot authenticate or renew access. Confirmed reuse after rotation revokes the entire session family and requires reauthentication. *(D-034; PRD §11 Authentication)*
- **BR-006** — Ordinary logout revokes the current server-side session family before clearing the browser cookie; subsequent use of any family credential is rejected. *(D-014/D-034; PRD §11 Authentication)*
- **BR-007** — A suspended account immediately loses all protected platform access, including from already active sessions, regardless of any active Entitlement, which is not mutated. Disabling a Student account is this transition; there is no separate disabled state. The later system design may use revocation, token versioning, an account-status check, or an equivalent mechanism, but it must deliver this outcome. *(D-014; PRD §11 Authentication)*
- **BR-008** — A Student account remains `PENDING_VERIFICATION` and cannot sign in until an expiring, single-use email-verification token succeeds. Resend is rate-limited; changing an email requires verification of the new address. *(D-014; PRD §11 Authentication)*
- **BR-009** — An Admin may create an expiring invitation for an Instructor or additional Admin, but no Account exists until valid invitation acceptance atomically consumes the token and creates it with the assigned role. An invitation address already attached to any Account is rejected. Every Account has exactly one role assigned at creation and immutable during MVP; there is no role conversion, identity merge, or multi-role membership. The one bootstrap Admin is created out-of-band during secure deployment, has no credential stored in the repository, and must change the initial password. *(D-014; PRD §11 Authentication)*

## 2. Course Rules

- **BR-010** — A course is structured as Course → Section → Lesson; ordering within that hierarchy is preserved. *(PRD §5 Course Management)*
- **BR-011** — A newly created course starts in Draft status and is invisible to students regardless of content completeness. *(PRD §11 Instructor Course Builder)*
- **BR-012** — A course cannot be submitted for admin review if any lesson is missing its video, or if the course has zero sections/lessons; the instructor sees a validation message identifying what's missing. *(PRD §11 Instructor Course Builder)*
- **BR-013** — A course becomes eligible for submission only once it has at least one section containing at least one lesson with a successfully transcoded video. *(PRD §11 Admin Moderation & Payouts)*
- **BR-014** — Every course has exactly one owning instructor. Ownership can only be reassigned by an admin — instructors cannot transfer or share ownership themselves. *(Formalizes BR-060; new 2026-07-20)*
- **BR-015** — An Instructor may edit an owned Course while it is `DRAFT` or `CHANGES_REQUESTED`, subject to revision rules for previously Published content. *(BR-011, BR-017, BR-072)*
- **BR-016** — A Course in `PENDING_REVIEW` is read-only to its Instructor until the Admin publishes it or requests changes, preventing simultaneous edits during review. *(D-021)*
- **BR-017** — Editing a Published Course's structure, video, or protected attachments never modifies the live approved version directly. It creates a pending revision; the live Course remains Published until the revision clears the same Admin review flow. Pricing is excluded because only Admins control prices under BR-019. *(D-015; PRD §11 Instructor Course Builder)*
- **BR-018** — A course with at least one enrollment can never be permanently deleted — it can only be moved to Archived status (removed from the catalog and from new access grants, but still accessible to already-enrolled students). A course with zero enrollments (e.g. a never-published Draft) may be deleted outright. *(Decision 2026-07-20)*
- **BR-019** — Only Admins can set or change Course and Section catalog prices. Instructors have read-only price visibility. The displayed Course price tells a Student what to pay through External Payment; Gradex charges nothing, and Section prices are retained but not displayed because Section is not an acquirable scope. A price change never mutates an existing Entitlement or its expiry, and records old/new value, acting Admin, reason, and timestamp. *(D-015 as amended by D-045; PRD §5 Admin Features)*

## 3. Course Access and Entitlement Rules

> Rewritten on 2026-07-28 by [D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
> MVP has no in-platform payments. The Course Access Invitation workflow that replaces checkout is in
> [§21](#21-course-access-invitation-rules).

- **BR-020** — All payment activity is **External Payment**: performed and verified outside Gradex by the admin team through its own operational process. Gradex never records a payment transaction, amount, currency, gateway reference, or payment status. An Admin-created Course Access Invitation records that an Admin decided to grant access; it is **never** evidence that payment occurred inside Gradex and must not be modelled as a payment transaction. *(D-045)*
- **BR-021** — A granted Entitlement is scoped to exactly **one complete Course**, not blanket platform access. Section-, Lesson-, bundle-, and partial-course scopes are not acquirable in MVP. Section remains the canonical structural entity and “Chapter” remains only an optional UI label. *(D-045; amends D-015)*
- **BR-022** — *(Deferred with in-platform payments on 2026-07-28. Reserved, not reused.)* The Order lifecycle — `PENDING_PAYMENT`, `PAID`, `FREE_GRANTED`, `CANCELLED`, `EXPIRED`, `RECONCILIATION_REQUIRED`, `PARTIALLY_REFUNDED`, `REFUNDED` — describes a feature outside MVP. No Order entity exists at launch. This number is retained so existing citations keep their original meaning. *(D-028, deferred by D-045)*
- **BR-023** — Entitlement is checked before each signed HLS playback URL and each protected resource/lab download. Lessons outside the entitled Course scope, or accessed after expiry, are denied regardless of whether the file exists. *(PRD §11 Course Access, Video Playback & Progress)*
- **BR-024** — An active Course Entitlement covers every Section and Lesson contained in that Course. At most **one** `ACTIVE` Entitlement may exist per `(Student, Course)`; the constraint is enforced in the database, not in a handler, so a repeated grant is impossible rather than merely unlikely. An Admin adjustment cannot extend an Entitlement into conflicting later coverage. *(D-045; formalizes BR-021)*
- **BR-025** — Before a Course Access Invitation for a Course can be **approved**, an Admin must have configured a future Course `default_access_ends_at` instant. Admin Approval snapshots that exact instant onto the Entitlement; changing the Course default afterwards affects only future approvals. When an Admin enters a Kuwait-local calendar date, the platform persists the exclusive boundary as the first instant of the following local day converted to UTC. The Entitlement is authoritative at runtime: access is allowed only while `current_timestamp < entitlement.access_ends_at` and otherwise is expired. *(D-026 as amended by D-045; supersedes D-009)*
- **BR-026** — An Entitlement preserves `original_access_ends_at` snapshotted at Admin Approval and a separately mutable effective `access_ends_at`. An elevated Admin may extend or shorten the effective instant only through an adjustment that atomically records old expiry, new expiry, reason, actor, timestamp, and any applicable support reference, with immutable Audit evidence and a transactional Student-notification event. Moving expiry into the past ends access immediately but never deletes Enrollment, Progress, Invitation, or adjustment history. *(D-026/D-045)*
- **BR-027** — A retired Course, Section, Lesson, or authored version blocks future acquisition/inclusion but remains accessible through an otherwise-active Entitlement only when `retirement_eligibility_at`, set from the **Admin Approval instant**, precedes the relevant `retired_at`. Invitation creation time, acceptance time, and record-insertion time are separate and none of them is the eligibility timestamp; a delayed approval cannot bypass retirement. *(D-026/D-045; BR-017/018)*
- **BR-028** — Every Entitlement carries a typed `grant_source` recording how access was granted, and no Entitlement may exist without one. MVP implements exactly one value, `MANUAL_INVITATION`, created only by Admin Approval of an accepted Course Access Invitation under BR-167. `PAID_ORDER`, `PROMOTIONAL`, and `DIRECT_ADMIN_GRANT` are reserved names that are **not implemented** and must not be creatable by any route, command, screen, fixture, or configuration flag in a production build. Admins may adjust an existing Entitlement's expiry under BR-026. *(D-045; supersedes the Order-origin rule in D-027)*
- **BR-029** — A Course Access Invitation is a **workflow record and never an access record**. Playback authorization, protected downloads, Progress writes, Instructor rosters, and every other protected Course operation authorise against the Entitlement alone and must not read Invitation state. Neither account registration, nor email verification, nor External Payment, nor invitation acceptance grants course access. *(D-045)*

## 4. Payment Rules

> **BR-031 through BR-034 are deferred out of MVP** with in-platform payments on 2026-07-28 by
> [D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
> They remain the design of record for whenever a gateway is integrated. BR-030 stays **active** as a
> standing security invariant.

- **BR-030** — Gradex never collects, transmits, or stores raw card/PAN data. In MVP this holds trivially because no payment is entered anywhere in Gradex; it remains binding on any future checkout, which must delegate fully to a PCI-DSS-compliant gateway's hosted page or tokenized SDK. *(PRD §6 Security; scope restated by D-045)*
- **BR-031** — *(Deferred, outside MVP.)* All payment webhooks are validated via signature verification before being trusted, to prevent spoofed "payment succeeded" callbacks. *(PRD §6 Security, §11 Purchase & Payment)*
- **BR-032** — *(Fast-follow, inactive for MVP; now behind the deferred gateway itself.)* If BNPL/installments are later approved, Gradex reflects the gateway-reported collection status and an explicitly approved access policy; it does not reimplement credit/risk or dunning. This rule does not authorize installment fields, states, screens, or flows in MVP. *(D-002, D-008)*
- **BR-033** — *(Deferred, outside MVP.)* At most one `CREATED`, `PENDING`, or `UNKNOWN` Payment Attempt exists per Order. `SUCCEEDED` means verified capture; `FAILED`/`CANCELLED` are provider-confirmed terminal outcomes; `TIMED_OUT` may later reconcile. Attempt state may change, but immutable provider events and transition history preserve every observation. Deadline eligibility uses provider-verified capture time, never arrival time. Duplicate/delayed/reordered events cannot double-record payment, consume Coupon capacity twice, or create more than one source-Order Entitlement. *(D-028; PRD §6 Reliability)*
- **BR-034** — *(Deferred, outside MVP.)* On gateway timeout/failure during checkout, Gradex fails safe — no access granted, no double charge — and surfaces a retryable error rather than a silent failure. *(PRD §6 Reliability)*

## 5. Refund Rules

> **BR-040 through BR-047 are deferred out of MVP** on 2026-07-28 by
> [D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
> Gradex processes no refunds; money returned to a Student is an External Payment matter handled
> outside the platform. Terms §8 is the authoritative no-commerce payment/consumer-rights
> disclosure under BR-153/`LG-011`; no standalone Refund Policy is required for this MVP. Ending a
> Student's access after an out-of-band refund uses the
> audited Entitlement adjustment in BR-026 or Entitlement revocation — never an unrecorded deletion.

- **BR-040** — *(Deferred, outside MVP.)* Only admins can initiate a refund. *(PRD §11 Purchase & Payment)*
- **BR-041** — A refund request calls the gateway with an integer-fils amount and required reason. Order, refund, and entitlement state change only after confirmed gateway success, including asynchronous confirmation. *(D-017; PRD §11 Purchase & Payment)*
- **BR-042** — Every refund stores amount, reason, acting Admin, idempotency reference, gateway reference/status, timestamps, and immutable audit history. *(D-017; PRD §11 Purchase & Payment)*
- **BR-043** — A confirmed refund reduces the associated Instructor's payable balance. If its statement was already paid, the amount becomes an auditable adjustment in the next payout cycle rather than an undocumented clawback. *(D-017, D-018; PRD §11 Admin Moderation & Payouts)*
- **BR-044** — Refund eligibility follows the versioned bilingual policy accepted at checkout. Exact streamed-digital-content eligibility remains configurable and must be approved by Kuwaiti counsel before production; Gradex does not assume that opening or streaming content automatically removes refund rights. *(D-017, D-020; PRD §7 Legal Constraints)*
- **BR-045** — A refund reduces reported revenue for the affected period; it does not retroactively remove the purchase from historical enrollment-count analytics — enrollment counts reflect what happened, revenue figures reflect current standing. *(Consistent with BR-043; new 2026-07-20)*
- **BR-046** — One or more full/partial refunds may be requested only up to the order's remaining refundable captured balance and only when the original payment method supports the requested refund type. A rejected/failed request has no entitlement effect. *(D-017)*
- **BR-047** — A confirmed partial Refund keeps access active. When cumulative confirmed Refunds equal captured amount, only the Entitlement whose `source_order_id` is that Order is revoked; the exact successful Refunds causing the threshold are retained. Enrollment, Progress, and other Entitlements remain. *(D-017/D-028)*

## 6. Video & Progress Rules

- **BR-050** — Playback authorization is issued only for an exact approved or historically qualifying Video Asset Version when the requesting Student has runtime access and the Course has no active emergency access suspension. Catalog delisting, retirement, or archival alone does not deny qualifying existing access. Other callers are denied regardless of file existence; Admin preview is separate and audited under BR-081. *(D-029; PRD §11 Video Playback & Progress; BR-023)*
- **BR-051** — A Lesson is marked complete once the server calculates at least 90% watched using the trusted duration of the exact Media Asset Version being played; client percentages are not authoritative. Positions are validated/bounded before monotonic update. `completed_at` is written once, the completing Asset Version is retained, and completion/max history never regresses across seeks, retries, or Video replacement. *(PRD §11 Video Playback & Progress; video design)*
- **BR-052** — A student reopening a lesson resumes from the position they last reached, not from the beginning. *(PRD §11 Video Playback & Progress)*
- **BR-053** — Transient technical failures during playback/progress tracking recover without interrupting an otherwise-authorized session. This does not override authorization: expiry, revocation, Account suspension, or emergency Course access suspension denies new playback and further playback-derived Progress writes. A delayed/retried Progress write must revalidate runtime Lesson access rather than silently updating after access ended. *(D-029; PRD §11 Video Playback & Progress; video design)*

> *Numbers 054–058 were retired on 2026-07-20. Playback/upload TTLs, replacement mechanics,
> retry/backoff, stale-upload cleanup, and endpoint idempotency parameters belong to the reconciled
> [video design](superpowers/specs/2026-07-17-video-streaming-design.md) and platform system design;
> they are not business-rule identifiers.*

- **BR-059** — Replacing a lesson's video preserves that lesson's existing student progress records — progress is keyed to the lesson, not the video file (video-streaming-design.md §3) — and resets only if the lesson itself is deleted and recreated. *(Formalizes video-streaming-design.md §3 data model; new 2026-07-20)*

## 7. Instructor Rules

- **BR-060** — An instructor can create, edit, and structure only their own courses. *(PRD §5 Instructor Features, §11 Instructor Course Builder)*
- **BR-061** — An instructor cannot publish a course directly — publishing requires admin approval. *(PRD §5 Admin Features, §11 Admin Moderation & Payouts)*
- **BR-062** — Video upload from the course builder hands off to the existing upload/transcode/HLS pipeline rather than reimplementing transcoding logic in the builder. *(PRD §11 Instructor Course Builder)*
- **BR-063** — Downloadable lesson attachments — both **lesson resources** (slides, notes, readings) and **lab materials** (project files + guide) — are stored in S3-compatible storage and exposed to enrolled students only via signed URLs, entitlement-checked per download (BR-023); no code execution or sandboxing is involved. *(PRD §11 Instructor Course Builder; resource/lab split added 2026-07-21 — see [DECISIONS.md](DECISIONS.md) D-011)*
- **BR-064** — Instructors see per-Course analytics (enrollments, completion rate), their own Course roster using only a Student-chosen display name/alias plus Course-scoped enrollment/progress fields, and current catalog prices read-only. They do not receive Student email, phone, payment data, legal identity, or cross-Course activity. They have no in-app earnings/payout dashboard; monthly payout statements are sent by email under BR-074. *(D-015, D-018, D-020; PRD §5 Instructor Features)*
- **BR-065** — Suspending an instructor blocks further editing of their courses and new submissions, but does not revoke already-enrolled students' access to that instructor's Published courses. *(Formalizes PROJECT_VISION.md §18 Product Principle "no student left alone after they pay"; new 2026-07-20)*
- **BR-066** — MVP has no general Student-visible file-version history or rollback. In Draft/Changes Requested content, replacing a Resource/Lab supersedes that draft file. For a Published Course, replacement belongs to a pending Course Revision and the approved live file remains available until Admin approval atomically applies the revision. *(BR-017; D-011)*
- **BR-067** — Lesson resources and lab materials are distinct per-lesson categories: **resources** are supplementary reference to consume (slides, notes, readings; allowed types PDF, slides, images), **lab materials** are hands-on practice to do (project files + a written guide; allowed types archives/project files plus a PDF/Markdown guide). Both are optional per lesson and independently uploaded and managed. *(Decision 2026-07-21; see [DECISIONS.md](DECISIONS.md) D-011)*
- **BR-068** — An upload that exceeds its bucket's configured per-file or per-lesson aggregate size cap is rejected with a validation error, leaving existing stored files unchanged. The specific caps are tunable implementation parameters set per [DECISIONS.md](DECISIONS.md) D-011 (resources 50 MB/file, 200 MB/lesson; labs 250 MB/file, 1 GB/lesson), not fixed business invariants. *(new 2026-07-21)*

## 8. Admin & Payout Rules

- **BR-070** — Submitting a Course for review moves it to `PENDING_REVIEW`, visible in the Admin queue but hidden from the Student catalog unless a previously approved live version exists under BR-017. *(D-021; PRD §11 Admin Moderation & Payouts)*
- **BR-071** — Admin approval moves a Course/revision to `PUBLISHED` (visible in the catalog) and notifies the Instructor. *(D-021; PRD §11 Admin Moderation & Payouts)*
- **BR-072** — An Admin request for changes requires a reason, moves a first-publication Course to `CHANGES_REQUESTED`, keeps it hidden, and lets the Instructor revise/resubmit. Rejecting a pending revision leaves the currently Published version unchanged. *(D-021; PRD §11 Admin Moderation & Payouts)*
- **BR-073** — *(Deferred, outside MVP by [D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation): with no in-platform revenue record there is no earning to calculate. The Instructor agreement's revenue-share terms remain required under `LG-020`.)* MVP uses one versioned platform-wide Instructor revenue-share percentage with no assumed default; it must be configured before production. Each paid Order snapshots the effective configuration and Course owner as its earning recipient. Reassignment/share changes affect later Orders only; Refund/chargeback adjustments stay with the original earning/Instructor and snapshotted policy. Every earning, fee, Refund, chargeback, payout adjustment, carry-forward, and correction is an immutable source-linked ledger entry; corrections append compensating entries. *(D-018/D-030; PRD §11 Admin Moderation & Payouts)*
- **BR-074** — *(Deferred, outside MVP with BR-073. Instructors are paid entirely out of band at launch.)* One monthly Statement exists per Instructor, currency, and accounting period. Approval freezes its ledger items, totals, and approved payout destination; later entries flow into a later open Statement. Transfer initiation creates an immutable attempt using that destination; `PAID` requires verified full-payment evidence, and failed/ambiguous retry reconciles first. Partial Statement payments and negative bank transfers are prohibited; negative balances carry forward. Admins transfer manually and email the Statement; no Instructor dashboard, withdrawal control, or automated settlement exists in MVP. *(D-006, D-018; PRD §11 Admin Moderation & Payouts)*

## 9. Access Control / Roles Matrix

| Action | Student | Instructor | Admin |
|---|---|---|---|
| Watch an entitled course | ✓ | — | ✓ (audited preview, see BR-081) |
| Create/edit own course | ✗ | ✓ (own courses only) | ✓ |
| Publish a course | ✗ | ✗ (admin-gated) | ✓ |
| Set/change Course or Section price | ✗ | ✗ (read-only) | ✓ (audited) |
| Create a Course Access Invitation | ✗ | ✗ | ✓ (audited, BR-165) |
| Accept a Course Access Invitation | ✓ (own invited email only, BR-166) | — | — |
| Approve or reject an accepted Invitation | ✗ | ✗ | ✓ (audited, BR-167) |
| Cancel a Course Access Invitation | ✗ | ✗ | ✓ (audited, BR-168) |
| View own access status and Invitation history | ✓ | — | — |
| View own per-course analytics | — | ✓ (own courses only) | ✓ (all courses) |
| View own Course-scoped Student roster | — | ✓ (own courses only, BR-064) | ✓ |
| View other instructors' course data | — | ✗ | ✓ |
| Adjust an Entitlement's expiry | ✗ | ✗ | ✓ (elevated, audited, BR-026) |
| Manage users (suspend, etc.) | ✗ | ✗ | ✓ |
| Report entitled content | ✓ | — | — |
| Moderate reported content | ✗ | ✗ | ✓ |
| Invite an Instructor/Admin | ✗ | ✗ | ✓ |
| Create/edit an office-hours session | ✗ | ✓ (own PUBLISHED Courses, BR-134) | ✗ |
| Join a live office-hours session | ✓ (if entitled, BR-135) | — | ✓ (moderation) |
| Cancel an office-hours session | ✗ | ✓ (own only) | ✓ (any, BR-137) |

> Rows for purchase history, refund initiation, payout management, and coupon create/redeem were
> removed on 2026-07-28 with the features they described
> ([D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)).
> No role gained a capability; four capabilities left MVP and four invitation capabilities replaced
> them.

- **BR-080** — This matrix is built strictly from the role-scoped language already in the PRD (e.g. "their own courses," "per-course," admin "manage everything") — it is the authoritative source for the authorization layer. *(PRD §3 Target Users, §5 Student/Instructor/Admin Features)*
- **BR-081** — An Admin may watch any Course's video content — including `PENDING_REVIEW` and Draft Lessons under active review — without a Student entitlement. This is a distinct, audited authorization path from Student playback: every Admin preview records Admin, Lesson, and timestamp and never creates an enrollment/entitlement. *(Supports BR-070/BR-071 review.)*
- **BR-082** — Only a Student Account may receive a Course Access Invitation, accept one, receive an Entitlement, create an Enrollment, or record Student Progress. Instructor Accounts author assigned content without Student consumption capability. Admin Accounts use BR-081's separate audited preview and never receive ordinary Student Entitlements or Progress. A person needing separate capabilities uses a separate Account with another normalized email. Role conversion and multi-role membership are unsupported MVP mutations. *(D-014)*

## 10. Content Lifecycle

- **BR-090** — Course presentation lifecycle is `DRAFT → PENDING_REVIEW → PUBLISHED`, with `PENDING_REVIEW → CHANGES_REQUESTED → PENDING_REVIEW`, `PUBLISHED ↔ DELISTED`, and `PUBLISHED/DELISTED → ARCHIVED`. `DELISTED` removes catalog discovery and blocks new access grants, but does not deny qualifying existing access: a Student holding an active Entitlement keeps it, unchanged. A Course Access Invitation for a `DELISTED` Course therefore cannot be approved (BR-167); the operational remedy is relist, approve, delist. Retirement is a separate future-acquisition/inclusion block. Emergency Course access suspension is a separate elevated command for constrained legal/security/malware/severe-moderation reasons; it immediately denies existing Student access without mutating Entitlements and requires reason, Audit, and notification/outbox evidence. A Published Course revision remains distinct and the current approved graph stays live until pointer swap. *(D-021/D-029 as amended by D-045 on 2026-07-29, which replaced the obsolete "new checkout" wording with "new access grants"; BR-017/018/167)*
- **BR-091** — Video processing distinguishes not-uploaded/uploading, queued/processing, ready, approved-current, and failed/retry states. Exact technical states are canonicalized in system design; `READY` never bypasses Admin Course/revision approval, and a replacement cannot interrupt the approved live Video. *(BR-017/061; video design)*

> Canonical User, invitation, Order, payment-attempt, Refund, entitlement, report, office-hours, coupon, statement, and payout lifecycles are defined in [DOMAIN_MODEL.md](DOMAIN_MODEL.md). Feature specs may add implementation detail but may not redefine those states.

## 11. Security & Data Rules

- **BR-100** — Signed URLs for video manifests/segments are session-scoped and short-lived — re-issued per playback session, never cached long-term client-side — rather than literally single-use: HLS playback requires repeated requests to the same segment within one session (seeking, rebuffering, ABR rendition switches), which true single-use would break. Lab-material download URLs MAY be single-use where the storage/CDN layer supports it, since a one-time file download has no repeat-access requirement. *(PRD §6 Security; single-use language corrected 2026-07-20 to match how HLS playback actually works — see [video-streaming-design.md](superpowers/specs/2026-07-17-video-streaming-design.md) §5's token-based manifest proxy.)*
- **BR-101** — Only authorized Admin operations can view direct Student account/contact/payment PII. An Instructor may view only BR-064's minimal Course-scoped display identity and learning fields for their own roster. Gradex minimizes collection, encrypts sensitive personal data at rest, protects it with TLS in transit, and excludes credentials, tokens, and personal data from application logs. *(D-020; PRD §6 Security)*
- **BR-102** — Signed-URL issuance and download endpoints are rate-limited and monitored per user/IP to detect bulk-scraping or credential-sharing. *(PRD §6 Security)*
- **BR-103** — Downloadable Lab Materials carry an opaque per-Entitlement/buyer identifier to deter and investigate redistribution; MVP does not claim DRM. Lesson Resources remain entitlement-gated/rate-limited but are not individually tagged. The tag must avoid exposing direct Student PII. *(D-011; PRD §9 Downloadable Content Leakage)*
- **BR-104** — Untrusted uploads are validated and quarantined. Public previews and downloadable assets are unavailable until malware scanning succeeds; a failed or unavailable scan fails closed and leaves the asset unavailable. *(D-019, D-020; PRD §6 Security)*
- **BR-105** — Every Account has a self-chosen display name: it defaults to the name supplied at registration or invitation acceptance, is editable by its owner at any time, and is **not** unique — Account identity remains the normalized email and internal identifier. It accepts 2–50 characters in Arabic or Latin script and rejects URLs, control characters, and markup. It is the only identity field an Instructor roster may expose under BR-064, is never required to carry legal identity, and an Admin may reset an abusive value through the audited moderation path. *(D-024; BR-064, BR-101)*

## 12. Data Integrity Rules

These are structural invariants — they translate directly into database constraints, API validation, and service-layer checks.

- **BR-110** — Every lesson belongs to exactly one section. *(Formalizes BR-010; new 2026-07-20)*
- **BR-111** — Every section belongs to exactly one course. *(Formalizes BR-010; new 2026-07-20)*
- **BR-112** — Deleting a course cascades to its sections and lessons only when BR-018 permits deletion at all (zero enrollments) — a course with any enrollment can only be Archived, so cascade-delete never applies to enrolled content. *(Formalizes BR-018; new 2026-07-20)*
- **BR-113** — An Entitlement must reference an existing Student, exactly one existing Course, and a typed `grant_source` under BR-028. A `MANUAL_INVITATION` Entitlement must also reference the exact approved Course Access Invitation it came from. *(D-045; PRD §11 Course Access)*
- **BR-114** — A progress record cannot exist without a corresponding enrollment. *(Formalizes BR-023's entitlement-before-access model; new 2026-07-20)*
- **BR-115** — Every lesson resource and every lab material belongs to exactly one lesson and is removed with that lesson; deletion follows the same enrollment/archival constraint as the lesson's course (BR-018, BR-112). *(Formalizes BR-063/BR-067; new 2026-07-21)*
- **BR-116** — A Progress write requires runtime Student access and a Lesson reachable through the current approved or qualifying acquired graph. The server uses the exact played Media Asset Version's trusted duration, bounds positions before monotonic update, and preserves `completed_at` plus the first completing Asset Version. `UNIQUE(enrollment_id, course_lesson_identity_id)` is the Progress identity: the durable Student-visible Lesson identity, never a revision-owned `course_lessons` row and never `lessons(id)`, so one Progress record survives Course revision cloning, Lesson metadata changes, and video or Asset Version replacement (BR-059). Current Lesson metadata is resolved through the live approved revision, and the exact played Asset Version is validated separately and retained as completion evidence. Expiry/revocation preserves the row but blocks further playback-derived updates. *(BR-023/050/051/053/059/114; identity amended on 2026-08-01 by [D-060](DECISIONS.md#d-060--s5-progress-uses-stable-lesson-identities), which replaced the original `UNIQUE(enrollment_id, lesson_id)` wording — a revision-row key cannot preserve Progress across revision cloning)*

## 13. Notification Rules

- **BR-119** — Required transactional/security messages cannot be disabled. Marketing, lifecycle automation, per-type preferences, SMS, WhatsApp, and push are outside MVP. *(D-010)*
- **BR-120** — Notification delivery is best-effort: a failed or delayed send never blocks, rolls back, or alters the triggering action. Course publish, entitlement grant, refund, invitation, password/security, and office-hours state changes succeed independently of delivery. *(D-010)*
- **BR-121** — An access-granted confirmation is recorded only after Admin Approval has created the Entitlement in its own transaction — never on invitation creation, never on Student acceptance, and never on a rejected or cancelled Invitation. *(D-045; BR-028, BR-167)*
- **BR-122** — Required in-app + email events are Course Access Invitation issued, access granted, invitation rejected, invitation cancelled after the Student was notified, password/security, Account invitation, Course approval/changes requested, office-hours cancellation/material rescheduling, Admin Entitlement expiry adjustment, and emergency Course access suspension/restoration. Invitation *accepted* targets Admin operations and is in-app. New office-hours sessions target currently entitled Students, while new Instructor Course/revision submissions target Admin operations; both are in-app and may also use email when appropriate. Video-processing completion targets the Instructor. *(D-010/D-026/D-029; PRD §5 Notifications)*
- **BR-123** — A Notification Event relationally snapshots exact `(Account, channel)` Recipients at source-event time; delayed workers never recalculate the audience from a current roster. The in-app Recipient is durable and remains valid regardless of idempotent best-effort email Attempts. Mandatory transactional/security notices cannot be suppressed; operational notices follow fixed product policy; optional marketing is outside MVP. *(D-010; PRD §5 Notifications)*

## 14. Coupon Rules

> **BR-124 through BR-133 are deferred out of MVP** on 2026-07-28 by
> [D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
> A coupon discounts a checkout, and MVP has no checkout. Free or promotional access is granted by
> the same audited Course Access Invitation path as any other access — there is no second grant
> mechanism. These rules remain the design of record for the deferred feature.

- **BR-124** — *(Deferred, outside MVP.)* Coupons are created and managed by Admins only; Instructors cannot create or apply discounts in MVP. *(D-012)*
- **BR-125** — A coupon's discount is either a percentage (1–100) or a fixed amount in fils. The computed discount is integer fils, rounded to the nearest fil for percentages, and clamped to the range `[0, subtotal]` — it can never be negative and never exceed the item price. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-126** — A coupon that reduces the order to 0 KWD grants enrollment directly, with no payment-gateway call. This free-grant path still creates a real Order and Enrollment, snapshots the Course-configured expiry, carries the same Entitlement checks (BR-023/025), and uses the Order identifier as its stable idempotency key under BR-129. *(D-012/D-026)*
- **BR-127** — At most one coupon applies per order; coupons do not stack. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-128** — Coupon validity—active window, target, global capacity, and Student eligibility—is enforced under Coupon/access-guard locks at Order acceptance. A paid Order atomically creates a `RESERVED` Redemption expiring at the Order payment deadline; capacity counts reserved plus historically consumed uses. Order expiry/cancellation releases an unused reservation. A zero-value Order creates and consumes its Redemption in the grant transaction. *(D-012/D-028)*
- **BR-129** — Verified timely paid success atomically moves the Order's Redemption `RESERVED → CONSUMED` in the same transaction that creates Enrollment/Entitlement. Reservation release is `RESERVED → RELEASED_UNUSED`; cumulative full Refund releases Student eligibility as `CONSUMED → RELEASED_AFTER_FULL_REFUND`. One capacity-affecting Redemption per `(Coupon, Student)` and one per Order are exact. Historical consumed count never decrements and full Refund never restores historical global quota. *(D-012/D-028; BR-033)*
- **BR-130** — Once a coupon has any redemption, its `code`, `discount_type`, and `discount_value` are immutable; only `is_active`, `valid_until`, `max_redemptions`, and its target scope remain editable. A coupon with any redemption may be deactivated but not deleted; a coupon with zero redemptions may be deleted outright (mirrors BR-018). *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-131** — A Refund on a Coupon Order returns no more than captured amount. Partial Refund leaves Redemption consuming. Cumulative full confirmed Refund releases that Student's eligibility but preserves Redemption/Refund history, historical consumed count, and global quota consumption; reuse still requires active/targeted Coupon and remaining global capacity. A zero-value Coupon Order has no Refund path and its Entitlement may be reversed only by a defined reconciliation, approved fraud/abuse, chargeback-equivalent, or legal workflow—never an unrestricted generic Admin revoke. *(D-012/D-017/D-028)*
- **BR-132** — Coupon codes are stored uppercase and trimmed and matched case-insensitively; a unique index on the code prevents duplicates. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-133** — Coupon create/edit/deactivate actions and every redemption are logged for audit, consistent with the refund-audit (BR-042) and admin-preview (BR-081) discipline. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*

## 15. Live Office Hours Rules

- **BR-134** — Only the owning Instructor can create/materially reschedule office hours for their own `PUBLISHED` Course. The owner may cancel an existing scheduled Session after the Course is Delisted/Archived; they cannot create/reschedule then. Admins may cancel for moderation but cannot create platform-wide sessions. *(D-013/D-029)*
- **BR-135** — An uncancelled Session is time-derived as `UPCOMING` before start, `LIVE` during `[starts_at, ends_at)`, and `ENDED` afterward; time never proves delivery or attendance. Existing entitled Students retain authorized historical Session/material/recording access after delisting/retirement/archival, while cancellation blocks joining without deleting Session, notification, attendance, or Audit history. *(D-013/D-029)*
- **BR-136** — The external join link is revealed only during the permitted `LIVE` window after authentication, runtime Course/contained-Section Entitlement authorization, and emergency-suspension checks. It is never rendered on public/catalog pages or included in unauthorized notification content. *(D-013/D-029)*
- **BR-137** — Office-hours sessions publish immediately without a content-approval gate. Admin moderation is reactive and limited to cancellation with an audited reason. *(D-013)*
- **BR-138** — A suspended instructor (BR-065) cannot create or edit office-hours sessions, consistent with suspension blocking new submissions. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-013)*
- **BR-139** — Cancelling a session is not deletion: cancelled sessions are retained for audit and hidden from upcoming lists. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-013)*
- **BR-140** — Session creation, material rescheduling, and cancellation produce deduplicated best-effort notifications under BR-120/BR-122 to currently entitled Students. *(D-010, D-013)*
- **BR-141** — Session times are stored in UTC and displayed in the user's local timezone and selected interface language, defaulting to Kuwait time when no preference is known. *(D-013, D-016)*

## 16. Public Preview & Content Reporting Rules

- **BR-143** — Protected Lesson resources and lab materials are never exposed as public samples. A Course may have at most one optional preview asset that is stored and authorized separately from protected Lesson content. *(D-019)*
- **BR-144** — A public preview may be a short video, PDF, image, or intentionally prepared sample file. It becomes available only after type/size validation, quarantine, successful malware scan, and Instructor confirmation that Gradex may publish it. *(D-019; BR-104)*
- **BR-145** — An entitled Student may report a Course, Lesson, video, resource, lab material, or Office-Hours Session as broken/unavailable, inaccurate, inappropriate, suspected copyright violation, or other; “other” requires explanation. The report relationally preserves both stable logical target and exact visible revision/version. Typed automated reports/findings may omit a reporting Account but must preserve source evidence. Duplicate/spam submissions are rate-limited. *(D-019)*
- **BR-146** — A report/finding never hides or retires content automatically. Media quarantine/rejection and emergency security suspension are separate constrained safety workflows. An Admin may dismiss, request changes, delist, retire, invoke emergency Course access suspension, cancel a Session, or suspend an Account. Every resolution/result is immutable and records actor, reason, exact source action, and time; notices follow fixed policy. *(D-019/D-029; BR-120/122)*

## 17. Responsive, Localization & Accessibility Rules

- **BR-147** — Gradex MVP is a responsive website. Every approved Student function must work on supported phones, tablets/iPads, laptops, and desktops; larger screens may change layout density but not unlock exclusive Student capability. *(D-016)*
- **BR-148** — Instructor/Admin portals remain responsive, while Course building, uploads, moderation, refunds, reporting, and payout operations may be optimized for tablet/laptop/desktop use. Native mobile applications are outside MVP. *(D-016)*
- **BR-149** — Arabic and English interfaces are available across every role. Arabic is the initial default, a user's language preference persists, and UI layout/navigation/forms/tables support RTL and LTR. *(D-016)*
- **BR-150** — Course content stays in its authored language; Gradex does not automatically translate Instructor-authored content in MVP. *(D-016)*
- **BR-151** — Platform-owned interfaces and player controls target WCAG 2.2 Level AA. Captions/transcripts are outside MVP, so Gradex must not claim complete product-level conformance; third-party hosted checkout accessibility is evaluated but not represented as directly controlled. *(D-016)*

## 18. Privacy & Legal-Readiness Rules

- **BR-152** — Gradex collects only personal data required for identity, learning, commerce, support, security, and legal operations. Full card/PAN data is never collected, transmitted, or stored by Gradex. *(D-020; BR-030)*
- **BR-153** — Bilingual Privacy and Terms must be approved before production, and the exact version accepted by a user is recorded wherever acceptance is required. For the no-commerce MVP, Terms §8 is the payment/consumer-rights disclosure, Privacy §4 and Terms §4 disclose course-access processing, and no standalone Refund Policy is required. *(D-017, D-020; LG-011 approved package 2026-08-09)*
- **BR-154** — Users can request access, correction, deactivation, or deletion of personal data. Eligible personal data is anonymized where practical. Financial ledger entries, Statements, payout evidence, and privileged-action Audit records remain append-only and are never rewritten or hard-deleted; approved policy may restrict access, archive storage, minimize separable payloads, or anonymize eligible personal references without destroying financial provenance or action evidence. *(D-020)*
- **BR-155** — Exact retention periods and the applicability/wording of Kuwaiti privacy, consumer, digital-commerce, and education-sector obligations are launch gates requiring counsel/accounting approval; they are not invented as business rules. *(D-020; [LAUNCH_GATES.md](LAUNCH_GATES.md))*
- **BR-156** — Secrets remain outside the repository. Credentials, tokens, and personal data are excluded from logs; sensitive data is encrypted in transit and at rest according to its classification. *(D-020)*

## 19. Catalog Taxonomy & Search Rules

- **BR-157** — Every Course is classified on exactly three dimensions: one **Major**, one **Subject**, and one **Study Year**. Major and Subject are Taxonomy Terms drawn from Admin-managed bilingual vocabularies; a Subject term may also carry an optional academic code such as `CS 101`. Study Year is the fixed enumeration `PREP`, `YEAR_1`, `YEAR_2`, `YEAR_3`, `YEAR_4`. No Course carries more than one value per dimension, and no fourth classification dimension exists in MVP. *(D-022)*
- **BR-158** — Only Admins create, rename, retire, or delete Taxonomy Terms, and every such action is audited like other privileged catalog actions. An Instructor selects among existing terms for an owned Course but cannot create, rename, or retire one; an Admin may override any Course's assignment. *(D-022; consistent with BR-019, BR-133)*
- **BR-159** — A Course cannot be submitted for review until all three dimensions are assigned; the Instructor sees which classification is missing, in the same validation as BR-012. Renaming a term changes its label everywhere it is displayed and never rewrites the Courses assigned to it. *(D-022; BR-012, BR-013)*
- **BR-160** — A retired Taxonomy Term cannot be newly assigned, but Courses already carrying it keep it and remain filterable until an Admin reassigns them. A term referenced by at least one Course may be retired but not deleted; a term with zero referencing Courses may be deleted outright. *(D-022; mirrors BR-018, BR-130)*
- **BR-161** — Catalog search matches a query against Course title, authored description, owning Instructor display name (BR-105), and the labels and code of the Course's assigned Taxonomy Terms. It returns only `PUBLISHED` Courses and never indexes Lesson titles, protected Resources/Lab Materials, or unpublished content. Results are ranked by relevance only — MVP has no personalization, recommendation, or paid placement — and search composes with the BR-157 filters. *(D-023; BR-090, BR-143)*
- **BR-162** — Search matches across Arabic and English content simultaneously, regardless of the selected interface language, and is case-insensitive. Arabic matching normalizes away tashkeel/diacritics and tatweel and folds alef variants (`أ إ آ ٱ` → `ا`), alef maqsura (`ى` → `ي`), taa marbuta (`ة` → `ه`), and Arabic-Indic digits (`٠–٩` → `0–9`), so the same word typed with different hamza forms or diacritics matches. The retrieval mechanism is a system-design choice; this rule fixes only the matching behavior. *(D-023; BR-149, BR-150)*

## 20. Operational Data Integrity Rules

- **BR-163** — PostgreSQL is the durable authority for asynchronous/scheduled work: stable ID, payload/source reference, state, availability, attempts, lease owner/expiry, last error, and completion evidence. Redis loss cannot lose work; delivery is at least once and every consumer is idempotent. Exhausted-work retry appends evidence and reconciles ambiguous external side effects before resend. *(D-025; approved July 26 Section 4)*
- **BR-164** — Retention deletion requires an approved effective policy, no legal hold, exact record/object version, serialized scope claim, verified provider deletion beyond a versioned-storage delete marker, and a confirming PostgreSQL transaction. Pending, failed, or unverifiable deletion is never recorded complete; automated and manual deletion obey the same hold checks. *(D-020; LG-003/LG-004; approved July 26 Section 4)*

## 21. Course Access Invitation Rules

Added 2026-07-28 by [D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
These rules govern the workflow that replaces checkout. The access consequence of that workflow lives
in [§3](#3-course-access-and-entitlement-rules); this section governs the workflow itself.

- **BR-165** — Only an Admin may create a Course Access Invitation, and each one binds exactly four things: one normalized Student email, one Course, the creating Admin, and its current state. It also records creation, acceptance, decision, and cancellation timestamps separately. Creation is audited and, on its own, grants nothing. At most one non-terminal Invitation may exist per `(normalized email, Course)`. *(D-045)*
- **BR-166** — Only an authenticated Account whose normalized email equals the Invitation's may accept it. Acceptance is refused server-side for any other identity regardless of how the acceptance link was obtained, and a Student who already has an Account must still receive and accept an Invitation. Acceptance moves the Invitation to pending Admin approval and **grants no access**. *(D-045; BR-029)*
- **BR-167** — **Admin Approval is the authoritative grant trigger.** Approving an accepted Invitation atomically creates or reuses the Student's Enrollment for that Course and creates exactly one `ACTIVE` Entitlement with `grant_source = MANUAL_INVITATION`, snapshotting the Course's `default_access_ends_at` under BR-025 and setting `retirement_eligibility_at` under BR-027. The operation is idempotent: repeating it, concurrently or later, returns the existing Entitlement and creates nothing. Approval requires the Admin course-access capability and a valid recent authentication; if either is absent the request is **refused**, not degraded. *(D-045; BR-024, BR-028)*
- **BR-168** — The Invitation lifecycle is `PENDING_STUDENT_ACCEPTANCE → PENDING_ADMIN_APPROVAL → APPROVED`, with `PENDING_ADMIN_APPROVAL → REJECTED` and cancellation available to an Admin before a decision. `APPROVED`, `REJECTED`, and `CANCELLED` are terminal. Rejection requires a reason. An Admin may reject an Invitation the Student has already accepted, and a new Invitation may afterwards be created for the same `(email, Course)` pair; every transition is audited. *(D-045)*
- **BR-169** — A Course Access Invitation has **no expiry state**. No approved business rule requires one and no duration is invented. The acceptance link is a separate expiring, single-use, purpose-bound action secret under BR-008's token discipline, and its expiry is resolved by reissuing the link — not by expiring the Invitation. *(D-045)*
- **BR-170** — External Payment evidence retained on an Invitation is limited to an optional free-text Admin note and an opaque external reference, carried on the audit record. Gradex stores no amount, currency, payment status, gateway identifier, or payer instrument anywhere. This evidence is Admin-only and is never exposed to the Student or to an Instructor roster. *(D-045; BR-020, BR-064, BR-101)*
- **BR-171** — Instructor and Course Access invitations are separate workflows with separate records, states, and purposes, and neither may be implemented in terms of the other. An Instructor Invitation creates an Account and assigns a role under BR-009; a Course Access Invitation creates no Account, assigns no role, and targets an existing or future Student identity. *(D-045; BR-009)*
