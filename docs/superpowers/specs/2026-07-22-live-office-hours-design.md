# Live Office Hours — Design Spec

**Date:** 2026-07-22
**Status:** Approved (brainstorming) — ready for implementation plan
**Scope:** MVP feature addition
**Related:** [PRD.md](../../PRD.md) §4 Scope, [BUSINESS_RULES.md](../../BUSINESS_RULES.md) BR-023/025/065/070/120, [DECISIONS.md](../../DECISIONS.md) D-003/D-004/D-010

---

## 1. Summary

Instructors and admins schedule one-off live sessions. Gradex owns **scheduling, access
control, and event-driven notifications**; the live audio/video itself happens on a proven
third party (Zoom / Google Meet / Discord voice) reached via a stored join link. Gradex holds
no streaming infrastructure.

## 2. Scope tension (acknowledged)

PRD §4 explicitly deferred "Live mentorship / live sessions" out of MVP, and the course
community was decided to live on external Discord (D-004). This feature pulls a *lightweight*
form back in. It is kept lightweight on purpose — external video, no RSVP, no scheduler — so
it does not reopen the scope that was cut to protect the 2026-08-15 date.

## 3. Decisions locked in brainstorming

| # | Decision | Rationale |
|---|----------|-----------|
| Delivery | **External link, Gradex schedules** | Days of work vs months; zero streaming infra; no new compliance. In-platform WebRTC and reusing the VOD HLS pipeline were both rejected as far too large. |
| Creator | **Instructor (own course) + Admin (any course or platform-wide)** | Instructor mentorship serves the "no student left alone" principle; admin can also run platform-wide Q&As. |
| Access | Course-scoped = active enrollment; platform-wide = admin per-session toggle (`enrolled` vs `open`) | Reuses playback entitlement (BR-023/025); admin flexibility for open/marketing sessions. |
| Recurrence | **One-off only** ("duplicate" to repeat) | One row per session; recurrence → V1. |
| RSVP | **None** — show-and-join | No registration/capacity tables; Zoom/Meet enforces its own room cap. |
| Reminders | **Event-driven "new session" notice only** (on publish/reschedule/cancel) | Fits D-010's transactional model; needs no scheduler. Timed "1h-before" reminder → V1 once a scheduler exists. |

## 4. Data model

### `office_hours_sessions`
| column | type | notes |
|--------|------|-------|
| `id` | uuid | PK |
| `created_by` | uuid | instructor or admin user id |
| `creator_role` | enum | `instructor` \| `admin` (audit) |
| `course_id` | uuid null | **null = platform-wide** (admin only) |
| `title` | text | |
| `description` | text null | optional |
| `scheduled_start` | timestamptz | stored **UTC**, displayed Kuwait time UTC+3 (consistent BR-025) |
| `scheduled_end` | timestamptz | |
| `join_url` | text | Zoom / Meet / Discord link |
| `audience` | enum | `course_enrolled` (implied when `course_id` set) · `platform_enrolled` · `platform_open` |
| `recording_url` | text null | instructor pastes after the session |
| `status` | enum | `scheduled` \| `cancelled` (live/ended derived from time) |
| `created_at` / `updated_at` | timestamptz | audit |

## 5. Access resolution

One function, `(user, session) → allowed?`, reusing existing entitlement:

- **course-scoped** (`course_id` set) → active, non-expired enrollment in `course_id` (same
  check as playback, BR-023 / BR-025 — a lapsed 150-day enrollment is denied).
- **platform_enrolled** → any user with ≥1 active enrollment.
- **platform_open** → any authenticated user.
- **admin** → always (moderation).

**Join-URL reveal:** `join_url` is returned **only** to an entitled user, **only** within
`[scheduled_start − 15min, scheduled_end]`, status `scheduled`. It is never rendered on public
or catalog pages — so the external link is not a leak path.

## 6. API

### Instructor / admin
- `POST /office-hours` — create. Instructor: `course_id` must be an own **PUBLISHED** course.
  Admin: any course, or `null` (platform-wide) with an `audience` toggle.
- `PATCH /office-hours/:id` — edit time / title / link / recording_url.
- `POST /office-hours/:id/cancel` — cancel (not delete).
- `GET /office-hours?scope=mine` — sessions I created.

### Student
- `GET /office-hours` — upcoming sessions I'm entitled to.
- `GET /office-hours/:id/join` — returns `join_url` if entitled and within the window; else
  a too-early / 403 response.

### Admin moderation
- `GET /admin/office-hours` — all sessions.
- Cancel any session.

## 7. Notifications

Event-driven, reusing the D-010 email + in-app notification center; best-effort per BR-120
(a failed send never blocks the action):

- **On publish, reschedule, cancel** → notify entitled students.
- **Guard:** `platform_open` sessions do **not** mass-notify (would spam non-buyers and raise
  PDPL exposure) — they rely on the in-app upcoming list only. Notifications fire only for
  `course_enrolled` / `platform_enrolled` audiences.

## 8. Business rules (new — to slot into BUSINESS_RULES.md)

- Session times stored UTC, displayed Kuwait time (UTC+3), consistent with BR-025.
- An instructor may create/edit sessions only on their own PUBLISHED courses; cannot schedule
  on a course that isn't theirs.
- A **suspended instructor (BR-065)** cannot create or edit sessions (consistent with "blocks
  new submissions").
- **No admin-approval gate** — sessions publish immediately (unlike course content BR-070);
  admin moderates reactively and may cancel any session. An office-hours event is not graded
  content, and an approval queue would kill the spontaneity of scheduling a Q&A.
- **Cancel ≠ delete** — cancelled sessions are retained for audit and hidden from upcoming
  lists.
- Course-scoped entitlement is identical to playback (BR-023 / BR-025).

## 9. Testing

- **Unit:** entitlement matrix (3 audiences × enrolled / lapsed / non-enrolled / admin);
  join-window gating (too-early / during / after / cancelled).
- **Integration:** create notifies entitled-only; `platform_open` does not mass-notify;
  cancel notifies + hides; instructor cannot schedule on a foreign course; suspended
  instructor blocked; lapsed enrollment denied join.

## 10. Out of scope (MVP)

In-platform live video; recurring series; RSVP / capacity; timed pre-session reminders
(needs the deferred scheduler, D-010); attendance tracking; automatic recording (only a
manually-pasted `recording_url`); mass-notifying `platform_open` sessions.
