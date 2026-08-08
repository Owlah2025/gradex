# Contract: S11 Release Acceptance Suite

## Invocation classes

### Local isolated run

No external origin is set. The existing Playwright setup allocates run-owned ports, creates a safety-named isolated database, starts the real API and frontend, and tears down only owned resources.

### Disposable production-like HTTPS run

The S11 verifier reuses the existing S12 topology and safety-gated database tunnel. It opts into S11 mode and registration for the isolated acceptance database; default S12 mode remains unchanged.

### Future T047 public staging run

The same Playwright selection is supplied a public credential-free HTTPS origin, certificate trust appropriate to that host, and explicit safety-gated database query settings. Test source is unchanged.

## Configuration

| Variable | Required when | Contract |
|---|---|---|
| `GRADEX_E2E_EXTERNAL_ORIGIN` | External run | Credential-free `https://` origin with no path, query, or fragment |
| `GRADEX_E2E_TLS_SPKI` | Disposable internal-CA Chromium run | Exact leaf certificate SPKI pin; omitted for publicly trusted T047 |
| `NODE_EXTRA_CA_CERTS` | Disposable internal-CA request client | Existing S12 CA file; omitted for publicly trusted T047 |
| `GRADEX_E2E_TMP_DIR` | External run | Run-owned directory containing only acceptance state and binaries |
| `GRADEX_E2E_ADMIN_DB_URL` | External database evidence | Administrative connection for the safety-gated test database only |
| `GRADEX_E2E_TARGET_DB_NAME` | External database evidence | Must begin `gradex_playwright_e2e_` |
| `GRADEX_E2E_TARGET_DB_URL` | External database evidence | Connection to that exact target database |
| `GRADEX_E2E_APPLICATION_DB_URL` | External helper configuration | Application database connection used only to load configuration; never reset |
| `GRADEX_STAGING_SMOKE_MODE` | Disposable wrapper | `s12` by default; `s11` selects release acceptance |
| `GRADEX_STAGING_REGISTRATION_ENABLED` | Disposable S11 wrapper | `true` only for the isolated S11 acceptance run; defaults to `false` |

## Fail-closed rules

- Unsafe external origins fail before browser startup.
- Missing run state, seed utility, CA/SPKI requirement, or database setting fails the run.
- S11 mode requires positive registration; it does not silently skip the first critical step.
- Replay must return the idempotent success contract through an authorized request.
- A missing database row is failure, never a zero-valued default.
- Active application databases are never reset, dropped, or migrated down.
- Evidence must not contain cookies, CSRF values, action secrets, database passwords, protected object URLs, or provider credentials.
