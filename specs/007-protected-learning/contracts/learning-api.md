# Contract: S5 Protected Learning API

**Spec**: [../spec.md](../spec.md) | **Plan**: [../plan.md](../plan.md) | **Data model**: [../data-model.md](../data-model.md)

All routes are mounted on the production router through `WithLearningFoundation`, under `/api/v1`.
Every route requires an authenticated **Student** Account (FR-007); an Instructor or Admin Account
receives the uniform refusal, not a role-specific error.

---

## The one rule that governs every route here

**Every request admitted past its route's required rate limits calls `entitlement.Evaluate(student,
lesson, now)` in its own handler path, at request time.** No route authorises from a decision cached at
page load, session start, or a prior request (FR-001). The Progress source and Student/Lesson ceilings
run before that protected-learning evaluation; neither is an authorisation decision. No route compares
an expiry, checks a revocation flag, or inspects Entitlement scope itself — that logic exists once, in
S4 (FR-005, Principle IV).

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

The typed reason **is** recorded — as a structured `authorization_denied` event at `WARN`, internal
only (Principle IX). A distinguishable external denial tells an unauthorised caller which Lessons
exist and which they merely cannot reach, which is a content inventory. This is operational
telemetry, not the privileged audit persistence Admin mutations carry: S5 writes no denial row.

Because every cause answers identically, the log is the only place they can be told apart, and a
protected **write** that fails for a reason that is not a decision — a store that refused the row —
is recorded as `protected_write_failed` at `ERROR` with its operation, stage, and a closed failure
class, never as a denial. Counting an outage as a refusal would send whoever is on call to the
entitlement model. Neither event carries a request body, a report context, an explanation, a session
identity, an Account, a Revision or Asset Version, an Entitlement or Enrollment identity, a storage
key, or the store's own error text; route templates are recorded rather than resolved paths, so
resource identifiers do not reach the log through the URL.

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

### D-063 success response contract

The three learning-surface routes return HTTP `200` with `Content-Type: application/json`, direct
snake_case objects (no `data` wrapper), string UUIDs, and RFC 3339 UTC timestamps. Protected responses
use `Cache-Control: no-store`. Titles are localized from `Accept-Language` (Arabic is the default),
and the only exposed state enum is the presentation-only `learning_status`, either `active` or
`expired`.

`GET /api/v1/learn/dashboard` returns:

```json
{
  "courses": [
    {
      "course_id": "uuid",
      "title": "Course title",
      "learning_status": "active",
      "expires_at": "2026-12-22T20:59:59Z",
      "progress": {
        "completed_lessons": 4,
        "total_lessons": 10,
        "percent": 40
      }
    }
  ]
}
```

`courses` and `progress` are always present; `expires_at` may be `null`. Percentage is
`floor(completed_lessons * 100 / total_lessons)`, constrained to 0–100, and is zero when total is
zero. Partial playback receives no completion credit. Courses are ordered by Enrollment creation
descending, then Course ID ascending. No S5 pagination is provided. Active qualifying Courses are
returned as `active`; narrowly retained expired Courses are returned as `expired`; revoked,
out-of-scope, retired-ineligible, and non-retained Courses are omitted. An empty result is exactly
`{"courses":[]}`.

`GET /api/v1/learn/courses/{courseId}` returns:

```json
{
  "course_id": "uuid",
  "title": "Course title",
  "learning_status": "active",
  "expires_at": "2026-12-22T20:59:59Z",
  "progress": {"completed_lessons": 4, "total_lessons": 10, "percent": 40},
  "sections": [
    {
      "section_id": "uuid",
      "title": "Section title",
      "lessons": [
        {
          "lesson_id": "uuid",
          "title": "Lesson title",
          "progress": {"position_seconds": 125.5, "completed": false},
          "materials": [{"kind": "resource"}, {"kind": "lab_material"}]
        }
      ]
    }
  ]
}
```

Sections and Lessons are in authored order from the current approved live graph. Lesson IDs are
stable identities. Every Lesson has a Progress object; absent Progress is position `0` and
`completed: false`. Historical Progress for removed Lessons remains durable but is excluded from the
current graph and aggregation.

`materials` is always present and contains only the deterministic presentation kinds `resource`
and `lab_material` reported by S4 for current ready material in the live graph, in that order. It
contains no URL, Asset Version, storage, expiry, or authorization field. Retained-expired read
models return an empty array. A listed kind is not an authorization decision; activation uses the
fixed S4 entry route and reauthorizes independently.

`GET /api/v1/learn/courses/{courseId}/lessons/{lessonId}` returns:

```json
{
  "course_id": "uuid",
  "lesson_id": "uuid",
  "section": {"section_id": "uuid", "title": "Section title"},
  "title": "Lesson title",
  "learning_status": "active",
  "expires_at": "2026-12-22T20:59:59Z",
  "progress": {"position_seconds": 125.5, "completed": false},
  "navigation": {"previous_lesson_id": "uuid", "next_lesson_id": null},
  "materials": [{"kind": "resource"}, {"kind": "lab_material"}]
}
```

The Section object and Progress object are always present. Navigation follows authored Lesson order
across Section boundaries; first and last Lessons have the corresponding null target. These read
models never include Entitlement IDs, Enrollment IDs, revision IDs, evaluator decisions,
capability booleans, trusted duration, Asset Version IDs, manifests, signed URLs, or playback
sessions. Expired retained read models never authorize playback or Progress mutation.
The `materials` array is empty for retained-expired reads.

When a Lesson's media is missing, quarantined, scan-failed, or transcode-failed, its metadata remains
readable through this contract without media fields; the separate S4 playback request fails through
the uniform protected-unavailable response. The later player surface renders that result as
content-unavailable without introducing another authorization enum into this read model.

---

## Playback access

### `POST /api/v1/learn/lessons/{lessonId}/playback`

Requests a freshly issued, short-lived, **session-scoped** same-origin manifest URL from S4 for one
playback session (FR-008, BR-100). The manifest request revalidates access and rewrites its media
references to exact-object private-storage signatures; video bytes do not pass through the API.

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
`visibilitychange` → hidden, and `pagehide` (via same-origin keepalive `fetch`)
([R-04](../research.md#r-04--progress-reporting-interval-and-rate-limit-sizing)).
Rate-limited to **12 writes / min / (Student, Lesson)** — sized against that interval per FR-017 —
plus a server-derived source-address ceiling of **1,200 requests / min** with a **120-request burst**.
The source ceiling uses an individual IPv4 address or an IPv6 `/64`, is evaluated first, and fails
closed when its limiter cannot decide (D-061).

**Server behaviour, in order:**

1. **Authenticate and validate the active Student Account** through the shared protected-route
   middleware. This is not an Entitlement decision.
2. **Derive and enforce the trusted source-address ceiling**, then enforce the server-derived
   `(Student, stable Lesson)` ceiling. Neither limiter reads the Course graph, Enrollment,
   Entitlement, media, or Progress state.
3. **Revalidate runtime access** — `Evaluate` at request time. A delayed, retried, duplicated, or
   out-of-order write that arrives **after** access ended is **refused** (FR-014, BR-053, BR-116).
   This is the acceptance scenario most likely to be missed, because the happy path never exercises it.
4. **Resolve the Enrollment.** No Enrollment → no Progress (BR-114). The route **reads**; it never
   creates one (FR-015a).
5. **Bound the position** into `[0, trusted duration]` — a position beyond the duration or below zero
   is **clamped, not rejected**, so a bad tick does not lose the session (FR-011, acceptance
   scenario 5).
6. **Compute completion server-side**: at least **90%** of the **trusted duration of the exact Asset
   Version played** (FR-010, BR-051). Any client-reported percentage or duration is **ignored** —
   not validated, not trusted as a hint, ignored.
7. **Upsert** on `(enrollment_id, course_lesson_identity_id)` with `GREATEST` for the maximum and `COALESCE` for `completed_at` and the completing
   Asset Version (FR-012). Monotonicity and write-once completion are database semantics.

**Idempotent and safe under concurrency.** Duplicate, delayed, out-of-order, and concurrent writes
converge to the same row. Two devices playing the same Lesson never regress each other
([R-06](../research.md#r-06--monotonicity-under-concurrency)).

**No regression** across seeks, retries, replays, reconnections, concurrent devices, or video
replacement (FR-012, BR-059). When an Instructor replaces the video, Progress is preserved on the
stable Lesson identity, and the **new** Asset Version's trusted duration governs subsequent completion.

**Transient failure does not interrupt playback** (FR-013, BR-053). The client retries with backoff;
playback continues. A failed Progress write is never a reason to stop an otherwise-authorised session.

**Retry policy (D-062).** A chain has one initial request and at most two retries. Network failures
other than deliberate cancellation, and HTTP 408, 429, 500, 502, 503, and 504 retry; all other
statuses and malformed local input do not. Ordinary retries use nominal 2-second then 4-second
exponential delays with symmetric ±20% injectable jitter. A 429 uses a valid `Retry-After`
delta-seconds or future HTTP-date without jitter, or a 15-second fallback when absent, malformed,
negative, or expired. The chain stays bounded; new samples coalesce without resetting its budget.

One reporter holds at most one in-flight request, one pending greatest position, and one retry timer,
scoped to one stable Lesson and exact Asset Version. Replacing either scope or disposing the reporter
aborts ordinary work where supported and discards its state. `pagehide` sends one best-effort,
same-origin JSON `PUT` with credentials and `keepalive: true`, never `sendBeacon`, and starts no
delayed retry. Every automatic retry is a normal request through the server behaviour above.

**No client-supplied time is trusted** for expiry, completion, or ordering.

---

### Report contexts on protected reads

Active Course Home and active Lesson responses carry an **opaque, server-encrypted report context**
(D-065) binding a future report to the exact content instance that response rendered.

| Response | Field | Contents |
|---|---|---|
| Course Home (active) | `report_context` | one `COURSE` context bound to the Revision this response rendered |
| Lesson (active) | `report_contexts` | `lesson`, plus `video`, `resource`, `lab_material` **only when that target is present in the visible Lesson** |

- The token is authenticated-encrypted; raw Revision and Asset Version identifiers stay hidden and
  are never exposed as public fields.
- Contexts are minted **in memory** from the same resolved graph that produced the response — no
  additional query, and no second resolution of current content.
- **Absent** from Dashboard, from retained-expired Course Home and Lesson reads, and from every
  unavailable response. Absence is omission, not an empty string, and there is no `can_report`
  capability flag.
- Possession grants **no** authority: not playback, Progress, materials, Enrollment, Entitlement,
  or moderation.
- An expired context is refused; the Student reloads, which legitimately re-renders — and
  re-reports — the newer content.
- **T063** owns submission: decrypting the context, verifying reporter/session, and enforcing
  FR-033's current-Entitlement requirement — see `POST /api/v1/learn/reports` below.
- The **client** treats a context as opaque evidence it carries and never interprets. A report
  action exists exactly when the rendered read model carried a context for that target — never
  inferred from a title, a mounted player, or a material link — so an absent context is an absent
  action. The context is passed from the read model to the request body unchanged, and reaches no
  other place: not rendered text, not a `data-*` attribute, not a URL, not browser storage, not a
  log, and not an error message.

All responses carrying a context remain `Cache-Control: no-store`.

## Content reporting

### `POST /api/v1/learn/reports`

Creates a report in the Admin queue (FR-029 – FR-034). Requires an authenticated, active **Student**
Account and its authenticated session, on the same gate as every other route here.

**Request.** The body **names no target**:

```json
{
  "report_context": "<opaque encrypted context from the read that rendered the page>",
  "reason": "broken_unavailable | inaccurate | inappropriate | suspected_copyright_violation | other",
  "explanation": "optional free text; required when reason = other (FR-029)"
}
```

There is no public `target_id`, `target_kind`, `course_id`, `revision_id`, `asset_version_id`,
`target_revision_ref`, `reporter_account_id`, or `session_id`. The Course, the target kind, the
stable target, and the exact visible Revision or Asset Version come **only** from decrypting the
context (D-065); a client can neither read nor choose them. There is no legacy bare-target form.

Strict admission applies as it does to every write here: `Content-Type: application/json`, one JSON
value, unknown fields rejected, duplicate members rejected, trailing content rejected, no coercion,
and the same **16 KiB** body bound as `PUT …/progress` — the established protected-learning
mutation bound, which also owns the explanation's length since no field limit is specified.

**Success — `201 Created`, `Cache-Control: no-store`, no `Location`:**

```json
{
  "report_id": "uuid",
  "created_at": "2026-08-04T09:00:00Z"
}
```

Those two properties are the **complete allowlist**, and the response is built from them by name —
never by serializing the stored row — so a column added to `content_reports` cannot reach a client
by being added upstream (FR-034).

`report_id` is the reporter's own new report and a random UUID: not sequential, not guessable, and
not a handle to any route, because S5 exposes none that reads a report back. `created_at` is the
persisted `created_at` in the same UTC RFC 3339 form the read models use — not a queue timestamp, a
review deadline, or an estimated resolution time.

Absent, and forbidden to add here: any `status`, `state`, or moderation field; queue position,
priority, severity, assignment, moderator identity, or SLA; the count of similar reports, whether
another Student has reported the same content, whether the target is under investigation, or any
duplicate history; `target_revision_ref`, the Revision, the Asset Version, the stable target, the
Course, the Lesson, the reason, the explanation, the decrypted context, Entitlement or Enrollment
state, limiter state or remaining quota, and internal audit identifiers. The database's unresolved
state is an Admin-queue concern and is never published.

The acknowledgement is **identical in shape for all five target kinds** — only the independently
generated identifier and timestamp differ — so it reveals neither which kinds carry an Asset
Version, nor whether the Lesson had materials, nor whether the submitted context was stale.

**Server behaviour:**

- Verifies the context's envelope, version, purpose, expiry, future-issuance bound, reporter
  Account, and authenticated session before anything else server-held is touched. An **expired**
  context is refused, never renewed: the Student reloads, which legitimately re-renders — and
  re-reports — the newer content (D-065).
- Records both the **stable logical target** and the **exact visible revision or version** the
  context names (FR-030) — never re-resolved from what is live at submission.
- **Requires a current, authoritative Entitlement** for the target's Course at request time
  (FR-033). Possessing a context grants nothing. The S4 decision runs **inside the insert's own
  transaction**, so access that ends between authorisation and the write refuses the write.
- **Refuses uniformly.** An invalid, tampered, foreign, expired, or replayed context; a missing
  Enrollment or Entitlement; an expired or revoked Entitlement; a suspended Account; an emergency
  Course suspension; an unavailable Course; and a target that is no longer relationally valid all
  produce the byte-identical `404` above. Public input failures — malformed JSON, an unknown field,
  an oversized body, a reason outside the fixed set, `other` without an explanation — are ordinary
  `400`/`413`/`415`/`422` problems, because their answer is the same for every caller and depends on
  no target.
- **Duplicates** — the Student's own still-open report for the same stable target and kind (D-066) —
  return `409` with the generic state-conflict problem, naming nothing about the earlier report.
  Refusal stays in the database's partial unique index, never an application pre-check, so it
  survives concurrent submission ([R-11](../research.md#r-11--reports-are-rate-limited-and-deduplicated-which-are-different-controls)).
- **Rate-limited** to **5 submissions / hour / Student**, keyed on the authenticated Account alone —
  not the session, so signing in again does not reset it; not the Course or the report target, so a
  second Course opens no second allowance. The decision runs **first**, before the body is read and
  before the context is decrypted, so **every authenticated request reaching this route costs one
  attempt** whatever it would have returned: `201`, `409`, `400`, `422`, or the protected `404`. A
  throttle that only counted valid reports would leave forged and malformed submissions unbounded.
  Anonymous, Instructor, and Admin callers are refused by the Student gate before the throttle, and
  spend nothing.

  This is **separate from** duplicate refusal (D-066), which is a database index and answers `409`;
  the two fail differently and FR-032 requires both
  ([R-11](../research.md#r-11--reports-are-rate-limited-and-deduplicated-which-are-different-controls),
  [R-15](../research.md#r-15--the-report-submission-throttle)).

  Over quota:

  ```http
  HTTP/1.1 429 Too Many Requests
  Content-Type: application/problem+json
  Cache-Control: no-store
  Retry-After: 3600
  ```

  `Retry-After` is the policy window in whole seconds. The body is the standard rate-limited problem
  and names no Course, Lesson, report, Entitlement, context, remaining quota, limiter key, or
  backend; it varies between requests only by the ordinary correlation identifier. No report row is
  created, and nothing downstream — context verification, Entitlement evaluation, insertion — runs.
  A limiter **dependency failure** is not this response: it returns the uniform protected `404`,
  the boundary [D-061](../../../docs/DECISIONS.md#d-061--s5-progress-source-address-ceiling) already
  set for S5.
- **Changes nothing about the reported content** — not hidden, not retired, not altered, not
  reordered, not marked (FR-031, BR-146) — and mutates no Account, Enrollment, Entitlement,
  Progress, Course, Revision, Lesson identity, or Asset Version. Success adds exactly one
  `content_reports` row; a refusal adds none.
- The acknowledgement **reveals nothing** about Admin queue state, other reports, position in queue,
  or moderation outcomes (FR-034) — see the allowlist above. The same boundary holds on every
  refusal: the duplicate `409`, the protected `404`, and the throttle `429` are the shared problem
  envelope and nothing more. None carries an acknowledgement field, names the report it collided
  with, says whether the context decrypted, or reports remaining quota; a field-level `422` names
  the offending request field and never echoes what was sent.

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
