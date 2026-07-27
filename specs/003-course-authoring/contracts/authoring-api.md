# Contract — Instructor Authoring API

**Slice**: S2 | **Plan**: [../plan.md](../plan.md) | **Base**: `/api/v1`

All routes require an authenticated session, `CONTENT_MANAGEMENT` through `identity.Authorize`, and —
on every `/courses/{id}` route — the single `RequireCourseOwnership` precondition. Errors use the
existing RFC 9457 problem envelope. Mutations require the established CSRF boundary; **CSRF is never
conditional** (S1C finding).

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
| `PATCH` | `/courses/{id}` | Edit bilingual title/description, taxonomy, preview | Writes to the editable revision, never the live one — FR-018 |
| `POST` | `/courses/{id}/sections` | Add a Section | |
| `PATCH`/`DELETE` | `/courses/{id}/sections/{sectionId}` | Edit, reorder, remove | Explicit `position` |
| `POST` | `/courses/{id}/sections/{sectionId}/lessons` | Add a Lesson | |
| `PATCH`/`DELETE` | `/courses/{id}/lessons/{lessonId}` | Edit, reorder, remove | |
| `PUT` | `/courses/{id}/lessons/{lessonId}/video` | Attach an Asset Version **reference** | Refused unless the version exists and is successfully processed — FR-005 |
| `PUT`/`DELETE` | `/courses/{id}/lessons/{lessonId}/files` | Manage resources and lab materials | Two distinct kinds — BR-067 |
| `PUT`/`DELETE` | `/courses/{id}/preview` | Set or clear the single preview asset | At most one — BR-143 |
| `POST` | `/courses/{id}/submit` | Submit for review | See below |
| `GET` | `/taxonomy/terms` | List assignable terms | Selection only; retired terms are not assignable — BR-158, BR-160 |

**No upload endpoint exists in this contract, in any form.** Media bytes are S4 (SLICES §3.2). A
request body carrying file content rather than an Asset Version identifier is a contract violation,
not an extension.

## Editing while `PUBLISHED`

Every mutation above, applied to a `PUBLISHED` Course, targets a **new pending revision** and leaves
the live graph untouched (FR-018, BR-017). The client does not choose this; the server does. There is
no route that edits a live revision, because such a route would be the defect BR-017 exists to
prevent.

## `POST /courses/{id}/submit`

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

On success the Course moves to `PENDING_REVIEW`, becomes read-only to its Instructor (FR-012,
BR-016), and writes an audit row plus an Admin-operations notification intent (FR-017) in the same
transaction.

A concurrent second submission loses on the partial unique index and returns `409` — not a duplicate
queue entry (plan §concurrency case 2).

## Read-only while in review

While `PENDING_REVIEW`, every mutation route above returns `409` with the current state named
(BR-016). This is enforced by an in-transaction state assertion, not by a UI affordance.
