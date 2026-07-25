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

Under the default launch workflow ([D-032](docs/DECISIONS.md#d-032--claude-builds-agy-reviews)),
Claude is the builder and `agy` (Antigravity CLI) is the independent read-only reviewer, dispatched
by `scripts/agy-review.sh <base>..<head>`. Codex held the builder seat until 2026-07-25 and no
longer does. The builder never approves its own slice. The user may explicitly reassign these roles.
