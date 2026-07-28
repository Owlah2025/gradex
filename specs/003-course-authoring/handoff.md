# D5 Implementation Handoff — S2 Revision Integrity

**To**: Antigravity (`agy`, builder through `speckit.implement`)

**From**: Codex (SpecKit planner)

**Independent reviewer**: Claude — review only, no implementation or repair

**Authority**:
[D-042](../../docs/DECISIONS.md#d-042--codex-plans-antigravity-implements-and-claude-independently-reviews)

**Application baseline**: `08b8857`

**Scope**: T032–T038 only; stop before T039

## Run protocol

Use the existing feature selected by `.specify/feature.json`:
`specs/003-course-authoring/`. Do not create another Phase 5 directory, rerun
`speckit.specify`, regenerate S2, or reinterpret completed T001–T031.

Read these completely, in order:

1. [spec.md](spec.md), especially the D5 clarification and FR-046–FR-055
2. [plan.md](plan.md), especially the D5 concurrency, transaction, mutation, and scope sections
3. [data-model.md](data-model.md), including schema-10 stable identity and pointer constraints
4. [contracts/authoring-api.md](contracts/authoring-api.md) and
   [contracts/review-api.md](contracts/review-api.md)
5. [tasks.md](tasks.md), then execute only T032–T038 in dependency order
6. [quickstart.md](quickstart.md), which is the required evidence contract

Before editing, run:

```bash
.specify/scripts/bash/check-prerequisites.sh --json --require-tasks --include-tasks
```

Keep the result uncommitted for Codex to inspect and freeze. Do not touch the user-owned
`.caveman.json`. If interrupted, report the exact completed task and leave the tree buildable.

## Non-negotiable implementation boundary

- First Published-Course edit creates or returns one complete candidate based on the captured
  `live_revision_id`, atomically and idempotently.
- PostgreSQL permits at most one active candidate (`DRAFT`, `CHANGES_REQUESTED`,
  `PENDING_REVIEW`) per Course.
- Clone revision-owned version rows and references only. Preserve stable Section/Lesson identities;
  allocate new logical identity only for a new or explicitly deleted-and-recreated entity. Create no
  media, upload/object, payment/Order, Enrollment, Entitlement, Progress, or other external row.
- Every mutation names `{revisionId}` and resolves stable child identity inside that exact candidate.
  No mutation selects the latest revision or writes the live revision.
- The live loader captures `live_revision_id` once and assembles every descendant from that identity.
  Wire it through the real production composition root and existing owned-Course read; do not add a
  Student catalogue, learning, search, or playback route.
- Approval is one PostgreSQL transaction with lock order Course → exact candidate → owner →
  taxonomy/Asset Version rows in ascending ID order. Revalidate state, base pointer, ownership,
  completeness, all asset kinds, and taxonomy through transaction-bound readers. Supersede old,
  approve candidate, swap pointer, audit, and outbox intent before commit. Make no external delivery
  call inside the transaction.
- Rejection locks and revalidates the exact candidate, preserves a non-empty reason, writes
  `COURSE_REVISION_REJECTED` audit/outbox evidence, and changes no live/access state.
- `409` is only stale/terminal/replaced/competing candidate state. Business validation and owner
  ineligibility are `422`; caller/session/ownership denials retain `401`/`403`.
- Production foundations require the real repository and session-mutation foundation. Every D5
  mutation applies origin/CSRF enforcement before authorization and ownership.

## Proven prerequisites admitted to D5

Close only D5-C01 through D5-C05:

1. remove the enabled `23505 → success` submission mutation;
2. make validators/outbox dependencies mandatory and propagate intent errors;
3. remove implicit/latest editable-revision resolution;
4. add stable Section/Lesson identity needed by BR-019 and BR-059;
5. restore production origin/CSRF enforcement and construction validation on catalogue mutations.

No other Phase 1–4 rewrite is authorized.

## Exact PostgreSQL evidence

T038 means exactly:

1. concurrent first edits both succeed with one candidate ID and no `409`;
2. concurrent approvals yield one commit and one `409`;
3. approval commits while a mutation of that submitted candidate returns `409`, regardless of lock
   order;
4. approval versus rejection yields exactly one terminal action and one `409`.

Also prove old-or-new graph reads, zero partial approval effects under injected failures, deep clone
equality/external-row counts, cross-Course/child refusal, dependency revalidation and locking, stable
identity behavior, rejection preservation, and the exact response matrix.

Run and restore these six independent mutations. For each, record what it proves and does not prove:

1. move the Course pointer/lifecycle update outside approval transaction and inject its failure;
2. remove active-candidate uniqueness and use direct concurrent `DRAFT` inserts;
3. make one live child loader choose latest rather than the captured live revision;
4. bypass shared approval revalidation and exercise completeness/owner/taxonomy/asset subcases;
5. let rejection alter live revision/pointer/lifecycle;
6. regenerate stable Section/Lesson identities during cloning.

## Standing quality clause

> A required dependency is validated at construction and the component refuses to build without it.
> No security-relevant control may silently degrade, default, or become optional.

Reuse the existing transaction, lock, graph-loader, audit, and outbox primitives. Split only the
authoring/review code D5 must touch; do not perform unrelated cleanup.

Run all local gates in [quickstart.md](quickstart.md), including real PostgreSQL with `-race`, the
exact production-router sweep, documentation/exposure guards, and clean frontend build. Mark a task
complete only after its named evidence passes.

Stop after T038. Do not implement Admin pricing, lifecycle/emergency controls, taxonomy
administration, frontend changes, search, or unrelated refactoring. Claude receives only the final
frozen exact implementation range and must remain independent.
