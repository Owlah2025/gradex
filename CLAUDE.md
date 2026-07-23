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

Under the default launch workflow, Claude reviews the builder's stable diff or commit without
editing it. Report critical and high findings for correction before the slice closes. The user may
explicitly reassign the builder and reviewer roles.
