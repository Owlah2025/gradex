# GradeX Hardening Plan

## Reconciled status

This plan is aligned with [TECHNICAL-AUDIT-2026-10.md](TECHNICAL-AUDIT-2026-10.md)
after the 2026-10-06 independent rejection and blocking-finding repairs. The original
audit base was `45e66f0550e3d0c6ec436bd2e9357f2de28ee33c`.

Valid prior work was inspected and continued; unrelated run artifacts remain
preserved. Exact checkout gates are in the
[final-review repair evidence](evidence/2026-10-06-final-review-repairs.md). This document is a
prioritized plan, not authorization to start another implementation batch.

Current decision:

- No Sev1 was established.
- **A4-001**, **A5-001**, and **CAT-01** have committed repairs and targeted
  regression evidence; the repaired tree needs a fresh independent review.
- The cross-course media/lesson binding, Admin lockout, metadata admission, and
  the primary durable resumable-upload deliverable are repaired in committed
  history.
- The original `GATES-1.md` claim of a green committed SHA was rejected and is
  withdrawn as closure evidence. Use the tracked repair record for new gates.
  Local passes are not independent final approval. Production R2/scanner behavior, manual
  acceptance, and deployment authority remain separate.

## P0 — correctness and security blockers

### P0.1 Repaired A4-001: User 360 connection ownership

Invariant: work holding a PostgreSQL connection must not acquire another
connection from the same pool before it can finish.

Repair: `3ddcbf6` passes the User 360 transaction to
`DeviceService.AdminOverviewInTransaction` for history and cooldown. Earlier
course-query and media-dispatch repairs are preserved. The bounded saturated
pool test uses the real device service and proves audit rollback as well as
successful reads. The following requirements are retained as review criteria.

Required repair:

- Add a transaction-compatible device overview query, or move the device read
  outside the transaction with an explicit consistency/audit decision.
- Preserve the privileged-read audit contract and the existing device privacy
  shape; do not solve pool pressure by dropping the audit or truncating history
  silently.
- Keep the media dispatcher’s prefetch-before-receipt behavior from `2403118`.

Required regression evidence:

- A disposable `MaxConns=1` User 360 test completes within a bounded deadline.
- Barrier-controlled concurrent User 360 requests do not deadlock or exhaust the
  pool.
- Device history, privileged-read audit, and failure rollback remain correct.
- `go test -race` and the canonical backend integration gate pass.

### P0.2 Repaired A5-001: Student signed-URL expiry recovery

Invariant: an authorized Student who pauses longer than the segment-signature
window either resumes playback after re-authorization or receives a bounded,
recoverable error; the player must not spin indefinitely or bypass the API
authorization decision.

Repair: `2edd386` handles fatal HLS authorization errors, expired delivery,
and native media errors with at most two automatic authorizations per lesson.
It fences stale callbacks, tears down listeners, restores position, and keeps
session/device/entitlement/lease checks. Explicit retry resets the budget.
Three short-expiry browser tests cover real expired delivery, persisted
progress and lease release, exhaustion/retry, and media-element recovery.
The follow-up replaces manifest interception with real local storage segment
denials and gives persistent non-fatal authorization retries a five-second
deadline, canceled by successful fragment delivery. The S5 no-mocks guard remains.
The following requirements are retained as review criteria; actual Safari
and production-provider acceptance remain separate from Chromium evidence.

Required repair:

- Detect fatal manifest/level/fragment 401/403 failures and distinguish them
  from non-fatal hls.js recovery events.
- Request a fresh application playback authorization through the existing
  session/device/lease authority; never construct a storage URL in the browser.
- Rebuild the HLS/native source and restore the prior position with a bounded
  retry count. Surface the existing retry/unavailable state after the bound.
- Cover native Safari HLS and ensure teardown removes listeners and old HLS
  instances.

Required regression evidence:

- Unit test the error-to-re-authorization state machine, including fatal expiry,
  non-fatal errors, retry exhaustion, stale callbacks, and teardown.
- Playwright test with a short local playback URL expiry: pause, cross expiry,
  resume, and prove continued playback or the bounded recovery UI.
- Verify progress/heartbeat authority and no second active playback lease is
  left behind.
- Run frontend lint, typecheck, unit tests, and the relevant protected-learning
  E2E suite.

### P0 closure ledger already completed

These repairs are recorded for traceability; they should not be reopened without
new evidence:

| Finding / deliverable | Committed repair and regression evidence |
|---|---|
| CAT-01 catalog fixtures, publication asset state, and CI omission | `a227226` creates owned media with trusted scan/processing evidence, locks logical asset and version during publication, and includes catalog/admin in CI. Exact regression proof is in the repair record. |
| False committed-head gate evidence | The test runner enumerates compiled test files, formatting is corrected, strict JSON and real-media fixtures retain refusal/persistence assertions, and fresh gates use a detached committed checkout. |
| A1-001 / A2-001 media attachment ownership and kind | `2403118`, migrations `0054` and `0057`, `backend/internal/catalog/media_binding_hardening_integration_test.go`, and the recorded integration gate. |
| A1-002 / A2-002 lesson binding and upload quota isolation | `2403118`, `2ece442`, multipart initialization tests, and the real-MinIO ownership/recovery run. Legacy single-PUT reservation cleanup remains P1/P2 work. |
| A3-001 / A2-008 last active Admin race | `2403118` and the identity suspension integration suite; active Admin rows are serialized with `FOR UPDATE`. |
| A4-003 metadata payload exhaustion | `2403118`, `bindStrictJSON`, route body limits, and `backend/internal/httpapi/binding_test.go`. |
| A0-RESUMABLE / A4-005 / A5-008 primary Instructor upload | `d8f001d`, `2ece442`, `4b67773`, `6f40d07`, multipart backend tests, frontend multipart tests, and `docs/audits/evidence/2026-10-05-media-recovery-repair.md`. |

## P1 — robustness required before serious paid usage

P1 work must not displace final-review repair verification. Each item needs a source-backed
regression and a clear failure/rollback path.

### P1.1 Complete the security boundary

- **A2-003:** derive the authorization matrix from the mounted production
  router, including media, device, academic, and public-catalogue families.
- **A2-004:** assert exact problem codes, distinguish ownership from CSRF and
  origin failures, and emit a typed monitored ownership-denial event.
- **A2-005:** compare the presented CSRF digest to the session-bound secret and
  apply Origin/session mutation protection to legacy media, learning, and
  protected-media mutation routes.
- **A2-006:** add deployment-appropriate HSTS, `nosniff`, referrer, and framing/
  CSP headers, with a report-only rollout if required by existing content.

Exit evidence: every mounted protected route has a denial row; wrong-but-
well-formed CSRF and foreign Origin are refused; denial logs contain the typed
reason; security headers are asserted at the edge in a local production-like
stack.

### P1.2 Repair lifecycle and database integrity gaps

- **A1-003 / A3-007:** store the original entitlement ID, disposition, and
  expiry on a settled purchase. Reconfirmations must return that relationship
  after revoke/regrant and remain idempotent.
- **A1-006:** preflight current enrollment, entitlement, purchase, media, and
  other restrictive dependencies and return a domain conflict instead of a
  generic internal error.
- **A1-007 / A3-008:** serialize academic hierarchy mutations per institution or
  use serializable transactions with bounded retry. Prove the four-node cycle
  interleaving cannot commit twice.
- **A3-006:** make resource upload completion plus lesson attachment one durable,
  idempotent command, or prove the current multipart checkpoint and duplicate
  attachment lookup cover every response-loss boundary.

Exit evidence: direct invalid combinations are rejected by the database or the
authoritative transaction, valid legacy/bundle records continue to load, and
concurrent tests prove one final relationship with no duplicate grants,
attachments, audits, or notifications.

### P1.3 Finish transaction, worker, and media failure hardening

- **A3-002:** normalize lock order across reset, invitation, purchase, and email
  flows; add bounded transaction retry only where external effects remain
  idempotent.
- **A3-003 / A4-006:** claim email work close to send time or renew/revalidate
  claims so a superseded worker cannot perform the external send.
- **A3-004:** persist first external-attempt time and stop automatic replay after
  the provider duplicate-suppression window; reconcile aged uncertain sends.
- **A3-005 / A4-004:** move legacy single-object completion verification out of
  the long transaction and add expiration/quota release/object reconciliation
  to the remaining direct upload path.
- **A4-002:** apply route-specific database and storage deadlines that cancel
  underlying work, not merely the HTTP write.
- **A4-008 / A4-013:** separate short media jobs from waiting transcodes and
  establish measured FFmpeg CPU, memory, and scratch-disk budgets.

Exit evidence: blocked dependency tests release connections, stale claims do
not send, uncertain external effects do not replay indefinitely, and media
workers make progress for scans and short jobs during a transcode backlog.

### P1.4 Finish frontend/API failure behavior

- **A5-002:** reconcile idle and absolute session lifetime semantics and make
  long uploads/learning flows recover or fail with an explicit expiry path.
- **A5-003 through A5-007 and A5-009:** repair sign-out failure handling,
  expired-session redirects, auth rehydration/CSRF drift, progress/report
  delivery, heartbeat retry behavior, and device-removal routing.
- Preserve the durable multipart UI recovery state and prove browser refresh,
  interruption, failed-part retry, cancellation, and completion response loss
  continue to work after auth/session changes.

Exit evidence: failure-path E2E tests distinguish 401/403/404/5xx, retries are
bounded and idempotent, and persisted state is checked after every protected
mutation denial.

### P1.5 Test-integrity closure

The committed campaign strengthened Admin audit, Arabic heading, strict schema,
mutation-persistence, and multipart recovery assertions. Remaining work:

- replace the omitted-route authorization matrix with a real mounted-route
  derivation;
- remove or justify the `test.fixme` in `frontend/e2e/uxi-global-sweep.spec.ts`;
- eliminate conditional assertions that skip schema-shape proof and broad catches
  that can turn setup failures into passing tests;
- retain isolated disposable databases and the existing protected-database
  safety checks.

Exit evidence: a failure-injection mutation test fails when persistence changes,
all required branches execute on empty/non-empty fixtures, and no test is green
because a dependency or fixture was silently skipped.

## P2 — scale preparation and structural strengthening

P2 work is intentionally sequenced after P0/P1 and must be driven by measured
plans or production-like local evidence. Do not claim capacity from account
counts alone.

- **A1-004 / A1-005:** add composite progress/completion and entitlement scope/
  provenance constraints, with an explicit migration strategy for supported
  historical and bundle rows.
- **A2-007:** make media authorization follow current course ownership after an
  Admin reassignment, or re-own media in the same transaction.
- **A4-007:** replace the current bounded outbox discovery horizon with a durable,
  indexed consumer/watermark after proving late/delayed event semantics.
- **A4-009 / A4-010:** measure roster/analytics plans and evaluate course-leading
  enrollment/entitlement indexes before adding write overhead.
- **A4-011 / A4-012:** paginate account/course/device histories, bound work before
  expensive projections, and avoid curriculum-wide completion checks on every
  position-only progress write without breaking revision-change semantics.
- **RU-02:** add retry scheduling or fair rotation to multipart abandoned-upload
  cleanup so a repeatedly failing oldest batch cannot starve newer sessions.
- Shorten or redesign multipart provider calls made while PostgreSQL locks are
  held; measure pool usage before increasing worker concurrency.
- Measure multipart verification backlog and define admission/backpressure for
  large files; preserve the bounded verification timeout and explicit retry UI.
- **A5-010 / A5-011:** localize transport/server errors and generate accurate
  per-route SEO metadata.

Required scale evidence is qualitative plan output, bounded query/response tests,
queue backlog/lease observations, and failure recovery. No benchmark number may
be added without a repository-retained benchmark or run artifact.

## P3 — engineering quality and maintainability

- **A2-009 / A2-010:** normalize media existence responses, rate-limit upload
  initiation, and restrict or cache public `/readyz` checks.
- **A5-012 through A5-014:** add localized app error boundaries/not-found routes,
  idempotency or reconcile-before-retry for ordinary creates, and optimistic
  concurrency for authoring saves.
- **A6-005:** provide worker liveness/readiness for orchestration instead of only
  logging repeated dependency errors.
- Finish remaining deprecation and dependency-vulnerability triage. Counts are
  not severity classifications.

## Verification gates and authority boundaries

For any future implementation batch, run the repository’s canonical backend,
frontend, integration, media-authoring, resumable, and canonical E2E gates from
the checked-in workflow and record the exact disposable database names and
artifacts. At minimum, the current gate record names:

- backend formatting/build/vet/race/integration/migration safety;
- frontend install/lint/typecheck/unit/build;
- release and media-authoring E2E;
- standalone resumable upload against local PostgreSQL and MinIO;
- canonical E2E with declared skips explained.

Passing local gates do not constitute independent review. Before any launch
decision, separately obtain the required frozen-range independent review and
production-provider/manual acceptance. Do not use the local MinIO run or the
development no-op scanner as evidence of R2 or production scanner behavior.
