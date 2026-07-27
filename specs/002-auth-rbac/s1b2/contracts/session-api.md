# S1B2 Session API Contract

All responses use `Cache-Control: no-store`. Successful session representations use
`Content-Type: application/json`; errors use the repository's Problem Details content type and
safe request identifier. Dates are RFC 3339 UTC strings.

## Shared browser credential

Successful login and renewal set:

```http
Set-Cookie: __Host-gradex_session=<opaque>; Path=/; Secure; HttpOnly; SameSite=Strict
```

The cookie has no `Domain` attribute. The server is authoritative for idle and absolute expiry.
Logout clears the same cookie only after server-side revocation commits. No response exposes the
credential value in JSON or another header.

## Shared authenticated representation

```json
{
  "status": "AUTHENTICATED",
  "role": "STUDENT",
  "display_name": "Safe display name",
  "csrf_token": "current-memory-only-token",
  "idle_expires_at": "2026-08-07T12:00:00Z",
  "absolute_expires_at": "2026-08-30T12:00:00Z"
}
```

`role` is one of `STUDENT`, `INSTRUCTOR`, or `ADMIN`. The CSRF token is a secret response value:
clients hold it in memory only and send it as `X-CSRF-Token` on state-changing authenticated
requests. It must never be logged, persisted, placed in a URL, or retained in an error.

## POST `/api/v1/sessions`

Creates a new authenticated family after S1B1 anonymous admission.

Request:

```json
{
  "email": "student@example.com",
  "password": "user supplied password"
}
```

Success: `201 Created`, shared representation, authenticated cookie, and expiration of the
anonymous admission cookie.

Public failure for unknown email, wrong password, unverified Account, or inactive Account:

```json
{
  "type": "https://api.gradex.com/problems/authentication-failed",
  "title": "Authentication failed",
  "status": 401,
  "code": "AUTHENTICATION_FAILED",
  "detail": "The email or password is incorrect."
}
```

Those hidden states have the same status, public headers, padded body class, cookie behavior, and
production-comparable verification path. Validation failures that can be used to distinguish
Account existence collapse to the same public authentication failure once the body is syntactically
admissible.

Other failures use the shared strict-admission boundary:
`400 MALFORMED_JSON`, `413 CONTENT_TOO_LARGE`, `415 UNSUPPORTED_MEDIA_TYPE`,
`403 CSRF_VALIDATION_FAILED`, `429 RATE_LIMITED`, or fail-closed
`503 AUTHENTICATION_UNAVAILABLE`. Login never creates a partial family on failure.

## GET `/api/v1/session`

Resolves the current cookie and rehydrates the in-memory CSRF token.

Success: `200 OK` with the shared representation. This read does not rotate credential/CSRF values
and does not extend idle expiry.

Failures:

- `401 AUTHENTICATION_REQUIRED`: missing, invalid, revoked, expired, inactive, or epoch-stale
  authority.
- `401 SESSION_REPLACED`: a first immediate non-sensitive use of a superseded credential.
- `401 SESSION_REUSE_DETECTED`: confirmed stale credential reuse; the family is revoked.
- `429 RATE_LIMITED` or fail-closed `503 AUTHENTICATION_UNAVAILABLE`.

No failure reveals whether the supplied opaque value ever existed.

## POST `/api/v1/session-renewals`

Requires the current cookie, a trusted Origin/Referer, and matching `X-CSRF-Token`. Atomically
rotates both values.

Request body: absent or empty.

Success: `200 OK` with the shared representation and replacement cookie.

Failures:

- `403 CSRF_FAILED` or `403 ORIGIN_NOT_ALLOWED` before protected work.
- `401 AUTHENTICATION_REQUIRED` for unusable current authority.
- `401 SESSION_REUSE_DETECTED` for any superseded credential presented to renewal; the family is
  revoked and no replacement credential is issued.
- `429 RATE_LIMITED` or fail-closed `503 AUTHENTICATION_UNAVAILABLE`.

When concurrent requests present one current generation, exactly one can return success. The loser
returns a safe authentication/reuse problem and performs no protected work.

## DELETE `/api/v1/session`

Requires the current cookie, trusted Origin/Referer, and matching `X-CSRF-Token`.

Success: revoke the family with reason `LOGOUT`, append evidence, commit, clear the authenticated
cookie, clear the frontend memory token, and return `204 No Content`.

Failures before a revocation commit use the same CSRF, origin, rate, and authentication classes as
renewal. A missing/invalid cookie may return `204` only if the handler can safely clear local
browser state without claiming that an identified server family was revoked. A database failure
must not clear a still-usable browser credential.

## Required headers and caching

- State-changing cookie-authenticated calls: `Origin` (or same-origin Referer fallback) and
  `X-CSRF-Token`.
- Problems may include `Retry-After` when rate limited.
- Authentication failures may include the stable repository `WWW-Authenticate` session challenge.
- Every success and failure from these routes: `Cache-Control: no-store`.
- No route reflects credential, CSRF, password, raw email, or hidden Account state.
