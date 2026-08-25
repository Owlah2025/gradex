# T6 — Academic Course Discovery

**Date:** 2026-08-23
**Tranche:** MVP-F22 (T6)
**Status:** `T6 PROVEN`
**Authority:** D-091; `SCREENS.md` ST01; MVP tracker gap **ST-03**
**Builds on:** T1–T4 (migrations 0023–0026) and T5

---

## 1. What changed

Before this tranche the public catalogue was a flat list. `GET /api/v1/catalog/courses` accepted only
`q` and paging, and the Academic Catalog — Institutions, Programs, Curricula, Subjects, and the
mappings between them — existed but drove nothing a Student could see. ST-03 was `BACKEND_MISSING`.

The catalogue is now driven by the canonical academic model: a visitor chooses a University, then a
Program, then an academic level, then a Subject, all by name, and reaches a Course.

## 2. The audience rule — the heart of T6

A Course reaches a Program's Students one of exactly two ways, and they are mutually exclusive by
construction (`catalogpublic/academic_filters.go`, `ProgramAudiencePredicate`):

**EXPLICIT.** The live revision carries `course_program_targets` rows. Those rows **are** the
audience. The Course is discoverable under exactly the Programs they name and no others, even when
its Subject appears in other Programs' curricula. An Instructor who narrowed the audience
deliberately must not have that narrowing widened by inference.

**AUTOMATIC.** The live revision carries no target rows at all. Zero rows means *"use the audience
the Subject implies"* (migration 0025 §4), never *"no audience"*, so the Course is discoverable under
every Program whose **active** Curriculum maps its Subject.

Both branches are `EXISTS` subqueries, not joins. That is what makes a Subject mapped into five
curricula still yield **one** row for its Course: `EXISTS` answers yes or no, it does not multiply
rows. `TestT6SubjectInManyCurriculaNeverDuplicatesACourse` proves this against real mapping rows.

This is inference **for reading only**. No target row is ever written by a read, and a Student
browsing the catalogue mutates nothing.

## 3. Filters

| Filter | Parameter | Authority read |
|---|---|---|
| University | `institution` (slug) | `courses.institution_id` via `institutions.slug`, active only |
| Program | `program` (slug) | the audience rule above |
| Academic level | `level` (1–12) | `curriculum_subjects.recommended_level` in an **active** Curriculum |
| Subject | `subject` (official code) | `courses.subject_id`, matched on `code_normalized` |
| Search | `q` | unchanged |
| Relevance | `relevant_to_program` (slug) | ordering only — see §6 |

Every filter is `AND`-ed onto `PublishedOnly`, which is applied first and unconditionally. **No
combination of query parameters can widen the visible set past the publication rule** — a filter can
only remove Courses. `TestT6AcademicFiltersNeverExposeNonPublicCourses` drives every filter
combination against Draft, Pending Review, Changes Requested, Delisted, Archived, and
emergency-suspended Courses and asserts only the published one is ever returned. There is no second
publication definition: `catalogpublic.PublishedOnly` remains the only one.

Pricing behaviour is untouched. The price projection, the public-listing rules, and the purchase
request path are exactly as they were.

**Academic level is real study-plan data**, taken from the Founder's manifest. It is never derived
from a Course title, a revision column, or a study year. A Subject the manifest records no level for
does not match a level filter rather than being given an invented one, and the level chooser offers
only levels a study plan actually records. **Semester is deliberately not exposed** — `SCREENS.md`
ST01 does not require it and inventing a semester dimension is exactly what D-091 removed.

An unknown, retired, or malformed filter value matches nothing and yields an **empty catalogue**,
never an error. That is the correct answer to a stale shared link.

## 4. API

```
GET /api/v1/catalog/courses
      ?institution=<slug>&program=<slug>&level=<n>&subject=<code>
      &q=<text>&relevant_to_program=<slug>&page=&page_size=

GET /api/v1/catalog/academic-options/institutions
GET /api/v1/catalog/academic-options/institutions/:slug/programs
GET /api/v1/catalog/academic-options/institutions/:slug/levels[?program=<slug>]
GET /api/v1/catalog/academic-options/institutions/:slug/subjects[?program=<slug>]
```

The option endpoints are the **smallest safe public surface**. They are deliberately not the Admin or
Student academic endpoints: those are authenticated and expose retired rows and audit metadata, and a
public page must never call an authenticated one. Each option returns a public value plus both
language names — no identifier, no audit field, no retired row. Retired Institutions, Programs, and
Subjects are excluded from every list, so a retired entity can never be offered for a new selection
while a Course that references one keeps its reference.

Only approved canonical Subjects appear. A pending Subject Request is not a Subject and never becomes
a public filter; the Instructor Subject Request workflow is untouched.

`GET /api/v1/catalog/courses/:idOrSlug` gained `program_audience`: the localized Program **names** a
Course reaches, by the same explicit-or-inferred rule, as a `UNION` of two disjoint sets so a Subject
in several curricula yields one name. Preview, price, purchase request, instructor information, and
localized Course fields are unchanged.

### Caching

A filtered response is still shared and cacheable (`public, max-age=60`). A **ranked** response is
personalised, so it is returned `private, no-store` — the shared cache entry would otherwise leak one
Student's ordering to every visitor.

## 5. One new field outside `catalogpublic`

`GET /api/v1/me/academic-profile` now reports `program_slug` beside `program_id` and `program_name`.
It exists so a public page can ask for its own Program's ranking without ever handling an internal
identifier and without calling an authenticated academic lookup to translate one. It is a discovery
hint and carries no authority.

## 6. Student profile relevance — what it does and does not do

**Does:** orders results into four named, deterministic tiers — explicit Program target (0), inferred
curriculum match (1), same Institution (2), everything else (3), then Course id. No learned weights,
no stored score, no opaque ranking, nothing dependent on machine learning.

**Does not:** appear in the `WHERE` clause at all. `TestT6ProfileRelevanceRanksWithoutRemovingCourses`
asserts a ranked request returns **every** Course an unranked one does, merely in a different order.
A profile cannot create an Entitlement, hide a Course, override paid access, alter invitation
acceptance, change ownership, or change publication state.
`TestT6DiscoveryCreatesNoAccessOrAudienceRows` counts `entitlements`, `enrollments`,
`course_access_invitations`, `purchase_requests`, `course_program_targets`, `courses`,
`course_revisions`, and `audit_events` before and after every filter and option call and asserts
nothing was written.

The client also drops the relevance hint once the visitor filters by a Program themselves: ranking is
then pointless and would needlessly make a cacheable response private.

> **Authority alignment, resolved 2026-08-23.** `SCREENS.md` ST01 previously read *"Ranking is
> relevance only — no recommendations, promotion, or personalization."* That sentence predates the
> Academic Catalog and read "personalization" as covering own-profile ordering. The Founder approved
> own-profile relevance ordering under the contract above and **ST01 has been updated to state it**,
> recorded as an [amendment to D-092](../../DECISIONS.md#d-092--the-student-academic-profile-persists-academic-unit-context-for-program-less-states-and-records-onboarding-as-an-explicit-three-state-decision).
> The prohibitions the old sentence existed to protect are retained verbatim: no paid promotion, no
> sponsored ranking, no recommendations disguised as relevance, and no access or eligibility
> personalization. **No T6 code changed** — the documentation was brought to the implementation, not
> the reverse, after the implementation was re-verified against the contract (§15).

## 7. Frontend

`src/components/catalog/academic-filter-state.ts` is a pure module and the **URL is the single owner**
of the selection. Nothing is mirrored into component state, so refresh, a shared link, and browser
back/forward are one code path rather than three behaviours to keep in step. Choosing a University
clears the Program, level, and Subject beneath it; choosing a Program clears the level and Subject —
because those options are drawn from the level above, and a stale child would silently filter on
something the visitor can no longer see. A URL naming a Program with no University drops the
dependent values rather than carrying them.

`src/components/catalog/academic-filters.tsx` renders four real `<select>` elements with real
`<label>`s, so keyboard operation, accessible naming, and RTL come from the platform instead of being
reimplemented. There is no div-only pseudo-control. If the academic option endpoints fail, the
filters say so and **browsing and search stay usable**.

Empty results are named rather than generic: no Courses at all, none for this University, none for
this Program, none for this level, none for this Subject, no search matches. None of them is
presented as an error, and each offers the appropriate reset.

Nothing was visually redesigned. No new colour, type scale, or layout system was introduced.

## 8. Bilingual and accessibility

Every academic name is rendered from the row's own `name_ar` / `name_en` or `title_ar` / `title_en`.
No slug, code-only value, enum, or UUID is ever a visible label. A Subject shows its **official
code** — real academic vocabulary a Student recognises from their own study plan — alongside its
title; a code-less Subject shows only its title while carrying an identifier as its non-visible
filter value.

A College whose name repeats the Program's is suppressed rather than rendered, which fixed a real
defect this tranche surfaced: the option query originally reported the Program's *owning unit*, which
at Kuwait University is a Department whose name repeats the Program's, so every option read
"Computer Science · Computer Science". The College is now the owning unit's parent when the Program
hangs off a Department and the unit itself when it hangs off a College directly — both shapes exist in
the launch catalog.

E2E case E drives the whole journey in Arabic at a phone viewport, asserts Arabic names are shown and
English ones are not, asserts the slug never appears, and asserts `dir` really is `rtl`.

## 9. Two real defects this tranche found and fixed

1. **An empty catalogue serialised as `"items": null`.** `scanCourses` returned a nil slice, which a
   client cannot tell apart from a response it has not received — an empty catalogue rendered as a
   permanent loading state. Caught by E2E case D, which is the first test to reach a genuinely empty
   real catalogue. `items` is now always an array.
2. **The Program option reported a Department as a College**, described above.

Both were found by tests against the real product, not by inspection.

## 10. Tests

### Backend — real PostgreSQL

```
go test -tags=integration ./internal/catalogpublic/ -count=1    ok   19.7s
go test -tags=integration ./internal/httpapi/      -count=1    ok
```

`internal/catalogpublic/academic_discovery_integration_test.go` — institution filtering · inferred
audience · explicit audience overrides inference · Subject filtering incl. code normalization ·
combined filters and count consistency · every non-public lifecycle under every filter · duplicate
prevention across multiple curricula · retired Institutions/Programs/Subjects excluded from option
lists · academic level from recorded study-plan data with malformed input rejected safely · profile
relevance ranks without removing · no write of any kind · localized `program_audience` on detail.

`internal/httpapi/catalog_public_academic_integration_test.go` — the HTTP contract: filters reach the
query, option endpoints are anonymous and leak no identifier, unknown values give empty lists not
errors, oversized values are bounded, a ranked response is `private, no-store`, a filtered one stays
publicly cacheable, and Course detail names the audience in the reader's language and leaks no
internal identifier.

### Frontend

```
npm run typecheck    clean
npm test             371 passed / 0 failed      (was 347)
```

`src/components/catalog/academic-filters.test.ts` — URL round-trip, dependent clearing, request
shape, relevance-is-not-a-filter, option endpoints are the public ones, localized display, code-less
Subject display, empty-state naming, plus structural assertions over the shipped `.tsx` that a later
edit cannot reintroduce a raw identifier, a single-language label, a div-only control, an
authenticated academic call, or a second copy of the selection state.

### E2E — real browser, real API, real lifecycle

`e2e/t6-academic-discovery.spec.ts` — **6 passed**. The Course under test is created through the real
Instructor authoring UI, submitted, priced and published through the real Admin review UI, and the
academic hierarchy is the real Kuwait University launch manifest imported through the real Admin API.
Nothing is injected into the frontend and no classification row is written directly.

- **A** — University → Program → Subject → Course → detail → preview boundary, entirely by name; no
  UUID appears anywhere on either page.
- **B** — Electrical Engineering does not surface a Course whose Subject the manifest maps only into
  the Computer Science and Cybersecurity curricula, while the unfiltered catalogue still shows it.
- **C** — a shared link restores the selection, refresh preserves it, reset clears the URL and not
  just the controls, and browser back returns to the shared selection.
- **D** — a Program with no matching Course shows a named empty state with no error and no alert.
- **E** — the same journey in Arabic at a phone viewport, RTL verified.
- **F** — the filter controls are focusable and operable by keyboard with real label associations.

## 11. Performance

Every filter is an `EXISTS` subquery against an indexed column: `institutions.slug` and
`programs.slug` are unique-indexed, `curriculum_subjects_subject_idx` covers the Subject lookup,
`course_program_targets_program_idx` and the table's `(revision_id, program_id)` primary key cover the
target lookups, and `courses_institution_subject_idx` covers the Institution/Subject narrowing. The
count query repeats the same predicate against the same joins, so the total can never describe a
different set than the page.

There is **no N+1**: the list is one query plus one count, and the detail audience is one query.
**No index was added**, because the query shapes fall on indexes migrations 0023 and 0025 already
create.

## 12. Database changes

**None.** No migration was added, no column, no index, no constraint. T6 is entirely query and
application code on schema 26.

## 13. Out of scope, and untouched

The purchase and access lifecycle was not redesigned: purchase request → Admin confirmation →
invitation → acceptance → entitlement → learning is exactly as it was. Password recovery's
decoupling from Student registration is untouched. The Instructor Subject Request workflow is
untouched. No visual redesign was performed.

## 14. Manual acceptance — a real human journey, observed

A development acceptance runtime was brought up on the canonical ports and the journey was driven in
a real browser.

**Isolation.** The existing `gradex_mailpit_acceptance` database was **not** reset, migrated, or
written to. Its API process was no longer running and its outbox encryption key was not recoverable,
so re-keying it in place would have made its existing protected payloads unreadable. Instead it was
cloned (`CREATE DATABASE gradex_t6_acceptance TEMPLATE gradex_mailpit_acceptance`) and the runtime
points at the clone. The original is untouched.

- API: `http://127.0.0.1:18080` — `/readyz` → `{"status":"ok","checks":{"postgres":"ok","redis":"ok","schema":"ok"}}`
- Frontend: `http://127.0.0.1:13000`
- Data: the existing acceptance dataset — one **published** Academic Course, Kuwait University,
  Subject `0418-201`, priced at 25.000 KWD, with a public preview and **zero** explicit program
  targets (so its audience is inferred). Nothing was fabricated in SQL.

### API, against real acceptance data

| Filter | `total` |
|---|---:|
| (none) | 1 |
| `institution=kuwait-university` | 1 |
| `+ program=computer-science` | 1 |
| `+ subject=0418-201` | 1 |
| `+ level=2` | 1 |
| `program=electrical-engineering` | **0** |
| `subject=0410-101` | **0** |

The two zeroes are the meaningful ones: the Course's Subject is mapped only into the Computer Science
and Cybersecurity curricula, so Electrical Engineering correctly infers nothing, and a different
Subject correctly matches nothing.

### Browser, English

Opening `/en/catalog` shows four labelled choosers. Selecting **Kuwait University** loads five real
Programs — each with its true College, e.g. *"Computer Science · College of Science"* and
*"Computer Engineering · College of Engineering and Petroleum"* — four academic levels, and 84
Subjects. Selecting **Computer Science** narrows the Subject list from 84 to the 19 in that Program's
active Curriculum. Selecting **0418-201 · Data Structures & Algorithms** leaves one result:

```
Kuwait University
Data Structures & Algorithms · 0418-201
Instructor: Development Instructor
Price guidance: 25.000 KWD
Public preview available
```

The URL is `/en/catalog?institution=kuwait-university&program=computer-science&subject=0418-201` —
shareable, with no identifier in it. A regex scan of the rendered page finds **no UUID**.

Opening the Course reaches its detail page, which shows the University, the Subject with its official
code, and:

```
Relevant to: Computer Science, Cybersecurity
```

— the inferred Program audience, resolved from the real curriculum mappings and rendered as names.
Preview, price guidance, the course outline, and **I want to buy this course** are all present and
unchanged.

### Browser, Arabic

`/ar/catalog` with the same query string renders `dir="rtl"`, the chooser labels as
*الجامعة · التخصص · المستوى الدراسي · المقرر*, the University as *جامعة الكويت*, the Program as
*علوم الحاسوب · كلية العلوم*, and the Subject as *0418-201 · هياكل البيانات والخوارزميات*. The single
result card reads:

```
جامعة الكويت | هياكل البيانات والخوارزميات · 0418-201 | المدرب: … | السعر الإرشادي: 25.000 د.ك | تتوفر معاينة عامة
```

No English academic name appears anywhere on the Arabic page, and again no UUID.

**A real user can now find a Course by where they study, what they study, and which subject they
need — without ever seeing or typing an identifier, in either language.**


## 15. Governance verification — 2026-08-23

Before `SCREENS.md` and `DECISIONS.md` were edited, the shipped implementation was re-read to confirm
the documented contract is the built one. Documentation is never written to describe behaviour that
was not checked.

| Claim | Verified how |
|---|---|
| **Ordering only** | `RelevantProgramSlug` is read in exactly one place — `repository.go`, inside the `ORDER BY` construction. `academicConditions`, which builds every `WHERE` clause, never reads it, and the count query shares that same builder, so the total is identical ranked or not. |
| **Own profile only** | The slug comes from `GET /me/academic-profile`, which the server scopes to the signed-in account and which takes no account parameter. |
| **Request-scoped / opt-in** | Ranking applies only when the `relevant_to_program` query parameter is present. Nothing is stored, defaulted server-side, or attached to the session. |
| **Anonymous not personalized** | A public page calling the profile endpoint anonymously gets 401; the client resolves that to an empty slug and omits the parameter. A 401 is treated as an ordinary state and never surfaces as an error or hides a Course. |
| **No visibility or access authority** | `TestT6ProfileRelevanceRanksWithoutRemovingCourses` asserts a ranked request returns every Course an unranked one does. `TestT6DiscoveryCreatesNoAccessOrAudienceRows` counts `entitlements`, `enrollments`, `course_access_invitations`, `purchase_requests`, `course_program_targets`, `courses`, `course_revisions`, and `audit_events` before and after every filter and option call and asserts nothing was written. |
| **Deterministic tiers** | `RelevanceExpression` is a four-branch `CASE`: explicit `course_program_targets` match → 0; no targets **and** an active-Curriculum Subject match → 1; same `institution_id` → 2; otherwise 3. Tie-break is `c.id`. No stored score, no learned weight. |
| **No paid promotion** | No sponsorship, bidding, or promotion concept exists anywhere in the catalogue path. |

The implementation matches the contract in every respect, so the authority documents were updated to
describe it. Had it differed, the correct action would have been to report the difference rather than
rewrite authority around a defect.