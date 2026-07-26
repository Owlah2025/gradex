# Contract: S1B1 Public Admission API

**Status**: Frozen for S1B1 implementation
**Base**: `/api/v1` private first-party JSON API
**Rules**: BR-001, BR-002, BR-003, BR-008, BR-105

All responses carry a fresh opaque `X-Request-ID`. Success is `application/json`; errors are RFC
9457 `application/problem+json`. Sensitive and credential-adjacent responses use
`Cache-Control: no-store`. Browser clients send:

```text
Accept: application/json, application/problem+json
Content-Type: application/json
Accept-Language: ar | en
Origin: <exact configured public origin>
X-CSRF-Token: <anonymous browser-memory token>
```

POST bodies are UTF-8 JSON objects with a bounded size, one document, no duplicate or unknown
members, and no trailing data. `Origin` must exactly match the configured public origin; controlled
HTTPS Referer fallback applies only when Origin is absent. Admission order is structure/media,
Origin/CSRF, rate decision, then domain command.

## Anonymous browser security bootstrap

### `GET /api/v1/session/bootstrap`

Safe-method exception that creates or reuses anonymous browser security state. It creates no
Account, credential, delivery intent, or authenticated session.

Response:

```http
HTTP/1.1 200 OK
Content-Type: application/json
Cache-Control: no-store
Set-Cookie: __Host-gradex_anon=<opaque-signed-value>; Secure; HttpOnly; SameSite=Strict; Path=/

{"csrf_token":"<opaque-browser-memory-value>"}
```

The cookie has no `Domain`. JavaScript never reads it. The CSRF token is kept in component/application
memory only and is never written to URL, localStorage, sessionStorage, IndexedDB, a JavaScript
cookie, logs, analytics, or errors.

## Current registration policy set

### `GET /api/v1/registration-policy-set`

Returns the exact required policy set currently available for the requested supported language.
Language negotiation uses `Accept-Language` and returns `Content-Language`.

```json
{
  "id": "registration-2026-08-v1",
  "policies": [
    {
      "kind": "PRIVACY_NOTICE",
      "version": "2026-08-v1",
      "label": "Privacy notice",
      "url": "/legal/privacy"
    },
    {
      "kind": "TERMS_OF_SERVICE",
      "version": "2026-08-v1",
      "label": "Terms of service",
      "url": "/legal/terms"
    }
  ]
}
```

The example values are illustrative contract shape, not approved production content or versions.
The server omits no required item. If no approved/current set is configured for the environment,
return generic `503 REGISTRATION_UNAVAILABLE`; do not invent a default. Production public
registration stays disabled while LG-011 is open.

## Register Student

### `POST /api/v1/student-registrations`

Request:

```json
{
  "display_name": "نورة أحمد",
  "email": "Student.Name@example.com",
  "password": "correct horse battery staple",
  "locale": "ar",
  "policy_set_id": "registration-2026-08-v1"
}
```

Rules:

- `role`, status, session, delivery, and verification fields are not accepted.
- Display name is 2–50 Unicode code points, Arabic/Latin script, non-unique, and contains no URL,
  markup, or control character.
- Email is validated and normalized for comparison; the trimmed submitted address is preserved for
  correspondence.
- Password is not trimmed. It is 15–128 Unicode code points, spaces allowed, and must pass common
  and required compromised-password screening.
- `locale` is `ar` or `en`.
- `policy_set_id` must equal the still-current server-resolved set that the client displayed.

New eligible and existing-normalized-email outcomes share exactly:

```http
HTTP/1.1 202 Accepted
Content-Type: application/json
Cache-Control: no-store

{"code":"REGISTRATION_REQUEST_ACCEPTED"}
```

The response has no `Location`, operation ID, Account ID, role, status, session cookie, delivery
claim, or identifier echo. An existing email is a true no-op: it creates no credential, action
secret, policy acceptance, Identity event, or outbox intent.

Ordinary field errors are `422 VALIDATION_FAILED`. A stale/unknown client policy-set ID is an
ordinary field error. Missing server policy configuration, unavailable required password screening,
or unsafe credential policy evaluation fails all admitted registration attempts with generic
`503 REGISTRATION_UNAVAILABLE`. Unsafe durable delivery admission fails all admitted attempts before
Account lookup with `503 TRANSACTIONAL_DELIVERY_UNAVAILABLE`.

## Request or resend verification

### `POST /api/v1/email-verification-requests`

Request:

```json
{"email":"Student.Name@example.com"}
```

Eligible pending Student, Active/ineligible Account, and unknown-email outcomes share exactly:

```http
HTTP/1.1 202 Accepted
Content-Type: application/json
Cache-Control: no-store

{"code":"VERIFICATION_REQUEST_ACCEPTED"}
```

There is no `Location`, delivery/provider state, identifier echo, or cookie difference. An eligible
request supersedes its prior live secret and co-commits one replacement action-secret digest,
Identity event, protected delivery payload, and outbox event. Unknown/ineligible requests mutate
nothing. Durable-delivery admission is checked before Account lookup so an unsafe boundary returns
the same `503 TRANSACTIONAL_DELIVERY_UNAVAILABLE` regardless of Account existence.

## Consume verification

### `POST /api/v1/email-verifications`

Request:

```json
{"token":"<URL-safe opaque bearer>"}
```

The token is accepted only in the JSON body. It is never accepted from a request path, query,
header, cookie, or persisted browser state.

Successful first consumption:

```http
HTTP/1.1 200 OK
Content-Type: application/json
Cache-Control: no-store

{"status":"VERIFIED"}
```

No session or authenticated cookie is issued. The frontend may offer navigation to `/login`, which
becomes functional in S1B2.

Malformed encoding, unknown digest, wrong purpose, elapsed expiry, prior consumption, revocation,
supersession, or conflicting Account state all share:

```json
{
  "type": "https://api.gradex.com/problems/token-invalid",
  "title": "Link unavailable",
  "status": 400,
  "detail": "This verification link cannot be used.",
  "instance": "urn:gradex:problem:<request-id>",
  "code": "TOKEN_INVALID",
  "request_id": "<request-id>"
}
```

A repeated call after success is `TOKEN_INVALID`, not an idempotent replay of the success.

## Common visible failures

These failures concern request/admission state rather than hidden Account state and remain visible:

| Status/code | Meaning |
|---|---|
| `400 MALFORMED_JSON` | Structurally ambiguous/unparseable JSON, including duplicate members or trailing documents. |
| `403 CSRF_VALIDATION_FAILED` | Exact Origin/Referer or anonymous CSRF validation failed. |
| `406 NOT_ACCEPTABLE` | Requested response representation is unsupported. |
| `413 CONTENT_TOO_LARGE` | Body exceeds the endpoint limit. |
| `415 UNSUPPORTED_MEDIA_TYPE` | Request is not supported UTF-8 JSON. |
| `422 VALIDATION_FAILED` | Known fields fail semantic validation; field violations never echo rejected values. |
| `429 RATE_LIMITED` | A configured policy was evaluated and quota was exceeded; safe `Retry-After` only. |
| `503 RATE_LIMITING_UNAVAILABLE` | Redis and bounded strict fallback could not make a safe decision. |
| `503 REGISTRATION_UNAVAILABLE` | Current policy or required credential-screening boundary is unavailable. |
| `503 TRANSACTIONAL_DELIVERY_UNAVAILABLE` | Durable outbox/protected-payload admission is unsafe before Account lookup. |

Every `429`/`503` is `no-store`. Error detail never names Redis, PostgreSQL, the screening source,
email provider, limiter dimension, policy internals, Account state, queue/backlog, or delivery state.

## Anti-enumeration equivalence

For each uniform-result endpoint, hidden outcomes have the same:

- status and body bytes;
- meaningful headers and response-size class;
- anonymous cookie behavior;
- frontend navigation and copy;
- timing class under the test tolerance;
- externally observable delivery behavior.

Routine logs contain only request ID, method, route template, status, duration, response size, safe
problem code, and typed internal limiter outcome. They never contain email, identifier HMAC, password
or hash, token or digest, query string, cookie, CSRF value, body, or provider payload.

## Frontend deep-link handling

The future email template targets:

```text
/verify-email/result#token=<opaque-bearer>
```

The fragment is not sent in the HTTP request. The result client copies it into component memory,
immediately removes it with `history.replaceState`, and POSTs it in the JSON body. Analytics are
disabled on that route. Missing fragments and all `TOKEN_INVALID` results render the same safe
invalid-link state.
