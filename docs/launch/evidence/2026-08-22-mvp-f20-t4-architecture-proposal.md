# T4 architecture — accepted basis

**Tranche:** MVP-F20 / T4 — Instructor Academic Context
**Date:** 2026-08-22
**Status:** `ACCEPTED 2026-08-22 with one Founder correction` — recorded canonically as
[D-093](../../DECISIONS.md#d-093--course-academic-identity-is-an-explicit-classification-model-and-an-official-subject-code-is-permanently-reserved)
**Authority consumed:** D-091 §7–§9, §13; D-092 §2–§3
**Pass type:** read-only architecture trace. The trace itself performed no production change; T4-A
implemented the accepted design separately.

This document records the repository evidence behind the three schema decisions that had to be
settled before migration 0025 was authored, so it was authored once. The trace facts below are
retained verbatim as the evidence they were. Where the Founder amended a conclusion, the amendment is
marked inline rather than by rewriting the finding.

> **Founder correction, §5 below.** The trace treated retired-Subject official-code reuse as a
> pre-existing T1 property to be acknowledged and left alone. The Founder rejected that: an official
> code, once used, is permanently reserved for its Subject within its Institution, and migration 0025
> hardens the index accordingly. See D-093 §7 and §9 of this document.

---

## 1. Evidence index

Every claim below is anchored to a repository fact observed in this pass.

| # | Fact | Evidence |
|---|---|---|
| E1 | Legacy taxonomy is **revision**-scoped, not Course-scoped | `0009_course_authoring.up.sql` — `major_term_id`, `subject_term_id`, `study_year` are columns of `course_revisions` |
| E2 | `courses` has no classification column and no immutable non-identity column | `courses` = `id`, `created_at`, `owner_account_id`, `lifecycle`, `live_revision_id`, `access_suspended_at`, `access_suspension_reason`, `retired_at`, `updated_at` (0009), `slug` (0011, generated from `id`), `default_access_ends_at` (0015) |
| E3 | A Course legitimately exists today with **no taxonomy at all** | `CreateCourse` (`authoring.go:323`) inserts `courses` + revision 1 with no taxonomy columns |
| E4 | Taxonomy is assigned later through the **shared** revision-update route | `UpdateCourseRevision` (`authoring.go:858`); frontend `assignInstructorTaxonomy` → `PATCH /courses/:id/revisions/:revisionId` (`lib/api/catalog.ts:284`) |
| E5 | There is **no dedicated Instructor taxonomy route** to delete | Same as E4 — the panel shares the route that also carries title/description |
| E6 | `live_revision_id` is monotonic: set only by `swapLiveRevision`, never nulled | `review.go:532`; no other `SET live_revision_id` exists in non-test code |
| E7 | "Has published history" is **already** a first-class projection | `review.go:118` — `(c.live_revision_id IS NULL) AS is_first_publish`, surfaced as `ReviewQueueItem.IsFirstPublish` |
| E8 | Submit and approval share one validation function | `SubmitCourse` and `revalidateApproval` both call `validateCourseForSubmission` (`validation.go:35`) |
| E9 | The legacy submit gate is one contiguous block | `validation.go:55–91`, labelled "1. Taxonomy dimension validation (FR-010)" |
| E10 | Public catalogue is already NULL-tolerant | `catalogpublic/repository.go` — `LEFT JOIN taxonomy_terms`, `CASE WHEN major.id IS NULL THEN NULL` |
| E11 | Public search does **not** depend on taxonomy | `search_text` is `GENERATED ALWAYS AS` over title/description only (`0011_catalog_search.up.sql:24`); the taxonomy join is an additional `OR` term |
| E12 | Subject search by code / normalized code / AR title / EN title **already exists** | `academic/subjects.go:335` `ListSubjects` |
| E13 | Subject search is Admin-only today | `GET /admin/academic/institutions/:id/subjects` behind `CapAcademicCatalog` (`academic_routes.go:57`) |
| E14 | A non-Admin read projection over the catalog is an established pattern | T3 `/me/academic-options/*` behind `CapLearningAccess` (`academic_routes.go:115–117`) |
| E15 | Instructors hold only `CapContentManagement` | `identity/policy.go:170–176` |
| E16 | Subject retirement is soft and preserves mappings | `RetireSubject` sets `retired_at`, counts and retains `curriculum_subjects` |
| E17 | Retiring a Subject **frees its code for reuse** | Subject unique indexes are partial: `WHERE ... retired_at IS NULL` (0023) |
| E18 | Composite-FK Institution pinning is the house pattern | `academic_units`, `programs`, `subjects`, `curricula`, `curriculum_subjects` (0023); `student_academic_profiles` (0024) |
| E19 | Revision-child tables carry `course_id` and use a composite FK to the revision | `course_sections` — `FOREIGN KEY (course_id, revision_id) REFERENCES course_revisions (course_id, id) ON DELETE CASCADE` (0010) |
| E20 | T3 solved the identical "required parent, optional child" shape | `student_academic_profiles` stores `institution_id` **and** `program_id` with composite FK, plus an explicit `setup_state` discriminator (0024) |
| E21 | `CreateCandidate` clones revision fields explicitly, field by field | `authoring.go:394` — a new child table needs an explicit clone step |
| E22 | T2 launch data contains **6** genuinely unmapped Subjects | `manifest.yaml`: 84 declared subject keys, 78 referenced by `curriculum_subjects`; unmapped = `ku-0418-301/335/365/390/466/470` |
| E23 | An existing E2E asserts the legacy submit gate by name | `s12-instructor-authoring.spec.ts:195` — `expect(failure).toContainText("TAXONOMY_DIMENSION_MISSING")` |
| E24 | An existing frontend unit test asserts the legacy panel file exists | `admin-catalog-surface.test.ts:315–316` reads `taxonomy-assignment-panel.tsx` and matches `assignInstructorTaxonomy` |
| E25 | The design report's T4 line contradicts D-091 §13 | Report §T4 says "deletion of `taxonomy-assignment-panel.tsx` and the Instructor taxonomy route"; D-091 §13 says legacy "remain operational and authoritative until the new catalog is proven on a dual path" |

---

## 2. Decision A — Course classification model

**Recommendation: Option A — an explicit `courses.classification_model` discriminator.**

Option B (infer from an existing immutable Course property) is **not available**. E2 enumerates every
`courses` column; none is both immutable and classification-bearing. `owner_account_id` is mutable
(`ReassignCourseOwner`), `lifecycle` is mutable, `slug` derives from `id`, and `created_at` is the
launch-date heuristic the tranche brief explicitly forbids.

Option C (infer from `courses.subject_id IS NULL`) is **refuted by E3**: a Course with no
classification data is the normal initial state today, and under T4 a subject-less Academic draft is
also legitimate. The two are indistinguishable by nullability.

Evaluation against the required cases:

| Case | Option A behaviour |
|---|---|
| New subject-less Academic draft | `ACADEMIC_CATALOG` + `subject_id IS NULL` — unambiguous |
| Legacy Course with incomplete taxonomy | `LEGACY_TAXONOMY` + NULL terms — unambiguous |
| Existing published legacy Course | Backfilled `LEGACY_TAXONOMY` by migration default; no row rewritten in substance |
| T5 migration | A single `UPDATE ... SET classification_model = 'ACADEMIC_CATALOG'` per migrated Course; `WHERE classification_model = 'LEGACY_TAXONOMY'` is the exact remaining-work query |
| Rollback | `DROP COLUMN` restores the pre-T4 shape; no legacy data was ever moved |
| Direct API mutation | Never accepted from a client payload; set by the server at create time only (see below) |
| Test fixtures | Existing fixtures get `LEGACY_TAXONOMY` by column default and stay green |
| Future legacy-schema deletion | The column becomes the guard that proves zero `LEGACY_TAXONOMY` rows remain before T5's destructive step |

**Forgery resistance (brief §73).** The discriminator must not be a request field. It is derived
server-side: `POST /courses` with an academic context block yields `ACADEMIC_CATALOG`; without it,
`LEGACY_TAXONOMY`. After creation it is immutable in normal authoring — no route accepts it, and T5's
migration command is the only writer. This mirrors how `lifecycle` is already handled: clients name a
*command*, never the target state directly.

**Default.** New rows default to `LEGACY_TAXONOMY`. This is the choice that keeps T4-A's blast radius
at zero (E23, E24 stay green), and T4-B flips the normal Instructor UI to always supply academic
context. The brief's §50 constraint — "new Course cannot be created in legacy mode through normal
UI" — is satisfied at the UI/API layer in T4-B, not by the column default.

---

## 3. Decision B — legacy compatibility sequencing

**Recommendation: retain the legacy path for `LEGACY_TAXONOMY` Courses; remove it in T5. The design
report's T4 deletion line is superseded.**

> **ACCEPTED — now D-093 §6.** The design report's T4 line has been corrected at source so this
> contradiction no longer stands in two places.

The brief's §22 suspicion is confirmed, and more strongly than expected:

1. **There is no separate route to delete (E5).** The Instructor taxonomy panel posts to
   `PATCH /courses/:id/revisions/:revisionId` — the same route carrying title and description.
   "Deleting the Instructor taxonomy route" would delete Instructor authoring.
2. **Canonical authority already says retain.** D-091 §13 requires legacy to remain operational
   "until the new catalog is proven on a dual path". The design report (E25) is a research artifact
   and ranks below the decision it fed. No new founder decision is required to correct this — D-091
   §13 already governs. This document records the contradiction; it does not resolve it by fiat.
3. **Deletion would strand every existing Course**, which has no `subject_id` until T5 runs.

**Server-side restriction (brief §21, §50).** The compatibility path must be gated in the domain, not
hidden in UI. The gate belongs in `UpdateCourseRevision` (`authoring.go:858`): when the locked
`CourseRow` is `ACADEMIC_CATALOG`, a non-nil `MajorTermID`, `SubjectTermID`, or `StudyYear` in the
request is rejected. The `CourseRow` is already loaded there via `LockCourse`, so this costs no extra
query. The same guard belongs in `AssignTaxonomyToRevision` (`taxonomy.go:227`, the Admin repair path)
for the same reason.

Corrected sequencing:

- **T4** — new Academic Courses use the new flow only; legacy Courses keep the compatibility flow;
  legacy fields become *unwritable* for Academic Courses.
- **T5** — migrate remaining legacy Courses; prove the new path; then remove the panel, the legacy
  branch of the revision-update route, the Admin repair route, and the old validation branch.
- **T5, final step** — drop `taxonomy_terms`, the three revision columns, and both enums.

---

## 4. Decision C — Course Institution model

**Recommendation: Option A — `courses.institution_id`, with `subject_id` pinned to it by composite
foreign key.**

The objection to Option A is redundancy: Subject already determines Institution. That objection
fails here for the reason D-092 §3 makes precise — redundancy is only harmful when two fields are
**independent authorities that can disagree**. Under the composite FK

```
FOREIGN KEY (subject_id, institution_id) REFERENCES subjects (id, institution_id)
```

disagreement is **unwritable**. There is one authority (`institution_id`); `subject_id` is
structurally constrained to it. This is precisely how T3 modelled the identical shape (E20) and how
0023 pins every catalog reference (E18).

Option B (Institution only on `subject_requests`) fails a real product flow: an Instructor who picks
Kuwait University, starts a draft, and raises no request has nowhere to persist the University.
Returning to the draft could not restore it — a regression against brief §46.

Option C (a separate academic-context entity) adds a table and a join to hold one column that the
Course itself must know. Rejected as unwarranted.

Flow validation:

| Flow | Behaviour |
|---|---|
| Normal create (KU + Subject) | Both written at insert; FK proves agreement |
| Subject missing | `institution_id` written, `subject_id` NULL; draft renders correctly |
| Request rejected | Course remains a meaningful KU Academic draft |
| Manual selection before Admin resolves | Cross-Institution Subject is rejected by the FK, not by application code |
| Future multi-university | A KU Course can never resolve to an AUK Subject |
| Published Course | Neither field drifts: `institution_id` is immutable once set, `subject_id` immutable after first publication |

**Invariant to document:** *For a Course, `institution_id` is the sole Institution authority.
`subject_id` may be NULL, but when present it belongs to `institution_id`, enforced structurally.*

**Coupling to Decision A:** `institution_id` is NOT NULL for `ACADEMIC_CATALOG` and NULL for
`LEGACY_TAXONOMY`, enforced by CHECK. That makes the two columns mutually consistent and gives T5 a
precise target state.

---

## 5. Subject immutability enforcement

**Recommendation: domain command + DB trigger. No new state.**

E6 and E7 settle the mechanism question: `live_revision_id IS NOT NULL` already *is* the canonical,
monotonic "has published history" fact, and the codebase already projects it as `is_first_publish`.
No `first_published_at` column is needed.

- **Domain**: `LockCourse` already returns `LiveRevisionID` inside the transaction. Any Subject
  mutation refuses when it is non-NULL.
- **Database**: a `BEFORE UPDATE OF subject_id ON courses` trigger raises when
  `OLD.live_revision_id IS NOT NULL AND NEW.subject_id IS DISTINCT FROM OLD.subject_id`. A CHECK
  cannot express this (it needs `OLD`), which is why a trigger is the minimum here — the same
  reasoning 0023 and 0024 already apply for their cross-row guards.

The trigger is what makes brief §36 ("no last-write race may change it") provable rather than
asserted, and it closes the API-mutable-but-UI-locked audit item.

**Retired historical Subject (brief §7).** Preserve operation. E16 shows retirement is soft and keeps
mappings; the FK from `courses.subject_id` stays valid. Eligibility is therefore checked **only on
assignment and on first publication**, never on later content revisions. This fits T1 semantics
unchanged.

> **RESOLVED BY FOUNDER DECISION — this is now D-093 §7, implemented in migration 0025.**
>
> *Trace finding, retained as written:* retiring a Subject frees its code for reuse, because the
> Subject unique indexes are partial on `retired_at IS NULL`. A published Course may therefore point
> at retired Subject `0418-320` while a *new* Subject holds the same code. This is a pre-existing T1
> property, not something T4 introduces, but T4 is the first tranche where a Course depends on it.
> The trace proposed no action.
>
> *Founder correction:* no action was the wrong call. A published Gradex Course keeps referencing the
> retired Subject, and a second Subject with the same Institution and code makes academic identity
> ambiguous with no temporal or supersession semantics to resolve it. Migration 0025 therefore widens
> `subjects_institution_code_unique` to span active **and** retired rows, and the domain refuses to
> change or clear a retired Subject's code — the index alone would leave a one-call bypass.
> **Extended by T4-A.1 (2026-08-22):** the same reasoning applies to an ACTIVE Subject renumbering
> itself, which T4-A left open. D-093 §7 was amended so the normalized code is immutable once
> established, on active and retired Subjects alike; migration 0026 enforces it in the database.
> Codeless Subjects keep their unchanged 0023 title-based rule: their identity is editable prose, so
> the same argument does not carry. Temporal Subject versioning is explicitly out of scope for MVP.

---

## 6. Derived audience and override

**Inference rule.** For Course Subject `S`: the Programs `P` such that `P`'s `ACTIVE`, non-retired
Curriculum has a `curriculum_subjects` row for `S`, with `P` non-retired and in the Course's
Institution. `curricula_one_active_per_program` (0023) guarantees at most one active plan per
Program, so the rule is single-valued.

**Zero rows = inference** (D-091 §8). An explicit empty audience must not be representable. With no
boolean mode flag, it is not: zero rows has exactly one meaning. Brief §11's warning against
"contradictory boolean + row states" is satisfied by adding no flag. Reset = delete the rows.

**Schema** (following E19 exactly):

```
course_program_targets (
    revision_id, course_id, program_id, institution_id, created_at
)
PRIMARY KEY (revision_id, program_id)                       -- duplicates impossible
FOREIGN KEY (course_id, revision_id)
    REFERENCES course_revisions (course_id, id) ON DELETE CASCADE   -- target belongs to Course
FOREIGN KEY (program_id, institution_id)
    REFERENCES programs (id, institution_id)                -- no cross-Institution target
```

The composite FKs make brief §44's integrity list structural rather than advisory. The subset rule
(target ∈ inferred audience) cannot be a constraint — it depends on curriculum rows — so it is
enforced in the domain at mutation, submit, and approval revalidation. E8 means submit and approval
share one code path, so the subset check is written once.

**Clone.** E21: `CreateCandidate` clones explicitly, so T4-C adds one `INSERT ... SELECT` for the
target rows. Absent override → nothing cloned → candidate stays inferred. Live revision is never
touched by a candidate edit because targets are keyed by `revision_id`.

---

## 7. Subject requests

**Recommendation: Flow 1 — Course first, request from within the Course.** Brief §25 requires
drafting to continue while a request is pending, which Flow 2 cannot express.

```
subject_requests (
    id, requester_account_id, institution_id,
    course_id            NULL,          -- explicit link, never title matching
    proposed_title_ar, proposed_title_en, proposed_official_code NULL,
    academic_context NULL, note NULL,
    status               PENDING | APPROVED_NEW | LINKED_EXISTING | REJECTED | CANCELLED,
    resolved_subject_id  NULL,
    resolution_reason    NULL,
    resolved_by_account_id NULL, resolved_at NULL,
    created_at, updated_at
)
FOREIGN KEY (course_id, institution_id) REFERENCES courses (id, institution_id)
FOREIGN KEY (resolved_subject_id, institution_id) REFERENCES subjects (id, institution_id)
CHECK (status <> 'REJECTED' OR resolution_reason IS NOT NULL)
CHECK (status IN ('APPROVED_NEW','LINKED_EXISTING')) = (resolved_subject_id IS NOT NULL)
UNIQUE INDEX (course_id) WHERE status = 'PENDING' AND course_id IS NOT NULL
```

Both composite FKs require `courses` to expose `UNIQUE (id, institution_id)` — cheap, and the same
device 0023 uses throughout.

- **Multiple sequential requests per Course:** yes, permitted.
- **Multiple simultaneous PENDING per Course:** prohibited, by the partial unique index. Two pending
  requests would make "which resolution wins" undecidable. Brief §11's principle applied to requests.
- **Course-less requests:** permitted (`course_id` NULL), per brief §30.
- **Authorization:** requester must own the Course, the Course must be `ACADEMIC_CATALOG`, must have
  `live_revision_id IS NULL`, and must be in the same Institution — the last enforced by FK.

**Race safety (brief §52).** Resolution must be compare-and-set, not blind write:

```sql
UPDATE courses SET subject_id = $resolved
WHERE id = $course AND subject_id IS NULL
  AND classification_model = 'ACADEMIC_CATALOG'
  AND live_revision_id IS NULL
```

Zero rows affected → the request resolves but reports a semantic conflict; the Course is left
untouched. This covers the "Instructor picked Subject A while Admin resolved to Subject B" case
without a silent overwrite. `LockCourse` provides the row lock; the predicate provides the decision.

**Duplicate safety (brief §28).** Approve-as-new must route through `academic.CreateSubject`, which
already carries T1 dedupe and whose partial unique indexes make a duplicate unwritable even under a
race. A conflict returns the existing Subject and the Admin is guided to *Link to existing*. No
`INSERT` may be issued from `subject_requests` code directly.

---

## 8. Dual validation

**Branch point: `validation.go:35`, `validateCourseForSubmission`.** E8 makes this one edit serve both
submit and approval revalidation. E9 shows the legacy gate is one contiguous block (lines 55–91) —
it moves under a branch rather than being rewritten.

`submissionValidationRequest` currently carries only `courseID` and `revision`. It gains the
`*CourseRow` (already loaded by both callers) so the branch reads `classification_model` from locked
state, never from a request:

```
LEGACY_TAXONOMY  → existing lines 55–91, unchanged
ACADEMIC_CATALOG → subject present; subject active on first publication;
                   targets active, in-Institution, ⊆ inferred audience;
                   no unresolved PENDING request when subject_id IS NULL
```

An Academic Course never reaches the legacy block, so it is never asked for `major_term_id`,
`subject_term_id`, or `study_year` — brief §19 and §43 satisfied by construction. A legacy Course
never reaches the new block, so it cannot be held to rules T5 has not yet prepared it for.

---

## 9. Public, search, and review compatibility

**Nothing is functionally blocked.** E10 and E11 are the decisive facts: the public projection is
NULL-tolerant by construction, and `search_text` is generated from title and description only. An
Academic Course with NULL legacy terms publishes, lists, and is searchable today with **zero**
changes to `catalogpublic`.

What remains is presentation, not function:

- **Course Details / cards** would render blank Major and Study Year. T4-E should render Subject code
  and title for Academic Courses instead — a projection change, no filter logic (T6 boundary held).
- **Search enrichment** (Subject code/title in the `OR` term) is optional and consistent with D-023.
  Recommended in T4-E, not required.
- **Admin review** (brief §30) needs the academic context block: University, Subject code, Subject
  title, effective audience (inferred or explicit). `ReviewQueueItem` and `GetCourseRevisionGraph`
  gain fields. No raw identifiers in rendered copy.

**Instructor Subject search** needs no new query — E12 shows `ListSubjects` already matches on code,
normalized code, and both titles. What T4-B adds is a **read projection for a non-Admin role**,
following the T3 precedent exactly (E14): routes under `CapContentManagement` (E15), never granting
`CapAcademicCatalog`, so D-091 §9 stays intact. Enrichment (owning unit, derived Programs) is a join
added at the projection, not a new search.

---

## 10. Proposed migration 0025 — one additive migration

> **IMPLEMENTED in T4-A.** The shipped migration follows this shape with one addition the Founder
> correction required: it also hardens `subjects_institution_code_unique` to span retired rows, and
> runs a preflight that fails with `FOUNDER_DATA_RESOLUTION_REQUIRED` — naming the conflicting rows —
> rather than tightening the index over conflicting data. See the T4-A evidence for the final SQL.

Every field below is justified; none is speculative. Behaviour ships later without schema churn.

```sql
CREATE TYPE course_classification_model AS ENUM ('LEGACY_TAXONOMY', 'ACADEMIC_CATALOG');
CREATE TYPE subject_request_status AS ENUM
    ('PENDING','APPROVED_NEW','LINKED_EXISTING','REJECTED','CANCELLED');

ALTER TABLE courses
    ADD COLUMN classification_model course_classification_model
        NOT NULL DEFAULT 'LEGACY_TAXONOMY',      -- Decision A; backfills every existing row
    ADD COLUMN institution_id UUID REFERENCES institutions (id),   -- Decision C
    ADD COLUMN subject_id     UUID,                                -- D-091 §7, Course-level
    ADD CONSTRAINT courses_id_institution_unique UNIQUE (id, institution_id),
    ADD CONSTRAINT courses_subject_same_institution
        FOREIGN KEY (subject_id, institution_id) REFERENCES subjects (id, institution_id),
    ADD CONSTRAINT courses_academic_has_institution CHECK (
        classification_model <> 'ACADEMIC_CATALOG' OR institution_id IS NOT NULL),
    ADD CONSTRAINT courses_legacy_has_no_academic_context CHECK (
        classification_model <> 'LEGACY_TAXONOMY'
        OR (institution_id IS NULL AND subject_id IS NULL));
```

The second CHECK is what makes brief §51's "no hybrid invalid Course" a database property.

`course_program_targets` and `subject_requests` as specified in §6 and §7 above, plus:

```sql
CREATE INDEX courses_institution_subject_idx ON courses (institution_id, subject_id)
    WHERE classification_model = 'ACADEMIC_CATALOG';
CREATE INDEX course_program_targets_program_idx ON course_program_targets (program_id);
CREATE INDEX subject_requests_pending_idx ON subject_requests (institution_id, created_at)
    WHERE status = 'PENDING';

CREATE FUNCTION courses_reject_published_subject_change() RETURNS TRIGGER ...   -- §5
CREATE TRIGGER courses_subject_immutability_guard
    BEFORE UPDATE OF subject_id ON courses FOR EACH ROW ...;
```

**Field justification and removability**

| Field | Why it exists | Removable later? |
|---|---|---|
| `classification_model` | Decision A; nullability cannot distinguish the two models (E3) | Yes — after T5 proves zero `LEGACY_TAXONOMY` rows |
| `institution_id` | Decision C; required before Subject exists | No — permanent academic identity |
| `subject_id` | D-091 §7 | No — permanent |
| `course_program_targets` | D-091 §8, revision-scoped audience | No |
| `subject_requests` | D-091 §9, Instructor cannot create Subjects | No |
| Immutability trigger | Brief §36 — must not be UI-only | No |

**Not touched:** `taxonomy_terms`, `major_term_id`, `subject_term_id`, `study_year`, both legacy enums.
**Down migration:** drops only T4-owned objects. Because no legacy row is read or rewritten, rollback
restores the pre-T4 database exactly — the property 0023 and 0024 both hold.

---

## 11. T5 compatibility

The proposed shape makes T5 a filtered loop rather than an inference exercise:

1. **Inspect** — `WHERE classification_model = 'LEGACY_TAXONOMY'` is the exact remaining-work query.
2. **Map Subjects** — legacy `subject_term_id` → canonical `subjects` via `academic_normalize_code`
   on `taxonomy_terms.academic_code`, reusing the T1 primitive rather than forking matching.
3. **Map Majors → Programs** — legacy MAJOR terms are Course-level intent; they become
   `course_program_targets` on the live revision where a founder mapping file supplies the pairing.
4. **Assign** — set `institution_id` then `subject_id`; the composite FK proves coherence per row.
5. **Switch** — flip `classification_model`; the CHECK constraints refuse an incoherent result, so a
   half-migrated Course cannot be committed.
6. **Prove** — dual path: both models coexist and are independently green throughout.
7. **Remove** — when the T4-owned count of `LEGACY_TAXONOMY` reaches zero, drop the compatibility
   surfaces, then the legacy schema.

Step 5 failing closed is the property that makes T5 safe to run incrementally.

---

## 12. Proposed sub-tranches

| Slice | Scope | Migration | Rollback risk | Stop condition |
|---|---|---|---|---|
| **T4-A** | Migration 0025 (all T4 schema); `CourseRow` fields; discriminator derivation; Institution/Subject lifecycle; immutability trigger; dual-validation branch; legacy write-guard for Academic Courses | **Owns 0025** | **Low** — no existing row read or rewritten; default keeps every fixture legacy | Migration up/down/up proof; backend tests 1–13, 41–47; full `go test ./...` green |
| **T4-B** | Instructor academic read projection under `CapContentManagement`; Subject search + context; new create flow; derived-audience display; unmapped-Subject state; legacy panel made conditional | none | **Medium** — touches the authoring write path S2 E2E covers | Journeys A + F; `s12` rewritten; frontend suite + typecheck |
| **T4-C** | `course_program_targets` behaviour: infer, customize, reset; clone in `CreateCandidate`; subset enforcement at mutation/submit/approval | none (schema in 0025) | Medium | Backend tests 14–24; Journeys B + C |
| **T4-D** | Subject requests: Instructor flow, Admin queue, link-existing, approve-new, reject, compare-and-set race safety | none (schema in 0025) | Medium | Backend tests 25–40; Journeys D + E |
| **T4-E** | Admin review academic context; Course Details presentation; optional search enrichment; full canonical regression | none | Low | Journey G; full Playwright vs the 133/6/3 baseline |

One migration, four behaviour slices. T4-A is the only slice that can strand the others, which is why
it ships alone and is proven against real Postgres before any UI exists.

---

## 13. Existing tests affected

**Extend (behaviour preserved, new cases added)**
`catalog/validation` callers; `catalog/lifecycle_integration_test.go`;
`httpapi/review_integration_test.go`; `httpapi/catalog_public_integration_test.go`;
`db/migrate_integration_test.go`; `db/academic_catalog_migration_integration_test.go`.

**Rewrite (T4-B, not earlier)**
`e2e/s12-instructor-authoring.spec.ts` — E23 asserts `TAXONOMY_DIMENSION_MISSING` by name. It stays
green through T4-A because new Courses still default to legacy; it is rewritten only when the flow
actually changes.
`frontend/src/components/admin/admin-catalog-surface.test.ts` — E24 asserts the panel file exists.

**Retain unchanged until T5 (these encode behaviour that must survive)**
`catalog/taxonomy_integration_test.go`; `httpapi/admin_taxonomy_integration_test.go`;
`httpapi/admin_taxonomy_routes_test.go`; `e2e/s2-taxonomy-viewport.spec.ts`;
the taxonomy portions of `e2e/s14-admin-catalog-review.spec.ts`.
None of these should be deleted in T4. They are the legacy-coexistence proof.

---

## 14. Risks, highest first

1. **Authoring write-path regression (T4-B).** `CreateCourse` and `UpdateCourseRevision` carry every
   proven S2/S12/S14/S15 journey. Mitigation: T4-A changes no existing behaviour — the discriminator
   defaults legacy and the new branch is unreachable until a Course is created Academic.
2. **The default-classification choice.** If new Courses defaulted to `ACADEMIC_CATALOG`, E23 breaks
   immediately and T4-A can no longer ship independently. The legacy default is load-bearing.
3. **Subject-request resolution race.** Mitigated by compare-and-set plus the single-PENDING index;
   must be tested with genuine concurrent transactions, not sequential calls.
4. **Subset enforcement drift.** Curriculum mappings can change between submit and approval. E8 means
   one implementation covers both, but approval revalidation must actually re-derive the inferred set
   rather than trust the submitted one.
5. **Retired-code reuse (E17).** Pre-existing T1 property, first exercised by T4. Recorded, not
   actioned.
6. **Dirty worktree overlap.** 277 modified/untracked paths at baseline, including
   `catalog/authoring.go`, `catalog/validation.go`, `catalogpublic/repository.go` — the exact files
   T4-A edits. Every T4 change must be additive against the working tree, never a checkout or revert.
7. **Migration ambiguity.** Low: 0025 is additive and its down migration drops only its own objects.

---

## 15. Founder decisions still required

**None outstanding.** Both items raised by this trace were resolved on 2026-08-22:

1. **D-091 §13 overrides the design report's T4 deletion line (E25).** Accepted. Recorded as D-093 §6,
   and the design report's T4 line has been corrected at source.
2. **The retired-code-reuse property (E17).** Accepted as a real problem and **corrected**: the trace
   proposed leaving it alone, and the Founder instead made official-code reservation permanent. This
   is D-093 §7, implemented in migration 0025 and proven in T4-A.

Everything else in the tranche brief is answered by implementation evidence above.

---

## 16. Recommended next slice

**T4-A — Course Academic Identity Foundation.** It owns the only migration, changes no existing
behaviour, and is provable entirely against real Postgres with no UI. Nothing else can be built
correctly before it lands.

> **T4-A is complete and proven as of 2026-08-22.** See
> [the T4-A evidence](2026-08-22-mvp-f20-t4a-course-academic-identity-foundation.md). The next slice
> is **T4-B — Instructor Subject-First Authoring**.
