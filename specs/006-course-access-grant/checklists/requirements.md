# Specification Quality Checklist: S6 — Course Access Invitation and Entitlement Grant

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-07-29
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) — with the recorded exceptions below
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — both resolved 2026-07-29
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
- [x] Deny-by-default backend enforcement is stated as a requirement, not assumed (FR-008, FR-014)
- [x] Every privileged action has a stated audit requirement (FR-031)
- [x] Data-integrity invariants are expressed as refusals enforced by the database, not intentions
      (FR-003, FR-016)
- [x] Testing method is implied per success criterion and will be named per task in `tasks.md`
      (Principle V)
- [x] The Principle IV conflict was surfaced and then **resolved** by constitution v1.1.0 before
      planning; the Alignment Note maps each guarantee to the amended principle

## D-045 Scope Boundary

- [x] No payment entity, field, route, or provider is introduced (FR-005, FR-020, SC-012)
- [x] Registration grants nothing (FR-028, SC-001)
- [x] Acceptance grants nothing (FR-010, SC-002)
- [x] Admin Approval is the sole grant trigger (FR-013)
- [x] The Invitation is never read by an authorization decision (FR-026, SC-007)
- [x] Entitlement evaluation is consumed from S4, not reimplemented (FR-027)
- [x] The grant-source discriminator is the only future-payment extension point (FR-019, FR-020)
- [x] Course Access Invitations stay separate from staff invitations (FR-030)
- [x] Invitations have no expiry state; the acceptance link expires instead (FR-007, FR-012, FR-025)

## Notes

### Approved exceptions to "no implementation details"

Deliberate proof-boundary constraints, following the precedent set by the S2 checklist:

1. **FR-003 and FR-016 name database enforcement.** BR-024's one-active-Entitlement rule and BR-165's
   one-non-terminal-invitation rule are the invariants this whole slice protects. Weakening them to
   "the system must prevent duplicates" is exactly the wording that permits a handler check which
   loses under concurrency. Constitution VII requires database enforcement where practical; naming it
   is the requirement.
2. **FR-015 names a single transaction.** Partial completion — an Enrollment without its Entitlement,
   or audit evidence without its grant — is the failure this requirement exists to forbid, and it
   cannot be expressed without naming the transaction boundary.
3. **FR-020 and SC-006 name the production build.** S4's SC-002 holds its test seed to
   build-exclusion rather than configuration; the same standard applies to the producer, or the
   provenance guarantee is only as strong as a flag nobody flipped.
4. **FR-011 names a validated return destination.** This is `CARRYOVER-S1B2-RETURNTO`, closed by S1B3
   Must 5. Naming it prevents the invited-Student-without-an-Account path from rediscovering a solved
   problem badly.

### Deliberate non-clarifications

Resolved from source documents rather than raised, because the source is authoritative under
Constitution Principle I:

- Whether an archived, delisted, or retired Course blocks approval — resolved from BR-018, BR-090, and
  BR-027 rather than asked.
- Whether the invitation notice can reach an address with no Account — resolved from the existing
  outbox delivery-intent mechanism already proven for staff invitations, which carries a destination
  address rather than an Account reference.
- Whether acceptance requires an Active Account — resolved from BR-008, since an unverified Student
  cannot sign in and acceptance requires a session.
- Whether a Student can decline — no source rule defines the state; recorded as an assumption rather
  than invented as policy.

### Questions raised and resolved

Both were genuine policy choices with no derivable default, so neither was resolved by the
specification. The product owner decided both on 2026-07-29:

- **FR-040** — approval when the Student's Account is suspended: **permitted**. The grant is created
  and is simply unusable until reinstatement, consistent with BR-007 never mutating an Entitlement.
- **FR-041** — separation of duties on approval: **not enforced**. Any Admin with the course-access
  capability may approve, including the creator, because a launch operations team may be one person.
  **FR-042 was added** so both actors are recorded separately and self-approval stays reconstructable
  — which also means a future four-eyes rule would be a business-rule change, not a schema change.

Neither answer weakens a control: the capability, recent-authentication, idempotency, and audit
requirements are unchanged.

### Carried into planning

- The five concurrency edge cases need designed outcomes in `plan.md`. A specification can name a
  race; only a plan can resolve one.
- FR-014's refusal clause is the standing carryover from the S1C closeout and must appear in the
  implementation handoff brief, not only here.
- **Done, 2026-07-29**: constitution amended to v1.1.0 (Principle IV → Access-Grant Correctness), so
  the plan's Constitution Check gate now reads a principle matching the current access model.
  Principle V additionally requires a concurrency test on any grant path, which SC-003 satisfies.
- The Course Access Invitation state machine, the entitlement grant-source discriminator, and the two
  uniqueness invariants all need migration design; downstream migration numbering was reserved by S2
  T062 as `0011_catalog_search` and `0012_media_and_entitlement`, so this feature's migration follows
  those.
  **Resolved 2026-08-06: the migration is `0015_course_access_grant`** and `MaxSchemaVersion` becomes
  15. Both reserved numbers exist, `0013_enrollments` and `0014_protected_learning` followed, and the
  sequence `0001`–`0014` has no gap. Of the two uniqueness invariants, **only
  `cai_one_non_terminal_per_pair` is S6's to create** — S4's `0012` already shipped
  `entitlements_one_active_student_course`, and the grant-source discriminator and its `CHECK` with it.

## Reconciliation findings, 2026-08-06

The specification and this checklist stand. Nothing below changes a requirement, a success criterion, or
an approved answer; each is a defect in what the *planning artifacts* asserted about the repository, found
by re-reading the implemented code at the S5 closure head `d5ce557`.

- [x] Every FR and SC re-checked against the implemented code. **No FR or SC was found unimplementable
      except through the gap below**, and none was reworded.
- [ ] **FR-015 and FR-017 have no schema to read.** BR-025 requires a configured future Course
      `default_access_ends_at`; **no migration `0001`–`0014` creates it**, and no closed slice owns it.
      The coverage grep could not see this, because the FR *was* cited — by tasks reading a column
      nothing created. Assigned to S6 as `T003a`/`T007a` under
      [D-073](../../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it),
      **pending product-owner acknowledgement of the effort consequence.** This is the slice's one
      unresolved blocker.
- [x] BR-025's Kuwait-local-date to UTC exclusive-boundary conversion now has a task. It had none, and it
      is a stated rule rather than a new decision.
- [x] The two questions the product owner answered on 2026-07-29 — FR-040 suspended-Student approval and
      FR-041 separation of duties — are unaffected and are **not** reopened.
- [x] No payment entity, field, route, or provider entered the slice during reconciliation. The struck S7
      row was not retargeted onto S6: no order, checkout session, coupon, refund, payment attempt, or
      callback appears in any S6 artifact (BR-020, FR-005, SC-012).
- [x] FR-003 and FR-016's "database, not a handler" exception below is **stronger** than when it was
      written: two of the three invariants were created and independently reviewed by closed slices
      before the code depending on them existed, which is what
      [SLICES.md §3.1](../../../docs/launch/SLICES.md#31-entitlement-evaluation-precedes-entitlement-creation)
      was arranged to produce.
- [x] FR-020 and SC-006's production-build exception has a working precedent rather than a plan:
      `internal/entitlement/production_exclusion_test.go` and the `!production`-tagged `cmd/e2e-seed`.
