# T8B / MVP-F24B — AD-13 Staff Invitation Browser Lifecycle Evidence Closure

Date: 2026-08-24
Branch: `ui-antigravity-20260817`
Tranche: T8B / MVP-F24B
Register row: **AD-13 — Staff invitation lifecycle + suspension**

---

## 1. Founder authorization

Fresh-session continuation tranche under
[D-089](../../DECISIONS.md#d-089--mvp-functional-completion-work-is-authorized-one-remediation-tranche-at-a-time).
The authorization scoped this session to closing **AD-13** through a real browser journey, forbade
reopening T8A, forbade starting AD-12 or GAP-06, forbade deploying, and forbade general UI polish.
Production code was to change only if browser verification proved a real canonical defect. One did.

## 2. Row identity — tracker AD-13, not a SCREENS identifier

**AD-13** in this document is the row in
[`docs/mvp/FUNCTIONAL_COMPLETION.md`](../../mvp/FUNCTIONAL_COMPLETION.md) §4.3:

> `| AD-13 | Staff invitation lifecycle + suspension | t108_staff_lifecycle_integration_test.go green in go test; s13 green | PARTIAL (backend proven, browser path partly) | MEDIUM |`

It is not the similarly numbered `SCREENS.md` identifier, and the two namespaces were not mixed.
The row names **two** obligations — invitation lifecycle *and* suspension — so suspension and
reinstatement are inside this tranche's authority, not borrowed from AD-12. AD-12's suspend/restore
is a **Course** lifecycle control and stays untouched.

## 3. Pre-T8B assessment — already implemented, unproven in a browser

Classification going in was `ALREADY_IMPLEMENTED_NEEDS_E2E`, and it held for the backend.

| Requirement | Implementation | Existing proof | What was missing |
|---|---|---|---|
| Admin issues a staff invitation | `POST /v1/staff-invitations` → `identity.CreateStaffInvitation` | `TestT108ProductionStaffLifecycle`, `TestInvitationInvariants` | no browser journey |
| Protected secret + encrypted outbox payload | `identity_action_secrets`, `outbox.Writer` protected payload | `TestStaffInvitationEmailReachesTheInviteeAndCompletes` | — (stays at integration layer) |
| Transactional email actually leaves the product | `email.Dispatcher` → renderer → sender | integration test with a fake/SMTP seam | **no run had a worker at all** |
| Invitation completion creates the account | `POST /v1/staff-invitation-completions` | `TestT108…`, contract tests | no browser journey |
| Replay refused | `CompleteStaffInvitation` consumed/expired/revoked/superseded checks | `TestStaffInvitationEmailRefusesWrongAndExpiredCredentials` | **browser UX was wrong — see §11** |
| Suspension / reinstatement | `POST`/`DELETE /v1/accounts/:id/suspension` | `TestT108…`, `TestReinstatementRequiresAReason` | no browser journey |
| No role from the client | completion request has no role field | `staff_contract_test.go` | — |

The gap was exactly the human path, and one product defect was hiding inside it.

## 4. Staff roles the invitation supports

The invitation carries a server-side `invited_role`. The Admin screen offers **Instructor only** —
`createStaffInvitation` sends `role: "INSTRUCTOR"` as a fixed value, with no role control in the
form. The acceptance screen *displays* the assigned role and states it cannot be changed there; the
completion request accepts no role field at all, so the stored invitation is the sole authority.

T8B therefore proves **Instructor** and does not manufacture an Administrator invitation path.

## 5. Test infrastructure added — a real worker in the E2E run

`e2e/global-setup.ts` previously started an API and a Next server and nothing else. An outbox row was
therefore the furthest any browser test could observe. The run now also owns a **real
`cmd/worker` process**, configured with `EMAIL_ENABLED=true`, `EMAIL_PROVIDER=mailpit`, and the
development Mailpit SMTP endpoint, sharing the API's `OUTBOX_PROTECTED_PAYLOAD_KEY` so it can open
the protected payload it is meant to render.

- `EMAIL_PROVIDER=mailpit` is refused by configuration unless `APP_ENV=development`, so this cannot
  select a development sender anywhere else.
- The worker binary path, PID, and ownership-verified termination follow the existing API pattern
  (`WORKER_BINARY_PATH`, `RunState.workerPid`, `cleanupRunResources`). `globalTeardown` stops it
  before dropping the database it polls.
- The per-run disposable database carries its own payload key, so no historical encrypted outbox
  payload was read, re-keyed, or destroyed.

The chain the tests exercise is the production one: **Admin action → API → outbox → worker →
renderer → SMTP → Mailpit**. Nothing constructs an invitation link, and nothing reads a token from
PostgreSQL.

## 6. Admin invitation journey (browser)

`frontend/e2e/t8b-staff-invitation-lifecycle.spec.ts`, Case A:

1. Admin session installed as a real `__Host-` cookie; the page's session rehydrator obtains the
   CSRF token exactly as it would after a login. No mutation security was disabled.
2. `/staff` → heading **Invite Instructor**.
3. `#staff-invite-email` filled with a run-unique recipient — `instructor+t8b-a-<run>@gradex.local`.
   No shared historical Instructor account is reused, and no rotating Student slot is consumed
   (`ROTATING_TEST_SLOTS` is unchanged at 34).
4. **Send Invitation** → the screen confirms *Invitation sent successfully.*
5. The Admin's own authority shows the invitation pending: `GET /v1/staff-invitations` returns the
   human-readable recipient address, `invited_role: INSTRUCTOR`, `state: PENDING`, and the assertion
   verifies the payload carries no bearer/token/secret field.

## 7. Transactional email

Case A waits for the message Mailpit received for that unique address and asserts:

- the recipient is the invited address;
- the subject is the staff-invitation template's — *You are invited to join Gradex staff* — and not
  the Course-access invitation template;
- the body contains a usable `/staff/accept#…` action link.

The link is extracted into process memory and passed between helpers. It is not logged, not written
to a file, not placed in a test title, and not recorded here. The fragment form means the secret is
never sent to the server in a URL path or query.

## 8. Invitation completion (browser)

The recipient opens the delivered link in a **separate browser context**, with no Admin session:

- the screen shows *Complete your staff invitation*, the assigned role **Instructor**, and the
  statement that the role is fixed by the invitation;
- display name and a policy-compliant password (23 characters, inside the 15–128 rule — the policy
  was not weakened) are entered and submitted;
- the screen confirms *Your staff account is ready. Sign in with the invited email address.*

No seed, SQL, or fixture created this account. The lifecycle under test created it.

## 9. Login and server-authoritative capability

Completion issues no session by design, so the new staff member signs in through the ordinary
`/login` form with the email and password they just chose.

- Login succeeds and the browser lands on `/en/instructor/…`.
- `GET /v1/session` on the resulting cookie returns `role: INSTRUCTOR` — the capability is read from
  the server, not from a field echoed by the completion response.
- `GET /v1/staff-invitations` on the same session is refused (4xx). The invited Instructor holds no
  Admin staff authority, proved by the server rather than by a hidden button.

## 10. Admin resulting state

The Admin reloads `/staff` and sees the accepted invitee as an Instructor row identified by the
human-readable **email address**, showing status **Active** — no UUID and no raw enum. The pending
list no longer contains the consumed invitation.

## 11. Replay protection — and the one real defect found

Case B reuses the exact link Case A's flow consumed, in a clean context.

**T8B-REMEDIATION-01 — a consumed invitation still offered the completion form.**

- **Observed:** reopening an already-used invitation link rendered the full account-creation form
  with the assigned role. Only after choosing and submitting a password did the screen say the
  invitation was invalid.
- **Expected:** the canonical invalid/used invitation message, with no form.
- **Authority:** the preview route answers **200** and names the state; the state vocabulary is
  `PENDING | CONSUMED | SUPERSEDED | EXPIRED | REVOKED` (`internal/identity/invitation.go`).
- **Root cause:** `StaffInvitationPreview.state` was typed `"PENDING"` in
  `frontend/src/lib/api/identity.ts`, asserting that reaching the type at all meant the invitation
  was open. `staff-invitation-acceptance.tsx` believed it and treated only a `TOKEN_INVALID` *error*
  as invalid.
- **Smallest fix:** widen the type to the real state union, and treat any non-`PENDING` state as
  invalid — releasing the captured fragment token and rendering the existing invalid message. Two
  files, no new copy, no new route, no security change.
- **Regression test:** Case B itself. The repository has no React component test harness (unit tests
  are `node --test` over compiled `.ts`), so the browser case is the correct and only layer for it.

Server behaviour was already correct and was **not** changed: completion of a consumed invitation was
refused before this fix and is refused after it. The defect was that the product asked a person to do
work it already knew it would reject.

Case B further asserts no second account and no second grant — the address resolves to exactly one
Instructor — and that replay sent no further mail.

## 12. Duplicate invitation policy

Not re-derived and not expanded into a browser negative-case suite. `createInvitation` maps
`ErrAccountAlreadyExists` to a state conflict and supersession is enforced in
`CompleteStaffInvitation`; both are proved by `TestInvitationInvariants` and
`TestT108ProductionStaffLifecycle`. Every T8B recipient address is unique, so no case pollutes
another.

## 13. Staff suspension and reinstatement (in-row)

Case C, from a freshly invited and completed Instructor:

1. Admin locates the staff member on `/staff` **by email address**, enters a suspension reason
   (the control refuses an empty reason), and suspends. The screen confirms *Account suspended.* and
   the row shows **Suspended**.
2. The suspended Instructor's existing session is refused (`GET /v1/session` 4xx) and a fresh login
   attempt does not reach an Instructor surface.
3. Admin reinstates the **same** account with a stated reason. The screen confirms *Account
   reinstated.* and the row returns to **Active**.
4. The same identity signs in again through the ordinary login form and reaches the Instructor
   surface. Suspension ended the earlier session, and that contract was respected rather than
   bypassed.

The account is suspended, never deleted: identity, address, and account history survive both
transitions. Audit rows for suspension and reinstatement stay proved where they already are, in
`TestT108ProductionStaffLifecycle` — no Admin screen exposes audit internals, so a browser assertion
would move that proof to a weaker layer.

## 14. Security

| Property | Layer | Result |
|---|---|---|
| Invitation list/read never returns the completion secret | browser (Case A assertion) + `TestInvitationCreateDoesNotReturnActionBearer` | green |
| Preview requires the bearer in a header, never a query string | `TestPreviewAcceptsBearerHeaderAndRefusesWithoutIt` | green |
| Completion accepts no role field; role comes from the stored invitation | contract test + `completeInvitationRequest` shape | green |
| Recipient cannot self-elevate | the acceptance form has no role control; the API has no role field | green |
| Replay refused | Case B (UX) + `TestStaffInvitationEmailRefusesWrongAndExpiredCredentials` (server) | green |
| Compromised password refused (422) via the test-only HIBP seam | `TestT108ProductionStaffLifecycle` | green, no external HIBP call |
| CSRF / session security | real browser session and CSRF token; nothing disabled | unchanged |

No token value appears in this document, in a test title, in a snapshot, or in any log this tranche
added.

## 15. Production defects found

One: **T8B-REMEDIATION-01** (§11). Frontend only, two files. No backend behaviour changed, no
authorization relaxed, no identity composition refactored, no Staff management redesign.

## 16. Observation recorded, not acted on

The Admin screen has no **pending invitations** list. `GET /v1/staff-invitations` exists and the EN
dictionary already carries `listTitle: "Pending Staff Invitations"`, `noPending`, and `revoke`
strings, but no component renders them, so an Admin cannot see or revoke an outstanding invitation
from the product.

This was **not** remediated. The invitation capability is fully reachable — the Admin invites,
confirms, and later sees the accepted staff member — so §34's condition for UI remediation is not
met, and building a new panel would be UI work the current phase pauses. It is recorded here for
founder decision, not folded into AD-13.

## 17. Tests

**Backend** — `go build ./...`, `go vet ./...`, `go vet -tags=integration ./...` all clean;
`go test ./...` green across all packages.

Staff lifecycle proof, re-run explicitly:

```
--- PASS: TestT108ProductionStaffLifecycle (6.04s)          cmd/api (integration)
--- PASS: TestInvitationInvariants (1.82s)                  internal/identity (integration)
--- PASS: TestInvitationRefusesWithoutOutboxWriter (1.26s)  internal/identity (integration)
--- PASS: TestStaffInvitationEmailReachesTheInviteeAndCompletes (1.24s)
--- PASS: TestStaffInvitationEmailRefusesWrongAndExpiredCredentials (1.15s)
--- PASS: TestInvitationCreateDoesNotReturnActionBearer      internal/httpapi
--- PASS: TestPreviewAcceptsBearerHeaderAndRefusesWithoutIt  internal/httpapi
--- PASS: TestSuspensionRoutesBoundTheirRequestBodies        internal/httpapi
--- PASS: TestReinstatementRequiresAReason                   internal/httpapi
--- PASS: TestReinstatementRefusesAnAbsentBody               internal/httpapi
```

**Frontend unit** — `npm run typecheck` clean; `npm test` **379 passed** (unchanged; the remediation
is proved in the browser, not by a new unit test).

**T8B focused Playwright** — 3 passed in 44.5s:

```
A an Admin invites an Instructor, the invitation arrives by email, and the invitee completes,
  signs in, and appears active
B a consumed invitation link cannot be used a second time
C an Admin suspends an Instructor account, capability is refused, and reinstatement restores it
```

**Canonical Playwright** — see §18.

## 18. Canonical regression

Run once, uncontended, `npx playwright test --workers=1`:

| | Before T8B | After T8B |
|---|---|---|
| passed | 157 | **160** |
| failed | 1 | **1** |
| did not run | 3 | **3** |

157 + 3 = 160; the three added tests are exactly Cases A, B, and C. The single failure is unchanged
and pre-existing:

```
[chromium] › e2e/s5-playback-performance.spec.ts:157:11 › T076 — SC-001 time-to-first-frame ›
  Viewport: phone (390x844) › first rendered frame within 5000 ms at phone
```

It is the production-build precondition — the suite refuses to report SC-001 against `next dev` — and
its remaining viewports are the three that did not run. No new failure identity appeared, and no
previously passing test regressed. Adding a worker to every canonical run changed no other spec's
result.

## 19. Manual acceptance

Not performed as a human walk. The Playwright harness destroys its API, worker, and database in
`globalTeardown`, and standing up a persistent acceptance stack would have required manufacturing
credentials the authorization does not grant. Everything above is **automated** browser evidence
driven through real screens against a real API, real PostgreSQL, a real worker, and real SMTP — it is
not a human acceptance walk and is not claimed as one.

## 20. T5 / T6 / T7 / T8A regression

- **T5** — SWE101 stays `KEEP_UNRESOLVED`; no migration mapping or legacy data was touched.
- **T6** — academic discovery and ranking untouched; its spec is green in the canonical run.
- **T7** — payload contract tests green; no learning payload code changed.
- **T8A** — the four Entitlement lifecycle cases green; no Entitlement code changed.

## 21. Files changed

| File | Change |
|---|---|
| `frontend/src/lib/api/identity.ts` | `StaffInvitationPreview.state` widened to the real state union (**production**) |
| `frontend/src/components/staff/staff-invitation-acceptance.tsx` | a non-`PENDING` preview renders the invalid message instead of the form (**production**) |
| `frontend/e2e/t8b-staff-invitation-lifecycle.spec.ts` | new — Cases A, B, C |
| `frontend/e2e/mailpit.ts` | new — reads the delivered message; keeps the link in memory |
| `frontend/e2e/global-setup.ts` | starts and drains the run-owned worker |
| `frontend/e2e/global-teardown.ts` | stops the run-owned worker |
| `frontend/src/lib/api/e2e-infrastructure.ts` | worker binary path, `workerPid` run state, ownership-verified worker termination |
| `docs/mvp/FUNCTIONAL_COMPLETION.md` | AD-13 promoted; tranche section added |
| `docs/launch/evidence/2026-08-24-t8b-staff-invitation-lifecycle.md` | this document |

## 22. Repository safety

No `git reset`, `clean`, `stash`, `restore`, broad `checkout`, package-wide formatting, or
repository-wide normalization. Protected dirty baseline files — including
`deploy/scripts/environment.sh`, `backend/cmd/api/main.go`, `backend/internal/httpapi/router.go`,
and `backend/internal/httpapi/admission_foundation.go` — were not edited. No
`docker compose down -v`, no volume removal, no retained-database `DROP` or `TRUNCATE`; the s12 stack
and every other running Gradex stack were left alone. Only per-run disposable
`gradex_playwright_e2e_*` databases were created and dropped by the harness's own teardown.
`git diff --check` is clean.

## 23. Matrix impact

**AD-13** is promoted from `PARTIAL` to `E2E_PROVEN`. The denominator stays **53**. No other row
moved: **AD-12**, **GAP-06**, and **GAP-08** are untouched.

| | Before | After |
|---|---|---|
| `E2E_PROVEN` | 42 / 53 = 79.2% | **43 / 53 = 81.1%** |

## 24. Remaining T8 work

**T8C / AD-12 — Course lifecycle evidence closure** (delist / relist / retire / archive / suspend /
restore). Not started, as instructed.
