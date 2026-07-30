# Implementation Plan: S3 — Public Catalogue and Bilingual Shell

**Spec**: [spec.md](spec.md) | **Tasks**: [tasks.md](tasks.md) | **Date**: 2026-07-28

**Builder**: Antigravity, under [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews).
**Reviewer**: Claude, **Tier 1** per [§2.1](../../docs/launch/AUGUST_15_EXECUTION_PLAN.md#21-launch-critical-slices).

> **Tier 1 is the review depth, not the care level.** S3 is Tier 1 because it is read-only and
> introduces no money, entitlement, or credential path. It is *not* Tier 1 because it is safe: the
> visibility filter in FR-002 through FR-006 is a real security boundary and is reviewed as one. The
> tier governs breadth; the leak surface still gets adversarial attention.

**Blocked until S2 closes** on an independent verdict. This plan is frozen and waiting.

---

## Summary

S3 opens Gradex to anonymous visitors. It adds public read routes over the Course graph S2 authors,
and it establishes the bilingual responsive shell every later screen inherits. It writes nothing.

The design has exactly one hard problem — guaranteeing that non-Published Courses are invisible on
every public route, permanently, including routes added later — and the rest is presentation. The
plan therefore spends its structure on that one problem and keeps everything else conventional.

## Technical Context

| | |
|---|---|
| Backend | Go, `net/http` via gin, in the existing modular monolith |
| New package | `backend/internal/catalogpublic` — public read surface only |
| Reads from | `catalog` (S2) tables; no new authority |
| Frontend | Next.js App Router, extending `frontend/src/lib/i18n` |
| New storage | One `IMMUTABLE` SQL normalize function, one generated column on `course_revisions`, one index. **No new table.** |
| Auth | None. These routes are deliberately anonymous |
| Migration | `0011_catalog_search` — additive only; schema 10 is already S2 revision integrity |

## Constitution Check

- **I — one source of truth**: S3 adds no authority. Every field it renders is owned by S2 or S1. The
  stored search column lives on `course_revisions` and is generated from that row's own authored
  columns; joined fields are normalized at read time and stored nowhere. Whether a revision may be
  exposed stays with `courses` and with `PublishedOnly` — storage carries no visibility claim.
- **II — deny by default**: inverted here and stated explicitly, because a public route is an
  allow-by-default surface. The compensating control is that **publication is the allowlist**: the
  shared predicate returns rows only for the one state that is public, so a new route inherits the
  restriction by construction rather than by remembering to add a filter.
- **III — traceability**: every FR cites its BR.
- **IV — no second decision point**: S3 adds no capability and no authorization gate. Its single
  decision is the visibility predicate.
- **V — rigor scales to risk**: the visibility filter, the identifier-enumeration case, and the search
  leak case get integration proofs with mutation checks. Layout and copy get component tests and a
  responsive audit, not integration proofs.

---

## Authorization — how a public slice stays safe

S1C's high finding was a **hand-maintained** authorization matrix that could not detect drift, and its
replacement — a matrix derived from `r.Routes()` — caught seven moved routes on the day it landed.
S2 carries that forward as FR-042. S3 carries it forward again, in the form this slice needs:

**One predicate, one place, derived enforcement.**

1. Every public read goes through a single exported predicate in `catalogpublic` — call it
   `PublishedOnly` — which is the *only* place a Course status is compared for public visibility.
   Handlers do not compare status. Queries do not inline `status = 'PUBLISHED'`.
2. `PublishedOnly` encodes all four exclusions together: lifecycle state, emergency access suspension,
   pending-revision selection, and live-revision pointer. They are one condition because they are one
   question — *may an anonymous caller see this?* — and splitting them is how three of them get
   applied on the list route and two on the detail route.
3. A test enumerates every route registered under the public prefix from `r.Routes()` and asserts each
   one is served by a handler that obtains its rows through `PublishedOnly`. **A new public route that
   queries the catalog tables directly fails that test.**

Point 3 is the load-bearing one. Points 1 and 2 are a convention; point 3 is what makes the convention
survive the next slice, and it is the direct descendant of the S1C finding.

### Why not a middleware

Ownership in S2 is a middleware because it gates a request. Visibility here is a *row filter*, and a
middleware cannot filter rows it never sees. Enforcing it at the query boundary is the only placement
that cannot be bypassed by a handler that constructs its own query — so the test asserts the query
boundary, not the route table alone.

## The enumeration case

FR-003 requires a non-Published Course to be indistinguishable from a non-existent one. Three leak
channels, with **different strengths of guarantee** — and the difference is stated rather than
flattened:

| Leak | Resolution | Strength |
|---|---|---|
| Different status codes (`403` vs `404`) | Both paths return the **same** `404` Problem Details document, from one constructor handlers cannot bypass | **Proven** by assertion |
| Different bodies, headers, or schema | The envelope is constructed before the lookup outcome is known — same `Cache-Control`, no cause-varying `detail` | **Proven** by assertion on the full response |
| Timing | Predicate **inside** the query boundary, never fetch-then-check in application code | **Reduced, not eliminated.** See below |

### The timing claim, stated honestly

An earlier draft of this plan said the `WHERE`-clause predicate made a hidden row and an absent row
"take the same path". **That was an overclaim and it is withdrawn** ([OD-002](spec.md#resolved-decisions)).

Keeping the predicate in the query boundary removes the *application-level* branch — a fetch-then-check
in Go returns measurably faster for an absent row than for a hidden one, and that is a real oracle
worth closing. But closing it is **necessary, not sufficient**. Index traversal, buffer cache state,
row width, and planner behaviour can still differ between a row that exists-but-is-hidden and a row
that does not exist. No amount of SQL structuring makes that difference provably zero.

So S3 claims exactly this, and no more:

1. **Proven**: responses identical in status, headers, schema, and body.
2. **Structural**: no application-level branch on visibility.
3. **Measured**: a timing **distribution** test over a sample, against a **documented tolerance**,
   reported as a statistical observation.

The measurement is a regression detector, not a proof. A run inside tolerance does not establish
indistinguishability; a run outside it is a finding with an owner. **No test asserts nanosecond
equality**, because equality of two timing samples is not a property that can hold, and a test that
demands it would be deleted by the first person it failed.

## Search

**[OD-001](spec.md#resolved-decisions) is resolved `ADJUST`**: normalization is in S3; ranking and
filtering stay deferred to S18.

### One implementation, not two

The requirement is that stored text and incoming query pass through *the same* normalization. The
obvious construction — a Go function for the query and a SQL expression for the stored column — is
**two** implementations of one rule, and their divergence is silent: English tests keep passing while
Arabic quietly stops matching. That is precisely the failure mode this work exists to prevent.

So normalization is implemented **once, in SQL**, as an `IMMUTABLE` function:

```
catalog_normalize_ar(text) RETURNS text   -- IMMUTABLE, the single definition
```

- The stored searchable column on `course_revisions` is **generated** by that function.
- The incoming query is normalized by **the same function**, called in the query itself, rather than
  pre-normalized in Go.

There is no Go normalization code, so write/query asymmetry is not merely tested against — it is
**unrepresentable**. That is the whole reason for choosing SQL over Go here, and it is why the
"assert both directions" test is a regression guard rather than the primary control.

### What the function does — enumerated, not gestured at

Exactly, per FR-023: alef/hamza folding (`أ إ آ ٱ` → `ا`); alef maqsura (`ى` → `ي`); taa marbuta
(`ة` → `ه`); Arabic-Indic digits (`٠–٩` → `0–9`); removal of tashkeel/diacritics and tatweel; Unicode
case folding; collapse of leading, trailing, and repeated whitespace.

`ة` → `ه` and `ى` → `ي` are **deliberate over-matching**. They will occasionally merge two genuinely
different words. BR-162 requires them, and at 8–12 Courses a false positive costs a visitor one glance
while a false negative costs a sale — recorded so the tradeoff is a decision rather than an accident.

Not implemented, per FR-023b: stemming, fuzzy/edit-distance matching, weighted or relevance ranking,
external search infrastructure.

### The cross-table constraint — a real finding against this plan's first draft

An earlier draft specified one generated column covering title, description, Instructor display name,
and taxonomy labels. **That is not implementable**: a PostgreSQL generated column may only reference
columns of its own row, and the display name and taxonomy labels live in other tables.

Rather than build the machinery that would make it possible — triggers on `catalog`, on taxonomy
assignment, and on `identity` display-name changes, which is a denormalization subsystem and a
module-boundary violation — S3 splits by where the data lives:

| Field | Where normalized | Why |
|---|---|---|
| `course_revisions.title_ar`, `title_en`, `description_ar`, `description_en` | **Stored** generated column on `course_revisions`, backfilled by migration `0011` | Same row; generation makes drift impossible |
| Instructor display name, taxonomy labels/code | **Query-time**, same function applied in the join | Cross-table; at launch catalogue size a join scan is free |

Both sides use `catalog_normalize_ar`, so the "one shared function" guarantee holds across the split.
This keeps S3 inside its slice — no trigger fabric, no cross-module coupling, no search subsystem — and
the cost is that joined-field search does not use an index. At 8–12 Courses that is not a cost. **It
becomes one somewhere in the hundreds**, and that threshold is recorded in
[research.md](research.md) as the trigger for revisiting it in S18 alongside ranking.

### Which table owns the column — corrected 2026-07-30

This section previously said "same row" without saying **whose** row, and the artefacts carried a
`<course table>` placeholder. Against the committed S2 schema that placeholder has no valid filling,
because the two things the old requirement needed together live apart:

- `courses` owns publication — `lifecycle`, `live_revision_id`, `access_suspended_at`, `retired_at` —
  and carries **no** authored text; `0009_course_authoring` dropped its stub `title`.
- `course_revisions` owns the authored text — `title_ar`, `title_en`, `description_ar`,
  `description_en` — and owns no Course-level publication state.

The rule, now explicit:

> **`course_revisions` owns the generated catalogue-search text. `courses` owns whether a revision may
> be exposed publicly.**

Full derivation and the rejected alternatives are in
[research.md §R-006](research.md#r-006--which-table-owns-the-generated-search-column); the exact DDL is
in [data-model.md §2](data-model.md#2-the-generated-column--owned-by-the-course-revision-row).

### Exposure boundary — replaces the population boundary

The earlier draft said the stored column carries text for **Published** Courses only. **That
requirement was withdrawn on 2026-07-30 because it cannot be built**: a generated column cannot consult
another table, so no expression on either table can make publication decide whether the value exists.

What replaces it is not weaker, because the withdrawn rule was itself documented as *"deliberately
redundant with `PublishedOnly`, which remains the control."* It was a second layer over the control
that was always doing the work. Search text is generated for **every** revision, and exposure is
decided entirely at query time:

1. The search joins `course_revisions` to `courses` through the committed **ownership** foreign key,
   `course_revisions.course_id = courses.id`. This establishes candidacy only; it decides nothing.
2. `PublishedOnly` — the same predicate the list and detail routes use, unchanged — narrows that
   ownership join to the live revision. Its fourth exclusion,
   `courses.live_revision_id = course_revisions.id`, is the **single** enforcement point for the
   live-revision boundary and is **not** repeated in a search-specific join condition.
3. The normalized query matches that revision's `search_text`.

Step 2 is load-bearing rather than incidental. `courses` to `course_revisions` is one-to-many, so the
ownership join alone would surface a Course through the text of **any** revision it ever had, including
a `SUPERSEDED` title that was withdrawn on purpose. `PublishedOnly` is what closes that, and it is the
only thing that closes it — which is why T032b's mutation weakens the clause **inside `PublishedOnly`**
rather than a duplicate of it. A second copy in a join would make the control unlocatable and its
mutation survivable: removing either copy would leave the other enforcing, the test would stay green,
and the proof would report a control that is not where this plan says it is.

**A populated `search_text` is not a visibility claim.** A Draft, `SUPERSEDED`, or `REJECTED` revision,
and a revision of a `DELISTED`, `ARCHIVED`, retired, or suspended Course, all legitimately hold indexed
text; none of them may ever appear in a public result. The defence-in-depth the old boundary claimed now
lives in named mutation proofs — T032a and T032b — which is a form of redundancy that can actually be
run.

## Data integrity

S3 writes no domain data, so the integrity question is narrow: **can the derived search text drift
from its sources?**

It cannot, and for two different reasons depending on the field:

- **Stored** (the revision's authored titles and descriptions): a generated column on
  `course_revisions`, maintained by the database rather than by application code. Drift is structurally
  impossible, and because the source columns are on the same row as the generated value there is no
  window in which the two disagree.
- **Query-time** (display name, taxonomy labels): nothing is stored, so there is nothing to drift
  *from*. The normalization is applied to live values at read time.

A third drift question exists only because the column moved to `course_revisions`: **can the searchable
text drift from the text a visitor is shown?** It cannot, and for the same reason — the detail projection
and the search predicate both resolve through the **same** `PublishedOnly` live-revision clause, so they
read the same row.
Repointing `live_revision_id` changes which text is searchable and which text renders in the same
instant, without copying anything into `courses`. T032a proves that.

Migration `0011` is additive — one function, one column, one index — and adds no constraint to any
existing table. Backfill of existing revision rows is handled by the `ALTER TABLE … ADD COLUMN …
GENERATED` itself, which computes the value for every existing row; it is verified rather than
assumed (T023a).

**The fallback if generation proves impractical is a trigger, never an application write path.** A
search column maintained by application code is a second source of truth for the text the catalogue is
judged on, and it desynchronizes the first time a title changes through a path that forgot it.

## Project Structure

### Documentation (this feature)

```
specs/004-public-catalogue/
├── spec.md          # 28 requirements, 9 success criteria, OD-001/OD-002 resolved
├── plan.md          # this file
├── research.md      # the decisions with real alternatives
├── data-model.md    # migration 0011, additive; search-text ownership
├── contracts/
│   └── catalogue-api.md
├── tasks.md         # dependency-ordered, with review checkpoints
└── quickstart.md    # how the slice is proven
```

### Source Code

```
backend/internal/catalogpublic/
├── doc.go              # module boundary: public reads only, no writes, no authority
├── visibility.go       # PublishedOnly — the single predicate
├── repository.go       # list, detail, search; every query via PublishedOnly
├── search.go           # query construction; normalization lives in SQL, not here
└── handlers.go         # thin; no status comparison, one not-found constructor

backend/internal/httpapi/
├── router.go           # public route registration under /api/v1/catalog
└── catalog_public_test.go   # the derived enforcement sweep

backend/internal/db/migrations/
├── 0011_catalog_search.up.sql
└── 0011_catalog_search.down.sql

frontend/src/
├── app/(public)/catalog/page.tsx
├── app/(public)/catalog/[slug]/page.tsx
├── components/catalog/…
└── lib/i18n/…          # EXTENDED, not replaced
```

## Standing implementation clause

> **A required dependency is validated at construction and the component refuses to build without it.
> No security-relevant control may silently degrade, default, or become optional.**

Carried from S1C, where six instances of that class shipped in one slice. In S3 the clause has one
specific target: **`PublishedOnly` must not be optional.** A repository constructor that accepts a
nil or omitted predicate and falls back to "no filter" would expose the entire draft catalogue, and it
would read like a reasonable default while doing it.

## Complexity Tracking

| Choice | Simpler alternative | Why the simpler one was rejected |
|---|---|---|
| A derived enforcement test over `r.Routes()` | Code review and convention | Convention failed in S1C and the derived matrix caught it. Same defect class, same answer |
| Normalization implemented once in SQL | A Go normalize function plus a SQL expression | Two implementations of one rule diverge silently — English keeps passing while Arabic stops matching. One `IMMUTABLE` SQL function makes asymmetry unrepresentable |
| Stored generation for same-row fields, query-time for joined fields | One column covering all four fields | Not implementable: a generated column cannot reference other tables. The alternative was a trigger fabric across three tables — a denormalization subsystem S3 must not become |
| `course_revisions` owns generated search text; `courses` owns exposure | A generated column on `courses`, populated for Published Courses only | Unbuildable on either table: `courses` has no authored text and PostgreSQL forbids cross-table generation. The withdrawn rule was documented as redundant with `PublishedOnly`; that predicate — live-revision clause included — is what actually enforces visibility ([R-006](research.md#r-006--which-table-owns-the-generated-search-column)) |
| One `PublishedOnly` predicate | A status filter per query | Four separate exclusions applied per-site is exactly how a route ends up with three of them |
