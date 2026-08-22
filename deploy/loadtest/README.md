# LG-019 load-test harness

This harness measures the Founder-approved launch envelope without changing authentication,
entitlement, media, or rate-limit behavior. It is evidence tooling, not evidence that LG-019 has
passed. Real launch evidence must come from an external load generator against the intended KVM2
baseline (2 vCPU, 7.8 GiB RAM, 96 GB disk, Germany) while staying within the approved $200/month
infrastructure ceiling.

## Scenarios

`api-surge` schedules exactly 250 application requests per second for 60 seconds across 500
production-valid Student sessions. Its fixed 250-request cycle is:

- 1 authenticated `GET /api/v1/session` resolution (0.4%);
- 24 public Course-list reads (9.6%);
- 25 public Course-detail reads (10%);
- 50 entitled learning-dashboard reads (20%);
- 75 entitled Course-home reads (30%);
- 75 entitled Lesson reads (30%).

This keeps cacheable catalogue traffic below 20%; most work re-enters authentication, PostgreSQL
learning projections, entitlement evaluation, Course graphs, Progress, and material visibility. The
single session resolution per cycle stays below the shipped 120/minute endpoint quota instead of
turning a representative application-read test into a session-rate-limit test. Health and readiness
requests never count toward the load.

`login-surge` starts one real browser-equivalent login flow for each of 500 distinct Students within
one minute: anonymous session bootstrap, cookie jar, CSRF token, password verification, PostgreSQL
session-family write, and hardened session cookie issuance. A `429` is a failure. Production
admission now preserves the per-identifier and per-browser brute-force ceilings while allowing up to
600 first attempts per minute from one exact IPv4 source and across the distributed expensive-work
budget. A local one-verifier/500-waiter Argon2id gate protects the KVM2. These controls remove the
known policy contradiction; only the real external run can establish capacity.

`playback-surge` schedules 250 distinct protected playback starts over 60 seconds. Every start uses a
production-valid session, calls the actual Lesson playback route, validates the exact Asset Version,
then fetches the returned Gradex manifest capability. It validates the HLS control-plane response but
never follows signed segment URLs or downloads video bytes through the Go API.

## Disposable data

The opt-in fixture extends the existing safety-gated E2E seed tool. It can target only an explicitly
acknowledged local/test database matching `gradex_playwright_e2e_<run>`, never the application or a
remote database. The resulting database contains exactly 5,000 registered Accounts. Five hundred
deterministic Students have ACTIVE credentials, invitation-provenanced Entitlements, Enrollments, and
access to the existing published Course, Lesson, and exact READY video fixture.

The preparation step also performs 500 out-of-band calls through the real `SessionRepository`. Those
calls verify the test password and write ordinary production-valid session families. They are not
measured traffic and are used only by `api-surge` and `playback-surge`; `login-surge` always performs
the real HTTP login flow itself.

Choose a new private output directory and enter a disposable password without echoing it:

Run this from a shell that already has the complete protected acceptance-runtime configuration
loaded. Session issuance uses the repository's normal configuration validation so that its cookie,
CSRF, and expiry semantics match the target API; the commands below show only the additional
load-test and isolated-database inputs.

```bash
read -rsp 'Disposable load-test Student password: ' GRADEX_LOADTEST_PASSWORD; printf '\n'
export GRADEX_LOADTEST_PASSWORD
export GRADEX_E2E_ALLOW_DATABASE_RESET=1
export GRADEX_E2E_ADMIN_DB_URL=...       # protected local/test administration DSN
export GRADEX_E2E_TARGET_DB_NAME=gradex_playwright_e2e_<unique-run>
export GRADEX_E2E_TARGET_DB_URL=...      # the same isolated database
export DATABASE_URL=...                  # regular application DB; must differ from target
export SESSION_CSRF_KEY=...              # same protected test key used by the target API
./deploy/loadtest/prepare-fixtures.sh /protected/path/loadtest-fixtures-<run>
```

Do not paste DSNs or keys into evidence. `fixture.json` contains IDs only. `sessions.json` is mode
0600 and contains live test session credentials; never publish it. Point a dedicated acceptance API
stack at the isolated database and install the existing `test/master.m3u8` and `test/segment000.ts`
storage fixture through the supported staging-smoke path before playback testing. Never repoint the
production application at this database.

## Running from the external generator

The runner has no target default. A remote run requires both an explicit HTTPS origin and an explicit
acknowledgement that the process is running on a separate load-generator machine. It uses the pinned
`grafana/k6:0.55.0` container and installs nothing on the VPS.

```bash
export GRADEX_LOADTEST_TARGET_URL=https://staging.example.com
export GRADEX_LOADTEST_FIXTURE_DIR=/protected/path/loadtest-fixtures-<run>
export GRADEX_LOADTEST_RESULTS_DIR=/protected/path/loadtest-results
export GRADEX_LOADTEST_RUN_ID=<unique-evidence-id>
export GRADEX_LOADTEST_ALLOW_REMOTE=I_UNDERSTAND_THIS_GENERATES_REMOTE_LOAD
export GRADEX_LOADTEST_EXTERNAL_GENERATOR=1

./deploy/loadtest/run.sh api-surge
./deploy/loadtest/run.sh playback-surge
./deploy/loadtest/run.sh login-surge
```

The password is required only for `login-surge`; keep it in the runner environment and unset it
afterward. If the target uses a private CA, set `GRADEX_LOADTEST_CA_FILE` to its readable CA bundle.
TLS verification is never disabled.

A harmless local smoke requires an explicit local target and opt-in. It reduces API load to 5 RPS
for 10 seconds and login/playback populations to five:

```bash
export GRADEX_LOADTEST_TARGET_URL=http://127.0.0.1:<port>
export GRADEX_LOADTEST_ALLOW_INSECURE_LOCAL=1
export GRADEX_LOADTEST_SMOKE=1
./deploy/loadtest/run.sh api-surge
```

## Results and pass/fail

Each scenario prints a human summary and writes one mode-0600 JSON document named
`<scenario>-<run-id>.json`. Results contain only aggregate metrics—never target URLs, passwords,
cookies, CSRF tokens, signed manifests, or signed object URLs. They report total/achieved RPS,
successes, HTTP/transport/5xx/429/correctness failures, duration, p50/p95/p99/max latency, scenario
success counts, and per-endpoint latency/failures.

All scenarios fail on an unexpected status (including `429`), transport failure, 5xx, malformed
authentication/entitlement response, or dropped iteration. API surge additionally requires all
15,000 requests, at least 250 achieved application RPS, and the canonical read p95 under 300 ms for
every endpoint. Login requires 500 attempts and 500 successes, and applies the canonical
transactional-write p95-under-800-ms target to successful logins. Playback requires 250 complete
authorization-plus-manifest starts. No unapproved playback latency threshold is invented.

## Server resource capture

Run resource capture on the target VPS while the external generator runs. It only reads Docker and
`/proc`, runs for a bounded duration, and never sources `runtime.env`:

```bash
export GRADEX_LOADTEST_COMPOSE_PROJECT=gradex-staging
export GRADEX_LOADTEST_CAPTURE_SECONDS=90
./deploy/loadtest/capture-server.sh /protected/path/server-capture-<run>
```

The private output includes five-second host CPU, memory, swap, and disk samples; per-container CPU,
memory, network, block-I/O and PID samples; final container state/restart counts; and at most 500 log
lines each for PostgreSQL, Redis, API, worker, frontend, and edge. Review logs for unexpected sensitive
data before copying any evidence.

## Cleanup and evidence boundary

Stop the dedicated acceptance stack, rebuild the E2E seed tool into a newly created temporary file,
and invoke its `-drop` operation with the same safety environment. The tool independently rechecks
the local/test database name, distinct application database, and explicit reset acknowledgement
before dropping anything:

```bash
cleanup_binary="$(mktemp)"
(cd backend && go test -c -o "$cleanup_binary" ./cmd/e2e-seed)
(cd backend && "$cleanup_binary" -drop)
rm -f -- "$cleanup_binary"
```

Then remove only the exact private fixture directory after the run; never retain or publish
`sessions.json`. Preserve the aggregate JSON and sanitized server capture under the launch-evidence
process only after confirming that they contain no credentials, signed URLs, personal data, or
unbounded logs.

LG-019 remains open until the real external run proves the 500-Student/250-RPS envelope on the KVM2,
including zero unexpected login `429`, gate saturation, or authentication failures; external
alerts and scheduled backups have production evidence, a provider backup is restored in isolation,
RPO/RTO are approved, and budget/region/sizing/security sign-off is complete. The approved 20 direct
uploads and no-more-than-two transcodes per worker also require their separate representative
validation; these three Student control-plane scenarios do not claim to test them.
