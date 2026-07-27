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

Under the current launch workflow
([D-035](docs/DECISIONS.md#d-035--claude-builds-s1b2-and-agy-reviews)), **Claude is the builder and
planner for the active S1B2 slice** and `agy` is the independent read-only reviewer, dispatched
through `scripts/agy-review.sh <base>..<head>`. Codex exhausted its quota mid-slice; its S1B2 work
through T029 is inherited unchanged, not rewritten. Route all `agy` work through the `agy-delegate`
skill.

Do not review the S1B2 range Claude authors. That is a self-check, not a review, and it cannot close
the slice.

The standing assignment
([D-033](docs/DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review)) is Codex builder
and Claude reviewer, restored only on an explicit developer instruction after Codex quota returns.
When Claude holds the reviewer seat, review only the frozen exact commit range supplied by the
builder, using read-only tools in a disposable detached worktree, and do not edit the review worktree
or the live repository.

Never self-approve. A slice does not close on its builder's own assessment; it closes on a recorded
reviewer verdict against one exact commit range, with every critical and high finding resolved. If
the review produces no retrievable verdict, that is review `UNAVAILABLE`, not approval. Whenever
Claude holds the builder seat, `agy` must hold the reviewer seat.

The user may explicitly reassign the builder and reviewer roles.
