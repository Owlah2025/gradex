# Phase 3C-B — manual enhancement recovery

**Status:** implementation only; no deployment or production observation.

The production baseline remains the deployed 3C-A artifact
`98e88fcc1105e8c638bb638d3f1c46630bcc51b2` on clean schema 41. The complete
3C-A image, manifest, tooling bundle and checksums remain the application
rollback floor and must stay staged.

## Manual-only contract

An authenticated Admin may `POST /api/v1/media/assets/:id/retry-enhancements`.
The request accepts only a current, non-retired VIDEO Asset Version in PLAYABLE
with no claim. It writes an audit event and a durable
`media.enhancement_requested` outbox event containing only `asset_version_id`.
It does not claim the asset, set a lease, probe media, choose rungs, or change
state. Duplicate requests are allowed because execution-time claim and the
database's append-only/unique constraints make the work safe.

The worker consumes the generic enhancement intent and claims PLAYABLE under the
existing database-time lease/fencing rules. It then hashes and downloads the
exact object version, re-probes it, derives the frozen compiled ladder, reads
canonical `video_renditions`, and computes `expected - existing`. Existing
rows, including legacy NULL provenance rows, are never re-encoded or updated.

If the missing set is non-empty, the worker creates an ENHANCEMENT operation and
encodes only those rungs. Each object is verified before its canonical row is
inserted with `processing_operation_id` for this operation. A failure leaves
already committed canonical rows in place, records a FAILED ENHANCEMENT, clears
the claim, and leaves the asset PLAYABLE for another explicit request.

If the missing set is empty, the worker performs no encode or storage write. It
records a zero-output SUCCEEDED FINALIZATION and atomically proves the complete
canonical ladder, sets `successful_processing_attempt_id`, and transitions
PLAYABLE to READY.

READY proof is asset-level: rows may span the original FULL operation and one or
more enhancement operations. Protected delivery continues to render its dynamic
master from each row's actual storage key; no common output prefix is assumed.

## Rollback floor

The first persisted ENHANCEMENT or FINALIZATION attempt, whether FAILED or
SUCCEEDED, permanently closes the schema-40 rollback floor. From that point,
schema 41 remains in force and a future 3C-B application rollback targets the
exact deployed 3C-A artifact `98e88fcc1105e8c638bb638d3f1c46630bcc51b2` on
schema 41. The supervised 41→40 command must refuse because an attempt kind is
non-FULL. No automatic rollback, scheduler, retry scan, backoff or 3C-C
bounded-growth mechanism belongs to this phase.

## Verification boundary

This phase is implemented and tested locally only. It is not deployed and has
no production-observed enhancement or finalization evidence. No schema 42,
ladder change, publication redesign, public delivery widening, provenance
backfill, rendition overwrite, PROCESS_FAILED generic retry, or student-facing
retry action is introduced.
