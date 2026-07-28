# Specification Quality Checklist: S2 — Course Authoring and Review

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-07-28
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Constitution Traceability (Gradex, Principle III)

- [x] Every functional requirement cites the BR-xxx rule(s) it implements
- [x] Deny-by-default backend enforcement is stated as a requirement, not assumed (FR-041, FR-042)
- [x] Every privileged action has a stated audit requirement (FR-043)
- [x] Data-integrity invariants are expressed as refusals, not as intentions (FR-020, FR-025, FR-033)

## D5 Revision-Integrity Freeze

- [x] T001–T031 completion markers cite repository commits and closure evidence
- [x] T032–T038 are the only unchecked D5 implementation tasks
- [x] Candidate creation is atomic, idempotent, based on the captured live revision, and protected by
      a database uniqueness invariant
- [x] Candidate cloning separates version-row IDs from stable Section/Lesson identities and creates
      no externally owned resource
- [x] Every mutation names the candidate explicitly; latest-revision lookup is forbidden as authority
- [x] Live graph assembly captures `live_revision_id` once
- [x] Approval lock order, transaction-bound dependency readers, conflicting dependency locks,
      rollback boundary, audit, and outbox behavior are explicit
- [x] Rejection preserves Course lifecycle, live pointer, live graph, access records, and reason
- [x] The exact four D5 races and all six independent mutations are named
- [x] Conflict, validation, and authorization response classes remain distinct
- [x] Production composition-root verification uses the real router and requires session mutation
      security on every D5 mutation
- [x] Scope stops after T038

## Notes

### Approved exceptions to "no implementation details"

These are deliberate proof-boundary constraints rather than incidental coding choices:

1. **FR-041 names `identity.Authorize`.** The alternative — "authorization must be enforced
   server-side" — is exactly the wording that let S1C ship a second, hand-maintained decision point
   alongside the real one. Naming the single gate is the requirement.
2. **FR-042 requires ownership coverage to be derived from the live route table.** This is a
   testability requirement rather than a design choice: it is the difference between an unenforced
   route failing a test and an unenforced route depending on a reviewer noticing it.
3. **FR-046 and FR-050 name database enforcement and transaction boundaries.** The product owner
   explicitly required a database constraint for candidate uniqueness and one PostgreSQL approval
   transaction; weakening these to generic wording would remove the acceptance boundary.
4. **FR-048 names explicit candidate identity and forbids latest-row lookup.** Repository evidence
   proved the implicit lookup already exists, so naming the forbidden authority is a corrective
   requirement rather than incidental implementation detail.
5. **FR-054 names the production composition root.** Two earlier S2 rounds passed self-contained
   tests while the production surface was absent; the production artifact is therefore part of the
   proof contract.
6. **FR-047 distinguishes stable logical identities from revision-owned version rows.** BR-059 keys
   progress to Lesson identity and BR-019 makes Section a purchasable scope; leaving identity to the
   implementer would permit a source-document violation.

### Deliberate non-clarifications

Three points had multiple readings and were resolved from source documents rather than raised as
questions, because the source is authoritative under Constitution Principle I:

- Delisting versus retirement versus emergency suspension are three distinct controls, resolved from
  BR-090 and BR-027 rather than collapsed.
- "Immediately" for emergency suspension is defined as next-protected-request, matching the S1C
  suspension precedent, and recorded in Assumptions.
- Section-level pricing exists alongside Course-level pricing per BR-019 and is not an open question.

### Carried into planning

- FR-044's standing clause is the process carryover from the S1C closeout and must appear in the
  Antigravity handoff brief, not only here.
- The edge-case list contains four concurrency cases that need explicit designed outcomes in
  `plan.md`; a spec can name them, only the plan can resolve them.
