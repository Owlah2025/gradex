# MVP-F20 / T4-D — Missing Subject Requests

**Date:** 2026-08-22  
**Status:** `PROVEN`

## Instructor flow

An Instructor may create an `ACADEMIC_CATALOG` draft with Institution and no Subject, request the
missing Subject, read their own request status/reason, and continue ordinary draft authoring. The
Course cannot be submitted until a canonical Subject is attached. The Instructor receives no
canonical Subject mutation or Curriculum authority; Student and anonymous callers receive no
request authority.

The existing `subject_requests` schema retains history and its one-pending-request-per-Course
constraint. Requests carry semantic Instructor, Course, Institution, optional proposed code,
bilingual titles, note, status, resolution, and timestamps. Audit records cover create, link,
approve-new, and reject without copying the request note/body into audit metadata.

## Admin resolution and race safety

Academic Catalog now contains the request queue and all three actions:

- **Link to Existing** searches canonical Subjects and attaches an eligible same-Institution Subject.
- **Approve as New** uses the canonical Subject domain path and database uniqueness; duplicate races
  return a semantic conflict and never synthesize a new code.
- **Reject** requires a reason, which the Instructor can read; the Course stays draft.

Resolution locks and compares the Course state. If the Instructor selects Subject A before a pending
request for B resolves, the request records `COURSE_SUBJECT_ALREADY_SELECTED` and the Course remains
A. Published Courses, another Instructor's Courses, cross-Institution Subjects, and otherwise
ineligible Courses cannot be reassigned.

## Proof

- Real-Postgres academic integration suite passed: `ok .../internal/academic 6.599s` on the final
  focused rerun; full relevant integration gate also passed in `28.863s`.
- HTTP request/auth/audit tests passed inside `internal/httpapi` (`294.114s` in the full relevant
  integration gate).
- D1 link, D2 approve-new/exactly-one, D3 rejection reason, and D4 no-overwrite race all passed in
  the focused `5 passed (1.0m)` run and final canonical run.
