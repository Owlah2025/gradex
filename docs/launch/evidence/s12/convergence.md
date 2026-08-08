# S12 convergence evidence

Date: 2026-08-08

Frozen-base candidate: `dde093bc9f8e75b89cc96667c73a30fea5f8baee`

Batch H base: `3ef16b62e7cde11a66ee270342eaf90d1838abbf`

## Application validation

Backend validation passed:

```text
test -z "$(gofmt -l .)"
go build ./...
go vet ./...
go vet -tags=integration ./...
go test -race ./...
go test -tags=integration -run 'TestMigrateUpDownUp|TestDirtySchema|TestUnsupportedSchema' ./internal/db/
go test -tags=integration ./internal/db
go test -tags=integration ./internal/identity ./internal/outbox ./internal/httpapi ./internal/catalogpublic ./internal/ratelimit ./internal/learning ./internal/access ./internal/entitlement
```

The first parallel integration execution produced one transient failure in
`TestReportRefusalsAreOneByteIdenticalAnswer/tampered_ciphertext`. The failing subtest passed alone,
the complete `internal/httpapi` package passed uncached in 170.451 seconds, and the complete
CI-equivalent package command then passed again with `internal/httpapi` uncached in 178.815 seconds.
No implementation was changed to hide or weaken the assertion.

Frontend validation passed:

```text
npm ci
npm run lint
npm run typecheck
npm test             # 163 passed, 0 failed
npm run build         # optimized Next.js production build passed
```

`npm audit --omit=dev` reports three High production dependency findings: direct `next`, direct
`postcss`, and transitive `nanoid`. The current production configuration has no production rewrite,
Server Action, remote image optimizer, custom server, or WebSocket upgrade surface, but the installed
Next.js 14 line remains affected by advisories for other App Router/request-processing paths. This is
a release finding, not a failed reproducible build; it remains open for upgrade or independently
reviewed mitigation before production release.

## Production artifacts and deployed checks

`./deploy/scripts/environment.sh build` rebuilt all production images successfully from the Batch H
tree. The resulting local image IDs were:

```text
gradex-backend:s12-local       sha256:478116e822a462aab6abaee5e976bb984600c646425e443f24163a65b90ab0b9
gradex-backend-proof:s12-local sha256:e1b2df14354b9317c65de20afa4805f378fafc3cf4e13cb433bb582cc22a0344
gradex-frontend:s12-local      sha256:70859559c59fb717e9acc96449bdb58c8506772b8667101c47bd8ea14028def5
```

The following checks passed against the production-like stack:

```text
./deploy/scripts/environment.sh verify
./deploy/scripts/environment.sh data-plane
./deploy/scripts/verify-edge-security.sh
```

They proved the frontend and `/healthz` plus `/readyz` through the TLS edge, clean schema version 15,
PostgreSQL and Redis operations, application-credential private-object write/read/delete, private
bucket policy, HTTPS redirect/certificate behavior, Secure/HttpOnly/SameSite=Strict cookies, trusted
origin and CSRF enforcement, hostile CORS denial, forwarded request IDs, fake-auth refusal, and
runtime/static secret scans.

Earlier committed batch evidence remains the authority for the real isolated restore, Redis/outbox
worker recovery, private HLS processing/playback, delivered disposable alert, and N to N+1 to N
application-only rollback. Batch H additionally passed the existing S5/S6 journey against
`https://gradex.localhost:18443`; see `staging-smoke.md`.

## Repository and evidence guards

The final tree was checked with:

```text
bash -n deploy/scripts/*.sh
./scripts/docs-guard.sh
./scripts/expose-guard.sh
git diff --check
.specify/scripts/bash/check-prerequisites.sh --json --require-tasks --include-tasks
```

The SpecKit prerequisite check resolved feature directory
`specs/008-staging-production-infrastructure` and found the research, data-model, contracts,
quickstart, and tasks artifacts. Convergence retained 44 completed tasks and added explicit remaining
tasks for production dependency remediation, authenticated/TLS Redis, provider staging execution,
and independent review. Independent review is intentionally not self-approved.

## Evidence boundary

The verified staging origin is loopback production-like infrastructure, not a public cloud URL.
Public DNS, a publicly trusted certificate, provider-managed PostgreSQL/Redis/object storage,
provider bucket CORS, delivery to the production alert destination, and an external-network rerun
remain pending credentials/provider access. Those external boundaries do not invalidate the local
deployment, restore, rollback, queue, media, TLS, alert-capability, or MVP-journey proofs.
