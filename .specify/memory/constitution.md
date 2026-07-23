<!--
Sync Impact Report
Initial ratification: unversioned placeholder template → 1.0.0
Modified principles: N/A — initial project constitution
Added sections: eleven Gradex principles and Governance
Removed sections: placeholder template sections
Templates requiring updates:
  ✅ .specify/templates/plan-template.md — generic, Constitution Check gate reads this file at runtime, no edit needed
  ✅ .specify/templates/spec-template.md — generic, no agent-specific or contradicting references found
  ✅ .specify/templates/tasks-template.md — generic, no agent-specific or contradicting references found
  ✅ .agents/skills/speckit-* — no contradicting references found
  ✅ .claude/skills/speckit-* — no contradicting references found
Follow-up TODOs: none
-->

# Gradex Constitution

## Core Principles

### I. Source Documents Are Authoritative

[PROJECT_VISION.md](../../docs/PROJECT_VISION.md) defines product direction.
[PRD.md](../../docs/PRD.md) defines approved scope and acceptance criteria.
[BUSINESS_RULES.md](../../docs/BUSINESS_RULES.md) defines business invariants using
BR-xxx identifiers. [DECISIONS.md](../../docs/DECISIONS.md) defines approved product and
architectural decisions. [GLOSSARY.md](../../docs/GLOSSARY.md) and
[DOMAIN_MODEL.md](../../docs/DOMAIN_MODEL.md) define canonical terminology,
relationships, ownership, and lifecycles. [LAUNCH_GATES.md](../../docs/LAUNCH_GATES.md)
identifies unresolved external, legal, accounting, and commercial items, including the
affected design sign-off or production milestone each blocks. Open gates do not prevent
starting platform system design unless explicitly stated.
[CODING_STANDARDS.md](../../docs/CODING_STANDARDS.md) defines implementation conventions. A feature
specification, plan, or task MUST NOT silently contradict any of these
documents. Where a proposed change conflicts with one of them, the conflict
MUST be surfaced and the source document updated (or the change rejected)
before implementation proceeds — never resolved by silent assumption.

**Rationale**: These source documents collectively define what Gradex is
building and why. Silent drift between what's documented and what's built is
how a solo-developer project accumulates undetectable inconsistency.

### II. Deny by Default, Enforce in the Backend

Access is denied by default; every capability must be explicitly granted.
Authorization MUST be enforced server-side — a UI-only restriction (hidden
button, disabled control) is never sufficient on its own. Instructors may
manage only their own resources (courses, sections, lessons, office-hours
sessions). Students may access only content covered by a valid, active Course
or Section Entitlement. Admin actions touching payments, refunds, payouts,
account suspension, course publishing, or PII MUST be both authorized (role
and scope checked server-side) and auditable (who did what, to what, when).

**Rationale**: This is a paid platform holding student PII and payment data;
a UI-only guard is trivially bypassed, and unaudited admin power over money
and personal data is a compliance and trust liability.

### III. Business-Rule Traceability

Feature specifications, tests, and implementation tasks MUST reference the
relevant BR-xxx rule(s) they implement or depend on. A business-rule change
MUST update BUSINESS_RULES.md (and DECISIONS.md when it reflects a decision)
before the corresponding implementation lands — the document leads the code,
not the other way around.

**Rationale**: BUSINESS_RULES.md is the cited source of truth across the
whole doc set (PRD, DECISIONS, specs). If code changes business behavior
without the BR entry changing first, the traceability the project relies on
breaks silently.

### IV. Payment Correctness

Verified gateway webhooks or reconciled gateway API results—never a
client-side redirect—are authoritative for whether a paid Order succeeded
(BR-020, BR-031, BR-121). Paid payment-attempt processing MUST be idempotent by
the relevant attempt/gateway reference (BR-033). The transaction that grants
Entitlement and commits Coupon redemption MUST be idempotent by stable Order
identifier for both paid and free branches, so retried, duplicate, delayed, or
out-of-order work cannot double-grant access or double-record redemption
(BR-129). A zero-value Coupon Order follows that gateway-independent free-grant
transaction without inventing a gateway reference (BR-126).
Gradex
MUST NEVER collect, transmit, or store raw card/PAN data (BR-030) — checkout
is fully delegated to the PCI-DSS-compliant gateway's hosted page or
tokenized SDK.

**Rationale**: Money and access are the two things this platform cannot get
wrong. Client-redirect-triggered access and non-idempotent payment handling
are the two most common ways commerce platforms leak free access or double
charges.

### V. Testing Commensurate with Risk

Business logic requires unit tests. Database and API behavior requires
integration tests. Critical user journeys (purchase/checkout, enrollment
entitlement, video playback authorization, refund, coupon redemption,
office-hours access) require end-to-end tests. Every acceptance criterion in
a spec or PRD MUST have a stated verification method — the method may be
unit, integration, or end-to-end, but it must be named, not left implicit.

**Rationale**: A solo developer without a test net regresses payment and
entitlement logic invisibly. Scaling test rigor to the risk of the code path
(business rule > CRUD plumbing) keeps this affordable at one-developer pace.

### VI. Modular Monolith, Simplicity by Default

Gradex is built as a modular monolith unless an approved DECISIONS.md entry
explicitly requires a different architecture for a specific concern. Prefer
simple, maintainable solutions sized for a single developer maintaining the
whole system. Do not introduce a new service, library, abstraction layer, or
piece of infrastructure without a demonstrated current requirement — no
building for hypothetical future scale.

**Rationale**: The team is one developer (see PRD §7 Constraints); every
extra moving part is operational burden this project cannot absorb before
launch. D-001's own-build video pipeline is the one already-approved
exception to "keep it simple" — new exceptions need the same bar: an
explicit decision, not an ad hoc choice.

### VII. Data Integrity

Use database constraints and transactions wherever practical to enforce the
structural invariants in BUSINESS_RULES.md §12 (Data Integrity Rules) rather
than relying solely on application-layer checks. Schema changes ship as
versioned migrations — never hand-edited or ad hoc schema drift. Destructive
operations (hard deletes, cascade deletes, data purges) require an explicit,
documented policy and a safeguard (e.g., the BR-018 enrollment-based
archive-vs-delete gate) before they can run.

**Rationale**: Structural invariants (BR-110–115) exist precisely so
corruption is caught by the database, not discovered later in production;
migrations and delete-safeguards are what make that durable over time.

### VIII. Quality Gate

Code MUST pass formatting, linting, type checking, and the test suite before
merge. No secrets, temporary debugging code, placeholder/stub
implementations, or silently ignored errors (empty catch blocks, swallowed
error returns) may be merged.

**Rationale**: These are the minimum, automatable bar that catches an
entire class of production incidents before they ship — non-negotiable
regardless of team size or timeline pressure.

### IX. Operational Discipline

Payment, video-processing, publishing, and administrative operations require
structured logging and meaningful error handling — enough to reconstruct
what happened without reproducing the bug live. External calls (payment
gateway, transcoding, storage, notification delivery) MUST support safe
retries where the operation is idempotent (per Principle IV and BR-033),
rather than failing silently or requiring manual recovery.

**Rationale**: These are exactly the failure-prone external-dependency paths
called out in PRD §9 Risks (gateway dependency, transcode load); good logging
and safe retries are what let a solo developer diagnose and recover from
incidents without a support team behind them.

### X. Responsive, Bilingual, Accessible Web Experience

Gradex MVP is a responsive website, not a native mobile application. Student
learning flows MUST provide the complete approved experience on phones,
tablets/iPads, laptops, and desktops. Instructor and admin portals MUST remain
responsive, while complex authoring, upload, moderation, refund, reporting,
and payout work MAY be optimized for tablet/laptop/desktop layouts. Arabic and
English interfaces, persistent language choice, and correct RTL/LTR behavior
MUST be designed from the start. Platform-owned interfaces and player controls
target WCAG 2.2 Level AA and the performance targets in PRD.md. Because
captions and transcripts are outside MVP, Gradex MUST NOT claim complete
product-level WCAG conformance until media accessibility is included.

**Rationale**: Gulf university students learn across phones, tablets, and
laptops; no device class should lose core Student capability. Retrofitting RTL
or accessible interaction after implementation is substantially riskier than
including both in the initial system and experience design.

### XI. Documentation Stays in Sync

Implementing a feature MUST update every contract, decision record, business
rule, and operational document it affects — a merged feature that changes
behavior documented in DECISIONS.md, BUSINESS_RULES.md, PRD.md, or an API
contract without updating that document is incomplete, not done. When an
implementation reveals a genuine conflict with an existing document, the
conflict MUST be reported and resolved explicitly (updating the document, or
rejecting the change) — never quietly implemented around.

**Rationale**: This is the same discipline already visible in
BUSINESS_RULES.md and DECISIONS.md's own cross-referencing — the doc set only
stays trustworthy as the source of truth (Principle I) if every change keeps
it current.

## Governance

This constitution supersedes all other project practices and conventions
where a conflict exists. The source documents named in Principle I remain
authoritative for product, business, domain, launch-readiness, and technical
content; this document governs how Gradex builds, regardless of which feature
is being built.

**Amendment procedure**: Anyone (currently: the solo developer, or an agent
acting on their behalf) may propose an amendment by editing this file and
running `/speckit-constitution` (or the equivalent constitution-update flow)
to regenerate the Sync Impact Report. A proposed amendment MUST state which
principle(s) it adds, removes, or redefines, and MUST propagate consistency
edits to `.specify/templates/*` and any `speckit-*` command/skill file found
to reference the changed guidance. Because the team is a single developer,
"review" is self-review before merge — the amendment is adopted the moment
it is committed with an updated Sync Impact Report; there is no separate
external approval body at this project stage.

**Versioning policy**: Semantic versioning (MAJOR.MINOR.PATCH) applies to
this constitution itself:

- MAJOR — backward-incompatible governance change: a principle is removed or
  redefined such that previously-compliant work would now violate it.
- MINOR — a new principle or materially expanded guidance is added without
  invalidating prior compliant work.
- PATCH — wording clarifications, typo fixes, or non-semantic refinements
  that don't change what's required.

**Compliance review**: Every feature spec, plan, and task list produced via
the speckit workflow MUST pass the Constitution Check gate in
[.specify/templates/plan-template.md](../templates/plan-template.md) before implementation begins,
and again after Phase 1
design if the design changed scope. A plan that violates a principle must
either be changed to comply or carry a justified entry in that plan's
Complexity Tracking table explaining why the simpler, compliant alternative
was rejected — silent violation is not an option.

**Version**: 1.0.0 | **Ratified**: 2026-07-23 | **Last Amended**: N/A
