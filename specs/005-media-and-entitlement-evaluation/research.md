# Research — S4 Media and Entitlement Evaluation

**Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md)

## R-001 — How to keep Entitlement creation out of a slice that evaluates Entitlements

**Decision**: three structural defences — no exported creation surface, build-tag exclusion of the
seed path, and a test asserting the exclusion.

| Alternative | Why rejected |
|---|---|
| Config flag defaulting off | A flag that *can* be flipped in production is not excluded from production. This project already holds `AUTH_FAKE_MODE` to the stricter standard, and D-031 forbids fake access becoming commercial provenance |
| Code review and convention | Conventions failed in S1C (hand-maintained matrix) and S2 (self-testing sweep). Both times the convention was correct and unenforced |
| Test-only package with an `_test.go` seed | Works until someone needs the fixture from another package's tests and promotes it. The build tag survives that pressure |
| Nothing — trust the plan | The seed mints access to paid content. Every downstream control assumes Entitlements are provenance-bound |

The `source_order_item_id NOT NULL` constraint in [data-model.md](data-model.md) is a fourth defence
and the strongest: it makes an Entitlement without an Order **unrepresentable**, so neither the seed
nor a future support script can cheat.

## R-002 — Fail-closed when the scanner may not exist at launch

**Decision**: the scanner is an adapter; absence leaves assets in `SCANNING`, which is already
non-deliverable. No code path reasons "no scanner configured, therefore proceed."

`LG-014` is unresolved and may stay that way. The design question is therefore not "how do we scan"
but **"what does an unresolved gate degrade?"** — and the answer must be availability, never safety.

| Alternative | Why rejected |
|---|---|
| Skip scanning when unconfigured | Exactly the exploit an unresolved LG-014 would represent. Assets publish unscanned and nothing reports it |
| A `SKIPPED` scan outcome | Creates a value meaning "not scanned but proceed". The schema deliberately has no such outcome |
| Block all uploads until LG-014 resolves | Blocks S4, S5, and the launch catalogue on an external dependency with no owner. FR-026's Admin-only mode is the survivable version |

**The state machine, not an exclusion list.** Deliverability is `state == READY`. An exclusion list is
a place to forget a state, and this pipeline will gain states — a reviewer cannot tell a forgotten
exclusion from a deliberate one.

## R-003 — Signed access lifetime, and what cannot be promised

**Decision**: short-lived session-scoped playback URLs; optional single-use for Lab Materials.

BR-100 already records the correction: true single-use **breaks HLS**, because playback re-requests
the same segment on seek, rebuffer, and rendition switch. A plan specifying single-use playback would
be specifying a broken player.

**What cannot be promised**: instant revocation of an already-issued presigned URL. Once issued, it is
valid until it expires. The exposure window is bounded by the **signature lifetime**, which is why the
lifetime is short — and [plan.md](plan.md#signed-access) says so rather than claiming a revocation
guarantee the storage layer cannot deliver. This is the same class of correction as S3's OD-002 timing
overclaim: state the bound you have, not the one you want.

## R-004 — The buyer tag

**Decision**: opaque, per purchase, Lab Materials only, no PII.

MVP claims **deterrence and investigability, not DRM** (BR-103), and the plan says so. A tag derived
from the account id or email — even hashed without a secret — is reversible against a known user set
of launch size, so the derivation must be keyed.

## R-005 — Retiring rather than dormanting the legacy video path

**Decision**: retire it; SC-008 asserts it is gone.

`internal/video` enqueues transcoding directly to asynq with **no scan step and no outbox**. Left
dormant it is one route registration away from publishing unscanned bytes, and its absence of a scan
step is invisible to every test that does not exercise it. D-031's forward-only cutover preserves
authentic legacy state; it does not preserve the code path.
