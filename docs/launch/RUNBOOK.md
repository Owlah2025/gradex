# Gradex Private Beta Launch Runbook (August 15)

**Scope**: launch runtime, transactional email, and Course access | **Target Date**: 2026-08-15

---

## 1. Environment & Operational Configuration

The application requires the following environment variables in production:

| Variable | Recommended Private Beta Setting | Purpose |
|---|---|---|
| `APP_ENV` | `production` | Enables hardened security defaults |
| `DATABASE_URL` | `postgres://gradex:<PASS>@<HOST>:5432/gradex?sslmode=verify-full` | Managed PostgreSQL connection string |
| `REDIS_ADDR` | `<HOST>:6379` | Credential-free Redis host and port |
| `REDIS_PASSWORD` | `<MANAGED_SECRET>` | Required Redis authentication secret |
| `REDIS_USERNAME` | blank or `<MANAGED_SECRET>` | Optional Redis ACL username |
| `REDIS_TLS_ENABLED` | `true` | Required in staging and production |
| `REDIS_TLS_SERVER_NAME` | `<CERTIFICATE_HOSTNAME>` | Optional certificate name override |
| `REDIS_TLS_CA_CERT_FILE` | blank or mounted PEM path | Optional private CA; blank uses system roots |
| `PUBLIC_ORIGIN` | `https://gradex.example` | Production origin for CSRF and action secrets |
| `EMAIL_ENABLED` | `true` | Required for the production worker; production refuses disabled delivery |
| `EMAIL_PROVIDER` | `resend` | Approved production transactional provider; `fake` is development/test only |
| `EMAIL_API_KEY` | secret | Resend API key; inject through the production secret facility |
| `EMAIL_FROM_ADDRESS` | verified sender address | Bare address on the verified Resend sender domain |
| `EMAIL_FROM_NAME` | `Gradex` | Safe display name shown to recipients |
| `EMAIL_REPLY_TO` | optional bare address | Optional operational reply address |
| `EMAIL_PROVIDER_TIMEOUT` | `10s` | Per-request bound; accepted range is 1–30 seconds |
| `CORS_ALLOWED_ORIGINS` | `https://gradex.example` | CORS policy restriction |
| `PLAYBACK_TOKEN_SECRET` | `<SECURE_RANDOM_SECRET>` | HMAC key for media playback tokens |

---

## 2. Database Migration & Schema Verification

Deploy schema version 16 before starting API or worker instances:

```bash
# 1. Run migrations to schema version 16
go run ./cmd/migrate up

# 2. Verify schema version equals 16
go run ./cmd/migrate version
```

Expected output: `Current schema version: 16`.

---

## 3. Administrator Bootstrap

Initialize the primary launch administrator account if not present:

```bash
go run ./cmd/bootstrap-admin --email admin@example.com --name "Launch Administrator"
```

The bootstrap process creates an active `ADMIN` account and emits a password change credential.

---

## 4. Course Setup & Access Expiry Configuration

1. Ensure the target Course is created and in lifecycle `DRAFT`, `PUBLISHED`, or `EMERGENCY_SUSPENDED`.
2. Configure the default access expiry date for the cohort:

```bash
curl -X PUT "https://gradex.example/api/v1/admin/courses/{COURSE_ID}/default-access-expiry" \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: {ADMIN_CSRF_TOKEN}" \
  -d '{"date":"2026-12-31","reason":"August 15 Private Beta Cohort"}'
```

---

## 5. Outbox & Invitation Intent Pipeline

Invitations insert outbox intent records into `outbox_events` and `outbox_protected_payloads` within the atomic creation transaction.

To inspect queued invitation outbox events:

```sql
SELECT id, event_type, aggregate_id, available_at
  FROM outbox_events
 WHERE event_type = 'access.invitation_issued'
 ORDER BY available_at DESC;
```

The worker discovers supported intents into `transactional_email_deliveries`, decrypts protected
payloads only in memory, renders the fixed locale template, and calls the configured delivery
adapter. PostgreSQL remains authoritative if Redis or Resend is unavailable.

### Diagnose a missing transactional email

Start with the Account, Invitation, Entitlement, or other safe aggregate identifier from the domain
record. Never paste a bearer token, action URL, email body, API key, or recipient address into a query,
ticket, log search, or evidence file.

```sql
SELECT e.id AS message_id,
       e.event_type,
       d.template_contract,
       d.locale,
       d.provider,
       d.status,
       d.attempt_count,
       d.next_attempt_at,
       d.last_failure_class,
       d.last_provider_code,
       d.accepted_at,
       d.terminal_at
  FROM outbox_events e
  LEFT JOIN transactional_email_deliveries d ON d.event_id = e.id
 WHERE e.aggregate_id = '<SAFE_AGGREGATE_UUID>'::uuid
 ORDER BY e.occurred_at DESC;
```

For one message, inspect the bounded attempt history:

```sql
SELECT attempt_number, outcome, failure_class, provider_code,
       started_at, finished_at, retry_at
  FROM transactional_email_attempts
 WHERE event_id = '<SAFE_MESSAGE_UUID>'::uuid
 ORDER BY attempt_number;
```

- No delivery row means the worker has not yet discovered the intent, the event is not a supported
  transactional contract, or its safe locale/template metadata is malformed.
- `QUEUED` means the first attempt or a bounded retry is pending. Compare `next_attempt_at` with the
  worker clock and check worker health.
- `SENDING` with an expired lease is recovered by another poll. Five expired attempts become
  `EXHAUSTED`.
- `PERMANENT_FAILED` is not retried. Correct recipient or configuration state through its owning
  domain workflow and issue a new intent; do not mutate the immutable outbox row.
- `ACCEPTED` means Resend accepted the request and returned an ID. It does not prove inbox placement.
- `EXHAUSTED` needs operator review of the safe failure class/provider code and provider health.

Retry timing is 30 seconds, 2 minutes, 10 minutes, then 30 minutes through the five-attempt limit.
Provider `Retry-After` can lengthen, but never shorten, that schedule. The stable provider
idempotency key is derived from the immutable message UUID and is never logged.

---

## 6. Backup & Restore Procedures

### Pre-Deployment Backup

```bash
pg_dump --format=custom --no-owner --no-acl -U gradex -h <HOST> -f gradex_pre_deploy.dump gradex
sha256sum gradex_pre_deploy.dump > gradex_pre_deploy.dump.sha256
```

The proven disposable S12 drill creates known identity/access records, writes a checksum-protected
backup into ignored local state, restores it into a new PostgreSQL container/database, and starts an
isolated API against the restored target:

```bash
./deploy/scripts/database-recovery.sh seed
./deploy/scripts/database-recovery.sh backup
./deploy/scripts/database-recovery.sh restore
./deploy/scripts/verify-restored-database.sh
```

### Emergency Rollback & Recovery Procedure

If a deployment fault occurs:
1. Roll back frontend, API, and worker artifacts to the previous approved application release.
2. Keep the forward-compatible database schema at version 16. After real S6 grants exist, do not run
   migration `0015_course_access_grant.down.sql`: it clears `source_invitation_id` and destroys grant
   provenance. This forward-compatible assumption does **not** hold for the D-103 media schema
   `0035_media_work_leases`; use the ordered procedure below instead.
3. For recovery proof, create a fresh separate database and restore into it. Never use the active
   Gradex database as the routine restore-drill target:

```bash
createdb -U gradex -h <HOST> gradex_restore_<TIMESTAMP>
sha256sum --check gradex_pre_deploy.dump.sha256
pg_restore --exit-on-error --single-transaction --no-owner --no-acl \
  -U gradex -h <HOST> -d gradex_restore_<TIMESTAMP> gradex_pre_deploy.dump
```

Verify schema and identity/access-critical records in the restored target, then start an isolated
Gradex instance whose `DATABASE_URL` points to that target. Database recovery and application rollback
are separate operations. Do not add `--clean` or point the restore command at the active source
database merely to demonstrate recovery.

### D-103 media work leases (schema 0035) — ordered deployment and rollback

D-102 binaries support schema `0034` only and refuse to start against `0035`; D-103 binaries require
`0035` and refuse to start against `0034`. Both directions fail closed at the readiness schema check,
so **application-only rollback while leaving schema 0035 in place is unsupported** — there is no
binary set that can serve it. Rollback therefore requires rolling the schema back **first**.

**Forward deployment — in this exact order:**

1. Stop or replace the old D-102 application containers.
2. Apply the migration as a controlled one-off release job: `gradex-migrate up` (schema `0034` -> `0035`).
3. Start the D-103 API.
4. Start the D-103 worker.
5. Start or deploy the D-103 frontend.

**The D-102 worker and the D-103 worker must never run concurrently.** No old-worker/new-worker
overlap is permitted at any point in either direction. The two disagree about who owns in-flight
`SCANNING`/`PROCESSING` work: a D-102 worker does not observe leases or claim tokens, so running one
alongside D-103 reintroduces exactly the duplicate-finalization and stale-completion faults the
lease model exists to prevent.

#### Why the normal migration command cannot perform this rollback

`gradex-migrate down` is **not** a valid production rollback path, and no flag should be added to make
it one. Three separate guards stand in the way, all of them deliberate:

- `cmd/migrate` refuses outright: `down migrations are not permitted when APP_ENV=production`.
- Even outside production, the down path runs `CheckManualPurchaseRollbackSafety` for any rollback
  from schema `21` or above. It raises if a single `purchase_requests` row or `PURCHASE_REQUEST`
  entitlement exists, so on a live database the `35 -> 34` step is refused before any DDL runs.
- The production backend image's documented surface is `gradex-migrate up`, `version`, and
  `max-version`. A generic down command is not part of it. (Schema 41 later added exactly one
  supervised production-capable downgrade, `rollback-schema-41`, which reverts only migration 0041
  and accepts no step count or target version — see D-107. It does not exist at schema 0035 and
  changes nothing about this D-103 procedure.)

For schema 41, follow the [artifact release plan](RELEASE_PLAN_2026-09-21_SCHEMA_41_FOUNDATION.md).
Forward cutover and supervised rollback execute `host.sh` from the same verified per-release tooling
bundle under `/home/deploy/gradex-production/releases/$RELEASE_SHA/tooling/`. No per-release production
Git checkout is required. Builder-side Git freezes the reviewed identity; the host checks bundle
checksums/content and manifest/runtime/image bindings. Keep the existing pinned backup/monitor
operational root and systemd units unchanged. Retain the complete deployed 3C-A artifact set as the
schema-41 application rollback floor before 3C-B; the first non-FULL attempt closes schema-40 rollback.

`deploy/scripts/application-rollback.sh` also fails closed here by design, and correctly so: it reads
`max-version` from the target image and dies with `schema 35 is newer than target release maximum 34`.
Its own usage text states that schema downgrade and database rollback are intentionally unsupported.
D-103 therefore falls **outside** the application-rollback boundary described in `deploy/README.md`;
it needs the supervised procedure below instead.

Do not modify those guards, and do not weaken purchase-rollback safety. What follows is a deliberate,
supervised emergency operation — not a routine migration path.

**Rollback — in this exact order:**

1. **Stop the D-103 application, API, and worker first.** No D-103 worker may remain running at any
   later step, and no D-102 worker may start before step 9.
2. **Take and verify a backup immediately before the destructive step**, using the established
   pre-deployment backup above (`pg_dump --format=custom` plus its `sha256sum`), or
   `./deploy/scripts/database-recovery.sh backup` in the S12 topology. Do not proceed on an unverified
   backup.
3. **Detect in-flight media.** Nothing is settled or dropped until you know what is in flight.

   ```bash
   psql --username gradex --host <HOST> --dbname gradex --no-psqlrc \
     --command "SELECT state, count(*) FROM media_asset_versions
                 WHERE state IN ('SCANNING', 'PROCESSING') GROUP BY state;" \
     --command "SELECT count(*) AS trusted_in_flight FROM media_asset_versions
                 WHERE state IN ('SCANNING', 'PROCESSING')
                   AND successful_validation_attempt_id IS NOT NULL;"
   ```

   A zero first result means steps 4 and 5 have nothing to do; run them anyway, since they are
   no-ops on an empty set and skipping them is how the next rollback forgets them. A non-zero
   `trusted_in_flight` is the limit described after this procedure: those Asset Versions will not be
   retryable in a scanner-mode deployment once they are settled.

4. **Settle in-flight media and clear its lease fields, in one supervised transaction.** Every row
   still `SCANNING` or `PROCESSING` moves to the terminal state its own path would have reached, and
   its lease columns are cleared in the same statement.

   ```bash
   psql --username gradex --host <HOST> --dbname gradex \
     --set ON_ERROR_STOP=1 --single-transaction \
     --command "UPDATE media_asset_versions
                   SET state = CASE state
                           WHEN 'SCANNING' THEN 'SCAN_ERROR'
                           WHEN 'PROCESSING' THEN 'PROCESS_FAILED'
                       END::media_asset_version_state,
                       processing_stage = NULL,
                       processing_progress_percent = NULL,
                       processing_updated_at = NULL,
                       processing_attempt_token = NULL,
                       work_claim_token = NULL,
                       work_claimed_at = NULL,
                       work_lease_expires_at = NULL,
                       last_failure_category = 'WORKER_INTERRUPTED'
                 WHERE state IN ('SCANNING', 'PROCESSING');"
   ```

   Three details are load-bearing, and all three are why this step precedes the down migration
   rather than following it:

   - **It must run while `0035` is still applied.** After the down migration those lease columns do
     not exist, so this statement could not be written at all.
   - **It must clear the lease columns in the same statement.** Leaving them populated on a row that
     is no longer `SCANNING` or `PROCESSING` violates
     `media_asset_versions_work_claim_coherent`, which is still in force at this point.
   - **It takes only edges the state machine already permits** — `SCANNING -> SCAN_ERROR` and
     `PROCESSING -> PROCESS_FAILED`. This is not a bypass of the transition trigger, and Admin
     Retry's own state validation is unchanged.

5. **Verify no incompatible rows remain.** This is a gate, not a formality: if it does not come back
   clean, **do not run step 6.**

   ```bash
   psql --username gradex --host <HOST> --dbname gradex --no-psqlrc --tuples-only --no-align \
     --command "SELECT count(*) FROM media_asset_versions WHERE state IN ('SCANNING', 'PROCESSING');" \
     --command "SELECT count(*) FROM media_asset_versions
                 WHERE work_claim_token IS NOT NULL
                    OR work_claimed_at IS NOT NULL
                    OR work_lease_expires_at IS NOT NULL;"
   ```

   Required: `0` and `0`. A non-zero first count means work was claimed after step 4 — the D-103
   worker is still running, and step 1 was not actually completed.

6. **Apply the supervised schema rollback in one transaction.** Only now. Apply *only*
   `0035_media_work_leases.down.sql` — never a generic sequence of older down migrations. The file
   contains no transaction wrapper of its own, and PostgreSQL DDL is transactional, so the schema
   change and the bookkeeping correction commit or abort together:

   ```bash
   psql --username gradex --host <HOST> --dbname gradex \
     --set ON_ERROR_STOP=1 --single-transaction \
     --file backend/internal/db/migrations/0035_media_work_leases.down.sql \
     --command 'UPDATE schema_migrations SET version = 34, dirty = false;' \
     --command 'SELECT version, dirty FROM schema_migrations;'
   ```

   Applying the SQL alone is **not** sufficient. `schema_migrations` still reads `35`, and readiness
   reads that marker, so D-102 would keep refusing to serve. The table holds exactly one row
   (`version bigint`, `dirty boolean`), so the bookkeeping correction is an `UPDATE`, not an insert.

7. **If the transaction fails, it has already rolled back — both the DDL and the bookkeeping.** Do not
   retry blindly and do not continue the deployment. **D-102 stays stopped.** Diagnose, or restore the
   backup from step 2 into a fresh database per the restore procedure above. The settle from step 4
   is already committed and is safe to leave in place: `SCAN_ERROR` and `PROCESS_FAILED` are ordinary
   states under both `0034` and `0035`.
8. **Prove both facts before starting any D-102 binary.** Bookkeeping and physical shape must *both*
   be at `0034`:

   ```bash
   psql --username gradex --host <HOST> --dbname gradex --no-psqlrc --tuples-only --no-align \
     --command 'SELECT version, dirty FROM schema_migrations;' \
     --command "SELECT count(*) FROM information_schema.columns
                WHERE table_name = 'media_asset_versions'
                  AND column_name IN ('work_claim_token', 'work_claimed_at',
                    'work_lease_expires_at', 'scan_attempt_count',
                    'processing_attempt_count', 'last_failure_category');"
   ```

   Required: `34 | f` and a column count of `0`. If either cannot be proven, **D-102 remains stopped.**
9. Deploy the D-102 backend and frontend artifacts, then start the D-102 API and worker. Each Asset
   Version settled in step 4 is now Retry-eligible; an Admin retries them from the Admin surface.
10. **Verify after start:** `gradex-migrate version` reports `34`; `/healthz` returns `200 OK`;
   `/readyz` returns `200 OK`; the API and worker are running the exact intended D-102 rollback SHA;
   the frontend rollback artifact is serving if one was part of the release; and no D-103 worker
   remains running anywhere.

**What the rollback removes, and what it does not.** Down-migrating `0035` drops only D-103's recovery
metadata: the lease claim columns, the two attempt counters, the failure category, and their
constraints and partial index. It does not touch media state, provenance, trusted duration, or
rendition data, so existing `READY` media stays deliverable under D-102.

**In-flight media work needs attention after rollback, and Admin Retry alone will not do it.**

An earlier version of this section said that in-flight work "requires the existing Admin retry
operation". That was wrong, and acting on it would have stranded assets:

- `Service.Retry` accepts only `SCAN_FAILED`, `SCAN_ERROR`, and `PROCESS_FAILED`. An Asset Version
  still in `SCANNING` or `PROCESSING` is in none of them and is refused with a state conflict.
- Rolling back `0035` also removes the lease columns, so the D-103 recovery pass that would
  otherwise reclaim those rows cannot run either. Under D-102 binaries **nothing** reclaims them.

The supported procedure is **steps 3, 4 and 5 of the numbered rollback above**: detect what is in
flight, settle it into the terminal state its own path would have reached, clear its lease fields,
and verify nothing incompatible remains — all of it *before* the `0035` down migration runs. The
statement and the three reasons its position matters are given there and are not repeated here, so
there is only one executable copy of it.

After the rollback completes, an Admin retries each settled Asset Version. Retry resets the scan and
processing attempt counters to zero, so a recovered asset starts from a clean budget — a fresh cycle,
with the earlier cycle's attempts preserved as audit history that does not consume the new budget.

**One limit to plan around.** An Asset Version holding D-088 trusted-validation provenance is
retryable only in a deployment where the trusted path still applies. In a scanner-mode deployment the
retry is refused with a state conflict, and the asset is left exactly as the settle found it. Count
those rows before rolling back:

```sql
SELECT count(*) FROM media_asset_versions
 WHERE state IN ('SCANNING', 'PROCESSING') AND successful_validation_attempt_id IS NOT NULL;
```

Prefer draining or letting in-flight media work settle before rolling back; the procedure above is
the fallback for work that is still in flight when the rollback has to proceed.

This procedure is executed by
`backend/internal/media/rollback_recovery_integration_test.go`, which runs the settle statement
verbatim, proves Retry refuses in-flight and healthy states without it, proves a settled asset
retries and reaches `READY` again, and pins the trusted-provenance limit.

---

Staging and production require authenticated Redis over verified TLS. Certificate verification
cannot be disabled. Supply `REDIS_PASSWORD` through the secret platform; add `REDIS_USERNAME` only
for an ACL account. `REDIS_ADDR` must contain only `host:port`, never a credential-bearing URL.

---

## Phase 3C-B — manual enhancement recovery

Phase 3C-B is manual-only and **requires schema 42**; its worker refuses to start on schema 41.
Production must retain the complete deployed 3C-A artifact floor
`98e88fcc1105e8c638bb638d3f1c46630bcc51b2` before this phase is released, and both the cutover and the
supervised downgrade refuse if any part of that artifact set is missing or fails its checksums. An
authenticated Admin may request enhancement recovery for a current, unclaimed PLAYABLE video Asset
Version through `POST /api/v1/media/assets/:id/retry-enhancements`. The request records audit evidence
and a durable outbox intent but does not claim the asset, probe it, change its state, or predict its
missing rungs.

The worker claims at execution, revalidates the exact object version and checksum, re-probes the
source, derives the frozen ladder, and compares it with canonical `video_renditions`. It encodes only
missing rungs and preserves every already committed row. A failed enhancement remains PLAYABLE with
its committed rows intact. A complete canonical ladder uses a zero-output FINALIZATION attempt and
atomically proves READY. Canonical rows may span multiple operation prefixes; protected delivery
continues to render its dynamic master from their persisted keys.

The first ENHANCEMENT or FINALIZATION attempt, failed or successful, closes schema-40 rollback. After
that evidence exists, normal application rollback targets the retained 3C-A artifact on schema 41;
the supervised 41→40 command must refuse. No automatic retry scheduler, periodic scan, backoff, or
3C-C behavior is permitted.

### Cutover and supervised 42→41 rollback

Forward and back both execute `host.sh` from the verified schema-42 tooling bundle. Full procedure,
gate list and failure matrix:
[schema-42 release plan](RELEASE_PLAN_2026-09-22_SCHEMA_42_ENHANCEMENT_RECOVERY.md).

- Forward: `host.sh up-core-schema-42-enhancement-recovery`. It verifies release identity and the
  schema-42 capability, verifies the 3C-A artifact floor, requires the api and worker containers
  absent, proves zero active media claims and a clean schema 41, migrates, verifies a clean schema 42,
  and only then starts the api, worker and frontend. **Do not start the 3C-B API before schema 42 is
  verified clean** — request-time semantics are harmless on 41, but an enhancement intent written
  before the schema-42 worker exists has nothing able to execute it.
- Back: `host.sh rollback-schema-42-enhancement-recovery`. It forwards exactly
  `gradex-migrate rollback-schema-42 -confirm-production=schema-42-to-41` — no target version, no step
  count, no 42→40 — and ends at a clean 41. The 3C-B candidate image stays selected throughout,
  because the 3C-A image contains neither migration 0042 nor the command that reverts it. Switch the
  runtime selection to `98e88fcc1105e8c638bb638d3f1c46630bcc51b2` only after the marker reads a clean
  41. Generic production `down` stays prohibited and no flag is added to change that.

**The enhancement drain is a blocking prerequisite, not a warning.** Before the schema moves, prove
zero incompatible work in every durable location: undispatched `media.enhancement_requested` outbox
events, and `media:enhancement` asynq tasks in the **pending**, **active**, **scheduled**, **retry**,
**archived** (dead-letter) and **aggregating** states. `host.sh enhancement-drain` reads all of them
read-only and reports the counts. Completed asynq tasks and historical terminal
ENHANCEMENT/FINALIZATION attempts do not block — they need no schema-42 producer and stay
representable on schema 41. A dispatched intent is not finished work: its outbox row carries a
receipt while its queue task still waits, so both the database and the queue must be proven.

**Why one surviving outbox event is a hard gate.** A surviving undispatched
`media.enhancement_requested` event is not merely rejected or skipped by the deployed 3C-A
application. Its media dispatcher returns `unsupported media outbox event`, the batch aborts, the
event remains undispatched, and because the feeding query is ordered by `occurred_at` and selects only
rows without a dispatch receipt, that same event heads every later batch. One such row therefore
**blocks all later media outbox dispatch**, scans and transcodes included. `outbox_events` is
append-only, so the row cannot be deleted or edited: clearing it afterwards requires an explicit
operator decision to record a dispatch receipt in `media_outbox_dispatches`, on a live production
database, during an outage of all media processing. Zero such rows is required before the downgrade,
every time.

If incompatible work exists, the rollback refuses and reports counts and identifiers. It deletes
nothing: discarding requested work is an operator decision with its own evidence, never a side effect
of a downgrade. Never mutate processing evidence to make a gate pass, and never force or
automatically repair a schema marker after a failed step.

## Phase 3C-C — automatic enhancement recovery (implemented, DISABLED)

3C-C schedules the 3C-B action above automatically. It ships **disabled in every
environment**, and the operational rule is an activation gate, not a configuration
preference.

**It may not be enabled in production until one legitimate real 3C-B manual
`ENHANCEMENT` or `FINALIZATION` operation has been observed end to end in
production.** That observation must be a real operational event. Do **not**
manufacture it: no synthetic broken video, no deliberately failed FFmpeg run, and
no production `RetryEnhancements` invoked in order to produce evidence. As of this
entry 3C-B is deployed, safe, and **not yet production-observed**, so the gate is
closed.

### Enablement preconditions

| | |
|---|---|
| Flag | `MEDIA_AUTO_ENHANCEMENT_RECOVERY_ENABLED` — default `false`, every environment |
| Schema | 45 or later. The worker **fails closed** if the flag is set against an earlier schema |
| Worker media floor | unchanged at 42. Automatic recovery is a separate capability gate |
| Prerequisite | one observed real 3C-B manual recovery in production |

When the flag is false the reconciler is not started: no candidate query, no
scheduler row, no outbox intent.

**Turning it off also stops work that is already queued.** This is worth stating
precisely, because an earlier revision of this runbook claimed it without the
runtime doing it. Stopping the scheduler loop does not reach a task that was
committed while the feature was enabled and is still sitting in Redis; that task
used to arrive at a worker and execute normally — taking the media claim, writing
a processing attempt and encoding — which is exactly what an operator switching
the flag off is trying to prevent.

A delivered automatic task is now refused before any operation identity is minted
and long before a claim is possible. It takes no `work_claim_token`, writes no
processing attempt, sets no `active_processing_attempt_kind`, encodes nothing,
persists no rendition, and charges no automatic failure. Its intent is explicitly
closed and the scheduler row becomes due immediately, so nothing is stranded and
the acknowledged task can never become active later. Re-enabling the flag
produces exactly one fresh intent on the next tick.

The switch is targeted at AUTOMATIC recovery. **Admin `RetryEnhancements` is
unaffected in both directions** — it remains available while the flag is false,
which is what makes it the operator escape hatch.

Turning the flag off deletes no state.

### What it does when enabled

It selects settled, claim-free `PLAYABLE` videos with valid source identity and
provenance, and commits — in one transaction — a scheduler row and the same
`media.enhancement_requested` outbox event the Admin action writes. It **never
takes the media work claim**; the execution-time worker remains the only claimant,
running the same path, with no second FFmpeg invocation and no second `READY`
proof.

Three consecutive automatic failures, backing off 15 minutes, 1 hour, then 4
hours, then `NEEDS_OPERATOR`. A newly committed canonical rendition is progress and
resets the budget even if the operation later failed. Permanent failures — invalid
media, checksum mismatch, missing immutable source, contradictory canonical
evidence — reach `NEEDS_OPERATOR` immediately and never loop.

### Operator actions

Read the state for one asset:

```sql
SELECT state, consecutive_failures, attempt_number, next_attempt_at,
       last_failure_category, last_outcome_at
FROM media_auto_enhancement_recovery
WHERE asset_version_id = '<asset-version-id>';
```

`NEEDS_OPERATOR` means automatic recovery has given up and a person must look. The
manual Admin retry remains available from **every** state, including
`NEEDS_OPERATOR` and after the budget is exhausted — it is the override. Taking it
resets the automatic budget, supersedes any queued automatic task, and blocks an
immediate duplicate. It deletes no historical evidence.

Do not edit `media_auto_enhancement_recovery` by hand to make a gate pass. The row
is reconciled from the actual linked execution outcome; rewriting it detaches the
scheduler's belief from what really happened.

Audit evidence is written with a NULL actor and the `SYSTEM` actor role:
`MEDIA_AUTO_ENHANCEMENT_SCHEDULED` and `MEDIA_AUTO_ENHANCEMENT_EXHAUSTED`.

## Public preview — the Lesson model and the legacy path

Public preview is a permission on a Lesson (`course_lessons.allow_public_preview`,
schema 46). An anonymous visitor watches the **same** video asset, transcode,
canonical renditions and storage objects a paying Student watches. Marking a
Lesson previewable creates no media asset, version, processing attempt, rendition,
or transcode intent.

**The legacy course-level preview is retained and still serving.** During the
transition:

- Do **not** clear `course_revisions.preview_asset_version_id`. It stays populated
  as rollback safety.
- Do **not** delete any `PREVIEW` asset, version or rendition.
- Do **not** map an existing course preview onto a Lesson.
- The legacy endpoints stay mounted.

A Course whose live revision marks no Lesson previewable keeps exactly the
course-level preview experience it has today. A Course with free Lessons shows them
in its outline instead. Retiring the legacy path is a **later, separate tranche**
after production observation, not an operator action.

Anonymous preview requires `READY`, never `PLAYABLE`, and fails closed with the
ordinary inventory-safe unavailable response. Preview tokens live in their own
signature domain, carry no Student, device or lease, and expire at the measured
video duration plus the configured grace, capped at two hours. A preview link is a
shared bearer capability until it expires and cannot be revoked mid-stream; there
is no DRM.

## 7. Health Checks & Verification Sequence

- **Readiness Check**: `GET /readyz` -> returns `200 OK`
- **Liveness Check**: `GET /healthz` -> returns `200 OK`
- **Go/No-Go Verification**:
  1. Admin signs in -> 200 OK
  2. Admin configures course default expiry -> 200 OK
  3. Admin issues invitation -> 201 Created
  4. Student accepts invitation -> 200 OK (`PENDING_ADMIN_APPROVAL`)
  5. Admin approves -> 200 OK (Entitlement & Enrollment created)
  6. Student streams lesson -> 200 OK
