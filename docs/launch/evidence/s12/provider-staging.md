# S12 T047 provider staging evidence

Date: 2026-08-08

Starting revision: `90f8768fd2829457ffb250d1dead9e640c7c812b`

Status: **provider execution pending; T047 remains open**

## Provider-neutral work completed

- Hostinger KVM 2 Compose topology with only edge HTTP/HTTPS ports published.
- Separate frontend, API, worker, migration, proof, PostgreSQL, isolated-restore PostgreSQL, TLS-only
  authenticated Redis, and Caddy processes.
- Cloudflare R2 endpoint/CORS configuration plus an explicit credential-gated private/version-bound
  provider compatibility test.
- Host baseline audit, public DNS/TLS/Cloudflare probe, protected provider smoke runner, bounded
  loopback-only database proof tunnel, backup/isolated restore, alert invocation, and persistent
  application release selection.
- Application rollback remains schema-forward and refuses pre-T046/unlabeled artifacts; migration 0015
  Entitlement provenance is checked before and after release selection.

Provider tooling commit: `6cfa46dd25fde9c81427b40500ef8f6cf662a44c`

Tar-stream build correction: `6d824a22e9c8bf919e820676cdba9014c3456a0d`

## Immutable release evidence

Rollback floor N (`6cfa46dd25fde9c81427b40500ef8f6cf662a44c`):

- backend: `sha256:55bf6a26d1e42aead17d172323bdec55e4ff988869560b9a31e3158cd7d0116d`
- proof: `sha256:8c665bf49f099443d200666bf3de30cda313df62378ee23906e9a2d1998117c9`
- frontend: `sha256:85687ca5bcbe334539eb74f2073f9db74e34a43e86a2f45c4c33b16d14d5e5b2`
- checksum-verified archive: 163,043,992 bytes, mode 0600

Release N+1 (`6d824a22e9c8bf919e820676cdba9014c3456a0d`):

- backend: `sha256:e47b26fc0205ea8b9d6e36173e926d5dd81c6d472a01bc6cc543f90d4faecd3b`
- proof: `sha256:9d1abe8948cedd278a5f1265b7db03af8f401a0b058d1e6002839b098e4a04a9`
- frontend: `sha256:016a6f997b35f59519d39f0a494d8ec1c176f02dfb09026257e5d37bca2d16bd`
- checksum-verified archive: 163,043,000 bytes, mode 0600

Every image reports its corresponding full Git SHA through
`org.opencontainers.image.revision`. Archives, checksums, and release manifests are ignored local
artifacts under `deploy/.state/hostinger/releases/`; they contain images and identifiers, not runtime
secrets.

## Repository validation

- `bash -n deploy/hostinger/*.sh`: pass.
- Hostinger Compose file rendered with the proof and restore profiles and a complete synthetic
  non-secret environment: pass.
- `gofmt -l`, `go build ./...`, `go vet ./...`, `go vet -tags=integration ./...`: pass.
- `go test -race ./...`: pass.
- R2 provider-tagged test compilation: pass. Live execution is pending real R2 credentials.
- `npm ci`: pass.
- `npm audit --omit=dev --audit-level=high`: zero vulnerabilities.
- frontend lint and typecheck: pass.
- frontend tests: 163 passed, zero failed.
- optimized Next.js 15.5.21 production build: pass, including both immutable frontend images.
- `scripts/docs-guard.sh`: pass across 160 Markdown files.
- `scripts/expose-guard.sh`: pass.
- `git diff --check`: pass.
- ShellCheck was unavailable on this workstation; Bash parsing passed.

The full public Playwright/provider runner was not executed against localhost as substitute evidence.

## External execution still required

No provider target or credential was available in the execution environment. The Product Owner must
make the following available through protected local/provider configuration, never Git or chat:

1. Hostinger VPS address plus an SSH-configured non-root operational user whose public key is authorized.
2. The controlled Gradex domain/Cloudflare zone and the chosen staging hostname.
3. A private R2 bucket plus a bucket-scoped object read/write/delete token. Apply the rendered exact-origin
   CORS policy, then run the provider test. R2 must return usable `x-amz-version-id` values and must never
   silently substitute current bytes for a requested historical version; otherwise the frozen S4 media
   provenance contract blocks R2 use.
4. A protected external alert webhook URL/token if real alert delivery is to be closed in this run.

After those inputs exist, follow `deploy/hostinger/README.md` and capture actual host audit, public DNS,
Cloudflare Full-strict HTTPS, health/readiness, schema `15|false`, Redis TLS/auth, worker/R2 processing,
private playback, provider backup/isolated restore, N → N+1 → N rollback, delivered alert, exposure scan,
and public S5/S6 browser journey evidence.

## Unproven acceptance evidence

The following are deliberately **not** claimed: Hostinger deployment, public hostname/DNS/TLS, live R2
compatibility or privacy, provider PostgreSQL/Redis/worker health, provider backup/restore, provider
rollback, external alert delivery, provider scan, protected-media retrieval, or public 30-step browser
smoke. T047 and T048 remain unchecked.
