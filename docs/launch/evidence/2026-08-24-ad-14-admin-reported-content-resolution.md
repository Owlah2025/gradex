# AD-14 — Minimal Admin Reported-Content Resolution

Date: 2026-08-24

## 1. Authority

- Founder authorization: AD-14 — Minimal Admin Reported-Content Resolution.
- D-094 requires a minimal Admin Reported-Content Resolution surface before limited paid beta.
- BR-145/BR-146 govern the existing Student report target/reason contract, no automatic takedown, and audited Admin resolution.
- D-065/D-066 remain unchanged: reports bind to the exact rendered instance and duplicate-open protection remains version-independent.
- Existing Admin Course lifecycle authority remains canonical; this tranche adds no alternate lifecycle semantics.

## 2. Report model

The existing content_reports model remains the Student submission source of truth:

- target kinds: COURSE, LESSON, VIDEO, RESOURCE, LAB_MATERIAL;
- target ID and exact rendered revision/asset reference;
- fixed reason set and Student explanation;
- reporter Account relation and created timestamp;
- existing open-report uniqueness constraint.

Migration 0028 adds only terminal resolution metadata:

- resolved_by_account_id;
- resolution_action: DISMISSED or DELISTED;
- bounded resolution_reason;
- consistency check for the terminal tuple;
- partial index for the oldest-open queue.

The Student creation route and acknowledgement shape are unchanged.

## 3. Queue semantics

- GET /api/v1/admin/reports.
- Default page size: 20.
- Maximum page size: 100; invalid/out-of-range values normalize to the default.
- Open reports only.
- Deterministic order: created_at ASC, id ASC (oldest first, stable UUID tiebreaker).
- One bounded SQL query includes safe target context; no per-row target lookup.

## 4. Detail and privacy

- GET /api/v1/admin/reports/{id} returns explicit DTO fields only.
- Detail includes target type/label, minimal Student-chosen display name, reason, explanation, submission time, current Course lifecycle/access state, and terminal resolution metadata.
- The response omits reporter IDs/contact data, Instructor private data, raw Course/revision/asset IDs, sessions, credentials, tokens, payment data, and audit internals.
- A target whose historical context is unavailable is represented as target.available = false; the report remains resolvable.

## 5. Authorization

All three Admin report routes use the existing authenticated principal resolver and ADMIN_OPERATIONS capability:

| Principal | Queue | Detail | Resolve |
|---|---:|---:|---:|
| Active Admin | allowed | allowed | allowed |
| Instructor | denied | denied | denied |
| Student | denied | denied | denied |
| Anonymous | denied | denied | denied |
| Suspended Admin | denied | denied | denied |

Denials are server-side and use the existing uniform problem responses.

## 6. Resolution model

- POST /api/v1/admin/reports/{id}/resolve.
- Required body: action and bounded reason.
- DISMISS records DISMISSED and leaves the target unchanged.
- DELIST delegates to catalog.Repository.TransitionCourseLifecycleTx with LifecycleDelisted, then records DELISTED in the same PostgreSQL transaction.
- Request Changes, Account suspension, bulk actions, automation, AI moderation, and advanced case states are not added; existing Course Review and staff surfaces remain the canonical locations for their existing commands.

## 7. Auditability

Each terminal resolution writes:

- audit_events.module = MODERATION;
- action = REPORT_RESOLVED;
- resolving Admin actor;
- report target;
- exact resolution reason;
- resolution action and target kind metadata.

The report row and moderation audit commit together. Delegated DELIST retains the existing COURSE_DELISTED catalog audit in the same transaction.

## 8. Concurrency and idempotency

Resolution locks the report row, checks that it is still OPEN, and conditionally writes the terminal tuple.

Proof:

- concurrent resolution produced one 200 and one 409;
- a later resolution attempt produced 409;
- the first outcome was retained and not overwritten.

## 9. Target-unavailable behavior

The focused PostgreSQL proof changed a report's historical revision reference so its target context could no longer be resolved. Admin detail still returned 200 with available = false, and dismissal still returned 200 with immutable audit history.

## 10. Frontend

Added:

- /[locale]/admin/reported-content;
- Reported Content Admin navigation entry;
- explicit Admin reports API client;
- queue/detail workspace.

States covered:

- loading skeleton;
- empty open queue;
- queue/detail error;
- open report;
- resolved history;
- already-resolved conflict;
- target unavailable;
- canonical Course delist action.

The UI does not render raw internal identifiers and links to the existing Course Lifecycle surface without duplicating Course management.

## 11. English, Arabic, and RTL

All new Admin-visible copy is present in en.ts and ar.ts. The workspace sets its own dir from the active locale and uses semantic controls, text status labels, keyboard focus styles, and disabled/loading states.

## 12. PostgreSQL and HTTP proof

Focused command:

go test -tags=integration -run 'TestAD14' -count=1 -v ./internal/httpapi

Result: 3 top-level tests passed, including Instructor, Student, and Suspended Admin subtests.

Coverage:

- Student creates the report through the existing protected route;
- Admin queue/detail are 200;
- malformed action is 422;
- dismissal is 200;
- resolved history remains readable;
- target is unchanged after dismissal;
- moderation audit exists;
- unknown report is 404;
- second resolution conflicts;
- concurrent resolution is safe;
- unavailable target can be dismissed;
- canonical Course DELIST changes public lifecycle and preserves the lifecycle audit plus report audit;
- Instructor, Student, Suspended Admin, and anonymous boundaries are denied.

Student exact-visible regression:

go test -tags=integration -run 'TestReportRouteRecordsTheRenderedInstanceForEveryTargetKind' -count=1 -v ./internal/httpapi

Result: the parent test and COURSE, LESSON, VIDEO, RESOURCE, and LAB_MATERIAL cases passed.

Schema proof:

go test -tags=integration ./internal/db

Result: passed; current schema is version 28 with migration 0028 applied and reversible.

## 13. Frontend proof

- npm run typecheck: exit 0.
- npm test: 386 passed, 0 failed, 0 skipped.
- Focused Admin API tests cover bounded pagination, CSRF/action payload, and typed 409 conflict handling.
- Existing workspace navigation tests cover the new bilingual navigation entry.

## 14. Canonical E2E

Command: cd frontend && npm run test:e2e:canonical

Observed result:

- production lane: 4 passed, 0 failed, 0 skipped;
- development lane: 166 passed, 0 failed, 0 skipped;
- aggregate: 170 passed, 0 failed, 0 skipped;
- exit status: 0.

The added browser case is ad14-admin-reported-content.spec.ts: Student submits a report through the existing UI, Admin opens Reported Content, dismisses it, and sees the resolved state/open queue result.

## 15. Backend gates

- go build ./...: exit 0.
- go vet ./...: exit 0.
- go vet -tags=integration ./...: exit 0.
- go test ./... -count=1: exit 0.
- git diff --check: clean.

## 16. Tracker effect

Before: 46 / 53 = 86.8%.

After: 47 / 53 = 88.7%.

AD-14 is promoted to E2E_PROVEN. ST-18 remains deferred and unchanged. SY-01, SY-02, SY-03, SY-08, and SY-09 remain untouched.

## 17. Product scope status

All Founder-required limited-paid-beta product feature tranches under D-094 are implemented/proven:

- IN-11 minimal Instructor Course Roster;
- AD-14 minimal Admin Reported-Content Resolution.

Advanced moderation, Instructor Analytics, Office Hours, Notification Center, Profile expansion, automated moderation, AI moderation, and bulk moderation remain outside this tranche.

## 18. Remaining paid-beta work

The remaining work is production/integration/operations only:

- SY-01;
- SY-02;
- SY-03;
- SY-08;
- SY-09;
- INF-01 real offsite proof.

No provider, deployment, Resend, R2, or deferred security work was started.

## 19. Repository safety

- Existing dirty worktree changes were preserved.
- No reset, clean, stash, restore, broad checkout, retained database drop, volume deletion, truncate, or process-wide kill was used.
- E2E cleanup was limited to test-owned resources.
- No unrelated Admin analytics, Instructor Analytics, Office Hours, Notification Center, Profile, payment, deployment, or system-row work was added.

## 20. Changed surfaces

- Backend report resolution repository, audit module selection, lifecycle transaction wrapper, router composition, migration 0028, focused integration/authorization fixtures.
- Frontend Admin API, workspace/page, navigation, dictionaries, API/navigation tests, rotating E2E fixture slot, and focused browser E2E.
- Tracker, design record, and this evidence file.
