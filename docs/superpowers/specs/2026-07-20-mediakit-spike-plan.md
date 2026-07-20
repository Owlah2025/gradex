# GritCMS MediaKit — Fit Evaluation & Spike Plan

> Status: **REJECTED — spike failed at install verification (2026-07-20). Do not adopt. Proceed with the fallback in §6.**
> Date: 2026-07-20
> Scope: build-vs-buy decision for the video upload/transcode/delivery slice of [2026-07-17-video-streaming-design.md](./2026-07-17-video-streaming-design.md). Does not replace that spec — it's a gate in front of it.

---

## 0. Spike result (2026-07-20, ~30 min, not 2 days)

The spike never reached Docker. Step one of MediaKit's own Quick Start — `go install github.com/MUKE-coder/grit/v3/cmd/grit@latest` then `grit new my-media --triple --next --style default` — was run exactly as documented. Findings:

1. **`grit`'s own README never mentions MediaKit.** It's a generic Go+Gin+GORM / Next.js full-stack scaffolder, unrelated on its face.
2. **The scaffolded project contains zero MediaKit functionality.** No transcode, no HLS, no private-assets/signed-URL endpoint, no webhooks, no analytics, no AI-analysis route — confirmed by grepping the generated Go source. The only upload/signed-URL code present is Grit's generic, generic-purpose S3 presigned-upload plumbing (any Grit app gets this for arbitrary file storage; it has nothing to do with video or purchaser-gating).
3. **`grit plugin list` shows exactly one plugin: `multitenant`.** No media/video plugin exists to install on top of the base scaffold either.
4. **`https://mediakitapi.gritcms.com`** — the base URL every single curl example across all 16 doc pages targets — **returns 404.** There is no live backend, hosted or otherwise.
5. **`github.com/MUKE-coder/mediakit`** (linked from the docs' own LLM-reference page) — 404, confirmed earlier in the doc-only eval.
6. The only real artifact found anywhere: an npm package `@mediakit-dev/react` (versions 0.1.0–0.4.1, published Mar–Apr 2026, real dependencies — `hls.js`, `plyr`, `blurhash`) — genuine, but frontend-only, no README, and useless without a backend implementing the documented API.

**Conclusion: MediaKit's backend does not exist in any obtainable form** — not self-hosted (the documented install path produces none of it), not hosted (the documented API base URL 404s). The docs site is detailed, internally consistent, and reads exactly like real API documentation — but it documents a server that was never shipped. A frontend component library was published; the Go service it's meant to talk to wasn't, or was pulled.

This is why the spike existed even after a thorough doc review: no amount of reading documentation can confirm a product actually runs. The prior doc-only assessment (§1–§7 below) concluded "partial fit, worth a 2-day spike" — that conclusion was reasonable given only doc evidence, but wrong, because the one thing docs can't show is whether the server exists. It doesn't.

**Everything below this point is the doc-only analysis that motivated the spike. It's kept for the record — none of it changes the outcome. Skip straight to §6 (fallback).**

---

## 1. Method

16 MediaKit doc pages (`https://mediakit.gritcms.com/docs/*`) fetched and cross-checked against the approved video-streaming spec's 12 concrete requirements. Findings were adversarially verified by 3 independent skeptic passes, then re-verified by hand where the skeptics disagreed with the initial read. One material correction came out of that process — see §3.

## 2. Requirement coverage (corrected)

| # | Requirement | Status | Notes |
|---|---|---|---|
| R1 | Presigned direct upload + validation | Partial | `POST /uploads/presign` → PUT → `/uploads/complete`, Bearer auth, 10GB default cap (`MAX_UPLOAD_SIZE_MB`). No documented file-type allowlist or instructor-ownership check — Gradex's backend must mediate the presign call regardless. |
| R2 | HLS ladder 240p–1080p | **Gap** | Documented floor is 360p across every page that lists it (video-streaming, admin-guide, react-sdk). No evidence the ladder is configurable. Real, confirmed shortfall for low-bandwidth GCC mobile. |
| R3 | Transcode-complete webhook → flip lesson status | Covered | `video.transcoded` / `video.transcode_failed` events, HMAC-SHA256 via `X-MediaKit-Signature`, 5-step backoff redelivery (immediate, +1m, +5m, +30m, +2h). No delivery-id for dedup — Gradex's handler must be idempotent on its own. |
| R4 | Purchaser-gated signed playback URL | **Covered (corrected)** | Confirmed by direct re-fetch: `PATCH /api/assets/{id} {is_private:true}`, `POST /api/mediakit/signed-url` (asset_id, expires_in, optional IP lock), 403 on expired/tampered signature, documented purchase-verify-then-sign backend pattern, `SIGNED_URL_SECRET` env var. First-pass eval mis-read this page as placeholder content — it isn't. This was the one requirement that could have killed the whole idea; it doesn't. |
| R5 | Per-user resume position + 90%-completion | Gradex still owns | MediaKit's analytics (`POST /api/analytics/events`) is unauthenticated, `session_id`-keyed, event-oriented (play/pause/seek/watch_duration). No `user_id`, no position-state, no completion rule. Build this regardless of the MediaKit decision. |
| R6 | Replace/re-upload over a published lesson | **Gap** | Undocumented on any page. Unknown whether re-upload mints a new `asset_id` (breaking stored `hls_url` references) or what happens to live viewers mid-retranscode. Own spec's dual-live-until-swap has no MediaKit equivalent. |
| R7 | Auto-retry w/ backoff on transcode failure | **Weaker than own spec** | Failure is surfaced (`transcode_status`, failure webhook), but retry is a manual "Retranscode" button / `POST .../retranscode`, no idempotency key, no auto-backoff. Own spec's 3x exponential backoff is more robust than what's documented here. |
| R8 | Data model integration | Real architectural cost | MediaKit requires its own Postgres — no option to point it at Gradex's schema. Integration means storing a `mediakit_asset_id` FK in Gradex's `videos` table and treating MediaKit's DB as a second, non-authoritative system of record, reconciled via webhook. Conflicts with the own spec's "Postgres is the single source of truth" principle. |
| R9 | Deployment/ops fit for solo dev, ~3.5wk runway | Real added surface | Self-hosting is real (Docker Compose, `grit` CLI scaffolder, own Next.js admin panel, own Redis/asynq pool) — but it's a second full service to run, not a library import. |
| R10 | License & vendor maturity | MIT confirmed, maturity thin | No pricing anywhere (genuinely free). GitHub org (`MUKE-coder`) is real and active — `grit` (94★, pushed today) and `gritcms` (18★) both live; only the specific `mediakit` repo name 404s, likely a stale/renamed doc link, not vendor abandonment. Still: no independent reviews, no case studies, no support channel/SLA anywhere. Young, single-maintainer-flavored project. |
| R11 | React SDK inside Next.js | Covered | App Router-native (`MediaKitProvider`, `app/videos/[id]/page.tsx` examples), 8 components incl. `VideoPlayer`/`PlyrPlayer` with HLS.js + quality switching wired in. Genuine player/upload-UI time savings if adopted. |
| R12 | Kuwait PDPL / data residency | Gradex still owns | Self-hosted means residency is purely Gradex's infra choice (which R2/S3 region, which Postgres/Redis host) — unaffected by this decision either way. |

## 3. Correction note

The first assessment pass read `/docs/private-assets` and `/docs/integration-nextjs` as templated placeholder pages ("Test"/"fact1"/"fact2") and flagged that — plus a supposedly "fabricated" Claude model ID string on `/docs/ai` and a 404 on `github.com/MUKE-coder/mediakit` — as vendor-maturity red flags serious enough to recommend against adoption outright. Independent re-fetch of both doc pages returned substantive, on-topic API documentation matching what a real signed-URL feature would look like, and the GitHub org check showed the author's other repos (`grit`, `gritcms`) are live and actively pushed. Net: the maturity picture is "young, unproven at scale, no third-party reviews" — not "possibly fake." Treat this as a reminder that a single doc fetch isn't ground truth on a make-or-break requirement; it got checked twice here specifically because R4 was the one gap serious enough to end the evaluation if real.

## 4. Verdict

**Not a wholesale swap of the approved spec.** R2 (240p), R6 (replace semantics), R7 (retry robustness), R8 (second DB), and R9 (second service) are real, uncompensated costs, and R5/R12 are on Gradex regardless. But R4 — the one requirement that would have ended this outright — checks out, and R3/R11 are solid, verified wins. That's enough to justify a short, hard-capped spike before deciding, rather than dropping the idea or committing blind.

**Recommendation: partial adoption, gated by a 2-day spike.** If adopted, MediaKit takes the upload/transcode/signed-delivery/player slice; Gradex's own backend keeps purchase/entitlement checks, progress tracking, and replace-semantics parity exactly as already spec'd, calling into MediaKit via its API/webhooks instead of running its own asynq+FFmpeg workers.

## 5. Spike plan (hard cap: 2 elapsed days, no extensions)

**Phase 0 — gate (before starting):** Confirm actual slack exists in the ~3.5-week runway. No slack → skip straight to §6 fallback, spend zero days here.

**Day 1 — stand up + verify R4 end-to-end:**
- `docker compose up` MediaKit locally (Postgres+Redis+MinIO per `/docs/getting-started`), scaffold via `grit`, migrate+seed.
- Presign → upload a real `.mp4` → confirm `video.transcoded` webhook actually fires and HMAC signature verifies against `WEBHOOK_SECRET`.
- Hands-on test the private-assets flow for real: `PATCH` asset to `is_private:true`, mint a signed URL, confirm it plays, confirm an expired/tampered URL returns 403.
- Check whether the rendition ladder can be configured down to 240p.
- **Go/no-go:** R4 must produce a real 403-on-expiry and a real signed playback tied to a specific asset. Fail or ambiguous → stop here, go to fallback.

**Day 2 — verify R6/R7 + final call:**
- Re-upload over an already-published asset; observe whether a new `asset_id` is minted and whether the old `hls_url` stays valid mid-retranscode.
- Induce a transcode failure; confirm whether retry is auto or manual-only, and whether `retranscode` accepts an idempotency key.
- Time a clean-machine setup of the full admin panel + worker pool as a proxy for ongoing solo-maintenance cost.
- **Final go/no-go:** adopt (partially) only if R4 stays confirmed-safe and R6/R7 gaps are small enough to compensate for in less time than building the equivalent slice from the existing spec directly.

## 6. Fallback (default outcome if spike is skipped or fails)

Proceed with [2026-07-17-video-streaming-design.md](./2026-07-17-video-streaming-design.md) exactly as written, zero MediaKit dependency. Rough solo-dev implementation order/estimate to fit the runway:

1. Data model + upload validation + presigned S3 upload — 2–3 days
2. asynq worker pool: `metadata.extract` + `video.transcode` chain, FFmpeg to 240p–1080p — 4–5 days
3. Direct Postgres status update (no reconciliation layer needed — Gradex owns both sides) — 1 day
4. Signed CDN playback endpoint + purchase check + progress tracking — 2–3 days
5. Replace-over-published dual-live-until-swap + reaper job + retry/backoff — 2 days
6. Error paths, correlation IDs, ops metrics, launch-week-spike load test — 2 days

~13–16 days total, entirely on tooling already chosen and trusted (Go, Postgres, Redis, asynq, FFmpeg, S3), every open question already resolved in writing.

## 7. If the spike passes: target architecture

Core principle (non-negotiable): **Gradex's Postgres stays the single source of truth for lesson/entitlement lifecycle. MediaKit is a processing/cache layer underneath it, never the system of record, never exposed to the frontend directly.**

**`VideoService` interface** — MediaKit is never called directly from handlers. All access goes through one Go interface, so swapping providers later doesn't touch business logic:

```go
type VideoService interface {
    RequestUpload(ctx, lessonID, filename, contentType) (UploadTicket, error)
    CompleteUpload(ctx, lessonID, providerAssetID string) error
    GetPlaybackURL(ctx, lessonID, viewerID string) (SignedURL, error)
    Retranscode(ctx, lessonID) error
    HandleWebhookEvent(ctx, payload []byte, sig string) error
}
```

`MediaKitVideoService` implements it for this slice. Handlers, the frontend, and the rest of the backend only ever see `VideoService` — no MediaKit types, URLs, or asset IDs leak past this boundary. This extends the existing spec's already-stated boundary ("Backend never: streams video, transcodes video, serves media bytes directly") to also cover "never exposes a provider's raw URLs/IDs to the client."

**Data model** — extend the existing `videos` table (already 1:1 with `lessons`) rather than introducing a new one, keeping `status` (Gradex's own lifecycle enum: DRAFT→UPLOADING→…→PUBLISHED) as the only field anything outside this slice reads:

```
videos  id, lesson_id, status (Gradex-owned lifecycle enum, unchanged from original spec),
        provider ('mediakit' — future-proofs a second implementation later, not built now),
        provider_asset_id, provider_status (MediaKit's own transcode-status mirror, informational only),
        provider_metadata JSONB (resolution, bitrate, codec, duration, fps — cached from MediaKit so
                                  reads don't need an API round-trip),
        last_sync_at, sync_version (monotonic, gates out-of-order/duplicate webhook writes),
        failed_reason, retry_count (unchanged — see retry note below)
```

`provider_metadata` is a cache, not a source of truth — if it's ever stale or missing, `status`/`failed_reason` (Gradex-owned) are still correct enough to gate playback; only display-only fields (resolution shown in UI, etc.) degrade.

**Webhook durability** — the webhook HTTP handler never writes to Postgres inline. It verifies the HMAC signature, then enqueues a `video.sync` job onto the *same* asynq/Redis queue the original spec already uses (no new infra — this is just one more typed job, consistent with the spec's stated open/closed extensibility). A dedicated sync worker consumes it and applies the update:

```
Webhook (verify HMAC) → enqueue video.sync job (asynq) → Sync Worker → Postgres (idempotent upsert)
```

Why: if Postgres is briefly down, the job sits in Redis and retries instead of the event being dropped on a failed inline write. MediaKit gives no delivery/event ID, so this is **idempotent at-least-once**, not exactly-once — the sync worker upserts gated on `sync_version`/`last_sync_at` so a redelivered or out-of-order event can't regress state.

**Retry note:** MediaKit's own `retranscode` stays manual/no-backoff per the doc eval (§2, R7). Gradex's `video.sync` worker is what watches for `video.transcode_failed` and drives Gradex's own 3x-exponential-backoff retry logic (already in the original spec) by calling `Retranscode()` through `VideoService` — the retry orchestration lives in Gradex, not assumed from MediaKit.

**Rollout order:**
1. Spike passes (§5) →
2. Build `VideoService` + `MediaKitVideoService`, `video.sync` job/worker, extended `videos` schema →
3. Nothing outside the `VideoService` package ever imports MediaKit's SDK/types →
4. Webhook → queue → worker, never webhook → DB inline →
5. Keep `provider_metadata` populated well enough that replacing MediaKit later doesn't require reprocessing any business logic — only re-pointing `VideoService`'s implementation.

**Spec deltas from the original 2026-07-17 doc:**
- "Architecture" / "Upload & Processing Flow": asynq+FFmpeg worker becomes a call-out to `MediaKitVideoService` (presign/transcode/webhook), fronted by the interface above.
- "Playback Flow" signed-URL issuance, purchase/entitlement check, and progress-tracking table: **unchanged** — MediaKit doesn't cover these regardless.
- "Data Model": `videos` table gains the `provider`/`provider_asset_id`/`provider_status`/`provider_metadata`/`last_sync_at`/`sync_version` columns above.
- "Failure & retry": 3x-auto-retry-with-backoff language stays as-is and moves into the `video.sync` worker's responsibility, since MediaKit's own retranscode doesn't provide it.

If the spike is skipped or fails: no spec changes. Record in this file that MediaKit was evaluated and the fallback was taken, so this isn't re-litigated without new cause.
