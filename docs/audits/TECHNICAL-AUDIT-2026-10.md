# GradeX Technical Audit 2026-10

This document synthesizes findings from the A0-A7 autonomous hardening campaign against `45e66f0550e3d0c6ec436bd2e9357f2de28ee33c`.

## 1. P0 Correctness & Security Launch Blockers

| ID | Description | Invariant | Scale Risk |
|---|---|---|---|
| **A1-001 / A2-001** | Lesson media attachment/publication write-side IDOR | A lesson's video must belong to the correct course and the authorized instructor. | Yes |
| **A1-002 / A2-002** | Upload ownership / resource quota denial | A supplied lesson belongs to the authorized course and quota is isolated. | Yes |
| **A3-001 / A2-008** | Admin suspension lockout race | At least one active Admin must always remain. | No |
| **A4-001** | Connection ownership pool exhaustion | Work holding a DB connection must not require another connection from the same pool. | Yes |
| **A4-003** | Metadata payloads missing admission limit | Metadata requests must have bounded decoding cost before domain validation. | Yes |
| **A5-001** | Student lesson playback silent stall | An authorized lesson must keep playing or show a recoverable state, never hanging. | Yes |
| **A0-RESUMABLE** | No resumable upload capability | Uploads must be durable, resumable on network drops, and not load entire files in-memory. | Yes |

### Resumable Upload Status
- **Current State:** Resumable upload does **not exist**. The browser uses a single XHR PUT loading the entire file into memory (2 GiB limit). Any network disconnect or timeout creates orphaned assets and requires restarting from scratch. There is no cleanup/sweeper for abandoned uploads.
- **Required Design:** Must integrate with S3/R2 multipart uploads using ETag identity. Uploads must maintain a durable server-side session, allow retry of individual chunks, bind strictly to the owning course, and ensure idempotency. Abandoned uploads require a sweeper.

## 2. P1 Robustness (Required before scale/paid usage)

| ID | Description | Subsystem |
|---|---|---|
| **A6-002** | Migration safety risks (table locks, missing `CONCURRENTLY`) | Infrastructure / DB |
| **A6-003** | Observability & Recovery unbounded outbox queries | Worker Dispatch |
| **A5-002** | Session lifetime inconsistencies (frontend + identity) | Auth |
| **A1-003** | Purchase grant provenance lacking stable entitlement relations | Data Integrity |
| **A1-006** | Course deletion checks legacy access instead of current deps | Data Integrity |
| **A1-007** | Academic hierarchy cycle protection write skew | Data Integrity |
| **A7-001** | Test false greens (status-only checks without persistence proof) | Tests |
| Various | Remaining Sev3 items from A2, A3, A4, and A5 reports | Various |

## 3. P2 Scale Preparation

Future verified risks that do not strictly block launch:
- **A1-004 / A1-005:** Application-derived inputs for progress and entitlement integrity (composite constraints recommended).
- **A4-007 to A4-012:** Unbounded projections, course-centric indexes, media queue scheduling, roster aggregations.
- **A6-001:** `IDENTITY_OTP_PEPPER` local tooling bootstrap gap (affects local dev/preview, not production).

## 4. P3 Engineering Quality & Maintainability

- **A2-009, A2-010, A5-012 to A5-014 (Sev4):** Minor UI state issues, existence oracles, public `/readyz` dependency checks.
- **A7 test coverage gaps:** Missing explicit bounds checks for RTL UI, broad catch blocks, conditional assertions masking schema failures.
