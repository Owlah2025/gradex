# Gradex Independent Full Repository Review

- Reviewer: ox-alpha (independent; no fixes applied)
- Date: 2026-08-24
- Branch: `ui-antigravity-20260817`
- Head at review start: `c7e1a02` (plus a large protected dirty worktree, treated as evidence, not modified)
- Method: static review of backend (Go/Gin/pgx/Postgres/Redis), frontend (Next.js App Router/React/TS), deployment tree (`deploy/hostinger`, `deploy/compose`, `deploy/scripts`, `deploy/monitoring`), plus build/vet/test execution and `npm audit`. No code or test files were modified.

---

## 1. Executive Summary

1. **Any Critical vulnerabilities?** — VERIFIED SAFE (none found).
2. **Any High vulnerabilities?** — One **High infrastructure** finding (unencrypted, host-only backups — INF-01). Zero High exploitable application-code findings.
3. **Can an unauthenticated user access paid content?** — Protected lesson media/playback/downloads: **no** (server-side entitlement evaluation verified on every path). Public preview video of a course under emergency access-suspension or retirement **can still be delivered** to anyone with the course ID (MED-01) — preview assets only, never protected content.
4. **Can a Student escalate privileges?** — VERIFIED SAFE (deny-by-default central policy, per-request principal resolution; no student-reachable admin capability found).
5. **Can an Instructor escalate to Admin?** — VERIFIED SAFE (role taken only from stored invitation; inviter authority re-verified at completion; capability model denies cross-role).
6. **Can one Student access another Student's private data/content?** — NOT FULLY VERIFIED exhaustively, but every traced route derives student identity from the session, not client IDs; playback/download tokens are HMAC-bound to the student and re-evaluated. No counter-example found.
7. **Can invitation/reset tokens be replayed?** — VERIFIED SAFE (digest-only storage, terminal-state triggers, `FOR UPDATE` serialization, purpose separation, TTL enforced at preview and completion).
8. **Can revoked/expired Course access be bypassed?** — VERIFIED SAFE for protected content (entitlement re-evaluated at read, issuance, manifest refresh, and inside write transactions). MED-01 is the preview-only exception.
9. **Is production capable of accidentally enabling development/test bypasses?** — VERIFIED SAFE for the audited seams: `AUTH_FAKE_MODE` refused in production config *and* staff-composition precondition; scanner `DEVELOPMENT_NO_OP` refused outside development; registration gated by LG-011/LG-021 approvals; wildcard CORS refused; placeholder token secrets refused.
10. **Are any real secrets tracked in git?** — VERIFIED SAFE (pattern sweep found placeholders/dev-local values only). Personal contact email addresses are committed in ~15 files (INFO-03).
11. **Database-integrity risks creating duplicate access/payment state?** — VERIFIED SAFE: partial unique indexes back one-active-entitlement-per-(student,course), one-non-terminal invitation/pair, one active purchase request per (course,email); purchase confirmation idempotent under `FOR UPDATE`.
12. **Are backups/monitoring sufficient for paid beta?** — **NO** as-is. Backups run hourly, are integrity-checked and restorable, but are unencrypted and never leave the host (INF-01); monitoring does not detect worker-down, disk-full, or email failure at runtime (MED-04/MED-05).
13. **What exactly must be fixed before the first paying Student?** — See §4 (P0): backup encryption/offsite copy, the KNOWN-BASELINE expiry-scan 500 (operational prerequisite for payment confirmation without pre-set expiry), and the preview takedown gap if emergency suspension is relied upon before launch.

Answers use VERIFIED SAFE / VULNERABLE / NOT FULLY VERIFIED as marked.

---

## 2. Paid-Beta Readiness Verdict

**B — SAFE FOR LIMITED PAID BETA AFTER P0 FIXES.**

The application core — identity, sessions, CSRF, entitlements, purchase/invitation flows, course lifecycle, media pipeline, outbox/email — is unusually well hardened and survived adversarial tracing without an exploitable High finding. What blocks immediate paid beta:

1. Backups are unencrypted and co-located with the database (total-loss and confidentiality exposure disproportionate to a paid platform holding PII + payment state) — INF-01.
2. The known NULL `default_access_ends_at` scan turns payment confirmation into a 500 whenever an Admin confirms before setting an expiry — operational breakage in the exact money→access path (KNOWN-BASELINE-01; workaround exists: set expiry first).
3. Emergency suspension / retirement does not retract anonymous preview delivery (MED-01) — matters if takedown capability is part of launch operations.
4. Monitoring cannot detect worker-down/disk-full/email-failure at runtime (MED-04/MED-05) — paid beta with background workers and transactional email needs this.

Identity, paid access, data integrity, and production configuration were each independently traced and hold. Media and email are strong. Infrastructure hygiene is good except backups/HSTS/cert-renewal.

---

## 3. Finding Counts

| Severity | New | Known baseline | Defense-in-depth |
|---|---|---|---|
| Critical | 0 | 0 | 0 |
| High | 1 | 0 | 0 |
| Medium | 5 | 0 | 1 |
| Low | 6 | 0 | 2 |
| Info | 5 | 1 | 0 |

New exploitable: 0 Critical, 0 High application-code; 1 High infrastructural (backup exposure).
Known baseline: 1 (expiry-scan 500 — reconfirmed, no added exploitability).

---

## 4. P0 — Before Any Paid Student

1. **INF-01** — Encrypt backups and replicate off-host/offsite; add failure alerting already partially present but storage exposure must close.
2. **KNOWN-BASELINE-01** — Apply the already-designed fix (nullable scan → 409 `ErrExpiryRequired`) or enforce a hard operational rule that no course is confirmed without an expiry; today it breaks the exact payment-confirmation step with a 500.
3. **MED-04/MED-05** — Wire worker-down, disk-full, and email-failure detection into the runtime monitor (rules exist declaratively; nothing evaluates them).

## 5. P1 — Before Broader Launch

1. **MED-01** — Add `access_suspended_at IS NULL AND retired_at IS NULL` (ideally reuse `catalogpublic.PublishedOnly`) to `issuePreview`.
2. **MED-02** — Add rate limiting to `POST /media/playback-authorizations` (+ manifest route), matching FR-017/BR-102.
3. **MED-03** — HSTS + baseline security headers at the Caddy edge.
4. **MED-06** — Redis TLS certificate auto-renewal or monitoring of expiry before the 90-day cliff.
5. **LOW-03** — Gate manual invitations/approvals on retirement/suspension mirroring `PublishedOnly`.

## 6. P2 — Post-Beta Priority

1. **MED-07** — Stuck-state recovery for media versions wedged in SCANNING/PROCESSING after worker crash.
2. **LOW-01/LOW-02** — Fail-closed recent-auth guard shape; extend authorization matrix sweep to media/academic routes.
3. **LOW-04** — Point `DeleteCourse` guard at real access tables; map FK conflict to 409.
4. **LOW-05** — Quarantine-prefix lifecycle cleanup for abandoned uploads.
5. **LOW-07** — Narrow `TRUSTED_PROXIES`; detach worker from edge network.
6. **LOW-08** — Unconditional requirement for `OUTBOX_PROTECTED_PAYLOAD_KEY` outside development.

## 7. P3 — Hardening

- INFO-02 (single HMAC secret blast radius), LOW-06 (manual rollback after failed health), INFO-03 (personal emails in tracked files), INFO-04 (nanoid advisory via build chain), INFO-05 (s12 env generator emits approval flags), DEF-01 (invitation token mirrored into sessionStorage).

---

## 8. Detailed Findings

### INF-01 — Backups unencrypted and never leave the host

Severity: High
Confidence: HIGH
Category: destructive operational risk / data confidentiality
Status: NEW

Affected components: `deploy/hostinger/host.sh` (backup section ~L279–329), `$S12_BACKUP_DIR` = `/var/lib/gradex/backups`.

Evidence:
- `pg_dump --format=custom` written locally, chmod 600/700, sha256 sidecar, flock-serialized, schema-clean invariant checked pre/post. All good — but no encryption (no age/gpg/Restic), no rsync/object-copy offsite anywhere in the repo (R2 is used for media only).

Attack/failure path: any code-execution as the operator user (docker group member) silently reads every retained backup = full PII + entitlement history. Host or disk loss destroys all copies simultaneously.

Preconditions: host compromise, operator-user compromise, or physical/disk failure.

Impact: catastrophic data loss window (hourly cadence mitigates but single-host storage negates DR value); bulk PII disclosure.

Why existing mitigations are insufficient: file permissions do not survive a compromised operator account; checksums prove integrity, not confidentiality.

Reproduction/verification: read `host.sh` backup function; grep repo for offsite transfer tooling (none).

Recommended remediation: encrypt at rest (age/Restic with key held off-host), push to object storage, retain N generations, keep existing restore-verification flow pointed at the encrypted artifact.

Recommended regression test: verify-restore script decrypts from the remote artifact and passes row-count assertions.

Launch blocker: YES (for paid students).

### MED-01 — Public preview endpoint ignores emergency access-suspension and retirement

Severity: Medium
Confidence: HIGH
Category: access control / lifecycle predicate divergence
Status: NEW

Affected components: `backend/internal/media/delivery.go` `issuePreview` (~L583–617, consumed by `IssuePreview` L556 and `IssueCoursePreview` L567); mounted anonymously at `backend/internal/httpapi/media_delivery_handlers.go:70–71` (`GET /api/v1/media/courses/:courseID/preview`, `GET /api/v1/media/previews/:id`) behind IP rate limiting only.

Evidence:
- SQL requires `c.lifecycle = 'PUBLISHED' AND cr.state='APPROVED'`, scan PASSED, kind/visibility/lineage — but **not** `c.access_suspended_at IS NULL` nor `c.retired_at IS NULL`.
- Canonical public predicate `catalogpublic.PublishedOnly` (`internal/catalogpublic/visibility.go:14–20`) includes both conditions; catalogue list/detail/search, purchase request creation and confirmation all inherit them.
- Verified directly against source during this review (the doc comment above the query claims "retirement state" is proven; only `media_assets.retired_at` is checked, which is media-asset retirement, not course retirement).

Attack/failure path: Admin suspends course access (e.g. `MALWARE`/`LEGAL` cause) or retires a published course → catalogue hides it and protected delivery stops, but anyone holding the public course ID fetches a fresh signed URL for its preview video indefinitely.

Preconditions: knowledge of the course ID/slug (public while listed; cached/shared afterwards).

Impact: takedown/emergency-control gap limited to preview assets. Protected lesson media unaffected (evaluator checks both columns — verified).

Why existing mitigations are insufficient: rate limiting throttles volume, not authorization correctness.

Reproduction/verification: static comparison of predicates; suspend a course via `catalog/suspension.go` then call the preview route (read-only analysis; not executed against shared systems).

Recommended remediation: reuse `PublishedOnly(c, cr)` inside `issuePreview`.

Recommended regression test: integration test asserting 404-style uniform refusal for suspended and retired courses.

Launch blocker: NO (but P1 if suspension is used operationally at launch).

### MED-02 — Duplicate playback-authorization route bypasses FR-017/BR-102 quotas

Severity: Medium
Confidence: HIGH
Category: business-logic abuse / resource exhaustion
Status: NEW

Affected components: `backend/internal/httpapi/router.go` mounts `POST /api/v1/media/playback-authorizations` via `mountMediaDeliveryRoutes` (`media_delivery_handlers.go:63–69`); handler calls `IssuePlayback` directly. Contrast `learning_handlers.go:652–667` where the `/learn` issuance route applies `learning-playback-source` (600/10min per source) and `learning-playback` (30/10min per student), both fail-closed.

Evidence: `downloadAuthorization` and `materialEntry` on the same handler apply `allowProtectedMaterialDownload`; `coursePreview` applies `allowPublicPreview`; `playbackAuthorization` alone has no limiter.

Attack/failure path: authenticated entitled student scripts unlimited playback-session minting + manifest refreshes, entirely outside the documented quota; amplifies signed-URL generation load.

Preconditions: valid session + active entitlement (entitlement enforcement itself remains intact on this route).

Impact: quota-policy violation, scripted-extraction amplification, avoidable signing/storage egress load. Not unauthorized access.

Why existing mitigations are insufficient: entitlement check ≠ quota; per-source ceilings never engage on this route.

Recommended remediation: attach the same two-tier policy (or route the UI through the `/learn` issuance endpoint exclusively).

Recommended regression test: mirror `learning_playback_rate_limit_test.go` for the duplicate route.

Launch blocker: NO.

### MED-03 — No HSTS or baseline security headers at the edge

Severity: Medium
Confidence: HIGH
Category: transport hardening
Status: NEW

Affected components: `deploy/hostinger/Caddyfile` (site block sets only `encode` and `Cache-Control "private, no-store"` on `@api`); `deploy/compose/Caddyfile` similar scope; frontend sets no headers beyond removing X-Powered-By.

Evidence: repo-wide grep finds `Strict-Transport-Security` nowhere; CSP absent; backend sends `nosniff` on sensitive responses individually.

Impact: first-request/downgrade MITM can strip HTTPS before HSTS could ever help — because HSTS is never sent. Absence of CSP weakens defense-in-depth for a site rendering instructor/user content (XSS sinks currently absent — see §10 — so this is layered defense, not exploitation).

Recommended remediation: `header Strict-Transport-Security "max-age=31536000; includeSubDomains"` + `X-Content-Type-Options: nosniff` globally; consider a conservative CSP after auditing inline-script usage.

Launch blocker: NO (P1).

### MED-04 — Runtime monitor does not evaluate worker-down or log alerts

Severity: Medium
Confidence: HIGH
Category: monitoring gap
Status: NEW

Affected components: `deploy/monitoring/rules.yml` declares `worker_process` and `log_alerts` (`worker_failure`, 5xx, `transactional_email_undelivered`); `deploy/hostinger/host.sh` `run_monitor` (~L249–256) exports only HEALTH/READY/BACKUP variables to `deploy/monitoring/monitor-once.sh`, which implements healthz/readyz/backup-staleness/webhook only.

Evidence: no evaluator exists for `platform_process` or `log_alerts` rules anywhere in the deploy tree.

Impact: worker crash, sustained 5xx, or undelivered transactional email (payment/invitation/reset mails!) persist until a human notices. Email failure directly stalls the manual-payment→access pipeline.

Recommended remediation: implement the declared rules in `monitor-once.sh` (process check via systemd status; email staleness via outbox oldest-unclaimed age) or delete the rules so the design matches reality.

Launch blocker: NO technically, but strongly recommended pre-paid-beta given email-driven payment flow.

### MED-05 — Disk exhaustion undetectable

Severity: Medium
Confidence: HIGH
Category: monitoring gap
Status: NEW

Affected components: `deploy/monitoring/*`, `deploy/hostinger/host.sh`.

Evidence: no disk-space check in rules.yml, monitor-once.sh, or audit-host.sh (audit runs at install time only).

Impact: disk full degrades Postgres/WAL/backups first; surfaces only indirectly as backup or DB failures.

Recommended remediation: add a threshold check (e.g. >85% used → webhook) to monitor-once.sh.

Launch blocker: NO.

### MED-06 — Internal Redis TLS certificates expire in 90 days with no renewal path

Severity: Medium
Confidence: HIGH
Category: availability time bomb
Status: NEW

Affected components: `deploy/hostinger/host.sh:115` (`openssl ... -days 90` for CA and server cert); `prepare_redis_tls` regenerates only when files absent.

Evidence: no cron/timer/monitor references cert expiry; Redis runs `--port 0 --tls-port 6379`, requirepass enforced.

Failure path: day ~91 the API/worker lose Redis (rate limiter fails closed → protected endpoints start refusing; queue unavailable). Detected only as generic downtime.

Impact: self-inflicted outage of rate limiting + queue + session-supporting infrastructure on a predictable date.

Recommended remediation: longer-lived internal CA + monitored renewal, or a monthly timer regenerating server cert signed by a long-lived CA.

Launch blocker: NO (P1 due to predictability).

### MED-07 (defense-in-depth flagged Medium) — Worker crash strands media versions in SCANNING/PROCESSING unrecoverably

Severity: Medium
Confidence: MEDIUM-HIGH
Category: availability / durability
Status: NEW

Affected components: `backend/internal/media/worker.go` (`beginScan`, `beginTranscode` claim-commit precedes work); `internal/media/service.go:934` (`Retry` accepts only `SCAN_FAILED|SCAN_ERROR|PROCESS_FAILED`); no lease/reaper/janitor exists.

Failure path: OOM/crash between claim-commit and completion → asynq retries see non-retryable current state and return vacuously; after MaxRetry(3) the task is dropped; Admin Retry refuses the stuck states; asset permanently non-deliverable (fails safe — never delivers unscanned bytes).

Impact: instructor lesson content stuck pending manual DB surgery. Availability only.

Recommended remediation: lease timeout moving `SCANNING→SCAN_ERROR`, `PROCESSING→PROCESS_FAILED` (making them Retry-eligible), or widen `Retry`.

Launch blocker: NO.

### KNOWN-BASELINE-01 — NULL `default_access_ends_at` scan → 500 during payment confirmation

Severity: Low–Medium (correctness/error contract; operationally blocking when hit)
Confidence: HIGH
Category: error handling
Status: KNOWN-BASELINE

Affected components: `backend/internal/access/purchase.go:331–344` (`ConfirmPurchaseRequest`) scanning nullable `timestamptz` into `time.Time`; surfaced as `problem.Internal("")` from `access_routes.go` default branch.

Verification during this review: reconfirmed in source. Additional sweep found **no other** value-type scans of nullable timestamps (all other sites use pointers; parallel `ApproveInvitation` path handles NULL correctly → 409). No added exploitability: transaction rolls back, request stays `WAITING_PAYMENT`, retryable after expiry set. Publication validation requires price but not expiry, so the trigger is realistic.

Launch blocker: YES as an operational prerequisite (see P0).

### LOW-01 — Recent-auth guard fails open if session context value is absent

Severity: Low (defense-in-depth defect; not currently reachable)
Confidence: HIGH
Status: NEW (defense-in-depth)

Affected components: `backend/internal/httpapi/access_routes.go:854–870` and duplicated inline at :653–666. Missing/mistyped `authenticated_session` context → `AuthenticatedAt.IsZero()` → guard returns true. Contrast staff handlers failing closed (`staff_handlers.go:473–480`). Production authenticator always sets the value (`internal/auth/session.go:72`).

Recommended remediation: return refusal when the context value is absent, matching staff shape.

Launch blocker: NO.

### LOW-02 — Authorization matrix sweep omits media and academic groups

Severity: Low (test gap)
Confidence: HIGH
Status: NEW

Affected components: `backend/internal/httpapi/authorization_test.go:299–455` (`expectedRouteMatrix`): no `/api/v1/media/*`, `/admin/academic/*`, `/authoring/academic/*`, `/me/academic-*`, or `/catalog` entries, so restricted/suspended sweeps never exercise those groups. Compensating coverage exists (`media_router_test.go:177` ordering proof; academic integration tests), but none assert suspended/restricted denial on those groups.

Recommended remediation: add the groups to the matrix.

Launch blocker: NO.

### LOW-03 — Manual invitation grant skips retirement/suspension gating ("RETIRED" case dead)

Severity: Low
Confidence: HIGH
Status: NEW

Affected components: `backend/internal/access/repository.go` `CreateInvitation` (:178–186, existence-only lock) and `ApproveInvitation` (:788–795, `switch lifecycle { case "ARCHIVED","DELISTED","RETIRED": refuse }` — `RETIRED` unreachable since retirement is `retired_at` with lifecycle unchanged; `access_suspended_at` never checked).

Impact: ACTIVE entitlement can be minted for a retired/suspended course — invariant pollution, misleading grant emails/outbox events. Downstream evaluator denies bytes (`ReasonRetired` / `CourseSuspended`) — verified — so no content leak.

Recommended remediation: mirror `PublishedOnly` conditions in both methods.

Launch blocker: NO.

### LOW-04 — DeleteCourse guards legacy fixture table; FK violation surfaces as 500

Severity: Low
Confidence: HIGH
Status: NEW

Affected components: `backend/internal/catalog/course.go` `DeleteCourse`: pre-check queries `fake_entitlements`; real FKs (`entitlements`, `enrollments`, `purchase_requests`, `course_access_invitations`) still prevent deletion, but the raw FK error falls through `handleLifecycleError`'s default branch → 500 instead of intended 409 `ErrCourseHasAccess`. No data loss.

Launch blocker: NO.

### LOW-05 — Abandoned presigned PUTs accumulate in quarantine/

Severity: Low
Confidence: HIGH
Status: NEW

Affected components: `backend/internal/storage/storage.go` `PresignPutURL` (no content-length constraint), `internal/media/service.go` (no lifecycle cleanup for abandoned uploads; `DeletePrefix` used only for failed HLS output).

Impact: cost/DoS by an authenticated instructor in SCANNER/TRUSTED_INSTRUCTOR mode; private bucket, so confidentiality intact.

Launch blocker: NO.

### LOW-06 — Failed post-release health leaves stack down; rollback is manual

Severity: Low
Confidence: HIGH
Status: NEW

Affected components: `deploy/hostinger/host.sh:451–494` (`apply_release` recreates containers, dies on failed health; previous images remain tagged locally so recovery is fast but manual). Schema forward-only guard prevents most bad cases.

Launch blocker: NO.

### LOW-07 — Broad trusted-proxy range; worker attached to edge network

Severity: Low
Confidence: HIGH (setting verified; exploit conditional)
Status: NEW

Affected components: `deploy/hostinger/compose.yml:6` `TRUSTED_PROXIES=172.16.0.0/12`; worker container joins the non-internal `edge` network.

Impact: any compromised/side-loaded container on `edge` can forge `X-Forwarded-For` toward rate limiting/admission decisions. Not reachable off-host (only Caddy publishes ports; verified).

Recommended remediation: pin Caddy's specific IP; move worker to the internal app network only.

Launch blocker: NO.

### LOW-08 — Outbox payload key required only when student registration enabled

Severity: Low (deployment guard compensates)
Confidence: HIGH
Status: NEW (defense-in-depth)

Affected components: `backend/internal/config/config.go:775–798` enforces `OUTBOX_PROTECTED_PAYLOAD_KEY(+_VERSION)` only under `STUDENT_REGISTRATION_ENABLED=true`; `deploy/hostinger/host.sh:53–58` requires it unconditionally and compose interpolates `:?`, but a bare-process production API could start without it while staff-invitation protected payloads need sealing.

Launch blocker: NO.

### INFO-01 — Admin invitation list exposes action-secret UUID (not secret)

Severity: Info
Confidence: HIGH
Status: NEW

`ListAdminInvitations` populates `action_secret_id` (UUID identifier only; digest/token never serialized). Route is ADMIN-gated. Student projections exclude it plus `admin_note`/`decision_reason`. Cosmetic over-fetching.

### INFO-02 — Single HMAC secret serves three MAC purposes

Severity: Info
Confidence: HIGH
Status: NEW

`cmd/api/main.go:393` uses `PlaybackTokenSecret` for playback sessions, admin-review sessions, buyer tags — domain-separated strings (`gradex:s4:playback-session:v2\x00`, etc.) make cross-protocol forgery infeasible; noted as single-secret blast-radius only.

### INFO-03 — Personal Gmail committed as privacy/support/security contact

Severity: Info
Confidence: HIGH
Status: NEW

~15 tracked files (runtime.env.example:14–16, compose files, verify-staging-smoke.sh:122–124, legal pages). Not a credential; harvestable PII rendered publicly.

### INFO-04 — nanoid <3.3.8 advisory via build-time chain

Severity: Info
Confidence: MEDIUM
Status: NEW

`npm audit --omit=dev`: 2 high findings, all rooted in `nanoid` (GHSA-2v37-7h3g-55p8 — infinite loop with zero-size generators) reached via postcss/tailwind build chain under Next; no runtime attack path identified in this app's usage. Upgrade when Next ships fixed postcss line.

Dependency vulnerability verification: PARTIAL — `govulncheck` not installed (not installed merely for this audit per instructions); Go CVE verification INCOMPLETE. `npm audit` executed successfully.

### INFO-05 — Disposable s12 env generator emits password-screening approval flags

Severity: Info
Confidence: HIGH
Status: NEW

Worktree diff of `deploy/scripts/environment.sh` adds `PASSWORD_SCREEN_MODE=adapter` + `COMPROMISED_PASSWORD_ADAPTER_APPROVED=true` to fresh s12 envs (gitignored/disposable; production defaults remain `unavailable`/`false`; config validates adapter-without-approval in production). Risk limited to copying generated env across environments.

### DEF-01 — Course-access invitation acceptance token mirrored into sessionStorage

Severity: Low (defense-in-depth observation)
Confidence: HIGH
Status: DEFENSE-IN-DEPTH

`frontend/src/lib/identity/validation.ts:140–150` persists the one-time acceptance token in sessionStorage alongside the invitation ID until consumed/released. Staff-invitation bearer stays memory-only. XSS would expose it — but no XSS sink exists today (verified §9/§10). Consider memory-only like the staff flow.

### FALSE-POSITIVE-REJECTED entries — see §10.

---

## 9. Verified Security Controls

Application core:

- Central deny-by-default authorization (`identity/policy.go:104–160`), per-request principal resolution from DB (`resolver.go:31–70`); suspension precedes role checks and revokes sessions + bumps epoch (`suspension.go:139–148`); self-suspension and last-admin-lockout guarded.
- Dual-layer course ownership: HTTP middleware (`catalog_ownership.go:27–66`) + per-transaction owner/status/parent-child locks at 10+ sites in `catalog/authoring.go`; revision/section/lesson identifiers joined through their owning course inside transactions.
- Entitlement authority evaluated at read, issuance, manifest refresh, and again inside write transactions (`learning_handlers.go:642–650, 727–734`; `media/delivery.go:249, 288, 446`); enrollment is history, never authorization.
- Playback/download tokens: HMAC-SHA256 constant-time, bound to student + exact asset version, purpose-separated; segments presigned ≤ remaining session; suspended/expired/revoked denied mid-stream.
- Purchase confirmation idempotent under `FOR UPDATE`; partial unique indexes: `entitlements_one_active_student_course` (0012), `cai_one_non_terminal_per_pair` (0015), `purchase_requests_one_active_course_email` (0021), `enr_one_per_student_course` (0013); 8-way concurrency tests assert exactly-one outcomes (`grant_concurrency_integration_test.go`).
- Money: BIGINT minor units, `KWD` CHECK, non-negative CHECK, server-derived snapshot at request creation; confirmation ignores later price changes.
- Staff invitations: 32-byte CSPRNG, SHA-256 digest-only storage, 7-day TTL enforced twice, supersede chains + terminal-state triggers, role from stored invitation only, inviter re-verified at completion, existing-account takeover refused, no session issued on completion, preview non-enumerating.
- Password reset: digest-only purpose-scoped secrets, supersedes prior under account lock, uniform non-enumerating responses with timing floor, atomic consume+credential swap+session-family revocation+epoch bump, HIBP screening, Argon2id.
- Session cookies: Secure, HttpOnly, SameSite=Strict (`auth/session_response.go:60,74`); logout clears hardened cookie.
- CSRF: exact-match Origin/Referer against PUBLIC_ORIGIN (https scheme enforced) + CSRF header constant-time compared, on admission and every mutation group (`admission_security.go:48–67`, `session_security.go:18–38`). Anonymous admission boundary additionally signed-cookie bound.
- Test backdoors: `AUTH_FAKE_MODE` refused in production config (`config.go:1175–1176`) and staff composition precondition (`cmd/api/main.go:716–723`); scanner `DEVELOPMENT_NO_OP` double-gated to development; default scanner mode UNAVAILABLE fails closed; registration gated LG-011/LG-021; sandbox sender domains refused; TAP test environment refused.
- Config fail-closed: required secrets always (DB, S3 keys, playback token); REDIS_PASSWORD, SESSION_CSRF_KEY, https PUBLIC_ORIGIN, resend-only email required outside development; wildcard CORS rejected; placeholder token secrets rejected; unknown enum values abort startup; `config.Secret` redacts through formatters (dedicated tests).
- Media pipeline: server-derived object keys, exact-version provenance join before any delivery, quarantine sole gateway with exhaustive transition table, byte-level format proof (MP4 ftyp/PDF/OOXML structural incl. macro & zip-bomb rejection, entry-name traversal), full SHA-256 recomputation, BR-068 caps at begin + completion + aggregate-under-advisory-lock, idempotent completions keyed on provider event receipts, DEVELOPMENT_NO_OP unreachable in production, D-088 scanner-gated public previews, manifest rewriting rejecting absolute URLs/query strings/tag-URIs/traversal.
- Rate limiting: fail-closed policies with local fallback limits; Redis mandatory outside development.
- Uniform denials (fixed-byte 404s) resist inventory oracles; `no-store` on sensitive responses.
- Frontend: `no-store` on all API fetches including server-side requests (`lib/api/http.ts`, `learning-server-request.ts`); RSC boundary discipline (lesson page explicitly refuses dictionary crossing — GAP-04 comment); fragment-borne tokens scrubbed from URL; `safeReturnTo` origin-relative validation rejecting protocol-relative/backslash/control chars/blocked roots (`identity/return-to.ts`); zero HTML injection sinks in src (only tests asserting absence); no tokens in localStorage; `force-dynamic` + `revalidate = 0` on learn pages; no error.tsx leaking internals (errors handled in-page).
- Reporting (D-065): report context is server-minted, encrypted, bound to reporter+session+exact revision, verified per submission; inertness/absence-of-moderation tested (t062/t068/t069 suites).
- Infrastructure: only edge publishes ports; app network `internal: true`; audit-host fails if backend ports exposed; non-root systemd operators with hardening flags; release provenance via OCI revision labels, `.partial`+`mv` atomicity, forward-only schema guard on rollback; secrets scanned in logs *and* frontend bundle by verify-edge-security.sh; Redis TLS-only + requirepass asserted; MinIO anonymous policy none; Dockerfiles multi-stage, non-root, no secret layers, `.env` excluded from build contexts.

---

## 10. Rejected Candidate Findings

| Candidate | Rejected because |
|---|---|
| SQL injection via dynamic fragments (`targetPredicate` in `issuePreview`, visibility predicates) | Predicates are compile-time string constants; attacker values remain bind parameters repo-wide (agents swept ILIKE/filter composition). |
| `dangerouslySetInnerHTML` XSS | Zero occurrences in `frontend/src`; only test files asserting their absence. |
| Tokens in localStorage | None; locale preference only. Invitation tokens: fragment-carried, scrubbed after capture (DEF-01 notes sessionStorage mirror). |
| Open redirect via `returnTo` | `safeReturnTo` rejects protocol-relative, backslash, control chars, oversized, and blocked-root paths; parses against opaque base. |
| AUTH_FAKE_MODE reachable in production | Refused by config validation and staff-composition precondition; fake authenticator constructed only when flag true, which production startup forbids. |
| CORS wildcard with credentials | `config.go:1149–1172` rejects `*` with credentials and requires https origins in production. |
| Malware-scanner bypass via TRUSTED_INSTRUCTOR mode | Mode selection is config-validated; trusted path writes distinct provenance and `requireProcessingProvenance` enforces evidence before READY; D-088 keeps public previews scanner-gated regardless. |
| Playback token replay across admin/student routes | Purpose-separated HMAC domains; bearer identity claim verified against authenticated account per fetch. |
| Duplicate payment confirmation double-grant | Idempotent branch returns stored invitation; unique indexes + `FOR UPDATE`; race suite covers concurrent approve/cancel/accept. |
| Price forgery | No client-owned price anywhere; snapshot server-side; zero price legal by design. |
| Progress manipulation unlocking content | Completion derived server-side (≥0.9 of trusted duration), monotonic via GREATEST/COALESCE, final in-tx entitlement evaluation. |
| E2E seed endpoints in prod binary | Proof/e2e binaries excluded from default runtime image (Dockerfile target separation, verified). |

---

## 11. Authentication Review

Sessions: opaque credential digest cookie (Secure/HttpOnly/SameSite=Strict), server-side session records resolved per request; login commits before hardened cookie issuance and clears anonymous cookie (tested); logout clears hardened cookie; password change/reset revoke session families + bump epoch. Login rate-limited per source + per email identifier, fail-closed; liveness preflight defeats CPU exhaustion while locked revalidation stays authoritative; uniform failures resist enumeration. Password hashing Argon2id with HIBP screening (adapter mode required outside development). Email verification and registration gated by policy approvals in production. Fake authentication impossible in production (double gate). Recent-auth (15 min step-up) enforced on elevated staff/elevated-access operations — with the LOW-01 fail-open shape noted.

Verdict: strong; no exploitable weakness found.

## 12. Authorization Review

See agent matrix summarized in §9 and finding LOW-01/LOW-02. Every privileged group runs capability middleware backed by fresh DB principals; ownership re-verified in-domain; batch endpoints that bypass per-item checks do not exist. Instructor↔instructor isolation holds at both layers. Suspended staff deny everywhere including self-service mutations.

## 13. Student Access / Entitlements

Expired courses keep read-only history by design; materials/report contexts withheld unless ReadActive; SECTION grants cannot widen dashboard. Resume drops inactive courses. Preview exception: MED-01.

## 14. Purchase / Invitation

Flow verified end-to-end sound except KNOWN-BASELINE-01 (500 instead of 409) and LOW-03 (grant-path gating divergence). Response-shape symmetry prevents purchase-request existence oracle (F-6 candidate rejected as Info).

## 15. Staff Identity

Verified controls in §9; consumed-invitation frontend behavior change (worktree diff) correctly hides the form for non-PENDING previews — server remained the authority either way; no server flaw introduced or hidden.

## 16. Course Lifecycle

Transition graph enforced in-domain with expected-state locks; archive terminal; stale-revision publish blocked via `based_on_revision_id == live_revision_id` under `FOR UPDATE`; append-only triggers protect price history, adjustments, outbox, action-secrets. Public-predicate consistency holds everywhere except MED-01 (preview) and LOW-03 (manual grant).

## 17. Media

Upload/playback/downloads hardened as detailed in §9; gaps MED-02 (route quota bypass), MED-07 (stuck states), LOW-05 (quarantine accumulation), INFO-02 (shared HMAC secret).

## 18. Email / Outbox

Outbox: AES-G sealed protected payloads with forbidden-key screening, append-only trigger, dispatch receipts idempotent, Postgres source of truth survives queue outage; atomicity with authority state verified for purchase/grant/staff flows. Renderer builds links from configured PUBLIC_ORIGIN only; credentials travel in URL fragments (#token=), never query strings. Resend sender: JSON body (no header injection), Authorization via Secret wrapper, redirect following deliberately disabled to avoid auth-header replay; preflight decouples sendability checks from runtime deps. Production fails closed (resend-only, API key required). Mailpit/fake providers confined to development. Gap: MED-04 (email-failure alerting unwired).

## 19. Frontend / RSC / Privacy

No remaining T7-class leak found: learn pages pass narrow props + locale labels only, with explicit GAP-04 documentation; dictionaries never cross the boundary; personalized fetches force-dynamic/no-store. Over-fetching: minor (INFO-01). Privacy classification: MED-01 affects public preview availability, not personal data; no cross-user PII visibility found on traced routes; admin lists clamp pagination (max 100, default 20).

## 20. Database / Transactions / Concurrency

Constraints, locking, and atomicity verified as in §9/§14; nullable-scan sweep clean beyond KNOWN-BASELINE-01; Kuwait date semantics respected per T8A (exclusive end-of-day conversion; intentional past-instant allowance under BR-026, audited + revision-checked).

## 21. Infrastructure / Docker / Deployment

Strong posture (§9); findings INF-01, MED-03, MED-06, LOW-06, LOW-07, LOW-08, INFO-03, INFO-05. Images version-pinned; no `latest`; healthchecks present except edge/worker.

## 22. Backup / Monitoring

Backup mechanics excellent (isolation, checksums, schema invariants, row-count restore assertions); confidentiality/location insufficient (INF-01). Monitoring: healthz/readyz/backup-staleness wired; worker/disk/email/log-alerts declared but unevaluated (MED-04/MED-05).

## 23. Test / E2E Integrity

Backend: `go build ./...`, `go vet ./...`, `go vet -tags=integration ./...` clean; `go test ./...` exit 0 (full package run during this review). Frontend: `tsc --noEmit` clean; `npm test` executed (legal-content check + node --test suite). Playwright suite not rerun wholesale (per instructions); baseline 164/1/3 acknowledged; `s5-playback-performance.spec.ts:157` treated as historical environment-dependent failure absent new evidence. Authorization-matrix test gap: LOW-02. No hardcoded accepted failures found in reviewed specs; SY-07 canonical command exists (`test:e2e:canonical`).

## 24. Dependency Audit

- npm audit (production deps): 2 high findings, nanoid-rooted build chain (INFO-04). No runtime exploit path identified.
- govulncheck: not installed → Go dependency CVE verification: INCOMPLETE.
- No CVEs asserted from memory.

## 25. Performance / Capacity Risks

CURRENT LAUNCH RISK: none identified beyond MED-02 amplification (bounded once quotas applied). FUTURE SCALE: lifecycle directory capped at 50 rows/read (good); admin lists clamped ≤100; media authorization joins stay indexed via live-graph membership (static inspection); outbox polling standard at-least-once. No N+1 patterns surfaced in reviewed hot paths.

## 26. Coverage Matrix

| Area | Reviewed? | Depth | Findings | Confidence |
|---|---|---|---|---|
| Auth | Yes | Deep (source-traced) | 0 new (LOW-01 adjacent) | High |
| Authorization | Yes | Deep, route-by-route | LOW-01, LOW-02 | High |
| Student access | Yes | Deep | 0 new | High |
| Entitlements | Yes | Deep + concurrency suite read | LOW-03 | High |
| Purchase | Yes | Deep | KNOWN-BASELINE-01 reconfirmed | High |
| Course lifecycle | Yes | Deep | LOW-04, MED-01 (delivery side) | High |
| Staff invitation | Yes | Deep | 0 new | High |
| Password recovery | Yes | Source-traced | 0 | High |
| Academic catalogue | Yes | Predicate/route level (not line-by-line) | 0 | Medium-High |
| Reporting | Yes | Control verification (D-065 suites) | 0 | Medium-High |
| Media upload | Yes | Deep | LOW-05, MED-07 | High |
| Media playback | Yes | Deep | MED-01, MED-02 | High |
| Downloads | Yes | Deep | 0 | High |
| Email | Yes | Sender/renderer traced | 0 (alerting gap in MED-04) | High |
| Outbox | Yes | Writer/dispatcher traced | 0 (LOW-08 config gate) | High |
| Database | Yes | Migrations + constraints | 0 | High |
| Frontend/RSC | Yes | Sinks/state/caching swept | DEF-01 | High |
| Infrastructure | Yes | Full deploy tree | INF-01, MED-03, MED-06, LOW-06/07/08, INFO-03/05 | High |
| Docker | Yes | Dockerfiles + compose | 0 | High |
| Deployment | Yes | Scripts + systemd | LOW-06 | High |
| Backups | Yes | Scripts traced | INF-01 | High |
| Monitoring | Yes | Rules vs implementation | MED-04, MED-05 | High |
| Tests/E2E | Partial | Build/vet/unit/typecheck run; Playwright not rerun | LOW-02 | Medium-High |
| Dependencies | Partial | npm audit done; govulncheck unavailable | INFO-04 | Medium |

Not fully verified: exhaustive cross-student enumeration of every learn route (spot-verified, architecture consistent); Go CVE scan; live E2E rerun; runtime reproduction of MED-01/02 (static proof only, kept off shared systems).

## 27. Recommended Remediation Order

1. INF-01 (backup encryption + offsite) → 2. KNOWN-BASELINE-01 fix or expiry-set rule → 3. MED-04/05 monitoring wiring → 4. MED-01 preview predicate → 5. MED-02 quota on duplicate route → 6. MED-03 HSTS/headers → 7. MED-06 cert renewal → 8. LOW-03 grant-path gating → 9. remainder of P2/P3.

## 28. Repository Safety Confirmation

- Only file created by this review: `docs/reviews/2026-08-24-ox-alpha-full-repository-review.md` (plus this directory).
- No source, test, doc, or configuration files modified; no commits; no resets/cleans/checkouts; no volumes or retained databases touched; no processes killed; no secrets printed (all values redacted).
- Final `git diff --check` output recorded below in the terminal summary.
