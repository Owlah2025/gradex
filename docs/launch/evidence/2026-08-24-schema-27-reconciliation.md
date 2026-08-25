# Schema 27 Reconciliation

Date: 2026-08-24
Repository: gradex-ui-antigravity
Branch: ui-antigravity-20260817
Authorization: Founder-authorized narrow schema-27 prerequisite remediation.

## 1. Verdict

**PROVEN — schema 27 reconciliation restored runtime compatibility and canonical E2E.**

## 2. Root cause

MED-04/MED-05 legitimately added migration 0027, but the runtime schema authority still ended at
schema 26:

~~~text
SubjectCodeIdentitySchemaVersion = 26
MaxSchemaVersion = SubjectCodeIdentitySchemaVersion
~~~

Fresh migration databases therefore reached version 27 while readiness rejected them as outside the
build range 14..26. The canonical E2E runner failed before tests executed.

## 3. Migration 0027 validation

Migration 0027 is canonical and intentional.

Its up migration creates:

~~~sql
transactional_email_monitor_terminal_idx
ON transactional_email_deliveries (terminal_at, event_id)
WHERE status IN ('PERMANENT_FAILED', 'EXHAUSTED')
~~~

Its down migration drops exactly that index with DROP INDEX IF EXISTS.

The MED-04/MED-05 evidence records that this index makes terminal-email monitoring queries
index-backed. The migration was not removed, rewritten, renumbered, or weakened.

## 4. Stale schema assumptions

Current assumptions reconciled:

- backend/internal/db/schema.go
  - schema 26 remains the Subject Code Identity milestone;
  - MaxSchemaVersion now derives from the new schema-27 milestone.
- backend/internal/db/migrate_integration_test.go
  - current MaxSchemaVersion test now checks the schema-27 milestone;
  - full migration up/down/reapply tests continue to derive expectations from MaxSchemaVersion;
  - a new test verifies 0027’s index and rollback/reapply behavior.
- backend/cmd/migrate/main.go
  - already derives reported supported range from db.MinSchemaVersion and db.MaxSchemaVersion;
  - no code change was needed.
- frontend/e2e/global-setup.ts
  - already waits on /readyz; no schema literal or E2E setup change was needed.
- readiness checks
  - already derive their upper bound from MaxSchemaVersion; no readiness logic change was needed.

Historical documents that recorded schema 26 at their historical time were not modified.

## 5. Changes

Changed:

- backend/internal/db/schema.go
  - added TransactionalEmailMonitorTerminalSchemaVersion = 27;
  - set MaxSchemaVersion to that authority.
- backend/internal/db/migrate_integration_test.go
  - renamed the current-schema authority test;
  - added TestTransactionalEmailMonitorTerminalMigrationIsAdditiveAndReversible.
- docs/launch/evidence/2026-08-24-schema-27-reconciliation.md
  - this record.

No migration file, MED-01 source, MED-04/MED-05 monitoring source, E2E setup, frontend, or
historical evidence was changed.

## 6. Migration tests

Passed:

~~~text
cd backend && go test -tags=integration ./internal/db -run '^Test(TransactionalEmailMonitorTerminalMigrationIsAdditiveAndReversible|MaxSchemaVersionTracksCurrentSchema)$' -count=1
ok  	github.com/Owlah2025/gradex/backend/internal/db 1.182s

cd backend && go test -tags=integration ./internal/db -count=1
ok github.com/Owlah2025/gradex/backend/internal/db 37.168s
~~~

The dedicated test proves:

- fresh migration reaches schema 27;
- 0027 index exists with the terminal monitoring definition;
- repeated up is ErrNoChange and leaves the index intact;
- migration 0027 down returns cleanly to schema 26 and removes the index;
- reapplying 0027 returns cleanly to schema 27 and restores the index.

## 7. MED-04/MED-05 regression

Passed:

~~~text
./deploy/scripts/verify-med-monitoring.sh
med-monitoring: MED-04 worker/email and MED-05 disk monitoring fixtures passed
~~~

The 0027 index remains present and its terminal-state monitoring behavior is covered by the
dedicated migration proof.

## 8. MED-01 focused regression

Passed unchanged:

- normal public preview succeeds;
- access-suspended preview denied;
- restored preview succeeds;
- retired preview denied;
- DELISTED preview denied;
- ARCHIVED preview denied;
- public catalogue suspension/retirement parity;
- protected Student regressions.

Focused media, catalogue, HTTP API, and full relevant integration suites all passed.

## 9. Backend gates

Passed:

~~~text
cd backend && go build ./...
cd backend && go vet ./...
cd backend && go vet -tags=integration ./...
cd backend && go test ./... -count=1
cd backend && go test -tags=integration ./internal/media ./internal/catalogpublic ./internal/httpapi -count=1
~~~

The previously failing media schema assertion now passes.

## 10. Canonical E2E

Authoritative command:

~~~text
cd frontend && npm run test:e2e:canonical
~~~

Result:

~~~text
production   passed 4   failed 0   flaky 0   skipped 0
development  passed 164 failed 0   flaky 0   skipped 0
aggregate    passed 168 failed 0   flaky 0   skipped 0
exit 0
~~~

Both API readiness probes passed at schema 27. Both run-owned workers started and cleanly exited
through teardown. No tests were skipped or weakened.

## 11. MED-01 final status

MED-01 is now:

**PARTIAL — focused remediation proven, canonical blocked**
→
**PROVEN — PUBLIC PREVIEW TAKEDOWN CONSISTENCY RESTORED**

No MED-01 implementation was redone.

## 12. MVP tracker

Old: 45 / 53 = 84.9%
New: 45 / 53 = 84.9%

Schema reconciliation is not a counted MVP row.

## 13. Files changed by this tranche

- backend/internal/db/schema.go
- backend/internal/db/migrate_integration_test.go
- docs/launch/evidence/2026-08-24-schema-27-reconciliation.md

0027 up/down files and immutable historical evidence remain unchanged.

## 14. Evidence references

- 0027 up/down migration:
  backend/internal/db/migrations/0027_transactional_email_monitor_terminal.up.sql
  backend/internal/db/migrations/0027_transactional_email_monitor_terminal.down.sql
- Runtime authority:
  backend/internal/db/schema.go
- Migration proof:
  backend/internal/db/migrate_integration_test.go
- MED-04/MED-05 monitoring evidence:
  docs/launch/evidence/2026-08-24-med-04-med-05-production-monitoring.md
- MED-01 remediation evidence:
  docs/launch/evidence/2026-08-24-med-01-public-preview-takedown.md

## 15. Repository safety

- No git reset, clean, stash, restore, checkout, or broad formatting.
- No retained database or Docker volume was touched.
- Only disposable integration/E2E databases were used.
- No unrelated process was killed.
- No deployment was performed.
- Historical schema-26 evidence and the immutable Ox review were preserved.
- Final git diff --check passed.

## 16. Recommended next step

MED-02 — Playback Authorization Rate-Limit Consistency.

**SCHEMA 27 RECONCILIATION PROVEN — CANONICAL E2E RESTORED**
