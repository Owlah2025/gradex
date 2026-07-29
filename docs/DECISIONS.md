# Decision Log

> Status: Active
> Last Updated: 2026-07-28 (real calendar)

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
`b32e28957efc16bb09d46765b1e949aa3587088f` through this decision's own commit. Continues to pause
[D-033](#d-033--codex-resumes-building-and-claude-resumes-review)'s seat assignment; D-033's
frozen-range, disposable-worktree, and never-self-approve rules remain in force unchanged.

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
