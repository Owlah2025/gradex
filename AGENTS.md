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

## Current phase — the integrated range was REJECTED; bounded remediation only

The repository is in a **post-review remediation phase**. This is the current authority
([D-084](docs/DECISIONS.md#d-084--the-independent-review-of-the-integrated-launch-range-returned-reject-and-bounded-remediation-of-its-seven-findings-is-authorized)),
which supersedes the freeze consequence of
[D-083](docs/DECISIONS.md#d-083--production-implementation-is-frozen-at-afe1624-for-authority-reconciliation-and-one-independent-review)
exactly as far as stated and no further:

- **The independent review of `18fb7e0..48e1f3f` returned `VERDICT: REJECT`** — 4 Critical, 3 High,
  from `agy` · `gemini-3.1-pro-high` under read-only containment with a clean reviewer worktree.
  **The range is not approved.**
- **Production implementation is authorized only for those seven findings** and the tests and evidence
  they directly require, each under its committed task or spec authority. No unrelated feature,
  refactor, redesign, commerce, payment or backlog item is authorized.
- **Authority precedes code.** The owning task or spec amendment is committed before the code that
  satisfies it. Media remediation is diagnostic-first.
- **Claude authored the launch-integration implementation and is ineligible to review it.** That is a
  self-check, not a review, and it cannot close anything.
- **`agy` is the independent reviewer**, dispatched through `scripts/agy-review.sh <base>..<head>`. The
  remediation head needs a fresh independent review of the complete integrated tree before approval.

The seven findings, their owners, the execution batches and the next single action are in
[`docs/launch/STATUS.md`](docs/launch/STATUS.md) and
[`docs/launch/evidence/launch-integration/2026-08-11-post-reject-remediation-plan.md`](docs/launch/evidence/launch-integration/2026-08-11-post-reject-remediation-plan.md).

## Seats

Whoever holds the reviewer seat, the rules do not change: review one frozen exact commit range from
a disposable detached worktree, and the builder never approves its own slice. Claude must not review
a Claude-authored range. A review that produces no retrievable verdict is `UNAVAILABLE`, not
approval. The user may explicitly reassign these roles.

Seats never renew implicitly. Each per-slice assignment expires when its slice closes, and the next
slice requires its own dated assignment.

**Historical, spent seat authority — do not act on these:**
[D-032](docs/DECISIONS.md#d-032--claude-builds-agy-reviews),
[D-035](docs/DECISIONS.md#d-035--claude-builds-s1b2-and-agy-reviews),
[D-036](docs/DECISIONS.md#d-036--claude-builds-s1b3-and-agy-reviews) (S1B3 — **closed 2026-08-01**),
[D-037](docs/DECISIONS.md#d-037--claude-builds-s1c-and-agy-reviews),
[D-042](docs/DECISIONS.md#d-042--codex-plans-antigravity-implements-and-claude-independently-reviews),
[D-043](docs/DECISIONS.md#d-043--codex-implements-s2-d5-and-claude-independently-reviews),
[D-044](docs/DECISIONS.md#d-044--antigravity-completes-s2-and-claude-reviews-the-whole-feature-once),
[D-074](docs/DECISIONS.md#d-074--antigravity-builds-s6-course-access-grant-and-claude-independently-reviews).
The dormant standing assignment
([D-033](docs/DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review)) is Codex builder
and Claude reviewer; the developer restores it explicitly when Codex quota returns. It is not in force
during the current frozen phase.
