# Phase 3C-B — manual enhancement recovery on schema 42

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

The schema-42 worker consumes the generic enhancement intent and claims
PLAYABLE under the existing database-time lease/fencing rules. The claim
atomically records its operation token and active kind ENHANCEMENT. It then
hashes and downloads the
exact object version, re-probes it, derives the frozen compiled ladder, reads
canonical `video_renditions`, and computes `expected - existing`. Existing
rows, including legacy NULL provenance rows, are never re-encoded or updated.

If the missing set is non-empty, the claimed ENHANCEMENT operation encodes only
those rungs. Each object is verified before its canonical row is
inserted with `processing_operation_id` for this operation. A failure leaves
already committed canonical rows in place, records a FAILED ENHANCEMENT, clears
the claim, and leaves the asset PLAYABLE for another explicit request.

If the missing set is empty, the worker changes its still-owned active kind to
FINALIZATION under the live lease. It performs no encode or storage write. It
records a zero-output SUCCEEDED FINALIZATION and atomically proves the complete
canonical ladder, sets `successful_processing_attempt_id`, and transitions
PLAYABLE to READY.

A crash before that classification records FAILED ENHANCEMENT: the durable
claim was still an enhancement request. A crash after classification records
FAILED FINALIZATION. Stale recovery uses only the kind and operation token on
the locked Asset Version; either failure leaves the asset PLAYABLE and its
committed canonical rows intact. A failed non-FULL attempt closes 41→40 rollback.

READY proof is asset-level: rows may span the original FULL operation and one or
more enhancement operations. Protected delivery continues to render its dynamic
master from each row's actual storage key; no common output prefix is assumed.

## Rollback floor

The first persisted ENHANCEMENT or FINALIZATION attempt, whether FAILED or
SUCCEEDED, permanently closes the schema-40 rollback floor. A 42→41 downgrade
may retain those historical rows once all active processing claims have settled;
it does not reopen 41→40. The supervised 41→40 command must still refuse.

The future cutover order is: keep 3C-A on clean schema 41, quiesce media
producers, apply 0042 only when no media work is active, verify clean schema 42,
then start the 3C-B worker and app. The 3C-B worker startup floor is 42. Its
API floor remains unchanged because the manual route writes the existing outbox
shape and does not acquire a processing claim.

Application rollback requires quiescing 3C-B, settling every active processing
claim, and proving no pending `media.enhancement_requested` outbox event and no
queued `media:enhancement` task remain. The deployed 3C-A dispatcher rejects
that event type and its worker does not register that task type. Only then may
the operator downgrade 42→41, verify clean 41, and restart the exact deployed
3C-A artifact `98e88fcc1105e8c638bb638d3f1c46630bcc51b2`. Its image,
manifest, tooling bundle and checksums must remain staged. Generic production
DOWN remains prohibited; a narrow supervised production 42→41 tool and its
intent-drain verification require separate review before release execution.

Manual QUEUE alone does not change publication eligibility. Once the worker
holds a live PLAYABLE claim, existing active-PLAYABLE publication semantics
apply. A failed or unclaimed PLAYABLE asset cannot newly satisfy that path.
No publication code changes are part of this phase.

## Verification boundary

This phase is implemented and tested locally only. It is not deployed and has
no production-observed enhancement or finalization evidence. No schema 43,
ladder change, publication redesign, public delivery widening, provenance
backfill, rendition overwrite, PROCESS_FAILED generic retry, or student-facing
retry action is introduced.
