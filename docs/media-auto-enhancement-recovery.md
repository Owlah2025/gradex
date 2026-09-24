# Automatic enhancement recovery (3C-C)

A `PLAYABLE` video is deliverable but incomplete: its canonical HLS ladder is
missing at least one rung, or it holds a complete ladder that was never accepted
as `READY`. Phase 3C-B gave an Administrator one manual action to finish such an
asset — `RetryEnhancements`. 3C-C schedules that same action automatically.

It ships **disabled**, in every environment, and stays disabled until one real
3C-B manual recovery has been observed end-to-end in production. See
[Activation](#activation).

## What this is not

- It is **not** a second recovery engine. The scheduler writes the same
  `media.enhancement_requested` outbox event and the same `media:enhancement`
  task that the Admin action writes, and execution runs the existing
  `Worker.RetryEnhancements` path. There is no second FFmpeg invocation, no
  second claim implementation, and no second `READY` proof.
- It **never takes the media work claim**. The execution-time worker remains the
  only claimant of `media_asset_versions.work_claim_token`. The scheduler only
  writes intent.
- It is **not** Redis-authoritative. Whether automatic recovery is outstanding
  for an asset is a fact in PostgreSQL. Losing Redis loses queued tasks, not the
  scheduler's knowledge of what it asked for.

## The linkage problem

Automatic recovery is only safe if the scheduler can attribute an execution
outcome to the intent that caused it. Proximity in time is not attribution, and
"there is an open automatic row for this asset" is not attribution either —
both would let a manual retry, a stale-recovery terminalization, or a second
worker's operation close out an automatic attempt that never ran.

The difficulty is that enhancement execution decides its own operation identity.
`appendEnhancementWork` carries only the Asset Version; the worker mints
`operationID` at claim time, precisely so that claim, source proof and ladder
planning are decided against current database truth rather than against a stale
queue payload. So the intent cannot know the operation id in advance, and the
operation cannot know the intent unless it is told.

### The durable chain

```
media_auto_enhancement_recovery row            (PK: asset_version_id)
    current_intent_id  ─────────────┐          committed by the scheduler
                                    │          in ONE transaction with…
outbox_events row (id = intent id) ─┘
    payload: {asset_version_id, auto_recovery_intent_id}
        │
        │  dispatcher (unchanged)
        ▼
media:enhancement task
        │
        │  worker claim transaction — ONE transaction that both
        │  takes the media claim AND binds the intent:
        ▼
media_auto_enhancement_recovery
    state = 'EXECUTING'
    executing_operation_id = <the operation the worker just minted>
        │
        ▼
processing_attempts row (asset_version_id, operation_id) terminal
    auto_recovery_intent_id  ← recorded on the attempt for audit
```

Every edge is a committed PostgreSQL fact:

| Question | Answered by |
|---|---|
| Which automatic intent caused this execution? | `media_auto_enhancement_recovery.current_intent_id`, bound to the operation in the claim transaction |
| Which operation belongs to that intent? | `media_auto_enhancement_recovery.executing_operation_id` |
| Did it start? | `state = 'EXECUTING'` — written by the claim itself, so it cannot be true without a claim |
| Did it make progress? | canonical `video_renditions` count against `progress_rendition_count`, the baseline the intent recorded |
| Did it succeed, fail, or finalize? | the `processing_attempts` row for `(asset_version_id, executing_operation_id)` |
| Which automatic attempt number was it? | `attempt_number` |
| What failure category ended it? | `last_failure_category` |

The intent id **is** the outbox event id. One identity, generated once, written
in the transaction that creates both rows, so the scheduler row can never name an
intent that was not committed and the event can never exist without its row.

### Why this survives the failure modes

- **Worker crash after claim.** `executing_operation_id` is already committed, so
  the operation is still attributable. `RecoverStale` terminalizes the stale
  `PLAYABLE` claim by its `work_claim_token`, which is that same operation id, and
  reconciles the scheduler row to a budgeted `WORKER_INTERRUPTED` failure.
- **Worker crash before claim.** Neither the claim nor the binding committed, so
  nothing ran and nothing is charged. The row is still `SCHEDULED`; see
  superseded intents below.
- **Redis loss.** The task is gone; the scheduler row is not. `SCHEDULED` past
  its `intent_expires_at` is recoverable, so the asset is scheduled again.
- **Task retry / duplicate delivery.** A task whose `auto_recovery_intent_id`
  does not equal the row's `current_intent_id` — including the case where no row
  exists — is a **superseded intent** and is a no-op. It does not claim, and it
  charges nothing. This is what makes late delivery, re-emission after
  `intent_expires_at`, and manual override all safe at once.
- **Process restart.** No scheduler state is process-local. Backoff deadlines are
  `TIMESTAMPTZ` compared against the database clock.
- **Two scheduler instances.** `asset_version_id` is the primary key, candidate
  selection is `FOR UPDATE … SKIP LOCKED`, and scheduling is a conditional
  `INSERT … ON CONFLICT … DO UPDATE … WHERE` compare-and-set. One intent wins;
  the other instance writes nothing.

### Backward compatibility

`auto_recovery_intent_id` is an **optional** field on `EnhancementWork`
(`omitempty`). Manual work omits it and decodes exactly as it does today, so the
currently deployed 3C-B task shape stays valid and in-flight manual tasks are
unaffected by the upgrade. Automatic work always carries it. A task that carries
no intent id is manual by definition and is never attributed to an automatic
attempt.

`processing_attempts.auto_recovery_intent_id` is nullable, because every attempt
that exists before schema 45 was either manual or a `FULL` transcode.

## Scheduler state

One row per Asset Version, created only when an intent is first scheduled.
**No row means eligible** — there is no `IDLE` state to write, so a repeated scan
of an asset that has never been scheduled cannot accumulate rows.

| State | Meaning | Invariant |
|---|---|---|
| `SCHEDULED` | intent committed, not yet claimed | `current_intent_id` set, `executing_operation_id` null, `intent_expires_at` set |
| `EXECUTING` | a worker holds the claim for this intent | both `current_intent_id` and `executing_operation_id` set |
| `BACKOFF` | nothing outstanding, eligible again at `next_attempt_at` | no intent, no operation, `next_attempt_at` set, `consecutive_failures` below the budget |
| `NEEDS_OPERATOR` | automatic recovery has given up | no intent, no operation, no `next_attempt_at` |

The contradictory combinations are refused by a CHECK constraint rather than by
Go, so no code path can leave the row claiming to execute an operation it never
bound.

On a successful `READY` the row is **deleted**. Automatic recovery is finished
for that asset, it can never be eligible again (eligibility requires `PLAYABLE`),
and a permanent outstanding row would be a false claim. The audit trail of what
happened lives in `processing_attempts` and in the audit log, not in scheduler
state.

## Eligibility

A candidate must be a settled, recoverable, unclaimed `PLAYABLE` video:

- `kind = 'VIDEO'`, `state = 'PLAYABLE'`, owning asset not retired
- `work_claim_token IS NULL` **and** no live `work_lease_expires_at`
- a non-empty `sha256_hex` (immutable source identity)
- successful scan or validation provenance
- either no scheduler row, or a `BACKOFF` row whose `next_attempt_at <= now()`,
  or a `SCHEDULED` row past `intent_expires_at`
- `next_attempt_at` compared against the **database** clock

`READY`, `PROCESSING`, `PROCESS_FAILED`, `UPLOADED`, `SCANNING`, retired media,
non-video kinds, and assets with incomplete evidence are all excluded.

The case that matters most is a **normal progressive `FULL` operation that has
reached `PLAYABLE` while still holding its claim**. That is healthy in-flight
work, not a recovery candidate, and `work_claim_token IS NULL` excludes it. At
schema 42 and later such a row additionally carries
`active_processing_attempt_kind`, which the same claim predicate covers.

## Attempt policy

Three consecutive automatic failures, then `NEEDS_OPERATOR`:

| Consecutive failures | Next automatic attempt |
|---|---|
| 1 | 15 minutes |
| 2 | 1 hour |
| 3 | 4 hours |
| 4 | none — `NEEDS_OPERATOR` |

**Progress resets the budget.** If an automatic execution commits at least one
new canonical `video_renditions` row against the baseline the intent recorded,
that is real forward movement even if the operation later failed, and
`consecutive_failures` returns to zero. Progress is read from canonical database
evidence; FFmpeg output is never consulted for it.

Failure classification is the existing taxonomy, mapped truthfully:

| Category | Automatic behaviour |
|---|---|
| `STORAGE_UNAVAILABLE`, `PROCESS_TIMEOUT`, `WORKER_INTERRUPTED`, `TRANSCODE_FAILED` | retried, budgeted |
| `INVALID_MEDIA`, checksum mismatch, missing immutable source, DB invariant contradiction | `NEEDS_OPERATOR` immediately, never looped |
| `ErrEnhancementNotEligible`, `ErrEnhancementActive` (claim race) | not charged; the row returns to `BACKOFF` with its budget unchanged |

A claim race means another worker owns the work. Charging an attempt for it would
punish the scheduler for being correct.

## Manual override

`RetryEnhancements` remains available **always**, including from
`NEEDS_OPERATOR` and after the automatic budget is exhausted. It is the operator
override, and nothing in 3C-C can take it away.

When a manual enhancement claims an asset, the scheduler row is reset to
`BACKOFF` with `consecutive_failures = 0`, no outstanding intent, and
`next_attempt_at` one backoff step out. That single write does three things: it
resets the automatic budget, it supersedes any automatic task still queued for
the old intent, and it stops the scheduler from immediately minting a duplicate
intent behind the operator's back. If the manual run reaches `READY`, the row is
deleted with every other success.

Claim semantics are untouched. If an automatic task and a manual task race, the
database claim decides which one runs; the loser is a no-op.

## Feature flag

`MEDIA_AUTO_ENHANCEMENT_RECOVERY_ENABLED` — default **`false` in every
environment**, including development. Tests enable it explicitly.

When it is false the reconciler loop does not start. There is no periodic
candidate `SELECT`, no scheduler write, and no outbox intent. The flag is not the
only gate: the reconciler also requires schema ≥ 45, because the table it is
authoritative over arrives in 0045.

The worker's media base floor stays at 42. Automatic recovery is an additional
capability gated separately, not a reason to refuse to start a worker that is
serving media correctly.

## Activation

3C-C may be implemented and deployed with the flag off. It may **not** be enabled
in production until one legitimate real 3C-B manual operation — an `ENHANCEMENT`
or a `FINALIZATION` — has been observed end-to-end in production.

That observation must be a real operational event. It is not to be manufactured:
no synthetic broken video, no deliberately failed FFmpeg run, no production
`RetryEnhancements` invoked to produce evidence. As of this document 3C-B is
**deployed, safe, and not yet production-observed**.
