# S9 Transactional Email — Implementation Evidence

Date: 2026-08-09

Launch target: 2026-08-15

Starting head: `18fb7e033d0fad162caebe150fb641a00201e259`

Planning commit: `c531fc5f3a09a6dc17c5de14d2ee8217211fa9e5`

Backend/provider commit: `1f0a043`

Frontend action-link commit: `5a66081`

Independently reviewed at `9be0020` and **REJECTED** on one High finding. Remediation commits follow
that head; see "Independent review of `9be0020` — REJECTED" below. The sections between here and
that heading describe the implementation as built and were written before the review; where they
conflict with the review outcome, the review section is authoritative.

S9 remains open. This record contains no recipient addresses, provider credentials, message bodies,
or action bearers.

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

## Independent review of `9be0020` — REJECTED

An independent Claude review of the frozen range ending `9be0020` returned **REJECTED**: Critical 0,
High 1. The builder's own pre-review self-assessment of High 0, recorded above this section, was
wrong and is superseded by the reviewer's verdict.

### High-1 — staff invitation dispatch was unreachable

Root cause. The real staff-invitation producer wrote `template_contract` only into the encrypted
delivery payload, never into the immutable safe payload. Email discovery joins on
`c.template_contract = e.safe_payload->>'template_contract'`, so the join matched no row: real
`identity.staff_invitation_created` events were never discovered, never reached the delivery ledger,
and were never mailed. The reviewer observed six real staff-invitation outbox events, all lacking
the field, and confirmed the exact discovery predicate returned zero rows.

Why every test missed it. All prior email coverage hand-seeded `template_contract` into the safe
payload, so no test ever exercised what the real identity transaction actually commits.

Remediation. The field is now written at the real producer, using the already-approved
`staff-invitation-v1` contract, where the other seven intents already put it. Discovery is unchanged:
no special case for staff invitation, no weakened join, and no path around the durable ledger. A
verification sweep confirmed the other seven producers were already correct, so this was the only
affected intent.

Evidence. `TestStaffInvitationEmailReachesTheInviteeAndCompletes` drives the real producer end to
end and seeds nothing: an Admin creates the invitation, the committed safe payload is asserted to
carry `staff-invitation-v1`, discovery finds the real event, a `transactional_email_deliveries` row
reaches `ACCEPTED` on attempt 1 keyed by the immutable event UUID, the rendered email is addressed to
the invitee, and the `/staff/accept#token=…` credential taken from the delivered message completes
acceptance as an INSTRUCTOR. Reverting the one-line producer change fails this test, which was
confirmed by deliberately reverting it. The test also proves single-use replay refusal, tampered and
expired credential refusal, that the safe payload exposes neither bearer nor recipient, that attempt
evidence carries neither, and that an email retry produces no duplicate message and no duplicate
domain side effect.

### M-1 — historical first-boot email backfill hazard

Root cause. Discovery scanned every `outbox_events` row ever written. Enabling S9 against an existing
Gradex database would have enqueued the entire history at once, mailing credentials that may be
stale, expired, or superseded.

Product Owner decision. Recorded as [D-078](../../../DECISIONS.md#d-078--transactional-email-never-sends-historical-intents-created-before-activation).

Remediation. Migration 0017 adds a single-row `transactional_email_activation` table stamped once, by
the migration itself, with the instant delivery became possible for that database. Discovery admits
only intents whose `occurred_at` is at or after it. The boundary is stamped by the migration rather
than at first poll, because a first-poll boundary would exclude intents legitimately written between
deploy and the first worker tick. Workers only ever read it, so it is durable, survives restart and
Redis loss, does not live in process memory, and cannot advance. An absent boundary is an error, not
a default. Historical outbox rows are neither deleted nor mutated.

Evidence. `TestDiscoveryIgnoresPreActivationHistory` proves a pre-activation intent creates no
delivery row and no provider attempt while remaining in the outbox, that a post-activation intent is
discovered and dispatched normally, and that a restarted worker neither moves the boundary nor
backfills history. `TestDiscoveryKeepsUndiscoveredPostActivationIntentsEligible` proves a
post-activation intent that was not yet due is deferred rather than lost.
`TestConcurrentPollersShareOneActivationBoundary` runs four concurrent pollers and proves one
boundary, one activation row, no backfill, and exactly one delivery row and one provider message per
eligible intent.

### Resend redirect refusal (reviewer Low L-1)

The production Resend client now refuses to follow redirects, so a 3xx cannot replay the
Authorization header and recipient body to a host named by the response. A 3xx is classified as a
permanent failure under its own `redirect_refused` class. `TestResendSenderRefusesRedirect` proves
the redirect target is never contacted and no provider message ID is produced.

## Findings after remediation

- Critical: 0
- High: 0 — High-1 is CLOSED by fix and regression test.
- Medium: 2 deferred, both explicitly out of scope for this remediation pass:
  - M-2, provider stamped on queued rows can orphan rows after a provider switch. Deferred; the
    activation-boundary design did not make it necessary, and production rejection of the fake
    provider already prevents a fake-to-Resend switch on the normal launch path.
  - M-3, `RejectInvitation` Account lookup fallback. Deferred; the reviewer classified the
    problematic state as currently unreachable.
- Low: 4 deferred — `PhaseQueued` logging, copy-variable shadowing, `next lint` deprecation, and the
  reviewer's remaining cosmetic items. None is an S9 delivery defect.

M-1 is CLOSED by the activation boundary and its four acceptance tests.

## Remediation validation

Backend: `gofmt` clean, `go build ./...`, `go vet ./...`, `go vet -tags=integration ./...`, full
`go test ./...`, and the full `go test -tags=integration ./...` suite all pass against real
PostgreSQL, Redis, and MinIO. Race coverage passes for `internal/email`, `internal/outbox`, and
`internal/identity`. Regression is green for the closed-S11 journey and S6 Course invitation and
approval semantics, and for verification, password reset, and Course Invitation email acceptance.
Repository guards pass: documentation guard, exposure guard, and `git diff --check`. The frontend was
not touched by this remediation, so its pipeline is unchanged from the reviewed head.

Migration 0017 raises the schema to version 17, which this build now declares as supported.

## Review readiness

The original 30 repository tasks remain complete; the items above are review-remediation defects
found by independent review, not newly discovered scope, and no task completion evidence has been
backdated. Remaining S9 actions are an independent re-review of the cumulative range and the external
live sender-domain/provider proof. No live Resend provider proof was performed in this pass.

The implementation is technically ready for independent re-review. It does not independently close
S9, and this record is not an approval.
