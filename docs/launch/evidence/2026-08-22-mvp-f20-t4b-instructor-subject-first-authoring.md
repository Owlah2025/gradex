# MVP-F20 / T4-B — Instructor Subject-First Authoring

**Date:** 2026-08-22
**Tranche:** MVP-F20 (T4) sub-tranche B of five
**Status:** `T4-B PROVEN`; later clean closure removed the two transient S15 identities (§14b), and
MVP-F20 is now proven by the [full T4 completion record](2026-08-22-mvp-f20-t4-completion.md).
**Authority:** [D-093](../../DECISIONS.md#d-093--course-academic-identity-is-an-explicit-classification-model-and-an-official-subject-code-is-permanently-reserved) §1–§6, D-091 §8–§9
**Builds on:** [T4-A](2026-08-22-mvp-f20-t4a-course-academic-identity-foundation.md) (migrations 0025/0026)

T4-B replaces the ordinary new-Course experience. **No new migration**: the T4-A/A.1 schema carried
everything this slice needed, so nothing was added at 0027.

---

## 1. Normal Course creation transition

| | Before | After |
|---|---|---|
| Flow | Course → separate taxonomy panel → Major term → Subject term → Study Year | University → canonical Subject → Course details |
| Classification | `LEGACY_TAXONOMY` by default | `ACADEMIC_CATALOG`, derived by the server |
| Academic identity | three revision-scoped legacy terms | Course-level Institution + canonical Subject |
| Identifiers | Course ID and revision ID copied between panels | never typed or pasted |

Omitting the academic context is now a **validation failure**, not a silent fall-back. That is the
whole point: a silent fall-back is how the old confusing Course would have kept being created after
the redesign shipped. Legacy construction survives only as a fixture and T5 path, never as a product
path.

## 2. Instructor academic APIs

Mounted at `/authoring/academic/*` behind **`CapContentManagement`** — the Instructor's own
capability, never `CapAcademicCatalog`. This is the same shape T3 used to give Students an option
projection over the same catalog without granting Admin authority.

| Route | Purpose |
|---|---|
| `GET /authoring/academic/institutions` | Active universities. Retired ones never appear. |
| `GET /authoring/academic/institutions/:id/subjects?q=` | Active Subject search with context and inferred audience |
| `GET /authoring/academic/institutions/:id/subjects/:subjectId` | One Subject, so a Course's stored Subject renders without re-searching |

The group mounts **read routes only**, so there is no Subject create, amend, retire, or
curriculum-mapping handler behind it to reach even with a forged method. Nothing hardcodes Kuwait
University: it arrives as catalog data, and a second Institution appears the moment one exists.

## 3. Subject search

`SearchAuthoringSubjects` reuses the **same predicate** as the Admin `ListSubjects` —
`catalog_normalize_ar` for titles, `academic_normalize_code` for codes — so the two surfaces cannot
drift apart. `0418-320`, `0418320`, `Principles of Computer Systems`, and `مبادئ` all resolve to one
canonical Subject.

The client never normalizes; the raw query reaches the server unchanged, keeping one implementation.

Projection: id (for form values only), official code, both titles, owning Department and its College
as descriptive context, and the derived Programs. No audit metadata, no taxonomy terms, no revision
identifiers.

## 4. Creation contract

```jsonc
POST /api/v1/courses
{ "title_ar": …, "title_en": …, "description_ar": …, "description_en": …,
  "institution_id": "…", "subject_id": "…" }
```

There is deliberately **no classification field**. The server derives `ACADEMIC_CATALOG` from the
presence of `institution_id`, so a payload naming `classification_model` is simply ignored — proven,
not assumed. Course, academic identity, and revision 1 are written in one transaction by the existing
`CreateCourse` command.

## 5. Academic Course result

Institution and Subject on the Course; revision 1 created atomically; `major_term_id`,
`subject_term_id`, and `study_year` all **NULL**; zero `course_program_targets` rows.

## 6. Automatic audience preview

**The rule:** a Program is associated with a Subject when the Program's own `ACTIVE`, non-retired
Curriculum carries a `curriculum_subjects` row for that Subject, and the Program is not retired.
`curricula_one_active_per_program` guarantees at most one active plan per Program, so the rule is
single-valued.

Nothing else contributes — not legacy Major terms, not Student profiles, not Course titles, not an
Instructor's history.

**Read-only and never persisted.** Displaying the audience writes no rows, proven by counting
`course_program_targets` after both search and creation. Zero rows remains the automatic state until
T4-C.

**Placement is shown only where authoritative.** Proven directly: a Subject mapped to two Programs
where only one publishes a study plan returns the level for that Program and **omits it entirely**
for the other. Nothing is derived from a course number.

## 7. Unmapped Subject

Selectable, and the empty audience is stated rather than hidden or invented:
*"No Programs are currently associated with this Subject in the Academic Catalog."* Course creation is
never blocked. Journey B exercises `0418-466`, one of the six genuinely unmapped canonical Subjects
in the T2 launch manifest.

## 8. Subject editing

`PUT /api/v1/courses/:id/subject`, which exposes the T4-A lifecycle rather than reimplementing it.

| State | Behaviour |
|---|---|
| Never published, not under review | correctable, within the Course's own Institution |
| Candidate in `PENDING_REVIEW` | refused; the UI explains why instead of disabling a control |
| Changes requested, never published | correctable again |
| Published | read-only identity; no selector rendered at all |

**Institution is stable after creation** (§23). A Subject from another Institution is refused, so a
Course never migrates university through a Subject edit. An Instructor who picked the wrong
university creates a new Course — no exceptional workflow was invented.

## 9. Legacy compatibility

The compatibility panel is gated on the **server's `classification_model`**, never on whether a field
happens to be null — a legacy Course and a subject-less Academic draft both have no Subject, and only
the discriminator separates them. A frontend test asserts the source cannot regress to null-inference.

The legacy study-year control and the legacy fields in the revision-save command are both gated the
same way, so an Academic Course cannot send legacy taxonomy even accidentally. The server refuses it
regardless (T4-A guard), which is what makes this presentation rather than the control.

## 10. Legacy creation closure

Proven three ways: omitting academic context is refused; a forged `classification_model` in the
payload changes nothing; and the studio's form offers a university and a Subject with no
classification control, with Create disabled until a Subject is chosen.

## 11. Authorization

An Instructor reads the catalog and mutates nothing in it. Proven by attempting Subject create,
Subject rename, and Subject retire as the Instructor — all refused. The Instructor client is
additionally checked to contain no write verb and no `/admin/` path at all.

## 12. Backend tests

New: `t4b_instructor_academic_integration_test.go`, `t4b_academic_course_creation_integration_test.go`.

Points 1–10 (reads), 11–22 (creation), 23–31 (editing), 32–37 (audience) all proven. Highlights:
retired Subject refused server-side and not merely hidden; cross-Institution Subject refused at
create, at edit, and by the composite FK; classification unforgeable; a legacy Course cannot be
converted through the academic route; a superseded Curriculum and a retired Program both drop out of
the inferred audience.

```
go build ./...                     clean
go vet ./...                       clean
go vet -tags=integration ./...     clean
go test ./...                      27 packages ok, 0 failures

integration:
  internal/httpapi                 ok  313.8s
  internal/catalog                 ok   90.5s
  internal/academic                ok   45.3s
  internal/academic/importer       ok   31.5s
  internal/academic/manifest       ok    0.3s
  internal/db                      ok   67.4s
  internal/catalogpublic           ok    5.7s
  cmd/api                          ok   16.6s
```

## 13. Frontend tests

`subject-first-authoring.test.ts` — behavioural assertions against a stubbed transport (route,
method, CSRF, body shape) plus structural assertions on shipped source, so a regression cannot
reintroduce a legacy control or a T4-C/T4-D affordance through a path no test renders.

```
typecheck: clean
tests 343 · pass 343 · fail 0        (baseline 325 + 18 new)
```

## 14. T4-B E2E — Journeys A–D

`e2e/t4b-instructor-subject-first-authoring.spec.ts`, **4 passed**. Every Subject comes from the real
Kuwait University manifest imported through the real Admin API.

- **A** — university → search `0418-320` → select → context and derived Programs shown → create.
  Server-verified: `ACADEMIC_CATALOG`, Institution, canonical Subject resolved back to `0418-320`,
  revision 1, all three legacy fields NULL, no UUID anywhere in the picker.
- **B** — shared Subject shows Computer Science and Cybersecurity; `0418-466` truthfully reports zero
  associations and still creates a Course. No customization control and no checkbox exists.
- **C** — Subject corrected before publication; classification and Institution unchanged; no legacy
  taxonomy created; the studio shows the corrected Subject as identity.
- **D** — legacy creation refused two ways; the form offers no legacy mode; Create disabled without a
  Subject; an Academic Course is refused the legacy taxonomy route called directly.

**One scope limit, stated plainly.** Journey C does **not** reach `PENDING_REVIEW`: an incomplete
Course cannot be submitted, and submission needs real media the media-authoring suite owns. The review
freeze and the post-publication lock are proven against the real API in
`TestT4BSubjectEditingAcrossTheLifecycle` instead of by driving the database behind the browser's
back. The journey title was corrected to say only what it proves.

## 14a. Full canonical Playwright regression

```text
135 passed · 8 failed · 3 did not run · 13.3m
```

Configuration: local `backend/docker-compose.yml` stack (PostgreSQL 16, Redis 7, MinIO, Mailpit),
Playwright + Chromium, **1 worker**, 146 tests collected (142 + the 4 T4-B journeys), branch
`ui-antigravity-20260817` with its protected uncommitted working tree in place.

**Six of the eight are the accepted identities**, byte-for-byte:

```text
s5-expired-entitlement.spec.ts:712
s5-playback-performance.spec.ts:157
s5-viewport-evidence.spec.ts:223  (phone, tablet, laptop, desktop)
```

**Two are new and are recorded rather than accepted:**

```text
s15-dashboard-resume.spec.ts:135   English continue-learning journey
s15-dashboard-resume.spec.ts:213   Arabic continue-learning journey
```

Investigation, per §52:

- Both **pass in isolation** — `npx playwright test e2e/s15-dashboard-resume.spec.ts` returned
  `2 passed (26.1s)` immediately after the failing suite run.
- Both failed with a Next dev-server `PageNotFoundError` and a 30s test timeout, not a product
  assertion.
- S15 creates no Course, calls no authoring route, and touches no academic surface, so no T4-B code
  path is exercised by it.
- Both passed in the T4-A baseline run and in the first clean T4-B run of this slice.

Classified `UNRESOLVED_INTERMITTENT_NONREPRODUCIBLE`, the same classification the project already
uses for C1. It is **not** claimed as green and **not** claimed as an accepted identity.

Arithmetic check: baseline 133 passed + 4 new T4-B journeys = 137 expected; two intermittent S15
failures give the observed 135.

**Two earlier runs of this slice are recorded for completeness and are not evidence of anything.**
A run showing `88 passed / 47 failed / 11 did not run` was invalidated by builder error: a second
Playwright invocation was started while the canonical run was in flight, and its teardown removed
`/var/tmp/gradex-s5-e2e-run-state.json`, after which 74 tests failed with
`E2E run state is missing`. A subsequent clean run showed `136 passed / 7 failed / 3 did not run`,
whose single non-accepted failure was the `t2` ordering coupling fixed above.

## 14b. Clean canonical baseline closure

Before T4-C/D/E began, the required uncontended one-worker rerun collected the same 146 tests and
returned `137 passed / 6 failed / 3 did not run (9.3m)`. The failures were exactly the accepted
`s5-expired-entitlement:712`, `s5-playback-performance:157`, and
`s5-viewport-evidence:223` ×4 identities. Both S15 cases passed, so neither was promoted to the
accepted-failure list. The exact record is retained in the
[baseline closure](2026-08-22-mvp-f20-t4b-canonical-baseline-closure.md).

## 15. Existing tests transitioned (§48)

No assertion was weakened. Each was classified by what it actually proves.

| Test | Classification | Action |
|---|---|---|
| Six review/submission lifecycle tests in `review_integration_test.go` | prove the **legacy** review lifecycle, which must survive until T5 | Course construction moved to an explicit `seedLegacyCourseFixture`; every assertion unchanged. Together they are the legacy-compatibility proof. |
| `TestProductionInstructorMutationRoutesCommitAuditEvidence` | audit evidence per production route | create scenario carries academic context; a scenario added for the new Subject route, so both audit `COURSE_CREATED` and `COURSE_SUBJECT_ASSIGNED` |
| `TestAuthorizationMatrixMatchesMountedRouter` | every mounted route is classified | matrix row added for `PUT /courses/:id/subject` (ownership-protected) |
| `s12-instructor-authoring.spec.ts` case E | the **server's own reason** appears at Submit | still asserts `COURSE_EMPTY`, and now asserts `TAXONOMY_DIMENSION_MISSING` is **absent** — an Academic Course must never be asked for the legacy dimension. The legacy gate stays proven for legacy Courses in the backend suite. |
| `s12` other cases | authoring persistence and ownership | helper now selects university + Subject; assertions unchanged |
| `s14-admin-catalog-review.spec.ts` case D | the failure renders **at** the Submit control (the founder's actual complaint) | Course created through the academic flow; asserts `COURSE_EMPTY` and that the legacy dimension is absent. The layout assertion — the real subject — is unchanged. |
| `s2-taxonomy-viewport.spec.ts` | direction and usability of both surfaces at every viewport | the mocked Course list now carries one `LEGACY_TAXONOMY` Course, which is the state the compatibility panel exists to serve. With an empty list the panel is correctly absent under T4-B. Viewport assertions unchanged. |

**Spec catalog ownership.** `s12` and `s14` need *a* university with *a* Subject to author against.
They first imported the Kuwait University manifest, which broke `t2-launch-catalog-data` — those
specs run earlier alphabetically, so `t2`'s dry run found the launch catalog already imported and
planned zero creates. They now create a small dedicated catalog of their own instead, leaving the
launch manifest for `t2` to import first. `t4b` keeps the real manifest, because its journeys assert
against real launch data and it runs after `t2`.

`setupAcademicAPIServer` additionally mounts the catalog foundation so both surfaces are reachable
from one server. That changes no existing assertion — it only makes more of the production router
available.

## 16. Migration

**None.** T4-A/A.1 schema was sufficient, exactly as designed. No 0027 was created.

## 17. Repository state

Protected dirty baseline preserved. **No package-wide formatting was run** — every `gofmt -w` named
specific files, per the correction after T4-A. The only files touched are the ones listed in this
slice.
