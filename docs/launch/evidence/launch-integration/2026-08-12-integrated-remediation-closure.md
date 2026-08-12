# Integrated remediation closure — 2026-08-12

## Approved frozen software tree

| Field | Evidence |
|---|---|
| Reviewed base | `18fb7e033d0fad162caebe150fb641a00201e259` |
| Reviewed software head | `2c43b90fcf7a5c5913f42412fad5369911f781aa` |
| Exact range | `18fb7e033d0fad162caebe150fb641a00201e259..2c43b90fcf7a5c5913f42412fad5369911f781aa` |
| Commits | 53 |
| Reviewer / model | `agy` · `gemini-3.1-pro-high` |
| Containment | Disposable detached worktree, bwrap read-only checkout, writable external scratch |
| Reviewer changes | `touchedFiles: []` |
| Original verdict | `VERDICT: APPROVE` |
| Findings | Critical 0 · High 0 · Medium 0 · Low 0 |

The original local run artifacts are
`docs/launch/review/artifacts/18fb7e0-2c43b90-20260812T012106Z/`. That directory is gitignored run
output; this file is its committed closure record.

## Targeted Admin preview supplement

The original report did not explicitly classify the HTTP 404 previously observed in the Admin
review-preview case of `TestProductionPrivilegedMutationRoutesCommitAuditEvidence`. A targeted
independent read-only supplement examined only that issue at the same frozen software head.

| Field | Evidence |
|---|---|
| Supplement verdict | `VERDICT: APPROVE` |
| Classification | `B — DETERMINISTIC_FIXTURE_OR_TEST_ENVIRONMENT_DEFECT` |
| Closure recommendation | `ACCEPT EXISTING APPROVAL` |
| Reviewer changes | `touchedFiles: []` |

The supplement reproduced the 404 deterministically. Authentication, Admin capability enforcement,
routing, submitted graph lookup, READY Asset Version binding, and `ADMIN_CONTENT_PREVIEWED` audit
commit all succeeded. The 404 came afterward from the handler's protected-unavailable guard because
`setupAdminPricingAPIServer` composed the Catalog foundation but omitted the Media foundation. The
known-green HTTP review test composed that same legitimate dependency and returned 200; the focused
media-delivery test also proved protected manifest success, Admin binding, and revalidation after the
revision left `PENDING_REVIEW`.

The supplement artifacts are
`docs/launch/review/artifacts/18fb7e0-2c43b90-20260812T013145Z/`. They are gitignored local output;
this file records the independently returned result.

## Closure semantics

The Product Owner accepts the existing approval under [D-086](../../../DECISIONS.md#d-086--the-integrated-remediation-tree-is-independently-approved-one-post-review-test-fixture-correction-is-authorized).
The independently approved software closure head is exactly
`2c43b90fcf7a5c5913f42412fad5369911f781aa`.

Later commits that record this decision or correct the confirmed test fixture are not members of the
reviewed software range and are not described as independently reviewed software. The authorized
fixture correction changes no production handler, router, authorization, CSRF, playback, media-state,
audit, or entity-binding behavior.

C1 remains `UNRESOLVED_INTERMITTENT_NONREPRODUCIBLE`. The current real-media path is green and T035a
remains installed for sanitized failure-only recurrence evidence; closure does not claim the
historical root cause was identified or fixed.

This record closes the integrated-remediation software review only. It does not resolve open launch
gates, complete S13–S16, perform manual acceptance, or authorize another implementation batch.

## Post-review test-fixture correction

After the software closure head, the independently confirmed fixture defect was corrected by adding
the existing `reviewMediaFoundation(t, p)` dependency to `setupAdminPricingAPIServer`. This changes
only `backend/internal/httpapi/admin_pricing_integration_test.go`. The production handler, router,
authorization, CSRF, protected playback, media-state, audit and entity-binding implementations are
unchanged.

The existing privileged-audit scenario remains non-vacuous: it creates a real PostgreSQL Course,
submitted `PENDING_REVIEW` revision, Lesson and attached READY Asset Version; sends the authenticated
Admin HTTP request through the mounted router; expects HTTP 200; and verifies the durable
`ADMIN_CONTENT_PREVIEWED` row and attached Asset Version.

Verification on 2026-08-12:

| Command | Result |
|---|---|
| `cd backend && go test -tags=integration -v -run TestProductionPrivilegedMutationRoutesCommitAuditEvidence ./internal/httpapi` | PASS; the previous Admin preview 404 is gone and the audit assertion passes |
| `cd backend && go test -tags=integration ./internal/httpapi -count=1` | PASS (`175.520s`) |
| `cd backend && go test ./...` | PASS |
| `cd backend && go vet ./...` | PASS |
| `cd backend && go vet -tags=integration ./internal/httpapi` | PASS |
| `scripts/docs-guard.sh` | PASS |
| `git diff --check` | PASS |
| `scripts/expose-guard.sh` | FAILS on the pre-existing `Secret.Expose()` call in `backend/cmd/e2e-media-diagnostic/main.go` |

The exposure-guard finding predates this post-review correction: `git log -1` attributes the relevant
diagnostic/guard path to `ddada44667b78fe102f0be2238d93ad72f651841` (`test(media): retain failure
pipeline evidence`), which is inside the already reviewed software history. This fixture correction
does not touch or allowlist that call. Resolving or accepting it remains separate automated
release-gate work outside D-086's test-only authority.
