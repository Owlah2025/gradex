# LG-018 evidence — production transactional email (Resend)

Date: 2026-08-13

Batch base: `57288fe`

Status: **IMPLEMENTATION COMPLETE — OPERATIONAL GATE OPEN.** Every repository-side condition of
LG-018 is implemented and tested. The gate stays open until the Gradex sending domain reports
`verified` in the Resend account and real-inbox delivery evidence exists; both require founder
action recorded in [§7](#7-founder-actions-outstanding) and [§8](#8-real-inbox-acceptance-record).

## 1. Launch-critical message inventory

Ten transactional contracts exist. Each is produced by a committed domain transaction, carried by
the outbox, and rendered in Arabic and English. There is no marketing or broadcast message.

| # | Event type | Template contract | Produced by | Launch journey |
|---|---|---|---|---|
| 1 | `identity.email_verification_requested` | `student-email-verification-v1` | Student registration and verification resend | Account verification |
| 2 | `identity.password_reset_requested` | `account-password-reset-v1` | Password recovery request | Password reset |
| 3 | `identity.password_reset_completed` | `account-password-reset-completed-v1` | Completed reset / mandatory change | Security notice |
| 4 | `identity.staff_invitation_created` | `staff-invitation-v1` | Admin invites Instructor or Admin | Instructor invitation |
| 5 | `access.invitation_issued` | `course-access-invitation-v1` | Admin creates a Course Access Invitation | Student invitation |
| 6 | `access.granted` | `course-access-granted-v1` | Admin approval of an accepted invitation | Access granted |
| 7 | `access.invitation_rejected` | `course-access-invitation-rejected-v1` | Admin rejects an accepted invitation | Access refused |
| 8 | `access.invitation_cancelled` | `course-access-invitation-cancelled-v1` | Admin cancels before a decision | Access withdrawn |
| 9 | `access.entitlement_adjusted` | `course-access-adjusted-v1` | Admin expiry extend/shorten (BR-026) | Access period changed |
| 10 | `access.entitlement_revoked` | `course-access-revoked-v1` | Admin revocation (BR-026, AD07) | Access ended |

Messages 1–3 carry an expiring action credential; 4 and 5 carry a single-use invitation link. The
rest carry no credential and link only to `/{locale}/access` or `/login`.

Only 1, 2, 4 and 5 are required as real-inbox acceptance cases in §8; 9 and 10 are included because
BR-026 requires a transactional Student notification on every adjustment.

## 2. Architecture used

No new email or outbox system was introduced. The existing path is unchanged:

```text
domain transaction (commits its own work + outbox_events + encrypted payload)
  → transactional_email_deliveries discovery (activation-bounded)
  → worker claim under a lease (FOR UPDATE SKIP LOCKED, provider-scoped)
  → protected payload opened, template rendered (ar/en)
  → provider send with a stable Idempotency-Key
  → ACCEPTED with provider_message_id | QUEUED with retry_at | PERMANENT_FAILED | EXHAUSTED
```

The domain transaction never calls the provider. A Resend outage cannot roll back a committed
Gradex action: the outbox row is already durable and the delivery attempt is a separate transaction.

## 3. Resend integration status

Already present before this batch and re-verified against current Resend documentation
(2026-08-13):

- `POST https://api.resend.com/emails`, `Authorization: Bearer <key>`, JSON body with `from`, `to`,
  `subject`, `html`, `text`, `reply_to` — matches the current send-email reference.
- `Idempotency-Key` request header, ≤256 characters.
- HTTPS-only endpoint, credential-free URL, redirects refused (a 3xx is recorded as
  `redirect_refused` rather than replaying the Authorization header to another host).
- Failure classification: 408/429/5xx and `concurrent_idempotent_requests` are transient and honour
  `Retry-After`; other 4xx are terminal. Provider text is never propagated — only a sanitised class
  and code reach the ledger and the log.

Added by this batch:

- Production refuses a provider sandbox sender (`resend.dev` and subdomains). `onboarding@resend.dev`
  cannot start a production deployment.
- `cmd/email-preflight` asks the provider whether the configured sending domain is verified.
- `email.ProviderIdempotencyWindow` and `email.RetryBudget()` make the duplicate-suppression
  relationship explicit and test-enforced.

## 4. Duplicate-delivery guarantee

The dispatcher derives its key from the immutable outbox event identity: `gradex/<event_id>`. It is
stable across every retry of the same message and unique across messages; nothing random is
involved.

Resend expires an idempotency key after **24 hours**. The retry budget is 30s + 2m + 10m + 30m =
**42m30s** across at most 5 attempts, so every retry of one message falls inside the window.

The guarantee is therefore: **at-most-once delivery per outbox event within the provider window, and
at-least-once intent**. A duplicate is possible only if the same event were retried more than 24
hours after its first attempt, which the attempt budget and the `EXHAUSTED` terminal state prevent.
`TestRetryBudgetStaysInsideProviderIdempotencyWindow` fails if either side of that relationship is
changed.

## 5. Delivery evidence and monitoring

`transactional_email_deliveries` (one row per event) and `transactional_email_attempts` (one row per
attempt) already distinguish the three outcomes the gate asks for:

| Outcome | Delivery status | Attempt outcome | Evidence retained |
|---|---|---|---|
| Provider acceptance | `ACCEPTED` | `ACCEPTED` | `provider_message_id`, `accepted_at` |
| Retryable failure | `QUEUED` | `TRANSIENT_FAILURE` | failure class, provider code, `retry_at` |
| Terminal rejection | `PERMANENT_FAILED` | `PERMANENT_FAILURE` | failure class, provider code, `terminal_at` |
| Retry budget spent | `EXHAUSTED` | `EXHAUSTED` | last failure class, `terminal_at` |

A message is only `ACCEPTED` when the provider returned a message ID; an attempt alone never marks
it delivered. Expired leases are recovered and, past the attempt budget, closed as `EXHAUSTED`.

Monitoring: `deploy/monitoring/rules.yml` now alerts on `transactional_email` events whose phase is
`permanent_failure` or `exhausted`, using the existing log-alert delivery mechanism.

## 6. Bounce, complaint and suppression decision

**Decision proposed for founder approval: rely on Resend's own suppression and dashboard for
post-acceptance bounce and complaint handling at launch. Do not build webhook ingestion in this
batch.**

Reasoning:

- No current MVP authority requires in-product bounce records. The domain design states that
  "exact provider, suppression, bounce, and retry policy remain blocked on `LG-018`", so this gate
  is where the policy is chosen rather than where a pre-existing requirement is met.
- Resend maintains its own suppression list and shows per-message delivery, bounce and complaint
  status in its dashboard, retained for the account. Launch volume is a handful of messages per
  Student, so operator review is tractable.
- Gradex already retains its own acceptance evidence (§5), so the two halves together cover the
  question "did we hand it over, and did it land".
- Webhook ingestion means a new unauthenticated public endpoint, Svix signature verification
  (`svix-id`, `svix-timestamp`, `svix-signature`), replay and out-of-order handling, and an event
  store. That is a subsystem, and building it unprompted for launch volume is not defensible.

Revisit trigger: implement webhook ingestion when any of these becomes true — sustained bounce rate
above 2%, any complaint, the first support case where a Student reports a message Gradex recorded as
`ACCEPTED`, or a business need for an in-product suppression list.

Until then the operator obligation is explicit: **check the Resend dashboard for bounces and
complaints as part of the daily launch check, and treat any `transactional_email_undelivered` alert
as an operational incident.**

## 7. Founder actions outstanding

These cannot be completed from the repository. Each is external and must be recorded here when done.

1. **Choose the sending subdomain.** Resend recommends a subdomain so sending reputation is isolated
   from the root domain. Recommended: `notifications.<gradex-domain>` with sender
   `no-reply@notifications.<gradex-domain>`.
2. **Add the domain in Resend.** Dashboard → Domains → Add Domain. Enter the subdomain and select
   the sending region closest to Kuwait (`eu-west-1` unless a data-residency decision says
   otherwise). Resend then generates the exact DNS records for that domain.
3. **Create the DNS records at the DNS provider** (Cloudflare or Hostinger, whichever is
   authoritative for the domain). Resend currently generates:
   - one **MX** record on host `send`, value copied from Resend
     (`feedback-smtp.<region>.amazonses.com` form), priority `10`;
   - one **TXT** SPF record on host `send`, value copied from Resend (`v=spf1 include:amazonses.com
     ~all` form);
   - one **TXT** DKIM record on host `resend._domainkey`, value copied from Resend.
   On Cloudflare, enter only the host prefix (`send`, not `send.example.com`) and set the DKIM
   record's proxy status to **DNS only**. Copy every value from the dashboard; none of them are
   written here, because a value copied from documentation instead of your own account will not
   verify.
4. **Add DMARC.** A `TXT` record on host `_dmarc` (or `_dmarc.notifications` for the subdomain),
   starting at `v=DMARC1; p=none; rua=mailto:<a mailbox you read>` so reports arrive before the
   policy is tightened.
5. **Wait for verification.** Resend → Domains shows the domain as `verified` once the records
   propagate. Success looks like: status `verified`, DKIM and SPF both green.
6. **Create the production API key.** Resend → API Keys → Create, with sending permission. Put it
   straight into the deployment secret store as `EMAIL_API_KEY`. Do not paste it into chat, a
   ticket, or any file in this repository.
7. **Set the sender in the production environment**: `EMAIL_FROM_ADDRESS=no-reply@notifications.<domain>`,
   `EMAIL_FROM_NAME=Gradex`, optionally `EMAIL_REPLY_TO=<a monitored support mailbox>`, and confirm
   `PUBLIC_ORIGIN` is the real `https://` origin — every link in every message is built from it.
8. **Run the preflight against production configuration**:
   ```bash
   go run ./cmd/email-preflight
   ```
   Success looks like: `provider domain status: verified` and
   `result: LAUNCH READY — provider reports the Gradex sending domain verified`. Exit status 0. The
   command sends no mail and prints no secret. `-offline` checks configuration only.
9. **Run the four acceptance journeys in §8** and record the results in this file.

## 8. Real-inbox acceptance record

Not yet performed. Each row is filled in after step 9 above, using real inboxes under Gradex control
and, where practical, more than one mailbox provider (for example one Gmail and one Outlook
address). Record no tokens and no secrets — the message ID and the outcome are sufficient.

| # | Gradex action | Recipient (mailbox provider) | Delivery row `ACCEPTED` | Resend message ID | Inbox result | Link correct | Journey completed |
|---|---|---|---|---|---|---|---|
| 1 | Student registers | | | | | | |
| 2 | Password reset requested | | | | | | |
| 3 | Admin invites an Instructor | | | | | | |
| 4 | Admin creates a Course Access Invitation | | | | | | |
| 5 | Admin adjusts entitlement expiry (BR-026) | | | | | | |
| 6 | Admin revokes access (BR-026) | | | | | | |

Check for each row: the sender is the Gradex verified domain and not a provider sandbox address; the
link origin is the production `https://` origin and never `localhost`; the credential in the link
still works when clicked; the message did not land in spam.

Delivery-row query for the middle column (no token or payload is selected):

```sql
SELECT d.event_id, d.template_contract, d.locale, d.status, d.provider_message_id, d.accepted_at
  FROM transactional_email_deliveries d
 ORDER BY d.created_at DESC
 LIMIT 20;
```

## 9. Security posture

- The API key is resolved through `config.Secret`, which redacts under every formatting verb. It is
  read only by the worker and by `cmd/email-preflight`, is sent only as an `Authorization` header to
  `api.resend.com`, and never reaches a log, an error, or any frontend bundle.
- `cmd/email-preflight` scrubs `EMAIL_API_KEY` and `DATABASE_URL` from anything it prints, the same
  defense `cmd/migrate` and `cmd/bootstrap-admin` use.
- Provider responses are never echoed: only a sanitised class and code are stored or logged, proven
  by `TestPreflightFailuresNeverEchoProviderBodyOrKey` and the existing sender tests.
- The lifecycle observer records phase, event ID, template, locale, provider, attempt, failure class
  and provider code — never recipient, subject, body, action URL, or token.
- The outbox payload carrying recipient and credential stays encrypted at rest; the dispatcher opens
  it only in memory, immediately before rendering.
- Production configuration cannot fall back to a development delivery mode: `mailpit` is refused
  outside development, production requires `resend`, and a provider sandbox sender is refused.
