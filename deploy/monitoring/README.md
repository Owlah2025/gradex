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

`disposable-alert-sink.py` exists only for the local proof harness. It is not a production service.
An externally delivered staging/production alert remains separate evidence and requires the final
alert destination credentials.
