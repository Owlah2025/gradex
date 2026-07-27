# Tasks: S3 — Public Catalogue and Bilingual Shell

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-28

**Builder**: Antigravity, under [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews).
**Reviewer**: Claude, Tier 1. **A builder never closes its own slice.**

**Blocked until S2 closes** on an independent verdict. Frozen and ready, not active.

**[OD-001](spec.md#resolved-decisions) resolved `ADJUST` on 2026-07-28**: Arabic query normalization
is **in** S3; relevance ranking and multi-dimension filtering stay deferred to S18. FR-023b forbids
implementing the deferred parts by accident.

---

## Standing clause — applies to every task below

> **A required dependency is validated at construction and the component refuses to build without it.
> No security-relevant control may silently degrade, default, or become optional.**

In this slice it has one specific target: **`PublishedOnly` must never be optional or defaultable.** A
repository that accepts a missing predicate and falls back to no filter exposes the entire draft
catalogue while reading like a sensible default.

**Tests are required, not optional.** Every acceptance proof must **fail under a deliberate mutation**.
A test that passes against broken code is not evidence — that standard comes from S1C, where two
proofs were trusted only after the reviewer reproduced their mutations independently.

---

## Phase 1 — Foundation and the visibility predicate

**This phase is the security surface. Nothing else in the slice matters if it is wrong.**

- [ ] T001 Create `backend/internal/catalogpublic/doc.go` stating the module boundary: public reads
      only, no writes, no authority, reads S2's tables
- [ ] T002 Implement `PublishedOnly` in `backend/internal/catalogpublic/visibility.go` as the single
      exported predicate encoding **all four** exclusions together — lifecycle state, emergency access
      suspension, pending-revision selection, and the live-revision pointer. One condition, because it
      answers one question
- [ ] T003 Implement the repository in `backend/internal/catalogpublic/repository.go` with list,
      detail, and search. **Every** query obtains its rows through T002. The constructor **refuses to
      build** without the predicate — no nil, no default, no fallback
- [ ] T004 Implement the single not-found response constructor for the public surface. Hidden and
      absent Courses return byte-identical `404` Problem Details — same status, same body, same
      headers, no cause-varying `detail` field

**Checkpoint 1** — the predicate exists, is unavoidable, and both not-found paths are identical.
No route is mounted yet.

## Phase 2 — Public routes and derived enforcement

- [ ] T005 Register public routes under `/api/v1/catalog` in `backend/internal/httpapi/router.go`,
      per [contracts/catalogue-api.md](contracts/catalogue-api.md)
- [ ] T006 Implement thin handlers: **no status comparison in a handler**, no second not-found
      constructor, no query built outside the repository
- [ ] T007 **The load-bearing test.** In `backend/internal/httpapi/catalog_public_test.go`, enumerate
      every route registered under the public prefix from `r.Routes()` and assert each is served
      through `PublishedOnly`. A new public route that queries the catalog tables directly **must fail
      this test.** Derive the route list; never hand-maintain it
- [ ] T008 Prove the enumeration case: request every non-Published state by **exact identifier** and
      assert the response is identical to a never-existing identifier in **status, headers, response
      schema, and body**. This is the exact, provable guarantee — assert on the full response, not the
      status code. Timing is **not** claimed here; it is measured separately in T038
- [ ] T009 Implement the detail lookup with the predicate **inside the query boundary**. Do **not**
      fetch-then-check in application code: that returns measurably faster for an absent row than for
      a hidden one, and closing that branch is **necessary but not sufficient** — see
      [plan.md](plan.md#the-timing-claim-stated-honestly). This task is where the mistake gets made by
      default

**Checkpoint 2 — MANDATORY REVIEW GATE.** Do not proceed to Phase 3 until T007 and T008 pass *and*
their mutations have been run. This is the only checkpoint in S3 that blocks on evidence rather than
on completion, because everything after it is presentation and cannot compensate for a leak here.

Required mutations, each of which must turn a test red:
1. Remove one of the four exclusions from `PublishedOnly` → T007 or T008 fails.
2. Add a public route that queries the catalog tables directly → T007 fails and **names that route**.
3. Make the hidden-Course path return `403` instead of `404` → T008 fails.
4. Add a cause-varying `detail` field to the hidden-Course response → T008 fails on the body
   assertion. Included because a differing body is the leak that survives a matching status code.

## Phase 3 — Catalogue list and Course detail (backend)

- [ ] T010 [P] Implement the paginated list projection: title, Instructor display name, three taxonomy
      dimensions, price, preview availability
- [ ] T011 [P] Implement the detail projection: description, Section outline, full-Course price, each
      individually priced Section's price, preview reference
- [ ] T012 Assert **no PII beyond display name** appears in any public response, against the full
      response body rather than the rendered page. Email, phone, and legal identity must be absent
      from the serialized payload, not merely unrendered
- [ ] T013 Assert Lesson titles, Resources, and Lab Materials are absent from every public response
- [ ] T014 Assert a Published Course whose owning Instructor is **suspended** remains publicly visible
      — suspension blocks authoring, not Student access (BR-065). This is an easy over-correction and
      the test exists to catch it

**Checkpoint 3** — the public API returns correct content and leaks nothing. Frontend has not started.

## Phase 4 — Bilingual responsive shell (frontend)

- [ ] T015 Extend `frontend/src/lib/i18n` — **extend, do not replace.** A second locale mechanism is a
      second source of truth for what language the user chose
- [ ] T016 Implement the public shell layout with document direction bound to locale: RTL for Arabic,
      LTR for English, applied to navigation, forms, lists, and iconography
- [ ] T017 Default to Arabic for a visitor with no stored preference; persist a chosen language across
      navigation and across sessions, for anonymous and authenticated visitors alike
- [ ] T018 [P] Implement the catalogue list screen
- [ ] T019 [P] Implement the Course detail screen, including the Section outline and both price forms
- [ ] T020 Render the optional public preview with no empty player frame when absent; ensure no
      protected media is reachable from the page
- [ ] T021 Present Instructor-authored content in its authored language under either interface
      language, with no machine translation (BR-150)
- [ ] T022 Responsive audit at phone, tablet, and desktop widths in **both** directions: no clipping,
      mirroring, or overlap

**Checkpoint 4** — a visitor can browse and evaluate a Course in Arabic and English.

## Phase 5 — Search and Arabic normalization

> **Red-first is mandatory in this phase.** Every test below is written and **observed failing**
> before its implementation exists, and the failure output is recorded. Arabic normalization is the
> one area of this slice where a test can pass for the wrong reason — an English-only assertion looks
> identical whether folding works or is absent entirely — so "I wrote the test after and it passed" is
> not evidence here.

- [ ] T023 Add migration `0010_catalog_search`. Additive only: the `catalog_normalize_ar` function,
      one generated column, one index. No constraint on any existing table, and **no modification of
      any existing migration file** — their checksums are enforced
- [ ] T023a **Verify the backfill, do not assume it.** `ALTER TABLE … ADD COLUMN … GENERATED … STORED`
      computes the value for existing rows as part of the statement. Assert after `up` that every
      pre-existing Published Course has non-empty `search_text` and that a known Arabic title is
      present in folded form. A migration that leaves the existing catalogue unsearchable passes every
      schema assertion while being obvious to a visitor
- [ ] T024 Implement `catalog_normalize_ar` as an `IMMUTABLE STRICT` SQL function — **the single
      definition of normalization in the system.** Exactly the transformations in
      [data-model.md](data-model.md#1-the-normalize-function--the-single-definition): alef/hamza
      folding, alef maqsura, taa marbuta, Arabic-Indic digits, tashkeel and tatweel removal, Unicode
      case folding, and whitespace collapse
- [ ] T025 **Write no Go normalization function.** The incoming query is normalized by calling the
      same SQL function inside the query. If a Go helper appears, write/query asymmetry has become
      representable again and the guarantee is gone. The review checks for its absence
- [ ] T026 Generate the stored column from **title and description only** (same-row constraint,
      [R-005](research.md#r-005--the-cross-table-constraint)), with `coalesce` so a null description
      cannot null the column and silently drop the Course from search. Populate for **Published**
      Courses only — deliberately redundant with T002
- [ ] T027 Normalize the joined fields — Instructor display name, taxonomy labels and code — **at
      query time through the same function**, and route the whole search through the **same**
      `PublishedOnly` predicate rather than a separate status condition (FR-022)

### Red-first test set — each observed failing before its implementation

- [ ] T028 [RED] **Normalized Arabic queries.** `احياء` matches a Course titled `أحياء`; a query
      written with tashkeel matches a title without it; a tatweel-padded query matches; Arabic-Indic
      digits match Western ones. Each assertion must fail before T024 exists
- [ ] T029 [RED] **Mixed Arabic/Latin content.** A Course whose title mixes both scripts is matched by
      an Arabic fragment and by a Latin fragment; a Latin query is case-insensitive; normalization of
      the Arabic portion does not corrupt the Latin portion
- [ ] T030 [RED] **Write/query symmetry.** Assert matching in **both** directions — stored-folded
      against raw query, and raw stored against folded query. This is a regression guard: T025 makes
      the asymmetry unrepresentable, and this test is what notices if someone reintroduces a Go
      normalizer
- [ ] T031 [RED] **Empty normalized query.** A query of only diacritics, only tatweel, or only
      whitespace normalizes to empty and MUST behave exactly as an absent query — the unfiltered
      published list, not an error and not an empty result (FR-023a, SC-009)
- [ ] T032 [RED] **Unpublished non-disclosure through search.** A Lesson title, a Resource filename,
      a Draft Course's title, and a Delisted Course's title each return nothing — including when
      queried in normalized Arabic form. Normalization must not become a path around `PublishedOnly`
- [ ] T033 [RED] **Degenerate input.** Empty, whitespace-only, 10 KB, and `%' OR 1=1 --` queries each
      return a well-formed result and never an error disclosing internals (FR-024)
- [ ] T034 Confirm no stemming, fuzzy matching, ranking, or external search dependency was introduced
      (FR-023b). Result ordering is stable and documented, not scored
- [ ] T035 Raise `db.MaxSchemaVersion` to 10 and confirm CI **derives** the assertion from that
      constant rather than carrying a literal

**Checkpoint 5** — search finds published Courses in both languages, reveals nothing else, and every
red-first test was observed failing first.

## Phase 6 — Performance and polish

- [ ] T036 Verify SC-006: p95 LCP under 2.5s on representative Kuwait 4G for the list and detail pages
- [ ] T037 Confirm indexes support the list and detail paths, and that the unindexed joined-field
      search stays inside budget at launch catalogue size ([R-005](research.md#r-005--the-cross-table-constraint))
- [ ] T038 **Timing-distribution check (SC-008).** Sample hidden-identifier and absent-identifier
      lookups, compare the distributions against a **documented tolerance**, and record the result as
      a statistical observation. **No nanosecond-equality assertion**, and no wording that calls a
      statistical property proven. Outside tolerance is a finding with an owner; inside tolerance is
      not a guarantee
- [ ] T039 Run the full gate suite from [quickstart.md](quickstart.md), including a **clean** frontend
      build with `.next` removed first

---

## Dependencies

| Phase | Blocked by | Why |
|---|---|---|
| 1 | S2 closing | Reads S2's tables; the graph must exist and be stable |
| 2 | Phase 1 | Routes cannot be safe before the predicate exists |
| 3 | **Checkpoint 2** | Hard gate on evidence, not completion |
| 4 | Phase 3 | Screens render the API's projections |
| 5 | Phase 1 | Search reuses `PublishedOnly`. OD-001 is resolved, so the shape is fixed |
| 6 | Phases 3–5 | Measures the finished paths |

**Phase 5 may run in parallel with Phase 4** — they share no file. Phase 3 may
not overlap Phase 2: it depends on the checkpoint.

## Parallel opportunities

`[P]`-marked tasks touch disjoint files: T010/T011, and T018/T019.

## Review checkpoints

| Checkpoint | Blocks | Evidence required |
|---|---|---:|
| 1 | Phase 2 | Predicate exists; constructor refuses to build without it |
| **2** | **Phase 3 — hard gate** | T007 and T008 pass **and** all three mutations turn a test red |
| 3 | Phase 4 | No PII, no Lesson content, no protected media in any public response |
| 4 | — | Responsive audit passes in both directions at three widths |
| 5 | — | All leak cases and degenerate inputs proven; **every red-first test observed failing before implementation** |
| Final | Slice closure | Full gate suite; independent Tier 1 review by Claude on a frozen exact range |

## Task count

**40 tasks** (T001–T039 plus T023a). Phase 5 grew from 8 to 13 with OD-001's resolution and the
red-first requirement; nothing was removed to accommodate it, and the slice estimate moves from 8h to
**10–11h**. That increase is reported rather than absorbed — see
[§Scope honesty](#scope-honesty).

## Scope honesty

OD-001 admitted real work into a slice that was already sized. Recorded plainly:

- **In**: one SQL normalize function, one generated column with verified backfill, query-time
  normalization of joined fields, six red-first test groups.
- **Still out**: ranking, multi-dimension filtering, sort options, stemming, fuzzy matching, external
  search infrastructure. FR-023b makes their absence a requirement rather than an omission.
- **Estimate impact**: +2–3h on an 8h slice. If implementation shows normalization cannot fit
  cleanly, **that is evidence to surface**, not licence to grow into a search subsystem — the
  fallback is to narrow the fold set, not to add machinery.

## MVP scope

Every task above is required for launch except T036–T038, which are measurement rather than behaviour
and may be recorded as findings if a target is missed, per PLAN.md's rule that a performance target is
not waived silently. T038 in particular reports an observation; it cannot "fail" into a guarantee.
