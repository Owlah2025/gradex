# MVP-F14 / ST-04 Public Preview — completion evidence

**Recorded:** 2026-08-21T08:02:00+03:00

This record closes the already-canonical ST-04 Public Preview remainder. It applies D-019 and
BR-143: a preview is a separately uploaded, revision-scoped `PREVIEW` asset, never protected Lesson,
resource, or Lab media. It does not create a denominator row.

## Focused implementation proof

```text
cd backend && go test -tags=integration ./internal/db ./cmd/migrate -count=1
  PASS

cd backend && go test ./internal/httpapi -run
  'Test(CourseScopedPreviewResponseDoesNotSerializeTheAssetVersion|D8ProtectedDeliveryDenialsAreByteIdenticalOnTheProductionRouter)' -count=1
  PASS

cd backend && go test -tags=integration ./internal/media ./internal/httpapi -run
  'Test(D8|PublicPreview|Media)' -count=1
  PASS

cd frontend && npm run typecheck && npm test
  PASS: 296 tests passed, 0 failed

cd frontend && npx playwright test e2e/s3-public-catalogue.spec.ts --workers=1 --reporter=line
  PASS: 34 passed (1.5m)

cd frontend && npx playwright test --config=playwright.media-authoring.config.ts --workers=1
  PASS: 1 passed (55.1s)
  run mt2gx6k80dy10ix9; actual API log:
  /var/tmp/gradex-s5-e2e-evidence/gradex-s5-e2e-api-mt2gx6k80dy10ix9.log
```

The media-authoring browser proof uses real private MinIO, a real scanner/worker lifecycle, and a
real H.264/AAC MP4. The Instructor creates a Course, uploads a separate PREVIEW before any Lesson
exists, waits for READY, adds a protected Lesson video separately, submits through the Studio, and
the Admin approves through the review UI. An anonymous visitor finds the Course in Catalogue, plays
the Course-scoped issued preview, and cannot obtain the sibling Lesson through anonymous preview
issuance. The browser asserts the player `src` is the signed URL returned by the course-scoped route.

Real-Postgres integration separately proves A→candidate B→approve B preview switching, candidate
removal, unauthorized/non-READY/cross-Course/cross-revision rejection, stale old-preview refusal,
scanner-only admission, rate-policy shape, and no Enrollment/Entitlement/Progress mutation.

## Backend quality gates

```text
cd backend && go build ./... && go vet ./... && go test ./... && go test -tags=integration ./...
  PASS
```

Migration 0022 passed the repository migration suite and the existing rollback guards remained
green. It adds revision-scoped preview provenance without altering any historic preview bytes.

## Final canonical Playwright reporter

Host load before the run was `3.14, 3.92, 3.87`; no media-authoring test worker was running. The
long-lived development worker predates the run and is not an E2E authoring worker.

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
118 passed (8.4m)
```

The precise canonical run ID was `mt2h4xq7zhwkpklx`; its run-owned API log is retained at
`/var/tmp/gradex-s5-e2e-evidence/gradex-s5-e2e-api-mt2h4xq7zhwkpklx.log`. The six failures exactly
match the protected `114 / 6 / 3` baseline identities. The four new Public Preview browser tests,
all three ST-19 Purchase Request tests, and both S6 access-lifecycle tests passed in this same run.
