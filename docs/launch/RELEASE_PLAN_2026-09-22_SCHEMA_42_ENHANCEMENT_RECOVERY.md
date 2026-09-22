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
manifest, tooling bundle and checksums must remain staged, and both the cutover
and the rollback refuse if any of it is missing. Generic production DOWN remains
prohibited. The narrow supervised production 42→41 tool and its exhaustive
intent-drain proof are specified and implemented below; they remain subject to
release review before any execution.

Manual QUEUE alone does not change publication eligibility. Once the worker
holds a live PLAYABLE claim, existing active-PLAYABLE publication semantics
apply. A failed or unclaimed PLAYABLE asset cannot newly satisfy that path.
No publication code changes are part of this phase.

## Forward cutover

Run `host.sh up-core-schema-42-enhancement-recovery` from the imported schema-42
tooling bundle. It is a single command because the order is the contract:

1. Validate production scope, the release artifact and its schema-42 capability
   marker, the backend image ceiling (exactly 42), the 0042 UP/DOWN migration
   hashes, and the presence of the `gradex-enhancement-drain` proof in the image.
2. Validate that the complete deployed 3C-A artifact floor
   `98e88fcc1105e8c638bb638d3f1c46630bcc51b2` is still staged and still
   validates. **No way back means no way forward:** the command refuses if any
   part of that set is missing or fails its checksums.
3. Require the api and worker containers to be absent, and prove no other local
   producer targets the production database.
4. Start PostgreSQL and Redis, then prove zero active media claims and a clean
   schema 41.
5. Apply 0042 and verify a clean schema 42.
6. Only then start the api, worker and frontend.

**Do not start the 3C-B API before schema 42 is verified clean.** Request-time
semantics are harmless on schema 41 — the manual route writes the existing outbox
shape and takes no claim — but an enhancement intent written before the schema-42
worker exists is intent with nothing able to execute it. Migrate first, verify
42, then start. The 3C-B worker's own startup floor is 42 and it refuses to run
on 41; that floor stays.

## Application rollback — supervised 42 → 41

Generic production `down` remains prohibited and no flag is added to change that.
The one authorized downgrade at this boundary is
`host.sh rollback-schema-42-enhancement-recovery`, which forwards exactly:

```
gradex-migrate rollback-schema-42 -confirm-production=schema-42-to-41
```

No target version, no step count, no 42 → 40. It requires a clean schema 42 and
refuses a dirty marker or any other version. It ends at a clean 41 and stops
there. Starting the 3C-A application is a separate, deliberate act afterwards.

The 3C-B candidate backend image stays selected for the whole downgrade: the
3C-A image contains neither migration 0042 nor the command that reverts it.
Switch the runtime selection to `98e88fcc1105e8c638bb638d3f1c46630bcc51b2` only
after the marker reads a clean 41.

### Enhancement drain — a hard gate, not a warning

Before the schema moves, prove there is **zero** incompatible enhancement work in
every durable location:

| Location | Read by | Blocks rollback |
| --- | --- | --- |
| `media.enhancement_requested` outbox events with no dispatch receipt | `gradex-migrate rollback-schema-42` preflight and `gradex-enhancement-drain` | yes |
| `media:enhancement` asynq tasks: **pending** | `gradex-enhancement-drain` | yes |
| `media:enhancement` asynq tasks: **active** | `gradex-enhancement-drain` | yes |
| `media:enhancement` asynq tasks: **scheduled** | `gradex-enhancement-drain` | yes |
| `media:enhancement` asynq tasks: **retry** | `gradex-enhancement-drain` | yes |
| `media:enhancement` asynq tasks: **archived** (dead-letter) | `gradex-enhancement-drain` | yes |
| `media:enhancement` asynq tasks: **aggregating** | `gradex-enhancement-drain` | yes (no aggregation is configured, so this set is empty; it is read, not assumed) |
| `media:enhancement` asynq tasks: **completed** | `gradex-enhancement-drain` | no — finished work needs no schema-42 producer |
| Active ENHANCEMENT/FINALIZATION processing claims | `CheckActiveProcessingKindRollbackSafety`, `CheckNoActiveMediaClaims`, and the host's own claim count | yes |
| Historical terminal ENHANCEMENT/FINALIZATION attempts | — | no — they stay representable on schema 41 |

A dispatched intent is **not** finished work. Its outbox row carries a dispatch
receipt and disappears from the database gate while its queue task is still
waiting, so both halves are required and the queue half runs in the same
candidate image through `gradex-enhancement-drain`.

If incompatible work exists the rollback **refuses** and reports counts and
identifiers. It deletes nothing: discarding work an Administrator requested is an
operator decision with its own evidence, never a side effect of a downgrade. It
also never rewrites processing evidence to make a gate pass.

### Why one surviving outbox event is a hard gate

The deployed 3C-A media dispatcher has no case for `media.enhancement_requested`.
It does not reject-and-skip that row. `dispatchEvent` returns `unsupported media
outbox event`, `DispatchPending` returns that error, and **the batch aborts**.
The feeding query is ordered by `occurred_at` and selects only rows without a
dispatch receipt, so the surviving event is the first row of every subsequent
batch as well. One such event therefore stops **all later media outbox dispatch**
— scans and transcodes included — permanently.

`outbox_events` is append-only: the blocking row cannot be deleted or edited.
Clearing it after the fact requires an explicit operator decision to record a
dispatch receipt in `media_outbox_dispatches`, retiring the intent without
destroying the evidence that it existed — a deliberate act on a live production
database, under an outage of all media processing.

That is why zero undispatched `media.enhancement_requested` rows is a blocking
rollback prerequisite rather than a warning: proving zero beforehand is the only
ordering that does not trade a rollback for that outage.

### No cascading rollback

The dedicated command ends at a clean 41 and never continues to 40. The
theoretical 41 → 40 capability is irrelevant to normal 3C-B operation, and once
any non-FULL attempt exists — FAILED included — the supervised 41 → 40 command
refuses outright. The application rollback boundary is:

```
3C-B / schema 42  ->  drain + quiesce  ->  42 -> 41  ->  3C-A / schema 41   [STOP]
```

This is the boundary both before and after the first non-FULL attempt. After one
exists, `78ee4227` / schema 40 is closed by historical evidence.

## Forward failure matrix

| # | Failure point | Schema | Safe runnable application | Recovery |
| --- | --- | --- | --- | --- |
| A | Refused before the old services are removed | 41 clean | 3C-A `98e88fcc` (still running) | Fix the reported gate and re-run the cutover. Nothing changed. |
| B | Refused after removal, before migration | 41 clean | 3C-A `98e88fcc` | Start 3C-A again with its retained release selection, or fix the gate and continue. |
| C | 41 → 42 migration fails | 41, possibly dirty | 3C-A `98e88fcc` only if the marker reads a clean 41 | Read the real marker. Do not force or repair it automatically. Resolve a dirty marker by hand before any application starts. |
| D | Clean 42 but the 3C-B application fails to start | 42 clean | **neither** 3C-A nor a healthy 3C-B | Run the supervised 42 → 41 rollback (drain gates apply), verify clean 41, then select and start `98e88fcc`. |
| E | 3C-B healthy, then a regression is observed | 42 clean | 3C-B, degraded | Quiesce producers, settle claims, drain enhancement work, run 42 → 41, verify clean 41, then start `98e88fcc`. |
| F | Rollback drain refuses | 42 clean | 3C-B (quiesced) | Resolve the reported outbox events and queue tasks by operator decision, then retry. Never delete them to pass the gate. |
| G | 42 → 41 DOWN fails | 42, possibly dirty | none until the marker is resolved | Read the real marker. No force, no automatic repair, no evidence rewrite, and do not start 3C-A. |
| H | 3C-A artifact floor missing | unchanged | whatever is running | The cutover and the rollback both refuse. Restore the complete floor artifact set before either is attempted. |

**Absolute rule:** the 3C-A application `98e88fcc1105e8c638bb638d3f1c46630bcc51b2`
must never start against schema 42.

## Artifact-backed release, no production Git

The schema-42 bundle extends the existing no-Git artifact model. It carries the
0042 UP and DOWN migrations, the updated `host.sh` and `release-artifact.sh`, the
schema-41 and schema-42 rollback guards, the enhancement drain tooling, and every
relative dependency the wrapper resolves. Its metadata declares exactly one
boundary marker:

```
SCHEMA42_CAPABILITY=manual-enhancement-v1
```

A bundle declaring the schema-41 marker, both markers, or an unrecognized value
is a mixed or stale bundle and every command refuses it. The schema-41
commands — `up-core-schema-41-foundation` and `rollback-schema-41-foundation` —
stay narrow one-release contracts: they require the schema-41 marker and a
backend image whose ceiling is exactly 41, so they refuse this release's image.
That is intentional and must not be relaxed.

Artifact validation proves, together: the bundle capability marker, the compiled
backend command surface (which must include `rollback-schema-42`), the presence
of `gradex-enhancement-drain` in the image, the 0042 UP and DOWN migration hashes
bound between bundle and image, and an image schema ceiling of exactly 42.

## Verification boundary

This phase is implemented and tested locally only. It is not deployed and has
no production-observed enhancement or finalization evidence. No schema 43,
ladder change, publication redesign, public delivery widening, provenance
backfill, rendition overwrite, PROCESS_FAILED generic retry, or student-facing
retry action is introduced.
