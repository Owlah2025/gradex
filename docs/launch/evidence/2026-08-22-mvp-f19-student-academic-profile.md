# MVP-F19 / T3 — Student Academic Profile — completion evidence

**Recorded:** 2026-08-22
**Authority:** [D-092](../../DECISIONS.md#d-092--the-student-academic-profile-persists-academic-unit-context-for-program-less-states-and-records-onboarding-as-an-explicit-three-state-decision),
amending [D-091](../../DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy) §10,
under [D-089](../../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).
**Depends on:** [MVP-F17 / T1](2026-08-21-mvp-f17-academic-catalog-foundation.md), [MVP-F18 / T2](2026-08-21-mvp-f18-launch-catalog-data.md)
**Seat:** Claude held the **builder** seat by Founder reassignment. §10 is a
`BUILDER_SELF_AUDIT` and **is not independent review.** T3 stops here.

T3 is an **implementation tranche**. It creates no canonical denominator feature
row, promotes none, and the MVP score remains `37 / 53 = 69.8%`.

---

## 1. Canonical decisions

[D-092](../../DECISIONS.md) records two corrections to the approved design and
five standing rules. The corrections:

1. **Academic-unit context is persisted for Program-less states.** The design
   said College is a transient filter never stored. Kuwait University admits
   Students into a College before a sub-major is assigned, so an undeclared
   Student would have been indistinguishable from one who named only their
   university. The profile now carries an optional `academic_unit_id` for
   exactly that case — and, per §3, never duplicates the College an enrolled
   Student's Program already determines.
2. **Onboarding is an explicit three-state decision**, not a `completed_at`
   timestamp. `NOT_STARTED` / `SKIPPED` / `COMPLETED` are distinct because a
   product that cannot tell "hasn't decided" from "decided to defer" either
   nags a Student who already said no, or treats a deferral as completion.

## 2. Migration

`0024_student_academic_profile`, schema version **24**. Additive; referenced by
nothing that already exists.

```text
go test -tags=integration ./internal/db -run 'TestStudentAcademicProfile|TestAcademicCatalogMigration|TestMaxSchemaVersion'
  ok — 4.691s
```

- Clean install; `up → down → up`.
- The Academic Catalog, the legacy taxonomy, and every account and access table
  survive the rollback untouched.
- **Zero foreign keys** from the profile into `courses`, `course_revisions`,
  `entitlements`, `enrollments`, `course_access_invitations`,
  `purchase_requests`, or `progress` — and **zero** references *to* the profile
  from anywhere. That is the schema-level half of "discovery data never gates".
- One correction: `TestAcademicCatalogMigrationIsAdditiveAndReversible` rolled
  back "one step from the top", which 0024 silently redirected at the wrong
  migration. It now targets 0023 by version.

## 3. Profile model

| Field | Purpose |
|---|---|
| `account_id` PK | One profile per Account, enforced by the primary key so a concurrent upsert cannot create two |
| `setup_state` | `SKIPPED` \| `COMPLETED`. `NOT_STARTED` is the absence of a row and is deliberately not a stored value |
| `enrollment_status` | `ENROLLED` \| `UNDECLARED` \| `FOUNDATION` \| `NON_DEGREE` |
| `institution_id` | Composite FK pinning every academic reference to one Institution |
| `academic_unit_id` | The College, **only** for Program-less states |
| `program_id`, `curriculum_id` | Server-resolved; present only for `ENROLLED` |
| `current_level` | Optional |

Seven CHECK constraints plus a trigger refuse every incoherent shape: a SKIPPED
profile carrying data, a COMPLETED profile with no institution, an ENROLLED
profile with no Program or plan, a non-enrolled profile holding one, a redundant
College, a curriculum belonging to another Program, and a level with no
institution. All proved refused at the database level.

## 4. Onboarding state

- **`NOT_STARTED`** — no row. The dashboard shows a dismissible invitation card.
- **`SKIPPED`** — an explicit deferral through its own command, which clears every
  academic field. The Student is **not asked again**.
- **`COMPLETED`** — a saved profile. `SKIPPED → COMPLETED` is an ordinary later
  transition producing exactly one row.

## 5. Enrollment status

`ENROLLED` requires a Program and a server-resolved plan. `UNDECLARED` keeps the
College and holds no Program. `FOUNDATION` is accepted only where the
Institution's own data declares one — refused for Kuwait University, accepted for
a seeded institution that declares it, with no per-university special case.
`NON_DEGREE` holds no Program. **No placeholder Program row exists for any of
these**, asserted by test.

## 6. Curriculum resolution

Client-supplied curricula are **refused outright** — even a correct one — so the
contract cannot be silently ignored. On first save and on any Program change the
server resolves that Program's `ACTIVE` plan. **Editing only the level preserves
the plan the Student enrolled under**, proved by superseding a curriculum,
publishing a newer ACTIVE one, editing the level, and asserting the original
survives; changing Program then does resolve the new plan. A Program with no
ACTIVE curriculum fails as `PROGRAM_HAS_NO_ACTIVE_CURRICULUM`, never a 500, and
writes nothing.

## 7. APIs

| Route | Purpose |
|---|---|
| `GET /api/v1/me/academic-profile` | Own profile with resolved display names |
| `PUT /api/v1/me/academic-profile` | Save; server validates the whole tuple |
| `POST /api/v1/me/academic-profile/skip` | Explicit deferral |
| `GET /api/v1/me/academic-options/institutions` | Active institutions + level bounds |
| `GET .../institutions/:id/colleges` | Colleges only; a Department is never offered as one |
| `GET .../institutions/:id/programs?college_id=` | Programs via a recursive walk of the College subtree |

The recursive walk is what lets a Student skip the Department step: a Program may
hang off a Department (Kuwait University) or off a College directly, and both
resolve. Every route is `/me` and derives the account from the session — **there
is no account parameter and no bulk listing**.

## 8. Launch Program projection

The Student sees exactly the five launch Programs, from catalog data:
Computer Science · Cybersecurity · Data Science and Artificial Intelligence ·
Computer Engineering · Electrical Engineering. Asserted absent: Mathematics,
Financial Mathematics, Software Engineering, Cybersecurity Engineering, a
standalone "Data Science", Programming. **No Program list exists in the
frontend** — asserted structurally.

## 9. Gates

**Entitlement isolation — the D-092 §1 release gate.**

```text
go test -tags=integration ./internal/academic -run 'TestAcademicProfileMutationDoesNotAffectEntitlementEvaluation|TestCurriculumIsNeverAnAccessInput'
  ok — 8/8 green
```

`TestAcademicProfileMutationDoesNotAffectEntitlementEvaluation` gives a Student a
real entitlement granted through a real approved invitation, then drives the
**real production evaluator** (`Evaluate`, `EvaluateRead`, `EvaluateCourseReads`)
before and after: first completion, level change, Program change, becoming
undeclared, becoming non-degree, skipping, and finally deleting the profile
entirely. The decision, the reason, and the entitlement, enrollment, invitation,
purchase, and progress record counts are **identical every time**. The gate
asserts access is granted first, so it cannot pass vacuously.

`TestCurriculumIsNeverAnAccessInput` maps an unrelated Subject into the Student's
plan and proves a Course outside it is still accessible — the anti-regression for
"if Course Subject not in Student Curriculum then deny".

**Backend**

```text
go build ./...                              OK
go vet ./...                                OK
go test ./... -count=1                      27 packages ok
go test -tags=integration ./... -count=1    32 packages ok, exit 0
  internal/academic profile domain          21/21
  internal/httpapi profile HTTP surface     9/9
```

**Frontend** — typecheck PASS, **325 passed / 0 failed** (311 + 14 new).

**Full canonical Playwright**

```text
133 passed · 6 failed · 3 did not run · 8.6m
```

Configuration: local `backend/docker-compose.yml` stack (PostgreSQL 16, Redis 7,
MinIO, Mailpit), Playwright 1.62.0 + Chromium, **1 worker**, branch
`ui-antigravity-20260817` with its protected uncommitted working tree in place.

Baseline before T3 was `127 passed`. T3 adds its own 6 journeys → **133**. The
failure set is byte-for-byte the six known accepted identities, **no new failure
identity**:

```text
s5-expired-entitlement.spec.ts:712
s5-playback-performance.spec.ts:157
s5-viewport-evidence.spec.ts:223  (phone, tablet, laptop, desktop)
```

**Browser journeys (all green)**

- **A** — NOT_STARTED → dashboard card → onboarding → Kuwait University →
  College of Science → Computer Science → Level 2 → save → dashboard. Server
  re-query: `COMPLETED`, `ENROLLED`, plan `2024` resolved server-side, College
  derived. The card is gone afterwards.
- **B** — College of Life Sciences → Data Science and Artificial Intelligence →
  Level 1. The T2.2 Program arrives from the Academic Catalog API.
- **C** — Arabic. College of Engineering and Petroleum → `لم أحدد تخصصي بعد`.
  `UNDECLARED` with the College retained, no Program, no plan.
- **D** — `تخطي الآن` → `SKIPPED`, dashboard works, **not asked again on reload**,
  editing still available.
- **E** — an unprofiled Student reaches `/access`, the invitation and access-history
  APIs, the dashboard, and an entitled Course. Still `NOT_STARTED` afterwards:
  nothing redirected, consumed, or forced onboarding.
- **F** — an entitled Student opens a Course, completes a CS profile, switches to
  Data Science and AI, and the same Course is still on the dashboard and still
  opens. The on-screen promise is asserted to be the one the server keeps.

**Existing feature regression** — all inside the 133-pass run: ST-19, S6, F14,
ST-15, IN-09, Instructor authoring, Admin Course review, public catalogue, T1
Academic Catalog, T2 import. T3 changed no Instructor or Admin catalog authority.

## 10. Builder self-audit

`BUILDER_SELF_AUDIT` — not independent review. Mechanically verified: no
reference to the profile from `internal/entitlement`, `internal/learning`,
`internal/media`, or `internal/access`; nothing academic in session or JWT
claims; no middleware file and therefore no global onboarding route guard; no
hardcoded institution, College, Program, or level range in any Student surface;
the redundant-College CHECK present; zero foreign keys between the profile and
any access table in either direction.

## 11. Repository state

The protected uncommitted working tree was preserved throughout. No `reset`,
`stash`, `restore`, `clean`, or broad `checkout` was run.
