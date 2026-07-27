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
| New storage | One `IMMUTABLE` SQL normalize function, one generated column, one index. **No new table.** |
| Auth | None. These routes are deliberately anonymous |
| Migration | `0010_catalog_search` — additive only |

## Constitution Check

- **I — one source of truth**: S3 adds no authority. Every field it renders is owned by S2 or S1. The
  stored search column is generated from its own row's columns; joined fields are normalized at read
  time and stored nowhere.
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

- The stored searchable column is **generated** by that function.
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
columns of its own row, and three of those four fields live in other tables.

Rather than build the machinery that would make it possible — triggers on `catalog`, on taxonomy
assignment, and on `identity` display-name changes, which is a denormalization subsystem and a
module-boundary violation — S3 splits by where the data lives:

| Field | Where normalized | Why |
|---|---|---|
| Course title, description | **Stored** generated column, backfilled by migration `0010` | Same row; generation makes drift impossible |
| Instructor display name, taxonomy labels/code | **Query-time**, same function applied in the join | Cross-table; at launch catalogue size a join scan is free |

Both sides use `catalog_normalize_ar`, so the "one shared function" guarantee holds across the split.
This keeps S3 inside its slice — no trigger fabric, no cross-module coupling, no search subsystem — and
the cost is that joined-field search does not use an index. At 8–12 Courses that is not a cost. **It
becomes one somewhere in the hundreds**, and that threshold is recorded in
[research.md](research.md) as the trigger for revisiting it in S18 alongside ranking.

### Population boundary

The stored column carries text for **Published** Courses only. A Draft title must not sit in a
searchable column waiting for a query bug to surface it — deliberately redundant with `PublishedOnly`,
which remains the control.

## Data integrity

S3 writes no domain data, so the integrity question is narrow: **can the derived search text drift
from its sources?**

It cannot, and for two different reasons depending on the field:

- **Stored** (title, description): a generated column maintained by the database, not by application
  code. Drift is structurally impossible.
- **Query-time** (display name, taxonomy labels): nothing is stored, so there is nothing to drift
  *from*. The normalization is applied to live values at read time.

Migration `0010` is additive — one function, one column, one index — and adds no constraint to any
existing table. Backfill of existing published records is handled by the `ALTER TABLE … ADD COLUMN …
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
├── research.md      # the two decisions with real alternatives
├── data-model.md    # migration 0010, additive
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
├── 0010_catalog_search.up.sql
└── 0010_catalog_search.down.sql

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
| One `PublishedOnly` predicate | A status filter per query | Four separate exclusions applied per-site is exactly how a route ends up with three of them |
