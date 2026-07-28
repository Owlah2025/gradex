# Contract — Instructor Authoring API

**Slice**: S2 | **Plan**: [../plan.md](../plan.md) | **Base**: `/api/v1`

All routes require an authenticated session, `CONTENT_MANAGEMENT` through `identity.Authorize`, and —
on every `/courses/{id}` route — the single `RequireCourseOwnership` precondition. Errors use the
existing RFC 9457 problem envelope. Mutations require the established CSRF boundary; **CSRF is never
conditional** (S1C finding). The production catalogue foundation cannot mount without the real
repository and session-mutation foundation; origin/CSRF enforcement runs before capability and
ownership checks on every mutation.

## Denial semantics

A non-owner, a Student, or an anonymous caller requesting a Course they may not see receives a
response that **does not distinguish "does not exist" from "not yours"** (FR-002, spec US1 scenario 2).
This is the same non-enumeration discipline S1B applied to Accounts.

A suspended Instructor receives a refusal on every mutation (FR-008, BR-065).

## Routes

| Method | Path | Purpose | Rules |
|---|---|---|---|
| `POST` | `/courses` | Create a Course in `DRAFT` | BR-011, BR-014 |
| `GET` | `/courses` | List owned Courses | Owner-scoped; never returns another Instructor's |
| `GET` | `/courses/{id}` | Read own Course with its editable revision | FR-002 |
| `PUT` | `/courses/{id}/candidate` | Create or return the one active candidate | Atomic and idempotent; always returns the candidate identity |
| `PATCH` | `/courses/{id}/revisions/{revisionId}` | Edit bilingual title/description, taxonomy, preview | Candidate identity is explicit; the live revision is refused |
| `POST` | `/courses/{id}/revisions/{revisionId}/sections` | Add a Section | Candidate-scoped |
| `PATCH`/`DELETE` | `/courses/{id}/revisions/{revisionId}/sections/{sectionId}` | Edit, reorder, remove | Section must belong to the named candidate |
| `POST` | `/courses/{id}/revisions/{revisionId}/sections/{sectionId}/lessons` | Add a Lesson | Candidate-scoped |
| `PATCH`/`DELETE` | `/courses/{id}/revisions/{revisionId}/lessons/{lessonId}` | Edit, reorder, remove | Lesson must belong to the named candidate |
| `PUT` | `/courses/{id}/revisions/{revisionId}/lessons/{lessonId}/video` | Attach an Asset Version **reference** | Refused unless the version exists and is successfully processed — FR-005 |
| `PUT`/`DELETE` | `/courses/{id}/revisions/{revisionId}/lessons/{lessonId}/files` | Manage resources and lab materials | Candidate-scoped; two distinct kinds — BR-067 |
| `PUT`/`DELETE` | `/courses/{id}/revisions/{revisionId}/preview` | Set or clear the single preview asset | Candidate-scoped; at most one — BR-143 |
| `POST` | `/courses/{id}/revisions/{revisionId}/submit` | Submit the exact candidate for review | See below |
| `GET` | `/taxonomy/terms` | List assignable terms | Selection only; retired terms are not assignable — BR-158, BR-160 |

**No upload endpoint exists in this contract, in any form.** Media bytes are S4 (SLICES §3.2). A
request body carrying file content rather than an Asset Version identifier is a contract violation,
not an extension.

## Editing while `PUBLISHED`

`PUT /courses/{id}/candidate` locks the Course and captures `live_revision_id`. It returns an existing
active candidate or clones that exact live graph as one complete `DRAFT` candidate. Concurrent calls
return the same candidate because the transaction locks the Course and the database permits only one
active candidate. The candidate records the captured pointer as `based_on_revision_id`; the response
is `200` in both create and already-exists cases so retry behavior is identical.

Every authoring mutation after that contains `{revisionId}`. The server verifies that it is the
Course's editable candidate (`DRAFT` or `CHANGES_REQUESTED`) and that every named Section, Lesson, or
file belongs to it. `PENDING_REVIEW` counts as active for uniqueness but is read-only. The server
never selects an editable revision by latest revision number. Supplying the current live revision, a
terminal/read-only revision, or a candidate replaced by a competing request returns `409`. A
revision or child from another Course retains the existing uniform `403` concealed-resource
response; it is not a concurrency conflict.

`{sectionId}` and `{lessonId}` are stable logical identities. The repository resolves their
revision-owned version rows only through the explicit `{revisionId}`. Candidate cloning preserves
those IDs for unchanged Sections and Lessons while allocating new internal version-row IDs. A newly
created or explicitly deleted-and-recreated entity receives a new stable ID; replacing only a
Lesson's video preserves its Lesson ID and therefore its progress identity (BR-059).

For a never-published Course, the same candidate route returns the initial Draft created with the
Course. This keeps one mutation contract for both first publication and later revision.

## `POST /courses/{id}/revisions/{revisionId}/submit`

Returns `422` with **every** failure listed together — never the first one only (FR-009, FR-010,
SC-008):

```json
{
  "type": "https://gradex.app/problems/submission-incomplete",
  "title": "Course cannot be submitted",
  "status": 422,
  "violations": [
    {"code": "SECTION_EMPTY", "target": "section:<id>"},
    {"code": "LESSON_VIDEO_MISSING", "target": "lesson:<id>"},
    {"code": "TAXONOMY_DIMENSION_MISSING", "target": "course:<id>", "dimension": "SUBJECT"}
  ]
}
```

On first publication, success moves the Course and candidate to `PENDING_REVIEW`. For a Course with
an existing `live_revision_id`, only the candidate moves to `PENDING_REVIEW`; the Course remains
`PUBLISHED` and Student reads stay pinned to the committed live revision. In both cases the candidate
becomes read-only to its Instructor and the same transaction writes the audit row plus mandatory
Admin-operations notification intent (FR-017).

A stale or terminal `{revisionId}` and a genuine concurrent second transition return `409` — never
success and never a duplicate queue entry. Submission validation remains `422` and is not converted
to a conflict. Missing or unprocessed assets, invalid or unavailable taxonomy, and incomplete graph
violations use that same `422` envelope. Anonymous callers remain `401`; unauthorized, non-owning, or
acting suspended Instructors remain the existing uniform `403`.

## Read-only while in review

While `PENDING_REVIEW`, every mutation route above returns `409` with the current state named
(BR-016). This is enforced by an in-transaction state assertion, not by a UI affordance.
