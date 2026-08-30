# Contract — Media Delivery and Upload API

**Spec**: [../spec.md](../spec.md) | **Plan**: [../plan.md](../plan.md)

**This contract is the authority.** In S1C every mounted staff route diverged from its frozen contract
on path, method, or both; the routes moved rather than the contract being rewritten to match what
shipped. Same rule here.

## Conventions

- Base path `/api/v1/media`. RFC 9457 Problem Details for errors.
- Every protected route is authenticated and **evaluated per request** — never per session.
- **No response caches a signed URL.** `Cache-Control: no-store` on every issuance.

## Upload — Instructor, owned Course only

`POST /api/v1/media/uploads` — begins a direct upload to **quarantine**. Returns a presigned upload
target plus an asset version identifier. Type and size are validated **before** the target is issued,
not after the bytes arrive.

`POST /api/v1/media/uploads/{id}/completions` — idempotent on a provider-supplied identifier.
Duplicate, delayed, and out-of-order calls converge to **one** Asset Version and are harmless.

Neither route makes anything deliverable. They move an object to `QUARANTINED`.

## Delivery — the only routes that read `READY`

`POST /api/v1/media/playback-authorizations` — issues short-lived **session-scoped** signed access to
the exact approved or historically qualifying Asset Version (BR-050, BR-100). Deliberately not
single-use: HLS re-requests segments on seek, rebuffer, and rendition switch.

`GET /api/v1/media/playback-manifests/{playbackSession}/index.m3u8` — authenticates the Student,
revalidates the exact approved Asset Version and current Entitlement, and returns an adaptive HLS
master generated from that version's persisted rendition metadata. Every child URI is a protected
same-origin route; the response contains no storage key or storage URL.

`GET /api/v1/media/playback-manifests/{playbackSession}/renditions/{rendition}/index.m3u8` — repeats
the authenticated Student, playback-session, exact-version, readiness, provenance, and current
Entitlement checks. The rendition selector must match exactly one persisted rendition for that Asset
Version; it is never converted into a storage path. The route reads only that small private media
playlist and rewrites each supported media-segment reference to an exact-object presigned URL whose
expiry is no later than the playback session. Both manifest routes return `Cache-Control: no-store`
and proxy no segment/video bytes.

`POST /api/v1/media/download-authorizations` — issues signed access to a protected Resource or Lab
Material. Lab Material URLs MAY be single-use. Lab Materials carry the opaque buyer tag; Lesson
Resources do not (BR-103).

`GET /api/v1/media/previews/{id}` — public preview, **anonymous**, available only after validation,
quarantine, scan success, and Instructor publication confirmation (BR-144).

### Stable learning-material browser entry points (D-064)

S4 also mounts the following same-origin, protected navigation routes:

- `GET /api/v1/media/lessons/{lessonId}/materials/resource`
- `GET /api/v1/media/lessons/{lessonId}/materials/lab-material`

They resolve the current material Asset Version for the stable Lesson identity internally, then
authenticate the active Student, evaluate current Enrollment/Entitlement and retirement/readiness
policy, and sign only after authorization succeeds. Success is `302 Found` with a fresh `Location`,
`Cache-Control: no-store`, and `Referrer-Policy: no-referrer`; the signed target is not returned in
JSON or a body and is never persisted or cached. Failure uses the same uniform protected-unavailable
`404 application/problem+json` with `Cache-Control: no-store` and no `Location`.

These routes do not accept an Asset Version ID and do not mutate Enrollment, Entitlement, Progress,
Asset Version, or material lifecycle state. They share the authoritative resolver, evaluator, and
signer with `POST /api/v1/media/download-authorizations`. S5 may render only these fixed entry
points; it never signs, proxies, or exposes storage details.

For S5 read models, S4 exposes a bounded read-only bulk classification that returns only whether
`resource` or `lab_material` is available for current stable Lesson identities. It never returns
Asset Version IDs, storage keys, signed targets, expiry, readiness internals, or capability state.

### The single refusal

Every one of these causes returns an **identical** response — same status, headers, schema, and body:

- no Entitlement covering the target
- Entitlement expired on its **effective** expiry
- Entitlement revoked
- Account suspended
- Course under emergency access suspension
- target retired without grandfathering
- **asset does not exist**
- asset exists but is not `READY`

Produced by one constructor. A handler may not build another. **No `403` exists on this surface** — a
`403` answers the question the refusal exists to refuse, and the set above is a content inventory if
it is distinguishable.

Internally each cause is typed and audited. Externally there is one refusal.

## What this contract does not contain

**No route that creates an Entitlement.** Not an admin grant, not a support repair, not a seeded
fixture endpoint. Entitlement creation is S6 (BR-028,
[SLICES §3.1](../../../docs/launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation)).
If implementation appears to need one, that is a finding against [../spec.md](../spec.md).

**No publicly readable object.** Every byte is presigned and time-bounded.
