# Implementation Plan: S9 — Transactional Email Delivery

**Branch**: `s9-transactional-email-20260809` | **Date**: 2026-08-09 | **Spec**: [spec.md](spec.md)

## Summary

Add a narrow transactional-message boundary and a Resend HTTPS adapter, then consume the existing encrypted PostgreSQL outbox from the current worker process through a separate durable delivery ledger. Render eight fixed account/access contracts in Arabic and English, add the two BR-122/S6 terminal invitation intents missing from current producers, preserve all existing credentials and domain state, and extend focused acceptance so links come from captured deliveries rather than database helpers.

## Technical Context

**Language/Version**: Go 1.26.5; TypeScript/Next.js 16 frontend
**Primary Dependencies**: `net/http`, pgx v5, existing asynq worker process, React/Next.js; no Resend SDK required
**Storage**: PostgreSQL authoritative outbox plus new delivery/attempt tables; Redis remains media queue infrastructure only
**Testing**: Go unit/integration/race tests, `httptest` TLS provider compatibility tests, Vitest, Playwright/S11 reuse
**Target Platform**: Linux API/worker and responsive bilingual web frontend
**Project Type**: Modular-monolith web application with API and worker processes
**Performance Goals**: Bounded batches of 25; one provider request per claim; provider timeout configurable from 1–30 seconds
**Constraints**: No direct HTTP-request send, no token/body logging, no production fake, five total attempts, no new domain credential/state
**Scale/Scope**: Eight fixed contracts, two locales, single approved provider, launch-scale transactional volume

## Constitution Check

| Principle | Design evidence | Gate |
|---|---|---|
| I — Source authority | Eight contracts reconciled to PRD/BRs/S6; commerce and marketing excluded | PASS |
| II — Deny by default | Production rejects fake/disabled/malformed delivery configuration | PASS |
| III — BR traceability | Spec/tasks cite BR-001/008/009/029/120/121/122/167/168/171 | PASS |
| IV — Access correctness | Email acceptance changes no Course access; Admin Approval remains sole grant | PASS |
| V — Risk testing | Deterministic TLS, integration, retry, privacy, and S11/S6 acceptance proofs | PASS |
| VI — Modular simplicity | One narrow interface, one provider adapter, eight templates; no generic communications platform | PASS |
| VII — Data integrity | Immutable outbox plus transactional delivery ledger and row claims | PASS |
| VIII — Quality gate | Full required Go/frontend/security validation in tasks | PASS |
| IX — Operational discipline | Safe attempt ledger and structured lifecycle signals | PASS |
| X — Bilingual accessibility | Arabic/English text+HTML, RTL, existing responsive routes | PASS |
| XI — Documentation sync | D-077, launch status, SpecKit, config/runbook evidence updated | PASS |

Post-design re-check: PASS. No justified constitution violation exists.

## Architecture

```text
domain transaction
  -> immutable outbox_events + encrypted outbox_protected_payloads
  -> worker dispatcher claims/creates transactional_email_deliveries in PostgreSQL
  -> decrypt + validate known contract
  -> render ephemeral bilingual text/html and canonical fragment link
  -> TransactionalSender.Send(message, stable delivery key)
  -> Resend HTTPS adapter OR deterministic fake
  -> transactional_email_attempts + delivery terminal/retry state
```

The dispatcher runs beside the existing asynq media dispatcher. Email scheduling and attempt truth never move to Redis. A worker crash leaves a stale lease that can be reclaimed; an ambiguous provider call reuses `gradex/<outbox-event-uuid>` as the Resend idempotency key.

## Project Structure

```text
backend/
├── cmd/worker/main.go
├── internal/config/config.go
├── internal/email/
│   ├── message.go
│   ├── renderer.go
│   ├── resend.go
│   ├── fake.go
│   ├── repository.go
│   └── dispatcher.go
├── internal/logging/logging.go
├── internal/outbox/protected_payload.go
└── internal/db/migrations/0016_transactional_email.*.sql
frontend/
├── src/app/staff/accept/page.tsx
├── src/components/staff/staff-invitation-acceptance.tsx
├── src/lib/api/identity.ts
└── src/lib/identity/validation.ts
specs/010-transactional-email/
docs/launch/evidence/s9/
```

**Structure Decision**: Keep provider and dispatch infrastructure in `backend/internal/email`; identity/access producers retain provider-neutral outbox contracts. Add only the two missing S6 terminal intents, the missing staff acceptance frontend, and fragment-safe Course invitation capture.

## Delivery and Failure Policy

- `QUEUED -> SENDING -> ACCEPTED` on provider acceptance.
- Transient failure returns to `QUEUED` with persisted `next_attempt_at` while attempts remain.
- Permanent/configuration/render/decrypt failure becomes `PERMANENT_FAILED`.
- Fifth transient failure becomes `EXHAUSTED`.
- Expired leases are reclaimable. Each attempt row is immutable after completion apart from its bounded result fields.
- Logs contain delivery/event IDs, contract, attempt number, state, provider name, provider code/class, and timing only.

## Complexity Tracking

No constitution violation requires justification. The separate ledger is the smallest design that preserves the existing append-only outbox while adding mutable delivery lifecycle evidence.
