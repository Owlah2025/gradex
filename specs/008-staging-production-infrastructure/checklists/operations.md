# Operational Requirements Quality Checklist: S12

**Purpose**: Validate that recovery, security, deployment, and evidence requirements are complete before implementation
**Created**: 2026-08-08

## Requirement Completeness

- [x] CHK001 Are requirements defined for all three application artifacts and their independent lifecycle? [Completeness, Spec §FR-001–FR-004]
- [x] CHK002 Are required production configuration categories and fail-closed cases enumerated? [Completeness, Spec §FR-005]
- [x] CHK003 Are PostgreSQL, Redis, private storage, and migration responsibilities explicit? [Completeness, Spec §FR-006–FR-010]
- [x] CHK004 Are TLS, cookie, CORS, CSRF, origin, and forwarding requirements specified together? [Coverage, Spec §FR-011]
- [x] CHK005 Are logging, monitoring, alerting, and secret-redaction requirements distinct and complete? [Completeness, Spec §FR-012–FR-014]
- [x] CHK006 Are backup creation and isolated restore verification requirements both defined? [Coverage, Spec §FR-015–FR-016]
- [x] CHK007 Are application rollback and database recovery explicitly separated? [Consistency, Spec §FR-017–FR-018]

## Acceptance Criteria Quality

- [x] CHK008 Can artifact build/start failure be objectively evidenced? [Measurability, Spec §SC-001–SC-002]
- [x] CHK009 Can clean migration and dependency readiness be objectively evidenced? [Measurability, Spec §SC-003]
- [x] CHK010 Can Redis recovery be evaluated without treating Redis as authoritative? [Consistency, Spec §SC-004]
- [x] CHK011 Can private media and protected delivery be verified independently? [Measurability, Spec §SC-005]
- [x] CHK012 Does restore acceptance require both data assertions and an application boot? [Completeness, Spec §SC-006–SC-007]
- [x] CHK013 Does rollback acceptance prohibit schema downgrade and require probe recovery? [Clarity, Spec §SC-008]
- [x] CHK014 Are disposable alert capability and external delivered-alert evidence clearly distinguished? [Clarity, Spec §SC-011]

## Dependencies and Conflicts

- [x] CHK015 Is S12 explicitly upstream of deployed S11 acceptance testing? [Consistency, Spec §Assumptions]
- [x] CHK016 Are missing provider credentials scoped only to the exact external action they prevent? [Clarity, Spec §Dependencies]
- [x] CHK017 Is migration 0015 provenance preservation stated without reopening S6 behavior? [Scope, Spec §FR-018]
- [x] CHK018 Are commerce and speculative-infrastructure exclusions explicit? [Scope, Spec §Scope Boundaries]
