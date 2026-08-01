# Implementation Plan: S5 — Protected Learning

**Spec**: [spec.md](spec.md) | **Research**: [research.md](research.md) | **Data model**: [data-model.md](data-model.md) | **Contracts**: [contracts/learning-api.md](contracts/learning-api.md) | **Quickstart**: [quickstart.md](quickstart.md)

**Branch**: `007-protected-learning` | **Date**: 2026-07-29

**Review Tier 3.** **Blocked until S2, S3, and S4 close** on independent verdicts.

---

## Summary

S5 is the only slice that renders paid content to the person paying for it. It builds three learning
surfaces (Dashboard learning half, Course Home, Lesson Player), server-authoritative Progress keyed by
`(enrollment, course_lesson_identity)`, and Student content reporting — and it does all of it as a **consumer** of
access decisions S4 owns and S6 creates.

The technical approach is deliberately narrow. One new backend package (`internal/learning`), one new
frontend route group (`[locale]/learn/`), two migrations, and **zero** new access-decision logic. Every
playback issuance and every Progress write calls S4's single `Evaluate` function at request time; S5
adds no second evaluator, no cached decision, and no handler-local expiry comparison.

Two things make this slice Tier 3 rather than presentation work: the access boundary above, and the
fact that S5 is the **first** slice to define the `enrollments` table while being **forbidden** to
create a row in it.

## Technical Context

| | |
|---|---|
| Language | Go (backend, version from `backend/go.mod`); TypeScript / Next.js App Router (frontend) |
| New backend package | `backend/internal/learning` — Progress and content reporting |
| Consumes | `internal/entitlement` (evaluation), `internal/media` (signed issuance, trusted duration), `internal/catalog` (Course graph), `internal/ratelimit` |
| New frontend routes | `frontend/src/app/[locale]/learn/**` |
| Player | `hls.js` + native HLS fallback; custom platform-owned controls ([R-07](research.md#r-07--player-hlsjs-with-native-fallback)) |
| Storage | PostgreSQL. **Two** migrations — `0013_enrollments`, `0014_protected_learning` ([R-02](research.md#r-02--migration-numbering-and-the-s5s6-split)) |
| Schema constant | `db.MaxSchemaVersion` → **14**; CI **derives** its assertion from the constant |
| Testing | `go test -race`; `-tags=integration` against real PostgreSQL; Playwright for rendered RTL/LTR and viewport evidence |
| Target | Modular monolith, one deployable (Principle VI) |
| Scope | 3 screens, ~8 endpoints, 2 migrations, 1 new backend package |

**No NEEDS CLARIFICATION remains.** Every unknown the spec deferred to plan time is resolved in
[research.md](research.md).

## Constitution Check

*Gate evaluated before Phase 0 and re-evaluated after Phase 1 design. No violations; Complexity
Tracking is empty.*

| Principle | How S5 satisfies it | Verdict |
|---|---|---|
| **I — source documents authoritative** | Both conflicts were surfaced, not absorbed: C1 changed S6's data model, C2 became [D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch). Neither was resolved on engineering authority | **PASS** |
| **II — deny by default, enforce in backend** | Every denial cause returns one byte-identical refusal from a **single** response constructor ([R-05](research.md#r-05--denial-uniformity-and-the-one-place-it-is-decided)). Locked markers are labels; the server is the enforcement | **PASS** |
| **III — traceability** | Every FR cites its BR; every task cites the FRs it discharges | **PASS** |
| **IV — access-grant correctness (consuming direction)** | S5 creates no Entitlement and no Enrollment **row**. `internal/learning` exports no `Enroll`/`Grant`/`Create`; the prohibition is unavailable, not discouraged ([R-09](research.md#r-09--backend-package-placement)) | **PASS** |
| **V — testing commensurate with risk** | Tier 3. Principle V's concurrency requirement is discharged on the monotonic maximum, S5's structural equivalent of a grant path ([R-06](research.md#r-06--monotonicity-under-concurrency)) | **PASS** |
| **VI — modular monolith, simplicity** | One new package, one new route group. `hls.js` chosen over Video.js/Shaka precisely on this principle | **PASS** |
| **VII — data integrity** | Monotonicity and write-once completion are **database** semantics (`GREATEST`/`COALESCE`), not application checks. Uniqueness is a constraint. The one destructive operation carries a fail-loud guard ([R-01](research.md#r-01--the-legacy-progress-cutover-cannot-preserve-rows-and-must-not-synthesise-enrollments)) | **PASS** |
| **VIII — quality gate** | Existing CI jobs; no placeholders, no stubs, no swallowed errors | **PASS** |
| **IX — operational discipline** | Denial reasons typed and audited internally while externally uniform; transient Progress-write failure retried without interrupting playback (FR-013) | **PASS** |
| **X — responsive, bilingual, accessible** | Arabic default and RTL, four viewports, WCAG 2.2 AA on platform-owned controls — with the explicit refusal to claim product-level conformance while captions are out of MVP (FR-025) | **PASS** |
| **XI — documentation in sync** | S6's stale ownership claims are corrected as part of this planning pass | **PASS** |

---

## The enrollment ownership boundary

This is the sentence the slice turns on, and it is written here so no task has to infer it:

> **S5 owns physical schema introduction required by protected learning. S6 owns enrollment lifecycle
> semantics and production mutations.**

Stated as capabilities rather than prose:

| Capability | S5 | S6 |
|---|---|---|
| `CREATE TABLE enrollments` | **Yes** — migration `0013` | No. Asserts the inherited shape and fails loudly on divergence |
| Insert an Enrollment row in a production path | **No** | **Yes** — the grant transaction, and only it |
| Insert an Enrollment fixture in an integration test | **Yes** | Yes |
| Read an Enrollment to resolve `enrollment_id` | **Yes** | Yes |
| Invitation, approval, grant, rejection, revocation, account-access workflows | **No** — none of them | **Yes** — all of them |
| Update or delete an Enrollment row | No | No (nothing deletes one) |

**Why the table and not the lifecycle.** BR-116 fixes Progress identity as
`UNIQUE(enrollment_id, course_lesson_identity_id)` and BR-114 forbids Progress without an Enrollment. S5 writes
Progress on D5–D6; S6 runs on D8. Defining a table and writing to it are different capabilities, and
S5 takes only the first. This mirrors the S4/S6 split exactly: the consumer slice defines the record,
the producer slice populates it.

**The structural defence.** `enrollments` gets **no Go package and no constructor**. S5 resolves an
`enrollment_id` by reading; there is no code path in the S5 production surface capable of inserting
one. A test asserts this over the production build, in the same shape as S4's seed-exclusion
assertion — because a boundary that exists only in a plan document is a boundary the next contributor
does not know about.

## What S5 must not acquire

Restated because the temptation in each case is real and locally reasonable:

- **No Entitlement creation, extension, or restoration** by any route, command, screen, fixture,
  background job, or config flag (FR-005).
- **No Enrollment row creation** in any production path (FR-015a).
- **No invitation or approval workflow** — S5 never reads invitation state on a learning surface
  (FR-006).
- **No nullable `progress.enrollment_id`**, no temporary `(student, course, lesson)` key, and no
  planned re-key migration. The identity is BR-116's from the first migration that creates it.
- **No revision-row or legacy Lesson Progress key.** `course_lesson_identity_id` is durable; current
  metadata is read from the live `course_lessons` row, and exact Asset Version validation remains
  separate ([D-060](../../docs/DECISIONS.md#d-060--s5-progress-uses-stable-lesson-identities)).
- **No community-link field, payload, or screen element** — deferred to S18 under D-046. FR-036 –
  FR-038 receive no implementation task and their absence is asserted over the production build.
- **No Course authoring.** S2 owns Course content; S5 renders it and writes none of it.

## The cached-decision failure, and how the code prevents it

The specification names this as the likeliest defect in the slice: a player that fetches its access
decision once at page load, caches it in the UI, and authorises its own subsequent segment and
Progress requests against that cached answer. That is not an optimisation — it is a second access
model, and it survives expiry, revocation, Account suspension, and emergency Course access suspension.

**Three defences, matching the pattern S4 used for its seed path:**

1. **The client is never given an authorisation answer to cache.** Payloads carry rendering state
   (locked marker, expired badge) but no token, capability, or decision the client could re-present.
2. **Every S5 endpoint that touches protected content calls `Evaluate` in its own handler path**, at
   request time, with no memoisation, no request-scoped cache, and no session-scoped cache.
3. **A test asserts the property, not the instances**: every route in the mounted production router
   that reaches protected content is enumerated, and each is proven to deny after a mid-session access
   mutation. Defences 1 and 2 are design; defence 3 is what survives the next contributor.

SC-002 additionally requires a **mutation proof**: removing one revalidation must produce a failing
test. A revalidation whose removal breaks nothing was never load-bearing.

## Fail-closed rendering states

FR-022 requires distinct, non-misleading states. They are enumerated here because "handle the error
case" is where a slice quietly invents a sixth behaviour:

| State | Student sees | Server behaviour |
|---|---|---|
| Active access | Full player, progress, access-until instant | Issues signed URLs; accepts Progress |
| Expired access | Retained Progress, retained Enrollment, expired badge, no player | Denies issuance **and** Progress writes |
| Delisted / retired but accessible | Full player | Issues, subject to BR-027's `retirement_eligibility_at` vs `retired_at` comparison |
| Emergency-suspended | Unavailable state, no player | Denies (BR-090) |
| Content unavailable (never uploaded, quarantined, scan-failed, transcode-failed) | Lesson reachable, marked unavailable; rest of Course usable | No issuance attempted |
| Out of scope / non-existent | **Identical** refusal to non-existence | One response constructor ([R-05](research.md#r-05--denial-uniformity-and-the-one-place-it-is-decided)) |

The last row is the one that matters. The other five are presentation; that one is the content
inventory an attacker would otherwise enumerate.

## Project Structure

### Documentation (this feature)

```text
specs/007-protected-learning/
├── spec.md
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── learning-api.md  # Phase 1
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks output
```

### Source code

```text
backend/internal/learning/
├── doc.go                  # boundary: consumes access decisions, creates none.
│                           #   No Enroll, no Grant, no Create — see FR-015a
├── progress.go             # bounding, monotonic upsert, write-once completion
├── completion.go           # ≥90% of the TRUSTED duration of the exact Asset Version played
├── coursehome.go           # qualifying Course graph + per-Course aggregation
├── report.go               # content report creation; no resolution path (S8 owns that)
└── repo.go                 # reads enrollments; exposes no insert

backend/internal/httpapi/
├── learning_foundation.go  # WithLearningFoundation RouterOption (mirrors catalog_foundation.go)
└── learning_handlers.go    # every protected handler calls Evaluate at request time

backend/internal/db/migrations/
├── 0013_enrollments.{up,down}.sql          # the cross-slice contract S6 asserts
└── 0014_protected_learning.{up,down}.sql   # progress cutover + content_reports

frontend/src/app/[locale]/learn/
├── page.tsx                                        # Dashboard learning half (ST05)
├── courses/[courseId]/page.tsx                     # Course Home (ST06)
└── courses/[courseId]/lessons/[lessonId]/page.tsx  # Lesson Player (ST07)

frontend/src/components/learning/                   # player, outline rail, progress, report modal
frontend/e2e/                                       # s5-*.spec.ts — rendered RTL/LTR + viewport evidence
```

**Structure Decision.** Web application: existing `backend/` (Go modular monolith) and `frontend/`
(Next.js App Router). S5 adds one package to each side and mounts through the established
`RouterOption` pattern rather than introducing new infrastructure (Principle VI).

## Migration sequence

| # | Slice | Migration | Status |
|---|---|---|---|
| 0001–0010 | S1/S2 | through `0010_revision_integrity` | **Committed** |
| 0011 | S3 | `0011_catalog_search` | Planned, may not exist — S3 states it introduces no write path |
| 0012 | S4 | `0012_media_and_entitlement` | Planned |
| **0013** | **S5** | **`0013_enrollments`** | **This plan** |
| **0014** | **S5** | **`0014_protected_learning`** | **This plan** |
| 0015 | S6 | `NNNN_course_access_grant` | Number derived at implementation time |

**Numbers are derived, not hardcoded.** The implementation task reads the highest existing migration
number and takes the next two; the table above is the *expected* sequence. If S3 ships without a
migration, everything shifts down by one and `db.MaxSchemaVersion` follows from the same read. S6's
plan already records this reasoning for its own number.

**Ordering is load-bearing.** `0013` must precede `0014` because Progress's foreign key depends on the
`enrollments` table. Both clean-install (`0001`→`0014` in sequence) and upgrade (`0010`→`0014`) paths
are exercised against real PostgreSQL, including the `up`/`down`/`up` round trip the existing
migrations CI job already runs.

## Complexity Tracking

*No Constitution Check violation requires justification. Non-obvious choices are recorded below for
review, not as exceptions.*

| Choice | Simpler alternative | Why rejected |
|---|---|---|
| Two S5 migrations | One combined migration | The cross-slice contract stops being independently reviewable, and S6's shape assertion would point at a file half-irrelevant to it |
| Fail-loud guard on legacy `progress` | Unguarded `DROP TABLE` | Destructive with no safeguard (Principle VII). Correct today, silent tomorrow |
| `GREATEST`/`COALESCE` upsert | Read-modify-write in Go | Needs its own locking and loses two-device races — the one case every sequential test passes |
| Custom player controls | Native browser controls | Native control sets are inconsistently labelled across browsers and cannot be audited by the automated WCAG check SC-009 requires |
| `enrollments` with no Go package | A repository type with `Create` | A constructor that exists is a constructor that gets called. FR-015a must be unavailable, not discouraged |
| Reports rate-limited **and** deduplicated | Either alone | They fail differently: a limit permits 5 identical reports; a constraint permits 500 distinct ones. FR-032 requires both |

## Review checkpoints

| Checkpoint | Blocks | Evidence |
|---|---|---|
| **A — no creation path** | All Progress work | Production build asserted free of any Entitlement-creating and Enrollment-row-creating symbol |
| **B — request-time revalidation** | Player work | Every protected route in the **mounted production router** denies after mid-session access mutation; removing one revalidation fails a test (SC-002) |
| **C — denial uniformity** | Slice closure | All six denial causes **byte-identical**, including never-authored Lesson ids |
| **D — monotonicity under concurrency** | Slice closure | N concurrent writers at one `(enrollment, course_lesson_identity)`; final maximum correct, `completed_at` written once (Principle V) |
| **E — D-046 absence** | Slice closure | No community-link column, payload field, or screen element in the production build |
| **Final** | Slice closure | Clean tree, hosted CI green on the exact head, independent Tier 3 review verdict |

**Never self-approve.** S5 closes on a recorded independent reviewer verdict against one exact commit
range, with every critical and high finding resolved — not on the builder's assessment.
