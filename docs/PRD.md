# Product Requirements Document (PRD)

---

# 1. Introduction

## Product Name

Gradex

## Purpose

Gradex exists to give Gulf university students a course platform that treats them as the priority, not the revenue source — students get structured, high-quality video courses, hands-on labs, and real follow-up support instead of being left alone once they've paid. It also gives instructors a way to monetize their expertise and reach students at scale, at a fair price point.

---

# 2. Goals

## Business Goals

- Reach 50K–200K registered users and 100+ courses within 3 years, positioning Gradex among the top 3 GCC university course platforms
- Launch v1 with 8–12 courses and 100–500 paid students in the first 6 months
- Build sustainable revenue through course/chapter/bundle sales, plus future bootcamps, live sessions, and private mentorship

## User Goals

- Students: master their coursework, get real hands-on/lab experience, get follow-up support instead of being left alone after purchase, at a fair price
- Instructors: monetize their expertise, build a personal brand/reach, work in a supportive platform environment

---

# 3. Target Users

## Student

Gulf university students (primarily Kuwait) seeking to master their coursework and build industry-ready skills. Primary persona: Fahd, 19, Kuwait, Computer Science, wants A's in first-year courses, frustrated by overpriced platforms with no mentorship. Secondary: bootcamp/self-taught learners (e.g. Ali, wants a systematic CS curriculum) and high schoolers prepping for university (e.g. Amjad, prepping for the CS placement test).

## Instructor

Subject-matter experts who want to monetize their expertise, reach students at scale, and build a personal brand — currently underserved by low pay and no platform/reach.

## Administrator

Gradex team members responsible for course approval/moderation, user management, revenue/payment oversight, and instructor payouts.

---

# 4. Scope

## MVP (Launch — 2026-08-15)

- Course catalog browsing (courses, chapters)
- Purchase flow (single course or single chapter)
- Video upload, processing, and HLS playback (see video-streaming-design.md)
- Per-lesson progress tracking
- Downloadable lab materials (project files + guide)
- Instructor course/section/lesson builder (own courses only)
- Instructor analytics view (earnings/payouts are admin-managed, not instructor-facing in v1)
- Admin user management, course approval, revenue dashboard, instructor payouts, refunds, moderation

## V1 / Fast-Follow

- Bundle catalog browsing + purchase (pricing, checkout, cross-course entitlement)
- BNPL installment payments (Deema, if merchant-category approval clears in time — see §5 Payments and [DECISIONS.md](DECISIONS.md) D-008)

## Future Features

- Live mentorship / live sessions
- TAs / follow-up sessions (scales with student volume)
- Course completion certificates

---

# 5. Functional Requirements

## Authentication

- Email/password signup + login (JWT)
- Session/token refresh

## Student Features

- Browse catalog (courses, chapters) — bundle browsing is V1/Fast-Follow (see §4 Scope)
- Purchase single course or single chapter — bundle purchase is V1/Fast-Follow (see §4 Scope)
- View purchase history
- Video playback (HLS adaptive bitrate, resume progress) — per video-streaming-design.md
- Mark lesson complete / track progress per lesson
- Download lab materials (project files + guide) per lesson/course
- Link to course community (external Discord/Telegram)
- Manage profile

## Instructor Features

- Create/edit course → section → lesson structure
- Upload lesson video (raw upload, async transcode — per video-streaming-design.md)
- Upload lab materials (downloadable project files + guide)
- View per-course analytics (enrollments, completion rate)
- View student roster per course

## Admin Features

- Manage users (students, instructors) — view, suspend
- Approve/publish courses (moderation gate before catalog visibility)
- View platform-wide revenue/payment dashboard
- Manage and process instructor payouts (no instructor-facing earnings view in v1)
- Process refunds
- Moderate reported content

## Course Management

- Course → Section → Lesson hierarchy (per video-streaming-design.md)
- Video status lifecycle: DRAFT → UPLOADING → UPLOADED → QUEUED → PROCESSING → READY → PUBLISHED

## Payments

- **Primary gateway: Tap Payments**, using its Deema BNPL product for installments. Chosen over MyFatoorah and PayTabs (researched 2026-07-20): Deema has the cleanest risk-transfer (Tap pays Gradex upfront in full, Deema/Tap owns the 2–4 month collection risk), the amount fit is clean (10 KWD production minimum, no max — covers the full 30–60 KWD range, unlike Tamara's reported 50 KWD Kuwait floor), and Tap is Kuwait-founded/HQ'd with native KNET support.
- **Launch sequencing:** integrate Tap's core REST API (cards, KNET, Apple/Google Pay) first — this is needed regardless of the BNPL decision. Confirm directly with Tap's sales/integration team whether Deema accepts digital/non-shippable goods (online courses) as a merchant category — this is unconfirmed for ALL three researched gateways, not just Tap, and is a merchant-category approval question, not an engineering unknown. If Deema clears in time, it's an additive `src_deema` payment source on the same integration. If not, ship Aug 15 with card/KNET-only checkout and add installments as a fast-follow — do not let unconfirmed BNPL eligibility delay launch.
- **Fallback: MyFatoorah** (via Tamara) if Tap/Deema doesn't clear for digital goods or fails to activate in time — but only after confirming in writing that Tamara accepts digital/e-learning products and that Gradex's 30–49 KWD tier isn't excluded by Tamara's reported 50 KWD Kuwait minimum.
- **Ruled out: PayTabs** — its Kuwait "installment" offering is just a reseller layer over the same Deema product Tap offers directly, with no upside and added integration overhead.
- No official Go SDK exists for any of the three — plan to hand-roll a thin internal Go client wrapping the REST API + HMAC-SHA256 webhook verification (normal, bounded work, not a blocker).
- Refund handling (admin-initiated), consistent with the Kuwait Consumer Protection Law 14-day refund right (see Legal Constraints) and its digital-content-once-accessed exemption.

## Notifications

-

---

# 6. Non-Functional Requirements

## Performance

- **Page load:** catalog/course-detail pages target p95 < 2.5s Largest Contentful Paint on 4G Kuwait mobile connections (Google's Core Web Vitals "good" threshold is ≤2.5s LCP) — realistic given the student persona is primarily mobile-first, so this is measured on mobile, not desktop.
- **Video start ("time to first frame"):** p95 < 2–3s from lesson-open to first HLS segment playing, in line with industry norms for adaptive-bitrate streaming (Mux/Akamai benchmarks put "good" startup time at ≤2s, "acceptable" up to 4s); this is bounded by the signed-URL issuance step (auth + ownership check) plus CDN edge fetch already defined in the video-streaming-design.md spec — no new playback logic here, just the target this PRD holds that pipeline to.
- **Rebuffering:** target rebuffer ratio < 0.5% of total play time (industry "good" streaming benchmark is typically <0.5–1%), enabled by HLS adaptive bitrate ladder switching per the existing design spec — Gradex does not need to re-solve ABR logic, only monitor the ratio as a launch health metric.
- **API response time:** p95 < 300ms for read endpoints (catalog, progress, dashboards) and p95 < 800ms for write/transactional endpoints (purchase, payment webhook handling) on the Go/Gin + PostgreSQL/Redis stack — consistent with typical SLOs for CRUD-style REST APIs at this scale; payment-gateway-dependent calls (installment setup) are excluded since latency there is vendor-controlled.
- **Scaling to 3-year targets:** at launch (100–500 paid students, 8–12 courses) current single-region Postgres + Redis cache + CDN comfortably meets the above targets with room to spare; by 1 year (~1,500–3,000 paid students) expect read traffic (catalog browsing, progress polling) to dominate and require Redis cache-hit-rate monitoring plus read replicas if p95 read latency drifts upward; by 3 years (50K–200K registered users, multi-country GCC) expect CDN edge coverage and signed-URL issuance throughput to become the binding constraint before compute or storage does, since video delivery is CDN-offloaded but auth/entitlement checks per playback request still hit the origin API.
- **Out of scope for this section:** transcode throughput, upload reliability, and CDN cache-key/TTL configuration are performance-relevant but owned by video-streaming-design.md — this PRD only sets the student-facing playback SLA, not the pipeline that produces it.

## Security

- **Auth tokens**: Short-lived JWT access tokens (signed, e.g. RS256/HS256) + longer-lived rotating refresh tokens; refresh tokens stored server-side (revocable) and access tokens never persisted in localStorage on the frontend to limit XSS-driven token theft; all endpoints validate JWT signature/expiry/claims on every request.
- **Signed CDN URLs (purchaser-only access)**: Every HLS manifest/segment and lab-material download request is authorized against the student's purchase record before a short-TTL signed CDN URL is issued (per video-streaming-design.md); URLs are single-use/IP- or session-scoped where the CDN supports it, never long-lived or shareable, and re-issued per playback session rather than cached client-side long-term.
- **Payment data handling**: Gradex never collects, transmits, or stores raw card/PAN data — checkout is fully delegated to the PCI-DSS-compliant regional gateway (MyFatoorah/Tap/PayTabs) via hosted payment page/redirect or tokenized SDK; Gradex only persists gateway transaction/token references, payment status, and amounts, and validates all payment webhooks via signature verification to prevent spoofed "payment succeeded" callbacks.
- **Student PII protection**: Encrypt sensitive PII (national ID/civil ID if collected, phone, address) at rest; enforce TLS everywhere in transit; apply role-based access control so only authorized admin roles can view student PII; minimize PII collection to what's needed for enrollment/invoicing/GCC compliance.
- **Anti-piracy / anti-scraping for video and lab materials**: Rate-limit and monitor signed-URL issuance and download endpoints per user/IP to detect bulk-scraping or credential-sharing patterns; watermark or fingerprint downloadable lab materials (e.g. embedded student ID) to trace leaks back to source, acknowledging v1 has no DRM (per non-goals) so protection is deterrence-based, not encryption-based.
- **General platform hardening**: Enforce HTTPS/TLS across all services, hash passwords with a strong adaptive algorithm (bcrypt/argon2), rate-limit auth and purchase endpoints against brute-force/abuse, and apply standard input validation/parameterized queries against injection on the Go/Gin + PostgreSQL stack.

## Scalability

- **API tier**: Go/Gin services are stateless (JWT auth, no server-side session) and horizontally scalable behind a load balancer; the 3-year target of 15K–60K MAU / 50K–200K registered users is modest request-volume-wise for this stack, so scaling is primarily about adding Gin instances and connection-pool headroom, not architectural rework.
- **Video pipeline (dependency on video-streaming-design.md)**: transcode jobs are dispatched via a Redis-backed queue to a worker pool (ffmpeg); worker count scales horizontally and independently of the API tier based on queue depth, so instructor upload spikes (e.g. bulk course launches) don't degrade student-facing request latency. Playback itself scales via CDN-delivered signed HLS URLs, not app-server bandwidth — see that spec for the transcode/packaging/playback design, not restated here.
- **Redis**: used for job queues, session/rate-limit state, and hot-path caching (catalog listings, course/lesson metadata, signed-URL issuance checks); start single-instance, move to Redis with replica(s) once queue throughput or cache hit-rate volume requires it — no architectural change needed to introduce replicas later.
- **PostgreSQL**: single primary is sufficient through the 1-year target (~1.5K–3K paid students); plan for read replicas for catalog/analytics/reporting queries ahead of the 3-year target, keeping writes (purchases, progress events) on the primary. Add indexes on high-traffic access patterns (course/section/lesson lookups, per-user progress, enrollment checks) and revisit progress-event write volume (frequent per-lesson updates) as a candidate for batching/async writes if it becomes a hot path.
- **Storage/CDN**: video assets and lab material downloads are served from S3-compatible storage via CDN, so storage/bandwidth scaling is offloaded to the CDN/object-store provider and doesn't bottleneck the app tier as catalog size grows toward 100+ courses.
- **Payments**: installment/BNPL risk and processing scale is carried by the gateway (MyFatoorah/Tap/PayTabs), not Gradex infrastructure; Gradex only scales its own webhook/callback handling and payment-state persistence in Postgres as transaction volume grows.

## Reliability

- **Uptime target:** 99.5% monthly uptime for catalog browsing, purchase, and video playback (core revenue paths) during v1 — appropriate for an early-stage, single-region (Kuwait) platform at 100–500 paid students; re-evaluate toward 99.9% as usage scales toward the 1-year target (~1,500–3,000 paid students) and multi-country GCC presence (3-year horizon) increases the cost of downtime.
- **Video processing failures:** handled per the approved video-streaming design spec — transcode failures move a lesson to `FAILED`, auto-retry up to 3x with exponential backoff, then fall back to manual retry via the instructor dashboard; no additional reliability design needed here beyond depending on that spec's retry/backoff and stale-`UPLOADING` reaper behavior.
- **PostgreSQL backup strategy (source of truth for course, purchase, and progress data):** automated daily full backups + continuous WAL archiving for point-in-time recovery (target: restore to within 5 minutes of failure); backups retained 30 days, stored off-provider (e.g. separate region/account from primary DB) to survive a single-provider incident; monthly restore-drill to a staging environment to verify backups are actually restorable, not just taken.
- **Payment/installment reliability:** the regional gateway (MyFatoorah/Tap/PayTabs, vendor TBD) is the system of record for installment/BNPL risk and scheduling, so Gradex's reliability obligation is narrower — reconcile via idempotent webhook handling (safe to receive duplicate/out-of-order events) plus a scheduled polling job as fallback if a webhook is missed; on gateway timeout/5xx during checkout, fail safe (no course access granted, no double-charge) and surface a retryable error to the student rather than a silent failure.
- **Payment data consistency:** purchase/entitlement state changes (grant course access, mark installment paid) are written transactionally in Postgres keyed by the gateway's idempotency/transaction ID, so a retried webhook or duplicate client request cannot double-grant access or double-record a payment.
- **Graceful degradation:** Redis (sessions/caching) outage degrades performance but must not block core purchase or playback flows — no hard dependency on Redis for correctness, only for speed; CDN/storage outage on the playback path returns a distinguishable error (per video-streaming spec) rather than a generic failure.

## Accessibility

- Target WCAG 2.1 AA as the baseline standard across all student- and instructor-facing web flows (catalog, course player, checkout, dashboards), audited before v1 launch and re-checked on major UI changes.
- Full keyboard navigation and visible focus states for all interactive elements — catalog browsing/filtering, cart/checkout, video player controls (play/pause, seek, volume, quality, fullscreen), and instructor course builder — with no keyboard traps.
- Screen-reader support (semantic HTML, ARIA landmarks/labels, alt text) for the core conversion path: catalog browsing, course/chapter/bundle detail pages, and the purchase/installment checkout flow, so a screen-reader user can find, evaluate, and buy a course end-to-end.
- Minimum 4.5:1 color contrast for body text and 3:1 for large text/UI components across the Tailwind theme (including price/discount badges and payment-status indicators), verified against both light backgrounds and any dark-mode palette.
- Video player accessibility is scoped to controls only (keyboard-operable, screen-reader-labelled, per video-streaming-design.md); subtitles/closed captions and transcripts are explicitly out of scope for v1 — do not block launch on caption support.
- Accessible form/error handling for account creation, login, and payment forms (labeled inputs, inline error messages announced to assistive tech, sufficient touch-target size on mobile), given payment flows run through a third-party gateway iframe/redirect outside Gradex's direct control.

---

# 7. Constraints

## Budget

- Bootstrapped / self-funded.

## Team

- 3 people: solo developer (you) building the full platform end-to-end, Tohamy (founder, logistics), Mokhtar (marketing/social media/advertising).
- No dedicated backend/frontend split, no QA, no design hire — one developer covering the entire stack (auth, payments, video pipeline, course builder, admin tooling).

## Timeline

- Target launch: 2026-08-15 (~3.5 weeks from today, 2026-07-20). See flagged risk below — this is tight against current v1 scope.

## Technology Constraints

- Locked stack (per README.md): Next.js/React/TypeScript/Tailwind frontend, Go/Gin backend, PostgreSQL, Redis, S3-compatible storage, JWT auth.

## Legal Constraints

Researched 2026-07-20 (preliminary scan, not legal advice — confirm with a Kuwaiti lawyer before launch):

- **⚠️ Commercial Registration (CR) is a launch-critical dependency.** To accept KNET or open a merchant gateway account with any Kuwait-facing payment provider, Gradex needs a valid CR from the Ministry of Commerce and Industry (MOCI) plus a business bank account at a KNET member bank. Gateway activation typically takes 7–15 business days once CR, bank account, and site HTTPS/SSL are in place. **Against the 2026-08-15 launch date, this should be started immediately if not already underway** — it's on the critical path before any payment integration can go live, independent of which gateway is chosen.
- **⚠️ New Digital Commerce Law (Decree-Law No. 10 of 2026, gazetted 2026-03-01).** Requires anyone conducting commercial activity via electronic means to register with MOCI as a digital-commerce provider before operating. Implementing regulations are due within a year of gazette publication and the law only takes effect a month after those are issued — the exact operative date/registration mechanism was not yet finalized as of this research. Track this and register once the mechanism is live.
- **Consumer Protection Law No. 39/2014 + Executive Regulations:** general 14-day cooling-off/refund right, but the new Digital Commerce Law exempts "downloaded software" and digital content generally once accessed — a "no refund once a lesson has been streamed or a file opened" policy is likely defensible, but confirm exact wording against the final legal text before relying on it.
- **BNPL/installment licensing (CBK):** the licensing obligation falls on the payment/BNPL provider (regulated under CBK Resolution No. 45/471/2023), not on a merchant that simply integrates a CBK-licensed gateway — consistent with Gradex's plan to use Tap/Deema rather than build its own installment logic. Decree-Law 10/2026 also requires e-commerce platforms to restrict payment processing to CBK-licensed providers only, reinforcing this.
- **Data protection:** no general data-localization/residency mandate found — hosting S3-compatible storage outside Kuwait appears legally permissible. Kuwait's Personal Data Protection Law No. 26/2024 (distinct from CITRA's narrower DPPR, which only applies to CITRA-licensed telecom/IT entities) is the broader law likely governing student PII; get explicit consent/privacy-policy disclosure for any cross-border data transfer. Breach-notification window is unconfirmed (sources conflict: 24h vs 72h).
- **Education-content licensing:** unclear — no primary source found on whether a non-degree, supplementary course platform (Gradex's model, same as Baims) needs an education-sector-specific license from the Ministry of Higher Education / Private Universities Council, versus just ordinary MOCI commercial + digital-commerce registration. Baims appears to operate as a standard commercial entity with no visible education-specific license. Treat as open — do not market content as "accredited" or "certified" without confirming this first.

---

# 8. Assumptions

- **Payment gateway installment/BNPL suitability** — assumes MyFatoorah/Tap/PayTabs (vendor TBD) can support installments for one-time digital purchases (30–60 KWD) in Kuwait without Gradex building its own risk/underwriting logic. Unvalidated: no vendor confirmed for consumer BNPL on this ticket size/product type yet.
- **Discord/Telegram as a retention/mentorship substitute** — assumes an unmoderated, unpaid, off-platform community is enough to deliver on the "no follow-up" differentiator. Unvalidated: no owner/process defined for who staffs it in v1.
- **Linear/affordable storage + CDN egress costs at scale** — assumes GB-per-course-hour and GCC CDN egress costs stay affordable at 30–60 KWD price points as the catalog and rewatch volume grow. Unvalidated: no real cost data yet (pre-launch).
- **Downloadable (non-sandboxed) labs are an acceptable proxy for "hands-on experience"** — assumes students can self-serve environment setup without support. Unvalidated: environment-setup friction is a common drop-off point for this format.
- **Admin-only instructor payouts (no dashboard) is acceptable to instructors** — assumes instructors will trust a black-box payout process. Unvalidated: no revenue-share percentage or payout cadence defined yet.
- **Kuwait/GCC compliance overhead** (refund law, VAT, instructor-payout tax withholding) is handleable ad hoc by admin ops rather than built into the payment/refund flow. Unvalidated: no refund window/eligibility policy defined yet.
- **8–12 launch courses in 6 months** assumes instructor supply can be recruited and onboarded in parallel with building the platform from scratch. Unvalidated: no instructor pipeline/commitments exist yet, and course production usually has a longer lead time than platform engineering.

---

# 9. Risks

**1. Third-party payment gateway dependency (installments/BNPL)**
- Impact: outage, pricing/API change, or a bad vendor relationship breaks Gradex's only revenue path platform-wide with no fallback; spoofed/delayed webhooks can also desync purchase records from actual payment state. Specifically: Deema's eligibility for digital/non-shippable goods (online courses) is unconfirmed by Tap or any researched vendor — if it's rejected at merchant-category underwriting, the "price && installments" USP has no v1 mechanism.
- Mitigation: build the payment integration behind an internal gateway-agnostic adapter (not scattered SDK calls), verify webhook signatures, make purchase-status transitions idempotent, add a reconciliation job, and weigh vendor choice on installment reliability + Kuwait support quality, not just fees. Get the digital-goods eligibility question to Tap's sales team in writing this week — it's a fast, binding answer, not an engineering unknown — and be ready to launch card/KNET-only if it doesn't clear in time (see §5 Payments).

**2. External Discord/Telegram community has no in-platform control**
- Impact: Gradex can't measure engagement, moderate content, or guarantee the community stays alive — undermining the exact USP (community/follow-up) meant to beat Baims on something other than price.
- Mitigation: treat the Discord/Telegram server as a real product surface with an assigned moderator and activity expectations, track engagement manually until it's worth building in-platform, and treat "external community" as a reviewed decision, not a default.

**3. Downloadable lab materials have no piracy protection**
- Impact: one paying student can redistribute lab files freely with no traceability, cannibalizing sales of Gradex's own stated differentiator (hands-on labs) — the thing meant to justify pricing above Baims.
- Mitigation: embed a per-purchase identifier into guide PDFs/project files, rate-limit and log download access per user, and enforce redistribution as a ToS violation with account-suspension consequences.

**4. Direct price competition against Baims**
- Impact: Gradex enters pre-launch with zero brand trust against a funded incumbent (450K+ enrollments) pricing at ~4 KWD/course vs Gradex's 30–60 KWD — if labs/mentorship/community don't land convincingly at first contact, students default to the cheaper, already-trusted option.
- Mitigation: don't compete on price messaging — make the price gap legible (graded labs, active community, follow-up) explicit on course pages/marketing, and validate willingness-to-pay at 30–60 KWD with real Kuwait students before the first 8–12 courses launch.

**5. Video transcoding pipeline untested against launch-week upload spikes**
- Impact: onboarding 8–12 courses at once for launch could exceed worker capacity and delay course availability right when reputational damage from a slipped launch is highest, with no alerting yet built to catch it.
- Mitigation: load-test the queue/worker pool against a simulated 8–12 course concurrent-upload scenario before the first real batch, and implement at least minimal alerting (queue depth, failed job count) ahead of launch.

**6. Instructor payout model has no instructor-facing dashboard**
- Impact: instructors — the pre-launch supply-side bottleneck for hitting the 8–12 course target — get no visibility into what they're owed or when they'll be paid, making recruitment/retention harder pre-launch when trust is most fragile.
- Mitigation: commit to a fixed, transparent, documented payout cadence/formula and give instructors a simple manual statement (emailed PDF/CSV) each cycle; prioritize a real earnings dashboard early in the post-v1 roadmap.

**7. Solo-developer timeline risk (AI-assisted)**
- Impact: v1 scope (JWT auth, purchase/installment payment integration, full video upload/transcode/HLS pipeline, course builder, progress tracking, admin approval/payout/refund dashboards) targeted for 2026-08-15 (~3.5 weeks from today) is being built by one developer using AI-assisted coding tools (Claude Code, antigravity-cli) rather than a traditional multi-engineer team. This meaningfully speeds up code-writing, but doesn't compress everything equally — third-party payment gateway integration/approval, video infra deployment and load-testing, and end-to-end testing/QA still take real wall-clock time regardless of how fast code gets generated. Risk is smaller than a fully unaided solo build, but not zero.
- Mitigation: track scope against the date week-by-week rather than assuming AI-assisted velocity closes the whole gap; prioritize getting the third-party-dependent pieces (payment gateway account/approval, CDN/storage provisioning) started immediately since those have external lead times AI assistance can't shorten, and keep the "cut scope vs. move date" options from this PRD on the table if week 2 shows those external dependencies slipping.
- **Resolved 2026-07-20:** scope-vs-date decision made — instructor portal stays fully in MVP; bundles and BNPL installments move to V1/Fast-Follow to reduce build risk without cutting the instructor differentiator. See [DECISIONS.md](DECISIONS.md) D-008.

---

# 10. Success Metrics

See [PROJECT_VISION.md](PROJECT_VISION.md) §11 for Business Metrics and Product Metrics targets (6-month/1-year/3-year) — inherited here rather than duplicated, to avoid drift between the two docs.

---

# 11. Acceptance Criteria

Each item below implements one or more rules from [BUSINESS_RULES.md](BUSINESS_RULES.md) — tagged inline so the two documents don't silently drift apart.

## Authentication

- Given a prospective student submits a signup form with a unique email and a password meeting complexity requirements, when the backend validates and persists the account, then Gradex creates the user record with a securely hashed password, issues a short-lived JWT access token plus a refresh token, and returns 201 with the tokens (or sets the refresh token as an HttpOnly secure cookie) without ever echoing the plaintext or hashed password. *(Implements BR-002, BR-004)*
- Given a registered student submits the correct email and password on login, when the credentials are verified against the stored hash, then Gradex returns a new JWT access token (short expiry, e.g. 15 min) and a new/rotated refresh token, and records the session in Redis so it can be revoked independently of token expiry. *(Implements BR-004)*
- Given a logged-in student's JWT access token has expired but their refresh token is still valid and unrevoked, when the client calls the token refresh endpoint, then Gradex issues a new access token (and rotates the refresh token) without requiring re-entry of credentials, and rejects the request with 401 if the refresh token is expired, revoked, or reused after rotation. *(Implements BR-005)*
- Given a signup or login request uses an email that is already registered, or invalid credentials, when the backend processes the request, then Gradex returns a clear 4xx error (409 for duplicate email, 401 for bad credentials) without revealing whether the email exists on the login path, and without issuing any token. *(Implements BR-001, BR-003)*
- Given a student explicitly logs out or an admin revokes a session, when the refresh token's session entry is invalidated in Redis, then subsequent refresh or protected-API calls using that token are rejected with 401. *(Implements BR-006)*

## Purchase & Payment

- Given a logged-in student viewing a published course, chapter, or bundle with a price, when they select an installment plan and complete the first payment via the GCC gateway's hosted checkout, then Gradex grants access only after receiving the gateway's payment-success callback/webhook (not on client-side redirect), and creates an enrollment record tied to that specific course/chapter/bundle scope. *(Implements BR-020, BR-021)*
- Given a student mid-checkout for a paid item, when the gateway reports a declined, cancelled, or timed-out payment (including installment setup failure), then no enrollment or access is granted, the order is marked failed, and the student is shown a clear retry path without being double-charged on resubmission (idempotent order reference). *(Implements BR-022)*
- Given a student who purchased via an installment plan and an upcoming installment is due, when the gateway attempts and fails to collect that installment per its own retry/dunning policy, then Gradex reflects the gateway's reported status change (e.g. past-due or plan-cancelled) by restricting access to the paid content per the agreed access policy, without Gradex re-implementing retry/risk logic itself. *(Implements BR-032)*
- Given an enrolled student who purchased a course, chapter, or bundle, when they open a lesson within their purchased scope, then the platform verifies entitlement against the enrollment record — including that the enrollment's term hasn't expired — before issuing a signed HLS playback URL and before allowing download of that lesson's lab materials, and denies access to lessons outside the purchased scope or past the enrollment's expiry. *(Implements BR-023, BR-025)*
- Given an admin viewing the revenue/payment dashboard, when a completed purchase (single payment or any installment) is refunded through Gradex's refund workflow, then Gradex calls the gateway's refund API, updates the order/enrollment status to reflect revoked or partial access per policy, and logs the refund event for audit without requiring manual reconciliation of gateway records. *(Implements BR-040, BR-041, BR-042)*

## Video Playback & Progress

- Given a student who has purchased the course containing a PUBLISHED lesson, when they request that lesson's video, then the backend returns a signed CDN playback URL (short expiry) for the HLS master manifest, and the CDN serves subsequent segment requests only while that signature remains valid. *(Implements BR-050)*
- Given a student who has NOT purchased/enrolled in the course (or an unauthenticated user), when they request a lesson's playback URL directly or via API, then the backend returns 403 without issuing a signed URL, regardless of whether the lesson's video file exists in storage. *(Implements BR-050)*
- Given a student reopening a lesson they previously watched partway through, when the player loads the lesson, then the backend returns `last_position_seconds` alongside the playback URL and the player resumes from that position rather than starting at 0:00. *(Implements BR-052)*
- Given a student actively watching a lesson, when the player posts progress (~every 10s) with `position_seconds`, then the backend updates `max_position_seconds = max(existing, new)` and `last_position_seconds = new`, and marks `completed = true` (once, permanently) once `max_position_seconds` reaches ≥90% of the lesson duration, with completion never regressing on seek-back. *(Implements BR-051)*
- Given a signed playback URL expires mid-session or a progress POST fails, when the frontend receives a 403 on a segment/manifest request or a progress-write error, then the player transparently refreshes the playback token and retries once (403 case) or silently retries progress on the next tick (progress-write case) without interrupting playback or surfacing an error to the student. *(Implements BR-053)*

## Instructor Course Builder

- Given an authenticated instructor creating a new course, when they save a course with a title, description, and at least one section containing one lesson, then the course is created in draft status and remains invisible to students until it passes the admin approval/publish gate. *(Implements BR-011)*
- Given an instructor building a course outline, when they add, reorder, or delete sections and lessons within a course, then the Course-Section-Lesson hierarchy and ordering persist correctly and are reflected immediately in the builder UI. *(Implements BR-010)*
- Given an instructor on a lesson's video tab, when they upload a video file for that lesson, then the file is handed off to the existing upload/transcode/HLS pipeline and the lesson shows a processing status until the transcoded asset is ready, without the builder re-implementing transcoding logic. *(Implements BR-062)*
- Given an instructor on a lesson's lab materials tab, when they upload one or more downloadable files (project files, PDF guide) within the platform's size/type limits, then the files are stored in S3-compatible storage and listed on the lesson as downloadable resources accessible to enrolled students via signed URLs, with no code execution or sandboxing involved. *(Implements BR-063)*
- Given an instructor attempts to submit a course for admin review, when any lesson is missing its required video or the course has zero sections/lessons, then submission is blocked and the instructor sees a validation message identifying the missing content before the course can enter the approval queue. *(Implements BR-012)*

## Admin Moderation & Payouts

- Given a course has at least one published section with at least one lesson containing a successfully transcoded video, when the instructor submits the course for review, then the course status changes to "Pending Approval" and becomes visible in the admin approval queue but remains hidden from the student catalog. *(Implements BR-013, BR-070)*
- Given a course is in "Pending Approval" status, when an admin reviews it and clicks "Approve & Publish," then the course status changes to "Published," it becomes visible in the student catalog, and the instructor receives a notification of approval. *(Implements BR-071)*
- Given a course is in "Pending Approval" status, when an admin rejects it with a required rejection reason/comment, then the course status reverts to "Draft," the course stays hidden from the catalog, and the instructor sees the reason to revise and resubmit. *(Implements BR-072)*
- Given a student's purchase (single payment or installment/BNPL) has been confirmed as collected by the payment gateway and reconciled in Gradex, when an admin opens the payouts screen for an instructor, then the admin sees that revenue itemized by course/purchase with gateway fees and refunds already deducted, and can mark it "Payout Approved" and later "Paid" with a reference/transaction note, without any earnings figures being exposed on the instructor-facing UI. *(Implements BR-073, BR-074)*
- Given a refund is processed by an admin for a student purchase, when the associated instructor payout for that purchase has not yet been marked "Paid," then the refunded amount is automatically excluded from that instructor's payable balance; if the payout was already marked "Paid," then the system flags the payout record for manual admin adjustment/clawback in a future payout cycle. *(Implements BR-043)*

---

# 12. Open Questions

- Persona 2 (second primary GCC-university-student persona) — intentionally left blank, revisit later
- Competitor table: Baims weaknesses / why-Gradex-is-better — not yet answered
- Payment gateway: Tap Payments (Deema) recommended 2026-07-20, MyFatoorah fallback — but Deema's digital-goods eligibility is unconfirmed, needs direct written confirmation from Tap before committing checkout architecture to it
- Kuwait Commercial Registration status — is one already in place, or does this need starting immediately given the 7–15 day KNET activation lead time against the 2026-08-15 launch date?
- Education-content licensing — does Gradex's non-degree supplementary-course model need any MOHE/Private Universities Council license, or does ordinary MOCI commercial registration suffice? (unconfirmed, consult a lawyer)
- Digital Commerce Law (Decree-Law 10/2026) registration mechanism — not yet live as of research date, track for when it opens
- ~~v1 scope vs. 2026-08-15 launch date~~ — resolved 2026-07-20, see [DECISIONS.md](DECISIONS.md) D-008
