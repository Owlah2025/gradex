# AD-14 — Minimal Admin Reported-Content Resolution

## Authority and scope

AD-14 implements the limited paid beta contract in D-094: an active Admin can inspect
Student-submitted reports, dismiss a report, or resolve it after invoking an existing
Course lifecycle action where that action is applicable. Student report creation remains
owned by the existing protected-learning route. This design does not add automated
moderation, bulk actions, analytics, Office Hours, Instructor Analytics, or a new Account
suspension command.

## Approach

Add a small moderation foundation to the HTTP composition root. It receives the existing
learning report repository and catalog repository, mounts `/api/v1/admin/reports` behind
the existing Admin capability and session-mutation middleware, and delegates Course delist
requests to the same catalog command used by the Admin Course Lifecycle surface. Other
existing lifecycle and staff commands remain available on their canonical Admin surfaces.

The report repository owns the report read model and terminal resolution transaction. The
HTTP layer owns explicit DTOs, request validation, capability wiring, and translation of
domain errors into the repository's problem responses. The frontend owns only display
copy and interaction state; it never infers report or Course state optimistically.

## Data and resolution flow

1. `GET /admin/reports` returns at most the repository-standard page size of open reports,
   ordered oldest first with a stable ID tiebreaker.
2. `GET /admin/reports/:id` returns the report reason/explanation, safe target label,
   current Course lifecycle/access state, and any terminal resolution metadata. Missing
   target context is represented as unavailable so historical reports remain closable.
3. `POST /admin/reports/:id/resolve` accepts one terminal action and a bounded Admin reason.
   Dismissal updates only the report. The supported Course delist action runs through the existing catalog
   command first, then the report is conditionally resolved. If the canonical action fails,
   the report remains open and the existing lifecycle error is returned.
4. Resolution uses a row lock plus a terminal-state check; a second or concurrent attempt
   receives the standard state-conflict problem and cannot overwrite the first outcome.
5. The resolution row and a `MODERATION` audit event commit together. Existing lifecycle
   commands retain their own catalog audit events.

## Privacy and authorization

Admin DTOs expose only a Student-chosen display name, target type/label, reason, Student
explanation, timestamps, current target state, and terminal resolution fields. They do not
expose reporter IDs/contact data, Instructor private data, raw revision/asset IDs, credentials,
sessions, payment data, or audit internals. Every queue/detail/resolve route uses the existing
active-Admin capability resolver, so Instructor, Student, anonymous, suspended, and restricted
principals are denied server-side.

## Frontend and verification

The Admin workspace gains a `Reported Content` navigation entry and a focused queue/detail
page with loading, empty, error, open, resolved, and already-resolved conflict states. All
new copy is in the English and Arabic dictionaries; the page follows the existing RTL/LTR
locale direction and accessible button/form conventions.

Focused proof covers real PostgreSQL persistence, auditability, bounded ordering, target
unavailability, dismissal non-mutation, conditional resolution and concurrent resolution,
HTTP authorization/error responses, Student-report regression, frontend behavior, and one
browser workflow that submits a Student report before an Admin resolves it.
