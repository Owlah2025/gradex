# Tasks: S4 — Media Pipeline, Protected Delivery, and Entitlement Evaluation

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-28

**Builder**: Codex under [D-056](../../docs/DECISIONS.md#d-056--codex-builds-s4-d7-and-an-independent-reviewer-reviews-the-frozen-range) for D7 (`T001`–`T013`) and [D-057](../../docs/DECISIONS.md#d-057--codex-builds-s4-d8-after-d7-approval-independent-tier-3-review-remains-required) for D8 (`T014`–`T032`).
**Reviewer**: Independent reviewers, separate from the builder. **D7 and D8 are independently approved; a builder never closes its own slice.**

**S4 is CLOSED** under [D-058](../../docs/DECISIONS.md#d-058--s4-closes-after-independent-approval-of-d7-and-d8-s5-is-unblocked): D7 is independently approved at `1e3d7c317e3552012b6c73c1f2a7522b2e6b5940`; D8 is independently approved at `944c0a77079d632c6b836c7d60c46ff6144e7aa5`; all `T001`–`T032` are complete.

**Split across two implementation days** per
[§3](../../docs/launch/AUGUST_15_EXECUTION_PLAN.md#3-nineteen-day-execution-plan): **D7** = Phases 1–3
(pipeline), **D8** = Phases 4–6 (delivery and evaluation), **D9** = remediation and Tier 3 review.
The split is deliberate — the halves have different failure modes, and merged they produce a range
too large to review honestly.

---

## Standing clause

> **A required dependency is validated at construction and the component refuses to build without it.
> No security-relevant control may silently degrade, default, or become optional.**

In S4 this clause has teeth it has not had before. The two constructions it forbids here are:

- a delivery path that proceeds when **no scan result exists**;
- an entitlement evaluator that can be constructed **without** its suspension or expiry inputs.

Both would read as reasonable defaults and both would ship unscanned or unauthorized access.

**Tests are required.** Every acceptance proof must **fail under a deliberate mutation**. Eight
instances of one defect class — *a control that reads as enforcement and enforces nothing* — have
appeared across S1C and S2. In every case the builder's own report said the work was clean.

---

# D7 — Pipeline

## Phase 1 — State machine and the fail-closed core

- [X] T001 `backend/internal/media/doc.go` — boundary: media **bytes** only; owns no Course metadata
- [X] T002 Implement the asset state machine in `state.go` exactly as
      [plan.md](plan.md#fail-closed-stated-as-a-state-machine-rather-than-as-an-adjective) specifies.
      **Deliverability is `state == READY`, expressed once.** Do **not** write a list of excluded
      states — that is a place to forget one, and this pipeline will gain states
- [X] T003 Implement the scan **adapter interface** in `scan.go`. `LG-014` is unresolved, so no
      implementation is chosen here. **There must be no code path that reasons "no scanner
      configured, therefore proceed."** Absence of a scanner leaves assets in `SCANNING`, which is
      already non-deliverable
- [X] T004 Bind a scan result to an **exact stored object version** (FR-004). A logical-asset binding
      would let a re-upload inherit a previous pass — silent, and the asset is unscanned

**Checkpoint A — MANDATORY GATE.** Prove fail-closed **per failure mode, individually**, not in
aggregate: scan failure, scanner error, scanner timeout, scanner absence, scanner misconfiguration.
Each must leave the asset non-deliverable. Aggregate proof hides the one mode that passes.

## Phase 2 — Upload, quarantine, Asset Versions

- [X] T005 Direct upload to **quarantine** storage with type and size validation before acceptance
- [X] T006 Immutable Asset Versions; a new upload creates a version and never mutates one
- [X] T007 **Idempotent** completion handling keyed on a provider-supplied identifier — duplicate,
      delayed, and out-of-order callbacks converge to exactly one Asset Version (FR-009)
- [X] T008 Scan/processing state visible to the owning Instructor and to an Admin, with Admin retry

## Phase 3 — Transcode and legacy cutover

- [X] T009 HLS transcode through the durable queue and outbox boundary
- [X] T010 Record the **trusted duration** of the exact Asset Version — S5 computes completion from
      it, so an approximate value here becomes a wrong completion there (BR-051)
- [X] T011 Zero renditions is a **failure**, not an empty success (FR-008)
- [X] T012 **Retire** the legacy `internal/video` direct-to-asynq path. Retire, not disable: a dormant
      path that publishes unscanned bytes is one route registration from live. SC-008 asserts it is
      **gone**
- [X] T013 Migration `0012_media_and_entitlement` up and down, round-tripped against real PostgreSQL;
      raise `db.MaxSchemaVersion` and confirm CI **derives** the assertion

**Checkpoint 1** — bytes flow upload → quarantine → scan → transcode → `READY`, and nothing reaches
delivery before `READY`.

---

# D8 — Delivery and evaluation

## Phase 4 — Entitlement evaluation, and the absence of creation

- [X] T014 `backend/internal/entitlement/doc.go` — boundary: **EVALUATION ONLY. Creation is S6.** *(D-045 removed S7)*
- [X] T015 Implement the grant record: scope, `original_access_ends_at`, effective `access_ends_at`,
      revocation state, and its **typed `grant_source`** with the originating approved course-access
      invitation where the source is `MANUAL_INVITATION` (BR-021, BR-026, BR-028, BR-113). **No Order,
      payment, or checkout reference** — D-045 removed in-platform payments and replaced Order
      provenance with the grant-source discriminator; S6 creates the record, S4 evaluates it
- [X] T016 Implement the **single** `Evaluate(student, lesson, now) → Decision` in `evaluate.go`,
      answering scope, expiry, emergency suspension, and retirement eligibility in that order.
      **No handler compares an expiry.** Two slices in a row were rejected for a control that existed
      but was not the only one
- [X] T017 Implement scope in `scope.go`: Course grant covers every contained Section; Section grant
      covers only its Section; overlapping grants are a **union** and stay independent (BR-024)
- [X] T018 **Export no creation surface.** No `Create`, no `Grant`, no exported constructor that
      writes a grant. The package's public API makes creation unavailable rather than discouraged
      (FR-017)
- [X] T019 Seed mechanism in `seed_nonprod.go` behind `//go:build !production` — **absent from the
      production binary**, not disabled within it (FR-018)
- [X] T020 **Assert the exclusion.** Build with production constraints and assert the seed symbol is
      unreachable. T018 and T019 are design; **this is what survives the next contributor**

**Checkpoint B — MANDATORY GATE, blocks all delivery work.** A production build contains no path that
can mint an Entitlement. Mutation: remove the build tag from `seed_nonprod.go` — T020 must fail.

> Why this much machinery for a test fixture: it mints access to paid content. Every other control in
> the release assumes Entitlements are provenance-bound, and a seed path reaching production would
> make that assumption silently false with nothing downstream detecting it.

## Phase 5 — Protected delivery

- [X] T021 Entitlement checked **before every** signed issuance and **every** download — per download,
      not per session (BR-023, BR-063)
- [X] T022 Playback access: short-lived, **session-scoped**, re-issued per playback session.
      Deliberately **not** single-use — HLS re-requests segments on seek, rebuffer, and rendition
      switch (BR-100)
- [X] T023 Authorize only the **exact** approved or historically qualifying Asset Version (BR-050)
- [X] T024 Protected Resource and Lab Material downloads; Lab Material URLs MAY be single-use
- [X] T025 Opaque per-purchase buyer tag on **Lab Materials only**, never Lesson Resources. Not the
      account id, not the email, not a reversible encoding of either (BR-103)
- [X] T026 Public preview delivery — available only after validation, quarantine, scan success, and
      Instructor publication confirmation (BR-144); no protected content reachable from it (BR-143)
- [X] T027 **No object is publicly readable.** Verify by direct unsigned request to storage (SC-006)
- [X] T028 A redirect never grants access; authorization is server-side on every issuance (FR-025)

## Phase 6 — Denial uniformity and the LG-014 operating mode

- [X] T029 **Checkpoint D.** All six denial causes — expired, revoked, out-of-scope, Account
      suspended, emergency-suspended, retired — return a response **byte-identical** to the response
      for an asset that does not exist. Internally typed and audited; externally one refusal. A
      distinguishable denial is a content inventory (BR-023, BR-050, FR-022)
- [X] T030 Implement FR-026: the documented operating mode with public upload disabled and Admin-only
      catalogue loading with recorded out-of-band scanning. **Build the switch now** — it is the
      planned response to an unresolved `LG-014`, not an improvisation for August 13
- [X] T031 Prove the mid-playback expiry boundary: an issued signature stays valid for its short
      lifetime, no new access is issued, and the exposure window is bounded by **signature lifetime**.
      Do not assert instant revocation of an issued presigned URL — that is not achievable and a test
      claiming it would be false
- [X] T032 Full gate suite per [quickstart.md](quickstart.md), including a **clean** frontend build

---

## Required mutations

Each must turn a test red. Restore after every one.

| # | Mutation | Must fail |
|---|---|---|
| 1 | Make delivery accept `SCAN_ERROR` as deliverable | Checkpoint A |
| 2 | Return "pass" from the scan adapter when no scanner is configured | Checkpoint A |
| 3 | Bind the scan result to the logical asset instead of the object version | T004 |
| 4 | Remove `//go:build !production` from the seed file | T020 |
| 5 | Make a Section grant cover the whole Course | T017 |
| 6 | Compare `original_access_ends_at` instead of the effective expiry | T016 |
| 7 | Skip the emergency-suspension check | T016 |
| 8 | Return `403` for revoked and `404` for non-existent | T029 |
| 9 | Apply the buyer tag to Lesson Resources too | T025 |
| 10 | Make the second transcode callback create a new Asset Version | T007 |

## Dependencies

| Phase | Blocked by |
|---|---|
| 1 | S2 closing |
| 2–3 | **Checkpoint A** |
| 4 | Phase 1 |
| 5 | **Checkpoint B** — no delivery work before creation is proven impossible |
| 6 | Phases 4–5 |

## Task count

32 tasks across two implementation days. **If D7's phases overrun, D8 does not absorb them** — S4 is
already the largest slice and compressing its failure paths is what
[PLAN.md §2](../../docs/launch/PLAN.md#daily-capacity) forbids. Overrun is reported and D9's
remediation window absorbs it, or the slice splits again.

## MVP scope

Every task is required. T030 is required **because** `LG-014` may be unresolved — it is the mechanism
that makes an unresolved gate survivable, so it is not optional polish.

---

## Amendment — 2026-08-11, post-independent-review remediation (T033–T035)

**These tasks did not exist during the original S4 review and are not part of its completion record.**
The 32 tasks above closed on their own evidence; nothing above is reopened, reinterpreted or
unchecked by this amendment. These three were added on 2026-08-11 after the **first valid independent
review** of the integrated launch range `18fb7e0..48e1f3f` returned `VERDICT: REJECT`, whose Critical
finding C1 is that the merged-tree media E2E stalls at `Processing` and Instructor uploads never reach
`READY`. Authority:
[D-084](../../docs/DECISIONS.md#d-084--the-independent-review-of-the-integrated-launch-range-returned-reject-and-bounded-remediation-of-its-seven-findings-is-authorized).

**Diagnostic first.** The cause is `UNKNOWN_NEEDS_EVIDENCE`. The media pipeline is **not** assumed
defective: dispatcher, worker and ffmpeg processor are all present, and environment configuration is
the strongest hypothesis. No production media code changes under T033, and no media redesign is
authorized at all.

- [ ] T033 Capture the **effective** media configuration of the running processes during one real
      `npm run test:e2e:media-authoring` run — not the values a config file appears to set. Record,
      separately for the API process and the worker process: `MEDIA_SCANNER_MODE`,
      `MEDIA_OPERATING_MODE`, `APP_ENV`, `REDIS_ADDR`, and the object-storage endpoint and bucket as
      each process resolved them. Record where the Asset Version stops. Write the capture to
      `docs/launch/evidence/launch-integration/` as the C1 diagnosis. No production code changes in
      this task.
- [ ] T034 **Branch A only** — if T033 shows the effective configuration is wrong (for example a
      scanner resolved to `UNAVAILABLE`, an operating mode resolved to `ADMIN_CATALOGUE`, or the
      worker and API resolved different Redis or bucket targets), apply the **narrow** E2E/runtime
      harness configuration fix in `frontend/playwright.media-authoring.config.ts` and the E2E
      environment/runtime wiring it starts, and prove one green `npm run test:e2e:media-authoring` on
      the merged tree. Scope is the harness/runtime configuration only.
- [ ] T035 **Branch B only** — if T033 shows the effective configuration is correct, stop. Diagnose
      the worker/outbox/dispatcher path read-only, record the finding as evidence, and obtain a
      further task amendment before any production media code is modified. This task closes by
      producing that diagnosis and that amendment request, not by changing the pipeline.

Exactly one of T034 and T035 applies; which one is decided by T033's evidence, never in advance. The
task not taken is closed as `NOT_APPLICABLE` with the diagnosis cited.

---

## Amendment — 2026-08-11, Product Owner-authorized failure-only diagnostic retention (T035a)

**Authority:** Product Owner authorization following the T033 C1 diagnosis recorded at
[`docs/launch/evidence/launch-integration/2026-08-11-t033-media-authoring-diagnosis.md`](../../docs/launch/evidence/launch-integration/2026-08-11-t033-media-authoring-diagnosis.md).

This amendment authorizes only a non-production, failure-only media-authoring E2E diagnostic. It
does not authorize a media behavior change, a scanner change, an authorization change, or any other
production implementation.

- [x] T035a When the media-authoring E2E fails or times out before proving `READY`, capture a
      sanitized, machine-readable artifact **before** normal teardown drops the isolated database and
      stops the worker. The artifact may contain only the current run's safe upload, Asset and Asset
      Version identifiers; authoritative media state and timestamp fields; related media outbox and
      dispatch receipt metadata; related scan, processing and rendition metadata; and Asynq state for
      the existing media task types. It must retain the separately captured non-secret API/worker
      runtime configuration and bounded, correlation-relevant API/worker log excerpts or paths.
      It must exclude credentials, cookies, CSRF values, tokens, API/signing keys, passwords,
      presigned URLs and query parameters, encrypted payloads/ciphertext, raw protected payloads,
      and unrelated database or queue rows. Successful runs retain normal automatic cleanup and emit
      no failure artifact. Prefer a dedicated Go helper using the existing configuration, queue,
      Asynq and database abstractions; add focused deterministic coverage for artifact sanitization
      or collection behavior where practical. No production media code changes are authorized.

T035a is diagnostic support for T035; it does not close T035 or classify the original intermittent
failure. T033's completion remains governed by its own evidence and the post-reject remediation
plan.

### T035a database-driver boundary clarification — 2026-08-12

The Product Owner authorizes the failure-only diagnostic executable's single bounded
`config.Secret.Expose()` call for `DATABASE_URL` solely to pass the resolved DSN directly to the
existing `db.Connect`/pgx driver boundary. The plaintext must not be logged, retained in the
diagnostic JSON, placed in command arguments or filenames, forwarded to another subprocess, or
used for any purpose beyond opening the run-owned PostgreSQL pool before teardown. The executable
must continue to discard connection-error details and retain only T035a's explicitly sanitized
evidence projection. This clarification authorizes the corresponding reviewed exposure-guard
allowlist entry and stronger artifact-sanitization regression coverage; it authorizes no production
media, scanner, storage, database, authorization, or secret-handling behavior change.

**Amended task count:** 36 tasks — the original 32, complete, plus 4 post-review remediation tasks.
