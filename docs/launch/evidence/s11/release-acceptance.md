# S11 Release Acceptance Evidence

Date: 2026-08-09  
Hard launch target: 2026-08-15  
Branch: `s11-release-e2e-20260808`

## Revision and artifacts

- Starting HEAD: `6bf694daa7a8a823a849a4e2da9588988b6d2358`
- Planning commit: `0d460230de2eb6f1a426039d9ee1d1afaf5d8da8`
- Implementation commits:
  - `ee59f0c42bff84e5fa8d2df5ffe1d4c89c6f7640` — release journey, evidence helpers, replay hardening, and reusable verifier
  - `c424e8a0be4baa8c97ccda3aa2c2a7e25d7fd801` — Next.js protected-request header fix and regression assertion
  - `182bfa59354f86485ca9d6dc13538101d5f4e5f4` — exact Invitation rejection/cancellation integration selection
- Evidence commit / frozen candidate end: `aff4fd7feddb9436d14244ca377c3235ead47046`
- S11 artifacts: `spec.md`, `plan.md`, `research.md`, `data-model.md`, `quickstart.md`, `contracts/release-suite.md`, `contracts/traceability.md`, `checklists/requirements.md`, and `tasks.md` under `specs/009-release-acceptance/`
- Task count: 29. Tasks T001–T028 are complete in the evidence candidate; T029 is completed by the subsequent documentation-only freeze marker.

### Production registration remediation

- Remediation starting HEAD: `0e8f3ed0de858462f8bdd6fab42acc056ca703f0`
- Approved design: `bd7555e` — HIBP production adapter design and LG-011 scope boundary
- Implementation: `710dbd3` — HIBP Range API adapter, shared runtime composition, bootstrap wiring, and three-second default
- Regression tests: `d768dd8` — deterministic TLS/provider tests, production composition tests, and real-PostgreSQL registration evidence
- TLS regression: `7936905` — explicit proof that an untrusted server certificate fails closed before an HTTP request is accepted
- Documentation/evidence commit and frozen remediation candidate end: `2a572d932b3a022fe67eb16c630da5f736d15d95`
- Frozen remediation range: `0e8f3ed0de858462f8bdd6fab42acc056ca703f0..2a572d932b3a022fe67eb16c630da5f736d15d95`
- Cumulative S11 range for independent review: `6bf694daa7a8a823a849a4e2da9588988b6d2358..2a572d932b3a022fe67eb16c630da5f736d15d95`
- Original High root cause: production composition had only development policy and deterministic compromised-password fixtures; non-development builders refused both missing dependencies as one combined error.
- LG-021 result: **RESOLVED** under D-075. HIBP is composed for production credential screening and fails closed.
- LG-011 result at the HIBP freeze: **OPEN**. This historical boundary is closed by the subsequent LG-011 remediation below.

### LG-011 policy remediation

- Remediation starting HEAD: `9212e637d152a79d2db82c44bebac3d60001bd7c`
- Approved authority/design: `54c529f2d0039cca871246818428239f2a00a33e` — exact Product Owner policy package and implementation design
- Implementation: `037ac63c029bca9cfd78677fffe7f02ca1f2dfba` — production policy resolver, legal configuration, four public routes, acceptance metadata, and production composition
- Regression/acceptance: `5ec8a50a964c0e38d8cc8f3661b0f603966a8faf` — configuration, resolver, persistence, HIBP composition, legal-page, and complete release-journey evidence
- Approved policy set: `gradex-legal-2026-08-09-v1`; set, Privacy, and Terms version `2026-08-09-v1`; effective date `2026-08-09`; minimum age 18; Arabic primary.
- Canonical routes: `/ar/privacy`, `/en/privacy`, `/ar/terms`, and `/en/terms`, derived from `PUBLIC_ORIGIN`.
- Acceptance persistence: registration records the exact resolved version and locale; integration evidence proves a later policy version does not rewrite historical rows.
- Composition: non-development registration uses `ApprovedPolicySetResolver` and the HIBP Range API source through the one shared `AdmissionService`; development retains only its controlled fixtures.
- Fail-closed configuration: missing/invalid identity or contact fields, non-HTTPS public origin, unsafe endpoint composition, or staging sentinels in public mode prevent startup/rendering. Controlled staging accepts both exact sentinels only at `https://gradex.localhost:18443`.
- Result: **LG-011 software blocker resolved**. Real public production remains externally blocked until an actual legal registration number and registered address replace both staging sentinels.

## Acceptance coverage

The local isolated Chromium journey proves the complete critical path through real browser screens, the real Go API, real PostgreSQL, real sessions, and the existing media fixture:

```text
Student registration -> delivered email verification -> password login
-> Admin Course expiry -> Course Access Invitation -> Student acceptance
-> exactly zero Entitlements and zero Enrollments before approval
-> protected Course/Lesson/playback/Progress denial before approval
-> authorized Admin Approval
-> exactly one ACTIVE MANUAL_INVITATION Entitlement with source_invitation_id
-> exactly one Enrollment -> protected Course -> protected Lesson -> playback
-> Progress persistence -> unrelated Student denial
-> authorized repeated approval returns 200 and preserves both identities/cardinalities
```

Negative coverage asserts Course, Lesson, playback, and Progress denial both before approval and for an unrelated Student, with zero grant/enrollment/progress side effects. Existing S4/S5 integration coverage supplies byte-identical denial, per-route authority revalidation, all-denial side-effect checks, and protected-media refusal. The HTTPS verifier additionally retrieves the protected manifest and a non-empty signed HLS segment.

Recovery coverage submits an invalid email-verification bearer before the valid bearer, an invalid Invitation bearer before the valid bearer, and an authorized approval replay. The focused release command also selects the complete identity journey, Invitation rejection/cancellation behavior, verification single-use/supersession, reset-secret expiry/replay/wrong-purpose, concurrent approval, denial equivalence, authority revalidation, and denial side-effect integration tests.

## Validation results

All commands below ran from the candidate containing `182bfa5` plus no uncommitted implementation changes.

| Gate | Command | Result |
|---|---|---|
| Go format/shell syntax/diff | `gofmt`, `bash -n`, `git diff --check` on changed paths | PASS |
| Backend static/full | `go vet ./... && go test ./...` | PASS across every backend package |
| Backend complete integration | `go test -tags=integration ./internal/... -count=1` | PASS across every internal package; HTTP API 201.639 s, identity 109.818 s, learning 61.349 s, media 59.908 s |
| Frontend type/unit | `npm run typecheck && npm test` | PASS; 169/169 tests |
| Frontend lint/build | `npm run lint && npm run build` | PASS; no ESLint warnings or errors and production build completed |
| Local S11 browser | `npm run test:e2e:release` | PASS; 1 Chromium test in 39.2 s against fresh database `gradex_playwright_e2e_mskvgmitxgqkatf8` |
| S11 consolidated HTTPS | `./deploy/scripts/verify-s11-release-acceptance.sh` | PASS; selected HTTP API and identity integrations, then 2/2 deployed Chromium tests in 4.1 s |
| HTTPS state/media | same command | PASS; state `1|1|1|1`, protected manifest and non-empty signed segment retrieved |
| Production dependencies | `npm audit --omit=dev` | PASS; 0 vulnerabilities |
| Full dependency audit | `npm audit` | FINDING; 2 High-severity advisories in ESLint-only transitive development dependencies |

### Remediation validation on 2026-08-09

| Gate | Command | Result |
|---|---|---|
| Backend build/static | `go build ./...`; `go vet ./...`; `go vet -tags=integration ./...` | PASS |
| Backend unit | `go test ./...` | PASS across all packages |
| HIBP unit/composition | `go test ./internal/identity ./internal/config ./cmd/api ./cmd/bootstrap-admin` | PASS |
| Registration integration | `go test -tags=integration ./internal/identity -run 'TestProductionHIBPRegistration' -count=1` | PASS; valid, policy-invalid, compromised, and unavailable scenarios |
| Complete affected integration | `go test -tags=integration ./internal/identity -count=1` | PASS in 74.223 s |
| API composition integration | `go test -tags=integration ./cmd/api -count=1` | PASS in 1.068 s |
| Race | `go test -race ./internal/identity -count=1`; focused tagged registration race | PASS in 2.439 s and 4.254 s |
| Live provider compatibility | `go test -tags=provider ./internal/identity -run '^TestHIBPProviderCompatibility$' -count=1` | PASS using fixed prefix `00000`; no password or identity input |
| Local S11 Chromium | `npm run test:e2e:release` | PASS; 1/1 in 40.3 s against isolated database `gradex_playwright_e2e_msl8a8amynq1ebgs` |
| S11 disposable HTTPS | `./deploy/scripts/verify-s11-release-acceptance.sh` | PASS; selected integrations, 2/2 deployed Chromium tests in 4.2 s, schema 15, state `1|1|1|1` |
| Production dependencies | `npm audit --omit=dev` | PASS; 0 vulnerabilities |
| Secret exposure | `./scripts/expose-guard.sh` | PASS; 13 approved exposure call sites and the existing reviewed password boundary |

No frontend source changed in this remediation, so the prior complete frontend lint, typecheck, unit,
and production-build evidence remains applicable. Chromium and deployed HTTPS regressions were rerun.

### LG-011 validation on 2026-08-09

| Gate | Command | Result |
|---|---|---|
| Backend build/static/unit | `go build ./...`; `go vet ./...`; `go vet -tags=integration ./...`; `go test ./...` | PASS across all packages |
| Complete affected integration | `go test -tags=integration ./internal/identity ./internal/httpapi ./cmd/api -count=1` | PASS; identity 91.225 s, HTTP API 185.814 s, API composition 2.273 s |
| Race | `go test -race ./internal/config ./internal/identity ./cmd/api -count=1`; focused tagged admission race | PASS |
| Frontend clean/static/unit/build | `npm ci`; `npm run lint`; `npm run typecheck`; `npm test`; `npm run build` | PASS; 171/171 unit tests, no lint/type errors, production build complete |
| Local Chromium | legal routes plus S11 journey; production-build S11 journey | PASS; 5/5 in 51.2 s and 1/1 in 11.0 s |
| S11 disposable HTTPS | `./deploy/scripts/verify-s11-release-acceptance.sh` | PASS; 5/5 deployed Chromium tests in 10.9 s, schema 15, state `1|1|1|1` |
| Production dependencies | `npm audit --omit=dev --audit-level=high` | PASS; 0 vulnerabilities |
| Secret exposure | `./scripts/expose-guard.sh` plus deployed API log scan | PASS; no changed exposure boundary and zero plaintext-password/full-digest/provider-detail log hits |

## Production-like environment

- Origin: `https://gradex.localhost:18443`
- Topology: existing S12 disposable Caddy HTTPS edge with separate frontend, Go API, worker, PostgreSQL, TLS-authenticated Redis, and private MinIO
- Acceptance database: `gradex_playwright_e2e_s12smoke01`
- Active application database: `gradex` (not reset or downgraded)
- Schema: both databases reported `15|f` (`version=15`, `dirty=false`)
- Deployed browser result: real production-mode registration, policy acceptance, verification, login, and the complete Invitation-to-protected-learning journey passed.
- Boundary: controlled non-public staging used the two explicit staging legal-identity sentinels. Public mode rejects them and still requires actual legal registration and registered-address values.
- Portability: Playwright retains the existing validated `GRADEX_E2E_EXTERNAL_ORIGIN` contract, external run-state/database variables, and CA/SPKI settings. A T047 origin can replace the disposable origin by configuration after real legal identity and external infrastructure exist.

## Reused S5/S6 evidence

- S5 `authenticates real Student via Go API session and renders Course Home from real PostgreSQL` ran against the HTTPS deployment.
- S6 `Complete 30-Step End-to-End Course Access Grant & Protected Learning Journey` ran against the HTTPS deployment.
- S6 replay coverage was strengthened: a real Admin CSRF token is now required, the replay must return exactly `200`, and the Entitlement and Enrollment identities and cardinalities must remain unchanged.
- Existing S4/S5 protected media, protected-learning denial, revalidation, and side-effect tests were selected/cited rather than copied.

## Findings and fixes

### Open

| Severity | Finding | Launch effect |
|---|---|---|
| Critical | None | — |
| High | None open after LG-011 software remediation. | — |
| Medium | None open | — |
| Low | `npm audit` reports `brace-expansion` and `js-yaml` advisories through ESLint development tooling; `npm audit --omit=dev` reports zero production vulnerabilities. | Non-runtime dependency-maintenance follow-up. |

### Fixed in S11

| Severity | Defect | Fix |
|---|---|---|
| High evidence gap | Existing S6 replay accepted `403` as possible success, so it could pass without reaching idempotent grant logic. | Use the real Admin CSRF token, require `200`, and assert identical Entitlement/Enrollment identities and counts. |
| Medium | Protected Course/Lesson server rendering synchronously accessed `headers()`, producing Next.js runtime errors. | Await `headers()` and retain a regression assertion. |
| Medium evidence gap | The HTTPS verifier used unsupported `psql -c` variable substitution, so its final authoritative state query failed after the browser passed. | Use the fixed internal fixture value through safe shell interpolation; the rerun passed with `1|1|1|1`. |

No commerce, S8 support/Entitlement-update, provider deployment, or product feature behavior was introduced. No migration or Hostinger provider file changed.

### Fixed in the registration remediation

| Severity | Defect | Fix |
|---|---|---|
| High component (LG-021) | Adapter mode had no production implementation; API and bootstrap could use only the deterministic development source and failed closed in production. | Integrate the approved HIBP prefix-5 Range API behind `CompromisedRangeSource`, share production composition, preserve a three-second bound/no retry, and prove privacy plus zero-side-effect failures. |
| High component (LG-011) | Production registration had no approved policy content or non-development `PolicySetResolver`, so composition refused startup. | Generate exact bilingual bodies from the approved package, compose the approved resolver, expose four public routes, persist exact acceptance versions, enforce public/staging identity modes, and prove the full HTTPS journey. |

## Disposition

- Remaining S11 implementation/validation tasks: none after the documentation-only freeze marker.
- Launch-blocking S11 software defects: none. LG-021 and the LG-011 software blocker are resolved.
- Independent review candidate: `6bf694daa7a8a823a849a4e2da9588988b6d2358..aff4fd7feddb9436d14244ca377c3235ead47046`.
- Ready for independent review: yes, as an exact acceptance implementation/evidence range.
- Ready for independent closure review: **yes, technically**. S11 is not independently approved or closed by this builder record.
- Remaining external public-production requirement: replace `LEGAL_REGISTRATION_NUMBER` and `LEGAL_REGISTERED_ADDRESS` with actual legal identity values before public T047. The approved staging sentinels are not launch values.
- Recommended next launch-critical action: freeze this remediation and request independent S11 closure review. Do not start S8 or T047/T048 in this pass.
