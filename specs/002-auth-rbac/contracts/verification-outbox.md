# Contract: Verification Notification Outbox Intent

**Status**: S1B1 source contract; provider dispatch remains S9/LG-018
**Rules**: BR-008, BR-120, BR-122

## Event identity

Each eligible registration or resend creates one immutable source event in the same PostgreSQL
transaction as the Account/action-secret/evidence mutation:

```text
event_id:          stable UUID (also logical delivery operation ID)
event_type:        identity.email_verification_requested
schema_version:    1
source_module:     IDENTITY_AND_ACCESS
aggregate_type:    ACCOUNT
aggregate_id:      Account UUID
aggregate_revision Account revision
occurred_at:       database UTC time
available_at:      database UTC time
correlation_id:    safe request ID
```

Safe payload schema:

```json
{
  "action_secret_id": "00000000-0000-0000-0000-000000000000",
  "purpose": "EMAIL_VERIFICATION",
  "locale": "ar",
  "template_contract": "student-email-verification-v1",
  "secret_expires_at": "2026-07-30T12:00:00Z"
}
```

The safe payload contains no email, display name, bearer/digest, identifier HMAC, protected link,
provider value, or policy content.

## Protected payload

The same transaction inserts exactly one protected payload keyed by `event_id`:

```text
key_version
nonce
authenticated ciphertext
```

Plaintext before encryption contains only:

```json
{
  "destination": "Student.Name@example.com",
  "locale": "ar",
  "template_contract": "student-email-verification-v1",
  "verification_token": "<opaque bearer>",
  "expires_at": "2026-07-30T12:00:00Z"
}
```

The key comes from the approved secret-resolution boundary and is never stored with the payload.
Encryption uses an authenticated algorithm and binds the event ID/type/schema as associated data.
Nonce/key-version reuse is prohibited. Only the future delivery worker role may read/decrypt this
table. Routine application queries, logs, support tooling, Identity evidence, and Audit cannot.

If required key configuration, encryption, event insertion, or protected-payload insertion fails,
the complete registration/resend transaction rolls back. The public response claims no delivery.

## Source-command behavior

- New registration: co-commit Account, credential, policy acceptance, action-secret digest,
  registration evidence, event, and protected payload.
- Eligible resend: lock Account then live secret; create replacement; supersede/link prior secret;
  co-commit resend evidence, event, and protected payload.
- Existing registration email: no event.
- Unknown/ineligible verification request: no event.
- Successful verification consumption: no email event in S1B1.

An outbox row means durable Gradex intent only. It is not provider acceptance, send, delivery, inbox
placement, or user receipt.

## Future S9 consumer obligations

S9 may add delivery rows, immutable attempts, leases, consumer receipts, provider references, and
reconciliation. It must preserve:

- stable event/operation identity and at-least-once delivery semantics;
- unique consumer receipt/deduplication;
- bounded lease recovery and visible exhaustion;
- provider idempotency where supported;
- timeout as ambiguous until reconciled;
- minimum remaining secret validity before first provider acceptance;
- atomic supersession/replacement when an unaccepted secret is too close to expiry;
- no silent replacement after provider acceptance;
- no source-state rollback on provider failure.

The email provider, sender domain, templates/links, bounce/suppression policy, and production
monitoring remain blocked by LG-018.

## Verification

Integration tests prove:

1. source state and both outbox rows commit together or all roll back;
2. event IDs are stable and unique;
3. ordinary event JSON contains no destination or bearer;
4. protected ciphertext does not contain plaintext canaries;
5. only the correct configured key version can authenticate/decrypt the fixture;
6. existing/unknown/ineligible paths create no event;
7. resend creates one new linked intent and invalidates the old secret;
8. no test or UI claims provider acceptance or delivery.
