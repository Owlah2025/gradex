# Decision Log

> Status: Active
> Last Updated: 2026-07-25

Central record of significant product/technical decisions for Gradex — what was decided, why, and what alternatives were rejected. This is the single source of truth for decisions; [PROJECT_VISION.md](PROJECT_VISION.md) §21 points here rather than keeping its own copy.

---

## D-001 — Own-build HLS video pipeline

**Date:** 2026-07-17
**Decision:** Build the video upload/transcode/playback pipeline in-house (Go backend + Redis job queue + FFmpeg workers + S3-compatible storage, adaptive-bitrate HLS, and short-lived authorized playback). A CDN remains a system-design/deployment decision, not a claim about the current repository.
**Reason:** Full control over the upload → transcode → playback flow and the auth/entitlement checks gating it; see [video-streaming-design.md](superpowers/specs/2026-07-17-video-streaming-design.md) for the full design.
**Alternatives rejected:** Not recorded — no vendor comparison was documented at the time this spec was written.

## D-002 — Tap Payments for MVP checkout; Deema BNPL is fast-follow

**Date:** 2026-07-20
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
**Status:** Capacity/commit timing amended on 2026-07-26 by D-028. The approved scope, ownership,
discount behavior, targets, and refund-release rule remain in force.
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
**Decision:** Admins can request one or more full/partial refunds up to the remaining captured balance. Partial success keeps entitlement active; cumulative successful refunds equal to the captured amount revoke it. State changes only after confirmed gateway success. Amount, reason, Admin, gateway reference, status, and history are audited. Refund-policy eligibility is configurable and must be approved by Kuwaiti counsel; the product will not assume that streaming automatically removes refund rights.
**Reason:** Tap supports amount-controlled refund requests but may reject partial refunds for some payment methods. Separating eligibility from processing lets system design proceed while legal interpretation remains a launch gate.
**Alternatives rejected:** Full-refund-only MVP; immediate access revocation on request; unverified “content accessed means no refund” language.
**Source:** Approved documentation reconciliation; Tap [refund API](https://developers.tap.company/reference/create-a-refund) and [response codes](https://developers.tap.company/reference/charge-response-codes).

## D-018 — Manual monthly payouts with system-recorded accounting

**Date:** 2026-07-23
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
**Decision:** Ordinary catalog delisting removes a Course from public discovery and new checkout but
does not deny existing entitled Students. Retirement blocks future acquisition/inclusion while
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
