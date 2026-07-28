# Contract — Admin Review API

**Slice**: S2 | **Plan**: [../plan.md](../plan.md) | **Base**: `/api/v1/admin`

Every route requires an authenticated Admin session and `CATALOG_PUBLISH` through
`identity.Authorize`. **`CATALOG_PUBLISH` is a new member of the closed capability set, not a check
performed beside it** (FR-041). No Instructor holds it, so FR-013 — an Instructor cannot publish by
any route — is a property of the capability grant rather than of a handler.

## Routes

| Method | Path | Purpose | Rules |
|---|---|---|---|
| `GET` | `/review/queue` | List Courses and revisions in `PENDING_REVIEW` | BR-070 |
| `GET` | `/review/courses/{id}/revisions/{revisionId}` | Read the exact candidate graph under review | Includes Draft content |
| `POST` | `/review/courses/{id}/revisions/{revisionId}/approve` | Publish the Course or apply the exact revision | See below |
| `POST` | `/review/courses/{id}/revisions/{revisionId}/request-changes` | Reject or return the exact revision | **Reason mandatory** — BR-072 |
| `POST` | `/review/courses/{id}/revisions/{revisionId}/preview/{lessonId}` | Authorize Admin video preview | Lesson must belong to candidate; audited, distinct path — BR-081 |

## `approve`

One transaction, with this lock and validation order, all-or-nothing:

1. Lock the Course row `FOR UPDATE`.
2. Lock the exact `{revisionId}` `FOR UPDATE`; verify it belongs to the Course, is still
   `PENDING_REVIEW`, and has `based_on_revision_id` equal to the locked Course's
   `live_revision_id`. A stale, replaced, approved, or rejected candidate returns `409`.
3. Lock the owner Account `FOR SHARE`, then referenced taxonomy and video/Asset Version rows
   `FOR SHARE` in ascending identifier order. Every owner, taxonomy, or asset mutation takes a
   conflicting row lock. Re-read owner eligibility, full submission completeness, processed Asset
   Versions, and taxonomy availability through readers bound to this same transaction. The
   deterministic order prevents dependency-lock inversion, and the locks protect validated state
   through commit; submission-time success is not trusted.
4. Mark the previous live revision `SUPERSEDED`, mark the candidate `APPROVED`, swap
   `courses.live_revision_id`, and keep/set `lifecycle = PUBLISHED`.
5. Write the `COURSE_PUBLISHED` audit row.
6. Write the Instructor notification intent to the durable outbox.
7. Commit.

Every step shares one PostgreSQL transaction, so a failure leaves the old pointer unchanged, the old
revision approved, the candidate unapproved, and no durable audit or outbox event claiming success.
The transaction persists intent only; no external notification delivery occurs inside it. A first
publication and a revision approval use the same review command, with the existing live pointer
nullable only for first publication.

`READY` media never bypasses this: processing state is an input to step 3, never a trigger (BR-091).
Missing/incomplete content, invalid or unavailable taxonomy, unavailable processed assets, and an
ineligible owning Instructor return the existing `422` validation envelope with a specific
violation. Caller authorization remains `401`/`403`. Only a stale, replaced, terminal, or competing
candidate state returns `409`.

## `request-changes`

Reason is required and non-empty at the schema level, so an unexplained change request cannot be
recorded (BR-072). The command locks the Course then exact candidate and revalidates
`PENDING_REVIEW`. A **first-publication** Course moves to `CHANGES_REQUESTED` and stays hidden. A
**pending revision** becomes `REJECTED` while Course lifecycle, `live_revision_id`, the currently
Published graph, enrollments, Entitlements, and Student access remain unchanged (FR-021). Rejection
persists `COURSE_REVISION_REJECTED` audit evidence and its notification intent in the same
transaction. Approval racing rejection yields one terminal state; the loser returns `409`.

## `preview`

Authorizes Admin playback of any Lesson, including `PENDING_REVIEW` and Draft content, through a path
distinct from Student playback. It records Admin, Lesson, and timestamp, and it creates **no**
enrollment and **no** Entitlement (BR-081, FR-016). The distinction matters: an Admin preview that
minted an Entitlement would violate the provenance invariant in
[SLICES.md §3.1](../../../docs/launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation).
`{lessonId}` is the stable Lesson identity and is resolved to a version row only inside the named
`{revisionId}`.
