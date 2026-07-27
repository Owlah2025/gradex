# Data Model — S3 Public Catalogue

**Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md)

## S3 defines no domain entity

Every field S3 renders is owned elsewhere:

| Data | Owner | S3's relationship |
|---|---|---|
| Course, lifecycle state, description, prices | S2 (`catalog`) | Read only |
| Section outline and per-Section prices | S2 | Read only |
| Live revision pointer, pending revision | S2 | Read only; reads the live pointer, never the pending revision |
| Emergency access suspension | S2 | Read only; an active suspension excludes the Course |
| Taxonomy terms, labels, codes | S2 | Read only |
| Instructor display name | S1 (`identity`) | Read only; the **only** identity field on a public surface |
| Asset Version reference for the public preview | S2 reference, S4 delivery | Read only |

**If implementing S3 appears to require a write to any of the above, stop.** That is a finding against
[spec.md](spec.md), not a licence to add a write path to a read-only slice.

## Migration `0010_catalog_search`

Additive only. One column, one index. It modifies no existing table's constraints and **no existing
migration file** — `scripts/docs-guard.sh` enforces the checksums of applied migrations, and
`0001_init` onward are applied to real databases.

### The column

A **generated** column on the Course table holding normalized searchable text, derived from:

- Course title (Arabic and English as authored)
- Authored description
- Owning Instructor display name *(BR-105)*
- Assigned taxonomy term labels and the optional Subject code *(BR-157, BR-158)*

Generated rather than application-maintained, deliberately. A search column written by application
code is a second source of truth for the text the catalogue is judged on, and it drifts the first time
a title is updated through a path that forgot it. Generation makes drift structurally impossible.

If a source field's normalization cannot be expressed as a generated column in PostgreSQL — Arabic
folding may require an `IMMUTABLE` helper function — the fallback is a **trigger**, not an application
write path. Record which was used and why.

### Population boundary

The column carries text for **Published Courses' public fields only**. A Draft Course's title must not
sit in a searchable column waiting for a query bug to surface it.

This is deliberately redundant with `PublishedOnly` ([plan.md](plan.md#authorization--how-a-public-slice-stays-safe)).
Defence in depth is the point: the predicate is the control, and this is what limits the blast radius
if the predicate is ever bypassed.

### The index

One index supporting the substring/prefix match the retained scope needs. **Not** a ranking structure
— relevance ranking is deferred to S18 per
[§2.2](../../docs/launch/AUGUST_15_EXECUTION_PLAN.md#22-reduced-slices--launch-critical-core-retained-remainder-reclassified).

### Down migration

Drops the index and the column. It must leave no orphaned function or type. Verify `up` → `down` →
`up` against real PostgreSQL rather than by inspection, and confirm the schema version reports 10.

## Schema version

`db.MaxSchemaVersion` rises to **10** (T030). CI derives its assertion from that constant through the
`migrate max-version` subcommand rather than a literal — a hardcoded version is the exact drift that
failed hosted CI during S1B2.

## OD-001 dependency

This entire migration exists to serve normalized search. If
[OD-001](spec.md#open-decisions) is rejected and Arabic normalization is deferred, the column is still
useful for case-insensitive English matching, but its Arabic folding drops and the specification must
stop claiming BR-162 compliance. **Do not resolve OD-001 by implementing one of its branches.**
