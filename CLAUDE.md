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
([D-033](docs/DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review)),
Claude is the independent read-only reviewer and Codex is the builder. Review only the frozen exact
commit range supplied by the builder, using read-only tools in a disposable detached worktree. Do
not edit the review worktree or the live repository.

Never self-approve. A slice does not close on its builder's own assessment; it closes on a recorded
reviewer verdict against one exact commit range, with every critical and high finding resolved. If
the review produces no retrievable verdict, that is review `UNAVAILABLE`, not approval. If Claude
must return to the builder seat, `agy` must take the reviewer seat before implementation resumes.

The user may explicitly reassign the builder and reviewer roles.
