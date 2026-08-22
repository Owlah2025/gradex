# ST-15 Protected Resource / Lab Download Design

**Status:** approved scope — 2026-08-21

## Authority and boundary

`docs/mvp/FUNCTIONAL_COMPLETION.md` defines ST-15 as **Protected Resource/Lab download**.  D-011 and BR-063/066/067/068/103/104/115 govern the existing `lesson_files` model.  The user confirmed that both Student-facing categories belong to this tranche, while new Instructor upload work is restricted to D-088's PDF/DOCX Lesson Resource profile.  No new Lab authoring system, file types, submissions, progress behavior, or public delivery is in scope.

## Existing model retained

`lesson_files` attaches immutable media asset versions to revision-scoped `course_lessons`; a stable `course_lesson_identities` row identifies the Lesson across revisions.  Candidate cloning copies file attachments, and the live pointer remains unchanged until Admin approval.  The existing media service already keeps objects private, checks exact-version scan or trusted-validation provenance, re-evaluates entitlement immediately before signing, and issues short-lived storage URLs.

## Changes

1. Project every eligible, live `lesson_files` row—not only one row per material kind—with localized filename, safe file-type label, size, and an opaque same-origin authorization route.  Resource and Lab Material collections stay distinct; storage, asset-version, revision, and scanner values stay absent from Student JSON/UI.
2. Add a same-origin, authenticated download-authorization endpoint addressed by stable Lesson plus revision-scoped file attachment.  Its database query proves current approved graph membership, correct Lesson/Course, allowed kind, READY state, exact provenance, non-retirement, then invokes the existing entitlement evaluator before signing.  Possession of the attachment identifier conveys no authority.  Legacy category-entry routes remain compatibility paths.
3. Render compact, localized Resources and Lab Materials sections on Course Home and Lesson pages.  Each row shows its meaningful title, safe type, size, and a Download action.  A small client action obtains the short-lived URL and reports authorization/issuance failures locally before navigating to the private object URL.
4. Retain existing Instructor Resource upload and attachment control, correcting only evidenced integration gaps.  It remains PDF/DOCX, private, exact-version validated, candidate-scoped, auditable, and uses immutable bytes.  Existing Lab fixtures/associations exercise Lab Student delivery without adding Lab authoring behavior.
5. Add integration, focused frontend, and browser evidence covering real bytes, Resource/Lab presentation, candidate isolation, removal, entitlement and inventory-safe denials, and no progress mutation.  Update ST-15 only after those observed runs.

## Acceptance invariants

- Live A remains downloadable while candidate B changes/removes material; B becomes visible only after approval.
- Resource and Lab Materials use the same server-side entitlement gate but retain their category and Lab buyer-tag behavior.
- A material is never public, never selected by only an asset/version identifier, and never projected from a candidate revision.
- Download issuance never writes progress, completion, access, or authority state.
