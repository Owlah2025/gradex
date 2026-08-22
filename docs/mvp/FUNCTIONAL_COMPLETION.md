# Gradex MVP Functional Completion Tracker

> Status: Opened 2026-08-20. **This is the single canonical MVP completion tracker.**
> Authorized by [D-089](../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time):
> gap-driven work only, one `MVP-Fxx` tranche at a time, no tranche starts automatically.
> Supersedes ad-hoc completion claims elsewhere for the purpose of "is the MVP functionally done".
> It does not supersede [DECISIONS.md](../DECISIONS.md) (scope authority) or
> [launch/STATUS.md](../launch/STATUS.md) (delivery/authority state).
> Last measured: 2026-08-22 (MVP-F20 T4 Instructor Academic Context regression, §23).
> **Score is unchanged at 37 / 53 = 69.8%.** MVP-F17 through MVP-F20 are implementation tranches:
> they add no canonical feature row, promote none, and do not move the denominator.

## 0. Why this exists

The repository contains an independently approved software head (`2c43b90`) and extensive slice
evidence. Approval of a commit range is **not** proof that a user journey works. This tracker records
what was actually *demonstrated to run*, separately from what was reviewed.

## 1. Status vocabulary

| Status | Meaning |
|---|---|
| `E2E_PROVEN` | A test drove the real journey across real layers (real Postgres/Redis/MinIO/API/browser) **and** the run was observed green in this repository. |
| `IMPLEMENTED_NOT_PROVEN` | Code path complete across layers, but no test drives the real journey, or the test exists and was not observed green. |
| `PARTIAL` | Happy path only, or a required sub-behavior is missing. |
| `FRONTEND_MISSING` / `BACKEND_MISSING` | One layer absent. |
| `INTEGRATION_BROKEN` | Layers exist but do not connect for a real user (includes ID-copy workflows and undiscoverable state). |
| `NOT_IMPLEMENTED` | Absent. |
| `BLOCKED` | Cannot proceed; blocker recorded. |
| `FOUNDER_DECISION_REQUIRED` | Genuinely unresolved product/architecture question. |
| `DEFERRED` / `OUT_OF_MVP` | Explicit decision removed it; decision cited. |

**Rule: `E2E_PROVEN` is never assigned from source-code inspection.**

## 2. Measured baseline — 2026-08-20

Environment: local `backend/docker-compose.yml` stack (PostgreSQL 16, Redis 7, MinIO, Mailpit),
Go 1.24.3, Node, ffmpeg present, Playwright 1.62.0 + Chromium. Branch `ui-antigravity-20260817`
with its uncommitted working tree in place.

> **Superseded by the canonical baseline in §14.** The whole-suite numbers in this section were
> measured under the non-deterministic 6-worker configuration and are retained only as the record
> that led to GAP-05. The authoritative baseline is now **107 passed / 6 failed / 3 did not run**
> at one worker.

| Suite | Before MVP-F01 | After MVP-F01 | After MVP-F02 | **After MVP-F03** |
|---|---|---|---|---|
| `go build ./...` + `go vet ./...` | clean | clean | clean | clean |
| `go test ./...` | 26 ok, 0 fail, 0 skip | unchanged | 26 ok, 0 fail | 26 ok, 0 fail |
| `tsc --noEmit` | clean | clean | clean | clean |
| Frontend unit (`npm test`) | 244 pass / 0 fail | 244 pass / 0 fail | **251 pass / 0 fail** | 251 pass / 0 fail |
| Playwright full suite | **82 passed / 27 failed** | **97 passed / 12 failed / 3 did not run** | **97 passed / 12 failed / 3 did not run** | **97 passed / 12 failed / 3 did not run** |
| `media-authoring` config | not run | not run | **1 passed** | **1 passed** (full publication + isolation journey) |

Remaining 12 after MVP-F01, **unchanged after MVP-F02** (same tests, same causes — no F02
regression): `s3` 4 (RC-2), `s5-viewport-evidence` 4 (RC-3), `s5-expired-entitlement` 1 (RC-4,
deliberately left), `s5-playback-performance` 1 (perf), `s13` 1 + `s6` 1 (RC-5 interference — both
pass in isolation). The 3 "did not run" are downstream of the `s6` interference failure.

### 2.1 The 27 E2E failures, root-caused

Each failing suite was re-run **in isolation** to separate real defects from cross-spec interference.

| Suite | Full-run fails | Isolated fails | Verdict |
|---|---:|---:|---|
| `s6-course-access-grant-launch` | 1 | **0** (2 passed) | cross-spec interference |
| `s12-instructor-authoring` | 1 | **0** (4 passed) | cross-spec interference |
| `s13-mandatory-password-change` | 1 | **0** (2 passed) | cross-spec interference |
| `s14-admin-catalog-review` | 1 | **0** (4 passed) | cross-spec interference |
| `s5-lesson-player` | 8 | 8 | real — RC-1 |
| `s5-expired-entitlement` | 5 | 5 | real — RC-1 (4) + RC-4 (1) |
| `s5-access-ends` | 1 | 1 | real — RC-1 |
| `s3-public-catalogue` | 4 | 4 | real — RC-2 |
| `s5-viewport-evidence` | 4 | 4 | real — RC-3 |
| `s5-playback-performance` | 1 | not isolated | perf/env, unverified |

### 2.2 Root causes

**RC-1 — Stale approved evidence (14 tests). FIXED in MVP-F01.**
`5cc8ede` (2026-08-05) created the S5 leak audits, which assert the bare substring `authorized`
is absent from rendered output. `f25a565` (2026-08-12) changed `siteConfig.description` to
"...and **authorized** learning access...". Both are in `HEAD` history; the S5 approval predates
`f25a565`. The substring additionally matches Next.js App Router's own
`"unauthorized":"$undefined"` router-boundary key, present in every route's flight payload.
Verified: all 9 occurrences in the failing DOM were the meta description or Next internals — **no
authorization data leaked**. The S5 slice therefore had **no valid E2E proof at HEAD** since
2026-08-12 and nobody re-ran the suite.

**RC-2 — Catalogue language-route regression (4 tests). OPEN.**
An **uncommitted** working-tree change to `frontend/src/components/catalog/public-catalogue.tsx`
(+101/−46) migrated `CatalogueList` from the legacy in-file `Shell` to the canonical
`Navbar`/`Footer`/`Container`, but left `CatalogueDetail` on the old `Shell`. Two functional
consequences: the canonical `LanguageToggle` (`components/common/language-toggle.tsx:11`) calls
`toggleLocale()`, which sets state and `localStorage` but **never rewrites the `[locale]` route
segment**; and its accessible name is `"Switch language to Arabic"` where the legacy toggle and the
tests use `"Switch to Arabic"`. Result: on `/ar/catalog` the language control changes `<html lang>`
but the URL stays `/ar/catalog`. The route-rewriting logic already exists correctly in
`components/learning/learning-locale-toggle.tsx:7` (`withLocale`) and in the legacy
`CatalogueLanguageToggle`. Three toggle implementations, only one of them wrong, and the wrong one
is now on the Student's highest-traffic public surface.

**RC-3 — `report_context` reaches the DOM (4 tests). OPEN — needs a decision.**
[D-065](../DECISIONS.md#d-065--exact-visible-report-context-binding) requires the opaque report
context be "carried, never interpreted… **do not render it, place it in the DOM or an attribute**".
`s5-viewport-evidence` enforces this. It fails: the token (`grc1…`) is present in the Course Home
page's RSC flight payload. Mechanism: `courseReportTargets(course)` returns `{kind, context}` objects
(`components/learning/report-targets.ts:18`) which are passed as the `targets` prop to the **client**
component `ReportTargetActions`. Next.js serialises every client-component prop into the HTML flight
payload, so the context is in the DOM by construction. This cannot be fixed by "being careful" — an
RSC page cannot hand a secret to a client component without serialising it. See GAP-03.

**RC-4 — Full learning dictionary ships to the client (1 test). OPEN — needs a decision.**
On an expired lesson the flight payload contains the whole `dictionary.learning` object, including
`"active":"Active access"`. `s5-expired-entitlement.spec.ts:730` asserts active-state copy does not
bleed into the expired context. The authoritative state marker is correct
(`data-learning-status="expired"`; that assertion passes) and no Student data or authority state
leaks — what leaks is **localization copy**. Deliberately left failing: unlike RC-1 this is not
provably benign noise, and narrowing the assertion would be weakening a test to go green. See GAP-04.

**RC-5 — The E2E suite cannot be run as a suite. OPEN.**
Four specs pass alone and fail in the full run. One `globalSetup` seeds one database shared by all
specs; specs that mutate entitlements/accounts (notably the `s5-*` family) leave state that later
specs depend on being pristine. Consequence: **the repository cannot produce whole-suite launch
evidence today**, only per-spec evidence. See GAP-05.

## 3. Canonical MVP scope (established from repository authority)

Derived from `docs/DECISIONS.md` (read in full), `PRD.md`, `BUSINESS_RULES.md`, `SCREENS.md`,
`USER_JOURNEYS.md`, `NAVIGATION_MAP.md`, and the `specs/` tree. Authority rule applied: **latest
explicit founder decision wins.**

### 3.1 IN scope

**Shared** — bilingual Arabic-default RTL/LTR responsive web (D-016, BR-147/149); WCAG 2.2 AA on
platform-owned UI and player controls only, no whole-product conformance claim (BR-151); one opaque
server-managed session cookie with CSRF rotation and family revocation (D-034, BR-004/005/006);
mandatory password change so `CHANGE_REQUIRED` is not terminal (D-080); HIBP compromised-password
screening, fail-closed, Argon2id, 15–128 chars (D-075); immediate suspension enforcement across
existing sessions (D-014, BR-007); bilingual Terms/Privacy policy set `gradex-legal-2026-08-09-v1`
with versioned acceptance (D-076); **exactly eight** transactional email contracts over an encrypted
Postgres outbox behind Resend (D-077) with a durable activation boundary (D-078).

**Student** — Student-only self-registration + mandatory verification, granting no access (D-014,
D-045, BR-001/008/029); display name 2–50 chars, non-unique (D-024, BR-105); public
`PUBLISHED`-only catalogue list and detail; bilingual Arabic-normalised search (D-023, D-054/055,
BR-161/162); taxonomy **display** of one Major + one Subject + one Study Year (D-022, BR-157/160);
displayed full-Course price in fils rendered KWD 3dp, informational only (D-045 Q1, BR-019);
Course Access Invitation Student side — receive, accept only from the invited normalized identity,
see pending-approval state, view access status and history (D-045, BR-166/168); adaptive HLS with
short-lived signed access, resume position, completion at 90% of trusted duration
(D-001, D-060, D-063, BR-050/051/052); playback issuance ceilings 30/10min per Student and
600/10min per source, fail-closed (D-071); progress write ceilings 12/min per (Student, Lesson) and
1200/min per source (D-061/062); protected learning read models, no-store, `learning_status` is
`active|expired` only (D-063); entitlement-protected Resource/Lab downloads via fixed same-origin
entry links that 302 to short-lived signed targets, buyer tag on Labs only (D-011, D-064, BR-063/103);
content reporting bound to the exact rendered instance via an AES-256-GCM opaque context
(D-019, D-065, D-066, BR-145/146).

**Instructor** — owned Course/Section/Lesson builder; submission for review; pending-revision
workflow that never mutates the live approved revision; validated video/resource upload under the
D-088 trusted-Instructor profile (mp4/PDF/DOCX, exact object-version binding, size/type/signature/
checksum validation, private quarantine, truthful validated-without-scan state, FFmpeg evidence
before `READY`); Course-scoped roster of enrolled Students showing display name only
(D-045 Q9, BR-105).

**Admin** — course review queue and content review; publish / request changes with reason /
delist / relist / retire / archive / emergency access suspension; taxonomy vocabulary management
(BR-158/159/160); Course and Section pricing with required reason and audit history (Section prices
maintained but **not displayed**, D-045 Q2); Course `default_access_ends_at` configuration, required
before any invitation may be approved (D-045 Q4); Course Access Invitation create/approve/reject
with reason/cancel/resend — **Approval is the only action in the entire product that grants access**
(D-045, BR-167); entitlement extend/shorten/revoke with required reason (BR-026); staff invitation
lifecycle and account suspension/reactivation.

### 3.2 OUT of scope — do not resurrect

All in-platform payments: checkout, cart, coupons, refunds, invoices, instructor payouts, BNPL,
payment webhooks, KNET, Apple Pay, Tap integration, earnings snapshots, revenue dashboards
(**D-045**, superseding D-027 and deferring D-002/012/017/018/028/030).
External Course community link (**D-046**, deferred to S18).
Production malware scanning for the bounded trusted-Instructor upload profile (**D-088**).
Captions/transcripts (out of MVP; no whole-product WCAG claim permitted).

### 3.3 Scope items still to be confirmed against the code

The parallel audit of these areas did not complete (session limits). They are listed so the next
session resumes precisely, not silently: **Notification Center (S07)**, **Profile/Account (S08)**,
**Office Hours (ST09/IN08/AD11)**, **Course Analytics (IN07)**, **Reported Content resolution
(AD10)**, **Entitlement Detail (AD07)**, **Public Preview Manager (IN05)**. Each is described in
`SCREENS.md`, but `SCREENS.md` predates several later decisions. **Resolve scope before treating any
of them as an MVP gap** — see GAP-06.

## 4. Feature completion matrix

Statuses reflect what was **measured** through 2026-08-22. Features whose area audit did not complete are
marked `UNAUDITED` in the Evidence column rather than guessed.

### 4.1 Student

| ID | Feature | FE | BE | Authz | Tests / evidence | Status | Sev |
|---|---|---|---|---|---|---|---|
| ST-01 | Register → verify → sign in | ✔ | ✔ | ✔ | Driven inside `s6` 30-step journey; observed green in isolation | `E2E_PROVEN` | — |
| ST-02 | Public catalogue list + search | ✔ | ✔ | public | **`s3-public-catalogue` 28/28 green after MVP-F05.** F03 proved a newly published Course is found by title from an anonymous context and that `DRAFT` and pending revisions are excluded server-side; F05 proved bilingual locale navigation, query preservation, and keyboard operation | `E2E_PROVEN` | — |
| ST-03 | Catalogue academic **filters** (University/Program/Level/Subject) | ✖ | ✖ | — | `SCREENS.md` ST01 requires; backend accepts only `q` + paging (`catalogpublic/repository.go:91-121`). **Re-pointed to MVP-F22 (T6) by [D-091](../DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy)**; the legacy Major/Subject/Study Year filter set is superseded. Blocked until MVP-F17→F21 land. | `BACKEND_MISSING` | MEDIUM |
| ST-04 | Course detail page | ✔ | ✔ | public | **MVP-F14.** A separately uploaded, revision-scoped preview follows the Course publication pointer; real Instructor upload → READY → Admin approval → anonymous Course Details playback is retained, with live/candidate isolation, removal, no-entitlement/progress proof, bilingual UI, and protected-Lesson denial ([evidence](../launch/evidence/2026-08-21-mvp-f14-public-preview.md)). | `E2E_PROVEN` | — |
| ST-05 | Receive + accept Course Access Invitation | ✔ | ✔ | ✔ BR-166 | `s6` steps 1–14 green in isolation | `E2E_PROVEN` | — |
| ST-06 | Pending-approval state grants nothing | ✔ | ✔ | ✔ | `s6` asserts 0 entitlements, 0 enrollments pre-approval | `E2E_PROVEN` | — |
| ST-07 | Access status + history | ✔ | ✔ | ✔ | **MVP-F12.** `s6` drives invitation → accept → leave token context → return via dashboard link → approval → "Access granted" → Go to course, in English and Arabic, with a no-internal-identifier audit at every step | `E2E_PROVEN` | — |
| ST-08 | Learning dashboard | ✔ | ✔ | ✔ | **MVP-F15.** Continue-learning proved by `s15-dashboard-resume` (EN + AR); the pending-access summary proved inside the real `s6` lifecycle in both windows — action-required before acceptance, waiting-for-Admin after it, and absent once approved — with its own route into `/[locale]/access` | `E2E_PROVEN` | — |
| ST-09 | Course Home + outline | ✔ | ✔ | ✔ | `s5-course-home` green | `E2E_PROVEN` | — |
| ST-10 | Lesson player, HLS, controls, keyboard, WCAG AA | ✔ | ✔ | ✔ | `s5-lesson-player` **21/21 green after MVP-F01** | `E2E_PROVEN` | — |
| ST-11 | Progress persistence + resume position | ✔ | ✔ | ✔ | `s5-lesson-player` progress-persistence test green; `s6` progress POST 200/204 | `E2E_PROVEN` | — |
| ST-12 | Continue-learning / resume pointer on dashboard | ✔ | ✔ | ✔ | **MVP-F15.** Derived server-side from existing `progress.last_watched_at` — one bounded query, live-revision-only, filtered through the entitlement evaluator. `s15-dashboard-resume` drives a real player Progress write, then the Dashboard pointer and its click-through to the correct Lesson, in English and Arabic; 12 selection cases proved at `internal/httpapi` integration level (§17.1) | `E2E_PROVEN` | — |
| ST-13 | Expired entitlement authorises nothing | ✔ | ✔ | ✔ | `s5-expired-entitlement` **12/13 green after MVP-F01**; 1 fail RC-4 | `PARTIAL` | MEDIUM |
| ST-14 | Access ending mid-session denies next issuance + progress write | ✔ | ✔ | ✔ | `s5-access-ends` **2/2 green after MVP-F01** | `E2E_PROVEN` | — |
| ST-15 | Protected Resource/Lab download | ✔ | ✔ | ✔ | Separate real-storage browser proof covers D-088 PDF Resource authoring, Admin publication, protected Resource and existing Lab Material presentation, authorized private byte retrieval, and A→B→remove isolation; canonical row evidence is retained ([record](../launch/evidence/2026-08-21-st15-protected-resource-lab-download.md)). | `E2E_PROVEN` | — |
| ST-16 | Content reporting | ✔ | ✔ | ✔ | `s5-viewport-evidence` **fails (RC-3)** | `INTEGRATION_BROKEN` | HIGH |
| ST-17 | RTL/LTR viewport evidence across 4 breakpoints | ✔ | — | — | `s5-viewport-evidence` 0/4 (RC-3) | `PARTIAL` | HIGH |
| ST-18 | Notifications / Profile / Office Hours | ✖ | ✖ | — | No route, no endpoint | `FOUNDER_DECISION_REQUIRED` (scope) | see GAP-06 |
| ST-19 | **Automated manual Course Purchase Request** — Student intent → WhatsApp → Admin confirmation → purchase-backed Invitation → automatic access | ✔ | ✔ | admitted public write / Admin-only commands / intended Student only | **MVP-F16 remediation.** `manual-purchase-flow` 3/3 drives a real `PUBLISHED` Course through persisted, server-priced intent and intercepted WhatsApp handoff; Admin confirmation/outbox invitation; new-account registration/verification/login return; one `PURCHASE_REQUEST` Entitlement/Enrollment; Course Home and a positive protected-`video` assertion. It also proves existing-Student access and Admin cancellation/recovery. The full suite is 114/6/3 with only the six retained non-ST-19 failures ([evidence](../launch/evidence/2026-08-21-st19-manual-purchase-flow.md)). This remains one combined Student/Admin capability, not a second denominator row. | `E2E_PROVEN` | — |

### 4.2 Instructor

| ID | Feature | Evidence | Status | Sev |
|---|---|---|---|---|
| IN-01 | Owned-course list, no UUID required | `s12` asserts `owned-course-list` visible; 4/4 green in isolation | `E2E_PROVEN` | — |
| IN-02 | Create Course from UI, survives reload | `s12` test A green | `E2E_PROVEN` | — |
| IN-03 | Sections + Lessons persist with exact structure | `s12` test B green | `E2E_PROVEN` | — |
| IN-04 | Ownership enforcement (other Instructor / Student refused) | `s12` test D green; `s14` test E green | `E2E_PROVEN` | — |
| IN-05 | Submit-for-review **rejection** path shows server reasons | `s12` test E + `s14` test D green (`TAXONOMY_DIMENSION_MISSING`, `COURSE_EMPTY`) | `E2E_PROVEN` | — |
| IN-06 | Submit-for-review **success** path | `media-authoring/s12-instructor-video-upload` green 2026-08-20: real MP4 → worker READY → taxonomy → submit → appears in the Admin queue | `E2E_PROVEN` (media-authoring config) | — |
| IN-07 | Instructor sees Admin change-request reason | **MVP-F02.** Same spec asserts the Instructor reloads the studio and reads the Admin's verbatim reason from the real API | `E2E_PROVEN` | — |
| IN-08 | Revise + resubmit after CHANGES_REQUESTED | **MVP-F02.** Instructor edits the title and resubmits **through the studio UI**; notice clears; Admin sees `PENDING_REVIEW` again | `E2E_PROVEN` | — |
| IN-09 | Video upload → processing → READY | `playwright.media-authoring.config.ts` 1/1 green 2026-08-21: real protected Lesson MP4 and separate PREVIEW MP4 each traverse private upload → scanner/worker → READY; both remain distinct in the same retained journey. | `E2E_PROVEN` | — |
| IN-10 | Revise an already-**published** Course from the UI | **MVP-F11.** Start-revision action driven by clicking the studio (never by the spec calling the endpoint); candidate cloned; published version keeps serving through `DRAFT` and `PENDING_REVIEW`; re-click does not fork; Admin receives it by title; owner/other-Instructor/Student/anonymous boundaries asserted | `E2E_PROVEN` | — |
| IN-11 | Analytics / roster / office hours | No route found | `FOUNDER_DECISION_REQUIRED` (scope) | see GAP-06 |

### 4.3 Admin

| ID | Feature | Evidence | Status | Sev |
|---|---|---|---|---|
| AD-01 | Review queue is the server's `PENDING_REVIEW` set, discoverable, no UUID launcher | `s14` tests A + B green — explicitly assert rows contain neither `course_id` nor `revision_id`, and that the UUID launcher is absent | `E2E_PROVEN` | — |
| AD-02 | Taxonomy vocabulary create → reaches Instructor selectors | `s14` test C green | `E2E_PROVEN` | — |
| AD-03 | Instructor cannot create taxonomy / override / read the queue | `s14` test E green | `E2E_PROVEN` | — |
| AD-04 | Select published Course **by title** for access grants | `s6` asserts `not.toContainText("Course ID (UUID)")` and selects by title | `E2E_PROVEN` | — |
| AD-05 | Configure Course default access expiry | `s6` step green (`admin/course-access/page.tsx:148`) | `E2E_PROVEN` | — |
| AD-06 | Create invitation by Student email | `s6` green | `E2E_PROVEN` | — |
| AD-07 | Approve → exactly one ACTIVE Entitlement + Enrollment, idempotent | `s6` asserts count 1, `grant_source=MANUAL_INVITATION`, and identical ids on re-approve | `E2E_PROVEN` | — |
| AD-08 | Reject with reason / cancel / resend | `s6` second test "Rejection & Expired Link UI Coverage" green | `E2E_PROVEN` | — |
| AD-09 | Entitlement extend / shorten / revoke | UI + API exist (`admin/course-access/page.tsx:290-345`); no E2E drives them | `IMPLEMENTED_NOT_PROVEN` | MEDIUM |
| AD-10 | **Publish** a reviewed Course | **MVP-F03.** Admin approves from the inspector; approval and publication are one transaction; the Course leaves the review queue **and reaches the public catalogue**, verified from an anonymous context | `E2E_PROVEN` | — |
| AD-11 | **Request changes** with reason | **MVP-F02.** Driven through the Admin inspector with a verbatim reason; the 200 and the Instructor-visible result are both asserted | `E2E_PROVEN` | — |
| AD-12 | Delist / relist / retire / archive / suspend / restore | `lifecycle-controls.tsx` present; no E2E | `IMPLEMENTED_NOT_PROVEN` | MEDIUM |
| AD-13 | Staff invitation lifecycle + suspension | `t108_staff_lifecycle_integration_test.go` green in `go test`; `s13` green | `PARTIAL` (backend proven, browser path partly) | MEDIUM |
| AD-14 | Reported-content resolution surface | No route, no component | `NOT_IMPLEMENTED` | see GAP-06 |

### 4.4 Shared / system

| ID | Feature | Evidence | Status | Sev |
|---|---|---|---|---|
| SY-01 | Ten outbox event types wired to handlers | Verified enqueued at `access/repository.go:918/1052`, `identity/admission.go:297`, `identity/invitation.go:256`; asserted in `access_routes_integration_test.go:390` | `IMPLEMENTED_NOT_PROVEN` (real provider unproven) | HIGH |
| SY-02 | Resend production sending | `docs/launch/evidence/2026-08-17-founder-beta-readiness.md`: no approved Resend credential/sender exists; registration blocked | `BLOCKED` | **BLOCKER** |
| SY-03 | Initial Admin bootstrap in production | Same record: requires a human-supplied bootstrap password; **no Founder-Beta Admin or Instructor exists** | `BLOCKED` | **BLOCKER** |
| SY-04 | Migrations + schema | `go test ./internal/db` green; schema version 22 adds revision-scoped public preview provenance while preserving migration rollback guards | `E2E_PROVEN` | — |
| SY-05 | Health / readiness | `/healthz` `/readyz` 200 on staging (2026-08-17 record) | `E2E_PROVEN` | — |
| SY-06 | Mandatory password change in the browser | `s13` 2/2 green in isolation | `E2E_PROVEN` | — |
| SY-07 | Whole-suite E2E runnability | 4 specs pass alone, fail together | `INTEGRATION_BROKEN` | HIGH |
| SY-08 | Backup + monitoring timers | 2026-08-17 record: `gradex-backup.timer` / `gradex-monitor.timer` not installed | `BLOCKED` | HIGH |
| SY-09 | Load/capacity (LG-019) | Canonical 250 RPS FAIL; no rate met the contract twice | `BLOCKED` | (launch gate) |

### 4.5 Completion score — 53 canonical MVP features

| Status | F15 documented column | F15 reconciled canonical rows | **After ST-15** | % after ST-15 |
|---|---:|---:|---:|---:|
| `E2E_PROVEN` | 33 | 33 | **37** | **69.8%** |
| `PARTIAL` | 5 | 5 | 3 | 5.7% |
| `IMPLEMENTED_NOT_PROVEN` | 2 | 3 | 3 | 5.7% |
| `BLOCKED` (ops/launch gates) | 4 | 4 | 4 | 7.5% |
| `INTEGRATION_BROKEN` | 3 | 2 | 2 | 3.8% |
| `BACKEND_MISSING` | 2 | 1 | 1 | 1.9% |
| `FOUNDER_DECISION_REQUIRED` | 2 | 2 | 2 | 3.8% |
| `FRONTEND_MISSING` | 0 | 0 | 0 | 0.0% |
| `NOT_IMPLEMENTED` | 1 | 1 | 1 | 1.9% |
| `UNAUDITED` | 1 | 1 | 0 | 0.0% |

> **Reconciliation of the pre-existing 52-vs-53 defect.** The persona denominators and the strict
> list of canonical feature rows are both **52**: Student 18 + Instructor 11 + Admin 14 + Shared 9.
> The old state column totalled 53 because it reported `IMPLEMENTED_NOT_PROVEN` 2 instead of the
> three actual rows, `INTEGRATION_BROKEN` 3 instead of two actual rows, and `BACKEND_MISSING` 2
> instead of one actual row. Inspecting every canonical row corrects the state totals to 52 without
> adding, deleting, or reclassifying a feature. The founder-approved ST-19 is one genuinely new,
> combined Student/Admin capability, so the canonical base is now **53**, not an invented 54.

MVP-F14 promotes the existing ST-04 row and independently re-proves the already-canonical IN-09
upload lifecycle; neither adds a denominator row. ST-15 then promotes the existing protected
Resource/Lab Student row without promoting an Instructor row. Per persona after ST-15:
**Student 14/19 (73.7%)**, Instructor 10/11 (90.9%), Admin 10/14 (71.4%), Shared 3/9 (33.3%).

## 5. Gap register — MVP blockers

Ordered by severity. Only items that stop Gradex being a complete MVP.

| ID | Gap | Persona | Exact failure | Root cause | Layers | Sev |
|---|---|---|---|---|---|---|
| ~~GAP-01~~ | ~~Instructor cannot see why a Course was rejected~~ | Instructor | **CLOSED by MVP-F02 on 2026-08-20** | — | — | — |
| ~~GAP-02~~ | ~~Published Course reaching the public catalogue is unproven~~ | All | **CLOSED by MVP-F03 on 2026-08-20.** The whole chain is now driven end to end and the publication was never seeded | — | — | — |
| GAP-03 | `report_context` is serialised into the DOM | Student | D-065 forbids the context entering the DOM; it is present in the Course Home flight payload | RSC serialises all client-component props; the context is passed as a prop to `ReportTargetActions` | FE architecture | **BLOCKER** (contract) |
| GAP-04 | Active-state copy present on expired pages | Student | `s5-expired-entitlement.spec.ts:730` fails on `"Active access"` in the flight payload | Whole `dictionary.learning` reaches the client | FE | HIGH |
| ~~GAP-05~~ | ~~E2E suite unusable as a suite~~ | — | **CLOSED by MVP-F06 on 2026-08-20.** Three consecutive full-suite runs now produce identical outcomes and identical failure identities. Original finding: specs passed alone but failed in the full run because file-level parallelism ran 6 workers against one shared seeded database while several specs mutated shared authority rows by fixed id | — | — | — |
| GAP-06 | Scope of 7 screens unresolved | All | Notifications, Profile, Office Hours, Analytics, Reported Content, Entitlement Detail, Public Preview Manager are in `SCREENS.md` but have no implementation and no post-D-045 confirmation | `SCREENS.md` predates later decisions | scope | **FOUNDER_DECISION_REQUIRED** |
| ~~GAP-07~~ | ~~Catalogue language switch does not change the route~~ | **CLOSED by MVP-F05 on 2026-08-20.** Original finding: Student | On `/ar/catalog` the toggle updates `<html lang>` but the URL stays `/ar/catalog`; accessible name mismatched | Uncommitted partial shell migration; canonical `LanguageToggle` lacks the `withLocale` rewrite the other two toggles have | FE | HIGH |
| GAP-08 | No production Admin/Instructor identity, no email provider | All | The founder could not complete any role journey on Founder Beta | No approved Resend credential; bootstrap needs a human password | ops | **BLOCKER** (launch) |
| GAP-09 | Catalogue has no taxonomy filters | Student | `SCREENS.md` ST01 requires Major/Subject/Study Year filters; API accepts only `q` | Never built | BE + FE | MEDIUM |
| ~~GAP-10~~ | ~~No Continue-learning resume pointer~~ | Student | **CLOSED 2026-08-21 by MVP-F15 (§17.1).** `dashboardResumeResponse` ships on `GET /learn/dashboard`; proved by `s15-dashboard-resume` (EN + AR) and 12 backend selection cases | — | — |
| ~~GAP-11~~ | ~~Instructor cannot start a revision of a **published** Course from the UI~~ | Instructor | **CLOSED by MVP-F11 on 2026-08-20.** Original finding: Once a Course is `PUBLISHED` the studio shows the Course lifecycle and no editable revision. `ListOwnedCourses` only returns `editable_revision` when one already exists (`catalog/authoring.go:594`); it never mints one. The endpoint that does — `PUT /api/v1/courses/:id/candidate` (`catalog_routes.go:95`) — is referenced nowhere in `course-builder.tsx` or `lib/api/authoring.ts`. A published Course is a dead end for its own Instructor | `BACKEND_WITHOUT_UI` | FE | HIGH |

## 6. Non-blocking debt (do **not** mix with §5)

**UX debt** — tracked in [`docs/ux/`](../ux/INVENTORY.md): no Student app shell, SSR emits
`lang="ar" dir="rtl"` for every locale, no route `loading.tsx`/`error.tsx`/`not-found.tsx`, all
learning failures collapse to one message, three page chromes, off-palette colors, three duplicate
course cards, missing design-system primitives.
**Code debt** — duplicate `/instructor/*` route trees; `access/page.tsx` `disabled={submitting || !token}`
tests the ref object not `token.current`; legal routes use hardcoded `/ar` `/en` prefixes and no
Refund Policy route exists; `/onboard` has no `SCREENS.md` contract.
**Perf** — `s5-playback-performance` phone TTFF failure, unverified in isolation; LG-019 capacity.

## 7. Remediation queue

| ID | Objective | Closes | Status |
|---|---|---|---|
| **MVP-F01** | Restore S5 E2E proof integrity | RC-1 | **DONE 2026-08-20** — see §8 |
| **MVP-F02** | Instructor sees the change-request reason; revise→resubmit completes | GAP-01, IN-06, IN-07, IN-08, AD-11 | **DONE 2026-08-20** — see §10 |
| **MVP-F03** | Prove an approved Course reaches the public catalogue | GAP-02, AD-10 | **DONE 2026-08-20** — see §11 |
| **MVP-F11** | Instructor revision of a published Course | GAP-11, IN-10 | **DONE 2026-08-20** — see §12 |
| **MVP-F04** | Decide + fix report-context DOM exposure | GAP-03, ST-16, ST-17 | blocked on decision |
| **MVP-F05** | Unify the language toggle; restore the language-addressable route contract | GAP-07, ST-02 | **DONE 2026-08-20** — see §13 |
| **MVP-F06** | Make the E2E suite deterministic | GAP-05 | **DONE 2026-08-20** — see §14 |
| **MVP-F07** | Resolve the scope of the 7 unconfirmed screens | GAP-06 | blocked on decision |
| **MVP-F08** | Catalogue taxonomy filters (API + UI) | GAP-09 | **superseded** — replaced by MVP-F22 under D-091 |
| **MVP-F09** | Dashboard resume pointer | GAP-10 | **superseded** — delivered by MVP-F15 (§17.1) |
| **MVP-F10** | Trim client-serialised localisation | GAP-04 | queued |
| **MVP-F12** | Student access status + history | ST-07 | **DONE 2026-08-20** — see §15 |
| **MVP-F13** | Course Details access guidance + entitlement-aware actions | ST-04 (partial) | **DONE 2026-08-20** — see §16 |
| **MVP-F14** | Revision-scoped Public Preview upload, signing, and player | ST-04 remainder | **DONE 2026-08-21** — [retained evidence](../launch/evidence/2026-08-21-mvp-f14-public-preview.md) |
| **MVP-F15** | Continue-learning pointer + dashboard | ST-12, ST-08 | **DONE 2026-08-21** — see §17 and §17.1 |
| **MVP-F16** | Automated manual Course Purchase Request | ST-19 | **DONE 2026-08-21** — see §18 |
| **MVP-F17** | **T1** — Academic Catalog Foundation (schema, domain, Admin catalog) | D-091 §1–6, §9 | **DONE 2026-08-21** — foundation tranche, see §20 |
| **MVP-F18** | **T2** — Launch Catalog Data (Kuwait University manifest + importer) | D-091 §11 | **DONE 2026-08-21**; launch data realigned to Founder scope 2026-08-22 (T2.1, T2.2) — see §21 |
| **MVP-F19** | **T3** — Student Academic Profile (onboarding + profile edit) | D-091 §10, D-092 | **DONE 2026-08-22** — implementation tranche, see §22 |
| **MVP-F20** | **T4** — Instructor Academic Context (`courses.subject_id`, Subject picker, Subject requests) | D-091 §7–9, D-093 | **PROVEN 2026-08-22** — T4-A/A.1/B/C/D/E proven together; [retained evidence](../launch/evidence/2026-08-22-mvp-f20-t4-completion.md), see §23 |
| **MVP-F21** | **T5** — Existing Taxonomy Migration (dual path, then legacy removal) | D-091 §13 | **PARTIAL 2026-08-22** — migration mechanism proven; cutover pending a Founder mapping, legacy removal deferred — [retained evidence](../launch/evidence/2026-08-22-mvp-f21-t5-legacy-taxonomy-migration.md) |
| **MVP-F22** | **T6** — ST-03 Catalogue Discovery (academic filters + personalisation) | GAP-09, ST-03 | queued — not started |

> **MVP-F17–MVP-F22 are implementation tranches, not canonical denominator features.**
> Authorized by [D-091](../DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy)
> under [D-089](../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).
> Adding them **does not change the 53-feature denominator** and does not by itself promote any
> feature row. Only MVP-F22 can promote ST-03. One tranche at a time; none starts automatically.
> **MVP-F08** (catalogue taxonomy filters) is superseded by MVP-F22, which delivers the same
> capability against the Academic Catalog rather than the legacy three-dimension model.

## 8. MVP-F01 — completed 2026-08-20

**Objective.** Restore valid end-to-end proof for the S5 protected-learning slice, which had none at
`HEAD` since `f25a565` (2026-08-12).

**Change.** New shared module `frontend/e2e/authority-leak.ts` exporting `AUTHORIZATION_FLAG`,
`expectAbsent`, `tokenLabel`. The three S5 leak audits now audit `authorized` at the shape their own
comments describe — a serialised field/flag key — instead of as a bare English substring.

**Not weakened.** The pattern `/(?<!un)authorized\\*["']?\s*:/i` was proven against real leak shapes
before the suites were run:

```
ok catches: {"authorized":true}   {\"authorized\":true}   { authorized: true }
ok catches: {"is_authorized":false}   "AUTHORIZED": true   authorized :true
ok ignores: "...and authorized learning access for GCC students."
ok ignores: \"unauthorized\":\"$undefined\"   {"unauthorized":null}
ok ignores: "You are not authorized to view this lesson."   authorization
```

Every other token in all three lists is unchanged. No production code was modified. Independently
verified beforehand that the only `authorized` occurrences in the failing DOM were the meta
description and Next.js router internals — **no authorization data was leaking**.

**Files.** `frontend/e2e/authority-leak.ts` (new), `frontend/e2e/s5-lesson-player.spec.ts`,
`frontend/e2e/s5-expired-entitlement.spec.ts`, `frontend/e2e/s5-access-ends.spec.ts`.

**Result.**

| Spec | Before | After |
|---|---|---|
| `s5-lesson-player` | 13 pass / 8 fail | **21 pass / 0 fail** |
| `s5-expired-entitlement` | 8 pass / 5 fail | **12 pass / 1 fail** (RC-4, deliberately left) |
| `s5-access-ends` | 1 pass / 1 fail | **2 pass / 0 fail** |

13 of 14 tests restored. Whole suite moved **82 passed / 27 failed → 97 passed / 12 failed**.
`tsc --noEmit` clean; frontend unit 244/244; backend `go test ./...` 26 packages ok, unchanged.
No regression: every previously passing test still passes.

**Moved to `E2E_PROVEN`:** ST-10 (lesson player, HLS, controls, keyboard, WCAG AA at 4 viewports ×
2 directions), ST-11 (progress persistence + resume), ST-14 (access ending mid-session).

**Deliberately left failing:** RC-4 / GAP-04. Narrowing that assertion too would be weakening a test
to go green; it raises a real design question and is queued as MVP-F10.

## 9. Founder decisions required

1. **GAP-06 — scope of 7 screens.** Are Notification Center, Profile/Account, Office Hours, Course
   Analytics, Reported Content, Entitlement Detail, and Public Preview Manager in the current MVP?
   `SCREENS.md` includes them; no post-D-045 decision confirms them; none is implemented. Until this
   is answered their status cannot move off `FOUNDER_DECISION_REQUIRED`.
2. **GAP-03 — report-context exposure.** D-065 forbids the context entering the DOM, but an RSC page
   cannot pass it to a client component without serialising it. Either (a) amend D-065 to permit
   flight-payload presence (the token grants no authority by design), or (b) fetch the context
   client-side only when the report dialog opens. This is an architecture decision, not a bug fix.
3. **GAP-04 — client localisation payload.** Accept shipping the full dictionary, or split it?
4. **Authority for this phase.** The repository is frozen for production behavior under D-086.
   MVP-F01 changed only test assertions, but MVP-F02 onward change production code and need a
   recorded decision and assigned seats.

## 10. MVP-F02 — completed 2026-08-20

Authorized by [D-089](../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).

**Objective.** Make `Admin requests changes → Instructor reads the reason → edits → resubmits →
Admin receives the resubmission` work as a product flow, with no database access, no raw SQL, no
UUID copying, no developer tools, and no out-of-UI API calls.

**Lifecycle before.** `catalog/review.go:672` writes `review_reason` and moves the revision to
`CHANGES_REQUESTED` (first publish) or `REJECTED` (published-Course revision, FR-052).
`loadRevisionGraphByID` (`catalog/review.go:195`) selects it, and it is served to the owning
Instructor by `GET /api/v1/courses` and `GET /api/v1/courses/:id` with the JSON tag
`review_reason` (`catalog/revision.go:111`). **The backend contract was already complete.** The
break was entirely in the frontend: `CourseRevisionWire` (`lib/api/catalog.ts`) omitted the field,
so TypeScript dropped it, and `course-builder.tsx` rendered only the raw wire enum
(`{revision?.state ?? lifecycle}`). An Instructor whose Course came back had no way inside the
product to learn why.

**No backend change was required or made.**

**Lifecycle facts established by tracing** (unchanged by this tranche):

- Resubmission **mutates the same revision** — `submit` sets `state = 'PENDING_REVIEW'` on the
  existing row (`catalog/authoring.go:1730`). It does not create a new revision.
- `review_reason` is **retained** on the row after resubmission; nothing clears it. It therefore
  describes the *last decision*, not the current state.
- The editable-candidate query includes `DRAFT`, `CHANGES_REQUESTED`, and `PENDING_REVIEW`
  (`catalog/authoring.go:596`), so a returned Course stays editable by its owner.

That retention is the subtle correctness point of this tranche: a surface keyed on the presence of
a reason would keep telling the Instructor to fix work they had already resubmitted. The notice is
keyed on `state` alone, and that rule carries its own unit tests.

**Files changed**

| File | Why |
|---|---|
| `src/lib/api/catalog.ts` | Added `review_reason` and `reviewed_at` to `CourseRevisionWire`, with the retention caveat documented |
| `src/components/instructor/change-request-state.ts` (new) | `isReturnedForChanges` — the state-gating rule, in a `.ts` module so it is unit-testable |
| `src/components/instructor/change-request-state.test.ts` (new) | 7 tests, including the resubmission and stale-draft cases |
| `src/components/instructor/change-request-notice.tsx` (new) | Standing notice: title, the Admin's verbatim reason, and the expected next step. Persistent region, not a toast — the Instructor usually returns in a later session |
| `src/components/instructor/course-builder.tsx` | Renders the notice; replaces the raw wire enum with a localized state label, keeping the enum on `data-revision-state` for tests and support |
| `src/lib/i18n/dictionaries/{en,ar}.ts` | New `instructor` namespace: localized revision-state labels and change-request copy. No visible string is hardcoded in the new component |
| `tsconfig.test.json` | Includes `src/components/instructor/**/*.ts` so the new unit tests build |
| `e2e/media-authoring/s12-instructor-video-upload.spec.ts` | Extended with the F02 journey and its authorization proofs |

**Why that E2E spec.** It is the only suite with real object storage, a running worker, and ffmpeg,
which submission requires: `validation.go:119` refuses any Lesson without a READY video. It already
drove upload → READY → submit → Admin queue → request-changes, but it **resubmitted through a raw
API call and never asserted the Instructor could see anything**. The extension replaces the raw
resubmit with the studio UI and adds the missing Instructor-side assertions. No state under test is
seeded.

**Journey driven** (all through the product):

1. Admin creates taxonomy terms · 2. Instructor creates a Course, Section, Lesson in the studio ·
3. real MP4 uploaded to MinIO, worker transcodes it to `READY` · 4. Instructor assigns taxonomy and
**submits** · 5. Admin opens the review queue and the submitted-revision inspector ·
6. Admin **requests changes** with the reason `Please update lesson 2 learning objectives` ·
7. **Instructor reloads the studio and reads that exact string** · 8. state reads
`Changes requested`, with `data-revision-state="CHANGES_REQUESTED"` · 9. Instructor **edits** the
English title and saves · 10. Instructor **resubmits from the studio** · 11. the notice disappears
and the state reads `In review` / `PENDING_REVIEW` · 12. Admin reloads and sees the resubmitted
revision in `PENDING_REVIEW`.

**Authorization proved in the same run** — a non-owning Instructor and a Student are both refused
`GET /courses/:id` and neither response body contains the reason; a Student and the owning
Instructor are both refused the Admin `request-changes` mutation. No check was weakened.

**Results**

| Suite | Before F02 | After F02 |
|---|---|---|
| `go build` / `go vet` / `go test ./...` | 26 ok, 0 fail | 26 ok, 0 fail (untouched) |
| `tsc --noEmit` | clean | clean |
| Frontend unit | 244 pass | **251 pass** (+7) |
| `media-authoring` targeted | 1 pass (without F02 assertions) | **1 pass** (with them) |
| `s12-instructor-authoring` isolated | 4 pass | 4 pass |
| `s14-admin-catalog-review` isolated | 4 pass | 4 pass |

**Moved to `E2E_PROVEN`:** IN-06 submit success, IN-07 change-request reason visible, IN-08 revise
and resubmit, AD-11 request changes with reason. AD-10 publish moved
`IMPLEMENTED_NOT_PROVEN → PARTIAL` (the approval transition was observed green; catalogue arrival
remains MVP-F03).

**Not addressed, by instruction:** GAP-02 catalogue arrival (MVP-F03), GAP-03 report context,
GAP-04 localisation payload, GAP-05 suite isolation, GAP-07 language route, GAP-06 screen scope,
and all ops blockers.

## 11. MVP-F03 — completed 2026-08-20

Authorized by [D-089](../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).

**Publication contract, as traced (not assumed).**

Approval **is** publication — one operation, one transaction. `ApproveCourse`
(`catalog/review.go:230`) runs `lockApprovalTarget → revalidateApproval → commitApproval`, and
`commitApproval` (`:485`) performs, atomically:

1. previous live revision → `SUPERSEDED` (`supersedePreviousRevision`, `:506`)
2. candidate revision → `APPROVED` (`approveCandidateRevision`, `:520`)
3. `courses.live_revision_id = candidate`, `courses.lifecycle = 'PUBLISHED'` (`swapLiveRevision`, `:532`)
4. audit event + approval notification

`revalidateApproval` (`:327`) re-runs the **full submission validation** inside the transaction —
owner eligibility, taxonomy locks, asset locks, and `validateCourseForSubmission`. Publication
constraints are therefore enforced server-side at the moment of publication, not merely at submit.

Public visibility is `catalogpublic.PublishedOnly` (`visibility.go:14`):

```
c.lifecycle = 'PUBLISHED'
AND c.access_suspended_at IS NULL
AND c.retired_at IS NULL
AND c.live_revision_id = cr.id
```

Answers to the traced questions: approval and publication are the **same** operation; only
`PUBLISHED`, unsuspended, unretired Courses are eligible; the publicly active revision is exactly the
one `live_revision_id` points at (state `APPROVED`); approval promotes the candidate and supersedes
the predecessor; visibility is a **combination** of lifecycle + suspension + retirement + the live
pointer; there is **no asynchronous or cache hop** — the same transaction, and the frontend fetches
`cache: "no-store"`; Arabic and English come from the **same** revision row selected by an `arabic`
flag, not separate entities; exclusion is **server-side in SQL**, and `NewRepository` refuses
construction without a predicate (`ErrVisibilityNil`), so a query cannot silently fall back to
unfiltered data; the detail route applies the same predicate and returns 404.

**Before F03: the feature was not broken — it was unproven.** Case A. The publication path and the
catalogue query were already correct. What did not exist was any test proving an approved Course
becomes publicly discoverable, so every published Course the Student-facing specs saw came from the
seeder.

**No production code was changed in this tranche.**

**Files changed:** `e2e/media-authoring/s12-instructor-video-upload.spec.ts` only (plus this record
and the matrix). Extending that spec avoided duplicating the expensive Course-building flow.

**Journey driven, end to end, nothing seeded into a published state:**

Instructor signs in → creates a uniquely titled Course → Section → Lesson → uploads a **real MP4** →
worker transcodes to `READY` → assigns Admin-created taxonomy → submits. Admin signs in → finds the
Course in the **review queue by title** → opens the inspector → sets Course and Section price →
requests changes → (F02 loop) → Instructor resubmits from the studio → Admin **approves** through the
inspector. Then a **fresh anonymous BrowserContext with no cookies** opens `/en/catalog`, searches by
the unique human title, sees the Course card carrying taxonomy (`Engineering`, `Programming`),
instructor (`Dr. Instructor`) and price (`25.000`), asserts no commerce CTA appears (D-045), clicks
through to Course Details, and matches it to the Instructor's Course.

**Negative visibility, all proven:**

- a `DRAFT` Course created through the real studio is absent from public search, absent from the
  rendered catalogue, and its direct public detail request returns **404**
- a `PENDING_REVIEW` revision **B** of the already-published Course never leaks: public search does
  not contain its title, the public detail route still returns revision **A**, and the rendered
  public page still shows A
- a second approval of an already-approved revision is refused (409/422) and changes nothing
- an anonymous request to `/api/v1/learn/courses/:id` is refused — public Course Details does not
  grant protected-learning access

**GAP-11 discovered, recorded, not fixed.** Driving revision B required
`PUT /api/v1/courses/:id/candidate` directly, because **no Instructor UI calls it**:
`course-builder.tsx` and `lib/api/authoring.ts` contain no reference, and `ListOwnedCourses` only
returns an `editable_revision` when one already exists — it never mints one. A published Course is
therefore a dead end for its own Instructor in the product. That is an authoring gap, not a
publication-visibility defect, so it is registered as GAP-11 against IN-10 and left for its own
tranche. The catalogue-isolation guarantee under test is unaffected and was exercised against
genuine published/pending states.

**Results**

| Suite | F02 baseline | After F03 |
|---|---|---|
| `go build` / `go vet` | clean | clean |
| `go test ./...` | 26 ok, 0 fail | 26 ok, 0 fail |
| `tsc --noEmit` | clean | clean |
| Frontend unit | 251 pass | 251 pass |
| `media-authoring` targeted | 1 pass | **1 pass** (now including the full publication + isolation journey) |
| `s12` isolated | 4 pass | 4 pass |
| `s14` isolated | 4 pass | 4 pass |
| `s6` isolated | 2 pass | 2 pass |
| `s3` isolated | 20 pass / 4 fail | 20 pass / 4 fail (GAP-07, unchanged) |
| Broader suite | 97 pass / 12 fail / 3 not run | **97 pass / 12 fail / 3 not run** |

The 12 broader-suite failures were compared **by test identity**, not by count: `s13:94`,
`s3:127` ×3, `s3:211`, `s5-expired-entitlement:712`, `s5-playback-performance:157`,
`s5-viewport-evidence:223` ×4, `s6:29`. Identical set to the F02 baseline. Zero F03 regressions.

**Matrix changes:** AD-10 publish `PARTIAL → E2E_PROVEN`. GAP-02 **CLOSED**. ST-02 and ST-04
evidence strengthened (both stay `PARTIAL` on GAP-07 and missing access guidance respectively).
IN-10 restated as GAP-11 `FRONTEND_MISSING`. Denominator unchanged at 52.

## 12. MVP-F11 — completed 2026-08-20

Authorized by [D-089](../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time). Closes **GAP-11**.

**Revision contract, as traced.** `courses.live_revision_id` names the published revision — the only
one the public sees, because `catalogpublic.PublishedOnly` joins on it. A *candidate* is a separate
`course_revisions` row. `CreateCandidate` (`catalog/authoring.go:395`):

- locks the Course and refuses a non-owner with `ErrCourseNotFound` (no existence leak), then
  requires an `ACTIVE` `INSTRUCTOR` owner (`checkOwnerActive`);
- **returns any existing candidate** in `DRAFT`/`CHANGES_REQUESTED`/`PENDING_REVIEW` instead of
  cloning a second — so it is idempotent and cannot fork a Course;
- otherwise clones the live revision: metadata, taxonomy, `preview_asset_version_id`, sections,
  lessons **including `video_asset_version_id`**, and lesson files, as `state = 'DRAFT'` with
  `based_on_revision_id = live` and `revision_number = max+1`;
- **never touches `live_revision_id`**, so A keeps serving throughout;
- writes a `COURSE_CANDIDATE_CREATED` audit event.

Course price and `default_access_ends_at` are Course-scoped, not revision-scoped, so they are not
cloned and survive independently. A Course that was never published has nothing to clone and the
server refuses.

**Before F11 the published Course was a UI dead end.** The studio rendered a terminal sentence —
`data-testid="no-editable-revision"`, "This Course has no editable revision." — with no action.
`PUT /api/v1/courses/:id/candidate` existed and worked, but neither `course-builder.tsx` nor
`lib/api/authoring.ts` referenced it, and `ListOwnedCourses` only returns an `editable_revision`
when one already exists. An Instructor could publish a Course and then never change it again.

**Root cause: frontend only.** Case A. No backend production code was changed. One frontend
contract bug was found and fixed along the way: the studio list payload carries `live_revision_id`
but **not** the expanded `live_revision` graph (only `GetOwnedCourse` expands it), so a first
implementation keyed on the graph reported "no published revision" for every Course in the list.

**Files changed** — all frontend:

| File | Why |
|---|---|
| `src/lib/api/catalog.ts` | Added `live_revision_id` to `OwnedCourseSummary`, documenting which payload expands the graph and which does not |
| `src/lib/api/authoring.ts` | `createCandidateRevision` — the client for the existing endpoint |
| `src/components/instructor/revision-workflow.ts` (new) | `revisionWorkflow` / `editsPublishedCourse` — mirrors the server's own candidate rule |
| `src/components/instructor/revision-workflow.test.ts` (new) | 8 tests, including both payload shapes and the PENDING_REVIEW case |
| `src/components/instructor/revision-workflow-panel.tsx` (new) | The start-revision action, the in-review state, and the published-vs-draft notice |
| `src/components/instructor/course-builder.tsx` | Replaced the dead-end sentence with the panel; wired `handleStartRevision` through the existing `command` helper so pending/failure behave like every other mutation |
| `src/lib/i18n/dictionaries/{en,ar}.ts` | `instructor.revision` namespace, Arabic and English. No visible string hardcoded |
| `e2e/media-authoring/s12-instructor-video-upload.spec.ts` | The F03 raw `PUT …/candidate` call replaced by real UI interaction, plus the F11 assertions |

**Instructor UX flow.** Published Course → panel says "This course is published" and that the
published version keeps serving until an administrator approves → **Start a new revision** →
studio opens the cloned `DRAFT` and shows "Students still see the published version" → edit → save →
**Submit for review** → state reads *In review*, and the start-revision action is gone.

**Proven in the E2E run**

- the candidate is created **by clicking the studio**; the spec never calls `PUT …/candidate` to
  create the revision under test
- the new revision id differs from the published one
- reload + re-open does **not** fork: the same candidate id is returned and the action is absent
- isolation while B is `DRAFT`: the saved edit does not reach the public Course
- isolation while B is `PENDING_REVIEW`: public search and public detail still return revision A,
  and the rendered public page still shows A
- the Admin finds the resubmitted revision in the normal queue, by title
- authorization: a non-owning Instructor, a Student, and an anonymous visitor are each refused
  candidate creation
- a never-published Course offers neither the start-revision action nor the published notice

**Results**

| Suite | F03 baseline | After F11 |
|---|---|---|
| `go build` / `go vet` / `go test ./...` | 26 ok, 0 fail | 26 ok, 0 fail (untouched) |
| `tsc --noEmit` | clean | clean |
| Frontend unit | 251 pass | **259 pass** (+8) |
| `media-authoring` targeted | 1 pass | **1 pass** (now UI-driven) |
| `s12` / `s14` isolated | 4 / 4 pass | 4 / 4 pass |
| Broader suite | 97 / 12 / 3 | **97 / 12 / 3** |

Failures verified **by identity**: `s13:94`, `s3:127`×3, `s3:211`, `s5-expired-entitlement:712`,
`s5-playback-performance:157`, `s5-viewport-evidence:223`×4, `s6:29`. Identical set. Zero regressions.

## 13. MVP-F05 — completed 2026-08-20

Authorized by [D-089](../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time). Closes **GAP-07**.

**Locale architecture, re-traced.** Locales `["en","ar"]`, default `ar`. Locale prefixes are
**not** global: only `/[locale]/{catalog,learn,access,admin,instructor}` carry one. The landing page,
the auth screens and `/staff` are served without a locale segment, and there is no
`/[locale]/page.tsx`. `<html lang>`/`<html dir>` are owned by `LocaleProvider`, which writes them in
an effect; the dictionary is selected by the same provider. Three language toggles existed:

| Toggle | Used by | Behaviour before F05 |
|---|---|---|
| `common/language-toggle.tsx` | `Navbar` → landing + catalogue **list** | `toggleLocale()` only — state + `localStorage`, **no navigation** |
| `learning/learning-locale-toggle.tsx` | learning routes | private `withLocale`, navigated correctly, **dropped query** |
| `CatalogueLanguageToggle` in `public-catalogue.tsx` | catalogue **detail** | own string edit, navigated, **dropped query**, and replaced `segments[1]` unconditionally so a non-prefixed path like `/login` became `/en` |

**Root cause.** The canonical header toggle never rewrote the locale route segment. The URL is part
of application state on locale-addressed routes, so mutating `lang`/`dir` alone left the document
disagreeing with its own address: reload, a shared link, or Back all returned the visitor to the
previous language. The accessible name compounded it — `en.ts` said "Switch language to Arabic"
while its Arabic counterpart said the equivalent of "Switch to English", so the two languages named
the same control differently and the English assertion could not find it.

**Before F05.** On `/ar/catalog` a visitor clicked the language control, saw the page turn English,
and the URL stayed `/ar/catalog`. Reloading returned them to Arabic.

**Canonical contract now implemented.** On a locale-addressed route the switch replaces **only the
locale segment**, preserves the rest of the path (including dynamic slugs) and the query string, and
navigates. On a route with no locale segment there is no locale-addressed equivalent, so the saved
preference switches in place rather than navigating to a manufactured 404. After settling, URL
locale, `<html lang>`, `<html dir>` and the rendered dictionary all agree.

**No backend production code changed.** Files:

| File | Why |
|---|---|
| `src/lib/i18n/locale-path.ts` (new) | `switchLocalePath` / `localeFromPath` — one segment-safe helper, query-preserving, returning `null` where no equivalent exists |
| `src/lib/i18n/locale-path.test.ts` (new) | 8 tests including paths containing `ar`/`en` elsewhere (`arabic-101`, `linear-algebra`, `engineering`, `dear-search`), query forms, and unsupported segments |
| `src/components/common/language-toggle.tsx` | Now navigates via the helper; reads the query from `window.location.search` at click time rather than `useSearchParams`, which would push every page carrying the header into dynamic rendering |
| `src/components/learning/learning-locale-toggle.tsx` | Private `withLocale` deleted in favour of the shared helper |
| `src/components/catalog/public-catalogue.tsx` | Inline segment edit replaced by the shared helper, removing the `/login → /en` corruption |
| `src/lib/i18n/dictionaries/en.ts` | `switchToAria` → "Switch to Arabic", mirroring the Arabic label |
| `e2e/s3-public-catalogue.spec.ts` | Four F05 tests added |

**E2E proof (all via the real control; no router API called by the tests).** `/ar/catalog` →
click → `/en/catalog` → click → `/ar/catalog`, asserting URL, `lang`, `dir`, and the real catalogue
heading each time. `/ar/catalog?q=algorithms` → `/en/catalog?q=algorithms` with the search input
still holding the term. `/ar/catalog/<slug>` → `/en/catalog/<slug>` — the same Course, not the
catalogue root. Keyboard: the control is focusable and operates on `Enter`. Both locales are served
from one mock keyed on `Accept-Language`, so a switch that failed to re-request would show stale
content and fail.

**SSR finding — `SEPARATE_GAP` (new GAP-12).** The root layout still hardcodes `lang="ar" dir="rtl"`
and `LocaleProvider` corrects it after mount. Only the root layout renders `<html>`, and a nested
`[locale]` layout cannot change it, so server-correct locale emission requires moving every route —
landing, auth, `/staff` included — under `app/[locale]/`. That is a routing restructure well beyond
this tranche. The **settled** state is fully consistent, which is what the canonical contract
(BR-149, NAVIGATION_RULES §4, "switch `lang`/`dir` at the application shell") requires; the
violation is limited to first paint before hydration.

### Regression — and a material finding about the broader suite

Per-spec isolation, which is the only trustworthy signal here:

| Spec | Isolated result |
|---|---|
| `s3-public-catalogue` | **28 passed / 0 failed** (was 20/4) |
| `s2-taxonomy-viewport` | 12 passed |
| `s5-lesson-player` | 21 passed |
| `s5-course-home` | 12 passed |
| `s5-access-ends` | 2 passed |
| `s5-infrastructure-smoke` | 3 passed |
| `s6`, `s13`, `s11` | 2 / 2 / 1 passed |
| `s12`, `s14`, `legal-policy-pages` | 4 / 4 / 4 passed |
| `s5-expired-entitlement` | 12 passed / **1 failed** (`:712`, GAP-04) |
| `s5-viewport-evidence` | **4 failed** (GAP-03) |
| `s5-playback-performance` | **1 failed** (perf) |

Six real failures remain, all pre-existing and all previously registered.

**The broader suite is now demonstrably non-deterministic.** Two consecutive runs of *identical*
code produced different failure sets — run A: `s11:78, s12:86, s12:181, s14:189, s5-course-home:125
(tablet), s5-expired:365, s5-infra:73, s3:127 (en desktop)…`; run B: `s2-taxonomy:36,
s5-expired:329, s5-course-home:125 (phone), s3:127 (en phone)…`. Every one of those specs passes
alone. The earlier stable `97/12` was an artefact of a fixed execution order; adding four tests
perturbed it and exposed the underlying instability. This is **GAP-05** — one seeded database shared
by all specs — and it means the whole-suite count cannot be used as a regression signal at all.
GAP-05 is therefore promoted from HIGH to **BLOCKER** for release evidence: the repository cannot
produce a trustworthy whole-suite run today.

Backend untouched and green: `go build`, `go vet`, `go test ./...` 26 ok / 0 fail.
`tsc --noEmit` clean. Frontend unit **267 pass** (+8).

## 14. MVP-F06 — completed 2026-08-20

Authorized by [D-089](../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time). Closes **GAP-05**.

**Harness architecture.** One Playwright `globalSetup` per run: it builds the Go API and seeder
binaries to **fixed** `/var/tmp` paths, creates one database `gradex_playwright_e2e_<runId>`, seeds
it once, starts one Go API and one Next server on run-allocated ports, and writes one
`RUN_STATE_FILE_PATH`. `acquireEnvironmentLock` throws outright if a second run appears. Redis,
MinIO and the media worker are likewise single instances shared by the whole run.

**Root cause.** `fullyParallel: false` only serialises tests *within* a file — Playwright still ran
spec **files** concurrently, defaulting to half the machine's cores (6 of 12 here) against that one
shared database. Several specs then mutate **shared seeded authority rows by fixed id** through the
seeder binary: `s5-access-ends` revokes entitlements and suspends accounts
(`applyAuthorityMutation`, `e2e/s5-access-ends.spec.ts:135`), `s5-expired-entitlement` rewrites
access windows — while `s5-lesson-player`, `s5-course-home`, `s6` and `s11` read those same rows.
Concurrently, `s5-access-ends` revoked a Student's entitlement while `s5-lesson-player` was mid
playback for that Student. The failing set became a property of execution order, not of the code.

This is **not** identifier collision, so unique-data factories (Option A) cannot fix it: the specs
are deliberately testing authority *removal* on shared fixtures.

**Isolation design chosen: pin the worker count to the model the harness already documents.**
Per-worker isolation (Option C) was evaluated and rejected as a rebuild rather than a fix — it would
require a per-worker database, Go API, Next server, port set, Redis namespace, MinIO prefix, lock
file, run-state file and seeder invocation, replacing `webServer` with hand-managed per-worker
stacks and multiplying resource use ~6×. Every one of those is a fixed module-level constant today.
Recorded as test-infrastructure debt, not attempted here.

To stop this regressing silently, `workers: 1` is pinned in **both** Playwright configs with the
rationale inline, and `globalSetup` now refuses to start under concurrency — `--workers=N` on the
command line otherwise overrides the config. Verified: `--workers=3` aborts with an explanatory
error instead of producing an untrustworthy green.

**Anti-gaming.** No retries, no `waitForTimeout`, no `.skip`/`.fixme`, no timeout inflation, no
caught failures, no weakened assertions, no deleted or reordered tests — the diff of the two changed
files contains zero such constructs. The suite is deterministic *including* its six genuine
failures, which is the intended outcome.

**Determinism proof.** Five clean full-suite runs, three of them consecutive and uninterrupted:

| Run | Passed | Failed | Did not run |
|---|---:|---:|---:|
| standalone `--workers=1` | 107 | 6 | 3 |
| Run 1 | 107 | 6 | 3 |
| Run 2 | **81** | **31** | **4** |
| Run 3 | 107 | 6 | 3 |
| Clean A | 107 | 6 | 3 |
| Clean B | 107 | 6 | 3 |
| Clean C | 107 | 6 | 3 |

Failure identities were compared, not just counts: every agreeing run failed on exactly
`s5-expired-entitlement:712`, `s5-playback-performance:157`, and `s5-viewport-evidence:223` ×4.

**Run 2 is recorded as an anomaly, not as a pass.** It is the run during which two
`--config=playwright.media-authoring.config.ts` commands were issued concurrently — an operator
error. Its profile suggests process-level disturbance rather than row contamination
(`s5-infrastructure-smoke` "a rotating Student's server-issued session is accepted by production
middleware" failed, plus a stray 502). But the media `globalSetup` acquires the lock *before*
compiling to the shared binary paths and both attempts did throw at the lock, so that explanation
is **not proven**. The clean triple was run afterwards specifically because three-of-four was not
good enough. Anyone re-running this suite must not invoke the media-authoring config concurrently.

**Genuine remaining failures — all registered, all reproduce in isolation:**

| Test | Cause |
|---|---|
| `s5-expired-entitlement:712` | **GAP-04** — full `dictionary.learning` in the flight payload |
| `s5-viewport-evidence:223` ×4 | **GAP-03** — `report_context` serialised into the DOM (D-065) |
| `s5-playback-performance:157` | Performance — phone time-to-first-frame over budget |

None disappeared under isolation; none is masked by the fix.

**Runtime.** Before: ~3.0–5.0 min at 6 workers, non-deterministic. After: **~6.8–7.1 min at 1
worker**, deterministic. Roughly +2 to +4 minutes bought a trustworthy release instrument. Recorded
as accepted test-infrastructure cost; per-worker isolation is the way to recover it later.

**Regression gates.** `go build`, `go vet`, `go test ./...` → 26 ok / 0 fail. `tsc --noEmit` clean.
Frontend unit 267 pass / 0 fail. `media-authoring` config 1 passed under its pinned worker count.
**No production code changed** — the diff is `playwright.config.ts`,
`playwright.media-authoring.config.ts`, and `e2e/global-setup.ts` only.

**No feature score change.** F06 repaired the measurement instrument; it produced no new feature
evidence, so no row was promoted and the denominator stays 52.

### New canonical E2E baseline

**`107 passed / 6 failed / 3 did not run`** — `npx playwright test`, one worker, against the local
`backend/docker-compose.yml` stack. The six failures above are the registered set. Every subsequent
tranche compares against this, **by failure identity**, not by count. A run that differs in identity
is a regression; a run that differs in count without explanation means the harness was disturbed.

## 15. MVP-F12 — completed 2026-08-20

Authorized by [D-089](../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time). Closes the **ST-07** gap.

**Access lifecycle contract.** Two entities, never to be conflated. The **invitation** is a workflow
record — `PENDING_STUDENT_ACCEPTANCE → PENDING_ADMIN_APPROVAL → APPROVED | REJECTED | CANCELLED` —
and grants nothing. The **entitlement** is the authority, created only by Admin approval, and is
what `has_active_access` / `access_ends_at` report. An approved invitation whose entitlement has
since ended is therefore *access ended*, not *active*: reading the invitation alone would tell a
Student they still hold a Course they cannot open.

**Before F12.** `GET /api/v1/me/course-access` was **already** token-free and session-scoped
(`access_routes.go:858` derives the Student from `ctxUserIDKey`, never from a parameter), and so was
`/me/course-access-invitations`. The persistent model existed; nothing surfaced it. The page was
reachable only by re-opening the invitation link, had no navigation entry, rendered
`Course: <uuid>` and `Status: PENDING_ADMIN_APPROVAL` as user-facing copy, and was 100% hardcoded
English in an Arabic-default product.

**Root cause: one backend contract gap plus a frontend surface.** The history projection carried
`course_id` but no title, so the page had nothing but the UUID to name the Course with. Everything
else was frontend.

**Backend production change (minimal, justified).** `StudentCourseAccessHistoryItem` gains
`course_title`, resolved at the HTTP boundary from `Accept-Language` exactly as
`localizedLearningTitle` does for the learning read models. `attachCourseTitles` loads both authored
titles for every Course in the history in **one** query, preferring the live revision and falling
back to the highest revision so a delisted or never-published Course is still nameable. The authored
pair is `json:"-"` and never leaves the process. No lifecycle, authorization, or entitlement
semantics changed.

**Frontend.** New `components/access/access-state.ts` (the backend→Student state map, with the
active-entitlement-wins rule and a rejection-reason guard) + 8 unit tests · `access-records.tsx` ·
the rebuilt `app/[locale]/access/page.tsx`, split into a token-dependent invitation panel and a
persistent record list · `access` namespace in both dictionaries · a `Course access` link on the
learning dashboard · `locale-provider.tsx` now treats `/[locale]/access` as language-addressable
alongside `catalog` and `learn` (Admin/Instructor stay excluded on purpose).

**Student state map.**

| Backend | English | Arabic | Student action |
|---|---|---|---|
| invitation `PENDING_STUDENT_ACCEPTANCE` | Action needed | مطلوب إجراء | Accept from the invitation link |
| invitation `PENDING_ADMIN_APPROVAL` | Waiting for approval | بانتظار الموافقة | None — Gradex waits on the Admin |
| active entitlement | Access granted | تم منح الوصول | **Go to course** |
| approved, entitlement ended | Access ended | انتهى الوصول | None |
| invitation `REJECTED` | Not approved | لم تتم الموافقة | Read the reason, if one was given |
| invitation `CANCELLED` | Withdrawn | تم السحب | None |
| unclassifiable | No access | لا يوجد وصول | None |

**E2E — extended `s6-course-access-grant-launch`, driven entirely through the product.** Admin
invites by Course title and Student email → Student follows the link, sees the **Course title** and
"Action needed" → accepts → sees "Waiting for approval" and that an administrator still has to
approve → **leaves the invitation context entirely**, goes to the dashboard, clicks *Course access*,
lands on `/en/access` with no `token` or `invitation_id` in the URL and no invitation panel, and the
record is still there → Admin approves → Student returns the same way and reads "Access granted"
with an access-until date → the same record in **Arabic** asserts real copy
(`الوصول إلى المقررات`, `تم منح الوصول`, `افتح المقرر`), not merely `dir="rtl"` → **Go to course**
reaches the entitled Course Home. `expectNoInternalIdentifiers` runs at every step and fails on any
UUID or backend term in the rendered text.

**Regression — canonical F06 baseline restored exactly.**

| | F06 baseline | After F12 |
|---|---|---|
| Playwright | 107 / 6 / 3 | **107 / 6 / 3** |
| Failure identities | `s5-expired-entitlement:712`, `s5-playback-performance:157`, `s5-viewport-evidence:223` ×4 | **identical** |
| `go build` / `vet` / `go test ./...` | 26 ok / 0 fail | 26 ok / 0 fail |
| `tsc --noEmit` | clean | clean |
| Frontend unit | 267 | **275** (+8) |

**Two test corrections, both recorded rather than quiet.** `s11-release-acceptance` drove the old
control name and asserted `PENDING_ADMIN_APPROVAL` in the Student's visible text — exactly the
behaviour F12 deliberately removed. Its selectors were updated to the current contract and the wire
state is still asserted, on the `data-access-state` attribute; its authority assertions
(`expectZeroGrantState`, denial statuses) are untouched. `s6`'s describe timeout moved 30 s → 120 s
because F12 added two dashboard round-trips and an Arabic pass to an already 30-step journey — a
work budget matching the convention the other long S5 journeys already use, not a retry or a
stabilisation device.

**ST-07 → `E2E_PROVEN`.** Rejection and cancellation state *rendering* is unit-proven and
API-proven, but is **not** E2E-proven through the browser — recorded honestly below rather than
claimed.

## 16. MVP-F13 — completed 2026-08-20 (ST-04 remains PARTIAL)

Authorized by [D-089](../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).

**Course Details contract.** `/[locale]/catalog/[idOrSlug]` is public and served by
`GET /api/v1/catalog/courses/:idOrSlug` under `PublishedOnly`. The Student's relationship to that
Course comes from the separate, session-scoped `GET /me/course-access` built in F12. Course Home
continues to enforce entitlement server-side; Course Details only chooses which action to offer.

**Before F13.** The funnel ended here. A visitor could evaluate a Course and the page never said how
access is obtained; an entitled Student got no way in; a Student mid-lifecycle saw nothing about
their pending request.

**Root cause: frontend only.** Both data sources already existed. No backend change was made.

**State precedence.** `GET /me/course-access` already collapses a Course's records into one item, so
precedence is settled server-side; `studentAccessState` then applies the rule that an active
entitlement outranks whatever the invitation says. The frontend adds one defence: if several rows
ever arrive for one Course, the row holding active access wins — ordering is never trusted. A failed
lookup resolves to `UNAVAILABLE`, never `NO_ACCESS`, so a transient outage cannot tell an entitled
Student they have nothing.

| Relationship | English | Arabic | Action offered |
|---|---|---|---|
| `ANONYMOUS` | Sign in to see whether you already have access… | سجّل الدخول لمعرفة… | Sign in |
| `NO_ACCESS` | …Access begins with an administrator invitation. | يبدأ الوصول بدعوة من المشرف. | none |
| `ACTION_REQUIRED` | Open the invitation link sent to your email… | افتح رابط الدعوة… | View access status |
| `AWAITING_APPROVAL` | An administrator still has to approve it… | يبقى على المشرف اعتمادها… | View access status |
| `ACTIVE` | You have access to this course. | لديك وصول إلى هذا المقرر. | **Go to course** |
| `ACCESS_ENDED` | Your access to this course has ended. | انتهى وصولك… | View access status |
| `REJECTED` | An administrator did not approve… | لم يوافق المشرف… | View access status |
| `CANCELLED` | The invitation … was withdrawn. | سُحبت الدعوة… | View access status |
| `UNAVAILABLE` | Your access status could not be loaded… | تعذّر تحميل حالة وصولك… | Retry |

**No purchase path exists in any state.** `expectNoCommerce` asserts, on every Course Details state
exercised, that no buy/cart/checkout/purchase/pay wording or control appears in either language.

### Preview requirement audit — `REQUIRED_BUT_MISSING`

[SCREENS.md](../SCREENS.md) ST02 line 157 lists "Play optional Public Preview" as a canonical MVP
action, and line 159 defines its absent state. No later decision defers it.

What exists: an **anonymous** delivery route `GET /api/v1/media/content/previews/:id`
(`media_delivery_handlers.go:40`, registered outside the protected group) backed by
`IssuePreview(ctx, assetVersionID)`.

What is missing: the public Course projection exposes only `has_preview bool`
(`catalogpublic/repository.go:42`) — it never carries the preview asset version id, so no client can
call that route. IN05, the Instructor surface that would set a preview asset, is also unbuilt.

Because a canonical ST02 requirement remains unmet, **ST-04 stays `PARTIAL`** rather than being
promoted. Its gap is now narrowed to exactly one item: the public preview player and the projection
field it needs. Access guidance and entitlement-aware actions — the structural dead end — are closed.

**Files changed — all frontend.** `catalog/course-access-relationship.ts` (+8 tests) ·
`catalog/course-access-panel.tsx` · `catalog/public-catalogue.tsx` (access lookup + panel) ·
`access` dictionary namespace extended in both languages · `e2e/s6-course-access-grant-launch.spec.ts`
and `e2e/s3-public-catalogue.spec.ts` extended.

**E2E.** Anonymous: Course renders in full, `ANONYMOUS` panel, guidance, sign-in, no Go-to-course, no
commerce, and protected learning still refused — plus an Arabic pass asserting real copy. Signed-in:
after acceptance Course Details reads `AWAITING_APPROVAL` with no entry action; after approval it
reads `ACTIVE`, the Arabic page shows `لديك وصول إلى هذا المقرر.` and `افتح المقرر`, and **Go to
course** reaches the entitled Course Home.

**Regression.** **109 passed / 6 failed / 3 did not run** — the canonical six by identity
(`s5-expired-entitlement:712`, `s5-playback-performance:157`, `s5-viewport-evidence:223` ×4), +2 net
new tests. Backend 26 ok / 0 fail (untouched). `tsc` clean. Unit **283** (+8).

**Matrix.** ST-04 stays `PARTIAL`, gap narrowed to public preview only. **No score change** — the
feature count is not moved for partial progress.

## 17. MVP-F15 — 2026-08-20 — IMPLEMENTED, NOT E2E-PROVEN

Authorized by [D-089](../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).
**Neither ST-12 nor ST-08 is promoted.** The implementation landed and is backend- and unit-proven;
the browser proof did not, and the tranche is recorded at the evidence level actually reached.

**Progress contract (traced).** `progress(enrollment_id, course_lesson_identity_id)` already carries
`last_position_seconds`, `max_position_seconds`, `completed_at` and **`last_watched_at`**
(`learning/progress.go:96-107`). **No new persistence was required** — the resume pointer is fully
derivable from rows that already exist. That is the central ST-12 finding.

**Resume algorithm.** `ListStudentResumeCandidates` (`learning/coursehome.go`) is one bounded query
with a per-Course window, joining **only** the approved live revision — so an Instructor's in-flight
candidate revision can never redirect a Student, and a Lesson dropped from the live graph stops being
a candidate rather than yielding a broken pointer. Ordering is total: a part-finished Lesson outranks
an unstarted one, then most recent `last_watched_at`, then the Course's own recency, then stable
Course id. Entitlement is deliberately **not** filtered in SQL — enrollment and Progress outlive
access by design — so `resumeReadModel` (`httpapi/learning_handlers.go`) applies the authoritative
evaluator and keeps only `ReadActive`. An expired Course keeps its history and yields no pointer.

**Student scoping** comes from the session (`ctxUserIDKey`), never a parameter.

**Delivered:** backend `dashboardResumeResponse` on `GET /learn/dashboard`; frontend
`LearningResume` wire type; a Continue-learning block on the dashboard using it, with
started-vs-unstarted copy; bilingual dictionary entries.

**Not delivered:** the ST-08 pending-access summary (`n courses waiting for approval`), which
`SCREENS.md` ST05 does require.

### Why this is not proven

The E2E was written into `s5-lesson-player`'s progress-persistence test — the one place with real
playback and real Progress writes. The English chain **did** pass on an earlier run: the block
rendered, named the right Lesson, offered "Continue", survived a fresh page, and its click landed on
the correct Lesson. But the Arabic dashboard resume never rendered on `/ar/learn/dashboard`, and
after removing that assertion the test still exceeded 180 s. The cause was not diagnosed.

The added block also left `s5-lesson-player` failing, i.e. a regression against the canonical
baseline, so **it was reverted**. Reverting the file restored it to `HEAD`, which silently undid the
MVP-F01 authority-leak fix in that same spec (all tranche work is uncommitted); that was detected by
re-running the spec (8 failures, the RC-1 signature) and re-applied.

**Baseline after revert: 109 passed / 6 failed / 3 did not run** — the canonical six by identity.
Backend 26 ok / 0 fail. `tsc` clean. Unit 283.

### Evidence level

| Behaviour | Level |
|---|---|
| Resume derivation, ordering, live-revision-only join | `BACKEND_PROVEN` (build, vet, `go test ./...`) |
| Expired/revoked Course yields no pointer | `BACKEND_PROVEN` (evaluator filter) |
| Student scoping | `BACKEND_PROVEN` |
| Dashboard renders the pointer, started vs unstarted copy | `IMPLEMENTED_NOT_PROVEN` |
| Continue → correct Lesson | observed green once, **not** in a retained passing test |
| Arabic dashboard resume | `NOT_PROVEN` — did not render; undiagnosed |
| Pending-access summary | `NOT_IMPLEMENTED` |

**ST-12 stays `BACKEND_MISSING` → now `IMPLEMENTED_NOT_PROVEN`.** **ST-08 stays `PARTIAL`.**
No score change. Denominator 52.

**Open follow-ups for the next session:** diagnose why `/ar/learn/dashboard` renders no resume block;
find why the Dashboard steps make that spec exceed 180 s (likely first-compile cost in `next dev`
across three route loads) and place the E2E in a cheaper spec; add the pending-access summary.

### §17 addendum — 2026-08-20, second attempt: still NOT PROVEN

A dedicated lightweight spec (`s15-dashboard-resume.spec.ts`) was written to move the proof out of
the media-bearing `s5-lesson-player`. It could not be made to pass and **was removed** rather than
left failing. What was learned is recorded here so the next session does not repeat it.

**The Arabic failure was never a dictionary or locale-handling defect.** The dashboard page resolves
locale from the route segment server-side (`app/[locale]/learn/dashboard/page.tsx:12-14`) and picks
the dictionary from it; both `resumeAction` entries exist. The block simply never rendered because
the test never got a Progress row created.

**The real blocker: Progress cannot be created outside the player in this harness.** Every attempt
returned the inventory-safe `404 NOT_FOUND`:

| Attempt | Result |
|---|---|
| `PUT /learn/lessons/:id/progress` with a session cookie | 404 |
| …after loading the Lesson page (h1 asserted, so the read itself succeeded) | 404 |
| …with cookie and CSRF from the *same* session | 404 |
| …preceded by `POST /learn/lessons/:id/playback` | playback itself 404 |
| …with an explicit `Origin` header | 404 |

The server-rendered Lesson page succeeded throughout, so the Student is genuinely entitled — the
refusal is specific to the browser-origin API calls. `s5-lesson-player` performs the same writes
successfully from **page JavaScript** after mounting the player, so something in that path (most
likely state established by the player's own playback-authorisation exchange) is a precondition the
external `page.request` context does not satisfy. **This was not root-caused.**

**Consequence:** a retained Continue-learning proof appears to require the real player, and therefore
the media-bearing spec — which is exactly what exceeded 180 s. Closing F15 needs one of: root-causing
the 404 so progress can be seeded cheaply; a `next build` run so route compilation stops dominating;
or splitting the media spec so the Dashboard journey gets its own budget.

**Repository state:** the failing spec was deleted. Baseline re-verified at
**109 passed / 6 failed / 3 did not run**, canonical six by identity. Backend 26 ok / 0 fail,
`tsc` clean, unit 283 pass. **F15 production code remains in the tree, unproven at the browser
level** — that is the one documented gap, and it is why ST-12 and ST-08 are not promoted.

## 17.1 MVP-F15 — 2026-08-21 — RESOLVED, PROVEN

The resolve-or-revert tranche closed on **resolve**. The blocker was root-caused by controlled
experiment, one genuine production defect was found and fixed, and both features now carry retained
browser proof. **ST-12 and ST-08 are promoted to `E2E_PROVEN`.**

### The 404, root-caused

The refusal was never a product defect and never an entitlement decision. It was a **test-client
defect**, and there were two independent causes stacked behind the same inventory-safe response.

A single diagnostic drove the real player write and then replayed the *identical* body through three
different clients from the same authenticated browser context:

| Client | Body | Result |
|---|---|---:|
| Real Lesson Player (page JavaScript) | production reporter | **204** |
| `page.request.put` (Playwright Node client, shared cookie jar) | exact same body | **404** |
| In-browser `fetch`, `credentials: "same-origin"` | exact same body | **204** |
| `page.request.put` **with the `Cookie` header set explicitly** | exact same body | **204** |
| `page.request.put` with explicit cookie, **wrong `asset_version_id`** | altered | **404** |

**Cause 1 — the session cookie was never sent.** The E2E session cookie is the production
`__Host-gradex_session`, installed with `secure: true`; the run's frontend origin is plain
`http://127.0.0.1:<port>`. Chromium treats loopback as a trustworthy origin and sends `Secure`
cookies over it, so every browser-driven request carried the session. Playwright's Node-side
`APIRequestContext` applies the `Secure` attribute strictly and withholds the cookie over `http://`.
Every synthetic call therefore arrived **unauthenticated**, failed
`requireProtectedLearningAccess` at `authenticator.UserFromRequest`, and was answered by
`writeProtectedUnavailable` — the same 404 that hides content inventory. This is why cookie, CSRF,
`Origin` and playback-ordering permutations all returned 404 identically: none of them changed the
one thing that mattered. It is also why the server-rendered Lesson page kept working — that read is
issued by the Next server, which forwards the browser's own cookie header.

**Cause 2 — `asset_version_id` is a real precondition.** `saveProgress` calls
`TrustedVideoDuration(lessonID, body.AssetVersionID)`, which resolves the exact S2-approved target
and returns `ErrProtectedUnavailable` on any mismatch or non-positive duration. The identifier is
only obtainable from the playback authorization response, which is where the production reporter
gets it (`lesson-player.tsx` passes `playback.asset_version_id` into `useProgressReporter`). A
guessed or absent value yields the same 404 even with a valid session — confirmed by the last row
above.

**No denial semantics were changed.** The inventory-safe 404 is correct and untouched.

### A real F15 production defect, found and fixed

`ListStudentResumeCandidates` selected **`cl.id`** — the revision-scoped `course_lessons` row id —
as the pointer's `lesson_id`. Progress is keyed on the *stable* Lesson identity
(`course_lesson_identities.id`), the Lesson routes resolve the identity, and a row id changes every
time a revision is replaced. The E2E seeder uses distinct id spaces (`30000000-…` identities,
`40000000-…` rows), so the Dashboard was emitting a Continue link the Student could not follow —
it would have resolved to the inventory-safe 404. Fixed to `cli.id`
(`backend/internal/learning/coursehome.go`). The earlier session's one-off observation that the
click "landed on the correct Lesson" cannot have been checking the resulting page.

**A second F15 regression was found the same way:** `TestLearningDashboardScopesOrdersAndRetainsExpiry`
pins the Dashboard's exact JSON keys and had been failing since F15 added `resume`, because the
`-tags=integration` suite was never run for that tranche. The assertion was **tightened**, not
relaxed: it now pins `{courses, resume}`, the resume object's exact five keys, and the pointer's
Course, Lesson, titles and `started`. The empty-Dashboard case still asserts the literal
`{"courses":[]}`, which is what proves `omitempty` still drops the key when nothing is pending.

### Retained proof

**Backend selection — 12 cases, `internal/httpapi` integration, all green.** Each drives the real
`GET /api/v1/learn/dashboard`, exercising the single SQL statement, the authoritative entitlement
evaluator and `resumeReadModel` together; none re-implements the ordering rules
(`learning_resume_selection_integration_test.go`).

| # | Case | Result |
|---:|---|---|
| 1 | No progress → first Lesson, unstarted | PASS |
| 2 | One part-finished Lesson selected, `started: true` | PASS |
| 3 | Several part-finished → most recently watched wins | PASS |
| 4 | Completed Lesson excluded even when it is the newest activity | PASS |
| 5 | Next incomplete Lesson in canonical section/lesson order | PASS |
| 6 | Fully completed Course → **no** pointer | PASS |
| 7 | Two active Courses → part-finished outranks untouched | PASS |
| 8 | More recent learning activity wins, and follows when recency moves | PASS |
| 9 | Expired entitlement excluded | PASS |
| 10 | Revoked entitlement excluded | PASS |
| 11 | Candidate (unapproved) revision Lessons and titles excluded | PASS |
| 12 | Student A never receives Student B's candidate | PASS |

Plus a no-N+1 assertion: the resume pointer costs **exactly one** `learning.resume` query for the
whole Dashboard with two Courses enrolled.

**ST-12 browser proof — `s15-dashboard-resume.spec.ts`, 2/2 green.** Nothing is seeded into
`progress` and no request is synthesised; the row is created by the real player through the real
`PUT …/progress`. English: active access → real Lesson → real playback authorization (200) → real
Progress write → verified in PostgreSQL → leave → `/en/learn/dashboard` → Continue-learning block
naming the right Course and Lesson → action reads "Continue", not "Start" → **click the real
control** → lands on the correct Lesson → the player resumes at the persisted position → pointer
survives a fresh page. No Lesson URL is ever constructed by the test. Arabic: its own Student, real
Arabic copy (`تابع التعلّم` / `تابع`), real Arabic action clicked, lands on `/ar/learn/…`.

**The Arabic failure recorded in §17 was never a locale defect.** With a Progress row present the
Arabic Dashboard renders correctly first time, exactly as §17 suspected. Nor is the media-bearing
spec expensive here: the two journeys run in **6.6 s and 4.8 s**.

**ST-08 pending-access summary.** Counts only, reusing F12's `studentAccessState` rather than
re-reading invitation and entitlement fields — no Course or invitation identifier, no lifecycle
enum, no entitlement vocabulary reaches the Student. Both required states are distinct: *waiting for
you to accept* (Student action required) and *waiting for approval* (waiting for Admin), each with a
route into `/[locale]/access`. Bilingual copy added. Proved **inside the real S6 lifecycle**, in
both genuine windows: action-required before acceptance, waiting-for-Admin after it, absent once
approved — each with the existing no-internal-identifier audit. `s6` is 2/2 green.
Seven focused unit tests cover the derivation, including that an active entitlement outranks its own
invitation state.

### Production-code audit

Live-revision-only join, Student scoped from the session (`ctxUserIDKey`, never a parameter),
authoritative evaluator applied in `resumeReadModel` (`CourseWide` + `ReadActive` only), total
deterministic ordering down to a stable Course id, one bounded query, no new Progress persistence,
no frontend authority, safe null handling, live-revision titles, correct locale behaviour. Two
defects were found and fixed (the `cl.id` pointer, above; and the Dashboard's two server reads were
issued **serially**, which added a round trip to every render — now `Promise.all`, with the access
read resolving to `null` on failure so only the Course list can fail the page).

### Gates

| Gate | Result |
|---|---|
| `go build ./...`, `go vet ./...`, `go vet -tags=integration ./...` | clean |
| `go test ./...` | **26 ok / 0 fail** |
| `go test -tags=integration ./internal/httpapi ./internal/learning` | **ok / ok** |
| `tsc --noEmit` | clean |
| Frontend unit | **290 pass / 0 fail** (+7) |
| `s15-dashboard-resume` isolated | **2 passed** |
| `s6-course-access-grant-launch` isolated | **2 passed** |
| **Full canonical Playwright suite** | **111 passed / 6 failed / 3 did not run** |

The six failures are the canonical six **by identity**: `s5-expired-entitlement:712`,
`s5-playback-performance:157`, `s5-viewport-evidence:223` ×4. 111 = the 109 baseline **+2**, the two
new S15 tests. No regression.

> **Measurement note.** An earlier execution of the same suite reported 93 / 13 / 14 in 20.3 min at
> host load average 42–61, caused by unrelated desktop applications on the workstation plus a stale
> `gradex-s12-worker-1` container that had been crash-looping for 47 h (stopped; restore with
> `docker start gradex-s12-worker-1`). Re-run on a quiet host the same tree produced 111 / 6 / 3 in
> 7.7 min. The canonical figure is the quiet-host run; the loaded run is recorded because a suite
> this timing-sensitive should not be measured under contention.

**ST-12 `IMPLEMENTED_NOT_PROVEN → E2E_PROVEN`. ST-08 `PARTIAL → E2E_PROVEN`.** Denominator stays 52;
`E2E_PROVEN` moves 31 → 33.

## 18. MVP-F16 — Automated manual Course Purchase Request — remediated 2026-08-21

Authorized by founder decision [D-090](../DECISIONS.md#d-090--automated-manual-payment-purchase-requests-close-the-pre-invitation-sales-gap). This is a new canonical MVP capability, not a relabelling of the standard invitation flow.

**Contract and recovery.** `WAITING_PAYMENT → INVITATION_CREATED → ACCESS_GRANTED` is the factual
Purchase Request sequence; `CANCELLED` is terminal. Only a `PUBLISHED` public Course can create a
request. The browser sends only `course_id` and email to `POST /api/v1/purchase-requests`; the
server snapshots the integer-fils KWD price and returns only a human, non-sequential
`GRX-<16 uppercase hexadecimal characters>` reference plus its configured WhatsApp URL. The
database unique constraint remains the collision authority. Neither public response nor refusal
discloses account, ownership, entitlement, or staff-role existence.

`WAITING_PAYMENT` has no Invitation or Entitlement; the public retry safely reuses its same active
request and an authorised Admin may confirm payment or cancel it. `INVITATION_CREATED` has exactly
one pending purchase-backed Invitation and no Entitlement; the matching Student may accept it and
an authorised Admin may cancel it. That cancellation is atomic: the linked Invitation is cancelled,
its action secret is consumed, the Request becomes `CANCELLED`, `cancelled_at` and audit evidence
are written, and no Entitlement is created. The legacy Admin Invitation-cancel command performs the
same linked-request transaction. `ACCESS_GRANTED` has its active Entitlement and is terminal.
`CANCELLED` has no usable Invitation/Entitlement and permits a fresh Request for the same
Course/email. The explicit Admin recovery command, `POST /api/v1/admin/purchase-requests/:id/cancel`,
is capability-gated and idempotent. Thus every reachable nonterminal Request has a valid next
Student or Admin action.

**Anonymous admission.** The public POST is behind the canonical anonymous admission binding and
the versioned `purchase-requests-v1` rate decision. It combines source-address and normalized-email
dimensions with the existing fail-closed limiter; normal idempotent retries retain the same public
shape, while missing admission or throttled callers receive the canonical opaque denial.

`GET /api/v1/admin/purchase-requests` is Admin-only and supports reference/email/Course/state
discovery. `POST /api/v1/admin/purchase-requests/:id/confirm-payment` is the only payment
transition: it locks/revalidates the request and Course, records external payment confirmation,
creates/reuses exactly one linked canonical Course Access Invitation, and appends exactly one
existing encrypted invitation-email outbox event in the same transaction. Retries return the linked
invitation without duplicating it or its event.

**No-double-approval refinement.** Standard invitations remain
`PENDING_STUDENT_ACCEPTANCE → PENDING_ADMIN_APPROVAL → APPROVED`; Student acceptance alone creates
no access. A linked purchase-backed invitation instead requires both prior Admin confirmation and
matching authenticated Student acceptance. Acceptance validates the action secret, intended email,
confirmed request, Course, active-entitlement uniqueness, and linkage; it then atomically creates
or reuses exactly one Enrollment and `PURCHASE_REQUEST` Entitlement, marks Invitation `APPROVED`,
marks the request `ACCESS_GRANTED`, and redirects to the locale-addressable Course Home. The
runtime entitlement evaluator explicitly recognises that new typed grant source.

**Invitation/auth handoff.** The bearer remains in the invitation fragment and is immediately
scrubbed. A tab-scoped invitation context keeps it only while registration, email verification and
login occur; `returnTo` contains the invitation identifier but never the bearer. The access page
derives its initial locale from `/[locale]/access` before provider hydration, so an English invitation
does not silently return through Arabic while unauthenticated. Terminal acceptance/expiry/not-found
handling releases the tab context. No bearer is sent to Course Home, WhatsApp, Admin, audit metadata,
or a newly introduced log field.

**Email/config and migration safety.** `SALES_WHATSAPP_NUMBER` is documented in
`backend/.env.example`, `deploy/env/production-like.env.example`, and
`deploy/hostinger/runtime.env.example`; both documented Compose topologies require it in their API
environment. Development/tests use deterministic `15550000000`, while production requires a
configured 7–15 digit number. `verify-compose-render.sh` checks both renders and a missing-value
rejection. The email uses the existing encrypted outbox/Resend delivery architecture. Purchase-backed
copy says payment was confirmed and acceptance activates access; queued does not mean delivered.

Migration `0021` refuses rollback before any destructive DDL when live
`entitlements.grant_source = 'PURCHASE_REQUEST'` data (or Purchase Request rows) exists. The
canonical `cmd/migrate down` command runs the same preflight before handing control to
`golang-migrate`, preserving a clean migration marker on refusal; the SQL down migration repeats the
guard for direct callers. The supported empty/non-purchase `up → down → up` path remains green.

**Small local remediations.** Confirming a staff recipient maps to the canonical Admin business
conflict (`409`) without exposing a role publicly. The protected Lesson proof now requires a visible
`video`, not merely the absence of an error word. The retained tests cover admission/rate policy,
reference shape, cancellation/retry/atomicity, migration guard, recipient conflict, price snapshot,
wrong identity, and standard-invitation regression.

**Retained remediation evidence observed green.**

- `bash deploy/scripts/verify-compose-render.sh`
- `go build ./... && go vet ./... && go test ./...`
- `go test -p 1 -tags=integration ./...`
- focused real-PostgreSQL Purchase Request, cancellation/recovery, recipient-conflict, migration
  guard (including the actual `cmd/migrate down` path), rate-limit, and configuration tests
- `npm test && npm run typecheck` — **291 passed / 0 failed**
- `npx playwright test e2e/manual-purchase-flow.spec.ts --workers=1` — **3 passed**: the new-Student
  primary journey intercepts `wa.me` after persistence, verifies no raw internal reference/token,
  registers/verifies/logs in/returns, accepts once, reaches Course Home, and requires visible
  protected video; companion tests prove existing Student and Admin cancellation/recovery.
- `npx playwright test e2e/s6-course-access-grant-launch.spec.ts --workers=1` — **2 passed**.

**Final canonical regression closure.** On a quiet one-worker host, `npx playwright test --workers=1
--reporter=line` completed in **7.9 minutes: 114 passed / 6 failed / 3 did not run**. The six failures
match the remediation baseline exactly by identity: `s5-expired-entitlement:712`,
`s5-playback-performance:157`, and `s5-viewport-evidence:223` ×4. All three
`manual-purchase-flow` tests and both retained `s6-course-access-grant-launch` tests passed; there
is no new failure identity. The 114 passes are the protected 111-pass baseline plus the three
retained purchase-flow tests. The textual reporter summary and exact command are retained in the
[ST-19 evidence record](../launch/evidence/2026-08-21-st19-manual-purchase-flow.md).

## 19. MVP-F14 — Revision-scoped Public Preview — completed 2026-08-21

**Canonical contract.** D-019 and BR-143 require a Public Preview to be a separately uploaded
revision-scoped media asset, never a Lesson video, Lesson resource, Lab, or protected learning asset.
The Instructor upload is bound to its editable Course revision before bytes reach storage. It remains
private through `DRAFT`, `PENDING_REVIEW`, and `CHANGES_REQUESTED`; only Admin approval's existing
atomic `courses.live_revision_id` switch can change the anonymous preview. A candidate can replace or
remove its preview without changing live A; approval of B activates B, while approval of a B with no
pointer truthfully removes the CTA and issuance path.

**Safety and delivery.** Migration 0022 adds preview-origin provenance on a `PREVIEW`
logical asset and a `(revision, Course)` relationship. The semantic set command requires owner access,
the same Course/revision, `PREVIEW`/`PUBLIC_PREVIEW`, MP4, READY state, exact passed scanner evidence,
and non-retirement. Submission/approval revalidate the reference. `GET
/api/v1/media/courses/:courseID/preview` resolves the published Course's actual live approved
revision server-side, applies the existing short-TTL private-store signing path and anonymous
source-address rate policy, then returns only `{url, expires_at}`. It never accepts browser-supplied
asset or Lesson IDs; the legacy exact-asset route retains the same strict live-pointer predicate.

**Student behavior.** Course Details exposes `Watch preview` / `شاهد المعاينة` only for the truthful
live projection, opens a native MP4 player, offers a localized retry for issuance failure, and leaves
the ST-19 Purchase Request CTA independent. Preview issuance has no entitlement, enrollment, progress,
resume, or completion write; protected Lesson delivery still requires its existing entitlement check.
The public catalogue's 60-second projection cache can delay CTA visibility after a publication change,
but the no-store issuance query always re-reads the live pointer and cannot authorize stale/candidate
media.

**Measured proof.** Real-Postgres integration covers separate upload/provenance, owner/role denial,
cross-Course/revision substitution, non-READY refusal, scanner gating, public projection, candidate
replacement/removal, approval switching, stale-handle denial, rate policy, and zero learning-state
mutation. The retained real browser journey uploads a PREVIEW before a Lesson exists, waits for the
worker to make it READY, submits and obtains Admin approval, opens it anonymously from the public
catalogue, and verifies the sibling Lesson version is refused. The same journey re-proves IN-09's real
protected Lesson upload and READY lifecycle without conflating the two assets.

**Gates.** `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -tags=integration ./...`
all passed. Frontend typecheck and unit suite passed **296/296**. `s3-public-catalogue` passed **34/34**;
the separate real media-authoring suite passed **1/1**. The quiet-host one-worker canonical run passed
**118 / 6 / 3 in 8.4m**; its six failure identities are unchanged. See the
[MVP-F14 evidence record](../launch/evidence/2026-08-21-mvp-f14-public-preview.md).

**Matrix.** ST-04 is `E2E_PROVEN`. IN-09 is also `E2E_PROVEN`, because this exact retained F14 run
drives the pre-existing protected Lesson upload through READY in addition to the new separate preview.
Both are existing canonical rows; the denominator stays **53**. `E2E_PROVEN` is **36 / 53 = 67.9%**.

## 20. MVP-F17 — T1 Academic Catalog Foundation — completed 2026-08-21

**Authority.** [D-091](../DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy),
against the corrected design report at
[docs/superpowers/specs/2026-08-21-academic-catalog-taxonomy-redesign.md](../superpowers/specs/2026-08-21-academic-catalog-taxonomy-redesign.md).
Retained evidence: [2026-08-21-mvp-f17-academic-catalog-foundation.md](../launch/evidence/2026-08-21-mvp-f17-academic-catalog-foundation.md).

**Seat.** Claude held the **builder** seat by Founder reassignment. The tranche's own audit is a
`BUILDER_SELF_AUDIT` and is **not** independent review. T1 stops here for external review.

**What shipped.** Migration `0023_academic_catalog` (schema version 23) creating `institutions`,
`academic_units` (self-nesting tree with database-enforced cycle and cross-Institution refusal),
`programs`, `curricula` (one `ACTIVE` per Program), `subjects` (Institution-scoped dedupe on
normalized official code, or on each normalized title where no code exists), and
`curriculum_subjects` (many-to-many, with a level bound tied to the owning Institution). A new
`internal/academic` domain package, an Admin-only semantic API behind the new `ACADEMIC_CATALOG`
capability, and the AD13 Admin Academic Catalog surface.

**Boundary — strictly additive.** `taxonomy_terms`, `taxonomy_kind`, `study_year`,
`course_revisions.major_term_id`, and `course_revisions.subject_term_id` are untouched and remain
authoritative for Course classification. No Course read or write path was switched. `courses.subject_id`
was **not** added; it belongs to T4/T5. The Academic Catalog holds no foreign key into `courses`,
`course_revisions`, `entitlements`, or `enrollments`, and no entitlement or access decision reads it.

**Gates observed green.**

```text
backend  go build ./...                              OK
backend  go vet ./...                                OK
backend  go test ./... -count=1                      26 packages ok
backend  go test -tags=integration ./... -count=1    29 packages ok, exit 0
frontend npm run typecheck                           PASS
frontend npm test                                    311 passed / 0 failed
frontend npx playwright test e2e/t1-admin-academic-catalog.spec.ts   4 passed
frontend npx playwright test (full canonical)        122 passed / 6 failed / 3 did not run  (8.1m)
```

The six failures are exactly the known accepted identities (`s5-expired-entitlement:712`,
`s5-playback-performance:157`, `s5-viewport-evidence:223` ×4). The canonical baseline moved from
`118 passed` to `122 passed` solely because T1 added its own four tests. **No new failure identity.**

**Two real defects found by these gates and fixed.** (1) Duplicate-Subject conflicts were reported
without naming the existing Subject, because the lookup ran inside the already-aborted transaction.
(2) The frontend catalog client double-prefixed `/api/v1`, which would have 404ed every call.

**Matrix impact — none.** MVP-F17 is a foundation tranche. It creates no canonical denominator row,
promotes none, and the denominator stays **53**. `E2E_PROVEN` remains **37 / 53 = 69.8%**. ST-03 stays
`BACKEND_MISSING` and is re-pointed at MVP-F22 (T6); it cannot be promoted until MVP-F18–F21 land.

## 21. MVP-F18 — T2 Launch Catalog Data — completed 2026-08-21

**Authority.** [D-091](../DECISIONS.md#d-091--gradex-adopts-an-institution-scoped-academic-catalog-and-retires-the-flat-course-taxonomy) §11.
Retained evidence: [MVP-F18 completion record](../launch/evidence/2026-08-21-mvp-f18-launch-catalog-data.md)
and the [Kuwait University launch catalog scope record](../launch/evidence/2026-08-21-kuwait-university-launch-catalog-scope.md).

**Seat.** Claude held the **builder** seat by Founder reassignment. The tranche audit is a
`BUILDER_SELF_AUDIT` and is **not** independent review. T2 stops here.

**What shipped.** Two capabilities. (1) Reproducible import infrastructure: a version-controlled,
embedded YAML manifest format with schema and domain validation, a `cmd/catalog-import` CLI with
`validate` / `dry-run` / `apply`, an idempotent and atomic importer, and an Admin-only
`POST /admin/academic/institutions/:id/import` endpoint that accepts a known manifest identifier and
never a path or a URL. (2) The Kuwait University launch catalog: 1 Institution, 9 Academic Units,
3 Programs, 3 Curricula, 27 Subjects, 41 CurriculumSubject mappings, all backed by 9 primary
Kuwait University sources retrieved 2026-08-21.

**No schema change.** T2 required no migration; the schema remains at version 23.

**Boundary held.** The legacy taxonomy is still authoritative for Courses. `courses.subject_id` still
does not exist. Course authoring, submission validation, the public catalogue, purchase, access, and
Student registration are all unchanged. No Student academic profile, Instructor Subject picker, or
public filter was added.

**Truthfulness of the data.** All 41 mappings carry `recommended_level = NULL` and
`recommended_semester = NULL`, because Kuwait University derives academic standing from credits
earned and no cited source places a course in a year or a term. 11 of 41 mappings carry no credits,
because the Computer Engineering page publishes only a block total. All 27 Subject Arabic titles are
declared `gradex_translation`; institution, college, and department Arabic names are `official` from
the Student Manual. Five uncertainties, including a genuine conflict between two Kuwait University
pages about the CS curriculum, are recorded rather than resolved by guessing.

**Gates observed green.**

```text
backend  go build ./...                              OK
backend  go vet ./...                                OK
backend  go test ./... -count=1                      27 packages ok
backend  go test -tags=integration ./... -count=1    31 packages ok, exit 0
backend  manifest validation                         19/19
backend  importer integration                        15/15
backend  Admin import API                            11/11
backend  T1 foundation + T2 focused re-run           63 green
frontend npm run typecheck                           PASS
frontend npm test                                    311 passed / 0 failed
frontend npx playwright test e2e/t2-launch-catalog-data.spec.ts   4 passed
frontend npx playwright test (full canonical)        126 passed / 6 failed / 3 did not run  (8.4m)
```

The six failures are exactly the known accepted identities. The canonical baseline moved from
`122 passed` to `126 passed` solely because T2 added its own four tests. **No new failure identity.**

**Matrix impact — none.** MVP-F18 is a foundation-data tranche. It creates no canonical denominator
row, promotes none, and the denominator stays **53**. `E2E_PROVEN` remains **37 / 53 = 69.8%**.
ST-03 stays `BACKEND_MISSING` and is still pointed at MVP-F22 (T6).

### 21.1 T2.1 — Founder Launch Scope Alignment — 2026-08-22

**Data-only pass.** No importer, schema, migration, or frontend production change.
Manifest `kuwait-university-launch-v1` moved from v1.0.0 to v1.1.0.

**Why.** The Founder stated that the launch teaching team covers Mathematics,
Cybersecurity, Data Science, and Software. Re-researching Kuwait University against
those four areas found that the T2 scope record contained a **factual error**: it
asserted Kuwait University confers no degree under those names. Kuwait University
in fact confers a **B.Sc. in Cybersecurity** (Computer Science department, College
of Science, initiated Summer 2024) and a **B.Sc. in Data Science and Artificial
Intelligence** (Information Science department, College of Life Sciences). It also
publishes an **official Suggested Study Plan** for the Computer Science 2024 major
that T2 concluded did not exist.

**Changed.** Added the Cybersecurity Program and its itemized 127-credit plan
(28 mappings); added 17 Subjects across the Software, Cybersecurity, and Data
Science areas; applied authoritative level and semester to 16 Computer Science
mappings; added `version_label_source` so a Gradex placeholder label can never be
shown as the university's own; expanded three abbreviated Subject titles to their
fuller official form. Counts: Programs 3→4, Curricula 3→4, Subjects 27→44,
mappings 41→69, sources 9→16.

**Not changed.** No Program was invented. "Software", "Software Engineering",
"Data Science", "Programming", and "Cybersecurity Engineering" remain absent
because Kuwait University confers no degree under those names — the College of
Engineering and Petroleum's own page enumerates exactly seven B.Sc. degrees and no
Cybersecurity Engineering. B.Sc. Mathematics and B.Sc. Financial Mathematics stay
out: Gradex's Mathematics area teaches Calculus to Computer Science and engineering
Students, and Subject ownership is not Student audience. DSAI stays out pending
transcription of its `.docx` major sheet. Nothing was retired: `absence != delete`.

**Level personalization is `PARTIAL`.** Computer Science has authoritative level
and semester; the other three curricula have none. T3 may collect
`المستوى الدراسي` truthfully, but **T6 can promise level-specific ranking only to
Computer Science Students**; everyone else falls back to curriculum relevance.

**CS source conflict RESOLVED.** The Cybersecurity major sheet independently
confirms `0418-143` (4 credits, compulsory), `0418-310`, and `0418-320`. The CS
course-catalogue page is stale, reflecting the pre-2024 plan. No Subject remains
flagged for review.

**Gates.** `go build`/`go vet` OK; 27 unit packages ok; 31 integration packages ok;
manifest validation and importer/Admin-import suites green; frontend typecheck PASS
and 311 passed / 0 failed; canonical Playwright **126 passed / 6 failed / 3 did not
run** (8.3m) with the same six accepted identities and no new failure identity.

**Matrix impact — none.** `E2E_PROVEN` remains **37 / 53 = 69.8%**; denominator 53.

### 21.2 T2.2 — Data Science & AI launch program — 2026-08-22

**Data-only pass.** No importer, schema, migration, or frontend production change.
Manifest v1.1.0 → **v1.2.0**.

**Founder Decision 1.** B.Sc. Data Science and Artificial Intelligence is included.
It is a real Kuwait University degree conferred by the Department of Information
Science in the College of Life Sciences, and it is the academic home of Gradex's
Data Science teaching area. Its `.docx` major sheets (English and Arabic) and
8-semester plan were extracted and transcribed from Kuwait University's own files.

**Founder Decision 2.** B.Sc. Mathematics and B.Sc. Financial Mathematics remain
out of launch scope as **product scope, not missing academic knowledge**. The
Mathematics department and its canonical Subjects stay, because Gradex teaches
Calculus and Linear Algebra to Students in all five launch Programs.

**Added.** 2 Academic Units (College of Life Sciences, Information Science),
1 Program, 1 Curriculum, 40 Subjects, 43 mappings. Counts: units 9→11,
Programs 4→5, Curricula 4→5, Subjects 44→84, mappings 69→112, sources 16→20.

**Shared-Subject architecture proof.** `0410-101`, `0410-111`, and `0330-100` were
**reused, not duplicated**. `0410-101` now serves all five launch Programs — and
the Computer Science plan places it in Year 1 Semester 1 while the Data Science
plan places the same Subject row in Year 1 Semester 2. One canonical Subject, two
official sequencings.

**Level personalization improves to two of five Programs.** Data Science and AI
carries 33 placed mappings from its official 8-semester plan; Computer Science
carries 16. Cybersecurity, Computer Engineering, and Electrical Engineering still
have none, so T6 ranking falls back to curriculum relevance for those. Overall
classification remains **`PARTIAL`**.

**Launch Program set is final and deliberate:** Computer Science, Cybersecurity,
Data Science and Artificial Intelligence, Computer Engineering, Electrical
Engineering. Tests assert that Software Engineering, Cybersecurity Engineering, a
standalone "Data Science", Programming, Mathematics, and Financial Mathematics are
all absent.

**Update path proved against the live v1.1 catalog without a reset:** dry run
`create=87 update=0 noop=131` writing nothing, apply the same, second apply
`noop=218`. Shared-Subject and existing-Program identifiers verified unchanged;
zero retirements.

**Gates.** `go build`/`go vet` OK; 27 unit packages; 31 integration packages;
manifest 25/25; importer 15/15; frontend typecheck PASS, 311 passed / 0 failed;
T1+T2 browser 9/9; canonical Playwright **127 passed / 6 failed / 3 did not run**
(8.3m) with the same six accepted identities and no new failure identity.

**Matrix impact — none.** `E2E_PROVEN` remains **37 / 53 = 69.8%**; denominator 53.
ST-03 stays `BACKEND_MISSING`, pointed at MVP-F22.

## 22. MVP-F19 — T3 Student Academic Profile — completed 2026-08-22

**Authority.** [D-092](../DECISIONS.md#d-092--the-student-academic-profile-persists-academic-unit-context-for-program-less-states-and-records-onboarding-as-an-explicit-three-state-decision),
amending D-091 §10. Retained evidence:
[MVP-F19 completion record](../launch/evidence/2026-08-22-mvp-f19-student-academic-profile.md).

**Seat.** Claude held the **builder** seat by Founder reassignment. The tranche audit is a
`BUILDER_SELF_AUDIT` and is **not** independent review. T3 stops here.

**What shipped.** Migration `0024_student_academic_profile` (schema version 24); the profile domain
in `internal/academic`; six Student-private `/me` routes for the profile, the skip command, and the
onboarding option projections; a Student onboarding screen, a profile-edit surface, and a dismissible
dashboard invitation card.

**Two design corrections recorded canonically.** Academic-unit context is now persisted for
Program-less states, so an undeclared Kuwait University Student keeps their College instead of being
reduced to "some university". And onboarding is an explicit `NOT_STARTED` / `SKIPPED` / `COMPLETED`
decision rather than a timestamp, so a Student who defers is never nagged again.

**The release gate holds.** `TestAcademicProfileMutationDoesNotAffectEntitlementEvaluation` drives
the real production entitlement evaluator before and after every profile mutation a Student can
perform — including deleting the profile outright — and proves the decision and every access record
are identical. `TestCurriculumIsNeverAnAccessInput` proves a Course outside the Student's curriculum
is still accessible. The profile holds no foreign key into any access table, and none references it.

**Onboarding never gates.** There is no middleware and no route guard. An unprofiled Student
completes access flows, reaches their Courses, and is only ever invited by a card on a page they
already reached.

**Gates.** `go build`/`go vet` OK; 27 unit packages; 32 integration packages; profile domain 21/21;
profile HTTP surface 9/9; migration proofs green; frontend typecheck PASS and 325 passed / 0 failed;
six T3 browser journeys green; canonical Playwright **133 passed / 6 failed / 3 did not run** (8.6m,
1 worker) with the same six accepted identities and no new failure identity.

**Matrix impact — none.** MVP-F19 is an implementation tranche. It creates no canonical denominator
row, promotes none, and the denominator stays **53**. `E2E_PROVEN` remains **37 / 53 = 69.8%**.
ST-03 stays `BACKEND_MISSING`, pointed at MVP-F22.

## 23. MVP-F20 — T4 Instructor Academic Context — E2E_PROVEN

T4 is split into five independently provable sub-tranches. The split exists because T4 as originally
scoped is a multi-week feature program — three tables, a Course-lifecycle discriminator, a new
Instructor flow, a new Admin queue, and seven E2E journeys — and shipping it as one pass would mean
either an unproven claim or a migration written twice.

**Authority:** [D-093](../DECISIONS.md#d-093--course-academic-identity-is-an-explicit-classification-model-and-an-official-subject-code-is-permanently-reserved),
on top of D-091 §7–§9 and §13.

| Slice | Scope | Status |
|---|---|---|
| **T4-A** | Migration 0025; classification discriminator; Course Institution + Subject; Subject lifecycle and post-publication immutability; dual-validation foundation; legacy coexistence guards | **PROVEN 2026-08-22** — [retained evidence](../launch/evidence/2026-08-22-mvp-f20-t4a-course-academic-identity-foundation.md) |
| **T4-A.1** | Migration 0026; Subject official-code identity hardening (D-093 §7 amended) | **PROVEN 2026-08-22** — [retained evidence](../launch/evidence/2026-08-22-mvp-f20-t4a-course-academic-identity-foundation.md) §15 |
| **T4-B** | Instructor Subject-first authoring: academic read projection, Subject search, new create flow, derived-audience display, legacy panel made conditional | **PROVEN 2026-08-22** — [retained evidence](../launch/evidence/2026-08-22-mvp-f20-t4b-instructor-subject-first-authoring.md) |
| **T4-C** | Revision audience override: inference, customization, reset, revision cloning, subset enforcement | **PROVEN 2026-08-22** — [retained evidence](../launch/evidence/2026-08-22-mvp-f20-t4c-revision-audience-override.md) |
| **T4-D** | Subject requests: Instructor flow, Admin queue, link-to-existing, approve-as-new, reject, race safety | **PROVEN 2026-08-22** — [retained evidence](../launch/evidence/2026-08-22-mvp-f20-t4d-missing-subject-requests.md) |
| **T4-E** | Admin review academic context, Course Details presentation, public/search compatibility, full tranche regression | **PROVEN 2026-08-22** — [retained evidence](../launch/evidence/2026-08-22-mvp-f20-t4e-admin-review-public-compatibility.md) |

**T4-A was a foundation slice with no product surface**, and the `LEGACY_TAXONOMY` column default was
a transition device rather than a product choice. **T4-B closed that path**: ordinary Instructor
creation — browser and API — now produces `ACADEMIC_CATALOG` Courses, and omitting the academic
context is refused rather than silently falling back. Existing legacy Courses keep their compatibility
editor until T5 migrates them.

Migration 0025 proved sufficient for both activated workflows. No migration 0027 was added: zero
`course_program_targets` rows remains automatic audience, while `subject_requests` retains pending
uniqueness and resolution history. The completed browser program proves customization, candidate/live
isolation, reset, all three request resolutions, and the no-overwrite resolution race.

**Completion evidence.** The clean pre-work T4-B baseline was 137 passed / 6 accepted failures / 3
did not run in 9.3m. The final canonical one-worker run was 142 passed / the same 6 accepted failures /
3 did not run in 10.2m, with no new identity; both formerly transient S15 tests passed. All five new
T4-C/D/E browser journeys passed, as did a 71-test academic/existing-feature regression and both
media-authoring journeys. Full backend build/vet/unit/integration and frontend typecheck/347-unit
gates were green. See the [completion record](../launch/evidence/2026-08-22-mvp-f20-t4-completion.md).

**Matrix impact — none.** MVP-F20 is an implementation tranche. It creates no canonical denominator
row, promotes none, and the denominator stays **53**. `E2E_PROVEN` remains **37 / 53 = 69.8%**.
ST-03 stays `BACKEND_MISSING`, pointed at MVP-F22.
