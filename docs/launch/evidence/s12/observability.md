# S12 Batch F evidence — observability and alerts

Date: 2026-08-08

Batch base: `15f7ec294d524b866cfee9ce8d46d1844962c2c9`

## Implemented signal contract

The API continues to emit JSON request, dependency, authorization, and unexpected-server-failure
events with request IDs. The worker now uses the same safe structured logger for `STARTING`, `READY`,
`DRAINING`, and `STOPPED` lifecycle events and for these closed failure operations:

- `job_process`, correlated by the stable Asynq task ID and bounded retry counters;
- `media_outbox_dispatch`;
- `redis_health`;
- `queue_runtime`, with the dependency library's free-form arguments excluded.

Worker failure records include only service, environment, operation, error class, job type, task ID,
and retry counts as applicable. They omit raw errors, payloads, credentials, media identifiers,
cookies, and tokens. The provider-neutral rules in `deploy/monitoring/rules.yml` cover API liveness,
API readiness (PostgreSQL, Redis, and schema), desired worker process count, backup freshness,
unexpected HTTP 5xx events, and worker/media failures.

The one-shot monitor accepts a trusted CA and runtime URLs, checks `/healthz`, `/readyz`, and the
successful-backup timestamp, then sends one fixed JSON alert containing only failed check IDs,
environment, observation time, and a correlation ID. The webhook URL and optional bearer token are
runtime-only values.

## Disposable delivery proof

After building and recreating the production-like services, this command passed:

```text
./deploy/scripts/verify-observability.sh
s12-observability: structured correlation, redaction, readiness/backup monitoring, and disposable alert delivery passed
```

The harness performed a fresh database backup and confirmed its completion marker, started an
authenticated loopback alert sink, and stopped Redis. It then proved:

- `/healthz` remained the dependency-independent liveness check;
- `/readyz` failed and selected the `api_readiness` alert condition;
- exactly one `gradex_monitor_failure` webhook event reached the sink with a monitor correlation ID;
- a newly emitted production worker `worker_failure` event identified `redis_health`, exposed only
  `*net.OpError` as its error class, and carried no raw error or payload field;
- the injected alert bearer did not occur in the alert body, monitor output, or worker output;
- after Redis restart, the frontend and API probes passed and the same monitor returned healthy
  without delivering another alert;
- an API 404 response request ID matched the structured API `http_request` record.

The build used these application image identities:

```text
gradex-backend:s12-local       sha256:49c997f6508a48931c6c56360475bcd0a67f8b4e2367a6cf2044b7ab99cee381
gradex-backend-proof:s12-local sha256:91eb7a382fc90766adeb1820e583fe5d3f09869e9eae0c80d670fc0f0d990103
gradex-frontend:s12-local      sha256:2083544fba1d8c60720b323cbfd6a2eba9eb036c32bad99b6d5827b70dbbf43c
```

## Evidence boundary

Alert capability and actual delivery to the disposable sink are proven. Delivery to a real external
staging/production alert destination is not claimed; it requires the Product Owner's selected webhook
destination and credential. The deployment platform must also implement its provider-native worker
replica and structured-log rules from the committed contract.
