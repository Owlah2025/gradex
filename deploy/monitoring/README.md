# Monitoring and alert contract

`rules.yml` is the minimum provider-neutral launch contract. The deployment platform must monitor
the public API health/readiness endpoints, the worker's desired process count, backup freshness, and
the structured API/worker error events. `/readyz` covers PostgreSQL, Redis, and schema compatibility;
`/healthz` remains dependency-independent liveness.

`host.sh monitor` collects the exact Compose `worker` and `postgres` service containers into a mode-0600
temporary runtime report. The worker result requires matching Compose project/service labels and
`State.Status=running`; it never searches by process name. The PostgreSQL result is a read-only query of
`transactional_email_deliveries` and selects only terminal-failure presence, oldest due age, and oldest
expired lease age. A failed Docker or PostgreSQL probe is unhealthy.

`monitor-once.sh` aggregates the runtime report, HTTP checks, backup freshness, and filesystem capacity.
It sends a fixed, non-sensitive JSON event to `GRADEX_ALERT_WEBHOOK_URL` when any check is unhealthy.
Configure URLs, thresholds, paths, CA file, backup completion marker, and webhook token through the
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
never refreshes freshness.

`GRADEX_ALERT_WEBHOOK_URL` selects the external destination. `GRADEX_ALERT_WEBHOOK_TOKEN` is an
optional bearer credential for destinations that require one; the URL may itself be a protected
capability. The monitor supplies both to `curl` through mode-0600 temporary files that are removed on
exit, rather than exposing either value in process arguments. `GRADEX_MONITOR_CA_FILE` optionally
selects a CA bundle for both probes and webhook TLS.

The Hostinger hourly backup profile uses a two-hour freshness threshold (`7200` seconds). That window
allows one delayed or missed hourly run and then reports stale state before a third hourly opportunity.
It replaces the earlier 26-hour default, which was too slow to detect failure of an hourly schedule.
The timer cadence, freshness threshold, and recoverable RPO are separate: only successful backup and
isolated restore evidence can establish the backup-based recovery envelope.

`disposable-alert-sink.py` exists only for the local proof harness. It is not a production service.
An externally delivered staging/production alert remains separate evidence and requires the final
alert destination credentials.
