# LG-014 Admin Catalogue Mode

Use this documented operating mode only while the production malware-scanner
provider gate (`LG-014`) remains unresolved. It preserves the S4 fail-closed
rule: no unscanned historical, uploaded, or replacement bytes become
deliverable.

## Configuration and startup validation

Set `MEDIA_OPERATING_MODE=ADMIN_CATALOGUE`. The API accepts only `SCANNER` or
`ADMIN_CATALOGUE`; an absent value selects `SCANNER`, and an unknown value
prevents startup. This is a deployment-owned operating switch, not a runtime
feature flag or an emergency source edit.

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

## Returning to scanner mode

After a scanner provider is approved and wired through the existing adapter,
deploy with `MEDIA_OPERATING_MODE=SCANNER`. Existing unscanned rows remain
unscanned: changing mode never retroactively approves them. Reprocess only
through a separately audited retry or an intentional new upload; never mark
historical bytes READY by configuration change.
