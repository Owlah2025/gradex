# MVP-F18 / T2 — Launch Catalog Data — completion evidence

**Recorded:** 2026-08-21 · **Revised:** 2026-08-22 (T2.1 scope alignment, then **T2.2 Data Science & AI**)
**Authority:** [D-091](../../DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy)
under [D-089](../../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).
**Curation record:** [Kuwait University launch catalog scope](2026-08-21-kuwait-university-launch-catalog-scope.md)
**Depends on:** [MVP-F17 / T1](2026-08-21-mvp-f17-academic-catalog-foundation.md) (`E2E_PROVEN_FOUNDATION`)
**Seat:** Claude held the **builder** seat by Founder reassignment. §9 is a
`BUILDER_SELF_AUDIT` and **is not independent review.** T2 stops here.

T2 is a **foundation-data tranche**. It creates no canonical denominator feature
row, promotes none, and does not move the MVP score, which remains
`37 / 53 = 69.8%`.

> **Research evidence, manifest data, and automated tests are separated on purpose.**
> §1 records what was researched and curated. §2 onward record what was
> mechanically proved. The two are not interchangeable.

---

## 1. Research evidence (not a test result)

Sixteen primary Kuwait University sources, all retrieved 2026-08-21, are recorded in
`backend/internal/academic/manifest/data/kuwait-university/sources.yaml` and
analysed in the [scope record](2026-08-21-kuwait-university-launch-catalog-scope.md).
No competitor, aggregator, or encyclopaedia source contributed any academic fact;
a test fails the build if one appears in the source catalog.

Scope was derived from repository authority and, in T2.1, from the Founder's four
stated teaching areas. The development database contained **zero**
`taxonomy_terms` rows, so nothing was inherited.
[`PROJECT_VISION.md`](../../PROJECT_VISION.md) is a persona document, not a
launch-inventory register: it is narrower than the Founder's stated scope, and
T2.1 records that conflict rather than letting the persona govern.

**Uncertainties are recorded, not hidden.** The revision-1 CS source conflict is
**resolved** by the independent Cybersecurity major sheet. The DSAI Program —
T2.1's open item — is **resolved in T2.2**: its `.docx` major sheets and
8-semester plan were extracted and transcribed. Two uncertainties remain, both
recorded in the scope record: a credits conflict on `0480-423` between the
English and Arabic sheets (credits left NULL), and the four Cybersecurity
`1830-` Subjects whose now-known owning department cannot be assigned without
tripping the importer's identity-rebind guard.

## 2. Manifest data

`backend/internal/academic/manifest/data/kuwait-university/manifest.yaml`
(`kuwait-university-launch-v1` **v1.2.0**). Data is checked-in YAML, embedded into
the binary; no Kuwait University record is hardcoded in Go.

| Entity | v1.0.0 | v1.1.0 (T2.1) | **v1.2.0 (T2.2)** |
|---|---|---|---|
| Institutions | 1 | 1 | **1** |
| Academic Units | 9 | 9 | **11** |
| Programs | 3 | 4 | **5** |
| Curricula | 3 | 4 | **5** |
| Subjects | 27 | 44 | **84** |
| CurriculumSubject mappings | 41 | 69 | **112** |
| Sources cited | 9 | 16 | **20** |

**T2.1 changed launch data only — no importer, schema, or migration change.**
It added the real B.Sc. Cybersecurity Program and its 127-credit plan, 17
Subjects across the Software, Cybersecurity, and Data Science teaching areas,
authoritative level and semester placement for 16 Computer Science mappings from
Kuwait University's official Suggested Study Plan, and explicit
`version_label_source` provenance on every curriculum. It also **corrected a
factual error in the revision-1 scope record**, which had asserted that Kuwait
University confers no Cybersecurity or Data Science degree. Full detail in the
[scope record](2026-08-21-kuwait-university-launch-catalog-scope.md).

The v1.0 → v1.1 upgrade was proved as a **non-destructive deterministic update**:
against a v1.0-shaped database the dry run planned `create=0 update=3 noop=128`
and wrote nothing, and the apply performed exactly those three in-place title
updates with the Subject count unchanged at 44 and zero duplicates.

Manifest identities are **semantic keys** (`ku`, `ku-science`,
`ku-computer-engineering`, `ku-cs-2024`, `ku-0410-101`). No database identifier
appears anywhere in the manifest, which is what makes a repeated import a no-op.

**No schema change.** T2 required no migration; the schema stays at version 23.
No migration was added to increment a version.

## 3. Importer validation

```text
cd backend && go test ./internal/academic/manifest -count=1
  ok — 21/21 tests (T2.1)
```

Validation is a pure function of checked-in data and needs no database. It
refuses: unparseable YAML, unknown fields (strict decoding), unknown parent keys,
manifest hierarchy cycles, duplicate manifest keys, **two keys normalizing to one
Subject identity**, code-less title collisions, out-of-range recommended levels,
placements asserted without their own citation, dangling source citations,
duplicate active curricula, and missing Arabic provenance.

Semantic assertions on the shipped manifest: Kuwait University with
`max_academic_level: 5` and no foundation stage; **exactly one** manifest present
(D-091 §11); `0410-101` declared once and mapped into **≥4** curricula; no code
collision after normalization; official display formatting preserved; every
Subject Arabic title declared `gradex_translation`; every source `https://` and
none from a non-academic host.

T2.1 added three semantic tests and rewrote one:
`TestPlacementExistsOnlyWhereAnOfficialPlanPublishesIt` (a placement is legal only
on the one curriculum Kuwait University publishes a plan for, must cite that plan,
and must fall inside the plan's own year and term ranges — replacing the
revision-1 test that asserted *no* placement may exist);
`TestOnlySourceBackedProgramsExist` (no Gradex teaching label may appear as a
Program, and Cybersecurity **must** appear because Kuwait University confers it);
`TestPlaceholderCurriculumLabelsDeclareThemselves`.

## 4. Database import proof

```text
cd backend && go test -tags=integration ./internal/academic/importer -count=1
  ok — 15/15 tests (15.9s)
```

| Property | Test |
|---|---|
| Dry run writes nothing — no rows, **no audit events** | `TestDryRunWritesNothing` |
| Apply into empty DB, then repeated apply is a pure no-op | `TestApplyIsIdempotent` |
| One canonical `0410-101` across **4** Programs; zero code collisions | `TestSharedSubjectIsOneRowAcrossPrograms` |
| CLI import audited as SYSTEM with NULL account | `TestImportRecordsSystemActorAudit` |
| Safe display-metadata update applies | `TestSafeMetadataUpdateIsApplied` |
| Re-formatting an official code keeps one Subject | `TestOfficialCodeReformattingKeepsOneSubject` |
| Identity-changing update **fails closed and unwinds** | `TestIdentityChangingUpdateFailsClosed` |
| Failure mid-import rolls back the whole institution | `TestFailureMidImportRollsBackEverything` |
| Absence from manifest reports drift, **never retires** | `TestAbsenceFromManifestNeverRetires` |
| One ACTIVE curriculum per Program survives import | `TestOnlyOneActiveCurriculumPerProgramSurvivesImport` |
| 4 concurrent imports produce no duplicates | `TestConcurrentImportsDoNotDuplicate` |
| Kuwait University data is semantically correct | `TestImportedKuwaitUniversityDataIsSemanticallyCorrect` |
| Legacy taxonomy and Course classification untouched | `TestImporterDoesNotTouchCourseOrLegacyTaxonomy` |
| Unvalidated manifest refused before any write | `TestImporterRefusesAnUnvalidatedManifest` |
| Import without an audited actor refused | `TestImporterRequiresAnActor` |

### Observed CLI run

```text
$ catalog-import -mode=validate -manifest=kuwait-university-launch-v1
manifest kuwait-university-launch-v1 v1.1.0 is valid: institution=kuwait-university
units=9 programs=4 curricula=4 subjects=44 mappings=69 sources=16

$ catalog-import -mode=dry-run -manifest=kuwait-university-launch-v1   # empty catalog
create=131 update=0 noop=0 drift=0
  rows after dry run: institutions=0 subjects=0 curriculum_subjects=0

$ catalog-import -mode=apply -manifest=kuwait-university-launch-v1
create=131 update=0 noop=0 drift=0
  inst=1 units=9 programs=4 curricula=4 subjects=44 mappings=69

$ catalog-import -mode=apply -manifest=kuwait-university-launch-v1   # second run
create=0 update=0 noop=131 drift=0
  inst=1 units=9 programs=4 curricula=4 subjects=44 mappings=69
```

The v1.0 → v1.1 upgrade path, run against a database holding v1.0-shaped rows:

```text
$ catalog-import -mode=dry-run -manifest=kuwait-university-launch-v1
  UPDATE   subject   ku-0418-101 0418-101
  UPDATE   subject   ku-0418-111 0418-111
  UPDATE   subject   ku-0418-143 0418-143
create=0 update=3 noop=128 drift=0
  database still holds the old titles: the dry run wrote nothing

$ catalog-import -mode=apply -manifest=kuwait-university-launch-v1
create=0 update=3 noop=128 drift=0
  subjects=44  updated_title="Introduction to Computer Science"  dupes=0
```

Direct database verification after apply:

```text
0410-101 | Calculus I | curricula_mapped = 4
duplicate normalized codes            = 0
levels_set=16  credits_set=58  of 69
audit: SYSTEM | system:catalog-import | account_null=t
```

## 5. Admin authorization

```text
cd backend && go test -tags=integration ./internal/httpapi -run TestAcademicCatalogImportAdminAPI -count=1
  ok — 11/11 (1.2s)
```

- Admin lists **only manifest identifiers**; the response leaks no filesystem path.
- **Instructor 403** on dry-run, apply, and the manifest listing. **Student 403.**
  **Anonymous 401/403.**
- Path and URL selectors all refused `422`: `../../../etc/passwd`, `/etc/passwd`,
  `https://example.test/manifest.yaml`, an internal repo path, and a `.yaml`
  suffix. An identifier-shaped but unknown manifest is `404`, never a fallback.
- Admin dry run returns an unapplied plan and writes **zero** institutions.
- Admin apply imports Kuwait University and is audited under the **acting Admin**
  (`actor_role = ADMIN`, non-null account); **zero SYSTEM audits on this path**.
- Repeated Admin apply: all no-ops, every table count unchanged.
- A manifest cannot be imported into a different institution (`422`).
- Both dry-run and apply sit behind session mutation security.

## 6. Audit semantics

| Path | Actor | Rationale |
|---|---|---|
| **CLI** `catalog-import` | `actor_role = SYSTEM`, `actor_account_id = NULL`, `actor_descriptor = system:catalog-import` | The importer has no human operator. This reuses the convention `internal/identity/bootstrap.go` already established for deployment principals. **No Admin account is fabricated**, and audit is never bypassed. |
| **HTTP** `POST /admin/academic/institutions/:id/import` | `actor_role = ADMIN`, the authenticated Admin's account | The acting Admin is preserved, exactly as for hand-entered catalog mutations. |

Audit metadata carries the manifest id and version, the entity's natural key,
and each Subject's `title_ar_source` so an investigator can distinguish an
official title from a Gradex translation without opening the manifest. A dry run
writes no audit at all.

## 7. Browser proof

```text
cd frontend && npx playwright test e2e/t2-launch-catalog-data.spec.ts --reporter=line
  4 passed (19.1s)
```

- **A** — Admin dry-runs (plan, no writes), applies, then opens the **unchanged
  T1** Academic Catalog surface: Kuwait University at level 5, both Colleges,
  a department naming its parent, `0410-101 · Calculus I`, plan `2024 — Active`,
  and the mapping as `College requirement`. **No UUID in the rendered body**, no
  "taxonomy", no "manifest.yaml".
- **B** — searching `0410-101`, `0410101`, and `calculus` each return exactly one
  canonical Subject; it is mapped into all three launch Programs; every mapping's
  recommended level is absent.
- **C** — Instructor 403 on dry-run and apply.
- **D** — legacy taxonomy API and Admin surface still work; public catalogue
  unchanged.

## 8. Regression

**Backend**

```text
go build ./...                              OK
go vet ./...                                OK
go test ./... -count=1                      27 packages ok, 0 failures
go test -tags=integration ./... -count=1    31 packages ok, exit 0
```

Focused T1 foundation + T2 re-run: **63 tests green** across
`internal/academic/...` and `internal/httpapi` (Subject dedupe, AcademicUnit
hierarchy, cross-Institution protection, Curriculum integrity, Admin
authorization, and the Admin surface against imported data).

**Frontend** — `NO T2 FRONTEND PRODUCTION CHANGE`. T2 added one E2E spec and no
production file. Run anyway because it is cheap:

```text
npm run typecheck   PASS
npm test            311 passed / 0 failed
```

**Full canonical Playwright**

```text
126 passed
6 failed
3 did not run
duration 8.4m
```

Configuration: local `backend/docker-compose.yml` stack (PostgreSQL 16, Redis 7,
MinIO, Mailpit), Playwright 1.62.0 + Chromium, 1 worker, branch
`ui-antigravity-20260817` with its protected uncommitted working tree in place.

Baseline before this tranche was `122 passed / 6 failed / 3 did not run`. T2 adds
exactly its own 4 tests → **126 passed**. The failure set is byte-for-byte the six
known accepted identities, with **no new failure identity**:

```text
s5-expired-entitlement.spec.ts:712
s5-playback-performance.spec.ts:157
s5-viewport-evidence.spec.ts:223  (phone, tablet, laptop, desktop)
```

**Proven features re-proved inside that run:** ST-19 manual purchase, S6 access
grant, F14 public preview, ST-15 protected materials, IN-09 upload→READY,
Instructor authoring, Admin Course review, public catalogue, legacy taxonomy
viewport. All green; no contract changed.

## 9. Builder self-audit

`BUILDER_SELF_AUDIT` — **not independent review.** Full check list in the tranche
report. Mechanically verified: no hardcoded Kuwait University in importer, CLI,
or handler logic (only comments and usage examples); no database identifier in
any manifest; no `os.Open`/`filepath.Join`/`http.Get`/`url.Parse` anywhere in the
manifest, importer, or import-handler code; no `DELETE FROM` and no
`retired_at = now()` in the importer; the importer writes only the six academic
tables plus `audit_events`; no reference to `taxonomy_terms`, `entitlements`,
`enrollments`, `purchase_requests`, `course_access_invitations`, progress, or
media anywhere in T2 code; `courses.subject_id` still does not exist.

## 10. Repository state

The protected uncommitted working tree was preserved throughout. No `reset`,
`stash`, `restore`, `clean`, or broad `checkout` was run, and no unrelated file
was normalized.

---

## 11. T2.2 — Data Science & Artificial Intelligence launch program (2026-08-22)

**Data-only pass.** `NO IMPORTER PRODUCTION CHANGE`, `NO FRONTEND PRODUCTION CHANGE`,
no schema change, no migration. Manifest v1.1.0 → **v1.2.0**.

**Founder Decision 1 applied:** the real B.Sc. Data Science and Artificial
Intelligence is imported, so a Kuwait University Data Science Student can select
their own Program in T3. **Founder Decision 2 applied:** B.Sc. Mathematics and
B.Sc. Financial Mathematics stay out as product scope; the Mathematics department
and its shared Subjects stay in.

**Added:** College of Life Sciences + Information Science department; the DSAI
Program with official Arabic; its curriculum; 40 Subjects; 43 mappings, of which
**33 carry authoritative level and semester** from the official 8-semester plan.
**Reused, not duplicated:** `0410-101`, `0410-111`, `0330-100`.

**Update path against the live v1.1 catalog — no reset:**

```text
dry-run   create=87 update=0 noop=131 drift=0   (0 rows written)
apply     create=87 update=0 noop=131 drift=0
apply #2  create=0  update=0 noop=218 drift=0
```

Shared-Subject and existing-Program database identifiers verified unchanged by
diff; zero retirements.

**Gates.** `go build`/`go vet` OK; 27 unit packages; 31 integration packages;
manifest **25/25**; importer **15/15**; frontend typecheck PASS and **311 passed /
0 failed**; T1+T2 browser specs **9/9**; canonical Playwright **127 passed / 6
failed / 3 did not run** (8.3m), the same six accepted identities, **no new
failure identity**.

**Tests corrected for new evidence** (none weakened): the placement test now
allows the two curricula Kuwait University actually publishes plans for; the
Arabic-provenance test now permits `official` where a cited source publishes
Arabic, instead of requiring every Subject to be a Gradex translation; the
shared-Subject assertions moved from four Programs to five. Added:
`TestDataScienceProgramMatchesOfficialStructure`,
`TestDataScienceReusesCanonicalSubjects`,
`TestMathematicsProgramsRemainOutOfLaunchScope`,
`TestLaunchProgramSetIsExactlyTheFounderSet`.

**Matrix impact — none.** `E2E_PROVEN` remains **37 / 53 = 69.8%**; denominator 53.
