# Phase 1 Data Model — S2 Course Authoring and Review

**Date**: 2026-07-28 | **Plan**: [plan.md](plan.md) | **Migration**: `0009_course_authoring`
(schema 8 → 9)

Every constraint below is a database constraint unless explicitly noted as an application check with
its reason. Constitution VII: structural invariants are caught by the database, not discovered in
production.

**D5 additive migration**: `0010_revision_integrity` (schema 9 → 10). It adds the candidate and
same-Course pointer invariants required by FR-046, plus stable Section/Lesson identities required by
BR-019 and BR-059. It does not rewrite migration `0009`.

---

## Enumerations

```text
course_lifecycle : DRAFT | PENDING_REVIEW | CHANGES_REQUESTED | PUBLISHED | DELISTED | ARCHIVED
revision_state   : DRAFT | PENDING_REVIEW | CHANGES_REQUESTED | APPROVED | SUPERSEDED | REJECTED
study_year       : PREP | YEAR_1 | YEAR_2 | YEAR_3 | YEAR_4
taxonomy_kind    : MAJOR | SUBJECT
lesson_file_kind : RESOURCE | LAB_MATERIAL
```

`course_lifecycle` is exactly BR-090's set — no more, no fewer. Emergency suspension is deliberately
**absent** from it (research R3).

`study_year` is the fixed enumeration from BR-157, not a taxonomy term.

---

## `courses`

The stable logical identity of a Course. Deliberately holds almost no content: content belongs to a
revision.

| Column | Type | Constraint | Rule |
|---|---|---|---|
| `id` | UUID | PK | |
| `owner_account_id` | UUID | `NOT NULL`, FK → `accounts` | Exactly one owning Instructor; Admin-only reassignment | BR-014 |
| `lifecycle` | `course_lifecycle` | `NOT NULL`, default `DRAFT` | BR-011, BR-090 |
| `live_revision_id` | UUID | nullable, same-Course FK → `course_revisions` | **`NULL` = never published.** The only pointer Student-visible reads join through | BR-017, BR-090 |
| `access_suspended_at` | TIMESTAMPTZ | nullable | Emergency suspension, orthogonal to lifecycle | BR-090 |
| `access_suspension_reason` | TEXT | `CHECK` non-empty when suspended | BR-090 |
| `retired_at` | TIMESTAMPTZ | nullable | Future-acquisition block, separate from lifecycle | BR-027 |
| `created_at` / `updated_at` | TIMESTAMPTZ | `NOT NULL` | |

**Constraints**

- `CHECK (access_suspended_at IS NULL) = (access_suspension_reason IS NULL)` — a suspension without a
  reason cannot exist.
- `CHECK (lifecycle <> 'PUBLISHED' OR live_revision_id IS NOT NULL)` — Published with nothing live is
  not a representable state.
- **D5 same-Course live pointer**: add `UNIQUE (course_id, id)` to `course_revisions` and replace the
  single-column live FK with
  `courses(id, live_revision_id) → course_revisions(course_id, id)`, deferred as in `0009`. A Course
  pointing at another Course's revision is unrepresentable.
- Owner role is validated in the same transaction as any write (application check: the role lives on
  `accounts` and a cross-table `CHECK` is not available).

---

## `course_revisions`

A complete candidate graph. The unit of review.

| Column | Type | Constraint |
|---|---|---|
| `id` | UUID | PK |
| `course_id` | UUID | `NOT NULL`, FK → `courses` |
| `based_on_revision_id` | UUID | nullable; D5 same-Course FK through `(course_id, based_on_revision_id)` |
| `state` | `revision_state` | `NOT NULL` |
| `revision_number` | INTEGER | `NOT NULL`, `UNIQUE (course_id, revision_number)` |
| `title_ar` / `title_en` | TEXT | `NOT NULL`, non-empty |
| `description_ar` / `description_en` | TEXT | `NOT NULL` |
| `major_term_id` | UUID | nullable, FK → `taxonomy_terms` |
| `subject_term_id` | UUID | nullable, FK → `taxonomy_terms` |
| `study_year` | `study_year` | nullable |
| `preview_asset_version_id` | UUID | nullable — **single column, not a collection** (BR-143) |
| `submitted_at`, `reviewed_at`, `reviewed_by_account_id`, `review_reason` | | `review_reason` `NOT NULL` non-empty for `CHANGES_REQUESTED` or `REJECTED` (BR-072) |

**Constraints**

- **D5 active-candidate unique index**:
  `UNIQUE (course_id) WHERE state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW')`. These are the
  only active candidate states. Only `DRAFT` and `CHANGES_REQUESTED` are editable;
  `PENDING_REVIEW` is active but read-only, and `APPROVED`, `SUPERSEDED`, and `REJECTED` are
  terminal. The Course row lock makes create-or-return deterministic, while this index independently
  makes a duplicate active candidate unrepresentable. Migration `0010` replaces `0009`'s narrower
  `PENDING_REVIEW` index with this superset; its down migration restores the narrower index.
- **D5 candidate-base identity**: use `UNIQUE (course_id, id)` and a composite FK
  `(course_id, based_on_revision_id) → course_revisions(course_id, id)`, plus
  `CHECK (based_on_revision_id IS NULL OR based_on_revision_id <> id)`. The initial
  first-publication Draft has no base. A candidate cloned from a Published Course stores the captured
  `live_revision_id`; approval refuses it with `409` if that base no longer equals the locked Course
  pointer.
- **D5 rejection evidence**: extend the review-reason constraint so a non-empty `review_reason` is
  mandatory for both `CHANGES_REQUESTED` and `REJECTED`.
- Taxonomy columns are nullable because a Draft may legitimately be incomplete; all three are
  required **at submission**, validated in application code so that every missing dimension can be
  reported together (BR-159, R5).

---

## Stable Section and Lesson identities

Revision rows are snapshots; Section purchase scope and Lesson progress are durable identities.
Migration `0010` therefore adds two small Course-owned registries:

`course_section_identities`: `id` UUID PK, `course_id` `NOT NULL` FK → `courses`, `created_at`, and
`UNIQUE (id, course_id)`.

`course_lesson_identities`: `id` UUID PK, `course_id` `NOT NULL`,
`section_identity_id` `NOT NULL`, `created_at`, with composite FK
`(section_identity_id, course_id) → course_section_identities(id, course_id)` and
`UNIQUE (id, course_id, section_identity_id)`.

For the pre-D5 graph, migration backfill uses each existing version row's `id` as its logical
identity, so existing API identifiers remain valid. Candidate cloning reuses those identity rows.
Creating a genuinely new Section or Lesson creates an identity row and its candidate version row in
one transaction. Deleting and recreating creates a new identity; an identity still referenced by a
historical revision is never deleted.

## `course_sections` and `course_lessons`

Both are **version rows** belonging to a revision. That is what makes the pointer swap sufficient.
Their stable identity references are separate from their version-row primary keys.

`course_sections`: `id` (version-row PK), `revision_id`, `course_id`, `section_identity_id`,
`title_ar`/`title_en`, `position` with `UNIQUE (revision_id, position)`, nullable
`price_minor_units`. Composite FKs
`(course_id, revision_id) → course_revisions(course_id, id)` and
`(section_identity_id, course_id) → course_section_identities(id, course_id)` prove that the revision
and stable Section identity belong to the same Course. `UNIQUE (revision_id, section_identity_id)`
prevents one logical Section appearing twice in a revision; `UNIQUE (id, course_id,
section_identity_id)` supports the Lesson ancestry FK.

`course_lessons`: `id` (version-row PK), `section_id`, `course_id`, `section_identity_id`,
`lesson_identity_id`, `title_ar`/`title_en`, `position` with `UNIQUE (section_id, position)`,
`video_asset_version_id` **nullable** (a Draft Lesson may not have one yet; required at submission —
BR-012/013). Composite FKs
`(section_id, course_id, section_identity_id) → course_sections(id, course_id,
section_identity_id)` and `(lesson_identity_id, course_id, section_identity_id) →
course_lesson_identities(id, course_id, section_identity_id)` prove that both the version Section and
stable Lesson identity share the same Course and stable Section. `UNIQUE (section_id,
lesson_identity_id)` prevents duplication within one version Section.

Explicit `position` rather than creation order, per the spec's Assumptions.

### D5 clone boundary

Candidate creation copies the rows owned by the captured live revision:

- the `course_revisions` authored fields and taxonomy/preview references;
- its `course_sections`;
- its `course_lessons`;
- its `lesson_files`.

The clone records the captured revision in `based_on_revision_id` and creates new identifiers for
revision-owned version rows. It preserves the stable Section/Lesson identity references and exact
Asset Version references. It creates no Asset Version, stored object, upload, Order, Payment,
Enrollment, Entitlement, progress, or other externally owned row. Replacing a Lesson's video changes
only the candidate version row's Asset Version reference, so the Lesson identity used by BR-059
remains unchanged.

Every candidate-scoped mutation carries the expected revision identifier and verifies that any
stable Section/Lesson or file identifier resolves to a version row in that same revision. No
mutation infers authority from the highest `revision_number`.

### D5 live-read identity

A Student-visible graph begins by capturing the Course's non-null `live_revision_id`. Every
subsequent query uses that captured revision identifier (or child identifiers reached from it).
Graph assembly never queries `courses.live_revision_id` again, so an approval committed between
queries cannot mix generations.

---

## `lesson_files`

Resources and lab materials — two distinct categories on one table, discriminated by kind (BR-067).

`id`, `lesson_id` FK, `kind` (`lesson_file_kind`), `asset_version_id` (`NOT NULL` — a reference, never
bytes), `display_name_ar`/`display_name_en`, `position`.

**S2 stores references only.** No upload, no scan, no processing — SLICES §3.2.

---

## `taxonomy_terms`

| Column | Constraint |
|---|---|
| `id` | PK |
| `kind` | `taxonomy_kind` `NOT NULL` |
| `label_ar` / `label_en` | `NOT NULL`, non-empty — bilingual per BR-157 |
| `academic_code` | nullable, Subject only (`CHECK`) |
| `retired_at` | nullable — retirement, never silent deletion when referenced (BR-160) |

Deletion of a referenced term is refused by an explicit reference count inside the transaction, not
by `ON DELETE` behaviour — the requirement is a **refusal**, not a cascade (BR-160, mirroring BR-018).

---

## `course_price_changes`

Append-only. **The current price is derived from the latest row, never duplicated into a mutable
column**, so history and effective value cannot disagree.

`id`, `course_id` FK, nullable `section_id` FK → `course_section_identities` (null = Course-level
price), `old_value_minor_units` nullable (null = first price), `new_value_minor_units` `NOT NULL`,
`changed_by_account_id` `NOT NULL`, `reason` `NOT NULL` non-empty, `changed_at` `NOT NULL DEFAULT
now()`. Migration `0010` remaps any existing version-row Section FK to its backfilled stable identity
and enforces same-Course membership. T039 remains the owner of pricing behavior; D5 changes identity
integrity only.

No `UPDATE` or `DELETE` is ever issued against this table. A price change affects future Orders only
and mutates no Order, Entitlement, Refund, or payout snapshot (BR-019, FR-029).

Money is integer minor units, consistent with the commerce design.

---

## Audit

**No new table.** Every privileged action writes to the existing `audit_events` from migration `0003`,
with `module = 'CATALOG_AND_AUTHORING'` — a value the enum already contains.

Actions written by this slice: `COURSE_SUBMITTED`, `COURSE_PUBLISHED`,
`COURSE_CHANGES_REQUESTED`, `COURSE_REVISION_REJECTED`, `COURSE_DELISTED`, `COURSE_RELISTED`,
`COURSE_RETIRED`, `COURSE_ARCHIVED`, `COURSE_DELETED`, `COURSE_OWNER_REASSIGNED`,
`COURSE_PRICE_CHANGED`, `COURSE_ACCESS_SUSPENDED`, `COURSE_ACCESS_RESTORED`,
`TAXONOMY_TERM_CREATED`, `TAXONOMY_TERM_RENAMED`, `TAXONOMY_TERM_RETIRED`,
`TAXONOMY_TERM_DELETED`, `ADMIN_CONTENT_PREVIEWED`.

Each is written **in the same transaction as its change** (domain design §17). `reason` is `NOT NULL`
and non-empty at the schema level, so an unexplained privileged action is not representable.

---

## Notification intents

**No new table.** The existing `outbox` protected-payload boundary carries: Course approved, changes
requested, submission received (to Admin operations), emergency suspension, and restoration
(BR-122).

Written inside the business transaction, dispatched outside it. Delivery failure never rolls back the
business action (BR-120, FR-045). **The intent is mandatory** — the "optional outbox intent"
construction was a finding in S1C's second round and must not reappear.

---

## State transitions

```text
DRAFT ──submit──▶ PENDING_REVIEW ──approve──▶ PUBLISHED ◀──relist── DELISTED
  ▲                     │                        │  │                  │
  └──request changes────┤                        │  └──delist─────────▶│
                        ▼                        │                     │
              CHANGES_REQUESTED ──resubmit──▶ PENDING_REVIEW           │
                                                 │                     │
                                                 └──archive──▶ ARCHIVED ◀┘
```

Orthogonal, applying at any lifecycle state and never displacing it:

- **Emergency access suspension** ⇄ restoration — denies access immediately, mutates no Entitlement.
- **Retirement** — blocks future acquisition; preserves qualifying existing access per BR-027.

Every transition is guarded by `SELECT … FOR UPDATE` on the Course row with an in-transaction
re-assertion of the expected current state. A caller that raced loses with a `409` naming the state
actually found — never last-write-wins (plan §concurrency).
