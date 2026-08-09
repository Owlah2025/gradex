# S9 Transactional Email — Implementation Evidence

Date: 2026-08-09

Launch target: 2026-08-15

Starting head: `18fb7e033d0fad162caebe150fb641a00201e259`

Planning commit: `c531fc5f3a09a6dc17c5de14d2ee8217211fa9e5`

Backend/provider commit: `1f0a043`

Frontend action-link commit: `5a66081`

S9 remains open until independent review. This record contains no recipient addresses, provider
credentials, message bodies, or action bearers.

## Fixed launch intent inventory

| Intent | Producer and durable source | Recipient / locale | Template and action | Expiry | Delivery identity/evidence |
|---|---|---|---|---|---|
| Student email verification | Identity admission transaction → `identity.email_verification_requested` | Account email / Account locale | `student-email-verification-v1` → `/verify-email/result#token=…` | Existing configured verification-secret expiry | Immutable outbox event UUID → stable provider key; delivery/attempt ledger |
| Password reset | Identity recovery transaction → `identity.password_reset_requested` | Account email / Account locale | `account-password-reset-v1` → `/recover/reset#token=…` | Existing configured reset-secret expiry | Same |
| Password changed | Identity recovery completion → `identity.password_reset_completed` | Account email / Account locale | `account-password-reset-completed-v1` → sign-in | Not applicable | Same |
| Staff invitation | Identity staff-invitation transaction → `identity.staff_invitation_created` | Invitation email / requested locale | `staff-invitation-v1` → `/staff/accept#token=…` | Existing staff invitation-secret expiry | Same |
| Course Access Invitation | Access invitation transaction → `access.invitation_issued` | intended Student email / Account locale, otherwise validated request locale | `course-access-invitation-v1` → `/{locale}/access?invitation_id=…#token=…` | Existing Course invitation-secret expiry | Same; accepting creates zero Course access |
| Course access granted | Admin Approval transaction → `access.granted` | Student Account email / Account locale | `course-access-granted-v1` → access status | Not applicable | Same; domain grant remains authoritative |
| Course invitation rejected | Admin rejection transaction → `access.invitation_rejected` | Student Account email / Account locale | `course-access-invitation-rejected-v1` → access status | Not applicable | Same; no grant side effect |
| Course invitation cancelled | Admin cancellation transaction → `access.invitation_cancelled` | invitation email / Account or original invitation locale | `course-access-invitation-cancelled-v1` → access status | Not applicable | Same; no grant side effect |

The protected outbox payload remains the only delivery source for recipient and existing bearer.
Email creates no verification, reset, invitation, Entitlement, or Enrollment state.

## Implementation proof

- Provider-neutral boundary: `email.Sender` accepts only the fixed Gradex `Message`, stable
  idempotency key, and safe result/error classifications. Domain packages import no Resend types.
- Resend: one HTTPS `POST /emails` per attempt, bounded 1–30 second client timeout, Authorization
  secret exposed only into the request header, official `Idempotency-Key`, safe response-ID capture,
  64 KiB response bound, and transient/permanent classification without raw provider content.
- Durability: migration 0016 adds PostgreSQL delivery and attempt ledgers beside the immutable
  protected outbox. Workers claim with row locking and leases; Redis is not authoritative.
- Retry: five attempts with 30 seconds, 2 minutes, 10 minutes, and 30-minute later delays. A longer
  provider `Retry-After` is honored. Permanent recipient/configuration refusals are terminal.
- Recovery/idempotency: expired leases close the prior attempt before retry; the fifth expired lease
  becomes exhausted. Concurrent pollers cannot claim the same message. Every retry uses
  `gradex/<immutable-event-uuid>` as the provider key.
- Templates: all eight contracts have authored English and Arabic text/HTML. Arabic HTML declares
  `lang="ar" dir="rtl"`; action links use fragments and the configured public origin. There are no
  marketing, tracking, unsubscribe, commerce, or debug elements.
- Privacy: renderer/provider errors and lifecycle logs omit recipient, body, link, token, and API key.
  Header injection is refused before network I/O. The exposure allowlist pins the single Resend API
  key plaintext boundary.
- Observability: PostgreSQL and safe structured events distinguish attempt start, provider accepted,
  transient failure, retry scheduled, permanent failure, and exhausted. The launch runbook includes
  safe message/attempt diagnosis queries.

## Acceptance results

- Verification: the real registration transaction creates its protected outbox intent; the dispatcher
  renders/captures the fake delivery; the HTTP journey consumes the delivered fragment bearer and
  verifies the Account. Existing integration cases still prove wrong-purpose, expired, superseded,
  and replay refusal.
- Password reset: the recovery request creates its protected intent; the dispatcher supplies the
  delivered reset bearer; completion changes the password, revokes live session families, returns no
  session, rejects the old password, and permits the new password. Existing integration cases prove
  wrong-purpose, expiry, and replay refusal.
- Course invitation: Admin creation produces the delivered fragment link; Student acceptance leaves
  zero Entitlements and zero Enrollments; Admin Approval creates exactly one provenance-bearing
  Entitlement and one Enrollment; repeated approval reuses the same grant. The final focused S11
  Chromium journey passed 1/1 after the fragment migration.
- Staff invitation: provider rendering, fragment-only frontend preview/completion requests, fixed-role
  completion, and existing staff secret lifecycle integrations pass. The UI distinguishes a permanent
  invalid bearer from a retryable preview transport failure.

## Validation

| Gate | Result |
|---|---|
| Backend formatting and build | PASS — clean `gofmt`; `go build ./...` |
| Backend static analysis | PASS — `go vet ./...`; `go vet -tags=integration ./...` |
| Backend default race suite | PASS — `go test -race ./...` |
| Backend complete integration | PASS — `go test -tags=integration ./... -count=1` against migrated PostgreSQL/Redis/MinIO |
| Focused email ledger/provider integration | PASS, including TLS, retries, permanent refusal, stale lease, concurrency, exhaustion, and stable idempotency |
| Verification/reset delivery journey | PASS against real PostgreSQL using captured rendered messages, not reconstructed bearers |
| Course delivery/approval journey | PASS against real PostgreSQL; zero-before-approval and exact-one-after-approval |
| Frontend install/static/unit | PASS — `npm ci`, typecheck, lint, 173/173 tests |
| Frontend production build | PASS — Next.js optimized build includes `/staff/accept` |
| Focused S11 Chromium | PASS — 1/1 |
| Dependency audit | PASS — full and production-only audits report zero vulnerabilities |
| Documentation/exposure/diff guards | PASS — 184 Markdown files; 14 approved exposure call sites; `git diff --check` clean |
| Clean-code/test guard | PASS after ledger/dispatcher decomposition, contract deduplication, header validation, and behavior-focused boundary review |

## Live Resend compatibility and external work

`EMAIL_API_KEY` and `EMAIL_FROM_ADDRESS` were absent from the implementation environment. No live
message was sent and no sender/domain acceptance is claimed. After T047 supplies the real domain/DNS:

1. verify the production sender domain in Resend;
2. inject the API key and verified From address through the production secret/configuration facility;
3. send one controlled transactional message to a Product Owner-controlled test recipient;
4. retain redacted evidence that authentication, sender acceptance, text/HTML acceptance, and provider
   ID return succeeded.

## Findings and review readiness

- Critical: 0
- High: 0
- Medium: 0
- Low: 1 — Next.js reports that `next lint` is deprecated; the current command still passes and this is
  a tooling migration, not an S9 delivery defect.

All 30 repository tasks are complete. Remaining S9 actions are the independent frozen-range closure
review and the external live sender-domain/provider proof. The implementation is technically ready
for independent closure review; it does not independently close S9.
