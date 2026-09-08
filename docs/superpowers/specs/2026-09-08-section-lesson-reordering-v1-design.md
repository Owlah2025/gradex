# D-102 — Section and Lesson Reordering V1 Design

## Scope

An Instructor may reorder Sections within one editable Course revision and Lessons within their
current Section. Cross-Section Lesson movement is not part of V1. Persisted server `position` fields
remain authoritative; browser order is only an optimistic presentation while a command is pending.

## Domain and API

Two ownership-protected Instructor commands accept complete ordered identity sets:

- `PATCH /api/v1/courses/:id/revisions/:revisionId/sections/order`
  with `{ "section_ids": ["..."] }`.
- `PATCH /api/v1/courses/:id/revisions/:revisionId/sections/:sectionId/lessons/order`
  with `{ "lesson_ids": ["..."] }`.

Both commands use stable Section or Lesson identity IDs already exposed by the authoring graph. The
server locks the Course and exact revision, applies the existing active-Instructor ownership check,
and permits only the existing editable states (`DRAFT` and `CHANGES_REQUESTED`). The supplied set
must equal the authoritative set exactly: duplicates, omissions, and foreign IDs are rejected.

## Persistence and concurrency

No migration is required. Existing immediate unique constraints cover `(revision_id, position)` and
`(section_id, position)`. Inside the existing transaction boundary, all current members are locked,
shifted above the current maximum position, then written to canonical zero-based dense positions.
This avoids transient unique collisions. The revision row lock serializes all authoring mutations for
that candidate; the repository has no ETag or version precondition, so serialized last-committed order
wins while every commit remains a valid permutation. Successful commands return the canonical exact
revision graph and write the same Instructor audit stream used by neighboring authoring mutations.

## Revision boundary

Only the named editable candidate is mutated. The Course's `live_revision_id` and live revision rows
are untouched. Admin exact-revision reads see the candidate positions immediately. Student and public
reads continue resolving the live revision and therefore change only through the existing approval or
publication flow.

## Authoring experience

Sections and Lessons expose dedicated drag handles; cards, disclosures, fields, media controls, and
delete actions remain independently interactive. `@dnd-kit` supplies pointer, touch, and keyboard
sensors plus sortable keyboard coordinates and screen-reader announcements. Touch activation uses a
delay/tolerance constraint to preserve vertical scrolling. Logical spacing keeps the same layout in
English and Arabic.

Each drop updates the curriculum optimistically and starts one persistence request. Further reorder
attempts are disabled until it settles. Success replaces only the candidate curriculum with the
canonical response, preserving local Course Details fields and disclosure identity. Failure restores
the prior order and shows an order-specific error. Keyboard focus stays on the moved item's handle.

## Verification

Domain and HTTP integration tests cover exact-set validation, ownership, editability, same-Section
enforcement, canonical positions, transaction rollback, serialization, audit behavior, candidate/live
isolation, and Admin candidate visibility. A separate HTTP integration test reads the Student routes
themselves: the Course Home order and the Lesson Player's Previous/Next (D-099) stay on the live
revision while the reordered revision is only a candidate, and follow the new order once that
revision is approved and promoted. Frontend tests cover ID permutations, single-flight behavior,
rollback, localized accessible labels, and narrow curriculum state replacement. Dedicated Playwright
coverage exercises pointer and keyboard persistence, errors, English/Arabic layouts, mobile
behavior, non-editable state, and the candidate/live boundary.

Reviewer status remains `PENDING / UNASSIGNED` until an independent Claude review of the exact final
range.
