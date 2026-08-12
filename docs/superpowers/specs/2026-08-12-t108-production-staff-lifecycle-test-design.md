# T108 production staff lifecycle test design

## Scope

T108 adds one deterministic integration journey. It does not alter the staff
lifecycle, session, email, or authorization implementation.

## Test boundary

The test builds the real API router from `APP_ENV=production` configuration,
the existing PostgreSQL schema, Redis connection, session foundation, HIBP
adapter boundary, and Resend-configured transactional-email settings. It uses
HTTP routes and server-managed session cookies; fake authentication and fake
email are not enabled.

## Journey

An authenticated Admin creates an Instructor invitation. The test asserts the
co-committed encrypted outbox intent, derives the action credential only from
the existing test-safe encrypted outbox seam, previews and completes the
invitation, and verifies the resulting immutable Instructor account.

The Instructor logs in and reaches a real Instructor endpoint. The Admin reads
the narrow Instructor status list, suspends the account, and the already-open
Instructor session is denied. Reinstatement does not revive that old session;
the Instructor logs in again and reaches the permitted endpoint.

## Negative evidence

The journey asserts anonymous, Student, and Instructor denials on staff reads
and mutations; invalid and consumed action credentials fail closed; and the
Admin invitation response does not contain an action bearer. Focused
composition tests retain the named fail-closed prerequisite coverage.

## Determinism and confidentiality

The repository integration schema is recreated per run. The test makes no
network email delivery call, never logs an action credential, and uses the
existing encrypted outbox test seam only after proving the durable intent.
