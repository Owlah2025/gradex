# S12 Batch E evidence — queue recovery and protected media

Date: 2026-08-08

Batch base: `db64dca`

This is a disposable production-mode proof using the existing S5 fixture in a safety-gated separate
database. It does not modify the active Gradex database and does not claim a cloud storage endpoint
or browser CORS policy is live.

## Isolation and production boundary

`./deploy/scripts/verify-worker-media.sh` recreates only
`gradex_playwright_e2e_s12media01`, which matches the repository's E2E database allowlist and is
different from the application database. The first attempted name contained a disallowed underscore;
the existing fail-closed safety gate refused it before seeding or application data changes. The
corrected proof database migrated from zero to schema 15.

The proof uses separate `api-media-proof`, `worker-media-proof`, and `media-proof-redis` processes,
the production backend image, and the same private MinIO bucket. The existing S5 seed utility is
available only in the explicitly selected `proof` image target. Runtime inspection proved
`gradex-e2e-seed` absent from `gradex-backend:s12-local` and present in
`gradex-backend-proof:s12-local`.

Final image identities were:

```text
gradex-backend:s12-local       sha256:ac9f2f18122c7c87f609a8f14b1d7f81ecb169f97ef81b8b19b47ef96115cfee
gradex-backend-proof:s12-local sha256:fc0ceea827078da4ddcdb81d82a7ca395866b1774bd14c0104a1b8371d64f36f
gradex-frontend:s12-local      sha256:643716e94da67462f255c42b8f2a97243814289ab32fac31e036385a39def5b7
```

## Redis outage and durable recovery

The harness created a versioned private MP4 source object, recorded exact-version scan evidence, and
committed a `media.transcode_requested` outbox row in PostgreSQL. It then stopped the proof Redis
before dispatch:

- proof API `/readyz` returned HTTP 503;
- the worker remained running and emitted a structured `worker_failure` with operation
  `media_outbox_dispatch`; the durable event under test was
  `90000000-0000-0000-0000-00000000e004`;
- `media_outbox_dispatches` remained zero for that event, while the authoritative outbox row and
  `SCAN_PASSED` Asset Version remained in PostgreSQL.

After Redis restart and an explicit worker restart, the dispatcher recovered the same committed
event and the real FFmpeg worker produced HLS. A second committed event for the already-READY Asset
Version was consumed without another processing attempt. The final database assertion was:

```text
schema version | dirty | asset state | dispatch receipts | processing attempts | renditions
15             | false | READY       | 2                 | 1                   | 1
```

The proof API then returned ready with PostgreSQL, schema, and Redis all `ok`. This demonstrates that
Redis contains disposable delivery state while PostgreSQL retains the intent and media lifecycle
authority.

## Private processing and playback

The source object and generated `240p` playlist were each refused by direct unsigned MinIO reads.
The worker downloaded the exact source object version, derived a real non-empty HLS segment, and
wrote the playlist/segment below the media-owned private prefix.

The first protected playback attempt exposed a real production compatibility defect: presigning the
private rendition playlist did not sign its relative segment, and MinIO correctly denied that child
request. The narrow remediation follows the existing video/API design records:

1. playback issuance returns a short-lived same-origin manifest URL carrying a signed playback
   session;
2. the authenticated manifest request verifies that session and re-evaluates the Student, exact
   approved Asset Version, Entitlement, suspension, expiry, and retirement state;
3. the API reads only the bounded text playlist and rewrites each safe relative media reference to
   an exact-object presigned URL whose expiry cannot exceed the playback session;
4. the HLS segment bytes are returned directly by MinIO, not the Go API.

After rebuilding, the repeatable proof passed: the manifest was non-cacheable
`application/vnd.apple.mpegurl` with `Referrer-Policy: no-referrer`, its segment URL pointed directly
at private MinIO, and a signed fetch returned non-empty media bytes. A real production session issued
through the existing S5 seed utility was accepted. A separate real session for the unrelated seeded
Student received the uniform 404 protected refusal and no manifest.

## Automated validation

The following passed after the remediation:

```text
go test ./...
go vet ./...
go test -tags=integration ./internal/media -run <playback-manifest-focused-expression> -count=1
./deploy/scripts/environment.sh build
./deploy/scripts/verify-worker-media.sh
```

Focused integration coverage includes successful exact-version rewriting, token tampering,
cross-Student replay, entitlement expiry before manifest refresh, path traversal, absolute targets,
and unsupported HLS tag URIs. Unsafe playlists fail before any child capability is returned.

## Remaining production boundary

- The provider's public storage delivery hostname must be HTTPS and browser-reachable, with private
  bucket CORS limited to `PUBLIC_ORIGIN` for signed `GET`/`HEAD`; the loopback proof consumes the
  signed segment from inside the isolated network and cannot prove provider CORS.
- The production scanner provider is still external. `MEDIA_OPERATING_MODE=ADMIN_CATALOGUE` remains
  fail-closed and requires recorded out-of-band exact-version scan evidence.
- Managed Redis authentication/TLS and the known frontend production dependency advisories remain
  High findings from earlier batches.
