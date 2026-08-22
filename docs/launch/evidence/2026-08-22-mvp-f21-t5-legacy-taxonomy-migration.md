# MVP-F21 / T5 — Legacy Academic Taxonomy Migration

**Date:** 2026-08-22
**Tranche:** MVP-F21 (T5)
**Status:** `T5 PARTIAL` — migration mechanism proven; the cutover itself is a pending Founder action
**Authority:** D-091 §13; the T5 line of
[the redesign report](../../superpowers/specs/2026-08-21-academic-catalog-taxonomy-redesign.md)
**Builds on:** T4-A/A.1/B/C/D/E (migrations 0025, 0026)

---

## 1. Scope implemented

The canonical T5 specification is a **tool**, not a schema change:
`cmd/catalog-migrate` with `--report` / `--apply`, a checked-in Founder mapping file, and a later
separate migration that drops the legacy schema. This tranche delivers the first two. The legacy-drop
migration is explicitly **not** part of it — D-091 §13 permits removal only after the new catalog is
proven on a dual path.

## 2. The decisive archaeology finding

Before designing anything I measured the real legacy corpus across every persistent database:

| Database | Courses | Taxonomy terms | Revisions with taxonomy |
|---|---|---|---|
| `backend-postgres-1` | 1 | 0 | 0 |
| `compose-postgres-1` | 0 | 0 | 0 |
| `gradex-founder-acceptance-postgres-1` | 1 | 2 | **1** |

**The entire legacy corpus is one Course.** Gradex is pre-launch, and since T4-B ordinary Instructor
creation produces `ACADEMIC_CATALOG` Courses, so the legacy set can only shrink. This shapes the whole
tranche: T5 is not a bulk data migration, it is a small, careful, auditable cutover mechanism.

## 3. Architecture decisions

**No schema migration. There is no 0027.** The cutover reuses 0025's columns entirely. This is proven,
not assumed: the classification flip, Institution, and Subject all move in one `UPDATE`, and 0025's
two CHECK constraints make any partial write impossible. The T4-A immutability trigger permits the
cutover because it fires only when the Course *already* has a Subject — a legacy Course has none — so
a published legacy Course receives its first canonical Subject and is immutable from that instant.

**A Founder mapping file, not inference.** The legacy vocabulary has **no Institution**: a
`taxonomy_terms` row is a bare label with an optional code. That is *missing* information, not
ambiguous information, so it cannot be closed by matching. Guessing a Subject from a label would
silently invent academic identity for a Course a Student may already have purchased — precisely the
defect the redesign exists to remove. The gap is closed by a checked-in, Founder-authored mapping.

**Matching is on normalized codes only.** `taxonomy_terms.academic_code` → `subjects.code_normalized`,
both through the same normalization the database applies. Labels are carried for human review and are
never matched on: a label is prose, not identity.

**Legacy Major becomes audience, not identity.** A legacy Major is closer to "who this was for" than
to "what this teaches", so it maps to revision-scoped `course_program_targets`, never to Course
identity.

**The outcome matrix.** Only `MIGRATE` is ever acted on:

| Outcome | Meaning | Action |
|---|---|---|
| `MIGRATE` | one Subject, one Institution, eligible | migrated |
| `UNMAPPED` | no Subject term, no code, or no mapping entry | left legacy, reported |
| `AMBIGUOUS` | the Course's own revisions name different legacy Subjects | left legacy, reported |
| `INELIGIBLE` | mapped Subject is retired or absent from the Institution | left legacy, reported |
| `ALREADY_ACADEMIC` | a previous run migrated it | reported, not re-migrated |

## 4. Idempotency and safety

The workset is exactly `classification_model = 'LEGACY_TAXONOMY'`. A migrated Course leaves that set
permanently, so a rerun neither re-migrates it nor has to remember anything. The per-Course `UPDATE`
is a compare-and-set on the same predicate, so a Course another process moved between plan and apply
is left alone rather than written over.

`--report` is the default and provably cannot write: the plan is computed inside a transaction that
is always rolled back — the same guarantee the Academic Catalog importer gives.

**Rollback:** no schema changed, so there is nothing to roll back at the schema level. The legacy
revision columns are never cleared, so a migrated Course retains its full legacy history and remains
readable.

## 5. Two real defects the tests caught

Recorded because they are the reason the tests exist, not despite it.

1. **`audit_events.module` is NOT NULL.** My first audit insert hand-wrote the SQL and omitted the
   module the catalog audit contract requires. Fixed to name `CATALOG_AND_AUTHORING`.
2. **Codeless-term panic.** A Course whose legacy Subject term carries a NULL `academic_code` reached
   `subjectCodes[0]` on an empty slice. The codeless check now precedes the index.

## 6. Test results — all observed

```
go build ./...                    clean
go vet ./...                      clean
go vet -tags=integration ./...    clean
go test ./...                     27 packages ok, 0 failures

integration (real PostgreSQL):
  internal/legacymigrate           ok    7.090s
  internal/catalog                 ok   94.687s
  internal/db                      ok   59.962s
  internal/catalogpublic           ok    5.295s
  internal/academic                ok   52.060s
  internal/academic/importer       ok   30.453s
  internal/academic/manifest       ok    0.140s

frontend:
  npm run typecheck                clean
  npm test                         347 passed / 0 failed
```

Migration coverage, every case against real PostgreSQL: exact translation migrates · unmapped legacy
code · legacy term with no code · Course with no Subject term at all · mapped Subject retired ·
mapped Subject absent from the Institution · revisions disagreeing about the Subject · report writes
nothing · apply migrates only exact Courses · **published Course keeps its id, live revision pointer,
lifecycle, revision count, and full legacy history** · mapped Major becomes one audience target ·
rerun migrates nothing and reports `ALREADY_ACADEMIC` · exactly one audit row per migrated Course ·
migrated published Course immediately inherits the T4 Subject lock · ambiguous mapping files rejected
at load · unknown Institution fails closed · path traversal and unknown identifiers rejected.

## 7. CLI proven against a real database

A real legacy corpus was seeded into a freshly migrated database and the real CLI run:

```
$ catalog-migrate --mapping kuwait-university-legacy-v1
mapping:     kuwait-university-legacy-v1 1.0.0
institution: kuwait-university
mode:        REPORT (nothing was written)

  UNMAPPED         22220000-…-000000000001  legacy Subject code 0418-320 is not in the mapping

migrate=0 unmapped=1 ambiguous=0 ineligible=0 already-academic=0
```

That is the correct and honest behaviour: the checked-in mapping is empty, so nothing is translated
and nothing is written.

## 8. Why the checked-in mapping is empty — and what remains

`data/kuwait-university-legacy-v1.yaml` ships with no entries. The mapping is a **Founder product
decision** about what each legacy term means, and inventing entries would be exactly the silent
identity invention the design forbids. The *mechanism* is proven; the *content* is pending.

**Remaining Founder action:** populate the mapping for the real legacy Course(s), run `--report`,
review, then `--apply`.

## 9. What is NOT done — stated plainly

- **The cutover has not been executed.** No production Course has been migrated, because the mapping
  is empty by design. `MVP-F21` is therefore **not** `E2E_PROVEN`.
- **No Admin UI.** The canonical T5 spec is a CLI plus a mapping file; resolution of an
  `UNMAPPED`/`AMBIGUOUS` Course is an ops action (edit mapping, re-run), not a screen. Adding an Admin
  migration console would be scope beyond the canonical specification.
- **No dedicated T5 browser E2E.** With no UI surface there is no browser journey to drive. The
  migration is proven at the integration layer against real PostgreSQL.
- **No canonical Playwright run for T5.** T5 changed no frontend file and no HTTP route; the last
  canonical result stands from the T4 review.
- **Legacy schema removal is not in this tranche**, per D-091 §13.

## 10. Repository state

Protected dirty baseline preserved. **No frontend file was changed** — verified by timestamp. No
package-wide formatting. No legacy route, column, table, or enum was removed. No existing test was
weakened.

Files added: `internal/legacymigrate/{mapping,planner,queries}.go`,
`internal/legacymigrate/data/kuwait-university-legacy-v1.yaml`,
`internal/legacymigrate/migrate_integration_test.go`, `cmd/catalog-migrate/main.go`.
Files modified: `go.mod`/`go.sum` are unchanged — the mapping reuses the `goccy/go-yaml` dependency
the Academic Catalog manifest already carries rather than adding a second YAML library.
