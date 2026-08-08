# Operational Evidence Model: S12

S12 adds no product-domain table or migration. These entities describe deployment configuration and
evidence stored outside the authoritative application schema.

## Application Release

| Field | Meaning |
|---|---|
| Revision | Exact Git SHA |
| Frontend artifact | Immutable image identifier/digest |
| API artifact | Backend image identifier/digest plus API command |
| Worker artifact | Backend image identifier/digest plus worker command |
| Schema compatibility | Minimum and maximum supported schema |
| Built at | UTC build time recorded by the build/deployment system |

## Deployment Environment

| Field | Meaning |
|---|---|
| Name | Disposable, staging, or production |
| Public origin | Canonical HTTPS origin |
| Service endpoints | Non-secret frontend/API/database/Redis/storage locations |
| Secret references | Platform-managed names, never values |
| Release | Currently selected Application Release |
| Schema version | Observed migrated version |

## Backup Artifact

| Field | Meaning |
|---|---|
| Backup ID | Unique timestamped identifier |
| Source identity | Non-secret database/environment identifier |
| Source schema version | Version observed before backup |
| Created at | UTC time |
| Integrity | File size and cryptographic checksum |
| Storage location | Protected evidence location |

## Restore Drill

| Field | Meaning |
|---|---|
| Backup ID | Backup restored |
| Fresh target identity | Separate newly created database |
| Started/completed at | UTC timestamps |
| Restored schema version | Exact observed version |
| Record assertions | Expected identity, invitation, Enrollment, Entitlement, and representative rows |
| Application assertions | Startup, `/healthz`, `/readyz`, representative reads |
| Outcome | Passed or failed with non-secret reason |

## Rollback Drill

| Field | Meaning |
|---|---|
| Known-good release | Release N |
| Candidate release | Release N+1 |
| Schema before/after | Must remain forward-compatible and preserve version 15 provenance |
| Actions | Exact application deployment selections |
| Probe results | Frontend reachability, `/healthz`, `/readyz` |
| Outcome | Passed or failed with non-secret reason |

## Operational Signal

| Field | Meaning |
|---|---|
| Timestamp | UTC emission time |
| Service/environment | API, worker, deployment monitor, or backup job |
| Severity/event | Stable event vocabulary |
| Correlation | Request, job, asset version, or deployment identifier where applicable |
| Redacted detail | Diagnostic context excluding secrets, credentials, tokens, signed URLs, and unnecessary PII |
| Delivery | Log sink or alert sink result |
