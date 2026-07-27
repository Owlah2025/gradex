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
| `GET` | `/review/courses/{id}` | Read the full candidate graph under review | Includes Draft content |
| `POST` | `/review/courses/{id}/approve` | Publish the Course or apply the revision | See below |
| `POST` | `/review/courses/{id}/request-changes` | Return it to the Instructor | **Reason mandatory** — BR-072 |
| `POST` | `/review/courses/{id}/preview/{lessonId}` | Authorize Admin video preview | Audited, distinct path — BR-081 |

## `approve`

One transaction, in this order, all-or-nothing:

1. `SELECT … FOR UPDATE` the Course row and re-assert `PENDING_REVIEW`. A caller that raced another
   Admin gets `409` naming the state actually found (concurrency case 1).
2. **Revalidate every referenced Asset Version** — present and successfully processed *now*, not at
   submission time (FR-025). A reference that decayed fails the approval closed.
3. Re-read the owning Instructor's account status. A suspended owner fails the approval closed
   (concurrency case 3), matching the S1B2 live-status-read precedent.
4. Re-check that no assigned taxonomy term was retired since submission.
5. Swap `courses.live_revision_id` to the approved revision, mark the previous live revision
   `SUPERSEDED`, and set `lifecycle = PUBLISHED`.
6. Write the `COURSE_PUBLISHED` audit row.
7. Write the Instructor notification intent.

Steps 5–7 share one transaction, so **no reader can observe a partial graph** (FR-020) and no publish
exists without its evidence. A first publication and a revision approval are the same code path —
BR-017 requires a revision to clear "the same Admin review flow".

`READY` media never bypasses this: processing state is an input to step 2, never a trigger (BR-091).

## `request-changes`

Reason is required and non-empty at the schema level, so an unexplained change request cannot be
recorded (BR-072). A **first-publication** Course moves to `CHANGES_REQUESTED` and stays hidden. A
**pending revision** is rejected while the currently Published version stays live and unchanged
(FR-021) — these two outcomes are different and the handler must not collapse them.

## `preview`

Authorizes Admin playback of any Lesson, including `PENDING_REVIEW` and Draft content, through a path
distinct from Student playback. It records Admin, Lesson, and timestamp, and it creates **no**
enrollment and **no** Entitlement (BR-081, FR-016). The distinction matters: an Admin preview that
minted an Entitlement would violate the provenance invariant in
[SLICES.md §3.1](../../../docs/launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation).
