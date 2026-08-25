# IN-11 — Minimal Instructor Course Roster

**Date:** 2026-08-24
**Verdict:** `PROVEN — MINIMAL INSTRUCTOR COURSE ROSTER READY FOR LIMITED PAID BETA`
**Authority:** Founder tranche IN-11; [D-045](../../DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation); [D-094](../../DECISIONS.md#d-094--limited-paid-beta-scope-narrows-gap-06-without-cancelling-post-beta-mvp-features)

## Scope

This tranche implements only the minimal Course-scoped Instructor roster required by D-094.
Instructor Analytics, Office Hours, Admin Reported-Content Resolution, Notifications, Profile expansion,
exports, bulk email, payment information, deployment, Resend, R2, and unrelated security findings were
not implemented.

## Membership and access authority

- Roster membership is the durable `enrollments` relationship, not an invitation and not a current-access
  check. The schema enforces one Enrollment per `(student_account_id, course_id)` in
  `backend/internal/db/migrations/0013_enrollments.up.sql`.
- `course_access_invitations` remains workflow state only. This follows D-045/BR-029.
- Access state is derived from the latest Course-scoped Entitlement for that Student/Course, ranked by
  `updated_at`, then `created_at`, then stable entitlement ID. Entitlement history therefore produces one
  roster row.
- State precedence follows access authority: `REVOKED`, stored expiry `EXPIRED`, retirement-ineligible
  `DENIED`, account/Course access suspension `SUSPENDED`, otherwise `ACTIVE`. An enrolled Student with no
  applicable Course Entitlement is represented as `DENIED` without hiding the historical membership.
- `access_ends_at` is the stored effective Entitlement expiry; the API serializes timestamps in UTC.
  `access_started_at` is the Entitlement creation instant, while `enrolled_at` is the Enrollment instant.
- Course `DELISTED`/`ARCHIVED` state does not remove an owner's historical roster visibility. A Course-level
  access suspension changes the displayed state without mutating Student Entitlement state.

## Data exposed

`GET /api/v1/courses/:id/students` returns:

The following shows the serialized shape; timestamp values are illustrative UTC instants.

```json
{
  "items": [
    {
      "display_name": "Active Student",
      "access_status": "ACTIVE",
      "enrolled_at": "2026-08-21T00:00:00Z",
      "access_started_at": "2026-08-21T00:00:00Z",
      "access_ends_at": "2026-09-20T00:00:00Z"
    }
  ],
  "page": 1,
  "page_size": 20,
  "has_next": false
}
```

The row contains no email, phone, payment data, purchase request data, Admin note, session/security
data, invitation token, entitlement identifier, student identifier, or Course identifier. The Instructor
reaches it by selecting an owned Course in the existing Course Builder; no ID-copy or manual route-entry
workflow was added.

## Authorization and routing

The route is mounted under the existing owned `GET /api/v1/courses/:id` group in
`backend/internal/httpapi/catalog_routes.go`. It uses the existing chain:

1. authenticated session;
2. `CONTENT_MANAGEMENT` capability, which applies the canonical active-principal and role policy;
3. `RequireCourseOwnership`, using the existing `CourseOwnershipChecker` predicate.

The real HTTP proof confirms:

| Caller | Result |
|---|---:|
| Owner Instructor | `200` |
| Non-owner Instructor | `403` |
| Instructor requesting another Course | `403` |
| Student | `403` |
| Suspended Instructor | `403` |
| Anonymous | `401` |

The endpoint does not create an Admin bypass or weaken the existing authorization matrix.

## Query and pagination design

`backend/internal/catalog/roster.go` uses one bounded PostgreSQL query:

- Enrollment is the driving relation.
- Course ownership is repeated in the SQL predicate as a defense-in-depth boundary.
- Entitlement history is reduced with `row_number()` before the left join, preventing duplicate rows.
- `LIMIT page_size + 1` determines `has_next`; page size defaults to 20 and is capped at 100.
- Ordering is deterministic: Enrollment creation time ascending, then the stable Student account ID as
  the server-side tie-breaker; the ID is never serialized.
- No per-Student entitlement query or N+1 evaluator loop is used.

## PostgreSQL and HTTP integration proof

Command:

```text
go test -tags=integration ./internal/httpapi -run TestInstructorCourseRosterHTTPAPIRealPostgreSQL -count=1
```

Result: `PASS`.

The disposable real-PostgreSQL fixture proves:

- Owner Course A returns one active, one expired, and one revoked historical Student.
- Two revoked Entitlement history rows still produce one revoked Student row.
- Page 1/page 2 metadata and bounded results are correct.
- An archived owned Course remains visible to its Instructor.
- Empty owned Course returns `200` with `items: []`.
- Course access suspension displays `SUSPENDED` while the active Entitlement remains `ACTIVE` in the database.
- Non-owner, cross-Course, Student, suspended-Instructor, and anonymous boundaries return the canonical
  denial statuses.
- Serialized JSON excludes password, phone, payment/purchase, Admin-note, session/security/token,
  entitlement/student/Course identifier fields and fixture emails.

## Frontend

The existing flow is:

`Instructor Portal → My Courses → selected Course → View students`.

`frontend/src/components/instructor/course-roster.tsx` renders a semantic responsive table with:

- Student display name;
- textual access status (not color-only);
- joined date;
- access-start date;
- access-until date;
- bounded previous/next controls;
- loading, error, and normal empty states.

Visible strings were added to both `frontend/src/lib/i18n/dictionaries/en.ts` and `ar.ts`. The existing
locale provider supplies RTL direction and locale-aware Kuwait date formatting through
`formatLearningExpiry`.

Focused frontend behavior is covered by `course-roster.test.ts`; the browser journey covers owner display,
active/expired labels, date elements, privacy checks, and non-owner denial.

## E2E evidence

Focused command:

```text
npm run test:e2e:development -- e2e/instructor-course-roster.spec.ts
```

Result: `1 passed`.

Authoritative command:

```text
cd frontend && npm run test:e2e:canonical
```

Result:

| Lane | Passed | Failed | Skipped | Exit |
|---|---:|---:|---:|---:|
| Production | 4 | 0 | 0 | 0 |
| Development | 165 | 0 | 0 | 0 |
| **Aggregate** | **169** | **0** | **0** | **0** |

The accepted baseline was 168 / 0 / 0. The one added E2E case is
`frontend/e2e/instructor-course-roster.spec.ts`.

## Gates

- `go build ./...`: exit `0`.
- `go vet ./...`: exit `0`.
- `go vet -tags=integration ./...`: exit `0`.
- `go test ./... -count=1`: exit `0`, all reported packages green.
- `go test -tags=integration ./internal/httpapi -run TestInstructorCourseRosterHTTPAPIRealPostgreSQL -count=1`: `PASS`.
- `cd frontend && npm run typecheck`: exit `0`.
- `cd frontend && npm test`: **383 passed / 0 failed / 0 skipped**.
- Canonical E2E: **169 passed / 0 failed / 0 skipped**.

## Tracker effect

The canonical tracker [FUNCTIONAL_COMPLETION.md](../../mvp/FUNCTIONAL_COMPLETION.md) moves IN-11 from
`PARTIAL` to `E2E_PROVEN` for the D-094 beta-scoped roster contract.

```text
Before: 45 / 53 = 84.9%
After:  46 / 53 = 86.8%
```

The denominator remains 53. ST-18 remains beta-deferred. AD-14 remains `NOT_IMPLEMENTED` and is the
remaining required product tranche before the limited paid beta.

## Safety and exclusions

- No migration was added.
- No payment, entitlement lifecycle, enrollment lifecycle, media, provider, deployment, or system-row
  behavior was redesigned.
- No Analytics, Office Hours, Notification Center, Profile expansion, export, or bulk email surface was
  added.
- PostgreSQL proof used a disposable integration database; canonical E2E used run-owned disposable
  databases and processes. No retained stack was stopped and no destructive repository command was used.
