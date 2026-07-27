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
([D-036](docs/DECISIONS.md#d-036--claude-builds-s1b3-and-agy-reviews)), **Claude is the builder and
planner for the active S1B3 slice** and `agy` is the independent read-only reviewer, dispatched
through `scripts/agy-review.sh <base>..<head>`. Route all `agy` work through the `agy-delegate`
skill.

D-036 is scoped to S1B3 alone and expires when that slice closes. Seats never renew implicitly —
S1C requires its own dated assignment. S1B2 ran under the equivalent
[D-035](docs/DECISIONS.md#d-035--claude-builds-s1b2-and-agy-reviews) and closed at reviewed head
`7d8710e`; Codex's inherited S1B2 work was never rewritten.

Do not review the S1B3 range Claude authors. That is a self-check, not a review, and it cannot close
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
