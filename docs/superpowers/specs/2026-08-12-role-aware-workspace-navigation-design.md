# Role-Aware Workspace Navigation Design

**Status:** Approved for implementation

**Date:** 2026-08-12

**Scope:** Founder-acceptance remediation for authenticated role entry and existing-workflow navigation

## Outcome

After ordinary authentication, each unrestricted Account reaches its existing role home without
knowing a hidden URL:

| Role | Default destination |
|---|---|
| `STUDENT` | `/{locale}/learn/dashboard` |
| `INSTRUCTOR` | `/{locale}/instructor/courses` |
| `ADMIN` | `/{locale}/admin/catalog` |

A validated internal `returnTo` continues to take precedence over the role default. Mandatory
password change continues to interrupt navigation and then resolves through the same role defaults.

## Existing Surfaces Reused

No dashboard or product route is added. The batch reuses:

- Instructor Studio and Course Builder at `/{locale}/instructor/courses`;
- Admin Course Review and administration at `/{locale}/admin/catalog`;
- Admin Course Access at `/{locale}/admin/course-access`;
- Admin Instructor/staff operations at `/staff`;
- Student learning at `/{locale}/learn/dashboard` without changing that experience.

## Design

The existing identity return-to module remains the single owner of post-authentication destination
selection. Its role-root function receives the active locale and maps the three canonical session
roles to the existing routes. Login passes the locale it already holds. The authenticated header
uses the same role-root function, so its primary workspace action agrees with post-login behavior.

A thin shared Admin/Instructor workspace shell reuses the current global Navbar and Footer and adds
one responsive role navigation row. Admin navigation exposes Course Review/Administration, Course
Access, and Staff Operations. Instructor navigation exposes Studio/Courses and a Course Builder
entry that targets the existing new-Course control. Existing pages are wrapped by this shell rather
than copied or replaced.

Navigation labels are bilingual and follow the current locale. The shell does not fetch data, grant
authority, or infer permissions.

## Security and Failure Behavior

`safeReturnTo` remains the trust-boundary validator. External, malformed, API, and admission-loop
destinations remain rejected. A safe deep link is preserved even when it differs from the default
role root.

Navigation visibility is not authorization. Existing backend authentication, capability, ownership,
and password-change enforcement remain unchanged and authoritative. Direct access by the wrong role
continues to receive the existing backend refusal.

## Validation

Focused tests cover:

- locale-aware default destinations for Student, Instructor, and Admin;
- safe `returnTo` precedence and hostile-value fallback;
- mandatory-password-change completion using the same role roots;
- the visible Admin and Instructor navigation destinations;
- unchanged backend refusal of unauthorized Admin capability access.

The completed batch must pass relevant frontend tests, TypeScript typecheck, lint, and production
build. Backend/auth tests are run as authorization-regression evidence even though backend code is
not changed.

## Non-Goals

- No unified Admin Course Workspace.
- No pricing, UUID-selector, Course Access, entitlement, email, preview, analytics, or reports work.
- No Student learning redesign.
- No authorization or role-model change.
- No new dashboard/product route and no architecture or design-system rewrite.
