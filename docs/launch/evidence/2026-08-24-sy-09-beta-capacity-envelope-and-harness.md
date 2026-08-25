# SY-09 — Beta capacity envelope approval and harness preparation

**Date:** 2026-08-24
**Status:** Harness preparation complete; representative capacity execution pending
**Decision:** [D-095](../../DECISIONS.md#d-095--founder-approves-the-limited-paid-beta-capacity-envelope-and-sy-09-harness-preparation)

## 1. Founder approval

The Founder approved a limited paid beta capacity contract for the initial approximately 20–50 paid
Students. The approval covers the beta stage only; it does not close SY-09, authorize a load run, or
change application behavior.

The approved envelope is:

| Area | Contract |
|---|---|
| Accounts | 110 registered Accounts, including 50 entitled paid Students |
| Authenticated sessions | 50 simultaneous Student sessions |
| Login surge | 100 distinct successful Student logins within 60 seconds |
| Sustained mixed traffic | 20 total application RPS for 10 minutes after warm-up; at least 18 RPS authenticated Student traffic |
| Short burst | 30 total application RPS for 60 seconds; at least 27 RPS authenticated Student traffic |
| Anonymous catalogue | 10 RPS standalone for 10 minutes and approximately 10% of mixed traffic |
| Progress | 15% of mixed traffic; approximately 3 RPS sustained and 4.5 RPS burst |
| Playback | 100 authorization + manifest starts within 60 seconds across 50 Students; at most two per Student |
| Privileged operators | Five concurrent operators: one Admin and four Instructors |
| Uploads | Three concurrent direct Instructor uploads during Student traffic |
| Transcodes | At most two active transcodes per worker; additional work queues safely |
| Latency | Read/control plane p95 <300 ms; transactional writes p95 <800 ms |
| Healthy first frame | Entitled-video p95 <3 seconds through a separate small browser/edge check |
| Errors | Zero expected valid-workload transport, status, auth, entitlement, manifest, iteration, upload, and terminal-worker failures |
| Repeatability | Two clean repetitions of every required scenario |

The resource gates are host CPU p95 ≤85%, no sustained CPU >90% for 60 seconds, memory ≤80% of the
representative KVM2 budget, no swap/OOM/unexpected restart, disk below the existing 85% warning
threshold with at least 5 GiB free, and safe PostgreSQL, Redis, and worker state. Required missing
evidence is a failure.

## 2. Historical LG-019 preservation

The original full-launch target remains unchanged: 5,000 registered Accounts, 500 simultaneous active
sessions, 500 Students beginning within one minute, 250 API RPS for 60 seconds, 250 playback starts
per minute, 20 concurrent uploads, no more than two transcodes per worker, and the original NFR,
recovery, budget, region, and availability requirements. This record does not rewrite historical
LG-019 evidence in [`PRD.md`](../../PRD.md) or [`LAUNCH_GATES.md`](../../LAUNCH_GATES.md).

The current KVM2 governance verdict remains **B — plausibly sufficient but needs beta-envelope proof**.
No KVM2 capacity claim is promoted to proven here.

## 3. Harness audit

The existing tooling in [`deploy/loadtest`](../../../deploy/loadtest) was verified before editing:

| Capability | Current support before this tranche | Beta requirement | Change |
|---|---|---|---|
| Fixture scale | Hard-coded 5,000 Accounts / 500 Students | 110 Accounts / 104 Students / 50 entitled | Added isolated beta seed and manifest path; historical path remains intact |
| Progress | Absent from the old API cycle | 15% real Progress writes | Added real `PUT /api/v1/learn/lessons/:lessonId/progress` workload |
| Playback | Old path and 250-start full-launch profile | 100 real authorization + manifest starts | Added actual media authorization and manifest control-plane flow |
| Anonymous catalogue | Old cycle attached a Student session to catalogue reads | No cookie, token, or CSRF | Beta catalogue operations use an explicitly anonymous header builder |
| Operators | Absent | One Admin + four Instructors | Added canonical read scenarios |
| Upload contention | Absent | Three direct-to-storage uploads | Added intent → provider PUT → completion scenario |
| Transcode contention | Absent | Two active, third queued | Added profile/procedure and worker capture; drain bound remains fixture-derived |
| PostgreSQL | No automatic pool/query capture | Bounded aggregate capture | Added read-only capture script and Go capture command |
| Redis | No automatic aggregate capture | Safe memory/client/error metrics | Added read-only INFO capture |
| Worker queue | No capture | Depth, age, active, retry, terminal metrics | Added Asynq Inspector capture command |
| Generator | No CPU/FD capture | CPU, memory, FD limit/pressure, dropped iterations | Added exact-PID generator capture |
| Server | Existing bounded `capture-server.sh` | Preserve host/container safety evidence | Reused it; no rewrite |
| Evaluator | k6 thresholds only | Deterministic fail-closed summary | Added pure evaluator and missing-artifact failure |

Intentional rate-limit diagnostics remain separate from the beta capacity proof; valid beta capacity
scenarios treat every 429 as a failure and do not alter limiter settings.

## 4. Canonical beta configuration

[`limited-paid-beta.json`](../../../deploy/loadtest/limited-paid-beta.json) is the single source for
fixture counts, RPS, durations, workload mix, playback/login/operator/upload values, latency classes,
resource gates, error counters, repeat count, provenance fields, storage mode, and cleanup prefixes.
The beta runner requires the explicit `limited-paid-beta` profile and does not silently substitute the
historical full-launch values.

Warm-up is deterministic and excluded from measured totals: stack health, separate cold-start smoke,
representative route warm-up, two-minute idle/baseline capture, then the measured scenario. The profile
requires two clean repetitions and prohibits automatic retry; failures remain evidence.

## 5. Fixture design and account reconciliation

The beta-only `-beta-loadtest` path does not call the much larger canonical acceptance seed. It creates
exactly:

| Persona | Count | Notes |
|---|---:|---|
| Student Accounts | 104 | All synthetic, active, and password-bearing only in the disposable database |
| Entitled Students | 50 | Distributed round-robin over the eight published Courses |
| Non-entitled Students | 54 | Registered and login-capable, with no Course entitlement |
| Admin | 1 | Used by the privileged operator scenario |
| Instructors | 5 | Four operator identities plus one additional fixture identity |
| **Total registered Accounts** | **110** | **104 + 1 + 5 = 110** |

The login scenario uses the first 100 distinct Student identities from the 104-Student manifest. It
does not repeat one identity or share one authenticated session. The mixed and playback scenarios use
the 50 entitled Student entries, each carrying a valid Course, Lesson, and READY Asset Version mapping.

Eight published Courses each contain two Sections and four Lessons. The first Lesson has a READY video
version whose object key is under the exact `capacity/{run_id}/` prefix; the remaining Lessons provide a
representative graph without adding unnecessary media bytes. The video fixture uses the smallest
existing READY playback-control object shape and does not attempt to create bandwidth pressure.

Fixtures are synthetic, disposable, non-PII, and use `.test` addresses. No production payment record,
retained acceptance corpus, real Student identity, or production email is used.

## 6. Scenario definitions

### Sustained mixed Student workload

The beta module schedules 20 total application RPS for 600 seconds after warm-up, with a 19-workflow/s
logical rate that accounts for the paired playback authorization + manifest requests and produces the
20-request/s application target. The burst uses the same request-level mix at 30 total RPS for 60
seconds, with the corresponding 28.5-workflow/s logical rate.

The mix is exactly:

| Operation | Share |
|---|---:|
| Anonymous catalogue list | 5% |
| Anonymous catalogue detail | 5% |
| Authenticated session check | 5% |
| Dashboard | 7.5% |
| Access-status read | 7.5% |
| Course Home | 20% |
| Lesson metadata | 25% |
| Playback authorization | 5% |
| Playback manifest | 5% |
| Progress write | 15% |
| **Total** | **100%** |

All authenticated operations use real session cookies and CSRF tokens where required. Dashboard,
Course Home, Lesson metadata, playback, and Progress use the fixture's real Course/lesson/entitlement
data; no direct database read replaces API traffic.

### Anonymous public catalogue

`public-catalogue` runs list/detail traffic at 10 RPS for 10 minutes. It sends no Student cookie, no
authorization header, and no CSRF token. List/detail distribution is deterministic at 70%/30% and uses
the current public routes `GET /api/v1/catalog/courses` and
`GET /api/v1/catalog/courses/:idOrSlug`. The tranche only validates this path; it does not execute it.

### Login surge

`login-surge` selects 100 distinct Student email identities. Each iteration clears its cookie jar,
performs anonymous `GET /api/v1/session/bootstrap`, then the real `POST /api/v1/sessions` with the
run-scoped password and bootstrap CSRF token. Password hashing and admission/rate limits are not
bypassed. The future run must use the supported shared public IPv4/NAT topology when available and must
record bootstrap and login successes separately.

### Playback surge and first frame

`playback-surge` assigns exactly two starts to each of the 50 entitled Students. Each start performs
the real protected `POST /api/v1/media/playback-authorizations` with the exact Lesson and Asset Version,
then fetches the returned `GET /api/v1/media/playback-manifests/:playbackSession/index.m3u8`. It asserts
authorization, expected Asset Version, manifest path, `#EXTM3U`, and media playlist content. It records
authorization and manifest p95 separately, applies the <300 ms control-plane class, and never follows
signed segment URLs.

The <3-second time-to-first-frame requirement is not inferred from k6 control-plane timing. It remains
a separate small browser/edge check, to be implemented only through existing media-performance tooling
and run separately from capacity traffic. No browser flood is prepared or executed here.

### Privileged operators

Five concurrent low-frequency read operators use existing canonical APIs. The Admin reads the reported-
content queue; Instructors read their owned Course roster through `GET /api/v1/courses/:id/students`.
No destructive lifecycle loop, repeated payment confirmation, shared mutation target, bypass header, or
production secret is introduced.

### Upload and transcode contention

The upload scenario uses the real Instructor `POST /api/v1/media/uploads` intent, the returned provider
upload URL, a bounded fixture PUT directly to the S3-compatible provider, and
`POST /api/v1/media/uploads/:id/completions`. It requires a provider version ID and records intent,
direct-upload, and completion failures separately; media bytes do not pass through Go merely for the
test.

The transcode acceptance procedure is future-only: submit three deterministic disposable media jobs,
observe two active transcodes per worker, observe the third queued, then prove the third reaches its
expected terminal state, the queue drains, and no worker/retry/terminal failure occurs. The profile
deliberately leaves the drain seconds unset until the actual approved media fixture size and existing
authority establish a realistic bound.

## 7. Latency classes and error accounting

Playback authorization and manifest are explicitly read/control-plane measurements despite authorization
being a POST. Password login, Progress, upload intent, direct upload, and upload completion are
transactional-write measurements. The healthy video first-frame check has its own browser metric.

Counters stay separate: transport failures, unexpected statuses, 4xx, 429, 5xx, 503, authentication,
entitlement, response-shape, manifest, dropped-iteration, upload, and terminal-worker failures. A future
capacity pass requires every expected-valid-workload counter to be zero; the evaluator never collapses
these into a single permissive `errors` number.

## 8. Resource and subsystem capture

The existing [`capture-server.sh`](../../../deploy/loadtest/capture-server.sh) remains the server-side
capture for host CPU/memory/swap/disk and Docker CPU/memory/network/block/PID/restart state. Bounded
service logs remain private and require sanitization before evidence publication.

New capture support is split by ownership:

- [`capture-generator.sh`](../../../deploy/loadtest/capture-generator.sh) samples the exact generator
  PID's process CPU, RSS, open FD count, and FD limit; k6 contributes dropped iterations and timing;
- [`capture-services.sh`](../../../deploy/loadtest/capture-services.sh) uses bounded read-only
  `pg_stat_activity` and Redis `INFO`/`commandstats`, with no row dump, key scan, flush, or mutation;
- `backend/cmd/loadtest-capture` uses the current validated Redis connection, pgx pool statistics,
  safe aggregate PostgreSQL wait/long-query counts, and the current Asynq Inspector for queue depth,
  oldest relevant job age, active jobs/transcodes, retry failures, terminal failures, and worker count.

No monitoring daemon or application instrumentation is added. Missing capture remains incomplete and
fails the evaluator.

## 9. Provenance and result artifacts

Every future run requires a unique run ID and repetition number. The run ID is carried through the
fixture database name, storage prefix, session manifest, result path, and capture directory. Required
provenance is release identity, container image identity, Compose/project identity, host class, KVM2
CPU/RAM/disk, storage provider mode, and environment profile.

The future sanitized artifact set is machine-readable and includes `summary`, latency metrics, every
error counter, generator metrics, server metrics, PostgreSQL metrics, Redis metrics, worker metrics,
fixture/config fingerprint, release identity, and run ID. Cookies, passwords, API keys, signed URLs,
real-looking emails, and raw private logs are excluded from published evidence.

## 10. Cleanup safety

Cleanup is exact-run only: drop the exact disposable `gradex_playwright_e2e_{run_id}` database, delete
the exact `capacity/{run_id}/` storage prefix, revoke the exact run sessions, remove exact private test
files, and stop only the exact capacity Compose project. The pure cleanup validator rejects wildcard
paths and cross-run database/storage names.

Forbidden operations are wildcard database drops, bucket-wide deletion, `docker system prune`,
`docker volume prune`, retained-stack teardown, and broad process killing. No retained service or
production database was manipulated in this tranche.

## 11. Validation and no-load execution

The following low-impact checks were permitted and are the only harness checks represented by this
record:

```text
node --test deploy/loadtest/harness.test.mjs
node deploy/loadtest/validate-profile.mjs --list
bash deploy/scripts/verify-loadtest-harness.sh
GIT_INDEX_FILE=<temporary-copy-with-untracked-targets-added> bash scripts/docs-guard.sh
cd backend && go test ./cmd/e2e-seed ./cmd/storage-fixture ./cmd/loadtest-capture
```

These checks parse configuration, exercise pure validators, compile/run inert fixture-tool tests, and
check shell/JavaScript safety. The documentation guard passed with a temporary index copy so the
protected dirty worktree index was not staged or changed. No command invoked k6, `deploy/loadtest/run.sh`, Docker Compose, a
remote target, sustained traffic, login flood, playback flood, upload contention, or FFmpeg/transcode
contention. No VPS was load-tested, no beta stack was deployed, and no PostgreSQL/Redis/Caddy/worker
tuning was performed.

## 12. Tracker impact and remaining proof

Before and after this tranche the MVP tracker remains **47 / 53 = 88.7%**. SY-09 remains `BLOCKED`;
it is not `E2E_PROVEN`. The explanatory note now records Founder-approved beta envelope and harness
preparation with representative execution and two clean repetitions pending. SY-01, SY-02, SY-03, and
SY-08 are unchanged, as are INF-01 and the deferred MED rows.

SY-09 closure still requires:

1. a representative beta KVM2 stack and the actual beta storage/edge topology;
2. a cold-start smoke and baseline capture kept separate from measured totals;
3. every required beta scenario executed once with complete provenance and sanitized artifacts;
4. a second clean repetition on the same release, topology, fixture shape, provider, and resource limits;
5. deterministic evaluator output of `PASS`; and
6. authority/tracker reconciliation that admits no growth beyond this envelope without retest.

## 13. Repository safety

Only harness/fixture/capture tooling, this evidence, the new Founder decision, and the explanatory
SY-09 tracker note are in scope. The branch was dirty before this tranche; unrelated user-owned and
T5–T9/paid-beta changes were preserved. No reset, clean, stash, restore, broad checkout, package-wide
formatting, retained database teardown, or production/application source change was used.

**Final tranche state:** `SY-09 HARNESS READY — BETA CAPACITY ENVELOPE APPROVED, REPRESENTATIVE EXECUTION PENDING`.
