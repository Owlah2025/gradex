# Quickstart: S6 — Course Access Invitation and Entitlement Grant

**Date**: 2026-07-29 | **Plan**: [plan.md](plan.md) | **Contract**: [contracts/course-access-api.md](contracts/course-access-api.md)

Runnable validation for this slice. Scenarios are ordered so each builds on the previous one; the
negative scenarios are the point of the slice and are not optional.

## Prerequisites

- PostgreSQL, Redis, and MinIO running (`backend/docker-compose.yml`)
- Schema migrated to the version this feature's migration produces (**derived from repository state**, not assumed to be 13 — see [plan.md §Migration](plan.md#migration-and-schema))
- S2 and S4 closed on independent verdicts — this slice does not start otherwise
- A published Course with a **future** configured access-expiry instant
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

1. Fire N concurrent approvals of one invitation → **exactly one** Entitlement and one Enrollment.
2. Fire approve and cancel concurrently → one wins; the loser returns `409`; no partial state.
3. Fire two creations for the same pair concurrently → one row; the loser returns `409`, **not 500**.
4. Approve two different invitations for the same Student and Course concurrently → **exactly one**
   Entitlement; the loser returns `409 already-has-active-access`.
5. Approve while another transaction changes the Course expiry → the snapshot equals exactly one
   committed value, never a torn or rolled-back one.

6. Approve two different invitations for the same Student and Course concurrently → **exactly one**
   Entitlement (this is race 6, distinct from 4's same-invitation case).

**Mutation checks**: drop the partial unique indexes and re-run 1, 3, and 6 — if they still pass they
are testing the handler rather than the invariant, and they are not evidence. Races 2, 4, and 5 are
not index-backed and carry their own mutations, documented in `tasks.md` Phase 6.

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

- [ ] Every scenario above passes, including all **six** concurrency proofs
- [ ] The mutation check in Scenario 8 **fails** the tests when the indexes are dropped
- [ ] Full gate suite green, with a **clean** frontend build
- [ ] Hosted CI green on the exact head, all jobs
- [ ] `migrate max-version` equals `db.MaxSchemaVersion` and CI derives its assertion rather than carrying a literal
- [ ] No unreviewed change to `internal/access/entitlement.go` — S4 owns evaluation
