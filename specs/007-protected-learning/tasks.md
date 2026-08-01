# Tasks: S5 — Protected Learning

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Data model**: [data-model.md](data-model.md) | **Contracts**: [contracts/learning-api.md](contracts/learning-api.md) | **Quickstart**: [quickstart.md](quickstart.md)

**Date**: 2026-07-29 | **Review Tier 3.** **A builder never closes its own slice.**

**S2, S3, and S4 are closed** on independent verdicts. S5 is active and unblocked, but implementation
has not started: all tasks remain uncompleted and these approved planning artifacts stay frozen. S5
consumes S4's evaluator, signed issuance, trusted duration, and non-production seed; S2's Course
graph; S3's bilingual shell.

---

## Standing clause

> **A required dependency is validated at construction and the component refuses to build without it.
> No security-relevant control may silently degrade, default, or become optional.**

In S5 this clause forbids three constructions specifically:

- a handler that authorises from a **decision cached** at page load, session start, or a prior request;
- a Progress path that **creates an Enrollment** to make itself work;
- a denial that is **distinguishable** from non-existence.

Each would read as reasonable and each would ship a second access model.

**Tests are required, and every acceptance proof must fail under a deliberate mutation** (SC-012).
Eight instances of one defect class — *a control that reads as enforcement and enforces nothing* —
have appeared across S1C and S2. In every case the builder's own report said the work was clean.

## The boundary every task in this file respects

> **S5 owns physical schema introduction required by protected learning. S6 owns enrollment lifecycle
> semantics and production mutations.**

No task below implements invitation, approval, grant, rejection, revocation, or account-access
workflow. No task below creates an Enrollment **row** in a production path. No task below implements
a community link — FR-036 – FR-038 are `DEFERRED — S18` under
[D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch).

---

## Phase 1 — Setup

- [ ] T001 Read the highest existing migration number under `backend/internal/db/migrations/` and record the two numbers S5 takes. Expected `0013`/`0014`, but **derive, do not hardcode** — S3's `0011` may never exist ([research.md R-02](research.md#r-02--migration-numbering-and-the-s5s6-split)). Every later task's migration filename and `db.MaxSchemaVersion` value follows from this read
- [ ] T002 [P] Create `backend/internal/learning/doc.go` stating the boundary: consumes access decisions, creates none. **No `Enroll`, no `Grant`, no `Create`, no exported constructor for an Enrollment** (FR-005, FR-015a)
- [ ] T003 [P] Add `hls.js` to `frontend/package.json` and create `frontend/src/components/learning/` ([research.md R-07](research.md#r-07--player-hlsjs-with-native-fallback))

## Phase 2 — Foundational (blocking prerequisites for every user story)

**No user story can start until this phase completes.** The schema and the access boundary are what
every story is built on.

- [ ] T004 Add migration `0013_enrollments.{up,down}.sql` in `backend/internal/db/migrations/` creating `enrollments` with **exactly** `id`, `student_account_id` (FK → `accounts`), `course_id` (FK → `courses`), `created_at`, and `UNIQUE (student_account_id, course_id)` — the shape [S6 asserts](../006-course-access-grant/data-model.md) verbatim. **No `status`, `enrolled_via`, `entitlement_id`, `revoked_at`, or any lifecycle column** — each encodes a judgement S6 owns (BR-114, BR-167, Principle IV)
- [ ] T005 Add migration `0014_protected_learning.up.sql` step 1: **assert the legacy `progress` table is empty and `RAISE EXCEPTION` naming the row count if it is not**. Do **not** drop rows and do **not** synthesise an Enrollment to preserve them — that fabricates provenance for a grant that never happened (FR-018, FR-015a, Principle VII, [research.md R-01](research.md#r-01--the-legacy-progress-cutover-cannot-preserve-rows-and-must-not-synthesise-enrollments))
- [ ] T006 Add `0014_protected_learning.up.sql` step 2: drop the legacy `progress` table and create it at the BR-116 identity per [data-model.md](data-model.md) — `enrollment_id UUID NOT NULL` FK → `enrollments(id)` (**never nullable, never re-keyed later**), `lesson_id`, `max_position_seconds`, `last_position_seconds`, `completed_at`, `completing_asset_version_id`, `last_watched_at`, `updated_at`, with `UNIQUE (enrollment_id, lesson_id)`, the four `CHECK` constraints, and `idx_progress_enrollment` *(FR-015, BR-114, BR-116)*
- [ ] T007 Add `content_reports` to `0014_protected_learning.up.sql` per [data-model.md](data-model.md), including the closed `target_kind` and `reason` `CHECK` enumerations — **the fixed reason set exactly as specified; introduce no new reason** — plus `rep_other_needs_explanation` and the partial unique index `rep_no_duplicate_open` *(FR-029, FR-032, BR-145)*
- [ ] T008 Write `0013` and `0014` down migrations. `0014`'s down must run before `0013`'s — the Progress foreign key depends on `enrollments`
- [ ] T009 Raise `db.MaxSchemaVersion` to the value T001 derived (expected **14**) and confirm CI **derives** its assertion from that constant rather than hardcoding a literal — the convention `specs/004-public-catalogue/tasks.md` T035 sets
- [ ] T010 Integration test in `backend/internal/db/` — clean install (`0001`→`0014`), upgrade path (`0010`→`0014`), and the `up`/`down`/`up` round trip, all against **real PostgreSQL**. Assert `0013` applies before `0014` on both paths
- [ ] T011 [P] Integration test `TestEnrollmentsShapeMatchesS6Contract` in `backend/internal/db/` asserting the `enrollments` columns, types, nullability, and unique constraint match [S6's asserted contract](../006-course-access-grant/data-model.md) exactly. This is the cross-slice contract; a divergence found in August costs a rebase of two slices
- [ ] T012 [P] Integration test `TestLegacyProgressGuardRefusesNonEmptyTable` in `backend/internal/db/` — insert a legacy row, run the migration, assert it **aborts** naming the row count and preserves the row
- [ ] T013 Create `backend/internal/learning/repo.go` reading `enrollments` to resolve an `enrollment_id`. **It exposes no insert, update, or delete against that table** (FR-015a)
- [ ] T014 Create `backend/internal/httpapi/learning_foundation.go` with `NewLearningFoundation` and the `WithLearningFoundation` `RouterOption`, mirroring `catalog_foundation.go`. **Required dependencies validated at construction** — a foundation constructible without its entitlement evaluator is the standing clause's forbidden construction
- [ ] T015 Implement the **single uniform refusal constructor** in `backend/internal/httpapi/learning_handlers.go`. Every non-allow outcome — expired, revoked, out-of-scope, suspended, emergency-suspended, retired-ineligible, and target-not-found — maps through it to one byte-identical `404 application/problem+json`. **No handler writes its own refusal** (FR-003, BR-023, BR-050, [research.md R-05](research.md#r-05--denial-uniformity-and-the-one-place-it-is-decided))
- [ ] T016 Log and audit the **typed** denial reason internally while the external response stays uniform (Principle IX). The reason must not reach the body, status, header set, or timing
- [ ] T017 Add the Student-Account-only guard to every S5 route: an Instructor or Admin Account receives the **uniform refusal**, not a role-specific error (FR-007, BR-082)
- [ ] T018 [P] Unit test in `backend/internal/learning/` asserting no exported symbol in the package can create an Entitlement or an Enrollment row
- [ ] T019 Build-constraint test `TestProductionBuildHasNoEnrollmentCreationPath` asserting the **production build** contains no Entitlement-creating and no Enrollment-row-creating symbol, in the same shape as S4's seed-exclusion assertion (FR-005, FR-015a, SC-006)

**Checkpoint A — MANDATORY GATE. Blocks every user story.** T019 passes and T011 proves the shape.
No Progress work begins before creation is proven **impossible**, not merely absent.

**Checkpoint C — denial uniformity.** T015 and T016 land together. A typed reason that reaches the
response is the content inventory this slice exists to prevent.

---

## Phase 3 — User Story 1 (P1): a Student watches a Lesson and never loses their place

**Goal**: resume position and per-Lesson completion, server-authoritative, non-regressing.

**Independent test**: seed an active Entitlement via S4's non-production seed, insert an Enrollment
fixture directly, play to a position, terminate the session, re-open, assert resume position and
completion state **server-side**.

- [ ] T020 [P] [US1] Implement position bounding in `backend/internal/learning/progress.go` — clamp into `[0, trusted duration]`. A position beyond the duration or below zero is **clamped, not rejected**, so a bad tick does not lose the session (FR-011)
- [ ] T021 [P] [US1] Implement server-side completion in `backend/internal/learning/completion.go` — at least **90% of the trusted duration of the exact Media Asset Version played**. Any client-reported percentage or duration is **ignored**, not validated as a hint (FR-010, BR-051)
- [ ] T022 [US1] Implement the monotonic upsert in `backend/internal/learning/progress.go` using `ON CONFLICT (enrollment_id, lesson_id) DO UPDATE` with `GREATEST` for the maximum and `COALESCE` for `completed_at` and `completing_asset_version_id`. Monotonicity and write-once completion are **database semantics**, not application checks. `last_position_seconds` is deliberately **not** monotonic — it is the resume point. The upsert's conflict target **is** the BR-116 Progress identity (FR-012, FR-015, BR-114, BR-116, [data-model.md](data-model.md#the-monotonic-upsert))
- [ ] T023 [US1] Implement `PUT /api/v1/learn/lessons/{lessonId}/progress` in `backend/internal/httpapi/learning_handlers.go` in the contract's order: **revalidate access at request time**, resolve the Enrollment (read only), bound, compute completion, upsert (FR-014, [contracts/learning-api.md](contracts/learning-api.md#put-apiv1learnlessonslessonidprogress))
- [ ] T024 [US1] Add the Progress-write rate limit — **12 writes / min / (Student, Lesson)** — using `backend/internal/ratelimit`, sized against the 15 s reporting interval per FR-017 ([research.md R-04](research.md#r-04--progress-reporting-interval-and-rate-limit-sizing))
- [ ] T025 [US1] Implement `POST /api/v1/learn/lessons/{lessonId}/playback` requesting a freshly issued, short-lived, **session-scoped** signed URL from S4 per playback session. **Never cached, persisted, shared, or reused** across sessions or Students (FR-008, BR-100)
- [ ] T026 [US1] Add the playback issuance rate limit — 30 issuances / 10 min / Student plus a per-source-address ceiling (FR-017, BR-102)
- [ ] T027 [P] [US1] Build the Lesson Player at `frontend/src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx` with `hls.js` and native HLS fallback. **Server-rendered per request, no caching** of authorisation-bearing payloads
- [ ] T028 [P] [US1] Implement the client progress reporter in `frontend/src/components/learning/` — every **15 s**, plus pause, seek-settled, `visibilitychange`→hidden, and `pagehide` via `sendBeacon`. Use `pagehide`, **not** `unload`, which is unreliable on mobile Safari ([research.md R-04](research.md#r-04--progress-reporting-interval-and-rate-limit-sizing))
- [ ] T029 [US1] Implement retry-with-backoff on the client so a **transient Progress-write failure never interrupts** an otherwise-authorised playback session (FR-013, SC-008)
- [ ] T030 [P] [US1] Unit tests in `backend/internal/learning/` for bounding and the ≥90% completion calculation, including falsified client percentages and durations (FR-010, FR-011, SC-004)
- [ ] T031 [US1] Integration tests against **real PostgreSQL** in `backend/internal/learning/`: resume after session termination; completion at ≥90% writing `completed_at` **once**; no regression across seek-backwards, replay, retry, reconnection, and **Instructor video replacement** (FR-009, FR-012, BR-059, SC-003, SC-007)
- [ ] T032 [US1] **Concurrency test** `TestProgressConcurrentWritersPreserveMonotonicMaximum` — N concurrent writers at one `(enrollment, lesson)` with interleaved, out-of-order positions; assert the final maximum is the true maximum and `completed_at` was written **exactly once**. Run under `-race`. This is Constitution **Principle V**'s requirement discharged on S5's structural equivalent of a grant path ([research.md R-06](research.md#r-06--monotonicity-under-concurrency))
- [ ] T033 [US1] Security integration test: a **delayed, retried, duplicated, or out-of-order** Progress write arriving **after** access ended is **refused**. The happy path never exercises this (FR-014, BR-053)

**Checkpoint D — monotonicity under concurrency.** T032 passes. Sequential tests cannot see the
two-device race, and it is the one that loses a Student's progress in production.

---

## Phase 4 — User Story 2 (P1): access that has ended stops working immediately, everywhere

**Goal**: every denial cause is effective for an **already-open** session, and every denial is
indistinguishable from non-existence.

**Independent test**: start an authorised playback session, mutate the access condition server-side
mid-session, assert the next issuance, next segment authorisation, and next Progress write are all
denied **without the client cooperating**.

- [ ] T034 [US2] Wire every protected S5 handler to call `entitlement.Evaluate(student, lesson, now)` **in its own handler path, at request time** — no memoisation, no request-scoped cache, no session-scoped cache. **No handler compares an expiry, checks a revocation flag, or inspects scope itself** (FR-001, FR-005, Principle IV)
- [ ] T035 [US2] Ensure no S5 payload carries a token, capability, or decision the client could re-present. Payloads carry **rendering state only** ([plan.md](plan.md#the-cached-decision-failure-and-how-the-code-prevents-it))
- [ ] T036 [US2] Confirm the BR-027 retirement path: a qualifying Entitlement against a **delisted, retired, or archived** Course continues to authorise, subject to `retirement_eligibility_at` vs `retired_at`. This is delegated to S4's evaluator — assert S5 does not add its own comparison (FR-004, BR-050, BR-090)
- [ ] T037 [US2] Security integration test `TestDenialsAreByteIdentical` in `backend/internal/httpapi/` — all seven causes (expired, revoked, out-of-scope, suspended, emergency-suspended, retired-ineligible, **never-authored Lesson id**) return **byte-identical** responses. Compare the **full** response; asserting only the status code passes while leaking existence through the body (FR-003)
- [ ] T038 [US2] Production-router test `TestEveryProtectedLearningRouteRevalidates` in `backend/internal/httpapi/` — enumerate protected S5 routes from the **mounted production router** via `r.Routes()`, **not** a hand-maintained list, and prove each denies after a mid-session access mutation. A hand-maintained matrix is the S1C finding; a sweep that tests its own router is the S2 finding (FR-001, SC-002)
- [ ] T039 [US2] Extend `TestAuthorizationMatrixMatchesMountedRouter` in `backend/internal/httpapi/authorization_test.go` to cover every S5 route
- [ ] T040 [US2] Integration test: on **every** denial cause, **no** Entitlement, Enrollment, or Progress record is mutated as a side effect (FR-016, BR-026)
- [ ] T041 [US2] Integration test: a signed URL issued moments before access ended does **not** extend access beyond S4's token boundary, and **no new issuance** is granted (FR-002, BR-100)
- [ ] T042 [US2] End-to-end test `frontend/e2e/s5-access-ends.spec.ts` mutating each condition — expiry, revocation, Account suspension, emergency Course access suspension — **mid-session**, asserting the next issuance and next Progress write are denied (SC-005)
- [ ] T043 [US2] End-to-end test: an **expired** Entitlement shows retained Enrollment, retained Progress, and an expired state, while **nothing** is authorised from any of it. Progress is history, never an authorisation input (FR-016, BR-029)

**Checkpoint B — request-time revalidation. Blocks slice closure.** T038 passes **and** the SC-002
mutation holds: delete one revalidation call and a test must go red. A revalidation whose removal
breaks nothing was never load-bearing.

---

## Phase 5 — User Story 3 (P2): a Student navigates a Course and sees exactly their scope

**Goal**: Course Home, the Dashboard learning half, and the player shell — bilingual, responsive,
accessible.

**Independent test**: render Course Home for an entitled Student against a seeded multi-Section
Course; assert ordering, progress aggregation, expiry display, and RTL/LTR at representative
viewports.

- [ ] T044 [US3] Implement the qualifying Course-graph read in `backend/internal/learning/coursehome.go` — Sections and Lessons in **authored order** from the current approved or qualifying acquired graph (FR-019, BR-010, BR-017, BR-027)
- [ ] T045 [US3] Implement per-Course progress aggregation — completed Lessons over total Lessons in the qualifying graph. **No duration weighting, no partial credit** (spec §Assumptions)
- [ ] T046 [US3] Implement `GET /api/v1/learn/courses/{courseId}` per [contracts/learning-api.md](contracts/learning-api.md#get-apiv1learncoursescourseid). Every Section and Lesson of an entitled Course is in scope — Course scope is whole-Course (FR-020, BR-021, BR-024). **No community-link field** (D-046)
- [ ] T047 [US3] Implement `GET /api/v1/learn/courses/{courseId}/lessons/{lessonId}` returning Lesson metadata, outline rail, previous/next targets, resume position, and completion state. **Carries no signed URL and no token** (FR-024)
- [ ] T048 [US3] Implement `GET /api/v1/learn/dashboard` — Continue Learning and My Courses with per-Course progress and access state. **Reads no Course Access Invitation state**; ST10 Access History and the invitation panel are S6's (FR-023, FR-006, BR-029)
- [ ] T049 [US3] Implement the five distinct rendering states — active, expired, delisted-but-accessible, emergency-suspended, content-unavailable — per [plan.md](plan.md#fail-closed-rendering-states). A Lesson whose video is missing, quarantined, scan-failed, or transcode-failed is **reachable and marked unavailable** while the rest of the Course stays usable (FR-022, BR-090)
- [ ] T050 [P] [US3] Build Course Home at `frontend/src/app/[locale]/learn/courses/[courseId]/page.tsx`, server-rendered per request with **no caching** of authorisation-bearing payloads
- [ ] T051 [P] [US3] Build the Dashboard learning half at `frontend/src/app/[locale]/learn/page.tsx`
- [ ] T052 [P] [US3] Build the outline rail and previous/next navigation in `frontend/src/components/learning/`
- [ ] T053 [US3] Implement **custom platform-owned player controls** — play, pause, seek, volume, quality selection, fullscreen where available. Custom rather than native because native control sets are inconsistently labelled across browsers and cannot be audited by the automated WCAG check SC-009 requires (FR-024, [research.md R-07](research.md#r-07--player-hlsjs-with-native-fallback))
- [ ] T054 [US3] Make every platform-owned control keyboard-operable and screen-reader-labelled to **WCAG 2.2 AA**. **Do not claim complete product-level conformance** while captions are outside MVP (FR-025, `LG-015`)
- [ ] T055 [US3] Render every S5 screen in Arabic and English with correct RTL/LTR, **Arabic default**, persistent preference, via the existing `[locale]` segment and `frontend/src/lib/i18n`. **Instructor-authored content is never translated** (FR-026, BR-149, BR-150)
- [ ] T056 [US3] Display per-Lesson and per-Course progress and the **access-until instant**, rendered in the Student's locale (FR-021, BR-025, BR-052)
- [ ] T057 [US3] Link to **S4's** protected Resource and Lab Material download entry points without issuing, signing, proxying, or caching those links. ST08 is S4's screen (FR-028, BR-103, BR-143)
- [ ] T058 [US3] Integration tests in `backend/internal/learning/`: authored ordering, whole-Course scope, per-Course aggregation, and the five rendering states
- [ ] T059 [US3] Integration test: a Student holding Entitlements for two Courses sharing an Instructor sees **no cross-Course leakage** in either Course Home (spec §Edge Cases)
- [ ] T060 [P] [US3] End-to-end test `frontend/e2e/s5-course-home.spec.ts` — **Arabic RTL and English LTR** at phone, tablet, laptop, and desktop, with **retained rendered evidence** (the S2 T066 standard). Assert no Student capability is missing at any viewport (FR-027, SC-010)
- [ ] T061 [P] [US3] End-to-end test `frontend/e2e/s5-lesson-player.spec.ts` — same four viewports, both directions, plus **keyboard-only operation** and an **automated WCAG 2.2 AA scan with zero violations** for platform-owned controls (SC-009, SC-010)

---

## Phase 6 — User Story 4 (P3): a Student reports content

**Goal**: reports reach the Admin queue; nothing about the reported content changes.

**Independent test**: submit reports from an entitled and a non-entitled Student against each target
type; assert queue entry, rate limiting, duplicate refusal, and that the reported content stays
visible.

- [ ] T062 [US4] Implement report creation in `backend/internal/learning/report.go` recording **both** the stable logical target and the **exact visible** Course revision or Media Asset Version at submission (FR-030, BR-145)
- [ ] T063 [US4] Implement `POST /api/v1/learn/reports`, refusing a report from a Student holding **no Entitlement** for the target's Course via the uniform refusal (FR-033)
- [ ] T064 [US4] Add the report rate limit — 5/hour/Student — via `backend/internal/ratelimit`. This is **separate from** the duplicate constraint in T007; they fail differently and FR-032 requires both ([research.md R-11](research.md#r-11--reports-are-rate-limited-and-deduplicated-which-are-different-controls))
- [ ] T065 [US4] Ensure the acknowledgement reveals **nothing** about Admin queue state, other reports, queue position, or moderation outcomes (FR-034, BR-146)
- [ ] T066 [P] [US4] Build the Report Content modal in `frontend/src/components/learning/`, reachable from Course Home (Course target) and the Lesson Player and material lists (Lesson, video, Resource, Lab targets), in both locales and directions
- [ ] T067 [US4] Integration tests in `backend/internal/learning/`: each target kind; every reason in the fixed set accepted and any reason outside it refused; `other` without explanation refused **at the database constraint**; duplicate refused by the partial unique index; throttling; non-entitled refusal; acknowledgement disclosure *(FR-029, FR-032, FR-033, FR-034, BR-145)*
- [ ] T068 [US4] Integration test: reported content is **not** hidden, retired, altered, reordered, or marked as a consequence of a report (FR-031, SC-011)
- [ ] T069 [US4] Assert **no** S5 route resolves, dismisses, delists, retires, suspends, or otherwise moderates a report. Resolution is S8's (FR-035)

---

## Phase 7 — Polish, evidence, and convergence

- [ ] T070 [P] Run the **required mutations** table below. Each must turn a test red; restore after every one. Record the mutation and its failure (SC-012)
- [ ] T071 [P] Assert **D-046 absence** over the production build: no `community`, `discord`, or `telegram` match in `backend/internal/learning/`, the S5 migrations, `frontend/src/app/[locale]/learn/`, or `frontend/src/components/learning/`. No column, no payload field, no screen element, no placeholder, no "coming soon" state
- [ ] T072 [P] Assert **no office-hours** element renders on ST05 or ST06 — not even an empty or "coming soon" state. Deferred to S17 (spec §Assumptions)
- [ ] T073 [P] Verify structured logging and audit coverage for denial reasons and Progress-write failures, sufficient to reconstruct an incident without reproducing it (Principle IX)
- [ ] T074 Update `docs/BUSINESS_RULES.md`, `docs/DECISIONS.md`, or an affected contract **only if** implementation reveals a genuine conflict. Report the conflict; **do not implement around it** (Principle XI, Principle I)
- [ ] T075 Retain rendered RTL/LTR viewport evidence for every S5 screen at the S2 T066 standard, referenced from the daily record (SC-010)
- [ ] T076 **Time-to-first-frame evidence (SC-001).** Add `frontend/e2e/s5-playback-performance.spec.ts` measuring the interval from Lesson-Player navigation to the first rendered video frame (the player's first `timeupdate` past zero, or `video.getVideoPlaybackQuality().totalVideoFrames > 0` — not merely `loadedmetadata`, which precedes any visible frame). Assert **< 5 s at every one of the four supported viewports** — phone, tablet, laptop, desktop. **Do not weaken the 5-second threshold.** Determinism is a requirement of this task, not a nicety: drive a **documented, repeatable "typical connection" profile** via Chromium CDP `Network.emulateNetworkConditions` (record the exact latency, throughput, and profile name in the spec file), serve **local media fixtures** through the existing Playwright `webServer` at `127.0.0.1:3000`, and take no dependency on public-network conditions or a live CDN. Record the measured per-viewport figures and the test environment alongside the T075 rendered evidence, referenced from the daily record *(SC-001)*
- [ ] T077 Local gate: after the S5 implementation commit, `git diff --quiet` and `git diff --cached --quiet` pass; NUL-delimited final porcelain status is byte-identical to the recorded pre-implementation baseline; no untracked path was introduced; no baseline path disappeared, was staged, committed, ignored, or relocated; and the authorized S5 implementation commit is present in the range. The baseline is captured dynamically with `git status --porcelain=v1 -z --untracked-files=all`, so documented pre-existing user-owned residue remains visible and uncommitted. **T078 remains unchecked** pending independent review.
- [ ] T078 **Convergence — cannot be marked complete on local evidence.** Requires **hosted CI green on the exact head commit** (Backend, Frontend, migrations, integration, and Guards jobs) **and** a recorded **independent Tier 3 reviewer verdict** against one frozen commit range, with every critical and high finding resolved. A local green run does not satisfy this task. If the review produces no retrievable verdict, that is review `UNAVAILABLE`, **not** approval, and the slice does not close. **The builder never approves its own slice**

---

## Required mutations

Each must turn a test red. Restore after every one.

| # | Mutation | Must fail |
|---|---|---|
| 1 | Remove one `Evaluate` call from a protected handler | T038, Checkpoint B (SC-002) |
| 2 | Cache the access decision at page load and authorise the Progress write from it | T038, T042 |
| 3 | Replace `GREATEST` with the incoming value in the upsert | T031, T032 |
| 4 | Replace `COALESCE(progress.completed_at, …)` with the incoming value | T031 |
| 5 | Trust the client-reported watched percentage | T030 (SC-004) |
| 6 | Use the Lesson's nominal duration instead of the played Asset Version's trusted duration | T030 |
| 7 | Reject an out-of-range position instead of clamping it | T030 |
| 8 | Return `403` for revoked and `404` for non-existent | T037 |
| 9 | Include the typed denial reason in the response body | T037 |
| 10 | Skip revalidation on a delayed or duplicated Progress write | T033 |
| 11 | Remove the legacy-`progress` emptiness guard | T012 |
| 12 | Add an `INSERT INTO enrollments` to the Progress path | T018, T019 |
| 13 | Make `progress.enrollment_id` nullable | T010, T011 |
| 14 | Drop the `other`-requires-explanation constraint | T067 |
| 15 | Drop the report duplicate partial unique index | T067 |
| 16 | Add a `community_link` column to the S5 migration | T071 |

## Dependencies

| Phase | Blocked by |
|---|---|
| 1 — Setup | S2, S3, and S4 closing on independent verdicts |
| 2 — Foundational | Phase 1. T004 → T005 → T006 → T007 → T008 in order; T009–T012 follow the migrations |
| 3 — US1 | **Checkpoint A** — no Progress work before creation is proven impossible |
| 4 — US2 | Phase 2. Runs alongside Phase 3; T034 touches the same handlers as T023 and T025 |
| 5 — US3 | Phase 2. Independent of Phases 3–4 except that T053 shares the player shell with T027 |
| 6 — US4 | Phase 2 (T007's `content_reports` table) |
| 7 — Polish | Phases 3–6. **T078 additionally requires hosted CI and an independent reviewer** |

**Story independence.** US1, US3, and US4 are independently testable and can be verified in any order
once Phase 2 lands. US2 is a property **of** US1's and US3's endpoints rather than a separate surface,
so its tests attach to those routes — but Checkpoint B blocks slice closure regardless of story order.

## Parallel opportunities

- **Phase 1**: T002 and T003 (different languages, different trees)
- **Phase 2**: T011 and T012 after the migrations land; T018 alongside them
- **Phase 3**: T020 and T021 (different files); T027 and T028 (frontend) alongside T030
- **Phase 5**: T050, T051, T052 (distinct frontend files); T060 and T061 (distinct spec files)
- **Phase 7**: T070–T073 are mutually independent; T076 is independent of them and of T075

Backend and frontend work in Phases 3 and 5 parallelise cleanly — they touch disjoint trees and meet
only at the contract.

## Task count

**78 tasks.** Setup 3 · Foundational 16 · US1 14 · US2 10 · US3 18 · US4 8 · Polish 9.

**No task in this file may be marked complete by the planner.** Tasks are marked by the builder as
they land, and T078 is marked only against hosted CI and an independent reviewer verdict.

## MVP scope

Every task is required. **US1 plus US2 is the minimum coherent increment** — resume and completion
without enforced expiry is a paid platform giving content away, and enforcement without playback has
nothing to enforce. US3 makes them reachable by a real user; US4 (P3) is the only phase that could
slip a day without breaking the product, and FR-035 means slipping it removes a feature rather than
leaving a half-built one.
