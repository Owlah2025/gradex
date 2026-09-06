# Media Upload Validation UX Design

## Scope

Make stored-byte container mismatches actionable for lesson-video and public-preview video upload completion without changing upload authorization, storage ordering, or server-side byte verification.

## Design

The media service will expose a dedicated typed reason only when the stored-byte probe recognizes a supported container that contradicts the declared content type. A declared `video/mp4` whose bytes begin as WebM/EBML will use `CONTENT_TYPE_MISMATCH`; empty, malformed, or otherwise unknown bytes remain on the existing generic validation path.

The shared HTTP media-problem mapper will translate that reason into the existing RFC 9457 `VALIDATION_FAILED` envelope with a safe field violation on `/content_type`. It will not expose signatures, storage keys, provider metadata, or wrapped internal error text. Lesson-video and public-preview completion routes will use the same mapping. Unknown validation errors retain the current generic response.

The frontend's centralized `describeApiError` will recognize the violation code and render localized English and Arabic MP4 guidance. Other violations continue through the existing detail/title fallback.

## Verification

Focused tests will cover:

- WebM/EBML bytes declared as MP4 → 422 with `CONTENT_TYPE_MISMATCH` and localized guidance.
- Valid MP4 bytes → unchanged successful completion path.
- Malformed/unknown bytes → generic safe validation response.
- Stored-byte inspection remains authoritative over filename and declared MIME.
- Both lesson-video and public-preview completion mapping, plus English and Arabic rendering.

Existing abandoned upload/quarantine cleanup was inspected and is not present; it is recorded as a separate follow-up risk and is not added to this hotfix.
