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

## Current phase — production code is FROZEN

The repository is in a **frozen launch-integration reconciliation and review phase**, not feature
implementation. **S1B3 is not the active slice** — it closed on 2026-08-01, and any statement naming
it as current is spent. The current authority is
[D-083](docs/DECISIONS.md#d-083--production-implementation-is-frozen-at-afe1624-for-authority-reconciliation-and-one-independent-review):

- **Production code is frozen at `afe1624d4cdb117c57aed3fc86594e5ebdb4074b`** pending one independent
  review. Do not change backend, frontend, migrations, tests, deploy scripts, or runtime
  configuration.
- **No new production implementation is authorized** until that review is complete and every Critical
  and High finding is resolved. A successful review does not by itself authorize the next feature —
  that still requires its own existing or amended SpecKit task authority, selected after the verdict
  exists.
- **Claude authored the launch-integration implementation and is ineligible to review it.** Reviewing
  a Claude-authored range is a self-check, not a review, and it cannot close anything.
- **`agy` holds the independent reviewer seat** for the frozen integrated range, dispatched through
  `scripts/agy-review.sh <base>..<head>` and routed through the `agy-delegate` skill.

The range is **not approved**. No verdict exists yet. Current delivery state, the exact frozen review
range, the recorded SpecKit task/spec gaps, and the open P0 items are in
[`docs/launch/STATUS.md`](docs/launch/STATUS.md); the read-only audit behind this phase is in
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
