<!--
Sync Impact Report — 2026-07-29

Version change: 1.0.0 → 1.1.0 (MINOR)

Modified principles:
  IV. Payment Correctness → IV. Access-Grant Correctness
      Rescoped from "every Entitlement originates from a verified payment" to
      "every authoritative course-access grant, whatever triggered it."
      Preserved unchanged in substance: idempotency by stable identifier;
      prevention of duplicate active access; authorized and audited provenance
      for every grant; the never-store-PAN rule.
      Strengthened rather than relaxed, in four places: duplicate prevention is
      now explicitly a database constraint rather than an application check and
      covers Enrollment as well as Entitlement (BR-167, BR-116); idempotency now names
      *concurrent* work alongside retried, duplicate, delayed, and out-of-order;
      refuse-never-degrade is raised to a principle-level rule; and all grant
      sources must converge on one access model.
      Added: Admin Approval named as the sole MVP grant source (D-045).
      Deferred, not deleted: gateway webhook authority, payment-attempt
      idempotency, and coupon-commit rules are retained verbatim as conditional
      requirements that bind whenever in-platform payments ship.

Consequential edits made for internal consistency, each recorded rather than
absorbed silently:
  II.  Deny by Default — dropped "or Section" from the Entitlement wording
       (Section is not an acquirable scope under D-045) and reordered the
       privileged-action list so course-access granting leads it, with payments,
       refunds, and payouts retained as conditional. No control weakened.
  V.   Testing Commensurate with Risk — critical-journey list rebuilt around the
       access-grant journey; checkout, refund, and coupon redemption retained as
       conditional. Added an explicit requirement that a grant path carries a
       concurrency test, not only a sequential one.
  IX.  Operational Discipline — rationale and external-call list no longer imply
       a payment gateway exists today; retry guarantee unchanged.

Added sections: none
Removed sections: none

Version rationale (against this constitution's own policy):
  MAJOR applies when a principle is "redefined such that previously-compliant
  work would now violate it." That test fails here and was checked against the
  repository rather than assumed: migrations 0001–0010 contain no entitlements,
  enrollments, orders, payments, or coupons table, so no shipped code creates an
  access grant and none can be newly non-compliant. The approved but unbuilt
  payment designs (specs/001-coupons-system, the deferred S7 design) also remain
  compliant, because their rules are retained rather than repealed. The change
  materially expands guidance without invalidating prior compliant work, which
  is the MINOR criterion.

Templates and command files requiring updates:
  ✅ .specify/templates/plan-template.md — Constitution Check reads this file at
     runtime and hardcodes no principle text; verified generic, no edit needed
  ✅ .specify/templates/spec-template.md — no payment or grant references found
  ✅ .specify/templates/tasks-template.md — no payment or grant references found
  ✅ .claude/skills/speckit-* — no references to Principle IV or payment rules
  ✅ .agents/skills/speckit-* — no references to Principle IV or payment rules
  ✅ specs/006-course-access-grant/spec.md — Constitution Alignment Note updated;
     the conflict it raised is the reason for this amendment
  ⚠ docs/superpowers/specs/2026-07-25-platform-architecture-design.md §409 cites
     "Constitution Principle IV" by number. The citation still resolves and its
     surrounding claim is about payment-adapter containment, which this amendment
     preserves as deferred. Left unedited deliberately: that document is approved
     baseline evidence and D-045 already marks its affected sections superseded
     for MVP.

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
sessions). Students may access only content covered by a valid, active
Entitlement. Admin actions touching course-access granting, account suspension,
course publishing, pricing, or PII — and payments, refunds, and payouts whenever
those ship — MUST be both authorized (role and scope checked server-side) and
auditable (who did what, to what, when).

**Rationale**: This is a paid platform holding student PII; a UI-only guard is
trivially bypassed, and unaudited admin power over access and personal data is a
compliance and trust liability. In MVP the money is collected off-platform, which
makes the granting of access — not the taking of payment — the privileged action
most in need of an audit trail.

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

### IV. Access-Grant Correctness

An **authoritative course-access grant** is the Entitlement — and the Enrollment
accompanying it — that every protected operation authorises against. The
following bind every grant, whatever triggered it:

- **Authorized, audited source.** No Entitlement may exist without a recorded,
  typed grant source and immutable audit evidence naming actor, target, and time
  (BR-028, BR-113). A grant with no provenance is a defect, not untidy data.
- **Idempotent by a stable identifier.** The transaction creating a grant MUST be
  idempotent, so retried, duplicate, delayed, out-of-order, or concurrent work
  cannot double-grant (BR-167).
- **No duplicate active access.** At most one active Entitlement per Student and
  Course, and at most one Enrollment per Student and Course, each enforced by
  database constraint rather than an application-layer check (BR-024, BR-167, and
  Principle VII). A grant transaction reuses an existing Enrollment; it never
  creates a second one, which is what keeps Progress single-homed under BR-116's
  `UNIQUE(enrollment_id, lesson_id)` identity.
- **Refuse, never degrade.** A grant path missing its capability, its recent
  authentication, or any required precondition MUST refuse the request. It MUST
  NOT proceed with a default, a fallback, a conditional check, or reduced
  authority.
- **One boundary, many sources.** Every present and future grant source MUST
  converge on this same authoritative record through the typed grant-source
  discriminator. A new source adds a value; it never adds a second access model.

**The MVP grant source is Admin Approval** of an accepted Course Access
Invitation, and it is the only one (D-045, BR-167, BR-028). Registration, email
verification, External Payment, and invitation acceptance each grant nothing on
their own (BR-029). No route, command, screen, fixture, or configuration flag in
a production build may create an Entitlement by any other path.

**Deferred — binding whenever in-platform payments ship.** These rules are not
repealed. They are inactive because the feature is (D-045):

- Verified gateway webhooks or reconciled gateway API results — never a
  client-side redirect — are authoritative for whether a paid Order succeeded
  (BR-020, BR-031, BR-121 as they read when payments return).
- Paid payment-attempt processing MUST be idempotent by the relevant
  attempt/gateway reference (BR-033).
- The transaction granting access and committing Coupon redemption MUST be
  idempotent by stable Order identifier for both paid and free branches (BR-129),
  and a zero-value Coupon Order follows that gateway-independent free-grant path
  without inventing a gateway reference (BR-126).

**Standing and unconditional**: Gradex MUST NEVER collect, transmit, or store raw
card/PAN data (BR-030). In MVP this holds trivially, because no payment is
entered anywhere in Gradex; it binds any future hosted checkout, which must
delegate fully to a PCI-DSS-compliant gateway's hosted page or tokenized SDK.

**Rationale**: Access is the thing this platform cannot get wrong, and money was
only ever one way to obtain it. Scoping this principle to payment left the MVP's
actual grant path — a human clicking approve, with no cryptographic verification
anywhere in it — governed by nothing.

That path deserves *more* assurance than the payment path it replaced, not less.
A verified callback carries its own proof; an Admin approval carries none, so the
capability check, the recent-authentication requirement, the idempotent
transaction, the database uniqueness constraint, and the audit record are the
entire control surface. Deferring payment integration removed a feature, and
removing a feature must never be allowed to quietly remove a guarantee.

Keeping the payment rules present but inactive is deliberate: they were reviewed
and approved once, and a deferred requirement that gets deleted comes back as an
unreviewed one.

### V. Testing Commensurate with Risk

Business logic requires unit tests. Database and API behavior requires
integration tests. Critical user journeys require end-to-end tests: course-access
granting end to end (invitation, identity-bound acceptance, Admin Approval,
Entitlement), video playback authorization, protected downloads, account
suspension enforcement, and office-hours access — plus checkout, refund, and
coupon redemption whenever those ship. Every acceptance criterion in a spec or
PRD MUST have a stated verification method — unit, integration, or end-to-end,
but named, not left implicit.

A grant path additionally requires a **concurrency** test, not only a sequential
one. Idempotency that has never been exercised concurrently is an assumption.

**Rationale**: A solo developer without a test net regresses access-control logic
invisibly. Scaling test rigor to the risk of the code path (business rule > CRUD
plumbing) keeps this affordable at one-developer pace, and the grant transaction
is the highest-risk path in the MVP.

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

Course-access granting, video-processing, publishing, and administrative
operations require structured logging and meaningful error handling — enough to reconstruct
what happened without reproducing the bug live. External calls (transcoding, storage,
notification delivery, and a payment gateway whenever one ships) MUST support
safe retries where the operation is idempotent (per Principle IV), rather than
failing silently or requiring manual recovery.

**Rationale**: These are exactly the failure-prone paths called out in PRD §9
Risks (manual granting as a single human control, off-platform reconciliation,
transcode load); good logging and safe retries are what let a solo developer
diagnose and recover from incidents without a support team behind them.

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

**Version**: 1.1.0 | **Ratified**: 2026-07-23 | **Last Amended**: 2026-07-29
