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
| storage_object_version | **Repeated deliberately.** A result is valid only for the object version it scanned |
| outcome | `PASSED`, `FAILED`, `ERROR` — no `SKIPPED`, no `NOT_CONFIGURED` |
| scanner_identity, scanned_at | Which scanner said so, and when |

**There is no outcome meaning "we did not scan but proceed."** The absence of a row is the absence of
a pass, and delivery requires a pass. That is the whole fail-closed guarantee expressed in the schema
rather than in a handler.

A unique constraint on `(asset_version_id, storage_object_version)` makes duplicate scan callbacks
harmless and makes a stale result for a replaced object structurally unable to satisfy delivery.

### Entitlement — the grant record S7 will later create

| Field | Rule |
|---|---|
| id | |
| student_id | |
| scope_kind | `COURSE` or `SECTION` (BR-021) |
| scope_id | The Course or Section |
| source_order_item_id | **Required, non-null.** Every Entitlement references exactly one Order Item (BR-028) |
| original_access_ends_at | From the source Order, immutable (BR-026) |
| access_ends_at | Effective, separately mutable by audited adjustment only (BR-026) |
| revoked_at | Null while active |
| retirement_eligibility_at | Copied from the Order's `accepted_at` (BR-027) |

**`source_order_item_id` is `NOT NULL` from this migration**, even though S7 has not yet built the
producer. That constraint is the provenance invariant written into the schema: it makes an Entitlement
without an Order **unrepresentable**, so the seed mechanism cannot cheat and neither can a future
support script.

### Entitlement Adjustment — audit for expiry changes

Records old expiry, new expiry, reason, actor, timestamp, and any support or refund reference,
atomically with the change and with immutable audit evidence (BR-026). Moving expiry into the past
ends access immediately and **deletes nothing** — Enrollment, Progress, Order, and adjustment history
all survive.

## What this migration deliberately does not contain

- **No Entitlement creation routine, function, or default.** Creation is S7.
- **No "grant" table, admin-grant record, or manual-grant audit type.** BR-028 forbids creation
  through a manual command, so no schema anticipates one.
- **No public-read grant on any storage object.** Objects are private; access is presigned only.

## Schema version

`db.MaxSchemaVersion` rises with this migration, and CI **derives** its assertion from that constant
via `migrate max-version` rather than carrying a literal — the drift that failed hosted CI during
S1B2.

## Down migration

Drops the new tables and types, leaving no orphaned enum. Verify `up` → `down` → `up` against real
PostgreSQL rather than by inspection.
