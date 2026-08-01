# Phase 0 Research: S5 — Protected Learning

**Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-29

Every unknown the specification deferred to plan time is resolved here, against repository evidence
rather than assumption. Items the spec already settled are not re-opened.

---

## R-01 — The legacy `progress` cutover cannot preserve rows and must not synthesise Enrollments

**The question.** FR-018 requires cutting the legacy `progress` table over to the BR-116 identity as a
forward-only migration that "preserves existing rows". But the legacy table is keyed
`UNIQUE(user_id, lesson_id)` with **no foreign key on `user_id`**, and the BR-116 identity requires a
`NOT NULL` `enrollment_id` referencing a row S5 is forbidden to create (FR-015a). Preserving a legacy
row therefore appears to require synthesising an Enrollment, which is exactly the capability S5 does
not hold.

**What the repository actually shows.** This was checked, not inferred:

- `progress` is defined once, in `0001_init.up.sql`, and no migration through `0010` alters it.
- Its only writer is `backend/internal/video` (`playback.go`, `repo.go`, `service.go`) — the direct-to-asynq
  legacy path that **S4 retires forward-only under D-031** (S4 plan §The legacy cutover).
- Access for that path is decided by `backend/internal/auth/fake.go` against the `fake_entitlements`
  dev seam, also created in `0001` — not by any Entitlement record, which does not exist before S4.

So every row the legacy table can hold was produced by a fake-access dev seam against a retired
writer. There is no authentic Student progress in it, because before S4 there is no authentic access
path capable of producing any.

**Decision.** The cutover is forward-only and **carries no rows**, guarded by a fail-loud assertion
rather than a silent drop. Migration `0014` refuses to run if the legacy `progress` table is
non-empty, naming the row count and requiring an explicit operator decision.

**Rationale.** This is S4's own precedent applied unchanged: *"Authentic legacy state is preserved
through the cutover; fake access never becomes commercial provenance."* A guard that fails loudly
satisfies Principle VII's requirement that a destructive operation carry a safeguard, and it converts
the one scenario that would be dangerous — real rows appearing unexpectedly — from silent data loss
into a stopped migration. A blind `DROP` would be indistinguishable from correctness right up until
it was wrong.

**Alternatives considered.**

| Alternative | Why rejected |
|---|---|
| Synthesise an Enrollment per distinct legacy `user_id` | Violates FR-015a and Principle IV directly. It also fabricates provenance: an Enrollment implies a grant that never happened |
| Make `enrollment_id` nullable and backfill later | Violates BR-116's identity as written and defers a re-key migration into S6 — explicitly forbidden by the slice's locked decision |
| `DROP TABLE progress` with no guard | Destructive with no safeguard (Principle VII). Correct today, silent tomorrow |
| Keep the legacy table alongside the new one | Two Progress models. FR-018 requires no route writing the legacy shape afterwards |

**Consequence for FR-018.** "Preserves existing rows" is satisfied vacuously and *provably* — the
guard is the proof. The plan states this rather than letting a reviewer discover the table was empty
and wonder whether that was known.

---

## R-02 — Migration numbering and the S5/S6 split

**The question.** Which numbers does S5 take, and how many migrations?

**Repository state.** Highest committed migration is `0010_revision_integrity`. Unbuilt slices have
claimed: S3 → `0011_catalog_search` (`specs/004-public-catalogue/`), S4 → `0012_media_and_entitlement`
(`specs/005-media-and-entitlement-evaluation/`). Both are specified and frozen ahead of S5 in the
execution order, so S5's lane begins at `0013`.

**Decision.** S5 takes **two** migrations, split on ownership rather than on convenience:

| Migration | Contents | Why separate |
|---|---|---|
| `0013_enrollments` | The `enrollments` table alone | This is the **cross-slice contract**. S6 asserts this exact shape before writing. Isolating it gives that assertion one small, reviewable migration to point at, and makes S5's physical-schema ownership auditable in a single diff |
| `0014_protected_learning` | Legacy `progress` cutover to the BR-116 identity, plus `content_reports` | S5-private schema. Nothing outside S5 asserts against it before S8 |

`db.MaxSchemaVersion` rises to **14**, and CI **derives** its assertion from that constant — the
convention `specs/004-public-catalogue/tasks.md` T035 and `specs/005-media-and-entitlement-evaluation/tasks.md`
T013 already establish. S6 then takes `0015`.

**Rationale.** `0013` must land before `0014` because the Progress foreign key depends on it; two
migrations in one slice make that ordering explicit in the filename rather than implicit in statement
order inside one file. The split also means a reviewer checking "did S5 create an Enrollment row?" reads
one 20-line migration.

**Numbering is derived, not hardcoded.** S6's plan already records the reason (`plan.md` §migration
numbering): S3's specification states it "introduces no write path", so `0011` may never exist. The
implementation task therefore **reads the highest existing migration number and takes the next two**,
and the numbers above are the expected sequence, not a hardcoded constant. If S3 ships without a
migration, S5 becomes `0012`/`0013` and every dependent constant follows from the same read.

**Alternatives considered.** One combined migration (rejected: the cross-slice contract stops being
independently reviewable, and S6's shape assertion points at a file half of which is irrelevant to
it). Three migrations splitting `content_reports` out (rejected: no other slice asserts against it,
so the extra number buys nothing and costs S6 a further rebase).

---

## R-03 — The minimal `enrollments` shape

**The question.** What exactly does S5 create, given S6 owns the lifecycle?

**Decision.** Exactly the four columns and one constraint that
[`specs/006-course-access-grant/data-model.md` §5](../006-course-access-grant/data-model.md) already
declares it will assert:

`id UUID PK`, `student_account_id UUID NOT NULL` FK → `accounts(id)`, `course_id UUID NOT NULL`
FK → `courses(id)`, `created_at TIMESTAMPTZ NOT NULL`, and
`UNIQUE (student_account_id, course_id)`.

**Nothing else.** No `status`, no `enrolled_via`, no `entitlement_id`, no soft-delete column. Each of
those encodes a lifecycle judgement that belongs to S6, and a column S5 guesses at is a column S6 must
either honour or migrate away.

**Rationale.** The shape is not S5's design decision to make — it is a contract S6 has already
written down, and S5's job is to satisfy it exactly. Divergence is caught by S6's own assertion, but
catching it in August is far more expensive than reading §5 now.

**On the `UNIQUE` constraint specifically.** It is Principle IV's "no duplicate active access" as a
database constraint rather than an application check, and it is what keeps Progress single-homed under
BR-116's `UNIQUE(enrollment_id, lesson_id)`. S5 creating it is not S5 enforcing enrollment policy —
it is S5 declining to build a table that would let S6 violate its own invariant.

---

## R-04 — Progress reporting interval and rate-limit sizing

**The question.** The spec's Assumptions defer the reporting interval and FR-017's rate-limit sizing
to plan time.

**Decision.**

| Setting | Value | Basis |
|---|---|---|
| Periodic report interval during playback | **15 s** | Bounds worst-case lost position at 15 s, which is below the threshold at which a Student notices a resume as wrong |
| Additional report triggers | pause, seek-settled, `visibilitychange` → hidden, `pagehide` | Covers the closed-browser case in SC-003 without waiting for the next tick |
| Progress write rate limit | **12 writes / minute / (Student, Lesson)** | 4 ticks/min at 15 s plus headroom for pause/seek bursts. Sized against the interval, per FR-017 |
| Playback issuance rate limit | **30 issuances / 10 min / Student**, and a separate per-source-address ceiling | An issuance is per playback *session*, not per segment. 30 accommodates heavy Lesson-hopping; sustained excess is scripted extraction |

Both limits reuse `backend/internal/ratelimit` — the package the admission and session routes already
use — rather than introducing a second limiter.

**Rationale.** The interval and the limit are one decision, not two: FR-017 requires the limit be
sized against the interval, so choosing 15 s fixes the floor at 4/min and everything above it is
declared burst headroom. Stating the derivation means a later interval change has an obvious
corresponding limit change instead of silently pushing legitimate traffic into the limiter.

**On `pagehide` rather than `unload`.** `unload` is unreliable on mobile Safari and is being removed
from the platform; `visibilitychange` + `pagehide` with `sendBeacon` is the supported pairing. This
matters because the phone viewport is a first-class target under Principle X, not a degraded one.

---

## R-05 — Denial uniformity, and the one place it is decided

**The question.** FR-003 requires out-of-scope content to deny identically to non-existent content.
Where is that enforced?

**Decision.** S5 handlers call S4's `entitlement.Evaluate(student, lesson, now) → Decision` and map
**every** non-allow outcome — expired, revoked, out-of-scope, suspended, retired-ineligible, and
lesson-not-found — through a **single** response constructor to one byte-identical refusal. The typed
reason is logged and audited internally; it never reaches the response body, status nuance, header
set, or timing path.

S5 adds **no** second evaluator and **no** handler-local expiry comparison. This is S4's plan §"one
function, total over its inputs" consumed as written.

**Rationale.** The specification calls this the likeliest defect in the slice, and the two prior
findings it cites (S1C's hand-maintained matrix, S2's self-testing sweep) were both "the check exists
somewhere" without "the check is the only one". A single constructor is testable by construction: a
test can assert every denial path produces the identical response, which a scattered set of
`c.JSON(403, ...)` calls cannot support.

**Test consequence.** The refusal is asserted **byte-identical** across all six causes, including for
a Lesson id that was never authored. Asserting only the status code would pass while leaking existence
through the body.

---

## R-06 — Monotonicity under concurrency

**The question.** Constitution Principle V requires a concurrency test on a grant path. S5 holds no
grant path; the spec names concurrent Progress writes from multiple devices as the equivalent risk.

**Decision.** The monotonic maximum is enforced **in the database statement**, not in Go:

```
INSERT ... ON CONFLICT (enrollment_id, course_lesson_identity_id) DO UPDATE
  SET max_position_seconds = GREATEST(progress.max_position_seconds, EXCLUDED.max_position_seconds),
      last_position_seconds = EXCLUDED.last_position_seconds,
      completed_at = COALESCE(progress.completed_at, EXCLUDED.completed_at),
      completing_asset_version_id = COALESCE(progress.completing_asset_version_id, EXCLUDED.completing_asset_version_id)
```

`GREATEST` gives monotonicity and `COALESCE` gives write-once completion, both under the row lock the
upsert already takes. A read-modify-write in application code would need its own locking and would
lose races between two devices.

The proof is a real-PostgreSQL test driving **N concurrent writers** at one `(enrollment, lesson)` with
interleaved and out-of-order positions, asserting the final maximum equals the true maximum and
`completed_at` was written exactly once.

**Rationale.** FR-012 forbids regression across "seeks, retries, replays, reconnections, concurrent
devices, or video replacement". Only the concurrent-devices case can fail while every sequential test
passes, and it is the case a single-threaded test suite cannot see. Principle V's own words: idempotency
never exercised concurrently is an assumption.

---

## R-07 — Player: `hls.js` with native fallback

**The question.** The frontend needs adaptive HLS playback; the repository has no player today.

**Decision.** `hls.js` where Media Source Extensions are available, falling back to native HLS on
Safari/iOS where `canPlayType('application/vnd.apple.mpegurl')` succeeds. Platform-owned controls are
**custom React components**, not the browser's default control set.

**Rationale.** Native HLS exists only on Apple platforms; Chrome, Firefox, and Edge require MSE, so a
native-only approach loses the majority of the Gulf desktop and Android market. Custom controls are
not aesthetic preference — FR-025 requires every platform-owned control be keyboard-operable and
screen-reader-labelled to WCAG 2.2 AA, and native control sets are not consistently labelled across
browsers and cannot be audited by the automated accessibility check SC-009 requires.

**Alternatives considered.** Video.js and Shaka Player (rejected under Principle VI: both are
substantially larger dependencies whose extra capability — DRM, ad insertion, plugin ecosystems — S5
does not use, and Gradex explicitly claims deterrence rather than DRM). Native-only (rejected: loses
non-Safari browsers).

**Boundary held.** The player receives a signed URL from the S5 backend per playback session and
**never** caches, persists, or reuses it (FR-008). It re-requests on session start; it does not
refresh a URL it already holds into a longer-lived one.

---

## R-08 — Frontend route placement and rendering strategy

**The question.** Where do the learning surfaces live in the existing Next.js App Router tree?

**Repository state.** `frontend/src/app/[locale]/` currently holds `admin/` and `instructor/`. Locale
is a route segment; `frontend/src/lib/i18n` holds the translation and direction machinery; components
are grouped by domain under `frontend/src/components/`.

**Decision.**

```
frontend/src/app/[locale]/learn/                       # Student learning surfaces
├── page.tsx                                           # Dashboard: Continue Learning, My Courses
├── courses/[courseId]/page.tsx                        # Course Home (ST06)
└── courses/[courseId]/lessons/[lessonId]/page.tsx     # Lesson Player (ST07)

frontend/src/components/learning/                      # Player, outline rail, progress, report modal
```

All three are **server-rendered per request with no caching** of authorisation-bearing payloads.

**Rationale.** `learn/` sits parallel to `admin/` and `instructor/`, matching the audience-segment
convention already in the tree. No caching is not a performance oversight: a cached learning payload
is a cached access decision, which is precisely the FR-001 failure the slice exists to prevent. The
existing `[locale]` segment gives FR-026's RTL/LTR and persistence for free rather than S5 inventing a
second locale mechanism.

---

## R-09 — Backend package placement

**Decision.** One new package, `backend/internal/learning`, holding Progress and content reporting.
Route mounting follows the established `RouterOption` + `Foundation` pattern
(`WithLearningFoundation`), matching `WithCatalogFoundation` in
`backend/internal/httpapi/catalog_foundation.go`, with required dependencies validated at
construction.

**Rationale.** Principle VI. S5 needs one boundary — the thing that owns Progress — and it consumes
`internal/entitlement` and `internal/media` rather than reimplementing either. The `Foundation` pattern
is already the repository's answer to "mount a product boundary onto the production router", and
`TestAuthorizationMatrixMatchesMountedRouter` in `backend/internal/httpapi/authorization_test.go` is
the existing production-router evidence convention S5's route tests extend rather than replace.

**`enrollments` has no Go package.** S5 creates the table and reads it to resolve an
`enrollment_id`; it exposes no constructor, no `Create`, and no `Enroll`. This is S4's structural
defence #1 (`internal/entitlement` exports evaluation only) applied to the Enrollment record, and it
makes FR-015a's prohibition unavailable rather than merely discouraged.

---

## R-10 — Content report target integrity

**The question.** FR-030 requires each report to record both the stable logical target and the exact
visible revision or version.

**Decision.** `content_reports` carries a typed `target_kind` (`COURSE`, `LESSON`, `VIDEO`,
`RESOURCE`, `LAB_MATERIAL`), the logical `target_id`, and a nullable
`target_revision_ref` capturing the Course revision id or Media Asset Version id visible at
submission. `target_kind` is a closed `CHECK` enumeration widened only by migration — the convention
S6's `grant_source` constraint already sets.

**Rationale.** A report naming only the logical target is unactionable by the time S8 reads it, because
the content may have been revised in between; a report naming only the revision loses the ability to
group reports about the same Lesson. S8 needs both, and S8 is not built yet — which means S5 must get
the shape right without a consumer to correct it.

**Immutability.** No S5 route updates or deletes a report. Resolution is S8's (FR-035).

---

## R-11 — Reports are rate-limited *and* deduplicated, which are different controls

**Decision.** FR-032's "rate-limit and refuse duplicates" is implemented as two mechanisms:

- **Rate limit**: 5 reports / hour / Student, via `internal/ratelimit`.
- **Duplicate refusal**: a partial unique index on
  `(reporter_account_id, target_kind, target_id)` where the report is unresolved, so a second report
  about the same target from the same Student is refused by the database.

**Rationale.** A rate limit alone permits 5 identical reports about one Lesson; a duplicate constraint
alone permits 500 reports about 500 different Lessons. They fail differently and both are required by
FR-032 as written. Putting duplicate refusal in the database rather than a pre-check follows Principle
VII and survives concurrent submission.

---

## Resolved: no NEEDS CLARIFICATION remains

Every unknown the specification deferred to plan time is decided above. The two conflicts the
specification raised (C1 enrollment ownership, C2 the community link) were resolved by the developer
on 2026-07-29 and are consumed here as settled inputs, not re-opened:

- **C1** → S5 creates `enrollments` (R-02, R-03) and creates no row (R-09).
- **C2** → the community link is deferred to S18 under
  [D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch).
  FR-036 – FR-038 receive **no** implementation task, **no** column, **no** payload field, and **no**
  screen element. They are carried in the spec as `DEFERRED — S18` so S18 inherits reviewed
  requirements, and the plan asserts their absence from the production build rather than assuming it.
