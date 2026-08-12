# Batch C design: production staff / Instructor composition

## Scope

Resolve C4 only: production must compose the existing PostgreSQL-backed staff lifecycle so an
authorized Admin can invite an Instructor, observe the relevant invitation/account state, suspend the
Instructor, and reinstate the same Instructor. This does not add role editing, generic account
management, Student administration, or a second identity system.

## Authority sequence

Before changing production behavior, amend S1C §19 and its remediation tasks. The amendment will
authorize the production prerequisite evaluation, the minimum Instructor/status read surface, the
existing staff routes in production, the needed Admin UI, and production-composition verification.
The amendment will expressly exclude fake auth/email, arbitrary role changes, bypasses of password or
invitation controls, and generic user management.

## Composition design

`buildProductionFoundations` will no longer use `EnvDevelopment` as the condition for staff
composition. Instead, production will evaluate explicit staff prerequisites before it constructs the
same `StaffFoundation` used in development. The evaluator will require the existing session and CSRF
boundary, capability and recent-auth policies, production-safe password screening and Redis limiting,
audit and durable outbox dependencies, and enabled Resend-backed transactional email configuration.
It will also reject fake authentication, fake email, and development-only identity seams.

If any prerequisite is absent, production startup fails with the named unmet prerequisite. It does not
mount a reduced route set or fall back to a development composition. The staff service continues to
write a durable outbox event; handlers never call Resend directly.

## Staff operations

Invitation preview and completion, suspension, reinstatement, action-secret handling, audit records,
and session-family revocation remain in the current identity/staff service. Add only a staff-scoped
read model if the existing pending-invitation listing cannot show the Instructor accounts and states
needed for operations. It will expose only Instructor invitations/accounts and their operational state
to an Admin with the existing capability boundary.

The Admin page will consume that read model and show status with per-row suspend/reinstate actions.
The current manual account-ID controls will be removed. The UI remains a convenience layer; backend
capability, origin, CSRF, and recent-auth checks remain authoritative.

## Verification

Production-mode PostgreSQL integration coverage will prove route mounting and the complete Admin
invite → durable outbox intent → Instructor completion/login/authorization → suspension denial →
reinstatement journey. It will also cover Student, Instructor, anonymous, and stale-recent-auth
denials; fake composition rejection; and each named fail-closed prerequisite. Frontend tests and the
existing automated E2E seam will cover the real invitation journey and Admin status actions without
manual browser operation.
