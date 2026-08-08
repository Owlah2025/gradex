# Course Access Grant — Private Beta Launch Runbook (August 15)

**Feature**: S6 Course Access Grant | **Version**: 1.0.0 | **Target Date**: 2026-08-15

---

## 1. Environment & Operational Configuration

The application requires the following environment variables in production:

| Variable | Recommended Private Beta Setting | Purpose |
|---|---|---|
| `APP_ENV` | `production` | Enables hardened security defaults |
| `DATABASE_URL` | `postgres://gradex:<PASS>@<HOST>:5432/gradex?sslmode=verify-full` | Managed PostgreSQL connection string |
| `REDIS_ADDR` | `<HOST>:6379` | Session and rate-limiting store |
| `PUBLIC_ORIGIN` | `https://gradex.example` | Production origin for CSRF and action secrets |
| `CORS_ALLOWED_ORIGINS` | `https://gradex.example` | CORS policy restriction |
| `PLAYBACK_TOKEN_SECRET` | `<SECURE_RANDOM_SECRET>` | HMAC key for media playback tokens |

---

## 2. Database Migration & Schema Verification

Deploy schema version 15 before starting API or worker instances:

```bash
# 1. Run migrations to schema version 15
go run ./cmd/migrate up

# 2. Verify schema version equals 15
go run ./cmd/migrate version
```

Expected output: `Current schema version: 15`.

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

Outbox delivery workers poll `outbox_events` and deliver acceptance link emails containing the single-use bearer secret.

---

## 6. Backup & Restore Procedures

### Pre-Deployment Backup

```bash
pg_dump -U gradex -h <HOST> -F c -b -v -f gradex_s6_pre_deploy.dump gradex
```

### Emergency Rollback & Recovery Procedure

If a deployment fault occurs:
1. Roll back frontend, API, and worker artifacts to the previous approved application release.
2. Keep the forward-compatible database schema at version 15. After real S6 grants exist, do not run
   migration `0015_course_access_grant.down.sql`: it clears `source_invitation_id` and destroys grant
   provenance.
3. For recovery proof, create a fresh separate database and restore into it. Never use the active
   Gradex database as the routine restore-drill target:

```bash
createdb -U gradex -h <HOST> gradex_restore_<TIMESTAMP>
pg_restore -U gradex -h <HOST> -d gradex_restore_<TIMESTAMP> gradex_s6_pre_deploy.dump
```

Verify schema and identity/access-critical records in the restored target, then start an isolated
Gradex instance whose `DATABASE_URL` points to that target. Database recovery and application rollback
are separate operations.

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
