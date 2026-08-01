# Data Model — S4 Media and Entitlement Evaluation

**Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md)

## Migration `0012_media_and_entitlement`

Additive. Modifies no existing migration file — their checksums are enforced by
`scripts/docs-guard.sh`, and `0001_init` onward are applied to real databases.

### Asset Version — immutable

| Field | Notes |
|---|---|
| id | |
| logical_asset_id | Groups versions; S2's references point at an **exact version**, not this |
| kind | `VIDEO`, `RESOURCE`, `LAB_MATERIAL`, `PREVIEW` |
| state | The state machine in [plan.md](plan.md#fail-closed-stated-as-a-state-machine-rather-than-as-an-adjective). `READY` is the only deliverable value |
| storage_object_key + storage_object_version | **Both.** The version is what a scan result binds to |
| trusted_duration_ms | Video only. S5 computes completion from this (BR-051) |
| created_at, immutable thereafter | Replacement creates a new row |

**No `UPDATE` to a completed Asset Version.** Enforced by constraint rather than convention — a
completed version is history, and S5's completion records reference the exact version that satisfied
them.

### Scan Result — bound to an object version, not an asset

| Field | Notes |
|---|---|
| asset_version_id | |
| attempt_number + work_id | A retry allocates a new append-only attempt for the same immutable object; one committed scan-work identity replays idempotently |
| storage_object_version | **Repeated deliberately.** A result is valid only for the object version it scanned |
| outcome | `PASSED`, `FAILED`, `ERROR` — no `SKIPPED`, no `NOT_CONFIGURED` |
| scanner_identity, scanned_at | Which scanner said so, and when |

**There is no outcome meaning "we did not scan but proceed."** The absence of a row is the absence of
a pass, and delivery requires a pass. That is the whole fail-closed guarantee expressed in the schema
rather than in a handler.

A unique constraint on `(asset_version_id, storage_object_version, attempt_number)` preserves each
historical retry attempt, while unique `work_id` makes duplicate delivery of one committed scan job
idempotent. The exact-version foreign key and the authoritative successful-attempt reference prevent
a stale result for replacement bytes from satisfying delivery.

### Legacy-converted asset posture

Legacy-converted rows remain deliberately **fail-closed and non-deliverable**. Migration `0012` does
not manufacture scan work for historical bytes, and historical `READY`, `PROCESSING`, or publication
flags are not grandfathered into delivery evidence. An Instructor must intentionally re-upload bytes,
or a future separately approved reprocessing procedure must be introduced; D7 provides neither an
automatic scan nor an automatic delivery path for those historical objects.

### Entitlement — the grant record S6 will later create

> **Amended 2026-07-29** by [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
> Entitlement creation moved from the removed S7 to **S6**, and Order provenance was replaced by the
> typed `grant_source` discriminator (BR-028, BR-113). The table below is the corrected shape. S4
> still creates **no** Entitlement.

| Field | Rule |
|---|---|
| id | |
| student_id | |
| scope_kind | `COURSE` in MVP. The column stays wide enough for `SECTION`, which is **not an acquirable scope** under D-045 (BR-021) |
| scope_id | The Course |
| grant_source | **Required, non-null.** The typed discriminator recording how access was granted. MVP implements `MANUAL_INVITATION` only (BR-028) |
| source_invitation_id | Required when `grant_source = 'MANUAL_INVITATION'` (BR-113). S6 adds the foreign key and check constraints — see [S6 data-model §4](../006-course-access-grant/data-model.md) |
| original_access_ends_at | Snapshotted from the Course at Admin Approval, immutable (BR-026) |
| access_ends_at | Effective, separately mutable by audited adjustment only (BR-026) |
| revoked_at | Null while active |
| retirement_eligibility_at | Set from the Admin Approval instant (BR-027) |

**`grant_source` is `NOT NULL` from this migration.** That constraint is the provenance invariant
written into the schema: it makes an Entitlement with no recorded origin **unrepresentable**, so the
S4 test seed cannot cheat and neither can a future support script. **There is no
`source_order_item_id` and no Order, payment, or checkout reference anywhere in this migration** —
D-045 removed in-platform payments, and D-045's `grant_source` seam is what a future paid source
would extend.

### Entitlement Adjustment — audit for expiry changes

Records old expiry, new expiry, reason, actor, timestamp, and any support or external-payment
reference, atomically with the change and with immutable audit evidence (BR-026). Moving expiry into
the past ends access immediately and **deletes nothing** — Enrollment, Progress, the Course Access
Invitation record, and adjustment history all survive.

## What this migration deliberately does not contain

- **No Entitlement creation routine, function, or default.** Creation is S6.
- **No `orders`, `order_items`, `payment_attempts`, `coupons`, or `refunds` table**, and no column
  referencing one. Those are deferred with in-platform payments under D-045.
- **No "grant" table, admin-grant record, or manual-grant audit type.** BR-028 permits creation only
  through Admin Approval of an accepted Course Access Invitation, which S6 builds; no schema here
  anticipates a manual-grant command.
- **No public-read grant on any storage object.** Objects are private; access is presigned only.

## Schema version

`db.MaxSchemaVersion` rises with this migration, and CI **derives** its assertion from that constant
via `migrate max-version` rather than carrying a literal — the drift that failed hosted CI during
S1B2.

## Down migration

Drops the new tables and types, leaving no orphaned enum. Verify `up` → `down` → `up` against real
PostgreSQL rather than by inspection.
