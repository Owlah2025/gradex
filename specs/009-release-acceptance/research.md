# Research: S11 — Release Acceptance

## R1. Compose existing slice evidence instead of rebuilding it

**Decision**: Select existing S1 identity integration, S4 protected-delivery, S5 protected-learning, S6 access-grant, and S12 HTTPS checks, adding only assertions the release journey currently lacks.

**Rationale**: These paths already exercise production routers, real PostgreSQL, real sessions, protected media, and Progress. Composition keeps S11 a release slice rather than a second implementation of product behavior.

**Alternatives considered**: A new isolated mock suite would not prove deployed behavior. Copying all earlier tests into an S11 directory would create drift and duplicate maintenance.

## R2. Keep one origin contract for local, disposable HTTPS, and T047

**Decision**: Continue using `GRADEX_E2E_EXTERNAL_ORIGIN` as the sole browser base-origin override and the existing explicit database and certificate variables for external runs.

**Rationale**: The current Playwright configuration already rejects unsafe origins and starts no local servers when an external origin is supplied. S12 has proven this contract at `https://gradex.localhost:18443`.

**Alternatives considered**: A T047-specific config file would require source changes at provider availability. A bare command-line base URL would bypass the existing validation and server-ownership guarantees.

## R3. Preserve the production registration safety gate

**Decision**: Prove positive registration, email verification, and password login in the real isolated development harness. Keep registration disabled in the disposable production-mode environment and prove real deployed login through the existing S5/S6 selection.

**Rationale**: Production startup explicitly refuses enabled registration until approved policy and compromised-password adapters are integrated. Bypassing that fail-closed boundary in an acceptance slice would create false launch evidence. The split exposes the missing production capability as a launch finding while still proving the rest of the deployed journey.

**Alternatives considered**: Enabling development adapters in production mode would weaken the shipped safety contract. Skipping positive registration entirely would leave the first critical step unproven.

## R4. Query protected action secrets through the safety-gated test utility

**Decision**: Extend the existing test-only `e2e-seed` binary to retrieve the latest email-verification secret for a known acceptance email, using the same authenticated-decryption path as Invitation delivery evidence.

**Rationale**: A browser acceptance test needs the email-delivered bearer, but no external mail provider exists in the disposable environment. The utility already has guarded database targeting, secret-key configuration, and protected outbox decryption.

**Alternatives considered**: Reading ciphertext directly cannot prove the delivery contract. Logging or returning the secret from a production route would be a security regression. Inserting a verified Account would skip registration.

## R5. Treat replay authentication as part of the assertion

**Decision**: Repeat approval with the real Admin session and matching CSRF token and require the documented `200` idempotent result plus identical grant identity and cardinality.

**Rationale**: The prior browser test accepted `403`, which can pass when replay never reaches grant logic. Authorization failure is not idempotency evidence.

**Alternatives considered**: Allowing `409` would contradict the existing S6 contract. Database counts alone could still pass if the repeated request were rejected before the transaction.

## R6. Prove provenance and Progress from authoritative storage

**Decision**: Extend the test-only learning-state snapshot with `grant_source`, `source_invitation_id`, Entitlement identity, and exact Progress values, then assert them after approval and replay.

**Rationale**: A row count cannot distinguish a valid Invitation grant from an unprovenance-bearing grant, and an HTTP success cannot prove Progress persisted.

**Alternatives considered**: UI text would be indirect evidence. Ad hoc SQL in each Playwright test would duplicate the safety-gated helper.

## R7. Preserve provider and S8 freezes

**Decision**: Do not edit Hostinger/R2/DNS deployment files, migrations, Entitlement updates, support operations, or S8/S12 SpecKit artifacts.

**Rationale**: T047 is externally blocked and S8 requires separate migration-0015 provenance reconciliation. Neither is needed for S11 acceptance against the existing disposable environment.

**Alternatives considered**: Folding provider or Entitlement remediation into S11 would violate the explicit scope and make the review range unsafe to close.
