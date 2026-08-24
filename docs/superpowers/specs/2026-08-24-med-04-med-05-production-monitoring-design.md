# MED-04/MED-05 Production Monitoring Design

Date: 2026-08-24
Repository: `gradex-ui-antigravity`
Branch: `ui-antigravity-20260817`

## Scope and authority

This design is limited to the founder-authorized paid-beta monitoring tranche:

- MED-04 — worker runtime and transactional email/outbox degradation;
- MED-05 — filesystem capacity pressure.

It does not change application product behavior, retry semantics, provider selection,
backup architecture, timer installation policy, or the immutable Ox Alpha review.

## Selected architecture

The existing `gradex-monitor.service` continues to run `host.sh monitor` every five minutes.
The host wrapper collects two host-owned runtime signals into a mode-0600 temporary report:

1. the exact Docker Compose project/service container for `worker`, verified by Compose labels and
   `State.Status=running`;
2. read-only PostgreSQL metrics from the exact Compose `postgres` container for transactional email
   due work, expired leases, and terminal failures.

`monitor-once.sh` remains the aggregate owner. It evaluates the host report, API probes, the existing
successful-offsite backup marker, and filesystem capacity. The webhook payload remains provider-neutral
and contains only check IDs, environment, timestamp, and correlation ID; journal output carries concise
check-level details without recipients, bodies, credentials, or provider response text.

The worker signal cannot match an unrelated process because it does not search process names: it resolves
the configured Compose project and `worker` service, then verifies the returned container's Compose project
and service labels before inspecting its state. A missing Docker/runtime probe is a failed check.

## Email health policy

The monitor queries `transactional_email_deliveries`, whose existing schema is the source of truth:

- `PERMANENT_FAILED` or `EXHAUSTED` is immediately unhealthy;
- an expired `SENDING` lease is unhealthy;
- a `QUEUED` row is unhealthy only when it is due and its due age exceeds
  `GRADEX_MONITOR_EMAIL_STALE_SECONDS` (default 3600 seconds);
- a future-scheduled retry is healthy.

The one-hour default is based on the existing five-attempt retry schedule of 42m30s, with operational
margin for polling and the five-minute monitor cadence. The query uses the existing partial indexes for
due work and stale leases; a narrow reversible partial index covers terminal states so the planner has an
index-only path as the ledger grows. No payload, recipient, or message body is selected.

## Filesystem health policy

The host state directory (`/var/lib/gradex` by default) and Docker's reported data-root are monitored,
because they back backup staging, PostgreSQL volumes, container writable layers, logs, and local temporary
work. `GRADEX_MONITOR_DISK_PATHS` may explicitly replace the derived list, but empty or invalid paths fail
closed. Paths are deduplicated by filesystem device before evaluation.

Filesystem statistics use `stat -f` with non-superuser-available blocks, not human-readable `df` output.
Defaults are:

- warning at 85% used: journal `WARN`, exit 0;
- critical at 95% used or less than 5 GiB available: journal `FAIL`, exit 1.

All thresholds are validated positive numeric configuration and can be changed through the protected
runtime environment.

## Failure and test behavior

The aggregate runs every safe check and reports all failures. Healthy is exit 0; warning-only is exit 0;
unhealthy is exit 1; invalid monitor configuration is exit 2. A missing/failed worker or database probe
cannot be interpreted as healthy.

The verification harness uses disposable report and filesystem-stat fixtures in a dedicated
`monitor-test` environment. The production Hostinger entrypoint always overwrites the temporary runtime
report and forces its runtime environment, so fixtures cannot be selected by the systemd production path.
No retained worker, database, volume, or host filesystem is stopped, filled, dropped, or removed.

## Acceptance evidence

The implementation will add focused monitoring tests for worker down/recovery, normal retry versus stale
or terminal email delivery, database failure, secret-free output, filesystem warning/critical thresholds,
device deduplication, invalid paths, aggregate multiple failures/recovery, and systemd rendering. Backup
freshness remains the existing successful-offsite-marker check, and `SY-08` remains blocked unless its
separate installed/enabled timer acceptance is independently performed.
