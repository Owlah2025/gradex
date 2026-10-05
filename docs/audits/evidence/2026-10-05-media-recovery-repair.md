# RU-01 media recovery evidence repair

Date: 2026-10-05. Repair base: `7964aeb43ee98dcfd1fe4859ec614ff83ac8c2d8`.
Review: `.hardening-campaign/reports/M2-media-review-2.md`.
Scope: the review's sole blocking finding, RU-01 (Sev2, recovery E2E fixture).

## Defect and repair

The recovery spec selected institution index 1 and searched for `CS101`.
`backend/internal/academic/institutions.go` sorts institutions by English name;
`seedSubjectCatalogueFixtures` in `backend/cmd/e2e-seed/seed_test.go` supplies
E2E Subject University with `SUB …` and `ZZZ 900` subjects. That selection cannot
provide `CS101`, and the upload assertions are never reached independently.

`frontend/e2e/media-authoring/resumable-upload.spec.ts` now selects the seeded
Kuwait University ID `91000000-0000-0000-0000-000000000001` and searches for
`0418-101`. Both are supplied by `seedLandingAcademicFixtures` on every isolated
run. The selector requires exactly one matching subject with the expected
English title, using the separator rendered by `subjectLabel`.
The dedicated course remains separate from shared course fixtures.
Production code and the existing upload assertions are unchanged.

## Validation

The committed spec was run alone, with one Chromium worker and retries disabled:

```sh
cd frontend
GRADEX_E2E_TMP_DIR=/tmp/gradex-ru01-20261005 \
GRADEX_PLAYWRIGHT_HTML_DIR=../.hardening-campaign/reports/RU-01-artifacts/playwright-report-2 \
npx playwright test --config=playwright.media-authoring.config.ts \
  --output=../.hardening-campaign/reports/RU-01-artifacts/test-results-2 \
  e2e/media-authoring/resumable-upload.spec.ts --workers=1 --retries=0
```

The run explicitly used the harness's default local PostgreSQL admin/application
DSNs, Redis at `localhost:6379`, and real MinIO at `http://localhost:9000` with
bucket `gradex-video` and development fixture credentials. The standard safety
harness created `gradex_playwright_e2e_muunw48mb366une8`, applied migrations,
seeded it, and launched the real API and media worker. The development scanner
mode was `DEVELOPMENT_NO_OP`; this does not establish production scanner acceptance.

Result: **1 passed (1.0m)**; test duration **47.1s**. Assertions proved:

- Provider parts `[1, 3]` remain after part 2 fails.
- Another Instructor cannot read or cancel the session.
- Page refresh restores the session and retries only part 2.
- Recovery creates no second upload session; parts 1 and 3 each upload once.
- The attached video remains attached after another page reload.
- Cancellation persists `ABORTED`, and MinIO refuses a previously signed part
  with HTTP 404 and `NoSuchUpload`.

`npx tsc --noEmit --incremental false`,
`npx eslint e2e/media-authoring/resumable-upload.spec.ts`, and
`git diff --check` passed. Test-guard review confirmed real persistence/provider
evidence and preserved behavioral assertions; docs-guard checked this report
against the source and run log.

Test source SHA-256:
`7fb77074a3a7658e6bc6b4c76dbd05e6c07411d7fe8045a1eff24c8bd8664645`.
Runtime log: `.hardening-campaign/reports/RU-01-artifacts/e2e-2.log`.
Archived API log:
`/tmp/gradex-ru01-20261005/gradex-s5-e2e-evidence/gradex-s5-e2e-api-muunw48mb366une8.log`.
Worker log:
`/tmp/gradex-ru01-20261005/gradex-media-e2e-worker-muunw48mb366une8.log`.

The first development attempt failed because the new selector initially used
the wrong subject-label separator; that was corrected before the passing run.
Its artifacts remain alongside the passing run. A read-only PostgreSQL query
after teardown confirmed both run-created databases were absent. Existing
untracked files and earlier processes were preserved. No remote push or
production operation was performed.

## Remaining scope

RU-01's required standalone recovery evidence is satisfied. RU-02 (Sev3) remains:
repeated failures in the oldest cleanup batch can starve later abandoned uploads
in `backend/internal/media/multipart_cleanup.go`. Retry scheduling or fair batch
rotation and a starvation regression remain a separate scale follow-up.
The complete campaign gates, independent final approval, and live production
provider/manual acceptance are not established by this focused repair.
