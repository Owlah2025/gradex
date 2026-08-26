# Monitoring and alert contract

`rules.yml` is the minimum provider-neutral launch contract. The deployment platform monitors the public
origin root, `/healthz`, and `/readyz`, the worker process, PostgreSQL schema state, transactional email
outbox, backup freshness, disk capacity, and structured API/worker error events. `/readyz` covers
PostgreSQL, Redis, and schema compatibility; `/healthz` remains dependency-independent liveness.

`host.sh monitor` collects the exact Compose `worker` and `postgres` service containers into a mode-0600
temporary runtime report. Canonical Hostinger deployments resolve those services through Compose. A
non-canonical deployment can set `GRADEX_MONITOR_COMPOSE_PROJECT` and either or both of
`GRADEX_MONITOR_WORKER_CONTAINER` and `GRADEX_MONITOR_POSTGRES_CONTAINER`. When a tooling-only runtime
does not provide `GRADEX_BACKEND_IMAGE`, it may also set `GRADEX_MONITOR_API_CONTAINER` so the monitor can
derive the selected image from a validated API container. Founder Beta sets all four.
Every selected container name is syntax-checked, must exist and be running, and must carry the configured Compose project
and expected service label. It never searches by process name.

The PostgreSQL schema check is read-only: it compares `schema_migrations` with the selected backend image's
`gradex-migrate max-version` output and requires a clean state. When no image is configured, it reads the
image only from the validated API container described above. The outbox check is also read-only and
selects only terminal-failure presence, oldest due age, and oldest expired lease age. A failed Docker,
schema, or PostgreSQL probe is unhealthy.

`monitor-once.sh` aggregates the runtime report, HTTPS HTTP checks, backup freshness, and filesystem capacity.
When a webhook URL is configured, it sends a fixed, non-sensitive JSON event when any check is unhealthy;
without one it records the failed checks locally and exits non-zero. Configure URLs, thresholds, paths, CA
file, backup completion marker, and webhook token through the
protected runtime environment. The webhook body contains only the environment, failed check IDs,
observation time, and a correlation ID; it never contains target URLs, recipients, message bodies,
database values, or credentials. Journal output identifies each failed check and its safe diagnostic.
Run it from the existing five-minute systemd timer.

Transactional email policy is fail-closed and based on the existing ledger: `PERMANENT_FAILED` and
`EXHAUSTED` are immediately unhealthy; expired `SENDING` leases are unhealthy; a `QUEUED` row is unhealthy
only when it is due and older than `GRADEX_MONITOR_EMAIL_STALE_SECONDS` (default one hour). A retry whose
`next_attempt_at` is still in the future is not actionable and does not alert. The one-hour default is the
existing 42m30s retry budget plus operational margin.

Disk monitoring uses `stat -f` non-superuser-available blocks, never human-readable `df`. By default the
Hostinger wrapper monitors `/var/lib/gradex` and Docker's reported data-root; `GRADEX_MONITOR_DISK_PATHS`
can explicitly replace those paths. Filesystems are deduplicated by device. Warning is ≥85% used and
exits 0 with a journal `WARN`; critical is ≥95% used or less than 5 GiB available and exits 1.

`monitor.env.example` lists the exact runtime keys with the webhook credential blank. The successful
backup marker remains the only freshness input and is updated only after the encrypted remote snapshot
is visible, the repository check passes, and temporary plaintext staging is removed; a local-only dump
never refreshes freshness. The marker must be readable, non-empty, a valid Unix timestamp, no more than
`GRADEX_BACKUP_MAX_FUTURE_SECONDS` ahead of the local clock (300 seconds by default), and no older than
`GRADEX_BACKUP_MAX_AGE_SECONDS`.

`GRADEX_ALERT_WEBHOOK_URL` selects the external destination. `GRADEX_ALERT_WEBHOOK_TOKEN` is an
optional bearer credential for destinations that require one; the URL may itself be a protected
capability. The monitor supplies both to `curl` through mode-0600 temporary files that are removed on
exit, rather than exposing either value in process arguments. `GRADEX_MONITOR_CA_FILE` optionally
selects a CA bundle for both probes and webhook TLS.

Public probes accept only HTTPS and use certificate verification. Outside `monitor-test`, webhook URLs must
also be HTTPS; `monitor-test` alone permits an HTTP loopback sink for disposable alert proof.

Test alert handling without changing staging by running `./deploy/scripts/verify-hostinger-systemd.sh`.
Its disposable curl boundary proves success, delivery failure, non-2xx handling, insecure-webhook rejection,
and credential isolation; it never calls a configured destination.

The Hostinger hourly backup profile uses a two-hour freshness threshold (`7200` seconds). That window
allows one delayed or missed hourly run and then reports stale state before a third hourly opportunity.
It replaces the earlier 26-hour default, which was too slow to detect failure of an hourly schedule.
The timer cadence, freshness threshold, and recoverable RPO are separate: only successful backup and
isolated restore evidence can establish the backup-based recovery envelope.

`disposable-alert-sink.py` exists only for the local proof harness. It is not a production service.
An externally delivered staging/production alert remains separate evidence and requires the final
alert destination credentials.
