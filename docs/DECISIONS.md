# Decision Log

> Status: Active
> Last Updated: 2026-07-22

Central record of significant product/technical decisions for Gradex — what was decided, why, and what alternatives were rejected. This is the single source of truth for decisions; [PROJECT_VISION.md](PROJECT_VISION.md) §21 points here rather than keeping its own copy.

---

## D-001 — Own-build HLS video pipeline

**Date:** 2026-07-17
**Decision:** Build the video upload/transcode/playback pipeline in-house (Go backend + Redis job queue + FFmpeg workers + S3-compatible storage + CDN, adaptive-bitrate HLS, signed URLs).
**Reason:** Full control over the upload → transcode → playback flow and the auth/entitlement checks gating it; see [video-streaming-design.md](superpowers/specs/2026-07-17-video-streaming-design.md) for the full design.
**Alternatives rejected:** Not recorded — no vendor comparison was documented at the time this spec was written.

## D-002 — Payment gateway: Tap Payments (Deema BNPL), MyFatoorah fallback

**Date:** 2026-07-20
**Decision:** Primary gateway is Tap Payments, using its Deema product for installments. MyFatoorah (via Tamara) is the fallback if Tap/Deema doesn't clear for digital goods in time.
**Reason:** Deema has the cleanest risk-transfer (Tap pays Gradex upfront, Deema/Tap owns collection risk), the amount fit is clean (10 KWD minimum, no max — covers the full 30–60 KWD range), and Tap is Kuwait-founded/HQ'd with native KNET support.
**Alternatives rejected:** PayTabs — its Kuwait "installment" offering is a reseller layer over the same Deema product Tap offers directly, with no upside and added integration overhead.
**Source:** [PRD.md §5 Payments](PRD.md)

## D-003 — GritCMS MediaKit rejected as a video-infra vendor

**Date:** 2026-07-20
**Decision:** Do not use MediaKit as a replacement for the own-build video pipeline (D-001).
**Reason:** A 21-agent workflow reviewed all 16 MediaKit doc pages and scored it a plausible fit worth a spike — but the hands-on spike died in ~30 minutes: the documented API base URL 404s, the official scaffolder produces no MediaKit-specific routes, and the only real artifact found was an orphaned frontend-only npm package with nothing to talk to. The docs read as completely genuine; only running the actual install command surfaced that the backend doesn't exist.
**Alternatives rejected:** N/A — MediaKit itself was the alternative being evaluated against D-001, and it was rejected.
**Source:** [gradex-video-vendor-eval memory](../../.claude/projects/-home-owlah-gradex/memory/gradex-video-vendor-eval.md); full history in [2026-07-20-mediakit-spike-plan.md](superpowers/specs/2026-07-20-mediakit-spike-plan.md)

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
**Decision:** No instructor-facing earnings/payout dashboard in v1; admin views and processes all payouts, instructors receive a manual statement.
**Reason:** Keep v1 lean — avoid building a self-service earnings dashboard before the platform has real revenue to show.
**Alternatives rejected:** Self-service instructor earnings dashboard (deferred to a future version, not rejected outright).
**Source:** [PRD.md §4 Scope](PRD.md), [PRD.md §9 Risk 6](PRD.md)

## D-007 — Course completion certificates deferred

**Date:** 2026-07-20
**Decision:** Course completion certificates are not part of v1.
**Reason:** Keep v1 lean pre-launch.
**Alternatives rejected:** N/A — straightforward deferral.
**Source:** [PRD.md §4 Scope](PRD.md), [PROJECT_VISION.md §9 Non-Goals](PROJECT_VISION.md)

## D-008 — MVP keeps the full instructor portal; bundles and BNPL installments move to V1/fast-follow

**Date:** 2026-07-20
**Decision:** The instructor portal (auth, own-course CRUD, section/lesson management, video/lab upload, submit-for-review, view submission status) stays fully in MVP. Bundle purchase (pricing + checkout + entitlement) and BNPL installments (Deema) move to V1/fast-follow, shipped after launch rather than blocking it.
**Reason:** Cut real build-time risk against the solo-developer, ~3.5-week timeline to the 2026-08-15 launch date ([PRD.md §9 Risk 7](PRD.md)) — without touching the instructor supply-side differentiator, which is core to the business model, not optional. Bundles and installments add real purchase/entitlement/checkout branching complexity that the 8–12 launch courses don't strictly need on day one.
**Alternatives rejected:** Admin-only course creation for launch (rejected — instructor self-service isn't optional, it's core to the business model); dropping installments entirely rather than keeping them conditional (rejected — installment risk/collection is gateway-carried at near-zero Gradex-side engineering cost, so there's no reason to foreclose it for V1).
**Source:** This session; see [PRD.md §4 Scope](PRD.md) and [PRD.md §12 Open Questions](PRD.md).

## D-009 — Enrollment access is per-semester, not lifetime

**Date:** 2026-07-20
**Decision:** Course/chapter/bundle access expires 150 days (~5 months, approximating one academic semester) after the purchase timestamp, calculated in Kuwait local time (UTC+3), valid through the end of day 150, rather than lasting indefinitely. MVP ships silent expiry — access simply ends, no dedicated renewal flow — since a lapsed student can already regain access through the normal purchase flow (see [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-024/BR-025). *(Exact day count/timezone/boundary made concrete 2026-07-20, operationalizing the originally-stated "4–5 months" range so the rule is enforceable in code — revisit if a different exact term length is wanted.)*
**Reason:** matches how the target student actually uses the product — access tied to the university course/semester they're taking right now — better than an open-ended lifetime default, and avoids building a separate renewal/repurchase flow before launch.
**Alternatives rejected:** lifetime access (the more common course-platform default, and the initially recommended option — rejected in favor of a semester-aligned term that better fits how Gulf university students actually consume this content); building a dedicated renewal flow in MVP (rejected — real added scope against the 3.5-week timeline; repurchase through the standard checkout covers this for now).
**Source:** This session; see [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-025.

## D-010 — Notifications: transactional-only MVP on email + in-app center; lifecycle/marketing deferred

**Date:** 2026-07-21
**Decision:** MVP ships transactional notifications only, delivered on two channels — email + a minimal in-app notification center (unread badge + list). Transactional set: instructor course approval/rejection, student purchase receipt, password reset. Lifecycle notifications (registered-no-purchase re-engagement, enrollment-expiry reminders, course-not-started nudge, abandoned checkout), marketing/broadcast, and additional channels (WhatsApp/SMS, push) are deferred to post-launch.
**Reason:** The three transactional events are already implied by existing MVP flows (BR-071/BR-072 approval, BR-020 enrollment grant, auth), so they add near-zero product scope. Email is day-one cheap; the in-app center is a small bounded build (a `notifications` table + list/unread-count endpoints + a frontend badge) and gives a durable per-user record independent of email deliverability. A full lifecycle/marketing engine (scheduler, segmentation, consent/unsubscribe management) is real scope against the solo-developer ~3.5-week timeline and carries PDPL consent obligations — same cut-to-protect-the-date logic as D-008.
**Alternatives rejected:** Email-only MVP (rejected — no durable in-app record, and course-approval/student updates read better in-app); full lifecycle/marketing engine in MVP (rejected — scheduler + segmentation + consent management is post-launch scope, and marketing to non-purchasers requires opt-in under Kuwait PDPL No. 26/2024); WhatsApp/SMS at launch (rejected — paid per message plus WhatsApp Business API approval overhead; deferred).
**Source:** This session; see [PRD.md §5 Notifications](PRD.md), [PRD.md §4 Scope](PRD.md), and [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-120–BR-123.

## D-011 — Lesson resources split from lab materials; both MVP, labs-only watermark

**Date:** 2026-07-21
**Decision:** Split per-lesson downloadable attachments into two distinct categories: **lesson resources** (supplementary reference to consume — slides, notes, readings; allowed types PDF, slides (PPT/PPTX), images) and **lab materials** (hands-on practice — project files + a written guide; allowed types archives (ZIP), common project files, plus a PDF/Markdown guide). Both ship in MVP, share the same upload/storage/signed-URL/entitlement plumbing, and are optional per lesson. Upload size caps: lesson resources 50 MB per file / 200 MB per lesson; lab materials 250 MB per file / 1 GB per lesson (set 2026-07-21; tunable in implementation, distinct from video's own cap). The per-purchase watermark/buyer-tag (BR-103) applies to lab materials only; lesson resources are entitlement-gated and rate-limited but not watermarked.
**Reason:** Slides/notes and hands-on lab files differ in purpose (consume vs. do) and value (labs are the paid differentiator most worth pirating). A single "lab materials" bucket conflated them and left non-lab PDFs like lecture slides with no clean home. Splitting is near-zero added infrastructure — the same download pipeline with a category flag — while giving each bucket its own allowed-type list and anti-piracy posture. Watermarking labs only keeps the anti-piracy effort on the high-value target; slide/image formats carry per-buyer tags poorly and are lower-stakes.
**Alternatives rejected:** Single "lab materials" bucket for everything (rejected — conflates reference material with hands-on labs, no home for slides/notes); deferring lesson resources to fast-follow (rejected — same plumbing as labs, near-zero marginal cost to include at launch); watermarking both buckets (rejected — slide/image formats don't carry per-buyer tags cleanly, and resources are lower-value than labs).
**Source:** This session; see [PRD.md §4 Scope](PRD.md), [PRD.md §11 Instructor Course Builder](PRD.md), and [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-063, BR-066, BR-067, BR-103, BR-115.

## D-012 — Coupons in MVP: admin-only discount codes applied pre-gateway

**Date:** 2026-07-22
**Decision:** Add an admin-managed coupon system to MVP. Admins (only) mint discount codes — percentage or fixed amount (integer fils) — optionally scoped to specific course(s)/chapter(s) or platform-wide. A code is validated and applied server-side *before* the Tap payment session is created; a code that reduces the order to 0 KWD grants enrollment directly with no gateway call (free-access path for beta testers/influencers). Redemption count commits on payment success / free-grant (soft global cap, exact per-user); one coupon per order, no stacking. Coupons never modify a course's listed price — the discount is per-order only.
**Reason:** Launch promos and seeding free access are standard go-to-market levers, and the insertion point is clean — Gradex already computes the order amount before delegating checkout, so the discount slots in ahead of the gateway with no change to Tap integration. Admin-only keeps it aligned with who controls pricing/revenue today (BR-064) and avoids a BR-017 price-change side door. Full design in [coupons-system-design.md](superpowers/specs/2026-07-22-coupons-system-design.md).
**Alternatives rejected:** Instructor-created coupons (deferred to V1 — collides with BR-017's no-silent-price-change guard, needs its own guardrails); disallowing free (100%) codes (rejected — free-seeding is a real launch need and the direct-grant path reuses existing enrollment/idempotency machinery); reserving redemptions at checkout with an expiry job (rejected — a background reservation-expiry job is real scope against the timeline for a global cap that is a marketing nicety, not a money-loss risk).
**Source:** This session; see [PRD.md §4 Scope](PRD.md), [PRD.md §5 Payments](PRD.md), and [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-124–BR-133.

## D-013 — Live office hours in MVP (external-link only) — reverses the live-sessions deferral

**Date:** 2026-07-22
**Decision:** Add lightweight live office hours to MVP. Instructors (own PUBLISHED courses) and admins (any course, or platform-wide) schedule one-off sessions; Gradex owns scheduling, access control, and an event-driven "new session" notice, while the live audio/video happens on an external third party (Zoom / Google Meet / Discord voice) reached via a stored join link. Course-scoped access reuses playback entitlement (BR-023/BR-025); platform-wide sessions carry an admin per-session audience toggle. No RSVP, no recurrence, no timed reminders, no in-platform video. **This explicitly reverses the earlier deferral** of "live mentorship / live sessions" out of MVP (still recorded in the PRD Future list) — but only for this lightweight external-link form; Gradex-hosted live streaming, recurring series, RSVP/capacity, and timed reminders remain future.
**Reason:** Live office hours directly serve the core "no student left alone after they pay" differentiator, and the external-link form is days of work with zero streaming infrastructure and no new compliance — so it can ship inside the timeline without reopening the scope that was cut. Keeping the video off-platform is what makes the reversal safe. Full design in [live-office-hours-design.md](superpowers/specs/2026-07-22-live-office-hours-design.md).
**Alternatives rejected:** In-platform live video (WebRTC/SFU or reusing the VOD HLS pipeline) — rejected as multi-month scope, unrealistic for a solo dev against 2026-08-15, and duplicating infrastructure video-streaming-design.md deliberately scoped to pre-recorded HLS only; RSVP/capacity + recurring series + timed reminders — deferred to V1 (the timed reminder specifically needs the scheduler D-010 deferred).
**Source:** This session; see [PRD.md §4 Scope](PRD.md) and [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-134–BR-141.
