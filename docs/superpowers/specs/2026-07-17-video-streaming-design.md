# Video Streaming & Playback — Design Spec

> Status: Approved (design phase)
> Date: 2026-07-17
> Scope: First implementation slice of Gradex — video upload, processing, and playback for pre-recorded course lessons.

---

## 1. Overview

Gradex courses are structured as **Course → Section → Lesson**, with one video per lesson. Instructors upload raw video files; the platform transcodes them to adaptive-bitrate HLS, serves them through a CDN with signed URLs (purchasers only), and tracks per-student watch progress.

Out of scope for this slice: live streaming, DRM, thumbnail/subtitle/watermark/virus-scan workers (designed for, not built), device-limit enforcement, cancellation UI, alerting implementation.

**Dependencies/assumptions:** this spec assumes an authentication system (JWT, per the platform's tech stack) and a course enrollment/purchase record already exist or are built in parallel — specifically, an "is this user authenticated," "does this user own this lesson as instructor," and "has this user purchased/enrolled in this course as student" check. Those systems are not designed here; if they don't exist yet, they're a blocking dependency for §5 (Playback Flow) and the authorization step of §4 (Upload & Processing Flow).

---

## 2. Architecture

```
Instructor Upload → Backend API → S3-compat storage (courses/{course-id}/{lesson-id}/raw/original.mp4)
                                    → Redis job queue (asynq, typed jobs)
                                         → Video Processing Service
                                              ├─ metadata.extract   → Postgres (duration, resolution, bitrate, codec, size, fps)
                                              ├─ video.transcode    → hls/master.m3u8 + 1080p/720p/480p/240p renditions
                                              ├─ thumbnail.generate (future)
                                              ├─ subtitle.generate  (future)
                                              ├─ watermark.apply    (future)
                                              └─ virus.scan         (future)
Student Playback → Backend API (auth + course ownership check) → signed CDN URL → CDN → S3-compat storage (hls/)
                  → Backend API (progress ping) → Postgres
```

**Storage layout:**
```
courses/{course-id}/{lesson-id}/
  raw/original.mp4
  hls/master.m3u8, 1080p/, 720p/, 480p/, 240p/
  thumbnails/
  subtitles/
```

**Video status lifecycle (state machine):**
```
DRAFT → UPLOADING → UPLOADED → QUEUED → PROCESSING → READY → PUBLISHED
                                              ↓
                                           FAILED → (retry) → QUEUED
                                           CANCELLED (from QUEUED/PROCESSING, nice-to-have, not v1-blocking)
```

**Backend responsibility (explicit boundary):**
- Owns: authentication, authorization, course ownership verification, signed URL generation, progress tracking, metadata API.
- Never: streams video, transcodes video, serves media bytes directly.

**Video Processing Service** is a worker pool consuming typed jobs from the Redis queue (`metadata.extract`, `video.transcode`, and future job types). New capabilities (thumbnails, subtitles, watermarking, virus scanning) are added as new job types/workers without redesigning the architecture (open/closed).

---

## 3. Data Model

```
courses         (existing/assumed — id, title, ...)
sections        id, course_id, title, order
lessons         id, section_id, title, order, status (video lifecycle enum), duration_seconds
videos          id, lesson_id, raw_key, hls_master_key, resolution, bitrate, codec,
                 file_size_bytes, fps, status, failed_reason, retry_count
progress        id, user_id, lesson_id, max_position_seconds, last_position_seconds,
                 completed, completed_at, last_watched_at, updated_at
```

- `videos` is 1:1 with `lessons` (one video per lesson).
- `progress` is per `(user_id, lesson_id)`.
- Postgres is the source of truth for lifecycle state; storage objects are derived artifacts (see §6 consistency).

---

## 4. Upload & Processing Flow

**Validation** (before signed upload URL is issued): lesson exists; instructor is authorized/owns the lesson; no conflicting in-flight or published video (re-upload while `PUBLISHED` is allowed — see replace behavior below); file type restricted to `.mp4` and `.mov`; max file size 5GB (covers a long lecture at high bitrate; revisit if instructor feedback says otherwise).

**Flow:**
```
Request Upload URL → Backend Validation → Signed Upload URL (15 min expiry)
  → Direct Upload → Object Storage (raw/original.mp4)
  → Complete Upload (idempotent: POST /video/complete — already QUEUED/PROCESSING → no-op success)
  → Queue metadata.extract
       → Metadata Worker → writes duration/resolution/bitrate/codec/size/fps to Postgres
       → Queue video.transcode (chained after metadata succeeds, not parallel —
         transcode ladder decisions depend on source resolution)
            → Transcoding Worker (FFmpeg) → HLS renditions + master.m3u8
            → READY
  → Instructor Review → PUBLISHED
```

**Replace behavior:** re-uploading over a `PUBLISHED` lesson resets lifecycle to `UPLOADING`. Old `hls/` assets are kept live until the new transcode reaches `READY`, then swapped — avoids serving a broken player mid-replace.

**Idempotency:** `POST /video/complete` and `POST /video/retry` are safe to call repeatedly; no duplicate jobs on frontend retry/timeout.

**Failure & retry:** transcode failure → `FAILED` with `failed_reason` stored; auto-retry up to 3x with exponential backoff; beyond that, manual-retry-only via instructor dashboard. Retry restarts the job chain from `metadata.extract`.

**Cleanup on failure:** `raw/original.mp4` retained 30 days after `FAILED` (allows retry without re-upload), then deleted via storage lifecycle policy.

**Internal events** (named now as extension points for future notifications/analytics/audit; not wired to any consumer in this slice): `VideoUploaded`, `MetadataExtracted`, `TranscodingStarted`, `TranscodingCompleted`, `VideoPublished`, `VideoFailed`.

**Security:** signed upload URL expires in 15 minutes; signed playback URL expires in 5 minutes.

**Operational metrics to log** (instrumented, no dashboards built in this slice): queue length, average processing time, failed job count, retry count, worker CPU usage, storage used.

---

## 5. Playback Flow

1. Student opens lesson → frontend calls `GET /lessons/{id}/video/playback-url`.
2. Backend checks: user authenticated, user enrolled/purchased the course containing this lesson, lesson status is `PUBLISHED`.
3. Backend issues a signed CDN URL/cookie (5 min expiry) for `hls/master.m3u8`; CDN validates the signature on each subsequent segment request as playback continues.
4. Frontend player (HLS.js / native HLS) loads the manifest and adapts rendition to bandwidth.
5. Player posts progress every ~10s (`POST /lessons/{id}/progress` with `position_seconds`). Backend updates `max_position_seconds = max(existing, new)` and `last_position_seconds = new`; sets `completed = true` (once, permanently) when `max_position_seconds ≥ 0.9 * duration`.
6. On reopen, backend returns `last_position_seconds` alongside the playback URL so the player resumes there.

**Seeking:** unrestricted. Free seeking counts toward progress — these are paid courses, not compliance training; the platform tracks learning progress, not frame-by-frame watch proof. Completion never regresses on seek-back because it's based on `max_position_seconds`.

**Concurrent sessions:** multiple devices per account allowed in v1. No device-limit enforcement. Account-sharing mitigation deferred to a future spec if it becomes a business problem.

**Playback error contract (frontend):**
- `403` → refresh playback token, retry once
- `404` → show "video unavailable" state
- `500` → retry with backoff

**Progress write failures:** a failed `POST /progress` never interrupts playback; the player retries on the next scheduled tick, no user-facing error.

**CDN cache config** (infrastructure note, not application logic): cache `.m3u8`/`.ts`/`.m4s` per standard HLS TTLs; never cache signed-URL responses in a way that outlives their signature.

---

## 6. Error Handling & Edge Cases

- **Transcode failure:** see §4 (retry/backoff/manual-retry, `failed_reason` surfaced to instructor).
- **Upload interrupted** (network drop mid-upload, `complete` never called): lesson stuck at `UPLOADING`. A reaper job resets entries stale >24h back to `DRAFT`; instructor re-uploads.
- **Worker crash mid-job:** asynq's visibility-timeout/redelivery requeues the job; both `metadata.extract` and `video.transcode` are safe to rerun (idempotent).
- **Storage/CDN outage:** playback-url endpoint returns `503` (distinguishable from `404`/missing-video); frontend shows a retry state.
- **Database vs. storage consistency:** Postgres is source of truth. A periodic reconciliation job scans for mismatches (DB says `READY` but object missing, or object exists but DB was never updated) and flags/corrects them. No distributed transaction — reconciliation is sufficient for this slice.
- **Orphan cleanup:** deleting a lesson/course enqueues an async storage-cleanup job removing its `courses/{course-id}/{lesson-id}/` prefix; not inline with the delete request.
- **Correlation IDs:** every upload/processing job carries a correlation ID threaded through request → queue → worker → storage logs, for tracing.
- **Virus scanning:** explicitly out of scope for this slice; the typed-job design in the Video Processing Service accommodates adding it later without redesign.
- **Alerts to define** (documented now, not implemented): queue depth too high, worker failure rate above threshold, average transcode time trending up, storage nearing capacity, CDN error rate rising.

---

## 7. Testing Considerations

- Upload flow: validation rejects bad file types/oversized files/unauthorized instructors; idempotency of `complete`/`retry` under duplicate calls.
- Processing: metadata extraction accuracy across a few sample codecs/resolutions; transcode job chaining order; retry/backoff behavior on induced ffmpeg failure; reconciliation job catches induced DB/storage mismatches.
- Playback: signed URL rejects expired/tampered signatures; non-purchasers get denied; progress `max()` semantics under seek-backward; resume-position correctness across sessions; concurrent-device playback.
- Failure paths: interrupted upload reaper; storage outage returns `503` not `404`; progress-post failure doesn't interrupt playback.
