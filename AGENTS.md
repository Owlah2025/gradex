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
([D-033](docs/DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review)),
Codex is the builder and Claude is the independent read-only reviewer. Review one frozen exact
commit range from a disposable detached worktree; the builder never approves its own slice. `agy`
remains the fallback reviewer when Claude is unavailable. The user may explicitly reassign these
roles.
