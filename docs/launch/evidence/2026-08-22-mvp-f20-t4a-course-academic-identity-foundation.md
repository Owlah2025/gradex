# MVP-F20 / T4-A — Course Academic Identity Foundation

**Date:** 2026-08-22
**Tranche:** MVP-F20 (T4) sub-tranche A of five
**Status:** `T4-A PROVEN` — MVP-F20 remains `IN_PROGRESS`
**Authority:** [D-093](../../DECISIONS.md#d-093--course-academic-identity-is-an-explicit-classification-model-and-an-official-subject-code-is-permanently-reserved),
on top of D-091 §7–§9 and §13
**Design basis:** [T4 architecture — accepted basis](2026-08-22-mvp-f20-t4-architecture-proposal.md)

T4-A is a foundation slice: schema, domain rules, and proof. It adds no route, no UI, and no frontend
change. Ordinary Instructor Course creation still produces `LEGACY_TAXONOMY` Courses, deliberately —
the Academic path is reachable only by supplying academic context, which nothing in the product does
until T4-B.

---

## 1. Authority

[D-093](../../DECISIONS.md#d-093--course-academic-identity-is-an-explicit-classification-model-and-an-official-subject-code-is-permanently-reserved)
was recorded for this slice. It settles the three schema questions the architecture trace raised and
adds one Founder correction the trace had got wrong.

Two contradictions were corrected at source rather than left standing:

- The design report's T4 line called for deleting `taxonomy-assignment-panel.tsx` **and the
  Instructor taxonomy route**. There is no such separate route — the panel posts to
  `PATCH /courses/:id/revisions/:revisionId`, the same route carrying title and description — so the
  instruction would have deleted Instructor authoring. D-091 §13 already required retention until
  dual-path proof. The report line now records the correction.
- The trace proposed acknowledging retired-Subject code reuse and leaving it alone. The Founder
  rejected that; it is now D-093 §7 and is implemented here.

## 2. Migration 0025

`backend/internal/db/migrations/0025_course_academic_identity.{up,down}.sql`. One additive migration
carrying all T4 schema, so later slices activate behaviour without schema churn. No historical
migration was edited.

**Course academic identity**

```sql
CREATE TYPE course_classification_model AS ENUM ('LEGACY_TAXONOMY', 'ACADEMIC_CATALOG');

ALTER TABLE courses
    ADD COLUMN classification_model course_classification_model
        NOT NULL DEFAULT 'LEGACY_TAXONOMY',
    ADD COLUMN institution_id UUID REFERENCES institutions (id),
    ADD COLUMN subject_id     UUID;

ALTER TABLE courses ADD CONSTRAINT courses_id_institution_unique UNIQUE (id, institution_id);

ALTER TABLE courses
    ADD CONSTRAINT courses_subject_same_institution
        FOREIGN KEY (subject_id, institution_id) REFERENCES subjects (id, institution_id),
    ADD CONSTRAINT courses_academic_has_institution CHECK (
        classification_model <> 'ACADEMIC_CATALOG' OR institution_id IS NOT NULL),
    ADD CONSTRAINT courses_legacy_has_no_academic_identity CHECK (
        classification_model <> 'LEGACY_TAXONOMY'
        OR (institution_id IS NULL AND subject_id IS NULL));
```

The two CHECKs together make the hybrid state — Academic Subject plus legacy classification —
impossible to write. T5 must therefore flip classification and assign academic identity in one
statement, which is the intended atomic migration shape.

**Post-publication Subject immutability** — `BEFORE UPDATE OF subject_id ON courses`, raising when
`OLD.live_revision_id IS NOT NULL AND OLD.subject_id IS NOT NULL AND NEW.subject_id IS DISTINCT FROM
OLD.subject_id`. A CHECK cannot express this because it compares NEW to OLD. The guard fires on a
Course that already *has* a Subject, so assigning a first Subject to an already-published legacy
Course stays possible — precisely what T5 needs.

**Official Subject code permanence (D-093 §7)** — the one rule 0025 tightens:

```sql
DROP INDEX subjects_institution_code_unique;
CREATE UNIQUE INDEX subjects_institution_code_unique
    ON subjects (institution_id, code_normalized)
    WHERE code_normalized IS NOT NULL;   -- no longer AND retired_at IS NULL
```

Preceded by a preflight `DO` block that names every conflicting `(institution_id, code_normalized)`
group and raises `FOUNDER_DATA_RESOLUTION_REQUIRED` rather than letting the index creation fail with
an opaque duplicate key. It never deletes, merges, or rewrites a Subject.

**Future-slice tables** — `course_program_targets` and `subject_requests`, both `SCHEMA_PROVEN_ONLY`
(§8 below).

**Down** — drops only T4-A objects and restores 0023's index verbatim. This is a deliberate
asymmetry, documented in the file: rolling 0025 down **reopens** official-code reuse, because the
permanence rule is owned by 0025 and can only exist while 0025 is applied.

## 3. Preflight (§11 of the tranche brief)

Required before changing the code index: prove no `(institution_id, code_normalized)` pair is shared
by more than one Subject across active and retired rows.

- **Persistent databases discovered on this machine:** no such database holds Academic Catalog
  schema. `backend-postgres-1`, `compose-postgres-1` and `gradex-founder-acceptance-postgres-1` are
  at schema **20**; `gradex-s12-postgres-1` at **18**. `subjects` does not exist in any of them. Zero
  rows at risk.

  > **Clarification (added by T4-A.1).** The T4-A report's phrase "live database" meant exactly this:
  > the long-running PostgreSQL containers found on this development machine, whose data survives
  > between runs and could therefore have held Subject rows the tightened index would refuse. It did
  > **not** mean a deployed or provider-hosted database — Gradex is pre-launch and has none — and it
  > did not mean the ephemeral per-package test databases, which every integration suite drops and
  > recreates from migrations on each run and which do reach schema 25/26. There is no migration gap:
  > the persistent containers simply predate T1 and have never been migrated past 20.
- **Launch manifest** (the only Subject data that will ever be imported): 84 coded Subjects, 84
  distinct normalized codes, **zero duplicates**.

Result: **no conflicts**. `FOUNDER_DATA_RESOLUTION_REQUIRED` was not triggered. The preflight is also
retained as an executable guard in the migration itself, and is proven to fire — see §6.

## 4. Transition discriminator

Explicit, server-controlled, never client-supplied. `CreateCourseRequest` gains an optional
`Academic *AcademicCourseContext`; the server derives `ACADEMIC_CATALOG` from its presence and
`LEGACY_TAXONOMY` from its absence. There is no field on any request that names the model, so no
request shape can move a Course between models.

The `LEGACY_TAXONOMY` default is a transition device, not a product choice. It is what keeps
`s12-instructor-authoring.spec.ts` and `admin-catalog-surface.test.ts` green and lets T4-A ship
alone. **T4-B must close the path by which ordinary UI creation produces a legacy Course.**

## 5. Course Institution and Subject

`courses.institution_id` is required for an Academic Course and forbidden for a legacy one.
`courses.subject_id` is Course-level; `course_revisions.subject_id` was not created and does not
exist.

The invariant — *`institution_id` is the sole Institution authority; when `subject_id` is present it
belongs to that Institution* — is structural, not advisory: the composite FK makes disagreement
unwritable. This is the same device 0023 and 0024 already use throughout.

**Subject lifecycle** (`SetCourseSubject`): assignable and changeable while the Course has never
published and no candidate is in `PENDING_REVIEW`; frozen during review; editable again after the
Admin requests changes on a never-published Course; immutable once published. Eligibility at
assignment requires an active Subject in the Course's own Institution.

**Immutability is enforced twice** — in the domain from the row locked inside the transaction, and by
the database trigger. The trigger is what makes it a property of the data rather than of the handler,
and it is proven by direct SQL that bypasses the domain entirely.

**Retired historical Subject:** retirement bars a *first* publication only. A Course that has already
published keeps its Subject as identity, stays readable, and stays submittable for later content
revisions. Proven in `TestT4ARetiredHistoricalSubjectKeepsPublishedCourseOperational`.

## 6. Migration proof

`go test -tags=integration ./internal/db/`

```
--- PASS: TestCourseAcademicIdentityMigrationIsAdditiveAndReversible (1.14s)
--- PASS: TestCourseAcademicIdentityMigrationMakesSubjectCodesPermanent (0.99s)
--- PASS: TestCourseAcademicIdentityMigrationRefusesPreexistingCodeConflicts (0.93s)
--- PASS: TestMaxSchemaVersionTracksCourseAcademicIdentitySchema (0.00s)
ok      github.com/Owlah2025/gradex/backend/internal/db  32.192s   (full package)
```

Proven: clean install to 24 → seed a published legacy Course with full revision taxonomy → up to 25 →
**snapshot identical** → down to 24 → all T4 objects gone, T1/T2/T3 schema intact, **snapshot still
identical** → up to 25 again → **snapshot still identical**. The snapshot covers lifecycle, live
revision pointer, `major_term_id`, `subject_term_id`, `study_year`, revision count and taxonomy-term
count.

Code permanence proven end to end: duplicate active code refused → retire the holder → **same code
still refused** → same code in a different Institution allowed → stored `official_code` keeps its
dashed display form → codeless titles keep their unchanged 0023 rule (a retired codeless title
remains reusable).

The preflight is proven to fire: a database seeded with one live and one retired Subject sharing a
normalized code refuses the migration with `FOUNDER_DATA_RESOLUTION_REQUIRED`, and **both Subjects
still exist afterwards** — the failed migration changed no data.

## 7. Backend proof

| Gate | Result |
|---|---|
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `go vet -tags=integration ./...` | clean |
| `go test ./...` | all packages **ok**, 0 failures |
| `go test -tags=integration ./internal/catalog/` | **ok** 56.652s |
| `go test -tags=integration ./internal/db/` | **ok** 32.192s |
| `go test -tags=integration ./internal/catalogpublic/` | **ok** 3.091s |
| `go test -tags=integration ./internal/academic/...` | **ok** (academic 19.802s, importer 20.838s, manifest 0.134s) |
| `go test -tags=integration ./internal/access/` | **ok** |

Course identity cases, all passing: existing Course classified legacy and unchanged · default create
path stays legacy · Academic Course requires Institution · Academic Course allows NULL Subject in
draft · cross-Institution Subject refused at create, at assign, and by the FK · active Subject
assigns · retired Subject refused for new assignment · Subject change before publication allowed ·
Subject locked during `PENDING_REVIEW` · Subject editable again after Request Changes · first
publication locks Subject in the domain · direct SQL Subject change refused by the trigger · clearing
Subject on a published Course refused · retired historical Subject keeps the published Course
readable and revisable · Academic Course not required to populate legacy taxonomy · Academic Course
refused legacy taxonomy on both the Instructor and Admin write paths · legacy Course keeps FR-010 ·
non-owner cannot assign.

Subject code permanence, domain half
(`TestSubjectRetiredCodeStaysReserved`): a retired Subject's code stays reserved; the conflict
**names the retired holder** rather than failing opaquely; the reservation cannot be released by
clearing or changing the retired Subject's code; a retired Subject's titles stay editable.

> **Superseded in part by T4-A.1 (§15).** T4-A recorded here that "a live Subject's code stays
> editable". That was the remaining gap: an active Subject could renumber itself and free its old
> code. Under the amended D-093 §7 a live Subject's normalized code is equally immutable, and only
> display-formatting corrections that preserve the normalized form remain possible — on active and
> retired Subjects alike. This test was updated to assert the single canonical
> `ErrSubjectCodeImmutable`.

**T4-A test inventory — 19 new test functions, all passing**

| File | Functions | Kind |
|---|---|---|
| `catalog/t4a_academic_identity_integration_test.go` | 10 | Course identity, Subject lifecycle, immutability, coexistence, dual validation |
| `catalog/t4a_future_schema_integration_test.go` | 2 | `SCHEMA_PROVEN_ONLY` constraints (§8) |
| `catalog/academic_identity_test.go` | 2 | Domain invariants and the legacy-taxonomy guard (unit, table-driven) |
| `db/course_academic_identity_migration_integration_test.go` | 3 | Migration additivity/reversibility, code permanence, preflight refusal |
| `academic/subject_code_permanence_integration_test.go` | 1 | D-093 §7 domain half |
| `catalogpublic/t4a_academic_course_compatibility_integration_test.go` | 1 | Mixed-model publish, list, and search |

## 8. `SCHEMA_PROVEN_ONLY` — T4-C and T4-D tables

`course_program_targets` and `subject_requests` ship their schema in 0025 so the T4 shape is designed
once. **Their behaviour is not implemented and is not proven.** No audience inference, customization,
reset, cloning, or subset rule exists; no request flow, Admin queue, or resolution exists.

Constraints exercised: duplicate `(revision_id, program_id)` refused · cross-Institution Program
target refused · dangling Program refused · target whose revision belongs to another Course refused ·
no mode column exists, so zero rows is the only representation of the inferred audience · one PENDING
Subject request per Course · Course-less request allowed · request Institution must match its
Course's · `REJECTED` without a reason refused · `LINKED_EXISTING` without a resolved Subject refused
· resolved Subject from another Institution refused.

## 9. Legacy coexistence

Every legacy path is intact. `taxonomy_terms`, `major_term_id`, `subject_term_id`, `study_year`, the
Instructor panel, the shared revision route, and the Admin per-Course override all remain, and the
full legacy authoring/review suite is green.

The guards are server-side, not UI-only: `UpdateCourseRevision` and `AssignTaxonomyToRevision` both
refuse legacy taxonomy fields for an `ACADEMIC_CATALOG` Course, reading the classification from the
row already locked in the transaction. The refusal is scoped to the legacy fields — an ordinary
title edit on the same shared route still works.

Dual validation branches once, at `validateCourseForSubmission`. Because `SubmitCourse` and
`revalidateApproval` share that function, one branch covers both the submission gate and approval
revalidation. A nil Course row is treated as `LEGACY_TAXONOMY`, which is the pre-T4 behaviour exactly.

## 10. Public catalogue compatibility

No production change was required, as the trace predicted, and this is now observed rather than
inferred: `TestT4AAcademicCourseIsPublishableListableAndSearchable` publishes a legacy Course and an
Academic Course side by side, and proves both list, the Academic Course projects NULL legacy labels
rather than blanks, `q` search finds the Academic Course by title through the generated `search_text`
column, and the legacy Course remains findable through the joined-field half of the predicate.

No T6 work: no University, Program, or level filter, and no ranking.

## 11. Frontend

**No T4-A frontend production change.**

```
typecheck: clean (tsc --noEmit)
tests 325 · pass 325 · fail 0 · duration 2583ms
```

Exactly the recorded baseline.

## 11a. Full canonical Playwright regression

```text
133 passed · 6 failed · 3 did not run · 8.9m
```

Configuration: local `backend/docker-compose.yml` stack (PostgreSQL 16, Redis 7, MinIO, Mailpit),
Playwright + Chromium, **1 worker**, 142 tests collected, branch `ui-antigravity-20260817` with its
protected uncommitted working tree in place.

**Identical to the recorded baseline of 133 / 6 / 3.** T4-A adds no browser journey — it has no
product surface to exercise — so the passed count is unchanged by design. The failure set is
byte-for-byte the six accepted identities, with **no new failure identity**:

```text
s5-expired-entitlement.spec.ts:712
s5-playback-performance.spec.ts:157
s5-viewport-evidence.spec.ts:223  (phone, tablet, laptop, desktop)
```

Existing-flow proof inside that run, all green: `s12-instructor-authoring` (including case E, whose
`TAXONOMY_DIMENSION_MISSING` assertion is the legacy submit gate), `s14-admin-catalog-review`,
`s2-taxonomy-viewport`, `s3-public-catalogue`, `s6-course-access-grant-launch`,
`t1-admin-academic-catalog`, `t2-launch-catalog-data` (including case D, "importing changes nothing
about the legacy Course path"), and all six `t3-student-academic-profile` journeys.

## 15. T4-A.1 — Subject code identity hardening

**Status:** `T4-A.1 PROVEN`, 2026-08-22. A corrective micro-slice on top of T4-A, authorized after
Founder verification of one remaining identity path.

**The gap, confirmed before any change.** T4-A closed code reuse from two directions but left a third
open. Observed directly against a real database at schema 25:

```text
UPDATE subjects SET official_code='0418-999' WHERE ... -- UPDATE 1, code_normalized -> 0418999
INSERT INTO subjects (... official_code='0418-320' ...) -- succeeded
subjects_holding_0418320 = 1                            -- a different Subject now holds it
```

An **active** coded Subject could renumber itself and free its old normalized code for a different
Subject, which is academic renumbering performed through an ordinary Admin edit. D-093 §7 as written
was therefore incomplete.

**Canonical rule** (D-093 §7 amended): the normalized official code is canonical Subject identity.

| Transition | Result |
|---|---|
| `0418 320` → `0418-320` (same normalized form) | **allowed** — display formatting is not identity |
| `0418-320` → `0418-321` / `CS320` | **refused** — `ErrSubjectCodeImmutable` |
| `0418-320` → NULL | **refused** — identity cannot be withdrawn |
| codeless + active → first code | **allowed**, subject to reservation collision |
| codeless + retired → first code | **refused** — retirement freezes identity both ways |
| second code change after the first | **refused** — same rule as any coded Subject |

**Migration 0026** (`0026_subject_code_identity.{up,down}.sql`) — 0025 was already accepted and
proven, so the guard ships forward rather than by editing it. A `BEFORE UPDATE OF official_code`
trigger raising when `academic_normalize_code(NEW.official_code) IS DISTINCT FROM OLD.code_normalized`
on an established code.

The guard recomputes the normalized form rather than reading `NEW.code_normalized`. That is not a
stylistic choice: `code_normalized` is a STORED generated column, and PostgreSQL computes generated
columns **after** BEFORE-triggers run. Verified directly before relying on it:

```text
NOTICE:  OLD.norm=ABC NEW.norm=<NULL> computed_from_NEW.raw=XYZ
```

`academic_normalize_code` is STRICT, so a NULL code normalizes to NULL, which is `DISTINCT FROM` an
established code — that is the coded-to-NULL case, closed by the same expression. The trigger is
scoped to `UPDATE OF official_code`, so retirement, renaming and owning-unit edits never enter it.

**Domain and API.** `assertSubjectCodeIdentityPreserved` states the rule once in
`academic/subjects.go`, and `writeAcademicError` maps it to a `422` validation problem carrying
`SUBJECT_CODE_IMMUTABLE` — never a raw 500 from the constraint. T4-A's narrower
`ErrRetiredCodeImmutable` is replaced by `ErrSubjectCodeImmutable`: the retired case is now one
instance of the general rule rather than a separate concept, and T4-A's retired-code test was updated
to assert the single canonical error. That mapping also closes a T4-A gap — the retired-code refusal
had no HTTP mapping and would have surfaced as a 500.

**Results — all passing**

| Suite | Result |
|---|---|
| `academic` identity (5 new functions) | PASS |
| `academic` T4-A retired-code (updated) | PASS |
| `httpapi` Admin HTTP path | PASS |
| `db` migration 0026 up/down/up + launch-manifest codes | PASS |
| `go build` / `go vet` / `go vet -tags=integration` | clean |
| `go test ./...` | 27 packages ok, 0 failures |
| `academic` / `db` / `catalog` / `catalogpublic` integration | all ok |
| `httpapi` integration | ok, 0 failures |
| Frontend typecheck + unit | clean, 325 passed / 0 failed |

Proven: formatting-only edits succeed and `code_normalized` is unchanged · lower-case, dash, dot and
bare variants all preserve identity · renumbering refused in the domain, over HTTP, and by direct SQL
· coded → NULL refused at all three layers · a refused renumber leaves no partial mutation (a title
supplied in the same call does not land) · titles stay freely editable · retired Subjects stay
immutable · the old code never becomes available after any sequence of edit, clear, and retire
attempts · the same code in another Institution remains valid · codeless → first code works and then
becomes immutable · a first code colliding with a retired Subject's reservation is refused · the
launch codes all satisfy the tightened rule.

**Migration 0026 rollback** removes only the trigger, touches no row, and leaves 0025's reservation
index in force — proven by attempting to claim a reserved code after rolling down. As with 0025,
rolling down reopens the behaviour the migration exists to prevent; that is recorded in the file.

**Frontend:** no production change. The Admin catalog surface calls `createSubject` and
`retireSubject` and has no `updateSubject` caller, so the contract it depends on is unchanged. The
refusal lands on a path the UI does not currently reach.

**Full canonical Playwright was NOT re-run for T4-A.1.** This slice is backend catalog-identity
hardening: no route shape changed, no frontend production code changed, and no browser contract
changed. The T4-A run of `133 passed / 6 failed / 3 did not run` stands as the canonical baseline.
No new E2E feature is claimed.

## 12. Existing tests adjusted

Four pre-existing tests broke on schema facts that T4-A legitimately changed. **No assertion was
weakened**; each was retargeted at the property it was written to prove.

| Test | Why it broke | Correction |
|---|---|---|
| `TestAcademicCatalogMigrationIsAdditiveAndReversible` | Ran `m.Up()` to HEAD, then asserted `courses.subject_id` does not exist. 0025 adds it legitimately. | Migrate to `AcademicCatalogSchemaVersion` first, so the case tests 0023 — which is what it claims to test. |
| `TestStudentAcademicProfileMigrationIsAdditiveAndReversible` | Rolled back with `m.Steps(-1)` — "one step from the top". The top is now 0025, so it rolled back the wrong migration. | Version-targeted `m.Migrate(...)`, the technique the T1 test's own comment already documents as correct. |
| `TestAcademicCatalogImportAdminAPI/the legacy Course taxonomy path is untouched by import` | Asserted the *column* `courses.subject_id` does not exist. | Restated against the invariant it protects: the import must not give any Course an academic reference. Strictly stronger — it now checks data, not schema. |
| `TestPublicCatalogQueryPlansAtLaunchScale` | Pinned the plan to `courses_pkey`. With `enable_seqscan=off` the planner may now pick `courses_id_institution_unique`, the composite-FK target. | Accept either index. Both lead with `c.id`, so the property under test — indexed access, never a sequential scan — is unchanged. The execution-time budget assertion was untouched and still passes. |

Tests encoding legacy behaviour that must survive until T5 were **retained unchanged**:
`catalog/taxonomy_integration_test.go`, `httpapi/admin_taxonomy_*`, `e2e/s2-taxonomy-viewport.spec.ts`,
`e2e/s12-instructor-authoring.spec.ts` (including its `TAXONOMY_DIMENSION_MISSING` assertion), and
`admin-catalog-surface.test.ts`.

## 13. Scope held

Not implemented, and not claimed: Instructor Subject search projection or any UI (T4-B); audience
inference, customization, reset, or cloning (T4-C); Subject request workflow (T4-D); Admin review
academic context or Course Details presentation (T4-E); any legacy data migration or schema removal
(T5); any catalogue filter, ranking, or personalisation (T6).

The Student academic profile was not read by anything in this slice. No JWT claim changed.

## 14. Repository state

The protected dirty baseline was preserved with **one exception, recorded here in full.**

`backend/internal/httpapi/learning_resume_selection_integration_test.go` is an untracked file
belonging to the pre-existing dirty baseline, and it was not gofmt-formatted. While formatting the
files this slice edited, a package-wide `gofmt -w internal/httpapi/` reformatted it too. The change is
**formatting only** — gofmt is semantics-preserving, and the file's tests are part of the green
`internal/httpapi` integration run reported in §7. Because the file is untracked there is no git
object to restore from, so the original byte layout cannot be recovered; it is now gofmt-clean, which
also removes the `gofmt -l .` failure it was causing in `make ci`.

No other unrelated modified or untracked file was reverted, reformatted, or staged. No file was
committed.
