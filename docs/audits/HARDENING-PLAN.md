# GradeX Hardening Plan

This plan operationalizes the findings from the October 2026 Technical Audit, prioritizing absolute correctness, safety, and operational resilience.

## Phase 1: P0 Launch Blockers

These tasks **must** be completed and gates passing before final go/no-go. 

### 1.1 Resumable Upload Architecture (A0 / Deliverable)
- **Objective:** Support robust, resumable uploads for large video assets.
- **Requirements:** 
  - Switch from single-PUT memory buffering to an S3/R2 multipart upload workflow.
  - Expose durable server-side session identity and part tracking.
  - Bind upload intent transactionally to the owning course (no cross-course misdirection).
  - Implement idempotent completion logic.
  - Add background cleanup/sweeper for abandoned incomplete uploads.

### 1.2 Data Integrity & Security IDORs (A1-001, A1-002, A2-001, A2-002)
- **Objective:** Fix lesson media attachment and upload quota ownership.
- **Requirements:**
  - `SetLessonVideo` and `ValidateAssetVersion` must strictly validate that the asset kind, ownership, and target course perfectly match.
  - Implement database relationship guards (e.g. composite FKs) for course-to-asset bindings while preserving historical media support.
  - Quota accounting must strictly evaluate against the proven course binding.

### 1.3 State Machine Safety: Admin Lock-out (A3-001, A2-008)
- **Objective:** Prevent concurrent suspension requests from eliminating all Admins.
- **Requirements:**
  - Serialize Admin status mutations via an explicit shared DB lock.
  - Re-evaluate the "at least one active Admin remains" invariant inside the serialized boundary before committing the suspension.

### 1.4 API Robustness: Pool Exhaustion (A4-001)
- **Objective:** Prevent `GetUser360` (and the media dispatcher) from deadlocking the pgxpool.
- **Requirements:**
  - Pass transaction-bound or pool-independent query interfaces into dependent reads.
  - Never request a second pool connection while holding an open transaction from the same pool.

### 1.5 Security: Metadata Payload Exhaustion (A4-003)
- **Objective:** Protect the API from out-of-memory crashes due to massive JSON bodies.
- **Requirements:**
  - Apply explicit maximum body size limits using bounded JSON decoding on all authenticated metadata mutation endpoints.

### 1.6 Frontend Player Robustness (A5-001)
- **Objective:** Prevent the Student player from silently freezing after video segment URLs expire.
- **Requirements:**
  - Attach an error handler to `hls.js` that detects 403s.
  - Gracefully recover or trigger a session/token refresh to request a new manifest when a legitimate pause exceeds the 5-minute URL expiry.

## Phase 2: P1 Robustness 

Must be addressed before significant paid scaling or broad external release.
- **Migration & Deploy Safety (A6-002):** Fix dangerous migrations (e.g., `0051_course_completions`, `0053_catalog_search_events`) to use `CONCURRENTLY` and bounded batch inserts, preventing statement timeouts.
- **Worker Dispatch Polling (A6-003):** Fix unbounded history scans in email and media outbox dispatchers to utilize indexed bounds and limit query payload size.
- **Test Integrity (A7):** Remove false-green tests. Require tests that assert 403/404s to include a subsequent state-read proving persistence didn't mutate.
- **Domain Lifecycles:** Fix purchase grant provenance recording (A1-003), dependency checks during course deletion (A1-006), and lock write-skew in academic hierarchies (A1-007).
- **Session Consistencies:** Fix front-end vs identity session lifetime management (A5-002).

## Phase 3: P2 Scale Preparation

Implement progressively as userbase grows:
- **Index Optimization:** Review course-centric index designs and implement pagination on unbounded account projections.
- **Data Model Constraints:** Add composite FKs to structurally enforce `progress` and `course_completions` alignment without relying exclusively on application code.

## Phase 4: P3 Quality & Maintainability

Deferred/Opportunistic improvements:
- Resolving local-dev tooling issues (e.g. `IDENTITY_OTP_PEPPER` missing in bootstrap).
- Restricting `/readyz` dependency checks.
- Asserting strict layout/RTL coordinates in E2E tests instead of loose `not.toBeNull()` checks.
