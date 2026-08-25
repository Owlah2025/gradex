# T8A / MVP-F24A — AD-09 Admin Entitlement Lifecycle Evidence Closure

Date: 2026-08-23
Branch: `ui-antigravity-20260817`
Tranche: T8A / MVP-F24A
Register row: **AD-09 — Entitlement extend / shorten / revoke**

---

## 1. Founder authorization

Continuation of the already-authorized T8A tranche under
[D-089](../../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).
The authorization scoped this session to *finishing* T8A: implement the browser journey, run it, fix
real failures, and record proof. It explicitly forbade re-running the previous session's
implementation audit, redesigning the Entitlement system, and starting AD-13 or AD-12.

It also directed that negative cases already covered at the backend layer (unauthorized actor, stale
revision) stay there rather than being duplicated in the browser to inflate a case count.

## 2. AD-09 authority

[BR-026](../../BUSINESS_RULES.md) is authoritative and unchanged. There is **one** expiry-adjustment
operation, not separate extend and shorten endpoints:

```
PUT  /api/v1/admin/entitlements/:id/expiry
POST /api/v1/admin/entitlements/:id/revocation
```

Direction is a consequence of the submitted date. A later date extends, an earlier future date
shortens, and a date already past ends access immediately while keeping Enrollment, Progress and
history.

Date semantics are the server's. `access.ConvertKuwaitDateToUTCBoundary` maps calendar date `D` to
the start of `D+1` in Asia/Kuwait expressed as UTC, so `2027-06-30` becomes `2027-06-30T21:00:00Z`.
This is intentional and was **not** modified; the new E2E asserts against that canonical instant
rather than against a browser-local midnight.

## 3. Previous-session handoff — what was reused, what proved stale

Reused without re-derivation, and confirmed correct against current code:

| Handoff fact | Status this session |
|---|---|
| AD-09 classified `A — ALREADY_IMPLEMENTED_NEEDS_E2E`; no production defect | Confirmed. No production code changed. |
| Backend integration coverage is comprehensive and green | Confirmed, re-run below. |
| Kuwait boundary conversion is intentional | Confirmed; asserted, not altered. |
| Past-dating is supported and must not gain an `expiry >= now` guard | Confirmed; Case C exercises it. |
| Admin reaches the Entitlement from the invitation row; no UUID paste | Confirmed. `manage-access-{invitationId}` renders when `inv.entitlement_id != nil`. |
| The rotating-Student seed already provides Student + APPROVED invitation + ACTIVE Entitlement + Enrollment | Confirmed. |
| Rotating slot map was full at 30 slots | Confirmed. |
| `courses.default_access_ends_at IS NULL` → 500 does not block T8A | Confirmed. Untouched. |

**Two handoff facts proved incomplete**, and both changed the work:

1. **AD-09 was not entirely unproven in the browser.**
   `frontend/e2e/s6-course-access-grant-launch.spec.ts` (steps 27–30) already drove *extension*,
   *shortening* and *revocation* through the real Admin modal, and checked the Student effect through
   the production progress route. What it did **not** cover was the past-date ending (BR-026's
   immediate-end case), the Student's rendered access surface, or a genuinely refetched Admin record.
   The new T8A spec covers all four operations end to end on isolated fixtures and adds those three
   missing dimensions; s6's coverage is complementary, not superseded.

2. **Slot expansion alone would not have made the Admin journey reachable.**
   The Admin Course Access queue reads `ORDER BY i.created_at DESC LIMIT 100`
   (`backend/internal/access/repository.go:560`), and the page requests page 1 at size 100. Every
   rotating-pool invitation was inserted inside one transaction, so all of them carried the single
   transaction timestamp and their relative order was decided arbitrarily by the query plan. The new
   high-index T8A slots would normally have fallen outside the first page, making the Admin journey
   non-deterministic. This is a **fixture** defect, not a product defect, and is fixed in the seed.

## 4. Rotating slot expansion

| | Before | After |
|---|---|---|
| `rotatingTestSlots` / `ROTATING_TEST_SLOTS` | 30 | **34** |
| `rotatingStudentPoolSize` / `ROTATING_POOL_SIZE` | 300 | **340** |
| `rotatingMaxRepeats` / `ROTATING_MAX_REPEATS` | 10 | 10 (unchanged) |
| expired pool / slots | 100 / 8 | 100 / 8 (unchanged) |

Slots allocated, one per BR-026 operation so no case observes another's mutated Entitlement:

| Slot | Constant | Case |
|---|---|---|
| 30 | `ENTITLEMENT_EXTEND_TEST_SLOT` | A — expiry extension |
| 31 | `ENTITLEMENT_SHORTEN_TEST_SLOT` | B — shortening, access stays open |
| 32 | `ENTITLEMENT_PAST_DATE_TEST_SLOT` | C — expiry moved into the past |
| 33 | `ENTITLEMENT_REVOKE_TEST_SLOT` | D — revocation |

**The allocation invariant is preserved.** Allocation remains `slot * repeats + repeat` and depends
on no pool size, so slots 0–29 keep indices 0–299 exactly and the new slots take 300–339. No existing
slot was reassigned and no existing name was moved.

That is asserted, not asserted-by-comment. `src/lib/api/e2e-students.test.ts` gained
`rotating pool: the T8A expansion reassigns no existing execution`, which walks every pre-existing
slot at every supported repeat and pins its index, then pins the four new slots to 300/310/320/330
and the last repeat to 339.

## 5. Seed synchronization (TypeScript ↔ Go)

Both mirrors were updated together; there is no third source of truth.

- `frontend/src/lib/api/e2e-students.ts` — constants, slot-map comment, four new exported slots.
- `frontend/e2e/rotating-students.ts` — re-exports the four new slots to the Playwright surface.
- `backend/cmd/e2e-seed/rotating_students_test.go` — `rotatingTestSlots = 34`,
  `rotatingStudentPoolSize = 340`, header comment recording slots 30–33 and the index range 300–339.

The Go seeder's own guard (`rotatingStudentPoolSize < rotatingTestSlots*rotatingMaxRepeats` returns
an error) and the TypeScript sizing test both hold at the new values. The TypeScript collision proof
now enumerates 27 active executions per repetition (was 23) and still finds no two executions sharing
a Student.

### Queue-ordering fixture correction

`seedRotatingStudents` / `seedRotatingExpiredStudents` now set `course_access_invitations.created_at`
explicitly, derived from the pool index:

```
created_at = now - (offsetSeconds + (poolSize - 1 - index)) seconds
```

with `rotatingActiveQueueOffsetSeconds = 60` and `rotatingExpiredQueueOffsetSeconds = 600`, so:

- every timestamp is strictly in the past — nothing is dated into the future;
- higher index is newer, so **growing a pool never reorders the rows already below it**, matching the
  allocation invariant;
- the two pools occupy disjoint windows and never interleave;
- the T8A slots (300–339) are the newest 40 rows of the rotating set and land well inside the Admin
  queue's first page.

No production code is involved. No existing test asserts a rotating invitation's `created_at`.

## 6. Case A — extension

`frontend/e2e/t8a-entitlement-lifecycle.spec.ts`, slot 30.

Admin journey, in the real browser against the real Admin UI:

1. Student is confirmed learning first, so the "after" answer is meaningful — `/en/learn/courses/…`
   renders the Course heading and the `data-learning-status="active"` badge reading `Active access`.
2. Admin opens `/en/admin/course-access`, finds the row by the Student's **email**, and presses
   **Manage access**. No Entitlement, Enrollment, Course or Account identifier is typed anywhere in
   this journey.
3. The record shows `ACTIVE` and the current access-end date.
4. A later date (run instant + 365 days), reason `T8A E2E entitlement extension`, reference
   `T8A-EXTEND`, submitted through the real form.
5. Success notice `Access expiry updated`, and `entitlement-error` absent.
6. **The record is refetched**: the page is reloaded and reopened from the queue, so the assertion is
   about what a later read returns, not about the mutation's own echoed response.
7. The refetched screen shows the canonical Kuwait boundary date, and the stored `access_ends_at`
   equals `kuwaitBoundaryInstant(date)` exactly and is strictly later than before.
8. The grant was adjusted, not re-issued: same Entitlement id, `original_access_ends_at` unchanged.

Student effect: protected learning still renders the active access surface.

## 7. Case B — shortening while access stays open

Slot 31. The distinguishing claim is that an earlier date is **not** a revocation.

The new date is run instant + 10 days — earlier than the seeded 30-day window, and far enough from
today that a slow run cannot carry the boundary into the past mid-test. The test asserts that
relationship rather than assuming it.

After the refetch the record is still `ACTIVE`, `entitlement-revoked-at` is absent, the stored
instant is the canonical boundary and is strictly earlier than before — and the Student is **still
learning**, with the active badge rendered.

## 8. Case C — past-date immediate ending

Slot 32.

Progress is created the way the product creates it: the authenticated Student's own browser issues
`PUT /api/v1/learn/lessons/:lessonId/progress` through the production route while access is still
active. Nothing is written by SQL.

The Admin then submits a date 5 days in the past, reason `T8A E2E past-date expiry`, reference
`T8A-PAST`. After the refetch the screen shows the canonical boundary, and the stored instant equals
`kuwaitBoundaryInstant(date)` and is in the past.

Nothing is deleted: same Entitlement id, Enrollment still found, Progress row still present.

Student effect, on the very next navigation: the retained-expired presentation (D-063) —
`data-learning-status="expired"` reading `Access expired`, with **no** `active` badge element present.

### T7 regression re-audited on this surface

Case C re-renders the exact surface MVP-F23 remediated, so it re-audits that boundary:

- `Active access` absent from the rendered text;
- the served payload (normalized, so `report_context`, `reportContext` and `data-report-context` are
  one concept) carries none of `report_context`, `asset_version_id`, `entitlement_id`,
  `enrollment_id`, `revision_id`, `object_key`, `storage_path`, `playback_session`, `can_play`,
  `can_update_progress`, or the runner-only asset version id;
- `No expiry date` — a label rendered by no state this page can be in — is absent, so the whole
  `dictionary.learning` was not handed to a component again.

All green.

## 9. Case D — revocation

Slot 33.

Admin fills the revocation reason `T8A E2E entitlement revocation`, reference `T8A-REVOKE`, ticks the
explicit confirmation checkbox, and submits. Notice: `Course access revoked`.

After the refetch: state `REVOKED`, `entitlement-revoked-at` visible, and **both** the expiry form and
the revoke form are gone — a terminal record offers no further operation.

Persistence: state `REVOKED`, `revoked_at` set, same Entitlement id, Enrollment retained.

Student effect: the generic unavailable state — heading `Learning is unavailable` — and the page names
no cause: `revoked`, `suspended`, `entitlement`, `enrollment` are all absent from the visible text.

The Course itself is untouched: the Case A Student's Entitlement for the same Course is still `ACTIVE`
after the revocation. Nothing was delisted, retired, or removed, and no Student academic profile was
changed.

## 10. Existing PostgreSQL proof — audit, outbox, history

Re-run this session, `go test -tags=integration ./internal/httpapi/`:

```
--- PASS: TestEntitlementExtendKeepsAccessAndRecordsAdjustment (1.29s)
--- PASS: TestEntitlementShortenEndsAccessAtTheNewInstant (1.28s)
--- PASS: TestEntitlementRevocationDeniesAccessAndPreservesHistory (1.26s)
--- PASS: TestEntitlementMutationsRefuseInvalidTransitions (1.23s)
--- PASS: TestEntitlementOperationsOverTheProductionAPI (2.18s)
PASS
```

These establish, against real PostgreSQL and the real production router: the immutable
`entitlement_adjustments` and `audit_events` rows, the `access.entitlement_adjusted` and
`access.entitlement_revoked` outbox events, the revision counter, the exact expiry instant, access
denial after revocation, and Enrollment/Progress preservation.

Those proofs are **not** duplicated in the browser. No browser test inspects an audit row, an outbox
payload, a revision counter, or adjustment-history internals, because no Admin screen exposes them as
part of AD-09.

## 11. Authorization and concurrency — why no browser duplication

The originally listed browser cases E (unauthorized actor) and F (stale revision) were kept at the
backend layer, as the authorization permits.

- **Unauthorized:** `TestEntitlementOperationsOverTheProductionAPI` includes the subtest
  *"a Student cannot adjust or revoke another actor's grant"*, driven over the production HTTP path.
- **Stale revision / invalid transition:** `TestEntitlementMutationsRefuseInvalidTransitions`.

Neither has a user-visible behaviour the Admin UI presents differently from the uniform problem
response, so a browser duplicate would add no product evidence. It was not added.

## 12. Production defects found

**NONE.**

No production code was changed by T8A. The only defect found was in test fixture infrastructure
(§5, queue ordering), which changes no product behaviour.

## 13. Files changed

| File | Change |
|---|---|
| `backend/cmd/e2e-seed/rotating_students_test.go` | slots 30→34, pool 300→340, deterministic invitation `created_at`, comments |
| `frontend/src/lib/api/e2e-students.ts` | mirrored constants, slot-map comment, four new slot exports |
| `frontend/src/lib/api/e2e-students.test.ts` | new slots in the collision proof (23→27), new allocation-stability test |
| `frontend/e2e/rotating-students.ts` | re-export of the four new slots |
| `frontend/e2e/t8a-entitlement-lifecycle.spec.ts` | **new** — the four AD-09 browser cases |

No production source file was touched.

## 14. Backend gates

```
go build ./...                 clean
go vet ./...                   clean
go vet -tags=integration ./... clean
go test ./...                  28 packages ok, 0 FAIL
```

Plus the five entitlement-operations integration tests in §10, all PASS.

## 15. Frontend gates

```
npm run typecheck   clean
npm test            379 passed / 0 failed
```

Was 378; the one added test is the T8A allocation-stability proof. E2E specs are not counted by
`npm test`.

## 16. T8A focused Playwright

```
npx playwright test e2e/t8a-entitlement-lifecycle.spec.ts --workers=1

  ✓  A: an Admin extends an active grant and the Student keeps access (14.9s)
  ✓  B: an Admin shortens an active grant to a future date and access continues (10.5s)
  ✓  C: an Admin moves expiry into the past and the Student loses access at once (10.5s)
  ✓  D: an Admin revokes a grant and the Student is refused (10.4s)

  4 passed (58.5s)
```

Four cases added, four green.

One iteration was required. The first run failed all four at the same assertion: the access badge was
selected as a `<p>`, but `LearningStatusBadge` renders a `<span>` carrying `data-learning-status`. The
fix was in the test's selector — now matching the state attribute **and** the copy together, so
neither can regress while the other passes. No production code was involved, and in particular the
Kuwait boundary conversion was not touched.

## 17. Canonical Playwright

One uncontended run, `cd frontend && npx playwright test --workers=1`.

| | Before (pre-T8A baseline) | After |
|---|---|---|
| passed | 153 | **157** |
| failed | 1 | **1** |
| did not run | 3 | **3** |

Elapsed 12.4 m. Four cases added, four passed: 153 + 4 = 157, exactly.

**Failure identity — unchanged and pre-existing:**

```
[chromium] › e2e/s5-playback-performance.spec.ts:157:11 › T076 — SC-001 time-to-first-frame
  › Viewport: phone (390x844) › first rendered frame within 5000 ms at phone

Error: T076 must measure the built frontend: run with GRADEX_E2E_FRONTEND_MODE=production
       after npm run build
  Expected: "production"
  Received: undefined
```

This is the spec asserting its own precondition, not a product failure, and it is not a
gap-register item. The three "did not run" are the remaining viewports of that same spec, which the
failed precondition short-circuits — identical to the baseline.

**No new deterministic failure identity was introduced.** The failed set is byte-for-byte the
baseline's.

## 18. T5 / T6 / T7 regression

T5 and T6 are untouched by this tranche; the T6 discovery spec ran green in the canonical suite
(cases A–F).

T7's remediated surface is re-audited **directly** by Case C (§8), which re-renders the expired
learning surface and re-checks the payload boundary.

The five T7-remediated cases remain green. In the canonical run the only failure is the
`s5-playback-performance` precondition named in §17, so `s5-expired-entitlement` and
`s5-viewport-evidence` are inside the 157 that passed.

## 19. Manual acceptance

**Manual acceptance was NOT separately performed.** Stating that plainly rather than dressing the
automated run up as one.

Why: the E2E runtime this tranche uses is disposable by construction. `globalSetup` creates a
per-run database, Go API, media server and Next server, and `globalTeardown` destroys all of it at
the end of the run — there is no surviving environment to walk through afterwards. Standing up a
separate long-lived acceptance stack would have required creating Admin and Student identities
outside the seeded fixtures, and the authorization forbids manufacturing credentials.

What stands in its place: the four cases in §6–§9 are real-browser journeys against the real Admin
UI, the real Go API, real PostgreSQL, production authentication and session middleware, and the real
protected-learning routes. Every Admin action is a genuine form interaction — the Admin locates the
Student by email on the queue, presses **Manage access**, types a date and a reason, and submits; no
route is intercepted, no request is synthesised, and every Student answer comes from the rendered
product. That is strong evidence, but it is automated evidence, and it is not called manual
acceptance here.

## 20. Tracker reconciliation

**AD-09** is promoted from `IMPLEMENTED_NOT_PROVEN` to **`E2E_PROVEN`**.

| | Before | After |
|---|---|---|
| `E2E_PROVEN` | 41 / 53 = 77.4% | **42 / 53 = 79.2%** |

The denominator stays 53. AD-09 is the only row promoted. **AD-12**, **AD-13**, **GAP-06** and
**GAP-08** were not touched, and the rotating slot-map change closes no gap of its own — it is test
infrastructure.

## 21. Known unrelated bugs

`courses.default_access_ends_at IS NULL` → `ConfirmPurchaseRequest` scans NULL into `time.Time` → 500
instead of `ErrExpiryRequired` / 409.

**Untouched by T8A**, per the authorization. It does not block AD-09: the rotating Student seed
supplies already-active Entitlements, and AD-09 begins from an active grant.

## 22. Repository safety

- No `git reset`, `git clean`, `git stash`, `git restore`, or broad `checkout`.
- No package-wide formatting or repository-wide normalization.
- The protected dirty baseline was inspected before editing every shared file; all T8A changes are
  additive and no unrelated working-tree change was disturbed.
- `git diff --check` clean.
- Database work used only the disposable per-run E2E database and the disposable
  `gradex_http_admission_test` integration database. No retained database was dropped or truncated,
  no `docker compose down -v`, no volume removal. The production-like `s12` environment was not
  touched.
