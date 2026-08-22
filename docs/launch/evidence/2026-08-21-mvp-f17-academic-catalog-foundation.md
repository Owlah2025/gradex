# MVP-F17 / T1 — Academic Catalog Foundation — completion evidence

**Recorded:** 2026-08-21
**Authority:** [D-091](../../DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy)
under [D-089](../../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).
**Design:** [2026-08-21-academic-catalog-taxonomy-redesign.md](../../superpowers/specs/2026-08-21-academic-catalog-taxonomy-redesign.md) (revision 2, D-A…D-D applied).
**Seat:** Claude held the **builder** seat by Founder reassignment. The audit in §9 is a
**builder self-audit and is not independent review.** T1 stops here for external review; T2 has not
started.

T1 is a **foundation tranche**. It creates no canonical denominator feature row, promotes none, and
does not move the MVP score, which remains `37 / 53 = 69.8%`.

---

## 1. Scope proven

An Admin-only, Institution-scoped Academic Catalog exists end to end: schema, domain, semantic API,
Admin surface, and one real browser journey. It is **strictly additive**. The legacy
`taxonomy_terms` / `taxonomy_kind` / `study_year` / `major_term_id` / `subject_term_id` model remains
operational and remains authoritative for Course classification. No Course read or write path was
switched. `courses.subject_id` was deliberately **not** added — that column belongs to T4/T5.

## 2. Migration proof

Migration `0023_academic_catalog` (`AcademicCatalogSchemaVersion = 23`, `MaxSchemaVersion = 23`).

```text
cd backend && go test -tags=integration ./internal/db -run 'TestAcademicCatalog|TestMaxSchemaVersionTracksAcademicCatalogSchema' -count=1
  ok  github.com/Owlah2025/gradex/backend/internal/db  3.300s
```

Proved by `internal/db/academic_catalog_migration_integration_test.go`:

| Property | Assertion |
|---|---|
| clean install | all six tables created from empty |
| `up → down → up` | down removes only T1-owned schema; re-apply restores it |
| additive | `taxonomy_terms`, `courses`, `course_revisions` intact; `major_term_id`, `subject_term_id`, `study_year` all still present |
| no premature Course link | `courses.subject_id` asserted **absent** |
| shared function safety | `catalog_normalize_ar` (owned by 0011) survives down; `academic_normalize_code` (T1-owned) is removed |
| existing data safety | a real legacy Course + revision + taxonomy term seeded at v22, migrated to v23, re-read unchanged, then rolled back with the row surviving |
| cross-Institution refusal | unit parent, program owning unit, curriculum program, curriculum-subject mapping |
| cycle refusal | self-parent and multi-node `A → B → C → A` |
| one ACTIVE curriculum | second ACTIVE insert refused |
| Subject dedupe | duplicate normalized code refused; same code in another Institution accepted; code-less duplicate title refused in either language |
| level bound | level 5 accepted where the institution declares five; level 9 refused |

## 3. Backend gates

```text
cd backend && go build ./...            BUILD OK
cd backend && go vet ./...              VET OK
cd backend && go test ./... -count=1    26 packages ok, 0 failures
cd backend && go test -tags=integration ./... -count=1 -timeout=1800s
  exit 0 — 29 packages ok, 0 failures
```

Focused T1 suite — `internal/httpapi/academic_catalog_integration_test.go`, real HTTP + real
PostgreSQL, **27/27 green** (25 subtests + 2 top-level):

```text
cd backend && go test -tags=integration ./internal/httpapi -run TestAcademicCatalog -count=1
  ok  github.com/Owlah2025/gradex/backend/internal/httpapi  2.209s
```

Covered: empty catalog · Institution create · Instructor refused (read **and** write) · Student
refused · anonymous refused · nested hierarchy · rename/detach/re-attach · cross-Institution parent
refused · self-parent refused · two-node and three-node cycles refused · Program ownership ·
cross-Institution Program refused · one ACTIVE Curriculum · explicit supersession retains the prior
version · coded Subject dedupe across four punctuation variants **with the existing Subject named** ·
same code in another Institution allowed · code-less dedupe in both languages · **8-way concurrent
duplicate creation yields exactly one row** · code-and-title search · same-Institution mapping ·
one canonical Subject serving two Programs · cross-Institution mapping refused · level bound ·
retirement preserves history and blocks new mapping · referenced unit cannot be retired · every
mutation audited and **no audit written by a non-Admin actor** · unmapping preserves the Subject ·
no academic foreign key into `courses` / `course_revisions` / `entitlements` / `enrollments`.

### Two real defects found and fixed by these gates

1. **Duplicate conflicts were unactionable.** The conflicting-Subject lookup ran inside the
   transaction that had just violated the unique index. PostgreSQL aborts a transaction on
   constraint violation, so the lookup always failed and the Admin received a generic
   `STATE_CONFLICT` with no indication of what it collided with. Fixed by resolving the conflict on
   a fresh connection after the transaction unwinds (`internal/academic/subjects.go`).
2. **Every frontend catalog call would have 404ed.** `authenticatedRequest` already prefixes
   `/api/v1`; the client's base path repeated it, producing `/api/v1/api/v1/admin/academic/...`.
   Caught by the frontend route assertions, fixed in `frontend/src/lib/api/academic.ts`.

## 4. Frontend gates

```text
cd frontend && npm run typecheck   PASS
cd frontend && npm test            311 passed / 0 failed
```

New: `frontend/src/components/admin/academic-catalog.test.ts` — behavioural route/CSRF/body
assertions against a stubbed transport, plus structural guards that hold for the shipped source:
no rendered identifier, no `uuid`, no legacy taxonomy vocabulary in user-facing copy, every empty
state handled, no hardcoded institution, no reintroduced `PREP`/`YEAR_1`–`YEAR_4`, and the level
input bounded by the selected institution's own maximum.

## 5. T1 E2E

`frontend/e2e/t1-admin-academic-catalog.spec.ts` — real browser, real API, real PostgreSQL, isolated
per-run test data, **no Kuwait University catalog seeded**.

```text
cd frontend && npx playwright test e2e/t1-admin-academic-catalog.spec.ts --reporter=line
  4 passed (28.1s)
```

- **A** — Admin opens the Academic Catalog → creates a university (five levels) → college →
  department nested under it → major owned by the department → canonical Subject `0410-101 ·
  Calculus I` → study plan `2026` → maps the Subject as `Major core` at recommended level 1 → the
  resulting hierarchy is visible → **no UUID appears anywhere in the rendered body** → the server is
  re-queried and agrees.
- **B** — creating `0410101` is refused and the surface **names the existing `0410-101 · Calculus I`**;
  no second row is created.
- **C** — an Instructor is refused on read (403) and write (403), and no catalog data leaks into the page.
- **D** — the legacy taxonomy API and the legacy Admin taxonomy surface still work alongside the new catalog.

## 6. Full canonical regression

```text
cd frontend && npx playwright test --reporter=line
  122 passed
  6 failed
  3 did not run
  duration 8.1m
```

Configuration: local `backend/docker-compose.yml` stack (PostgreSQL 16, Redis 7, MinIO, Mailpit),
Playwright 1.62.0 + Chromium, 1 worker, branch `ui-antigravity-20260817` with its protected
uncommitted working tree in place.

**Baseline comparison.** Canonical baseline before this tranche was `118 passed / 6 failed / 3 did
not run`. T1 adds exactly its own 4 tests → **122 passed**. The failure set is byte-for-byte the six
known accepted identities, with **no new failure identity**:

```text
s5-expired-entitlement.spec.ts:712  — cache/stale-state
s5-playback-performance.spec.ts:157 — phone time-to-first-frame
s5-viewport-evidence.spec.ts:223    — phone
s5-viewport-evidence.spec.ts:223    — tablet
s5-viewport-evidence.spec.ts:223    — laptop
s5-viewport-evidence.spec.ts:223    — desktop
```

## 7. Protected feature regression

All inside the 122-pass run above, all green:

| Feature | Spec | Result |
|---|---|---|
| ST-19 manual purchase | `manual-purchase-flow.spec.ts` | pass |
| S6 course access grant | `s6-course-access-grant-launch.spec.ts` (30-step journey + rejection/expiry) | pass |
| F14 public preview | `media-authoring/` preview coverage | pass |
| ST-15 protected materials | `media-authoring/s15-protected-materials.spec.ts` | pass |
| IN-09 video upload → READY | `media-authoring/s12-instructor-video-upload.spec.ts` | pass |
| Instructor authoring | `s12-instructor-authoring.spec.ts` | pass |
| Admin Course review | `s14-admin-catalog-review.spec.ts` | pass |
| Public catalogue | `s3-public-catalogue.spec.ts` | pass |
| Legacy taxonomy viewport | `s2-taxonomy-viewport.spec.ts` | pass |

No contract of any of these was changed by T1.

## 8. Test assertions updated (and why)

Four existing assertions were corrected because T1 legitimately changed what they name. None was
weakened; each still asserts the same property.

| File | Change |
|---|---|
| `internal/identity/policy_test.go` | The closed capability set gained `ACADEMIC_CATALOG`; all three role expectation maps updated, and the per-role totality assertion still requires exact coverage |
| `internal/db/migrate_integration_test.go` | `TestMaxSchemaVersionTracksRevisionScopedPublicPreviewSchema` → `…TracksAcademicCatalogSchema`, plus the 22→23 chain link |
| `internal/db/migrate_integration_test.go:711` | Refused-rollback marker now compares to `MaxSchemaVersion` rather than the literal-by-constant `RevisionScopedPreviewSchemaVersion`; the property ("a refused rollback leaves the fully-migrated marker clean") is unchanged |
| `cmd/migrate/manual_purchase_rollback_integration_test.go:81` | Same correction, same reason |
| `components/layout/role-workspace-navigation.test.ts` | Admin navigation gained the Academic Catalog entry; both the `en` and `ar` expectations updated and still asserted exactly |

## 9. Builder self-audit

**This is `BUILDER_SELF_AUDIT`. It is not independent review.** See the tranche report for the full
check list and the two findings that were fixed before this record was written.

## 10. Repository state

The protected uncommitted working tree was preserved throughout. No `reset`, `stash`, `restore`,
`clean`, or broad `checkout` was run, and no unrelated file was normalized. Only files owned by this
tranche were edited, plus the five assertion corrections in §8.
