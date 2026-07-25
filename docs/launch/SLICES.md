# Dependency-Ordered Slice Register

> Derived from: the [M1 architecture baseline](M1_ARCHITECTURE_BASELINE.md)
> Assembled: 2026-07-28 (Day 6, Must 2)
> Calendar authority: [PLAN.md §6](PLAN.md#6-three-week-delivery-calendar)

This register converts the approved M1 baseline into implementable slices. It is the ordering of
record: if a slice needs something a later slice owns, the ordering is wrong and gets fixed here
rather than absorbed as improvisation during implementation.

## 1. Rules

1. **Every MVP capability appears in exactly one owning slice.** §6 proves this against the PRD.
2. **No slice depends on a later slice.** Where the calendar created a forward dependency, §3 records
   the split that removed it.
3. A slice closes on its own acceptance evidence plus a recorded independent-review verdict against
   one exact commit range. Partial completion becomes visible carryover.
4. Recovery days (July 31, August 7) take no new slice. They absorb carryover only.

## 2. Slice order

| ID | Slice | Day | Primary modules | Depends on |
|---|---|---|---|---|
| S0 | Delivery foundation | Jul 28 | — (cross-cutting) | — |
| S1 | Identity, sessions, and RBAC | Jul 29 | Identity and Access, Audit | S0 |
| S2 | Course authoring and review | Jul 30 | Catalog and Authoring, Audit | S1 |
| S3 | Public catalog, search, and shell | Aug 1 | Catalog and Authoring | S2 |
| S4 | Media pipeline, delivery, and Entitlement evaluation | Aug 2 | Media and Assets, Entitlements | S1, S2 |
| S5 | Protected learning | Aug 3 | Learning, Moderation | S3, S4 |
| S6 | Orders, checkout, and coupons | Aug 4 | Commerce | S2, S4 |
| S7 | Payments, entitlement grants, and refunds | Aug 5 | Commerce, Entitlements, Audit | S6 |
| S8 | Instructor and Admin operations | Aug 6 | Reporting and Payouts, Moderation, Audit | S5, S7 |
| S9 | Office hours and notifications | Aug 8 | Office Hours, Notifications | S4, S5 |
| S10 | Revenue, payouts, compliance, and recovery | Aug 9 | Reporting and Payouts | S7, S8 |
| S11 | End-to-end integration | Aug 10 | all | S1–S10 |
| S12 | Production infrastructure and observability | Aug 11 | operational | S11 |
| S13 | Security and quality gate | Aug 12 | all | S12 |
| S14 | Staging acceptance and gate audit | Aug 13 | all | S13 |
| S15 | Blocker-only soft launch | Aug 14 | operational | S14 |
| S16 | Public go/no-go | Aug 15 | — | S15 |

Every `Depends on` entry points backwards. No forward dependency remains.

## 3. Ordering decisions that removed forward dependencies

Three places where the calendar, read literally, would have required a slice to consume something a
later slice owns. Each is resolved by splitting ownership along the module boundary the architecture
already defines, not by moving a calendar day.

### 3.1 Entitlement evaluation precedes Entitlement creation

Media delivery (S4, Aug 2) and protected learning (S5, Aug 3) both gate on Entitlements — but
Entitlements are created from verified payment in S7 (Aug 5). Read naively, S4 and S5 depend on S7.

The architecture already separates these concerns: the Entitlements module owns "grant records,
validity, scope evaluation, expiry, and revocation," while creation is *triggered* by Commerce.
So the module splits across two slices:

- **S4 delivers** the grant record, scope evaluation (a Course grant covers every contained Section;
  a Section grant covers only its Section), expiry, and revocation.
- **S7 adds** the transactional creation of grants from verified payment and free grants.

S4 and S5 are then fully testable on Aug 2–3, and S7 wires the real producer to an already-proven
consumer. This is the single most load-bearing ordering decision in the register: getting it wrong
pushes all access-control verification to Aug 5.

**The S4 test path is not a production capability.** S4 exercises Entitlement evaluation through
isolated integration fixtures or a non-production-only seed mechanism that cannot be enabled in
production. It does not introduce a manual-grant command, an Admin grant screen, or any runtime flag
that could mint an Entitlement outside the provenance rule below. The production invariant is
unchanged by this split:

> Every real Entitlement originates from a completed paid or zero-value Coupon Order Item, except
> reconciliation that restores one from an already valid completed Order Item.

Design §11.2 already forbids any flag from weakening Entitlement provenance. The seed mechanism is
therefore build- or environment-excluded from production images rather than merely disabled by
configuration, in the same way the current `AUTH_FAKE_MODE` seam must never reach production.

### 3.2 Authoring owns media *metadata*; the Media slice owns media *bytes*

Course authoring (S2, Jul 30) covers "resources/labs metadata, preview metadata" while the upload,
quarantine, scanning, and processing pipeline is S4 (Aug 2). S2 therefore creates and validates
references to Asset Versions and must not acquire its own upload or processing path. A Course
revision references an exact `media_asset_version`; S2 owns that reference, S4 owns what it points at.

### 3.3 The legacy video path is migration input, not a second authority

`backend/internal/video` currently enqueues transcoding directly to asynq with no scan step and no
outbox. Domain §7.3 already classifies this as a migration input rather than a production source of
truth. It is cut over inside S4 and is not a dependency of any earlier slice. Confirmed by the
developer at M1 sign-off as correctly assigned and not an M1 blocker.

## 4. S0 — Delivery foundation (July 28)

Today's slice. Typed configuration with fail-closed validation, structured logging, request-ID
propagation, the RFC 9457 Problem Details retrofit across `/api/v1`, health and readiness endpoints,
migration automation, CI, and the documentation guard. Detailed tasks and acceptance evidence live in
[the July 28 record](daily/2026-07-28.md).

Everything after S0 assumes these rails exist. No later slice re-invents configuration loading,
logging, error shape, or CI.

## 5. S1 — Identity, sessions, and RBAC (July 29)

The first product slice, and the one carrying the four M1 obligations.

### 5.1 Bootstrap chain — fixed order

This order is mandatory. July 29 cannot implement Authentication/RBAC without resolving each link,
and no link may be started before the one above it is complete.

1. **Bootstrap schema/state.** An explicit Identity-owned constrained state for
   `PASSWORD_CHANGE_REQUIRED` — *not* an overload of `accounts.status`, which already reads `ACTIVE`
   for this Account. Plus a dedicated bootstrap singleton/completion record with its own uniqueness
   constraint; shared `idempotency_records` must not be used as domain uniqueness.
2. **Controlled bootstrap command.** A one-off release command under its own `cmd/` entrypoint. Not
   an HTTP endpoint, not an ordinary worker task, not a schema migration. Takes the deployment
   principal, production configuration gate, stable operation ID, normalized Admin email, and a
   password injected only through the approved secret manager.
3. **Restricted-session principal/policy.** Deny-by-default typed authorization policies that permit
   the restricted bootstrap principal *only* the required password-change and session-termination
   operations. Derived from the state in step 1; no capability column is added to `sessions`.
4. **Password-change completion.** Normal recent-authentication rules, password-policy validation,
   and Argon2id hashing.
5. **Session rotation and restriction removal.** Atomically clears the requirement, rotates the
   credential generation, and re-establishes an ordinary Admin session.
6. **Normal Admin authorization.** The Account becomes an ordinary Admin. Further Admins come only
   through the approved invitation workflow.

### 5.2 Bootstrap tests — required for S1 to close

| # | Proves |
|---|---|
| 1 | Bootstrap cannot complete twice. A second run — same operation ID, different operation ID, or concurrent — never mints a second Admin. |
| 2 | The plaintext bootstrap password is never persisted, logged, written into a migration file, placed in process arguments, or emitted to telemetry. Only the Argon2id hash reaches the database. |
| 3 | The bootstrap Admin cannot call unrelated APIs before changing its password. Admin, financial, security, retention, provider, and content-management capabilities all deny. |
| 4 | A successful password change atomically clears the requirement *and* rotates/re-establishes the Session. Neither half can land alone. |
| 5 | Failure midway leaves neither a second usable Admin nor a falsely completed bootstrap marker. The transaction rolls back whole. |

Test 3 is the one most likely to pass vacuously — it must assert against real protected endpoints,
not against a policy function in isolation.

### 5.3 Rest of S1

Student registration with mandatory email verification; email/password login; rotating refresh
sessions with family-reuse detection; password reset; logout and revocation; staff invitations with
initial-password setup; immediate suspension enforcement across protected actions; non-enumerating
responses per the §5 privacy boundary; and responsive Arabic/English authentication screens.

## 6. MVP coverage map

Every PRD MVP bullet mapped to its one owning slice. This is the completeness check for Must 2.

### Identity and access

| Capability | Slice |
|---|---|
| Student-only public registration with mandatory email verification | S1 |
| Email/password login, rotating refresh sessions, password reset, logout | S1 |
| Admin invitation and initial-password setup for Instructors and Admins | S1 |
| One out-of-band bootstrap Admin created during secure deployment | S1 |
| Immediate enforcement of account suspension across protected actions | S1 |

### Catalog, learning, and content

| Capability | Slice |
|---|---|
| Public Course catalog and Course detail pages | S3 |
| Major/Subject/Study Year classification, Admin vocabulary, filtering, Arabic-normalized bilingual search | S3 (vocabulary administration: S8) |
| Canonical `Course → Section → Lesson` structure | S2 |
| Adaptive HLS playback, resume position, per-Lesson completion | S5 (pipeline and delivery: S4) |
| Entitlement-protected Lesson resources and downloadable lab materials | S4 |
| Optional separate public Course preview asset | S4 |
| External Discord/Telegram Course community link | S5 (authoring of the link: S2) |
| Student content reporting and Admin resolution | S5 (Admin resolution: S8) |

### Instructor and Admin operations

| Capability | Slice |
|---|---|
| Instructor Course/Section/Lesson builder, submission, revision workflow | S2 |
| Video/resource/lab upload | S4 |
| Instructor Course analytics and Course-scoped Student roster; read-only price visibility | S8 |
| Admin-only Course/Section pricing with audit history | S2 |
| Admin review, publish, delist/relist, retire, archive, emergency access suspension | S2 |
| Reported-content moderation | S8 |
| Admin user provisioning, suspension, coupons, refunds, revenue reporting, payout records | S8 (coupons: S6; refunds: S7; revenue/payouts: S10) |

### Commerce and communication

| Capability | Slice |
|---|---|
| One full Course or one Section per order | S6 |
| Tap-hosted card/KNET checkout; webhook/API confirmation controls successful payment | S6 initiation, S7 confirmation |
| Admin coupons: percentage/fixed, scope, global cap, one consuming redemption, zero-value grants | S6 |
| Full and partial refunds | S7 |
| Course-configured semester expiry disclosed at checkout, snapshotted onto Order and Entitlement | S6 snapshot, S7 grant, S8 audited Admin adjustment |
| Manual monthly Instructor payout with recorded accounting and emailed statement | S10 |
| Course-scoped one-off live office hours with entitlement-protected external meeting link | S9 |
| Fixed transactional in-app/email notifications | S9 |

### Experience and compliance baseline

| Capability | Slice |
|---|---|
| Responsive website across phone, tablet, laptop, desktop | S3 shell; each later slice for its own screens |
| Arabic/English for every role, Arabic default, persistent preference, full RTL/LTR | S3 shell; each later slice for its own screens |
| WCAG 2.2 AA for platform-owned UI and player controls | S13 audit; each slice for its own screens |
| Bilingual Privacy Notice, Terms, Refund Policy, checkout disclosures | S10 |

No MVP bullet is unassigned, and no bullet is owned by two slices — where a capability spans slices,
the split is stated explicitly above and follows a module boundary.

## 7. Gate-dependent slices

These slices carry work that cannot be completed by code alone. Each fails closed rather than
inventing policy.

| Slice | Blocking gate | Behavior while the gate is open |
|---|---|---|
| S4 | `LG-014` malware scanner | Scanner adapter stays provider-neutral; missing configuration leaves Asset Versions quarantined and unpublishable |
| S7 | `LG-010` Tap authenticity scheme | Adapter built against documented vectors; production Tap processing stays disabled until the contract is tested and approved |
| S9 | `LG-018` transactional email | Outbox and templates built; delivery adapter remains configurable |
| S10 | Revenue-share configuration, legal copy | Missing revenue-share configuration fails closed; legal pages cannot ship placeholder text |

An open gate does not stop the slice from being built. It stops the slice from being declared
production-ready.
