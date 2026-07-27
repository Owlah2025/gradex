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

Additive only: one function, one column, one index. It modifies no existing table's constraints and
**no existing migration file** — `scripts/docs-guard.sh` enforces the checksums of applied migrations,
and `0001_init` onward are applied to real databases.

### 1. The normalize function — the single definition

```sql
CREATE FUNCTION catalog_normalize_ar(input text) RETURNS text
  LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
```

This is **the only implementation of normalization in the system.** It is called both to generate the
stored column and to normalize the incoming query, so write/query asymmetry — the failure mode where
English keeps matching while Arabic silently stops — is not merely tested against but
**unrepresentable**. There is deliberately **no Go equivalent**; adding one would recreate the exact
divergence this design exists to prevent. See [plan.md §Search](plan.md#search).

`IMMUTABLE` is required for the generated column and for expression indexing. It is honest here: the
transformation depends only on its input, with no locale, collation, or time dependency.

**Transformations, exactly** (FR-023, BR-162):

| Class | Rule |
|---|---|
| Alef/hamza folding | `أ` `إ` `آ` `ٱ` → `ا` |
| Alef maqsura | `ى` → `ي` |
| Taa marbuta | `ة` → `ه` |
| Arabic-Indic digits | `٠١٢٣٤٥٦٧٨٩` → `0123456789` |
| Tashkeel / diacritics | `U+064B`–`U+0652`, `U+0670`, `U+0653`–`U+0655` removed |
| Tatweel | `U+0640` removed |
| Case | Unicode case folding (affects Latin; Arabic is caseless) |
| Whitespace | Leading and trailing trimmed; internal runs collapsed to one space |

`ة` → `ه` and `ى` → `ي` **over-match by design** and will occasionally merge genuinely different
words. BR-162 requires them, and at launch catalogue size a false positive costs one glance while a
false negative costs a sale. Recorded so it reads as a decision, not an oversight.

### 2. The generated column — same-row fields only

```sql
ALTER TABLE <course table>
  ADD COLUMN search_text text
  GENERATED ALWAYS AS (catalog_normalize_ar(coalesce(title,'') || ' ' || coalesce(description,'')))
  STORED;
```

**Only title and description.** A PostgreSQL generated column may reference only columns of its own
row, and the Instructor display name and taxonomy labels live in other tables. That constraint is
real and it invalidated this document's first draft, which specified all four fields in one column.

The rejected alternative was a trigger fabric over `catalog`, taxonomy assignment, and `identity` —
a denormalization subsystem plus a module-boundary violation, in a slice explicitly told not to grow
into a search subsystem. See [research.md §R-005](research.md#r-005--the-cross-table-constraint).

`coalesce` is deliberate: a `NULL` description must not null the whole column and silently remove the
Course from search.

### 3. Backfill of existing published records

**`ALTER TABLE … ADD COLUMN … GENERATED … STORED` computes the value for every existing row** as part
of the statement. Backfill is therefore inherent to the migration rather than a follow-up script.

It is **verified, not assumed** (T023a): after `up`, assert that every pre-existing Published Course
has a non-empty `search_text` and that a known Arabic title is present in folded form. A migration
that adds a column and leaves the existing catalogue unsearchable would pass every schema assertion
while making search work only for Courses created afterwards — a defect that is invisible to structure
checks and obvious to a visitor.

Note the table rewrite this implies: `STORED` generation rewrites the table. At launch catalogue size
that is instant. Recorded because it is a lock worth knowing about, not because it is a risk here.

### 4. The index

One index over `search_text` supporting the substring match FR-023b permits. **Not** a ranking
structure — ranking is deferred to S18.

Joined fields (display name, taxonomy labels) are normalized **at query time** through the same
function and are deliberately **not indexed**. At 8–12 Courses the join scan is free. See
[research.md §R-005](research.md#r-005--the-cross-table-constraint) for the catalogue size at which
that stops being true.

### 5. Down migration

Drops the index, the column, and the function, in that order. It must leave no orphaned function or
type. Verify `up` → `down` → `up` against real PostgreSQL rather than by inspection, and confirm the
schema version reports 10 at each `up`.

## Schema version

`db.MaxSchemaVersion` rises to **10** (T030). CI derives its assertion from that constant through the
`migrate max-version` subcommand rather than a literal — a hardcoded version is the exact drift that
failed hosted CI during S1B2.
