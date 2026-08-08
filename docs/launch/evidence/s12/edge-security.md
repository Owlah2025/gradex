# S12 Batch D evidence — HTTPS and browser security

Date: 2026-08-08

Batch base: `55abcf587c1f2ef45df72fbea95989f17c6625c3`

This proof uses the loopback-only production-like topology and its disposable internal certificate
authority. It proves the application and reverse-proxy security contract; it is not evidence of a
public staging hostname or publicly trusted certificate.

## Repeatable edge proof

`./deploy/scripts/verify-edge-security.sh` completed successfully against the running production-mode
frontend, API, worker, and Caddy edge. It performed these checks without `--insecure`:

- HTTP returned 308 to the exact canonical HTTPS URL.
- The TLS chain verified against the extracted environment CA and the leaf matched
  `gradex.localhost`.
- `/healthz` and `/readyz` succeeded through TLS.
- `/api/v1/session/bootstrap` set `__Host-gradex_anon` with `Path=/`, `HttpOnly`, `Secure`, and
  `SameSite=Strict`, without a `Domain` attribute.
- A correctly bound token from `https://attacker.example` and an invalid token from the trusted
  origin each returned 403 `CSRF_VALIDATION_FAILED`.
- A correctly bound trusted-origin request passed browser security and reached credential validation,
  returning the expected 401 `AUTHENTICATION_FAILED` for the fixed nonexistent identity.
- A foreign CORS preflight returned 405 with neither `Access-Control-Allow-Origin` nor
  `Access-Control-Allow-Credentials`. This is the intended same-origin BFF contract: the public
  browser surface grants no cross-origin API access.
- A client-supplied `X-Request-ID: attacker-controlled` was replaced with a fresh trusted response
  identifier.
- API startup logs contained `fake_auth=false` and did not contain `fake_auth=true`.
- None of the generated PostgreSQL, restore, object-storage, playback, session, admission, or outbox
  secrets appeared in API, worker, frontend, or edge logs or the frontend static bundle.

The script stores its cookie jar and response material only in a mode-0700 temporary directory under
ignored `deploy/.state/`, gives each file mode 0600, and removes the directory on exit.

## Certificate and image identity

The certificate inspected during this drill reported:

```text
issuer=CN=Caddy Local Authority - ECC Intermediate
serial=C2DD2C82E73AD2A51855C439C566162B
notBefore=Aug  8 13:46:52 2026 GMT
notAfter=Aug  9 01:46:52 2026 GMT
X509v3 Subject Alternative Name: critical
    DNS:gradex.localhost
```

The running proof used these locally built immutable image identities:

```text
gradex-backend:s12-local  sha256:a4af0e00e3cdd2cfbc159db069a1e4aabdc9df439903957ee95d736c8c650092
gradex-frontend:s12-local sha256:3c664ffa481edb2e0d05e227548f4667807264532d235e16ab60c441684a04c3
gradex-edge:s12-local     sha256:901a198f1000e20e87ef563b08f1737ed1b277077882e19597de1bad319baf9b
```

## Focused automated coverage

The backend trusted-proxy, production configuration, hardened-cookie, exact-origin/CSRF, trusted
request-ID, and sensitive-log tests passed:

```text
go test ./internal/config ./internal/httpapi -run <focused-security-expression> -count=1
ok github.com/Owlah2025/gradex/backend/internal/config
ok github.com/Owlah2025/gradex/backend/internal/httpapi
```

The full frontend unit command passed all 161 tests, including the Batch A production-origin test
that requires explicit `GRADEX_API_ORIGIN` and verifies the resulting server-only URL.

## Remaining boundary

- A publicly trusted certificate, DNS, and external staging edge still require provider/domain access.
- The known High production dependency advisories still block public exposure until remediated or
  explicitly mitigated.
- No cross-origin browser API consumer is part of the MVP. If one is authorized later, it requires an
  explicit CORS implementation and security review rather than relying on the currently validated but
  non-granting allowlist configuration.
