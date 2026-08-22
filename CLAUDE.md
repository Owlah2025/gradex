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

## Current phase — MVP functional completion, one tranche at a time

[D-089](docs/DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time)
opens a bounded implementation stream on top of the D-086 freeze. Read
[`docs/mvp/FUNCTIONAL_COMPLETION.md`](docs/mvp/FUNCTIONAL_COMPLETION.md) first: it is the canonical
completion tracker and its `MVP-Fxx` queue is the only authorized work list.

- Scope is limited to gaps recorded in that tracker. One tranche at a time; none starts automatically.
- Production code changes only where tracing proves it is necessary to close an identified gap.
- Visual UI/UX work stays paused ([`docs/ux/`](docs/ux/README.md)). Functional UI changes are
  authorized only when a canonical capability is otherwise unreachable through the product.
- Security, authorization, entitlement, lifecycle, and product contracts remain authoritative. Never
  relax an assertion to obtain a green run.
- `E2E_PROVEN` requires an observed green run against real layers. Code inspection never qualifies.

The freeze below remains the baseline that D-089 amends.

### Prior phase — integrated remediation approved; release gates remain

The complete integrated remediation range
`18fb7e033d0fad162caebe150fb641a00201e259..2c43b90fcf7a5c5913f42412fad5369911f781aa`
received an independent `VERDICT: APPROVE` with no findings. A targeted independent supplement also
returned `VERDICT: APPROVE` and classified the Admin preview audit-test 404 as
`B — DETERMINISTIC_FIXTURE_OR_TEST_ENVIRONMENT_DEFECT`. The Product Owner accepted the existing
approval under
[D-086](docs/DECISIONS.md#d-086--the-integrated-remediation-tree-is-independently-approved-one-post-review-test-fixture-correction-is-authorized).

- The independently approved software head is exactly `2c43b90fcf7a5c5913f42412fad5369911f781aa`.
- Later closure-documentation and test-fixture commits are not part of that reviewed software range.
- C1 remains `UNRESOLVED_INTERMITTENT_NONREPRODUCIBLE`; T035a stays installed.
- D-086's minimum post-review Admin preview test-fixture correction is complete. It changed no
  production behavior and authorizes no new implementation batch.
- Release-gate, external-provider/infrastructure, and manual-acceptance work remain separate and must
  follow their current authority.

Current delivery state and committed closure evidence are in [`docs/launch/STATUS.md`](docs/launch/STATUS.md)
and [`docs/launch/evidence/launch-integration/2026-08-12-integrated-remediation-closure.md`](docs/launch/evidence/launch-integration/2026-08-12-integrated-remediation-closure.md).

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
