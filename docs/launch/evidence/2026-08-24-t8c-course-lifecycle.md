# T8C / MVP-F24C — AD-12 Course Lifecycle Evidence Closure

**Date:** 2026-08-24
**Branch:** `ui-antigravity-20260817`
**Tranche:** T8C / MVP-F24C — AD-12, Course lifecycle
**Verdict:** PROVEN

---

## 1. Founder authorization

Authorized as a fresh-session continuation tranche under
[D-089](../../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).
Scope: prove the Course lifecycle controls recorded as tracker row **AD-12** end to end — Admin
browser UI → real API → real PostgreSQL lifecycle state → real public catalogue effect → real
entitled-Student effect → audit at the appropriate layer. T8A and T8B were not reopened; GAP-06 was
not started; nothing was deployed.

## 2. Row identity — tracker AD-12, not a SCREENS identifier

**AD-12** here is the row in [`docs/mvp/FUNCTIONAL_COMPLETION.md`](../../mvp/FUNCTIONAL_COMPLETION.md)
§4.3: *"Delist / relist / retire / archive / suspend / restore"*. It is unrelated to any similarly
numbered screen in `SCREENS.md`. The suspension in this row is **Course access suspension**; Staff
suspension belongs to AD-13 and was proved by T8B.

## 3. Pre-T8C assessment

Going in, the expected classification was `A — ALREADY_IMPLEMENTED_NEEDS_E2E`. It held for the
backend and **failed for the product surface**:

| Layer | Finding | Class |
|---|---|---|
| Domain (`internal/catalog`) | delist, relist, retire, archive, suspend, restore all implemented, audited, transactionally locked | A |
| HTTP (`/api/v1/admin/courses/:id/...`) | all six routes mounted under `CapCatalogPublish` | A |
| API client (`lib/api/catalog.ts`) | all six wrappers present and unit-tested | A |
| Admin UI | `lifecycle-controls.tsx` existed but was **mounted on no page** — dead code, and `admin-catalog-surface.test.ts` asserts the review queue must *not* carry it | **C — incomplete** |
| Admin read surface | **absent**: no endpoint lists Courses in non-published states, so a delisted Course could not be found to relist it | **C — incomplete** |

The capability was therefore unreachable through the product. Under CLAUDE.md's current-phase rule
("Functional UI changes are authorized only when a canonical capability is otherwise unreachable
through the product"), the minimum surface needed to reach the existing commands was built. **No
lifecycle policy was changed.**

## 4. Canonical transition matrix

Derived from `catalog/course.go`, `catalog/suspension.go`, `catalogpublic.PublishedOnly`,
`internal/entitlement/evaluate.go`, and BR-027.

| Action | From | Effect | Reversible? | Public catalogue | Existing entitled Student |
|---|---|---|---|---|---|
| Delist | `PUBLISHED` | `lifecycle = DELISTED` | yes — relist | removed | **unchanged** (lifecycle is not an access input) |
| Relist | `DELISTED` | `lifecycle = PUBLISHED` | yes — delist | returns | unchanged |
| Archive | `PUBLISHED` / `DELISTED` | `lifecycle = ARCHIVED` | **no — terminal**; no un-archive route exists and `allowsLifecycleTransition` refuses every target | removed | unchanged |
| Retire | `PUBLISHED` / `DELISTED`, `retired_at IS NULL` | `retired_at = now()`; lifecycle untouched | **no** — a second retirement is a `LifecycleConflictError` | removed | keeps access when the Entitlement's `retirement_eligibility_at` **precedes** `retired_at` (BR-027) |
| Access suspend | any, `access_suspended_at IS NULL` | `access_suspended_at`, `access_suspension_reason`; outbox `catalog.course_access_suspended` | yes — restore | removed | **denied**, `COURSE_ACCESS_SUSPENDED`; Entitlement untouched |
| Access restore | `access_suspended_at IS NOT NULL` | both columns cleared; outbox `catalog.course_access_restored` | yes | returns if the other publication conditions hold | access returns; no new grant |

Public visibility is a single predicate — `c.lifecycle = 'PUBLISHED' AND c.access_suspended_at IS
NULL AND c.retired_at IS NULL AND c.live_revision_id = cr.id` — so no lifecycle action can hide a
Course from one public path and not another.

**Retirement eligibility (BR-027)** is a property of the *Entitlement*, not a precondition on the
Course: `RetireCourse` requires only `PUBLISHED`/`DELISTED` and a null `retired_at`. The evaluator
then allows a Student through only when `retirement_eligibility_at < retired_at`. Nothing in the
retirement path was relaxed to make the happy path easier.

## 5. Existing backend proof (not duplicated in the browser)

- `catalog/lifecycle_integration_test.go` — the transition graph, refusal of `PUBLISHED → DRAFT`,
  retirement, owner reassignment with a pending candidate, and that no lifecycle action rewrites
  revisions, prices or access records.
- `TestLifecycleCompatibilityStateKeepsExistingAccessUntilEmergencySuspension` — delist, relist,
  retire and archive leave existing access intact; only suspension changes it; restoration reverses
  it; the access fixture is never rewritten.
- `httpapi/privileged_audit_integration_test.go` — one immutable audit row per lifecycle route
  (`COURSE_DELISTED`, `COURSE_RELISTED`, `COURSE_ARCHIVED`, `COURSE_RETIRED`,
  `COURSE_ACCESS_SUSPENDED`, `COURSE_ACCESS_RESTORED`) with actor, target, reason and resulting state.
- `httpapi/authorization_test.go` — the derived Admin-route sweeps refuse an Instructor, a suspended
  account, a restricted principal and an unknown principal on every mounted Admin route, including
  the new directory read.

## 6. What was added (production)

1. **`GET /api/v1/admin/courses`** (`CapCatalogPublish`) → `catalog.ListCourseLifecycleDirectory`.
   Returns id, both titles, owner display name, lifecycle, `access_suspended_at` (+ reason) and
   `retired_at` for Courses in **every** state, optionally filtered by a title substring, capped at
   50 rows. This is the read the public catalogue must never be: it is what makes a delisted,
   retired or archived Course reachable by the Admin who has to act on it.
2. **`/[locale]/admin/course-lifecycle`** — `CourseLifecycleWorkspace`. Search by title, pick a
   Course by name (no UUID is typed anywhere in the journey), see its current state, and issue the
   six existing commands. Every command is followed by a directory refetch, so what the screen shows
   is the server's state; a refusal renders the server's problem detail. The dead
   `lifecycle-controls.tsx` was removed, its capabilities preserved by the new workspace.
3. **Admin navigation** — a `Course Lifecycle` / `حالة المقررات` item, so the surface is reachable
   by ordinary navigation rather than only by URL.

Owner reassignment (`POST /admin/courses/:id/owner`) is outside the AD-12 row and was not carried
into the new workspace; the route and its API client are unchanged.

## 7. Fixture design

`seedT8CLifecycleFixtures` (backend/cmd/e2e-seed) seeds **four** published Courses — one per case —
each with an approved live revision promoted to `live_revision_id`, a price, one section and one
lesson:

| Course | Case |
|---|---|
| `T8C Delist Relist Course` | A — delist / relist |
| `T8C Access Suspension Course` | B — access suspend / restore (+ entitled Student) |
| `T8C Retirement Course` | C — retire, then refused second retirement |
| `T8C Archive Course` | D — archive, terminal |

Case B additionally seeds its own Student (`t8c-suspension-student@example.test`), an APPROVED
invitation, one `ACTIVE` course-scoped Entitlement, an Enrollment and a Progress row. No rotating
slot was consumed and `ROTATING_TEST_SLOTS` is unchanged at 34; T8A's fixtures were not reused.

**No fixture seeds the state under test.** No row is written with `lifecycle = 'DELISTED'`, with
`access_suspended_at`, or with `retired_at`; the Admin UI performs every mutation being proved.

## 8. Case A — delist and relist (browser)

Anonymous visitor searches the public catalogue by title → the Course is discoverable. Admin opens
the lifecycle workspace, finds the Course **by title**, sees `PUBLISHED`, presses **Delist**. After a
full page reload and a re-read of the directory the Admin surface shows `DELISTED`, and the visitor's
catalogue search returns nothing. Admin presses **Relist**; the refetched state is `PUBLISHED` and
the visitor sees the Course again — as exactly one listing (`toHaveCount(1)`), not a duplicate beside
a stale card. The Course, its identity and its revision are the same throughout; nothing is recreated.

## 9. Case B — Course access suspension and restoration (browser)

Before: the entitled Student is learning (`data-learning-status="active"` on the protected Course
home) and the Course is publicly discoverable.

Admin suspends with cause `SECURITY` and the reason `T8C E2E course access suspension`. On the
refetched Admin surface the Course is **still `PUBLISHED`** with `data-access-suspended="true"` —
suspension is orthogonal to the presentation lifecycle. The visitor's catalogue search returns
nothing. The Student's Course home renders *Learning is unavailable* and no active badge.

PostgreSQL, read through the seeder's learning-state query at that moment: the same Entitlement id,
still `ACTIVE`, `revoked_at` null, the same `access_ends_at`, the same Enrollment id, and the same
number of Progress rows. Suspension denied a read; it rewrote no access record.

Admin restores with the reason `T8C E2E course access restoration`. The refetched state clears the
suspension, the Course returns to the public catalogue, and the Student is learning again — under
the **same** Entitlement id and the same Enrollment, with Progress intact. No grant was re-issued.

## 10. Case C — retirement (browser)

The Course is publicly discoverable, then the Admin presses **Retire**. The refetched Admin surface
shows `data-retired="true"` while the lifecycle stays `PUBLISHED`: retirement records the
future-acquisition boundary, it is not a presentation transition. The Course leaves the public
catalogue, so it can no longer be discovered or requested by a new Student.

A second retirement attempt on the same Course is **refused** — the workspace renders the server's
409 and shows no success message, and the Course's state is unchanged afterwards. The screen stays
coherent; there is no generic 500. The equivalent domain-level refusals (`PUBLISHED → DRAFT`,
relisting an archived Course, retiring twice) are proved in PostgreSQL by the integration tests
in §5 and §12.

## 11. Case D — archival (browser)

The Course is publicly discoverable, then the Admin presses **Archive**. The refetched state is
`ARCHIVED` and the Course is gone from the public catalogue. Archival is **terminal**: there is no
canonical un-archive route, and pressing **Relist** on the archived Course is refused with the
Course still `ARCHIVED` afterwards. No un-archive UI was invented. The Course is not deleted — it is
still reachable by name on the Admin surface after archival, with its revision and history intact.

## 12. Backend proof added

`catalog/lifecycle_directory_integration_test.go` (PostgreSQL, `-tags=integration`): one Course is
carried through published → delisted → suspended → restored → retired → archived, and the Admin
directory returns it, with the correct state and reason, at **every** step; relisting the archived
Course is refused; a non-matching search returns an empty directory rather than the whole catalogue.

## 13. Production defects found

**None in the lifecycle domain.** The two gaps closed here were reachability gaps, not behavioural
ones: the lifecycle commands were correct and unreachable. No `T8C-REMEDIATION-nn` behavioural fix
was required, and no lifecycle rule, refusal or audit was altered.

## 14. Authorization and audit

Authorization is unchanged: every lifecycle mutation stays behind `CapCatalogPublish` plus the
session mutation-security middleware, and the new directory read joins the same capability. The
derived authorization sweeps (Instructor, suspended account, restricted bootstrap principal, unknown
principal) cover the new route automatically and are green; a new matrix row records it as
`CAPABILITY_PROTECTED`. Audit is unchanged and already proved per route in
`privileged_audit_integration_test.go`; the browser does not re-assert audit internals because the
Admin UI exposes none.

## 15. Invariants held

- Course identity is preserved by every action: no Course, revision, media asset or price row is
  created, copied or deleted by a lifecycle command.
- Entitlements are never rewritten by a lifecycle command — not revoked on delist, not shortened on
  suspend, not re-issued on restore or relist.
- Student Progress, Enrollment and purchase history survive suspension and restoration.
- T6 discovery stays correct: the public effects were proved through the real catalogue search, by
  human-readable title, never by reading the database.
- T7's narrowed learning payload is untouched: the suspended Student renders the existing generic
  unavailable state, and no new props were added to it.

## 16. Repository safety

No `git reset`, `clean`, `stash`, `restore`, broad checkout, or repo-wide formatting. No
`docker compose down -v`, no volume removal, no `DROP`/`TRUNCATE` against a retained database; every
Playwright run used its own disposable database and the standard teardown, which continues to stop
T8B's worker. Unrelated working-tree changes were preserved. `git diff --check` is clean.

## 17. Known unrelated observations (not acted on)

- `courses.default_access_ends_at IS NULL` → `ConfirmPurchaseRequest` answers **500** instead of
  `ErrExpiryRequired` / 409. T8C used seeded, test-owned Courses and Entitlements and did not need
  the purchase path. Recorded as a known unrelated defect.
- The Admin screen still renders no pending Staff-invitation list or revocation control, though
  `GET /v1/staff-invitations` and the strings exist (first recorded by T8B). Not T8C; not fixed.
- `handleLifecycleError` maps a `LifecycleConflictError` to `problem.StateConflict()` ("the resource
  changed while this request was in flight"), whereas `problem.UnsupportedStateTransition()` exists
  for a permanently illegal command. Both are 409 and the Admin surface stays coherent, so nothing
  was changed here; recorded for a future product-copy decision.

## 18. Tests

**Backend gates** — all clean:

```
go build ./...            OK
go vet ./...              OK
go vet -tags=integration ./...  OK
go test ./...             28 packages ok, 0 failures
```

Course-lifecycle integration (real PostgreSQL), reported separately:

```
go test -tags=integration ./internal/catalog/ \
  -run 'TestLifecycleDirectory|TestCourseLifecycle|TestLifecycleCompatibility'   ok (3.9s)
go test -tags=integration ./internal/httpapi/ -run 'PrivilegedAudit|Authorization'  ok (4.4s)
```

**Frontend unit:** `npm run typecheck` clean; `npm test` → **379 passed** (unchanged: the added
surface is proved in the browser, and the repository has no React component test harness). The
navigation unit test was extended for the new Admin item.

**Focused T8C Playwright** (`npx playwright test e2e/t8c-course-lifecycle.spec.ts --workers=1`):

```
✓ A an Admin delists a published Course and relists it
✓ B an Admin suspends Course access, the entitled Student is blocked, and restoration returns access
✓ C an Admin retires a Course and a second retirement is refused
✓ D an Admin archives a Course and the archived state is terminal
4 passed (1.2m)
```

No case was skipped or weakened to obtain a green run.

## 19. Canonical regression

One uncontended run, `npx playwright test --workers=1`:

| | Before T8C | After T8C |
|---|---|---|
| passed | 160 | **164** |
| failed | 1 | **1** |
| did not run | 3 | **3** |

The single failure is the unchanged, pre-existing
`s5-playback-performance.spec.ts:157` — *T076 — SC-001 time-to-first-frame, phone (390x844)* — the
production-build precondition, together with the same three non-runs from that spec. **No new
failure identity appeared.** T5, T6, T7, T8A and T8B all stayed green; 160 + 4 = 164.

## 20. Manual acceptance

**Not separately performed.** `globalTeardown` destroys the API, the worker and the disposable
database at the end of every run, and keeping a persistent stack alive would have required
manufacturing credentials the current authority does not grant. The four cases are real-browser
evidence driven through the production HTTP path — they are not a claim of a human walk.

## 21. Files changed

**Backend**
- `internal/catalog/lifecycle_directory.go` *(new)* — `ListCourseLifecycleDirectory`
- `internal/catalog/lifecycle_directory_integration_test.go` *(new)* — PostgreSQL proof
- `internal/httpapi/admin_lifecycle_handlers.go` — `directory` handler
- `internal/httpapi/catalog_routes.go` — `GET /admin/courses` under `CapCatalogPublish`
- `internal/httpapi/authorization_test.go` — matrix row for the new route
- `cmd/e2e-seed/t8c_lifecycle_fixtures_test.go` *(new)* — four lifecycle Courses + entitled Student
- `cmd/e2e-seed/seed_test.go` — calls the T8C fixtures

**Frontend**
- `src/components/admin/course-lifecycle-workspace.tsx` *(new)* — the Admin lifecycle surface
- `src/components/admin/lifecycle-controls.tsx` *(removed)* — dead, unmounted predecessor
- `src/app/[locale]/admin/course-lifecycle/page.tsx` *(new)*
- `src/lib/api/catalog.ts` — `getCourseLifecycleDirectory`
- `src/components/layout/role-workspace-navigation.ts` + `.test.ts`,
  `src/components/layout/role-workspace-shell.tsx`, `src/lib/i18n/dictionaries/{en,ar}.ts` — nav item
- `e2e/t8c-course-lifecycle.spec.ts` *(new)* — the four browser cases

**Docs**
- this evidence record; `docs/mvp/FUNCTIONAL_COMPLETION.md` §28 and the AD-12 row

## 22. Matrix impact

**AD-12** is promoted from `IMPLEMENTED_NOT_PROVEN` to `E2E_PROVEN`. The denominator stays **53**.
No other row moved; **GAP-06** and **GAP-08** are untouched, and SWE101 was not touched.

| | Before | After |
|---|---|---|
| `E2E_PROVEN` | 43 / 53 = 81.1% | **44 / 53 = 83.0%** |
