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

## Search-text ownership — the rule this slice turns on

**Found on 2026-07-30 by the implementation builder, before any file was edited, and it invalidated
this document's second draft.** The committed S2 schema splits searchable text from publication state
across two tables:

| Concern | Owning table | Columns, exactly as committed |
|---|---|---|
| Authored searchable text | `course_revisions` | `title_ar`, `title_en`, `description_ar`, `description_en` |
| Whether a revision may be exposed publicly | `courses` | `lifecycle`, `live_revision_id`, `access_suspended_at`, `retired_at` |

`courses` carries **no authored text at all**: `0009_course_authoring` dropped its stub `title` column
when it expanded the S1 placeholder into the S2 domain table, and description was never on it.
`course_revisions` carries the text but owns no Course-level publication state — its own `state` enum
describes the *revision's* review position, not whether the Course is public.

A PostgreSQL generated column may reference only columns of its own row. So the earlier requirement —
*a same-row generated column, populated for Published Courses only* — is not merely hard, it is
**unsatisfiable on either table**. On `courses` there is nothing to generate from; on
`course_revisions` there is nothing to test publication against.

The rule, stated once:

> **`course_revisions` owns the generated catalogue-search text. `courses` owns whether a revision may
> be exposed publicly.**

The generated column therefore lives on `course_revisions`, is generated for **every** revision row,
and is **never** conditioned on the owning Course's lifecycle. Publication is an **exposure rule
applied at query time**, not a storage rule — see
[§6](#6-public-exposure--the-rule-that-replaced-the-population-boundary) and
[research.md §R-006](research.md#r-006--which-table-owns-the-generated-search-column).

**This is still a read-only slice.** The generated column is derived storage maintained by the database
on an S2-owned table. S3 adds no trigger, no application write path, and no change to any S2 authoring
or publication transaction.

## Migration `0011_catalog_search`

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

### 2. The generated column — owned by the Course Revision row

```sql
ALTER TABLE course_revisions
  ADD COLUMN search_text text
  GENERATED ALWAYS AS (
    catalog_normalize_ar(
      coalesce(title_ar, '')       || ' ' ||
      coalesce(title_en, '')       || ' ' ||
      coalesce(description_ar, '') || ' ' ||
      coalesce(description_en, '')
    )
  ) STORED;
```

**`course_revisions`, not `courses`** — that is the whole content of the ownership rule above, and the
only table on which this statement can be written at all.

**Only the four authored text columns of the same revision row.** They are the exact committed column
names from `0009_course_authoring`; do not introduce `title` or `description` aliases, and do not add
authored fields to `courses`. The Instructor display name lives in `accounts` and the taxonomy labels
in `taxonomy_terms`, so neither can appear here — that constraint invalidated this document's first
draft, which specified all four *concepts* in one column.

All four columns are `NOT NULL` in the committed schema (the descriptions additionally
`DEFAULT ''`), so `coalesce` changes nothing today. It stays as a guard: `catalog_normalize_ar` is
`STRICT`, so a single `NULL` would make the whole concatenation `NULL` and silently drop the revision
from search if a later migration relaxed one of those constraints. Cheap insurance against a failure
mode that is invisible until a visitor reports it.

Both languages are in one column deliberately. FR-023 requires an Arabic query and an English query to
match regardless of interface language, and a single normalized document satisfies that without a
per-locale column or a locale branch in the query.

The rejected alternative was a trigger fabric over `catalog`, taxonomy assignment, and `identity` —
a denormalization subsystem plus a module-boundary violation, in a slice explicitly told not to grow
into a search subsystem. See [research.md §R-005](research.md#r-005--the-cross-table-constraint) and
[§R-006](research.md#r-006--which-table-owns-the-generated-search-column).

### 3. Backfill of existing revision rows

**`ALTER TABLE … ADD COLUMN … GENERATED … STORED` computes the value for every existing row** as part
of the statement. Backfill is therefore inherent to the migration rather than a follow-up script, and
it applies to **every** pre-existing `course_revisions` row regardless of revision state or of the
owning Course's lifecycle.

It is **verified, not assumed** (T023a): after `up`, assert that every pre-existing revision row has a
non-empty `search_text`, and that a known Arabic title is present in folded form. Assert it
specifically for the live revision of a pre-existing Published Course — the row a visitor's search must
actually reach. A migration that adds a column and leaves the existing catalogue unsearchable would
pass every schema assertion while making search work only for Courses created afterwards — a defect
that is invisible to structure checks and obvious to a visitor.

Draft, `SUPERSEDED`, and `REJECTED` revisions receive a populated `search_text` too. **That is correct
and it is not an exposure.** The value's presence on a row says nothing about whether the row is
publicly reachable; [§6](#6-public-exposure--the-rule-that-replaced-the-population-boundary) is what
decides that, and T032/T032a are what prove it.

Note the table rewrite this implies: `STORED` generation rewrites the table. At launch catalogue size
that is instant. Recorded because it is a lock worth knowing about, not because it is a risk here.

### 4. The index

One index over `course_revisions (search_text)` supporting the substring match FR-023b permits.
**Not** a ranking structure — ranking is deferred to S18.

The index covers every revision row, for the same reason the column does: a partial index conditioned
on the owning Course's lifecycle would need to read `courses`, which an index predicate cannot do
either. Restricting *what is indexed* is not how this surface is kept safe; restricting *what is
returned* is.

Joined fields (display name, taxonomy labels) are normalized **at query time** through the same
function and are deliberately **not indexed**. At 8–12 Courses the join scan is free. See
[research.md §R-005](research.md#r-005--the-cross-table-constraint) for the catalogue size at which
that stops being true.

### 5. Down migration

Drops the index and the column from `course_revisions`, then the function, in that order. It must leave
no orphaned function or type. Verify `up` → `down` → `up` against real PostgreSQL rather than by
inspection, and confirm the schema version reports **11** after each `up` and **10** after the `down`.

The `down` **does not** null, blank, or delete search text for any Course that has become unavailable.
There is no state in which search text is removed as a hiding mechanism; removal happens only when the
column itself is dropped.

### 6. Public exposure — the rule that replaced the population boundary

An earlier draft made storage carry a security claim: *populate the column for Published Courses only.*
That is what proved unsatisfiable, and replacing it costs nothing, because the control it was
"deliberately redundant" with is the one that was always doing the work.

A revision is publicly searchable **only** when all three hold:

1. It is the owning Course's current live revision — `courses.live_revision_id = course_revisions.id`.
2. The owning Course passes `PublishedOnly` (T002), the single predicate encoding all four exclusions:
   `lifecycle = 'PUBLISHED'`, no active `access_suspended_at`, not retired, and the live-revision
   pointer itself.
3. The normalized query matches that revision's `search_text`.

Condition 1 is what makes historical text unreachable, and it is not a convenience: `courses` and
`course_revisions` are one-to-many, so a search that joins on `course_id` alone would return a Course
through the text of **any** revision it has ever had, including a `SUPERSEDED` one whose title was
withdrawn. That is the exposure this rule closes.

**Stated as an invariant:**

> A generated `search_text` value on a row is **not** evidence that the row is publicly visible. It is
> a storage and query accelerator. Exposure is decided by the live-revision join and by
> `PublishedOnly`, and by nothing else.

So it is **acceptable** for a Draft, `SUPERSEDED`, or `REJECTED` revision, or a revision of a
`DELISTED`, `ARCHIVED`, retired, or suspended Course, to hold indexed search text. It is **not
acceptable** for any of them to appear in a public result. Removing either the live-revision join or
the `PublishedOnly` predicate must break a named test — T032a and T032b exist for exactly that, because
an invariant with no failing mutation behind it is a sentence, not a control.

## Schema version

Committed migrations currently end at schema version **10** (`RevisionIntegritySchemaVersion`, from
`0010_revision_integrity`). Applying `0011_catalog_search` raises the schema version to **11**: add
`CatalogSearchSchemaVersion = 11` and make `db.MaxSchemaVersion` **11**, following the per-migration
named-constant pattern in `backend/internal/db/schema.go`. Implemented by **T035**.

CI derives its assertion from that constant through the `migrate max-version` subcommand rather than a
literal — a hardcoded version is the exact drift that failed hosted CI during S1B2.
