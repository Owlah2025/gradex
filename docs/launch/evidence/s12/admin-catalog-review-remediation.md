# Admin Catalog review remediation

Date: 2026-08-10
Branch: `launch-integration-20260810`
Route: `/en/admin/catalog`
Component: `frontend/src/components/admin/review-queue.tsx`

Founder manual acceptance found two launch-blocking defects on the Admin Catalog surface. Both are
fixed here, together with the Instructor-side submission-failure visibility problem the same journey
exposed.

## Observed defects

### A — taxonomy administration coupled to the pricing modal

`review-queue.tsx` rendered

```tsx
{pricingCourseID && <TaxonomyControls courseID={pricingCourseID} />}
```

so a single state variable carried two unrelated meanings: *which Course is being administered* and
*is the pricing dialog on screen*. Manual sequence: enter a real Course UUID, click **Manage
Pricing**, taxonomy controls appear behind the modal, close the modal, taxonomy controls disappear.
`LifecycleControls` was gated the same way.

### B — the review queue was component state

The queue was initialised from a literal:

```tsx
const [items, setItems] = useState<ReviewQueueItem[]>([{ course_id: "demo-course-1", ... }])
```

so `/en/admin/catalog` displayed "Introduction to Programming" / `demo-course-1` — a Course that does
not exist — while a real founder Course sat in the database. **Approve & Publish** and **Request
Changes** removed rows from that local array and called no API at all.

### C — an Instructor submission failure was invisible

`SUBMISSION_INCOMPLETE` rendered only in the page-level error region at the top of the Course
Authoring Studio, while **Submit for Review** sits far below it. A founder clicking Submit saw
nothing change and read the click as a no-op.

## Root causes

1. **A** — one piece of state (`pricingCourseID`) used as both the administered-Course identity and
   the modal's open/closed flag. Closing the modal cleared the identity.
2. **B** — the Admin surface was never wired to `/api/v1/admin/review/*`. The Go API already served
   `GET /queue`, `GET /courses/:id/revisions/:revisionId`, `POST .../approve`,
   `POST .../request-changes` and `POST .../preview/:lessonId`; the frontend had no client for any of
   them.
3. **C** — the failure was rendered once, at a location unrelated to the control that produced it,
   and nothing brought it into view or took focus.

## Changes

### Demo-state removal

`review-queue.tsx` no longer contains any Course literal. The queue starts as `[]` and is replaced by
the server's response. `frontend/src/components/admin/admin-catalog-surface.test.ts` scans every
production file under `src/components/admin` and `src/app/[locale]/admin` for the removed fixture and
for the spellings a reintroduction would use, and asserts the scan is not vacuous.

An empty server response renders the honest empty state (`review-queue-empty`); it is never replaced
with substitute content. A `204`/null body is *not* treated as an empty queue — `listReviewQueue`
fails closed there, so "nothing pending" and "the server said nothing" stay distinguishable.

### Real review API

New client `frontend/src/lib/api/review.ts`:

| Function | Route |
| --- | --- |
| `listReviewQueue` | `GET /api/v1/admin/review/queue` |
| `getReviewCourseRevision` | `GET /api/v1/admin/review/courses/:id/revisions/:revisionId` |
| `approveCourseRevision` | `POST /api/v1/admin/review/courses/:id/revisions/:revisionId/approve` |
| `requestCourseRevisionChanges` | `POST /api/v1/admin/review/courses/:id/revisions/:revisionId/request-changes` |

No second review system was added and no backend route, handler, or domain rule was changed. Approve
and request-changes address `item.course_id` and `item.revision_id` from the queue row — the
server's own identifiers — so revision binding and the backend's concurrency behaviour are unchanged.
Mutations fail closed without a session CSRF token, before any `fetch`.

### Pricing / taxonomy decoupling

`frontend/src/lib/admin/catalog-administration.ts` models the surface state with the two concepts
separated: an `administered` Course (with its revision, when known) and a `pricingModalOpen` flag.
`closePricingModal` closes only the dialog. Taxonomy and lifecycle visibility follow the administered
Course. Selecting a different Course closes a dialog bound to the previous one; re-selecting the same
Course leaves it alone. The module is React-free so the rule is provable directly.

### Taxonomy management

`TaxonomyControls` now takes an optional `defaultRevisionID`, so a Course opened from the queue
prefills the override form's revision field. The field stays editable and the override still binds to
the revision named in it — the existing explicit-revision semantics are unchanged. Vocabulary
creation (MAJOR, SUBJECT, SUBJECT `academic_code`) and assignment continue to use the existing
`/admin/taxonomy/terms` and `/admin/courses/:id/taxonomy` routes untouched.

### Submission failure visibility

`course-builder.tsx` now reports a submission failure a second time, beside the Submit control
(`data-testid="submit-error"`, `role="alert"`), scrolls it into view and focuses it. The text is
`describeApiError(...)` verbatim — the server's detail plus every violation code. Nothing is
suppressed or reworded, and the page-level error region is unchanged.

## Authorization and security

No authorization, CSRF, recent-auth, audit, revision-binding, or concurrency behaviour was weakened;
no backend production code was modified at all. Proofs:

- `TestCatalogAdminMutationRoutesDenyInstructor` (existing) — every mounted `/api/v1/admin/*`
  mutation route returns 403 for an Instructor, including the taxonomy vocabulary and review routes.
- `TestCatalogAdminReadRoutesDenyInstructor` (**added**) — the read half. Every mounted
  `GET /api/v1/admin/*` route returns 403 for an Instructor, so the newly-consumed review queue and
  revision-graph reads are refused. Derived from the router, so a future Admin read route is covered
  the moment it is mounted.
- `admin-catalog-surface.test.ts` — the Instructor surface imports no Admin taxonomy mutation, no
  Admin route prefix, and neither Admin taxonomy component.
- E2E `S14 C` — an Instructor session is refused `POST /admin/taxonomy/terms`,
  `PUT /admin/courses/:id/taxonomy` and `GET /admin/review/queue`, and the refused term never appears
  in the vocabulary.

Out of scope and untouched: S6 access semantics, transactional email, Instructor authoring/media
semantics.

## Tests

Frontend unit (`npm test`, 206 pass / 0 fail):

- `src/lib/api/review.test.ts` — real routes and methods; real Course/revision IDs; `[]` returned as
  an empty queue; `204` fails closed; CSRF fail-closed before `fetch`; empty change-request reason
  refused client-side; no anonymous bootstrap on an Admin read.
- `src/lib/admin/catalog-administration.test.ts` — taxonomy survives `closePricingModal`; taxonomy
  without ever opening pricing; pricing cannot open with nothing administered; switching Course
  closes the stale dialog; blank identifier administers nothing.
- `src/components/admin/admin-catalog-surface.test.ts` — no demo fixture anywhere on the Admin
  surface (with a non-vacuity check); the queue is server state; actions use `item.course_id` /
  `item.revision_id`; taxonomy and lifecycle are not gated on pricing state; the Instructor surface
  holds no Admin taxonomy capability; the submission failure renders after the Submit control, is
  scrolled to, focused, and carries the server's own message.

Backend (`go test ./internal/httpapi/... ./internal/catalog/... ./internal/identity/... ./cmd/api/...`):
all pass, including the added read-route denial sweep.

E2E `frontend/e2e/s14-admin-catalog-review.spec.ts` — 4 passed:

- **A** the rendered queue equals `GET /admin/review/queue` row for row; an empty real queue renders
  the honest empty state; no demo Course anywhere in the body.
- **B** the founder's exact sequence — administer, open pricing, close pricing — leaves taxonomy and
  lifecycle administration on screen with the same administered Course.
- **C** the Admin creates MAJOR `علوم الحاسب` / Computer Science and SUBJECT `هندسة البرمجيات` /
  Software Engineering / `SWE101` through the UI; both survive a reload; the Instructor sees both in
  its taxonomy dropdown; the Instructor is refused every Admin taxonomy and review route.
- **D** an incomplete submission renders `TAXONOMY_DIMENSION_MISSING` and `COURSE_EMPTY` at the
  Submit control, within the same screenful as the button.

E2E `frontend/e2e/media-authoring/s12-instructor-video-upload.spec.ts` — extended: after a real MP4
upload, worker transcode and a **successful** submission, the Admin Catalog screen shows the real
submitted Course, **Approve & Publish** is clicked in the UI, the row leaves the queue, and the
server's queue no longer contains it.

**This extension was written but not observed passing.** The suite was run
(`npm run test:e2e:media-authoring`) and failed at its pre-existing line 136 — the Asset Version
never left `Processing` within the 4-minute budget, so the run never reached the submission at line
164 or any of the added Admin steps that follow it. The worker started and reported `READY` but
picked up no job; the API exposes `POST /api/v1/media/assets/:id/out-of-band-scan-evidence`, so the
scan step this environment does not satisfy is the likely stall. The failure is upstream of every
added line and is unrelated to this change, but the added assertions are **unverified** and must be
observed on an environment where the media suite is green before they count as evidence.

## Commands run

```
cd frontend && npm run typecheck
cd frontend && npm test
cd frontend && npm run test:s2-ui
cd frontend && npm run lint
cd frontend && npm run build
cd frontend && npx playwright test e2e/s14-admin-catalog-review.spec.ts --workers=1
cd frontend && npm run test:e2e:media-authoring
cd backend  && go build ./... && go vet ./internal/httpapi/... ./internal/catalog/...
cd backend  && go test ./internal/httpapi/... ./internal/catalog/... ./internal/identity/... ./cmd/api/...
git diff --check
```

## Manual founder retest

1. Sign in as Admin, open `/en/admin/catalog`. With nothing submitted the queue shows
   "No courses pending review currently." — no demo Course.
2. Paste a real Course UUID into **Administer a Course by UUID** and click **Administer Course**.
3. Under **Taxonomy Vocabulary** create MAJOR `علوم الحاسب` / `Computer Science`, then SUBJECT
   `هندسة البرمجيات` / `Software Engineering` with academic code `SWE101`.
4. Click **Manage Pricing**, then **Close**. Taxonomy and lifecycle controls are still on screen and
   still usable.
5. As Instructor, refresh `/en/instructor/courses`. The taxonomy dropdowns contain both new terms.
   Select Major and Subject, **Save Taxonomy**, then **Submit for Review**. Any remaining refusal is
   reported next to the Submit button with the server's own violation codes.
6. Once the Course is complete, submission succeeds.
7. As Admin, refresh `/en/admin/catalog`. The real submitted Course appears with its Course and
   revision IDs. Use **Approve & Publish** or **Request Changes**; both call the real backend and the
   queue is re-read from the server afterwards.

## Remaining blockers

- This slice is **not reviewed**. Claude authored it and must not review it. A recorded reviewer
  verdict against the exact commit range is still required before the slice can close.
- Submission still requires a READY Lesson video, so the founder Course
  `22f215eb-42fc-4bcd-b01e-37ea967a90b8` reaches the Admin queue only once its taxonomy **and** its
  Lesson media are complete. Nothing in this change alters that validation.
- `npm run test:e2e:media-authoring` does not pass in this environment (stalls at `Processing`, see
  above), so the real submit-then-approve journey is proved through the API and the shared suite but
  not yet through that browser run.
- Admin review lesson preview (`POST .../preview/:lessonId`) is served by the API but is not surfaced
  in the Admin UI; approval today is made without in-UI playback of the submitted revision.
