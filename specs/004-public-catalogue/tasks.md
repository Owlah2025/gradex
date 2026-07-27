# Tasks: S3 — Public Catalogue and Bilingual Shell

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-28

**Builder**: Antigravity, under [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews).
**Reviewer**: Claude, Tier 1. **A builder never closes its own slice.**

**Blocked until S2 closes** on an independent verdict. Frozen and ready, not active.

**Blocked on a developer decision**: [OD-001](spec.md#open-decisions) — whether Arabic query
normalization is in S3 or deferred. Phase 5 changes shape depending on the answer. **Do not guess it.**

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
      assert the response is byte-identical to a never-existing identifier — status, body, and headers
- [ ] T009 Implement the detail lookup with the predicate **in the WHERE clause**. Do **not**
      fetch-then-check in application code: that returns faster for an absent row than for a hidden
      one, which is a timing oracle. This task is where that mistake gets made by default

**Checkpoint 2 — MANDATORY REVIEW GATE.** Do not proceed to Phase 3 until T007 and T008 pass *and*
their mutations have been run. This is the only checkpoint in S3 that blocks on evidence rather than
on completion, because everything after it is presentation and cannot compensate for a leak here.

Required mutations, each of which must turn a test red:
1. Remove one of the four exclusions from `PublishedOnly` → T007 or T008 fails.
2. Add a public route that queries the catalog tables directly → T007 fails and **names that route**.
3. Make the hidden-Course path return `403` instead of `404` → T008 fails.

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

## Phase 5 — Search

> **Shape depends on [OD-001](spec.md#open-decisions).** Written assuming the recommendation is
> accepted. If the developer rejects it, T024 drops, FR-023 weakens to English-only, and
> [spec.md](spec.md) is amended to stop claiming BR-162 compliance. **Do not resolve this by choosing.**

- [ ] T023 Add migration `0010_catalog_search` — additive: one generated column, one index. No
      constraint on existing tables, no modification of any existing migration file
- [ ] T024 Implement the shared normalize function in `backend/internal/catalogpublic/search.go`:
      fold tashkeel, tatweel, alef variants (`أ إ آ ٱ` → `ا`), alef maqsura (`ى` → `ي`), taa marbuta
      (`ة` → `ه`), and Arabic-Indic digits (`٠–٩` → `0–9`); case-insensitive (BR-162)
- [ ] T025 Apply normalization **identically on write and on query** through that one function.
      Asymmetry is the failure mode; assert both directions
- [ ] T026 Populate the column from title, description, Instructor display name, and taxonomy
      labels/code — **for Published Courses' public fields only.** A Draft title must not sit in a
      searchable column waiting for a query bug. Deliberately redundant with T002
- [ ] T027 Route search through the **same** `PublishedOnly` predicate — not a separate status
      condition in the search query (FR-022)
- [ ] T028 Prove the leak cases: a Lesson title, a Resource filename, and a Draft Course's title each
      return nothing
- [ ] T029 Prove degenerate inputs: empty, whitespace-only, over-long, and SQL/regex metacharacter
      queries each return a well-formed result and never an error disclosing internals
- [ ] T030 Raise `db.MaxSchemaVersion` to 10 and confirm CI **derives** the assertion from that
      constant rather than carrying a literal

**Checkpoint 5** — search finds published Courses and reveals nothing else.

## Phase 6 — Performance and polish

- [ ] T031 Verify SC-006: p95 LCP under 2.5s on representative Kuwait 4G for the list and detail pages
- [ ] T032 Confirm indexes support the list, detail, and search paths at launch catalogue size
- [ ] T033 Run the full gate suite from [quickstart.md](quickstart.md), including a **clean** frontend
      build with `.next` removed first

---

## Dependencies

| Phase | Blocked by | Why |
|---|---|---|
| 1 | S2 closing | Reads S2's tables; the graph must exist and be stable |
| 2 | Phase 1 | Routes cannot be safe before the predicate exists |
| 3 | **Checkpoint 2** | Hard gate on evidence, not completion |
| 4 | Phase 3 | Screens render the API's projections |
| 5 | Phase 1, **OD-001** | Search reuses `PublishedOnly`; its shape needs the decision |
| 6 | Phases 3–5 | Measures the finished paths |

**Phase 5 may run in parallel with Phase 4** once OD-001 is answered — they share no file. Phase 3 may
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
| 5 | — | All four leak cases and all four degenerate inputs proven |
| Final | Slice closure | Full gate suite; independent Tier 1 review by Claude on a frozen exact range |

## Task count

33 tasks. T024 and its assertions drop if OD-001 is rejected, leaving 31.

## MVP scope

Every task above is required for launch except T031–T032, which are measurement rather than
behaviour and may be recorded as findings if the target is missed, per PLAN.md's rule that a
performance target is not waived silently.
