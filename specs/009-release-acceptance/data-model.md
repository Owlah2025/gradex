# Data Model: S11 — Release Acceptance

S11 creates no production entity and no migration. It observes existing schema-version-15 records through test-only snapshots.

## AcceptanceRun

Evidence record retained outside the application database.

| Field | Rule |
|---|---|
| revision | Exact full Git commit tested |
| origin_class | `local`, `disposable-production-like`, or `public-staging` |
| origin | Credential-free HTTPS origin for external runs; no secret-bearing URL |
| database | Safety-gated isolated database name |
| schema_version | Exactly 15 for this slice |
| checks | Exact commands and selected test names |
| results | Pass/fail counts and explicit unproven boundaries |
| findings | Severity, evidence, disposition, and closure impact |

## LearningStateSnapshot

Test-only observation of existing authoritative state.

### Entitlement observation

- `found`, `count`, `id`, `state`
- `grant_source` must be `MANUAL_INVITATION`
- `source_invitation_id` must equal the approved Invitation
- `original_access_ends_at` and `access_ends_at` must equal the approval snapshot before any separately scoped future adjustment
- `revision` and optional `revoked_at`

### Enrollment observation

- `found`, `count`, `id`, `created_at`
- exactly one per Student and Course before and after replay

### Progress observation

- stable `course_lesson_identity_id`
- `max_position_seconds`, `last_position_seconds`, completion fields, and update time
- belongs to the unique Enrollment and never appears as a side effect of denial

## Existing state transitions under acceptance

```text
Account registration -> email verification -> authenticated session

PENDING_STUDENT_ACCEPTANCE
  -> PENDING_ADMIN_APPROVAL     (zero Entitlements, zero Enrollments)
  -> APPROVED                   (one ACTIVE MANUAL_INVITATION Entitlement,
                                 one Enrollment)
  -> APPROVED replay            (same Entitlement, same Enrollment)
```

Invalid action secrets leave the prior state unchanged. Protected authorization reads Entitlement state; it never treats Invitation state as access.
