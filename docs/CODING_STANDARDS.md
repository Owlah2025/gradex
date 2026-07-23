# Gradex Coding Standards

> Status: Active baseline for system design and implementation
> Last updated: 2026-07-23

These standards cover the repository's current Go backend and Next.js/TypeScript frontend. They do
not choose the future service topology, API shape, data model, or deployment architecture; those
belong to system design.

## Authority and Scope

- Product behavior comes from [PRD.md](PRD.md), [BUSINESS_RULES.md](BUSINESS_RULES.md),
  [DECISIONS.md](DECISIONS.md), and [DOMAIN_MODEL.md](DOMAIN_MODEL.md).
- The [project constitution](../.specify/memory/constitution.md) governs engineering principles.
- Feature-level behavior must be specified before production implementation.
- Existing code is implementation evidence, not authority for contradictory product behavior.

## General Rules

- Prefer the smallest implementation that satisfies approved MVP acceptance criteria.
- Use canonical domain terms exactly: `Course → Section → Lesson`; do not introduce `Chapter` as a
  domain object.
- Enforce authorization and ownership server-side. Hiding an action in the UI is not authorization.
- Make financial, publication, entitlement, refund, coupon-redemption, and suspension transitions
  explicit, validated, idempotent where retried, and auditable.
- Store money as integer fils with an explicit currency. Never use binary floating point for money.
- Store timestamps as UTC instants and localize them only at the presentation boundary.
- Do not log secrets, credentials, tokens, signed URLs, raw payment data, or unnecessary personal
  data.
- No production credentials, private keys, or environment files containing secrets belong in the
  repository. Checked-in examples use unmistakably non-production values.
- Do not leave placeholders, test bypasses, debug authentication, or unsupported claims reachable
  in a production build.

## Go Backend

The current module is `github.com/Owlah2025/gradex/backend` and declares Go `1.26.5` in
`backend/go.mod`.

### Formatting and Structure

- Run `gofmt` on every changed Go file; imports follow standard Go grouping.
- Keep executable wiring under `backend/cmd/` and domain/infrastructure packages under
  `backend/internal/`.
- Package names are short, lowercase nouns. Export only what another package needs.
- Keep HTTP concerns at the transport boundary. Domain/service code must not depend on Gin request
  objects.
- Accept `context.Context` as the first argument for request-scoped I/O and propagate cancellation.
- Put interfaces at the consumer boundary when they provide a real seam; avoid one-implementation
  abstractions without a testing or architectural reason.

### Errors, Validation, and Logging

- Add operation context when returning errors and preserve the cause with `%w` when callers need
  `errors.Is`/`errors.As`.
- Map domain errors to stable HTTP status/error codes at the transport layer; do not expose internal
  error text, SQL, storage keys, or vendor payloads to clients.
- Validate untrusted input at the boundary and enforce invariants again at the authoritative domain
  or database boundary.
- Use structured, secret-safe logging for production paths once the system-design logging standard
  is selected. Temporary `fmt.Printf`/standard-log output is not the final observability contract.
- Fatal process exits belong in executable startup, not reusable packages.

### Persistence and Migrations

- Every schema change uses an ordered forward migration under `backend/internal/db/migrations/`.
  Provide a down migration only when reversal is demonstrably safe and non-destructive. After an
  authority cutover or destructive Contract migration, database recovery is forward-only; do not
  provide a down migration that can re-enable legacy authority or discard accepted state.
- Use constraints and transactions for invariants that must survive concurrency.
- State transitions use guarded updates rather than unvalidated status assignment.
- Do not edit an already-applied production migration; add a new migration.
- Query parameters are bound, never interpolated from user input.

### Backend Verification

From `backend/`, run `gofmt` directly and use the current Makefile targets:

```bash
gofmt -w internal/video/service.go
make build
make test
make test-integration
```

Integration tests require the local dependencies/configuration documented by the backend setup and
must not be assumed to run in an isolated unit-test environment. `make migrate-down` and
`make migrate-force` change database state and require an explicitly selected non-production target.

## Next.js and TypeScript Frontend

The current frontend uses Next.js 14, React 18, TypeScript strict mode, Tailwind CSS, and the Next.js
Core Web Vitals ESLint rules as declared in `frontend/package.json`, `frontend/tsconfig.json`, and
`frontend/.eslintrc.json`.

### TypeScript and Components

- Keep strict TypeScript enabled. Do not suppress errors with broad `any`, `@ts-ignore`, or unsafe
  casts when a real type/narrowing can express the contract.
- App Router route files compose screens; reusable behavior and presentation live in focused
  components/modules.
- Default to Server Components. Add `"use client"` only where browser state, effects, or event
  handling require it.
- Keep API/domain types distinct from localized display models when their responsibilities differ.
- Represent loading, empty, error, denied, pending, and success states explicitly.
- Do not embed authorization assumptions or Admin/Instructors' commercial permissions only in UI
  conditions; the backend remains authoritative.

### Styling, Localization, and Accessibility

- Reuse semantic theme tokens and established UI components. Do not hardcode new component colors
  when a token exists.
- Build Arabic and English together. Arabic is the approved initial default; update document
  `lang`/`dir`, persist the choice, and preserve intentional LTR islands.
- Student functionality must remain complete across phones, tablets/iPads, laptops, and desktops.
- Use semantic HTML first; provide labelled controls, visible focus, complete keyboard operation,
  reduced-motion behavior, and AA contrast within the approved platform-owned boundary.
- Course content stays in its authored language and must not be silently machine-translated.
- Treat all external text/HTML as untrusted; render sanitized content using a reviewed policy.

### Frontend Verification

From `frontend/`, these commands are defined in `package.json`:

```bash
npm run typecheck
npm run lint
npm run build
```

Run the checks proportional to the change. A production build may need network access because the
current application uses Google-hosted fonts through `next/font`.

## Tests

- Test user-visible behavior and domain invariants, not private implementation details.
- Every financial or entitlement state machine needs success, failure, retry/idempotency,
  authorization, and concurrency-sensitive coverage where applicable.
- Each role/ownership boundary includes positive and negative tests.
- External gateway, email, storage, and malware-scanning integrations use contract seams and
  deterministic fakes in unit tests; integration tests verify the real adapter separately.
- Tests must fail if the behavior is removed. Avoid tautological assertions and mocks that merely
  repeat the implementation.
- Production bypasses such as the current fake authentication seam must be impossible to enable in
  a production environment.

## API and Contract Discipline

The final REST/other transport conventions will be selected during system design. Regardless of
transport:

- version and document externally consumed contracts;
- define stable machine-readable error codes and localized client presentation separately;
- require idempotency for retryable payment/refund/webhook operations;
- authenticate webhook origin and make duplicate/out-of-order delivery safe;
- paginate unbounded collections and apply authorization before returning records;
- never rely on client-supplied price, role, ownership, entitlement, or payout values.

## Review Gate

Before merging production work:

- acceptance criteria and affected Business Rules are identified;
- formatting, static checks, and relevant tests pass;
- authorization, privacy, money, idempotency, and audit implications are reviewed;
- documentation and contracts are updated with the behavior;
- no out-of-MVP feature or speculative abstraction was added;
- no secret, placeholder, debug path, or fabricated public content ships.

## Known Baseline Gaps

- The backend currently contains a fake authentication/entitlement seam for development. Real
  authentication and production-safe configuration remain future implementation work governed by
  the approved auth specification.
- A repository `backend/.env` exists in the working tree. It must contain development-only values,
  remain excluded from version control, and be replaced by managed production secrets; verify this
  before any release.
- A repository-wide Go linter/CI policy and the final structured logging/error-envelope conventions
  are not configured yet. Select them during system design and then update this file and automation
  together.
