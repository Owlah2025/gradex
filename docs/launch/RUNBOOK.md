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
   provenance.
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
