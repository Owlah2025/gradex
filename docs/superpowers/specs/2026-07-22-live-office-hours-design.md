# Live Office Hours — MVP Feature Design

**Date:** 2026-07-22

**Reconciled:** 2026-07-23

**Status:** Approved feature design; technical mechanics require platform system design

**Scope:** MVP, external meeting link only

**Related:** [PRD](../../PRD.md), [Business Rules](../../BUSINESS_RULES.md) BR-120/122 and
BR-134–141, [Decision D-013](../../DECISIONS.md)

## 1. Summary

The owning Instructor schedules one-off office-hours sessions for their own Published Course.
Gradex owns the schedule, server-side authorization, safe join-link disclosure, status history, and
fixed notifications. Zoom/Google Meet/another approved external service owns the live call. Gradex
does not provide live-video infrastructure.

An Admin may inspect and cancel a session for moderation with a reason. Admins cannot create Course
or platform-wide office hours in MVP.

## 2. Locked MVP Boundary

| Area | Approved behavior |
|---|---|
| Creator | Owning Instructor only, for their own `PUBLISHED` Course |
| Admin | Read/moderate and cancel only; no creation or rescheduling on Instructor's behalf |
| Scope | Exactly one Course; no platform-wide/open audience |
| Student access | Any active Course Entitlement or any active Section Entitlement within the Course |
| Delivery | Stored external meeting link revealed only after authorization |
| Schedule | One-off; Instructor may create, materially reschedule, or cancel |
| Notification | Fixed event-driven policy; no preferences or timed reminders |
| Lifecycle | `SCHEDULED → COMPLETED` or `SCHEDULED → CANCELLED` |

This feature intentionally excludes recurring series, RSVP/capacity, calendar sync, attendance,
recordings, built-in video, platform-wide events, and automated reminders.

## 3. Conceptual Data

The system design must preserve these fields/meanings without treating this as a final schema:

| Field | Meaning |
|---|---|
| ID | Stable Session identifier |
| Course | Required owning Published Course |
| Created by | Required owning Instructor |
| Title/description | Localized platform fields or Instructor-authored text according to final content model |
| Start/end | UTC instants; end must be after start |
| External join link | Sensitive value excluded from public/list payloads and notifications |
| Status | Scheduled, completed (possibly derived), or cancelled |
| Cancellation/moderation | Actor, role, reason, timestamp |
| Created/updated timestamps | Audit and conflict handling |

Do not add audience type, platform scope, RSVP, capacity, recurrence, recording URL, attendance, or
reminder fields for MVP.

## 4. Authorization

### Instructor Mutation

Creation/material rescheduling succeeds only when all are true:

- caller is an Active, non-suspended Instructor;
- Course exists and caller is its owning Instructor;
- Course is currently `PUBLISHED`;
- transition is valid and input/link validation succeeds.

An Instructor cannot manage another Instructor's Session. Cancellation retains the record.
The owning Instructor may cancel an existing scheduled Session even after its Course becomes
Unpublished/Archived, but cannot create or reschedule in those states.

### Student Discovery and Join

A Student may discover/join the Course Session only when all are true:

- Account is Active and not suspended;
- Session is not cancelled and the Course remains `PUBLISHED`;
- Student has a current Course Entitlement or at least one current Section Entitlement belonging to
  that Course;
- any time-window rule selected during system design permits join.

The join link is returned only from an authenticated, authorized join operation. It is absent from
public/catalog payloads, notification content, logs, analytics, and unauthorized error responses.
The exact early-join/late-close window is a tunable system-design decision, not a product rule.

### Admin Moderation

An authenticated Admin may list/inspect Sessions and cancel any scheduled Session with a required
moderation reason. Admin access is audited. Admin cannot use this path to create a Session, turn it
platform-wide, impersonate the Instructor, or add attendees.

## 5. Proposed Capability Surface

Final paths and error envelopes belong to system design. The feature needs capabilities equivalent
to:

- Instructor: create, list own Course Sessions, reschedule/update, cancel.
- Student: list Sessions for entitled Courses, request authorized join-link disclosure.
- Admin: list/inspect all Sessions and cancel with a moderation reason.

Every mutation enforces role/ownership/status on the backend. List responses should not carry join
links; obtain the sensitive link only through the authorized join operation.

## 6. Notifications

Notification recording/delivery is best-effort and never controls the schedule mutation.

- New Session: in-app for currently entitled Students; email may also be used when operationally
  appropriate.
- Material reschedule: in-app and email to currently entitled Students.
- Cancellation: in-app and email to currently entitled Students.
- Deduplicate retried events and never include the external join link in notification content.

There are no preferences, timed reminders, marketing messages, SMS/WhatsApp, push, or calendar
invites in MVP.

## 7. Validation and Failure Behavior

- Validate supported HTTPS meeting-link shape/provider policy without fetching arbitrary URLs from
  the application network unless system design provides SSRF-safe handling.
- Store UTC instants and display in the user's locale/timezone; default to Kuwait time only when no
  preference is known.
- Prevent invalid intervals and edits after cancellation/completion except audited moderation data.
- Handle concurrent Instructor/Admin cancellation idempotently.
- A cancelled Session disappears from upcoming lists but remains available in authorized history.
- A newly expired/revoked Entitlement or suspended Account must fail join authorization even if the
  Student saw the Session earlier.
- External-provider outage/link failure is reported as supportable failure; Gradex does not claim to
  control third-party call availability.

## 8. Verification

- Role/ownership matrix: owner, foreign Instructor, Student, Admin, suspended Instructor.
- Course state matrix: Published allows an otherwise authorized Student to discover/join; Draft,
  Pending Review, Unpublished, and Archived deny Student discovery/join.
- Entitlement matrix: active Course; active Section in Course; Section in another Course; expired;
  revoked; none.
- Join-link secrecy: public/list/notification/log/error payloads never contain it.
- Lifecycle: create, reschedule, Instructor cancel, Admin moderation cancel, completed derivation,
  duplicate/concurrent cancellation.
- Notification: correct current recipients, deduplication, and business mutation surviving email
  failure.
- Localization/responsiveness/accessibility: Arabic/English, RTL/LTR, local time, and all Student
  actions on phone/tablet/laptop/desktop.

## 9. Out of Scope

- platform-wide/open sessions or Admin-created sessions;
- in-platform audio/video or reuse of the VOD pipeline;
- recurrence/duplication automation, RSVP, capacity, waitlists, attendance;
- recordings or recording links;
- timed reminders, calendar integration, SMS/WhatsApp, or push;
- Session chat, polls, materials, or payment separate from Course/Section Entitlement.
