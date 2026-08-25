# T5 — Legacy Academic Migration completion

**Date:** 2026-08-23
**Tranche:** MVP-F21 (T5), continuation
**Status:** mechanism `PROVEN`; corpus cutover **complete except one intentionally unresolved record**
(tracker status remains `PARTIAL` — see §16)
**Authority:** D-091 §13; supersedes nothing in
[the 2026-08-22 T5 record](2026-08-22-mvp-f21-t5-legacy-taxonomy-migration.md), which remains the
account of how the mechanism was designed.

---

## 1. What this tranche changed

The 2026-08-22 record delivered the migration mechanism. This one closes the reporting hole it
shipped with, adds the two outcomes the real corpus turned out to need, and runs the tool against
every legacy corpus that actually exists on this machine.

## 2. The reporting defect, and why it mattered

`loadLegacyCourses` joined `course_revisions` with an **INNER JOIN**. A legacy Course carrying no
Course Revision at all therefore produced no row, so it did not merely fail to migrate — it never
appeared in the report and was not counted. The summary described a corpus smaller than the corpus.

That is disqualifying for a cutover tool: the whole purpose of `--report` is to let a human account
for every record before authorising a write, and a record the tool cannot see cannot be accounted
for.

Measured against the real `backend-postgres-1 / gradex` corpus, which holds exactly one such Course:

```
legacy courses in corpus:     1
OLD inner-join workset rows:  0     <- the Course was invisible
NEW left-join workset rows:   1
```

The join is now a LEFT JOIN, `has_revision` is computed from it, and such a Course is classified
`NO_REVISION`.

### `NO_REVISION` semantics

A Course with no revision has **no legacy taxonomy to translate** — the legacy vocabulary lives on
revisions — and **can hold no audience target**, because `course_program_targets` is revision-scoped.
There is nothing to migrate and nothing may be invented, so the outcome:

- always appears in the report, with a stated reason;
- is never marked `would_mutate`;
- is skipped by `--apply`, which fabricates no revision.

Proven by `TestT5RevisionLessCourseIsReportedAndNeverMigrated` against real PostgreSQL, which asserts
both that the Course is present in the report and that after `--apply` its classification, columns,
and revision count (zero) are unchanged.

## 3. `FOUNDER_MAPPING_REQUIRED` — a recorded question, not a gap

`UNMAPPED` means the tool found nothing. The real Kuwait University record is a different fact: the
tool found **several defensible answers** and must not choose between them. Collapsing the two into
one outcome would tell an operator "add a mapping entry" when the correct instruction is "this needs
a product decision".

The mapping file therefore gained a `pending_decisions` section. An entry records the legacy term,
the candidates, and why the choice is unsafe. Validation refuses a term that is both mapped and
pending, a pending entry with no candidates, and one with no stated reason — so the file cannot
quietly become a guess.

A Course whose legacy term is pending fails closed under **every** flag: `--apply` reports it and
writes nothing.

## 4. Drift is reported, never repaired

An already-Academic Course whose legacy code the current mapping now sends to a **different** Subject
is classified `DRIFT`. It is never overwritten. Either an Admin corrected the Course deliberately or
the mapping was edited after the cutover, and this tool is not entitled to decide which. Proven by
`TestT5DriftIsReportedAndNeverOverwritten`.

## 5. The report now accounts for everything

- Already-academic Courses are reported **by id** rather than as a bare count, so a rerun can be
  diffed against the corpus.
- Each row carries: Course id, title, current classification, legacy code and label, current
  canonical Subject, target Subject, mapping source, candidates, audience, reason, and an explicit
  `would_mutate` flag.
- The summary emits `total`, and the CLI **fails** if the row count and `total` disagree.

`TestT5ReportAccountsForEveryLegacyCourse` asserts the report covers exactly the set of legacy
Courses in the database, each exactly once.

## 6. Mapping authority — unchanged and re-proven

There is exactly one automatic authority: the Founder mapping file, matched on **normalized official
code** (`academic_normalize_code`), the same identity rule the database enforces. It is recorded in
one constant, `mappingSourceNote`, so a second softer authority cannot be added without editing that
line.

`TestT5MappingNeverFallsBackToTitleSimilarity` seeds a legacy term whose label is character-for-
character a canonical Subject title and asserts the Course stays `UNMAPPED`. No fuzzy matching, no
Course-title matching, no similarity, no embedding.

## 7. Real legacy corpus results

Every persistent PostgreSQL instance on this machine was surveyed **read-only**. No original volume
was reset, dropped, or mutated.

| Corpus | Schema | Legacy | Academic | MIGRATE | ALREADY_ACADEMIC | UNMAPPED | AMBIGUOUS | FOUNDER_MAPPING_REQUIRED | NO_REVISION | INELIGIBLE | DRIFT |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `backend-postgres-1 / gradex` | 26 | 1 | 0 | 0 | 0 | 0 | 0 | 0 | **1** | 0 | 0 |
| `gradex-founder-acceptance-postgres-1 / gradex` (via clone) | 20 → 26 | 1 | 0 | 0 | 0 | 0 | 0 | **1** | 0 | 0 | 0 |
| `backend-postgres-1 / gradex_mailpit_acceptance` | 26 | 0 | 1 | — | — | — | — | — | — | — | — |
| `gradex-s12-postgres-1 / gradex` | 26 | 0 | 0 | — | — | — | — | — | — | — | — |
| `compose-postgres-1 / gradex` | 20 | 0 | 0 | — | — | — | — | — | — | — | — |

**The entire real legacy corpus is two Courses.** One has no revision; one needs a Founder decision.

The schema-20 corpus was never touched. It was `pg_dump`-ed (a read), restored into a disposable
`gradex_t5_founder_clone`, migrated 20 → 26 with the ordinary `cmd/migrate` tool, and had the
canonical Kuwait University manifest imported through `cmd/catalog-import`. Every T5 run below is
against that clone.

## 8. Real-corpus report output

```
$ catalog-migrate --mapping kuwait-university-legacy-v1        # backend-postgres-1 / gradex
  NO_REVISION              00000000-0000-0000-0000-000000000010
      reason:   the Course has no Course Revision, so it carries no legacy taxonomy to
                translate and can hold no audience target

total=1 migrate=0 already_academic=0 unmapped=0 ambiguous=0
founder_mapping_required=0 no_revision=1 ineligible=0 drift=0
```

```
$ catalog-migrate --mapping kuwait-university-legacy-v1        # gradex_t5_founder_clone
  FOUNDER_MAPPING_REQUIRED 3a98811f-f021-41bf-a065-572c4585aafd  Introduction to Algorithms
      legacy:   SWE101 Software Engineering
      candidates: 0418-390, 0418-301, 0418-201, 1830-245
      reason:   legacy Subject code SWE101 awaits a Founder decision between 4 canonical
                candidates: [recorded reason]

total=1 migrate=0 already_academic=0 unmapped=0 ambiguous=0
founder_mapping_required=1 no_revision=0 ineligible=0 drift=0

1 Course(s) need a Founder mapping decision before they can migrate.
```

## 9. Migration invariants on the real corpus

`--apply` was run against the clone with the real checked-in mapping. A 24-field invariant snapshot
was taken immediately before and immediately after and compared:

```
--- INVARIANT DIFF (empty = identical) ---
IDENTICAL
```

Covering: course count and ids, owner, slug, lifecycle, `live_revision_id`, suspension, revision
count / ids / states, legacy revision term columns, sections, lessons, media assets and versions,
price changes and amounts, entitlement count and rows, invitations, purchase requests, and lesson
progress. Nothing changed, which is the correct outcome: nothing in that corpus is migratable.

Because a real-corpus no-op cannot prove that a *successful* migration preserves identity,
`TestT5ApplyPreservesEveryCommercialInvariant` builds the full commercial state a purchased Course
carries — published live revision, section, lesson, price change, real invitation → entitlement
chain, enrolment, and lesson progress — migrates it for real, and compares a 19-key snapshot before
and after. Every key is unchanged; only `classification_model`, `institution_id`, and `subject_id`
move.

## 10. Idempotency

`--apply` was run twice in the commercial-invariant test. The second run:

- migrated nothing (`Counts.Migrate == 0`);
- left every invariant key identical to the first run's post-state;
- did not duplicate the audience target (`course_program_targets` count stayed 1).

The workset is exactly `classification_model = 'LEGACY_TAXONOMY'`, so a migrated Course leaves it
permanently and a rerun has nothing to remember.

## 11. Schema policy

**No migration 0027 was added.** Schema 26 expresses the whole contract: 0025's `CHECK` constraints
make classification, Institution, and Subject move in one atomic `UPDATE`, and the T4-A immutability
trigger permits the first Subject assignment on an already-published Course. Nothing in this tranche
needed a column, an index, or a constraint that does not already exist. Every change is a query or
application correction.

## 12. Legacy taxonomy usage audit

| Dependency | Classification | Action |
|---|---|---|
| `catalogpublic/repository.go` legacy `major`/`subject` projection | **compatibility fallback** — an Academic Course projects its canonical Subject; a legacy Course falls back to its taxonomy term | keep |
| `catalog/taxonomy.go`, `catalog/validation.go`, `catalog/revision.go` | **still required** — the two unresolved legacy Courses remain fully operational | keep |
| `httpapi/admin_taxonomy_*`, Admin taxonomy UI panels | **still required** — the vocabulary those Courses reference must stay administrable | keep |
| `instructor/taxonomy-assignment-panel.tsx`, `course-builder.tsx` | **compatibility fallback** — new Courses have been Subject-first since T4-B; this serves existing legacy Courses only | keep |
| `catalog/taxonomy*_test.go`, `e2e/s2-taxonomy-viewport.spec.ts`, fixtures | **test-only** | keep while the fallback exists |
| migrations 0009, 0023, 0025 | **historical schema** | never removed by this tranche |

**Nothing is obsolete yet**, because a legacy Course still exists. D-091 §13 permits legacy removal
only after a proven cutover, and the cutover is not complete. No legacy route, column, table, enum,
or test was removed.

**User-facing UX does not depend on legacy taxonomy where canonical data exists**: the public
catalogue prefers `academic_subject` over the legacy term and reports the Institution only from the
canonical Academic Catalog, and the new T6 discovery filters read canonical data exclusively.

## 13. Tests

All observed green against real PostgreSQL.

```
go test -tags=integration ./internal/legacymigrate/ -count=1     ok   16.2s
```

New: `TestCheckedInMappingLoads` · `TestMappingRejectsResolvedAndPendingTerm` ·
`TestMappingRejectsPendingWithoutCandidates` · `TestMappingRejectsPendingWithoutReason` ·
`TestT5RevisionLessCourseIsReportedAndNeverMigrated` · `TestT5ReportAccountsForEveryLegacyCourse` ·
`TestT5PendingFounderDecisionFailsClosed` · `TestT5MappingNeverFallsBackToTitleSimilarity` ·
`TestT5DriftIsReportedAndNeverOverwritten` · `TestT5AlreadyAcademicRowsCarryCourseIdentity` ·
`TestT5ApplyPreservesEveryCommercialInvariant` ·
`TestT5MigratedCourseBecomesDiscoverableThroughT6Filters`.

Every pre-existing T5 test still passes unchanged. No assertion was weakened.

## 14. The T5 → T6 bridge

`TestT5MigratedCourseBecomesDiscoverableThroughT6Filters` is the join between the two halves of this
tranche. A published legacy Course with a real entitlement is migrated by the real migrator and is
then found through the real discovery filters — by University, by Subject, by Program, and by all
three combined — while keeping its id, revisions, publication state, price, and entitlement. Its
legacy Major became an **explicit** audience target, so its detail page names exactly that one
Program, proving explicit audience overrides inference across the migration boundary.

## 15. What remains

Legacy schema removal remains out of scope per D-091 §13. An intentionally unresolved record keeps the
legacy vocabulary in use, so the removal migration stays unauthorized.

## 16. Founder decision — SWE101 is intentionally unresolved (2026-08-23)

**Decision date:** 2026-08-23
**Disposition:** `KEEP_UNRESOLVED`
**Authority:** Founder decision, recorded as an
[amendment to D-091 §13](../../DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy)

The one remaining record has been decided, and the decision is **not to choose**.

```
Legacy Course:   Introduction to Algorithms   (3a98811f-f021-41bf-a065-572c4585aafd)
Legacy Subject:  Software Engineering
Legacy Code:     SWE101      — no canonical Kuwait University Subject carries this normalized code
Candidates:      0418-390 · 0418-301 · 0418-201 · 1830-245
Founder ruling:  KEEP_UNRESOLVED, 2026-08-23 — no canonical Subject selected
```

**Reason.** The legacy record contradicts itself: its SUBJECT term names one subject area and the
Course title names another, and the only authority this migrator may match on — the normalized code —
resolves to nothing. Selecting any candidate would mean ruling that either the legacy Subject label or
the Course title is the real academic identity, and neither is; both are prose. The Course is published
and a Student already holds an Entitlement against it, so an invented Subject would misclassify
something already bought. The Founder reviewed all four candidates and determined that none can be
chosen from the evidence that exists.

**This is a terminal accepted state, not outstanding work.** The distinction the tranche now encodes is
between *nobody has answered the question* and *the question was answered, and the answer is to wait for
evidence*. Both are fail-closed; only the second is finished.

### What changed

`PendingDecision` gained `decision`, `decided_on`, and `resolution_requires`. `decision` defaults to
`AWAITING_FOUNDER_DECISION`, so an entry can never silently claim to have been decided, and validation
refuses a recorded decision that is undatable, states no reopening evidence, or names an unknown
disposition. The outcome enum is unchanged: `FOUNDER_MAPPING_REQUIRED` already expressed the state
safely, and adding a second outcome purely for wording would have split one fail-closed behaviour
across two names.

### Runtime behaviour — unchanged where it matters

| | Behaviour |
|---|---|
| `--report` | Reports the Course with outcome `FOUNDER_MAPPING_REQUIRED`, the four candidates, `founder: KEEP_UNRESOLVED (2026-08-23)`, the reopening evidence, and the full reason. Counted in `total` and in `founder_mapping_required`. |
| `--apply` | Writes **nothing**. No classification flip, no `institution_id`, no `subject_id`, no Program target, no audit row, no fabricated revision. |
| Summary line | No longer announces the record as pending work. It now reads *"1 Course(s) are intentionally unresolved by Founder decision and are not pending work."* A genuinely undecided entry would still be announced as needing a decision. |
| The Course | Unchanged in every respect: same id, published, priced, publicly listed, purchasable, entitlement intact. It simply carries no canonical academic identity. |

### Observed against the real corpus clone

```
$ catalog-migrate --mapping kuwait-university-legacy-v1 --apply
mode:        APPLIED

  FOUNDER_MAPPING_REQUIRED 3a98811f-…-572c4585aafd  Introduction to Algorithms
      legacy:   SWE101 Software Engineering
      candidates: 0418-390, 0418-301, 0418-201, 1830-245
      founder:  KEEP_UNRESOLVED (2026-08-23)
      reopens on: the official Kuwait University subject code …; the Course syllabus …;
                  historical Course documentation …; an explicit Founder mapping supported by one of the above
      reason:   legacy Subject code SWE101 is intentionally unresolved by Founder decision of 2026-08-23 …

total=1 migrate=0 already_academic=0 unmapped=0 ambiguous=0 founder_mapping_required=1
no_revision=0 ineligible=0 drift=0

1 Course(s) are intentionally unresolved by Founder decision and are not pending work.
```

Post-apply state, read back from the database:

```
classification=LEGACY_TAXONOMY  institution=NULL  subject=NULL  lifecycle=PUBLISHED
entitlements=1                  targets=0
```

### Condition for future resolution

The mapping may change **only** when one of these authoritative sources establishes what the Course
teaches. They are recorded in the mapping data itself, not only here:

1. the official Kuwait University subject code for this Course, from the university's own records;
2. the Course syllabus naming the subject it teaches;
3. historical Course documentation from the Instructor or the original catalogue;
4. an explicit Founder mapping supported by one of the above.

Inference from the Course title, the legacy Subject label, fuzzy or semantic similarity, embeddings,
or model judgement remains prohibited and is enforced by
`TestT5MappingNeverFallsBackToTitleSimilarity`.

### Tests

- `TestCheckedInMappingLoads` — the shipped mapping records `KEEP_UNRESOLVED`, is dated, and states its
  reopening evidence.
- `TestPendingDecisionDefaultsToAwaiting` — an entry with no decision is still an open question.
- `TestMappingRejectsUndatableOrIrreversibleDecision` — a decision with no date, no reopening evidence,
  or an unknown disposition is refused at load.
- `TestT5IntentionallyUnresolvedRecordStaysFailClosed` — real PostgreSQL: a published Course with a real
  entitlement, under a `KEEP_UNRESOLVED` mapping, survives `--apply` with its classification, identity,
  and full commercial snapshot unchanged, and zero target and audit rows written.
- `TestT5PendingFounderDecisionFailsClosed` — the undecided path still reports
  `AWAITING_FOUNDER_DECISION` and still writes nothing.

### Status language

**Migration mechanism: `PROVEN`.** Every legacy Course is reported, every outcome is explicit,
`NO_REVISION` no longer disappears, mapping is deterministic and code-only, `--apply` is idempotent and
fail-closed, and course/commercial/access invariants are proven against real PostgreSQL and a real
corpus clone.

**Real-corpus data migration: complete except one intentionally unresolved record.** Zero Courses
migrated, because zero Courses were migratable: one has no revision and one is intentionally
unresolved. That is the correct outcome for this corpus, not a shortfall.

**Tracker status stays `PARTIAL`.** A legacy Course remains unmigrated, which is what `PARTIAL` means in
`FUNCTIONAL_COMPLETION.md`, and the legacy-removal migration stays gated. The change is that `PARTIAL`
no longer carries a pending Founder action — **no Founder mapping decision remains.**
