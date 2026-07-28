# Quickstart — Validating S2 Course Authoring and Review

**Slice**: S2 | **Plan**: [plan.md](plan.md)

How to prove this slice works. Every scenario below is a **required** acceptance proof, and each one
must **fail under a deliberate mutation** — a test that passes against broken code is not evidence.
That standard comes from S1C, where two proofs were only trusted after the reviewer reproduced their
mutations independently.

## Prerequisites

Same environment as every prior slice — no new dependency:

- PostgreSQL at schema **10**, Redis, and MinIO running locally
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
2. Call the idempotent candidate endpoint concurrently twice; both responses name one candidate
   whose `based_on_revision_id` is the captured live revision.
3. Confirm the clone has new version-row IDs but the same stable Section/Lesson IDs, and that counts
   of Asset Versions, stored objects/uploads, Orders/Payments, Enrollments, Entitlements, and Progress
   records did not change.
4. Edit that explicit candidate as the Instructor — restructure Sections, replace a Lesson video,
   replace a Resource. Video replacement preserves the Lesson identity; deleting and recreating the
   Lesson allocates a new identity.
5. Re-read through the production live-graph loader: **identical** to step 1. The replaced Resource
   still resolves to the approved file.
6. Approve while concurrent readers assemble the graph. Every result equals the complete old graph
   or complete new graph.
7. Repeat with rejection instead: Course lifecycle, pointer, live revision, enrollments,
   Entitlements, and selected resource/video references remain unchanged; the reason is preserved.

**Verify by**: real PostgreSQL integration tests. The live loader captures `live_revision_id` once
and uses it for the complete graph.

## Scenario 5 — The exact four D5 races (plan §D5 concurrency contract)

| Case | Setup | Expected |
|---|---|---|
| 1 | Two first edits create a candidate for one Published Course | Both receive one candidate identity; exactly one active candidate row exists |
| 2 | Two Admins approve the exact same candidate | Exactly one commits; the loser gets `409`; one live pointer and one approval evidence set |
| 3 | Approval races an Instructor mutation of the same submitted candidate | Approval commits once; the mutation gets `409` whether it locks before or after approval |
| 4 | Approval races rejection of the exact same candidate | Exactly one terminal action commits; the loser gets `409`; no contradictory audit/outbox evidence |

**Verify by**: real PostgreSQL under `-race`, with genuine parallel transactions — not sequential
calls that merely look concurrent.

### D5 invariant, rollback, and mutation proofs

- Directly attempt two `DRAFT` candidate inserts concurrently, bypassing the application Course
  lock. The database permits one. Removing the `0010` partial unique index must make this test fail;
  it proves the database backstop, not route idempotency.
- Make completeness, owner eligibility, taxonomy, and each asset kind invalid after submission.
  Approval returns `422` and changes nothing. Bypassing the shared approval-time revalidation call
  must make the subcases fail; it proves submission-time state is not trusted, not dependency-lock
  serialization.
- Force a failure after each load-bearing approval write. Pointer, old and candidate states, audit,
  and outbox remain unchanged. Move the `courses.live_revision_id`/lifecycle update to an
  auto-commit write after the approval transaction, then inject failure at that write. The snapshot
  must fail; it proves the shared rollback boundary, not read-path consistency.
- Make one live child loader select the latest revision instead of the captured `live_revision_id`.
  The pending-visibility or concurrent-reader assertion must fail; it proves candidate content
  cannot leak, not approval dependency checks.
- Make rejection alter Course lifecycle, `live_revision_id`, or the live revision. The rejection
  preservation assertion must fail; it proves live-state preservation, not approval atomicity.
- Regenerate stable Section/Lesson identities during cloning. The clone-lineage/video-replacement
  proof must fail; it proves stable entity identity, not external-resource clone counts.

Each mutation report states both the proof and its limit, and the original tree is restored before
the next mutation.

### D5 stop condition

Only T032–T038 are run. The exact production composition-root route sweep, all D5 PostgreSQL proofs,
the complete local gates, and hosted CI on the exact implementation head must pass. Stop before
T039; pricing, lifecycle/emergency controls, taxonomy administration, search, unrelated frontend
work, and unrelated refactoring are outside the range.

The production sweep must construct through `buildProductionFoundations`, require the real catalogue
repository and session-mutation foundation, and assert every exact D5 method/path. A self-contained
router or prefix-only assertion is not evidence. Missing/invalid origin or CSRF on each mutation is
`403`; anonymous is `401`; authorization/ownership concealment is the existing uniform `403`;
business validation is `422`; only stale or competing candidate state is `409`.

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
