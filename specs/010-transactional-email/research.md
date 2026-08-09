# Research: S9 Transactional Email

## Repository reality

**Decision**: Consume the six existing protected outbox contracts and add the rejection/cancellation notification intents already required by BR-122 and S6 FR-032; do not add domain credential or invitation state.

**Rationale**: Registration, recovery, staff invitation, Course invitation, and Admin Approval already co-commit encrypted delivery payloads. Rejection/cancellation are the only source-contract gaps on this exact path. The remaining missing capability is dispatch, templates, production composition, and safe evidence.

**Alternatives considered**: Reconstruct credentials from domain tables was rejected because only digests are stored and outbox ciphertext is the intended delivery boundary. Adding new notification products was rejected as stale/wider scope.

## Durable lifecycle

**Decision**: Add separate `transactional_email_deliveries` and `transactional_email_attempts` tables keyed by immutable outbox event ID.

**Rationale**: `outbox_events` is intentionally append-only. A separate ledger supports mutable claims/retry status without weakening that contract, and PostgreSQL remains authoritative across Redis loss.

**Alternatives considered**: Updating outbox rows would violate immutability. Redis/asynq-only status would lose authority and recovery evidence. Direct sends in HTTP handlers would couple domain success to provider availability.

## Provider boundary

**Decision**: Define a minimal `TransactionalSender` interface using provider-neutral message/result/failure types; implement Resend with `net/http`.

**Rationale**: The official API is a single HTTPS `POST /emails` operation and Go's standard client gives explicit timeout/control without importing provider concepts into application code.

**Alternatives considered**: The Resend Go SDK is unnecessary for one operation and would not change the boundary. SMTP was rejected because the approved provider exposes a direct API and no custom mail infrastructure is needed.

## Resend current behavior (verified 2026-08-09)

**Decision**: Send `from`, one `to`, `subject`, `html`, `text`, optional `reply_to`; require a JSON success body with `id`; set `Authorization: Bearer`, `Content-Type: application/json`, `User-Agent`, and `Idempotency-Key`.

**Rationale**: Official Resend documentation states `POST /emails` accepts HTML and text and returns an email ID. Idempotency keys are supported for 24 hours, are limited to 256 characters, and return the original response for the same request. `concurrent_idempotent_requests` is retryable; a reused key with a different payload is permanent. Rate-limit responses expose `Retry-After`.

**Sources**: Resend official send-email, idempotency-key, error, and usage-limit documentation inspected on 2026-08-09.

## Error classification

**Decision**: Retry transport/timeouts, 408, `concurrent_idempotent_requests`, 429 rate limiting/quota, and 5xx. Treat malformed success as permanent internal/provider-contract failure. Treat other 4xx, authentication, invalid sender/domain/recipient, validation, security, and idempotent-payload conflicts as permanent.

**Rationale**: This avoids indefinite retry of operator/recipient faults while preserving safe retry of provider and network failures.

**Alternatives considered**: Retrying all 4xx would amplify permanent faults; treating all 429 quota errors as permanent would discard recoverable delivery intent.

## Links and locale

**Decision**: Use `PUBLIC_ORIGIN` and URL fragments for raw credentials. Use existing Account locale when present, otherwise validated invitation-request locale with Arabic default.

**Rationale**: Fragments do not reach HTTP access logs or Referer headers. Account locale already governs registration/recovery. Course invitation producers currently hard-code English and must be reconciled.

**Alternatives considered**: Query-string credentials were rejected for new email links because they can reach server/proxy logs. Provider-hosted templates were rejected because repository-owned bilingual content must be deterministic and reviewable.

## Live proof boundary

**Decision**: Always prove adapter compatibility against a deterministic local TLS endpoint. Run one real Resend send only if a safe API key, verified sender/test sender, and Product Owner-controlled recipient are available.

**Rationale**: T047 may still lack the real domain and DNS. Repository implementation must continue without inventing public delivery evidence.
