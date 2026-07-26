# S1B1 Student Admission Validation Quickstart

This guide validates the implemented S1B1 slice. It does not prove production readiness or resolve
LG-003, LG-004, LG-011, LG-015, LG-018, LG-019, or LG-021.

## Prerequisites

- Docker with Compose
- Go toolchain from `backend/go.mod`
- Node.js 22 and npm
- `curl` and `jq` for optional transport checks
- development/test-only policy, encryption-key, and compromised-password fixtures configured
- no production credentials or providers

Keep the user-owned root files outside this workflow. Run commands from the paths shown.

## 1. Start disposable local dependencies

```bash
cd backend
make up
make migrate-up
make migrate-version
```

Expected:

- PostgreSQL, Redis, and the existing local dependencies become healthy;
- schema reports version 5 and is not dirty;
- `accounts`, `policy_acceptances`, `identity_action_secrets`, `identity_security_events`,
  `outbox_events`, and `outbox_protected_payloads` exist;
- running `make migrate-up` again is a no-op.

## 2. Run focused backend evidence

```bash
cd backend
go test -race ./internal/identity ./internal/httpapi ./internal/ratelimit ./internal/outbox
go test -tags=integration -run 'TestBootstrap|TestRegister|TestVerification|TestAdmission|TestOutbox' ./internal/identity ./internal/httpapi ./internal/db ./internal/outbox
```

Expected evidence:

1. same bootstrap operation ID with identical semantics returns the recorded result;
2. changed email, display name, deployment principal, or password fails closed;
3. correspondence email is preserved while normalized email remains the unique key;
4. the compromised-password adapter never receives plaintext or the full derived digest;
5. unavailable/unconfigured screening fails every credential-creation path closed;
6. registration creates only a pending Student, no session, and one complete atomic evidence/outbox
   graph;
7. duplicate normalized email returns the hidden no-op and creates no related mutation;
8. forced final-write failure leaves no Account, credential, acceptance, secret, evidence, or outbox
   fragment;
9. resend supersedes exactly one live secret and creates one linked replacement intent;
10. valid consumption activates exactly once; expired, wrong-purpose, consumed, superseded, and
    concurrent losing calls all return the same invalid result;
11. ordinary JSON/text storage contains neither password nor verification-bearer canaries.

## 3. Run transport, privacy, and limiter evidence

The HTTP integration suite must exercise the complete middleware order rather than calling handlers
directly.

```bash
cd backend
go test -tags=integration -run 'TestAdmissionHTTP|TestAdmissionPrivacy|TestAdmissionRateLimit|TestAnonymousBootstrap' ./internal/httpapi
```

Expected:

- anonymous bootstrap uses a host-only Secure/HttpOnly/Strict cookie and returns a memory-only CSRF
  value with `no-store`;
- missing/wrong Origin or CSRF fails before quota/domain mutation;
- duplicate/unknown JSON is `MALFORMED_JSON`; semantic errors are `VALIDATION_FAILED`;
- new/existing registration responses match in status, body bytes, meaningful headers, cookie
  behavior, response-size class, navigation class, and bounded timing class;
- eligible/ineligible/unknown verification-request responses match on the same dimensions;
- all unusable verification secrets are `TOKEN_INVALID`;
- true quota exhaustion is `429`, Redis outage uses only bounded strict fallback, and absence of a
  safe decision is `503`, never fabricated `429`;
- unsafe policy/screening/delivery admission fails uniformly without revealing Account existence.

Optional manual transport smoke after starting the API:

```bash
cd backend
make run-api
```

In another terminal, acquire the anonymous cookie/CSRF token, fetch the current policy set, and
submit registration using the shapes in
[contracts/admission-api.md](contracts/admission-api.md). Use only local non-secret fixtures. Do not
paste real passwords, email addresses, or verification bearers into shell history or captured logs.

## 4. Validate the frontend

```bash
cd frontend
npm run lint
npm run typecheck
npm run build
npm run dev
```

Inspect:

- `/register`
- `/verify-email`
- `/verify-email/result`

Browser evidence:

| Scenario | Expected |
|---|---|
| First load | Arabic is the initial language and the document is RTL before interaction. |
| Language toggle | English switches the document to LTR; both dictionaries expose the same auth keys. |
| Phone/tablet/desktop | One-column admission task remains usable without horizontal scrolling or clipped controls. |
| Keyboard only | Logical tab order, visible focus, operable language/back/submit controls, and focused error summary. |
| Registration validation | Code-point bounds and BR-105 guidance are immediate; backend violations remain authoritative. |
| Accepted registration | Generic pending guidance says “if eligible” and never claims delivery or Account creation. |
| Verification request | Eligible/ineligible/unknown outcomes render the same guidance/navigation. |
| Verification deep link | Fragment is copied to memory and scrubbed immediately; network request carries it only in the POST body. |
| Verification result | Success offers login navigation but creates no session; every unusable link has one combined safe state. |
| `429` / `503` | Retry guidance is actionable without naming the limiter, provider, queue, or Account state. |
| Assistive behavior | Labels, descriptions, invalid state, alert summary, pending state, and result announcements are exposed semantically and do not depend on color. |

In browser storage/network/history tooling, prove:

- password, CSRF token, verification bearer, and future session credentials are absent from
  localStorage, sessionStorage, IndexedDB, readable cookies, analytics, console, and retained URL;
- the anonymous cookie is host-only, Secure, HttpOnly, SameSite=Strict, and `Path=/`;
- API calls are relative same-origin and carry no bearer in path/query.

The full automated browser journey remains the S1B3 integration close; S1B1 still requires the
manual responsive/bilingual/accessibility evidence above.

## 5. Run complete repository gates

```bash
cd backend
make ci
make test-integration
cd ../frontend
npm run lint
npm run typecheck
npm run build
cd ..
./scripts/docs-guard.sh
./scripts/expose-guard.sh
git diff --check
git status --short
```

Expected:

- all commands pass;
- builds/tests leave no generated working-tree changes;
- only the intended S1B1 files and pre-existing user-owned files appear;
- no plaintext-boundary, documentation, formatting, lint, type, race, migration, or integration
  failure remains.

## 6. Hosted and independent review evidence

Push the frozen implementation head and record:

1. hosted CI URL/run ID and green Backend, Frontend, Migrations, Admission Integration, and Guards
   jobs;
2. exact base and head commits;
3. a disposable detached reviewer worktree for that exact range;
4. independent Claude read-only review with no critical/high finding;
5. all lower-severity findings either fixed or explicitly dispositioned with evidence.

Do not mark S1B1 complete from local tests alone. Update the
[Day 8 record](../../docs/launch/daily/2026-07-30.md) and
[launch status](../../docs/launch/STATUS.md) only with the actual frozen-head evidence.

## Acceptance-to-proof map

| S1B1 close condition | Primary proof |
|---|---|
| Public registration creates only pending Student and no session (BR-001/008) | PostgreSQL + HTTP integration |
| Password rules, Argon2id, privacy-preserving required screening (BR-002) | Unit + PostgreSQL integration + exposure guard |
| Hidden Account outcomes do not enumerate (BR-001/003) | HTTP security integration |
| BR-105 display name and original email preservation | Unit + PostgreSQL integration + bilingual UI |
| Digest-only, expiring, supersedable, single-use verification (BR-008) | PostgreSQL concurrency integration |
| Source/evidence/protected delivery/outbox atomicity (BR-120/122) | Forced-rollback integration |
| Layered limiter and safe outage behavior (FR-014) | Redis/local-fallback integration |
| Responsive Arabic/English admission UI | lint + typecheck + production build + browser evidence |
