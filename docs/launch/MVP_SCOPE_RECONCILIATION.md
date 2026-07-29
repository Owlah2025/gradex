# MVP Scope Reconciliation — Payments Move Outside the Platform

> Status: **APPROVED and APPLIED on 2026-07-28**, recorded as
> [D-045](../DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation).
> All twelve questions in §9 were resolved on approval; their answers are recorded in D-045 and are
> summarised in §11 below. This document is retained as the analysis of record — the canonical
> artefacts it names are now the current state, not this file.
> Created: 2026-07-28
> Scope: reconcile the approved artefact set against the locked decision that the MVP launches as a
> fully functional educational video platform with **no in-platform payments**, and with course
> access granted through an Admin-created **Course Access Invitation** that becomes an **Enrolment or
> Entitlement** only after **Admin Approval**.

This document does exactly what was asked and nothing else: it states what conflicts, proposes the
precise change, preserves unaffected approved decisions, and **flags unresolved questions instead of
answering them**. No PRD, business rule, domain model, specification, or task list has been edited.
Implementation is not started.

---

## 1. Impact analysis

### 1.1 The change is cheap in code and expensive in documents

Verified against the repository rather than against the plan:

- Migrations `0001`–`0010` define `accounts`, `sessions`, `session_credentials`,
  `password_credentials`, `bootstrap_operations`, `staff_invitations`, `identity_action_secrets`,
  `identity_security_events`, `policy_acceptances`, `audit_events`, `outbox_events`,
  `outbox_protected_payloads`, `courses`, `sections`, `lessons`, `videos`, `taxonomy_terms`,
  `course_revisions`, `course_sections`, `course_lessons`, `course_section_identities`,
  `course_lesson_identities`, `lesson_files`, `course_price_changes`, `progress`, and
  `fake_entitlements`.
- **There is no `orders`, `payment_attempts`, `entitlements`, `enrollments`, `coupons`,
  `coupon_redemptions`, `refunds`, `financial_ledger_entries`, or `payout_statements` table, and no
  Go package implementing any of them.** The only access-shaped table is `fake_entitlements`, the
  legacy development seam that
  [D-031](../DECISIONS.md#d-031--preserve-authentic-legacy-state-through-forward-only-context-cutovers)
  already forbids from becoming commercial provenance.

**Nothing has to be un-built.** The commerce subsystem exists only as approved prose and frozen
specifications. That is the single most important fact for costing this change.

### 1.2 Three populations of affected work

| Population | Contents | Effect |
|---|---|---|
| **Built and shipped** | S1A–S1C identity/sessions/RBAC; S2 Phases 1–6 authoring, review, revision integrity, Admin pricing | Essentially unaffected. One open question on pricing (§9 U1) |
| **Specified, not implemented** | S3 [`004-public-catalogue`](../../specs/004-public-catalogue/spec.md), S4 [`005-media-and-entitlement-evaluation`](../../specs/005-media-and-entitlement-evaluation/spec.md), coupons [`001-coupons-system`](../../specs/001-coupons-system/spec.md) | S4 needs a bounded amendment; coupons deferred wholesale; S3 needs a price-display decision |
| **Planned, never specified** | S6 (orders/checkout/coupons), S7 (payments/entitlement creation/refunds) | Deleted from the MVP runway and replaced by one smaller slice |

### 1.3 Schedule effect

[AUGUST_15_EXECUTION_PLAN.md §2.1](AUGUST_15_EXECUTION_PLAN.md#21-launch-critical-slices) prices S6 at
12h and S7 at 14h, both Review Tier 3. Both are removed. The replacement — a course-access
invitation and grant slice — is materially smaller because it has no gateway adapter, no callback
verification, no replay/idempotency-against-a-provider surface, no refund state machine, and no
coupon capacity reservation.

**Net effect: roughly 26h of Tier-3 work replaced by roughly 8–10h of Tier-3 work**, against a
calendar whose only float is D14 (August 11). This is the largest schedule relief available in the
current plan and it comes from scope, not from compression.

### 1.4 Two things this change does *not* buy — stated because assuming otherwise is the risk

**(a) It does not proportionally reduce legal exposure.** Moving money off-platform retires the Tap
gates (`LG-008`, `LG-009`, `LG-010`) and the payment-activation gate (`LG-007`). It does **not**
self-evidently retire `LG-005` (Digital Commerce Law registration), `LG-006` (education-sector
licensing), `LG-011` (bilingual policies), or `LG-016` (tax/invoice/accounting treatment). Gradex
still sells course access commercially; where the card is charged is a counsel question, not an
engineering one, and taking payment outside the platform may *increase* the manual record-keeping
burden under `LG-016` rather than remove it. **These gates must not be downgraded on engineering
authority.** See §9 U10.

**(b) It does not reduce review depth.** The MVP replaces "a cryptographically verified gateway
callback grants access" with "an authorised Admin approved a request". That is a **new
critical-authorization surface**, and it is the only thing standing between a registered account and
paid content. It must carry capability gating, recent-authentication, idempotency, immutable audit,
and Tier-3 review at the same depth S7 would have carried. Less scope, same rigour.

### 1.5 The load-bearing architectural property survives

[SLICES.md §3.1](SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation) separates
Entitlement **evaluation** (S4) from Entitlement **creation** (S7) and calls it *"the single most
load-bearing ordering decision in the register"*. That separation is exactly what makes this scope
change safe and cheap: the consumer side is unchanged, and only the producer changes from "verified
payment" to "Admin Approval". **Keep the split.** The new grant slice replaces S7 as the producer and
depends on S4 as the already-proven consumer.

---

## 2. Revised domain workflow

Both flows converge on the same authoritative access record. Only the left-hand flow is built for
MVP.

```text
MVP — manual flow (built)                    FUTURE — online payment (not built)
─────────────────────────                    ──────────────────────────────────
External Payment confirmed off-platform      Student completes hosted checkout
            │                                            │
            ▼                                            ▼
Admin creates Course Access Invitation       Verified capture / provider confirmation
  (student email + one Course)                           │
            │                                            │
            ▼                                            │
Student signs in or registers with                       │
  the invited email                                      │
            │                                            │
            ▼                                            │
Student accepts the Invitation                           │
            │                                            │
            ▼                                            │
PENDING_ADMIN_APPROVAL                                   │
            │                                            │
            ▼                                            │
Admin Approval  ─────────────┐              ┌────────────┘
                             ▼              ▼
                    ┌──────────────────────────────────┐
                    │  idempotent grant transaction    │
                    │  → Enrollment (create or reuse)  │
                    │  → Entitlement ACTIVE            │
                    └──────────────────────────────────┘
                                    │
                                    ▼
                    Student notified; playback, downloads,
                    progress, and Instructor roster all
                    authorise against the Entitlement
```

Three couplings are forbidden by construction and each must have a negative test:

1. **Registration does not grant access.** A verified Active account with no Entitlement reaches no
   protected Course content.
2. **Acceptance does not grant access.** An Invitation in `PENDING_ADMIN_APPROVAL` grants nothing.
3. **Playback never reads Invitation state.** Media delivery, downloads, progress writes, and
   Instructor rosters authorise against the Entitlement only — never against an Invitation, and in
   future never against provider state.

---

## 3. Required entity and state-machine changes

### 3.1 New — Course Access Invitation

A **workflow record, never an access record.**

Bound to: normalized student email (same normalization as Identity), exactly one Course, the creating
Admin, current state, and timestamps for creation, acceptance, decision, and cancellation. Optional
Admin note and external reference per §9 U8.

```text
PENDING_STUDENT_ACCEPTANCE ──accept──→ PENDING_ADMIN_APPROVAL ──approve──→ APPROVED
            │                                    │
            │                                    └──reject──→ REJECTED
            └──cancel──→ CANCELLED               (Admin may also cancel before decision)
```

- `APPROVED`, `REJECTED`, and `CANCELLED` are terminal.
- **No `EXPIRED` state is proposed.** No existing approved business rule requires course-access
  invitation expiry, and the instruction forbids inventing a duration. The acceptance *link* is an
  `identity_action_secrets` row that necessarily expires; that is link expiry with resend, not
  invitation expiry. See §9 U6 — this needs a decision before implementation.
- Acceptance MUST verify that the authenticated Account's normalized email equals the invitation's.
  A different identity cannot accept.
- Proposed uniqueness: at most one non-terminal Invitation per `(normalized_email, course_id)`.

**It is a separate table from `staff_invitations`.** The instruction's §7 constraint and the existing
schema agree: `staff_invitations` carries `invited_role`, `CONSUMED`/`SUPERSEDED` semantics, a
one-pending-per-email global index, and account *creation* on consumption. Course access invitations
create no account, target no role, and must allow multiple concurrent invitations per email for
different Courses. **Do not generalise these into one abstraction.**

### 3.2 Changed — Entitlement

| Today (approved) | Proposed |
|---|---|
| `source_order_id` required, exactly one Order | `grant_source` typed discriminator + nullable source reference |
| Scope: Course **or** Section | Scope column retained; **MVP issues `COURSE` scope only** |
| Origin: paid Order or zero-value Coupon Order | Origin: `MANUAL_INVITATION` (MVP). Reserved and **not implemented**: `PAID_ORDER`, `PROMOTIONAL`, `DIRECT_ADMIN_GRANT` |

- Lifecycle `ACTIVE → EXPIRED`, `ACTIVE → REVOKED` is **unchanged**.
- Proposed constraint: a partial unique index preventing more than one `ACTIVE` Entitlement per
  `(student_account_id, course_id)` — this is the idempotency guarantee, enforced in the database
  rather than in a handler.
- Approval is idempotent: re-approving an already-approved Invitation returns the existing
  Entitlement and creates nothing.
- `grant_source` is a discriminator on an existing column set, **not** a speculative payment-provider
  table. No `payment_providers`, `checkout_sessions`, or `webhook_events` table is proposed.

### 3.3 Unchanged — Enrollment, Progress

`Enrollment` remains the durable Student-to-Course learning relationship used for roster and
progress; `Progress` remains keyed `UNIQUE(enrollment_id, lesson_id)`. Approval creates or reuses the
Enrollment in the same transaction. BR-114 and BR-116 are unaffected.

**Terminology caution:** the instruction says "Enrolment **or** Entitlement: the authoritative
record." Gradex already defines these as two distinct entities with different jobs
([DOMAIN_MODEL.md §1](../DOMAIN_MODEL.md#1-canonical-language)). Collapsing them would be a redesign,
not a reconciliation. See §9 U3.

### 3.4 Removed from MVP — deferred, not deleted

`Order`, `Payment Attempt`, `Refund`, `Coupon`, `Coupon Target`, `Coupon Redemption`,
`Financial Ledger Entry`, `Payout Statement`. Each moves to a deferred register with a recorded
destination. `Catalog Price` / `Price Change` are **already built** and their status is an open
question (§9 U1), not a deletion.

---

## 4. Required API and authorisation changes

### 4.1 New surface

| Route (shape, not final) | Actor | Control |
|---|---|---|
| `POST /api/v1/admin/course-access-invitations` | Admin | New capability, recent-auth, audited |
| `GET /api/v1/admin/course-access-invitations` | Admin | Queue filtered by state |
| `POST …/{id}/approve` | Admin | New capability, recent-auth, audited, **idempotent** |
| `POST …/{id}/reject` | Admin | Reason required, audited |
| `POST …/{id}/cancel` | Admin | Audited |
| `GET /api/v1/me/course-access-invitations` | Student | Own normalized email only |
| `POST /api/v1/me/course-access-invitations/{id}/accept` | Student | Identity-bound; rejects a different account |

**Recommendation (requires approval):** introduce a distinct capability — `COURSE_ACCESS_GRANT` —
rather than folding this into `ADMIN_OPERATIONS`. Granting paid access is the money-equivalent action
in a platform with no money in it, and S1C's round-3 rejection was precisely a capability chosen too
broadly. Recent-authentication window: the shorter Admin financial/security window described in
[SLICES.md §5.4](SLICES.md#54-staff-lifecycle-enforcement-and-authorization-matrix-s1c). Both are
recommendations, not decisions.

### 4.2 Removed surface

Order creation, checkout initiation, coupon preview/apply, payment callback ingress, refund
initiation, and reconciliation endpoints. **None of these exist yet**, so this removes planned
contracts, not shipped routes.

### 4.3 Changed surface

- The authorization matrix test (`backend/internal/httpapi/authorization_test.go`, derived from
  `r.Routes()`) picks up the new routes automatically. That derivation is why this change does not
  require hand-editing a matrix — preserve it.
- Instructor roster (deferred to S18 under
  [§2.3](AUGUST_15_EXECUTION_PLAN.md#23-deferred-to-post-launch--recorded-not-removed)) must derive
  from Enrollment/Entitlement and must **not** expose Admin notes, external payment references, or
  approval evidence. This is an explicit projection requirement, not a default.

---

## 5. Required notification and audit events

### 5.1 Notifications

| Event | Recipient | Channel |
|---|---|---|
| Course Access Invitation created | Invited student email | Email (in-app once an account exists) |
| Invitation accepted | Admin operations | In-app |
| **Access granted** (replaces purchase receipt) | Student | In-app + email |
| Invitation rejected | Student | In-app + email, reason-bearing |
| Invitation cancelled | Student, if previously notified | In-app + email |

Removed: purchase receipt, refund/reconciliation status. Retained unchanged: password/security, staff
invitation, Course approval/changes-requested, Admin Entitlement expiry adjustment, emergency Course
access suspension/restoration, video-processing completion.

BR-120 (best-effort delivery never alters the source transaction) and BR-123 (relational recipient
snapshot) apply unchanged to every new event.

### 5.2 Audit events

New, all immutable with actor, target, reason where applicable, and timestamp:
`COURSE_ACCESS_INVITATION_CREATED`, `_ACCEPTED`, `_APPROVED`, `_REJECTED`, `_CANCELLED`,
`ENTITLEMENT_GRANTED_BY_ADMIN_APPROVAL`.

Account disable/re-enable audit already exists through S1C's staff-lifecycle and suspension events —
§6 of the instruction is **already satisfied by BR-007 and the shipped S1C enforcement**, and needs
terminology alignment only (see §9 U12 note in §6 below).

---

## 6. Affected documents and sections

Legend: **C** = conflicts and must change · **A** = amend for terminology/consistency · **D** = defer
content wholesale · **✓** = verified unaffected.

### 6.1 Canonical product documents

| Document | Section | Conflict | Proposed change |
|---|---|---|---|
| [PRD.md](../PRD.md) | §2 Business Goals | "100–500 **paid** Students"; "revenue through full-Course and single-Section purchases" | **C** — restate as externally-paid access; remove single-Section purchase as an MVP revenue mode |
| PRD.md | §4 MVP → Commerce and communication | Entire block: one Course/Section per order, Tap checkout, coupons, refunds, checkout-disclosed expiry, payouts | **C** — replace with Course Access Invitation, Admin Approval, and Entitlement grant. Move the rest to Deferred |
| PRD.md | §4 MVP → Identity and access | Accurate as written | ✓ — add one line: registration grants no course access |
| PRD.md | §5 Student Features | "Purchase one Course or one Section", coupon at checkout, order/payment/refund history | **C** — replace with accept-invitation and view access status |
| PRD.md | §5 Admin Features | Coupons, refunds, gateway monitoring, payout reconciliation | **C** — replace with invitation creation/approval/rejection; defer the rest |
| PRD.md | §5 Payments, Orders, Coupons, and Refunds | Entire section | **D** — remove from MVP; preserve verbatim in a deferred register |
| PRD.md | §5 Payouts | Entire section | **D** — same |
| PRD.md | §5 Notifications | Purchase receipt, refund status | **A** — per §5.1 above |
| PRD.md | §6 Security | "Gradex never stores full card/PAN data"; webhook verification | **A** — keep the PAN sentence (now trivially true and worth stating); mark webhook verification deferred |
| PRD.md | §9 Risks → Payment Provider and Reconciliation | Framed as an MVP risk | **A** — reframe as a deferred-feature risk; **add** a new risk: manual approval is a single human control on all paid access |
| PRD.md | §11 Acceptance Criteria → Catalog/Checkout/Entitlement, Refunds and Payouts | Whole blocks | **C** — replaced by §8 below |
| [BUSINESS_RULES.md](../BUSINESS_RULES.md) | §3 Enrollment/Purchase — BR-020, BR-021, BR-022, BR-024, BR-025, BR-027, **BR-028** | BR-028 is the direct contradiction: *"Admins … cannot create an Entitlement through a separate manual-grant command"* | **C** — rewrite BR-020/021/028; retire BR-022 (Order lifecycle) to deferred; amend BR-024 to Course-scope-only for MVP; amend BR-025/BR-026/BR-027 per §9 U4/U5 |
| BUSINESS_RULES.md | §4 Payment — BR-030–BR-034 | Gateway rules | **D** — BR-030 retained as a standing security statement; BR-031–034 deferred |
| BUSINESS_RULES.md | §5 Refunds — BR-040–BR-047 | All | **D** |
| BUSINESS_RULES.md | §8 — BR-073, BR-074 | Payout ledger and statements | **D** |
| BUSINESS_RULES.md | §9 matrix — rows for refund, coupon redemption, payouts | Rows reference deferred features | **A** — remove those rows; **add** rows for create/approve/reject invitation and accept invitation |
| BUSINESS_RULES.md | §12 — BR-113 | "entitlement must reference … exactly one purchasable item" | **A** — restate against `grant_source` and Course scope |
| BUSINESS_RULES.md | §13 — BR-121, BR-122 | Purchase confirmation after gateway success | **C** — restate against Admin Approval |
| BUSINESS_RULES.md | §14 Coupons — BR-124–BR-133 | All | **D** |
| BUSINESS_RULES.md | §1 BR-001–BR-009, §2 BR-010–BR-019, §6 BR-050–BR-059, §7 BR-060–BR-068, §10 BR-090/091, §11 BR-100–BR-105, §12 BR-110–BR-112/114–116, §15–§20 | — | ✓ **verified unaffected** |
| [DOMAIN_MODEL.md](../DOMAIN_MODEL.md) | §1 Canonical Language | "A paid or zero-value Coupon Order may create or reuse an Enrollment" | **C** — restate; add Course Access Invitation and grant-source language |
| DOMAIN_MODEL.md | §4 Catalog Price and Commerce | Order, Payment Attempt, Refund entities and lifecycles | **D** — remove Order/Attempt/Refund; **retain** Catalog Price pending §9 U1 |
| DOMAIN_MODEL.md | §4 Entitlement | `source Order` required; Section scope | **C** — per §3.2 |
| DOMAIN_MODEL.md | §5 Coupons, §8 Earnings and Payouts | All | **D** |
| DOMAIN_MODEL.md | §2 Identity | — | **A** — add Course Access Invitation as a distinct entity beside Invitation |
| DOMAIN_MODEL.md | §9 Relationship Summary, §10 Cross-Cutting Invariants | Order-centric lines; "gateway success is authoritative" | **C** — redraw; replace the gateway invariant with "Admin Approval is the authoritative grant trigger" |
| DOMAIN_MODEL.md | §3 Course and Learning Content, §6 Office Hours/Notifications, §7 Moderation and Audit | — | ✓ |
| [GLOSSARY.md](../GLOSSARY.md) | Commerce block (Order, Payment Attempt, Refund, Coupon, Zero-Value Grant, Net Collected Revenue, Earning, Payout…) | Terms leave MVP | **A** — mark deferred; **add** Course Access Invitation, External Payment, Admin Approval, Grant Source |
| [PROJECT_VISION.md](../PROJECT_VISION.md) | §9, §11 metrics, §18, and the purchase-flow lines | Revenue and purchase framing | **A** — MVP monetises through externally confirmed payment; vision unchanged |

### 6.2 Design and journey documents

| Document | Conflict | Proposed change |
|---|---|---|
| [SCREENS.md](../SCREENS.md) | **ST03 Checkout**, **ST04 Payment Confirmation and Receipt**, **ST10 Orders and Refunds** | **C** — remove from MVP. **Add:** invitation acceptance screen, pending-approval state, access-granted state, and an Admin invitation console. ST02 loses its Checkout action and gains a "how to get access" route |
| [USER_JOURNEYS.md](../USER_JOURNEYS.md) | **SJ-05 Apply a Coupon**, **SJ-06 checkout**, **SJ-11 Request and Track a Refund**; the header flow line | **C** — replace the flow with Discover → Evaluate → Register/Sign in → Receive invitation → Accept → Await approval → Access. SJ-12 (expiry/repurchase) needs §9 U4 first |
| [NAVIGATION_MAP.md](../NAVIGATION_MAP.md) | Checkout / Tap / Orders & Refunds / Coupons / Refunds / Payouts routes and the cross-role event map | **C** — remove those routes; add invitation routes on both Student and Admin trees |
| [docs/design/landing-page/LANDING_SPEC.md](../design/landing-page/LANDING_SPEC.md) | Purchase-oriented copy; STATUS already records a stale 150-day access promise | **A** — reconcile copy in the same pass |
| [2026-07-26-domain-data-state-design.md](../superpowers/specs/2026-07-26-domain-data-state-design.md) | Commerce, coupon capacity, refund, ledger, and payout sections | **A** — mark the affected sections superseded-for-MVP; **do not rewrite the approved document**, per the instruction to avoid broad redesign |
| [2026-07-27-api-security-integration-design.md](../superpowers/specs/2026-07-27-api-security-integration-design.md) | §8 provider ingress and reconciliation | **A** — mark deferred; the malware-scanner adapter in §7.1 is unaffected |
| [2026-07-22-coupons-system-design.md](../superpowers/specs/2026-07-22-coupons-system-design.md) | Whole document | **D** — status header only; content preserved for the future |

### 6.3 Launch-control documents

| Document | Conflict | Proposed change |
|---|---|---|
| [SLICES.md](SLICES.md) §2, §6 | S6 and S7 rows; the MVP coverage map's Commerce block | **C** — replace S6/S7 with one grant slice; remap coverage. **Keep §3.1's evaluation/creation split verbatim** |
| [AUGUST_15_EXECUTION_PLAN.md](AUGUST_15_EXECUTION_PLAN.md) §2.1, §2.3, §2.4, §3, §4 | S6/S7 rows; D12/D13/D14 assignments; the payment gates table; go/no-go criteria 3 and 8 | **C** — see §7 and §8 |
| [LAUNCH_GATES.md](../LAUNCH_GATES.md) | `LG-002`, `LG-007`, `LG-008`, `LG-009`, `LG-010`, `LG-012`, `LG-016`, `LG-017`, `LG-001` | **C** — proposed reclassification below, **subject to §9 U10** |
| [STATUS.md](STATUS.md) | Blockers, next task, milestone M3 "product/revenue journey" | **A** — reconcile after approval |
| [DECISIONS.md](../DECISIONS.md) | D-002, D-012, D-015, D-017, D-018, D-026, D-027, D-028, D-030 | **C** — record this change as a **new decision entry, the next free ID after D-044**, and mark each above superseded, amended, or deferred with its exact scope |

Proposed launch-gate reclassification — **engineering may not apply the legal rows unilaterally**:

| Gate | Proposed | Basis |
|---|---|---|
| `LG-007`, `LG-008`, `LG-009`, `LG-010` | `DEFERRED` (fast-follow) | No gateway integration ships. Purely a payments-integration dependency |
| `LG-002` refund eligibility | `DEFERRED` for platform mechanics; **counsel input still required** for the published Refund Policy under `LG-011` | Refunds happen outside Gradex but consumers still have rights |
| `LG-001` revenue share, `LG-017` disputes | `DEFERRED` | No in-platform earnings calculation or dispute handling |
| `LG-016` tax/invoice/accounting | **STAYS OPEN** | Off-platform collection changes *where* records live, not whether they are required |
| `LG-005`, `LG-006`, `LG-011` | **STAY OPEN, UNCHANGED** | Counsel decision. Selling course access commercially is unchanged by where payment is captured |
| `LG-012` launch prices | **Depends on §9 U1** | Only meaningful if prices remain displayed |
| `LG-003`, `LG-004`, `LG-013`–`LG-015`, `LG-018`–`LG-021` | Unchanged | Unrelated to payment location |

### 6.4 Specifications and contracts

| Artefact | Conflict | Proposed change |
|---|---|---|
| [`specs/001-coupons-system/`](../../specs/001-coupons-system/spec.md) (+ `data-model.md`, `plan.md`, `research.md`, `quickstart.md`, 3 contracts) | Entire feature | **D** — status header `DEFERRED — post-MVP`; content untouched |
| [`specs/005-media-and-entitlement-evaluation/spec.md`](../../specs/005-media-and-entitlement-evaluation/spec.md) | **FR-017** forbids *any* path that creates an Entitlement; **FR-018** and **SC-002** require the seed mechanism to be excluded from production builds; the Key Entities table names S7 as creator; the Dependencies table calls S7 "deliberately absent" | **C, narrow and important** — FR-017 becomes "S4 creates no Entitlement; creation belongs to the grant slice"; the provenance invariant is restated as *every Entitlement originates from a recorded grant source*; FR-018/SC-002 are **retained unchanged** — the test seed must still be absent from production builds, and the Admin approval path is not that seed. **FR-011–FR-016 and FR-019–FR-025 survive verbatim** |
| [`specs/004-public-catalogue/spec.md`](../../specs/004-public-catalogue/spec.md) | Scope table says "S6 buys"; FR-009/FR-010 render prices; US1 rationale calls the catalogue "the entry point of the entire commercial journey" | **A** — repoint to the grant slice; price display pending §9 U1 |
| [`specs/003-course-authoring/tasks.md`](../../specs/003-course-authoring/tasks.md) T039–T042 (pricing, complete) | Admin pricing shipped, no checkout consumes it | **No rework.** Status is a policy question (§9 U1), not a defect |
| `specs/003-course-authoring/tasks.md` T044 | Deletion safeguard keyed to "≥1 enrollment" | ✓ — Enrollment survives; T044 is unaffected |
| `specs/003-course-authoring/tasks.md` T043, T045–T064 | Lifecycle, emergency suspension, taxonomy, evidence, convergence | ✓ — **unaffected. The active S2 queue does not stop.** T062 (BR cross-reference update) should run *after* this reconciliation is approved |
| [`specs/002-auth-rbac/`](../../specs/002-auth-rbac/spec.md) and `s1b2/`, `s1c/` | — | ✓ — staff invitations, sessions, suspension all stand. S1C's suspension enforcement already satisfies instruction §6 |

---

## 7. Implementation task changes

### 7.1 Remove from the MVP runway

- **S6 — Orders, checkout, and coupons** (12h, Tier 3) in its entirety.
- **S7 — Payments, entitlement grants, and refunds** (14h, Tier 3) in its entirety.
- Calendar slots D12 (Aug 8) and D13 (Aug 10) are freed; D14's Tier-3 S7 review is freed.

### 7.2 Defer with a recorded destination

Checkout, shopping cart, coupons, automated refunds, instructor payout processing, invoice
generation, BNPL, payment-provider webhooks, payment-gateway integration, automated reconciliation,
chapter purchases, bundle purchases, partial-course purchases. Each is **deferred, not rejected**,
and each keeps its design boundary: the `grant_source` discriminator, the evaluation/creation split,
and the entitlement scope column are the extension points. **No placeholder provider table is
added.**

### 7.3 Amend

| Task | Amendment |
|---|---|
| S4 FR-017 / Key Entities / Dependencies | Repoint from S7 to the grant slice (§6.4) |
| S4 FR-018, SC-002 | **Unchanged** — keep the production-build exclusion test |
| S3 scope table, FR-009/FR-010 | Repoint; price display pending §9 U1 |
| S8-reduced "manual refund initiation", "order lookup" | Remove; replace with invitation queue administration |
| S9 "purchase confirmation" email | Rename to access-granted |
| S10-reduced "checkout disclosures" | Becomes access-terms disclosure; Refund Policy still required under `LG-011` |
| S11 end-to-end journey | Critical journey becomes register → invitation → accept → approve → play |

### 7.4 Add — one new slice

**S6′ — Course Access Invitation and Entitlement Grant.** Category A, Review **Tier 3**, estimated
8–10h, depends on S2 and S4, and it is the producer S4 was built to wait for.

1. Migration: `course_access_invitations` table and state constraint; `entitlements` and
   `enrollments` tables with `grant_source`; partial unique index on one `ACTIVE` Entitlement per
   `(student, course)`.
2. New capability, recent-auth binding, and deny-by-default policy entries.
3. Admin create / list / approve / reject / cancel.
4. Student list / accept, with identity binding enforced server-side.
5. Idempotent grant transaction creating or reusing Enrollment and creating one Entitlement.
6. Audit and outbox intents for all six events.
7. Bilingual Arabic/English screens on the S3 shell for both actors.
8. Negative tests, each of which must fail under deliberate mutation:
   registration grants nothing · acceptance grants nothing · a different identity cannot accept ·
   double approval creates one Entitlement · a disabled account with an active Entitlement is denied
   · playback reads Entitlement and never Invitation state.

### 7.5 Order of operations

Approve this reconciliation → record the new decision entry → update PRD, BUSINESS_RULES,
DOMAIN_MODEL, GLOSSARY →
update SLICES, execution plan, LAUNCH_GATES → amend S4 and S3 specs → specify S6′ through SpecKit.
**The active S2 queue (T043–T064) continues in parallel and is not blocked by any of this.**

---

## 8. Updated MVP acceptance criteria

Replacing PRD §11's Catalog/Checkout/Entitlement and Refunds/Payouts blocks. All other §11 criteria
stand unchanged.

**Registration and access separation**

- Given a Student registers and verifies their email, when they request any protected Course content,
  then access is denied, because registration creates no Entitlement.
- Given a Student holds an Account but no Invitation for a Course, then that Course's protected
  content is unreachable and the denial is identical to a non-existent asset.

**Invitation creation**

- Given an Admin confirms External Payment out of band, when they create a Course Access Invitation
  for one email and one Course, then the Invitation records email, Course, creating Admin, state, and
  timestamps, and an audit event is written. No Entitlement exists yet.
- Given an email already has an Account, an Invitation is still required and still must be accepted.
- Given a non-terminal Invitation already exists for `(email, Course)`, a second creation is refused.

**Acceptance**

- Given the invited Student signs in with the invited email, when they accept, then the Invitation
  moves to `PENDING_ADMIN_APPROVAL` **and no access is granted**.
- Given a different Account attempts to accept, then acceptance is refused server-side regardless of
  how the link was obtained.

**Admin Approval and grant**

- Given an Invitation in `PENDING_ADMIN_APPROVAL`, when an authorised Admin with a valid recent
  authentication approves it, then in one transaction an Enrollment is created or reused, exactly one
  `ACTIVE` Entitlement scoped to that Course is created, audit evidence is written, and the Student
  is notified.
- Given the same approval is submitted twice, concurrently or sequentially, then exactly one
  Entitlement exists.
- Given an Admin rejects, then a reason is required, the Student is notified, and no Entitlement is
  created.
- Given approval is attempted without the required capability or recent authentication, it is refused
  — it does not proceed with less.

**Authoritative access**

- Given an active Entitlement, then playback, protected downloads, progress writes, and the
  Instructor roster all authorise against it and none reads Invitation state.
- Given the Account is disabled, then every protected action is denied immediately even though the
  Entitlement remains `ACTIVE`, and the Entitlement is not mutated.
- Given emergency Course access suspension, then access is denied without mutating any Entitlement.

**Instructor visibility**

- Given an Instructor opens the roster for an owned Course, then only Students with a qualifying
  Enrollment for that Course appear, showing display name and Course-scoped learning fields only, and
  no Admin note, external payment reference, or approval evidence is exposed.
- Given a Course the Instructor does not own, the roster is refused.

**No payment surface**

- Given a production build, then no route, command, screen, or configuration flag performs checkout,
  accepts a payment callback, issues a refund, or applies a coupon.
- Given a production build, then no Entitlement can be created except through recorded Admin Approval
  — proven by asserting the S4 test seed is absent from the build, not merely disabled.

---

## 9. Genuinely unresolved decisions

Flagged rather than invented. Each blocks a specific artefact.

| # | Question | Blocks | Note |
|---|---|---|---|
| **U1** | **Do Course and Section prices remain displayed in MVP?** Admin pricing is built and shipped (`course_price_changes`, S2 T039–T042) and S3 FR-009/FR-010 render prices. With no in-platform checkout, a displayed price is a marketing statement that the student pays elsewhere. | S3 spec, `LG-012`, PRD §4/§5, DOMAIN_MODEL §4 | Three options: keep and display; keep but hide; retire. Recommend **keep and display** — the price is how the student knows what to pay externally — but this is a product call |
| **U2** | **Does Section survive as a priced concept?** The decision bans partial-course access. It does not say whether Section prices disappear from the catalogue. | GLOSSARY, DOMAIN_MODEL §3, BR-021/BR-024, S3 | Section as a *structural* entity is unaffected either way |
| **U3** | **Enrolment or Entitlement — which is authoritative?** Gradex already defines both distinctly. | DOMAIN_MODEL §1, BR-113/114, all authorisation code | Recommend: **Entitlement** authoritative for access, **Enrollment** durable for roster/progress. Needs explicit confirmation because the instruction names them as one thing |
| **U4** | **Does a manually granted Entitlement carry an expiry?** [D-026](../DECISIONS.md#d-026--course-configured-semester-expiry-with-audited-entitlement-adjustments) ties `access_ends_at` to a Course default disclosed *at checkout*. There is no checkout. | BR-025/026, DOMAIN_MODEL §4, S6′ migration | Sub-question: must a Course have a future `default_access_ends_at` before an Invitation can be **approved**, mirroring the old pre-checkout precondition? Recommend yes, but it is a policy call |
| **U5** | **What sets `retirement_eligibility_at`?** It was copied from Order `accepted_at`. Candidates: invitation creation, acceptance, or approval. | BR-027, DOMAIN_MODEL §4 | Recommend **approval**, as the moment access actually begins |
| **U6** | **Does a Course Access Invitation expire?** No existing approved rule covers it, and inventing a duration is forbidden. | Invitation state machine, S6′ | Link expiry (action secret) and invitation expiry are different things. If the answer is "no expiry", the acceptance link needs a resend path |
| **U7** | **Can an Admin reject an Invitation the Student has already accepted, and can a new Invitation be created for a rejected `(email, Course)` pair?** | State machine, uniqueness constraint | Recommend: yes to both, with audit |
| **U8** | **What External Payment evidence, if any, is stored?** The instruction permits an optional note or external reference "only if this fits the existing audit model", and forbids becoming an accounting system. | Invitation schema, `LG-003` retention, `LG-016` | Recommend: free-text note plus an opaque external reference on the audit record only — **no amount, no currency, no payment status field.** Confirm |
| **U9** | **Does the Instructor roster ship in MVP?** Instruction §10 requires instructor visibility into enrolled students; [§2.3](AUGUST_15_EXECUTION_PLAN.md#23-deferred-to-post-launch--recorded-not-removed) currently defers the roster to S18. | Execution plan, S8 scope, PRD §5 | These two now conflict directly. A decision is required |
| **U10** | **Do `LG-005`, `LG-006`, `LG-011`, and `LG-016` change because payment moved off-platform?** | LAUNCH_GATES, go/no-go criterion 8 | **Counsel and accounting only.** Engineering must not downgrade these. Under [D-041](../DECISIONS.md#d-041--legal-and-accounting-outreach-deferred-to-the-final-days-the-resulting-exposure-is-accepted-rather-than-resolved) neither adviser is engaged, so the question currently has no owner |
| **U11** | **How are Instructors compensated with no in-platform revenue record?** [D-018](../DECISIONS.md#d-018--manual-monthly-payouts-with-system-recorded-accounting)/[D-030](../DECISIONS.md#d-030--earnings-snapshot-instructor-ownership-and-share-configuration-at-order-completion) compute earnings from paid Orders, which will not exist. `LG-020`'s Instructor agreement still needs revenue-share terms. | D-018/D-030 status, `LG-001`, `LG-020` | Payout *processing* is deferred by the instruction; the *contractual* obligation is not |
| **U12** | **Is "disable" a new state or the existing `SUSPENDED`?** | BR-007, Account lifecycle | Recommend reusing the shipped `SUSPENDED ↔ ACTIVE` enforcement rather than adding a state. Instruction §6 appears **already satisfied** by S1C |

---

## 10. Scope boundaries held during application

- No payment-provider abstraction, table, or adapter was added. The `grant_source` discriminator is
  the only extension point.
- The approved July 25/26/27 design documents were **not** rewritten; affected sections are marked.
- The active S2 queue (T043–T064) was not touched beyond two clarifying notes and remains unblocked.
- No launch gate was **resolved**. Seven moved to `DEFERRED`; the legal and accounting rows
  (`LG-005`, `LG-006`, `LG-011`, `LG-016`) stayed `OPEN` and unchanged.
- No application code was written. This change is documentation and specification only.

## 11. Resolved decisions and what was applied

All twelve §9 questions were resolved on approval. The authoritative record is
[D-045](../DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation);
this is the index.

| # | Resolution |
|---|---|
| U1 | Catalog prices remain displayed as External Payment guidance; `LG-012` stays required |
| U2 | Section prices retained in schema and Admin surface, **not displayed** in the student catalogue. Recorded as the one derived call — the reconciliation offered no recommendation |
| U3 | Entitlement is authoritative for access; Enrollment stays the durable learning relationship. Not merged |
| U4 | Entitlement carries an expiry; a Course needs a future `default_access_ends_at` before an invitation can be **approved** |
| U5 | `retirement_eligibility_at` is set from the Admin Approval instant |
| U6 | Invitation has **no expiry state**; the acceptance link expires and is reissued |
| U7 | An accepted invitation may be rejected; a new invitation may follow for the same pair. Both audited |
| U8 | Optional Admin note plus opaque external reference, on the audit record only. No amount, currency, or payment status anywhere |
| U9 | Instructor roster **returns to MVP**, overturning its post-launch deferral |
| U10 | `LG-005`, `LG-006`, `LG-011`, `LG-016` stay `OPEN`. Counsel and accounting only |
| U11 | Payout processing deferred; `LG-020`'s revenue-share terms still required |
| U12 | Account disabling reuses the shipped `ACTIVE ↔ SUSPENDED` enforcement |

### Artefacts updated on 2026-07-28

`DECISIONS.md` (D-045 added; D-002, D-012, D-015, D-017, D-018, D-026, D-027, D-028, D-030 marked) ·
`BUSINESS_RULES.md` (§3 rewritten, §21 added, §4/§5/§14/BR-073/074 deferred, matrix and BR-082/113/121/122 amended) ·
`DOMAIN_MODEL.md` · `PRD.md` · `GLOSSARY.md` · `LAUNCH_GATES.md` · `PROJECT_VISION.md` ·
`SCREENS.md` · `USER_JOURNEYS.md` · `NAVIGATION_MAP.md` · `NAVIGATION_RULES.md` ·
`launch/SLICES.md` · `launch/AUGUST_15_EXECUTION_PLAN.md` · `launch/STATUS.md` ·
`specs/001-coupons-system/**` (deferred banner) · `specs/005-media-and-entitlement-evaluation/spec.md` ·
`specs/004-public-catalogue/spec.md` · `specs/003-course-authoring/tasks.md` ·
`specs/002-auth-rbac/spec.md`.

### Still outstanding

S6′ has **no SpecKit specification yet**. Writing it is the next planning action, and no
implementation may begin before it is frozen and the slice's seats are assigned.
