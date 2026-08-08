# Quickstart: S11 Release Acceptance

## Prerequisites

- PostgreSQL and Redis available for the existing local harness
- Docker with the existing S12 disposable production-like environment already prepared for the HTTPS run
- Frontend dependencies installed and Playwright Chromium available
- Clean worktree at the exact revision being accepted

## 1. Static and integration gate

```bash
cd backend
gofmt -w ./cmd/e2e-seed
go test ./cmd/e2e-seed ./internal/httpapi ./internal/access ./internal/entitlement ./internal/learning ./internal/media
go test ./...

cd ../frontend
npm run typecheck
npm test
npm run lint
npm run build
```

The focused command must include the complete identity journey, invalid/replayed action-secret coverage, concurrent approval, deny-side-effect checks, and protected learning tests selected in `contracts/traceability.md`.

## 2. Local browser journey

```bash
cd frontend
npx playwright test e2e/s11-release-acceptance.spec.ts --workers=1
```

Expected: registration, verification, login, Invitation acceptance, zero pre-approval access, approval, exact provenance/cardinality, Course/Lesson/playback, Progress, unrelated denial, invalid-secret recovery, and authorized replay all pass against a fresh isolated database.

## 3. Disposable production-like HTTPS journey

```bash
./deploy/scripts/environment.sh verify
./deploy/scripts/verify-s11-release-acceptance.sh
```

Expected: the S11 entry point runs the selected recovery/concurrency integrations, then reuses the S12 deployment, database safety gate, certificate pin, media fixture, browser harness, and protected manifest/segment verification. The active `gradex` database remains untouched. This production-mode run begins at real HTTP login because registration is safety-disabled until the approved production adapters exist.

## 4. Schema and scope audit

Verify schema version 15, no new migration, no provider file change, no S8 or Entitlement-update behavior, and no commerce term in changed production paths.

## 5. Evidence and freeze

Record exact commands and results in `docs/launch/evidence/s11/release-acceptance.md`, verify a clean final HEAD, and freeze `6bf694daa7a8a823a849a4e2da9588988b6d2358..<ending-head>` for independent review. Do not record S11 closed until the independent reviewer approves the frozen range.
