# KNOWN-BASELINE-01 — Payment confirmation with a NULL Course default access expiry

**Date:** 2026-08-24
**Branch:** `ui-antigravity-20260817`
**Tranche:** KNOWN-BASELINE-01 — Payment Confirmation NULL Expiry Remediation
**Verdict:** `PROVEN` — the NULL default access expiry is now an expected `409` business conflict with
zero partial mutation, and the same Purchase Request confirms normally after the Admin configures the
expiry.

## 1. Authority

- Founder authorization, 2026-08-24: a fresh, narrow remediation tranche against KNOWN-BASELINE-01
  only. Explicitly prohibited: redesigning Course Access or Entitlements, changing Course publication
  rules, fabricating or inferring an expiry, and touching any other Ox Alpha finding.
- [D-089](../../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time) —
  gap-driven work only, one tranche at a time.
- The defect was twice recorded and deliberately deferred by earlier tranches:
  [`docs/mvp/FUNCTIONAL_COMPLETION.md`](../../mvp/FUNCTIONAL_COMPLETION.md) records it under T8C and
  again under T9 as *"Recorded, not acted on: the `default_access_ends_at IS NULL` → 500
  purchase-confirmation defect."*
- The independent Ox Alpha review reconfirmed the same issue as KNOWN-BASELINE-01. The Founder
  authorization fixes its operational severity from the concrete product flow, not from the external
  review's technical Low–Medium classification.

## 2. Historical symptom

Observed manually before this tranche:

1. A published, priced Course whose `courses.default_access_ends_at` was still `NULL`.
2. Admin pressed **Confirm payment** on a `WAITING_PAYMENT` Purchase Request.
3. The API answered **HTTP 500 Internal Server Error**.
4. After the Admin set `default_access_ends_at = 2027-01-31 …`, the *same* confirmation succeeded.

That history established the shape of the defect but produced no automated proof. This tranche
creates it.

## 3. Exact root cause

`courses.default_access_ends_at` is nullable. It is added without a `NOT NULL` constraint by
[`backend/internal/db/migrations/0015_course_access_grant.up.sql:62`](../../../backend/internal/db/migrations/0015_course_access_grant.up.sql):

```sql
ADD COLUMN default_access_ends_at TIMESTAMPTZ;
```

`ConfirmPurchaseRequest` nevertheless scanned it into a non-nullable `time.Time`
(`backend/internal/access/purchase.go`, pre-fix):

```go
var expiry time.Time
err = tx.QueryRow(ctx, `
    SELECT c.default_access_ends_at
      FROM courses c
      JOIN course_revisions cr ON cr.id = c.live_revision_id
     WHERE c.id = $1::uuid AND ` + catalogpublic.PublishedOnly("c", "cr") + `
     FOR SHARE
`, request.CourseID).Scan(&expiry)
```

When the column is `NULL`, pgx returns a **scan error**, not `pgx.ErrNoRows`. The scan error therefore
fell through the `pgx.ErrNoRows` branch into the generic `fmt.Errorf("locking purchasable course: %w", err)`
wrapper, which the HTTP layer could only classify as `problem.Internal("")` — a 500.

The exact error, captured by the new regression test **before** the fix was applied:

```
ConfirmPurchaseRequest with NULL default_access_ends_at returned locking purchasable course:
can't scan into dest[0] (col: default_access_ends_at): cannot scan NULL into *time.Time,
want ErrExpiryRequired
```

```
confirm payment with NULL default expiry status = 500, want 409
```

The domain error `access.ErrExpiryRequired` and its `409` mapping already existed. The value simply
never reached them, because the scan failed one step earlier.

## 4. Code change

Two files, 18 changed lines of production code. No new domain error, no new database column, no schema
migration, no publication-rule change.

### 4.1 `backend/internal/access/purchase.go`

The nullable instant is now scanned as a nullable instant. `*time.Time` is the convention already used
by adjacent access code — including the directly analogous Course lock in
`Repository.ApproveInvitation` (`backend/internal/access/repository.go:770-797`), which reads the same
column and already distinguishes `NULL` from a real timestamp:

```go
// courses.default_access_ends_at is nullable: a Course may be published and
// purchasable before an Admin has configured its default access expiry. That
// is an expected business state, so it is scanned as a nullable instant and
// refused as ErrExpiryRequired rather than surfacing as an internal error.
var defaultAccessEndsAt *time.Time
err = tx.QueryRow(ctx, `
    SELECT c.default_access_ends_at
      FROM courses c
      JOIN course_revisions cr ON cr.id = c.live_revision_id
     WHERE c.id = $1::uuid AND `+catalogpublic.PublishedOnly("c", "cr")+`
     FOR SHARE
`, request.CourseID).Scan(&defaultAccessEndsAt)
if errors.Is(err, pgx.ErrNoRows) {
    return ConfirmPurchaseRequestResult{}, ErrCourseNotPurchasable
}
if err != nil {
    return ConfirmPurchaseRequestResult{}, fmt.Errorf("locking purchasable course: %w", err)
}
if defaultAccessEndsAt == nil || defaultAccessEndsAt.IsZero() {
    return ConfirmPurchaseRequestResult{}, ErrExpiryRequired
}
expiry := *defaultAccessEndsAt
if !expiry.After(now.UTC()) {
    return ConfirmPurchaseRequestResult{}, ErrExpiryRequired
}
```

The `NULL` state and a real timestamp stay explicitly distinguishable. Nothing is coalesced to a
sentinel date, no zero time is allowed to continue silently, and no expiry is fabricated or inferred.
The pre-existing "expiry is in the past" branch is unchanged and keeps returning `ErrExpiryRequired`
exactly as before.

### 4.2 `backend/internal/httpapi/access_routes.go`

`ErrExpiryRequired` already mapped to `409`, but it shared a problem code with the state-transition
conflicts, so the Admin was told *"The purchase request is no longer eligible for this action."* — an
actively wrong statement in this case, since the request **is** still eligible and the Course is what
needs configuring. The status is unchanged; only the code and detail were split out:

```go
case errors.Is(err, access.ErrExpiryRequired):
    // The request itself is still eligible; the Course simply has no valid
    // future default access expiry yet, so the Admin is told what to fix.
    writeProblem(c, problem.New(http.StatusConflict, "course-default-access-expiry-required",
        "Course access expiry is not configured",
        "Set the Course access expiry before confirming payment."))
case errors.Is(err, access.ErrPurchaseRequestTransition), errors.Is(err, access.ErrDuplicateInvitation):
    writeProblem(c, problem.New(http.StatusConflict, "purchase-request-state-conflict", …))
```

This is the existing canonical problem-response mechanism (`problem.New` at `409`), not a new HTTP
contract. No consumer depended on the old code: a repository-wide search for
`purchase-request-state-conflict` finds only the two backend call sites, none in the frontend, E2E
suite, or documentation.

## 5. Domain behavior

| Course `default_access_ends_at` | `ConfirmPurchaseRequest` | HTTP |
|---|---|---|
| `NULL` | `ErrExpiryRequired` | `409 course-default-access-expiry-required` |
| in the past / not after `now` | `ErrExpiryRequired` (unchanged) | `409 course-default-access-expiry-required` |
| valid future instant | success | `200` with the linked invitation |

Course publication rules are untouched. A Course may still be published and purchasable before its
default access expiry is configured; that remains a legitimate product state, and the Admin recovers
from it through the existing Course Access configuration control.

## 6. Transaction safety — zero partial mutation

The refusal returns before `issuePurchaseInvitationTx`, before the `purchase_requests` `UPDATE`, and
before the audit insert. `defer tx.Rollback(ctx)` therefore discards the transaction and `tx.Commit`
is never reached.

`TestConfirmPurchaseRequestRequiresDefaultAccessExpiry` and
`TestConfirmPaymentReturnsConflictWhenDefaultExpiryMissing` both prove this against real PostgreSQL by
reading every mutation the confirmation is capable of making, after the refusal:

| asserted after the refusal | required |
|---|---|
| `purchase_requests.state` | `WAITING_PAYMENT` |
| `purchase_requests.payment_confirmed_at` | `NULL` |
| `purchase_requests.invitation_id` | `NULL` |
| `purchase_requests.access_ends_at_snapshot` | `NULL` |
| `course_access_invitations` for the Course | `0` |
| `entitlements` for the Course | `0` |
| `outbox_events` (whole table) | `0` |
| `audit_events` where `action = 'PURCHASE_REQUEST_PAYMENT_CONFIRMED'` | `0` |

The outbox count is taken across the entire table, not filtered, so no transactional email of any kind
can escape unnoticed.

## 7. Retry proof

Both tests prove the real supported recovery end to end, without SQL-forcing any confirmed state:

1. Course published, priced, `default_access_ends_at IS NULL` (asserted `NULL` before the attempt).
2. Purchase Request created through `CreatePurchaseRequest` — `WAITING_PAYMENT`.
3. Confirmation refused: `ErrExpiryRequired` / HTTP `409`.
4. Purchase Request still `WAITING_PAYMENT`, no side effects (§6).
5. Expiry configured through the canonical domain command
   `Repository.SetCourseDefaultAccessExpiry` — the same command the Admin Course Access surface calls.
6. The **same** confirmation retried.
7. Success: state `INVITATION_CREATED`, an invitation is linked, and
   `access_ends_at_snapshot` equals the configured instant.
8. Exactly `1` invitation row and exactly `1` `access.invitation_issued` outbox event.

The successful-path architecture is unchanged; the retry runs the same code the always-configured
Course has always run.

## 8. HTTP proof

`POST /api/v1/admin/purchase-requests/{id}/confirm-payment`, authenticated Admin, against a Course
with `default_access_ends_at IS NULL`:

- status **`409`** (was `500`)
- problem body `status` field **`409`**
- the body contains actionable expiry guidance
- the correlation `X-Request-Id` header is still present
- the body leaks none of `default_access_ends_at`, `time.Time`, `pgx`, `SQL`, `scan`, `courses c`,
  `goroutine` — asserted case-insensitively as an explicit negative list

## 9. Frontend

**Unchanged. No frontend production file was modified.**

`src/components/admin/purchase-requests.tsx:22-26` already renders `problem.detail` verbatim for any
`ProblemError`, and `confirm()` already routes the rejection into the panel's error banner without
crashing. Before this tranche it displayed the generic *"no longer eligible"* text; it now displays
**"Set the Course access expiry before confirming payment."** — actionable guidance, delivered by the
backend problem detail, through the frontend code that was already there.

The existing Course Access expiry control remains the single canonical configuration path. No second
expiry workflow was added and the Course Access page was not redesigned. Because no frontend source
changed, no UX regression test was added and no frontend gate was manufactured (§12).

## 10. PostgreSQL integration tests

New file: `backend/internal/httpapi/purchase_confirmation_expiry_integration_test.go`
(build tag `integration`, real PostgreSQL through the existing `freshSchema` / `setupAdminAccessAPIServer`
harness against the disposable `gradex_authoring_test` database).

| test | proves |
|---|---|
| `TestConfirmPurchaseRequestRequiresDefaultAccessExpiry` | the domain contract directly: `ErrExpiryRequired`, zero partial mutation, and successful retry after `SetCourseDefaultAccessExpiry` |
| `TestConfirmPaymentReturnsConflictWhenDefaultExpiryMissing` | the real production HTTP mapping: `409`, actionable detail, no internal leak, request-id preserved, zero partial mutation, and successful retry |

Both were written **before** the fix and observed failing with the exact defect (§3), then observed
passing after it:

```
=== RUN   TestConfirmPurchaseRequestRequiresDefaultAccessExpiry
--- PASS: TestConfirmPurchaseRequestRequiresDefaultAccessExpiry (1.15s)
=== RUN   TestConfirmPaymentReturnsConflictWhenDefaultExpiryMissing
--- PASS: TestConfirmPaymentReturnsConflictWhenDefaultExpiryMissing (1.17s)
PASS
ok  	github.com/Owlah2025/gradex/backend/internal/httpapi	2.335s
```

## 11. Backend gates

| gate | result |
|---|---|
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `go vet -tags=integration ./...` | clean |
| `go test ./...` | green, 28 packages |
| `go test -tags=integration ./internal/access/... ./internal/httpapi/...` | `ok internal/access 0.004s`, `ok internal/httpapi 310.051s` |

The whole `httpapi` integration suite — including the retained `TestManualPurchaseFlowHTTPAPI_RealPostgreSQL`
journey that confirms payment on a Course **with** an expiry — is green, so the successful path is
proven unregressed.

## 12. Frontend gates

Not run, and deliberately so: no frontend source file changed in this tranche. The historical
**379 passed** unit baseline is untouched.

## 13. Canonical E2E

Command (the single authoritative T9 entrypoint, unchanged):

```bash
cd frontend && npm run test:e2e:canonical
```

| | before | after |
|---|---|---|
| passed | 168 | 168 |
| failed | 0 | 0 |
| skipped | 0 | 0 |

Observed post-remediation run (exit 0):

```
════════ canonical E2E result ════════
production   passed 4  failed 0  flaky 0  skipped 0  (26.6s, exit 0)
development  passed 164  failed 0  flaky 0  skipped 0  (834.5s, exit 0)
aggregate    passed 168  failed 0  flaky 0  skipped 0
══════════════════════════════════════
```

This tranche adds no canonical E2E case, so the expected count is unchanged. No existing case
regressed, and zero flaky results were recorded in either lane.

## 14. Files changed

| file | change |
|---|---|
| `backend/internal/access/purchase.go` | nullable `*time.Time` scan; `NULL` → `ErrExpiryRequired` |
| `backend/internal/httpapi/access_routes.go` | `ErrExpiryRequired` gets its own `409` problem code and actionable detail |
| `backend/internal/httpapi/purchase_confirmation_expiry_integration_test.go` | **new** — both regression tests |
| `docs/launch/evidence/2026-08-24-payment-confirmation-expiry-null-remediation.md` | **new** — this record |
| `docs/mvp/FUNCTIONAL_COMPLETION.md` | KNOWN-BASELINE-01 closure recorded; score unchanged |

No other tracked file was touched. `git diff --check` is clean.

## 15. MVP tracker impact

**No counted row closes. The score stays `45 / 53 = 84.9%`.**

This defect was never a counted 53-row MVP item. It was recorded twice as *"Recorded, not acted on"*
alongside the T8C and T9 matrix-impact sections, explicitly outside those tranches' row sets. The two
rows this flow touches were already `E2E_PROVEN` before this tranche and are unaffected by it:

- **AD-05 — Configure Course default access expiry** — `E2E_PROVEN`
- **AD-09 — Confirm manual payment** — `E2E_PROVEN`

The distinction that matters: the **counted MVP score is unchanged**, while the **paid-beta blocker is
resolved**. An Admin can no longer be shown a 500 for a supported product state.

## 16. Ox Alpha status

**KNOWN-BASELINE-01: `OPEN` → `CLOSED`.**

The Ox Alpha review file itself was not modified; project convention records remediation in a dated
evidence document, which is this file.

No other Ox finding was touched. INF-01, MED-01 through MED-07 and the LOW-tier findings are untouched
and remain open for their own separate remediations.

## 17. Repository safety

- No `git reset`, `git clean`, `git stash`, `git restore`, or broad `checkout`.
- No repository-wide or package-wide formatting. Only the two production hunks shown in §4 were
  written, plus the new test file and the two documentation files.
- The pre-existing dirty working tree — the T5–T9 work across `backend/`, `frontend/`, and `docs/` —
  was inspected before editing and is preserved byte-for-byte. T9's `frontend/playwright.config.ts`,
  `frontend/scripts/e2e-canonical.mjs` and `frontend/package.json` were not opened for writing.
- No destructive database or Docker action. Integration tests used only the disposable
  `gradex_authoring_test` database, which the existing `freshSchema` harness owns and recreates. The
  retained `s12` stack and its volumes were never touched.
- `git diff --check` clean.
