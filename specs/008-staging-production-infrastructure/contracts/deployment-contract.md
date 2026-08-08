# S12 Deployment Contract

## Immutable processes

| Process | Artifact/command | Public ingress | Required dependencies |
|---|---|---|---|
| Frontend | Standalone Next.js artifact | HTTPS edge only | Explicit server-side API origin |
| API | Shared backend image, API command | Edge/API route | PostgreSQL, compatible schema, role-required Redis, private storage |
| Worker | Shared backend image, worker command | None | PostgreSQL, Redis, private storage, FFmpeg/FFprobe, scanner boundary |
| Migration | Shared backend image, migrate command | None; one-off release job | PostgreSQL |

## Probe contract

- `GET /healthz`: process liveness only; `200` when live, `503` before lifecycle start/after stop.
- `GET /readyz`: dependency-aware readiness; `200` only when required checks pass, otherwise `503`.
- Probe responses contain closed status values, not connection strings or dependency error text.
- The worker has no public HTTP ingress. Process exit, structured startup/shutdown events, and queue
  health/age monitoring form its lifecycle contract.

## Edge contract

- Public traffic is HTTPS; HTTP redirects to HTTPS where the provider exposes HTTP.
- Browser frontend and API paths share the canonical public origin.
- The edge replaces or sanitizes forwarding headers and connects from configured trusted proxy CIDRs.
- Private processes and administrative storage endpoints are not publicly routed.
- Session cookies remain Secure, HttpOnly, host-only, path `/`, and SameSite Strict.

## Configuration contract

The authoritative key names remain `backend/.env.example` and the frontend server-side
`GRADEX_API_ORIGIN`. Deployment examples contain no secret values. Real values enter through the
platform's secret facility at runtime. Production rejects fake authentication, non-HTTPS public/CORS
origins, wildcard production CORS, missing secrets, and placeholder playback signing material.

## Migration/recovery contract

- Migrate from zero to `db.MaxSchemaVersion` using a controlled one-off job before application rollout.
- Re-running `migrate up` is safe; production down migrations are refused.
- Backup reads the source database and records checksum/schema evidence.
- Restore creates and targets a fresh separate database, refuses the active source identity, and does
  not use `--clean --if-exists` as the proof path.
- Application rollback selects earlier artifacts and retains forward schema 15. It never runs migration
  0015 down after real S6 grants.

## Evidence contract

No checkbox closes from configuration text alone. Evidence records exact revision/artifact identity,
commands, UTC time, exit status, probe output, schema version, and non-secret assertions. Cloud staging,
public TLS, and external alert delivery remain explicitly pending until actually executed.
