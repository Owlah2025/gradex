# Video Upload, Processing, and Playback — Design Record

> Status: Implemented technical slice with known product/security gaps; revalidate during platform system design
> Original date: 2026-07-17
> Reconciled: 2026-07-23
> Scope: Pre-recorded Lesson video only

## 1. Purpose and Authority

This record documents the existing Go video slice and the product constraints it must ultimately
satisfy. It does not replace [PRD.md](../../PRD.md),
[BUSINESS_RULES.md](../../BUSINESS_RULES.md), or [DOMAIN_MODEL.md](../../DOMAIN_MODEL.md).

The canonical hierarchy is `Course → Section → Lesson`, with at most one current approved Video
Asset per Lesson. Raw Instructor uploads are processed into HLS renditions; entitled Students play
approved video and maintain per-Lesson progress.

This slice excludes live/office-hours video, DRM, captions/transcripts, thumbnails, watermarks,
device limits, and public Course previews. A public preview is a separate asset governed by
BR-143/144 and cannot reuse protected Lesson authorization accidentally.

## 2. Current Repository Implementation

The current backend implements:

- direct presigned upload to S3-compatible storage;
- `.mp4`/`.mov` extension checks and a configured post-upload size limit;
- Redis/asynq jobs for metadata extraction and FFmpeg HLS transcoding;
- 1080p/720p/480p/240p renditions without upscaling above the source (except the minimum rung);
- Postgres-backed video/progress state;
- a token-authorized manifest proxy that rewrites child playlists and signs segment URLs;
- per-Lesson resume position and permanent completion at 90%;
- retries, stale-upload reset, and integration tests around storage/worker behavior;
- fake development authentication/entitlements pending the real auth/commerce systems.

Relevant source:

- `backend/internal/video/`
- `backend/internal/httpapi/router.go`
- `backend/internal/auth/fake.go`
- `backend/internal/db/migrations/0001_init.up.sql`

No CDN is configured in this repository. The current path serves manifest content through the Go
API and HLS segment bytes through presigned object-storage URLs. System design may add a CDN but
must then define cache/signature behavior explicitly.

## 3. Current Processing Flow

```text
Instructor requests upload URL
  → server checks lesson ownership through the current entitlement seam
  → client uploads raw .mp4/.mov directly to object storage
  → client completes upload
  → metadata.extract job (ffprobe)
  → video.transcode job (FFmpeg HLS ladder)
  → READY
  → approved publication operation
  → PUBLISHED/playable
```

Current Video Asset lifecycle:

```text
DRAFT → UPLOADING → QUEUED → PROCESSING → READY → PUBLISHED
                    ↘                     ↗
                      FAILED → retry ─────
```

The migration includes `UPLOADED`, although the current service transitions directly from
`UPLOADING` to `QUEUED`. System design should remove unused state or define its meaning rather than
letting code and model drift.

The source of truth for processing/publication state is Postgres. Object-storage files are derived
artifacts. The final system design must define orphan cleanup, reconciliation, backup/recovery, and
whether raw uploads are retained after successful processing.

## 4. Product Constraints the Final Design Must Preserve

### Upload and Publication

- Only the owning Active Instructor may upload/retry video for their own Course.
- A first Course cannot become Published without Admin approval and required content readiness.
- A structural/video change to an already Published Course creates a pending Course Revision. The
  approved live Video remains playable until Admin approves the replacement; processing `READY` is
  not publication approval. *(BR-016/017/061)*
- Upload/complete/retry operations must be idempotent under client and queue retry.
- File validation must not trust extension or client MIME alone; system design must define content
  inspection, quarantining, and safe FFmpeg execution.

### Entitlement and Playback

- Before issuing every playback authorization, the server verifies Active Account, Course/video
  status, and a current Course Entitlement or a current Section Entitlement covering the Lesson.
  *(BR-007/023/050)*
- Admin preview uses its separate audited authorization path. Instructor preview/edit behavior must
  be explicitly designed; it cannot masquerade as a Student purchase.
- HLS authorization is short-lived and scoped to the Video/playback session. HLS segment requests
  may repeat for seek/rebuffer/adaptive bitrate; “single use” is inappropriate. *(BR-100)*
- Unauthorized, expired, revoked, suspended, missing, and temporarily unavailable outcomes have
  distinct safe client behavior without leaking storage keys or existence.
- Access is denied when Entitlement expires/revokes or Account is suspended, even if the Student
  previously obtained a URL/token. The mechanism belongs to system design.

### Progress

- Progress belongs to Student Enrollment/Lesson, not the replaceable Video file.
- Track last position for resume and maximum position for completion.
- Completion becomes true at 90% and never regresses. *(BR-051/052/059)*
- Progress-write failure never interrupts otherwise authorized playback; retry safely. *(BR-053)*
- Never accept impossible/negative positions; clamp or reject beyond-duration values consistently.

## 5. Proposed Capability Contract

Final API paths and error envelopes are system-design decisions. The platform needs capabilities
equivalent to:

- owning Instructor requests an upload ticket, completes upload, and retries failed processing;
- Admin/approved Course workflow publishes the ready Asset/revision;
- entitled Student requests a playback authorization and posts progress;
- authorized manifest/segment delivery validates the short-lived playback grant.

The current repository exposes these under `/api/v1/lessons/:lessonID/...` and a
`/api/v1/videos/:videoID/manifest/...` route. Treat those paths as existing implementation, not a
frozen public contract.

## 6. Security and Reliability Requirements

- Run FFmpeg/ffprobe with bounded CPU, memory, time, disk, output size, and isolated untrusted-input
  handling selected during system design.
- Validate object existence/size after direct upload; do not trust client completion.
- Signed upload/playback TTLs, maximum upload size, retry counts, stale thresholds, and retention are
  configurable operational parameters with safe bounds.
- Queue delivery is at-least-once: every job and state transition must tolerate duplicates and
  out-of-order/stale work.
- Keep raw/storage paths and signed URLs out of logs; use correlation identifiers instead.
- Define metrics/alerts for queue age/depth, processing duration/failure, retry exhaustion, storage
  errors/capacity, and playback authorization/delivery failures.
- Define reconciliation for DB/object divergence and recovery for a worker crash during publish/swap.
- Storage deletion follows Course/Lesson history rules; never delete an enrolled/financial/audit
  record merely because media cleanup occurs.

## 7. Known Implementation Gaps

These are mandatory follow-ups, not alternate product decisions:

- `backend/internal/auth/fake.go` trusts `X-Debug-User-ID` and fake entitlement rows. The API process
  refuses non-fake mode because real auth is not implemented; this must be impossible in production.
- The Instructor route currently exposes `POST .../publish` and changes Video state directly. It must
  be controlled by the Admin-approved Course/revision workflow.
- Re-upload over `PUBLISHED` currently changes the same Video record to `UPLOADING`, so the approved
  video is no longer playable while processing. This violates the live-version/pending-revision rule
  and needs versioned/staged replacement before release.
- The fake entitlement check is per Lesson and does not yet model Course/Section Entitlements,
  expiry, Account suspension, Course status, or the Admin preview path.
- The manifest token proves only Video/expiry and is not tied to Account/session status. The final
  design must satisfy immediate suspension and entitlement revocation throughout playback.
- Current token errors flow through the generic conflict mapping rather than a finalized
  authentication/authorization error contract.
- MIME/content inspection, malware/security scanning policy for applicable public/downloadable
  assets, FFmpeg isolation, reconciliation, cleanup/retention, CDN, and production observability are
  not complete.

## 8. Verification Matrix

- Upload: owner/non-owner/suspended Instructor; wrong type/content; oversize; interrupted upload;
  duplicate complete; stale reset.
- Processing: source-resolution ladder; malformed media; bounded FFmpeg failure; duplicate job;
  retries exhausted; storage/DB divergence.
- Publication: first approval; pending replacement while old stays live; rejection keeps old live;
  atomic approved swap.
- Playback: Course Entitlement; matching/nonmatching Section Entitlement; expired/revoked;
  suspended Account; emergency Course access suspension; qualifying Delisted/Archived access;
  Admin audited preview; expired/tampered
  token; repeated HLS seek/rebuffer.
- Progress: resume, seek backward, monotonic maximum, 90% boundary, over-duration input, concurrent
  updates, replacement preservation, transient write failure.
- Privacy/operations: no secrets/URLs/PII in logs; correlation; metrics; backup/recovery; retention.

## 9. System-Design Inputs Still Open

- media deployment topology, worker isolation, storage/CDN and regional placement;
- staged Asset/version model integrated with Course Revision approval;
- production auth/entitlement/playback-token strategy and immediate suspension;
- exact TTLs, limits, retry/backoff, retention, and cleanup policies;
- API/error/event contracts, observability, reconciliation, and disaster recovery;
- supported upload content policy and security-scanning boundary.
