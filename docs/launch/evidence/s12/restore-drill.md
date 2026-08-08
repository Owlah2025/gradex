# S12 Batch C evidence — isolated database restore drill

Date: 2026-08-08

Batch base: `add61eaab1ae037a24a8cf0dfaadb80d67bf9cf2`

## Source and known records

The source was the running production-like `gradex` database in container
`052a13a372d99de6e427f83869aff62170cf53ec9e915e489ed320c017b044d3`. The drill inserted fixed,
idempotent records for an Admin, Instructor, Student, Course, approved Course Access Invitation,
ACTIVE Entitlement, and Enrollment. The Entitlement retained
`source_invitation_id=00000000-0000-4000-8000-00000000d001`.

## Backup

`./deploy/scripts/database-recovery.sh backup` ran `pg_dump` in custom format with owner/ACL metadata
excluded. The resulting ignored file was 155,954 bytes, mode 0600. Its SHA-256 was
`749b5224611c1b08ecd898477a7ef40a49bdc9d880c0ac37b7a821761488c8d9`, and
`sha256sum --check` returned `OK` before restore.

## Fresh target and restore

`./deploy/scripts/database-recovery.sh restore` removed only any previous `gradex-s12_restore-data`
target, created a fresh named volume and separate `restore-postgres` server/database, and used
`pg_restore --exit-on-error --single-transaction --no-owner --no-acl`. It did not use `--clean` and
did not alter the source database. The restore target container was
`f981f076ae30dfcab4add918dafc1a38a9c2e74c9b088f556eef9abb78bb8511`, mechanically distinct from
the source container.

`./deploy/scripts/verify-restored-database.sh` asserted this exact result:

```text
schema=15:false
known Accounts=3
known Courses=1
approved Invitations=1
ACTIVE Entitlements with expected source_invitation_id=1
known Enrollments=1
```

The source and target independently returned `gradex|1` and `gradex_restore|1` for the known
Entitlement after restore.

## Application proof against restored data

The verifier started an isolated `api-restore` process whose `DATABASE_URL` named only
`restore-postgres/gradex_restore`. It returned:

```json
{"status":"ok"}
{"status":"ok","checks":{"postgres":"ok","redis":"ok","schema":"ok"}}
{"items":null,"page":1,"page_size":20,"total":0}
```

The last response is a representative application read from the restored database. The restored API
had no public/edge port.

## Migration and provenance safeguards

- `go test -tags=integration ./internal/db`: pass in 16.207s, including zero/up/down/up, dirty/unsupported
  schema, migration 0015 provenance, and rollback/re-upgrade cases on a dedicated disposable test DB.
- `gradex-migrate down 1` under the running production configuration exited 1 with
  `down migrations are not permitted when APP_ENV=production`.
- A post-refusal source query returned
  `15:false|00000000-0000-4000-8000-00000000d001`, proving schema 15 and the S6 provenance link
  remained intact.

This proves the repository-controlled database backup and restore path. It does not claim that a
managed provider's scheduled backup or console/API restore has been configured or exercised.
