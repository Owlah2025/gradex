# Remaining S2 Implementation Handoff

**Planner/orchestrator**: Codex

**Implementer**: Antigravity, exact model `gemini-3.6-flash-high`

**Authority**:
[D-044](../../docs/DECISIONS.md#d-044--antigravity-completes-s2-and-claude-reviews-the-whole-feature-once)

## Objective

Complete the existing S2 feature in `specs/003-course-authoring/`. Do not create or select another
feature directory. T001–T038 and their evidence are historical and must not be rewritten or
reimplemented. Process only the task range named in each dispatch.

Before editing, read `.agents/skills/speckit-implement/SKILL.md` completely and follow it as the
implementation workflow. The Antigravity CLI has no imported SpecKit plugin; this repository skill
is the command authority.

## Queue

1. T039–T042 — pricing
2. T043–T050 — lifecycle, ownership, deletion, retirement, and emergency controls
3. T051–T054 — taxonomy administration
4. T055–T057 — voluntary password-change evidence
5. T058–T064 — whole-feature wiring, findings, documentation, gates, convergence, CI handoff

Each dispatch starts from a clean tree and stops after its named range. Do not run `git add`,
`git commit`, `git push`, or Claude review. Codex reviews, reruns gates, and commits.

## Standing constraints

- Use the existing `identity.Authorize` closed capability set and production composition root.
- Every mutation is protected by the existing session Origin/CSRF boundary before authorization.
- Required dependencies fail construction when absent; no control is optional or defaulted.
- Privileged state, audit evidence, and required outbox intent commit atomically.
- No external notification delivery occurs inside a transaction.
- No real Entitlement, Order, payment, refund, payout, Enrollment, progress, upload, or media object
  is created or rewritten.
- Preserve stable Section/Lesson identities and explicit revision authority.
- Keep `.caveman.json` untouched.
- Leave unrelated user changes and unrelated refactoring alone.

## Verification

Run the task-specific real-PostgreSQL integration and mutation proof, then the applicable backend
and frontend gates. The final queue runs every command in `quickstart.md`, including integration
tests under `-race`, a clean frontend build, documentation guard, and exposure guard.

End every dispatch with:

1. What changed and why
2. Files touched
3. Exact gate outcomes and counts
4. Deviations, unresolved decisions, or scope pressure
