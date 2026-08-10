# Gradex Claude Instructions

For launch work, treat [`docs/launch/PLAN.md`](docs/launch/PLAN.md) as the operating protocol and
[`docs/launch/STATUS.md`](docs/launch/STATUS.md) as the current delivery state.

When the user says any of the following, follow the matching section in the launch plan before
taking action:

- `Start the day`
- `Close the day`
- `Launch status`
- `Replan`
- `Go/no-go check`

Inspect repository evidence instead of relying on conversation memory. Preserve unrelated and
user-owned working-tree changes. Do not mark work complete without the evidence required by the
current daily record.

## Current phase — the integrated range was REJECTED; bounded remediation only

The repository is in a **post-review remediation phase**. **S1B3 is not the active slice** — it closed
on 2026-08-01, and any statement naming it as current is spent. The current authority is
[D-084](docs/DECISIONS.md#d-084--the-independent-review-of-the-integrated-launch-range-returned-reject-and-bounded-remediation-of-its-seven-findings-is-authorized),
which supersedes the freeze consequence of
[D-083](docs/DECISIONS.md#d-083--production-implementation-is-frozen-at-afe1624-for-authority-reconciliation-and-one-independent-review)
exactly as far as stated and no further:

- **The independent review of `18fb7e0..48e1f3f` returned `VERDICT: REJECT`** — 4 Critical, 3 High.
  `agy` · `gemini-3.1-pro-high`, contained, clean worktree. The report is transcribed in the
  remediation plan below; the raw run output under `docs/launch/review/artifacts/` is gitignored.
  **The range is not approved.**
- **Production implementation is authorized only for those seven findings** and the tests and evidence
  they directly require, each under its committed task or spec authority. No unrelated feature,
  refactor, redesign, commerce, payment or backlog item. Removing a false commerce *claim* from public
  copy is in scope; adding commerce *function* is not.
- **Authority precedes code.** The owning task or spec amendment is committed before the code that
  satisfies it. Media remediation is **diagnostic-first**: capture the effective configuration before
  changing anything.
- **Claude authored the launch-integration implementation and is ineligible to review it.** Reviewing
  a Claude-authored range is a self-check, not a review, and it cannot close anything.
- **`agy` holds the independent reviewer seat**, dispatched through `scripts/agy-review.sh <base>..<head>`
  and routed through the `agy-delegate` skill. The remediation head needs a fresh independent review of
  the complete integrated tree before approval.

The seven findings, their owners, the four execution batches and the next single action are in
[`docs/launch/STATUS.md`](docs/launch/STATUS.md) and
[`docs/launch/evidence/launch-integration/2026-08-11-post-reject-remediation-plan.md`](docs/launch/evidence/launch-integration/2026-08-11-post-reject-remediation-plan.md).
The read-only audit behind the preceding freeze phase is in
[`docs/launch/evidence/launch-integration/2026-08-10-reality-audit-afe1624.md`](docs/launch/evidence/launch-integration/2026-08-10-reality-audit-afe1624.md).

## Seats

Seats never renew implicitly. Each per-slice assignment expires when its slice closes, and the next
slice requires its own dated assignment.

**Historical, spent seat authority — do not act on these as current:**
[D-032](docs/DECISIONS.md#d-032--claude-builds-agy-reviews),
[D-035](docs/DECISIONS.md#d-035--claude-builds-s1b2-and-agy-reviews) (S1B2, closed at reviewed head
`7d8710e`; Codex's inherited S1B2 work was never rewritten),
[D-036](docs/DECISIONS.md#d-036--claude-builds-s1b3-and-agy-reviews) (S1B3, **closed 2026-08-01**),
[D-037](docs/DECISIONS.md#d-037--claude-builds-s1c-and-agy-reviews),
[D-042](docs/DECISIONS.md#d-042--codex-plans-antigravity-implements-and-claude-independently-reviews),
[D-043](docs/DECISIONS.md#d-043--codex-implements-s2-d5-and-claude-independently-reviews),
[D-044](docs/DECISIONS.md#d-044--antigravity-completes-s2-and-claude-reviews-the-whole-feature-once),
[D-074](docs/DECISIONS.md#d-074--antigravity-builds-s6-course-access-grant-and-claude-independently-reviews).

The dormant standing assignment
([D-033](docs/DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review)) is Codex builder
and Claude reviewer, restored only on an explicit developer instruction after Codex quota returns. It
is not in force during the current frozen phase. When Claude does hold the reviewer seat, review only
the frozen exact commit range supplied by the builder, using read-only tools in a disposable detached
worktree, and do not edit the review worktree or the live repository.

Never self-approve. A slice does not close on its builder's own assessment; it closes on a recorded
reviewer verdict against one exact commit range, with every critical and high finding resolved. If
the review produces no retrievable verdict, that is review `UNAVAILABLE`, not approval. Whenever
Claude holds the builder seat, `agy` must hold the reviewer seat.

The user may explicitly reassign the builder and reviewer roles.
