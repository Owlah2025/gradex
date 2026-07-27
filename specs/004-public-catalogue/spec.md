# Feature Specification: S3 — Public Catalogue and Bilingual Shell

**Feature Branch**: `feature/004-public-catalogue`

**Created**: 2026-07-28

**Status**: Draft — frozen for D6 implementation under [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews)

**Input**: S3 — Public catalogue list and Course detail; the responsive bilingual application shell
with Arabic default, RTL/LTR layout, and persistent locale; taxonomy display; simple published-Course
text search. This is the first slice that renders anything a non-authenticated visitor can browse.

**Depends on**: **S2 (Course authoring and review)**, hard. S3 reads the published Course graph S2
produces; it authors none of it. S3 planning may be frozen while S2 is implemented — freezing a spec
commits no behaviour — but **S3 implementation does not begin until S2 closes on an independent
verdict.**

**Governing rules**: BR-010, BR-019, BR-021, BR-090, BR-105, BR-143, BR-149, BR-150, BR-157,
BR-158, BR-161, BR-162. Traceability is carried per requirement below, per Constitution Principle III.

**Scope authority**: [AUGUST_15_EXECUTION_PLAN.md §2.2](../../docs/launch/AUGUST_15_EXECUTION_PLAN.md#22-reduced-slices--launch-critical-core-retained-remainder-reclassified)
reduces S3. What it moved out is recorded in [§Deferred](#deferred-by-scope-decision-not-dropped) with
its destination, and **one of those deferrals is challenged in [§Open Decisions](#open-decisions) rather
than silently implemented.**

---

## Scope Boundaries

Stated first, because S3 sits between two slices that own most of what it displays.

| S3 owns | S3 must not acquire |
|---|---|
| Reading and rendering the **published** Course graph | Any authoring, lifecycle, or review transition — S2 |
| Displaying Course and Section prices as authored | Setting prices, orders, checkout, coupons — S2 sets, S6 buys |
| The public preview asset's playback surface | Protected media, signed URLs, entitlement evaluation — S4 |
| The bilingual responsive shell, RTL/LTR, locale persistence | Per-screen content for later slices; each slice ships its own screens on this shell |
| Taxonomy **display** on a Course | Taxonomy administration — S2 (Admin), S8 (UI) |
| Simple text search over published Courses | Ranked relevance, personalization, recommendation — deferred, see below |

**S3 is a read-only slice over Course state.** It introduces no write path to any Course, Section,
Lesson, price, or taxonomy record. If a requirement below appears to need one, that is a finding
against this specification, not licence to add one.

**S3 introduces no second authorization decision point.** Its routes are public and unauthenticated,
so the relevant control is not a capability check but a **visibility filter**, and that filter is the
whole security surface of this slice. See FR-002 and FR-003.

---

## The one thing this slice can get catastrophically wrong

Everything else here is presentation. This is not:

> **A Course that is not `PUBLISHED` must be indistinguishable from a Course that does not exist**,
> on every public route, for every caller, including by exact identifier, including by timing, and
> including in error messages.

S2 protects private drafts behind ownership. S3 opens a set of routes that deliberately serve
anonymous callers, and it reads the same tables. Every leak class this project has already paid for
lives here: enumeration by identifier, a filter applied to the list route but not the detail route, a
404 that says "not found" for one case and "forbidden" for another, and a search index built from
rows the reader may not see.

`DELISTED`, `ARCHIVED`, `PENDING_REVIEW`, `CHANGES_REQUESTED`, `DRAFT`, and a Course under emergency
access suspension are **all** non-public here (BR-090). A pending revision of a Published Course is
non-public while its approved live version remains public (BR-017).

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 — A visitor browses the catalogue without an account (Priority: P1)

An anonymous visitor opens Gradex, sees the published catalogue in Arabic by default, and can read
enough about a Course to decide whether to buy it.

**Why P1**: it is the entry point of the entire commercial journey. Nothing downstream — orders,
payment, entitlement, learning — is reachable if a visitor cannot find a Course.

**Acceptance**
1. **Given** a catalogue containing Published, Draft, Pending Review, Changes Requested, Delisted,
   Archived, and emergency-suspended Courses, **when** an anonymous visitor opens the catalogue,
   **then** only the Published ones appear, and the count matches exactly.
2. **Given** the identifier of a non-Published Course, **when** the visitor requests it directly by
   that exact identifier, **then** the response is byte-identical to the response for an identifier
   that has never existed.
3. **Given** a Published Course, **when** the visitor opens its detail page, **then** the title,
   description, Instructor display name, all three taxonomy dimensions, the Section outline, and both
   the full-Course and per-Section prices are shown.
4. **Given** a Published Course with a preview asset, **when** the visitor opens the detail page,
   **then** the preview is playable and **no** protected Lesson video, Resource, or Lab Material is
   reachable from that page.

### User Story 2 — Arabic is the default and the layout follows it (Priority: P1)

A visitor arriving with no stored preference gets Arabic, in a right-to-left layout, and their choice
of language survives navigation and a return visit.

**Why P1**: BR-149 makes Arabic the initial default for a Kuwait-focused product. A shell that
defaults to English is wrong for the primary audience, and this is the slice that establishes the
shell every later screen inherits.

**Acceptance**
1. **Given** a first-time visitor with no stored preference, **when** they open any public page,
   **then** the interface is Arabic and the document direction is RTL.
2. **Given** a visitor who switches to English, **when** they navigate to another page and later
   return in a new session, **then** English persists and the direction is LTR.
3. **Given** either language, **when** any public page is rendered, **then** navigation, forms, lists,
   and price/number formatting are correct for that direction with no clipped, mirrored, or
   overlapping content at phone, tablet, and desktop widths.
4. **Given** a Course authored in Arabic and one authored in English, **when** either is viewed under
   either interface language, **then** the Course content appears in its authored language and is
   **not** machine-translated. *(BR-150)*

### User Story 3 — A visitor finds a Course by typing part of its name (Priority: P2)

A visitor types a fragment of a Course title, an Instructor's name, or a subject code and gets the
matching published Courses.

**Why P2**: with a launch catalogue of 8–12 Courses, browsing alone is navigable. Search is what
makes the catalogue feel like a product rather than a list, but it does not block a purchase.

**Acceptance**
1. **Given** a query matching a Course title, description, Instructor display name, or taxonomy label
   or code, **when** it is searched, **then** the matching Published Courses are returned. *(BR-161)*
2. **Given** a query matching a Lesson title, a Resource filename, or any text belonging to a
   non-Published Course, **when** it is searched, **then** nothing is returned and the existence of
   that content is not revealed. *(BR-161, BR-143)*
3. **Given** a query in Arabic and a query in English, **when** either is searched under either
   interface language, **then** both Arabic and English fields are matched. *(BR-162)*
4. **Given** an empty query, a whitespace query, a very long query, or a query containing SQL or
   regex metacharacters, **when** it is searched, **then** the response is a well-formed empty or
   filtered result and never an error disclosing internals.

### Edge Cases

- A Course published while the visitor is reading the list — the detail page works; the stale list is
  not an error.
- A Course delisted while the visitor is on its detail page — the next request behaves as
  non-existent. In-flight requests are not retroactively invalid.
- A Published Course whose owning Instructor is suspended — the Course **remains** publicly visible.
  Instructor suspension blocks authoring, not Student access to already-Published Courses (BR-065).
- A Published Course with zero Sections cannot exist (S2 refuses submission), but the catalogue must
  render defensively rather than crash if one is encountered.
- A taxonomy term retired after assignment still displays on the Course that carries it (BR-160).
- A Course with no preview asset renders a detail page with no empty player frame.

---

## Requirements *(mandatory)*

### Functional Requirements

**Visibility — the security surface of this slice**

- **FR-001**: System MUST expose public catalogue and detail routes to unauthenticated callers.
  *(PRD §Student Features)*
- **FR-002**: System MUST return only `PUBLISHED` Courses from every public route — list, detail, and
  search — and MUST derive that filter from **one** shared predicate rather than repeating a status
  comparison per route. *(BR-090, BR-161)*
- **FR-003**: System MUST make a non-`PUBLISHED` Course indistinguishable from a non-existent one on
  every public route, including by exact identifier, with an identical status code and body. *(BR-090)*
- **FR-004**: System MUST exclude a Course under emergency access suspension from all public routes
  while that suspension is active. *(BR-090)*
- **FR-005**: System MUST serve only the currently approved live revision of a Published Course and
  MUST NOT expose a pending revision. *(BR-017)*
- **FR-006**: System MUST NOT expose Lesson titles, protected Resources, Lab Materials, or any
  non-Published content through any public route, including search. *(BR-143, BR-161)*
- **FR-007**: System MUST keep a Published Course publicly visible when its owning Instructor is
  suspended. *(BR-065)*

**Catalogue and detail content**

- **FR-008**: System MUST present a paginated public list of Published Courses with title, Instructor
  display name, all three taxonomy dimensions, price, and preview availability. *(BR-157, BR-105)*
- **FR-009**: System MUST present a Course detail view containing the authored description, the
  Section outline, the full-Course price, and each individually priced Section's price. *(BR-010,
  BR-021)*
- **FR-010**: System MUST present prices exactly as authored, in integer minor units rendered as KWD,
  and MUST NOT compute, discount, or infer any price. *(BR-019)*
- **FR-011**: System MUST identify an Instructor publicly by display name only, and MUST NOT expose
  email, phone, legal identity, or any other Instructor or Student PII. *(BR-105, BR-064)*
- **FR-012**: System MUST make the optional public preview asset playable without authentication and
  MUST authorize it separately from protected Lesson content. *(BR-143)*
- **FR-013**: System MUST display all three taxonomy dimensions — Major, Subject with its optional
  code, and Study Year — in the active interface language. *(BR-157, BR-158)*
- **FR-014**: System MUST continue to display a retired taxonomy term on a Course that already
  carries it. *(BR-160)*

**Bilingual responsive shell**

- **FR-015**: System MUST default to Arabic for a visitor with no stored preference. *(BR-149)*
- **FR-016**: System MUST persist a chosen interface language across navigation and across sessions,
  and MUST apply it to authenticated and anonymous visitors alike. *(BR-149)*
- **FR-017**: System MUST render the document direction RTL for Arabic and LTR for English, applying
  it to navigation, forms, lists, and iconography. *(BR-149)*
- **FR-018**: System MUST render every public screen correctly at phone, tablet, and desktop widths.
  *(PRD §Non-Functional)*
- **FR-019**: System MUST present Instructor-authored Course content in its authored language under
  either interface language, without machine translation. *(BR-150)*
- **FR-020**: System MUST provide the shell — layout, locale provider, direction handling, and
  navigation — as the **single** foundation later slices build their screens on, extending the
  existing `frontend/src/lib/i18n` locale mechanism rather than introducing a second one.

**Search**

- **FR-021**: System MUST match a query against Course title, authored description, Instructor
  display name, and taxonomy labels and code. *(BR-161)*
- **FR-022**: System MUST restrict search results to Published Courses using the **same** FR-002
  predicate the list and detail routes use, not a separate status condition in the query. *(BR-161)*
- **FR-023**: System MUST match case-insensitively and MUST match Arabic and English content
  simultaneously regardless of interface language. *(BR-162)*
- **FR-024**: System MUST treat an empty, whitespace-only, over-long, or metacharacter-bearing query
  as an ordinary input producing a well-formed result, never an error revealing internals.
- **FR-025**: System MUST NOT apply personalization, recommendation, or paid placement. *(BR-161)*

### Key Entities

S3 defines **no new domain entity.** It reads Course, Section, Revision, Taxonomy Term, and the
Instructor display name, all owned by S2 and S1. Any storage this slice adds — see
[data-model.md](data-model.md) — is a derived read-optimisation, never a second source of truth.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Across a fixture catalogue containing every non-Published state, **zero** non-Published
  Courses appear in any list, detail, or search response, proven by enumerating the live public route
  table rather than by testing a chosen subset.
- **SC-002**: A direct request for a non-Published Course by exact identifier is byte-identical to a
  request for a never-existing identifier — same status, same body, same headers.
- **SC-003**: A first-time visitor receives Arabic with RTL direction; a stored preference survives
  navigation and a new session.
- **SC-004**: Every public screen renders without clipping, mirroring, or overlap at phone, tablet,
  and desktop widths, in both directions.
- **SC-005**: A search for a Lesson title, a Resource filename, or a Draft Course's title returns
  nothing.
- **SC-006**: Public catalogue and Course pages meet the PRD target of p95 LCP under 2.5 seconds on
  representative Kuwait 4G. *(PRD §Non-Functional)*
- **SC-007**: No public response contains Instructor or Student PII beyond a display name, asserted
  against the full response body rather than the rendered page.

---

## Deferred by scope decision, not dropped

Recorded per [§2.2](../../docs/launch/AUGUST_15_EXECUTION_PLAN.md#22-reduced-slices--launch-critical-core-retained-remainder-reclassified)
with destination S18. None is deleted from the PRD.

| Deferred | Reason of record | Destination |
|---|---|---|
| Multi-dimension filtering by Major, Subject, Study Year | Launch catalogue is 8–12 Courses; browsing is navigable without it | S18 |
| Relevance ranking and sort options | Same; result sets are small enough that ordering is not a discovery problem | S18 |
| Arabic normalization of queries | **Challenged — see below** | S18 *(proposed: pull into S3)* |

---

## Open Decisions

### OD-001 — Deferring Arabic normalization makes Arabic search fail, not degrade

**This needs a developer decision before implementation, and the recommendation is to overturn the
deferral.**

§2.2 defers "Arabic-normalized ranked search" to post-launch as one item. That bundles two things with
very different costs:

- **Relevance ranking** is genuinely optional at 8–12 Courses. Deferring it is sound.
- **Arabic normalization** (BR-162) is not a ranking feature. It is what makes matching *work* at all
  in the product's default language. Arabic is routinely typed with different hamza forms (`أ إ آ ٱ`),
  with or without diacritics, with `ة`/`ه` and `ى`/`ي` interchanged, and with Arabic-Indic digits.
  Without folding, a visitor searching `احياء` does not match a Course titled `أحياء`.

The failure mode is not "results are ordered badly". It is "the search box returns nothing for a
correctly spelled query", on an Arabic-default platform, for the majority of real queries.

**Recommendation**: pull normalization into S3 as a shared normalize-on-write/normalize-on-query
function plus one generated column. Estimated 2–3 hours against an 8h slice. Keep ranking and
filtering deferred as §2.2 decided.

**If the deferral stands as written**, then FR-023 must be weakened to English-only matching and this
specification must say so plainly, rather than claiming BR-162 compliance it does not have. Silently
shipping a search box that fails in Arabic is the outcome this entry exists to prevent.

**Status**: OPEN. Requires the developer. FR-023 is written assuming the recommendation is accepted;
if it is rejected, FR-023 and SC-005's Arabic case change with it.

---

## Assumptions

- S2 has closed, so the Course lifecycle, taxonomy assignment, and price fields exist as specified in
  [specs/003-course-authoring/data-model.md](../003-course-authoring/data-model.md).
- The launch catalogue is 8–12 Courses (LAUNCH_GATES `LG-012`), which is what makes deferring
  filtering and ranking defensible. **If that count grows materially before launch, OD-001's
  companion deferrals should be reconsidered too.**
- The existing `frontend/src/lib/i18n` locale provider and dictionary mechanism from S1B are the
  foundation to extend; S3 introduces no second i18n mechanism.
- The public preview asset's delivery mechanism is defined by S4. S3 renders it; where S4 has not yet
  landed, S3 targets the same contract rather than inventing a temporary one.

## Dependencies

| Depends on | For | State |
|---|---|---|
| S2 | The published Course graph, taxonomy assignment, prices | **Blocking.** Implementation waits for S2's independent verdict |
| S1B | Locale provider, dictionaries, responsive shell primitives | Closed |
| S4 | Public preview asset delivery contract | **Not blocking**; S3 renders against the contract, and the preview is the only media on a public page |
