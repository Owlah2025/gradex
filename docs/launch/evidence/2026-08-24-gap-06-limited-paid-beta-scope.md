# GAP-06 — Limited Paid Beta Scope Decision

Date: 2026-08-24
Repository: gradex-ui-antigravity
Branch: ui-antigravity-20260817
Authorization: Founder-authorized limited paid beta scope decision.

## 1. Verdict

**PROVEN — GAP-06 limited paid beta scope is frozen.**

This is governance reconciliation only. No application, frontend, backend, migration, deployment,
systemd, provider, database, or security implementation was performed.

## 2. Previous GAP-06 ambiguity

The canonical tracker listed ST-18, IN-11, and AD-14 as unresolved or absent under GAP-06 because
SCREENS.md contained Notification Center, Profile, Office Hours, Analytics, Reported Content,
Entitlement Detail, and Public Preview Manager while later decisions and implementation slices did
not align on current MVP scope.

The principal contradiction was:

- D-013 and PRD describe lightweight external-link Office Hours as MVP;
- later T072/S5 material describes Office Hours as deferred;
- the tracker therefore kept scope as FOUNDER_DECISION_REQUIRED.

This evidence preserves that historical contradiction rather than rewriting it.

## 3. Founder decision

D-094 governs limited paid beta scope as of 2026-08-24.

Deferred until after limited paid beta:

- dedicated Student Notification Center UI/API;
- general Student Profile/Account enhancements beyond the existing academic profile and proven
  account/security flows;
- all Office Hours functionality;
- Instructor Analytics.

Still required before limited paid beta:

- minimal Course-scoped Instructor Roster;
- minimal Admin Reported-Content Resolution surface.

Existing transactional/security notification delivery remains required, including verification,
password reset, invitation, security events, existing outbox events, and worker behavior.

This is a beta-scope deferral, not permanent product cancellation. Deferred features may return after
beta.

## 4. ST-18 disposition

ST-18 is now beta-deferred.

Preserved:

- existing academic profile;
- existing authentication, password, recovery, and account-security flows;
- existing transactional/security email and outbox behavior.

Deferred:

- dedicated in-app Notification Center;
- richer general Profile/Account enhancements;
- Office Hours.

ST-18 is not E2E_PROVEN and is not counted as newly complete.

## 5. IN-11 disposition

IN-11 is now PARTIAL and Founder-scoped.

Required before beta:

- minimal Course-scoped Instructor Roster, preserving D-045's restored roster authority.

Deferred:

- Instructor Analytics;
- Instructor Office Hours.

IN-11 is not E2E_PROVEN. Its next implementation tranche is the minimal roster only; no analytics
or office-hours implementation is authorized by D-094.

## 6. AD-14 disposition

AD-14 remains NOT_IMPLEMENTED and is required before beta.

Student report creation remains intact and proven. The missing capability is the minimal Admin path
to inspect and resolve reports using existing audited Course/Account lifecycle actions where
appropriate.

No moderation implementation was performed.

## 7. Authority changes

Added D-094 to docs/DECISIONS.md.

Updated docs/mvp/FUNCTIONAL_COMPLETION.md:

- ST-18 → DEFERRED;
- IN-11 → PARTIAL with roster-required evidence;
- AD-14 remains NOT_IMPLEMENTED with beta-required evidence;
- GAP-06 → CLOSED FOR LIMITED PAID BETA;
- status vocabulary now explains that beta-only DEFERRED rows retain their canonical denominator.

The 53-row denominator remains unchanged because beta deferral is time-bounded, not removal from
the canonical MVP.

## 8. Tracker impact

Before:

~~~text
45 / 53 = 84.9%
~~~

After:

~~~text
45 / 53 = 84.9%
~~~

Canonical E2E remains 168 passed / 0 failed / 0 skipped. No row was marked E2E_PROVEN.

## 9. Required product work before beta

1. Minimal Instructor Course Roster.
2. Minimal Admin Reported-Content Resolution.

Implementation order is intentional: roster first, moderation second. Neither tranche starts under
this governance decision.

## 10. Deferred product work

- Dedicated Student Notification Center.
- General Profile/Account expansion.
- Office Hours.
- Instructor Analytics.

Entitlement Detail and Public Preview Manager were not among the remaining 8 rows and remain
unchanged.

## 11. Production blockers unchanged

These system rows remain unchanged:

- SY-01 — IMPLEMENTED_NOT_PROVEN; real provider proof pending.
- SY-02 — BLOCKED; Resend domain/credential/sender/inbox proof pending.
- SY-03 — BLOCKED; production Admin bootstrap pending.
- SY-08 — BLOCKED; retained-host backup/monitor timer installation and execution pending.
- SY-09 — BLOCKED; capacity envelope/proof pending.

INF-01 remains IMPLEMENTED, REAL OFFSITE PROOF PENDING.

Payment NULL-expiry remediation, MED-01, MED-04, MED-05, and schema-27 reconciliation remain
PROVEN. MED-02, MED-03, and MED-06 remain deferred.

## 12. Historical authority preservation

No historical D-013, D-045, PRD, S5, T072, or prior tranche evidence was rewritten to pretend D-094
existed earlier.

D-013 and PRD Office Hours wording remains the post-beta product record unless amended later.
D-094 controls only the limited paid beta scope.

## 13. Recommended next product tranches

1. Minimal Instructor Course Roster.
2. Minimal Admin Reported-Content Resolution.

No feature code was written for either tranche.

## 14. Repository safety

- No application source code changed.
- No tests, migrations, Docker, database, systemd, provider, or deployment actions occurred.
- No tracker denominator was changed.
- No accepted security or operations status was changed.
- No historical evidence was rewritten.
- git diff --check passed.
- The dirty-worktree status after reconciliation matches the captured status before reconciliation.

## 15. Final status

**GAP-06 PROVEN — LIMITED PAID BETA PRODUCT SCOPE FROZEN**
