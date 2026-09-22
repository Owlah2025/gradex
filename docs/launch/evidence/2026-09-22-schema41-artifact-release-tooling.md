# Phase 3C-A — artifact-backed release tooling verification

Date: 2026-09-22. Baseline: `a2339c25908ba3138fb3cb08f2b1fe7f1f838c79`.
The initial working tree was clean. The supplied production baseline remains
`78ee4227e470014b768da2654c2395f52822385b`, schema 40 clean; no live production inspection,
deployment or migration was performed in this task.

## Implementation and dependency audit

`release.sh` retains the existing per-SHA release directory and image/manifest conventions.
It now exports backend/proof/frontend build contexts and deploy tooling with `git archive` from
the same clean builder HEAD. `record` and `export` also require that exact clean HEAD.
The image checksum uses a portable basename, matching the new bundle checksum.

`host.sh` sources `backup-restic.sh` and `release-artifact.sh`, reads the CORS template and Redis
certificate extension, invokes `monitor-once.sh`, and renders the adjacent Compose file.
Compose's only relative bind is `Caddyfile`; TLS binds are absolute protected-state paths and
volumes/networks derive from the explicitly supplied project. These dependencies, the rollback
guard, migration 0041 UP/DOWN files and generated metadata/hash inventory form the bundle.
External restic/password/certificate dependencies remain configured in protected state.

The trusted importer is streamed from the frozen builder over SSH stdin. The five incoming files
are copied to staging, both checksums are verified before extraction/load, the manifest binds the
bundle digest before its helper is sourced, and metadata/image IDs/labels are verified before
publishing a read-only release directory. Re-import refuses to overwrite existing tooling.

Schema-41 wrappers require their own root to match the protected runtime-selected release directory,
reverify archive/inventory/content and manifest/image bindings, require max schema 41, inspect the
binary's no-argument usage, and compare image migration hashes. They require no production Git.
Forward identity checks precede prepare/CORS writes. API/worker absence and the local producer
inventory precede migration; forward startup verifies exactly the selected worker afterwards.

## Focused verification

Commands:

```bash
python3 deploy/scripts/verify-deploy-bundle.py
bash deploy/scripts/verify-schema-41-rollback.sh
```

Both passed. The new guard creates a disposable clean Git builder, uses the real release exporter
and importer, and executes the actual bundled `host.sh` CLI under `bwrap` at the production paths.
It supplies a failing Git probe, real filesystem/checksum/archive operations and mocked Docker
service boundaries. The extracted tooling has no `.git` or Git history. The real Docker Compose
renderer separately verifies identical named volumes and project identity at the stable and bundle
paths, plus the bundle-local Caddy bind. No application container is deployed by this guard.

Coverage includes:

- bundle export, exact builder SHA metadata, valid checksum, executable/read-only extraction;
- dirty builder refusal for build/record/export and immutable re-import refusal;
- corrupt archive, manifest SHA/digest mismatch, missing/mismatched/stale metadata, content drift;
- backend/frontend/proof revision mismatch, image ID mismatch, runtime SHA/image mismatch;
- stale rollback usage, wrong schema ceiling, mismatched migration hashes and the historical
  `a272011` selection;
- successful no-Git forward and rollback invocations, with mixed artifacts refused before Compose;
- absent-only API/worker gates, preserved production project/state/runtime/database checks;
- same-production-DB worker refusal, distinct Founder Beta/LG019 database acceptance, missing or
  ambiguous database target refusal, query-override refusal, enumeration/inspection failure;
- production worker topology and full container-ID comparison after startup.

## Requested verification sequence

The integration packages ran serially, with each process completed before starting the next.

| Command | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go test ./... -count=1` | PASS |
| `go test -race ./internal/media -count=1` | PASS |
| `go test -race ./... -count=1` | PASS |
| `go test -tags=integration ./internal/db -count=1` | PASS |
| `go test -tags=integration ./cmd/migrate -count=1` | PASS |
| `go test -tags=integration ./internal/media -count=1` | Initial failure; full rerun PASS |
| `go test -tags=integration ./internal/catalog -count=1` | PASS |
| `go vet ./...` | PASS |
| `go vet -tags=integration ./...` | PASS |
| `scripts/docs-guard.sh` | PASS |
| `scripts/expose-guard.sh` | PASS |
| `deploy/scripts/verify-compose-render.sh` | PASS |
| `deploy/scripts/verify-hostinger-production-render.sh` | PASS |
| `deploy/scripts/verify-hostinger-systemd.sh` | PASS |
| `deploy/scripts/verify-hostinger-first-cutover.sh` | PASS |
| `deploy/scripts/verify-schema-41-rollback.sh` | PASS |
| `python3 deploy/scripts/verify-deploy-bundle.py` | PASS |
| `git diff --check` | PASS |
| `gofmt -l backend` | Empty output |
| `bash -n` on each changed/new shell script | PASS |

### Failure/retry record

The first full media integration run reported:

```text
--- FAIL: TestPlaybackManifestRejectsInvalidSessionsAndUnsafeReferences
    delivery_integration_test.go:417: tampered session error=<nil>, want protected media is unavailable
```

Ten targeted repetitions passed:

```bash
go test -tags=integration ./internal/media -run '^TestPlaybackManifestRejectsInvalidSessionsAndUnsafeReferences$' -count=10
```

The test changes only the final Base64 character (A becomes B when necessary), while the decoder
uses non-strict `base64.RawURLEncoding`. Different final characters can represent the same bytes.
This is a plausible explanation for the intermittent assertion, not an independently established
classification. Neither that test nor media implementation was changed by this release-tooling task.

After the original serialized sequence and the targeted repetitions completed, the full media
integration command was run again without overlapping another integration package. It passed:

```text
ok  github.com/Owlah2025/gradex/backend/internal/media 262.763s
```

## Scope, limitations and final G0 handoff

No backend/Go code, 0041 SQL, media behavior, data model, Compose definition or systemd template
changed. Generic production down remains prohibited; dedicated 41→40 acknowledgement, clean 41,
all-FULL/zero-claim preflight and post-step clean 40 remain unchanged. No 3C-B/3C-C producer or
scheduler work was introduced. No live application rollback is claimed.

The producer inventory covers local Docker containers, not remote machines or native processes.
It conservatively refuses same-name databases on different servers and URI options other than
`sslmode`. Operators must preserve the maintenance boundary against new starts. Bundle checksums
rely on trusted build/SSH provenance; process immutability does not resist a privileged administrator.

Clean-code-guard reviewed the tooling changes; fixes included binding the manifest before sourcing
bundle code, comparing full container IDs, and refusing database-query overrides. Test-guard reviewed
the real archive/filesystem and mocked Docker boundary tests. Docs-guard checked the transfer commands,
paths, metadata fields and operational-root distinction against the implementation.

Independent G0 must review the frozen exact range from the baseline above to the final submitted
HEAD, including this evidence and the initial test failure. This builder's verification is not
independent approval. Only after final G0 approval may that HEAD become `RELEASE_SHA`; any later
commit requires a new review/freeze. Preserve the exact deployed 3C-A images, manifest, bundle and
checksums as the 3C-B schema-41 application rollback floor.
