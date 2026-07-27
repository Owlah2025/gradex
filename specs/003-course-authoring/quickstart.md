# Quickstart — Validating S2 Course Authoring and Review

**Slice**: S2 | **Plan**: [plan.md](plan.md)

How to prove this slice works. Every scenario below is a **required** acceptance proof, and each one
must **fail under a deliberate mutation** — a test that passes against broken code is not evidence.
That standard comes from S1C, where two proofs were only trusted after the reviewer reproduced their
mutations independently.

## Prerequisites

Same environment as every prior slice — no new dependency:

- PostgreSQL at schema **9**, Redis, and MinIO running locally
- `GOCACHE=/tmp/gradex-go-cache` (the workspace sandbox refuses the default)
- A bootstrap Admin, one Instructor, a second Instructor, and one Student
- At least one successfully processed Asset Version to reference

## Gate commands

```bash
# Backend
cd backend
gofmt -l .                                  # must be empty
go build ./... && go vet ./... && go vet -tags=integration ./...
go test -count=1 -race ./...
go test -count=1 -race -tags=integration ./...

# Frontend
cd frontend
npm run typecheck && npm run lint && npm test
rm -rf .next && npm run build                # clean build or it is not evidence

# Guards
./scripts/docs-guard.sh && ./scripts/expose-guard.sh
```

**`rm -rf .next` is not optional.** A build claim that does not say "clean" is to be read as not
having been made (Decisions in Force, in effect since August 2 / D1).

## Scenario 1 — Private drafts are private (FR-002, SC-002)

1. Create a Course as Instructor A and fill it completely.
2. Request it by **exact identifier** as Instructor B, as a Student, and anonymously.
3. Each is refused, and the refusal does not distinguish "not found" from "not yours".

**Verify by**: integration test hitting every read route, enumerated from the live route table —
absence from a listing is not the proof.
**Mutation**: remove the ownership precondition from one route; the sweep must fail.

## Scenario 2 — Submission names everything wrong at once (FR-009, FR-010, SC-008)

Submit a Course with an empty Section, a Lesson lacking video, and no Subject term. **One** response
lists all three. Fix all three; submission succeeds and the Course becomes read-only to its
Instructor.

**Mutation**: make validation return on first failure; the assertion on violation count must fail.

## Scenario 3 — Only an Admin publishes (FR-013, SC-003)

Attempt publication as the owning Instructor by direct API call to every review route. All refused.
Publish as Admin; the Course becomes catalogue-visible and the Instructor notification intent exists.

**Mutation**: grant `CATALOG_PUBLISH` to the Instructor role in `policy_set.go`; the authorization
sweep must fail.

## Scenario 4 — The live graph is untouchable while a revision is pending (FR-018–FR-021, SC-004)

1. Publish a Course; capture the Student-visible graph.
2. Edit it as the Instructor — restructure Sections, replace a Lesson video, replace a Resource.
3. Re-read as a Student: **identical** to step 1. The replaced Resource still serves the approved file.
4. Approve. The new graph appears whole.
5. Repeat with rejection instead: the live version is unchanged.

**Verify by**: integration test with concurrent readers during approval, asserting every read returns
either the complete old graph or the complete new one — never a mixture.
**Mutation**: apply the revision outside a transaction; the concurrent-reader assertion must fail.

## Scenario 5 — The four concurrency cases (plan §concurrency)

| Case | Setup | Expected |
|---|---|---|
| 1 | Two Admins approve and request-changes on one Course simultaneously | Exactly one succeeds; the loser gets `409` naming the state found |
| 2 | One Instructor submits the same Course twice concurrently | One `PENDING_REVIEW`; the second gets `409`; no duplicate queue entry |
| 3 | Approve a revision while suspending its owner | Approval fails closed |
| 4 | Emergency suspension races revision approval | Both serialize; the Course stays inaccessible until restored regardless of order |

**Verify by**: real PostgreSQL under `-race`, with genuine parallel transactions — not sequential
calls that merely look concurrent. Case 3 must be observed to fail closed, not asserted to.

## Scenario 6 — Delist, retire, archive, and emergency suspension are four different things (FR-031–FR-036, SC-007)

With one entitled Student holding active access:

| Action | Student access |
|---|---|
| Delist | **continues** |
| Relist | continues |
| Retire (Order qualifies under BR-027) | **continues** |
| Archive | continues for the enrolled Student |
| **Emergency suspension** | **denied on the very next request** |
| Restoration | restored |

Assert throughout that **no Entitlement row is mutated** by any of them.

**Mutation**: make delisting deny access; the delist assertion must fail. If it passes, delisting and
suspension have been conflated — the exact confusion this slice must not ship.

## Scenario 7 — Deletion and taxonomy refusals (FR-033, FR-040)

- Delete a Course with one enrollment → refused, archiving offered. Zero enrollments → deleted.
- Delete a taxonomy term referenced by one Course → refused, retirement offered. Zero references →
  deleted.
- Retire a term, then attempt to assign it → refused. Courses already carrying it keep it.

## Scenario 8 — Every privileged action is answerable (FR-043, SC-006)

Enumerate the privileged routes **from the live route table**, exercise each, and assert an
`audit_events` row exists with actor, target, action, non-empty reason where required, and time, under
`module = CATALOG_AND_AUTHORING`.

**Mutation**: remove one audit write; enumeration must fail. Sampling would miss it, which is why the
test enumerates.

## Scenario 9 — Bilingual and responsive (SC-009)

Authoring and review screens in Arabic and English, correct RTL and LTR, usable at tablet, laptop, and
desktop widths. Validation messages, review reasons, and taxonomy labels all localize.

## Definition of done for this slice

- All nine scenarios pass, each demonstrated to fail under its stated mutation.
- Every gate command above is green, including a **clean** frontend build.
- Hosted CI passes all five jobs on the exact head offered for review.
- An **independent** review of the exact frozen range returns no critical or high finding.
  A builder's own reading of its own diff is a self-check and closes nothing.
