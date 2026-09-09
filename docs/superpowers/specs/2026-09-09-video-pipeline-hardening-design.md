# D-103 Video Pipeline Hardening

**Date:** 2026-09-09
**Base:** `b8dea967196de68914440b2092cd80daf85d9546`
**Status:** Implemented pending independent review. Reviewer unassigned.
**Schema:** `0035_media_work_leases` (one additive migration)

## Scope and design

D-103 keeps the existing direct-to-private-storage upload, PostgreSQL outbox,
Asynq delivery, FFmpeg HLS, and signed-delivery architecture. It adds durable
ownership to work that previously had only a coarse `SCANNING` or `PROCESSING`
state. Each claim now records an opaque token, claim time, lease expiry, and a
stage-specific attempt count on the immutable Asset Version. The worker's
periodic recovery pass locks expired rows, records failure evidence, and uses
the existing outbox to schedule another bounded attempt. There is no second
queue or coordination service.

Processing output is immutable per `(Asset Version, operation)`. The operation
identifier is SHA-256-derived before it becomes an object-key component, so it
cannot inject a path. FFmpeg output is checked locally for a master, rendition
playlists, safe relative segment references, and referenced segments. Each
object is uploaded and HEAD-verified; the master is uploaded last. READY is a
conditional database transition requiring the same claim token, successful
processing evidence, trusted duration, and rendition rows.

The alternatives rejected were queue-only retries, which cannot distinguish a
live committed claim from a dead worker, and a new media coordinator or queue,
which would redesign the platform rather than harden it.

## Pipeline before D-103

An Instructor asks the API for an upload intent. The API authorizes an active
Course owner, creates an immutable `media_asset_versions` row in `UPLOADED`,
creates an expiring `upload_intents` row, generates
`quarantine/{courseID}/{assetVersionID}/source`, and returns a presigned PUT.
The browser uploads directly and sends the provider object identity, declared
type, size, and SHA-256 to completion. Completion locks the intent, checks a
provider-event receipt, HEADs and reads the exact object version, verifies real
format, size, and hash, then moves it to private quarantine.

In scanner mode completion appends `media.scan_requested`. In the production
trusted-Instructor profile, MP4 Lesson video and public preview receive exact
object validation evidence and append `media.transcode_requested`; PDF/DOCX
Lesson Resources become READY after validation. The outbox dispatcher polls
committed PostgreSQL rows and enqueues an Asynq task whose task ID is the outbox
event ID. The worker scans or downloads the exact source version to a unique
temporary file, probes it, runs argument-array `ffmpeg` commands, uploads HLS,
and records immutable attempt/rendition evidence before READY.

The combined completion-and-selection HTTP operations use two idempotent
transactions: media completion first, then editable revision selection. A lost
response can be retried with the same provider event; the duplicate completion
receipt converges and selection is retried. Replaceable Lesson video and public
preview selection follows strictly increasing upload-intent creation order, so
a late older completion cannot displace a newer completed selection.

Student playback resolves the exact video selected by the approved live (or
eligible superseded) Lesson, requires READY processing provenance and
renditions, evaluates the Student's entitlement, and issues a Student/Lesson/
Asset-Version-bound HMAC session. The API renders the master, re-evaluates the
session and entitlement for rendition playlists, parses stored playlists, and
replaces safe relative segment references with absolute-expiry presigned URLs.
Admin candidate playback uses a separate Admin-bound session and an exact
`PENDING_REVIEW` Course/revision/Lesson/version query. Anonymous public preview
resolves only the approved live revision's explicitly selected
`PUBLIC_PREVIEW` Asset Version and signs its source object; that route does not
authorize protected Lesson media.

## Authoritative state machine

| State | Entered by | Forward event | Terminal / retry | Recovery owner | Required storage | Instructor / publication / playback |
|---|---|---|---|---|---|---|
| `UPLOADED` | upload-intent transaction | verified completion | Not terminal; browser retries completion | browser/API | source may be absent or incomplete | shown as interrupted after reload; never submittable or playable |
| `QUARANTINED` | completion or retry | scan claim or trusted validation | Waiting | worker/outbox | exact source version | processing/waiting; never submittable or playable |
| `SCANNING` | atomic worker claim | exact scan result | Leased; expired claim is retried up to 3 attempts | worker recovery | exact source version | processing/waiting; never submittable or playable |
| `SCAN_PASSED` | successful scan evidence | transcode claim for video; READY for non-video | Waiting | worker/outbox | exact source version | processing/waiting; video never playable |
| `SCAN_FAILED` | scanner rejection | Admin retry/re-upload | Terminal until intentional action | Admin/Instructor | rejected source retained | visible failure; never submittable or playable |
| `SCAN_ERROR` | invalid/unavailable scan or exhausted scan recovery | automatic retry while budget remains; otherwise Admin retry/re-upload | Terminal only after retry budget/direct manual execution | worker, then Admin/Instructor | source retained | visible failure when terminal; never submittable or playable |
| `VALIDATED` | trusted exact-version validation | transcode claim for video/preview; READY for admitted resource | Waiting | worker/outbox | exact source version | processing/waiting for video/preview; never playable yet |
| `PROCESSING` | atomic transcode claim | conditional successful completion or failure | Leased; storage failure/expired claim retried up to 3 attempts | worker recovery | source plus one attempt-scoped partial/complete HLS prefix | measured progress only; never submittable or playable |
| `PROCESS_FAILED` | permanent/exhausted processing failure | Admin retry/re-upload | Terminal until intentional action | Admin/Instructor | source retained; failed attempt prefix cleanup is best effort | visible failure; never submittable or playable |
| `READY` | evidence-backed conditional transition | none | Terminal and database-immutable | none | exact source; video has trusted duration, renditions, and verified HLS output | ready; eligible for attachment validation and policy-specific playback |

`READY` is the only deliverable state. Unknown states fail closed. The database
trigger enforces the legal state edges and exact scan/validation/processing
provenance. D-103's recovery deliberately traverses existing legal edges:
interrupted scan uses `SCANNING → SCAN_ERROR → QUARANTINED`; interrupted
processing uses `PROCESSING → PROCESS_FAILED → QUARANTINED`, then returns to
the original safety path. Scanner-proven media is rescanned. Trusted validation
may be reused only for the same immutable source object version after a worker
interruption or transient storage failure; an Admin-requested retry continues
to re-read and revalidate the exact bytes.

## Claims, retry, backoff, and crashes

- Scan and processing claims use a row lock plus conditional UPDATE. Claim
  token, timestamps, state, and attempt increment commit together.
- A live duplicate delivery sees a non-claimable state and performs no work.
- The maximum is three attempts per stage. Durable retry delays are 5 seconds,
  30 seconds, then 2 minutes through `outbox_events.available_at`.
- Scanner/provider unavailability and storage GET/PUT/HEAD failure are
  transient. Malformed media, missing video metadata, FFmpeg failure, timeout,
  zero/invalid output, and unsafe/partial HLS are terminal processing results.
- The configured processing timeout defaults to 15 minutes. Both scan and
  transcode handlers are context-bounded. The work lease is that timeout plus
  one minute, leaving a bounded persistence/cleanup grace period.
- A crash before work, during FFmpeg, after object upload, or before READY leaves
  a lease. Recovery records `WORKER_INTERRUPTED`, schedules a new operation,
  and prevents the old token from finalizing. A crash after READY is harmless:
  the callback receipt and immutable READY row make replay a no-op.
- Each processing operation has a distinct HLS prefix. Recovery best-effort
  deletes only the abandoned operation prefix after committing the database
  recovery. Cleanup failure cannot invalidate canonical media.

Stable failure categories persisted for diagnosis are `INVALID_MEDIA`,
`STORAGE_UNAVAILABLE`, `PROCESS_TIMEOUT`, `TRANSCODE_FAILED`, `SCAN_REJECTED`,
`SCAN_UNAVAILABLE`, and `WORKER_INTERRUPTED`. Status responses expose the
category without exposing object keys, signed URLs, tokens, or FFmpeg command
arguments. Scanner reasons and FFmpeg/ffprobe diagnostics are bounded to 2,000
bytes; ffprobe JSON is bounded to 1 MiB.

## Format, FFmpeg, temporary files, and storage

The upload API accepts MP4 and QuickTime Lesson-video containers in scanner
mode. The production trusted-Instructor profile and browser authoring control
accept MP4 only. WebM is recognized when it contradicts an MP4/QuickTime claim
but is not an accepted upload type. Browser MIME is not authority: completion
reads the stored exact version and validates its bytes. Zero-size requests are
rejected before presigning; zero-size or size-mismatched completion is rejected
before work. A truncated MP4 may pass the container prefix check but fails
ffprobe and becomes `PROCESS_FAILED` without crashing the worker.

FFmpeg and ffprobe use `exec.CommandContext` with argument arrays, never a
shell. Input and output paths are server-generated. Source download streams to
a unique temporary file and HLS uses a unique temporary directory; both are
removed on return. FFmpeg's structured progress stream is measured against the
trusted duration. Progress is attempt-token scoped, monotonic, clamped, never
reaches 100 before READY, and stops being presented in failure states.

The source key is immutable and version-scoped. HLS is additionally
operation-scoped. Existing READY rows keep their persisted legacy rendition
keys and require no reprocessing. D-103 does not delete old successful source
or rendition objects because repository reference status is not sufficient to
prove deletion safe. Expired, never-completed upload intents also remain a
known orphan-retention limitation; their rows are unselected and fail closed,
but no retention policy was invented.

## Replacement and publication semantics

A completed replacement is selected on the editable revision while it is
still scanning/processing. Therefore the editable candidate temporarily has no
playable video until the replacement is READY. If the Course already has a
published live revision, that separate live revision continues serving its old
READY selection until the candidate is published. If the replacement fails,
the failed selection remains visible to the Instructor and blocks submission;
the Instructor re-uploads or an Admin uses the existing retry operation.

Concurrent replacements serialize on the Course/Lesson or revision row and
compare strictly ordered intent creation. The newest completed intent wins,
not the callback that happens to finish last. Workers mutate only their Asset
Version; they never set `course_lessons.video_asset_version_id`,
`course_revisions.preview_asset_version_id`, or a live-revision pointer.
Removing Lesson video while processing is not a current product operation.
Superseding a Course revision changes no media ownership and cannot redirect a
worker into the new candidate.

Submission and publication re-read attached Asset Versions through the server
validator and require READY. Student, public-preview, and Admin-preview queries
also require READY at the point of authorization. Processing, failed, unknown,
foreign-Course, foreign-Instructor, and unentitled requests fail closed.

## Signed URL model

`PLAYBACK_URL_EXPIRY` remains the configurable signing grace (default 5
minutes). For a video with trusted duration, the playback session and segment
signatures now last for `trusted duration + PLAYBACK_URL_EXPIRY`. This is needed
because the current VOD rendition playlist contains all presigned segment URLs
up front; a fixed five-minute expiry stopped a 90-minute lecture midway. The
server still re-evaluates identity, exact version, READY state, and entitlement
when issuing the master and rendition playlists. Once a segment URL is issued,
the storage provider cannot revoke it before its absolute expiry. Public
preview source URLs use the same duration-aware lifetime when trusted duration
exists. Older scanner-mode READY previews without trusted duration retain the
configured fixed lifetime.

Trusted duration is server-measured, but `ffprobe` reports the duration a
container *declares*, and a crafted upload can declare far more than its bytes
carry. Because an issued segment URL cannot be recalled before its absolute
expiry, the derived lifetime is clamped: a duration that is non-positive,
exceeds the 12-hour `maxPlaybackLifetime` bound, or would overflow the computed
lifetime falls back to the configured grace alone rather than extending the
capability. No client input reaches this value on any path.

## Instructor UX

The existing EN/AR upload surfaces and responsive layout are unchanged.
`UPLOADED` reloads as interrupted; quarantine, scanning, validated,
scan-passed, and processing states display background processing; READY and the
three failure states stop polling. Polling is scoped to one Asset Version,
aborts on unmount/version change, ignores late responses, retries transient
status-read failure, and has a 30-minute whole-watch bound. The existing retry
button opens re-upload; the Admin-only server retry remains separate. D-103
adds `failure_category` to the owner/Admin status payload but exposes no storage
identity.

## F1–F40 disposition

| Failure | Status | Evidence / disposition |
|---|---|---|
| F1 zero-byte input | SAFE | request and completion require positive exact size; no work is scheduled |
| F2 truncated video | FIXED | bounded ffprobe failure becomes non-deliverable `PROCESS_FAILED`; real E2E fixture added |
| F3 MIME mismatch | SAFE | exact stored bytes decide MP4/QuickTime; WebM contradiction is typed |
| F4 unsupported format/codec | SAFE | WebM is rejected at intent; undecodable MP4/unsupported FFmpeg input terminates cleanly |
| F5 interrupted upload | DEFERRED | unselected `UPLOADED` row/intent expires and fails closed; automated orphan retention needs a separately approved deletion policy |
| F6 upload succeeds/finalization lost | SAFE | same completion event replays from durable receipt; combined route then retries selection |
| F7 duplicated finalization | SAFE | provider-event receipt and fingerprint converge or reject conflicting evidence |
| F8 job delivered twice | FIXED | atomic claim token, idempotent receipt, conditional READY, and operation prefix |
| F9 crash before processing | FIXED | expired durable claim is recovered and requeued |
| F10 crash during FFmpeg | FIXED | partial attempt is never READY; isolated prefix is best-effort cleaned |
| F11 crash after upload/before READY | FIXED | new attempt uses a new prefix; stale token cannot commit READY |
| F12 READY commit/ack lost | SAFE | READY is immutable and duplicate callback receipt is a no-op |
| F13 storage write/read/HEAD failure | FIXED | no READY; transient storage category requeues with backoff |
| F14 storage read timeout | FIXED | bounded transient retry, maximum three attempts |
| F15 FFmpeg non-zero | FIXED | exit checked; diagnostic capture is bounded; worker survives |
| F16 FFmpeg hang | SAFE | pre-existing 15-minute context deadline retained; failure persistence hardened |
| F17 API restart during upload | SAFE | intent/object identity/completion receipt are persisted; same request converges |
| F18 worker restart loop | FIXED | leases, attempt budget, isolated outputs, and recovery prevent corruption/hot loops |
| F19 stuck SCANNING | FIXED | legacy-null or expired lease is recovered, evidenced, and boundedly retried |
| F20 stuck PROCESSING | FIXED | legacy-null or expired lease is recovered, evidenced, and boundedly retried |
| F21 scan failure | SAFE | `SCAN_FAILED` means rejection; `SCAN_ERROR` means unavailable/invalid scan; neither is deliverable |
| F22 process failure | SAFE | authoring, submission, Student, public, and Admin paths all reject it |
| F23 replace while old processes | SAFE | ordered selection lives outside worker; old worker mutates only old version |
| F24 replace READY video | SAFE | editable candidate selects new processing version; published live revision stays on old READY version |
| F25 concurrent replacements | SAFE | strict intent order plus row locks makes newest completed intent authoritative |
| F26 remove while processing | DEFERRED | Lesson-video removal is not a current product operation |
| F27 superseded revision while processing | SAFE | worker owns only exact version; revision/live pointers are never worker writes |
| F28 publish while processing | SAFE | server submission validator requires READY in its transaction |
| F29 Student requests processing | SAFE | exact target query and `readyVideo` fail closed without key leakage |
| F30 public requests non-ready | SAFE | approved-live exact preview query requires READY |
| F31 signed URL expires | FIXED | session/segments cover trusted duration plus configured grace |
| F32 HLS consistency | FIXED | generated paths/references checked, stored objects HEAD-verified, delivery parser fails closed |
| F33 partial HLS output | FIXED | missing master/playlist segment or failed PUT/HEAD blocks READY |
| F34 object-key collision | FIXED | version plus hashed operation scopes every new HLS prefix |
| F35 object-key injection | SAFE | source keys are server-generated; operation input is hashed; playlist references are constrained |
| F36 presigned upload abuse | SAFE | active owner, exact Course/Lesson/revision scope, PUT/content type/expiry, and completion size/hash checks; provider PUT cannot enforce video length before upload |
| F37 protected playback IDOR | SAFE | exact Course/Lesson/version relationship and entitlement are re-evaluated before manifests |
| F38 public-preview isolation | SAFE | only approved live `PUBLIC_PREVIEW` is anonymous; protected Lesson version cannot use that route |
| F39 old object cleanup | DEFERRED | abandoned attempt prefixes are cleaned; successful historical objects and expired-upload objects are retained rather than risk referenced-data loss |
| F40 cleanup failure | SAFE | cleanup is secondary; canonical/new attempt state remains valid |

## Known limitations

- No automatic deletion policy exists for expired incomplete uploads or old
  successful media versions. This is bounded in authorization, not in storage
  consumption.
- Storage PUT presigning constrains method, key, content type, and expiry but
  does not enforce the declared video byte length before bytes arrive; exact
  size and configured maximum are enforced at completion.
- Segment URLs are third-party bearer capabilities until their absolute
  duration-aware expiry; mid-session entitlement revocation prevents new API
  authorizations but cannot revoke an already issued storage URL. That window is
  bounded by the 12-hour maximum playback lifetime, not by trusted duration
  alone.
- A single legitimate lecture longer than the 12-hour bound would be signed for
  the configured grace only and could not be watched to the end in one
  authorization. No such media exists, and raising the bound is a deliberate
  security trade rather than a tuning change.
- Existing scanner-mode READY public previews may lack trusted duration and
  therefore retain the fixed configured URL lifetime.

No production access, push, deployment, storage-provider change, CDN change,
DRM, adaptive-bitrate redesign, or existing-media invalidation is part of
D-103.
