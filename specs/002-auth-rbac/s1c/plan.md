# Implementation Plan: S1C — Staff lifecycle, enforcement, and authorization matrix

**Branch**: `feature/002-authentication-rbac` | **Date**: 2026-07-27 | **Spec**: [spec.md](spec.md)
**Status**: **FROZEN.** Antigravity implements this as written. Contract and architecture changes are
not Antigravity's to make — stop and report instead.

## Summary

Extend the existing Go modular monolith with the staff half of Identity. Everything here reuses a
primitive that already exists: `identity_action_secrets` for the invitation bearer, `identity.Authorize`
for the capability decision, `sessionRecord.usable` for live account-status enforcement,
`session_epoch` for race closure, the layered Redis limiter for abuse control, the outbox for
delivery intent, and S1B3's fragment-bearer pattern for the screens. **No new security mechanism is
introduced.** The novel work is one migration, two domain operations, seven routes, five screens,
and the evidence that proves them.

## Technical Context

**Language/Version**: Go 1.26.5; TypeScript 5.5; React 18.3; Next.js 14.2
**Primary Dependencies**: Gin 1.12, pgx/v5, go-redis/v9, `golang.org/x/crypto/argon2`, Next.js App
Router, Tailwind CSS
**Storage**: PostgreSQL 16 authoritative for Identity, evidence, and outbox intent; Redis 7 for
disposable limiter state; browser persists locale only
**Testing**: Go unit and `-race` tests, PostgreSQL integration tests behind the `integration` build
tag, HTTP security integration tests, frontend ESLint/TypeScript/clean production build
**Schema**: currently 7; this slice raises it to 8. CI derives the expected version from
`db.MaxSchemaVersion`

## What already exists — do not rebuild

Verified by reading the code, not inferred:

| Exists | Location | Consequence for S1C |
|---|---|---|
| Deny-by-default decision point over a closed nine-capability set, with suspension outranking everything and `PASSWORD_CHANGE_REQUIRED` overriding role | `backend/internal/identity/policy.go` | Gate the new routes through `identity.Authorize`. **Never** a handler-local role string |
| Session resolution re-reads `accounts.status` live and refuses any non-`ACTIVE` account | `backend/internal/identity/session_repository.go` (`sessionRecord.usable`) | Immediate suspension enforcement is already present at the **read** path. S1C owes the suspension **operation**, the revocation write, and the proof |
| `RevokedByAccountSuspended` revocation reason | `backend/internal/identity/session.go` | Reuse it |
| Purpose-bound, digest-only, expiring, supersedable, single-use action secrets under a row lock | `backend/internal/identity/action_secret.go` | Add a purpose. Do not build a second token type |
| `DenyReason` values stay in security monitoring and never reach the response; the response is a uniform refusal | `policy.go` | Recent-auth refusal reuses this. It does **not** depend on the denial-vocabulary reconciliation |
| Fragment-carried, purpose-namespaced, monotonic bearer capture released only on terminal outcome | S1B3 recovery screens | Copy the pattern for the invitation screens |

Currently mounted protected surface: the legacy Instructor video group, the Student lesson group, and
the session-lifecycle routes. The matrix covers **those plus what this slice adds** — not a
hypothetical surface.

## Constitution check

| Principle | Satisfied by |
|---|---|
| I — source documents authoritative | Spec cites FR-005/010/011/012/014/015/016 and SLICES.md §5.4; no silent contradiction |
| II — deny by default, enforce in the backend | Every new route gated by `identity.Authorize`; recent-auth enforced at the policy boundary, verified by direct API call; suspension audited |
| III — business-rule traceability | Every acceptance item in spec §15 traces to a listed FR |

No deviation requested.

## Task breakdown

Ordered. Each task's stop condition is its acceptance gate; do not proceed past a failing one.

### T1 — Migration `0008_staff_lifecycle` (est. 45 min)

`staff_invitations` per spec §5, the `STAFF_INVITATION` action-secret purpose, and the
`ACCOUNT_SUSPENDED` / `ACCOUNT_REINSTATED` security-event allowlist entries. Partial unique index for
at most one `PENDING` invitation per normalized email.

**Stop**: `up` → `down` → `up` clean against real PostgreSQL; every constraint refuses what it claims
to refuse, including a second `PENDING` invitation for the same normalized email; CI's derived schema
assertion tracks to 8 with no manual edit.

### T2 — Suspension and reinstatement (est. 90 min)

One transaction: set `SUSPENDED`, revoke every family with `ACCOUNT_SUSPENDED`, advance
`session_epoch`, write evidence. Reinstatement is a separate audited operation that does **not**
restore revoked sessions. Both require `SECURITY_OPERATIONS` and a fresh Admin recent-auth window.

Build **before** invitations: invariant I9 depends on it.

**Stop**: proofs 4a, 4b, 4c each pass and each **fails under its own mutation** (spec §14), against
real PostgreSQL. Suspension is idempotent. A stale-recent-auth Admin cannot suspend, refused at the
policy boundary by direct API call.

### T3 — Invitation domain (est. 100 min)

Create, supersede, revoke, complete. The invitation row is authoritative for the invited role; the
bearer proves issuance, not authority. The role ceiling is checked from the **stored invitation** and
the **inviter's current authority** at completion time — never from the token or the request body.
Completion runs compromised-password screening (fails closed), creates the credential, consumes the
invitation, and writes evidence, all atomically. Completion issues **no** session.

**Stop**: all nine invariants asserted; I7 under real PostgreSQL contention with exactly one winner
and no 500 for the loser; the plaintext initial password never reaches storage, logs, argv, or
telemetry.

### T4 — HTTP boundary (est. 70 min)

The seven routes of spec §7. Capability gating through `identity.Authorize`. Recent-auth enforced at
the backend policy boundary on invitation creation, invitation revocation, suspension, and
reinstatement. Layered rate limits on creation and completion. Non-enumerating preview: it returns
the invited role and display state and **never** the email.

**Stop**: a non-Admin is refused by capability, not by a role string; a stale-recent-auth Admin is
refused by direct API call returning the existing uniform refusal; the preview leaks no
account-existence signal.

### T5 — Authorization matrix and bootstrap test 3 rerun (est. 105 min)

The route inventory is **derived from the mounted router** at test time and classified per spec §8.
Direct API calls only; no redirect grants anything.

**Stop**: the proof fails in all six cases of spec §8 — verified by deliberately introducing each
condition, including mounting an unlisted protected route and listing an unmounted one. Bootstrap
test 3 denies the restricted bootstrap Admin across the whole surface, and this run is recorded as
the **final full-surface proof**.

### T6 — Bilingual screens (est. 70 min)

The five screens of spec §9, in Arabic and English.

**Stop**: RTL/LTR, phone/desktop, keyboard, expired-secret and reused-secret paths pass; no bearer in
DOM, storage, or address bar **after hydration**; production build passes with `.next` removed first.

### T7 — Verification and evidence package (est. 60 min)

Full local gates with real PostgreSQL at schema 8, Redis, and MinIO: `gofmt`, `go build ./...`,
`go vet ./...`, `go vet -tags=integration ./...`, `go test -race ./...`, the full integration suite
under race. Frontend typecheck, lint, `node:test`, clean production build. `scripts/docs-guard.sh`
and `scripts/expose-guard.sh`. Push; verify hosted CI green on the exact head.

**Stop**: the evidence package of spec §15 and the handoff prompt is complete, with a frozen commit
range.

## Deferred from this slice

- **A4** — observed evidence that *voluntary* password change revokes another family. Different code
  path from the proven recovery path. Cut twice already; scheduled explicitly into S2's review scope
  rather than left in a queue.
- **A5** — reconciling the S1B-wide policy-denial vocabulary against API design §6.1. Internal
  monitoring naming only; no acceptance item depends on it. Post-launch.

Both are recorded here rather than dropped.

## Risk

The matrix in T5 is the item most likely to pass vacuously — a hand-copied inventory reports coverage
it does not have. Cases 1 and 2 of spec §8 exist specifically to detect that, and they are the two
that must be negative-tested most carefully. If T5 cannot be made to fail on a deliberately unlisted
route, T5 is not done.
