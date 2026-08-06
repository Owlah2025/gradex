# Decision Log

> Status: Active
> Last Updated: 2026-08-02 (real calendar)

Central record of significant product/technical decisions for Gradex — what was decided, why, and what alternatives were rejected. This is the single source of truth for decisions; [PROJECT_VISION.md](PROJECT_VISION.md) §21 points here rather than keeping its own copy.

---

## D-001 — Own-build HLS video pipeline

**Date:** 2026-07-17
**Decision:** Build the video upload/transcode/playback pipeline in-house (Go backend + Redis job queue + FFmpeg workers + S3-compatible storage, adaptive-bitrate HLS, and short-lived authorized playback). A CDN remains a system-design/deployment decision, not a claim about the current repository.
**Reason:** Full control over the upload → transcode → playback flow and the auth/entitlement checks gating it; see [video-streaming-design.md](superpowers/specs/2026-07-17-video-streaming-design.md) for the full design.
**Alternatives rejected:** Not recorded — no vendor comparison was documented at the time this spec was written.

## D-002 — Tap Payments for MVP checkout; Deema BNPL is fast-follow

**Date:** 2026-07-20
**Status:** Deferred out of MVP on 2026-07-28 by
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
No payment gateway is integrated for launch. The provider analysis and the BNPL rejection remain the
approved starting point whenever online payment is taken up.
**Decision:** MVP checkout uses Tap Payments hosted card/KNET payments. Deema BNPL remains Fast-Follow and is not a launch dependency. MyFatoorah is considered only if Tap cannot activate Gradex's digital-course merchant account; it is not integrated speculatively in MVP.
**Reason:** One hosted gateway keeps MVP payment and reconciliation behavior bounded. BNPL adds entitlement and payment-state branches and still requires written digital-goods approval, so it must not delay the core checkout path.
**Alternatives rejected:** PayTabs — its Kuwait "installment" offering is a reseller layer over the same Deema product Tap offers directly, with no upside and added integration overhead.
**Source:** [PRD.md §5 Payments](PRD.md)

## D-003 — GritCMS MediaKit rejected as a video-infra vendor

**Date:** 2026-07-20
**Decision:** Do not use MediaKit as a replacement for the own-build video pipeline (D-001).
**Reason:** The documented install/API path was verified hands-on and did not produce or expose the claimed backend: the API base returned 404 and the official scaffolder contained no MediaKit-specific routes. The obtainable package was frontend-only, so it could not replace the required service.
**Alternatives rejected:** N/A — MediaKit itself was the alternative being evaluated against D-001, and it was rejected.
**Source:** Full repository-local history in [2026-07-20-mediakit-spike-plan.md](superpowers/specs/2026-07-20-mediakit-spike-plan.md). The original external workspace-memory link was removed because it was not portable or available to repository readers.

## D-004 — Labs ship as downloadable files, not sandboxed execution

**Date:** 2026-07-20
**Decision:** Hands-on labs ship as downloadable project files + a written guide.
**Reason:** Avoid building expensive sandboxed in-browser code-execution infrastructure before validating the core video-course product with real students.
**Alternatives rejected:** Sandboxed in-browser code execution.
**Source:** [PRD.md §4 Scope](PRD.md), [PROJECT_VISION.md §9 Non-Goals](PROJECT_VISION.md)

## D-005 — Community is an external Discord/Telegram link-out

**Date:** 2026-07-20
**Decision:** The course community lives on an external Discord/Telegram server, linked from the platform — not an in-platform forum or comment system.
**Reason:** Avoid building and moderating in-platform community infrastructure before validating the core product.
**Alternatives rejected:** In-platform forum/comment system.
**Source:** [PRD.md §5 Student Features](PRD.md), [PROJECT_VISION.md §9 Non-Goals](PROJECT_VISION.md)

## D-006 — Instructor payouts are admin-managed only

**Date:** 2026-07-20
**Decision:** No instructor-facing earnings/payout dashboard in MVP; admin views and processes all payouts, and instructors receive a monthly statement by email.
**Reason:** Keep MVP lean—avoid building a self-service earnings dashboard before the platform has real revenue to show.
**Alternatives rejected:** Self-service instructor earnings dashboard (deferred to a future version, not rejected outright).
**Source:** [PRD.md §4 Scope](PRD.md), [PRD.md §9 Risk 6](PRD.md)

## D-007 — Course completion certificates deferred

**Date:** 2026-07-20
**Decision:** Course completion certificates are outside MVP.
**Reason:** Keep the launch scope focused on purchase, learning, practice, and follow-up.
**Alternatives rejected:** N/A — straightforward deferral.
**Source:** [PRD.md §4 Scope](PRD.md), [PROJECT_VISION.md §9 Non-Goals](PROJECT_VISION.md)

## D-008 — MVP keeps the Instructor portal; bundles and BNPL move to Fast-Follow

**Date:** 2026-07-20
**Decision:** The Instructor portal (invitation/auth, own-Course CRUD, Section/Lesson management, video/resource/lab upload, submit-for-review, and status) stays in MVP. Bundles and Deema BNPL move to Fast-Follow after launch.
**Reason:** Cut real build-time risk against the solo-developer, ~3.5-week timeline to the 2026-08-15 launch date ([PRD.md §9 Risk 7](PRD.md)) — without touching the instructor supply-side differentiator, which is core to the business model, not optional. Bundles and installments add real purchase/entitlement/checkout branching complexity that the 8–12 launch courses don't strictly need on day one.
**Alternatives rejected:** Admin-only Course creation for launch; permanently ruling out BNPL before provider and Student-demand validation.
**Source:** Approved scope reduction; see [PRD.md §4 Scope](PRD.md).

## D-009 — Enrollment access is per-semester, not lifetime

**Date:** 2026-07-20
**Status:** Superseded on 2026-07-26 by D-026. This entry preserves the original fixed-duration
decision for history.
**Decision:** Course/Section access expires 150 days (~5 months, approximating one academic semester) after the purchase timestamp, calculated in Kuwait local time (UTC+3), valid through the end of day 150, rather than lasting indefinitely. MVP ships silent expiry — access simply ends, no dedicated renewal flow — since a lapsed student can regain access through the normal purchase flow (see [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-024/BR-025). Future bundle entitlements must adopt an explicit duration before bundle purchase ships. *(Canonical term changed from “chapter” to Section on 2026-07-23; exact day count/timezone/boundary remains unchanged.)*
**Reason:** matches how the target student actually uses the product — access tied to the university course/semester they're taking right now — better than an open-ended lifetime default, and avoids building a separate renewal/repurchase flow before launch.
**Alternatives rejected:** lifetime access (the more common course-platform default, and the initially recommended option — rejected in favor of a semester-aligned term that better fits how Gulf university students actually consume this content); building a dedicated renewal flow in MVP (rejected — real added scope against the 3.5-week timeline; repurchase through the standard checkout covers this for now).
**Source:** This session; see [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-025.

## D-010 — Notifications: transactional-only MVP on email + in-app center; lifecycle/marketing deferred

**Date:** 2026-07-21
**Decision:** MVP ships a fixed notification policy using a minimal in-app center plus email where required. The source transaction relationally snapshots each exact `(Account, channel)` recipient so delayed delivery never recalculates the audience. In-app + email events are purchase receipt, refund/reconciliation status, password/security events, Account invitation, Course approval/changes requested, office-hours cancellation/material rescheduling, Admin Entitlement expiry adjustment, and emergency Course access suspension/restoration. New office-hours sessions (to currently entitled Students) and new Instructor Course/revision submissions (to Admin operations) are recorded in-app and may also use email when operationally appropriate. Video-processing completion targets the Instructor. Mandatory transactional/security messages cannot be disabled; operational notices follow fixed product channel policy. Marketing, preferences, WhatsApp/SMS, and push are post-MVP. Email failure never invalidates the durable in-app record. *(Expanded 2026-07-26 for exact audience snapshots and operational events.)*
**Reason:** These messages complete existing account, commerce, moderation, and office-hours flows without creating a segmentation or preference engine. The in-app record remains durable; email delivery remains best-effort and never controls the underlying transaction.
**Alternatives rejected:** Email-only MVP; granular notification settings; lifecycle/marketing automation; WhatsApp/SMS; push.
**Source:** This session; see [PRD.md §5 Notifications](PRD.md), [PRD.md §4 Scope](PRD.md), and [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-120–BR-123.

## D-011 — Lesson resources split from lab materials; both MVP, labs-only watermark

**Date:** 2026-07-21
**Decision:** Split per-lesson downloadable attachments into two distinct categories: **lesson resources** (supplementary reference to consume — slides, notes, readings; allowed types PDF, slides (PPT/PPTX), images) and **lab materials** (hands-on practice — project files + a written guide; allowed types archives (ZIP), common project files, plus a PDF/Markdown guide). Both ship in MVP, share the same upload/storage/signed-URL/entitlement plumbing, and are optional per lesson. Upload size caps: lesson resources 50 MB per file / 200 MB per lesson; lab materials 250 MB per file / 1 GB per lesson (set 2026-07-21; tunable in implementation, distinct from video's own cap). The per-purchase watermark/buyer-tag (BR-103) applies to lab materials only; lesson resources are entitlement-gated and rate-limited but not watermarked.
**Reason:** Slides/notes and hands-on lab files differ in purpose (consume vs. do) and value (labs are the paid differentiator most worth pirating). A single "lab materials" bucket conflated them and left non-lab PDFs like lecture slides with no clean home. Splitting is near-zero added infrastructure — the same download pipeline with a category flag — while giving each bucket its own allowed-type list and anti-piracy posture. Watermarking labs only keeps the anti-piracy effort on the high-value target; slide/image formats carry per-buyer tags poorly and are lower-stakes.
**Alternatives rejected:** Single "lab materials" bucket for everything (rejected — conflates reference material with hands-on labs, no home for slides/notes); deferring lesson resources to fast-follow (rejected — same plumbing as labs, near-zero marginal cost to include at launch); watermarking both buckets (rejected — slide/image formats don't carry per-buyer tags cleanly, and resources are lower-value than labs).
**Source:** This session; see [PRD.md §4 Scope](PRD.md), [PRD.md §11 Instructor Course Builder](PRD.md), and [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-063, BR-066, BR-067, BR-103, BR-115.

## D-012 — Coupons in MVP: admin-only discount codes applied pre-gateway

**Date:** 2026-07-22
**Status:** Deferred out of MVP on 2026-07-28 by
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation);
coupons require a checkout Gradex no longer has. Capacity/commit timing was amended on 2026-07-26 by
D-028. The approved scope, ownership, discount behavior, targets, and refund-release rule remain the
design of record for the deferred feature.
**Decision:** Add an admin-managed coupon system to MVP. Admins (only) mint percentage or fixed-amount codes (integer fils), optionally scoped to Course(s)/Section(s) or platform-wide. A code is validated and applied server-side before the Tap payment session; a zero-value order grants entitlement without a gateway call. One coupon applies per order. Each Student may consume a code once at a time: failed/abandoned attempts do not consume it, and a fully refunded purchase releases that Student's redemption eligibility while retaining the historical redemption/refund records. Global caps remain configurable; per-user limits greater than one are not supported. Coupons never modify catalog prices. *(Section terminology and redemption/refund behavior amended 2026-07-23.)*
**Reason:** Launch promos and seeding free access are standard go-to-market levers, and the insertion point is clean—the server computes the Order amount before hosted checkout. Admin-only matches the BR-019 pricing boundary and prevents an Instructor discount side door. Full design in [coupons-system-design.md](superpowers/specs/2026-07-22-coupons-system-design.md).
**Alternatives rejected at the time:** Instructor-created coupons; disallowing free codes; capacity
reservation at checkout. The reservation rejection was superseded by D-028 after payment-race
review showed that exact pre-payment capacity is required.
**Source:** This session; see [PRD.md §4 Scope](PRD.md), [PRD.md §5 Payments](PRD.md), and [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-124–BR-133.

## D-013 — Live office hours in MVP (external-link only) — reverses the live-sessions deferral

**Date:** 2026-07-22
**Decision:** Add lightweight, Course-scoped live office hours to MVP. Instructors create/materially reschedule one-off sessions only for their own PUBLISHED Courses; an owner may still cancel an existing scheduled Session after the Course is Delisted/Archived. Gradex owns scheduling, entitlement checks, and event-driven notices, while audio/video uses an external Zoom/Meet/Discord link. An uncancelled Session is time-derived as UPCOMING before start, LIVE during its half-open scheduled window, and ENDED afterward; time never proves attendance or delivery. Join-link access is limited to the authorized LIVE window, while qualifying historical Session/material access may remain afterward. Delisting/retirement/archival alone does not hide that history; cancellation blocks joining without deleting Session, notification, attendance/delivery, or Audit evidence. Admins may cancel for moderation but do not create platform-wide sessions. No RSVP, recurrence, timed reminders, platform attendance capture, recordings, calendar integration, or in-platform video ships in MVP. *(Lifecycle/access semantics amended 2026-07-26 by D-029 and Section 4.)*
**Reason:** Course-scoped external links directly support follow-up without adding live-video infrastructure or a platform-wide event/audience model. The reconciled design is in [live-office-hours-design.md](superpowers/specs/2026-07-22-live-office-hours-design.md).
**Alternatives rejected:** Platform-wide sessions in MVP; in-platform video; RSVP/capacity; recurring series; timed reminders; attendance and recordings.
**Source:** This session; see [PRD.md §4 Scope](PRD.md) and [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-134–BR-141.

## D-014 — Student-only registration and admin-provisioned staff accounts

**Date:** 2026-07-23
**Decision:** Public registration creates Student accounts only and requires email verification before sign-in. Instructors and additional Admins are invited by an existing Admin; the Account is created only when the invitation is accepted. An invitation cannot target an email already attached to any Account. Every MVP Account has exactly one role assigned at creation, and that role is immutable: MVP does not convert roles, merge identities, or support multi-role Accounts. Student Accounts alone can purchase, receive Entitlements, and record Progress. Instructor Accounts author assigned content but do not consume Student content. Admins use the separate audited preview path and never receive ordinary Student Entitlements or Progress. A person who needs separate capabilities uses a separate Account with another normalized email. One bootstrap Admin is created once through a secure deployment operation, has no credential in the repository, and must change the initial password. Passwords allow 15–128 Unicode characters, reject common/compromised values, use Argon2id, and have no composition or periodic-rotation rule. Account suspension blocks all protected actions immediately, including existing sessions; system design selects the enforcement mechanism.
**Reason:** This keeps privileged-role creation controlled, ensures account recovery/receipts use a verified address, and defines the security outcome before choosing token/session mechanics.
**Alternatives rejected:** Public Instructor/Admin signup; creating placeholder Accounts when invitations are sent; implicit/partial role conversion or multi-role Accounts; repository-stored bootstrap credentials; composition rules; suspension delayed until refresh-token expiry.
**Source:** Approved documentation reconciliation; see [documentation-reconciliation-design.md](superpowers/specs/2026-07-23-documentation-reconciliation-design.md) §4.2.

## D-015 — Section is canonical; Admin owns all catalog pricing

**Date:** 2026-07-23
**Status:** Amended on 2026-07-28 by
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)
**on purchasable scope only.** MVP grants `COURSE` scope exclusively; Section is not an acquirable
scope at launch and Section prices are retained in schema and the Admin surface but not displayed in
the student-facing catalogue. The `Course → Section → Lesson` hierarchy, the Chapter labelling rule,
and Admin-exclusive pricing authority are unchanged and remain in force.
**Decision:** The only content hierarchy is `Course → Section → Lesson`. “Chapter” may be a localized/student-facing label for Section but is not a separate entity. Students may buy one Course or one Section per Order. A Course Entitlement covers all its Sections; a Section Entitlement covers only that Section. A Student with a Section may later buy another Section or the Course, but MVP gives no automatic upgrade credit/proration. Admins exclusively set/change Course and Section prices; Instructors have read-only price visibility. Price changes affect future orders only and are audited.
**Reason:** The repository already implements Sections, while treating Chapter as separate would create a second overlapping domain entity. Admin pricing prevents Instructor content edits from changing commercial terms.
**Alternatives rejected:** Separate Chapter and Section entities; Instructor-controlled pricing; retroactively changing transaction values.
**Source:** Approved documentation reconciliation; see [documentation-reconciliation-design.md](superpowers/specs/2026-07-23-documentation-reconciliation-design.md) §4.3.

## D-016 — Responsive Arabic-first bilingual website

**Date:** 2026-07-23
**Decision:** MVP is a responsive website providing the complete Student experience on phones, tablets/iPads, laptops, and desktops. Instructor/Admin experiences remain responsive but complex operations are desktop/tablet optimized. Arabic and English are supported for every role, Arabic is the initial default, preference persists, and layouts support RTL/LTR. Platform-owned UI/player controls target WCAG 2.2 AA. Captions/transcripts remain fast-follow, so Gradex will not claim complete product-level WCAG conformance in MVP.
**Reason:** The target audience learns across device classes, and both RTL and accessible interaction need to be part of initial design. The scoped conformance wording is honest about the approved media-accessibility boundary.
**Alternatives rejected:** Native apps in MVP; phone-only/mobile-only positioning; English-only launch; unsupported full-product WCAG claims.
**Source:** Approved documentation reconciliation; see [documentation-reconciliation-design.md](superpowers/specs/2026-07-23-documentation-reconciliation-design.md) §4.13–4.14.

## D-017 — Full and partial refunds with counsel-approved eligibility

**Date:** 2026-07-23
**Status:** Deferred out of MVP on 2026-07-28 by
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
Gradex processes no refunds; any refund is an External Payment matter handled outside the platform.
The counsel-approved eligibility requirement is **not** deferred — it remains part of `LG-011`'s
published Refund Policy.
**Decision:** Admins can request one or more full/partial refunds up to the remaining captured balance. Partial success keeps entitlement active; cumulative successful refunds equal to the captured amount revoke it. State changes only after confirmed gateway success. Amount, reason, Admin, gateway reference, status, and history are audited. Refund-policy eligibility is configurable and must be approved by Kuwaiti counsel; the product will not assume that streaming automatically removes refund rights.
**Reason:** Tap supports amount-controlled refund requests but may reject partial refunds for some payment methods. Separating eligibility from processing lets system design proceed while legal interpretation remains a launch gate.
**Alternatives rejected:** Full-refund-only MVP; immediate access revocation on request; unverified “content accessed means no refund” language.
**Source:** Approved documentation reconciliation; Tap [refund API](https://developers.tap.company/reference/create-a-refund) and [response codes](https://developers.tap.company/reference/charge-response-codes).

## D-018 — Manual monthly payouts with system-recorded accounting

**Date:** 2026-07-23
**Status:** Deferred out of MVP on 2026-07-28 by
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
With no in-platform revenue record, no earning can be calculated, so the ledger, Statement, and
transfer lifecycle do not ship. The **contractual** obligation is not deferred: `LG-020`'s Instructor
agreement still requires revenue-share terms, and payment to Instructors is an entirely out-of-band
founder operation at launch.
**Decision:** MVP uses one platform-wide Instructor revenue-share percentage, configured before launch with no assumed default. Share is calculated from net collected revenue after coupons, confirmed refunds, and payment fees. Every earning, fee, Refund, chargeback, payout adjustment, carry-forward, and approved correction is an immutable source-linked ledger entry; corrections append compensating entries. One monthly Statement exists per Instructor/currency/period. `DRAFT → READY_FOR_REVIEW → APPROVED → PAYMENT_PENDING → PAID`, with review blocking and retryable payment-failure paths. Approval freezes included entries, totals, and approved payout destination; transfer initiation creates an immutable attempt using that destination; `PAID` requires verified full-payment evidence. Partial Statement payments and negative bank transfers are prohibited, negative balances carry forward, and late Refunds/chargebacks adjust a later period. Admins transfer manually and email the Statement. No Instructor payout dashboard or automated settlement ships in MVP. *(Detailed lifecycle amended 2026-07-26 by Sections 4/6; recipient snapshot remains D-030.)*
**Reason:** The accounting model must be designable before the commercial percentage is chosen, while automated settlement and per-course negotiation would add unnecessary launch scope.
**Alternatives rejected:** Hard-coded placeholder percentage; per-Course revenue-share rules; Instructor withdrawals; automated marketplace settlement.
**Source:** Approved documentation reconciliation; see [documentation-reconciliation-design.md](superpowers/specs/2026-07-23-documentation-reconciliation-design.md) §4.8.

## D-019 — Separate public preview and end-to-end content reporting

**Date:** 2026-07-23
**Decision:** Protected Lesson Resources/Lab Materials are never exposed as samples. An Instructor may optionally upload one separate, explicitly public Course preview asset, validated/quarantined/malware-scanned and covered by a permission confirmation. Entitled Students may report a Course, Lesson, video, Resource, Lab Material, or Office-Hours Session. Every report preserves the stable target and exact visible revision/version. Typed automated findings preserve source evidence but never auto-hide or retire content; Media quarantine/rejection and emergency security suspension stay separate safety workflows. Admins dismiss, request changes, delist, retire, invoke constrained emergency Course access suspension, cancel a Session, or use Account suspension with an audited reason and immutable result event. Duplicate/spam reports are rate-limited. *(Moderation evidence/actions amended 2026-07-26 by D-029 and Section 4.)*
**Reason:** This resolves the public sample-lab contradiction while retaining a safe evaluation asset, and completes the Student action required to feed the existing Reported Content queue.
**Alternatives rejected:** Publicly exposing protected labs; automatic removal on report; an Admin queue with no report origin.
**Source:** Approved documentation reconciliation; see [documentation-reconciliation-design.md](superpowers/specs/2026-07-23-documentation-reconciliation-design.md) §4.9–4.10.

## D-020 — Conservative privacy and legal-readiness posture

**Date:** 2026-07-23
**Decision:** Gradex uses bilingual Privacy/Terms/Refund/checkout disclosures, data minimization, hosted payment entry, encryption, secret/PII-safe logging, versioned consent records, and data-subject request handling. Instructor rosters expose only a Student-chosen display name/alias and Course-scoped learning fields, never direct contact/payment/legal-identity or cross-Course data; direct Student PII remains Admin-only. Retention periods, refund eligibility, privacy-regulation applicability, and consumer wording are counsel/accounting launch gates rather than invented rules. Destructive retention remains disabled until approval: exact-version remote deletion requires a serialized no-hold authorization, provider deletion beyond a delete marker, verification, and a confirming PostgreSQL transaction; pending/failed deletion is never reported complete. Documentation names CITRA Decision No. 26 of 2024 correctly and states that the official announcement describes Digital Commerce Law No. 10 of 2026 as applying six months after Gazette publication. *(Deletion boundary amended 2026-07-26 by Section 4.)*
**Reason:** System design needs data classes and lifecycle obligations, but unresolved legal interpretations must not be presented as settled product rules.
**Alternatives rejected:** Treating unverified legal summaries as enforceable requirements; postponing all privacy design; storing raw payment credentials.
**Source:** [CITRA Decision No. 26 of 2024](https://www.citra.gov.kw/sites/ar/Pages/DecisionsDetails.aspx?id=6), [Kuwait Government Digital Commerce Law announcement](https://e.gov.kw/sites/KGOArabic/Pages/ApplicationPages/NewsDetail.aspx?nid=64409149), and [documentation-reconciliation-design.md](superpowers/specs/2026-07-23-documentation-reconciliation-design.md) §4.15.

## D-021 — Explicit Course review and moderation lifecycle

**Date:** 2026-07-23
**Status:** Visibility/access semantics superseded on 2026-07-26 by D-029. The review/revision and
archival decisions remain in force.
**Decision:** First publication follows `DRAFT → PENDING_REVIEW → PUBLISHED`, with `CHANGES_REQUESTED` and resubmission when an Admin requires revision. An approved Course may be unpublished for moderation and republished after resolution. Unpublishing removes it from catalog/new purchase and temporarily blocks Student access to its protected content without deleting Entitlements or progress. Instructor changes to a Published Course use a separate pending revision so the approved live version is not silently changed. Courses with enrollment history are archived rather than deleted.
**Reason:** Publication readiness, Admin review, live visibility, temporary moderation, revision review, and terminal archival are different states and must not be collapsed into an ambiguous “status.”
**Alternatives rejected:** Reverting every rejection to Draft with no reason-bearing state; editing live content directly; deleting Courses with commercial history.
**Source:** Approved documentation reconciliation; see [documentation-reconciliation-design.md](superpowers/specs/2026-07-23-documentation-reconciliation-design.md) §4.4–5.

## D-022 — Catalog taxonomy is three Admin-controlled dimensions; Instructors select, never invent

**Date:** 2026-07-23
**Decision:** A Course is classified on exactly three dimensions: **Major**, **Subject**, and **Study Year**. Major and Subject are Admin-managed bilingual controlled vocabularies of Taxonomy Terms; Subject terms additionally carry an optional academic code (for example `CS 101`). Study Year is a fixed enumeration — `PREP`, `YEAR_1`, `YEAR_2`, `YEAR_3`, `YEAR_4`. Each Course carries exactly one Major, one Subject, and one Study Year. Only Admins create, rename, retire, or delete vocabulary terms, and every such action is audited; Instructors choose from existing terms while authoring and may not add new ones. An Admin may override any Course's assignment. Classification is required before a Course can be submitted for review. A retired term cannot be newly assigned but keeps existing Courses filterable until an Admin reassigns them; a term with zero referencing Courses may be deleted outright.
**Reason:** [SCREENS.md](SCREENS.md) ST01 already promises subject/major/year catalog filtering, but no entity existed to back it, so system design had nothing to model. Three fixed dimensions keep filter queries exact-match and trivial for a solo developer, and matching the vocabulary boundary to the D-015 catalog-versus-content split keeps discovery quality Admin-owned while leaving per-Course data entry with the Instructor who actually knows the subject.
**Alternatives rejected:** A single flat Category list plus a difficulty level (rejected — drops the major/year filters SCREENS commits to); a generic polymorphic Tag entity (rejected — join-heavy filters, kind validation pushed into the application layer, and moderation scope this team cannot absorb); free-text Instructor entry (rejected — produces duplicate and mistranslated bilingual terms immediately, which defeats controlled-vocabulary filtering); Admin assigning taxonomy during Course review (rejected — adds a manual step to every review and every correction).
**Source:** This session; see [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-157–BR-160, [DOMAIN_MODEL.md](DOMAIN_MODEL.md) §3, and [PRD.md §5](PRD.md).

## D-023 — Catalog search is bilingual, Arabic-normalized, and scoped to published Courses

**Date:** 2026-07-23
**Decision:** Catalog search matches a Student's query against Course title, authored description, owning Instructor display name, and the labels/code of the Course's assigned Taxonomy Terms — in **both** Arabic and English regardless of the currently selected interface language — and returns only `PUBLISHED` Courses. Matching is case-insensitive and applies Arabic normalization: strip tashkeel/diacritics and tatweel, fold alef variants (`أ إ آ ٱ` → `ا`), alef maqsura (`ى` → `ي`), taa marbuta (`ة` → `ه`), and Arabic-Indic digits (`٠–٩` → `0–9`). Results are ranked by relevance only; there is no personalization, recommendation, or paid placement in MVP. Search combines with the D-022 filters. The retrieval mechanism (PostgreSQL full-text search with an Arabic configuration, trigram similarity, a materialized search column, or an external index) is a system-design choice, not a product decision.
**Reason:** Search appeared in the screen inventory with no stated behavior, leaving the single highest-consequence storage decision in the catalog unanchored. Arabic normalization in particular cannot be retrofitted cheaply: an unnormalized index silently fails to match the same word typed with a different hamza or with diacritics, which is the normal case for the Arabic-default audience under D-016.
**Alternatives rejected:** Searching only in the active interface language (rejected — Gulf students mix Arabic and English terms in one query, and Instructor-authored content is not translated under BR-150); exact-substring matching with no normalization (rejected — fails the majority Arabic case); including Lesson titles or protected content in the index (rejected — leaks structure of paid content into public results); naming a specific search engine here (rejected — belongs to system design under Principle VI).
**Source:** This session; see [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-161–BR-162 and [SCREENS.md](SCREENS.md) ST01.

## D-024 — Students carry a non-unique, self-chosen display name

**Date:** 2026-07-23
**Decision:** Every Account has a display name that the owner chooses and may change at any time. It defaults to the name supplied at registration or invitation acceptance, is **not** unique (identity remains the normalized email and the internal identifier), accepts 2–50 characters in either script, and rejects URLs, control characters, and markup. The display name is the only identity field an Instructor roster exposes under BR-064; it must never be required to contain legal identity. An Admin may reset an abusive display name through the existing audited moderation path, and the reset is recorded like any other privileged action.
**Reason:** BR-064, [PRD.md §5](PRD.md), and [SCREENS.md](SCREENS.md) IN07 all depend on a "Student-chosen display name/alias," but the Account entity defined no such attribute, so the privacy boundary those rules describe had nothing concrete behind it. Keeping it non-unique avoids inventing a global namespace — with availability checks, reserved words, squatting, and an RTL/ASCII handle policy — for zero MVP benefit.
**Alternatives rejected:** A globally unique handle (rejected — a whole namespace and its UX for no MVP requirement); a system-generated opaque pseudonym such as "Student 4821" (rejected — makes rosters useless for the office-hours and community follow-up that is the product's stated differentiator); reusing legal identity in rosters (rejected — contradicts BR-101/D-020 PII minimization).
**Source:** This session; see [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-105, [DOMAIN_MODEL.md](DOMAIN_MODEL.md) §2, and [PRD.md §5](PRD.md).

## D-025 — Modular monolith on a split managed PaaS

**Date:** 2026-07-25
**Decision:** Deploy the Gradex modular monolith as an edge-hosted Next.js frontend, a separately scalable Go API and Go worker, managed PostgreSQL as authority, managed Redis as disposable queue/cache infrastructure, and managed object storage/CDN. PostgreSQL preserves enough state, lease, attempt, error, and completion evidence to reconstruct every pending asynchronous/scheduled item after Redis loss; delivery is at least once and consumers are idempotent. Ambiguous external side effects are reconciled before retry. External providers remain behind configurable adapters. The launch is single-region and avoids Kubernetes, speculative microservices, and fixed overprovisioning. Production approval requires no unresolved critical defects; a high-severity defect requires documented risk acceptance, mitigation, and owner approval. *(Durable-work boundary amended 2026-07-26 by Section 4.)*
**Reason:** This structure preserves the working repository stack and own-build video pipeline while minimizing operational work for one developer. It allows the frontend, API, worker, data, and media paths to scale independently without making open provider, budget, legal, load, or recovery choices permanent.
**Alternatives rejected:** A unified managed PaaS (rejected because it increases platform coupling and narrows region/runtime choices); cloud-managed primitives (rejected because their networking, identity, and operational burden is too high for the August 15 launch); a self-managed production host (rejected because it conflicts with the approved low-operations priority).
**Source:** Approved platform architecture; see [2026-07-25-platform-architecture-design.md](superpowers/specs/2026-07-25-platform-architecture-design.md).

## D-026 — Course-configured semester expiry with audited Entitlement adjustments

**Date:** 2026-07-26
**Status:** Amended on 2026-07-28 by
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)
**on the disclosure and snapshot trigger only.** There is no checkout, so the precondition becomes:
a Course MUST have a future `default_access_ends_at` before a Course Access Invitation for it can be
**approved**, and Admin Approval snapshots that exact instant onto the Entitlement as
`original_access_ends_at`. Everything else — the separate effective `access_ends_at`, the audited
elevated-Admin adjustment, the Kuwait-local boundary conversion, and the rule that changing the
Course default never mutates an existing Entitlement — is unchanged and in force.
**Decision:** D-009's fixed 150-day duration is replaced by an Admin-managed Course
`default_access_ends_at` instant for future purchases. A Section has no independent access-period
override. Checkout discloses the exact expiry and the Order snapshots it; the granted Entitlement
stores both that original instant and its current authoritative `access_ends_at`. Access is allowed
only while `current_timestamp < access_ends_at`. An elevated Admin may extend or shorten an
individual Entitlement with a required reason and immutable adjustment/Audit history; changing the
Course default never silently changes an existing Entitlement. For a Kuwait-local calendar date,
the persisted exclusive boundary is the first instant of the following local day converted to UTC.
**Reason:** Semester end dates vary, and a Student may purchase near the end of a semester. An exact
disclosed expiry avoids granting an unintended fixed five-month term while separate original and
effective values preserve both the commercial agreement and later support decisions.
**Alternatives rejected:** Lifetime access; the superseded fixed 150-day term; Section-level expiry
overrides in MVP; dynamically changing existing Entitlements when the Course default changes;
unaudited expiry edits.
**Source:** Approved July 26 domain/data/state design Section 1; see
[the July 26 daily record](launch/daily/2026-07-26.md).

## D-027 — Every MVP Entitlement originates from an Order

**Date:** 2026-07-26
**Status:** **Superseded in full on 2026-07-28** by
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
Orders do not exist in MVP, so Entitlement provenance is a typed `grant_source` discriminator whose
only implemented value is `MANUAL_INVITATION`, created by Admin Approval. The **principle** D-027 was
protecting survives and is restated by D-045: an Entitlement never appears without a recorded,
audited grant source. This entry is retained for history and is not the current rule.
**Decision:** Every ordinary Course/Section Entitlement originates from exactly one Order. Paid
Orders grant only after verified payment success; a valid 100%/fixed-to-zero Coupon uses the
existing `FREE_GRANTED` Order path without contacting Tap. Admins may extend or shorten an existing
Entitlement but cannot create access through a separate manual-grant command.
**Reason:** A single commercial origin keeps expiry disclosure, Course revision snapshot, Coupon
history, access, receipts, refunds, reporting, and Audit evidence on one transaction model. Targeted
100% Coupons already provide auditable free seeding without a second grant workflow.
**Alternatives rejected:** Direct Admin Entitlement creation; an Entitlement with no Order origin;
using a zero catalog price as an implicit grant.
**Source:** Approved July 26 domain/data/state scope decision; see
[the July 26 daily record](launch/daily/2026-07-26.md).

## D-028 — Reserve Coupon capacity when Gradex accepts an Order

**Date:** 2026-07-26
**Status:** Deferred out of MVP on 2026-07-28 by
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation),
with D-012. It remains the design of record for coupon capacity whenever checkout is built.
**Decision:** A paid Coupon Order atomically reserves Coupon capacity when Gradex accepts its
immutable commercial terms. The reservation shares the Order payment deadline, counts against
global capacity, and blocks the Student's concurrent reuse. Verified timely capture consumes it in
the Entitlement transaction; Order expiry/cancellation releases an unused reservation. A
zero-value Coupon Order consumes immediately. Historical consumed count never decrements, including
after full Refund; full Refund only releases that Student's consuming eligibility. Orders have
explicit cancellation, expiry, and reconciliation-required outcomes, and retirement eligibility is
copied from the Order's pre-retirement `accepted_at`, while acquisition completion remains separately
recorded.
**Reason:** Deferring all Coupon capacity until capture can accept more discounted payments than the
cap can honor. Separating acceptance, payment occurrence, acquisition, and retirement eligibility
also makes delayed callbacks and retirement races deterministic.
**Alternatives rejected:** Soft global capacity after pricing; creating Redemption only at payment
success; using webhook arrival time for deadline/retirement eligibility; silently granting or
discarding late/conflicting payments.
**Source:** Approved July 26 Commerce review; see
[Gradex Domain, Data, and State Design](superpowers/specs/2026-07-26-domain-data-state-design.md).

## D-029 — Catalog delisting is separate from emergency Course access suspension

**Date:** 2026-07-26
**Status:** Amended on 2026-07-29 **on wording only**, with BR-090: delisting blocks "new access
grants" rather than "new checkout", because MVP has no checkout. The substance — delisting never
denies qualifying existing access, and is separate from retirement and from emergency suspension —
is unchanged and in force.
**Decision:** Ordinary catalog delisting removes a Course from public discovery and blocks new access
grants — "new checkout" in the original wording, amended 2026-07-29 with BR-090 because MVP has no
checkout — but does not deny existing entitled Students. Retirement blocks future acquisition/inclusion while
preserving qualifying existing access. Immediate denial of existing Student access requires a
separate elevated Course access-suspension command with a constrained legal, security, malware, or
severe-moderation reason, immutable Audit evidence, and notification/outbox intent. Entitlements are
not rewritten by any of these Course operations.
**Reason:** Visibility, future sellability, content retirement, and emergency access denial have
different actors, consequences, and evidence. A generic “unpublish” transition made ordinary
catalog operations capable of unexpectedly removing paid access.
**Alternatives rejected:** Treating delisting as access revocation; dynamically mutating
Entitlements; an unrestricted generic Course access toggle; automatic content hiding on report.
**Source:** Approved July 26 Commerce/Entitlement review; supersedes D-021's prior
`UNPUBLISHED` access-blocking semantics.

## D-030 — Earnings snapshot Instructor ownership and share configuration at Order completion

**Date:** 2026-07-26
**Status:** Deferred out of MVP on 2026-07-28 by
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation),
with D-018. No paid Order exists to snapshot. The rule that historical commercial responsibility must
survive Course reassignment is preserved for the deferred feature.
**Decision:** Each paid Order's earning ledger entry snapshots the owning Instructor and the
effective versioned revenue-share configuration when the Order completes. Course reassignment never
rewrites earlier entries. Orders completed after reassignment credit the new Instructor; a later
Refund/chargeback adjustment remains tied to the original earning/Instructor.
**Reason:** Historical commercial responsibility must remain deterministic across ownership changes,
Refund timing, and payout periods. Mutable Course ownership or a later share percentage cannot
rewrite an accepted calculation.
**Alternatives rejected:** Paying the current Course owner for all historical Orders; recalculating
earlier earnings when the global percentage changes; moving late Refund adjustments to a new owner.
**Source:** Approved July 26 Reporting/Payouts design decision; see
[the July 26 daily record](launch/daily/2026-07-26.md).

## D-031 — Preserve authentic legacy state through forward-only context cutovers

**Date:** 2026-07-26
**Decision:** Keep applied `0001_init` unchanged and evolve through in-place
expand–backfill–cutover–contract. Preserve safe Course/Section/Lesson UUIDs, compatible Account
identity/credentials, exact Media/object evidence, and semantically valid Learning state through
resumable typed mappings and context authority epochs. Never derive commercial/legal history from
`fake_entitlements` or fabricate approval/Audit evidence. Each context converges live deltas,
promotes constraints, fences writes, switches authority once, and thereafter repairs forward.
Legacy structures are removed only by later rehearsed forward migrations after reconciliation,
observation, queue drain, and new-authority restore evidence.
**Reason:** A development reset would hide migration defects that could later lose stable identity,
Media, Progress, or external-work evidence in staging/production. Independent dual writes or
post-cutover legacy rollback would create split-brain authority.
**Alternatives rejected:** Reset/squash `0001_init`; one big-bang shadow-schema switch; converting
fake access into Orders/Entitlements; assuming legacy `READY` proves new Media readiness; restoring
legacy authority after a context epoch changes.
**Source:** Approved July 26 domain/data/state design Sections 5–6; see
[Gradex Domain, Data, and State Design](superpowers/specs/2026-07-26-domain-data-state-design.md).

## D-032 — Claude builds, agy reviews

**Date:** 2026-07-25
**Status:** Partially superseded by D-033. The temporary builder/reviewer seat assignment is no
longer active; the `agy` fallback, disposable-worktree containment, and tainted/unavailable review
rules remain in force.
**Decision:** Reassign the delivery roles one seat: Claude becomes the builder and planner — owning
slice planning, SpecKit, implementation, checks, evidence, and correction of findings — and `agy`
(Google Antigravity CLI) becomes the independent read-only reviewer on model
`gemini-3.1-pro-high`. Reviews are dispatched by `scripts/agy-review.sh <base>..<head>` against a
fixed brief checked in at
[the review brief template](launch/review/REVIEW_BRIEF_TEMPLATE.md). Read-only is enforced
structurally, not by instruction: the reviewer receives a disposable detached worktree at the exact
reviewed commit, its workspace is asserted unmodified afterwards, and the live repository is
snapshotted before and after. A run that modifies its workspace is `TAINTED` and discarded; a run
that yields no parseable verdict is `UNAVAILABLE`. Neither is ever recorded as an approval. The
reviewer model must remain a different family from the builder; if Claude ever reviews a
Claude-authored slice, that is a self-check and cannot close it.
**Reason:** Codex exhausted its quota on 2026-07-25 with 19 days left before the readiness-gated
August 15 launch and confidence already Red, leaving the builder seat empty. Claude is the only
remaining agent able to plan and implement at the required rate. Moving Claude into that seat
vacates the reviewer seat, and the property the workflow actually depends on — that the thing
reviewing the work is not the thing that wrote it — has to be preserved by filling it with a
different model family rather than by dropping review.
**Alternatives rejected:** Claude reviewing its own diffs (nominally independent, shares the
builder's blind spots, and removes the only external check on a Red-confidence launch);
`claude-opus-4-6-thinking` as the agy reviewer (strong, but same family as the builder); pausing
delivery until Codex quota returns (no credible date, and the schedule has no slack); trusting the
reviewer's prompt-level promise not to edit instead of containing it in a disposable worktree.
**Operational note:** agy's headless mode cannot prompt for tool permissions and auto-denies them,
producing an empty report. The developer authorised `--dangerously-skip-permissions` for review runs
on 2026-07-25 on the basis that the grant applies to a throwaway checkout rather than the working
repository. The containment and the post-run assertions, not the flag, are what keep this safe.
**Source:** Developer decision on 2026-07-25 after Codex quota exhaustion; supersedes the
Codex-builder/Claude-reviewer model recorded in the approved
[platform architecture](superpowers/specs/2026-07-25-platform-architecture-design.md), which is left
unedited as approved-baseline evidence.

## D-033 — Codex resumes building and Claude resumes review

**Date:** 2026-07-25
**Status:** Temporarily superseded by [D-035](#d-035--claude-builds-s1b2-and-agy-reviews) for the
S1B2 handoff. The seat assignment below is paused, not retired; its frozen-range, disposable-worktree,
and never-self-approve rules stay in force. The developer may explicitly restore this assignment when
Codex quota returns.
**Decision:** Restore the original launch seats when Codex quota becomes available: Codex owns
planning, implementation, checks, evidence, and finding correction; Claude performs the independent
read-only review of one frozen exact commit range. Claude reviews from a disposable detached
worktree with read-only tools and may not modify the review tree or the live repository. `agy`
remains the approved fallback reviewer under D-032 when Claude is unavailable. The reviewer never
reviews work it authored.
**Reason:** D-032 was an availability response to an exhausted Codex quota, not a product or
architecture preference. Restoring the original model split preserves delivery capacity while
retaining the independent-review boundary and the reviewed evidence Claude and `agy` produced during
the temporary reassignment.
**Alternatives rejected:** Keeping Claude in the builder seat after Codex returns, which leaves the
original builder capacity unused; letting Codex approve its own work; discarding the D-032/agy
fallback and its proven containment harness.
**Source:** Developer instruction during S1A on 2026-07-25: Codex quota returned and the launch
workflow should return to its pre-exhaustion role assignment.

## D-034 — Browser authentication uses one opaque server-managed session cookie

**Date:** 2026-07-26
**Decision:** Gradex uses one opaque, server-managed session credential stored in a `Secure`,
`HttpOnly`, host-only cookie with explicit `SameSite=Strict` policy. References to separate access
and refresh tokens in older requirements are superseded. Session renewal uses controlled
session-credential and CSRF rotation; PostgreSQL stores only credential/CSRF digests and remains
authoritative for role-specific idle/absolute expiry, generation, epoch, revocation, and confirmed
reuse. Authentication credentials never enter browser storage or JavaScript-readable cookies.
Confirmed reuse of a rotated credential revokes its entire family and requires reauthentication.
Logout revokes server state before clearing the cookie.
**Reason:** This matches the later approved same-origin security design and the existing immutable
session-generation schema while keeping authentication bearers inaccessible to JavaScript. A second
browser bearer or a Next.js token vault adds state, expiry, race, and operational boundaries without
enough additional protection for the current modular-monolith architecture and launch timeline.
**Alternatives rejected:** Separate access/refresh cookies; a Next.js token-vault/BFF; client-visible
refresh tokens; local/session-storage credentials.
**Source:** Developer approval during the schedule-advanced S1B2 start on 2026-07-26; see the
[S1B2 authenticated-session design](superpowers/specs/2026-07-31-s1b2-authenticated-session-design.md).

## D-035 — Claude builds S1B2 and agy reviews

**Date:** 2026-07-31
**Status:** Active. Temporarily supersedes [D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s
seat assignment for the duration of the S1B2 handoff; D-033's frozen-range, disposable-worktree, and
never-self-approve rules remain in force unchanged.
**Decision:** Move the delivery seats one place for the remainder of the active S1B2 slice. Claude
becomes the builder and planner — owning the remaining implementation, checks, evidence, and
correction of review findings — and `agy` (Google Antigravity CLI, `gemini-3.1-pro-high`) becomes the
independent read-only reviewer under D-032's existing containment harness, dispatched through
`scripts/agy-review.sh <base>..<head>` against
[the review brief template](launch/review/REVIEW_BRIEF_TEMPLATE.md). Codex's completed S1B2 work and
its exact commit history stay unchanged and are inherited, not rewritten: the handoff point is
implementation head `24b0d21` plus the T013/T019–T029 backend work Codex left uncommitted in the
working tree. Claude must not review the S1B2 range it now authors; that would be a self-check and
cannot close the slice. The handoff is temporary. When Codex quota returns, the developer may
explicitly restore D-033's assignment, and Claude returns to the reviewer seat.
**Reason:** Codex exhausted its quota mid-S1B2, on 2026-07-31, with the backend complete but the
frontend, verification, and review tasks outstanding, and 15 calendar days left before the
readiness-gated August 15 launch at Red confidence. Leaving the builder seat empty stalls the
critical path for an unknown duration. The property the workflow depends on is not which model
builds, but that the reviewing model is not the authoring model — so the reviewer seat is refilled
with a different model family rather than dropped, exactly as D-032 established when the same
failure occurred during S1A.
**Alternatives rejected:** Waiting for Codex quota with no credible reset date and no schedule slack;
Claude building and also reviewing its own S1B2 range (removes the only external check on a
Red-confidence launch); restarting S1B2 under Claude from a clean tree (discards working,
test-backed implementation and burns critical-path hours); recording D-032 as simply reactivated
(its `Status` field is already historical, and the launch record needs a dated entry naming the
exact S1B2 handoff point).
**Source:** Developer instruction on 2026-07-31 after Codex quota exhaustion during S1B2.

## D-036 — Claude builds S1B3 and agy reviews

**Date:** 2026-08-01
**Status:** Active. Extends [D-035](#d-035--claude-builds-s1b2-and-agy-reviews)'s seat assignment to
the S1B3 slice and continues to pause
[D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s. D-033's frozen-range,
disposable-worktree, and never-self-approve rules remain in force unchanged.
**Decision:** Claude holds the builder and planner seat for S1B3, and `agy`
(Google Antigravity CLI, `gemini-3.1-pro-high`) holds the independent read-only reviewer seat under
D-032's containment harness, dispatched through `scripts/agy-review.sh <base>..<head>`. Claude must
not review any S1B3 range it authors. This decision is scoped to S1B3 and expires when that slice
closes; the seats for S1C require their own explicit assignment and do not renew implicitly.
**Reason:** D-035 was scoped to "the remainder of the active S1B2 slice", and S1B2 closed at reviewed
head `7d8710e`. Continuing to build under an expired decision would leave the seats unrecorded during
a Red-confidence slice, which is exactly the ambiguity the never-self-approve rule exists to prevent.
The arrangement is also evidenced rather than assumed: across S1B2 it produced two frozen ranges,
both independently reviewed to `APPROVE` with zero findings, with `touched files: 0` and clean
disposable worktrees on both runs. Codex quota has not been reported as returned, so D-033's
restoration condition is still unmet.
**Alternatives rejected:** Treating D-035 as implicitly covering S1B3 (its own text scopes it to one
slice, and silently widening a seat decision is the failure mode the launch protocol forbids);
restoring D-033 now (its stated precondition, returned Codex quota, has not been met, and guessing
would stall the critical path); Claude building and reviewing its own S1B3 range (removes the only
external check); dropping the reviewer seat for speed (a slice cannot close on its builder's own
assessment).
**Source:** Developer instruction on 2026-08-01 at S1B3 start of day, in response to the recorded
open-seat blocker.

## D-037 — Claude builds S1C and agy reviews

**Date:** 2026-08-02
**Status:** Active. Scoped to the S1C slice only. Continues to pause
[D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s seat assignment; D-033's
frozen-range, disposable-worktree, and never-self-approve rules remain in force unchanged.
**Decision:** Claude holds the builder and planner seat for S1C — staff lifecycle, suspension
enforcement, and the full authorization matrix — and `agy` (Google Antigravity CLI,
`gemini-3.1-pro-high`) holds the independent read-only reviewer seat under
[D-032](#d-032--claude-builds-agy-reviews)'s containment harness, dispatched through
`scripts/agy-review.sh <base>..<head>` against
[the review brief template](launch/review/REVIEW_BRIEF_TEMPLATE.md). Claude must not review any S1C
range it authors, including the S1 integration review that spans S1A, S1B, and S1C together: an
integration review whose scope contains Claude-authored commits is dispatched to `agy`, not
self-checked.

This decision **expires at S1C's frozen reviewed head** — the exact commit that carries S1C's
recorded reviewer verdict. It does not survive to S2, and seats never renew implicitly. S2 requires
its own dated assignment.

**D-033 stays paused.** Its stated precondition is returned Codex quota, and that has not been
reverified. Codex availability must be explicitly reverified before work begins under D-033 again;
absence of a report is not a return of quota.

**Reason:** [D-036](#d-036--claude-builds-s1b3-and-agy-reviews) was scoped to S1B3 and expired at
reviewed head `9d3db91`, leaving the S1C seats unassigned and recorded as a blocker in
[STATUS.md](launch/STATUS.md). S1C carries S1's complete close conditions — the full role and
ownership authorization matrix and immediate suspension enforcement — so it is the least acceptable
slice on which to leave the reviewing seat ambiguous. The arrangement is evidenced rather than
assumed: across S1B2 and S1B3 it produced three frozen ranges, all independently reviewed to
`APPROVE`, each reporting `touched files: 0` with a clean disposable worktree on exit.
**Alternatives rejected:** Treating D-036 as implicitly covering S1C (its own text scopes it to one
slice, and silently widening a seat decision is the failure mode the launch protocol forbids);
restoring D-033 without reverifying Codex availability (guessing at a precondition is how an
unstaffed builder seat becomes invisible); Claude building and also reviewing the S1 integration
range (a slice never closes on its builder's own assessment, and this is the range where that matters
most); dropping the reviewer seat to buy back critical-path hours at Red confidence.
**Source:** Developer instruction on 2026-08-02 at S1C start of day.

## D-038 — August 8 is no longer a credible runway start; S3–S8 remain undated pending a developer remedy

**Date:** 2026-08-02
**Status:** Active. Records a forecast, not a scope or date change. The three remedies in
[the downstream reconciliation](launch/DOWNSTREAM_RECONCILIATION.md#5-remedies-requiring-developer-approval)
require explicit developer approval and are not adopted by this decision.
**Decision:** Record explicitly, on repository evidence, that **the fixed August 8 runway start is no
longer credible** and that a full-PRD public launch on **August 15 is not forecastable** from the
current calendar. S3–S8 therefore stay `TBD` in [SLICES.md §2](launch/SLICES.md#2-slice-order) rather
than receiving dates the evidence cannot support. Their dependency *order* is unchanged and remains
correct; only the calendar is unresolved.

The arithmetic is not close. Nine undelivered feature slices (S2–S10) compete for six remaining
feature dates (August 3, 4, 5, 6, 8, 9), a deficit of at least three days *before* any velocity
correction. The only completed product slice, S1, was scoped as one day and took five —
S1A, S1B1, S1B2, S1B3, and S1C. At that observed expansion the remaining feature work does not fit
the runway by a margin measured in weeks, not hours. The final week also already assigns work to all
seven days of August 9–15, which violates the plan's own six-workdays-per-week rule.

What this decision does **not** do: compress a slice below the
[PLAN.md §2](launch/PLAN.md#daily-capacity) envelope, spend the protected August 7 recovery day,
remove a PRD capability, or move the public target. Each of those is a developer decision, and three
of them are offered as dated remedies in the reconciliation.

**Reason:** The conflict has carried the label "unresolved" since 2026-07-30 with a July 31 deadline
that has now passed, and carrying it a third time would let the launch forecast rest on dates nobody
has checked. [PLAN.md §9](launch/PLAN.md#9-workflow-validation) requires that a missed calendar day
be reconciled from Git evidence before replanning, and §5 makes an unresolvable required forecast a
Red condition. Recording the forecast honestly costs nothing and is available without developer
authority; assigning invented dates would manufacture the appearance of a plan while removing the
signal that a remedy is needed.
**Alternatives rejected:** Assigning one date per slice to August 4–6 (three dates for six slices —
arithmetically impossible without compressing evidence); spending August 7 (protected, and spending
it silently is explicitly forbidden); adopting a scope reduction or a new launch date on Claude's own
authority (both are canonical changes reserved to the developer under
[PLAN.md §3](launch/PLAN.md#replan)); leaving the rows `TBD` with no recorded verdict a third time
(the status quo that produced this decision).
**Source:** Downstream-calendar reconciliation performed 2026-08-02 before S1C planning, at developer
instruction, and recorded in [DOWNSTREAM_RECONCILIATION.md](launch/DOWNSTREAM_RECONCILIATION.md).

## D-039 — Remedy A adopted: scope preserved, public target moves to September

**Date:** 2026-08-02
**Status:** Active. Supersedes the August 15 public go-live target recorded in
[PLAN.md](launch/PLAN.md) and resolves the remedy decision left open by
[D-038](#d-038--august-8-is-no-longer-a-credible-runway-start-s3s8-remain-undated-pending-a-developer-remedy).
**Decision:** **Retire the August 8 runway start and the August 15 full-PRD public launch target as
non-credible.** Gradex preserves full PRD scope and moves the public target into September.

**No exact September date is set.** "Early-to-mid September" was a derived forecast in the
reconciliation, resting on estimates with a single calibration point, 21 open launch gates, and
external dependencies nobody has contacted yet. It is a forecast hypothesis, not an evidence-backed
date, and it must not be committed to publicly or recorded as a target. The exact public date is
selected only after two inputs exist:

1. the August 6 outreach results — acknowledged requests with delivery dates from counsel, accounting,
   Tap, email, hosting, and malware scanning; and
2. a critical-path rebaseline of S2–S16 performed against those results.

**Remedy B (reduce launch scope) is not adopted and is not rejected.** It remains available after the
August 6 outreach, as an optimization of the new plan rather than an attempt to rescue August 15. A
scope cut decided now would buy nothing if Tap activation or counsel lead times independently push past
any August date, and would permanently lose capability for no schedule gain.

**Remedy C (change the operating envelope) is rejected.** Spending the protected August 7 recovery day
or compressing slices below the [PLAN.md §2](launch/PLAN.md#daily-capacity) envelope attacks the one
part of this delivery that is demonstrably working: slice quality and evidence integrity.

**Reason:** D-038 established on date arithmetic alone — six slices, three dates — that August 8 could
not hold, and that a full-PRD August 15 launch was not forecastable. Leaving a retired target in the
plan while knowing it is dead is the failure mode the launch protocol exists to prevent: every
downstream confidence, gate deadline, and go/no-go criterion would continue to be measured against a
date already known to be false. The asymmetry decides which remedy: missing a self-imposed date on a
pre-launch product with no announced launch and no promised counterparty costs credibility with nobody,
while launching a payment and media platform on a compressed security gate costs money, student trust,
and possibly Kuwait Digital Commerce Law exposure that cannot be rolled back.
**Alternatives rejected:** Keeping August 15 and cutting scope to fit (Remedy B alone recovers roughly
half the deficit and reaches office hours — the follow-up loop the product exists to deliver — and the
cut would be decided blind to the outreach results); spending August 7 or compressing the envelope
(Remedy C, one day against a twenty-day gap, paid for by removing the evidence that catches the exact
defect class the last two carryovers were); naming an exact September date now (would replace one
uncredible target with another and repeat the error being corrected); leaving the remedy undecided past
August 4 (the first unassigned date would then be spent against a conflict rather than a plan).
**Source:** Developer decision on 2026-08-02, on the analysis in
[DOWNSTREAM_RECONCILIATION.md](launch/DOWNSTREAM_RECONCILIATION.md).

## D-040 — August 15 restored as the hard MVP launch date; Claude plans, Antigravity implements, Claude reviews

**Date:** 2026-07-27
**Status:** Active. Supersedes [D-039](#d-039--remedy-a-adopted-scope-preserved-public-target-moves-to-september)
**on the launch date only** — D-039's evidence, risk analysis, and rejection of envelope compression
survive intact and are carried forward. Replaces the S1C-scoped seat assignment in
[D-037](#d-037--claude-builds-s1c-and-agy-reviews) with a standing workflow that does not expire per
slice. Continues to pause [D-033](#d-033--codex-resumes-building-and-claude-resumes-review).

**Decision:** Two things are decided together, because neither holds without the other.

### 1. August 15, 2026 is the hard MVP public launch date

Restored by product-owner decision. It is a commitment, not a forecast, and it takes priority over
non-critical scope and visual polish. What it does **not** take priority over is fixed below.

**Every requirement is classified into exactly one of three categories**, recorded in
[the August 15 scope matrix](launch/AUGUST_15_EXECUTION_PLAN.md#2-scope-matrix):

- **A — Launch Critical.** Required for the critical student journey, security, authorization,
  payment correctness, access control, data integrity, a legal launch blocker, or basic production
  operation. Complete by August 15.
- **B — Manual but Supported.** The behaviour remains available, but an authorised Admin performs
  part of it by hand. Manual operation must still be authorised, auditable, documented, and safe.
- **C — Post-Launch.** Explicitly deferred, with a recorded reason and a post-launch destination.

**No feature disappears silently.** Every deferral is recorded in
[the execution plan](launch/AUGUST_15_EXECUTION_PLAN.md) and in
[STATUS.md](launch/STATUS.md). A requirement that is not in one of the three categories is not
deferred — it is unclassified, and that is a planning defect to fix, not a scope reduction.

**These may not be sacrificed for the date, under any pressure:** backend-enforced authorization,
ownership checks, account-suspension enforcement, payment-callback verification, callback idempotency
and replay handling, order/payment state integrity, entitlement provenance, short-lived signed media
access, private-draft and protected-download controls, database constraints, negative tests on
critical boundaries, audit records for sensitive actions, backups with a tested restore, and minimum
monitoring. Speed is bought from scope, polish, and automation — never from these.

**The schedule arithmetic that retired August 15 was measured on the wrong calendar.** D-038 counted
remaining *schedule* days (August 3–9) as if they were real days. They are not: the repository's
schedule calendar runs ahead of the real calendar, and every record from Day 6 through the Day 11
S1C plan — eleven schedule days, S0 through S1C — was produced across the five real days
2026-07-23 to 2026-07-27, at roughly 2.2 schedule-days per real day. On the real calendar, today is
July 27 and 19 calendar days remain. D-038's deficit was an artifact of double-counting elapsed time.
Its *qualitative* warnings — 21 open gates, four uncontacted external dependencies, S1 expanding
fivefold against its estimate — are unaffected and remain live risks.

### 2. The feature-development workflow is fixed and standing

```text
Claude plans the feature using SpecKit
→ Antigravity implements the frozen plan
→ Claude performs the final review
→ Antigravity fixes Claude's findings
→ Claude re-reviews and either accepts or rejects the slice
```

- **Claude** owns repository analysis, feature planning, SpecKit artifacts, architectural decisions,
  implementation instructions, review, acceptance, and documentation consistency. Claude does **not**
  perform the primary implementation.
- **Antigravity** owns implementation, tests, migrations, frontend, backend, and fixing findings.
  Antigravity does **not** redesign the feature or change frozen decisions.
- No slice is complete until Claude explicitly marks it accepted. Review returns exactly one of
  `ACCEPT`, `ACCEPT WITH LOW-RISK FOLLOW-UP`, `REJECT WITH FINDINGS`, or
  `BLOCKED BY PRODUCT DECISION`.
- Review depth is risk-based: Tier 1 standard (CRUD, display, admin tables), Tier 2 sensitive
  (ownership, publication transitions, resource access, refund administration, earnings, account
  state), Tier 3 critical (authentication, sessions, payment callbacks, idempotency, entitlement
  creation and evaluation, suspension, signed playback, protected downloads, refund effects on
  access). Tier 3 depth is not applied to ordinary UI work.
- **This assignment is standing and does not expire per slice.** It replaces the per-slice seat
  decisions D-035 through D-037, which are historical. `agy` and Codex hold no seat under it.

**No further planning agent, architecture reviewer, implementation workflow, or approval authority is
introduced.**

**The never-self-approve rule survives unchanged.** Claude reviews what Antigravity implements, so a
slice still never closes on its builder's own assessment. Reviews run against one frozen exact commit
range with read-only tools in a disposable detached worktree. A review with no retrievable verdict is
`UNAVAILABLE`, not approval.

**Reason:** The product owner accepted the schedule risk and requires execution rather than further
forecasting. Restoring the date without also fixing the workflow would restore the condition that
produced the deficit — one agent planning, building, and negotiating its own evidence, at roughly one
slice per day. The workflow change is what makes the date arithmetically reachable: planning and
implementation run concurrently on non-overlapping contracts, and Claude's capacity moves from
building to specifying and reviewing. The three-category scope policy is what makes it *safely*
reachable — the date is paid for out of category B and C, never out of the quality boundaries listed
above.

**Alternatives rejected:** Keeping the September target (the product owner has decided; and the date
arithmetic behind September was computed on the schedule calendar, not the real one); restoring
August 15 without a scope policy (a date with no recorded classification produces silent deferral,
which is the failure mode the launch protocol exists to prevent); restoring August 15 while keeping
one agent as builder-planner (the observed constraint is the single agent's serialized capacity, not
the calendar); compressing the daily envelope or spending the protected recovery day (D-039's
Remedy C rejection stands — the correction comes from parallelism and scope, not from removing
evidence); deleting D-038/D-039 rather than superseding them (their evidence about open gates and
external lead times is still the live risk register).

**Source:** Product-owner instruction on 2026-07-27, on repository evidence reconciled the same day.

## D-041 — Legal and accounting outreach deferred to the final days; the resulting exposure is accepted rather than resolved

**Date:** 2026-07-28
**Status:** Active. Does **not** supersede
[D-040](#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews);
it records a risk acceptance beneath it.

**Decision:** By product-owner instruction, sourcing Kuwaiti counsel and an accountant is deferred to
the final days of the runway, and the August 15 public commercial launch proceeds without waiting for
either. The developer accepts the resulting exposure explicitly.

### What is being accepted

Stated concretely, because a risk acceptance that does not name the risk accepts nothing:

1. **Launching without a confirmed answer on Digital Commerce Law registration.** Whether a
   Kuwait-based online course platform must register, and what lead time that registration carries,
   is **unknown**. If it is required, it will not have been obtained. `LG-005`, `LG-006`.
2. **Launching without counsel-reviewed bilingual Privacy, Terms, Refund, and checkout disclosures**,
   and without a counsel-defined evidencing method for accepted policy versions. `LG-011`.
3. **Launching without a counsel-reviewed Instructor agreement** covering content rights, revenue
   share, payout and tax responsibility, warranties, takedown, and termination. `LG-020`.
4. **Launching without accountant-approved tax, invoice, KWD rounding, and financial-record retention
   treatment**, and without an approved platform-wide revenue-share percentage. `LG-001`, `LG-007`,
   `LG-016`, `LG-017`.
5. **Launch prices (`LG-012`, due August 11) will be set without the approved revenue share** they are
   meant to be computed against.

### What this decision does not do

- **It does not resolve any launch gate.** [LAUNCH_GATES.md](LAUNCH_GATES.md) is unchanged: 21
  entries, same owners, same evidence requirements, same deadlines, all `OPEN`. An accepted risk and a
  satisfied requirement are different states, and collapsing them would destroy the only register the
  go/no-go decision reads.
- **It does not move launch confidence off Red.** Red is recorded against
  [PLAN.md §5](launch/PLAN.md#5-launch-confidence) — a required gate lacking a credible resolution
  path — and accepting a risk does not create a path. Confidence returns to Amber when counsel and an
  accountant are engaged with dated actions, not when the gap is acknowledged.
- **It does not amend [PLAN.md §8](launch/PLAN.md#8-public-launch-criteria).** Criterion 1 (all
  required gates `RESOLVED` with evidence) and criterion 6 (policies and consent versions
  production-approved) will **fail** on August 15 under this decision. §8 states that failure of any
  criterion is a no-go and "does not authorize a reduced public launch unless the canonical MVP and
  gate register are explicitly revised and reapproved." **That revision has not been made.** This
  decision therefore records a launch proceeding against its own stated criteria, knowingly.
- **It does not change the technical gates.** Security, authorization, payment correctness, privacy
  enforcement, data integrity, and protected-media controls are unaffected and remain non-negotiable.
  Nothing here weakens a control in the product.

### What remains available and cheap

The **Tap** outreach requires no named contact — Tap publishes a merchant-onboarding intake — and it
carries `LG-007`, `LG-008`, and `LG-010`. It is roughly fifteen minutes of work and is **not** blocked
by this decision. Deferring it specifically buys nothing, and item 4 of that message (webhook
signature procedure and test vectors) is a direct input to S7 implementation on August 10 rather than
a compliance artifact.

### Alternatives that were on the table

Presented to the developer on 2026-07-28 with their consequences, and not chosen:

- **August 15 as a soft/internal launch** with public commerce disabled until legal clearance. The
  code ships to the same date and the public go-live slips honestly.
- **Deprioritise rather than abandon**, keeping counsel and accounting dated before the August 12 gate
  deadline.
- **Send Tap now** while deferring the other two.

### Why this is recorded at this length

The developer chose this with the consequences stated. That is their authority under
[PLAN.md §2](launch/PLAN.md#responsibilities), which assigns accepted risks to the product owner. What
the protocol does not permit is the exposure becoming invisible — a deferral absorbed into a schedule
reads later as an oversight, and this was not an oversight. It is a decision, made on 2026-07-28, with
the alternatives on the table.

**Source:** Product-owner instruction on 2026-07-28, after the consequence for PLAN.md §8 was stated
and the alternatives were offered.

## D-042 — Codex plans, Antigravity implements, and Claude independently reviews

**Date:** 2026-07-28
**Status:** Active from S2 Phase 5 onward. Supersedes the **workflow and seat assignment only** in
[D-040](#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews).
D-040's August 15 target, scope classification, quality boundaries, and all other decisions remain
in force.

**Decision:** Codex owns feature planning and specification through SpecKit, beginning with
`speckit.specify`. Antigravity (`agy`) owns implementation and correction of implementation findings
through `speckit.implement`. Claude remains the independent read-only reviewer and acceptance
authority for each frozen range.

The active D4 record and its closed S2 Phase 1–4 evidence remain historical and are not reassigned.
The first work under this decision is S2 Phase 5, revision integrity.

**Review boundary:** Claude must review one exact frozen commit range from a disposable detached
worktree, without modifying either that worktree or the live repository. Antigravity may not approve
its own work, and Codex's planning check is not acceptance. An unavailable or tainted review is not
an approval; critical and high findings return to Antigravity for correction and re-review.

**Reason:** The product owner explicitly assigned the three seats and their Speckit stages, retaining
Claude's independent review boundary.

**Source:** Product-owner instructions on 2026-07-28: Codex is the planner using `speckit.specify`,
Antigravity uses `speckit.implement`, and Claude reviews.

## D-043 — Codex implements S2 D5 and Claude independently reviews

**Date:** 2026-07-28
**Status:** Active for S2 D5. Supersedes only D-042's D5 builder seat; D-042's frozen specification,
scope boundary, and Claude review protocol remain in force.

**Decision:** Codex takes the S2 D5 implementation and correction seat through
`speckit.implement`, beginning from the existing partial uncommitted Antigravity worktree.
Antigravity is removed from the active D5 seat. Claude remains the independent read-only reviewer
of one frozen exact implementation range and does not implement or repair it.

Codex may plan and build, but may not accept its own range. Claude reviews from a disposable
detached worktree. Any critical or high finding returns to Codex for correction and another
independent Claude review. This staffing change does not reopen T001–T031, change the frozen
T032–T038 requirements, or authorize T039+.

**Reason:** Antigravity quota interruptions produced incomplete implementation passes. The product
owner explicitly reassigned implementation to Codex while retaining Claude as reviewer.

**Source:** Product-owner instruction on 2026-07-28: "implement yourself instead of antigravity and
claude will review."

## D-044 — Antigravity completes S2 and Claude reviews the whole feature once

**Date:** 2026-07-28
**Status:** Active for remaining S2 T039–T064. D-043 expired when D5 closed at reviewed head
`3b6d752`. This decision restores D-042's Antigravity builder seat for the rest of S2 and replaces
its per-range Claude review cadence for this feature only.

**Decision:** Codex owns the existing S2 SpecKit specification, plan, task reconciliation,
orchestration, and implementation verification. Antigravity on exact model
`gemini-3.6-flash-high` implements T039–T064 through the repository `speckit.implement` workflow in
five sequential, bounded queues. Codex reviews and commits each queue. Claude participates only
after the complete S2 feature converges and hosted CI is green, then independently reviews the one
exact cumulative range `3d9604e..<final-head>` from a disposable detached worktree.

There is no Claude review of an individual queue, phase, or correction slice. A critical or high
whole-feature finding returns to Antigravity for correction, after which Claude re-reviews the whole
cumulative S2 range. Antigravity never approves or commits its own output, and Codex verification is
not independent acceptance.

**Reason:** The Antigravity quota returned, and the product owner explicitly reassigned
implementation to Antigravity while requiring Claude to wait until the entire feature—not a phase
or slice—is finished.

**Source:** Product-owner instructions on 2026-07-28: use Antigravity with Gemini 3.6 Flash High
after Codex plans through SpecKit; Antigravity follows `speckit.implement`; Claude reviews only after
the whole feature is complete.

## D-045 — MVP launches without in-platform payments; course access is granted by admin-approved Course Access Invitation

**Date:** 2026-07-28
**Status:** Active. Supersedes [D-027](#d-027--every-mvp-entitlement-originates-from-an-order)
entirely. Amends [D-015](#d-015--section-is-canonical-admin-owns-all-catalog-pricing) and
[D-026](#d-026--course-configured-semester-expiry-with-audited-entitlement-adjustments) on scope and
grant trigger only. Defers [D-002](#d-002--tap-payments-for-mvp-checkout-deema-bnpl-is-fast-follow),
[D-012](#d-012--coupons-in-mvp-admin-only-discount-codes-applied-pre-gateway),
[D-017](#d-017--full-and-partial-refunds-with-counsel-approved-eligibility),
[D-018](#d-018--manual-monthly-payouts-with-system-recorded-accounting),
[D-028](#d-028--reserve-coupon-capacity-when-gradex-accepts-an-order), and
[D-030](#d-030--earnings-snapshot-instructor-ownership-and-share-configuration-at-order-completion)
out of MVP. Does not change
[D-040](#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews)'s
date or quality boundaries, or the workflow seats in
[D-042](#d-042--codex-plans-antigravity-implements-and-claude-independently-reviews)/[D-044](#d-044--antigravity-completes-s2-and-claude-reviews-the-whole-feature-once).

**Decision:** Gradex launches as a fully functional educational video platform with **no payment
processing inside the platform**. All payment activity is External Payment — performed and verified
outside Gradex by the admin team through its own operational process. Gradex does not collect
payments, verify them automatically, store checkout sessions, process refunds, receive payment
webhooks, generate invoices, calculate payouts, handle BNPL, provide a cart, or apply coupons.

Course access is granted through this workflow and no other:

```text
External Payment confirmed off-platform
  → Admin creates a Course Access Invitation for one student email and one Course
  → Student signs in or registers with the invited email
  → Student accepts                       (state: PENDING_ADMIN_APPROVAL — grants nothing)
  → Admin Approval                        (the authoritative grant trigger)
  → idempotent transaction creates or reuses the Enrollment and creates one ACTIVE Entitlement
  → Student is notified that access is active
```

**Registration grants no course access. Acceptance grants no course access.** Only Admin Approval
does. The Course Access Invitation is a workflow record and is never the authoritative access record;
protected reads, playback authorization, progress writes, and Instructor rosters all authorise
against the Entitlement.

### Resolved questions

Twelve questions were raised as unresolved in the reconciliation and resolved by product-owner
approval on 2026-07-28. Recorded individually because each one changes an artefact:

1. **Catalog prices remain displayed.** The price tells the Student what to pay externally. Admin
   pricing (`course_price_changes`, S2 T039–T042) is retained as shipped, and `LG-012` stays required.
2. **Section prices are retained in schema and the Admin surface but are not displayed in the
   student-facing catalogue for MVP.** Displaying a price for a scope that cannot be acquired is
   misleading. This item carried no recommendation in the reconciliation and was derived from the
   locked "one complete course only" rule plus item 1; it is the one call recorded here as derived
   rather than recommended.
3. **Entitlement is the authoritative access record; Enrollment remains the durable Student-to-Course
   learning relationship** for roster and progress. The two are not merged.
4. **A granted Entitlement still carries an expiry.** A Course MUST have a future
   `default_access_ends_at` before an Invitation for it can be **approved** — the direct replacement
   for D-026's pre-checkout precondition. The approval snapshots it as `original_access_ends_at`.
5. **`retirement_eligibility_at` is set from the Admin Approval instant**, the moment access begins.
6. **A Course Access Invitation does not expire.** No approved business rule required it and no
   duration is invented. The acceptance *link* is an expiring `identity_action_secrets` row with a
   resend path; link expiry and invitation expiry are different things.
7. **An Admin may reject an already-accepted Invitation, and a new Invitation may be created for a
   previously rejected or cancelled `(email, Course)` pair.** Both are audited.
8. **External Payment evidence is an optional free-text admin note plus an opaque external
   reference, recorded on the audit record only.** No amount, currency, or payment-status field
   exists anywhere in Gradex. This is deliberately not an accounting system.
9. **The Course-scoped Instructor roster returns to MVP**, overturning its post-launch deferral in
   [the execution plan §2.3](launch/AUGUST_15_EXECUTION_PLAN.md#23-deferred-to-post-launch--recorded-not-removed).
   Instructor visibility into enrolled Students is part of the locked MVP scope.
10. **`LG-005`, `LG-006`, `LG-011`, and `LG-016` stay `OPEN` and unchanged.** Where payment is
    captured is not an engineering answer to a counsel question, and off-platform collection may
    increase rather than remove the `LG-016` record-keeping burden.
11. **Instructor payout processing is deferred**, so no earnings are calculated in MVP. The
    contractual obligation is not deferred: `LG-020`'s Instructor agreement still requires
    revenue-share terms, and `LG-001` moves with the deferred payout feature.
12. **Account disabling reuses the shipped `ACTIVE ↔ SUSPENDED` enforcement** rather than adding a
    state. S1C already satisfies the requirement.

### Access-model boundaries preserved for future payment

The Entitlement carries a typed `grant_source` discriminator. MVP implements `MANUAL_INVITATION`
only. `PAID_ORDER`, `PROMOTIONAL`, and `DIRECT_ADMIN_GRANT` are reserved names, **not implemented**,
and no speculative payment-provider table, checkout-session table, or webhook-event table is added.
The Entitlement scope column remains expressive enough for Section scope even though MVP issues
`COURSE` scope only.

[SLICES.md §3.1](launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation)'s
separation of Entitlement **evaluation** from Entitlement **creation** is unchanged and is what makes
this change bounded: only the producer changes, from verified payment to Admin Approval. A future
online payment flow converges on the same authoritative result without redesigning access.

**Reason:** Payment-gateway activation is gated on `LG-007`/`LG-008`/`LG-010`, none of which has a
resolution path under [D-041](#d-041--legal-and-accounting-outreach-deferred-to-the-final-days-the-resulting-exposure-is-accepted-rather-than-resolved),
while the product's actual value — structured video learning with follow-up — needs none of it. The
change is also unusually cheap: repository evidence at migration `0010` shows no `orders`,
`payment_attempts`, `entitlements`, `enrollments`, `coupons`, `refunds`, ledger, or statement table
exists, so no shipped code is discarded. It removes roughly 26 hours of Tier-3 work (S6 and S7) and
replaces it with roughly 8–10 hours.

**What this decision does not do:** it does not reduce review depth. Admin Approval replaces a
cryptographically verified gateway callback as the sole control between a registered account and paid
content, so the grant path is Tier 3, capability-gated, recent-authentication-bound, idempotent, and
audited. It also does not reduce legal exposure proportionally — see resolved question 10.

**Alternatives rejected:** Delaying launch until Tap activation completes (the gate has no owner and
no date under D-041); treating the Admin invitation as evidence that payment occurred inside Gradex
(it is not, and modelling it as a payment transaction would rebuild the accounting system this
decision removes); granting access on student acceptance without Admin Approval (removes the only
control point and makes an emailed link sufficient for paid content); building a thin
payment-provider abstraction now to "keep the seam warm" (speculative complexity the reconciliation
explicitly forbids — the `grant_source` discriminator is the seam); merging Course Access Invitations
into the existing `staff_invitations` table (different lifecycle, different uniqueness rule, and
account-creation semantics that must not touch course access).

**Source:** Product-owner scope decision on 2026-07-28, approved with all twelve listed questions
resolved.

---

## D-046 — The external Course community link is deferred to post-launch

**Date:** 2026-07-29
**Status:** Approved
**Decision:** The **external Discord/Telegram Course community link leaves the MVP scope** and is
deferred to **S18, post-launch**. No slice authors it, stores it, serves it, or renders it before
launch.

**Affected artefacts:** [PRD §MVP](PRD.md) drops the "External Discord/Telegram Course community
link" bullet; [SLICES.md §6](launch/SLICES.md) drops the row assigning display to S5 and authoring to
S2; [AUGUST_15_EXECUTION_PLAN.md §2.3](launch/AUGUST_15_EXECUTION_PLAN.md#23-deferred-to-post-launch--recorded-not-removed)
gains a deferral row; [DOMAIN_MODEL.md](DOMAIN_MODEL.md) retains the field on the Course revision as a
post-launch attribute. [S5's specification](../specs/007-protected-learning/spec.md#c2--the-community-link-is-not-authored-anywhere)
retains User Story 5 and FR-036 – FR-038 marked `DEFERRED — S18` rather than deleting them.

**Reason:** The gap was found while specifying S5 and was verified against the repository, not
inferred: `specs/003-course-authoring/` contains no `community`, `discord`, or `telegram` match, and
no migration through `0010` defines such a field. The PRD assigns the capability to MVP and SLICES.md
assigns *authoring* of it to S2 — but S2 never specified it, and S2 is mid-implementation with
T043–T064 frozen under [D-044](#d-044--antigravity-completes-s2-and-claude-reviews-the-whole-feature-once).

Closing the gap correctly would mean adding a field to a frozen queue inside an in-flight slice. The
link is a convenience that points at a third-party service Gradex neither hosts nor moderates; it is
not on the access-to-playback critical path, and no Student is blocked from learning without it.
Against 17 days of runway and a RED launch confidence, that is not worth reopening S2 for.

**What this decision does not do:** it does not remove the community *strategy*. The external Discord
community remains the approved answer to post-purchase follow-up ([D-005]); it is reached by a link
shared out of band until S18 puts it in the product.

**Alternatives rejected:** Adding the field to S2's frozen queue (a scope change to an in-flight
slice, for a non-critical convenience); giving S5 a Course-level community-link field (splits Course
authoring across two slices and gives the learning slice a write path into Course content, which the
S2/S5 boundary exists to prevent).

**Source:** Product-owner scope decision on 2026-07-29, raised as conflict C2 in the S5 specification.

---

## D-047 — Claude plans S5 and S6 and agy reviews the frozen planning range

**Date:** 2026-07-29
**Status:** Active. Scoped to the S5/S6 **planning** range only — commits `785d71c..` through this
decision's own commit. Continues to pause
[D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s seat assignment; D-033's
frozen-range, disposable-worktree, and never-self-approve rules remain in force unchanged.

**Decision:** Claude holds the builder and planner seat for the
[S5 Protected Learning](../specs/007-protected-learning/spec.md) and
[S6 Course Access Grant](../specs/006-course-access-grant/spec.md) planning artefacts, and `agy`
(Google Antigravity CLI, `gemini-3.1-pro-high`) holds the independent read-only reviewer seat under
[D-032](#d-032--claude-builds-agy-reviews)'s containment harness, dispatched through
`scripts/agy-review.sh <base>..<head>`. Claude must not review the planning range it authored.

**This decision covers planning only.** It confers no implementation authority for S5 or S6, and it
does not name a builder or reviewer for either slice's implementation. Both require their own dated
assignment.

This decision **expires at the frozen reviewed head of this planning range** — the exact commit that
carries the recorded reviewer verdict. Seats never renew implicitly.

**Reason:** [D-036](#d-036--claude-builds-s1b3-and-agy-reviews) was scoped to S1B3 and
[D-037](#d-037--claude-builds-s1c-and-agy-reviews) to S1C. Both are spent, and neither reaches S5 or
S6 planning. Claude authored every artefact in this range, so reviewing it would be a self-check,
which cannot close it.

**Alternatives rejected:** Treating D-036 or D-037 as implicitly covering this range (both scope
themselves to one slice, and silently widening a seat decision is the failure mode the launch
protocol forbids); Claude reviewing its own planning output; deferring the review until
implementation starts, which would put unreviewed planning defects into built code.

**Source:** Developer instruction on 2026-07-29 to freeze the S5/S6 planning work and dispatch an
independent review.

---

## D-048 — Claude plans S5 and S6 and agy re-reviews the expanded planning range

**Date:** 2026-07-29
**Status:** Active. Scoped to the expanded S5/S6 **planning** range only — base
`785d71ce0b44ba4f591f2274285a6bc2f890b6c6` through this decision's own commit. Continues to pause
[D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s seat assignment; D-033's
frozen-range, disposable-worktree, and never-self-approve rules remain in force unchanged.

**Decision:** Claude holds the builder and planner seat for the
[S5 Protected Learning](../specs/007-protected-learning/spec.md) and
[S6 Course Access Grant](../specs/006-course-access-grant/spec.md) planning artefacts together with
the canonical business-rule and launch-register corrections this range adds, and `agy` (Google
Antigravity CLI, `gemini-3.1-pro-high`) holds the independent read-only reviewer seat under
[D-032](#d-032--claude-builds-agy-reviews)'s containment harness, dispatched through
`scripts/agy-review.sh <base>..<head>`. Claude must not review the range it authored.

**[D-047](#d-047--claude-plans-s5-and-s6-and-agy-reviews-the-frozen-planning-range) is spent.** It
was scoped to head `0f0fe06`, which `agy` reviewed and returned `REJECT` on one HIGH finding: the
S5/S6 specifications cited BR-165 – BR-171 while `docs/BUSINESS_RULES.md` ended at BR-164. D-047 is
not edited to cover the corrected range, because retroactively widening a spent seat decision is the
failure mode the launch protocol forbids. This decision covers the expanded range instead.

**This decision covers planning only.** It confers no implementation authority for S5 or S6, and it
does not name a builder or reviewer for either slice's implementation. Both require their own dated
assignment.

This decision **expires at the frozen reviewed head of this planning range** — the exact commit that
carries the recorded reviewer verdict. Seats never renew implicitly.

**Reason:** A rejected range cannot close on the builder's own correction of it. The corrections that
answer the finding — the D-045 business rules and the S5/S6 launch-register boundary — are
themselves authored by Claude and are therefore inside the range needing independent review, not
outside it.

**Alternatives rejected:** Amending D-047's stated range (retroactive widening of a spent seat);
re-running the review under D-047 unchanged (the assignment names a head that is no longer the head);
treating the single HIGH finding as builder-correctable without re-review (a slice does not close on
its builder's assessment that its own fix worked).

**Source:** Developer instruction on 2026-07-29 to resolve the review findings, re-freeze, and
re-dispatch.

---

## D-049 — Claude reconciles the D-045/D-046 downstream documents and agy reviews the range

**Date:** 2026-07-29
**Status:** Active. Scoped to the downstream reconciliation range only — base
`bae064d285f82703ee7cd61696e09c20d237a349` through this decision's own commit. Continues to pause
[D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s seat assignment; D-033's
frozen-range, disposable-worktree, and never-self-approve rules remain in force unchanged.

**Decision:** Claude holds the builder seat for the remaining
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)
and [D-046](#d-046--the-external-course-community-link-is-deferred-to-post-launch) downstream
reconciliation — the constitution, PRD, domain model, glossary, vision, screens, journeys,
navigation, launch gates, the prior specifications under `specs/001`, `specs/002`, `specs/004`, and
`specs/005`, and the status file — and `agy` (Google Antigravity CLI, `gemini-3.1-pro-high`) holds
the independent read-only reviewer seat under [D-032](#d-032--claude-builds-agy-reviews)'s
containment harness, dispatched through `scripts/agy-review.sh <base>..<head>`. Claude must not
review the range it authored.

**[D-048](#d-048--claude-plans-s5-and-s6-and-agy-re-reviews-the-expanded-planning-range) is spent.**
It was scoped to the S5/S6 planning range `785d71c..bae064d`, which `agy` reviewed to `APPROVE` with
zero findings. That range is closed and is not reopened by this one. D-048 is not edited to cover a
different range, because retroactively widening a spent seat decision is the failure mode the launch
protocol forbids.

**This decision grants no S5 implementation authority.** It covers documentation reconciliation only.
S5 implementation requires its own dated builder and reviewer assignment, and no such assignment
exists at the time of writing.

This decision **expires at the frozen reviewed head of this reconciliation range** — the exact commit
that carries the recorded reviewer verdict. Seats never renew implicitly.

**Reason:** The approved S5/S6 planning range is self-contained, but S5 implementation will read the
domain model, screens, journeys, and the S4 specification it depends on. Those still described the
payment-era MVP, and `specs/005-media-and-entitlement-evaluation/data-model.md` still required
`source_order_item_id NOT NULL` on the Entitlement — a constraint that would have made S6's grant
transaction unimplementable. Reconciling that is a change to authoritative documents, so it is
reviewed rather than assumed correct.

**Alternatives rejected:** Starting S5 implementation against unreconciled downstream documents (the
S4 Order-provenance constraint would have surfaced as a schema conflict mid-slice); folding this
range into the approved planning range (it would reopen an approved head); Claude reviewing its own
reconciliation.

**Source:** Developer instruction on 2026-07-29 to complete and independently review the remaining
downstream reconciliation before S5 implementation begins.

---

## D-050 — Claude reconciles the launch plan and agy reviews the correction

**Date:** 2026-07-29
**Status:** Active. Scoped to the launch-plan reconciliation range only — base
`b32e28957efc16bb09d46765b1e949aa3587088f` through the frozen head this range is reviewed at.
Continues to pause [D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s seat
assignment; D-033's frozen-range, disposable-worktree, and never-self-approve rules remain in force
unchanged.

**Scope extended the same day, before review, to include [`docs/launch/STATUS.md`](launch/STATUS.md).**
Validating the `PLAN.md` correction in a clean worktree surfaced a direct contradiction inside
`STATUS.md`: its header records August 15 as hard under D-040 while six passages still presented
D-039's September target and D-037's S1C seats as in force. Correcting `PLAN.md` alone would have
moved that contradiction rather than resolved it. This is a forward extension of an **active,
unreviewed** decision, recorded here rather than left implicit — it is not a retroactive widening of
a spent seat, which the launch protocol forbids and which D-047, D-048, and D-049 were each refused.

**Decision:** Claude holds the builder seat for the correction of stale launch state in
[`docs/launch/PLAN.md`](launch/PLAN.md), and `agy` (Google Antigravity CLI, `gemini-3.1-pro-high`)
holds the independent read-only reviewer seat under [D-032](#d-032--claude-builds-agy-reviews)'s
containment harness, dispatched through `scripts/agy-review.sh <base>..<head>`. Claude must not
review the range it authored.

**[D-049](#d-049--claude-reconciles-the-d-045d-046-downstream-documents-and-agy-reviews-the-range) is
spent.** It was scoped to the downstream reconciliation range `bae064d..b32e289`, which `agy`
reviewed to `APPROVE` with zero findings. That range is closed and is not reopened here. D-049 is not
edited to cover a different range, because retroactively widening a spent seat decision is the
failure mode the launch protocol forbids.

**This decision grants no S5 implementation authority.** It covers one document's launch-state
correction. S5 implementation requires its own dated builder and reviewer assignment, and no such
assignment exists at the time of writing.

This decision **expires at the frozen reviewed head of this range** — the exact commit that carries
the recorded reviewer verdict. Seats never renew implicitly.

**Reason:** `PLAN.md` is named in its own §1 as an operating source of truth, and implementation
agents read it. It still presented S1C as the active slice under the spent
[D-037](#d-037--claude-builds-s1c-and-agy-reviews) and September as the public target under
[D-039](#d-039--remedy-a-adopted-scope-preserved-public-target-moves-to-september), both contradicted
by [D-040](#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews),
and its launch criteria still required a checkout step and payment gates that
[D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)
deferred. Leaving that in place while assigning implementation seats would hand a builder a
source-of-truth document that disagrees with every register around it.

**Alternatives rejected:** Rewriting §6's three-week calendar rather than banner-marking it
historical (it is accurate as the record of what was planned and what happened through S1C and S2,
and deleting history to remove a contradiction loses the evidence behind D-038 and D-039); assigning
S5 implementation seats first and correcting the plan afterwards; Claude reviewing its own
correction.

**Source:** Developer instruction on 2026-07-29 to resolve the final launch-plan contradiction before
assigning S5 implementation seats.

---

## D-051 — Claude remediates the S3 planning gaps and agy reviews the correction

**Date:** 2026-07-30
**Status:** Active. Scoped to the S3 planning-correction range only — base
`e98e0db2e858c9aaf5af150f28de4bc7c4156e52` through this decision's own commit. Continues to pause
[D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s seat assignment; D-033's
frozen-range, disposable-worktree, and never-self-approve rules remain in force unchanged.

**Decision:** Claude holds the **planning-remediation builder** seat for
[`specs/004-public-catalogue/`](../specs/004-public-catalogue/tasks.md) — S3, Public Catalogue and
Bilingual Shell — and `agy` (Google Antigravity CLI, `gemini-3.1-pro-high`) holds the independent
read-only reviewer seat under [D-032](#d-032--claude-builds-agy-reviews)'s containment harness,
dispatched through `scripts/agy-review.sh <base>..<head>`. Claude must not review the range it
authored.

**This decision grants no implementation authority of any kind.** It covers corrections to S3's task
list and nothing else. **S3 implementation seats remain unassigned**, and S3 implementation may not
begin until a separate dated decision assigns a builder and a distinct independent reviewer. This
decision also grants no authority for S4, S5, or S6.

**[D-050](#d-050--claude-reconciles-the-launch-plan-and-agy-reviews-the-correction) is spent.** It was
scoped to the launch-plan reconciliation range `b32e289..e98e0db`, which `agy` reviewed to
`APPROVE WITH FINDINGS` — zero critical, zero high, zero medium, one LOW concerning D-050's own
single-document phrasing after its same-day scope extension. That LOW is **accepted as non-blocking
review evidence** and no correction cycle was opened for it. D-050 is not edited retroactively.

This decision **expires at the frozen reviewed head of this range** — the exact commit carrying the
recorded reviewer verdict. Seats never renew implicitly.

**Reason:** S3 is the next slice in the implementation order, and its task list carried four defects
that a builder would have implemented as written. `T011` and `T019` instructed rendering per-Section
prices, which [D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)
removed and the reconciled `FR-009` forbids — the downstream reconciliation corrected the requirement
but not the tasks implementing it. `FR-010a` had no task whatsoever, so the prohibition on checkout,
cart, coupon, and purchase controls existed only as prose. Explicit requirement traceability covered
4 of 28 requirements and 3 of 9 success criteria, against Constitution **Principle III**, which is
constitutional rather than tier-dependent and therefore binds a Tier 1 slice exactly as it binds a
Tier 3 one. And `T035` told the builder to raise `db.MaxSchemaVersion` to 10 when
`backend/internal/db/schema.go` already holds 10.

Mapping requirements to tasks surfaced two further genuine gaps that the citation audit existed to
find: `FR-014`'s retired-taxonomy-term display had no task, and `FR-025`'s prohibition on
personalization and paid placement was uncovered. Both now have coverage.

**Alternatives rejected:** Accepting S3's traceability at its prior level on the grounds that it is a
Tier 1 slice (Principle III sets no tier condition, and the audit itself found two real coverage gaps,
so the citation exercise was not ceremony); handing S3 to a builder with `T011` uncorrected;
assigning implementation seats in the same pass as the planning correction that has not yet been
reviewed; inferring Codex availability from the absence of a quota error.

**Source:** Developer instruction on 2026-07-30 to perform a bounded S3 planning remediation and
independent review.

---

## D-052 — Claude corrects the S3 schema-version statements and agy reviews the correction

**Date:** 2026-07-30
**Status:** Active. Scoped to the S3 schema-version correction range only — base
`25c37af96c4127baa927e320dd7b1dc46c2c4dad` through this decision's own commit. Continues to pause
[D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s seat assignment; D-033's
frozen-range, disposable-worktree, and never-self-approve rules remain in force unchanged.

**Decision:** Claude holds the builder seat for the schema-version correction in
[`specs/004-public-catalogue/data-model.md`](../specs/004-public-catalogue/data-model.md), and `agy`
(Google Antigravity CLI, `gemini-3.1-pro-high`) holds the independent read-only reviewer seat under
[D-032](#d-032--claude-builds-agy-reviews)'s containment harness, dispatched through
`scripts/agy-review.sh <base>..<head>`. Claude must not review the range it authored.

**This decision grants no implementation authority.** It covers three stale statements in one design
document. **S3 implementation seats remain unassigned**, and S3 implementation may not begin until a
separate dated decision assigns a builder and a distinct independent reviewer. Codex may hold that
seat only after its availability is explicitly reverified. This decision grants no authority for S4,
S5, or S6.

**[D-051](#d-051--claude-remediates-the-s3-planning-gaps-and-agy-reviews-the-correction) is spent.** It
was scoped to the S3 planning-remediation range `e98e0db..25c37af`, which `agy` reviewed to
`APPROVE WITH FINDINGS` — zero critical, zero high, zero medium, and one LOW: `data-model.md`
§Schema version still stated that `0011_catalog_search` raises `db.MaxSchemaVersion` to 10, while
`T035` had been corrected to 11. That range is closed. D-051 is not edited retroactively.

This decision **expires at the frozen reviewed head of this range** — the exact commit carrying the
recorded reviewer verdict. Seats never renew implicitly.

**Reason:** The reviewer's LOW was acted on rather than accepted, because it differs in kind from
D-050's accepted wording LOW. That one sat inside a spent governance decision with no downstream
consumer. This one was a **wrong number in a design document a builder implements from** — both `T023`
and `T035` point at `data-model.md` — and it is the same off-by-one the D-051 pass was commissioned to
fix, surviving in the source document while the task referencing it was corrected. Verifying the
finding surfaced two further errors in the same three lines that the reviewer did not name: the
up/down/up check also expected version 10 after each `up`, and the schema-version statement cited
`T030`, the write/query-symmetry red-first test, rather than `T035`.

`backend/internal/db/schema.go` already holds `MaxSchemaVersion = 10`, so a builder following the
design document would have set the constant to a value it already had and shipped an application that
refuses to serve against the migration it just applied.

**Alternatives rejected:** Recording the LOW as accepted non-blocking evidence and handing S3 to a
builder with the contradiction in place (a wrong instruction is not the same as an infelicitous
phrasing, and the design document is what a builder reads for schema shape); folding the correction
into the later implementation-seat decision (it would ship an unreviewed correction inside a seat
assignment); widening D-051 to cover a range it did not author.

**Source:** Developer instruction on 2026-07-30 to correct the remaining S3 schema-version defect and
re-review the narrow correction.

---

## D-053 — Codex availability is reverified; Codex implements S3 and agy reviews it

**Date:** 2026-07-30
**Status:** Active. Scoped to **S3 only** — Public Catalogue and Bilingual Shell,
[`specs/004-public-catalogue/`](../specs/004-public-catalogue/tasks.md). Expires when S3 is formally
closed on a recorded independent reviewer verdict.

### Codex availability is reverified — positively, on the record

**The product owner explicitly confirmed Codex availability on 2026-07-30.** This is the positive
reverification [D-033](#d-033--codex-resumes-building-and-claude-resumes-review) requires, and it is
recorded here as an affirmative statement by the developer rather than as an inference. It was **not**
derived from silence, from a previous session, or from the absence of a quota error — the standing rule
that "no report of quota is not a return of quota" is satisfied by an explicit confirmation and by
nothing weaker. Availability was last recorded as exhausted under
[D-032](#d-032--claude-builds-agy-reviews); that condition is now cleared for S3.

### Seats

**Implementation base: `343aacb15c860b5d3dae91314769de541d3be92b`** — the approved S3 planning head,
which `agy` reviewed to `APPROVE` with zero findings at every severity.

- **Codex — S3 implementation builder.** Codex **may** create and modify the production files required
  by the approved S3 tasks: backend Go packages, the `0011_catalog_search` migration, schema-version
  constants, frontend sources, and their tests. Codex works only from task IDs authorised in a bounded
  batch handoff, and **may not independently approve its own work.** A slice does not close on its
  builder's assessment.
- **`agy` — independent read-only S3 implementation reviewer.** `agy`
  (Google Antigravity CLI, `gemini-3.1-pro-high`) reviews frozen exact commit ranges through
  `scripts/agy-review.sh <base>..<head>` under [D-032](#d-032--claude-builds-agy-reviews)'s containment
  harness. `agy` **may not** edit, stage, commit, push, or implement anything. A `TAINTED` or
  `UNAVAILABLE` run is never recorded as an approval.
- **Claude — planner and implementation coordinator only.** Claude prepares bounded batch handoffs,
  inspects evidence, validates frozen ranges, and reports. Claude **may not provide the independent
  implementation verdict for S3**, because Claude authored S3's planning artefacts and a review of work
  derived from one's own plan is not independent in the sense this protocol means.

### Boundaries

This assignment grants **no S4, S5, or S6 implementation authority.** Each requires its own dated
assignment. S3 must not implement authenticated routes, write routes, content authoring, media upload
or transcoding, protected media delivery, entitlement evaluation, Enrollment creation, progress,
invitations, approval workflows, checkout, cart, coupons, payment callbacks, refunds, invoices, BNPL,
or payouts. **No Section price may be exposed in the public API or UI** — Section is not an acquirable
scope under [D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).

**Work is handed over in bounded batches, not as all 46 tasks at once.** Each batch names its exact
task IDs, its required evidence, and its prohibitions, and each freezes a range for independent review
before the next batch begins.

### Spent decisions

**[D-052](#d-052--claude-corrects-the-s3-schema-version-statements-and-agy-reviews-the-correction) is
spent at `343aacb`**, which `agy` reviewed to `APPROVE` with zero findings. The chain of spent S3
planning seats is D-051 then D-052; neither is edited retroactively.

**The accepted LOW from
[D-050](#d-050--claude-reconciles-the-launch-plan-and-agy-reviews-the-correction)'s review remains
non-blocking and is not reopened.** It concerns D-050's own single-document phrasing after a same-day
pre-review scope extension, has no downstream consumer, and was accepted as review evidence rather
than corrected.

**Reason:** S3's planning chain closed clean — 46 tasks, 28/28 active functional requirements cited,
9/9 success criteria covered, zero tasks complete, and a final independent verdict of `APPROVE` with no
findings. The one condition that still blocked implementation was an unstaffed builder seat, and the
repository standard for filling it with Codex was an explicit availability confirmation, which now
exists. Claude cannot hold both the builder and reviewer seats, and cannot review implementation of its
own plan, so the reviewer seat goes to `agy` exactly as it did for every planning range in this
sequence.

**Alternatives rejected:** Handing Codex all 46 tasks in one batch (an unbounded handoff produces an
unreviewable range and defeats Checkpoint 2, which exists to gate on evidence before presentation work
begins); Claude reviewing S3 implementation (not independent of the plan it authored); starting S3
before the schema-version correction landed; assigning S4 authority in the same decision.

**Source:** Product-owner instruction on 2026-07-30, carrying the explicit Codex availability
reverification.

---

## D-054 — Claude corrects the S3 catalogue-search ownership defect and agy reviews the correction

**Date:** 2026-07-30
**Status:** Active. Scoped to the **S3 search-design planning correction only**. Expires at the
reviewed head.

### What went wrong, and what went right

Codex opened Batch 1 under
[D-053](#d-053--codex-availability-is-reverified-codex-implements-s3-and-agy-reviews-it) and **stopped
before editing a single file**, reporting that the approved design could not be built. It was correct.
T026 required a generated `search_text` column that was simultaneously same-row and populated for
Published Courses only, and the artefacts left the owning table as an `ALTER TABLE <course table>`
placeholder. Against the committed S2 schema that placeholder has no valid filling:

- `courses` owns publication — `lifecycle`, `live_revision_id`, `access_suspended_at`, `retired_at` —
  and holds **no** authored text; `0009_course_authoring` dropped its stub `title`.
- `course_revisions` holds the authored text — `title_ar`, `title_en`, `description_ar`,
  `description_en` — and owns **no** Course-level publication state.
- PostgreSQL generated columns cannot reference another table, so neither table satisfies both halves.

**The builder stopping is the outcome this protocol is built to produce.** A builder that had picked a
table to satisfy the letter of the task would have shipped either delisted and archived revision text
sitting in a searchable column, or a column generated from fields that do not exist. Batch 1 produced
**no implementation range** — no production file, migration, test, task closure, or commit — and that is
recorded as a success, not a stall.

### Seats

**Base: `77656aec0c512ae590092e62bcd42b74c33a3362`** — the head at which Batch 1 was dispatched and
stopped.

- **Claude — builder of this planning correction only.** Claude may edit the S3 planning artefacts under
  [`specs/004-public-catalogue/`](../specs/004-public-catalogue/tasks.md) and this decision record.
  Claude has **no production implementation authority**: no backend or frontend source, no migration, no
  schema-version constant, no test.
- **`agy` — independent read-only reviewer.** `agy` (Google Antigravity CLI, `gemini-3.1-pro-high`)
  reviews the frozen exact range through `scripts/agy-review.sh <base>..<head>` under
  [D-032](#d-032--claude-builds-agy-reviews)'s containment harness. `agy` **may not** edit, stage,
  commit, push, or implement. A `TAINTED` or `UNAVAILABLE` run is never recorded as an approval.

Claude authored the correction, so Claude **may not** provide its verdict. Never self-approve.

### The correction

> **`course_revisions` owns the generated catalogue-search text. `courses` owns whether a revision may
> be exposed publicly.**

Search text is generated for **every** revision from that row's own authored columns. Publication is an
exposure rule applied at query time, enforced by two conditions and nothing else: the live-revision join
`courses.live_revision_id = course_revisions.id`, and the canonical `PublishedOnly` predicate.

**A populated `search_text` is not a claim that a row is publicly visible.** Draft, `SUPERSEDED`, and
`REJECTED` revisions, and revisions of `DELISTED`, `ARCHIVED`, retired, or suspended Courses, all
legitimately hold indexed text; none may ever appear in a public result.

**Reason the correction loses nothing.** The withdrawn population boundary was documented in the
approved plan as *"deliberately redundant with `PublishedOnly`, which remains the control."* It was a
second layer over a control that was already load-bearing — and a layer that cannot be built is not a
layer. Two new tasks replace it with redundancy that can be executed: **T032a** asserts the
live-revision exposure boundary, and **T032b** runs the two mutations that must fail — removing the
live-revision join, and removing `PublishedOnly`. Task count moves from 46 to 48; **T026 and T027 were
rewritten in place and keep their identifiers.**

**Alternatives rejected:** a generated column on `courses` (no same-row text to generate from); a
generated column referencing `course_revisions` (PostgreSQL forbids it); a trigger copying revision text
onto `courses` (the denormalization subsystem R-005 already rejected, now also touching S2's authoring
transaction while S2 is closed); an application-maintained search column (the second source of truth
R-002 rejected); a new materialized search-document table (S3 growing into the search subsystem its
scope boundary forbids); keeping "Published only" and choosing a table anyway (shipping an unmeetable
requirement as though it were met).

### Boundaries

Migration numbering is **unchanged**: `0011_catalog_search` for S3, schema version **11** after it, then
`0012_media_and_entitlement` for S4 and `0013_enrollments` / `0014_protected_learning` for S5. This
decision grants **no** S4, S5, or S6 authority, and no authority to implement S3.

### D-053 is not edited retroactively

D-053 recorded a true state of affairs — Codex availability was genuinely reverified, and the planning
head it named had genuinely been reviewed to `APPROVE`. What has changed is that its frozen planning
premise is now known to be defective. Therefore:

- **D-053's implementation authorization is paused and spent.** `343aacb` is no longer a valid
  implementation base and implementation **must not** resume under it.
- D-053's text stands as written. Superseding a decision by editing it destroys the record of what was
  believed when it was made.
- After this correction is approved, a **new** implementation-seat decision must reassign Codex and
  `agy` against the new exact base. That decision is deliberately **not** created in this pass — it
  would be authorizing implementation against a range no reviewer has seen.

**Source:** Codex's Batch 1 blocker report and the product owner's instruction to correct the design, on
2026-07-30.

---

## D-055 — Codex implements S3 from the corrected planning head and agy reviews it

**Date:** 2026-07-30
**Status:** Active. Scoped to **S3 only** — Public Catalogue and Bilingual Shell,
[`specs/004-public-catalogue/`](../specs/004-public-catalogue/tasks.md). Expires when S3 is formally
closed on a recorded independent reviewer verdict.

### Seats

**Implementation base: `f4269d4aad2d146547f7c1184ba2a6fec95bc818`** — the corrected S3 planning head,
which `agy` reviewed over
`77656aec0c512ae590092e62bcd42b74c33a3362..f4269d4aad2d146547f7c1184ba2a6fec95bc818` to `APPROVE` with
zero findings at every severity. `343aacb` is **not** a valid base and must not be used.

- **Codex — S3 implementation builder.** Codex **may** create and modify the production files the
  authorized S3 tasks require: backend Go packages, the `0011_catalog_search` migration,
  schema-version constants, frontend sources, and their tests. Codex works only from task IDs
  authorized in a bounded batch handoff, and **may not approve its own work.** A slice does not close
  on its builder's assessment.
- **`agy` — independent read-only S3 implementation reviewer.** `agy` (Google Antigravity CLI,
  `gemini-3.1-pro-high`) reviews frozen exact commit ranges through `scripts/agy-review.sh <base>..<head>`
  under [D-032](#d-032--claude-builds-agy-reviews)'s containment harness. `agy` **may not** edit, stage,
  commit, push, or implement anything. A `TAINTED` or `UNAVAILABLE` run is never recorded as an approval.
- **Claude — planner and implementation coordinator only.** Claude prepares bounded batch handoffs,
  inspects evidence, validates frozen ranges, and reports. Claude **may not provide the independent
  implementation verdict for S3**, because Claude authored S3's planning artefacts — including the
  2026-07-30 search-ownership correction — and a review of work derived from one's own plan is not
  independent in the sense this protocol means.

### The architecture Codex implements

Settled by [D-054](#d-054--claude-corrects-the-s3-catalogue-search-ownership-defect-and-agy-reviews-the-correction)
and not reopenable by an implementation batch:

> **`course_revisions` owns the generated catalogue-search text. `courses` owns whether a revision may
> be exposed publicly.**

The generated column is built for **every** revision row from that row's own four authored columns —
`title_ar`, `title_en`, `description_ar`, `description_en` — with no publication condition and no
cross-table reference. Public search resolves candidates through
`courses.live_revision_id = course_revisions.id` and applies the canonical `PublishedOnly` predicate.
**A populated `search_text` is not evidence of public visibility.** Historical, draft, superseded,
delisted, retired, and suspended content must never be publicly returned.

**Forbidden**: cross-table generated expressions; copied authored fields on `courses`; synchronization
triggers; a materialized search-document table; application-side Arabic normalization; any change to an
S2 authoring or publication write path.

### Boundaries

This assignment grants **no S4, S5, or S6 implementation authority.** Each requires its own dated
assignment. S3 must not implement authenticated routes, write routes, content authoring, media upload
or transcoding, protected media delivery, entitlement evaluation, Enrollment creation, progress,
invitations, approval workflows, checkout, cart, coupons, payment callbacks, refunds, invoices, BNPL,
or payouts. **No Section price may be exposed in the public API or UI** — Section is not an acquirable
scope under [D-045](#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).

Migration numbering is fixed: `0011_catalog_search` for S3 at schema version **11**, then
`0012_media_and_entitlement` for S4 and `0013_enrollments` / `0014_protected_learning` for S5.

**Work is handed over in bounded batches, not as all 48 tasks at once.** Each batch names its exact
task IDs, its required evidence, and its prohibitions, and each freezes a range for independent review
before the next batch begins. **Batch 1 authorizes storage and the visibility foundation only; the
public search query is deliberately not in it**, so T027 and the exposure proofs T032, T032a, and T032b
belong to a later batch.

### Spent decisions — recorded, not rewritten

**[D-053](#d-053--codex-availability-is-reverified-codex-implements-s3-and-agy-reviews-it) is spent for
implementation purposes.** Codex opened Batch 1 under it, found the approved search design unbuildable
against the committed S2 schema, and **stopped before editing any file.** No implementation range, no
commit, no production file, no migration, no test, and no task completion resulted. That was the correct
outcome — the defect was in the plan.

Two parts of D-053 have different fates, and conflating them would lose real evidence:

- Its **implementation authorization** is spent. Its frozen planning premise was defective, so
  `343aacb` cannot be built from.
- Its **Codex-availability reverification remains valid.** The product owner confirmed Codex
  availability explicitly on 2026-07-30, satisfying
  [D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s precondition positively rather
  than by inference from silence. Nothing about the schema defect touches that confirmation, so this
  decision inherits it rather than asking for it again.

**[D-054](#d-054--claude-corrects-the-s3-catalogue-search-ownership-defect-and-agy-reviews-the-correction)
is spent at `f4269d4`**, reviewed to `APPROVE` with zero findings. Its correction is now the approved
design, above.

**Neither D-053 nor D-054 is edited.** Both recorded true states of affairs when written; superseding a
decision by rewriting it destroys the record of what was believed at the time. The chain of spent S3
seats is D-051, D-052, D-053, D-054.

**Reason:** the one condition that blocked implementation after D-053 was a defective planning premise,
and it is now corrected and independently approved with zero findings. The builder that surfaced the
defect is the builder best placed to implement the correction, and its availability is already
confirmed on the record. Claude cannot hold both builder and reviewer seats and cannot review
implementation of a plan it authored, so the reviewer seat goes to `agy` exactly as it did for every
range in this sequence.

**Alternatives rejected:** resuming under D-053 without a new decision (its base is invalid and its
authorization spent, so the range would have no valid premise); editing D-053 to point at the new head
(destroys the record of the defect and of Codex's correct stop); handing Codex all 48 tasks in one batch
(an unbounded handoff produces an unreviewable range and defeats Checkpoint 2); including the public
search query in Batch 1 (its exposure proofs are the highest-risk evidence in the slice and deserve
their own frozen range); Claude reviewing S3 implementation (not independent of the plan it authored,
now doubly so after authoring the correction).

**Source:** Product-owner instruction on 2026-07-30, following `agy`'s `APPROVE` of the corrected
planning head.

---

## D-056 — Codex builds S4 D7 and an independent reviewer reviews the frozen range

**Date:** 2026-08-01
**Status:** Active. Scoped to **S4 D7 only** — Media Pipeline, tasks `T001`–`T013`, in
[`specs/005-media-and-entitlement-evaluation/`](../specs/005-media-and-entitlement-evaluation/tasks.md).

### Seats and boundaries

- **Codex — implementation builder for S4 D7.** This dated product-owner assignment replaces the
  stale Antigravity builder assignment for this implementation run only. Codex may implement only
  `T001`–`T013`, may not review or close its own range, and receives no authority over D8, S5, or S6.
- **Independent reviewer — separate review seat.** The reviewer evaluates one frozen exact D7 range;
  the builder does not provide the independent verdict.
- **S4 remains split into D7 and D8.** D7 is the media pipeline. D8 is protected delivery and
  entitlement evaluation and does not begin in this run.
- **S5 remains blocked until S4 closes independently.** Its approved planning artifacts stay frozen;
  this decision does not authorize S5 implementation.

**Reason:** the approved dependency order requires S4 before S5, while the active selector and launch
status still pointed at stale earlier work. The product owner explicitly assigned Codex to the bounded
S4 D7 queue on 2026-08-01 and retained independent review and the D7/D8 split.

**Source:** Product-owner instruction on 2026-08-01.

---

## D-064 — S4 stable learning-material entry points

**Date:** 2026-08-02
**Status:** Active. Scoped to T057 and the S4-owned Resource/Lab Material browser entry boundary.

**Decision:** S4 remains the sole owner of material discovery, current Asset Version resolution,
Student authentication and Entitlement evaluation, retirement/readiness policy, signed-target
issuance, storage details, and redirect success. S5 may render only fixed same-origin entry links.

S4 mounts `GET /api/v1/media/lessons/:lessonId/materials/resource` and
`GET /api/v1/media/lessons/:lessonId/materials/lab-material` through the existing media foundation.
Each request resolves the current live Lesson material internally, reauthorizes the current
Student, and signs only after that decision. Success is `302 Found` with `Location`,
`Cache-Control: no-store`, and `Referrer-Policy: no-referrer`; the signed target is never a JSON
field or persisted value. Failure uses the existing uniform protected-unavailable `404` with no
`Location`.

S4 also exposes a bounded read-only bulk material-kind boundary for S5. It returns only the
presentation kinds `resource` and `lab_material` for current ready material attached to stable
Lesson identities. D-063 Course Home Lesson entries and Lesson responses expose an always-present,
deterministically ordered `materials` array containing only those kinds; retained-expired and
unavailable reads expose an empty array. No Asset Version, URL, object key, expiry, capability,
evaluator decision, or storage field crosses the S5 boundary. The existing POST download
authorization contract remains supported and shares the same resolver/evaluator/signer path.

**Source:** Product-owner instruction on 2026-08-02.

---

## D-063 — S5 protected learning read-model contract

**Date:** 2026-08-02
**Status:** Active. Scoped only to the T046–T049 protected learning read surfaces.

**Decision:** Mount exactly `GET /api/v1/learn/dashboard`,
`GET /api/v1/learn/courses/:courseId`, and
`GET /api/v1/learn/courses/:courseId/lessons/:lessonId` through the mandatory
`LearningFoundation`. Successful responses are direct HTTP 200 JSON objects using snake_case,
string UUIDs, and RFC 3339 UTC timestamps. Protected responses are `Cache-Control: no-store` and
contain presentation state only; the only exposed state enum is `learning_status: "active" | "expired"`.

The Dashboard returns `courses`, ordered by Enrollment creation descending then Course ID ascending,
with Course ID, localized title, learning status, nullable expiry, and completed/total/percentage
Progress. Course Home returns the current live Course title, status, expiry, Course Progress, and
authored Sections/Lessons with stable IDs and per-Lesson Progress. Lesson metadata returns stable
Course/Lesson/Section IDs, localized titles, status, expiry, Student-scoped Progress, and previous/
next stable Lesson IDs across Section boundaries.

Expired read state is narrowly retained only when S4 classifies the access as effective expiry and a
retained Enrollment exists. It never issues playback, accepts Progress, refreshes or extends access,
creates Enrollment, or exposes signed media. All other denied or inconsistent states use the existing
uniform protected-unavailable response. No Entitlement IDs, Enrollment IDs, revision IDs, evaluator
decisions, capability booleans, trusted duration, Asset Version IDs, manifests, or playback sessions
are exposed.

When media is unavailable, the Lesson read model remains present without media metadata and the
separate S4 playback request fails uniformly; a later player surface renders that result as
content-unavailable without adding another read-model authorization enum.

**Boundary:** S4 owns active-versus-expired classification. S5 reads the authoritative Enrollment,
live graph, and stable Progress only; it does not reproduce Entitlement policy. The read routes are
strictly read-only and use bounded bulk queries with no per-Course or per-Lesson authority loops.

**Reason:** The frozen S5 artifacts defined semantic learning surfaces but omitted concrete success
schemas. This decision supplies the smallest deterministic contract needed for production handlers,
integration tests, and later browser surfaces without creating a second authorization model.

**Source:** Product-owner instruction on 2026-08-02.

---

## D-057 — Codex builds S4 D8 after D7 approval; independent Tier 3 review remains required

**Date:** 2026-08-01
**Status:** Active. Scoped to **S4 D8 only** — Entitlement evaluation and protected delivery, tasks
`T014`–`T032`, in
[`specs/005-media-and-entitlement-evaluation/`](../specs/005-media-and-entitlement-evaluation/tasks.md).

### Seats and boundaries

- **D7 is independently approved** at `1e3d7c317e3552012b6c73c1f2a7522b2e6b5940`. Its approved
  implementation range remains immutable.
- **Codex — implementation builder for S4 D8.** This dated product-owner assignment authorizes only
  `T014`–`T032`. Codex may not approve or close S4.
- **Independent Tier 3 reviewer — separate review seat.** The reviewer evaluates one frozen exact D8
  range after implementation. D8 and S4 do not close on the builder's assessment.
- **S4 remains split into D7 and D8.** D7 is complete and approved; D8 is the active implementation
  queue. S4 remains open until D8 has an independent closing verdict.
- **S5 remains blocked until the full S4 slice closes independently.** Its approved planning
  artifacts remain frozen.
- **S6 remains the sole owner of production Entitlement creation.** D8 evaluates Entitlements and
  must not add a producer, invitation workflow, or grant surface.

**Reason:** the product owner independently approved the completed D7 range and explicitly assigned
Codex the bounded D8 queue on 2026-08-01, while retaining the Tier 3 independent-review requirement
and the established consumer-before-producer boundary.

**Source:** Product-owner instruction on 2026-08-01.

---

## D-058 — S4 closes after independent approval of D7 and D8; S5 is unblocked

**Date:** 2026-08-01
**Status:** Closed. Applies only to the completed S4 Media Pipeline, Protected Delivery, and
Entitlement Evaluation slice.

**Decision:**

- **D7 — Media Pipeline (`T001`–`T013`)** is independently approved at
  `1e3d7c317e3552012b6c73c1f2a7522b2e6b5940` after remediation.
- **D8 — Entitlement Evaluation and Protected Delivery (`T014`–`T032`)** is independently approved
  at `944c0a77079d632c6b836c7d60c46ff6144e7aa5` with no implementation findings remaining.
- **S4 is formally closed.** All `T001`–`T032` are complete. The immutable approved S4
  implementation range is
  `2bc8329016f76115d8a3243538f1e2bde81d2768..944c0a77079d632c6b836c7d60c46ff6144e7aa5`.
- **S5 — Protected Learning** is unblocked and becomes the active implementation feature at
  [`specs/007-protected-learning/`](../specs/007-protected-learning/tasks.md). Its planning remains
  frozen; no S5 implementation or task completion is recorded by this closure decision.
- **S6 remains not started and is the exclusive production Entitlement-creation owner.** S4
  evaluates Entitlements only; closing S4 does not transfer creation ownership to S5.

**Reason:** independent review approved both completed S4 ranges with no remaining implementation
findings. Recording the closure releases the next dependency-ordered feature without changing its
approved plan or authorizing its implementation.

**Source:** Product-owner closure instruction on 2026-08-01.

---

## D-059 — S5 clean-tree evidence compares the recorded baseline

**Date:** 2026-08-01
**Status:** Active. Scoped only to S5 validation mechanics.

**Decision:** The former absolute-empty S5 T077 status gate conflicts with the mandatory preservation
of documented user-owned untracked paths. S5 cleanliness is therefore evaluated relative to a
NUL-delimited porcelain baseline captured immediately before implementation. After the implementation
commit, the final status must be byte-identical to that baseline, with no newly introduced tracked or
untracked residue and no baseline path removed, staged, committed, ignored, or relocated. The
documented user-owned paths remain visible and uncommitted. T078 remains independently owned by hosted
CI and Tier 3 review.

**Reason:** An absolute-empty status is not a valid implementation gate in a repository where the
safety contract requires pre-existing user work to remain present. Baseline comparison proves the
actual requirement: implementation leaves no residue while preserving unrelated work.

**Scope:** This changes validation mechanics only. It changes no S5 product requirement, acceptance
criterion, implementation boundary, or independent-review requirement.

**Source:** Product-owner instruction on 2026-08-01.

---

## D-060 — S5 Progress uses stable Lesson identities

**Date:** 2026-08-01
**Status:** Active. Scoped only to S5 Progress identity.

**Decision:** S5 Progress is durably keyed by
`course_lesson_identity_id → course_lesson_identities(id)`. `course_lessons.id` remains a
revision-owned content row and `lessons(id)` is not an S5 Progress authority. Current metadata is
resolved through the authoritative live Course revision; an exact approved Asset Version is validated
separately and may be retained as completion evidence.

**Boundary:** No compatibility mapping or synthetic legacy `lessons` rows will be introduced, and S5
does not create a second Student-visible learning graph. This reconciles S5 with the approved S2
identity model without expanding scope or changing S4/S6 ownership. Production Entitlement creation
remains absent. The uncommitted, unapproved `0013`/`0014` implementation may be corrected in place.

**Reason:** Stable Lesson identity preserves one Progress record across Course revision cloning,
metadata changes, and video or Asset Version replacement; a revision-row key cannot provide that
guarantee.

**Source:** Product-owner instruction on 2026-08-01.

---

## D-061 — S5 Progress source-address ceiling

**Date:** 2026-08-01
**Status:** Active. Scoped only to S5 Progress admission.

**Decision:** Progress writes have an additional server-derived source-address ceiling of **1,200
requests per minute** with a **120-request burst**. IPv4 keys use the individual address; IPv6 keys
use the `/64` network prefix. The ceiling is evaluated before the existing 12 writes/minute per
`(Student, stable Lesson identity)` policy and before Enrollment, media, or Progress persistence.

**Boundary:** Source addresses are derived only through the configured trusted-proxy and remote-address
policy. A quota denial is `429` with `Cache-Control: no-store` and `Retry-After`; a limiter dependency
failure returns the existing protected-unavailable response. This adds a network ceiling and does not
replace the Student/Lesson policy, create a second limiter, or alter S4/S6 ownership.

**Reason:** The 1,200/minute rate and 120-request burst support roughly 100 active learners behind a
shared NAT while bounding abusive traffic from one source.

**Source:** Product-owner instruction on 2026-08-01.

---

## D-062 — S5 Progress retry policy

**Date:** 2026-08-01
**Status:** Active. Scoped only to S5 client Progress reporting.

**Decision:** A logical client Progress chain has one initial request and at most two automatic
retries. Ordinary retryable failures use exponential nominal delays of 2 seconds and 4 seconds with
symmetric ±20% jitter from an injectable randomness source. Network failures except deliberate
cancellation, and only HTTP 408, 429, 500, 502, 503, and 504, are retryable. Every other status,
malformed local input, and lifecycle cancellation is final. A new sample coalesces into the active
chain and never resets its three-attempt budget.

**429 boundary:** A valid `Retry-After` delta-seconds value or future HTTP-date is used without
jitter. A missing, malformed, negative, or expired value falls back to 15 seconds. It still consumes
the same attempt budget and cannot create a separate chain.

**Lifecycle:** One reporter instance permits one in-flight request, one pending greatest valid sample,
and one retry timer, all scoped to a stable Lesson and exact Asset Version. Replacing either identity or
disposing the reporter aborts the ordinary request where supported and discards timers and pending
state. `pagehide` uses a same-origin `PUT` JSON `fetch` with credentials and `keepalive: true`; it
does not use `sendBeacon`, whose POST method cannot satisfy the strict Progress `PUT` contract, and
never starts a delayed retry.

**Boundary:** Every retry is an ordinary Progress request. It remains subject to authentication,
both rate limits, S4 entitlement evaluation, strict decoding, trusted-media validation, and the
stable-key atomic upsert. The client never caches a decision or treats its Asset Version ID as
trusted.

**Reason:** The bounded sequence fits inside the 15-second reporting cadence for ordinary failures,
avoids a request storm against the 12-writes/minute limit, and preserves an otherwise-authorised
playback session without concealing server-side mutation ambiguity.

**Source:** Product-owner instruction on 2026-08-01.

---

## D-065 — Exact-visible report-context binding

**Date:** 2026-08-04
**Status:** Active. Scoped to S5 T062 (report creation) and T063 (the report route).

**Decision:** A content report references the content instance **actually rendered to the Student**.
The server must not resolve "whatever is current" at report-submission time while the Student may
still be viewing an older instance.

The rendered instance is captured as an **authenticated-encrypted opaque report-context token**,
minted as part of the authoritative read that produced the visible page. "Opaque" means the public
token does not reveal the encrypted internal binding when decoded: a signed-but-base64-readable
payload is **not** opaque, because base64 is an encoding rather than a secret and any holder could
decode the internal graph. The token uses AES-256-GCM under a key derived from the root
application secret with the explicit domain separation label
`gradex/learning/report-context/encryption/v1`; the version and purpose are bound as authenticated
additional data, a fresh random nonce is used per mint, and authentication failure yields no
plaintext. The context binds: reporter Account
identity; authenticated session identity; Course identity; report target kind; stable logical
target identity; exact visible Course Revision; exact visible Media Asset Version where
applicable; issuance time; expiry; a purpose/audience restricted to content reporting; a token
format version; and a cryptographically strong nonce.

**The context is evidence, never capability.** It grants no authority for playback, Progress,
Resource access, Lab Material access, Enrollment, Entitlement, moderation, report resolution, or
any other operation.

**Consequences:**

- A page rendered from Revision A reports **A** even after Revision B becomes live; likewise for a
  replaced Media Asset Version.
- Contexts are minted at original render time. A context requested lazily when the Student clicks
  "Report" would re-resolve current state and bind **B**, so late issuance that re-resolves is
  **forbidden**.
- An expired context is **refused**; the page reloads, which legitimately re-renders — and
  re-reports — the newer content. Automatic renewal against current content is forbidden, because
  renewal is silent rebinding.
- Raw Revision and Asset Version identifiers remain absent from public learning read models
  (D-063) and are **encrypted** inside the token; the client receives only an opaque string it can
  neither read, choose, edit, nor forge.
- Token decryption, expiry, purpose, and session/reporter verification belong to **T063** at the
  HTTP boundary. Relational verification of the binding — that the revision belongs to the Course,
  the stable target existed in that revision, and the Asset Version was the one bound to that
  target and kind — remains inside the **T062** domain boundary, because authenticity proves only
  that the server minted the values, not that they form a coherent target.
- Relational verification accepts a genuinely historical instance. It does **not** require the
  bound revision to still be the live pointer; that requirement is what produced the defect.

**Reason:** Resolving current content at submission records an instance the Student never saw, so
the report describes content other than the one complained about. An encrypted context is the
narrowest correction that preserves exact-visible fidelity without exposing internal identifiers to
the client at all, and without granting any capability.

**Source:** Product-owner instruction on 2026-08-04, after review found T062 resolved current
content at submission. Amended the same day, after review found the first token format was signed
but base64-readable and therefore not opaque.

---

## D-066 — Duplicate-open-report granularity

**Date:** 2026-08-04
**Status:** Active. Scoped to S5 report submission.

**Decision:** One unresolved report per `(reporter_account_id, target_kind, stable target_id)`. The
duplicate key is intentionally **independent of** the exact Revision or Asset Version, and the
existing `rep_no_duplicate_open` partial unique index is retained unchanged.

**Consequences:**

- A Student cannot create a second unresolved report for replacement version B while their report
  for version A remains open.
- After report A is resolved (S8's behaviour), the Student may file a new report for B.
- Another Student may independently report B.
- Different target kinds on the same stable Lesson remain distinct reports.
- No migration is added for version-granular duplication.

**Reason:** A Student who has already reported a target has been heard; a replacement version does
not entitle them to a second open queue entry for the same logical content. Duplicate refusal and
the 5/hour throttle remain separate controls because they fail differently (R-11).

**Source:** Product-owner instruction on 2026-08-04.

---

## D-067 — S5 convergence is gated on authorized history, not a commit count

**Date:** 2026-08-05
**Status:** Active. Scoped only to S5 validation mechanics.

**Decision:** T077's `git rev-list --count 9c8348a..HEAD` **must equal `3`** clause is replaced by a
semantic authorized-history gate, implemented as `scripts/s5-convergence-gate.sh`. The three accepted
commits are pinned by exact SHA — `f81d8327` (the D-059 clean-tree gate correction), `e7736077` (the
D-060 reconciliation), and `5cc8ede0` (the protected-learning implementation). Every later commit in
the range must match exactly one authorized subject class, and its changed paths must fall inside that
class's allowlist. Merge commits are forbidden. No commit after the implementation commit may touch S5
production scope; an authorized subject is not a licence to edit a handler, a repository, a migration,
or a UI component. An unclassified commit fails the gate.

Authorized classes are the convergence-gate correction, the T075 evidence-workflow validation
correction, the T075/T076 evidence closure, and the final convergence and independent-review record.

**Why the numeral was wrong:** it was a derived prediction, introduced by `e7736077` before any
CI-produced evidence existed, and it assumed every remaining S5 task would land inside one final
implementation commit. That assumption cannot hold. T075's evidence is a GitHub Actions artifact and
T078's is hosted CI green on an exact head plus an independent Tier 3 verdict, and none of those can
exist until a commit has been pushed — so the range must legitimately grow after the implementation
commit, and the fixed count fails for a reason unrelated to the property it was protecting.

**This is a correction, not a relaxation.** A raw count cannot distinguish an authorized commit from
an unauthorized one of the same number; membership plus path boundaries can, and additionally rejects
merge commits, unclassified commits, and production edits smuggled under a well-formed subject. The
gate is therefore strictly stronger than the numeral it replaces.

**Scope:** This changes validation mechanics only. It changes no S5 product requirement, acceptance
criterion, implementation boundary, performance threshold, information-hiding rule, or
independent-review requirement. D-059's clean-tree guarantees are unchanged and still enforced: the
working tree must be clean, the index must be clean, and the NUL-delimited final porcelain status must
be byte-identical to the recorded pre-implementation baseline, with no baseline path removed, staged,
committed, ignored, or relocated. T078 remains independently owned by hosted CI and Tier 3 review, and
the builder still never approves its own slice.

**Amended 2026-08-05**, after the first hosted run that actually created jobs
([31035395606](https://github.com/Owlah2025/gradex/actions/runs/31035395606)) failed two jobs and
exposed test- and evidence-infrastructure defects none of the forecasted classes covered: the E2E
seeder's `TestMain` refused an ordinary `go test -race ./...`, failing the Backend job, and a failing
evidence run was undiagnosable because hosted logs need admin rights while `continue-on-error`
rewrote the step conclusion to success. One further class, **`CI_STABILIZATION`**, is therefore
authorized under the anchored subject `fix(ci): stabilize hosted S5 verification`. Its allowlist is
an **exact file list** — `backend/cmd/e2e-seed/seed_test.go`,
`backend/cmd/e2e-seed/invocation_test.go`, `.github/workflows/ci.yml`, and
`frontend/scripts/t075-evidence-manifest.mjs` — deliberately not a directory glob, so a future
non-test file in the same directory is not quietly admitted.

The class cannot reach S5 production learning scope: no handler, repository, migration, schema, API
contract, or learning page or component. Product behaviour, every S5 acceptance criterion, the
5-second SC-001 threshold, the information-hiding rules, and T078's independent-review requirement
are unchanged; this is validation-mechanics maintenance, not product implementation. The exact SHA
requirements, the no-merge and no-unclassified-commit rules, the global production-scope deny, and
D-059's clean-tree and baseline checks all remain in force.

**Source:** Product-owner instruction on 2026-08-05.

---

## D-068 — S5 task closures are authorized separately, one truthful commit each

**Date:** 2026-08-06
**Status:** Active. Scoped only to S5 validation mechanics.

**Decision:** The convergence gate's combined closure subject `docs(s5): close T075 and T076` is
replaced by two exactly matched subjects, `docs(s5): close T075` and `docs(s5): close T076`. Each is
matched with no trailing glob, so neither the obsolete combined subject nor a near-match such as
`docs(s5): close T075 evidence` classifies. The gate-extension commit that introduces this carries
its own exact `GATE` subject, `docs(s5): authorize separate S5 closures`.

**Why:** T075 and T076 are independently evidenced tasks. T075 closes on a verified retained hosted
artifact; T076 closes on its own time-to-first-frame measurement under SC-001. The combined subject
forced a choice between two bad options — commit a subject asserting that T076 was closed when it had
not been begun, or leave verified T075 evidence uncommitted indefinitely. A commit message is
permanent history, so a subject that misstates what a commit does is not an acceptable cost of
satisfying a gate. Separate authorization also means T076's own closure needs no further gate change.

**Boundary:** The closure path allowlist is **unchanged** — the same closure evidence paths as before,
with no new application, test, workflow, or general documentation scope, and no broad subject prefix
or path glob. Neither closure subject may touch S5 production scope; the global production denial
still applies. The exact accepted SHA requirements, the no-merge and no-unclassified rules, and
D-059's clean-tree and baseline comparison all remain in force.

**Scope:** Validation mechanics only. No product requirement, acceptance criterion, implementation
boundary, performance threshold, information-hiding rule, or independent-review requirement changes.
T077 and T078 remain independently gated, and no task may be marked complete without the evidence its
own text requires — separate authorization changes who may commit a closure, never what closing it
demands.

**Source:** Product-owner instruction on 2026-08-06.

---

## D-069 — SC-001 is measured against the built frontend behind a run-owned same-origin proxy

**Date:** 2026-08-06
**Status:** Active. Scoped only to S5 T076 evidence mechanics.

**Decision:** T076 measures SC-001 against `next build` output, not `next dev`, and serves it behind a
test-only loopback proxy that reproduces the deployed same-origin frontend/API boundary. One new gate
class, **`T076_EVIDENCE`**, is authorized under the exact subject
`test(s5): add production-origin SC-001 evidence`, permitting exactly four files:
`frontend/e2e/s5-playback-performance.spec.ts`, `frontend/e2e/production-origin-proxy.mjs`,
`frontend/playwright.config.ts`, and `.github/workflows/ci.yml`. The gate-extension commit carries the
exact `GATE` subject `docs(s5): authorize production-origin T076 evidence`.

**Why not `next dev`:** measured there, the figure is dominated by on-demand compilation and
unoptimized, unbundled assets. Under the deterministic profile every viewport measured
between 8722 ms and 9941 ms, and a diagnostic showed roughly 86% of that elapsed before the Play
control appeared, with media start itself about 1.2 s. That measures the development server, not the
shipped product SC-001 describes.

**Why a proxy is required:** the browser client calls relative `/api/v1/...`, and `next.config.mjs`
deliberately provides rewrites **only** in development because the deployed edge fronts the frontend
and the Go API behind one external origin. A standalone `next start` therefore serves those calls
itself and returns 404 — observed as "This lesson could not start." The proxy supplies that one
deployed property for evidence only: `/api/*` forwards to the run-owned Go API, every other route to
the built Next server, and the generated HLS fixture stays on its existing dynamic loopback origin.
`next.config.mjs` is **not** modified, no production rewrite is added, and no package or lockfile
changes — the proxy uses Node built-ins alone.

**Measurement semantics, unchanged:** the deterministic CDP profile
`gradex-sc001-deterministic-4g` (offline false, 150 ms latency, 500,000 B/s down, 125,000 B/s up,
`cellular4g`, cache disabled) applied through `Network.enable`, `Network.setCacheDisabled`, and
`Network.emulateNetworkConditions`; the clock starts immediately before navigation and the real Play
action stays inside the measured interval; first-frame evidence is `totalVideoFrames > 0` or a
`timeupdate` with `currentTime > 0`, never `loadedmetadata`, `loadeddata`, `canplay`, `readyState`, or
visibility; and every viewport must remain **strictly below 5000 ms**, asserted per viewport rather
than averaged. Neither the profile nor the threshold may be tuned after observing a result.

**Boundary:** browser-visible frontend and API share one origin, which is the deployed contract and
must not be mistaken for a defect; the media fixture keeps its own dynamic loopback port and is never
claimed to be on port 3000; every application, API, and media dependency stays loopback with a public
dependency count of zero. Production mode is opt-in through `GRADEX_E2E_FRONTEND_MODE=production`, so
T075 and every existing suite keep their current behaviour, and a missing production build fails
closed rather than falling back to the development server.

**Scope:** Evidence mechanics only. No production behaviour, player preload, autoplay, buffering,
media architecture, acceptance criterion, or independent-review requirement changes. T077 and T078
remain separately gated.

**Source:** Product-owner instruction on 2026-08-06.

---

## D-070 — T077 closes on the verified convergence gate, independently of T078

**Date:** 2026-08-06
**Status:** Active. Scoped only to S5 closure authorization.

**Decision:** The convergence gate authorizes one further closure subject, `docs(s5): close T077`,
matched exactly with no trailing glob and restricted to the existing closure evidence paths. No new
application, test, workflow, or general documentation scope is added.

**Why:** T077 is a *local* gate — clean working tree and index, final NUL-delimited porcelain
byte-identical to the recorded pre-implementation baseline, the accepted commits present at their exact
SHAs, every later commit classified inside its class's path allowlist, no post-implementation
production-scope change, no merge commit, and no unclassified commit. All of that is verifiable by the
builder on the final history, so T077 can and should close on its own evidence.

T078 cannot. It requires hosted CI green on one exact head **and** a recorded independent Tier 3
reviewer verdict against one frozen commit range, with every critical and high finding resolved.
Builder verification is not independent review, a local green run does not satisfy it, and a review
that yields no retrievable verdict is `UNAVAILABLE` rather than approval. Closing T077 therefore says
nothing about T078, and the exact-match subject exists so no commit can imply otherwise.

**Boundary:** the closure path allowlist is unchanged; the exact accepted SHA requirements, the
no-merge and no-unclassified rules, the global production-scope denial, and D-059's clean-tree and
baseline comparison all remain in force. No production behaviour changes.

**Source:** Product-owner instruction on 2026-08-06.

## D-071 — Playback issuance enforces two rate ceilings, and the gate opens exactly one production exception

**Date:** 2026-08-06
**Status:** Active. Scoped to the S5 protected-learning slice.

**Decision:** Playback issuance enforces two independent ceilings, decided before authorization and
before any issuance:

| Ceiling | Dimension | Quota | Window |
| --- | --- | --- | --- |
| Student | `identifier` | 30 issuances | 10 minutes |
| Source address | `source_address` | 600 issuances | 10 minutes |

Both policies set `FailClosed: true`. The source-address ceiling is asked first, then the Student
ceiling, matching the order the Progress mutation already uses. A denied quota answers `429` with
`Cache-Control: no-store` and a `Retry-After` equal to the policy window. An *undecidable* ceiling —
the rate-limit dependency unavailable — is refused as protected-unavailable and never issues.

The convergence gate gains one GATE anchor (`docs(s5): authorize Tier 3 playback remediation`), a
`TIER3_REMEDIATION` class for the fix, a `TIER3_EVIDENCE` class for the records, and exactly one
production-scope exception, expressed as (class, exact path) pairs.

**Why:** Independent Tier 3 review of frozen head `f5985c76a9c69dea7a3d2e8128ad069ae6a663fd` found
(H-1, High) that playback issuance shipped with **no** rate limit, although FR-017, BR-102,
`contracts/learning-api.md`, and research R-04 all require one. The Student figure of 30 per 10 minutes
is the quota those documents already state; it is not a new number and was not re-derived here.

The source-address ceiling **was** chosen in this pass, because no numeric value for it exists in any
requirement. It is set at 600 per 10 minutes — twenty times one Student's quota — on three constraints:

1. **No ordinary Student may exhaust it.** A Student who fully spends their own 30 consumes 5% of the
   source ceiling, so a shared address cannot be bricked by one legitimate learner.
2. **It must survive a real campus NAT.** D-061 already sizes the Progress source ceiling against a
   shared university or campus egress address. At a plausible 6 issuances per learner per 10 minutes,
   600 accommodates roughly 100 concurrent learners behind one address.
3. **It must still catch bulk extraction.** 600 per 10 minutes caps sustained scripted issuance at one
   per second — far below what unattended enumeration of a course library needs, so the abnormal case
   is bounded even when the attacker rotates Student sessions behind one address.

The two ceilings stay separate policies with separate keys. Merging them would let one heavy Student
consume a shared address's headroom, or let a NAT population mask a single extracting account.

The Student bucket is keyed on the Student alone — not on the Lesson, and not on the session. Keying it
per Lesson would hand an extractor a fresh quota for every Lesson, which is the exact pattern R-04
sized this control against.

`FailClosed: true` is required rather than incidental. The local in-process fallback cannot decide a
shared quota, so approximating one when the dependency is down would answer a security question with a
guess. Refusing is the only honest answer, and it matches `learning-progress-source`.

**Why the gate needed a production exception:** clause 4 forbids production edits after the
implementation commit, and it was right to — that clause is what stops an authorized *subject* from
becoming a licence to edit a handler. But a rate limit has nowhere to live except production code, so
H-1 could not be remediated under any existing class. The exception is therefore written as an explicit
(class, exact path) predicate rather than as a hole in the production-path detector: only
`TIER3_REMEDIATION` can use it, only five named files are reachable through it, no directory glob
appears in it, and every one of those files is *also* inside the class's own path allowlist, so a
mistyped path fails two checks instead of passing one. `TIER3_EVIDENCE` deliberately reaches no
production and no test path.

**Also in this pass:**

- **M-1 (Medium).** The hosted Admission Integration job's package list omitted `./internal/learning`,
  so that package's integration tests compiled under vet but never executed in hosted CI. The package
  is added to the list.
- **M-2 (Medium, evidence chain).** The T075 closure record cited a GitHub artifact ID and byte size
  that are no longer retrievable. The record is corrected to state the supersession history truthfully
  rather than re-pointing the original audit claim at a different byte set. See the T075 note in
  `specs/007-protected-learning/tasks.md`.
- **FR-017 traceability.** T025 is amended so the playback half of FR-017 has an explicit owning task.
  The amendment is dated and marked as review remediation; it does **not** claim the original
  implementation commit contained the control, because it did not.

**Boundary:** playback authorization semantics are unchanged, and the playback signature lifetime is
unchanged. The three Low findings from the same review (stale report-throttle comments, `completed_at`
using `time.Now()` rather than an injected clock, and no server-side CSRF middleware on learning
mutations) are **not** addressed here and remain open. Head `f5985c7` is superseded. T077 is revalidated
against the remediated history; **T078 remains open** and requires a fresh independent Tier 3 review,
because this pass was authored by the builder.

**Amendment, same day — one more authorized path.** Hosted run
[31070393404](https://github.com/Owlah2025/gradex/actions/runs/31070393404) failed `Admission Integration`
on head `b1190f0`. Three places assemble the protected-learning policy map independently — `cmd/api/main.go`,
the `!production` test helper, and the integration fixture in
`backend/internal/httpapi/learning_progress_integration_test.go` — and `NewLearningFoundation` refuses to
construct unless every endpoint in `requiredLearningPolicyEndpoints` is present. Adding the two playback
endpoints to the first two left the third short, so every `internal/httpapi` integration test that builds
the production foundation failed. `TIER3_REMEDIATION` therefore also admits that one integration fixture.

This is a *test* path, not a production one: it does not use the production exception, and the exception's
five-file list is unchanged. The underlying duplication — one required-endpoint list, three independently
maintained policy maps — is a real defect that will reappear on the next added endpoint, but consolidating
it means a new exported production constructor, which is outside this pass's authorized scope. It is
recorded here as open rather than fixed silently.

**Source:** Independent Tier 3 review findings H-1, M-1, M-2 against head `f5985c7`, and the
product-owner remediation instruction on 2026-08-06.

## D-072 — T078 closes on hosted CI plus an independent Tier 3 APPROVE, and the closure commit is not the reviewed candidate

**Date:** 2026-08-06
**Status:** Active. Scoped to S5 closure.

**Decision:** T078 closes when two things exist together and not otherwise: hosted CI green on the exact
frozen candidate, and an independent Tier 3 verdict of `APPROVE` against that candidate's range with no
unresolved Critical or High finding. Both conditions are met. The approved reviewed range ends at
`41373a865bf4dc310f9b9b20139daecbb65767e0`; hosted run
[31100802602](https://github.com/Owlah2025/gradex/actions/runs/31100802602) was green on all six jobs.

The convergence gate gains one GATE anchor (`docs(s5): authorize independent T078 closure`) and one
`T078_CLOSURE` class permitting four exact record paths — the task register, the 2026-08-06 daily record,
`STATUS.md`, and one named review record. No glob, and deliberately not `docs/launch/review/*`.

**Why the closure commit sits outside the reviewed range:** recording a verdict necessarily happens after
the verdict exists, so the commit that records it cannot be inside the range the verdict covers. That is
not a loophole to be papered over — it is a boundary that has to be stated, or the record would imply the
reviewer approved its own citation. The reviewer approved `9c8348a1..41373a865bf4dc310f9b9b20139daecbb65767e0`.
The closure commit is documentation and evidence only, is confined to the four paths above, and changes no
production behaviour, so it needs no further independent review. A closure commit that strayed outside
that boundary would change what was approved and would require a new review.

**Follow-ups remain open, and closure does not resolve them.** The reviewer retained one Medium
(`F-1`, playback rate limiting has no attributable per-Student/per-source monitoring signal) and several
Low findings, alongside three previously disclosed Low items. Only `F-2` — `STATUS.md` materially
understating S5 delivery state — is reconciled in this pass, because it is a truthfulness defect in the
status record itself. The rest are tracked, not fixed, and S5 does not reopen merely because they remain.

**Boundary:** no production code, test, migration, CI, or rate-limit value changes in this pass. The
retained T075/T076 artifacts are untouched. The reviewed frozen range is unaltered.

**Source:** Independent Tier 3 rereview verdict `APPROVE` against frozen head
`41373a865bf4dc310f9b9b20139daecbb65767e0`, transmitted by the product owner on 2026-08-06, and the
product-owner closure instruction of the same date.

## D-073 — S6 owns the Course default access-expiry column, because no closed slice created it

**Date:** 2026-08-06
**Status:** Active. **Acknowledged by Product Owner Ahmed Hazem (2026-08-07)** with effort and schedule consequences explicitly acknowledged. Scoped to S6 planning and implementation.

**Provenance.** On August 7, 2026, Product Owner Ahmed Hazem issued an explicit product-owner instruction acknowledging D-073, approving S6 ownership of `courses.default_access_ends_at`, acknowledging the associated effort and schedule consequences, confirming that the D-073 work already implemented in the first S6 subgroup was authorized, and establishing this instruction as the authoritative provenance. This acknowledgement was transmitted through the implementation instruction for the S6 documentation remediation pass. This acknowledgement does not approve any independently rejected implementation range (`d9e483f..a5a2748` and `a5a2748..681f4a9` remain rejected).

**The gap.** BR-025 requires that "before a Course Access Invitation for a Course can be **approved**, an
Admin must have configured a future Course `default_access_ends_at` instant," which Admin Approval then
snapshots onto the Entitlement. The S6 planning artifacts treat that column as inherited: `spec.md`
lists Course as "Read only — lifecycle state and configured access-expiry instant," and
[data-model.md §6](../specs/006-course-access-grant/data-model.md#6-the-grant-transaction) step 5
asserts `course.default_access_ends_at` is present and in the future.

**It does not exist.** Verified against the committed schema at the S5 closure head `d5ce557`: no
migration `0001`–`0014` creates a `default_access_ends_at` column, and no other course-level
access-expiry or access-duration column exists on `courses` or `course_revisions`. The only expiry
columns in the schema are on `entitlements` (`original_access_ends_at`, `access_ends_at`), which are the
*snapshot targets*, not the source. `course_price_changes` and `course_sections.price_minor_units` carry
price, not duration.

**Consequence if unowned:** every approval refuses under FR-017, so FR-015 never executes and no
Entitlement is ever created. The single grant path in the product would be unreachable — a total
functional failure, not a degradation.

**Decision:** **S6 owns the column and its Admin configuration surface.** The owner is derived, not
chosen: S2 is closed and frozen at `785d71c` and reopening it would discard the frozen-range evidence
that closed it; S4 and S5 are closed and did not create it; and
[SLICES.md §2 rule 2](launch/SLICES.md#1-rules) forbids a forward dependency, so it cannot be left to
S8. S6 is the first and only slice that needs it, which is the same
consumer-before-producer test that assigned `enrollments` to S5 under
[§3.4](launch/SLICES.md#34-s5-introduces-the-enrollments-table-s6-owns-every-enrollment-write) and
`entitlements` to S4 under [§3.1](launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation).

Migration `0015_course_access_grant` therefore adds `courses.default_access_ends_at TIMESTAMPTZ`,
**nullable**, because BR-025 makes its absence a refusal condition rather than an invalid state, and a
`NOT NULL` column would require inventing a default duration that no approved rule supplies.

**BR-025's local-date conversion is part of this scope and had no task.** The rule states that when an
Admin enters a Kuwait-local calendar date, the platform persists the exclusive boundary as the first
instant of the following local day converted to UTC. That conversion is a stated rule, so implementing
it is not a new product decision — but it was uncovered by any S6 task and is now `T003a` and `T007a`.

**What this decision does not do.** It invents no duration, no default, and no policy. It does not
reopen S2, S4, or S5, and it changes no closed production behaviour. It assigns an owner to an
already-approved requirement that the slice boundaries left unassigned.

**Effort consequence, stated rather than absorbed:** S6 was sized at 9h Tier 3 on the assumption that
the Course expiry instant was inherited. It is not, so S6 now also carries a column, a validated Admin
write path with the local-date conversion, its audit evidence, and an Admin configuration screen. That
is a real increase against a 2026-08-15 date with 8 days remaining, and it is surfaced for the product
owner rather than quietly absorbed into the estimate.

**Source:** Explicit Product Owner instruction issued by Ahmed Hazem on 2026-08-07; S6 pre-implementation reconciliation on 2026-08-06 against the S5 closure head `d5ce557c67befacaef85fef2d1516e97fd57aee4`; BR-025; [SLICES.md](launch/SLICES.md) §2 rule 2.

## D-074 — Antigravity builds S6 Course Access Grant and Claude independently reviews

**Date:** 2026-08-07
**Status:** Active. Scoped to S6 Course Access Grant (`specs/006-course-access-grant/`).

**Decision:**
1. **Scope:** S6 Course Access Grant (`specs/006-course-access-grant/`).
2. **Seats:** Antigravity (`agy`) is assigned as the implementation builder; Claude is assigned as the independent read-only reviewer.
3. **Effective Date:** August 7, 2026.
4. **Authority:** Explicit Product Owner instruction issued by Ahmed Hazem on August 7, 2026.
5. **Review Protocol:** The builder (`agy`) cannot independently review or approve its own work. The independent reviewer (Claude) must review one exact frozen commit range from a clean disposable worktree.
6. **Subgroup Scope:** Approval applies only to the bounded subgroup reviewed and does not automatically authorize later S6 work.
7. **Independence & Rejection Persistence:** Seat assignments do not silently carry to another slice. The ranges rejected by Claude (`d9e483f..a5a2748` and `a5a2748..681f4a9`) remain rejected until a later independent approval explicitly accepts them.
8. **Duration:** This decision remains effective for S6 until replaced by another explicit Product Owner decision or until S6 closes.

**Reason:** Scopes seat authority explicitly to S6 under direct Product Owner authorization, enforcing strict independent review protocol and preventing seat carry-over.

**Source:** Explicit Product Owner instruction issued by Ahmed Hazem on 2026-08-07.
