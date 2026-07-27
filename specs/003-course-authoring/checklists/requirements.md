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

## Notes

### Two named exceptions to "no implementation details"

Both are deliberate and both are constraints on *where a decision is made*, not on how it is coded:

1. **FR-041 names `identity.Authorize`.** The alternative — "authorization must be enforced
   server-side" — is exactly the wording that let S1C ship a second, hand-maintained decision point
   alongside the real one. Naming the single gate is the requirement.
2. **FR-042 requires ownership coverage to be derived from the live route table.** This is a
   testability requirement rather than a design choice: it is the difference between an unenforced
   route failing a test and an unenforced route depending on a reviewer noticing it.

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
