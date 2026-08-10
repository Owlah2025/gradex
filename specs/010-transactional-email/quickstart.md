# Quickstart: S9 Transactional Email Validation

## Automated provider and template proof

From `backend/`:

```bash
go test ./internal/email ./internal/config ./internal/outbox ./cmd/worker
go test -race ./internal/email
```

Expected: all eight fixed contracts render in `ar` and `en`; the Resend adapter passes local TLS acceptance, idempotency, timeout, malformed response, and error-classification cases; no live credential is required.

## Database integration proof

With the repository integration database available:

```bash
go test -tags=integration ./internal/email ./internal/identity ./internal/access ./internal/httpapi
```

Expected: outbox rows become durable delivery/attempt rows; concurrent claims do not overlap; transient retries and permanent failures persist truthfully; existing identity/access transactions remain committed during delivery failure.

## Frontend proof

From `frontend/`:

```bash
npm ci
npm run lint
npm run typecheck
npm test -- --run
npm run build
```

Expected: staff invitation completion and fragment capture pass in both locales; Course invitation email links retain exact S6 preapproval semantics.

## Acceptance proof

Run the S9-focused extension of the closed S11 journey. Capture messages through the deterministic sender boundary, not direct SQL token extraction.

Verify:

1. registration -> delivered verification link -> success -> replay/wrong/expired refusal;
2. reset request -> delivered reset link -> password changed -> replay/wrong/expired refusal -> completion notice;
3. Course invitation -> delivered link -> acceptance -> zero Entitlement/Enrollment -> Admin Approval -> exactly one of each with `MANUAL_INVITATION` -> grant notice;
4. staff invitation -> delivered link -> preview -> Account creation with assigned immutable role.

## Optional controlled live proof

Run only when `RESEND_API_KEY`, a verified/test sender, and a Product Owner-controlled recipient are supplied safely outside the repository. Send exactly one message and record only timestamp, provider acceptance ID, sender-domain result, recipient class `controlled-test`, and redacted configuration status. If unavailable, record `EXTERNAL_PENDING_DOMAIN_OR_CREDENTIALS`; do not substitute fake evidence.
