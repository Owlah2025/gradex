# Monitoring and alert contract

`rules.yml` is the minimum provider-neutral launch contract. The deployment platform must monitor
the public API health/readiness endpoints, the worker's desired process count, backup freshness, and
the structured API/worker error events. `/readyz` covers PostgreSQL, Redis, and schema compatibility;
`/healthz` remains dependency-independent liveness.

`monitor-once.sh` implements the HTTP and backup-freshness checks and sends a fixed, non-sensitive
JSON event to `GRADEX_ALERT_WEBHOOK_URL`. Configure its URLs, environment, CA file, backup completion
marker, and webhook token through runtime secrets/environment. The webhook body contains only the
environment, failed check IDs, observation time, and a correlation ID; it never contains the target
URLs or token. Run it from a scheduler at least every five minutes. Provider-native process and log
rules should implement the remaining entries in `rules.yml`. `monitor.env.example` lists the exact
runtime keys with the webhook credential blank.

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
