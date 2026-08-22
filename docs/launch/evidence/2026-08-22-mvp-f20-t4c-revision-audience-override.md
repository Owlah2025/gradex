# MVP-F20 / T4-C — Revision Audience Override

**Date:** 2026-08-22  
**Status:** `PROVEN`

## Model and lifecycle

`course_program_targets` from migration 0025 is sufficient. Zero rows means automatic inference
from the Course Subject's active Curriculum mappings; an unmapped Subject truthfully produces an
empty effective audience and remains publishable. A customized audience is a nonempty explicit
subset of that inferred set. The server refuses duplicate, unrelated, inactive, and
cross-Institution Programs.

Targets belong only to `CourseRevision`. Candidate creation clones explicit rows without touching
the live revision, while automatic state clones as zero rows. Reset deletes candidate target rows.
Pending review locks mutation; submission and approval revalidate the target set, and approval fails
closed if Program eligibility, Institution coherence, or the Curriculum mapping changed.

The Instructor UI shows semantic Programs in EN/AR, offers **Customize Audience** and **Use automatic
audience**, and never offers Programs outside the inferred set. Admin review labels the mode and
shows the effective Programs without UUIDs.

## Proof

- Real-Postgres catalog and HTTP integration suites passed, including inference, subset validation,
  cross-Institution refusal, reset, cloning, live/candidate isolation, and approval revalidation.
- `e2e/t4cde-instructor-academic-context.spec.ts`: C1–C5 passed in the focused 5-test run and in the
  final canonical run.
- Focused T4-C/D/E E2E result: `5 passed (1.0m)`.
- Final canonical result: `142 passed / 6 accepted failures / 3 did not run (10.2m)`.
