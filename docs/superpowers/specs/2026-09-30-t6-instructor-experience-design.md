# T6 Instructor Experience and Course Communications

Date: 2026-09-30
Status: approved for the local-only T6 tranche

## Scope

T6 adds owner-scoped instructor dashboard and course analytics reads, progress-safe roster
fields, immutable course announcements, and the instructor/student UI surfaces required to reach
those capabilities. Existing course revision, approval, entitlement, playback, audit, and outbox
semantics remain unchanged.

## Decisions

- Extend `catalog.Repository` with bounded, read-only projections because it already owns the
  course-owner SQL and roster surface; ownership stays in the SQL predicates, not in a later Go
  filter.
- Use migration 0052 for `course_announcements`. Announcements are published immediately and are
  immutable; no edit or delete route is opened in this tranche.
- Keep announcement delivery in-app only. The current outbox/email contracts are transactional
  auth/access notifications and do not provide a clean recipient fan-out abstraction; adding one
  would expand delivery semantics beyond this tranche. This decision is local-only and sends no
  email.
- Analytics explicitly reports learning activity based on progress timestamps; watch time is not
  collected and is not inferred.

## Backend design

`GET /api/v1/instructor/dashboard` requires `CONTENT_MANAGEMENT` and returns only courses owned by
the authenticated instructor. Each course includes lifecycle, published and candidate revision
state, latest change-request reason, enrollment count, learning-active students in the last seven
days, average current progress, durable completions, and alert codes. Nullable left-joined values
are scanned through nullable Go fields and normalized before JSON encoding.

`GET /api/v1/courses/:courseId/analytics` uses the existing owner route group and capability. It
returns course totals plus lesson reach in authored curriculum order. Reach counts distinct enrolled
students with any progress row for the lesson; completion counts rows with `completed_at`; the
response includes definitions and `watch_time_collected: false`.

The roster query retains all existing fields and adds progress percent, durable completion flag/date,
and last learning activity. It never selects email or phone. Progress is calculated against the
published lesson graph where present and remains zero-safe for enrolled students without progress.

Migration 0052 creates `course_announcements` with UUID identity, course and author foreign keys,
title/body length checks, publication timestamp, and an index for course chronology. The course
foreign key is `ON DELETE RESTRICT`: `DeleteCourse` returns the existing lifecycle-conflict class
when announcements exist, so the immutable trigger cannot turn deletion into a 500. A trigger keeps
published content immutable. Owner POST is CSRF/session protected, rate limited, strict-JSON bound,
and requires a published course; owner GET uses the same SQL ownership predicate and is available for
every course lifecycle, returning empty history for a never-published course. Owner and student reads
return bounded pages with `items`, `page`, `page_size`, and `has_more`; student GET first uses the
authoritative course read evaluator, then reads only announcements for an entitled course.

## Frontend design

The instructor home is a calm operations surface: a status-led course list, compact operational
numbers, alert strip, and action links for edit, analytics, students, announcements, and profile.
Analytics uses definition-aware tiles and a curriculum-ordered reach table with both counts and
percentages, so it remains useful without charts or hidden hover data. The roster keeps its privacy
boundary visible by showing learner display name, access, progress, completion, and last activity
only.

Announcements use the same course-owner shell: a bilingual title/body composer with length counters,
clear publish action, inline validation, loading/error/empty states, and a chronological list. The
student course home adds an announcements section after the course status/progress summary and
before the lesson graph.

The builder now places the existing status banner and change-request reason immediately below the
selected-course header, adds a derived readiness/media guidance region above the authoring workflow,
and keeps the existing submission checklist as the detailed review step. Curriculum empty states name
the next action for missing sections, lessons, and videos; upload controls expose processing, ready,
and failure/retry guidance from the server media state. The selected-course region and action groups
use wrapping/min-width-safe layout for mobile and tablet. No revision state transition or approval
rule is changed. Locale routes are canonical under `[locale]`; the Instructor role root is
`/[locale]/instructor`, with a separate course-builder navigation entry at
`/[locale]/instructor/courses`. All new labels are present in Arabic and English, with RTL/LTR-safe
layout and keyboard-visible focus.

## Verification

Integration coverage seeds populated course/revision/enrollment/progress/completion fixtures and
proves instructor isolation, three-lesson analytics ordering/drop-off, archived-roster visibility,
dashboard alerts, roster privacy, announcement entitlement isolation for revoked and never-entitled
students, draft POST/owner-history behavior, validation, and migration up/down/up behavior. The
announcement rate-limit denial is covered at the route middleware boundary, while the course-delete
announcement conflict is covered against real PostgreSQL. Frontend tests cover announcement draft
validation, builder readiness/media helpers, dashboard announcement reachability, wire-shape and
description-list contracts, bilingual dictionary parity, load/publish error separation, and the
localized Instructor root/navigation routes.
