# Research — S3 Public Catalogue

**Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md)

Two decisions in this slice had real alternatives. The rest follow from existing precedent.

## R-001 — Where the visibility filter lives

**Decision**: at the **query boundary**, in one shared `PublishedOnly` predicate, with a test that
derives its coverage from the live route table.

**Alternatives considered**

| Option | Why rejected |
|---|---|
| Middleware, as S2 does for ownership | A middleware gates a *request*; visibility filters *rows*. It cannot filter rows it never sees, and a handler that builds its own query bypasses it entirely |
| A status condition inline in each query | This is the S1C hand-maintained-matrix failure in a new costume. Four exclusions × N routes is a matrix maintained by memory, and one route will end up with three of the four |
| A database view exposing only public Courses | Genuinely attractive, and close. Rejected because the emergency-suspension and live-revision-pointer conditions make the view non-trivially updatable and because it moves the control out of the code the review reads. Reconsider post-launch |
| Row-level security in PostgreSQL | Strongest option and the wrong slice for it. It would need a session-role convention across an application that has none, seventeen days before launch |

**Evidence behind it**: S1C shipped a hand-maintained authorization matrix as a high finding; its
derived replacement caught seven moved routes the same day. The derived test is the transferable part
of that lesson, and it is why R-001's answer is a *test strategy* and not only a code placement.

## R-002 — Arabic normalization: generated column vs. query-time folding

**Decision**: a generated column, with one shared normalize function applied on write and on query.

**Alternatives considered**

| Option | Why rejected |
|---|---|
| Normalize the query only, match against raw stored text | Asymmetric by construction: `احياء` still fails to match a stored `أحياء`. This is the failure mode, not a lighter version of the fix |
| Application-maintained normalized column | A second source of truth for the text the catalogue is judged on. It drifts the first time a title changes through a path that forgot to update it |
| A Go normalize function plus a SQL expression | **Two implementations of one rule.** They diverge silently: English tests keep passing while Arabic stops matching. Rejected in favour of a single `IMMUTABLE` SQL function used by both sides, which makes asymmetry unrepresentable rather than merely tested |
| PostgreSQL full-text search with an Arabic configuration | Correct long-term answer and out of scope: it brings ranking, which §2.2 explicitly deferred. Adopting it here would smuggle a deferred feature in as an implementation detail |
| `unaccent` extension | Handles diacritics but not alef-variant folding, taa marbuta, alef maqsura, or Arabic-Indic digits — most of BR-162 |

**Resolved 2026-07-28 — `ADJUST`.** The developer pulled normalization into S3 and kept ranking and
filtering deferred, on the grounds that §2.2's single line bundled a genuinely optional feature
(ranking) with a non-optional one (matching working in the product's default language). See
[OD-001](spec.md#resolved-decisions).

## R-005 — The cross-table constraint

**Found while reconciling this plan to the OD-001 decision, and it invalidated the first draft of
[data-model.md](data-model.md).**

That draft specified one generated column covering Course title, description, Instructor display name,
and taxonomy labels. **It is not implementable.** A PostgreSQL generated column may reference only
columns of its own row, and three of those four fields live in other tables.

**Options considered**

| Option | Verdict |
|---|---|
| Trigger fabric on `catalog`, taxonomy assignment, and `identity` | **Rejected.** A denormalization subsystem plus a module-boundary violation — a trigger in the catalogue firing on identity writes — in a slice explicitly told not to expand into a search subsystem |
| Materialized view refreshed on a schedule | **Rejected.** Introduces staleness into a correctness-adjacent surface, and a refresh job is operational machinery S3 does not otherwise need |
| Application-maintained column written in S2's transactions | **Rejected.** Requires editing S2 while it is being implemented, and re-creates the second-source-of-truth problem R-002 rejected |
| **Split by locality**: generate same-row fields, normalize joined fields at query time | **Adopted** |

The adopted split keeps one shared function across both sides, so the OD-001 guarantee holds. Its cost
is that joined-field matching is unindexed.

**When that cost becomes real**: a sequential scan with two joins over normalized text stays
comfortably inside the 2.5s p95 LCP budget into the **low hundreds** of Courses. Past roughly
**500 Courses** it should be revisited — and the right answer then is PostgreSQL full-text search with
an Arabic configuration, adopted **together with ranking** in S18, rather than a trigger fabric bolted
on earlier. Recorded here so the threshold is a documented trigger rather than a surprise.

## R-003 — Reusing the S1B locale mechanism

**Decision**: extend `frontend/src/lib/i18n`.

Not a close call, recorded because "the public shell is different from the authenticated shell" is a
plausible-sounding reason to build a second one. Two locale mechanisms means two answers to *what
language did this visitor choose*, and the one the user sees would depend on which part of the app
rendered last. The existing provider already handles anonymous visitors — it serves the registration
and sign-in screens, which are unauthenticated.

## R-004 — Preview media on a public page

**Decision**: render against the S4 contract; do not invent a temporary delivery path.

The public preview is the only media on an unauthenticated page, and it is authorized separately from
protected Lesson content (BR-143). S4 owns delivery. A temporary unsigned path built here would be a
public media endpoint created in a slice whose scope boundary explicitly excludes protected media —
and temporary public media endpoints have a way of outliving their slice.
