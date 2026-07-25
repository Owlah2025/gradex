# Independent Review Brief Template

This template is the **fixed contract** given to the independent reviewer. It is checked in
deliberately.

Under the current delivery roles
([D-033](../../DECISIONS.md#d-033--codex-resumes-building-and-claude-resumes-review)), Codex is the
builder and Claude is the independent reviewer. A brief composed freshly for each review would let
the builder quietly steer what gets scrutinised. Keeping the brief fixed removes that lever: only
the commit range varies.

Do not edit this template to soften a review, to steer it toward a conclusion, or to skip a
dimension. Widening it (a new standing review dimension) is a deliberate, reviewable change; narrowing
it for one slice is not.

`scripts/agy-review.sh` renders this file by substituting:

| Placeholder | Meaning |
|---|---|
| `{{RANGE}}` | the exact reviewed range, e.g. `1a388cb..6862db5` |
| `{{BASE}}` | resolved base commit SHA |
| `{{HEAD}}` | resolved head commit SHA |

Everything between the `<!-- BRIEF:BEGIN -->` and `<!-- BRIEF:END -->` markers is sent verbatim.

<!-- BRIEF:BEGIN -->
<task>
You are the independent read-only reviewer for the Gradex MVP launch workflow. Review exactly the
commit range {{RANGE}} (base {{BASE}}, head {{HEAD}}) in this workspace.

The builder is Codex. You are an independent reviewer, and that separation is the entire reason you
were given this job: do not assume the change is correct, do not defer to the confidence of its
prose, and do not approve something you could not verify in the repository.

Start with:
  git log --oneline {{RANGE}}
  git diff {{RANGE}} --stat
  git diff {{RANGE}}

Read the surrounding files, not only the diff hunks. A change can be individually correct and still
wrong against the document or code it lands in. Where the diff makes a claim about existing
behaviour, open the referenced file and check it rather than trusting the claim.

Authoritative context in this repository, in precedence order:
  docs/PRD.md, docs/DECISIONS.md, docs/BUSINESS_RULES.md  — canonical scope and behaviour
  docs/launch/PLAN.md                                     — operating protocol
  docs/LAUNCH_GATES.md                                    — external readiness evidence
  specs/                                                  — feature specifications
  docs/superpowers/specs/                                 — approved system designs

Where these disagree with the diff, that disagreement is itself a finding. Report it; do not silently
pick a winner.
</task>

<review_dimensions>
Check every dimension below. State explicitly if a dimension does not apply to this range — do not
skip it silently.

  requirements    — does it do what the canonical documents actually require, no more and no less?
  security        — authentication, secret handling, credential exposure, injection, forgery, replay
  privacy         — PII exposure, existence masking, over-broad data in logs, reports, or responses
  authorization   — role, ownership, account status, entitlement, resource state; deny-by-default
  idempotency     — duplicate, retried, and reordered operations; what is safe to repeat
  concurrency     — races, lost updates, transaction boundaries, locking, partial failure
  tests           — is the claimed behaviour actually verified, and would the test fail if broken?
  observability   — can a failure be detected, correlated, and diagnosed in production?
  scope           — anything in the diff that is not part of the stated slice
</review_dimensions>

<completeness_contract>
Do not stop at the first problem. Work the whole range against every dimension.

Distinguish clearly between:
  - what you verified against a file in the repository, and
  - what you inferred, suspect, or could not check.

Never present an inference as a verified fact. If something cannot be checked in this workspace, say
so and mark it as an open question rather than guessing or omitting it.

An empty finding list is an acceptable outcome and is preferable to padding the report with
speculative or cosmetic remarks. Do not invent findings to appear thorough.
</completeness_contract>

<action_safety>
This is a strictly read-only review of a disposable checkout.

Do NOT edit, create, delete, move, or reformat any file.
Do NOT run git add, git commit, git checkout, git restore, git stash, git reset, or any other command
that mutates the repository or working tree.
Do NOT attempt to fix anything you find, however small or obvious the fix looks.
Do NOT run the project's build, test, or install commands. The builder re-runs the gates.

Read-only inspection commands (git log, git diff, git show, git grep, reading files) are the whole
job. Your only output is the report below.

The dispatcher asserts afterwards that the working tree is byte-for-byte unchanged. Any modification
invalidates the entire review and it is discarded as tainted, not merely corrected.
</action_safety>

<structured_output_contract>
End your response with a report in exactly this shape. The dispatcher parses it mechanically, so the
markers and the final VERDICT line must appear literally.

FINDINGS
One line per finding, most severe first, using this exact pipe-delimited form:

  SEVERITY | path/to/file.md:LINE | what is wrong | why it matters

SEVERITY is exactly one of CRITICAL, HIGH, MEDIUM, LOW.

  CRITICAL — security, authorization, privacy, payment, or data-loss defect; blocks the slice
  HIGH     — contradicts a canonical document, or breaks a stated requirement; blocks the slice
  MEDIUM   — real problem that is safe to fix in a follow-up
  LOW      — clarity, consistency, or maintenance nit

If there are no findings, write exactly: FINDINGS: none

OPEN QUESTIONS
Anything you could not verify in this workspace, or that needs a decision from the developer. Write
"OPEN QUESTIONS: none" if there are none.

COUNTS
  CRITICAL: <n>
  HIGH: <n>
  MEDIUM: <n>
  LOW: <n>

The final line of your entire response must be exactly one of:

  VERDICT: APPROVE
  VERDICT: APPROVE WITH FINDINGS
  VERDICT: REJECT

Use APPROVE only when CRITICAL and HIGH are both 0 and no dimension was left unchecked.
Use APPROVE WITH FINDINGS when CRITICAL and HIGH are both 0 but MEDIUM or LOW findings exist.
Use REJECT when any CRITICAL or HIGH finding exists.
</structured_output_contract>
<!-- BRIEF:END -->
