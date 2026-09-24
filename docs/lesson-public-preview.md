# Lesson public preview

An anonymous visitor watches a real Lesson. Not a separate promotional clip — the
same video file, the same transcode, the same canonical HLS renditions, the same
storage objects a paying Student streams. What changes between the two viewers is
only what the server proves before it hands out a manifest.

## What replaces what

| | Legacy course preview | Lesson public preview |
|---|---|---|
| Media | a separate `PREVIEW` asset, uploaded once per Course revision | the Lesson's own `VIDEO` asset version |
| Delivery | a presigned URL for the original MP4 | protected HLS: dynamic master, dynamic rendition manifest, signed segments |
| How many | one per Course | zero, one, or many Lessons |
| Where the permission lives | `course_revisions.preview_asset_version_id` | `course_lessons.allow_public_preview` |
| Extra transcode | yes | none |

Many previewable Lessons is the intended model, not a tolerated edge case.
Nothing in the new curriculum API assumes one.

## Why the permission is on the Lesson, not the media

`course_lessons` is revision-scoped. A candidate revision holds its own lesson
rows, cloned from the revision it is based on, and those rows become public only
when an Administrator approves the candidate and it becomes the live revision.
Preview intent has exactly those semantics, so it is carried by exactly that row
and cloned by exactly that clone.

Putting it on `media_assets` or `media_asset_versions` would have been wrong in a
way that is hard to see and hard to undo. A media asset is globally reusable. A
permission on the asset is a permission that escapes the revision that granted
it: marking one Lesson previewable would silently expose the same video wherever
else it was used, and revoking it in one place would revoke it everywhere.

## Revision semantics

- The flag is edited only on a **candidate** revision.
- It is cloned with the rest of the revision's lesson data.
- It is part of the submitted revision an Administrator reviews, and the Admin
  inspector shows it per Lesson, because it is part of what is being approved.
- It becomes public only when that revision becomes live.

Changing a candidate never changes current live preview access. Approving is the
only thing that does.

## Video only

V1 previews the Lesson **video** and nothing else. `RESOURCE` and `LAB_MATERIAL`
attachments stay entitlement-protected on a previewable Lesson exactly as they are
on any other.

`allow_public_preview = true` on a Lesson with no video is rejected by the
authoring API rather than accepted and quietly serving nothing — an Instructor who
marked a Lesson previewable should be told it has no video, not left believing
they published something. If the video exists but is not `READY`, the flag stays
as candidate intent and public playback fails closed until it is; the interface
makes that state visible rather than hiding it behind an empty player.

## The anonymous authorization chain

Nothing is taken from the request except the identifiers. Every link is re-proved
server-side, against live data:

```
Course is published and publicly eligible
        ↓
courses.live_revision_id
        ↓
the Lesson belongs to THAT revision
        ↓
allow_public_preview = true on that revision's row
        ↓
the Lesson carries the requested video asset version
        ↓
the logical asset is not retired or superseded
        ↓
media state = READY
        ↓
canonical video_renditions exist
```

Candidate revision data can never satisfy this resolver. Neither can a Lesson
that was previewable in a previous live revision and is not in the current one.

## READY only

Anonymous preview requires `READY`, not `PLAYABLE`. A `PLAYABLE` asset is
deliverable but holds an incomplete ladder, and the partial-delivery behaviour
that exists for entitled Students is not extended to anonymous visitors. A
non-`READY` preview fails closed with the existing inventory-safe unavailable
response, which reveals nothing about why.

## A separate token domain

The Student playback token is not reused and is not loosened. Preview gets its
own signature domain and its own claims.

| | Student playback token | Preview token |
|---|---|---|
| Student identity | yes | **none** |
| Device identity | yes | **none** |
| Playback lease | yes | **none** |
| Entitlement | proves one | grants none |
| Scope | student, lesson, asset version, device, lease | course, revision, lesson, asset version |

The domains are distinct HMAC domains over the same key, which is the pattern the
Admin review playback session already uses. The consequences are structural
rather than checked: a preview token presented to a protected Student playback
endpoint fails signature verification, and a Student token presented to a preview
endpoint fails the same way. Cross-course, cross-revision, cross-lesson and
cross-asset replay are all denied, because each of those identities is inside the
signed claims and re-proved against the live chain above on every request.

### TTL

The preview token's lifetime is the configured signature grace plus the
server-measured trusted duration of that exact video, clamped — the same
`playbackLifetime` rule protected playback uses, and for the same reason: an HLS
rendition manifest mints every segment URL up front, so a fixed short expiry
breaks a long lesson while the player is still open.

It is **not** given the 12-hour student ceiling merely because that ceiling
exists. A preview token is an anonymous bearer capability with no lease behind it
and nothing to revoke it mid-stream, so its lifetime is bounded by the length of
the thing being watched rather than by the longest lifetime the system tolerates
elsewhere. A normal Lesson plays to the end inside it; a 12-hour anonymous bearer
token would outlive the viewing by hours for no benefit.

## Delivery

Preview reuses the canonical machinery without exception:

- the same `video_renditions` rows and the same storage object keys as paid
  playback
- the dynamic master and dynamic rendition manifests
- signed, private segment access

It **must not** presign the original uploaded MP4, must not create a second
master, and must not transcode again. Marking a Lesson previewable creates no
`media_asset`, no `media_asset_version`, no `processing_attempt`, no
`video_rendition`, and writes no transcode outbox event. It is authorization
metadata, and there is a test whose only job is to prove that.

Response headers stay as they are for public preview: `Cache-Control: no-store`,
`Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`, and the
existing public preview rate limiting. There is no DRM, and the existing HLS
limitation is unchanged: segment URLs already issued cannot be revoked
mid-stream.

## Legacy compatibility

**Production has real legacy preview data in use.** The transition does not
disturb it.

- If a Course's live revision marks one or more Lessons previewable, the public
  curriculum exposes those Lessons and the new preview experience is used.
- If it marks none, the existing course-level legacy preview continues to serve
  exactly as it does today.
- `course_revisions.preview_asset_version_id` is **not** cleared when a Lesson
  preview goes live. It stays populated as rollback safety.
- No `PREVIEW` asset, version, or rendition is deleted.
- No existing preview is auto-mapped onto a Lesson.

`has_preview` on the public Course card stays, as a **derived** value: true when
the live revision has at least one previewable Lesson, or when a legacy course
preview exists. Existing catalogue UI keeps working without learning anything new.

The legacy authoring endpoints are marked transitional. They are not deleted in
this tranche, and no new architecture depends on them. Removal is a later
explicit tranche, after production observation.

## Analytics

Preview authorization issuance records the minimum: course id, lesson id, asset
version id, and an anonymous marker. No visitor fingerprinting, no visitor
identity, no conversion tracking, and no IP retention beyond what the existing
rate limiter already requires.
