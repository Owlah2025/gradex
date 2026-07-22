# Business Rules

> Status: Draft
> Last Updated: 2026-07-22

This document is the single source of truth for Gradex's business logic — the rules governing users, courses, enrollment, payments, refunds, video/progress, instructors, admin actions, access control, content lifecycle, and data integrity. Most rules below are extracted from [PRD.md](PRD.md) (and, where PRD.md explicitly defers to it, [video-streaming-design.md](superpowers/specs/2026-07-17-video-streaming-design.md)). A smaller set fill real gaps the PRD was silent on — those are tagged "Decision" or "new," dated, and cross-referenced to [DECISIONS.md](DECISIONS.md) where significant enough to warrant a log entry. Either way, nothing here is silently assumed — each rule cites where it comes from.

Business rules state *what must always be true*; they intentionally omit tunable technical parameters (TTLs, retry counts, backoff schedules, cleanup windows) — those belong to the implementation spec that owns them, since changing a number there doesn't change the business. Acceptance Criteria in [PRD.md §11](PRD.md) are the testable Given/When/Then specs for these same rules — each AC bullet is tagged with the BR(s) it implements, so the two stay linked instead of silently drifting apart.

---

## 1. User & Auth Rules

- **BR-001** — A prospective student/instructor can only register with a unique email; a duplicate email returns 409 with no account created. *(PRD §11 Authentication)*
- **BR-002** — Passwords are hashed before storage; plaintext or hashed password is never echoed back in any response. *(PRD §11 Authentication)*
- **BR-003** — Login failure (bad credentials) returns 401 without revealing whether the email exists on the platform. *(PRD §11 Authentication)*
- **BR-004** — Successful login issues a short-lived access token plus a rotating refresh token; the session is independently revocable, not solely reliant on token expiry. *(PRD §11 Authentication, §6 Security)*
- **BR-005** — A refresh token that is expired, revoked, or reused after rotation is rejected with 401 — access cannot be renewed from it. *(PRD §11 Authentication)*
- **BR-006** — Logout or admin-initiated session revocation invalidates the refresh-token session in Redis; subsequent refresh calls with that token are rejected with 401. Access tokens are stateless JWTs validated by signature/expiry only (no per-request Redis check, per BR-004) — an already-issued access token keeps working until its own short TTL naturally expires, so revocation takes effect on the next refresh, bounded by that TTL, not instantly on every protected-API call. *(PRD §11 Authentication; scope narrowed 2026-07-20 to match BR-004's stateless-access-token design and PRD §6 Reliability's Redis graceful-degradation requirement.)*
- **BR-007** — A suspended account loses all platform access — login, video playback, and lab downloads — regardless of prior purchases. *(Formalizes PRD §9 Risk 3's "account-suspension consequences"; new 2026-07-20)*

## 2. Course Rules

- **BR-010** — A course is structured as Course → Section → Lesson; ordering within that hierarchy is preserved. *(PRD §5 Course Management)*
- **BR-011** — A newly created course starts in Draft status and is invisible to students regardless of content completeness. *(PRD §11 Instructor Course Builder)*
- **BR-012** — A course cannot be submitted for admin review if any lesson is missing its video, or if the course has zero sections/lessons; the instructor sees a validation message identifying what's missing. *(PRD §11 Instructor Course Builder)*
- **BR-013** — A course becomes eligible for submission only once it has at least one section containing at least one lesson with a successfully transcoded video. *(PRD §11 Admin Moderation & Payouts)*
- **BR-014** — Every course has exactly one owning instructor. Ownership can only be reassigned by an admin — instructors cannot transfer or share ownership themselves. *(Formalizes BR-060; new 2026-07-20)*
- **BR-015** — An instructor may freely edit a course while it is in Draft or Rejected status. *(Formalizes BR-011, BR-072; new 2026-07-20)*
- **BR-016** — A course in Pending Approval status is read-only to its instructor until the admin approves or rejects it, preventing simultaneous edits during review. *(Decision 2026-07-20)*
- **BR-017** — Editing a Published course's structure, video, or price never modifies the live course directly. It creates a separate pending-revision record referencing the course; the live course's status stays `PUBLISHED` and its content stays unchanged and visible to students throughout the review. The pending revision must clear the same admin review flow as first-time submission (BR-070–072); on approval, its changes are applied to the live course atomically, and on rejection it's discarded/returned to the instructor while the live course remains untouched. *(Decision 2026-07-20 — covers price changes too: an instructor cannot silently drop or raise a live course's price. Data-model note added 2026-07-20: a pending revision is a distinct record, not a course-status transition — see BR-090.)*
- **BR-018** — A course with at least one enrollment can never be permanently deleted — it can only be moved to Archived status (removed from the catalog and new purchases, but still accessible to already-enrolled students). A course with zero enrollments (e.g. a never-published Draft) may be deleted outright. *(Decision 2026-07-20)*

## 3. Enrollment / Purchase Rules

- **BR-020** — Course/chapter/bundle access is granted only after the payment gateway's success callback/webhook is received — never on client-side redirect alone. *(PRD §11 Purchase & Payment)*
- **BR-021** — A successful purchase creates an enrollment record scoped to the specific course, chapter, or bundle purchased — not blanket platform access. *(PRD §11 Purchase & Payment)*
- **BR-022** — A declined, cancelled, or timed-out payment (including installment setup failure) grants no enrollment or access; the order is marked failed. A transport-level retry of the same payment attempt reuses that attempt's idempotency key (so a network blip can't double-charge); a new payment attempt made after a definitive decline/cancellation/timeout is issued a fresh idempotency key linked to the same order. If the outcome is ambiguous (e.g. gateway timeout with no definitive response), Gradex reconciles the order's true status with the gateway (webhook or polling) before permitting another attempt on that order. *(PRD §11 Purchase & Payment; idempotency-key scoping and reconciliation clauses added 2026-07-20.)*
- **BR-023** — Entitlement is checked against the enrollment record before every signed HLS playback URL is issued and before every lab-material download — lessons outside the purchased scope, or accessed after the enrollment's term has expired (BR-025), are denied regardless of whether the underlying file exists. *(PRD §11 Purchase & Payment, Video Playback & Progress; expiry clause added 2026-07-20)*
- **BR-024** — A student cannot purchase a course, chapter, or bundle they already hold an active (non-expired) enrollment for; checkout blocks the attempt or redirects to their existing access. *(Formalizes BR-021; new 2026-07-20)*
- **BR-025** — Enrollment access lasts 150 days (~5 months, approximating one academic semester) from the purchase timestamp, calculated in Kuwait local time (UTC+3). Access remains valid through the end of day 150 and is denied from day 151 onward. When a term expires, access ends automatically — v1 has no dedicated "renewal" flow; a lapsed student regains access by purchasing again through the normal checkout (which BR-024 permits once the prior enrollment is no longer active). *(Decision 2026-07-20; exact duration/timezone/boundary made concrete 2026-07-20 — see [DECISIONS.md](DECISIONS.md) D-009. The 150-day figure operationalizes the originally-stated "4–5 months" — revisit if a different exact term length is wanted.)*

## 4. Payment Rules

- **BR-030** — Gradex never collects, transmits, or stores raw card/PAN data; checkout is fully delegated to the PCI-DSS-compliant gateway's hosted page or tokenized SDK. *(PRD §6 Security)*
- **BR-031** — All payment webhooks are validated via signature verification before being trusted, to prevent spoofed "payment succeeded" callbacks. *(PRD §6 Security, §11 Purchase & Payment)*
- **BR-032** — When an installment collection attempt fails per the gateway's own retry/dunning policy, Gradex reflects the gateway-reported status (e.g. past-due, plan-cancelled) by restricting access per the agreed policy — Gradex does not reimplement collection or risk logic itself. *(PRD §11 Purchase & Payment, §5 Payments)*
- **BR-033** — Purchase/entitlement state changes (grant access, mark installment paid) are written transactionally, keyed by the gateway's idempotency/transaction ID, so a retried or duplicate webhook cannot double-grant access or double-record payment. *(PRD §6 Reliability)*
- **BR-034** — On gateway timeout/failure during checkout, Gradex fails safe — no access granted, no double charge — and surfaces a retryable error rather than a silent failure. *(PRD §6 Reliability)*

## 5. Refund Rules

- **BR-040** — Only admins can initiate a refund. *(PRD §11 Purchase & Payment)*
- **BR-041** — A refund calls the gateway's refund API and updates the order/enrollment status to reflect revoked or partial access per policy. *(PRD §11 Purchase & Payment)*
- **BR-042** — Every refund event is logged for audit, without requiring manual reconciliation of gateway records. *(PRD §11 Purchase & Payment)*
- **BR-043** — If the associated instructor payout has not yet been marked "Paid," a refund automatically excludes that amount from the instructor's payable balance; if it was already "Paid," the payout record is flagged for manual adjustment/clawback in a future cycle. *(PRD §11 Admin Moderation & Payouts)*
- **BR-044** — Refund policy is consistent with Kuwait's 14-day consumer-protection right, subject to the digital-content-once-accessed exemption — a "no refund once a lesson has been streamed or a file opened" policy is the working assumption pending final legal-text confirmation. *(PRD §5 Payments, §7 Legal Constraints)*
- **BR-045** — A refund reduces reported revenue for the affected period; it does not retroactively remove the purchase from historical enrollment-count analytics — enrollment counts reflect what happened, revenue figures reflect current standing. *(Consistent with BR-043; new 2026-07-20)*

## 6. Video & Progress Rules

- **BR-050** — A signed playback URL is issued only for a PUBLISHED lesson the requesting student has purchased/enrolled in; an unauthenticated or non-purchasing user is denied, regardless of whether the file exists. Admins have a separate, audited preview path instead of an enrollment-based exception here — see BR-081. *(PRD §11 Video Playback & Progress; video-streaming-design.md §5)*
- **BR-051** — A lesson is marked complete once a student has watched at least 90% of it; completion is permanent and never regresses, even if the student seeks backward or rewatches. *(PRD §11 Video Playback & Progress; implementation mechanics in video-streaming-design.md §5)*
- **BR-052** — A student reopening a lesson resumes from the position they last reached, not from the beginning. *(PRD §11 Video Playback & Progress)*
- **BR-053** — Transient technical failures during playback or progress-tracking (e.g. a momentary signed-URL refresh, a dropped progress-write request) must never surface as an error to the student or interrupt their session — the platform retries/recovers transparently. This does not cover authorization failures: an expired enrollment (BR-025) or a non-entitled user must still produce the access-denied behavior required by BR-023 and BR-050, not a silent retry. *(PRD §11 Video Playback & Progress; implementation mechanics in video-streaming-design.md §5; transient-vs-authorization distinction added 2026-07-20.)*

> *BR-054–058 (signed-URL TTLs, replace-while-published swap timing, transcode retry/backoff count, stale-upload cleanup window, endpoint idempotency) were retired 2026-07-20 — they're tunable implementation parameters, not business rules. They live in [video-streaming-design.md](superpowers/specs/2026-07-17-video-streaming-design.md) §4–§6, which remains their source of truth.*

- **BR-059** — Replacing a lesson's video preserves that lesson's existing student progress records — progress is keyed to the lesson, not the video file (video-streaming-design.md §3) — and resets only if the lesson itself is deleted and recreated. *(Formalizes video-streaming-design.md §3 data model; new 2026-07-20)*

## 7. Instructor Rules

- **BR-060** — An instructor can create, edit, and structure only their own courses. *(PRD §5 Instructor Features, §11 Instructor Course Builder)*
- **BR-061** — An instructor cannot publish a course directly — publishing requires admin approval. *(PRD §5 Admin Features, §11 Admin Moderation & Payouts)*
- **BR-062** — Video upload from the course builder hands off to the existing upload/transcode/HLS pipeline rather than reimplementing transcoding logic in the builder. *(PRD §11 Instructor Course Builder)*
- **BR-063** — Downloadable lesson attachments — both **lesson resources** (slides, notes, readings) and **lab materials** (project files + guide) — are stored in S3-compatible storage and exposed to enrolled students only via signed URLs, entitlement-checked per download (BR-023); no code execution or sandboxing is involved. *(PRD §11 Instructor Course Builder; resource/lab split added 2026-07-21 — see [DECISIONS.md](DECISIONS.md) D-011)*
- **BR-064** — Instructors see per-course analytics (enrollments, completion rate) and their own student roster, but no earnings/payout figures — those are admin-managed only. *(PRD §5 Instructor Features, §4 Scope)*
- **BR-065** — Suspending an instructor blocks further editing of their courses and new submissions, but does not revoke already-enrolled students' access to that instructor's Published courses. *(Formalizes PROJECT_VISION.md §18 Product Principle "no student left alone after they pay"; new 2026-07-20)*
- **BR-066** — Replacing a lesson resource or lab material overwrites the previous version in place; v1 has no file versioning or rollback for either, matching the video pipeline's replace behavior (BR-059). *(Consistent with video-streaming-design.md §4 replace behavior; new 2026-07-20; extended to lesson resources 2026-07-21 per D-011)*
- **BR-067** — Lesson resources and lab materials are distinct per-lesson categories: **resources** are supplementary reference to consume (slides, notes, readings; allowed types PDF, slides, images), **lab materials** are hands-on practice to do (project files + a written guide; allowed types archives/project files plus a PDF/Markdown guide). Both are optional per lesson and independently uploaded and managed. *(Decision 2026-07-21; see [DECISIONS.md](DECISIONS.md) D-011)*
- **BR-068** — An upload that exceeds its bucket's configured per-file or per-lesson aggregate size cap is rejected with a validation error, leaving existing stored files unchanged. The specific caps are tunable implementation parameters set per [DECISIONS.md](DECISIONS.md) D-011 (resources 50 MB/file, 200 MB/lesson; labs 250 MB/file, 1 GB/lesson), not fixed business invariants. *(new 2026-07-21)*

## 8. Admin & Payout Rules

- **BR-070** — Submitting a course for review moves it to "Pending Approval," visible in the admin queue but still hidden from the student catalog. *(PRD §11 Admin Moderation & Payouts)*
- **BR-071** — Admin approval moves a course to "Published" (visible in the catalog) and notifies the instructor. *(PRD §11 Admin Moderation & Payouts)*
- **BR-072** — Admin rejection requires a reason/comment, reverts the course to "Draft" (still hidden from the catalog), and the instructor sees the reason to revise and resubmit. *(PRD §11 Admin Moderation & Payouts)*
- **BR-073** — The payout screen shows instructor revenue itemized by course/purchase, with gateway fees and refunds already deducted, before an admin can mark it "Payout Approved" and later "Paid" with a reference note. *(PRD §11 Admin Moderation & Payouts)*
- **BR-074** — No earnings figures are ever exposed on the instructor-facing UI — payouts are visible to admins only. *(PRD §4 Scope, §11 Admin Moderation & Payouts)*

## 9. Access Control / Roles Matrix

| Action | Student | Instructor | Admin |
|---|---|---|---|
| Watch a purchased/enrolled course | ✓ | — | ✓ (audited preview, see BR-081) |
| Create/edit own course | ✗ | ✓ (own courses only) | ✓ |
| Publish a course | ✗ | ✗ (admin-gated) | ✓ |
| View own purchase history | ✓ | — | — |
| View own per-course analytics | — | ✓ (own courses only) | ✓ (all courses) |
| View other instructors' course data | — | ✗ | ✓ |
| Initiate a refund | ✗ | ✗ | ✓ |
| Manage instructor payouts | — | ✗ (no self-service view) | ✓ |
| Manage users (suspend, etc.) | ✗ | ✗ | ✓ |
| Moderate reported content | ✗ | ✗ | ✓ |
| Manage coupons (create/edit/deactivate) | ✗ | ✗ | ✓ (see BR-124) |
| Redeem a coupon at checkout | ✓ | — | — |
| Create/edit an office-hours session | ✗ | ✓ (own PUBLISHED courses, BR-134) | ✓ (any course or platform-wide) |
| Join a live office-hours session | ✓ (if entitled, BR-135) | — | ✓ (moderation) |
| Cancel an office-hours session | ✗ | ✓ (own only) | ✓ (any, BR-137) |

- **BR-080** — This matrix is built strictly from the role-scoped language already in the PRD (e.g. "their own courses," "per-course," admin "manage everything") — it is the authoritative source for the authorization layer. *(PRD §3 Target Users, §5 Student/Instructor/Admin Features)*
- **BR-081** — An admin may watch any course's video content — including PENDING_APPROVAL and DRAFT lessons under active review — without holding a student enrollment record. This is a distinct, audited authorization path from student playback (BR-050): every admin preview request is logged (admin ID, lesson ID, timestamp) and never creates or requires an enrollment record. *(New 2026-07-20 — resolves the roles-matrix "Admin oversight" cell against BR-050's enrollment-only rule; admin course review during BR-070/BR-071 approval requires watching lesson content that has no enrollment yet.)*

## 10. Content Lifecycle

- **BR-090** — Course status machine: `DRAFT → PENDING_APPROVAL → PUBLISHED → ARCHIVED`. Rejection at `PENDING_APPROVAL` returns the course to `DRAFT`. Editing a `PUBLISHED` course (BR-017) does **not** transition the course's own status — the course stays `PUBLISHED` throughout — it instead creates a pending-revision record that goes through its own `PENDING_APPROVAL`-equivalent review and, on approval, applies its changes to the live course atomically. `ARCHIVED` is reachable only from `PUBLISHED` and is terminal — per BR-018, only courses with enrollments are archived; zero-enrollment courses are deleted instead. *(PRD §11 Admin Moderation & Payouts; ARCHIVED state added 2026-07-20 per BR-018; pending-revision model corrected 2026-07-20 to match BR-017 — a course-status transition can't represent "hidden pending" and "still live from last-approved version" at the same time.)*
- **BR-091** — Video status machine: `DRAFT → UPLOADING → UPLOADED → QUEUED → PROCESSING → READY → PUBLISHED`, with a `FAILED` branch from `PROCESSING` (auto-retry or manual retry) and a `CANCELLED` branch from `QUEUED`/`PROCESSING` (nice-to-have, not v1-blocking). *(video-streaming-design.md §3)*

> Order, Payment, Refund, Enrollment, Instructor, and User lifecycles are good candidates to add here once those flows are designed in enough detail to state as state machines — noted for later, not needed for v1.

## 11. Security & Data Rules

- **BR-100** — Signed URLs for video manifests/segments are session-scoped and short-lived — re-issued per playback session, never cached long-term client-side — rather than literally single-use: HLS playback requires repeated requests to the same segment within one session (seeking, rebuffering, ABR rendition switches), which true single-use would break. Lab-material download URLs MAY be single-use where the storage/CDN layer supports it, since a one-time file download has no repeat-access requirement. *(PRD §6 Security; single-use language corrected 2026-07-20 to match how HLS playback actually works — see [video-streaming-design.md](superpowers/specs/2026-07-17-video-streaming-design.md) §5's token-based manifest proxy.)*
- **BR-101** — Only authorized admin roles can view student PII; sensitive PII (national ID, phone, address) is encrypted at rest and TLS-protected in transit. *(PRD §6 Security)*
- **BR-102** — Signed-URL issuance and download endpoints are rate-limited and monitored per user/IP to detect bulk-scraping or credential-sharing. *(PRD §6 Security)*
- **BR-103** — Downloadable **lab materials** carry an embedded per-purchase identifier (e.g. student ID watermark) to trace leaks back to source; v1 has no DRM, so this is deterrence-based, not encryption-based. **Lesson resources** (slides/notes) are entitlement-gated and rate-limited (BR-063, BR-102) but not individually watermarked — lower-value, and slide/image formats tag poorly. *(PRD §9 Risk 3; resources-excluded clause added 2026-07-21 per [DECISIONS.md](DECISIONS.md) D-011)*

## 12. Data Integrity Rules

These are structural invariants — they translate directly into database constraints, API validation, and service-layer checks.

- **BR-110** — Every lesson belongs to exactly one section. *(Formalizes BR-010; new 2026-07-20)*
- **BR-111** — Every section belongs to exactly one course. *(Formalizes BR-010; new 2026-07-20)*
- **BR-112** — Deleting a course cascades to its sections and lessons only when BR-018 permits deletion at all (zero enrollments) — a course with any enrollment can only be Archived, so cascade-delete never applies to enrolled content. *(Formalizes BR-018; new 2026-07-20)*
- **BR-113** — An enrollment must reference an existing user and an existing purchasable item (course, chapter, or bundle). *(Formalizes the enrollment model implied throughout PRD §11 Purchase & Payment; new 2026-07-20)*
- **BR-114** — A progress record cannot exist without a corresponding enrollment. *(Formalizes BR-023's entitlement-before-access model; new 2026-07-20)*
- **BR-115** — Every lesson resource and every lab material belongs to exactly one lesson and is removed with that lesson; deletion follows the same enrollment/archival constraint as the lesson's course (BR-018, BR-112). *(Formalizes BR-063/BR-067; new 2026-07-21)*

## 13. Notification Rules

- **BR-120** — Notification delivery is best-effort, not guaranteed: a failed or delayed send never blocks, rolls back, or alters the triggering action. Course publish (BR-071), enrollment grant (BR-020), and password reset all succeed regardless of whether their notification is delivered. *(Decision 2026-07-21; see [DECISIONS.md](DECISIONS.md) D-010)*
- **BR-121** — A purchase/enrollment confirmation notification fires only after the payment gateway's success webhook has granted the enrollment (BR-020) — never on client-side redirect, and never for a declined, cancelled, timed-out, or still-pending order. *(Ties BR-020; new 2026-07-21)*
- **BR-122** — Transactional notifications (course approval/rejection, purchase receipt, password reset) are sent without a marketing-consent check. Lifecycle and marketing/broadcast notifications (post-launch) require the recipient's explicit opt-in and must honor unsubscribe, per Kuwait PDPL No. 26/2024. *(Decision 2026-07-21; PRD §5 Notifications, §7 Legal Constraints; see [DECISIONS.md](DECISIONS.md) D-010)*
- **BR-123** — The in-app notification center is the durable per-recipient record (read/unread state); the email channel is a best-effort mirror. A notification is considered recorded once written to the in-app center, independent of email-send outcome. *(Decision 2026-07-21; PRD §5 Notifications; see [DECISIONS.md](DECISIONS.md) D-010)*

## 14. Coupon Rules

- **BR-124** — Coupons are created and managed by admins only; instructors cannot create, apply, or discount via coupons in v1. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-125** — A coupon's discount is either a percentage (1–100) or a fixed amount in fils. The computed discount is integer fils, rounded to the nearest fil for percentages, and clamped to the range `[0, subtotal]` — it can never be negative and never exceed the item price. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-126** — A coupon that reduces the order to 0 KWD grants enrollment directly, with no payment-gateway call. This free-grant path still creates a real order and enrollment, carries the same entitlement checks (BR-023) and the same 150-day term (BR-025) measured from grant time, and is idempotent per BR-024. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-127** — At most one coupon applies per order; coupons do not stack. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-128** — Coupon validity — active, within `[valid_from, valid_until]`, scope matches the purchased item, global cap not reached, per-user limit not exceeded — is enforced hard at order creation, and the resulting discount and total are snapshotted onto the order (locking the amount the gateway will charge). *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-129** — A coupon's redemption count is committed only on payment success or free-grant, within the same transaction that grants enrollment and keyed by the gateway idempotency/transaction ID (BR-033), so a duplicate or replayed webhook cannot double-count. The global redemption cap is therefore *soft* under concurrency (an already-priced order is still honored if the cap fills between order creation and webhook); the per-user limit is *exact*, enforced by a unique `(coupon, user)` constraint together with BR-024. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-130** — Once a coupon has any redemption, its `code`, `discount_type`, and `discount_value` are immutable; only `is_active`, `valid_until`, `max_redemptions`, and its target scope remain editable. A coupon with any redemption may be deactivated but not deleted; a coupon with zero redemptions may be deleted outright (mirrors BR-018). *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-131** — A refund on a coupon order returns the amount actually charged (the discounted total), not the pre-discount subtotal. The redemption record is retained and the count is not decremented, consistent with BR-045 (analytics reflect what happened). *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-132** — Coupon codes are stored uppercase and trimmed and matched case-insensitively; a unique index on the code prevents duplicates. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*
- **BR-133** — Coupon create/edit/deactivate actions and every redemption are logged for audit, consistent with the refund-audit (BR-042) and admin-preview (BR-081) discipline. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-012)*

## 15. Live Office Hours Rules

- **BR-134** — A live office-hours session is created either by the course's own instructor (on an own PUBLISHED course only) or by an admin (on any course, or platform-wide with no course attached). *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-013)*
- **BR-135** — Session access: a course-scoped session requires the requester to hold an active, non-expired enrollment in that course (same entitlement as BR-023/BR-025 — a lapsed 150-day enrollment is denied). A platform-wide session is restricted per the admin's per-session audience toggle: either any user with ≥1 active enrollment, or any authenticated user. Admins may always access for moderation. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-013)*
- **BR-136** — The session join link is revealed only to an entitled user, and only within the window `[scheduled_start − 15 min, scheduled_end]` while status is `scheduled`. It is never rendered on public or catalog pages. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-013)*
- **BR-137** — Office-hours sessions publish immediately, with no admin-approval gate (unlike course content, BR-070). Admin moderation is reactive — an admin may cancel any session. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-013)*
- **BR-138** — A suspended instructor (BR-065) cannot create or edit office-hours sessions, consistent with suspension blocking new submissions. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-013)*
- **BR-139** — Cancelling a session is not deletion: cancelled sessions are retained for audit and hidden from upcoming lists. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-013)*
- **BR-140** — Session create / reschedule / cancel fire best-effort, event-driven notifications (BR-120) to entitled students. Platform-wide sessions whose audience is "open" (any authenticated user) do not mass-notify — they rely on the in-app upcoming list only — to avoid notifying non-purchasers, consistent with the PDPL consent posture of BR-122. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-013)*
- **BR-141** — Session times are stored in UTC and displayed in Kuwait local time (UTC+3), consistent with BR-025. *(Decision 2026-07-22; see [DECISIONS.md](DECISIONS.md) D-013)*
