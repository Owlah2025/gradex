# Gradex Agent Instructions

For launch work, treat [`docs/launch/PLAN.md`](docs/launch/PLAN.md) as the operating protocol and
[`docs/launch/STATUS.md`](docs/launch/STATUS.md) as the current delivery state.

When the user says any of the following, follow the matching section in the launch plan before
taking implementation action:

- `Start the day`
- `Close the day`
- `Launch status`
- `Replan`
- `Go/no-go check`

Inspect repository evidence instead of relying on conversation memory. Preserve unrelated and
user-owned working-tree changes. Do not mark work complete without the evidence required by the
current daily record.

Under the current launch workflow
([D-035](docs/DECISIONS.md#d-035--claude-builds-s1b2-and-agy-reviews)), Claude is the builder and
planner for the active S1B2 slice and `agy` is the independent read-only reviewer, because Codex
exhausted its quota mid-slice. The standing assignment
([D-033](docs/DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review)) is Codex
builder and Claude reviewer, and the developer restores it explicitly when Codex quota returns.

Whoever holds the reviewer seat, the rules do not change: review one frozen exact commit range from
a disposable detached worktree, and the builder never approves its own slice. Claude must not review
a Claude-authored range. The user may explicitly reassign these roles.
