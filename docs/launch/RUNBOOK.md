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

On the Hostinger production host this order is not performed by hand. It is encoded in
`./deploy/hostinger/host.sh apply-schema-release <manifest> 34 35`, which is the only sanctioned
command for a schema-advancing release. It takes a fresh backup, stops the worker and then the API
and proves both stopped, runs `gradex-migrate up` from the **target** release image, verifies clean
schema `35` (set `GRADEX_SCHEMA_RELEASE_EXPECTED_COLUMNS=media_asset_versions.work_claim_token` to
assert the migrated objects as well), starts the API alone and requires its readiness, then starts
the worker only after re-proving that no other worker is running, and finally the frontend. It opens
an explicit maintenance window and fails closed at every boundary, pointing back at this document.
`apply-release` cannot be used here — it is application-only and never migrates — and `up-core` must
not be used, because it migrates while the old worker is still running. See
`deploy/hostinger/README.md`, "Schema-advancing releases".

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
- The production backend image exposes only `gradex-migrate up`, `version`, and `max-version`. A down
  command is not part of the documented production surface at all.

`deploy/scripts/application-rollback.sh` also fails closed here by design, and correctly so: it reads
`max-version` from the target image and dies with `schema 35 is newer than target release maximum 34`.
Its own usage text states that schema downgrade and database rollback are intentionally unsupported.
D-103 therefore falls **outside** the application-rollback boundary described in `deploy/README.md`;
it needs the supervised procedure below instead.

Do not modify those guards, and do not weaken purchase-rollback safety. What follows is a deliberate,
supervised emergency operation — not a routine migration path.

**Rollback — in this exact order:**

1. **Stop the D-103 application, API, and worker first.** No D-103 worker may remain running at any
   later step, and no D-102 worker may start before step 5.
2. **Take and verify a backup immediately before the destructive step**, using the established
   pre-deployment backup above (`pg_dump --format=custom` plus its `sha256sum`), or
   `./deploy/scripts/database-recovery.sh backup` in the S12 topology. Do not proceed on an unverified
   backup.
3. **Apply the supervised schema rollback in one transaction.** Apply *only*
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

4. **If the transaction fails, it has already rolled back — both the DDL and the bookkeeping.** Do not
   retry blindly and do not continue the deployment. **D-102 stays stopped.** Diagnose, or restore the
   backup from step 2 into a fresh database per the restore procedure above.
5. **Prove both facts before starting any D-102 binary.** Bookkeeping and physical shape must *both*
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
6. Deploy the D-102 backend and frontend artifacts, then start the D-102 API and worker.
7. **Verify after start:** `gradex-migrate version` reports `34`; `/healthz` returns `200 OK`;
   `/readyz` returns `200 OK`; the API and worker are running the exact intended D-102 rollback SHA;
   the frontend rollback artifact is serving if one was part of the release; and no D-103 worker
   remains running anywhere.

**What the rollback removes, and what it does not.** Down-migrating `0035` drops only D-103's recovery
metadata: the lease claim columns, the two attempt counters, the failure category, and their
constraints and partial index. It does not touch media state, provenance, trusted duration, or
rendition data, so existing `READY` media stays deliverable under D-102.

**In-flight media work needs attention after rollback.** Any Asset Version still in `SCANNING` or
`PROCESSING` loses its lease evidence and reverts to pre-D-103 behaviour: it will not self-recover,
and it requires the existing Admin retry operation. Prefer draining or letting in-flight media work
settle before rolling back.

---

Staging and production require authenticated Redis over verified TLS. Certificate verification
cannot be disabled. Supply `REDIS_PASSWORD` through the secret platform; add `REDIS_USERNAME` only
for an ACL account. `REDIS_ADDR` must contain only `host:port`, never a credential-bearing URL.

---

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
