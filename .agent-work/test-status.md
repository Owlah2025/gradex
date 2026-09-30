# Test status

## Baseline (2026-09-29 ~21:30)
- backend go build/vet/test: PASS
- frontend typecheck/lint/test: PASS
- integration core pkgs (db identity outbox catalogpublic ratelimit learning access entitlement): PASS
- integration httpapi: PASS (240s)

## Final XHigh repair pass (2026-09-30)

### Backend gates

- `gofmt -w <changed Go files> && git diff --check`: PASS.
- `go build ./...`: PASS.
- `go vet ./...`: PASS.
- `go test ./...`: PASS.
- `go test -tags=integration ./...`: PASS after starting the repository-local `minio` and `minio-init` services from `backend/docker-compose.yml`.
- Schema-range regression: `go test ./cmd/migrate` PASS; output contract tested as `53 53`.

### Frontend gates

- `npm run typecheck`: PASS.
- `npm run lint`: PASS; Next lint reported no warnings or errors.
- `npm test`: PASS; Node test summary: 847 tests, 847 pass, 0 fail, 0 skipped.
- `npm run build`: PASS.

### Playwright

- New V2 Admin + Student + Instructor command: PASS, 20 passed.
- Focused Student rerun: PASS, 7 passed.
- Final raw V2 Admin + Student + Instructor rerun: PASS, 20 passed.
- Affected existing acceptance bundle (`s2-taxonomy-viewport`, `t3-student-academic-profile`, `uxh-public-auth-account`, `uxj-release-acceptance`): 102 passed, 4 failed, 7 did not run. The failures were deterministic fixture/state defects: T3/UX-H launch-catalog import returns HTTP 409 on a fresh isolated run; UX-J phone task has no pending access-request row at its point in the shared lane.
- Isolated T3 rerun: 1 failed, 5 did not run; the same launch-catalog import HTTP 409 reproduced before any browser journey.
- Isolated UX-J phone-task rerun: 1 failed because no actionable access request was present.
- Isolated `s2-taxonomy-viewport`: PASS, 12 passed.
