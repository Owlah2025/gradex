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

`POST /api/v1/media/download-authorizations` — issues signed access to a protected Resource or Lab
Material. Lab Material URLs MAY be single-use. Lab Materials carry the opaque buyer tag; Lesson
Resources do not (BR-103).

`GET /api/v1/media/previews/{id}` — public preview, **anonymous**, available only after validation,
quarantine, scan success, and Instructor publication confirmation (BR-144).

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
fixture endpoint. Entitlement creation is S7 (BR-028,
[SLICES §3.1](../../../docs/launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation)).
If implementation appears to need one, that is a finding against [../spec.md](../spec.md).

**No publicly readable object.** Every byte is presigned and time-bounded.
