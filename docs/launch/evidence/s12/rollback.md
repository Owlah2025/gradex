# S12 Batch G evidence — application rollback

Date: 2026-08-08

Batch base: `735d1b69b02e1b0bb6185f3a577643dd82fd2cab`

## Procedure and safety boundary

`deploy/scripts/application-rollback.sh apply RELEASE_MANIFEST` reads a constrained manifest with a
release ID and backend/frontend image references, verifies the live database is clean schema 15,
then force-recreates only API, worker, and frontend. It does not run the migration service and has no
database rollback path. Explicit `migrate`, `downgrade`, `schema`, and `database` invocations fail.

This separates application recovery from database recovery. Migration 0015 remains forward-applied,
so S6 `source_invitation_id` provenance is not cleared during a release rollback.

## Executed drill

The repeatable harness built release N from Git commit
`15f7ec294d524b866cfee9ce8d46d1844962c2c9`, selected the current release
`735d1b69b02e1b0bb6185f3a577643dd82fd2cab` as N+1, and executed:

```text
N -> N+1 -> N
```

After every transition, the frontend and API passed through the TLS edge, `/healthz` returned 200,
`/readyz` returned 200 with PostgreSQL, Redis, and schema `ok`, and the worker was running. The exact
application artifacts were:

```text
release  backend image                                                       frontend image
N        sha256:bd6c48e77ca0f6c4d68a612a30bc3a67dd10e171199ae093c692b1c134f2dfd8 sha256:5680641b56f12d2375e07a4e3329cecae8008383b7a7c10d974da14fca677a70
N+1      sha256:49c997f6508a48931c6c56360475bcd0a67f8b4e2367a6cf2044b7ab99cee381 sha256:2083544fba1d8c60720b323cbfd6a2eba9eb036c32bad99b6d5827b70dbbf43c
rollback sha256:bd6c48e77ca0f6c4d68a612a30bc3a67dd10e171199ae093c692b1c134f2dfd8 sha256:5680641b56f12d2375e07a4e3329cecae8008383b7a7c10d974da14fca677a70
```

The database assertion was identical before N, after N, after N+1, and after rollback:

```text
schema version | dirty | Entitlements | invitation-sourced Entitlements with provenance
15             | false | 1            | 1
```

The final deployed containers were healthy/running on the exact N image IDs. The proof command and
result were:

```text
./deploy/scripts/verify-application-rollback.sh
s12-rollback-proof: N=15f7ec294d524b866cfee9ce8d46d1844962c2c9 N+1=735d1b69b02e1b0bb6185f3a577643dd82fd2cab N restored; probes passed and schema/provenance state stayed 15|false|1|1
```

This is an actual disposable application rollback, not a runbook-only claim. It does not claim a
provider-specific cloud release rollback; the same immutable release manifest contract must be wired
to the selected staging/production platform.
