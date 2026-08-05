# Data Model: S5 — Protected Learning

**Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Research**: [research.md](research.md)

S5 introduces two migrations. Numbers are **derived at implementation time** from the highest existing
migration, not hardcoded; `0013`/`0014` is the expected sequence ([R-02](research.md#r-02--migration-numbering-and-the-s5s6-split)).

| Table | Created by | This slice |
|---|---|---|
| `enrollments` | **S5** | **Created** — table only. No row is written by any S5 production path |
| `progress` | S1 (`0001`), re-identified here | **Cut over** to the BR-116 identity, forward-only |
| `content_reports` | **S5** | Created and written |
| `accounts`, `courses`, `sections`, `lessons` | S1/S2 | Read only. Not redefined |
| `entitlements`, `media_asset_versions` | S4 (`0012`) | Read only. Not redefined |

---

## Migration `0013_enrollments` — the cross-slice contract

Isolated in its own migration because **S6 asserts this exact shape** before writing to it. A reviewer
checking S5's enrollment boundary reads one small file.

### `enrollments`

The durable Student-to-Course learning relationship (DOMAIN_MODEL §4). Survives Entitlement expiry and
revocation (BR-026). **Never an authorisation input** (BR-029, FR-016).

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID PRIMARY KEY DEFAULT gen_random_uuid()` | |
| `student_account_id` | `UUID NOT NULL` | FK → `accounts(id)` |
| `course_id` | `UUID NOT NULL` | FK → `courses(id)` |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT now()` | First grant for this pair |

| Constraint | Rule | Enforces |
|---|---|---|
| `enr_one_per_student_course` | `UNIQUE (student_account_id, course_id)` | Principle IV's "no duplicate active access" as a **database constraint**, and it keeps Progress single-homed under BR-116's `UNIQUE(enrollment_id, course_lesson_identity_id)` identity |

| Index | Purpose |
|---|---|
| `enr_one_per_student_course` (the unique constraint) | Serves the S5 lookup `(student_account_id, course_id) → enrollment_id`, which is the only access pattern S5 has. **No additional index** — a `course_id` index serves S8's roster, and S8 adds it when S8 needs it |

**Exactly these four columns.** This matches
[`specs/006-course-access-grant/data-model.md` §5](../006-course-access-grant/data-model.md) verbatim,
which is the contract S6 asserts against.

**Deliberately absent** — every one of these encodes a lifecycle judgement S6 owns, and a column S5
guesses at is a column S6 must honour or migrate away:

`status`, `enrolled_via`, `entitlement_id`, `invitation_id`, `granted_by_account_id`, `revoked_at`,
`updated_at`, any soft-delete marker.

> **Migration ownership is not runtime ownership.** S5 issues the `CREATE TABLE`. S5's production code
> contains no `INSERT`, `UPDATE`, or `DELETE` against this table, exposes no constructor for it, and
> has no Go package representing it. S6's grant transaction is the only production writer, and it
> **reuses** an existing row rather than creating a second (BR-167).

**S5 reads it** to resolve `enrollment_id` for a Progress write. **S5 integration tests insert
fixtures directly** — permitted, because a fixture in a test binary is not a production path.

### `0013` down migration

`DROP TABLE enrollments`. Safe only because nothing references it yet at this point in the sequence —
`0014` adds the Progress foreign key, so `0014`'s down must run first. The existing migrations CI job
exercises the `up`/`down`/`up` round trip and will catch a violation of that ordering.

---

## Migration `0014_protected_learning`

### `progress` — cut over to the BR-116 identity

**The legacy shape** (`0001_init.up.sql`) is keyed `UNIQUE(user_id, lesson_id)` with **no foreign key
on `user_id`**. Its only writer is `internal/video`, the direct-to-asynq path **S4 retires under
D-031**, and its access decisions came from the `fake_entitlements` dev seam. It holds no authentic
Student progress, because before S4 no authentic access path exists that could produce any.

**The cutover is forward-only and carries no rows, behind a fail-loud guard**
([R-01](research.md#r-01--the-legacy-progress-cutover-cannot-preserve-rows-and-must-not-synthesise-enrollments)):

```text
1. Assert the legacy `progress` table is empty.
   If it is not, RAISE EXCEPTION naming the row count and abort the migration.
2. Drop the legacy table.
3. Create the BR-116-identified table below.
```

Step 1 is the safeguard Principle VII requires for a destructive operation. It converts the one
dangerous scenario — real rows appearing unexpectedly — from silent data loss into a stopped
migration that a human must resolve. **S5 does not synthesise an Enrollment to preserve a legacy row**;
that would fabricate provenance for a grant that never happened (FR-015a, Principle IV).

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID PRIMARY KEY DEFAULT gen_random_uuid()` | |
| `enrollment_id` | `UUID NOT NULL` | FK → `enrollments(id)`. **NOT NULL from creation** — never nullable, never re-keyed later (BR-114) |
| `course_lesson_identity_id` | `UUID NOT NULL` | FK → `course_lesson_identities(id)`. The durable Student-visible Lesson identity; current metadata is resolved through the live revision's `course_lessons` row. |
| `max_position_seconds` | `NUMERIC(10,3) NOT NULL DEFAULT 0` | Monotonic maximum |
| `last_position_seconds` | `NUMERIC(10,3) NOT NULL DEFAULT 0` | Resume point (BR-052) |
| `completed_at` | `TIMESTAMPTZ` | `NULL` until complete. Written **exactly once** (FR-012) |
| `completing_asset_version_id` | `UUID` | FK → media asset versions. The exact version that completed the Lesson (BR-059). Set with `completed_at`, never rewritten |
| `last_watched_at` | `TIMESTAMPTZ` | Server-assigned. No client-supplied time is trusted |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT now()` | |

| Constraint | Rule | Enforces |
|---|---|---|
| `prog_identity` | `UNIQUE (enrollment_id, course_lesson_identity_id)` | **BR-116's Progress identity.** The upsert conflict target |
| `prog_max_non_negative` | `CHECK (max_position_seconds >= 0)` | FR-011 bounding, at the database |
| `prog_last_non_negative` | `CHECK (last_position_seconds >= 0)` | FR-011 |
| `prog_max_ge_last` | `CHECK (max_position_seconds >= last_position_seconds)` | The maximum is a maximum. Carried forward from the legacy shape |
| `prog_completion_pair` | `CHECK ((completed_at IS NULL) = (completing_asset_version_id IS NULL))` | A completion without the version that caused it is unattributable (BR-059) |

| Index | Purpose |
|---|---|
| `prog_identity` (the unique constraint) | The upsert, and the per-Lesson read |
| `idx_progress_enrollment` on `(enrollment_id)` | Per-Course aggregation for Course Home and My Courses (FR-019, FR-021, FR-023) — the second real S5 access pattern |

No index on `course_lesson_identity_id` alone: S8's analytics will need one and S8 adds it. Speculative indexes cost
write throughput on the hottest table in the slice.

#### The monotonic upsert

Monotonicity and write-once completion are **database semantics**, not application checks
([R-06](research.md#r-06--monotonicity-under-concurrency)):

```sql
INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds,
                      completed_at, completing_asset_version_id, last_watched_at, updated_at)
VALUES (...)
ON CONFLICT (enrollment_id, course_lesson_identity_id) DO UPDATE SET
  max_position_seconds        = GREATEST(progress.max_position_seconds, EXCLUDED.max_position_seconds),
  last_position_seconds       = EXCLUDED.last_position_seconds,
  completed_at                = COALESCE(progress.completed_at, EXCLUDED.completed_at),
  completing_asset_version_id = COALESCE(progress.completing_asset_version_id,
                                         EXCLUDED.completing_asset_version_id),
  last_watched_at             = EXCLUDED.last_watched_at,
  updated_at                  = now();
```

`GREATEST` is FR-012's no-regression guarantee. `COALESCE` is FR-012's write-once completion. Both hold
under the row lock the upsert already takes, so two devices racing converge correctly without
application-level locking.

**`last_position_seconds` is deliberately not monotonic** — it is the resume point, and a Student who
seeks backwards and stops there should resume there. Only the *maximum* is monotonic, and only the
maximum feeds completion.

`course_lessons.id` is revision-owned and `lessons(id)` is legacy-only; neither is a durable Progress
key. An exact Asset Version is validated separately through S4 and remains supporting evidence, not
the logical Progress identity ([D-060](../../docs/DECISIONS.md#d-060--s5-progress-uses-stable-lesson-identities)).

### `content_reports`

Immutable once created. Resolution is S8's (FR-035); S5 has no route that updates or deletes one.

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID PRIMARY KEY DEFAULT gen_random_uuid()` | |
| `reporter_account_id` | `UUID NOT NULL` | FK → `accounts(id)`. Must be a Student Account (FR-007) |
| `target_kind` | `TEXT NOT NULL` | Closed `CHECK` enumeration — see below |
| `target_id` | `UUID NOT NULL` | The **stable logical** target (FR-030) |
| `target_revision_ref` | `UUID` | The **exact visible** Course revision or Media Asset Version the Student was shown (FR-030). Supplied by the encrypted report context minted at render time (D-065), never re-resolved from current content at submission |
| `reason` | `TEXT NOT NULL` | Closed `CHECK` enumeration — see below |
| `explanation` | `TEXT` | **Required when `reason = 'other'`** (FR-029) |
| `resolved_at` | `TIMESTAMPTZ` | Always `NULL` in S5. Present so S8's partial index target exists from creation. **Never published** — an unresolved state is the Admin-queue concern FR-034 forbids disclosing ([R-16](research.md#r-16--the-acknowledgement-is-an-allowlist-not-a-projection)) |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT now()` | Submission instant, server-assigned |

| Constraint | Rule | Enforces |
|---|---|---|
| `rep_target_kind` | `CHECK (target_kind IN ('COURSE','LESSON','VIDEO','RESOURCE','LAB_MATERIAL'))` | FR-029's fixed target set. Widened only by migration, so adding one is a reviewable diff — the convention S6's `grant_source` constraint sets |
| `rep_reason` | `CHECK (reason IN (...fixed set..., 'other'))` | FR-029's fixed reason set |
| `rep_other_needs_explanation` | `CHECK (reason <> 'other' OR (explanation IS NOT NULL AND length(btrim(explanation)) > 0))` | FR-029 at the database, so a handler bug cannot bypass it. Single-argument `btrim` strips **spaces**, so this refuses `NULL`, empty, and spaces-only; the domain's `strings.TrimSpace` strips all Unicode whitespace and refuses tab- or newline-only before any SQL runs. The pair is what FR-029 rests on, and both halves are asserted by T067 |
| `rep_no_duplicate_open` | `UNIQUE (reporter_account_id, target_kind, target_id) WHERE resolved_at IS NULL` | FR-032's duplicate refusal, enforced concurrently-safely rather than by a pre-check ([R-11](research.md#r-11--reports-are-rate-limited-and-deduplicated-which-are-different-controls)) |

Rate limiting (5/hour/Student) is a **separate** control in `internal/ratelimit`; the constraint above
does not replace it, because they fail differently ([R-11](research.md#r-11--reports-are-rate-limited-and-deduplicated-which-are-different-controls)).
The throttle holds **no** state in this table and reserves no row: its counters live only in Redis or
the bounded local fallback, and a throttled submission writes nothing here
([R-15](research.md#r-15--the-report-submission-throttle)).

The row is inserted in the **same transaction** as the current-Entitlement decision FR-033 requires,
so access that ends between authorisation and the write refuses the write
([R-14](research.md#r-14--report-submission-is-authorised-inside-its-own-insert-transaction)).
`created_at` comes from the injected server clock, never from the client and never from a wall-clock
read inside the insert.

Only `id` and `created_at` are ever published, and then only to the Student who created the row
([R-16](research.md#r-16--the-acknowledgement-is-an-allowlist-not-a-projection)). Every other column
— reporter, stable target, exact visible version reference, reason, explanation, and `resolved_at` —
is server-side, on every response class this route can return.

---

## Entities S5 reads and never writes

| Entity | Owner | S5's use |
|---|---|---|
| **Entitlement** | S4 evaluates, S6 creates | Read **only** through `entitlement.Evaluate(student, lesson, now) → Decision`. S5 never reads the record's fields to make its own judgement |
| **Course / Section / Lesson** | S2 | Rendered in authored order from the current approved or qualifying acquired graph (BR-010, BR-017, BR-027) |
| **Media Asset Version** | S4 | Trusted duration for the ≥90% completion calculation; the version id recorded on completion |
| **Course Access Invitation** | S6 | **Never read.** Not in an authorisation decision, not on a learning surface (FR-006, BR-029) |

---

## What is deliberately not in this data model

### Read-model material presentation (D-064)

Course Home Lesson entries and Lesson responses carry an always-present presentation-only
`materials` array. Its closed values are `resource` and `lab_material`, ordered in that sequence.
The values are read from S4's bounded current-live material discovery boundary; no Asset Version,
storage key, signed target, expiry, or capability state is stored in S5. Retained-expired and
unavailable read models use an empty array. Material activation remains an independently authorized
S4 operation.

- **No community-link column** anywhere. Deferred to S18 under
  [D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch).
  FR-036 – FR-038 get no column, no payload field, and no screen element, and the production build is
  asserted free of them.
- **No `progress.enrollment_id` nullability**, no temporary `(student, course, lesson)` key, and no
  planned re-key migration. The identity is BR-116's from the first migration that creates it.
- **No enrollment lifecycle columns.** See the "deliberately absent" list under `0013`.
- **No report resolution columns beyond `resolved_at`.** S8 adds actor, outcome, and notes when S8
  builds resolution. `resolved_at` exists only because the partial unique index needs its target from
  creation.
- **No second Progress model.** After `0014`, no route writes the legacy shape (FR-018).
