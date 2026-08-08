# S12 T045/T046 release-hardening evidence

Date: 2026-08-08

Starting revision: `18583aabe4818d92e38f1d5444de7832e4b5a051`

## T045 — production frontend dependencies

The starting lockfile installed Next.js 14.2.35, direct PostCSS 8.5.21, Next-nested PostCSS 8.4.31,
and nanoid 3.3.16. `npm audit --omit=dev --audit-level=high` reported three High production
dependency findings:

- `nanoid <3.3.17`: `GHSA-2v37-7h3g-55p8`.
- `postcss <=8.5.22`: `GHSA-qx2v-qp2m-jg93`, `GHSA-6g55-p6wh-862q`,
  `GHSA-r28c-9q8g-f849`, and `GHSA-fxqj-rqcc-2cmp`, through both the direct and Next-nested paths.
- Next.js 14.2.35: `GHSA-9g9p-9gw9-jx7f`, `GHSA-h25m-26qc-wcjf`,
  `GHSA-ggv3-7p47-pfv8`, `GHSA-3x4c-7xq6-9pq8`, `GHSA-q4gf-8mx6-v5v3`,
  `GHSA-8h8q-6873-q5fj`, `GHSA-3g8h-86w9-wvmq`, `GHSA-ffhc-5mcf-pf4q`,
  `GHSA-vfv6-92ff-j949`, `GHSA-gx5p-jg67-6x7h`, `GHSA-h64f-5h5j-jqjh`,
  `GHSA-c4j6-fc7j-m34r`, `GHSA-wfc6-r584-vfw7`, `GHSA-36qx-fr4f-26g5`,
  `GHSA-m99w-x7hq-7vfj`, `GHSA-89xv-2m56-2m9x`, `GHSA-68g3-v927-f742`,
  `GHSA-4633-3j49-mh5q`, `GHSA-4c39-4ccg-62r3`, `GHSA-p9j2-gv94-2wf4`, and
  `GHSA-955p-x3mx-jcvp`.

The remediated lock installs Next.js 15.5.21, PostCSS 8.5.23 on every path, nanoid 3.3.17, and
sharp 0.35.0. The sharp constraint is necessary because resolving Next.js 15 without it selected
sharp below 0.35.0 and exposed `GHSA-f88m-g3jw-g9cj`. React and React DOM remain 18.3.1.

These commands passed from the remediated lockfile:

```text
npm ci
npm audit --omit=dev --audit-level=high  # found 0 vulnerabilities
npm run lint
npm run typecheck
npm test                                # 163 passed, 0 failed
npm run build                           # optimized Next.js production build passed
./deploy/scripts/environment.sh build   # frontend production image built
./deploy/scripts/verify-staging-smoke.sh
                                          # 2 Playwright tests passed
```

The full development-tool audit still reports High advisories in `brace-expansion` and `js-yaml`.
They are absent from the production dependency audit and were not changed because T045 is scoped to
the reported production paths and forbids unrelated dependency cleanup.

T045 implementation revision:
`a3ce0b57e077ae6e38bb529e10c04186d1b8b02f`.

## T046 — authenticated verified-TLS Redis

`REDIS_ADDR` remains a credential-free `host:port`. `REDIS_PASSWORD` enables password-only
authentication and optional `REDIS_USERNAME` enables Redis ACL authentication. `REDIS_TLS_ENABLED`,
`REDIS_TLS_SERVER_NAME`, and `REDIS_TLS_CA_CERT_FILE` configure TLS with hostname and certificate
verification. There is no skip-verification setting. Development accepts the existing plaintext,
unauthenticated configuration; staging and production reject missing authentication or TLS.

The API queue, readiness client, and rate-limit clients plus the worker queue, health client, and
asynq server all derive options from `queue.Connection`. Secrets remain redacting `config.Secret`
values until the two exposure-guard-pinned driver assignments.

Focused configuration/connection tests passed for development compatibility, staging/production
fail-closed validation, password-only authentication, ACL username/password authentication, verified
TLS, server-name/custom-CA handling, invalid CA rejection, credential redaction, and identical
go-redis/asynq settings. The full backend CI-equivalent build, vet, integration-tag vet, and race
suite also passed.

The rebuilt disposable production-like environment ran TLS-only password-authenticated Redis. These
checks passed without printing the generated password:

```text
./deploy/scripts/environment.sh up
./deploy/scripts/environment.sh verify
./deploy/scripts/environment.sh redis-security
./deploy/scripts/environment.sh data-plane
./deploy/scripts/verify-worker-media.sh
./deploy/scripts/verify-observability.sh
./deploy/scripts/verify-edge-security.sh
./deploy/scripts/verify-staging-smoke.sh
```

The explicit security proof verified the generated CA chain, refused plaintext, returned `NOAUTH`
for unauthenticated verified TLS, and returned `PONG` only for authenticated verified TLS. The API
reported healthy `/healthz` and ready `/readyz` with PostgreSQL, Redis, and schema checks all `ok`;
the worker remained running. Schema version remained clean at 15. Worker Redis outage/restart,
outbox reconciliation, private-media processing/playback, alert delivery, and the S5/S6 deployed
journey all passed on the hardened topology.

No migration or database implementation changed, so the earlier isolated restore proof remains
applicable and was not repeated. Mandatory Redis TLS/authentication creates an application-config
compatibility floor: after the Redis service is hardened, pre-T046 backend artifacts are not valid
rollback targets. The first T046-capable deployed release must become the new known-good rollback
floor; future N to N+1 to N drills must use two T046-capable artifacts.

T046 implementation revision:
`6d96348aa7b8cebe6dec2ffeaa31847c48a7d281`.

T047 provider execution and T048 independent review remain open and untouched.
