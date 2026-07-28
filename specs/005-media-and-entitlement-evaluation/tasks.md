# Tasks: S4 — Media Pipeline, Protected Delivery, and Entitlement Evaluation

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-28

**Builder**: Antigravity under [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews).
**Reviewer**: Claude, **Tier 3**. **A builder never closes its own slice.**

**Blocked until S2 closes** on an independent verdict.

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

- [ ] T001 `backend/internal/media/doc.go` — boundary: media **bytes** only; owns no Course metadata
- [ ] T002 Implement the asset state machine in `state.go` exactly as
      [plan.md](plan.md#fail-closed-stated-as-a-state-machine-rather-than-as-an-adjective) specifies.
      **Deliverability is `state == READY`, expressed once.** Do **not** write a list of excluded
      states — that is a place to forget one, and this pipeline will gain states
- [ ] T003 Implement the scan **adapter interface** in `scan.go`. `LG-014` is unresolved, so no
      implementation is chosen here. **There must be no code path that reasons "no scanner
      configured, therefore proceed."** Absence of a scanner leaves assets in `SCANNING`, which is
      already non-deliverable
- [ ] T004 Bind a scan result to an **exact stored object version** (FR-004). A logical-asset binding
      would let a re-upload inherit a previous pass — silent, and the asset is unscanned

**Checkpoint A — MANDATORY GATE.** Prove fail-closed **per failure mode, individually**, not in
aggregate: scan failure, scanner error, scanner timeout, scanner absence, scanner misconfiguration.
Each must leave the asset non-deliverable. Aggregate proof hides the one mode that passes.

## Phase 2 — Upload, quarantine, Asset Versions

- [ ] T005 Direct upload to **quarantine** storage with type and size validation before acceptance
- [ ] T006 Immutable Asset Versions; a new upload creates a version and never mutates one
- [ ] T007 **Idempotent** completion handling keyed on a provider-supplied identifier — duplicate,
      delayed, and out-of-order callbacks converge to exactly one Asset Version (FR-009)
- [ ] T008 Scan/processing state visible to the owning Instructor and to an Admin, with Admin retry

## Phase 3 — Transcode and legacy cutover

- [ ] T009 HLS transcode through the durable queue and outbox boundary
- [ ] T010 Record the **trusted duration** of the exact Asset Version — S5 computes completion from
      it, so an approximate value here becomes a wrong completion there (BR-051)
- [ ] T011 Zero renditions is a **failure**, not an empty success (FR-008)
- [ ] T012 **Retire** the legacy `internal/video` direct-to-asynq path. Retire, not disable: a dormant
      path that publishes unscanned bytes is one route registration from live. SC-008 asserts it is
      **gone**
- [ ] T013 Migration `0012_media_and_entitlement` up and down, round-tripped against real PostgreSQL;
      raise `db.MaxSchemaVersion` and confirm CI **derives** the assertion

**Checkpoint 1** — bytes flow upload → quarantine → scan → transcode → `READY`, and nothing reaches
delivery before `READY`.

---

# D8 — Delivery and evaluation

## Phase 4 — Entitlement evaluation, and the absence of creation

- [ ] T014 `backend/internal/entitlement/doc.go` — boundary: **EVALUATION ONLY. Creation is S7.**
- [ ] T015 Implement the grant record: scope, `original_access_ends_at`, effective `access_ends_at`,
      revocation state, source Order reference (BR-021, BR-026, BR-028)
- [ ] T016 Implement the **single** `Evaluate(student, lesson, now) → Decision` in `evaluate.go`,
      answering scope, expiry, emergency suspension, and retirement eligibility in that order.
      **No handler compares an expiry.** Two slices in a row were rejected for a control that existed
      but was not the only one
- [ ] T017 Implement scope in `scope.go`: Course grant covers every contained Section; Section grant
      covers only its Section; overlapping grants are a **union** and stay independent (BR-024)
- [ ] T018 **Export no creation surface.** No `Create`, no `Grant`, no exported constructor that
      writes a grant. The package's public API makes creation unavailable rather than discouraged
      (FR-017)
- [ ] T019 Seed mechanism in `seed_nonprod.go` behind `//go:build !production` — **absent from the
      production binary**, not disabled within it (FR-018)
- [ ] T020 **Assert the exclusion.** Build with production constraints and assert the seed symbol is
      unreachable. T018 and T019 are design; **this is what survives the next contributor**

**Checkpoint B — MANDATORY GATE, blocks all delivery work.** A production build contains no path that
can mint an Entitlement. Mutation: remove the build tag from `seed_nonprod.go` — T020 must fail.

> Why this much machinery for a test fixture: it mints access to paid content. Every other control in
> the release assumes Entitlements are provenance-bound, and a seed path reaching production would
> make that assumption silently false with nothing downstream detecting it.

## Phase 5 — Protected delivery

- [ ] T021 Entitlement checked **before every** signed issuance and **every** download — per download,
      not per session (BR-023, BR-063)
- [ ] T022 Playback access: short-lived, **session-scoped**, re-issued per playback session.
      Deliberately **not** single-use — HLS re-requests segments on seek, rebuffer, and rendition
      switch (BR-100)
- [ ] T023 Authorize only the **exact** approved or historically qualifying Asset Version (BR-050)
- [ ] T024 Protected Resource and Lab Material downloads; Lab Material URLs MAY be single-use
- [ ] T025 Opaque per-purchase buyer tag on **Lab Materials only**, never Lesson Resources. Not the
      account id, not the email, not a reversible encoding of either (BR-103)
- [ ] T026 Public preview delivery — available only after validation, quarantine, scan success, and
      Instructor publication confirmation (BR-144); no protected content reachable from it (BR-143)
- [ ] T027 **No object is publicly readable.** Verify by direct unsigned request to storage (SC-006)
- [ ] T028 A redirect never grants access; authorization is server-side on every issuance (FR-025)

## Phase 6 — Denial uniformity and the LG-014 operating mode

- [ ] T029 **Checkpoint D.** All six denial causes — expired, revoked, out-of-scope, Account
      suspended, emergency-suspended, retired — return a response **byte-identical** to the response
      for an asset that does not exist. Internally typed and audited; externally one refusal. A
      distinguishable denial is a content inventory (BR-023, BR-050, FR-022)
- [ ] T030 Implement FR-026: the documented operating mode with public upload disabled and Admin-only
      catalogue loading with recorded out-of-band scanning. **Build the switch now** — it is the
      planned response to an unresolved `LG-014`, not an improvisation for August 13
- [ ] T031 Prove the mid-playback expiry boundary: an issued signature stays valid for its short
      lifetime, no new access is issued, and the exposure window is bounded by **signature lifetime**.
      Do not assert instant revocation of an issued presigned URL — that is not achievable and a test
      claiming it would be false
- [ ] T032 Full gate suite per [quickstart.md](quickstart.md), including a **clean** frontend build

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
