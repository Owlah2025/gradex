# S12 Batch B evidence — disposable production-like topology

Date: 2026-08-08

Batch base: `0ff6cb3a466456b35b766a828fac6d6926df4949`

This is a loopback-only disposable deployment proof, not a claim that cloud staging or a public
certificate is live. Backup/restore, application rollback, alert delivery, protected-media E2E, and
the full access-to-learning smoke remain separate S12 batches.

## Clean provisioning

`./deploy/scripts/environment.sh reset` removed only the `gradex-s12` Compose project, its four named
volumes, and generated ignored state. A subsequent `up` created fresh PostgreSQL, media, and Caddy
volumes; generated new uncommitted secrets; initialized a private versioned bucket and non-root
application storage user; ran migrations; and started separate API, worker, frontend, and edge
processes. A second `up` also passed, including idempotent bucket/user setup and migration execution.

The migration job reported:

```text
migrate up: version=15 dirty=false (supported; this build supports 2..15)
```

The running versions resolved by the pinned Compose tags were PostgreSQL 16.14, Redis 7.4.9,
MinIO `RELEASE.2025-09-07T16-13-09Z`, MinIO Client
`RELEASE.2025-08-13T08-35-41Z`, and Caddy 2.10.2.

## Process and network status

- PostgreSQL, Redis, MinIO, API, and frontend were healthy.
- The independent worker process remained running after PostgreSQL, Redis, and bucket preflight.
- The migration and MinIO initialization jobs exited 0.
- PostgreSQL, Redis, MinIO, API, worker, and frontend published no host port.
- Only Caddy published loopback ports `127.0.0.1:18081` and `127.0.0.1:18443`.
- Redis persistence was deliberately disabled because Redis is disposable queue infrastructure;
  authoritative state remained in PostgreSQL.

## TLS and application probes

`./deploy/scripts/environment.sh verify` extracted the disposable Caddy CA from the edge container
and validated the following through the TLS edge without `--insecure`:

```text
https://gradex.localhost:18443/          -> HTTP 200
https://gradex.localhost:18443/healthz   -> {"status":"ok"}
https://gradex.localhost:18443/readyz    -> {"status":"ok","checks":{"postgres":"ok","redis":"ok","schema":"ok"}}
```

An HTTP request to loopback port 18081 returned 308 with
`Location: https://gradex.localhost:18443/`. The presented leaf certificate had the critical SAN
`DNS:gradex.localhost`, was issued by the disposable Caddy local authority, and verified against the
extracted root. A public catalog read through the same HTTPS origin returned HTTP 200 and an empty
page, proving an application-level PostgreSQL read.

## Data-plane proof

`./deploy/scripts/environment.sh data-plane` reported schema row `15 | f`, Redis `PONG`, then used the
non-root application storage credentials to write, stat, read, and delete a 25-byte object. MinIO
returned a version ID, the bucket policy reported `private`, and an unauthenticated read was refused
while the proof object existed. The storage administration service and credentials were never
published through the edge.

## Remaining technical findings

- **High — production dependency security:** the Batch A Next.js/PostCSS/nanoid production advisories
  remain unresolved; the disposable origin stays loopback-only and cloud exposure remains blocked.
- **High — managed Redis compatibility:** the current backend Redis boundary accepts only an address
  and does not yet inject authentication or TLS settings. Batch B proves a private-network disposable
  Redis topology, not compatibility with managed Redis products that require credentials/TLS.
- **Pending by design:** cloud credentials, DNS, a publicly trusted certificate, production provider
  resources, backup/restore, rollback, monitoring/alerts, queue consumption, protected media, and the
  deployed MVP smoke have no evidence in this batch.
