# Quickstart: Validating S5 — Protected Learning

**Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Contracts**: [contracts/learning-api.md](contracts/learning-api.md)

How to prove S5 works. Every scenario names the requirement it discharges, so a gap in this guide is a
gap in the evidence.

> **Prerequisite: S2, S3, and S4 must be closed.** S5 consumes S4's evaluator, S4's signed issuance and
> trusted duration, S4's non-production Entitlement seed, S2's Course graph, and S3's bilingual shell.
> Validating S5 against stubs proves the stubs work.

---

## Setup

```bash
# Real PostgreSQL — the integration and migration evidence is not meaningful against a fake
cd backend && docker compose up -d postgres

# Clean install: 0001 → 0014 in sequence
go run ./cmd/migrate up

export DATABASE_URL='postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable'
```

Seed access through **S4's non-production seed mechanism** (`//go:build !production`). S5 adds no seed
path of its own and inherits S4's production exclusion (spec §Assumptions).

Enrollment fixtures are inserted **directly** by integration tests. This is permitted and is the only
sanctioned way to obtain one in S5: a fixture in a test binary is not a production path, and S5 has no
production code capable of creating an Enrollment row (FR-015a).

---

## 1. Migration safety — both paths, plus the round trip

```bash
cd backend

# Clean install
go run ./cmd/migrate up
go run ./cmd/migrate down && go run ./cmd/migrate up   # up/down/up round trip

# Upgrade path: stop at the last committed migration, then advance
go run ./cmd/migrate up --to 0010 && go run ./cmd/migrate up

go test -tags=integration -run 'TestMigrateUpDownUp|TestDirtySchema|TestUnsupportedSchema' ./internal/db/
```

**Expect:** `0013_enrollments` applies **before** `0014_protected_learning` on both paths — the
Progress foreign key depends on it. `db.MaxSchemaVersion` is **14** and CI **derives** its assertion
from that constant rather than hardcoding a literal.

**The legacy-progress guard** ([R-01](research.md#r-01--the-legacy-progress-cutover-cannot-preserve-rows-and-must-not-synthesise-enrollments)):

```bash
# Insert a legacy progress row, then attempt the migration
go test -tags=integration -run 'TestLegacyProgressGuardRefusesNonEmptyTable' ./internal/db/
```

**Expect:** the migration **aborts with an exception naming the row count**. It does not drop the rows,
and it does not synthesise an Enrollment to preserve them (FR-018, FR-015a, Principle VII).

## 2. The enrollment boundary — S5 creates the table, never a row

```bash
go test -tags=integration -run 'TestEnrollmentsShapeMatchesS6Contract' ./internal/db/
go test -run 'TestProductionBuildHasNoEnrollmentCreationPath' ./internal/learning/
```

**Expect:**
- `enrollments` has exactly `id`, `student_account_id`, `course_id`, `created_at` and the
  `UNIQUE (student_account_id, course_id)` constraint — the shape
  [S6 asserts](../006-course-access-grant/data-model.md) before writing.
- The production build contains **no** Enrollment-row-creating symbol and **no** Entitlement-creating
  symbol (FR-005, FR-015a, SC-006). Same shape as S4's seed-exclusion assertion.

## 3. Resume and completion (User Story 1 · FR-009 – FR-012)

```bash
go test -tags=integration -run 'TestProgress' ./internal/learning/
```

| Scenario | Expect | Requirement |
|---|---|---|
| Play to a position, terminate session, re-open | Resumes at the last position, not zero | FR-009, SC-003 |
| Reach ≥90% of the **trusted** duration of the exact Asset Version | Complete; `completed_at` written **once** | FR-010, BR-051 |
| Seek backwards, replay, or Instructor replaces the video | Completion and maximum **do not regress**; `completed_at` not rewritten | FR-012, BR-059 |
| Client reports a percentage disagreeing with the server | Client value **ignored** | FR-010, SC-004 |
| Position beyond the trusted duration, or below zero | **Clamped**, not rejected — the session is not lost | FR-011 |
| Progress write fails transiently | Playback **not interrupted**; write retried | FR-013, SC-008 |

## 4. Concurrency — the Principle V proof

```bash
go test -race -tags=integration -run 'TestProgressConcurrentWritersPreserveMonotonicMaximum' ./internal/learning/
```

N concurrent writers at one `(enrollment, lesson)`, interleaved and out-of-order positions.

**Expect:** the final maximum equals the true maximum, and `completed_at` is written **exactly once**.
This is the only case that can fail while every sequential test passes — idempotency never exercised
concurrently is an assumption (Principle V).

## 5. Access ending mid-session (User Story 2 · FR-001 – FR-004)

```bash
go test -tags=integration -run 'TestAccessRevalidation' ./internal/httpapi/
cd frontend && npx playwright test e2e/s5-access-ends.spec.ts
```

Start an authorised playback session, then mutate each access condition server-side **mid-session**:

| Mutation | Expect | Requirement |
|---|---|---|
| Entitlement expires / is revoked | Next issuance **and** next Progress write denied | FR-002, SC-005 |
| Account suspended | Same | FR-002, BR-007 |
| Emergency Course access suspension | Same | FR-002, BR-090 |
| Any of the above | **No** Entitlement, Enrollment, or Progress record mutated as a side effect | FR-016, BR-026 |
| Delisted / retired / archived Course, qualifying Entitlement | Access **continues**, subject to BR-027's `retirement_eligibility_at` vs `retired_at` | FR-004 |
| Signed URL issued moments before access ended, presented after | Does not extend access beyond S4's token boundary; **no new issuance** | FR-002 scenario 6 |
| Expired Entitlement, Student signs in | Sees retained Enrollment and Progress, expired state; **nothing** authorised from any of it | FR-016, BR-029 |

**The client must not have to cooperate.** Denial is asserted server-side against the mutated
condition, not by observing the UI stop.

## 6. Denial uniformity (FR-003)

```bash
go test -tags=integration -run 'TestDenialsAreByteIdentical' ./internal/httpapi/
```

**Expect:** all seven causes — expired, revoked, out-of-scope, suspended, emergency-suspended,
retired-ineligible, and **never-authored Lesson id** — return **byte-identical** responses: same
status, same body, same header set.

> Asserting only the status code passes while leaking existence through the body. Compare the full
> response.

## 7. Request-time revalidation, over the **mounted production router** (FR-001 · SC-002)

```bash
go test -run 'TestAuthorizationMatrixMatchesMountedRouter|TestEveryProtectedLearningRouteRevalidates' ./internal/httpapi/
```

**Expect:** every protected S5 route in the **mounted production router** — enumerated from
`r.Routes()`, not from a hand-maintained list — revalidates at request time.

> A hand-maintained matrix is exactly the S1C finding, and a sweep that tests its own router is the S2
> finding. Enumerate the real router.

**Mutation proof (SC-002):** delete one revalidation call and re-run. A test **must** fail. A
revalidation whose removal breaks nothing was never load-bearing.

## 8. Learning surfaces, RTL/LTR, and viewports (User Story 3 · FR-019 – FR-027)

```bash
cd frontend
npx playwright test e2e/s5-course-home.spec.ts e2e/s5-lesson-player.spec.ts
```

| Scenario | Expect | Requirement |
|---|---|---|
| Course Home for a multi-Section Course | Sections and Lessons in **authored order** from the qualifying graph | FR-019 |
| Course Entitlement | **Every** Section and Lesson in scope — Course scope is whole-Course | FR-020 |
| Arabic selected or defaulted | Layout, navigation, controls, and dates **RTL**; preference persists | FR-026, BR-149 |
| Instructor-authored content, either language | **Not translated** | FR-026, BR-150 |
| Phone, tablet, laptop, desktop | **No Student capability missing** at any viewport; rendered evidence retained — the S2 T066 standard | FR-027, SC-010 |
| Lesson Player, keyboard only and screen reader | Every platform-owned control reachable and labelled; automated WCAG 2.2 AA with **zero** violations | FR-025, SC-009 |
| Dashboard | Continue Learning and My Courses show progress and access state; **no** invitation state read | FR-023, FR-006 |
| Any S5 screen | **No** community-link element anywhere | D-046 |

Rendered evidence — Arabic RTL and English LTR at all four viewports — is **retained**, matching the S2
T066 standard.

## 9. Content reporting (User Story 4 · FR-029 – FR-035)

```bash
go test -tags=integration -run 'TestContentReport' ./internal/learning/
```

| Scenario | Expect | Requirement |
|---|---|---|
| Entitled Student reports each target kind | Report created with **both** logical target and exact visible revision/version | FR-030 |
| `reason = 'other'` with no explanation | **Refused** — at the database constraint, not only the handler | FR-029 |
| Report submitted | Reported content **not** hidden, retired, or altered | FR-031, SC-011 |
| Same Student, same target, twice | Second **refused** by the partial unique index | FR-032 |
| Beyond 5/hour | **Throttled** | FR-032 |
| Student with no Entitlement for the target's Course | **Refused** server-side, uniform refusal | FR-033 |
| Acknowledgement | Reveals **nothing** about queue state, other reports, or outcomes | FR-034 |
| Any S5 route | **No** resolution, dismissal, delisting, retirement, or moderation path exists | FR-035 |

## 10. D-046 absence over the production build

```bash
cd backend && grep -rniE 'community|discord|telegram' internal/learning/ internal/db/migrations/001[34]_*.sql
cd frontend && grep -rniE 'community|discord|telegram' src/app/\[locale\]/learn src/components/learning
```

**Expect: no matches.** No column, no payload field, no screen element, no placeholder, no
"coming soon" state. FR-036 – FR-038 are `DEFERRED — S18` under
[D-046](../../docs/DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch)
and S5 implements none of them.

## 11. Mutation discipline (SC-012)

Every acceptance proof must **fail** under a deliberate mutation of the control it claims to verify:
remove a revalidation, remove `GREATEST`, remove `COALESCE`, remove the `other`-explanation
constraint, remove the legacy-progress guard, widen a denial response.

A proof that survives its mutation is not evidence. Record the mutation and the resulting failure.

---

## Convergence

```bash
baseline_status=/tmp/gradex-s5-status-baseline.z

git status \
  --porcelain=v1 \
  -z \
  --untracked-files=all \
  >"$baseline_status"
```

After the implementation commit:

```bash
git diff --quiet
git diff --cached --quiet

final_status=/tmp/gradex-s5-status-final.z
git status \
  --porcelain=v1 \
  -z \
  --untracked-files=all \
  >"$final_status"

cmp --silent "$baseline_status" "$final_status"
git status --short
```

A passing comparison proves that final repository residue is identical to the pre-implementation
residue; it does **not** claim that a repository with documented user-owned work has no residue.

Run the local gates before the implementation commit:

```bash
cd backend && gofmt -l . && go build ./... && go vet ./... && go vet -tags=integration ./... && go test -race ./...
cd frontend && npm run lint && npm run build && npx playwright test
```

**Local green is not closure.** S5 closes on **hosted CI green on the exact head commit** — Backend,
Frontend, migrations, integration, and Guards jobs — plus an **independent Tier 3 reviewer verdict**
against one frozen commit range, with every critical and high finding resolved.

The builder never approves its own slice. If the review produces no retrievable verdict, that is review
`UNAVAILABLE`, not approval.
