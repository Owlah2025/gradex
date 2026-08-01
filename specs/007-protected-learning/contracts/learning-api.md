# Contract: S5 Protected Learning API

**Spec**: [../spec.md](../spec.md) | **Plan**: [../plan.md](../plan.md) | **Data model**: [../data-model.md](../data-model.md)

All routes are mounted on the production router through `WithLearningFoundation`, under `/api/v1`.
Every route requires an authenticated **Student** Account (FR-007); an Instructor or Admin Account
receives the uniform refusal, not a role-specific error.

---

## The one rule that governs every route here

**Every route below calls `entitlement.Evaluate(student, lesson, now)` in its own handler path, at
request time.** No route authorises from a decision cached at page load, session start, or a prior
request (FR-001). No route compares an expiry, checks a revocation flag, or inspects Entitlement scope
itself — that logic exists once, in S4 (FR-005, Principle IV).

## The uniform refusal

Six causes — **expired**, **revoked**, **out of scope**, **Account suspended**, **emergency Course
access suspension**, **retired beyond BR-027 eligibility** — and a seventh, **the target does not
exist**, all produce one **byte-identical** response (FR-003, BR-023, BR-050):

```http
HTTP/1.1 404 Not Found
Content-Type: application/problem+json

{
  "type": "https://gradex.app/problems/not-found",
  "title": "Not found",
  "status": 404
}
```

Identical status, identical body, identical header set. No `Retry-After`, no `WWW-Authenticate`, no
distinguishing field, and no measurable timing difference between "you may not" and "it is not there".

The typed reason **is** recorded — structured log plus audit event, internal only (Principle IX). A
distinguishable external denial tells an unauthorised caller which Lessons exist and which they merely
cannot reach, which is a content inventory.

> Constructed in **one** place. Handlers return the typed decision and the response constructor maps
> every non-allow outcome; no handler writes its own refusal. This is what makes "all six are
> identical" a testable property rather than a convention.

---

## Learning surfaces

### `GET /api/v1/learn/dashboard`

The Student Dashboard's **learning half** — Continue Learning and My Courses (FR-023, ST05).

Returns, per Course the Student has an Enrollment for: course identity, per-Course progress
(completed Lessons / total Lessons in the qualifying graph — no duration weighting, no partial
credit), access state, and the access-until instant.

**Reads no Course Access Invitation state** (FR-006, BR-029). Access History (ST10) and the invitation
panel are S6's; this route serves neither.

Expired and revoked Entitlements still appear — with their retained Enrollment, retained Progress, and
an expired state (FR-016, BR-026). **None of that is an authorisation input**: the Course appears in
the list *and* every protected operation against it is refused.

### `GET /api/v1/learn/courses/{courseId}`

Course Home (FR-019 – FR-022, ST06).

Returns the Course's Sections and Lessons **in authored order** from the current approved or
qualifying acquired graph (BR-010, BR-017, BR-027), per-Lesson completion, per-Course progress, the
access-until instant, and per-Lesson availability.

- A Course Entitlement covers the **whole** Course — every Section and Lesson is in scope (FR-020,
  BR-021, BR-024).
- A Lesson whose video is missing, quarantined, scan-failed, or transcode-failed is **reachable and
  marked unavailable**; the rest of the Course remains usable.
- Locked and unavailable markers are **labels**. They are not the enforcement (Principle II).
- **No community-link field.** Deferred to S18 (D-046).

Refusal is the uniform 404 when no qualifying Entitlement exists — indistinguishable from a Course
that was never authored.

### `GET /api/v1/learn/courses/{courseId}/lessons/{lessonId}`

Lesson Player payload (FR-024, ST07): Lesson metadata, the outline rail's Section/Lesson list,
previous/next navigation targets, the Student's resume position, and completion state.

**Carries no signed URL and no token.** Playback access is issued by the separate request below, per
playback session. A payload that carried a signed URL would be a payload that could be cached into a
second access model.

---

## Playback access

### `POST /api/v1/learn/lessons/{lessonId}/playback`

Requests a freshly issued, short-lived, **session-scoped** signed URL from S4 for one playback session
(FR-008, BR-100).

- Issued **per playback session**, not per segment. HLS re-requests the same segment on seek,
  rebuffer, and rendition switch, so a single-use signature would break playback (S4's plan §Signed
  access).
- The client **must not** cache, persist, share, or reuse the URL across sessions or Students
  (FR-008).
- Rate-limited: **30 issuances / 10 min / Student**, plus a per-source-address ceiling (FR-017,
  BR-102), via `internal/ratelimit`.

**Mid-session expiry.** An already-issued signature stays valid for its short lifetime and **no new
access is issued**. The exposure window is bounded by the signature lifetime, not the session — which
is why the lifetime is short. Stated plainly because "revocation is instant" is impossible against an
already-issued presigned URL, and a contract claiming otherwise would be lying (S4's plan §Signed
access, FR-002 acceptance scenario 6).

### Protected downloads — not an S5 route

Resources and Lab Materials are issued by **S4's** endpoints (ST08 is S4's screen). S5 **links** to
them and never issues, signs, proxies, or caches those links (FR-028, BR-103, BR-143).

---

## Progress

### `PUT /api/v1/learn/lessons/{lessonId}/progress`

The only Progress write path (FR-009 – FR-016).

Request carries the reported **position in seconds** and the **Media Asset Version id** being played.
At every public API boundary, `lessonId` is the stable `course_lesson_identities.id`; the server
resolves current metadata through the authoritative live `course_lessons` row. It never persists a
revision-row ID or a legacy `lessons(id)` value as Progress identity.

Reported by the client every **15 s** during playback, and on pause, seek-settled,
`visibilitychange` → hidden, and `pagehide` (via `sendBeacon`)
([R-04](../research.md#r-04--progress-reporting-interval-and-rate-limit-sizing)).
Rate-limited to **12 writes / min / (Student, Lesson)** — sized against that interval per FR-017.

**Server behaviour, in order:**

1. **Revalidate runtime access** — `Evaluate` at request time. A delayed, retried, duplicated, or
   out-of-order write that arrives **after** access ended is **refused** (FR-014, BR-053, BR-116).
   This is the acceptance scenario most likely to be missed, because the happy path never exercises it.
2. **Resolve the Enrollment.** No Enrollment → no Progress (BR-114). The route **reads**; it never
   creates one (FR-015a).
3. **Bound the position** into `[0, trusted duration]` — a position beyond the duration or below zero
   is **clamped, not rejected**, so a bad tick does not lose the session (FR-011, acceptance
   scenario 5).
4. **Compute completion server-side**: at least **90%** of the **trusted duration of the exact Asset
   Version played** (FR-010, BR-051). Any client-reported percentage or duration is **ignored** —
   not validated, not trusted as a hint, ignored.
5. **Upsert** on `(enrollment_id, course_lesson_identity_id)` with `GREATEST` for the maximum and `COALESCE` for `completed_at` and the completing
   Asset Version (FR-012). Monotonicity and write-once completion are database semantics.

**Idempotent and safe under concurrency.** Duplicate, delayed, out-of-order, and concurrent writes
converge to the same row. Two devices playing the same Lesson never regress each other
([R-06](../research.md#r-06--monotonicity-under-concurrency)).

**No regression** across seeks, retries, replays, reconnections, concurrent devices, or video
replacement (FR-012, BR-059). When an Instructor replaces the video, Progress is preserved on the
stable Lesson identity, and the **new** Asset Version's trusted duration governs subsequent completion.

**Transient failure does not interrupt playback** (FR-013, BR-053). The client retries with backoff;
playback continues. A failed Progress write is never a reason to stop an otherwise-authorised session.

**No client-supplied time is trusted** for expiry, completion, or ordering.

---

## Content reporting

### `POST /api/v1/learn/reports`

Creates a report in the Admin queue (FR-029 – FR-034).

Request: `target_kind` (`COURSE` | `LESSON` | `VIDEO` | `RESOURCE` | `LAB_MATERIAL`), `target_id`,
`reason` from the fixed set, and `explanation` — **required when `reason = 'other'`** (FR-029).

**Server behaviour:**

- Records both the **stable logical target** and the **exact visible revision or version** at
  submission (FR-030).
- **Refuses** a report from a Student holding no Entitlement for the target's Course (FR-033) — the
  uniform refusal, since the alternative confirms the target exists.
- **Rate-limited** to 5/hour/Student **and** duplicate-refused by the partial unique index. Both,
  because they fail differently ([R-11](../research.md#r-11--reports-are-rate-limited-and-deduplicated-which-are-different-controls)).
- **Changes nothing about the reported content** — not hidden, not retired, not altered, not
  reordered, not marked (FR-031, BR-146).
- The acknowledgement **reveals nothing** about Admin queue state, other reports, position in queue,
  or moderation outcomes (FR-034).

**No resolution path exists in S5** — no dismiss, no request-changes, no delist, no retire, no
suspend. Those are S8's (FR-035). A report is immutable once created.

---

## Absent by decision

| Not here | Why |
|---|---|
| Any Entitlement create / extend / restore route | FR-005, Principle IV. S5 holds no grant path |
| Any Enrollment row-creating route | FR-015a. S5 creates the table, never a row |
| Any invitation, approval, rejection, or revocation route | S6 owns the enrollment lifecycle entirely |
| Any Course, Section, or Lesson authoring route | S2 owns Course content |
| Any community-link field on any payload above | Deferred to S18 under [D-046](../../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch) |
| Any report resolution route | S8 |
| Resource / Lab Material download issuance | S4 |
| Access History (ST10), invitation panel on ST05 | S6 |
