# Decision Log

> Status: Active
> Last Updated: 2026-07-20

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
**Decision:** Course/chapter/bundle access expires roughly 4–5 months (one academic semester) after purchase, rather than lasting indefinitely. MVP ships silent expiry — access simply ends, no dedicated renewal flow — since a lapsed student can already regain access through the normal purchase flow (see [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-024/BR-025).
**Reason:** matches how the target student actually uses the product — access tied to the university course/semester they're taking right now — better than an open-ended lifetime default, and avoids building a separate renewal/repurchase flow before launch.
**Alternatives rejected:** lifetime access (the more common course-platform default, and the initially recommended option — rejected in favor of a semester-aligned term that better fits how Gulf university students actually consume this content); building a dedicated renewal flow in MVP (rejected — real added scope against the 3.5-week timeline; repurchase through the standard checkout covers this for now).
**Source:** This session; see [BUSINESS_RULES.md](BUSINESS_RULES.md) BR-025.
