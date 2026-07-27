# Implementation Handoff — S2 Course Authoring and Review

**To**: Antigravity (builder) | **From**: Claude (planner/reviewer) | **Date**: 2026-07-28
**Authority**: [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews)

**Do not start until S1C closes.** S1 does not close until S1C closes, and no S2 work begins before
it does. This brief is frozen and waiting, not active.

## Read these, in this order

1. [spec.md](spec.md) — 45 requirements, each traced to the BR rule it implements
2. [plan.md](plan.md) — the design, the four resolved concurrency cases, and the authorization rule
3. [data-model.md](data-model.md) — migration `0009`, every constraint
4. [contracts/](contracts/) — the three API surfaces
5. [tasks.md](tasks.md) — 64 tasks in dependency order
6. [quickstart.md](quickstart.md) — how the work is proven

## The standing clause

> **A required dependency is validated at construction and the component refuses to build without
> it. No security-relevant control may silently degrade, default, or become optional.**

This is not boilerplate. It exists because S1C shipped six instances of one defect class — a control
that silently degrades instead of refusing — in a slice whose entire subject was deny-by-default
enforcement:

| Instance | What it did |
|---|---|
| Conditional CSRF | Protection applied only when a token happened to be present |
| Defaulted recent-auth window | A missing configuration value silently became a permissive default |
| Optional outbox intent | A required delivery record that could be skipped |
| Hand-maintained authorization matrix | Could not detect drift; two real gaps were found once it was derived |
| Context key nobody set | `sessionFromContext` read a key nothing wrote — every staff mutation returned `401` |
| Unvalidated outbox writer | Constructed without its dependency, failing later instead of refusing to build |

If a control cannot be satisfied, **refuse the request**. Do not proceed with less.

## Five rules this slice will be reviewed against

1. **One authorization decision point.** Role and capability decisions go through
   `identity.Authorize` over its closed set. New capability? Add it to the set. A check performed
   beside the gate is a finding, not a style preference.
2. **Ownership coverage is derived, never hand-maintained.** The sweep reads `r.Routes()`. A new
   unguarded route must fail a test rather than depend on a reviewer noticing it.
3. **No upload, scan, or transcode path.** S2 references Asset Versions; S4 produces them
   ([SLICES.md §3.2](../../docs/launch/SLICES.md#32-authoring-owns-media-metadata-the-media-slice-owns-media-bytes)).
   A request body carrying file bytes is a contract violation, not an extension.
4. **Audit and notification intent go in the same transaction as their change.** Not after it, not
   best-effort, not optional.
5. **Every proof must fail under a deliberate mutation.** A test that passes against broken code is
   not evidence. The mutation checks are named tasks, not suggestions.

## How this closes

- Push each checkpoint and let hosted CI verify it as it lands — S1B3 needed only one frozen review
  range because it did this; S1B2 needed two because it did not.
- A **green local suite is not evidence of green CI.** S1B2 proved that with a schema assertion that
  passed locally and failed hosted.
- A frontend build is not evidence unless `.next` was removed first. A build claim that does not say
  "clean" reads as not having been made.
- Offer one exact frozen commit range for review. **Do not review your own range.** The S1C
  self-review returned `APPROVE — 0 critical` and missed nine findings including three criticals;
  it was recorded as a process violation, not absorbed.

## What to do when blocked

Say so and stop. A quota limit mid-edit left `router.go` uncompilable in S1C and the reviewer had to
repair it, which contaminated the review boundary. **An interrupted task reported as interrupted
costs far less than one left in an unknown state.**
