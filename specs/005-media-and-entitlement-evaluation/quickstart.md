# Quickstart — Validating S4

**Slice**: S4 | **Plan**: [plan.md](plan.md) | **Tasks**: [tasks.md](tasks.md)

Every scenario is a **required** acceptance proof and must **fail under a deliberate mutation**. The
ten required mutations are listed in [tasks.md](tasks.md#required-mutations).

## Prerequisites

- PostgreSQL at the S4 schema, Redis, MinIO
- `GOCACHE=/tmp/gradex-go-cache`
- A **stub scanner** that can be driven to pass, fail, error, time out, and be absent entirely. The
  absent case is a configuration state, not a stub behaviour, and it must be exercised
- Fixtures: a Course-scoped Entitlement, a Section-scoped one, a Student holding both, an expired one,
  a revoked one, a suspended Account, an emergency-suspended Course, and a retired version with and
  without grandfathering

## Gate commands

```bash
cd backend
gofmt -l . && go build ./... && go vet ./... && go vet -tags=integration ./...
go test -count=1 -race ./...
go test -count=1 -race -tags=integration ./...
cd ../frontend && npm run typecheck && npm run lint && npm test
rm -rf .next && npm run build      # clean build or it is not evidence
cd .. && ./scripts/docs-guard.sh && ./scripts/expose-guard.sh
```

## Scenario 1 — Fail-closed, proven per mode (SC-001, Checkpoint A)

Drive the scanner to **each** of: pass, fail, error, timeout, absent, misconfigured. For every
non-pass outcome assert the asset is not deliverable through **any** route.

**Per mode, individually.** An aggregate assertion hides the one mode that passes, and the modes fail
for different reasons — an error returns, a timeout does not, and absence never calls the scanner.

**Mutations 1 and 2** from tasks.md must each turn this red.

## Scenario 2 — No production path can create an Entitlement (SC-002, Checkpoint B)

1. Build with production constraints; assert the seed symbol is unreachable.
2. Enumerate the live route table; assert no route writes an Entitlement.
3. Attempt to insert an Entitlement with a null `grant_source`; the **database** must refuse. *(D-045 replaced Order provenance with the typed grant source — BR-028, BR-113.)*

**Mutation 4** must turn step 1 red. Step 3 is the defence that survives a code change.

## Scenario 3 — Scope evaluation over the full graph (SC-003)

Enumerate **every Lesson in the Course**, not a sample:

- Course grant → every Lesson in every contained Section reachable.
- Section grant → that Section's Lessons reachable, **all others denied**.
- Both grants → union; revoke the Course grant, the Section grant still works.
- Expired on **effective** expiry → denied. Shorten expiry into the past → denied immediately, and
  Enrollment, Progress, the Course Access Invitation record, and adjustment history all survive.
- Emergency suspension → denied, **and no Entitlement row is mutated**.
- Retired with grandfathering → allowed; without → denied.

**Mutations 5, 6, and 7** each turn this red.

## Scenario 4 — One refusal (SC-004, Checkpoint D)

All eight causes — no entitlement, expired, revoked, Account suspended, emergency-suspended, retired,
asset absent, asset not `READY` — return responses identical in **status, headers, schema, and body**.

Assert on the full response. **Mutation 8** must turn it red.

## Scenario 5 — Idempotent callbacks (SC-005)

Duplicate, delayed, and out-of-order upload and transcode completions produce exactly **one** Asset
Version. **Mutation 10** must turn this red.

## Scenario 6 — Nothing is public (SC-006)

Request storage objects directly, **unsigned**. Every one must be refused by storage itself, not by
the application.

## Scenario 7 — The buyer tag (SC-007)

Lab Materials carry it; Lesson Resources do not. Assert on the tag's **bytes** that no Student PII is
present. **Mutation 9** must turn this red.

## Scenario 8 — Legacy path is gone (SC-008)

Assert the `internal/video` direct-to-asynq path is not a reachable code path — absent, not dormant.

## Scenario 9 — Mid-playback expiry (T031)

An issued signature stays valid for its short lifetime; no new access is issued after expiry.

**Do not assert instant revocation of an issued presigned URL.** It is not achievable, and a test
claiming it would be false. Assert the bound that exists: the signature lifetime.

## Closing the slice

- Full gates green, clean frontend build, hosted CI on the **exact** frozen head.
- Independent **Tier 3** review by Claude on one frozen exact commit range.
- All ten mutations run and restored.
- **A builder never closes its own slice.**
