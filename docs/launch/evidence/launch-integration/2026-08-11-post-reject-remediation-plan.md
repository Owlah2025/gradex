# Post-REJECT remediation plan — launch-integration `18fb7e0..48e1f3f`

**Date:** 2026-08-11
**Authority:** [D-084](../../../DECISIONS.md#d-084--the-independent-review-of-the-integrated-launch-range-returned-reject-and-bounded-remediation-of-its-seven-findings-is-authorized)
**Review artifacts:** `docs/launch/review/artifacts/18fb7e0-48e1f3f-20260810T233209Z/` — gitignored
local run output, so the report is transcribed verbatim in §0 below and **this document is the
committed evidence**
**Verdict:** `VERDICT: REJECT` — 4 Critical, 3 High
**State:** PLANNED — nothing in this document is implemented. No task below is complete.

This document is the bounded execution plan for exactly the seven findings of the first valid
independent review. It adds no scope. Where it names a batch, the batch is authorized only by the
task or spec amendment listed against it, and those amendments land before their code.

## 0. The review report, transcribed verbatim

Run metadata from `result.json`: `"tool": "agy"`, `"model": "gemini-3.1-pro-high"`,
`"agyVersion": "1.1.8"`, `"status": "completed"`, `"exitCode": 0`, `"touchedFiles": []`,
`"workdir": "/tmp/gradex-review-18fb7e0-48e1f3f-20260810T233209Z"`, started `2026-08-10T23:32:10.276Z`,
finished `2026-08-10T23:35:01.679Z`. The brief pinned the range `18fb7e0..48e1f3f`.

```text
This review covers the integrated MVP launch range `18fb7e0..48e1f3f`.

While the branch successfully implements significant remediations—including a functional mandatory
password-change flow, real Admin review API backing, transactional email integration, and Instructor
authoring persistence—the integrated tree at HEAD exhibits multiple open Critical and High defects
that block launch.

**Critical Defects (P0):**
*   **Media E2E Failure:** The merged-tree media E2E test fails (stalls at `Processing`). This
    indicates a reproducible integration defect (likely a harness configuration gap or a dispatcher
    issue) that prevents Instructor video uploads from reaching `READY`.
*   **Missing Admin Revision Inspector:** The Admin review queue is now real, but the
    submitted-revision inspector UI is completely absent. An Admin currently approves revisions
    blindly without seeing the submitted content.
*   **Missing Admin Lesson Preview:** The backend serves the `preview` route, but no frontend client
    or UI exists to let an Admin play a submitted video before approval.
*   **Production Staff Onboarding Blocked:** The `EnvDevelopment` hard-gate remains in
    `cmd/api/main.go`, making it impossible to onboard Instructors in staging or production.

**High Defects (P1):**
*   **Fabricated Public Content:** The landing page ships six fabricated Courses with fake prices and
    fabricated testimonials instead of rendering the real public catalog.
*   **Broken Navigation:** The landing page CTAs and footer contain multiple dead links (404s) to
    non-existent routes (`/courses`, `/dashboard`, `/about`, `/teach`, `/contact`).
*   **False Commerce Claims:** The FAQ copy claims Tap hosted checkout is active, violating the
    decision to defer commerce capabilities.

VERDICT: REJECT
```

Two notes on the report, recorded rather than smoothed over. It did not use the brief's pipe-delimited
`FINDINGS` / `COUNTS` sections; the Critical and High counts of 4 and 3 are read from the enumerated
defects above, not from a `COUNTS` block. The verdict sentinel itself is exact, which is why this run
parsed where `20260810T231855Z` did not. The line-wrapping inside the fenced block is this document's;
no wording is changed.

## 1. Finding-to-authority map

| # | Finding | Owning spec | Authority | State |
|---|---|---|---|---|
| C1 | Merged-tree media E2E stalls at `Processing` | [`specs/005-media-and-entitlement-evaluation/`](../../../../specs/005-media-and-entitlement-evaluation/spec.md) | `T033`–`T035a` (diagnostic-first) | UNRESOLVED_INTERMITTENT_NONREPRODUCIBLE |
| C2 | Admin submitted-revision inspector absent | [`specs/003-course-authoring/`](../../../../specs/003-course-authoring/spec.md) | `T067`, `T069`, `T071` | IMPLEMENTED — pending independent re-review |
| C3 | Admin Lesson video preview absent | [`specs/003-course-authoring/`](../../../../specs/003-course-authoring/spec.md) | `T068`, `T070`, `T071`, `T072` | IMPLEMENTED — pending independent re-review |
| C4 | Production staff onboarding blocked | [`specs/002-auth-rbac/s1c/`](../../../../specs/002-auth-rbac/s1c/spec.md) | spec §19 + `T101`–`T105` | OPEN |
| H1 | Fabricated Courses, prices and testimonials on the landing page | [`specs/004-public-catalogue/`](../../../../specs/004-public-catalogue/spec.md) | `T040`, `T041`, `T044` | IMPLEMENTED — pending independent re-review |
| H2 | Dead public routes | [`specs/004-public-catalogue/`](../../../../specs/004-public-catalogue/spec.md) | `T042`, `T045` | IMPLEMENTED — pending independent re-review |
| H3 | FAQ claims Tap hosted checkout exists | [`specs/004-public-catalogue/`](../../../../specs/004-public-catalogue/spec.md) | `T043` | IMPLEMENTED — pending independent re-review |

Every Critical and High has exactly one owner. No finding is owned by "the launch branch".

## 2. Batch A — media E2E diagnosis (C1)

**Diagnostic first. No speculative production media change.**

Step 1 — capture, during one real `npm run test:e2e:media-authoring` run, the *effective* values used
by the API process and by the worker process, not the values a config file appears to set:

- `MEDIA_SCANNER_MODE`
- `MEDIA_OPERATING_MODE`
- `APP_ENV`
- `REDIS_ADDR`
- object-storage endpoint and bucket as the running processes resolved them

Step 2 — run `npm run test:e2e:media-authoring` and record where the Asset Version stops.

Step 3 — classify against the captured evidence:

```text
effective configuration wrong
  (e.g. scanner UNAVAILABLE, operating mode ADMIN_CATALOGUE,
   worker pointed at a different Redis or bucket than the API)
        │
        ├─▶ BRANCH A: the harness/runtime configuration is the defect.
        │   Authorized remediation: the narrow E2E/runtime configuration fix under T034.
        │   Exit: one green test:e2e:media-authoring on the merged tree.
        │
effective configuration correct
        │
        └─▶ BRANCH B: the defect is in the worker / outbox / dispatcher path.
            STOP. Record the diagnosis under T035 and obtain a further task
            amendment before touching production media code.
            No media redesign is authorized by D-084.
```

Batch A exits when the root cause is *proven*, not hypothesised. A green run obtained by changing
production media code without a proven cause does not close it.

## 3. Batch B — Admin submitted-revision inspection and preview (C2, C3)

Implemented together: they are one Admin journey — an Admin cannot responsibly approve what they
cannot read and cannot watch. Authority: `T067`–`T072` in
[`specs/003-course-authoring/tasks.md`](../../../../specs/003-course-authoring/tasks.md).

The backend is not redesigned. Both routes already exist and are served:

```text
GET  /api/v1/admin/review/courses/:id/revisions/:revisionId
POST /api/v1/admin/review/courses/:id/revisions/:revisionId/preview/:lessonId
```

A backend change is authorized only if inspection proves an existing contract is genuinely
insufficient for the required Admin workflow, and that proof is recorded before the change.

**Exit:** an Admin opens the exact `course_id` + `revision_id` from the queue, reads the submitted
content, plays the submitted Lesson video, and approves *that* revision.

## 4. Batch C — production staff composition (C4)

**Blocked on the S1C spec amendment landing first** — §19 of
[`specs/002-auth-rbac/s1c/spec.md`](../../../../specs/002-auth-rbac/s1c/spec.md). Implementation
tasks `T101`–`T105` in [`specs/002-auth-rbac/s1c/tasks.md`](../../../../specs/002-auth-rbac/s1c/tasks.md)
are not authorized to start before that amendment is committed.

The remediation is **not** deleting the environment check. It is defining the production composition
preconditions and failing closed when they are not met.

**Exit:** production-mode composition and authorization tests green.

## 5. Batch D — public surface truthfulness (H1, H2, H3)

Real catalogue data, no fabricated testimonials, no dead public links, no false checkout claims.
Authority: `T040`–`T045` in
[`specs/004-public-catalogue/tasks.md`](../../../../specs/004-public-catalogue/tasks.md).

**Exit:** the anonymous founder acceptance path succeeds and the automated link-integrity and
deferred-commerce copy checks pass.

## 6. Ordering and isolation

```text
Batch A (media diagnosis)   ← first: the Admin journey terminates in media that must be READY
   └─▶ Batch B (Admin inspection + preview)
Batch C (production staff)  ← independent of A/B; starts after the S1C spec amendment
Batch D (public surface)    ← independent of A/B/C
```

Batches B, C and D may be developed in isolated worktrees after their authority commits. Unfinished
work is not merged into `launch-integration-20260810`.

## 7. Validation matrix

| Scope | Required before the batch is called done |
|---|---|
| Batch A | Captured effective media configuration for API **and** worker; a green `npm run test:e2e:media-authoring` under Branch A, or a recorded Branch B diagnosis and a new task amendment |
| Batch B | Frontend `npm test`, `npm run typecheck`, `npm run lint`, `npm run build`; the Admin review E2E of §8; Instructor/Student denial proof for review detail and preview |
| Batch C | Backend `gofmt`, `go build ./...`, `go vet ./...`, identity unit/integration tests, race tests where affected; production-mode composition test; authorization denial matrix |
| Batch D | Frontend `npm test`, `npm run typecheck`, `npm run lint`, `npm run build`; link-integrity regression; deferred-commerce copy check; anonymous acceptance path |

**Global, after all four batches:**

- Backend — `gofmt`, `go build ./...`, `go vet ./...`, relevant unit and integration tests, race tests
  where affected, migration/schema verification where relevant.
- Frontend — `npm test`, `npm run typecheck`, `npm run lint`, `npm run build`.
- Browser proof — media-authoring E2E green; Admin submitted-revision inspector; Admin protected
  Lesson preview; approval of the inspected revision; public landing showing the real catalogue or an
  honest empty state; navigation with no 404s; no deferred-commerce claims in public copy.
- Identity — production composition tests; authorization denial matrix.
- Repository — `scripts/docs-guard.sh`, `scripts/expose-guard.sh`, `git diff --check`.
- Merged-tree regression — the S6 access-grant suites and the S5 protected-learning suites against
  the current schema, not against the ancestors they were written on.

## 8. Founder acceptance journey

Recorded as the post-remediation acceptance path. It is **not** complete until it succeeds end to end
on the remediation head:

```text
Admin signs in on a production-safe identity composition
  └─▶ invites an Instructor
        └─▶ Instructor accepts the invitation and logs in
              └─▶ creates a Course with bilingual metadata (AR + EN title and description)
                    └─▶ sets taxonomy: study year, Major, Subject
                          └─▶ adds a Section
                                └─▶ adds a Lesson
                                      └─▶ uploads a real MP4
                                            └─▶ Asset Version reaches READY
                                                  └─▶ attaches it to the Lesson
                                                        └─▶ submits the Course for review
Admin opens the exact submitted revision from the queue
  └─▶ reads the submitted content (metadata, taxonomy, Sections, Lessons, media state)
        └─▶ previews the actual submitted Lesson video
              └─▶ approves and publishes that exact revision
                    └─▶ the public catalogue displays the real published Course
Admin creates a Course Access Invitation
  └─▶ Student accepts it
        └─▶ Student has zero access until approval
              └─▶ Admin approves
                    └─▶ exactly one Entitlement and one Enrollment exist
                          └─▶ Student opens the Lesson
                                └─▶ the protected video plays
                                      └─▶ progress persists
```

## 9. Final independent review strategy

When all seven findings are remediated and §7 passes, freeze one clean remediation head and dispatch:

```bash
scripts/agy-review.sh 18fb7e033d0fad162caebe150fb641a00201e259..<FINAL_REMEDIATION_HEAD>
```

The range starts at the same base as the rejected review, deliberately. The previous review rejected
the **complete integrated tree**; approval must establish that the complete current tree resolves
those findings without regression, not merely that a diff since the rejection looks reasonable.

- The harness executes from the current tooling line; the reviewer's workspace is the frozen
  remediation head, so the tooling commits are not part of the substantive range.
- A builder self-review closes nothing. The implementation agent does not hold the reviewer seat.
- Approval requires Critical and High both zero, or each remaining one explicitly resolved under the
  repository's defect-acceptance rules.
- A run with no retrievable verdict is `UNAVAILABLE`; a run whose reviewer wrote in its workspace is
  `TAINTED`. Neither is an approval.

## 10. Next single action

Batch A, step 1: capture the effective `MEDIA_SCANNER_MODE`, `MEDIA_OPERATING_MODE`, `APP_ENV`,
`REDIS_ADDR` and storage endpoint/bucket of the running API and worker processes during one
`npm run test:e2e:media-authoring` run, under `T033`. Nothing else starts first.

## 11. D-085 C1 sequencing supersession — 2026-08-11

[D-085](../../../DECISIONS.md#d-085--c1-remains-an-unresolved-intermittent-non-reproducible-defect-batch-b-is-authorized-to-proceed)
supersedes only the Batch A → Batch B sequencing statement in §§2, 6, and 10. The historical C1
failure is `UNRESOLVED_INTERMITTENT_NONREPRODUCIBLE`, not resolved: repeated real-MP4 E2E runs reached
`READY` under matching effective API/worker development configuration, while the original stop did
not reproduce. T035a remains the failure-only sanitized recurrence-evidence mechanism.

Batch B is now the next authorized implementation action. The original Batch A evidence and its
diagnostic-first production-media boundary remain unchanged; this decision authorizes no media
behavior change and does not close C1.
