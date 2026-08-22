# ST-15 — Protected Resource / Lab download completion evidence

**Recorded:** 2026-08-21

ST-15 is the existing canonical Student row, **“Protected Resource/Lab download.”** This record
closes both protected categories. It does not create a denominator row or a Lab-authoring feature.

## Product and domain boundary

- The Resource authoring journey uses the bounded D-088 trusted-Instructor profile: PDF and DOCX
  only. The existing configured Resource limits remain 50 MB per file and 200 MB per Lesson.
  The server owns type/size/exact-version validation; the browser's `.exe` rejection is only
  user feedback.
- Lab Materials remain their distinct canonical category. No Lab upload UI, asset type, or
  authoring workflow was introduced. The canonical E2E seed already owns the live Lab Material
  association; the test-only `cmd/e2e-material-fixture` writes deterministic private fixture bytes
  only when its development, loopback-storage, and explicit opt-in guards all hold.
- There is no new schema migration. Existing revision-scoped `lesson_files`, stable Lesson
  identities, live-revision cloning, private object storage, trust state, and publication
  validation are reused.

## Protected delivery contract

`POST /api/v1/media/courses/:courseId/lessons/:lessonId/materials/:materialId/download-authorizations`
is behind protected-learning authentication and rate limiting. Its server-side resolver joins the
requested Course, current approved live revision, stable Lesson, and attachment; it accepts only a
current `READY` target with a private storage key, then invokes the canonical entitlement evaluator.
The attachment identifier is therefore insufficient for authorization.

The Student projection separates `resources` from `lab_materials`. Each item has localized title,
human-readable file type, size, and an opaque same-origin authorization path; it has no storage
key, asset-version ID, revision ID, scanner data, or raw kind enum. The authorization response is
`no-store` and `no-referrer` and contains only a short-lived URL and expiry. Private S3 delivery
sets an attachment content disposition from a sanitized display filename.

The D-088 PDF/DOCX path may become `READY` only after its exact stored version passes the existing
trusted validation. Lab Material trust remains governed by its existing canonical mechanism. A
failed, unavailable, or non-ready attachment is not projected or authorized.

## Retained real browser proof

```text
cd frontend && npx playwright test --config=playwright.media-authoring.config.ts \
  s15-protected-materials.spec.ts --workers=1 --reporter=line

PASS: 1 passed
run mt2w7c96q7dsxftp
API log: /var/tmp/gradex-s5-e2e-evidence/gradex-s5-e2e-api-mt2w7c96q7dsxftp.log
```

This is deliberately a separate media-authoring configuration, not a mocked canonical run. It uses
an isolated PostgreSQL database and real private MinIO delivery. The browser proof does all of the
following through product UI and production routes:

1. An entitled Student opens the approved live Lesson and sees separate Resources and Lab Materials.
   Clicking each Download control first obtains protected authorization, then retrieves actual bytes:
   the Resource is compared byte-for-byte with the PDF fixture and the Lab ZIP's `README.txt` is
   extracted and checked.
2. The Instructor starts candidate B, removes Resource A, sees an unsupported `.exe` rejected,
   uploads a real D-088 PDF through the Resource UI, waits for `Attached`, and submits. No UUID,
   storage key, or asset version is entered by the Instructor.
3. Before Admin approval, the Student still sees and downloads A, not B. Direct candidate, anonymous,
   unentitled, wrong-Course, and wrong-Lesson authorization probes all return the same inventory-safe
   404 response without the attachment ID.
4. The Admin review inspector shows the meaningful Resource name and Resource type, then approves B.
   The live Student projection atomically switches to B; B's real PDF bytes and the existing Lab
   ZIP bytes download through the protected path.
5. Candidate C removes B. Before its approval B remains downloadable; after approval Resources are
   absent while the separately modelled Lab Material remains downloadable. The Lab association is
   unchanged across these revisions; this proves its protected Student presentation/download without
   claiming an unbuilt Lab-mutation workflow.
6. Progress is read before and after Resource/Lab downloads and remains unchanged. Arabic verifies
   `مواد المختبر` and `تحميل`; English verifies Resources, Lab Materials, and Download. Student
   markup is checked for raw UUID leakage.

## Focused and full quality gates

```text
cd backend && go test ./internal/httpapi ./internal/media ./internal/storage ./internal/ratelimit
  PASS

cd backend && go test -tags=integration ./internal/httpapi ./internal/media -run \
  'TestLearningReadModelsMatch|TestST15LessonFileDownloadProjectsEveryLiveAttachmentAndPreservesRevisionIsolation|TestD064MaterialKindsBulkReadExposesOnlyCurrentReadyKinds' -count=1
  PASS

cd backend && go build ./... && go vet ./... && go test ./...
  PASS

cd backend && go test -tags=integration ./...
  PASS

cd frontend && npm test && npm run typecheck
  PASS: 297 tests passed, 0 failed
```

The focused backend coverage exercises current-live graph resolution, Resource and Lab discovery,
revision switching/removal, ready/trust denial, entitlement denial, anonymous/wrong-Course/wrong-
Lesson/candidate/retired probes, unchanged progress, repeated authorization inertness, filename
sanitization, and response privacy. Focused frontend coverage checks the Resource selection,
validation/upload/remove states, material display/download failure state, API path, and EN/AR copy.

## Coexistence regression

```text
cd frontend && npx playwright test --config=playwright.media-authoring.config.ts \
  s12-instructor-video-upload.spec.ts --workers=1 --reporter=line
  PASS: 1 passed (56.6s)

cd frontend && npx playwright test e2e/manual-purchase-flow.spec.ts \
  e2e/s6-course-access-grant-launch.spec.ts e2e/s3-public-catalogue.spec.ts \
  --workers=1 --reporter=line
  PASS: 39 passed (2.9m)
```

The first run rechecks IN-09's video READY lifecycle. The second rechecks ST-19, the legacy S6
access journey, and F14's public Catalogue/Preview surfaces. The Resource/Lab projection adds no
public Course Details or public-preview material handle.

## Final canonical Playwright reporter

The single-worker canonical suite was run with no media-authoring worker in parallel. Host load was
sampled before regression interpretation; a pre-existing unrelated Next development process was
using CPU, and the performance failure below is the accepted non-production-mode failure.

```text
cd frontend && npx playwright test --workers=1 --reporter=line

6 failed
  [chromium] › e2e/s5-expired-entitlement.spec.ts:712:7
  [chromium] › e2e/s5-playback-performance.spec.ts:157:11
  [chromium] › e2e/s5-viewport-evidence.spec.ts:223:11  (phone)
  [chromium] › e2e/s5-viewport-evidence.spec.ts:223:11  (tablet)
  [chromium] › e2e/s5-viewport-evidence.spec.ts:223:11  (laptop)
  [chromium] › e2e/s5-viewport-evidence.spec.ts:223:11  (desktop)
3 did not run
118 passed (8.5m)
```

Run `mt2xc6szd8cag478` retained its run-owned API log at
`/var/tmp/gradex-s5-e2e-evidence/gradex-s5-e2e-api-mt2xc6szd8cag478.log`. The identities exactly
match the accepted six-failure baseline; ST-15's real-storage browser proof is green and is labeled
separately above.
