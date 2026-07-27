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
| New storage | One generated/normalized search column and its index. **No new table.** |
| Auth | None. These routes are deliberately anonymous |
| Migration | `0010_catalog_search` — additive only |

## Constitution Check

- **I — one source of truth**: S3 adds no authority. Every field it renders is owned by S2 or S1. The
  search column is derived and reproducible from its source columns.
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

## The enumeration case, resolved

FR-003 requires a non-Published Course to be indistinguishable from a non-existent one. Three ways
this leaks, all closed here rather than left to the implementer:

| Leak | Resolution |
|---|---|
| Different status codes (`403` vs `404`) | Both paths return the **same** `404` Problem Details document. There is one not-found response constructor for the public surface and handlers cannot build another |
| Different bodies or headers | The response is constructed before the lookup result is known to differ — same envelope, same `Cache-Control`, no `detail` field that varies with cause |
| Timing | The query is a single indexed lookup with the predicate **in the WHERE clause**, so a hidden row and an absent row take the same path. Do **not** fetch-then-check in application code: that returns faster for a missing row than for a hidden one |

The third is the one that gets implemented wrong by default, because fetch-then-check reads more
naturally. It is called out in the tasks file at the task that would introduce it.

## Search

Scoped to what §2.2 retained plus the OD-001 recommendation, and **OD-001 is unresolved** — see
[spec.md §Open Decisions](spec.md#open-decisions). The plan below assumes it is accepted; if the
developer rejects it, T-search-2 drops and FR-023 weakens to English-only.

- Matching is a substring/prefix match over a normalized text column, not a ranking engine. No
  `tsvector` weighting, no trigram scoring, no relevance ordering — those are the deferred parts.
- The normalized column is **generated from** title, description, Instructor display name, and
  taxonomy labels/code. It is derived, so it cannot disagree with its sources.
- Normalization is one function applied identically on write and on query. Applying it to stored text
  but not to the query, or vice versa, is the failure mode; a single shared function is what prevents
  it, and the test asserts both directions.
- The column is populated **only for Published Courses' public fields**. A Draft Course's title must
  not sit in a searchable column waiting for a query bug to surface it. Defence in depth behind
  `PublishedOnly`, deliberately redundant.

## Data integrity

S3 writes no domain data, so the integrity question is narrow: **can the derived search column drift
from its sources?**

It cannot, because it is a generated column maintained by the database rather than by application
code. Migration `0010` is additive — one column, one index — and adds no constraint to existing
tables. If a future change makes generation impractical, the fallback is a trigger, **not** an
application-level write path; a search column maintained by the application is a second source of
truth for text the catalogue is judged on.

## Project Structure

### Documentation (this feature)

```
specs/004-public-catalogue/
├── spec.md          # 25 requirements, 7 success criteria, OD-001
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
├── search.go           # the shared normalize function (OD-001 dependent)
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
| A generated search column | Search the source columns with `ILIKE` directly | Normalization must apply identically on write and query; a generated column makes that structural instead of remembered |
| One `PublishedOnly` predicate | A status filter per query | Four separate exclusions applied per-site is exactly how a route ends up with three of them |
