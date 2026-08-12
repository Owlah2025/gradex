# Data Model: S9 Transactional Email

## Existing immutable source

`outbox_events` and `outbox_protected_payloads` remain unchanged and authoritative for what must be delivered. Every S9 event has exactly one protected payload and an authenticated associated-data binding to event identity/type/version/aggregate.

## Transactional Email Delivery

One row per supported outbox event.

| Field | Meaning |
|---|---|
| `event_id` | Primary/foreign key to immutable outbox event; message identity and idempotency root |
| `template_contract` | Validated fixed template contract copied for safe operations |
| `locale` | `ar` or `en` |
| `status` | `QUEUED`, `SENDING`, `ACCEPTED`, `PERMANENT_FAILED`, or `EXHAUSTED` |
| `attempt_count` | Completed/started attempt count, constrained to 0–5 |
| `next_attempt_at` | PostgreSQL schedule for queued retry |
| `lease_token`, `lease_expires_at` | Opaque concurrent-worker claim; no provider credential |
| `provider` | `fake`, development-only `mailpit`, or `resend` |
| `provider_message_id` | Optional provider acceptance identifier, infrastructure evidence only |
| `last_failure_class`, `last_provider_code` | Safe bounded classifications; never raw provider message/body |
| timestamps | queued, accepted/terminal, created, updated |

Invariants:

- Exactly one delivery per event.
- Only the eight approved event/template pairs are insertable by the dispatcher.
- Only events that occurred at or after the durable activation boundary are
  discoverable, so switching delivery on never mails historical credentials.
- Terminal rows cannot return to queued/sending.
- Only an unexpired matching lease may complete an attempt.
- A stale `SENDING` lease becomes claimable without changing message identity.

## Delivery Attempt

One row per numbered claim.

| Field | Meaning |
|---|---|
| `id` | Opaque attempt UUID |
| `event_id`, `attempt_number` | Unique durable attempt identity |
| `lease_token` | Binds completion to the claimant |
| `started_at`, `finished_at` | Timing evidence |
| `outcome` | `STARTED`, `ACCEPTED`, `TRANSIENT_FAILURE`, `PERMANENT_FAILURE`, `EXHAUSTED` |
| `failure_class`, `provider_code` | Safe classifications |
| `provider_message_id` | Optional accepted provider ID |
| `retry_at` | Persisted schedule when transient and attempts remain |

No recipient, subject, body, action link, token, ciphertext, API key, or raw provider error is stored.

## Ephemeral types

- `DeliveryPayload`: decrypted existing destination, locale, template, optional credential and expiry.
- `TransactionalMessage`: destination, subject, text, HTML, optional reply-to; lives only for one call.
- `SendResult`: accepted provider message ID.
- `SendError`: transient/permanent class, safe provider code, optional retry-after.

## State transitions

```text
new supported outbox event -> QUEUED
QUEUED/stale SENDING -> SENDING (attempt + lease)
SENDING -> ACCEPTED
SENDING -> QUEUED (transient, attempts < 5)
SENDING -> EXHAUSTED (transient, attempt 5)
SENDING -> PERMANENT_FAILED (permanent/render/decrypt/config)
```
