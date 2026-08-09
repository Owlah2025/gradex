# Contract: Transactional Sender and Dispatcher

## Provider-neutral sender

The application/infrastructure dispatch layer supplies:

- immutable message identity;
- one recipient;
- sender display/address resolved by configuration;
- optional reply-to;
- subject;
- plain text;
- HTML.

The sender returns either:

- accepted with an opaque provider message identifier; or
- a typed error containing only transient/permanent classification, a bounded safe provider code, and optional retry-after duration.

Provider response bodies, HTTP types, SDK types, API keys, and raw provider errors do not cross this boundary.

## Resend HTTP contract

```http
POST https://api.resend.com/emails
Authorization: Bearer <secret>
Content-Type: application/json
User-Agent: gradex-transactional-email/1
Idempotency-Key: gradex/<outbox-event-uuid>
```

Payload fields are limited to `from`, `to`, `subject`, `text`, `html`, and optional `reply_to`. One synchronous attempt has the configured total timeout and performs no internal retry.

An accepted response is HTTP 2xx JSON with a non-empty `id`. The ID may be stored in the delivery ledger but never enters a domain entity or public API.

## Fixed event/template mapping

| Event type | Template contract | Action destination |
|---|---|---|
| `identity.email_verification_requested` | `student-email-verification-v1` | `/verify-email/result#token=…` |
| `identity.password_reset_requested` | `account-password-reset-v1` | `/recover/reset#token=…` |
| `identity.password_reset_completed` | `account-password-reset-completed-v1` | No credential; `/login` may be referenced |
| `identity.staff_invitation_created` | `staff-invitation-v1` | `/staff/accept#token=…` |
| `access.invitation_issued` | `course-access-invitation-v1` | `/{locale}/access?invitation_id=<aggregate>#token=…` |
| `access.granted` | `course-access-granted-v1` | `/{locale}/access` |
| `access.invitation_rejected` | `course-access-invitation-rejected-v1` | `/{locale}/access` |
| `access.invitation_cancelled` | `course-access-invitation-cancelled-v1` | `/{locale}/access` |

Any other pair is permanently refused by S9; it is not guessed or silently rendered.

## Privacy contract

Structured events may carry only event/delivery IDs, template contract, locale, attempt number, state, provider, safe error class/code, duration, and retry time. Destination, subject, content, credential, canonical action link, API key, Authorization header, and raw provider response/error text are forbidden.
