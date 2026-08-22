# LG-014 Admin Catalogue Mode

> **Status after D-088:** retained as a scanner-gated fallback, not the default
> launch authoring path.

D-088 defers mandatory malware scanning only for the bounded trusted-Instructor
Lesson-media profile. MP4 Lesson video and PDF/DOCX Lesson Resources in that
profile use the separately authorized exact-version validation path and do not
claim scan evidence.

`ADMIN_CATALOGUE` remains available for content that still requires malware
scanning, including public previews and uploads outside the D-088 boundary, or
for an intentional Admin-operated out-of-band scanning procedure. It preserves
the fail-closed rule for that scanner-required path: no bytes become deliverable
without exact-version successful scan evidence.

## Configuration and startup validation

Set `MEDIA_OPERATING_MODE=ADMIN_CATALOGUE` when intentionally using this
scanner-gated fallback. The API accepts `SCANNER`, `ADMIN_CATALOGUE`, or
`TRUSTED_INSTRUCTOR`; an absent value selects `SCANNER`, and an unknown value
prevents startup. This remains a deployment-owned operating switch, not a
runtime feature flag or an emergency source edit.

The D-088 `TRUSTED_INSTRUCTOR` support has landed: the `VALIDATED` state,
append-only `validation_attempts` evidence, the `successful_validation_attempt_id`
provenance column and its database enforcement, the service upload/retry paths,
mode-agnostic worker processing, and configuration validation. The
production-like Compose default remains `ADMIN_CATALOGUE`, so a stack started
without the acceptance environment file cannot accept unscanned uploads by
accident; `deploy/env/production-like.env.example` selects `TRUSTED_INSTRUCTOR`
for the controlled D-088 acceptance run. Manual acceptance of that run is still
outstanding — see [`STATUS.md`](STATUS.md).

### What each mode does with an Instructor upload

| Mode | Instructor direct upload | Evidence required before delivery |
| --- | --- | --- |
| `SCANNER` | Allowed for every BR-067 type | Exact-version successful malware scan |
| `ADMIN_CATALOGUE` | Refused | Admin-recorded exact-version out-of-band scan evidence |
| `TRUSTED_INSTRUCTOR` | Allowed **only** for MP4 Lesson video and PDF/DOCX Lesson Resources | Exact-version validation (size, actual size, real format, SHA-256); video additionally requires trusted FFmpeg evidence |

In `TRUSTED_INSTRUCTOR` mode an upload outside that profile — a public preview,
a Lab Material, an image or slide Resource, a QuickTime video — is refused at
intent rather than accepted and left quarantined. That deployment has no
scanner, so accepting it would produce an object that could never become
deliverable and no message the Instructor could act on. Use `ADMIN_CATALOGUE`
for that content instead.

In `ADMIN_CATALOGUE` mode, Instructor upload requests are refused. An active
Admin with `ADMIN_OPERATIONS` capability uses the catalogue-load routes:

1. `POST /api/v1/media/catalogue-loads` creates a private quarantine upload
   target.
2. `POST /api/v1/media/catalogue-loads/{id}/completions` verifies size,
   bounded type evidence, and the complete storage checksum before recording
   `QUARANTINED` state.
3. `POST /api/v1/media/assets/{id}/out-of-band-scan-evidence` records a
   method, provider, reference, actor, timestamp, audit event, and the exact
   immutable storage object version.

The evidence endpoint rejects a different or replacement object version and
never reuses an earlier scan result. Video evidence moves only to
`SCAN_PASSED` and schedules durable transcoding; non-video evidence moves
through `SCAN_PASSED` to `READY`. No evidence path creates an Entitlement.

## Admin procedure and audit

Before recording evidence, the Admin retains the external scan provider's
reference and verifies the uploaded object identity against the quarantine
completion record. The system writes `MEDIA_CATALOGUE_LOADED` and
`MEDIA_OUT_OF_BAND_SCAN_RECORDED` audit events, including the exact object
version, method/provider/reference, and attempt identity. Audit evidence is
the source for incident investigation; the request body and a former logical
Asset Version are not substitutes.

If evidence is missing, malformed, for the wrong version, or cannot be
persisted, the Asset Version remains non-deliverable. Public previews remain
unavailable until exact scan evidence, `READY`, and live Instructor publication
confirmation exist.

## Reading the two provenance paths apart

An Asset Version records which safety path admitted it, and the two are never
merged:

- `successful_scan_attempt_id` → a scanner inspected these exact bytes;
  `scan_attempts` holds the outcome and scanner identity.
- `successful_validation_attempt_id` → **no malware scan was performed**;
  `validation_attempts` holds the exact-version validation outcome, the
  validator identity, and the `D-088-TRUSTED-INSTRUCTOR` profile label.

The media diagnostic artifact reports the same distinction in its `provenance`
field and its separate `validations` list. Never describe a
`TRUSTED_VALIDATION` asset as scanned or malware-free.

The database refuses to let one stand in for the other: `SCAN_PASSED` requires
scan evidence specifically, `VALIDATED` requires validation evidence
specifically, and validation provenance is rejected outright for a `PREVIEW` or
`LAB_MATERIAL` kind or any content type outside the D-088 allowlist.

## Returning to scanner mode

After a scanner provider is approved and wired through the existing adapter,
deploy with `MEDIA_OPERATING_MODE=SCANNER`. Existing unscanned rows remain
unscanned: changing mode never retroactively approves them, and an Asset
Version that carries D-088 validation provenance keeps it — leaving that mode
does not convert past validation into a scan.

An Admin retry follows the asset's own provenance, not the current mode: a
scanner-gated asset gets a fresh scan-work intent, and a D-088 asset re-runs
the full exact-version validation from scratch. If the deployment has left
`TRUSTED_INSTRUCTOR`, a retry of a D-088 asset is refused rather than routed
into a scanner that never saw it. Reprocess only through that audited retry or
an intentional new upload; never mark historical bytes READY by configuration
change.
