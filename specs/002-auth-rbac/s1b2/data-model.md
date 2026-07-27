# S1B2 Data Model

S1B2 uses the schema established by migration `0004_sessions`; no new table or column is planned.
All timestamps are server generated and stored in PostgreSQL.

## Account

Existing authentication authority.

Relevant fields:

- `id`: stable account identifier.
- `email_normalized`: lookup key; never emitted in generic login failure.
- `password_hash`: Argon2id verifier.
- `role`: `STUDENT`, `INSTRUCTOR`, or `ADMIN`; selects server expiry policy.
- `status`: only `ACTIVE` can create or use a session.
- `credential_epoch`: copied into a new family and rechecked to invalidate older authority.
- verification state: must satisfy the existing Active/verified login invariant.

## Session family (`sessions`)

One independently revocable authenticated login.

Relevant fields:

- `id`, `account_id`: family identity and owner.
- `admitted_epoch`: credential epoch captured at login.
- `state`: `ACTIVE`, `REVOKED`, or `EXPIRED`.
- `current_generation`: the sole generation permitted to authenticate.
- `authenticated_at`, `reauthenticated_at`, `last_activity_at`.
- `idle_expires_at`, `absolute_expires_at`: computed from the Account role at login/rotation.
- `revoked_at`, `revocation_reason`: includes `LOGOUT` and `REUSE_DETECTED`.

Invariants:

- Only an Active, epoch-current family before both expiry timestamps is usable.
- Absolute expiry never moves during renewal.
- Idle expiry never exceeds absolute expiry.
- Revocation is terminal.

## Session credential generation (`session_credentials`)

Immutable credential and CSRF authority for one family generation.

Relevant fields:

- `session_id`, `generation`: composite identity.
- `credential_digest`: unique SHA-256 digest of the opaque browser cookie.
- `csrf_digest`: SHA-256 digest of the independently keyed pseudorandom CSRF token.
- `state`: `CURRENT` or `SUPERSEDED`.
- `issued_at`, `superseded_at`, `replaced_by_generation`.
- `stale_use_count`, `first_stale_use_at`, `last_stale_use_at`: safe reuse evidence.

Invariants:

- Plaintext credential and CSRF values never enter PostgreSQL.
- A usable generation matches both `sessions.current_generation` and `CURRENT`.
- Supersession is one-way and links to the next generation.
- Exactly one transaction can supersede a current generation.

## Security event

Existing append-only evidence records login, renewal, stale use, reuse revocation, and logout using
allowlisted action/outcome codes. Evidence must not contain passwords, email addresses, plaintext
cookies, plaintext CSRF tokens, or hidden Account-state distinctions.

## Runtime-only values

- `SessionCredential`: 32 random bytes encoded for the cookie; exists only at creation/request
  boundaries and in secret-aware wrappers.
- `CSRFToken`: derived by server HMAC from immutable generation facts with a separate key; returned
  in no-store JSON and held in frontend memory only.
- `AuthenticatedSession`: non-secret account/family/generation/role/expiry facts placed in request
  context.

## State transitions

```text
login:
  no family -> ACTIVE family / generation 1 CURRENT

renewal:
  generation N CURRENT -> generation N SUPERSEDED
                           generation N+1 CURRENT
                           family.current_generation = N+1

ordinary stale request inside classification window:
  generation N SUPERSEDED -> reject SESSION_REPLACED + increment stale evidence

confirmed reuse:
  ACTIVE family -> REVOKED(REUSE_DETECTED)

logout:
  ACTIVE family -> REVOKED(LOGOUT) -> clear browser cookie after commit

idle/absolute/account/epoch failure:
  reject before protected work; persist terminal expiry/revocation where the repository contract
  requires it
```

## Transaction boundaries

- **Login:** verify public failure class, then create family + generation + security evidence in one
  transaction; set the cookie only after commit.
- **Renewal:** lock Account/family/generation, recheck all authority, supersede + insert + advance +
  append evidence, commit, then issue cookie/CSRF.
- **Logout:** lock and recheck family, revoke + append evidence, commit, then send the clearing
  cookie.
- **Protected mutation:** resolve and validate Origin/CSRF, then recheck Account status, epoch,
  family state/expiry, and current generation immediately before domain commit.

## Migration clarification

Migration `0006_authenticated_sessions` expands the closed `identity_security_events.event_type`
constraint for S1B2 session evidence. Migration 0004 already supplies all family/generation storage;
no new session table or column is introduced.
