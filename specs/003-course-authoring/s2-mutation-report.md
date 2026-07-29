# S2 Convergence Mutation Report

Date: 2026-07-29

Scope: T059 — route-derived privileged-mutation audit proof.

The mutation below was applied only to the local working tree, its real-PostgreSQL proof was run,
and the exact original code was restored before the green convergence gates.

| Mutation | Failing proof | Observed failure | What it proves | What it does not prove |
|---|---|---|---|---|
| Omit `SetCoursePrice`'s `WriteAuditEvent` call | `TestProductionPrivilegedMutationRoutesCommitAuditEvidence/PUT_/api/v1/admin/courses/:id/price` | The successful HTTP mutation left no `COURSE_PRICE_CHANGED` audit row for the committed Course target. | The production-router enumeration executes the price route and rejects an implementation that commits its state without the required audit evidence. | It does not by itself prove every other route's audit call; the restored complete route enumeration supplies that coverage. |

## Restored-state proof

```text
cd backend
go test -tags=integration ./internal/httpapi -run 'TestProduction(Privileged|Instructor)MutationRoutesCommitAuditEvidence' -count=1
```

## T064 hosted convergence

GitHub Actions run [30465620015](https://github.com/Owlah2025/gradex/actions/runs/30465620015),
attempt 2, completed successfully for commit
`1cedfc51f97b79d662146e22cc6127ef71729da2` on
`feature/002-authentication-rbac`. Backend, Frontend, Migrations, Admission Integration, and Guards
all succeeded. This is the final hosted-CI evidence for S2 range `3d9604e..1cedfc5`.
