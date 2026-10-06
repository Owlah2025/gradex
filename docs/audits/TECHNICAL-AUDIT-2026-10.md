# GradeX Technical Audit 2026-10

## Scope and evidence boundary

The A0–A7 audit was originally performed against campaign base
`45e66f0550e3d0c6ec436bd2e9357f2de28ee33c`. This reconciliation covers the
repair work after the independent rejection of `16aa3c2` on 2026-10-06.
The exact frozen software revision and fresh checkout gates are recorded in
[final-review repair evidence](evidence/2026-10-06-final-review-repairs.md).

Valid prior fixture and UI repairs were inspected and continued; unrelated run
logs and temporary review artifacts remain preserved. Only committed changes
and explicitly identified run artifacts count as repair evidence.

The A0–A7 runtime reports are under `.hardening-campaign/reports/`. Primary
tracked evidence is the final-review repair record above and the
[media-recovery repair evidence](evidence/2026-10-05-media-recovery-repair.md).
The original `GATES-1.md` claim of a green committed SHA was rejected: it mixed
working-tree changes with committed code. It is withdrawn as closure evidence.
No production deployment, production database mutation, external-provider
acceptance, or manual production acceptance is claimed here.

## Executive status

- No Sev1 was established by the source-backed A0–A7 audit.
- The original P0 findings for media ownership, upload lesson binding, Admin
  lockout, and metadata body limits are repaired in the committed tree. The
  User 360 device-history and cooldown reads now use its existing transaction;
  one-connection and saturated-pool regressions preserve history and audit
  rollback. Media dispatch retains its corrected connection ownership.
- The resumable-upload deliverable is implemented for the Instructor video,
  resource, and public-preview workflows. It has durable server state, direct
  multipart storage transfer, retry/resume behavior, cancellation, cleanup,
  and recovery evidence. Small thumbnails and Admin catalogue loads still use
  the legacy single-object path; that path is not described as multipart.
- **A4-001 and A5-001 are repaired.** The Student player handles fatal HLS
  authorization failures and native media errors with bounded reauthorization,
  restores position, and retains the normal session/device/entitlement/lease
  authority. Non-fatal authorization retries have a five-second deadline canceled
  by successful fragments. Short-expiry browser tests verify real storage failures,
  progress, and lease persistence without protected-route mocks.
- **CAT-01 is repaired.** Catalog fixtures use actual owned media versions and
  trusted processing evidence. Publication validates and locks the logical
  asset as well as its version, including retirement and kind. CI now includes
  the catalog suite.
- The independent final campaign review, production R2/scanner acceptance,
  and manual production acceptance remain separate gates and are not satisfied
  by this document.

## A0 — architecture and domain model

| Boundary or subsystem | Current implementation | Invariant or authority |
|---|---|---|
| Public surface | Next.js App Router under locale routes; Caddy sends `/api/*`, `/healthz`, and `/readyz` to the Go API and other traffic to the frontend. | The API remains the authorization authority; the browser never receives storage credentials. |
| Student | Session-scoped learning, protected media, progress, completion, reports, and course-access views. | Student data and protected delivery are evaluated from the authenticated account and entitlement state. |
| Instructor | Owned course authoring, revision/lesson mutation, media upload, selection, and processing observation. | Course ownership, active Instructor status, asset ownership, kind, and course/lesson binding are checked before mutation. |
| Admin | Account, staff, review, catalogue, entitlement, moderation, analytics, and operational media controls. | Admin capabilities are denied by default and sensitive reads/mutations are audited where implemented. |
| API | Go/Gin route composition in `backend/internal/httpapi`; session, capability, role, ownership, strict JSON, and media guards are layered per route family. | Unsafe cookie-authenticated mutations must have origin and session-bound CSRF protection; this remains inconsistent on several legacy groups. |
| Worker | Asynq plus media/email dispatch, stale-work recovery, multipart verification, abandoned multipart cleanup, and media processing. | Durable database state and claim/lease identities fence worker retries and crashes. |
| PostgreSQL | Schema/migrations in `backend/internal/db/migrations`; pgx pool for API and worker. | Relational constraints and transaction boundaries are authoritative for lifecycle and ownership invariants. |
| Redis | Asynq, rate limits, and playback lease state. | Playback leases are account/device scoped and use atomic Redis operations. |
| Object/media storage | S3-compatible abstraction; production configuration targets Cloudflare R2, local production-like tests use MinIO. | Browser transfers use presigned URLs; object identity is represented by ETag/version metadata and exact-version checks. |
| Video processing | Worker-side FFmpeg/ffprobe, scanner/trusted-instructor operating modes, durable media state transitions, and exact-version work claims. | Only an approved media state/provenance can be delivered or selected for publication. |
| Email/outbox | Transactional outbox, encrypted payloads, database discovery, provider dispatch, leases, and receipts. | Domain commit precedes external delivery; retries must not create duplicate domain effects. |
| Entitlement/course lifecycle | Course revisions switch live content; invitations, purchases, bundles, entitlements, enrollments, progress, and completion are separate state machines. | Access is derived from current lifecycle, entitlement, enrollment, and content state rather than a client-supplied decision. |
| Analytics/operations | Admin User 360, roster, metrics, audit events, media failures, and operational exports. | Expensive projections and retention histories need explicit bounds as account and course histories grow. |

Important trust boundaries are browser → edge → API, API → PostgreSQL/Redis,
API/worker → object storage, and worker → FFmpeg/scanner. The browser may hold
an opaque session/device cookie and short-lived signed URLs, but it cannot choose
an owner, course relationship, storage object identity, or publication state.

The principal state machines are Account (`ACTIVE`/`SUSPENDED` and credential
states), Session/device trust, Staff invitation, Course/revision publication,
Media asset version (`UPLOADED` through scan/processing to `READY` or failure),
Entitlement (`ACTIVE`/`REVOKED`), access invitation, purchase request,
Progress/completion, and transactional email delivery. Media state transitions
are additionally guarded in the database.

## Current upload architecture

The original audit correctly found a single-request upload at the base SHA. The
committed tree now has two deliberate paths:

1. **Primary Instructor multipart path.** `POST /api/v1/media/uploads/multipart`
   creates or reuses a durable upload intent. Migrations `0056` and `0057` store
   the client request identity, provider multipart ID, lifecycle status,
   agreed part manifest, checksum, immutable object identity, and verification
   claim. The server authorizes the active Instructor, course owner, course,
   lesson, asset kind, and expected size before signing a part.
2. **Direct-to-storage transfer.** `frontend/src/lib/api/media-multipart.ts`
   slices the file into provider parts, uploads at most three parts in parallel,
   retries only failed parts up to three attempts, persists a local browser
   checkpoint, and asks the server for authoritative provider parts on resume.
   Video bytes do not pass through the GradeX API.
3. **Completion and verification.** The server records one immutable manifest
   and checksum, reconciles a lost provider response by object identity, and
   rejects changed evidence. A worker claim verifies exact-object size, format,
   and checksum outside a long PostgreSQL row lock before the existing guarded
   completion path admits the asset to media processing.
4. **Cancellation and expiry.** `DELETE /api/v1/media/uploads/:id/multipart`
   records the aborting state before provider abort/deletion. The worker scans
   expired abandoned multipart sessions, handles a missing persisted provider ID
   by reconciling the storage key, removes an assembled unverified object, and
   marks the intent `ABORTED`.
5. **Legacy bounded path.** Course thumbnails and Admin catalogue loads still
   use `POST /api/v1/media/uploads` plus one presigned PUT. The legacy completion
   path still performs full-object verification while holding its transaction;
   this is retained as the residual A4-004 scale follow-up, not silently counted
   as part of the multipart guarantee.

The focused evidence is the 11-test multipart backend suite recorded in
`M2-media-review-3.md`, the frontend multipart unit tests, and the standalone
real-MinIO recovery run documented in
[`2026-10-05-media-recovery-repair.md`](evidence/2026-10-05-media-recovery-repair.md).
That run proved mid-upload interruption, retained parts, retry of only the
failed part, page-refresh recovery, ownership denial, attachment persistence,
provider cancellation, and `NoSuchUpload` after cancellation. It used local
PostgreSQL/Redis/MinIO and `DEVELOPMENT_NO_OP` scanning; it is not production
R2 or production-scanner acceptance.

## Finding ledger after repairs

### Fixed or materially mitigated in committed `HEAD`

| Finding | Severity / priority at base | Current status and evidence | Launch blocker | Scale blocker |
|---|---|---|---|---|
| A1-001 / A2-001 — cross-course or wrong-kind lesson media attachment | Sev2 / P0 | **Fixed.** `2403118` added transactional binding checks; migrations `0054` and `0057` add course-keyed relationship enforcement. `a227226` repairs fixture setup and proves wrong owner/course/lesson/kind denials, unchanged selection/audit, and direct SQL refusal. | No, subject to remaining review authority | No for this defect |
| A1-002 / A2-002 — arbitrary lesson binding and quota interference | Sev2 / P0 | **Fixed for the committed upload paths.** The lesson/course relationship is checked before persistence and multipart initialization, and quota reservations are tied to the authorized binding. Multipart abandonment/cancellation releases the new path’s reservation. Evidence: `2403118`, `2ece442`, `multipart_initialization_integration_test.go`, and the real-MinIO recovery record. | No | No for the repaired path; legacy reservation cleanup remains a follow-up |
| A3-001 / A2-008 — concurrent suspension can remove every Admin | Sev2 in A3 / Sev4 in A2; P0 hand-off | **Fixed in code.** `identity.SuspendAccount` locks all active Admin rows with `FOR UPDATE` before applying the threshold. The suspension integration suite and the recorded backend gate are the regression evidence. | No | No for this race |
| A4-001 — nested pool acquisition / dispatcher connection retention | Sev2 / P0 | **Fixed.** `3ddcbf6` routes device history and cooldown through the User 360 transaction. The bounded `MaxConns=1/2` integration regression asserts concurrent completion, history, audits, and rollback. Earlier course-query and dispatcher fixes are retained. | No after repair; independent review pending | No for this defect |
| A5-001 — Student playback expiry stall | Sev2 / P0 | **Fixed.** `2edd386` and its follow-up add HLS/native recovery, two automatic refreshes, a non-fatal denial deadline, stale-callback fencing, teardown, and explicit retry. Runtime tests and real-storage short-expiry tests prove recovery, progress, exhaustion, and lease release. | No after repair; independent review pending | No for this defect |
| CAT-01 — catalog fixture failures and publication asset race | Sev2 / P0 | **Fixed.** `a227226` replaces invalid legacy attachment fixtures with real media and trusted processing. Publication checks kind/retirement and locks both asset and version; catalog is in CI. See detailed proof below. | No after repair; independent review pending | No for this race |
| A4-003 — unbounded authenticated metadata JSON | Sev2 / P0 | **Fixed in code.** `bindStrictJSON` applies bounded, single-document, unknown-field-rejecting decoding; the documented admission bound is 64 KiB on the critical metadata routes. `binding_test.go` covers malformed/ambiguous bodies and the backend gate passed. | No | No for the original body-amplification path |
| A0-RESUMABLE / A4-005 / A5-008 — non-resumable large Instructor upload | P0 deliverable; Sev3 / P1 in A4/A5 | **Implemented and focused-tested.** Commits `d8f001d`, `2ece442`, `4b67773`, and `6f40d07` complete the durable multipart workflow and repair the recovery fixture. The remaining provider-lock and cleanup-fairness risks are listed below. | No separate upload blocker remains | Yes, for the follow-ups below |
| A6-001 — `IDENTITY_OTP_PEPPER` local bootstrap | Sev3, local tooling only | **Fixed.** `3ee02d8` generates the pepper on fresh and upgrade paths in `deploy/scripts/environment.sh`. This never established a production defect. | No | No |
| A6-002 / A6-003 — blocking/large migration work | Sev2 / launch and scale risk | **Fixed in the committed migration chain.** `b30abc1` batches the completion backfill and moves the email index to a standalone `CREATE INDEX CONCURRENTLY` migration. Fresh migration gate evidence is in the repair record; live data compatibility remains a release gate. | No local campaign blocker remains | No for the identified migration mechanisms |
| A6-004 and the corresponding A4-007 outbox scan risk | Sev2 in A6 / Sev3 P2 in A4 | **Bounded/mitigated.** `b30abc1` adds a 14-day discovery horizon and releases media discovery connections before dispatch. A durable per-consumer watermark and measured plan evidence remain useful scale work. | No | Yes for large retained histories |
| A7-001 — false-green mutation and localization assertions | Sev3 / P1 | **Partially fixed.** `b30abc1`, `3ee02d8`, `6f40d07`, and `8a37d70` strengthen persistence proofs, Admin audit assertions, Arabic heading geometry, conditional schema assertions, and resumable fixture independence. The remaining coverage gaps are explicit below. | No | No direct runtime scale blocker |

### Open findings and residual risks

#### Final-review P0 repair proof

The four blocking review findings have source-backed repairs and regression
evidence in the linked repair record. Independent approval has not been granted
by this implementation task.

**CAT-01 supplement — logical asset publication integrity.** Severity Sev2;
subsystem catalog/media; invariant: publication must validate current video
kind and retirement and serialize conflicting changes. Replacing invalid
fixtures exposed that the READY branch in `ValidateLessonVideoForPublication`
ignored `media_assets.retired_at` and locked only `media_asset_versions`.
An authorized retirement could therefore race publication; larger concurrent
review/authoring workloads increase its reachability. This is an integrity
failure, not an unauthenticated exploit. The existing
`TestD5ApprovalRevalidatesEveryDependencyClass` and
`TestD5ApprovalDependencyLocksSerializeConflictingWrites`, changed to mutate
the actual logical asset, reproduced acceptance of a retired video and an
escaping concurrent writer. `validateLessonVideoAsset` now checks both kinds,
retirement, and uses `FOR SHARE OF mav, ma` inside publication transactions.
Required regressions cover READY resources, retired assets, concurrent
retirement, publication rollback, and supported legacy-only validation.
Launch blocker before repair: yes; scale blocker before repair: yes. Both
are repaired locally; frozen-range independent review remains required.

#### P1 robustness required before serious paid usage

These remain source-backed findings from the A1–A5 reports; no later committed repair proves them closed:

- **A1-003:** preserve the original entitlement ID/disposition for settled purchases so reconfirmation cannot select a newer grant.
- **A1-006:** classify current enrollment, entitlement, purchase, media, and other course-deletion dependencies as a domain conflict before restrictive foreign keys produce an internal error.
- **A1-007 / A3-008:** serialize academic hierarchy writes per institution or use serializable transactions with retry; the four-node concurrent cycle proof still needs a PostgreSQL regression.
- **A2-003:** derive authorization sweeps from the real mounted router or cover the currently omitted media, device, academic, and public-catalogue families.
- **A2-004:** assert precise denial problem codes and record ownership denials for monitoring; current status-only mount checks can still pass for the wrong 403.
- **A2-005:** enforce a session-bound CSRF comparison and an Origin check on every cookie-authenticated mutation, including legacy media, learning, and protected-media POST groups.
- **A2-006:** add browser security headers at the edge/frontend, including HSTS, `nosniff`, a referrer policy, and a framing/CSP policy appropriate to deployment.
- **A3-002 / A3-003 / A3-004:** normalize lock ordering across identity/access/email flows, prevent a superseded email claim from sending, and bound replay after the provider’s duplicate-suppression window.
- **A3-005 / A3-006:** extend durable cancellation, expiration, quota release, and completion/attachment convergence to every remaining legacy upload or resource-attachment path, not only the multipart Instructor flow.
- **A3-007:** persist purchase-to-entitlement provenance so replay responses cannot drift after revocation and regrant.
- **A4-002:** add route-appropriate database/storage execution deadlines rather than relying only on HTTP socket timeouts.
- **A4-004:** move legacy single-object completion verification outside the long transaction; multipart verification already uses a durable worker claim, but the legacy path still reads and hashes the object while holding locks.
- **A4-006 / A4-008 / A4-013:** make email claim ownership valid at send time, separate waiting transcodes from short queue work, and isolate FFmpeg CPU/scratch resources.
- **A5-002 through A5-007 and A5-009:** reconcile session idle/absolute lifetime behavior, sign-out failure handling, expired-session redirects, client auth recovery, progress reporting, heartbeat retries, and device-removal routing.

#### P2 scale and data-model preparation

- **A1-004 / A1-005:** add composite relationship constraints or guarded writers tying progress/completion, entitlement scope, invitation, purchase, recipient, and course relationships together while preserving supported legacy and bundle semantics.
- **A2-007:** re-authorize media actions against the current course owner or re-own course media during Admin reassignment.
- **A4-007:** replace the bounded rolling discovery horizon with a durable indexed consumer/watermark after measuring late and delayed event semantics.
- **A4-009 through A4-012:** measure and then optimize roster/analytics aggregation, course-leading enrollment/entitlement indexes, unbounded User 360/account projections, and repeated completion checks on position-only progress writes.
- **RU-02:** `multipart_cleanup.go` always selects the oldest limited batch. Repeated provider cleanup failures in that batch can starve newer abandoned uploads. Add retry scheduling or fair batch rotation and a starvation regression.
- Multipart provider calls still occur while transactions/row locks are held in parts of session recovery, completion, cancellation, and cleanup. Measure pool impact and shorten those critical sections before adding substantial worker concurrency.
- Multipart verification is one claimed upload per worker tick and has a bounded 15-minute verification window. Measure backlog behavior for large files and define explicit admission/backpressure before scaling upload volume.
- **A5-010 / A5-011:** localize transport/server errors and generate route-accurate SEO metadata.

#### P3 engineering quality and operational hardening

- **A2-009 / A2-010:** normalize media existence responses and rate-limit upload initiation; restrict or cache public `/readyz` dependency checks.
- **A5-012 through A5-014:** add localized app error boundaries/not-found handling, idempotency or reconcile-before-retry for ordinary create mutations, and optimistic-concurrency protection for authoring saves.
- **A6-005:** expose worker liveness/readiness or an equivalent orchestration signal instead of only logging repeated dependency failures.
- Complete the remaining `test.fixme`/conditional coverage work, remove broad catches that can hide assertion/setup failures, and make omitted route families fail the authorization matrix.
- Triage the 12 frontend dependency vulnerabilities and existing deprecation warnings recorded by the gate run; no severity or remediation is inferred from the count alone.

## Domain-by-domain conclusion

| Domain | Conclusion at committed `HEAD` |
|---|---|
| A0 architecture | Mapped. The current primary upload architecture is durable multipart direct-to-storage; legacy single PUT remains deliberately visible. |
| A1 data integrity | The verified cross-course media/lesson-binding blockers are repaired structurally. Progress/completion, entitlement provenance/scope, deletion conflicts, and hierarchy concurrency remain. |
| A2 security/privacy | Cross-Instructor media attachment and upload-binding IDORs are repaired and protected delivery already fails closed. CSRF consistency, route-sweep completeness, headers, ownership-reassignment media authority, and low-severity oracles remain. |
| A3 transactions/concurrency | The Admin lockout race and major media retry/convergence paths are repaired. Cross-subsystem lock ordering, email claim/retry, purchase provenance, legacy upload reservations, and academic write skew remain. |
| A4 scale/async | Media-dispatch row retention, User 360 transaction ownership, and metadata admission are repaired. Multipart reduces browser memory/retransmission risk. Request deadlines, legacy completion locks, worker queue fairness/resource isolation, projections/indexes, and cleanup fairness remain. |
| A5 frontend/API | Resumable Instructor recovery and bounded Student signed-URL reauthorization are present. Session, heartbeat retry, error localization, and ordinary mutation recovery findings remain. |
| A6 infrastructure/recovery | Local OTP bootstrap, migration batching/concurrent index creation, and outbox scan bounds are repaired. Worker readiness, provider-specific production acceptance, backups/restore and release gates remain operational authorities outside this audit closure. |
| A7 false greens | Key Admin, Arabic-heading, schema-shape, mutation-persistence, and multipart recovery assertions were strengthened. The authorization matrix omissions and explicit `test.fixme` remain. |

## Scale assessment

This is qualitative; the repository contains no benchmark proving capacity at
1k, 10k, or 100k accounts.

| Workload | Evidence-backed expectation |
|---|---|
| ~1k accounts | Small reads may remain manageable, but request deadline gaps, legacy completion lock duration, and email/transcode behavior are already reachable under concurrency. |
| ~10k accounts | Course-first indexes, roster/analytics aggregation, outbox history, progress completion checks, and account projections become measurement priorities. |
| ~100k accounts | Global analytics, retained history discovery, deep pagination, connection budgets, rollups, and queue/resource isolation require measured plans and explicit backpressure. |
| High simultaneous viewing | HLS segments bypass the API through signed storage URLs; authorization/manifests, Redis playback leases, heartbeats, and progress writes remain API/Redis/database work. Expiry recovery is bounded and reuses these authorities. |

## Recorded verification and authority boundary

Use the [repair evidence](evidence/2026-10-06-final-review-repairs.md) for exact
checkout identity, commands, outcomes, disposable infrastructure, and declared
skips. Local gates passed on frozen software `0235c43`, including the full
catalog/HTTP API integrations and real storage/playback recovery suites.
The rejected `GATES-1.md` committed-head assertion is not closure proof.
Local repair completion is distinct from overall campaign completion: a fresh
independent final review must inspect the frozen tree and return APPROVE.
Production R2/scanner/manual acceptance, migration 0054 legacy-ID preflight,
and deployment authority remain explicitly separate. No production operation
is authorized or performed by this task.
