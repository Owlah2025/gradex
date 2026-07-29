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
