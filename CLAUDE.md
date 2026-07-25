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

Under the default launch workflow ([D-032](docs/DECISIONS.md#d-032--claude-builds-agy-reviews)),
Claude is the builder: plan the bounded slice, implement it, run its checks, document the evidence,
and correct review findings. The independent read-only reviewer is `agy` (Antigravity CLI), a
different model family, dispatched by `scripts/agy-review.sh <base>..<head>`.

Never self-approve. A slice does not close on Claude's own assessment of Claude's own work — it
closes on a recorded reviewer verdict against one exact commit range, with every critical and high
finding resolved. If the reviewer produces no retrievable verdict, that is review `UNAVAILABLE`, not
approval.

The user may explicitly reassign the builder and reviewer roles.
