# MVP-F20 / T4-E — Admin Review and Public Compatibility

**Date:** 2026-08-22  
**Status:** `PROVEN`

Admin Course Review now projects Academic Course University, official Subject code and localized
title, owning Academic Unit context when available, audience mode, and effective Programs. Academic
review has no Subject repair control: a wrong Subject goes through Request Changes and the existing
Instructor lifecycle. Legacy taxonomy repair remains classification-gated for legacy Courses.

Public catalogue list/detail project semantic University and Subject data for Academic Courses while
retaining legacy projection. Existing `q` search additionally matches Academic Subject code/title and
University through the current search architecture. No University, Program, Level, or Subject filter
was added. Purchase, entitlement, invitation, access, progress, learning, video, and material
authority remain independent of Academic metadata and Student Academic Profile.

## Proof

- Real-Postgres Admin review and public projection/search integration tests passed.
- Focused T4-C/D/E journey published an Academic Course, found it by academic `q`, rendered semantic
  details and the purchase CTA, then proved revision audience live/candidate isolation.
- Existing-feature browser regression: `71 passed (5.8m)`, covering T1, T2, T3, T4-B, legacy
  authoring/review, public catalogue, ST-19 purchase, S6 access/invitations, and Admin review.
- Media-authoring regression: `2 passed (1.5m)`, covering IN-09, F14 preview, and ST-15 materials.
- Final canonical: `142 passed / 6 accepted failures / 3 did not run (10.2m)`; no new identity.
