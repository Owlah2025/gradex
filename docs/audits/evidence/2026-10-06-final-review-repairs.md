# Final-review blocking repairs — 2026-10-06

## Scope and authority

The input is `.hardening-campaign/reports/FINAL-REVIEW-1.md`, which rejected
`45e66f0..16aa3c2`. This task repairs its four blocking findings. It grants no
production access, deployment, push, or independent approval. Optional P2/P3
redesigns remain deferred. A fresh independent frozen-range review is required.

The original `GATES-1.md` assertion that all gates passed on committed
`7e0c328` is withdrawn: the independent reviewer reproduced failures in the
committed tree. Working-tree passes were incorrectly attributed to that SHA.
New closure evidence must identify the committed checkout actually tested.

## Repairs and invariants

| Review finding | Repair | Regression contract |
|---|---|---|
| A4-001, Sev2, admin/identity | `3ddcbf6`: User 360 passes its transaction through device history and cooldown reads. | One pool connection suffices; two concurrent requests saturate a two-connection pool without nested acquisition. Active/revoked history and privileged-read audit are preserved; failed audit returns no view and rolls back. |
| A5-001, Sev2, Student playback | `2edd386`: HLS/native error handling, source fencing, two automatic reauthorizations per lesson, explicit retry, and teardown. | Normal authorization evaluates session/device/entitlement/lease; recovery restores position and play state. Exhaustion shows a recoverable error. Stale requests release their own leases. |
| False-green gate evidence | `2edd386` enumerates compiled unit test files; `1b87319` commits inspected prior formatting, strict payload, owned-media fixtures, and constant-time secret comparison. | Actual tests execute. Forged unknown fields fail with exact MALFORMED_JSON and no course persistence. Privileged mutation/audit assertions remain behavioral. |
| CAT-01, Sev2, catalog/media | `a227226`: real course-owned asset versions, trusted scan/processing fixtures, catalog/admin CI coverage, and logical asset validation/locking. | Wrong owner/course/lesson/kind denials preserve selected video and audit count; composite FK rejects direct cross-course writes. Publication refuses retired assets and serializes concurrent retirement. Legacy-only validation remains supported. |

Correcting catalog fixtures also reproduced a real publication defect: its
READY branch ignored logical asset retirement and locked only the version.
`validateLessonVideoAsset` checks both kinds and retirement and share-locks
both rows when the validator is transactional. The existing dependency
revalidation and controlled concurrency tests exposed the failure before this
repair; their assertions were retained.

Inspected prior browser/seeder repairs were continued in `6f4b7fc` and
`7909730`. Course selection uses the existing server search contract; resolved
academic context updates an already-mounted study-plan section. Dynamically
created courses receive their own test media through the protected isolated
E2E seeder. Arabic headings, real audit/security-event reasons, and denied
mutation persistence retain explicit assertions. Unrelated logs, a temporary
review spec, and generated reports were preserved outside the commits.

## Targeted repair evidence

Local PostgreSQL/Redis/MinIO only; backend fixtures use their disposable
`gradex_*_test` databases and browser runs use the isolated E2E database safety
system. No protected application database was reset.

- User 360 `MaxConns=1/2` regression passed, including saturated concurrency,
  history, exact audits, and rollback (`/var/tmp/gradex-repair-user360.log`).
- Full HTTP API integration passed in 687.442s after the fixture repairs
  (`/var/tmp/gradex-repair-httpapi.log`).
- Full catalog integration passed in 300.451s after real-media and publication
  repairs (`/var/tmp/gradex-repair-catalog-complete.jsonl`).
- Catalog ownership/publication concurrency tests passed with `-race`,
  including a READY resource refusal (`/var/tmp/gradex-repair-catalog-race.log`).
- Backend build, ordinary/integration vet, and race-enabled unit tests passed
  (`/var/tmp/gradex-repair-backend-unit.log`).
- Frontend lint/typecheck passed and `npm test` executed 861 passing tests.
- Short-expiry Chromium playback suite passed 3/3
  (`/var/tmp/gradex-repair-playback-e2e.log`, run `muvuf46xrvsd2iuy`). The first
  test proves an old capability returns exact 404 NOT_FOUND, a fresh authorized
  source plays past the saved position, database progress persists, the old
  lease is gone, and SPA navigation releases the current lease. Further tests
  prove bounded repeated 403 failures/user retry and terminal media-element
  recovery with paused position preserved.

These are targeted working-tree repair checks. The frozen-checkout gate ledger
below must be completed before treating the repair task as verified.

## Frozen-checkout gates

Pending. No all-green committed-head assertion is made by this draft ledger.

## Remaining boundaries and risks

Independent final approval is pending. Actual Safari playback and production
R2/scanner acceptance are separate from local Chromium/MinIO evidence. Before
a future release, preflight legacy lesson-video IDs for migration 0054 and
check strict-JSON client compatibility. No production read was made here.
RU-02 cleanup fairness, provider calls holding locks, unbounded histories,
remaining authorization-matrix gaps, the recorded suspension lock-order/
iteration risks, existing C1 intermittency, and dependency/deprecation triage
remain non-blocking follow-ups for this repair task. No capacity benchmark or
overall campaign completion is claimed.
