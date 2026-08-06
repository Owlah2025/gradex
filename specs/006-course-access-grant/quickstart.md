# Quickstart: S6 — Course Access Invitation and Entitlement Grant

**Date**: 2026-07-29 | **Plan**: [plan.md](plan.md) | **Contract**: [contracts/course-access-api.md](contracts/course-access-api.md)

Runnable validation for this slice. Scenarios are ordered so each builds on the previous one; the
negative scenarios are the point of the slice and are not optional.

## Prerequisites

- PostgreSQL, Redis, and MinIO running (`backend/docker-compose.yml`)
- Schema migrated to **15** — this feature's migration is `0015_course_access_grant`, recalculated
  2026-08-06 from the committed schema, whose highest pair is `0014_protected_learning` with
  `db.MaxSchemaVersion = 14`. See [plan.md §Migration](plan.md#migration-and-schema)
- **S2, S4, and S5** all closed on independent verdicts — this slice does not start otherwise. S5 closed
  2026-08-06 at `d5ce557` on a Tier 3 `APPROVE`
- A published Course with a **future** configured access-expiry instant. **This requires
  `courses.default_access_ends_at`, which no closed slice created** — see
  [D-073](../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it).
  Until `0015` adds the column and its Admin write path, this prerequisite cannot be satisfied and every
  approval scenario below refuses at step 5 of the grant transaction
- One Admin holding `COURSE_ACCESS_GRANT`, one Student account, one unrelated Student account

```bash
cd backend
export GOCACHE=/tmp/gradex-go-cache          # the workspace sandbox refuses the default
go run ./cmd/migrate up
go run ./cmd/migrate max-version             # must equal db.MaxSchemaVersion; CI derives its assertion from this
```

## Gate suite

Run all of it before offering any range as evidence.

```bash
# Backend
cd backend
gofmt -l .                                   # must print nothing
go build ./...
go vet ./...
go vet -tags=integration ./...
go test -count=1 -race ./...
go test -count=1 -race -tags=integration ./...

# Frontend — .next MUST be removed first (CARRYOVER-LOCAL-BUILD-CACHE)
cd ../frontend
npm run typecheck && npm run lint && npm test
rm -rf .next && npm run build

# Guards
cd ..
scripts/docs-guard.sh
scripts/expose-guard.sh
```

A build claim that does not say **clean** reads as not having been made.

---

## Scenario 1 — Registration grants nothing *(SC-001)*

Register a Student, verify the email, sign in. Request playback, a protected download, and a progress
write on any Course.

**Expected**: every request denied, and each denial byte-identical to the denial for a Course that
does not exist. No Enrollment and no Entitlement row exists for that Student.

## Scenario 2 — Create an invitation *(US1)*

As the Admin, create an invitation for the Student's email and the Course, with a note and an external
reference.

**Expected**: `201`. Invitation is `PENDING_STUDENT_ACCEPTANCE`. An audit record exists. An outbox
delivery intent exists carrying the destination address. **No Enrollment, no Entitlement.** The
acceptance secret appears nowhere in the response body or the logs.

Repeat the same request. **Expected**: `409 duplicate-invitation`.

## Scenario 3 — Only the invited identity can accept *(SC-004)*

Attempt acceptance as: the unrelated Student, an Instructor, an Admin, and unauthenticated.

**Expected**: `404` for every authenticated wrong identity — identical to a non-existent invitation.
The unauthenticated request is asked to authenticate and is shown **no** Course, email, or invitation
detail.

## Scenario 4 — Acceptance grants nothing *(SC-002)*

Accept as the invited Student.

**Expected**: `200`, state `PENDING_ADMIN_APPROVAL`, audit record written, Admin queue shows it.
Then request playback again — **still denied, still byte-identical to a non-existent Course.** Still no
Enrollment, still no Entitlement.

This is the single most important scenario in the slice.

## Scenario 5 — Approval refuses without its preconditions *(SC-005)*

Attempt approval:
1. as an Admin without `COURSE_ACCESS_GRANT`
2. as a capable Admin whose last authentication is older than the security window

**Expected**: `403 insufficient-capability` and `403 recent-authentication-required`. After both, the
database contains **no** Enrollment, Entitlement, entitlement-grant audit record, or notification
intent. Refused, not degraded.

## Scenario 6 — Approval grants access *(US3)*

Approve with a capable, recently authenticated Admin.

**Expected**: `200`. Exactly one Enrollment **row** (created by this slice, in S5's table) and exactly one `ACTIVE` Entitlement with
`grant_source = 'MANUAL_INVITATION'`, `source_invitation_id` set, `original_access_ends_at` equal to
the Course's configured instant, and `retirement_eligibility_at` at the approval time. Audit records
for both the approval and the grant. An access-granted notification intent. The Student can now play a
Lesson.

Approve again. **Expected**: `200` returning the **same** Entitlement. Still exactly one.

## Scenario 7 — Course state gates the grant

Set the Course to each state and attempt approval on a fresh accepted invitation.

| State | Expected |
|---|---|
| Archived | `409 course-not-grantable` |
| Delisted | `409 course-not-grantable` |
| Retired | `409 course-not-grantable` |
| Emergency access suspension active | `200` — granted, and playback is denied while suspension holds |
| Expiry instant absent or in the past | `422 validation-failed`, naming the missing configuration |

## Scenario 8 — Concurrency *(SC-003, mandatory under Constitution V)*

Each of these runs under `-race` against real PostgreSQL. **A sequential repeat is not a substitute.**

> **Corrected 2026-08-06.** This list previously enumerated race 6 twice — as items 4 and 6, with item 6
> annotated "distinct from 4's same-invitation case" when item 4 described the same two-invitation case —
> and **omitted race 3 entirely**. The mutation-check sentence then mislabelled which proofs are
> index-backed. The list below is one item per race, numbered to match
> [plan.md §The five races](plan.md#the-five-races) and `tasks.md` Phase 6, so "all six proofs" now means
> six distinct proofs.

| # | Proof | Task | Backed by |
|---|---|---|---|
| 1 | Fire N concurrent approvals of one invitation → **exactly one** Entitlement and one Enrollment | `T046` | Index |
| 2 | Fire approve and cancel concurrently → one wins; the loser returns `409`; no partial state | `T047` | Lock |
| 3 | Fire accept and cancel concurrently → one wins; the loser returns `409` | `T048` | Lock |
| 4 | Fire two creations for the same pair concurrently → one row; the loser returns `409`, **not 500** | `T049` | Index |
| 5 | Approve while another transaction changes the Course expiry → the snapshot equals exactly one committed value, never torn and never rolled back | `T050` | Lock |
| 6 | Approve two *different* invitations for the same Student and Course concurrently → **exactly one** Entitlement; the loser returns `409 already-has-active-access` | `T051` | Index |

**Index mutation check** — proofs **1, 4, and 6**: drop `cai_one_non_terminal_per_pair` and
`entitlements_one_active_student_course` and re-run them. If they still pass they are testing the handler
rather than the invariant, and they are not evidence.

> The index name matters. `entitlements_one_active_student_course` is S4's, shipped in
> `0012_media_and_entitlement`. Dropping the planned-but-nonexistent
> `ent_one_active_per_student_course` would be a silent no-op, and the mutation check would pass while
> proving nothing.

**Lock mutation checks** — proofs **2, 3, and 5** are not index-backed and carry their own, in `tasks.md`
`T053`: replace the invitation `SELECT … FOR UPDATE` with a plain `SELECT` (proofs 2 and 3 must fail), and
replace the Course `FOR SHARE` with a plain `SELECT` (proof 5 must fail). These are ordering invariants
over one row rather than uniqueness invariants over a set, so no constraint expresses them and removing
the lock is the only mutation that tests them.

## Scenario 9 — Suspension *(SC-009)*

Suspend the Student holding active access. Request every protected operation.

**Expected**: all denied. The Entitlement row is **byte-identical** before, during, and after.
Reinstate; access resumes without any Entitlement mutation. Then approve a fresh invitation for a
*suspended* Student — **expected `200`**, per FR-040, with the grant unusable until reinstatement.

## Scenario 10 — No payment surface exists *(SC-006, SC-012)*

```bash
# No payment column anywhere in the schema
psql "$DATABASE_URL" -c "\d+ course_access_invitations" | grep -iE 'amount|currency|gateway|payment|payer'   # expect no rows
psql "$DATABASE_URL" -c "\d+ entitlements"              | grep -iE 'amount|currency|gateway|payment|payer'   # expect no rows

# No route creates an Entitlement except approve — enumerate the live route table, do not eyeball it
go test -run TestOnlyApprovalCreatesEntitlement ./internal/httpapi/...
```

**Expected**: no payment-shaped column exists, and the route enumeration proves a single creation path.

## Scenario 11 — Bilingual and responsive *(SC-011)*

ST03, ST04, ST10, AD06, and AD07 in Arabic and English, RTL and LTR, at phone, tablet, laptop, and
desktop widths.

**Expected**: complete and correct at every combination. No clipped, mirrored, or overflowing layout.
ST03 states explicitly that acceptance does not grant access.

---

## Acceptance evidence for this slice

A range is offered for review only when all of the following hold:

- [ ] Every scenario above passes, including all **six distinct** concurrency proofs
- [ ] The index mutation check in Scenario 8 **fails** proofs 1, 4, and 6 when the indexes are dropped, and
      the lock mutation checks fail proofs 2, 3, and 5
- [ ] Full gate suite green, with a **clean** frontend build
- [ ] Hosted CI green on the exact head, all jobs — **including `./internal/access` in the hosted
      integration list**, so the new package does not join the six integration-tagged packages that
      already run only locally (S5 `F-7`)
- [ ] `migrate max-version` equals `db.MaxSchemaVersion` = **15**, and CI derives its assertion rather
      than carrying a literal *(already true in `ci.yml`; confirm, do not rebuild)*
- [ ] **No change at all to `backend/internal/entitlement/`** — S4 owns evaluation. *(Corrected
      2026-08-06: the path was `internal/access/entitlement.go`, which does not exist. `internal/access`
      is the package S6 creates; `internal/entitlement` is S4's.)*
- [ ] No edit to any migration `0001`–`0014`, verified by `scripts/docs-guard.sh` and by diff
