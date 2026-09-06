# Course thumbnails

Course thumbnails are optional Asset Version references on `course_revisions`.
They use the existing media tables, upload intents, S3-compatible client, course
ownership policy, revision locks, audit events, and approval transaction.

## Authoring and publication

Create the course draft first, then upload its cover in the Instructor studio.
The upload control supports file selection, drag/drop, progress, processing,
replacement, removal, and a preview with the card's center-crop behavior. The
desktop preview is 340 px wide with the existing 180 px cover height; mobile
covers retain the existing 168 px height. Card widths remain responsive.

Candidate creation copies the live thumbnail reference. Uploading and selecting
a replacement changes only that candidate. Clearing it writes SQL NULL; this is
an explicit removal, not an instruction to inherit the live image again.
Submission and publication validate the reference against its course, origin
revision lineage, readiness, processing evidence, and retirement state.

Who activates the candidate depends on whether the course has published before
([D-097](DECISIONS.md#d-097--admin-review-gates-a-courses-first-publication-only)):

- **Before first publication** the cover reaches the public only through Admin
  approval, exactly as before. The Admin inspector renders the exact candidate
  cover alongside the live cover when one exists.
- **After first publication** the instructor uploads a replacement into their
  candidate and publishes it themselves. No Admin decision is involved.

The boundary that did not move is the one that matters: a candidate cover is
never public. While the instructor is editing, the previously published cover
keeps serving; the new one becomes public only when that exact revision is
promoted, through the same atomic `live_revision_id` switch. There is still no
separate thumbnail publication write, and the fallback order is unchanged —
published cover, then the generated subject artwork, then the generic fallback.

Request-changes history is preserved and unchanged. First-publication drafts
still become `CHANGES_REQUESTED` when an Admin returns them. Existing `REJECTED`
records from the earlier policy remain immutable history; because an already
published course can no longer be submitted for review, that path produces no
new records. A subsequent candidate copies the live revision. None of this
changes the public image.

The public catalogue, its personalized ordering, and course detail responses
include only the live thumbnail. Carousel cards prefer that image and retain
their generated subject artwork underneath as the absent/error fallback. The
catalogue's existing text cards display a cover when supplied and retain their
text-only presentation when absent or unavailable. The detail page has no image
cover header, so its existing layout and video-preview surface are unchanged.

## API contract

All routes below are relative to `/api/v1`. Existing session authentication,
capabilities, mutation security, and problem responses apply.

| Operation | Route | Contract |
|---|---|---|
| Upload intent | `POST /media/uploads` | Existing body, with `kind: "THUMBNAIL"`, owned `course_id`, editable `revision_id`, raster `content_type`, and `size_bytes`. Lesson and logical asset IDs must be absent. |
| Complete/process | `POST /media/uploads/{assetVersionID}/completions` | Existing exact-version completion body: provider event ID, issued object key, provider version/ETag identity, content type, byte count, SHA-256. Returns `READY` only after derivative generation. |
| Publish changes | `POST /courses/{courseID}/revisions/{revisionID}/publish` | Instructor self-publication of one exact candidate revision, cover included. Refused with `FIRST_PUBLICATION_REQUIRES_REVIEW` for a course that has never published. |
| Select/remove | `PUT /courses/{courseID}/revisions/{revisionID}/thumbnail` | `thumbnail_asset_version_id` and `expected_asset_version_id`, each UUID or null. Returns the saved revision. Null selection removes the cover. |
| Instructor preview | `GET /courses/{courseID}/revisions/{revisionID}/thumbnails/{assetVersionID}/{variant}` | Owned course and exact revision; `variant` is `card` or `large`. |
| Admin preview | `GET /admin/review/courses/{courseID}/revisions/{revisionID}/thumbnails/{assetVersionID}/{variant}` | Catalog-publish capability and exact revision. |
| Public delivery | `GET /catalog/courses/{courseID}/thumbnails/{assetVersionID}/{variant}` | Current published revision only; suspended/retired/delisted courses and candidate assets are refused. |

Revision responses gain optional `thumbnail_asset_version_id`. Public course
responses gain optional `thumbnail`, containing `asset_version_id`, `card_url`,
and `large_url`. URLs are same-origin paths, not bucket URLs or signed credentials.
Old clients ignore these additive fields; new clients tolerate absent/null fields.

Selection compares the caller's expected asset with the current candidate
reference. A replay of the already-selected value succeeds without another audit
event. A stale conflicting selection returns a state conflict. A failed or
ambiguous completion/selection leaves submission blocked in the UI until the
saved cover is reloaded or the upload succeeds. Completion retries reuse the same
provider event and exact evidence.

## Storage and hostile-input handling

The existing private bucket and `S3_*` configuration are reused. No new bucket,
public ACL, storage credential, base64 payload, or image blob in PostgreSQL is
introduced. Thumbnail PUT signatures also bind `Content-Length`; completion
independently checks the actual stored length, signature/type, and SHA-256 against
the exact provider object version or strong ETag identity.

- Inputs: JPEG, PNG, static WebP; at most 5 MiB, or the existing deployment upload
  ceiling if smaller. SVG, animated PNG/WebP, spoofed types and malformed images
  are rejected.
- Decode limits: maximum side 8192 px and 16 million pixels, checked before full
  decode. Oriented dimensions must be at least 800 × 450 px.
- Recommended input: landscape 16:9, 1200 × 675 px or larger. The responsive card
  applies its existing centered `object-fit: cover` crop.
- JPEG, PNG and WebP EXIF orientation is normalized. Derivatives are re-encoded
  without EXIF/GPS or other source metadata. Transparent pixels composite on white.
- Derivatives: JPEG quality 82, fit within 800 × 800 and 1440 × 1440, preserving
  the source ratio and never upscaling. Two simultaneous decodes per API process.

Each upload creates a new THUMBNAIL media asset and one immutable Asset Version.
Source keys follow the existing `quarantine/{courseID}/{assetVersionID}/source`
convention. Derivatives use `media/{assetVersionID}/thumbnail/card.jpg` and
`large.jpg`. User filenames never participate in key generation.

Thumbnail readiness has its own exact-source raster-processing evidence in
`media_thumbnail_variants`. It does not fabricate malware-scan evidence or widen
the existing video/resource trusted-validation profile. Thumbnails are accepted
in the existing SCANNER and TRUSTED_INSTRUCTOR deployments; ADMIN_CATALOGUE still
disables Instructor uploads. Only regenerated derivatives can be delivered.

Private preview responses are `private, no-store`. Approved public derivatives
use immutable URLs and `public, max-age=86400, immutable`; replacement produces a
new URL. Already-public images can remain in browser/CDN caches for that day.
Every new server delivery request checks live authority. All successful image
responses carry `X-Content-Type-Options: nosniff`.

## Lifecycle and deployment

The worker collects unreferenced thumbnail assets after seven days, at startup
and hourly, in bounded batches. Every revision reference counts, including
rejected and superseded history. An abandoned but still-referenced draft remains
authored data; the collector does not delete drafts.

Collection locks and retires the asset before storage deletion, rechecks revision
references after locking, and records a retryable tombstone. Attachment uses the
same asset lock and refuses retired assets. Deletion covers source/derivative
object versions and delete markers on versioned S3/MinIO, with current-object
deletion for providers without version listing. Per-object deletion failures
remain retryable. Database processing and audit evidence are retained.

Migrations `0032` and `0033` expand the enum and schema separately. No existing
course needs a backfill. `0033` adds the optional reference, processing evidence,
cleanup tombstones, indexes, and database guards. READY media identity remains
immutable, and submitted/live thumbnail references cannot be edited.

Apply migrations and deploy the matching backend before enabling the new frontend.
API readiness now requires schema 33. Previous binaries reject schema versions
above their compiled maximum, so coordinate this rollout; an older binary is not
a drop-in rollback against schema 33. A rollback build must explicitly support
the expanded schema. Once thumbnail history exists, destructive schema rollback
is refused. The canonical development migration command checks this before
golang-migrate can mark the schema dirty; production down migrations remain
disabled by the existing policy.

The default production-like compose profile remains ADMIN_CATALOGUE. An
Instructor-upload deployment must already be configured for SCANNER or
TRUSTED_INSTRUCTOR. Existing bucket credentials need version-listing and object
deletion permissions for collection. No production deployment or production
migration is part of this implementation task.
