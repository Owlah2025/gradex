# Tasks: S3 — Public Catalogue and Bilingual Shell

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-28

**Implementation seats: ACTIVE. Reissued 2026-07-30 under
[D-055](../../docs/DECISIONS.md#d-055--codex-implements-s3-from-the-corrected-planning-head-and-agy-reviews-it).**

| Seat | Holder | Authority |
|---|---|---|
| Implementation builder | **Codex** | May create and modify the production files the authorized tasks require. Works only from task IDs named in a bounded batch. **May not** approve its own work |
| Independent reviewer | **`agy`** (`gemini-3.1-pro-high`) | Reviews frozen exact ranges through `scripts/agy-review.sh`. **May not** edit, stage, commit, push, or implement |
| Planner / coordinator | **Claude** | Prepares bounded batch handoffs, inspects evidence, validates ranges. **May not** give the independent implementation verdict — it authored this plan *and* the search-ownership correction |

**Implementation base: `f4269d4aad2d146547f7c1184ba2a6fec95bc818`** — the corrected planning head,
reviewed over `77656aec..f4269d4` to `APPROVE` with zero findings at every severity.
**Implementation has not started; 0 of 48 tasks are complete.**

> **`343aacb` is not a valid base and must not be built from.** Codex opened Batch 1 under
> [D-053](../../docs/DECISIONS.md#d-053--codex-availability-is-reverified-codex-implements-s3-and-agy-reviews-it),
> found that the approved search design could not be built against the committed S2 schema, and
> **stopped before editing any file** — no production file, migration, test, task closure, or commit was
> produced. That was the correct outcome: the defect was in the plan. The generated `search_text` column
> was specified as same-row *and* conditioned on Course publication, but `courses` holds no authored text
> and `course_revisions` holds no Course publication state, so no PostgreSQL generated column can satisfy
> both. See [R-006](research.md#r-006--which-table-owns-the-generated-search-column).
>
> T023, T023a, T026, T027, T032, and T034 were corrected on that pass, and T032a and T032b were added.
> **D-053's implementation authorization is spent and D-054's planning-correction seat is spent at
> `f4269d4`. Neither decision is edited retroactively** — each recorded a true state of affairs when
> written. D-053's product-owner Codex-availability reverification remains valid and is inherited by
> D-055 rather than re-requested.

**Codex availability was explicitly reverified by the product owner on 2026-07-30**, satisfying
[D-033](../../docs/DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review)'s precondition
positively rather than by inference from silence or from the absence of a quota error. That confirmation
is unaffected by the schema defect and carries forward under D-055.

**Work is handed over in bounded batches, not as all 48 tasks at once.** Each batch names its exact
task IDs and evidence, and freezes a range for independent review before the next begins. D-055 expires
when S3 closes on a recorded reviewer verdict.

**Batch 1 — authorized: T001, T002, T003, T004, T023, T023a, T024, T026, T035.** Storage and the
visibility foundation only. **The public search query is not authorized in Batch 1**, so T027 and the
exposure proofs T032, T032a, and T032b belong to a later frozen range. Note that T003's wording spans
list, detail, **and search**: in Batch 1 its search entry point carries no query semantics — no
ownership join under `PublishedOnly`, no normalization matching — and **T003 therefore stays unchecked
until T027 lands.**

**Batch 1 implementation evidence — 2026-07-30.** `GOCACHE=/tmp/gradex-go-cache go test
./...` passed from `backend`; `GOCACHE=/tmp/gradex-go-cache go test -tags=integration
./internal/db -run 'Test(MigrateUpDownUp|CatalogSearchMigrationSupportsCleanInstallAndUpgrade)$'
-count=1` passed against the local PostgreSQL test database; and `GOCACHE=/tmp/gradex-go-cache
go run ./cmd/migrate max-version` returned `11`. The integration proof covers clean `0001`→`0011`
installation, a schema-10 upgrade with pre-existing Published, Draft, and SUPERSEDED revisions,
generated-column backfill, Arabic normalization, and the `PublishedOnly` candidate condition. The
`catalogpublic` unit proof covers all four predicate exclusions, constructor refusal without its
required policy, and byte-identical public not-found responses. **T003 remains intentionally
unchecked:** Batch 1 adds only the construction/policy foundation; public `q` parsing, normalized
matching, live-revision search joins, and search-result semantics remain for T027, T032, T032a, and
T032b.

**Batch 2 implementation evidence — 2026-07-30.** `GOCACHE=/tmp/gradex-go-cache go test ./...`,
`go test -race ./internal/catalogpublic ./internal/httpapi ./cmd/api`, `go build ./...`, `go vet
./...`, `go vet -tags=integration ./...`, `go run ./cmd/migrate max-version` (`11`),
`scripts/docs-guard.sh`, `scripts/expose-guard.sh`, and `git diff --check` passed. Real PostgreSQL
evidence passed for the public visibility route test, both normally and under `-race`, and for the
Batch 1 migration acceptance suite, both normally and under `-race`. Checkpoint 2 mutations failed
their named proofs and were restored: removing `retired_at` from `PublishedOnly` made the retired
Course return `200`; a temporary GET route executing `SELECT 1 FROM courses` bypassed
`publicCatalogHandlers` and T007 named `/api/v1/catalog/catalog-table-leak`; a hidden-only `403`
branch made T008 compare `404` with `403`; a hidden-only detail string made T008 report differing
bodies; and temporary `POST /api/v1/catalog/checkout` made T007a name the non-read-only checkout
route. After every restoration, the affected proof passed; no mutation code remains. T003 remains
unchecked because its public search consumer is still deferred to T027, T032, T032a, and T032b.

**Batch 1–2 correction-gate evidence — 2026-07-30.** The local, unhosted `0011` definition was
corrected before feature-wide review: it now enables `pg_trgm` using the repository's extension
convention and creates `course_revisions_search_text_trgm_idx` with `GIN`/`gin_trgm_ops`; schema version
remains `11`, generated text remains on `course_revisions`, and no checksum-covered applied migration
changed. Real PostgreSQL proved multi-thousand-character Arabic/English revision insert and update,
normalized leading-wildcard matching, and `EXPLAIN` participation by that exact index, alongside clean,
upgrade, down, and up/down/up regression checks and existing S2 authoring writes. T007a now examines
every production `r.Routes()` entry for commerce terms while preserving public-prefix-only method and
anonymous checks. Its temporary `POST /api/v1/checkout/orders` mutation failed and named the route,
then passed after restoration. The behavioural public-route sweep materializes every derived catalogue
path and sends a valid session cookie through authentication/session/credential, principal/capability,
and entitlement tripwires; its temporary auth-wrapped `GET /api/v1/catalog/promoted` route failed and
named all invoked tripwires, then passed after restoration. `writeAnonymousProblem` is the shared
enumeration-safe writer: it omits client-visible request identifiers while the request context retains
server-log correlation; its response-shape and byte-identity tests pass. No correction changes task
status: T003 remains unchecked and all Phase 3+ work remains deferred.

**Phase 3 implementation evidence — 2026-07-30.** `0011` now also creates the generated stable
`courses.slug` (`course-` plus UUID without hyphens) and its unique lookup index. Real PostgreSQL
proves upgrade backfill, new-row generation, title/revision/live-pointer stability, and absence from
`course_revisions`; UUID and slug detail lookup both reach the shared `PublishedOnly` boundary, while
unknown/malformed slugs use the same anonymous 404 response. List/detail return only the live revision's
localized title/description, display name, taxonomy labels, stable slug, optional full-Course KWD price,
preview flag, and ordered section title/lesson-count outline. Full serialized payload assertions prove
no Section price, PII, Lesson title, Resource, Lab, owner, or review metadata; retired assigned taxonomy
still displays and a suspended Instructor's Published Course remains visible. T003 remains unchecked;
no public search, frontend, S4–S6, or commerce behavior was added.

**Phase 4 implementation evidence — 2026-07-30.** The public shell extends the existing locale provider:
catalogue routes bind document direction to their language-addressed URL, while legacy routes retain the
saved visitor preference. The browser suite proves Arabic RTL and English LTR list/detail renderings at
phone (390px), tablet (768px), and desktop (1440px): twelve screen combinations plus loading, empty,
safe-failure, anonymous-not-found, and stable-slug navigation. It asserts semantic landmarks, headings,
accessible names, focusable navigation, rendered accessibility snapshots, no viewport overflow, retired
taxonomy display, and the absence from both DOM and accessibility tree of Section prices, private email,
and Arabic/English commerce controls or vocabulary. `npm run typecheck`, `npm run lint`, `npm test`
(28 tests), the full Chromium suite (29 tests), and a clean production build passed; its build-artifact
sweep found no checkout, cart, coupon, buy-now, payment, purchase, or Section-price artifact. T003
remains unchecked because public search semantics remain for T027, T032, T032a, and T032b.

**Phase 5 implementation evidence — 2026-07-30.** `GET /api/v1/catalog/courses?q=` now uses one
PostgreSQL-owned normalization function and the existing list projection. The query joins revisions to
Courses only by `cr.course_id = c.id`; list, detail, and search each apply `PublishedOnly(c, cr)`, whose
live-revision clause is the sole selector. Before this implementation,
`GOCACHE=/tmp/gradex-go-cache go test -tags=integration ./internal/httpapi -run
'^TestPublicCatalogSearchUsesPublishedOnlyLiveRevision$' -count=1` failed because a query for a
historical title returned the live Course. The same real-PostgreSQL test now covers raw/folded Arabic,
tatweel, diacritics, Arabic-Indic digits, mixed Arabic/Latin text, display name and taxonomy matching,
empty-normalized equivalence, bounds/metacharacters, retired taxonomy display, live-pointer repointing,
and projection exclusion. It also establishes `search_text` for every hidden state before proving search
omits it. A deterministic `EXPLAIN` test disables sequential and plain index scans so the bitmap plan
names `course_revisions_search_text_trgm_idx`; this demonstrates eligible trigram participation without
claiming a tiny fixture must prefer it. The long-description migration/authoring regression remains green.

**T032b mutation evidence — 2026-07-30.** Removing only
`c.live_revision_id = cr.id` from `PublishedOnly`, then running the named live-revision test above,
failed with `search historical non-live text exposed course` and rendered `عنوان تاريخي فريد`; restoring
`PublishedOnly` verbatim made the same command pass. Replacing only Search's `visibility :=
r.visibility("c", "cr")` with `visibility := "TRUE"`, then running
`GOCACHE=/tmp/gradex-go-cache go test -tags=integration ./internal/httpapi -run
'^TestPublicCatalogSearchDoesNotExposeHiddenCourses$' -count=1`, failed with `search draft course
exposed course` and `عنوان مسودة مخفي`; restoration made the same command pass. A temporary Go
`normalizeCatalogueQuery` function was detected by the T025 grep, then removed. Final searches found no
Go/TypeScript catalogue normalizer, no mutation flag/comment residue, no frontend Section-price or
commerce artifact, and no S2 write-path change. The frontend forwards the raw query to the existing
route and the full Chromium suite (33 tests) proves Arabic RTL/English LTR search, accessible controls,
loading, no-results, safe-failure, stable-slug navigation, and phone/tablet/desktop rendering. Full Go,
race, real-PostgreSQL, frontend, build, vet, migration/max-version, exposure, documentation, clean-code,
test, and diff gates passed.

**Phase 6 local convergence evidence — 2026-07-30.** The production-server Chromium performance
spec uses a repeatable local `Network.emulateNetworkConditions` profile named **Kuwait 4G local
emulation**: 170ms latency, 4 Mib/s downlink, 1 Mib/s uplink, and `cellular4g`. Five samples per
screen produced p95 LCP **1352.0ms** for `/en/catalog` and **1348.0ms** for the slug detail, below the
2.5s target. The real-PostgreSQL launch-scale test seeds 12 Courses, proves `courses_pkey` can support
the list and canonical-UUID detail plans, and records 201µs `EXPLAIN ANALYZE` execution for the
deliberately unindexed Instructor/taxonomy joined-field search—well inside the 2.5s budget. The
real-PostgreSQL timing distribution samples 100 hidden and 100 absent lookups after warmup: hidden p95
264.984µs, absent p95 266.899µs, absolute delta 1.915µs against a documented 25ms tolerance. This is a
statistical observation, not a timing-indistinguishability claim.

The complete local Go suite, race packages, real-PostgreSQL catalogue/migration/S2-authoring suites,
build, both vet modes, schema max-version (11), frontend typecheck/lint/unit suite, 33-test Chromium
suite, clean production build, anonymous-route/exposure guard, migration checksum guard, documentation
guard, and diff guard passed. The clean-build T039a sweep covered exactly these emitted public
catalogue artifacts: `server/app/[locale]/catalog/page.js`, its client-reference manifest,
`server/app/[locale]/catalog/[idOrSlug]/page.js`, its manifest, and their two static route chunks. It
found no commerce, Section-price, private-email, or client-normalizer string. Repository scans found
no public commerce, S4–S6, Section-price, or application-normalization implementation; the only
S4–S6 terms are the pre-existing boundary documentation. The earlier named mutations for lifecycle,
anonymous 404, whole-router commerce, behavioural unauthenticated routes, live revision, hidden
search, and application normalization remain documented with fail/restore/pass evidence and no
mutation residue. **T039 remains unchecked:** its exact wording requires hosted CI on the final frozen
head, and no push or hosted-CI authorization exists.

*Historical, spent:* this file previously named Antigravity as builder and Claude as Tier 1 reviewer
under [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews).
That assignment is **not in force** and confers nothing. The Tier 1 review depth D-040 set for S3 is
unchanged.

**S2 has closed** on an independent verdict at `785d71c`, so the Phase 1 dependency is satisfied.

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

- [X] T001 Create `backend/internal/catalogpublic/doc.go` stating the module boundary: public reads
      only, no writes, no authority, reads S2's tables. State explicitly that this module implements
      **no** entitlement evaluation (S4), no protected delivery (S4), no progress (S5), and no
      invitation or Enrollment behaviour (S6) *(FR-020 boundary; SLICES.md §2)*
- [X] T002 Implement `PublishedOnly` in `backend/internal/catalogpublic/visibility.go` as the single
      exported predicate encoding **all four** exclusions together — lifecycle state, emergency access
      suspension, pending-revision selection, and the live-revision pointer. One condition, because it
      answers one question *(FR-002, FR-004, FR-005; BR-090, BR-017, BR-161)*
- [X] T003 Implement the repository in `backend/internal/catalogpublic/repository.go` with list,
      detail, and search. **Every** query obtains its rows through T002. The constructor **refuses to
      build** without the predicate — no nil, no default, no fallback *(FR-002; BR-161)*
- [X] T004 Implement the single not-found response constructor for the public surface. Hidden and
      absent Courses return byte-identical `404` Problem Details — same status, same body, same
      headers, no cause-varying `detail` field *(FR-003; BR-090)*

**Checkpoint 1** — the predicate exists, is unavoidable, and both not-found paths are identical.
No route is mounted yet.

## Phase 2 — Public routes and derived enforcement

- [X] T005 Register public routes under `/api/v1/catalog` in `backend/internal/httpapi/router.go`,
      per [contracts/catalogue-api.md](contracts/catalogue-api.md). Every route is unauthenticated and
      read-only *(FR-001)*
- [X] T006 Implement thin handlers: **no status comparison in a handler**, no second not-found
      constructor, no query built outside the repository *(FR-002, FR-003)*
- [X] T007 **The load-bearing test.** In `backend/internal/httpapi/catalog_public_test.go`, enumerate
      every route registered under the public prefix from `r.Routes()` and assert each is served
      through `PublishedOnly`. A new public route that queries the catalog tables directly **must fail
      this test.** Derive the route list; never hand-maintain it *(FR-002, SC-001)*
- [X] T007a **Route and exposure guard — what S3 must NOT add.** In the same derived-enumeration
      style as T007, assert over the **whole** application route table that this slice introduces:
      **no** non-`GET`/`HEAD` route under the public prefix; **no** route under the public prefix
      requiring or reading a session, credential, or capability; and **no** route whose path or handler
      names an order, checkout, cart, coupon, payment, payment-callback, webhook, refund, invoice, or
      entitlement concept. Derive the assertion from `r.Routes()`; a hand-maintained list is not
      evidence. **This test must fail if any such route is added later**, which is what makes the
      absence a property of the code rather than of this document *(FR-010a, FR-001;
      [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation);
      BR-020, BR-029; [contracts/catalogue-api.md](contracts/catalogue-api.md))*
- [X] T008 Prove the enumeration case: request every non-Published state by **exact identifier** and
      assert the response is identical to a never-existing identifier in **status, headers, response
      schema, and body**. This is the exact, provable guarantee — assert on the full response, not the
      status code. Timing is **not** claimed here; it is measured separately in T038
      *(FR-003, FR-004, SC-002)*
- [X] T009 Implement the detail lookup with the predicate **inside the query boundary**. Do **not**
      fetch-then-check in application code: that returns measurably faster for an absent row than for
      a hidden one, and closing that branch is **necessary but not sufficient** — see
      [plan.md](plan.md#the-timing-claim-stated-honestly). This task is where the mistake gets made by
      default *(FR-003, FR-005; BR-017)*

**Checkpoint 2 — MANDATORY REVIEW GATE.** Do not proceed to Phase 3 until T007, T007a, and T008 pass *and*
their mutations have been run. This is the only checkpoint in S3 that blocks on evidence rather than
on completion, because everything after it is presentation and cannot compensate for a leak here.

Required mutations, each of which must turn a test red:
1. Remove one of the four exclusions from `PublishedOnly` → T007 or T008 fails.
2. Add a public route that queries the catalog tables directly → T007 fails and **names that route**.
3. Make the hidden-Course path return `403` instead of `404` → T008 fails.
4. Add a cause-varying `detail` field to the hidden-Course response → T008 fails on the body
   assertion. Included because a differing body is the leak that survives a matching status code.
5. Register a `POST /api/v1/catalog/checkout` route → **T007a fails and names it.** Included because
   FR-010a's guarantee is only as good as its regression test.

## Phase 3 — Catalogue list and Course detail (backend)

- [X] T010 [P] Implement the paginated list projection: title, Instructor display name, three taxonomy
      dimensions in the active interface language, the **full-Course price only** in integer minor
      units, and preview availability *(FR-008, FR-010, FR-013; BR-157, BR-105, BR-158, BR-019)*
- [X] T011 [P] Implement the detail projection: authored description, Section outline, the
      **full-Course price**, and the preview reference. **Serve no Section price.** Section is **not an
      independently acquirable scope** in the MVP, so a per-Section price is not part of this
      projection and must not appear in the payload — not as a field, not as null, not as zero. The
      Course price is **informational external-payment guidance only**: it tells the Student what to pay
      out of band, and neither the payload nor any field name may imply Gradex takes payment. The
      Section outline keeps its titles, ordering, and Lesson counts — **only the price is removed**
      *(FR-009, FR-010, FR-010a;
      [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation);
      BR-010, BR-021, BR-019, BR-020)*
- [X] T011a Assert the **absence** of any Section price in the detail payload, against the full
      serialized response rather than the rendered page, for a Course whose Sections carry authored
      prices in S2's tables. S2 still stores Section prices and the Admin surface still shows them
      (D-045 resolved question 2); this test is what keeps them from reaching a public payload
      *(FR-009, FR-010a; BR-021)*
- [X] T012 Assert **no PII beyond display name** appears in any public response, against the full
      response body rather than the rendered page. Email, phone, and legal identity must be absent
      from the serialized payload, not merely unrendered *(FR-011, SC-007; BR-105, BR-064)*
- [X] T013 Assert Lesson titles, Resources, and Lab Materials are absent from every public response
      *(FR-006; BR-143, BR-161)*
- [X] T013a Assert a **retired** taxonomy term still displays on a Course that already carries it, in
      both the list and the detail projection. Retirement blocks new assignment, not the display of an
      existing one, and dropping it silently changes what a Course appears to be about
      *(FR-014; BR-160)*
- [X] T014 Assert a Published Course whose owning Instructor is **suspended** remains publicly visible
      — suspension blocks authoring, not Student access (BR-065). This is an easy over-correction and
      the test exists to catch it *(FR-007; BR-065)*

**Checkpoint 3** — the public API returns correct content and leaks nothing. Frontend has not started.

## Phase 4 — Bilingual responsive shell (frontend)

- [X] T015 Extend `frontend/src/lib/i18n` — **extend, do not replace.** A second locale mechanism is a
      second source of truth for what language the user chose *(FR-020)*
- [X] T016 Implement the public shell layout with document direction bound to locale: RTL for Arabic,
      LTR for English, applied to navigation, forms, lists, and iconography *(FR-017, FR-020; BR-149)*
- [X] T017 Default to Arabic for a visitor with no stored preference; persist a chosen language across
      navigation and across sessions, for anonymous and authenticated visitors alike
      *(FR-015, FR-016, SC-003; BR-149)*
- [X] T018 [P] Implement the catalogue list screen, rendering the list projection including the three
      taxonomy dimensions in the active interface language *(FR-008, FR-013; BR-157, BR-158)*
- [X] T019 [P] Implement the Course detail screen, including the Section outline and the
      **full-Course price only**. **Render no Section price and no per-Section price column, row, or
      badge** — Section is not an independently acquirable scope in the MVP. Present the Course price as
      **informational external-payment guidance**; the page may link to guidance on how to obtain
      access, and may not imply Gradex takes payment *(FR-009, FR-010, FR-010a;
      [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation);
      BR-010, BR-021, BR-019, BR-020)*
- [X] T019a **Render no commerce control.** Neither the catalogue list nor the Course detail screen may
      render a checkout button, an add-to-cart control, a cart indicator, a coupon or promo-code field,
      a buy-now control, a price-total or discount computation, or any control that submits toward a
      purchase. There is no client route, form action, or fetch target for checkout, cart, coupon,
      payment, or payment callback. An informational link explaining how to obtain access is permitted
      and is the **only** access-related affordance on these screens *(FR-010a, FR-010;
      [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation);
      BR-020, BR-029)*
- [X] T020 Render the optional public preview with no empty player frame when absent; ensure no
      protected media is reachable from the page *(FR-012, FR-006; BR-143)*
- [X] T021 Present Instructor-authored content in its authored language under either interface
      language, with no machine translation (BR-150) *(FR-019; BR-150)*
- [X] T022 Responsive audit at phone, tablet, and desktop widths in **both** directions: no clipping,
      mirroring, or overlap *(FR-018, FR-017, SC-004)*
- [X] T022a **Rendered-browser commerce-absence evidence.** In the Playwright suite, assert against the
      **rendered DOM and accessibility tree** — not the source and not the API payload — that no
      checkout, cart, coupon, buy-now, or purchase-submission control is present on the catalogue list
      or the Course detail screen, and that no Section price is rendered. Run the full matrix:
      **Arabic RTL and English LTR × every S3 viewport (phone, tablet, desktop)**, which is six
      renderings per screen. Assert by accessible role and name plus a text sweep for the localized
      commerce vocabulary in **both** languages, so an Arabic-only checkout affordance cannot pass an
      English-only assertion. Record the artefacts alongside the T022 responsive evidence
      *(FR-010a, FR-009, FR-017, FR-018; SC-004;
      [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation))*

**Checkpoint 4** — a visitor can browse and evaluate a Course in Arabic and English.

## Phase 5 — Search and Arabic normalization

> **Red-first is mandatory in this phase.** Every test below is written and **observed failing**
> before its implementation exists, and the failure output is recorded. Arabic normalization is the
> one area of this slice where a test can pass for the wrong reason — an English-only assertion looks
> identical whether folding works or is absent entirely — so "I wrote the test after and it passed" is
> not evidence here.

- [X] T023 Add migration `0011_catalog_search`, the **next** number after committed
      `0010_revision_integrity`. Additive only: the `catalog_normalize_ar` Arabic-normalization
      function; the generated `search_text` column **on `course_revisions`**; the generated
      `courses.slug` column; the unique `courses_slug_unique_idx`; and the
      `course_revisions_search_text_trgm_idx` trigram index. `ALTER TABLE course_revisions` — the
      table that actually holds the authored text; **not** `courses`, which holds no title or
      description at all. No constraint on any existing table, and **no modification of any existing
      migration file** — their checksums are enforced by `scripts/docs-guard.sh`. **Clarified
      2026-07-30 after final feature-wide review; migration `0011_catalog_search` remains the
      authority.**
      *(FR-002, FR-021 storage support; data-model.md §Migration `0011_catalog_search`)*
- [X] T023a **Verify the backfill and the upgrade path, do not assume them.**
      `ALTER TABLE … ADD COLUMN … GENERATED … STORED` computes the value for existing rows as part of
      the statement. Assert after `up`, on a database seeded **before** the migration ran:
      **(a)** every pre-existing `course_revisions` row has a non-empty `search_text` — every row, not
      only those belonging to Published Courses; **(b)** a known Arabic title is present in folded form,
      so the assertion proves normalization rather than mere non-emptiness; **(c)** the live revision of
      a pre-existing Published Course is reachable by a search for its authored text; **(d)** a Draft
      revision and a `SUPERSEDED` revision **do** carry generated text and are **still** absent from
      every public result. Run both paths: clean install (`0001`→`0011` on an empty database) and
      upgrade (schema 10 with real rows, then `0011`), and confirm they agree. A migration that leaves
      the existing catalogue unsearchable passes every schema assertion while being obvious to a
      visitor *(FR-021, FR-023, FR-002; data-model.md §3)*
- [X] T024 Implement `catalog_normalize_ar` as an `IMMUTABLE STRICT` SQL function — **the single
      definition of normalization in the system.** Exactly the transformations in
      [data-model.md](data-model.md#1-the-normalize-function--the-single-definition): alef/hamza
      folding, alef maqsura, taa marbuta, Arabic-Indic digits, tashkeel and tatweel removal, Unicode
      case folding, and whitespace collapse *(FR-023; BR-162 matching only — ranking is not in S3)*
- [X] T025 **Write no Go normalization function.** The incoming query is normalized by calling the
      same SQL function inside the query. If a Go helper appears, write/query asymmetry has become
      representable again and the guarantee is gone. The review checks for its absence
- [X] T026 **Generate `search_text` on `course_revisions`, from that row's own authored columns, for
      every revision.** Exactly:
      - The column is added to **`course_revisions`** — the table that owns the authored text. Adding it
        to `courses` is impossible, not merely discouraged: `0009_course_authoring` dropped that table's
        stub `title` and it never had a description.
      - The expression reads **only same-row columns of `course_revisions`**: `title_ar`, `title_en`,
        `description_ar`, `description_en`. Use those committed names; do not introduce `title` or
        `description` aliases and do not add authored fields to `courses`.
      - **No cross-table dependency of any kind.** The expression must not reference `courses`,
        `accounts`, or `taxonomy_terms`. PostgreSQL forbids it, and the workarounds — a trigger, an
        application-written column, a materialized search table — are all rejected by
        [R-006](research.md#r-006--which-table-owns-the-generated-search-column).
      - Keep `coalesce` on all four columns. They are `NOT NULL` today, so it changes nothing now; it
        prevents `catalog_normalize_ar`'s `STRICT` behaviour from nulling the whole document, and
        silently dropping the revision from search, if a later migration relaxes one of them.
      - **Generate it for every revision row — no publication condition.** There is deliberately **no**
        `WHERE`, no partial index, and no expression testing lifecycle. A populated `search_text` is
        **not** a claim that the row is publicly visible; it is storage. Exposure is decided by
        `PublishedOnly` — including its live-revision clause — and by nothing else.
      - Add the approved index over `course_revisions (search_text)` — a substring-match structure, not
        a ranking one (FR-023b).

      *This task previously required population for Published Courses only. That requirement was
      withdrawn on 2026-07-30 as unbuildable — see
      [R-006](research.md#r-006--which-table-owns-the-generated-search-column). It was documented as
      redundant with `PublishedOnly`; T032a and T032b now carry that redundancy as runnable proofs.*
      *(FR-021, FR-002; BR-161; data-model.md §2)*
- [X] T027 **Build the public search query: live revision, published Course, generated column.** The
      search must:
      - join `course_revisions` to `courses` through the committed **ownership** foreign key —
        `course_revisions.course_id = courses.id` — and stop there. The join establishes which Course a
        revision belongs to; it decides **nothing** about visibility;
      - apply the **same** `PublishedOnly` predicate from T002 that the list and detail routes use as
        the **sole** authority narrowing that ownership join to the live revision. `PublishedOnly`
        already carries `courses.live_revision_id = course_revisions.id`; the search query **must not
        repeat it** in a search-specific join condition. One clause, one enforcement point — a second
        copy is what makes a mutation survivable and a control unlocatable (FR-022);
      - match the normalized query against `course_revisions.search_text` on that predicate-narrowed
        row set;
      - **never** search a Course's historical revisions as public candidates. That exclusion is
        `PublishedOnly`'s live-revision clause doing its work, not a separate search rule;
      - normalize the joined fields — Instructor display name, taxonomy labels and code — **at query
        time through the same function**, on the same live-revision row set.
      *(FR-021, FR-022, FR-023, FR-005; BR-161, BR-017; plan.md §Exposure boundary)*

### Red-first test set — each observed failing before its implementation

- [X] T028 [RED] **Normalized Arabic queries.** `احياء` matches a Course titled `أحياء`; a query
      written with tashkeel matches a title without it; a tatweel-padded query matches; Arabic-Indic
      digits match Western ones. Each assertion must fail before T024 exists *(FR-023; BR-162)*
- [X] T029 [RED] **Mixed Arabic/Latin content.** A Course whose title mixes both scripts is matched by
      an Arabic fragment and by a Latin fragment; a Latin query is case-insensitive; normalization of
      the Arabic portion does not corrupt the Latin portion *(FR-023, FR-021; BR-162)*
- [X] T030 [RED] **Write/query symmetry.** Assert matching in **both** directions — stored-folded
      against raw query, and raw stored against folded query. This is a regression guard: T025 makes
      the asymmetry unrepresentable, and this test is what notices if someone reintroduces a Go
      normalizer *(FR-023)*
- [X] T031 [RED] **Empty normalized query.** A query of only diacritics, only tatweel, or only
      whitespace normalizes to empty and MUST behave exactly as an absent query — the unfiltered
      published list, not an error and not an empty result (FR-023a, SC-009)
- [X] T032 [RED] **Unpublished non-disclosure through search.** A Lesson title, a Resource filename,
      a Draft Course's title, and a Delisted Course's title each return nothing — including when
      queried in normalized Arabic form. Normalization must not become a path around `PublishedOnly`.
      **Assert the harder version of this now that storage no longer filters:** for each of a `DRAFT`,
      `PENDING_REVIEW`, `CHANGES_REQUESTED`, `DELISTED`, and `ARCHIVED` Course, plus a retired Course
      and a Published Course under an active `access_suspended_at` suspension, first confirm the
      revision row **does** hold matching `search_text`, then confirm the public search returns nothing
      for it. A test that only checks the absence of the result would also pass if the text were never
      generated, and would therefore stop proving anything the day generation changed
      *(FR-006, FR-002, FR-022, SC-005; BR-143, BR-161)*
- [X] T032a [RED] **The live-revision exposure boundary.** This is the task that carries the guarantee
      the withdrawn population boundary used to claim, and each assertion must fail before T027 exists:
      1. A Published Course **is** found by text authored on its **live** revision.
      2. A non-live revision of that same Course carrying **identical** matching text returns nothing —
         the same query string, so the only difference is which revision is live.
      3. A `SUPERSEDED` historical revision is not returned while another revision is live, even though
         its `search_text` is populated and indexed.
      4. Repointing `courses.live_revision_id` to a different revision changes which authored text is
         searchable **immediately and in both directions** — the new text matches, the old text stops
         matching — with **no** text copied into `courses` and no S2 write path involved. Assert that
         `courses` gained no title, description, or search column.
      5. Search and detail agree: the text that matches is the text the detail projection renders,
         because both resolve through `PublishedOnly`'s live-revision clause.
      *(FR-005, FR-002, FR-021, FR-022; BR-017, BR-161; plan.md §Exposure boundary)*
- [X] T032b **Mutation proofs for the two exposure controls.** Both must be named, run, and their
      failure output recorded — this is the redundancy that replaced the population boundary, and an
      unrun mutation is prose. Both mutate `PublishedOnly` itself, because that is where both controls
      live; neither requires touching a search-specific join, and **no mutation may require weakening
      two places at once.** A control that takes two edits to break is a control this plan cannot
      locate:
      - **Live-revision mutation.** (1) Temporarily remove or weaken **only** the
        `live_revision_id = id` clause inside `PublishedOnly`, leaving its other three exclusions
        intact. (2) Run the T032a historical-revision test. (3) Confirm it fails by proving matching
        text on a **non-live** revision has become publicly searchable — a Course returned through a
        revision that is not its live one. (4) Restore `PublishedOnly` verbatim. (5) Re-run the same
        test and confirm it passes. (6) Confirm no mutation residue remains — `git diff` against the
        pre-mutation tree is empty and no commented-out or flag-guarded variant survives.
      - **Published-only mutation.** Remove **only** the `PublishedOnly` predicate from the search
        query and confirm a named test in T032 fails by returning a non-Published Course. Restore, and
        confirm the same test passes with no residue.
      Neither mutation may be survivable. If either passes, the control is not where this plan says it
      is *(FR-002, FR-005, FR-022, SC-005; standing clause on mutation evidence)*
- [X] T033 [RED] **Degenerate input.** Empty, whitespace-only, 10 KB, and `%' OR 1=1 --` queries each
      return a well-formed result and never an error disclosing internals (FR-024)
- [X] T034 Confirm no stemming, fuzzy matching, ranking, or external search dependency was introduced
      (FR-023b). Result ordering is stable and documented, not scored. Confirm in the same task that
      **no personalization, recommendation, or paid-placement input** reaches result selection or
      ordering — no visitor identity, no session, no behavioural signal, and no sponsored or boosted
      flag. Confirm three further absences, each of which is a rejected alternative from
      [R-006](research.md#r-006--which-table-owns-the-generated-search-column) rather than a style
      preference:
      - **The generated expression reads same-row `course_revisions` columns only.** Read the column's
        definition back out of `information_schema`/`pg_attrdef` and assert it names no other table.
        Assert `courses` has no title, description, or search column.
      - **No Go catalogue-normalization function exists** anywhere in the backend (reinforces T025,
        T030).
      - **No S2 authoring or publication write path was modified.** No trigger on `courses` or
        `course_revisions`, and no change to S2's authoring or publication transactions. `git diff`
        against the implementation base touches no S2 write path
      *(FR-023b, FR-025, FR-021; BR-161; R-006)*
- [X] T035 Add `CatalogSearchSchemaVersion = 11` and raise `db.MaxSchemaVersion` to it, matching the
      per-migration named-constant pattern in `backend/internal/db/schema.go`. **The current value is
      already 10** (`RevisionIntegritySchemaVersion`), so `0011_catalog_search` makes the new maximum
      **11**. Confirm CI **derives** its assertion from the constant via `migrate max-version` rather
      than carrying a literal *(FR-002 storage support; data-model.md §Schema version)*

**Checkpoint 5** — search finds published Courses in both languages **through their live revisions**,
reveals nothing else, T032b's two mutations were run and both failed as required, and every red-first
test was observed failing first.

## Phase 6 — Performance and polish

- [X] T036 Verify SC-006: p95 LCP under 2.5s on representative Kuwait 4G for the list and detail pages
      *(SC-006; PRD §Non-Functional)*
- [X] T037 Confirm indexes support the list and detail paths, and that the unindexed joined-field
      search stays inside budget at launch catalogue size ([R-005](research.md#r-005--the-cross-table-constraint))
      *(SC-006 supporting evidence)*
- [X] T038 **Timing-distribution check (SC-008).** Sample hidden-identifier and absent-identifier
      lookups, compare the distributions against a **documented tolerance**, and record the result as
      a statistical observation. **No nanosecond-equality assertion**, and no wording that calls a
      statistical property proven. Outside tolerance is a finding with an owner; inside tolerance is
      not a guarantee *(SC-008, FR-003)*
- [X] T039 Run the full gate suite from [quickstart.md](quickstart.md), including a **clean** frontend
      build with `.next` removed first, on hosted CI at the exact reviewed head. **Hosted evidence:**
      GitHub Actions [CI run 30590627163](https://github.com/Owlah2025/gradex/actions/runs/30590627163)
      tested `c05b8f3fb11ff238a4b94484eba6423197b64445` on
      `feature/002-authentication-rbac` and completed successfully. Guards passed the Documentation
      and Secret exposure guards; Frontend passed lint, typecheck, tests, and production build;
      Migrations passed migration and schema-integration evidence; Backend passed build, vet, and
      tests; and Admission Integration passed its database, identity, outbox, HTTP API, public
      catalogue, and rate-limit integration packages. Its hosted command includes
      `./internal/catalogpublic`, so the integration-tagged package run includes
      `TestDetailSecondaryReadsRecheckPublishedOnly`. This hosted record does **not** claim the
      local-only Playwright browser, RTL/LTR responsive, accessibility, production-LCP, or mutation
      evidence.
- [X] T039a **Production-build commerce-absence evidence.** Against the **locally produced clean
      production build** — not the dev server — sweep the emitted client bundle and server output and assert the
      absence of any checkout, cart, coupon, buy-now, purchase-submission, payment, payment-callback,
      or webhook route, form action, fetch target, or handler, in either language's rendered output.
      A control absent from the dev render but shipped by a conditional build is the failure this task
      exists to catch. T039 is the separate hosted-CI confirmation on the reviewed head. Record the artefact paths in the daily record *(FR-010a;
      [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation);
      BR-020)*

---

## Dependencies

| Phase | Blocked by | Why |
|---|---|---|
| 1 | S2 closing — **satisfied**, S2 closed at `785d71c` | Reads S2's tables; the graph must exist and be stable |
| 2 | Phase 1 | Routes cannot be safe before the predicate exists |
| 3 | **Checkpoint 2** | Hard gate on evidence, not completion |
| 4 | Phase 3 | Screens render the API's projections |
| 5 | Phase 1 | Search reuses `PublishedOnly`. OD-001 is resolved, so the shape is fixed |
| 6 | Phases 3–5 | Measures the finished paths |

**Phase 5 may run in parallel with Phase 4** — they share no file. Phase 3 may
not overlap Phase 2: it depends on the checkpoint.

## Parallel opportunities

`[P]`-marked tasks touch disjoint files: T010/T011, and T018/T019. T007a runs with T007; T011a with
T011; T013a with T013; T019a with T019; T022a with T022; T039a after T039's clean build. T032a runs
with T032 — they share a fixture; **T032b runs last in Phase 5**, because a mutation proof needs the
tests it mutates to be passing first.

## Review checkpoints

| Checkpoint | Blocks | Evidence required |
|---|---|---:|
| 1 | Phase 2 | Predicate exists; constructor refuses to build without it |
| **2** | **Phase 3 — hard gate** | T007, T007a, and T008 pass **and** all five mutations turn a test red |
| 3 | Phase 4 | No PII, no Lesson content, no protected media, and **no Section price** in any public response |
| 4 | — | Responsive audit passes in both directions at three widths; T022a's six-rendering commerce-absence matrix passes |
| 5 | — | All leak cases and degenerate inputs proven; **every red-first test observed failing before implementation** |
| Final | Slice closure | Full gate suite including T039a; **Tier 1 independent review by whichever seat holder did not author the range**, on a frozen exact range |

## Requirement and success-criterion coverage

Constitution **Principle III** requires traceability per requirement, and it is not tier-dependent.
Every active FR below names at least one implementing or evidencing task, and every SC names an
explicit verification task. Recorded on 2026-07-30.

| Requirement | Tasks |
|---|---|
| FR-001 public unauthenticated routes | T005, T007a |
| FR-002 published-only from one predicate | T002, T003, T006, T007, T027, T032, T032b, T035 |
| FR-003 hidden is indistinguishable from absent | T004, T006, T008, T009, T038 |
| FR-004 emergency suspension excluded | T002, T008, T032 |
| FR-005 live revision only | T002, T009, T027, T032a, T032b |
| FR-006 no Lesson, Resource, or Lab exposure | T013, T020, T032 |
| FR-007 suspended Instructor stays visible | T014 |
| FR-008 paginated list projection | T010, T018 |
| FR-009 detail projection, **no Section price** | T011, T011a, T019 |
| FR-010 price exactly as authored | T010, T011, T019, T019a |
| FR-010a **no commerce control** | T007a, T011a, T019, T019a, T022a, T039a |
| FR-011 display name only, no PII | T012 |
| FR-012 public preview without authentication | T020 |
| FR-013 three taxonomy dimensions, active language | T010, T018 |
| FR-014 retired taxonomy term still displayed | T013a |
| FR-015 Arabic by default | T017 |
| FR-016 language persists across navigation and sessions | T017 |
| FR-017 RTL/LTR direction | T016, T022, T022a |
| FR-018 phone, tablet, desktop | T022, T022a |
| FR-019 authored language, no machine translation | T021 |
| FR-020 the shell is the single foundation | T001, T015, T016 |
| FR-021 match title, description, name, taxonomy | T023a, T026, T027, T029, T032a, T034 |
| FR-022 search reuses the FR-002 predicate | T027, T032, T032a, T032b |
| FR-023 one normalization function | T023a, T024, T025, T027, T028, T029, T030 |
| FR-023a empty normalized query behaves as absent | T031 |
| FR-023b no stemming, fuzzy, ranking, external service | T034 |
| FR-024 degenerate input never errors | T033 |
| FR-025 no personalization, recommendation, paid placement | T034 |

**28 of 28 active FRs cited.** No FR is deferred out of S3. Three requirements had **no** task before
this pass — FR-010a, FR-014, and the personalization half of FR-025 — and each now has one; the rest
were substantively covered but uncited.

| Success criterion | Verification task |
|---|---|
| SC-001 zero non-Published in any response | T007 |
| SC-002 identical response for hidden and absent | T008 |
| SC-003 Arabic + RTL first visit, preference survives | T017 |
| SC-004 renders at three widths in both directions | T022, T022a |
| SC-005 search reveals no unpublished content | T032, T032a, T032b |
| SC-006 p95 LCP under 2.5s | T036, T037 |
| SC-007 no PII beyond display name | T012 |
| SC-008 timing distribution within documented tolerance | T038 |
| SC-009 empty normalized query returns unfiltered list | T031 |

**9 of 9 SCs have an explicit verification task.**

**Deferred, and therefore uncited by design:** relevance ranking, multi-dimension filtering, and sort
options stay in S18 under OD-001, and FR-023b makes their absence a requirement rather than a gap.
BR-162's *"ranked by relevance"* clause is **not** claimed by S3 — see the traceability note in
[spec.md](spec.md#functional-requirements).

## Task count

**48 tasks** (T001–T039 plus T007a, T011a, T013a, T019a, T022a, T023a, T032a, T032b, T039a). Six tasks
were added earlier on 2026-07-30 to close requirement-coverage gaps: T007a and T039a plus T019a and
T022a for FR-010a, T011a for FR-009's Section-price prohibition, and T013a for FR-014.

**Two tasks were added later the same day by the search-ownership correction: T032a and T032b.** They
are not scope growth. The withdrawn population boundary claimed a layer of defence in storage that
cannot be built against the committed S2 schema; T032a asserts the live-revision exposure boundary that
actually enforces it, and T032b runs the two mutations that prove both controls are load-bearing. A
redundancy that can be executed replaces one that could only be asserted. No task was removed and no
requirement was weakened; T026 and T027 were rewritten in place and keep their identifiers.

**Phase 5 planning correction — 2026-07-30. No task added, removed, or renumbered; the count stays at
48 and no checkbox changed.** The plan previously required T027 to carry
`courses.live_revision_id = course_revisions.id` as its own join condition *and* to apply
`PublishedOnly`, which already contains that clause. T032b's first mutation then asked for the join
condition alone to be removed — a mutation that **could not turn a test red**, because `PublishedOnly`
would still have enforced the boundary. The proof was unsatisfiable as written, and it would have been
discovered only after the test was built.

Corrected in place across [tasks.md](tasks.md), [plan.md](plan.md), [data-model.md](data-model.md),
[research.md](research.md), and [quickstart.md](quickstart.md): the search joins `course_revisions` to
`courses` on the committed ownership foreign key, `PublishedOnly` remains the **single** authority
selecting the live revision, and T032b's live-revision mutation weakens that clause **inside
`PublishedOnly`** — one enforcement point, one edit, one red test, then restore and re-prove. No
mutation in this slice may require weakening two places at once. The contract
([contracts/catalogue-api.md](contracts/catalogue-api.md)) describes externally observable behaviour
only and needed no change.

Phase 5 previously grew from 8 to 13 with OD-001's resolution and the red-first requirement; the slice
estimate moves from 8h to **11–13h** with the coverage additions, and the correction's two tasks add
roughly **1h** for a range of **12–14h**. That increase is reported rather than absorbed — see
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

The 2026-07-30 coverage pass added a further **+1–2h**, almost entirely evidence rather than behaviour:
the six-rendering RTL/LTR × viewport commerce-absence matrix in T022a and the production-build sweep in
T039a. Recorded here rather than absorbed. Proving a control's **absence** costs more than rendering
one, and that cost is the point — D-045 removed the feature, and only a derived, failing-on-regression
assertion keeps it removed.

## MVP scope

Every task above is required for launch except T036–T038, which are measurement rather than behaviour
and may be recorded as findings if a target is missed, per PLAN.md's rule that a performance target is
not waived silently. T038 in particular reports an observation; it cannot "fail" into a guarantee.

---

## Amendment — 2026-08-11, post-independent-review remediation (T040–T045)

**These tasks did not exist during the original S3 review and are not part of its completion record.**
Everything above closed on its own evidence and is neither reopened nor unchecked here. These six were
added on 2026-08-11 after the **first valid independent review** of the integrated launch range
`18fb7e0..48e1f3f` returned `VERDICT: REJECT`. All three of its High findings are owned by this spec:

- **H1 — the landing page renders fabricated Courses, fabricated prices and fabricated testimonials.**
- **H2 — public navigation contains dead routes:** `/courses`, `/dashboard`, `/about`, `/teach`,
  `/contact`.
- **H3 — the FAQ tells users Tap hosted checkout exists**, contradicting
  [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).

Authority:
[D-084](../../docs/DECISIONS.md#d-084--the-independent-review-of-the-integrated-launch-range-returned-reject-and-bounded-remediation-of-its-seven-findings-is-authorized).
This is a truthfulness remediation, not a marketing redesign. It adds no commerce: no checkout, no
payments, no refund flow, no KNET or Apple Pay. Removing a false claim is in scope; building the thing
it claimed is not.

- [ ] T040 Bind the landing page's Featured Courses to the authoritative published public catalogue
      API used by `/[locale]/catalog`. Remove `frontend/src/data/courses.ts` and every other
      fixture, demo or fabricated Course source from production UI, including the fabricated prices.
      Nothing on a production surface may render a Course the catalogue does not publish.
- [ ] T041 Render an honest empty state when the published catalogue is empty, and a real error state
      when it cannot be read. An empty catalogue never falls back to sample Courses, and a failed load
      never renders as "no Courses".
- [ ] T042 Reconcile every production-visible link against the real App Router routes, covering at
      minimum `/courses`, `/dashboard`, `/about`, `/teach` and `/contact` in landing CTAs, header and
      footer. Each one either points at the authoritative existing locale-aware route — such as
      `/[locale]/catalog` or `/[locale]/learn/dashboard` — or the link is removed because the
      destination is not in launch scope. **No placeholder page may be created merely to stop a 404**,
      and the result must agree with [`docs/NAVIGATION_MAP.md`](../../docs/NAVIGATION_MAP.md),
      [`docs/NAVIGATION_RULES.md`](../../docs/NAVIGATION_RULES.md) and
      [`docs/SCREENS.md`](../../docs/SCREENS.md).
- [ ] T043 Rewrite or remove the FAQ copy that claims Tap hosted checkout exists so that public copy
      describes only the external/manual sales and Admin-approved Course Access model the repository
      actually authorizes under D-045. In the same narrow sweep, correct any directly related
      surviving D-045 or
      [D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch)
      claim the sweep surfaces — deferred-commerce and deferred-community wording only. Do not extend
      this into a general copy rewrite.
- [ ] T044 Remove or hide the Testimonials surface until Product Owner-approved testimonials from real,
      consenting people exist. Fabricated testimonials must not ship, and no replacement quotes,
      names, photographs or institutions may be invented to fill the space.
- [ ] T045 Add link-integrity and deferred-commerce regression evidence: an automated check that every
      production-visible public link resolves to a real route, and an assertion that public copy makes
      no in-platform checkout, payment or refund claim. Both must fail if the H2 or H3 defect returns.

**Amended task count:** 54 tasks — the 48 recorded above, complete, plus 6 post-review remediation
tasks, all open.
