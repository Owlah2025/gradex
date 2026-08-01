# Feature Specification: S5 — Protected Learning

**Feature Branch**: `feature/007-protected-learning`

**Created**: 2026-07-29

**Status**: Draft — both cross-slice conflicts raised during specification were **resolved by the
developer on 2026-07-29**; see [§Resolved Conflicts](#resolved-conflicts). Ready for
`/speckit-clarify` or `/speckit-plan`. Not yet frozen.

**Input**: S5 — the Student-facing learning experience for entitled Courses: adaptive HLS playback of
entitled Lessons through S4's short-lived signed media access, resume position, per-Lesson completion,
the entitled-Student learning shell (Course Home and Lesson Player with Section/Lesson navigation),
~~display of the external Course community link~~ (**struck — deferred to S18 by
[D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch)**;
see [C2](#c2--the-community-link-is-not-authored-anywhere)), and Student content reporting into the
Admin queue.

**Depends on**: **S2** (the authored Course graph and its lifecycle states), **S3** (the bilingual
responsive shell and locale persistence), and **S4** (Entitlement evaluation and signed media access).
**S5 implementation does not begin until S2, S3, and S4 close** on independent verdicts.

**Effort**: 10h. **Review Tier 3** — shared only with S1C, S4, and S6.

**Governing rules**: BR-007, BR-010, BR-017, BR-018, BR-021, BR-023, BR-024, BR-025, BR-026, BR-027,
BR-029, BR-050, BR-051, BR-052, BR-053, BR-059, BR-081, BR-082, BR-090, BR-100, BR-102, BR-103,
BR-112, BR-114, BR-115, BR-116, BR-143, BR-145, BR-146, BR-147, BR-149, BR-150, BR-151. Traceability
is carried per requirement, per Constitution Principle III.

**Journeys**: [SJ-07](../../docs/USER_JOURNEYS.md#sj-07--orient-in-course-home),
[SJ-08](../../docs/USER_JOURNEYS.md#sj-08--watch-and-resume-a-lesson),
[SJ-09](../../docs/USER_JOURNEYS.md#sj-09--use-resources-labs-community-and-office-hours) (**Resource
and Lab links only** — community deferred to S18 by D-046, office hours to S17),
[SJ-10](../../docs/USER_JOURNEYS.md#sj-10--report-content),
[SJ-12](../../docs/USER_JOURNEYS.md#sj-12--return-after-expiry).
**Screens**: ST05 (learning half), ST06, ST07, and the Report Content modal.

---

## The three boundaries this slice exists to hold

Everything else in S5 is presentation. These three are why it is Tier 3.

### 1. S5 is a consumer of access. It creates, extends, and infers nothing

S5 renders the only screens through which paid content is actually watched, and it does so for a
Student whose access was decided elsewhere. Every access decision S5 needs already exists: S4 owns
Entitlement evaluation, S6 owns Entitlement creation.

S5 therefore **MUST NOT** contain any path — route, command, screen, fixture, background job, or
configuration flag — that creates an Entitlement, extends `access_ends_at`, marks a revoked or
expired Entitlement usable, or substitutes its own access judgement for the evaluator's
(BR-028, BR-029, Constitution Principle IV).

The failure this prevents is specific and it is the likeliest defect in the slice: a player that
fetches its access decision once when the Course Home page loads, caches it in the UI, and then
authorises its own subsequent segment and Progress requests against that cached answer. That is not a
performance optimisation; it is a second access model, and it survives expiry, revocation, Account
suspension, and emergency Course access suspension. **Every playback issuance, every download, and
every Progress write revalidates at request time, server-side** (BR-023, BR-050, BR-053, BR-116,
PRD §11).

### 2. A rendered lock is not a denial

Course Home shows locked markers. A Lesson outside the entitled scope, a Course under emergency
access suspension, and an expired Entitlement all render as unavailable. None of that is enforcement
— it is a label (Constitution Principle II).

The enforcement is that **the server never issues a signed URL, never returns a manifest, and never
accepts a Progress write for content the requester cannot access**, and that it denies identically
whether the Lesson exists, is retired, is out of scope, or was never authored (BR-023, BR-050, and
S4's FR-022). A test that asserts a padlock icon renders proves nothing about this slice.

### 3. Progress survives access; access never survives through Progress

Enrollment and Progress outlive Entitlement expiry and revocation — a Student who returns after
expiry sees everything they completed (BR-026, SJ-12). This is a product requirement, and it is also
the slice's sharpest trap: the record proving a Student *once* had access must never become the
reason they *still* have access.

A retained Progress row, a retained Enrollment, and a Course in the Student's "My Courses" list are
all history. None of them is an authorisation input. The Entitlement alone authorises (BR-029).

---

## Scope Boundaries

Stated explicitly because S5 sits on top of three slices and beneath two more.

### In scope

- Course Home for an entitled Student: ordered Sections and Lessons from the current approved or
  qualifying acquired Course graph, per-Course and per-Lesson progress, access-until display, locked
  markers, and the entry point to each Lesson.
- Lesson Player: adaptive HLS playback through S4's signed access, resume, previous/next navigation,
  the Lesson outline rail, and the platform-owned player controls.
- Resume position and per-Lesson completion: server-authoritative position bounding, monotonic
  maximum, and the ≥90% completion calculation against the trusted duration of the exact played
  Asset Version.
- The Student Dashboard's **learning** surfaces: Continue Learning, My Courses with progress and
  expiry state.
- Student content reporting: target selection, fixed reason set, required explanation for `other`,
  rate limiting, and creation of the report record in the Admin queue.
- Arabic/English and RTL/LTR for every screen this slice owns, on phone, tablet, laptop, and desktop.
- Cutover of the legacy `progress` table to the BR-116 identity.

### Out of scope — owned elsewhere, and S5 must not acquire it

| Not in S5 | Owner | Why it would be wrong here |
|---|---|---|
| Creating, extending, or revoking an Entitlement | S6 grant; S8 adjustment | Principle IV: one grant boundary, one source |
| Creating the Enrollment record | S6 | The grant transaction owns it — see [C1](#c1--s5-needs-the-enrollment-record-and-s6-creates-it) |
| Media bytes, signing keys, upload, quarantine, scanning, transcoding, trusted duration | S4 | S5 consumes signed access; it never mints it |
| Issuing the protected Resource and Lab Material download links | S4 | S5 links to S4's endpoints; ST08 is S4's screen |
| Authoring Course content or structure | S2 | S5 renders the Course graph and writes none of it |
| The external Course community link, in any form | Deferred to S18 | [C2](#c2--the-community-link-is-not-authored-anywhere) / [D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch) — it leaves MVP entirely |
| Public catalogue, Course detail for non-entitled visitors, the bilingual shell itself | S3 | S5 renders inside S3's shell |
| Admin resolution of a content report — dismiss, request changes, delist, retire, suspend | S8 | S5 creates the report and stops |
| Instructor analytics and the Course roster | S8 | Reads Progress; does not produce it |
| Office hours, and any join link | Deferred to S17 | Removed from MVP; ST09 is not built |
| Access History (ST10), invitation states on ST05 | S6 | Invitation state is never read by a learning surface (BR-029) |
| Certificates, ratings, notes, wishlist, recommendations | Future register | Outside MVP |

---

## Resolved Conflicts

Constitution Principle I requires a conflict with an approved source document to be surfaced and
resolved explicitly, never absorbed by assumption. Three were found while writing or implementing this
spec and verified against the repository rather than inferred from the plan. The analysis is retained
below rather than deleted, because a conflict
that leaves no trace comes back as a surprise.

| ID | Conflict | Resolution | Artefact changed |
|---|---|---|---|
| **C1** | Progress is keyed by `enrollment_id`, but `enrollments` was assigned to S6, which runs after S5 | **Option A — S5 creates the table; S6 writes to it** | [`specs/006-course-access-grant/data-model.md`](../006-course-access-grant/data-model.md) §1 and §5, corrected 2026-07-29 |
| **C2** | The community link is a PRD MVP bullet that no slice authors | **Option B — deferred to S18, post-launch** | PRD scope reduction, recorded as [D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch) |
| **C3** | S5's proposed Progress FK targeted legacy `lessons(id)`, while S2 defines durable Student-visible Lessons as `course_lesson_identities` | **Progress uses the stable S2 Lesson identity** | [D-060](../../docs/DECISIONS.md#d-060--s5-progress-uses-stable-lesson-identities) |

**Effect of C1 on this spec**: FR-015 is unblocked. S5 owns the `enrollments` migration, and S5 is the
first slice to define the record — but it still **creates no Enrollment rows**. Enrollment creation
remains S6's grant transaction alone (BR-167, Principle IV). Defining a table and writing to it are
different capabilities, and S5 takes only the first.

**Effect of C2 on this spec**: User Story 5 and FR-036 – FR-038 are struck from the slice. They are
retained below, marked `DEFERRED — S18`, on the same reasoning the constitution gives for keeping the
deferred payment rules present: a requirement that gets deleted comes back as an unreviewed one.
Nothing else in the slice depended on them.

**Effect of C3 on this spec**: Progress is unique on `(enrollment_id,
course_lesson_identity_id)`. The live revision's `course_lessons` row supplies current metadata; an
exact approved Asset Version is validated separately. No legacy mapping or synthetic legacy Lesson
row is introduced.

### C1 — S5 needs the Enrollment record, and S6 creates it

**The conflict.** BR-116 fixes Progress identity as `UNIQUE(enrollment_id, lesson_id)`, and BR-114
states a Progress record cannot exist without a corresponding Enrollment. S5 writes Progress and runs
**August 5–6**. But
[`specs/006-course-access-grant/data-model.md`](../006-course-access-grant/data-model.md) assigns the
`enrollments` table to **S6**, which runs **August 8**, and records that ownership was moved there on
2026-07-29 because S6 is the only slice that *writes* to it. S4's specification never claimed the
table either — it names Enrollment only as something that survives revocation.

So the table Progress is keyed by does not exist when S5 needs it. This is precisely the forward
dependency [SLICES.md §2 rule 2](../../docs/launch/SLICES.md) forbids, and it is invisible in that
register's dependency table because the table lists modules, not tables.

**Why this cannot be resolved by informed guess.** The three available resolutions have materially
different consequences:

| Option | Consequence |
|---|---|
| **A — S5 creates the `enrollments` table; S6 writes to it** | Matches the S4/S6 split precedent exactly: the consumer slice defines the record, the producer slice populates it. Costs S6 a migration-number rebase and a one-line ownership correction in its data model. Preserves both slices' calendar positions. **Recommended.** |
| **B — Progress is keyed by `(student, course, lesson)` until S6** | Violates BR-116 as written and requires a data migration inside S6 to re-key live Progress. Rejected unless BR-116 is amended first, which is a business-rule change, not an implementation choice. |
| **C — S5 moves after S6 in the calendar** | Removes the conflict but costs the only remaining float and inverts the [SLICES.md §3.1](../../docs/launch/SLICES.md) rationale, which exists so the consumer is proven before the producer wires to it. |

**RESOLVED 2026-07-29 — option A.** S5 creates the `enrollments` table; S6 writes to it and asserts
the expected shape before doing so. [S6's data model](../006-course-access-grant/data-model.md) was
corrected the same day. S5 gains the migration and gains **no** ability to create an Enrollment row.

### C2 — The community link is not authored anywhere

**The conflict.** [PRD §MVP](../../docs/PRD.md) lists "External Discord/Telegram Course community
link" as in scope; [SLICES.md §6](../../docs/launch/SLICES.md) assigns display to S5 and *authoring*
to S2; [DOMAIN_MODEL.md](../../docs/DOMAIN_MODEL.md) carries the community link on the Course
revision. But a repository search of `specs/003-course-authoring/` for `community`, `discord`, and
`telegram` returns **no match**, and no migration through `0010` defines such a field.

S2 is the active slice and is mid-implementation under D-044, with T043–T064 frozen. S5 cannot
display a value no slice produces, and S5 must not author it — that would put a Course-content field
outside the authoring slice that owns Course content.

**Resolution required from the developer**, choosing between:

| Option | Consequence |
|---|---|
| **A — Add the field to S2's remaining frozen queue** | Correct owner. Requires touching a frozen queue under D-044, which is a scope change to an in-flight slice and needs explicit approval. |
| **B — Defer the community link to post-launch (S18)** | Removes one PRD MVP bullet. Cheapest, and the link is a convenience rather than a launch-critical access path — but it is a **PRD scope reduction** and only the product owner may make it. |
| **C — S5 owns a Course-level community link field** | Cheapest to schedule and **not recommended**: it splits Course authoring across two slices and gives the learning slice a write path into Course content. |

**RESOLVED 2026-07-29 — option B.** The external Course community link is **deferred to S18,
post-launch**, and leaves the MVP scope. Recorded as
[D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch)
because it is a PRD scope reduction, not an engineering choice. User Story 5 and FR-036 – FR-038 are
struck from this slice and retained as `DEFERRED — S18`. S2's frozen queue is untouched.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 — An entitled Student watches a Lesson and never loses their place (Priority: P1)

A Student with an active Entitlement opens their Course, starts a Lesson, watches part of it, closes
the browser, and returns the next day on a different device. Playback resumes from where they stopped.
When they finish enough of the Lesson, it is marked complete and stays complete.

**Why this priority**: This is the product. Every other slice exists so that this journey works; if it
fails, Gradex has taken money for something a Student cannot use.

**Independent Test**: Seed an active Entitlement through S4's non-production seed mechanism, play a
Lesson to a position, terminate the session, re-open, and assert the resume position and the
completion state server-side.

**Acceptance Scenarios**:

1. **Given** an active Entitlement, **When** the Student opens an entitled Lesson, **Then** playback
   is authorised through a freshly issued short-lived signed URL and begins from the last position
   reached, not from zero. *(BR-023, BR-052, BR-100; integration + E2E)*
2. **Given** the Student has watched at least 90% of the exact Asset Version being played as
   calculated by the server from its trusted duration, **When** the position is reported, **Then** the
   Lesson is marked complete and `completed_at` is written once. *(BR-051; integration)*
3. **Given** a completed Lesson, **When** the Student seeks backwards, replays it, or the Instructor
   replaces the Lesson video, **Then** completion and the maximum position do not regress and
   `completed_at` is not rewritten. *(BR-051, BR-059; integration)*
4. **Given** the client reports a watched percentage that disagrees with the server's calculation,
   **When** the Progress write is handled, **Then** the client value is ignored. *(BR-051; security
   integration)*
5. **Given** a reported position beyond the trusted duration or below zero, **When** the Progress
   write is handled, **Then** the position is bounded before the monotonic update rather than
   rejected in a way that loses the session. *(BR-051, BR-116; unit + integration)*
6. **Given** a Progress write fails transiently, **When** playback continues, **Then** playback is not
   interrupted and the write is retried. *(BR-053; integration)*
7. **Given** a Progress write is delayed or retried and arrives **after** access ended, **When** it is
   handled, **Then** runtime access is revalidated and the write is refused. *(BR-053, BR-116;
   security integration)*

---

### User Story 2 — Access that has ended stops working immediately, everywhere (Priority: P1)

A Student's Entitlement expires, is revoked, their Account is suspended, or an Admin invokes emergency
access suspension on the Course. Playback stops being authorised, downloads stop, and Progress stops
being accepted — including for a session that was already open and playing.

**Why this priority**: Same priority as Story 1 and for the opposite reason. This is the boundary
between a paid platform and a free one, and it is why this slice is Tier 3.

**Independent Test**: Start an authorised playback session, mutate the access condition server-side
mid-session, and assert that the *next* signed issuance, the next segment authorisation, and the next
Progress write are all denied without the client having to cooperate.

**Acceptance Scenarios**:

1. **Given** an open, playing session, **When** the Entitlement expires, is revoked, the Account is
   suspended, or emergency Course access suspension is invoked, **Then** the next playback issuance
   and the next Progress write are denied. *(BR-007, BR-023, BR-053, BR-090; E2E)*
2. **Given** any of those conditions, **When** access is denied, **Then** no Entitlement, Enrollment,
   or Progress record is mutated as a side effect of the denial. *(BR-026, BR-090; integration)*
3. **Given** a Lesson in a Course the Student holds no Entitlement for, **When** playback or a
   Progress write is requested, **Then** it is denied identically to a Lesson that does not exist —
   no existence, title, duration, or structural detail is disclosed. *(BR-023, BR-050; security
   integration)*
4. **Given** a Course that is delisted, retired, or archived, **When** an otherwise-qualifying
   Entitlement requests playback, **Then** access continues, subject to the BR-027 retirement
   eligibility comparison. *(BR-027, BR-050, BR-090; integration)*
5. **Given** an expired Entitlement, **When** the Student signs in, **Then** they see their retained
   Enrollment, retained Progress, and an expired state — and no playback, download, or Progress write
   is authorised from any of it. *(BR-026, BR-029; E2E)*
6. **Given** a signed URL issued moments before access ended, **When** it is presented after access
   ended, **Then** it does not extend access beyond S4's token boundary and no new issuance is
   granted. *(BR-023, BR-100; security integration)*

---

### User Story 3 — A Student navigates a Course and sees exactly their scope (Priority: P2)

The Student opens Course Home, sees the ordered Sections and Lessons of the Course, their progress
through it, when access ends, and which Lessons remain — in Arabic by default, laid out right-to-left,
on a phone.

**Why this priority**: Necessary for Stories 1 and 2 to be reachable by a real user, but the security
boundary lives in those stories, not here.

**Independent Test**: Render Course Home for an entitled Student against a seeded multi-Section Course
and assert ordering, progress aggregation, expiry display, and RTL/LTR correctness at representative
viewports.

**Acceptance Scenarios**:

1. **Given** an entitled Student, **When** Course Home loads, **Then** Sections and Lessons appear in
   their authored order from the current approved or qualifying acquired Course graph. *(BR-010,
   BR-017, BR-027; integration)*
2. **Given** a Course Entitlement, **When** scope is displayed, **Then** every Section and Lesson in
   that Course is shown as in scope — Course scope is whole-Course. *(BR-021, BR-024; integration)*
3. **Given** Arabic is selected or defaulted, **When** any S5 screen renders, **Then** layout,
   navigation, controls, and date display are right-to-left and the preference persists across the
   session. *(BR-149, BR-151; rendered E2E)*
4. **Given** Instructor-authored content in either language, **When** it renders, **Then** it is not
   translated. *(BR-150; E2E)*
5. **Given** each S5 screen, **When** it is exercised at phone, tablet, laptop, and desktop
   viewports, **Then** no Student capability is missing at any of them. *(BR-147; responsive E2E)*
6. **Given** the Lesson Player, **When** it is operated by keyboard alone and by a screen reader,
   **Then** every platform-owned control is reachable and labelled. *(BR-151, `LG-015`; accessibility
   audit)*
7. **Given** the Student Dashboard, **When** it loads, **Then** Continue Learning and My Courses show
   per-Course progress and access state, and read no Course Access Invitation state. *(BR-029;
   integration)*

---

### User Story 4 — An entitled Student reports content (Priority: P3)

A Student finds a broken video, an inaccurate Lesson, or something that should not be published, and
tells Gradex. Nothing disappears; an Admin decides later.

**Why this priority**: Required for MVP and legally useful, but no Student is blocked from learning
without it, and resolution is S8's.

**Independent Test**: Submit reports from an entitled and a non-entitled Student against each target
type, and assert queue entry, rate limiting, and that the reported content remains visible.

**Acceptance Scenarios**:

1. **Given** an entitled Student, **When** they report a Course, Lesson, video, Resource, or Lab
   Material with a fixed reason, **Then** a report is created that preserves both the stable logical
   target and the exact visible revision or version. *(BR-145; integration)*
2. **Given** the reason `other`, **When** no explanation is supplied, **Then** submission is refused.
   *(BR-145; integration)*
3. **Given** a submitted report, **When** it enters the queue, **Then** the reported content is not
   hidden, retired, or altered in any way. *(BR-146; E2E)*
4. **Given** repeated or duplicate submissions, **When** they exceed the configured limit, **Then**
   they are throttled. *(BR-145; integration)*
5. **Given** a Student with no Entitlement for the target's Course, **When** they submit a report,
   **Then** it is refused server-side. *(BR-145; security integration)*
6. **Given** a successful submission, **When** the acknowledgement renders, **Then** it reveals
   nothing about Admin queue state, other reports, or moderation outcomes. *(BR-146; integration)*

---

### ~~User Story 5 — An entitled Student opens the Course community~~ — `DEFERRED — S18`

Struck from this slice by [C2](#c2--the-community-link-is-not-authored-anywhere) /
[D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch).
Retained, not deleted, so that S18 inherits reviewed requirements rather than rewriting them.

**Acceptance Scenarios** *(deferred)*:

1. **Given** an entitled Student and a Course with an authored community link, **When** Course Home
   renders, **Then** the link is shown. *(BR-029; integration)*
2. **Given** a Student with no active Entitlement, or an unauthenticated visitor, **When** any Course
   payload is served, **Then** the community link value is absent from the payload — not hidden in
   the interface. *(Principle II; security integration)*

---

### Edge Cases

- A Lesson whose video has never been uploaded, is still quarantined, failed its scan, or failed
  transcode: the Lesson is reachable and marked unavailable; no signed issuance is attempted; the
  rest of the Course remains usable.
- The Instructor replaces a Lesson's video while a Student is mid-Lesson: Progress is preserved
  because it is keyed to the Lesson, and the new Asset Version's trusted duration governs subsequent
  completion calculation. *(BR-051, BR-059)*
- A Course revision is approved while a Student is inside the Course: the Student's view of structure
  follows the qualifying graph rather than changing under them mid-session.
- Two devices playing the same Lesson simultaneously: the monotonic maximum is preserved and neither
  session regresses the other's progress.
- Clock skew or a client-supplied timestamp: no client-supplied time is trusted for expiry,
  completion, or ordering.
- A Student holding Entitlements for two Courses that share an Instructor: no cross-Course data leaks
  into either Course Home.
- An Account that is not a Student Account requesting a learning surface: refused. Instructor and
  Admin Accounts never receive Student Progress or Entitlements; Admin preview is the separate
  audited path. *(BR-081, BR-082)*
- Progress rows for a Lesson later deleted with its Course under BR-018: removal follows the authoring
  slice's deletion constraint; S5 introduces no delete path of its own. *(BR-018, BR-112, BR-115)*

---

## Requirements *(mandatory)*

### Functional Requirements

#### Access evaluation — S5 consumes, never decides

- **FR-001**: System MUST authorise every playback issuance, every protected download entry point,
  and every Progress write against the S4 Entitlement evaluator **at request time**, and MUST NOT
  authorise any of them against a decision cached from page load, session start, or a prior request.
  *(BR-023, BR-050, BR-053, BR-116)*
- **FR-002**: System MUST deny playback and Progress writes when the Entitlement is expired or
  revoked, the Account is suspended, or the Course is under active emergency access suspension, and
  MUST make each denial effective for an already-open session, not only for a new one. *(BR-007,
  BR-023, BR-053, BR-090)*
- **FR-003**: System MUST deny access to content outside the Student's entitled scope identically to
  content that does not exist, disclosing no existence, title, duration, ordering, or structural
  detail. *(BR-023, BR-050)*
- **FR-004**: System MUST continue to authorise a qualifying Entitlement against a delisted, retired,
  or archived Course, applying the BR-027 comparison of `retirement_eligibility_at` against
  `retired_at`. *(BR-027, BR-050, BR-090)*
- **FR-005**: System MUST NOT provide any path — route, command, screen, background job, fixture, or
  configuration flag — that creates an Entitlement, extends `access_ends_at`, or restores a revoked or
  expired Entitlement. *(BR-028, Principle IV)*
- **FR-006**: System MUST NOT read Course Access Invitation state in any authorisation decision or on
  any learning surface. *(BR-029)*
- **FR-007**: System MUST refuse a learning surface, Progress write, or content report to any Account
  that is not a Student Account. *(BR-082)*

#### Playback, resume, and completion

- **FR-008**: System MUST obtain playback access as a freshly issued, short-lived, session-scoped
  signed URL from S4 for each playback session, and MUST NOT cache, persist, share, or reuse a signed
  URL across sessions or Students. *(BR-100)*
- **FR-009**: System MUST resume a reopened Lesson from the last position the Student reached.
  *(BR-052)*
- **FR-010**: System MUST calculate completion server-side as at least 90% of the **trusted duration
  of the exact Media Asset Version played**, and MUST treat any client-reported percentage or duration
  as untrusted input. *(BR-051)*
- **FR-011**: System MUST validate and bound a reported position before applying it, and MUST update
  the maximum position monotonically. *(BR-051, BR-116)*
- **FR-012**: System MUST write `completed_at` exactly once, retain the Asset Version that completed
  the Lesson, and MUST NOT regress completion or maximum position across seeks, retries, replays,
  reconnections, concurrent devices, or video replacement. *(BR-051, BR-059)*
- **FR-013**: System MUST NOT interrupt an otherwise-authorised playback session because a Progress
  write failed transiently, and MUST retry the write. *(BR-053)*
- **FR-014**: System MUST revalidate runtime access when handling a delayed, retried, duplicated, or
  out-of-order Progress write, and MUST refuse it if access has since ended. *(BR-053, BR-116)*
- **FR-015**: System MUST key Progress by `(enrollment, lesson)` with that pair unique, and MUST NOT
  create a Progress record without a corresponding Enrollment. `lesson` means the stable
  `course_lesson_identity_id`, never a revision row or `lessons(id)`. *(BR-114, BR-116, D-060)*
- **FR-015a**: System MUST create the `enrollments` table with the shape S6 asserts against —
  `(student_account_id, course_id)` unique — and MUST NOT create, modify, or delete any Enrollment
  **row**. Row creation is S6's grant transaction alone. *(BR-114, BR-167, Principle IV;
  [C1](#c1--s5-needs-the-enrollment-record-and-s6-creates-it))*
- **FR-016**: System MUST preserve Enrollment and Progress across Entitlement expiry and revocation,
  and MUST NOT treat either record as an authorisation input. *(BR-026, BR-029)*
- **FR-017**: System MUST rate-limit and monitor playback issuance requests per Student and per
  source address. *(BR-102)*
- **FR-018**: System MUST cut the legacy `progress` table over to the BR-116 identity as a
  forward-only migration that preserves existing rows, and MUST leave no route writing the legacy
  shape afterwards. *(BR-116, D-031)*

#### Learning surfaces

- **FR-019**: System MUST present Course Home with the Course's Sections and Lessons in authored
  order, drawn from the current approved or qualifying acquired Course graph. *(BR-010, BR-017,
  BR-027)*
- **FR-020**: System MUST show every Section and Lesson of an entitled Course as in scope, because a
  Course Entitlement covers the whole Course. *(BR-021, BR-024)*
- **FR-021**: System MUST display the Student's per-Lesson and per-Course progress and the
  access-until instant, rendered in the Student's locale. *(BR-025, BR-052, BR-149)*
- **FR-022**: System MUST render distinct, non-misleading states for active access, expired access,
  delisted-but-accessible, emergency-suspended, and content-unavailable. *(BR-090)*
- **FR-023**: System MUST provide the Student Dashboard's Continue Learning and My Courses surfaces
  showing per-Course progress and access state. *(BR-029; ST05)*
- **FR-024**: System MUST provide a Lesson Player with play, pause, seek, volume, quality selection,
  fullscreen where available, previous/next Lesson navigation, and a Lesson outline rail. *(BR-147;
  ST07)*
- **FR-025**: System MUST make every platform-owned player control keyboard-operable and
  screen-reader-labelled to WCAG 2.2 AA, and MUST NOT claim complete product-level conformance while
  captions are outside MVP. *(BR-151, `LG-015`)*
- **FR-026**: System MUST render every S5 screen in Arabic and English with correct RTL and LTR
  layout, Arabic as the default, and a persistent preference; and MUST NOT translate
  Instructor-authored content. *(BR-149, BR-150)*
- **FR-027**: System MUST deliver every S5 Student capability at phone, tablet, laptop, and desktop
  viewports without capability loss at any of them. *(BR-147)*
- **FR-028**: System MUST link to S4's protected Resource and Lab Material download entry points
  without issuing, signing, proxying, or caching those links itself. *(BR-023, BR-103, BR-143)*

#### Content reporting

- **FR-029**: System MUST allow an entitled Student to report a Course, Lesson, video, Resource, or
  Lab Material with a fixed reason set, requiring an explanation when the reason is `other`.
  *(BR-145)*
- **FR-030**: System MUST record on each report both the stable logical target and the exact visible
  revision or version at the time of reporting. *(BR-145)*
- **FR-031**: System MUST NOT hide, retire, alter, or otherwise change the visibility of reported
  content as a consequence of a report. *(BR-146)*
- **FR-032**: System MUST rate-limit report submission per Student and refuse duplicates. *(BR-145)*
- **FR-033**: System MUST refuse a report from a Student holding no Entitlement for the target's
  Course. *(BR-145)*
- **FR-034**: System MUST acknowledge a submitted report without disclosing Admin queue state, other
  reports, or moderation outcomes. *(BR-146)*
- **FR-035**: System MUST NOT implement report resolution, dismissal, delisting, retirement, or any
  Admin moderation action. *(BR-146; S8 boundary)*

#### Community link — `DEFERRED — S18`, not implemented in this slice

Struck by [C2](#c2--the-community-link-is-not-authored-anywhere) /
[D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch).
Retained verbatim so S18 inherits reviewed requirements. **S5 implements none of these, and a
production build must contain no community-link field, payload, or screen element.**

- **FR-036**: System MUST display the Course's external community link to Students holding an active
  Entitlement for that Course. *(BR-029)* — `DEFERRED — S18`
- **FR-037**: System MUST omit the community link value entirely from any payload served to a
  non-entitled or unauthenticated requester, rather than hiding it in the interface. *(Principle II)*
  — `DEFERRED — S18`
- **FR-038**: System MUST NOT provide any path to author, edit, or validate the community link value.
  *(S2 boundary)* — `DEFERRED — S18`

### Key Entities

- **Progress**: A Student's position and completion state for one Lesson. Identified by
  `(enrollment, lesson)`. Carries the maximum position reached, the last position, completion state,
  the instant of completion, and the Asset Version that completed the Lesson. Survives Entitlement
  expiry and revocation. Never an authorisation input.
- **Enrollment**: The Student's membership of a Course, which Progress hangs from. Survives expiry
  and revocation. **The table is defined by S5 and populated only by S6's grant transaction** —
  see [C1](#c1--s5-needs-the-enrollment-record-and-s6-creates-it).
- **Content Report**: A Student-submitted record naming a target (Course, Lesson, video, Resource, or
  Lab Material), a fixed reason, an explanation required for `other`, the reporting Student, the exact
  revision or version visible at submission, and the submission instant. Immutable once created; its
  resolution belongs to S8.
- **Entitlement** *(read-only here)*: Owned and evaluated by S4, created by S6. S5 reads the
  evaluator's verdict and never the record's mutability.
- **Course graph** *(read-only here)*: Course, Section, and Lesson as authored by S2. S5 renders it
  and writes none of it.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A Student with active access can start a Lesson and see video playing within 5 seconds
  on a typical connection, at every supported viewport.
- **SC-002**: 100% of playback issuances, protected download entry points, and Progress writes are
  authorised at request time; zero authorise from a cached or page-load decision. *Verification: an
  automated assertion across every S5 entry point, plus a mutation removing one revalidation that
  must produce a failing test.*
- **SC-003**: A Student returning to a Lesson resumes at their last position in 100% of cases, across
  device changes and browser restarts.
- **SC-004**: Completion is calculated from the trusted server-side duration in 100% of cases; no
  client-supplied percentage or duration can mark a Lesson complete. *Verification: integration test
  submitting falsified client values.*
- **SC-005**: Access ending — by expiry, revocation, Account suspension, or emergency Course access
  suspension — stops new playback authorisation and further Progress writes within one request, for
  sessions already open. *Verification: end-to-end test mutating each condition mid-session.*
- **SC-006**: Zero Entitlements are created, extended, or restored by any S5 path. *Verification:
  build-level assertion over the production build, matching S4's FR-017 and S6's equivalent.*
- **SC-007**: Completion and maximum position never regress across 100% of seek, retry, replay,
  reconnect, concurrent-device, and video-replacement scenarios exercised.
- **SC-008**: A transient Progress-write failure interrupts playback in 0% of otherwise-authorised
  sessions.
- **SC-009**: Every S5 screen passes automated WCAG 2.2 AA checks with zero violations for
  platform-owned controls, and the Lesson Player is fully operable by keyboard alone.
- **SC-010**: Every S5 screen renders correctly in Arabic RTL and English LTR at phone, tablet,
  laptop, and desktop viewports, with retained rendered evidence — the S2 T066 standard.
- **SC-011**: A reported item remains visible and unaltered in 100% of report submissions.
- **SC-012**: Every acceptance proof in this slice fails under a deliberate mutation of the control it
  claims to verify. A proof that survives its mutation is not evidence.

---

## Assumptions

Recorded because the feature description did not settle them and a reasonable default exists.
Anything without a reasonable default became a conflict in
[§Resolved Conflicts](#resolved-conflicts) instead.

- **S4's evaluator is consumed as an in-process interface inside the modular monolith**, not called
  over HTTP. Constitution Principle VI; S5 ships in the same deployable.
- **S4's Entitlement seed mechanism is how S5's tests obtain access.** S5 adds no seed path of its
  own, and inherits S4's FR-018 build- or environment-exclusion from production images.
- **Office hours are absent from ST05 and ST06** — not rendered as an empty or "coming soon" state.
  Deferred to S17 by the execution plan.
- **ST08 (Resources and Labs) is S4's screen.** S5 links to it. If S4 shipped only the endpoints and
  not the screen, that surfaces at plan time as an S4 carryover, not as new S5 scope.
- **ST10 Access History and the invitation panel on ST05 are S6's.** S5's dashboard work covers the
  learning half only.
- **The legacy `progress` table** (migration `0001`, keyed `UNIQUE(user_id, lesson_id)` with no
  Account foreign key) is migration input under D-031, cut over forward-only in this slice — the same
  treatment the legacy video path receives in S4. It is not a second Progress model to be preserved.
- **Progress is reported periodically during playback and on pause, seek, and unload**, at an interval
  chosen at plan time. FR-017's rate limit is sized against that interval.
- **A Course-level report target** is reachable from Course Home; Lesson, video, Resource, and Lab
  targets are reachable from the Lesson Player and material lists, per the ST-level Report Content
  modal ownership.
- **Per-Course progress** is completed Lessons over total Lessons in the qualifying graph — no
  weighting by duration, no partial credit.
- **No expiry reminder and no self-service renewal** are built. SJ-12 states this explicitly.

---

## Dependencies

| Depends on | What S5 needs from it | Status |
|---|---|---|
| **S2** — Course authoring | The approved Course graph, its lifecycle states, and emergency access suspension | **Active, not closed.** Its frozen queue is untouched by this slice |
| **S3** — Public catalogue and shell | The bilingual responsive shell, locale persistence, RTL/LTR | Specified and frozen; blocked on S2 |
| **S4** — Media and Entitlement evaluation | The Entitlement record and evaluator, signed playback issuance, trusted Asset Version duration, protected download endpoints, the non-production seed mechanism | Specified and frozen; blocked on S2 |
| **S6** — Course access grant | Nothing. **S5 now defines `enrollments` and S6 consumes it** | Inverted by [C1](#c1--s5-needs-the-enrollment-record-and-s6-creates-it); S6 planned and tasked, blocked on S2 and S4 |

**Downstream consumers.** S8's Instructor roster and analytics read the Progress this slice produces,
and S8's moderation queue resolves the reports it creates. Neither is built here, but both fix the
shape of what is.

**No launch gate blocks S5.** `LG-015` (accessibility) is validated in S13 against work built here.
`LG-014` (malware scanning) can leave assets quarantined, which S5 renders as content-unavailable
rather than failing.

---

## Constitution Alignment Note

This slice is governed most directly by **Principle II (Deny by Default, Enforce in the Backend)** and
**Principle IV (Access-Grant Correctness)** — the latter in its consuming direction. S5 is where
Principle IV's guarantees are either honoured or quietly bypassed, because it is the only slice that
renders paid content to the person paying for it.

**Principle V** requires a concurrency test on a grant path. S5 holds no grant path, but its
equivalent risk is concurrent Progress writes from multiple devices against one monotonic maximum, and
the same standard applies: idempotency and monotonicity never exercised concurrently are assumptions.
A concurrency test is required.

**Principle I** produced the two entries in [§Resolved Conflicts](#resolved-conflicts). Neither was
resolved on engineering authority: C1 changed S6's data model and C2 required a product-owner scope
decision recorded as D-046.
