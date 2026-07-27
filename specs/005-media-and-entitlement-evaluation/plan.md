# Implementation Plan: S4 — Media Pipeline, Protected Delivery, and Entitlement Evaluation

**Spec**: [spec.md](spec.md) | **Tasks**: [tasks.md](tasks.md) | **Date**: 2026-07-28

**Builder**: Antigravity under [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews).
**Reviewer**: Claude, **Tier 3** — the deepest tier in the release.

**Blocked until S2 closes.** Frozen and waiting.

---

## Summary

S4 turns uploaded bytes into safely deliverable media, and defines the record that decides who may
reach it. It is scheduled across **two implementation days** by
[§3](../../docs/launch/AUGUST_15_EXECUTION_PLAN.md#3-nineteen-day-execution-plan) — D7 for the
pipeline, D8 for delivery and evaluation — with D9 for remediation and the Tier 3 review. That split
is not administrative: the two halves have different failure modes and different reviewers' questions,
and merging them produces a range too large to review honestly.

## Technical Context

| | |
|---|---|
| New packages | `backend/internal/media` (bytes, scan, transcode), `backend/internal/entitlement` (evaluation) |
| Retires | `backend/internal/video` direct-to-asynq path |
| Storage | S3-compatible, **private**; presigned time-bounded access only |
| Queue | Existing durable queue + outbox boundary |
| Migration | `0011_media_and_entitlement` |
| Scanner | **Adapter behind an interface.** `LG-014` unresolved |

## Constitution Check

- **I — one source of truth**: Asset Versions are immutable; the scan result binds to an exact object
  version. The legacy video path is **retired**, not left as a second way to publish bytes.
- **II — deny by default**: every delivery decision is an explicit allow. Absence of a scan result,
  absence of an Entitlement, and absence of the asset are the **same** outcome.
- **III — traceability**: every FR cites its BR.
- **IV — no second decision point**: entitlement evaluation is **one** function. Handlers do not
  compare expiry, scope, or revocation themselves.
- **V — rigor scales to risk**: Tier 3. Every scanner failure mode, every entitlement scope case, and
  the production-exclusion of the seed mechanism get proofs with mutations.

---

## The ordering decision, and how the code enforces it

S4 evaluates Entitlements; S7 creates them. That is easy to write and easy to erode — the erosion
looks like a test helper that grows a production import, or an Admin "fix access" button added in S8
because support needs one.

**Three structural defences, not one convention:**

1. **`internal/entitlement` exports evaluation only.** No exported constructor, no `Create`, no
   `Grant`. The package's public surface makes creation unavailable rather than discouraged.
2. **The seed mechanism lives behind a build tag** (e.g. `//go:build !production`) so it is **absent
   from the production binary**, not disabled within it. `AUTH_FAKE_MODE` is the precedent this
   project already set; a config flag that *could* be flipped in production is not exclusion.
3. **A test asserts the exclusion**, by building with production constraints and asserting the seed
   symbol is unreachable. Points 1 and 2 are design; point 3 is what survives the next contributor.

> **Why this much machinery for a test fixture.** Because the fixture mints access to paid content.
> Every other control in this release assumes Entitlements are provenance-bound; a seed path reaching
> production would silently make that assumption false, and nothing downstream would detect it.

## Fail-closed, stated as a state machine rather than as an adjective

An asset is deliverable **only** in one state, and everything else is the same non-delivery:

```
UPLOADED → QUARANTINED → SCANNING → SCAN_PASSED → PROCESSING → READY
                              ↓            ↓            ↓
                        SCAN_FAILED   SCAN_ERROR   PROCESS_FAILED
                              └────────────┴────────────┘
                                   all → NOT DELIVERABLE
```

The delivery check is `state == READY`, expressed once. It is **not** a list of excluded states —
an exclusion list is a place to forget a state, and this pipeline will gain states.

**Scanner absence is not a distinct branch.** A missing, unreachable, or unconfigured scanner leaves
assets in `SCANNING` forever, which is already non-deliverable. There is no code path that reasons
"no scanner configured, therefore proceed" — that path is what `LG-014` being unresolved would exploit.

## Entitlement evaluation — one function, total over its inputs

```
Evaluate(student, lesson, now) → Decision
```

The single decision point. It answers, in one place and in this order:

1. Is there a non-revoked Entitlement whose **scope** covers this Lesson? Course grant → every
   contained Section; Section grant → that Section only. Overlapping grants are a **union**. *(BR-024)*
2. Is `now < access_ends_at` on the **effective** expiry, not the original? *(BR-025, BR-026)*
3. Is the Course free of active **emergency access suspension**? *(BR-090)*
4. If the target is retired, does `retirement_eligibility_at` precede `retired_at`? *(BR-027)*

Handlers call it and act on the result. **No handler compares an expiry.** The S1C finding that
produced this rule was a hand-maintained matrix; the S2 finding was a sweep that tested its own
router. Both were "the check exists somewhere" without "the check is the only one."

### The four denial reasons collapse to one response

Expired, revoked, out-of-scope, suspended, retired, and *asset does not exist* return an **identical**
refusal *(BR-023, BR-050, FR-022)*. Internally the reason is typed and audited; externally it is one
response. A distinguishable denial tells an unauthorized caller which Lessons exist and which they
merely cannot reach — which is a content inventory.

## Signed access

- **Playback**: short-lived, **session-scoped**, re-issued per playback session. Deliberately **not**
  single-use — HLS re-requests the same segment on seek, rebuffer, and rendition switch, so true
  single-use breaks playback. *(BR-100, corrected 2026-07-20 in the video design.)*
- **Lab Materials**: MAY be single-use where storage supports it; a one-time download has no repeat
  requirement. *(BR-100)*
- **Expiry mid-playback**: an issued signature remains valid for its short lifetime and no new access
  is issued. The exposure window is bounded by the **signature lifetime**, not by the session, and
  that bound is the reason the lifetime is short. Stated because "revoke instantly" is impossible
  against an already-issued presigned URL, and a plan that claims it would be lying.

## The buyer tag

Applied to **Lab Materials only** *(BR-103)*. Lesson Resources are entitlement-gated and rate-limited
but untagged.

The tag is opaque, derived per purchase, and **must not expose Student PII** — so it is not the
account id, not the email, and not a reversible encoding of either. MVP claims deterrence and
investigability, **not DRM**, and the plan says so rather than implying protection it does not have.

## The legacy cutover

`internal/video` enqueues transcoding directly to asynq with **no scan step and no outbox**. S4
retires it forward-only *(D-031)*.

**Retire, not leave dormant.** A dormant path that publishes unscanned bytes is one route registration
away from being live, and SC-008 asserts it is gone rather than unused. Authentic legacy state is
preserved through the cutover; fake access never becomes commercial provenance.

## Data integrity

- Asset Versions are **immutable**. Replacement creates a version.
- Scan results bind to an **exact object version** — so replacing an object after a passing scan does
  not inherit the pass *(FR-004)*. This is the subtle one: a logical-asset binding would let a
  re-upload ride a previous scan.
- Callback handling is **idempotent** on a provider-supplied identifier, so duplicate, delayed, and
  reordered completions converge to one Asset Version *(FR-009)*.
- Entitlement carries `original_access_ends_at` **and** a separately mutable effective
  `access_ends_at`; adjustments record old, new, reason, actor, timestamp, and support reference
  atomically with immutable audit *(BR-026)*.

## Project Structure

```
backend/internal/media/
├── doc.go            # boundary: bytes only; owns no Course metadata
├── state.go          # the state machine above; READY is the only deliverable state
├── scan.go           # adapter interface; no implementation decides to proceed without a result
├── transcode.go      # HLS, trusted duration, zero-rendition = failure
├── assetversion.go   # immutable versions, idempotent completion
└── delivery.go       # presigned issuance; short-lived, session-scoped

backend/internal/entitlement/
├── doc.go            # boundary: EVALUATION ONLY. Creation is S7
├── evaluate.go       # the single Evaluate function
├── scope.go          # Course covers Sections; Section covers itself; union
└── seed_nonprod.go   # //go:build !production — absent from production builds

backend/internal/db/migrations/0011_media_and_entitlement.{up,down}.sql
```

## Complexity Tracking

| Choice | Simpler alternative | Why rejected |
|---|---|---|
| Build-tag exclusion of the seed path | A config flag defaulting off | A flag that *can* be flipped in production is not excluded from production. This project already holds `AUTH_FAKE_MODE` to the stricter standard |
| Scan bound to object version | Scan bound to logical asset | A re-upload would inherit the previous pass — the failure is silent and the asset is unscanned |
| `state == READY` | A list of excluded states | An exclusion list is a place to forget a state, and this pipeline will gain states |
| One `Evaluate` function | Per-handler checks | Two slices in a row were rejected for a control that existed but was not the only one |
| Retiring the legacy path | Leaving it dormant | A dormant unscanned-publish path is one route registration from live |

## Review checkpoints

| Checkpoint | Blocks | Evidence |
|---|---|---|
| **A — fail-closed** | All delivery work | Every scanner failure mode proven non-delivering, individually |
| **B — no creation path** | D8 start | Production build asserted free of the seed symbol |
| **C — scope evaluation** | Delivery routes | Section/Course/union/expiry/suspension/retirement enumerated over the full graph |
| **D — denial uniformity** | Slice closure | All six denial causes byte-identical to non-existence |
| Final | Slice closure | Full gates, hosted CI on the exact head, independent Tier 3 review |
