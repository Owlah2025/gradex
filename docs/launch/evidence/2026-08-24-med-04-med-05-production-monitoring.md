# MED-04/MED-05 — Production Monitoring Failure Detection

Date: 2026-08-24
Repository: `gradex-ui-antigravity`
Branch: `ui-antigravity-20260817`
Authorization: Founder-authorized narrow remediation tranche for MED-04 and MED-05.

## 1. Overall verdict

**PROVEN — MED-04/MED-05 production monitoring failure detection is ready.**

The existing Gradex monitor now evaluates worker ownership/runtime, transactional email/outbox health,
and filesystem capacity while preserving API, readiness, backup freshness, webhook, and systemd
contracts. The proof is deterministic and disposable; no retained worker, database, volume, or host
filesystem was stopped, filled, dropped, or removed.

This closes the detection software gap. It does not install or enable retained-host timers, prove delivery
to a real external alert destination, or close INF-01's real offsite-provider proof.

## 2. Previous monitoring gap

Ox Alpha recorded MED-04/MED-05 as open because `rules.yml` declared worker/email/disk concerns but the
runtime monitor evaluated only API health/readiness, backup freshness, and webhook delivery. The runtime
did not materially detect an owned worker becoming unavailable, a stuck/terminal transactional email
delivery, or unsafe filesystem capacity.

The immutable review at
`docs/reviews/2026-08-24-ox-alpha-full-repository-review.md` was not modified.

## 3. Final monitor architecture

The existing `gradex-monitor.timer` remains the scheduler and the existing `host.sh monitor` remains the
entrypoint. The flow is:

```text
gradex-monitor.timer (every five minutes, Persistent=true)
  -> host.sh monitor
     -> exact Compose worker probe
     -> exact Compose PostgreSQL read-only outbox query
     -> mode-0600 temporary runtime report
     -> monitor-once.sh
        -> API health/readiness
        -> worker/email runtime report
        -> successful-offsite backup marker
        -> stat -f filesystem checks, deduplicated by device
        -> journal details + existing safe webhook payload
```

The aggregate evaluates all safe checks. A check failure is recorded and evaluation continues, so one
crash cannot produce a misleading healthy result.

## 4. Worker health signal

The signal is the exact Compose project/service identity selected by the existing Hostinger Compose
wrapper:

1. resolve `worker` through the configured project name and Compose file;
2. reject a missing/non-container identifier;
3. inspect `com.docker.compose.project` and `com.docker.compose.service` labels;
4. require `<configured-project>|worker` and `State.Status=running`.

There is no `pgrep`, process-name match, broad kill, or unrelated Go-process match. A different Gradex
stack, historical container, or unrelated worker label is unhealthy rather than a false pass. Docker
runtime failure is also unhealthy.

## 5. Worker failure proof

`./deploy/scripts/verify-med-monitoring.sh` uses a disposable fake Compose control plane and invokes the
real `host.sh monitor` path:

- matching project/service labels + `running` → exit 0;
- matching labels + `exited` → exit 1 with `FAIL worker: owned Compose worker state=exited`;
- unrelated project label → exit 1 with `FAIL worker: owned Compose labels do not match the configured project`;
- restored matching labels + `running` → exit 0.

No retained worker process was stopped.

## 6. Email/outbox health policy

The monitor queries only `transactional_email_deliveries`, the existing transactional-email ledger owned
by the worker. It selects no recipient, payload, token, subject, body, provider response, or credential.
The query returns only:

- whether a `PERMANENT_FAILED` or `EXHAUSTED` row exists;
- age of the oldest due `QUEUED` row, based on `next_attempt_at`;
- age of the oldest expired `SENDING` lease.

Policy:

- `PERMANENT_FAILED` or `EXHAUSTED` → unhealthy immediately;
- expired `SENDING` lease → unhealthy;
- due `QUEUED` work older than `GRADEX_MONITOR_EMAIL_STALE_SECONDS` → unhealthy;
- a retry whose `next_attempt_at` is still in the future → healthy;
- PostgreSQL/Docker/query failure → unhealthy.

`GRADEX_MONITOR_EMAIL_STALE_SECONDS=3600` is the default. Existing email retry delays total 42m30s over
the five-attempt budget, so one hour leaves operational margin for polling and the five-minute monitor
cadence without flagging normal retries.

The due and stale-lease queries use the existing partial indexes
`transactional_email_dispatch_idx` and `transactional_email_stale_lease_idx`. Migration 0027 adds the
narrow partial `transactional_email_monitor_terminal_idx` for terminal states. Each selection is bounded
with `ORDER BY ... LIMIT 1`; terminal detection uses `EXISTS ... LIMIT 1`, not a historical `COUNT(*)`.
On a disposable PostgreSQL fixture, `EXPLAIN` selected index-only scans for due/stale work; the empty
terminal table reasonably chose a sequential scan by cost, while `enable_seqscan=off` confirmed the new
partial index supplies an index-only path.

## 7. Email failure proof

The disposable harness proves:

- fresh/future retry → exit 0;
- due item age `3601s` over the `3600s` threshold → exit 1;
- terminal count `1` → exit 1;
- expired lease age `4s` → included in the unhealthy aggregate;
- PostgreSQL query failure → exit 1;
- output contains neither `recipient@example.test` nor `PRIVATE_CREDENTIAL_CANARY`.

The existing email unit suite also remains green, preserving the provider-neutral retry and terminal
state transitions. No Resend credential or real provider availability is required.

## 8. Disk filesystems and thresholds

The Hostinger wrapper monitors:

- `/var/lib/gradex` (or the configured `GRADEX_HOST_STATE_DIR`) for backup staging, markers, TLS state,
  and local Gradex state;
- Docker's runtime-reported `DockerRootDir` for PostgreSQL named volumes, container writable layers,
  logs, and worker/media temporary space.

If `GRADEX_MONITOR_DISK_PATHS` is explicitly set, those absolute paths replace the derived list and are
validated. If Docker's data-root cannot be resolved, the root-selection check fails while the host-state
filesystem is still evaluated. Multiple paths are deduplicated by `stat` device ID, so one filesystem
cannot produce duplicate warnings/failures.

The monitor uses `stat -f` values for blocks available to the non-superuser, total blocks, and block size;
it does not parse human-readable `df -h` output.

Defaults:

- warning: `GRADEX_MONITOR_DISK_WARN_PERCENT=85` percent used; journal `WARN`, exit 0;
- critical: `GRADEX_MONITOR_DISK_CRITICAL_PERCENT=95` percent used, or
  `GRADEX_MONITOR_DISK_MIN_FREE_BYTES=5368709120` (5 GiB) available; journal `FAIL`, exit 1.

All values are configurable and validated at monitor startup; warning must be below critical and both
percentages must be ≤100.

## 9. Disk failure proof

The disposable harness proves:

- healthy capacity → exit 0;
- 85% used warning → `WARN disk`, exit 0;
- 95% used critical → exit 1;
- minimum-free-bytes critical with percentage below the critical percentage → exit 1;
- two paths on one device plus one path on a second device → exactly two filesystem evaluations;
- an unreadable/invalid path fixture → exit 1, never silently skipped.

No large files were created and no host disk was filled.

## 10. Aggregate semantics and output

Healthy is exit 0. Warning-only is exit 0. Unhealthy checks return exit 1. Invalid monitor configuration
or malformed runtime reports returns exit 2. Failure lines are concise and check-specific, for example:

```text
FAIL worker: owned Compose worker state=exited
FAIL email_outbox: oldest_due_age=3601s>3600s
FAIL disk: path=/var/lib/gradex device=... free=... used=95.0%
```

The webhook remains the existing fixed JSON contract containing only environment, failed check IDs,
observation time, and correlation ID. Secrets and transactional message data are not logged or sent.

## 11. Backup monitoring preservation

The existing `GRADEX_BACKUP_COMPLETED_AT_FILE` check remains in `monitor-once.sh`. Hostinger points it at
`/var/lib/gradex/backups/latest.completed-at`. INF-01's current semantics are preserved: this marker is
advanced only after encrypted offsite snapshot visibility, repository checks, and successful staging
cleanup. A local-only dump or failed remote backup cannot make backup freshness healthy.

INF-01 remains implemented with real offsite proof pending; this tranche did not change restic, MinIO,
R2, restore, or marker semantics.

## 12. systemd and timer boundary

`gradex-monitor.service` remains:

- `Type=oneshot`;
- supplied non-root `User`/`Group`;
- `UMask=0077`, `NoNewPrivileges=true`, `PrivateTmp=true`, `ProtectSystem=full`;
- existing `host.sh monitor` entrypoint;
- journal output.

`gradex-monitor.timer` remains `OnCalendar=*:0/5` with `AccuracySec=1s` and `Persistent=true`. The
installer still renders, verifies, installs, and daemon-reloads units without enabling or starting either
timer. Therefore `SY-08` is deliberately not promoted by this tranche.

## 13. Tests and verification

Observed focused results:

```text
./deploy/scripts/verify-med-monitoring.sh
med-monitoring: MED-04 worker/email and MED-05 disk monitoring fixtures passed

./deploy/scripts/verify-hostinger-systemd.sh
hostinger-systemd: rendering, cadence, entrypoints, persistence, secret isolation, freshness, and unit syntax passed

cd backend && go test ./internal/email
ok github.com/Owlah2025/gradex/backend/internal/email (cached)

cd backend && go test ./internal/db
? github.com/Owlah2025/gradex/backend/internal/db [no test files]
```

Disposable PostgreSQL fixture: migration `0027_transactional_email_monitor_terminal.up.sql` applied with
`CREATE INDEX`; the query-plan check then confirmed the due/stale index-only paths and the terminal
partial-index path described in §6.

The systemd verification harness renders the units and runs `systemd-analyze verify`; the installed/enabled
timer acceptance was not performed. All modified shell files pass `bash -n`, and `git diff --check` is
clean for the tranche changes.

## 14. Files changed by this tranche

- `deploy/hostinger/host.sh`
- `deploy/hostinger/runtime.env.example`
- `deploy/hostinger/systemd/gradex-monitor.service.in`
- `deploy/monitoring/monitor-once.sh`
- `deploy/monitoring/monitor.env.example`
- `deploy/monitoring/rules.yml`
- `deploy/monitoring/README.md`
- `deploy/scripts/verify-med-monitoring.sh`
- `deploy/scripts/verify-hostinger-systemd.sh`
- `deploy/scripts/verify-observability.sh`
- `deploy/hostinger/README.md`
- `deploy/README.md`
- `backend/internal/db/migrations/0027_transactional_email_monitor_terminal.up.sql`
- `backend/internal/db/migrations/0027_transactional_email_monitor_terminal.down.sql`
- this evidence file

The design spec was separately recorded in
`docs/superpowers/specs/2026-08-24-med-04-med-05-production-monitoring-design.md`.
Pre-existing dirty application, migration, E2E, backup, and documentation changes were preserved.

## 15. Tracker reconciliation

The canonical MVP tracker remains unchanged at **45 / 53 = 84.9%**. MED-04 and MED-05 are Ox Alpha
operational findings, not counted one-for-one as 53-row MVP features. `SY-08` remains **BLOCKED** because
its acceptance covers installed/enabled and executed backup and monitor timers; this tranche only proved
the monitor logic and unit rendering and intentionally did not enable retained-host services.

The canonical application E2E baseline remains **168 passed / 0 failed / 0 skipped**; no application source
or shared runtime composition changed in this tranche.

## 16. Ox Alpha and paid-beta P0 status

The immutable Ox Alpha review remains untouched.

| Finding | Status |
|---|---|
| MED-04 — Worker / Email Monitoring | **OPEN → CLOSED** |
| MED-05 — Disk Monitoring | **OPEN → CLOSED** |
| INF-01 — Encrypted Offsite Backups | **IMPLEMENTED, REAL OFFSITE PROOF PENDING** |

Paid-beta P0 status: payment confirmation NULL-expiry remediation remains closed; INF-01 remains partial
pending real offsite proof; MED-04 and MED-05 are proven closed for failure detection.

## 17. Residual blockers and next step

Remaining separate prerequisites are:

- real INF-01 provider/offsite smoke backup, remote restore, and key-custody evidence;
- accepted production-like installation/enabled execution proof for `SY-08`;
- founder-selected external alert destination credentials if external delivery is required.

No application deployment, Cloudflare R2 work, GAP-06 work, P1 security work, Resend redesign, or retained
service mutation was performed. The next scoped production-readiness action is the real INF-01 offsite
proof when the approved provider and credentials are available, or another independently authorized item
that does not depend on it.

## 18. Repository safety

No `git reset`, `git clean`, `git stash`, `git restore`, broad checkout, repo-wide formatting, retained
database drop, retained volume removal, `docker compose down -v`, volume prune, worker kill, disk-fill
operation, application deployment, or Ox review modification was performed.

Final status: **MED-04 / MED-05 PROVEN — PRODUCTION MONITORING FAILURE DETECTION READY**.
