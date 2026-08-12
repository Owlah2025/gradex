# Unified Admin Course Review Workspace Design

**Status:** Approved for implementation

**Date:** 2026-08-12

**Scope:** Founder-acceptance Batch 2 for the pending Admin Course review journey

## Outcome

An Admin can complete the launch-critical review flow without developer knowledge or copying an
internal identifier:

`Review Queue → select Course → inspect exact revision → optional taxonomy override → set
Course/Section pricing → preview Lessons → request changes/approve`

The selected queue item carries its Course and revision identities into every review operation. The
loaded submitted revision carries each Section and Lesson identity into pricing and preview
operations. UUIDs remain internal API parameters and may remain in diagnostics, but are not normal
inputs or primary interface content.

## Workspace Design

`ReviewQueue` remains the Admin entry point and server-backed master list. Its UUID launcher and
separate Inspect/Administer actions are removed. Selecting one pending submission opens an inline
detail workspace for that exact submitted revision.

`SubmittedRevisionInspector` remains responsible for loading and verifying the exact Course and
revision through `getReviewCourseRevision`. The detail workspace composes the existing submitted
metadata, Sections, Lessons, media status, and protected Lesson preview with two administration
panels:

- an optional taxonomy override bound to the already-selected Course and revision;
- Course and Section pricing bound to the selected Course, with Section choices labeled by their
  bilingual submitted titles.

The workspace keeps approve and request-changes actions in the same selected-Course context.
Successful review decisions refresh the authoritative queue, clear the selected submission, and
return the Admin to the queue. Pricing or taxonomy failures stay local to their panel and do not
discard the selected review.

## Data and Authority

The existing backend remains authoritative and unchanged:

- `listReviewQueue` supplies only server `PENDING_REVIEW` submissions;
- `getReviewCourseRevision` supplies the exact submitted graph;
- `assignAdminTaxonomy` applies the optional override to that selected revision;
- `setCoursePrice` and `setSectionPrice` receive identities selected by the UI;
- `previewAdminLesson` issues the protected preview for the selected submitted Lesson;
- `approveCourseRevision` and `requestCourseRevisionChanges` complete the review decision.

The client continues to reject a detail or preview response whose Course, revision, Lesson, or media
identity does not match the current selection. Existing authentication, CSRF handling, backend
authorization, audit logging, and review invariants are preserved.

## Taxonomy and Pricing

Instructor-submitted Major and Subject remain visible. Admin taxonomy override is optional, as
required by D-022; the workspace does not introduce a mandatory taxonomy step before approval. The
override form receives the selected revision identity as an immutable prop instead of exposing an
editable `revision_id` field.

Taxonomy vocabulary management remains a separate Admin operation on the existing Admin Course
surface and is not embedded in the selected submission workflow.

Pricing continues to require a non-negative integer amount in fils and an audit reason. Course
pricing uses the selected Course automatically. Section pricing replaces the free-text Section ID
with a select control populated from the submitted revision's Sections. A Course with no submitted
Sections cannot select Section pricing.

## Loading, Empty, and Error Behavior

- Queue loading, empty, refresh, and server-error states remain explicit.
- Selecting a different queue row replaces the current detail context and reloads its exact
  revision.
- Detail actions remain unavailable until the returned Course and revision identities match the
  selected queue item.
- Non-`READY` video remains visible with its media state but cannot request playback.
- Failed pricing, taxonomy, preview, or review calls show localized errors without inventing
  success or losing the selected submission.
- A successful approve or request-changes call refreshes the queue before clearing the selection.
  If the refresh fails, the review action remains completed and the queue error is shown rather
  than re-enabling the completed action.

## Validation

Focused frontend tests must prove:

- no UUID launcher, raw revision input, or raw Section input remains in the pending-review journey;
- one queue selection carries Course and revision identities into exact-detail, taxonomy, pricing,
  preview, and review calls;
- Section pricing presents human-readable submitted titles and sends the selected Section identity;
- taxonomy override remains optional and is fixed to the selected revision;
- protected preview still binds Course, revision, Lesson, and media identities;
- approve and request-changes refresh the queue and clear the completed selection;
- queue, detail, and action failure states remain usable;
- existing backend authorization tests still deny unauthorized review operations.

The completed batch must pass focused frontend tests, TypeScript typecheck, lint, and production
build. Backend authorization regression tests are run even though backend production code is not
changed.

## Non-Goals

- No post-publication Course inventory or discovery.
- No lifecycle, archive, relist, retire, suspend, or owner-reassignment workflow changes.
- No staff selector or Account UUID remediation.
- No taxonomy vocabulary redesign or new taxonomy route.
- No backend endpoint, authorization, pricing invariant, or publication-policy change.
- No Course Access, entitlement, email, public preview, analytics, reports, or Student-learning work.
