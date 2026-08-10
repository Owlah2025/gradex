# Gradex Launch Reality Audit — read-only, frozen HEAD `afe1624`

> Read-only repository reality audit performed 2026-08-10 against
> `/var/tmp/gradex-launch-integration`, branch `launch-integration-20260810`,
> frozen HEAD `afe1624d4cdb117c57aed3fc86594e5ebdb4074b`.
> No repository file was modified. See §19.

---

## 1. Audit verdict

**REPOSITORY REQUIRES AUTHORITY RECONCILIATION BEFORE MORE IMPLEMENTATION**

Every commit on `launch-integration-20260810` (24 commits, `18fb7e0..afe1624`) is unreviewed, and the two most recent remediations (staff composition decoupling, Admin Catalog review wiring) carry no Product Owner decision at all — `docs/DECISIONS.md` stops at D-080, which covers only authoring and password-change. `STATUS.md` still declares S6 the active slice at 13/85 tasks while `specs/006/tasks.md` shows 80/85 and the code is fully implemented; `AGENTS.md`/`CLAUDE.md` still name S1B3 (D-036, ~Aug 1) as the current slice. No technical safety stop exists — the tree is clean, no secret is exposed, and no authorization rule was weakened — but the map is wrong in enough places that the next implementation step cannot be chosen from the documents as they stand.

---

## 2. Repository identity

| | |
|---|---|
| Worktree | `/var/tmp/gradex-launch-integration` |
| Branch | `launch-integration-20260810` |
| Exact HEAD | `afe1624d4cdb117c57aed3fc86594e5ebdb4074b` |
| Cleanliness | `git status --short` empty — confirmed clean |
| Max schema version | **17** (`db.MaxSchemaVersion = EmailActivationSchemaVersion`), migrations `0001`–`0017` contiguous, up/down pairs complete |
| API required schema | `14` (`ProtectedLearningSchemaVersion`) — readiness accepts 14–17 |
| Worker required schema | `16` (`TransactionalEmailSchemaVersion`) when email enabled |
| CI schema assertion | Derived from `go run ./cmd/migrate max-version` — no stale literal (the S1B2 defect class is closed) |

### Migration inventory (ordering verified contiguous)

```
0001_init                      0010_revision_integrity        0015_course_access_grant
0002_identity_bootstrap        0011_catalog_search            0016_transactional_email
0003_audit_events              0012_media_and_entitlement     0017_transactional_email_activation
0004_sessions                  0013_enrollments
0005_student_admission         0014_protected_learning
0006_authenticated_sessions
0007_password_recovery
0008_staff_lifecycle
0009_course_authoring
```

### Backend runtime modules (`backend/internal`)

`access`, `auth`, `catalog`, `catalogpublic`, `config`, `db`, `email`, `entitlement`, `health`, `httpapi`, `identity`, `learning`, `logging`, `media`, `outbox`, `problem`, `queue`, `ratelimit`, `requestid`, `storage`

Entrypoints: `cmd/api`, `cmd/worker`, `cmd/migrate`, `cmd/bootstrap-admin`

### Mounted HTTP routes (read from real router composition, not tests)

**health** (outside `/api/v1`, no session/CSRF/auth)
- `GET /healthz`
- `GET /readyz`

**identity / admission** (`mountAdmissionRoutes*`, gated on `cfg.Admission().Enabled()`)
- `GET /api/v1/session/bootstrap` *(only when no session foundation)*
- `GET /api/v1/registration-policy-set`
- `POST /api/v1/student-registrations`
- `POST /api/v1/email-verification-requests`
- `POST /api/v1/email-verifications`
- `POST /api/v1/password-reset-requests`
- `POST /api/v1/password-resets`

**sessions / password lifecycle** (`mountSessionRoutes`, gated on `cfg.Sessions().Enabled()`)
- `GET /api/v1/session/bootstrap`
- `POST /api/v1/sessions`
- `GET /api/v1/session`
- `POST /api/v1/session-renewals`
- `DELETE /api/v1/session`
- `POST /api/v1/password-changes` — `requireAuth` + `requireCapability(CapPasswordChange)`

**staff** (`mountStaffRoutes`, gated on `EnvDevelopment && Sessions().Enabled()`)
- `GET /api/v1/staff-invitations/preview` *(anonymous, bearer header)*
- `POST /api/v1/staff-invitation-completions` *(anonymous)*
- `GET /api/v1/staff-invitations` — `CapAdminOperations`
- `POST /api/v1/staff-invitations` — session mutation security + `CapAdminOperations`
- `DELETE /api/v1/staff-invitations/:id`
- `POST /api/v1/accounts/:id/suspension` — `CapSecurityOperations`
- `DELETE /api/v1/accounts/:id/suspension`

**catalog / authoring** (`mountCatalogRoutes`)
- `GET /api/v1/courses`
- `GET /api/v1/taxonomy/terms`
- `POST /api/v1/courses`
- `GET /api/v1/courses/:id`
- `PUT /api/v1/courses/:id/candidate`
- `PATCH /api/v1/courses/:id/revisions/:revisionId`
- `POST|PATCH|DELETE .../revisions/:revisionId/sections[/:sectionId]`
- `POST|PATCH|DELETE .../revisions/:revisionId/sections/:sectionId/lessons` and `.../lessons/:lessonId`
- `PUT .../revisions/:revisionId/lessons/:lessonId/video`
- `PUT|DELETE .../revisions/:revisionId/lessons/:lessonId/files`
- `PUT|DELETE .../revisions/:revisionId/preview`
- `POST .../revisions/:revisionId/submit`

**review (Admin)**
- `GET /api/v1/admin/review/queue`
- `GET /api/v1/admin/review/courses/:id/revisions/:revisionId`
- `POST /api/v1/admin/review/courses/:id/revisions/:revisionId/approve`
- `POST /api/v1/admin/review/courses/:id/revisions/:revisionId/request-changes`
- `POST /api/v1/admin/review/courses/:id/revisions/:revisionId/preview/:lessonId`

**Admin pricing / lifecycle / taxonomy**
- `GET .../price-history`; `PUT .../price`; `PUT .../sections/:sectionId/price`
- `POST .../delist|relist|retire|archive`; `DELETE` (course); `POST .../owner`
- `POST|DELETE .../access-suspension`
- `PUT /api/v1/admin/courses/:id/taxonomy`
- `POST|PATCH|POST(:id/retire)|DELETE /api/v1/admin/taxonomy/terms[/:id]`

**public catalogue**
- `GET /api/v1/catalog/courses`
- `GET /api/v1/catalog/courses/:idOrSlug`

**media**
- `POST /api/v1/media/uploads` — `CapContentManagement` + `RoleInstructor`
- `POST /api/v1/media/uploads/:id/completions`
- `GET /api/v1/media/assets/:id` — `CapContentManagement`
- `POST /api/v1/media/assets/:id/retries` — Admin
- `POST /api/v1/media/catalogue-loads` + `/:id/completions` — Admin
- `POST /api/v1/media/assets/:id/out-of-band-scan-evidence` — Admin
- `POST /api/v1/media/playback-authorizations`
- `GET /api/v1/media/playback-manifests/:playbackSession/index.m3u8`
- `POST /api/v1/media/download-authorizations`
- `GET /api/v1/media/lessons/:lessonId/materials/resource|lab-material`
- `GET /api/v1/media/previews/:id`

**Course Access Invitation** (`mountAccessRoutes`)
- `PUT /api/v1/admin/courses/:id/default-access-expiry`
- `POST /api/v1/admin/course-access-invitations`
- `POST /api/v1/admin/course-access-invitations/:id/approve|reject|cancel|resend`
- `GET /api/v1/admin/course-access-invitations`
- `GET /api/v1/admin/entitlements/:id`
- `GET /api/v1/me/course-access-invitations[/:id]`
- `GET /api/v1/me/course-access`
- `POST /api/v1/me/course-access-invitations/:id/accept`

**protected learning**
- `GET /api/v1/learn/dashboard`
- `GET /api/v1/learn/courses/:courseId`
- `GET /api/v1/learn/courses/:courseId/lessons/:lessonId`
- `POST /api/v1/learn/lessons/:lessonId/playback`
- `PUT /api/v1/learn/lessons/:lessonId/progress`
- `POST /api/v1/learn/reports`

### Frontend App Router pages (`frontend/src/app`)

| Route | Class |
|---|---|
| `/` | PUBLIC |
| `/[locale]/catalog`, `/[locale]/catalog/[idOrSlug]` | PUBLIC |
| `/ar/privacy`, `/en/privacy`, `/ar/terms`, `/en/terms` | LEGAL |
| `/login`, `/register`, `/onboard`, `/verify-email`, `/verify-email/result`, `/recover`, `/recover/reset`, `/password-change` | IDENTITY |
| `/[locale]/learn/dashboard`, `/[locale]/learn/courses/[courseId]`, `/[locale]/learn/courses/[courseId]/lessons/[lessonId]`, `/[locale]/access` | STUDENT |
| `/[locale]/instructor/courses`, `/instructor/courses` | INSTRUCTOR |
| `/[locale]/admin/catalog`, `/[locale]/admin/course-access`, `/staff` | ADMIN |
| `/staff/accept` | OTHER (invitation acceptance, anonymous bearer) |
| `/layout.tsx` | OTHER (root shell) |

### Configuration gates observed

- `AUTH_FAKE_MODE` — refused when `APP_ENV=production` (`config.go:1033`)
- `MEDIA_SCANNER_MODE` — `UNAVAILABLE` (default) or `DEVELOPMENT_NO_OP`; the latter refused unless `APP_ENV=development` (`config.go:990`)
- `MEDIA_OPERATING_MODE` — `SCANNER` or `ADMIN_CATALOGUE`
- `EMAIL_PROVIDER` — production requires `resend` + non-empty API key
- `PASSWORD_SCREEN_MODE` — `deterministic` development-only; production registration requires `adapter` + `COMPROMISED_PASSWORD_ADAPTER_APPROVED=true` (LG-021)
- `PUBLIC_ORIGIN` — must be an HTTPS origin in production; CORS origins must be HTTPS and non-wildcard; credentialed wildcard CORS refused in every environment
- `PLAYBACK_TOKEN_SECRET` — rejects the `changeme` example placeholder
- Legal — `LEGAL_IDENTITY_MODE` public rejects both staging sentinels; registration number, registered address, and three contact emails required outside development
- Staff foundation — composed only when `APP_ENV=development`

### Storage / media operating modes

Private object storage via S3-compatible client with exact-version addressing (`storage.go:114,200,215,246` set `input.VersionId`). Two operating modes: `SCANNER` (Instructor `BeginUpload` permitted, worker scans and transcodes) and `ADMIN_CATALOGUE` (Instructor `BeginUpload` returns `ErrNotAuthorized`; Admin catalogue-load path plus immutable out-of-band scan evidence).

### Transactional email composition

Producer writes an encrypted outbox row co-committed with the domain transaction → `email_deliveries` ledger discovery joins on `safe_payload->>'template_contract'` and an activation boundary → worker `runEmailDispatcher` polls every 500 ms, batch 25 → repository-owned Arabic/English renderer → `Sender` interface → `FakeSender` (dev/test) or `ResendSender` (production, redirect-refusing). Eight contracts, listed in §5 Journey H.

### Scanner composition

`media.NewConfiguredScanner(mode, appEnv)` → `DevelopmentScanner` (identity `development-no-op-scanner`, inspects nothing, development only) or `UnavailableScanner` (default, fail-closed). **No real provider adapter exists.**

### Authentication / session composition

Opaque server-managed cookie session, generation-bound CSRF re-derived from server key material on every resolve, role-scoped idle/absolute windows, family revocation on confirmed reuse, PostgreSQL-authoritative principal resolution on every protected request (`identity.NewDBPrincipalResolver`), Redis used only for rate limiting and queueing.

---

## 3. Authority chain

**Controlling, current:**

| Decision | Effect |
|---|---|
| D-045 (2026-07-28) | MVP ships **no in-platform payments**. S7 struck. Course access = Admin-approved Course Access Invitation. |
| D-046 (2026-07-29) | External Course community link deferred to S18. No slice renders it before launch. |
| D-040 | 2026-08-15 hard public go-live target (supersedes D-039's September move). |
| D-058 / D-072 | S4 closed, S5 closed, on independent approvals. |
| D-073, D-074 | S6 authority; Antigravity built, Claude reviewed. |
| D-075, D-076 | LG-021 (HIBP) and LG-011 (legal package) resolved. |
| D-077, D-078 | Resend behind provider-neutral durable boundary; email activation boundary never sends historical intents. |
| **D-079** (2026-08-10) | Instructor authoring UI wired to real APIs; `MEDIA_SCANNER_MODE=DEVELOPMENT_NO_OP` development-only seam. |
| **D-080** (2026-08-10) | Mandatory password change mounted; CHANGE_REQUIRED no longer terminal. |

**Superseded / historical only:** D-027 (every Entitlement originates from an Order), D-028 (Coupon reservation), D-030 (earnings snapshot at Order completion), D-038/D-039 (September target), D-032/D-033/D-035/D-036/D-037 (per-slice S1x seat assignments — all expired with their slices).

**Authority gaps found:**

1. **`AGENTS.md` and `CLAUDE.md` are stale.** Both declare D-036 / S1B3 the active slice and the current seat assignment. S1B3 closed on 2026-08-01. These are the first files an agent reads and they point nine slices back.
2. **No decision authorizes the last four commits.** `1afe40f` (staff composition) and `23e35bb`/`a00a97a`/`afe1624` (Admin Catalog) landed with evidence files but no D-081/D-082 equivalent. D-079 and D-080 set the precedent that founder-manual remediations get a recorded decision; these two did not.
3. **`.specify/feature.json` points at `specs/010-transactional-email`.** S9's 30 tasks are all checked. The selector does not reflect the active launch-integration work — which has no spec directory at all.
4. **Evidence directory naming is arbitrary.** Authoring + Admin Catalog remediation filed under `evidence/s12/` (infrastructure), password-change + staff under `evidence/s13/` (security gate), E2E spec named `s14-admin-catalog-review.spec.ts`. None of these slices own that work.

---

## 4. Slice completion matrix

| ID | Name | Spec dir | Tasks | Impl head / range | Independent review | Real state |
|---|---|---|---|---|---|---|
| S0 | Delivery foundation | — (daily record) | n/a | `4d4bbe8..7bd4d84` | recorded | **CLOSED** |
| S1A | Bootstrap + Admin security core | `002-auth-rbac` | part of 38 | `70b4809` | APPROVE | **CLOSED** |
| S1B1 | Student admission | `002-auth-rbac` | part of 38 | `ad1b8f6` | APPROVE WITH FINDINGS | **CLOSED** |
| S1B2 | Authenticated sessions | `002-auth-rbac` | part of 38 | `7d8710e` | APPROVE ×2 | **CLOSED** |
| S1B3 | Recovery + Student integration | `002-auth-rbac` | part of 38 | `9d3db91` | APPROVE 0/0/0/0 | **CLOSED** |
| S1C | Staff lifecycle + authz matrix | `002-auth-rbac/s1c` | 38/38 | — | recorded | **CLOSED (dev-only in production composition — see §7)** |
| S2 | Course authoring and review | `003-course-authoring` | 64/64 | `785d71c` | recorded | **CLOSED**, but see Journey D — the Admin review *UI* half was never built |
| S3 | Public catalogue | `004-public-catalogue` | 48/48 | — | recorded | **CLOSED** |
| S4 | Media + Entitlement evaluation | `005-media-and-entitlement-evaluation` | 32/32 | `944c0a7` | D7+D8 approved | **CLOSED** |
| S5 | Protected learning | `007-protected-learning` | 78/78 | `41373a8` | Tier 3 APPROVE | **CLOSED** (F-1 Medium open as tracked follow-up) |
| S6 | Course Access Invitation + grant | `006-course-access-grant` | **80/85** | `d9e483f..681f4a9` + later | **REJECT** on last recorded range | **IMPLEMENTED_UNREVIEWED** — code complete and correct on inspection; no approving verdict exists |
| ~~S7~~ | Payments/refunds | — | — | — | — | **DEFERRED_BY_MVP** (D-045) |
| S8 | Admin support ops + Instructor roster | — | no spec dir | — | — | **NOT_STARTED** |
| S9 | Transactional email | `010-transactional-email` | 30/30 | `c531fc5..381bd40` | **REJECT** at `9be0020`; remediation `9be0020..381bd40` unreviewed | **IMPLEMENTED_UNREVIEWED** |
| S10 | Bilingual legal/support pages | — (D-076 package) | — | `/ar|/en/privacy`,`/terms` live | folded into S11 | **PARTIAL** — policy pages exist; `/about`, `/teach`, `/contact` are 404 |
| S11 | End-to-end integration / release acceptance | `009-release-acceptance` | 29/29 | `7cf0fa1` | APPROVED WITH NON-BLOCKING FINDINGS | **CLOSED** — but its evidence predates 3 merges and 2 migrations |
| S12 | Production infrastructure | `008-staging-production-infrastructure` | **46/48** | frozen `91ab1e3` | not dispatched (T048 unchecked) | **BLOCKED_EXTERNAL** — T047 needs KVM/R2/domain |
| S13 | Security and quality gate | — | no spec dir | — | — | **NOT_STARTED** (name reused for evidence filing) |
| S14 | Staging acceptance + gate audit | — | no spec dir | — | — | **NOT_STARTED** (name reused for an E2E filename) |
| S15 | Production rehearsal / soft launch | — | — | — | — | **NOT_STARTED** |
| S16 | Public go/no-go | — | — | — | — | **NOT_STARTED** |
| — | **Launch-integration remediation** (authoring, password-change, staff, Admin Catalog) | **none** | **none** | `18fb7e0..afe1624` | **none** | **IMPLEMENTED_UNREVIEWED + SPEC/TASK GAP** |

**Discrepancy:** S6's five unchecked tasks are `T013` (Enrollment create-or-reuse), `T016`/`T024`/`T032` (mutation checks), `T075` (bilingual/RTL sweep). `T013` is **implemented** — `internal/access/repository.go:816-826` performs `INSERT … ON CONFLICT (student_account_id, course_id) DO UPDATE` inside the grant transaction. The checkbox is stale bookkeeping, not missing work.

`specs/001-coupons-system/` has spec, plan, research, data-model, quickstart and contracts but **no tasks.md** — deferred commerce scope, never converted to work.

---

## 5. MVP journey matrix

| Journey | Classification | Basis |
|---|---|---|
| **A — Admin bootstrap / identity** | **COMPLETE_BUT_UNREVIEWED** | `POST /api/v1/password-changes` mounted behind `CapPasswordChange` (`router.go:283-306`); `auth/session_response.go:26` exposes `password_change_required`; `/password-change` page + `password-change-guard.tsx` + login redirect; integration test asserts `true→false` transition and old-password rejection. CSRF derivation defect fixed per D-080. |
| **B — Staff / Instructor onboarding** | **PARTIAL (development only)** | Dev: composed and working. **Production: impossible.** `cmd/api/main.go:716` composes staff only when `cfg.Environment() == config.EnvDevelopment`; `buildStaffFoundation:535` hard-errors `"production staff admission composition remains unavailable pending launch approval"`. Every `/api/v1/staff-invitations*` and `/accounts/:id/suspension` route is absent in production. |
| **C — Instructor authoring** | **PARTIAL** | Studio wired to real APIs (`lib/api/authoring.ts` → `/courses`, sections, lessons, `PUT …/video`, `POST …/submit`); real upload via `POST /media/uploads` → presigned PUT → completions. Ownership, revision binding, submission validation enforced server-side. **Blocked in production**: `MEDIA_SCANNER_MODE` defaults to `UNAVAILABLE` → `UnavailableScanner` → no Asset Version can reach `READY`; `DEVELOPMENT_NO_OP` is refused unless `APP_ENV=development` (`config.go:990`). `deploy/env/production-like.env.example` sets `MEDIA_OPERATING_MODE=ADMIN_CATALOGUE`, which makes `Service.BeginUpload` return `ErrNotAuthorized` (`service.go:134`) — Instructor upload is refused outright in that topology. |
| **D — Admin Course review** | **PARTIAL** | Queue is now real (`listReviewQueue` → `GET /admin/review/queue`), real Course/revision IDs, approve/request-changes hit real routes. **The submitted-revision inspector does not exist**: `getReviewCourseRevision` is exported in `lib/api/review.ts:73` and referenced **only** from `review.test.ts`. **No client exists at all** for `POST /admin/review/courses/:id/revisions/:revisionId/preview/:lessonId`. "Open" sets `administeredCourseID` and reveals taxonomy/lifecycle/pricing controls — administration, not inspection. An Admin today approves a revision without seeing its bilingual titles, descriptions, sections, lessons, media state, or any video. |
| **E — Public catalogue** | **PARTIAL** | `GET /catalog/courses`, `/catalog/courses/:idOrSlug` mounted; repository constructed `catalogpublic.PublishedOnly` so draft/submitted revisions are invisible. **But** the landing page `/` ships six fabricated Courses with fabricated instructors and KWD prices from `src/data/courses.ts`, and every landing CTA points at `/courses` and `/dashboard` (`nav-items.ts:17-22`) — neither route exists, no rewrite, no middleware → 404. |
| **F — Course Access Invitation** | **COMPLETE_BUT_UNREVIEWED** | Full route set mounted (`access_routes.go:91-145`). Grant transaction verified by inspection: locks Course `FOR SHARE`, requires `default_access_ends_at` future-dated, requires role `STUDENT`, `FOR UPDATE` on existing ACTIVE entitlement, enrollment create-or-reuse, one entitlement with `grant_source='MANUAL_INVITATION'` + `source_invitation_id`, two audit events, outbox co-commit, all in one transaction. Idempotency: step 2 returns the pre-existing entitlement for the same invitation; DB constraints `entitlements_one_active_student_course` and `enr_one_per_student_course` are the second line. Acceptance creates nothing. No approving review verdict exists. |
| **G — Protected learning** | **COMPLETE_AND_REVIEWED (at S5 head) / NEEDS REGRESSION (at current HEAD)** | S5 Tier 3 APPROVE at `41373a8`. Playback authorization, HLS manifest, progress, report-context signer, runtime revalidation all present with integration tests (`learning_runtime_revalidation_integration_test.go`). No re-proof since three merges and migrations 0015–0017. |
| **H — Transactional email** | **COMPLETE_BUT_UNREVIEWED** | All eight contracts emitted and renderable: `student-email-verification-v1`, `account-password-reset-v1`, `account-password-reset-completed-v1`, `staff-invitation-v1`, `course-access-invitation-v1`, `course-access-granted-v1`, `course-access-invitation-rejected-v1`, `course-access-invitation-cancelled-v1`. Producer → encrypted outbox → `email_deliveries` ledger (discovery joins on `safe_payload->>'template_contract'`) → worker `runEmailDispatcher` → `Sender` → Resend adapter. Production refuses `EMAIL_PROVIDER=fake` and requires an API key. No commerce/marketing contract added. **Live proof is `onboarding@resend.dev` only** — no verified production sender domain. |
| **I — Password reset** | **COMPLETE_BUT_UNREVIEWED** | `POST /password-reset-requests` + `/password-resets` behind admission security and a dedicated stricter rate policy (the only anonymous route reaching Argon2id). Non-enumerating, digest-only, single-use, all-family revocation + epoch advance. Closed under S1B3 review; unchanged since. |
| **J — Operational readiness** | **SOFTWARE COMPLETE / LIVE EXTERNAL PROOF MISSING** | `/healthz`, `/readyz` correct (`health.go:17-18`). Compose, Caddyfile, monitoring rules, alert sink, restore drill, rollback scripts, staging smoke all exist under `deploy/`. Evidence records schema `15|false` — two migrations stale. Hostinger deployment, public DNS/TLS, live R2, external alert delivery all unexecuted. |

---

## 6. What is actually complete

Defensible at `afe1624`:

- Delivery foundation: typed fail-closed config, structured logging with field allowlist, request IDs, RFC 9457 across `/api/v1`, `/healthz` + `/readyz`, migrations under `cmd/migrate`, CI with schema/doc/secret guards.
- Identity: bootstrap Admin, restricted CHANGE_REQUIRED principal, **mandatory password change now reachable end to end in a browser**, login/logout, opaque rotating server-managed sessions with family reuse detection, generation-bound CSRF, password recovery, HIBP screening.
- Catalog: authoring (Course/revision/Section/Lesson/video/submit), taxonomy vocabulary + assignment, lifecycle, pricing, review backend (queue, revision graph, approve, request-changes, lesson preview).
- Media: upload intent → private quarantine PUT → completion → scan → ffmpeg transcode → READY → exact-version provenance; protected delivery with signed manifests.
- Entitlement evaluation (S4) and Course Access grant (S6) — one entitlement, one enrollment, provenance-bearing, idempotent, audited, outbox co-committed.
- Protected learning: dashboard, course home, lesson, playback authorization, progress, reports, runtime revalidation, two rate ceilings.
- Transactional email: eight contracts, encrypted outbox, delivery/attempt ledger, worker dispatch, retry/idempotency, activation boundary (D-078), Resend adapter refusing redirects.
- Public catalogue: published-only listing and detail.
- Legal: `/ar|/en/privacy`, `/terms` from the approved package; production rejects staging sentinels.

---

## 7. Incomplete software features

| # | Feature | Exact gap |
|---|---|---|
| 1 | **Production staff composition** | `cmd/api/main.go:716` gates on `EnvDevelopment`; `:535` hard-errors. No Instructor can be invited, onboarded, suspended, or reinstated in production. |
| 2 | **Admin submitted-revision inspector** | `getReviewCourseRevision` client exists, unused by any component. No UI renders bilingual titles/descriptions, study year, Major, Subject, Sections, Lessons, or media state before approval. |
| 3 | **Admin Lesson video preview** | `POST /admin/review/courses/:id/revisions/:revisionId/preview/:lessonId` served; **no frontend client function exists**, no UI. |
| 4 | **Production malware scanner** | Only `UnavailableScanner` and `DevelopmentScanner` exist. LG-014 has selected no provider. Production upload → READY is impossible. |
| 5 | **Landing page fake catalogue** | `src/data/courses.ts` — six fabricated Courses with prices, rendered by `FeaturedCourses` on `/`. |
| 6 | **Landing page fabricated testimonials** | `src/data/testimonials.ts`, whose own header comment says *"do not ship fabricated reviews"*. Rendered on `/`. |
| 7 | **Broken public routes** | `routes.courses = "/courses"`, `routes.dashboard = "/dashboard"`, footer `/about`, `/teach`, `/contact` — five 404s reachable from the landing page. Real routes are `/[locale]/catalog` and `/[locale]/learn/dashboard`. |
| 8 | **FAQ describes deferred commerce** | `src/data/faq.ts:22` — *"Payment is completed on Tap's hosted checkout"*; `:44` — *"purchase access again through the normal checkout"*. Contradicts D-045. |
| 9 | **Copy promises deferred community** | `dictionaries/en.ts:347,376,387,418` promise a course community; footer links a Discord social icon. D-046 defers this to S18. |
| 10 | **`DeleteCourse` guards on the wrong table** | `internal/catalog/course.go:285` checks `fake_entitlements`, not `entitlements`. Real access records are invisible to the guard. Impact is bounded — `entitlements.course_id` and `enrollments.course_id` both `REFERENCES courses(id)`, so the transaction rolls back on FK violation — but the Admin gets an opaque 500 instead of `ErrCourseHasAccess`. Same defect class D-079 fixed in `DBAssetVersionValidator`. |
| 11 | **Hard-coded `https://gradex.com`** | `src/config/site.ts:4` drives `metadataBase` and every OpenGraph URL. Named as an S11 non-blocking follow-up; still present. |
| 12 | **S8/S13/S14 have no spec directories** | Admin support operations, Instructor roster, security gate, staging acceptance — no spec, no tasks, no implementation. |

---

## 8. External production blockers

| Item | Classification | Detail |
|---|---|---|
| Hostinger KVM 2 | **EXTERNAL_BLOCKER** | T047 unchecked. `deploy/hostinger/` package frozen and complete; never executed on real hardware. |
| Cloudflare R2 | **EXTERNAL_BLOCKER** | Provider test compiles; `evidence/s12/provider-staging.md:54` — *"Live execution is pending real R2 credentials."* R2 must return usable `x-amz-version-id` and must never substitute current bytes for a requested historical version, or the S4 provenance contract blocks its use. Code side is ready (`storage.go:114,200,215,246`). |
| Domain / DNS / TLS | **EXTERNAL_BLOCKER** | No real domain. Production config *requires* an HTTPS `PUBLIC_ORIGIN` and HTTPS CORS origins, so the software gate is closed correctly. |
| Resend production sender | **SOFTWARE_COMPLETE_EXTERNAL_PROOF_PENDING** | Adapter done, production refuses `fake`. Proven only against `onboarding@resend.dev`. SPF/DKIM/DMARC on a real sender domain unproven (LG-018 OPEN). |
| Legal identity values | **EXTERNAL_BLOCKER** | `LEGAL_REGISTRATION_NUMBER`, `LEGAL_REGISTERED_ADDRESS` required outside development; public mode rejects both staging sentinels (`config.go` `validateLegalIdentityMode`). Real values do not exist. |
| Malware scanner LG-014 | **SOFTWARE_BLOCKER + EXTERNAL_BLOCKER** | External: no provider selected. Software: no adapter for any real provider exists, so selection alone will not unblock it. |
| Monitoring / alerts | **SOFTWARE_COMPLETE_EXTERNAL_PROOF_PENDING** | `deploy/monitoring/rules.yml`, `disposable-alert-sink.py`, `monitor-once.sh` present. No delivered alert to a real external destination. |
| Backup / isolated restore | **SOFTWARE_COMPLETE_EXTERNAL_PROOF_PENDING** | `database-recovery.sh`, `verify-restored-database.sh`, `evidence/s12/restore-drill.md` present. Never run against provider infrastructure. |
| Application rollback | **SOFTWARE_COMPLETE_EXTERNAL_PROOF_PENDING** | `application-rollback.sh` + verifier; non-destructive schema posture. Local only. |
| Production staff composition | **SOFTWARE_BLOCKER** | Not external. A hard-coded environment gate. |
| LG-003/004/005/006/012/013/015/016/019/020 | **EXTERNAL_BLOCKER** | Counsel, accountant, prices, community owner, accessibility audit, operating envelope, Instructor agreement — all OPEN, none owned in practice under D-041. |

**Launch gate register state:** LG-011 and LG-021 `RESOLVED`. LG-001/002/007/008/009/010/017 `DEFERRED` under D-045. All remaining ten `OPEN`.

---

## 9. Deferred MVP functionality

All governed by **D-045** unless noted. None is an implementation defect.

| Feature | Governing decision | Slice status | Reachable UI? |
|---|---|---|---|
| Checkout, payment capture, Tap, KNET, Apple Pay | D-045 | S7 struck | **Yes — FAQ copy claims Tap hosted checkout exists.** See §10 item 5 |
| Cart | D-045 | never specified | No |
| Coupons | D-045 | `specs/001-coupons-system/` has spec+plan, **no tasks.md** | No |
| Refund automation | D-045, LG-002 DEFERRED | not built | No — footer correctly omits a Refund Policy link (Terms §8 carries it) |
| Payment webhooks | D-045, LG-010 DEFERRED | not built | No |
| Invoices / receipts | D-045, LG-016 still OPEN | not built | No |
| BNPL (Deema) | FF-001 | deferred | No |
| Instructor payouts / settlements | D-045, LG-001 DEFERRED, FF-004 | not built | No |
| Commerce reconciliation | D-045 | not built | No |
| Community / social | **D-046** — deferred to S18 | not built | **Yes — landing copy promises a course community; footer renders a Discord icon** |
| Built-in conferencing / office hours | SLICES §2 (S9 row) | deferred post-launch | No |
| Captions / transcripts | FF-003 | deferred | No |
| MFA / social login | FF-005 | deferred | No |
| Certificates | pre-launch scope decision | not built | No |

**Backend is clean** — zero hits for coupon/checkout/payment_attempt/refund in `backend/internal`. Both leaks are frontend marketing copy.

---

## 10. Production-reachable demo / fake audit

| # | File / area | Journey | Severity | Remediated at HEAD? |
|---|---|---|---|---|
| 1 | `frontend/src/data/courses.ts` → `components/sections/featured-courses.tsx` → `app/page.tsx` | Public landing (E) | **P1** | **No** |
| 2 | `frontend/src/data/testimonials.ts` → `components/sections/testimonials.tsx` → `/` | Public landing | **P1** — fabricated reviews attributed to named people; file header forbids shipping them | **No** |
| 3 | `frontend/src/components/layout/nav-items.ts:17-22` + `footer.tsx:15-19` | Public landing | **P1** — five 404 CTAs | **No** |
| 4 | `frontend/src/config/site.ts:4` `https://gradex.com` | All pages (metadata) | **P2** | **No** |
| 5 | `frontend/src/data/faq.ts:16-22,44` Tap hosted checkout | Public landing | **P1** — states a deferred capability as a live fact | **No** |
| 6 | `frontend/src/lib/i18n/dictionaries/{en,ar}.ts` community promises + footer Discord icon | Public landing | **P2** — violates D-046 | **No** |
| 7 | `backend/internal/catalog/course.go:285` `fake_entitlements` | Admin lifecycle delete | **P2** | **No** |
| 8 | `review-queue.tsx` `demo-course-1` fixture | Admin review (D) | was P0 | **Yes** — `23e35bb`; `admin-catalog-surface.test.ts` scans for reintroduction with a non-vacuity assertion |
| 9 | Instructor "Local Demo Drafts" | Authoring (C) | was P0 | **Yes** — `b68ede6`, `0e43410` (D-079) |
| 10 | `pricingCourseID` dual-meaning state | Admin review (D) | was P0 | **Yes** — `23e35bb`, `lib/admin/catalog-administration.ts` |

**Correctly excluded as legitimate seams, not defects:** `internal/auth/fake.go` (`AUTH_FAKE_MODE`, refused when `APP_ENV=production`, `config.go:1033`); `internal/email/fake.go` (production requires `EMAIL_PROVIDER=resend` + API key); `internal/entitlement/seed_nonprod.go` (`//go:build !production` — build-excluded, exactly as SLICES §3.1 requires); `internal/media/scan_development.go` (refused unless `APP_ENV=development`, in both config validation and constructor); all `e2e-*.ts` clients and `*_test.go`.

---

## 11. Manual-defect reconciliation

| # | Defect | Remediating commits | In HEAD? | Reviewed? | Regression evidence | Residual |
|---|---|---|---|---|---|---|
| 1 | Authoring used Local Demo / `course-demo` | `b68ede6`, `0e43410`, `4fb29b1`, `2a4008c` | **Yes** | **No** | Browser persistence E2E + unit | None on the demo path |
| 2 | CHANGE_REQUIRED had no HTTP/browser path | `5d605c2`, `2818bf1`, `a6d6070`, `5f21c61` | **Yes** | **No** | `session_routes_integration_test.go:440-521` + browser spec | None |
| 3 | `CompletePasswordChange` minted an incompatible CSRF credential | `5d605c2` (D-080 §1) | **Yes** | **No** | Integration test resolves the replacement session | None |
| 4 | Staff lifecycle gated on `STUDENT_REGISTRATION_ENABLED` | `1afe40f`, `5d0a933` | **Yes, for development** | **No** | `cmd/api/main_test.go`, `authorization_test.go` | **Production gate remains — see §7 item 1. No decision authorizes the fix or records the remaining stop.** |
| 5 | S9 isolated on another branch | `6e016b6` merge | **Yes** | **No** — the merge itself is unreviewed | none post-merge | Merge-tree behaviour unproven |
| 6 | Staff invitation producer lacked `template_contract` | `2e31edd` | **Yes** — `identity/invitation.go:257,263` | **No** — post-dates the S9 REJECT | acceptance test | None |
| 7 | Review queue used `demo-course-1` | `23e35bb` | **Yes** | **No** | `review.test.ts`, `admin-catalog-surface.test.ts`, E2E S14 A | None |
| 8 | `pricingCourseID` carried two meanings | `23e35bb` | **Yes** | **No** | `catalog-administration.test.ts`, E2E S14 B | None |
| 9 | Submission errors far from Submit | `049cfb2` | **Yes** — `data-testid="submit-error"`, `role="alert"`, scroll + focus | **No** | E2E S14 D | None |
| 10 | Admin Open cannot inspect submitted content | **none** | **Not remediated** | — | — | **Open. See §7 item 2.** The remediation's own evidence admits it. |
| 11 | Admin Lesson preview not surfaced | **none** | **Not remediated** | — | — | **Open. See §7 item 3.** |
| 12 | Media E2E stalled at `Processing` | — | — | — | — | **Reproducible current defect in that environment, cause not isolated.** |

**Item 12, determined rather than guessed.** `evidence/s12/admin-catalog-review-remediation.md` records the failure at pre-existing line 136, 4-minute budget, worker started and reported READY but picked up no job. The three candidate causes are all live at HEAD:

(a) `MEDIA_SCANNER_MODE` not set to `DEVELOPMENT_NO_OP` in the run environment — `playwright.media-authoring.config.ts` passes only `GRADEX_API_ORIGIN`, `PUBLIC_ORIGIN`, and two port variables to the web server and sets **nothing** on the API/worker processes, which are expected from `backend/` Compose where `.env.example:84` ships `MEDIA_SCANNER_MODE=UNAVAILABLE`;
(b) `MEDIA_OPERATING_MODE=ADMIN_CATALOGUE` in the same environment, which would make `BeginUpload` refuse before any job;
(c) a genuine dispatcher/enqueue defect.

(a) is the strongest hypothesis and is consistent with every observed symptom — worker healthy, zero jobs, asset stuck `Processing`. **Classification: reproducible current defect, most likely a test-environment configuration gap, but not proven.** `UNKNOWN_NEEDS_EVIDENCE` on the exact cause; the minimum proof is one run with the API and worker processes' resolved `MEDIA_SCANNER_MODE` and `MEDIA_OPERATING_MODE` captured.

---

## 12. Documentation contradictions

| File | Contradiction |
|---|---|
| `AGENTS.md`, `CLAUDE.md` | Both declare D-036 / S1B3 the active slice and current seat assignment. S1B3 closed 2026-08-01, nine slices ago. |
| `docs/launch/STATUS.md` | *"S6 … is the ACTIVE slice … 13 of 85 tasks are complete"* — `specs/006/tasks.md` shows **80/85** and the code is complete. |
| `docs/launch/STATUS.md` | Top block dated 2026-08-09, ends at S9 awaiting re-review. **No mention of `launch-integration-20260810` or any of its 24 commits.** Stale by one full working day. |
| `docs/launch/STATUS.md` | *"Current date: 2026-08-09"*, *"Days remaining: 6"*. Today is 2026-08-10; 5 days remain. |
| `docs/launch/STATUS.md` | Retains multi-page historical Red/Amber analysis and the retired September target inline with current statements, separated only by prose markers. |
| `docs/launch/STATUS.md` | *"Last repository reconciliation: 2026-08-07 … against head `681f4a9`"* — three merges and two migrations stale. |
| `docs/DECISIONS.md` | Ends at D-080. No decision covers the staff composition decoupling or the Admin Catalog remediation, both of which changed production composition and a launch surface. |
| `.specify/feature.json` | Selects `specs/010-transactional-email`; the active work has no spec directory. |
| `docs/launch/evidence/s12/provider-staging.md` | Records schema `15|false`. Current max is **17**. |
| `docs/launch/evidence/s12/`, `s13/` | Remediation evidence filed under slices that do not own the work; E2E named `s14-*` for a slice that does not exist. |
| `docs/launch/SLICES.md` §2 | S1C dated **Jul 27–28**, S2 **Jul 29–31**, i.e. *before* S1B3's Aug 1 close — an ordering the same table's `Depends on` column forbids. |
| `docs/launch/SLICES.md` §2 | S9 row reads *"folded into S4/S6"*; S9 in fact shipped as its own slice with its own spec directory and 30 tasks. |
| `docs/LAUNCH_GATES.md` | LG-012 (launch prices) *"due August 3"* — five days past, still OPEN. LG-014 *"validation by August 12"* with no provider shortlisted. LG-018 *"completion by August 12"* with no sender domain. |
| `docs/launch/PLAN.md` §8 | Criteria 1, 3, 5, 6 all currently fail. §8 says any failure is a no-go. No revision or reapproval has been recorded. |
| `frontend/src/data/faq.ts` | Describes Tap hosted checkout as live product behaviour, contradicting D-045. |

**Not found (checked, and clean):** obsolete health endpoint names — `/healthz` and `/readyz` are correct everywhere; stale CI schema literal — CI derives from `migrate max-version`.

---

## 13. Unreviewed implementation ranges

| # | Base | Head | Commits | Behaviour affected | Can an existing review cover it? |
|---|---|---|---|---|---|
| 1 | `18fb7e0` | `2a4008c` | 4 | Instructor authoring persistence, real MP4 upload, `DBAssetVersionValidator` fix, `storage_object_key` in the upload intent, `MEDIA_SCANNER_MODE` seam | No. D-079 explicitly states *"the range remains subject to independent review."* |
| 2 | `2a4008c` | `5f21c61` | 4 | `POST /password-changes`, `password_change_required`, `/password-change` screen + guard, CSRF derivation fix, `NewSessionFoundation` now requires a compromised-password source | No. D-080 states the same. |
| 3 | `5f21c61` | `5d0a933` | 2 | Production router composition — staff foundation decoupled from Student admission | No. No decision, no review. **Changes composition; must not be bundled with UI work.** |
| 4 | `9be0020` | `381bd40` | 5 | S9 remediation: `template_contract` discoverability, activation boundary (D-078), Resend redirect refusal, email activation schema admission | No. The recorded S9 verdict is **REJECT at `9be0020`**; these commits post-date it. |
| 5 | `6e016b6` | `6e016b6` | 1 (merge) | Integration of S9 into the launch line | No. Merge-tree behaviour has never been proven — and the only merged-tree E2E run failed. |
| 6 | `6e016b6` | `afe1624` | 4 | Admin Catalog real review API, pricing/taxonomy decoupling, submission-error visibility, `TestCatalogAdminReadRoutesDenyInstructor` | No. Evidence file states *"This slice is not reviewed. Claude authored it and must not review it."* |

**Recommended frozen review range:** `18fb7e0d..afe1624d` — the complete integrated branch, 24 commits, one reviewer, one verdict. Splitting it would leave the merge commit and the merged-tree interaction uncovered, which is where the only observed failure lives.

**Reviewer seat:** `agy` via `scripts/agy-review.sh 18fb7e0..afe1624`. Claude authored the entire range and must not review it. Per `CLAUDE.md`, a review producing no retrievable verdict is `UNAVAILABLE`, not approval.

---

## 14. Regression and evidence gaps

| Journey | Unit | Integration | E2E | Manual founder | Production-like | Live external | Independent review |
|---|---|---|---|---|---|---|---|
| A — bootstrap/identity | PRESENT_CURRENT_HEAD | PRESENT_CURRENT_HEAD | PRESENT_CURRENT_HEAD | PRESENT_CURRENT_HEAD | MISSING | MISSING | **MISSING** |
| B — staff onboarding | PRESENT_CURRENT_HEAD | PRESENT_CURRENT_HEAD (dev) | PRESENT_ANCESTOR | PRESENT (dev, Resend-delivered) | **MISSING — impossible** | MISSING | **MISSING** |
| C — authoring | PRESENT_CURRENT_HEAD | PRESENT_CURRENT_HEAD | **FAILED_CURRENT_HEAD** (stalls at `Processing`) | PRESENT | MISSING | MISSING | **MISSING** |
| D — Admin review | PRESENT_CURRENT_HEAD | PRESENT_CURRENT_HEAD | PARTIAL — S14 A–D pass; the submit→approve extension inside the media suite is **written but never observed passing** | PRESENT (defects found) | MISSING | MISSING | **MISSING** |
| E — public catalogue | PRESENT_CURRENT_HEAD | PRESENT_ANCESTOR | PRESENT_ANCESTOR | PRESENT | MISSING | MISSING | at S3 close only |
| F — Course Access | PRESENT_ANCESTOR | PRESENT_ANCESTOR | PRESENT_ANCESTOR | MISSING | MISSING | MISSING | **REJECT on the last recorded range** |
| G — protected learning | PRESENT_ANCESTOR | PRESENT_ANCESTOR | PRESENT_ANCESTOR | MISSING | MISSING | MISSING | APPROVE at `41373a8` only |
| H — transactional email | PRESENT_CURRENT_HEAD | PRESENT_CURRENT_HEAD | PRESENT_CURRENT_HEAD | PRESENT (`onboarding@resend.dev` → Gmail) | MISSING | PARTIAL — test sender only | **REJECT at `9be0020`**; remediation unreviewed |
| I — password reset | PRESENT_ANCESTOR | PRESENT_ANCESTOR | PRESENT_ANCESTOR | MISSING | MISSING | MISSING | APPROVE at S1B3 |
| J — operational | n/a | n/a | n/a | n/a | **STALE** (schema `15`, current `17`) | MISSING | MISSING |

**Minimum regression proof needed on the merged tree at `afe1624`:** one green `npm run test:e2e:media-authoring` (which requires the item-12 cause resolved), plus one green integrated run of the S6 access-grant and S5 protected-learning integration suites against a database migrated to 17. Journeys F, G, I currently rest entirely on ancestors that predate migrations 0015–0017 and three merges.

---

## 15. P0 / P1 / P2 / P3 gap matrix

| P | Slice | Feature / journey | Classification | Exact missing work | SW/Ext | Governing spec | Task IDs | Existing implementation | Existing evidence | Required evidence | Depends on | Next action |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **P0** | S2 | **Admin submitted-revision inspector (D)** | PARTIAL | A component that calls `getReviewCourseRevision(courseID, revisionID, locale)` on Open and renders the returned graph: bilingual title, bilingual description, study year, Major, Subject, ordered Sections, ordered Lessons, per-Lesson attached Asset Version + state — placed above Approve/Request Changes | SW | `specs/003-course-authoring/spec.md` (review lifecycle, D-021) | **none — TASK_AMENDMENT_REQUIRED** | `GET /admin/review/courses/:id/revisions/:revisionId` (`catalog_routes.go:115`); client `review.ts:73` | route integration tests; client unit tests | unit + E2E asserting the served graph renders; founder retest | none | Amend `specs/003-course-authoring/tasks.md` |
| **P0** | S2 | **Admin Lesson video preview (D)** | PARTIAL | `previewLesson` client + player wiring for `POST /admin/review/courses/:id/revisions/:revisionId/preview/:lessonId` | SW | same | **none — TASK_AMENDMENT_REQUIRED** | route mounted (`catalog_routes.go:128`); **no client** | backend tests only | E2E: Admin plays a submitted Lesson video before approving | inspector above | Amend same tasks.md |
| **P0** | S1C | **Production staff composition (B)** | PARTIAL | Remove the `EnvDevelopment` gate at `main.go:716` and the hard-stop at `:535`, replaced by whatever production preconditions the Product Owner requires | SW | `specs/002-auth-rbac/s1c/spec.md` §7 | **none — SPEC_AMENDMENT_REQUIRED** (the stop is deliberate: *"pending launch approval"*) | full staff surface built and dev-composed | dev integration + route sweeps | production-mode composition test + `/api/v1/staff-invitations` reachable with `APP_ENV=production` | **explicit Product Owner launch approval** | Obtain the decision first |
| **P0** | S12/S4 | **Merged-tree media E2E (C)** | UNKNOWN_NEEDS_EVIDENCE | Capture the API and worker processes' resolved `MEDIA_SCANNER_MODE` and `MEDIA_OPERATING_MODE` during the run; if `UNAVAILABLE`/`ADMIN_CATALOGUE`, fix the harness; if correct, debug the dispatcher | SW | `specs/005-…/spec.md` | `T047`-adjacent; **TASK_AMENDMENT_REQUIRED** if a harness fix | dispatcher, worker, ffmpeg processor all present | one failing run | one green `test:e2e:media-authoring` at `afe1624` | none | Diagnose before any further media work |
| **P1** | — | **Independent review of `18fb7e0..afe1624`** | IMPLEMENTED_UNREVIEWED | Dispatch `agy` on the frozen range; resolve every Critical and High | SW | `CLAUDE.md` closure protocol | n/a | 24 commits | none | recorded verdict | range frozen (it is) | **Highest-value single action** |
| **P1** | S10/S3 | **Landing page fake catalogue + testimonials (E)** | PARTIAL | Replace `featuredCourses` with `GET /catalog/courses` (the honest `EmptyState` already exists and renders on `[]`); remove or gate `Testimonials` until consented quotes exist | SW | `docs/SCREENS.md` Screen 1 | **none — TASK_AMENDMENT_REQUIRED** | `lib/api/public-catalog.ts` complete | public catalogue tests | unit: landing renders server data; scan test forbidding `@/data/courses` in production components | none | Amend `specs/004-public-catalogue/tasks.md` |
| **P1** | S10 | **Broken public routes (E)** | PARTIAL | Point `routes.courses`→`/${locale}/catalog`, `routes.dashboard`→`/${locale}/learn/dashboard`; build or remove `/about`, `/teach`, `/contact` | SW | `docs/NAVIGATION_MAP.md` | **none — TASK_AMENDMENT_REQUIRED** | target routes exist | none | link-integrity test over `routes` + footer | none | Amend nav/IA tasks |
| **P1** | S10 | **FAQ claims Tap checkout (E)** | scope/UI defect | Rewrite `faq.ts` payment + expiry answers to the D-045 external-payment model | SW | D-045, `MVP_SCOPE_RECONCILIATION.md` | **none — TASK_AMENDMENT_REQUIRED** | n/a | none | copy review against D-045 | none | Amend |
| **P1** | S12 | **Production scanner (LG-014)** | BLOCKED_EXTERNAL + SW | Select a provider (external), then build its adapter behind the existing `Scanner` interface | Both | `specs/005-…`, LG-014 | none | `Scanner` interface + `UnavailableScanner` fail-closed | LG-014 OPEN | provider selection + adapter + fail-closed validation | Product Owner selection | Escalate to founder |
| **P1** | S12 | **T047 provider deployment** | BLOCKED_EXTERNAL | Execute the frozen Hostinger package on real KVM 2 + live R2 + real domain | Ext | `specs/008/tasks.md` | **T047** — AUTHORIZED_BY_EXISTING_TASK | package frozen at `91ab1e3` | local + disposable-topology only | public TLS/DNS, R2 `x-amz-version-id`, external alert, restore, rollback, smoke | KVM, R2, domain | Founder procurement |
| **P2** | S6 | **S6 approving verdict** | IMPLEMENTED_UNREVIEWED | Re-dispatch review; last verdict was REJECT | SW | `specs/006` | `T048`-equivalent | complete | integration suites | recorded APPROVE | branch review first | Fold into the branch review |
| **P2** | S9 | **S9 re-review** | IMPLEMENTED_UNREVIEWED | Cumulative range re-review | SW | `specs/010` | 30/30 checked | complete | acceptance tests | recorded verdict | branch review | Fold in |
| **P2** | S12 | **T048 convergence + freeze** | PARTIAL | Run `speckit.converge`, reconcile, freeze, dispatch | SW | `specs/008/tasks.md` | **T048** — AUTHORIZED_BY_EXISTING_TASK | 46/48 | — | verdict | T047 | After T047 |
| **P2** | S2 | **`DeleteCourse` guards `fake_entitlements`** | PARTIAL | Point the guard at `entitlements` (and `enrollments`), returning `ErrCourseHasAccess` | SW | `specs/003`, D-029 | **none — TASK_AMENDMENT_REQUIRED** | `course.go:270-310` | none | integration: delete refused cleanly with a real ACTIVE Entitlement | none | Amend |
| **P2** | S6 | **Stale task checkboxes** | STALE_DOCUMENTATION | Check `T013`; run or reclassify `T016`/`T024`/`T032`/`T075` | SW | `specs/006/tasks.md` | T013, T016, T024, T032, T075 | T013 implemented | code inspection | mutation-check runs, RTL sweep | none | Bookkeeping pass |
| **P2** | S11 | **Journey F/G/I regression on merged tree** | PRESENT_ANCESTOR | Rerun the access-grant and protected-learning integration suites at `afe1624` against schema 17 | SW | `specs/009` | 29/29 | suites exist | ancestor-only | green run at HEAD | none | Run before go/no-go |
| **P3** | S10 | Hard-coded `gradex.com` | PARTIAL | Drive `siteConfig.url` from `PUBLIC_ORIGIN` | SW | S11 follow-up | none | `site.ts:4` | named in S11 closure | build-time assertion | real domain | Backlog |
| **P3** | S10 | Community/Discord copy | scope/UI defect | Remove or gate per D-046 | SW | D-046 | none | dictionaries + footer | none | copy scan | none | Backlog |
| **P3** | — | `AGENTS.md`/`CLAUDE.md`/`STATUS.md` staleness | STALE_DOCUMENTATION | Reconcile to current authority | SW | `PLAN.md` protocol | none | — | — | — | none | Do with the authority pass |
| **P3** | S5 | `F-1` playback rate-limit monitoring signal | tracked follow-up | Per-Student/per-source attribution | SW | S5 review register | none | — | reviewer register | — | none | Observability backlog |

---

## 16. SpecKit authority gaps

For every P0/P1 software gap:

| Gap | Determination | Citation / reason |
|---|---|---|
| Admin submitted-revision inspector | **TASK_AMENDMENT_REQUIRED** | `specs/003-course-authoring` is 64/64 checked and its spec requires a review lifecycle (D-021), but no task owns the Admin-facing inspection UI. The requirement is real; the task is absent. Owner: `specs/003-course-authoring/tasks.md`. |
| Admin Lesson video preview | **TASK_AMENDMENT_REQUIRED** | Same spec, same gap. The backend route exists precisely because the spec anticipated it; only the UI task is missing. |
| Production staff composition | **SPEC_AMENDMENT_REQUIRED** | The stop is deliberate and self-describing: *"remains unavailable pending launch approval."* No spec currently says production staff onboarding must work at launch, yet `PLAN.md` §8 criterion 3 requires Instructor authoring to meet MVP acceptance — which presupposes an Instructor exists. The spec must state the production precondition before a task can implement it, and it needs a Product Owner decision, not an engineering judgement. |
| Merged-tree media E2E | **AUTHORIZED_BY_EXISTING_TASK** if the fix is harness configuration (it falls under existing S12/S4 acceptance obligations); **TASK_AMENDMENT_REQUIRED** if it turns out to be a dispatcher defect. Determine the cause first. |
| Landing page real catalogue | **TASK_AMENDMENT_REQUIRED** | `specs/004-public-catalogue` is 48/48 and delivered the API and the `/[locale]/catalog` screens. The landing page's Screen 1 obligation in `docs/SCREENS.md` was never converted into a task binding it to the API. |
| Broken public routes | **TASK_AMENDMENT_REQUIRED** | `docs/NAVIGATION_MAP.md` defines the IA; no task asserts the shipped `routes` map matches it. |
| FAQ Tap checkout copy | **TASK_AMENDMENT_REQUIRED** | D-045 is unambiguous authority; no task swept public copy for surviving commerce claims. |
| Production scanner | **Not a task gap — external gate.** | LG-014 must select a provider before any task can name an adapter. |
| T047 / T048 | **AUTHORIZED_BY_EXISTING_TASK** | `specs/008-staging-production-infrastructure/tasks.md` T047, T048, both unchecked, both exact. |
| `DeleteCourse` guard | **TASK_AMENDMENT_REQUIRED** | D-029 separates delisting from suspension and presumes an accurate access check. The check exists but reads a legacy table. |

**Browser-discovered behaviour is not implementation authority.** Every item above needs its task or spec amendment landed *before* implementation, not alongside it.

---

## 17. Dependency-ordered completion plan from `afe1624`

**Step 1 — Reconcile authority (documentation only, no production code).**
Priority P1. Owner: whoever holds the planner seat. Update `AGENTS.md`/`CLAUDE.md` to the current slice and seats; record the two missing Product Owner decisions (staff composition remediation; Admin Catalog remediation) with their boundaries; correct `STATUS.md`'s active slice, S6 task count, date, and last-reconciliation head; point `.specify/feature.json` at the active work. Validation: `scripts/docs-guard.sh`. Closure: the documents describe `afe1624`. No independent review needed — no behaviour changes.

**Step 2 — Independent review of `18fb7e0..afe1624`.**
Priority P1. Seat: `agy`, dispatched by `scripts/agy-review.sh 18fb7e0..afe1624` through the `agy-delegate` skill. Claude authored the range and must not review it. Every Critical and High must be resolved before any further feature work lands on the branch. No retrievable verdict = `UNAVAILABLE`, not approval. This step gates everything below because six behaviour-changing ranges — including a merge whose tree has never passed its own E2E — are currently unvalidated.

**Step 3 — Diagnose the media E2E stall.**
Priority P0, but strictly diagnostic. Capture the resolved `MEDIA_SCANNER_MODE` and `MEDIA_OPERATING_MODE` on the API and worker processes during `npm run test:e2e:media-authoring`. If they are `UNAVAILABLE`/`ADMIN_CATALOGUE`, the harness is the defect and the fix is a config amendment under existing S12 authority. If they are correct, the dispatcher is the defect and needs a task amendment. Closure: one green run at `afe1624`. This precedes Step 4 because the Admin approval journey terminates in media that must be READY.

**Step 4 — Amend `specs/003-course-authoring/tasks.md`, then build the Admin submitted-revision inspector and Lesson preview.**
Priority P0. Spec authority: `specs/003-course-authoring/spec.md` review lifecycle, D-021. Task authority: **must be created first**. Files: new `frontend/src/components/admin/revision-inspector.tsx`; `frontend/src/lib/api/review.ts` (add `previewLesson`); `review-queue.tsx` (render the inspector on Open, above the decision controls). Backend untouched — both routes are served. Seat: implementation agent. Reviewer: `agy`. Validation: unit tests that the served graph renders every required field; E2E extending `s14-admin-catalog-review.spec.ts` — submit a real Course, Open it, assert bilingual title/description, study year, Major, Subject, Sections, Lessons, per-Lesson media state, play the Lesson video, then approve. Founder acceptance: approve a real submitted Course having seen its content and watched its video. Closure: recorded reviewer verdict on the frozen range with zero Critical/High.

**Step 5 — Public surface truthfulness.**
Priority P1, parallelizable with Step 4 (disjoint files). Amend `specs/004-public-catalogue/tasks.md` and the nav/IA tasks, then: bind `FeaturedCourses` to `GET /catalog/courses`; delete `src/data/courses.ts` and `src/data/testimonials.ts` (or gate `Testimonials` behind consented quotes); correct `routes.courses`/`routes.dashboard`; build or remove `/about`, `/teach`, `/contact`; rewrite the two `faq.ts` payment answers; remove community promises and the Discord icon per D-046; drive `siteConfig.url` from `PUBLIC_ORIGIN`. Validation: a link-integrity test over `routes` + footer, and a scan test forbidding `@/data/courses` and `@/data/testimonials` in production components, with a non-vacuity assertion — the pattern `admin-catalog-surface.test.ts` already establishes. Founder acceptance: load `/` anonymously; every card is a real Course, every link resolves, no claim describes a deferred capability.

**Step 6 — Production staff composition.**
Priority P0, but **blocked on a Product Owner decision**, not on engineering. Obtain the launch approval the hard-stop names, amend `specs/002-auth-rbac/s1c/spec.md` to state the production precondition, then remove the gate at `main.go:716` and the error at `:535`. Validation: composition test with `APP_ENV=production` proving `/api/v1/staff-invitations` mounts and the full route sweep still denies non-Admins. Founder acceptance: invite an Instructor in a production-mode environment and complete the invitation.

**Step 7 — Merged-tree regression proof.**
Priority P2. Rerun the S6 access-grant and S5 protected-learning integration suites at the post-remediation head against a database migrated to 17, and refresh `evidence/s12/provider-staging.md`'s stale schema `15|false`. Journeys F, G, and I currently rest entirely on ancestors.

**Step 8 — External gates.**
Priority P1, founder-owned, no engineering dependency: LG-014 scanner selection (then one adapter), LG-018 sender domain, LG-012 launch prices, and T047 (Hostinger KVM 2 + live R2 with verified `x-amz-version-id` behaviour + real domain in Cloudflare DNS). Then T048 — converge, freeze, dispatch.

**Step 9 — Go/no-go against `PLAN.md` §8.**
Criteria 1, 3, 5, and 6 fail today. §8 states that failure of any criterion is a no-go and does not authorize a reduced launch unless the canonical MVP and gate register are explicitly revised and reapproved. That revision is a Product Owner act, not an engineering one.

**Explicitly not recommended:** any refactor, any new feature, any redesign of identity, media, or access. Every P0 above is *connecting existing APIs to existing UI*, *removing a demo seam*, or *obtaining a verdict*.

---

## 18. ONE recommended next task

**NEXT TASK**
Dispatch an independent read-only review of the frozen range `18fb7e0d4b..afe1624d4c` (24 commits) to `agy` via `scripts/agy-review.sh 18fb7e0..afe1624`, routed through the `agy-delegate` skill.

**WHY NOW**
Every commit on this branch is unreviewed, and the branch already changed production router composition (`1afe40f`), mounted a new credential-installing route (`5d605c2`), introduced a scanner seam (`0e43410`), and merged a slice whose only recorded verdict is REJECT (`6e016b6`). `CLAUDE.md` forbids the builder from approving its own work and forbids Claude from reviewing a Claude-authored range. Every other candidate action — the Admin inspector, the landing-page cleanup, the staff gate — adds more unreviewed code on top of an unreviewed base, and the one merged-tree E2E that exists fails. Reviewing first is the only move that does not deepen the debt, and the range is already frozen on a clean tree, so it costs no preparation.

**SLICE OWNER**
Cross-slice launch integration. No single slice owns the range; that is itself a finding, and the review is what makes the range attributable.

**SPEC AUTHORITY**
`CLAUDE.md` / `AGENTS.md` closure protocol: *"A slice does not close on its builder's own assessment; it closes on a recorded reviewer verdict against one exact commit range, with every critical and high finding resolved."* Reinforced by D-079 and D-080, which both state *"the range remains subject to independent review."*

**TASK AUTHORITY**
The closure protocol itself. Reviewing is not a spec task and needs none.

**TASK AMENDMENT REQUIRED?**
**No.**

**IMPLEMENTATION SCOPE**
Read-only review of exactly `18fb7e0..afe1624` in a disposable detached worktree. Dimensions: production router composition changes; the mounted password-change route and its capability gate; the CSRF derivation fix; the `MEDIA_SCANNER_MODE` development seam and its production refusal; `DBAssetVersionValidator` re-pointing to `media_asset_versions`; the S9 merge and its post-REJECT remediation commits; the Admin review client and its CSRF fail-closed behaviour; the demo-fixture removal scan tests and their non-vacuity assertions; and the added `TestCatalogAdminReadRoutesDenyInstructor` sweep.

**MUST NOT TOUCH**
The live repository or its working tree. No commit, no branch, no checkbox, no `STATUS.md` edit. `scripts/agy-review.sh` enforces this structurally — disposable worktree, porcelain snapshot before and after — but the reviewer must also not be asked to fix anything it finds.

**VALIDATION**
The review is the validation. Its output must be a retrievable verdict with per-finding severity. A run that produces no retrievable verdict is `UNAVAILABLE`, not approval.

**INDEPENDENT REVIEW RANGE STRATEGY**
One range, `18fb7e0..afe1624`, not six. The merge commit `6e016b6` and the S9-plus-remediation interaction are only visible in the integrated tree, and the sole observed failure at this HEAD lives exactly there. Splitting the range would leave that interaction uncovered by construction.

**FOUNDER ACCEPTANCE**
Read the verdict. Confirm zero unresolved Critical and High. Only then authorize Step 3 (media E2E diagnosis) and Step 4 (Admin inspector).

---

## 19. No-code-change confirmation

No repository file was modified.
No task checkbox was changed.
No commit was created.
Frozen HEAD remains
`afe1624d4cdb117c57aed3fc86594e5ebdb4074b`
