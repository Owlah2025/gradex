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

**Governing rules**: BR-010, BR-019, BR-020, BR-021, BR-029, BR-090, BR-105, BR-143, BR-149, BR-150,
BR-157, BR-158, BR-161, BR-162.

**Amended 2026-07-28** by [D-045](../../docs/DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation):
the Course price is displayed as External Payment guidance, Section prices are not displayed, and no
checkout control exists. Traceability is carried per requirement below, per Constitution Principle III.

**Scope authority**: [AUGUST_15_EXECUTION_PLAN.md §2.2](../../docs/launch/AUGUST_15_EXECUTION_PLAN.md#22-reduced-slices--launch-critical-core-retained-remainder-reclassified)
reduces S3. What it moved out is recorded in [§Deferred](#deferred-by-scope-decision-not-dropped) with
its destination. One of those deferrals was challenged rather than silently implemented, and the
developer **overturned** it on 2026-07-28 — see
[§Resolved Decisions](#resolved-decisions).

---

## Scope Boundaries

Stated first, because S3 sits between two slices that own most of what it displays.

| S3 owns | S3 must not acquire |
|---|---|
| Reading and rendering the **published** Course graph | Any authoring, lifecycle, or review transition — S2 |
| Displaying the Course price as authored | Setting prices — S2. Course Access Invitations and the grant transaction — S6 |
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
> on every public route, for every caller, including by exact identifier and including in error
> messages.

The guarantee is **exact** on response content — status, headers, schema, body — and **bounded** on
timing: measured against a documented tolerance, never claimed as proven. That distinction is
[OD-002](#resolved-decisions) and it is load-bearing, because a specification that promises timing
indistinguishability promises something it cannot deliver and invites a test that asserts it.

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

**Why P1**: it is the entry point of the entire journey. Nothing downstream — course access,
entitlement, learning — is reachable if a visitor cannot find a Course.

**Acceptance**
1. **Given** a catalogue containing Published, Draft, Pending Review, Changes Requested, Delisted,
   Archived, and emergency-suspended Courses, **when** an anonymous visitor opens the catalogue,
   **then** only the Published ones appear, and the count matches exactly.
2. **Given** the identifier of a non-Published Course, **when** the visitor requests it directly by
   that exact identifier, **then** the response is identical in status, headers, schema, and body to
   the response for an identifier that has never existed.
3. **Given** a Published Course, **when** the visitor opens its detail page, **then** the title,
   description, Instructor display name, all three taxonomy dimensions, the Section outline, and the
   full-Course price are shown. **Per-Section prices are not shown** — Section is not an acquirable
   scope under D-045.
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
  Section outline, and the full-Course price. It MUST NOT display Section prices, because Section is
  not an acquirable scope in MVP. *(BR-010, BR-021)*
- **FR-010**: System MUST present the Course price exactly as authored, in integer minor units
  rendered as KWD, and MUST NOT compute, discount, or infer any price. The price is informational —
  it tells the Student what to pay through External Payment, and no page may imply Gradex will take
  payment. *(BR-019, BR-020)*
- **FR-010a**: System MUST NOT render any checkout, cart, coupon, or purchase control. A Course
  detail page MAY link to informational guidance on how to obtain access. *(BR-020, BR-029)*
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
  simultaneously regardless of interface language, applying **one** normalization function identically
  to stored searchable text and to the incoming query. The normalization implemented in S3 is exactly:
  alef/hamza folding (`أ إ آ ٱ` → `ا`), alef maqsura (`ى` → `ي`), taa marbuta (`ة` → `ه`),
  Arabic-Indic digits (`٠–٩` → `0–9`), removal of tashkeel/diacritics and tatweel, Unicode case
  folding, and collapsing of leading, trailing, and repeated whitespace. *(BR-162 — see the
  traceability note below)*
- **FR-023a**: System MUST treat a query that normalizes to empty — for example one containing only
  diacritics, tatweel, or whitespace — exactly as an absent query, returning the unfiltered published
  list rather than an error or an empty result.
- **FR-023b**: System MUST NOT implement stemming, fuzzy or edit-distance matching, weighted or
  relevance ranking, or any external search service. Matching is deterministic substring matching over
  normalized text, and result ordering is stable and documented rather than scored. *(BR-161)*

> **BR-162 traceability, stated exactly.** FR-023 implements the **matching** behaviour BR-162
> specifies and **all** of the folds it enumerates. BR-162's sentence *"ranked by relevance"* belongs
> to BR-161 and is **not** implemented in S3 — ranking is deferred to S18. This specification therefore
> claims **partial** BR-162 compliance: complete on normalization and matching, absent on ranking. It
> does not claim the rest. *(Resolved by the developer on 2026-07-28 — see
> [OD-001](#od-001--resolved-adjust--normalization-in-s3-ranking-deferred).)*
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
- **SC-002**: A direct request for a non-Published Course by exact identifier is identical to a
  request for a never-existing identifier in **status, headers, response schema, and body**. This is
  the exact guarantee; it is asserted on the full response, not the status code.
- **SC-008**: The timing **distribution** of hidden-identifier and absent-identifier lookups is
  compared over a sample against a **documented tolerance**, and the observation is recorded as
  statistical evidence rather than as a proof of indistinguishability. A run outside tolerance is a
  finding with an owner, not a silent pass; a run inside it is not a guarantee.
- **SC-009**: A query normalizing to empty returns the unfiltered published list, identically to an
  absent query.
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
| Arabic query normalization | **Overturned — pulled into S3** by the developer on 2026-07-28 | **S3** |

Ranking and filtering remain deferred. Their deferral is **not** weakened by OD-001's resolution, and
FR-023b forbids implementing them by accident.

---

## Resolved Decisions

### OD-001 — RESOLVED `ADJUST` — normalization in S3, ranking deferred

**Decided by the developer, 2026-07-28.** §2.2 deferred "Arabic-normalized ranked search" as one item,
bundling a genuinely optional feature with a non-optional one:

- **Relevance ranking** is optional at 8–12 Courses. **Stays deferred** to S18.
- **Arabic normalization** is what makes matching work at all in the product's default language.
  Without alef/hamza folding a visitor searching `احياء` does not match a Course titled `أحياء`. The
  failure mode is not "results ordered badly" — it is "nothing found for a correctly spelled query".
  **Pulled into S3.**

**Scope admitted to S3**: one shared normalization function applied identically to stored searchable
text and to the incoming query; the folds enumerated in FR-023; migration and backfill for existing
published records; red-first tests. See FR-023, FR-023a, FR-023b, and
[data-model.md](data-model.md).

**Explicitly excluded**: stemming, fuzzy or edit-distance matching, weighted or relevance ranking,
external search infrastructure, and multi-dimension filtering. S3 must not expand into a search
subsystem; if normalization cannot fit cleanly within the slice, that is evidence to surface, not
licence to grow.

### OD-002 — RESOLVED — the timing claim was an overclaim, and is withdrawn

**Raised by the developer, 2026-07-28**, against this specification's own earlier wording.

An earlier draft of [plan.md](plan.md) stated that putting the visibility predicate in the `WHERE`
clause made a hidden row and an absent row "take the same path". **That was an overclaim and it is
withdrawn.** Query-boundary filtering removes the *application-level* branch, which is necessary but
not sufficient: index traversal, buffer cache state, row width, and planner behaviour can all still
differ measurably between a row that exists-but-is-hidden and a row that does not exist.

What S3 claims instead, and proves:

1. The predicate stays inside the database query boundary. *(FR-002, unchanged)*
2. Responses are identical in **status, headers, schema, and body**. *(FR-003 — this is the exact,
   provable guarantee)*
3. Timing is checked by a **distribution** test against a **documented tolerance**, and the result is
   reported as a statistical observation, not a proof. *(SC-008)*

No test asserts nanosecond equality, and no document here calls a statistical property formally
proven.

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
