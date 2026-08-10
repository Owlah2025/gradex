# Instructor Course Authoring Studio — founder manual finding and remediation

**Date:** 2026-08-10
**Authority:** Product Owner launch remediation instruction; recorded as
[D-077](../../../DECISIONS.md#d-077--the-instructor-authoring-ui-is-wired-to-the-existing-authoring-and-media-apis-and-a-development-only-scanner-mode-makes-the-whole-path-testable).
**Status:** Implemented and locally proved. **Not reviewed, not closed.** The range this record
describes has had no independent reviewer verdict.

## 1. The manual finding, as observed

A founder manual browser test recorded the following, and every point of it is accurate:

| Observation | Verdict |
| --- | --- |
| Instructor login works | Confirmed — real session, real role |
| `/en/instructor/courses` renders | Confirmed |
| Server-backed reads on that page work | Confirmed — the pricing and taxonomy panels were already reading `GET /api/v1/courses` |
| Created Course disappeared after refresh | Confirmed — it was never sent to the server |
| The page showed a "Local Demo Drafts" section | Confirmed — a hard-coded fixture including `course-demo-1` |
| Real authoring APIs already existed | Confirmed — Course, revision, Section, Lesson, video, submit, and Admin review routes, with passing integration tests |

## 2. Root cause

`frontend/src/components/instructor/course-builder.tsx` was a self-contained React component. Its
Course list was a `useState` initial value containing `course-demo-1`, and `handleCreateCourse` and
`handleAddSection` appended to that array. No authoring endpoint was called from the studio at all,
so a reload discarded everything by construction. The heading over the list — "Local Demo Drafts" —
described the behavior accurately; what was wrong was that it was the production Instructor journey.

Two backend defects sat behind the same journey and would have blocked it even with the UI wired:

- **Asset Version validation read the wrong table.** `catalog.DBAssetVersionValidator` queried the
  legacy `videos` table, while every Instructor upload produces a `media_asset_versions` row. A
  genuinely uploaded, scanned, processed video was therefore rejected as an invalid reference, so
  `PUT .../lessons/:lessonId/video` could never succeed for real content.
- **The upload intent withheld the object key the completion requires.** `POST /media/uploads`
  returned only the Asset Version ID, presigned URL, and expiry, but
  `POST /media/uploads/:id/completions` requires `storage_object_key`. The browser had no
  non-fragile way to supply it.

## 3. What changed

Frontend:

- The studio now loads Instructor-owned Courses from `GET /api/v1/courses` and re-reads the Course
  graph after every successful command. It holds no authored content of its own.
- Create Course, revision details (`title_ar`, `title_en`, `description_ar`, `description_en`,
  taxonomy, study year), add/delete Section, add/delete Lesson, attach video, and submit for review
  all call the existing routes with the existing session/CSRF conventions.
- A per-Lesson MP4 control drives the existing upload contract and reports
  `Preparing → Uploading → Processing → Ready`, or `Failed` with a retry. A command in flight
  disables the controls, so a double click cannot issue it twice.
- Submission rejections render the server's own violation codes.
- The demo fixture and `course-demo-1` are deleted; no demo Course exists in the production journey.

Backend:

- `ValidateAssetVersion` treats `media_asset_versions` as authoritative and accepts only `READY`,
  falling back to the legacy table only for identifiers with no media row.
- The upload ticket returns `storage_object_key`.
- `MEDIA_SCANNER_MODE` selects the scanner boundary. The default remains `UNAVAILABLE`;
  `DEVELOPMENT_NO_OP` is refused outside `APP_ENV=development` and records the scanner identity
  `development-no-op-scanner` in scan evidence.

Fixture:

- The E2E seed now gives `instructor@example.test` the same real test credential the Admin and
  Students already had, and adds an unrelated second Instructor
  (`instructor-other@example.test`) so Course ownership can be proved rather than assumed. The
  manual SQL workaround used during founder testing is no longer needed. No production default
  credential was introduced.

## 4. Acceptance evidence

`frontend/e2e/s12-instructor-authoring.spec.ts` (shared E2E environment) — 4 passed:

- **A** Course created in the studio survives a page reload, with its server-issued ID; "Local Demo
  Drafts" and `course-demo-1` are absent from the page.
- **B** Section and Lesson persist with their exact authored structure across two reloads.
- **D** A second authenticated Instructor is refused both mutation and read of another Instructor's
  Course, and a Student is refused Course creation and the owned-Course list; the owner's Course is
  unchanged afterwards.
- **E** An incomplete submission is refused and the studio displays the server's own violation codes
  (`TAXONOMY_DIMENSION_MISSING`, `COURSE_EMPTY`).

`frontend/e2e/media-authoring/s12-instructor-video-upload.spec.ts` (dedicated environment with real
MinIO, a running worker, and ffmpeg) — 1 passed:

- **C/E** A real ffmpeg-produced MP4 is uploaded by the browser straight to private storage, the API
  verifies the exact stored object version, the worker scans and transcodes it, the Asset Version
  reaches `READY`, it is attached to the Lesson, the attachment survives a full reload and is
  confirmed by `GET /media/assets/:id` returning `READY`, and the completed Course is submitted and
  observed in the Admin review queue.

Backend: `gofmt`, `go build ./...`, `go vet ./...`, `go vet -tags=integration ./...`, `go test ./...`,
and `go test -tags=integration ./internal/catalog/` all pass, including a new integration test that
pins Asset Version validation to the media pipeline.

Frontend: `npm ci`, `npm run typecheck`, `npm test` (171 passed), `npm run lint`, `npm run build`.

## 5. Remaining external dependencies

- **`LG-014` is still OPEN.** No production malware scanner is selected, so in staging and production
  no Instructor upload can reach `READY`. The production-like stack still runs
  `MEDIA_OPERATING_MODE=ADMIN_CATALOGUE`, where Instructor uploads are refused by design and Admin
  catalogue loading with out-of-band scan evidence is the only path. The Instructor browser journey
  proved here is complete in software and blocked in production solely by that gate.
- Storage must have object versioning enabled and must expose `x-amz-version-id` to the browser. The
  developer Compose bucket now enables versioning; Cloudflare R2's exposure is already asserted by
  `internal/storage/r2_provider_integration_test.go`.

## 6. Local manual acceptance environment

From the repository root:

```bash
# 1. Infrastructure: PostgreSQL, Redis, and a version-enabled private MinIO bucket.
cd backend && docker compose up -d

# 2. Schema.
DATABASE_URL='postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable' go run ./cmd/migrate up

# 3. API and worker, development mode with the no-op scanner. Run each in its own terminal.
export DATABASE_URL='postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable'
export APP_ENV=development PUBLIC_ORIGIN=http://localhost:3000 CORS_ALLOWED_ORIGINS=http://localhost:3000 CORS_ALLOW_CREDENTIALS=true
export REDIS_ADDR=localhost:6379
export S3_ENDPOINT=http://localhost:9000 S3_BUCKET=gradex-video S3_ACCESS_KEY=gradexminio S3_SECRET_KEY=gradexminio S3_USE_PATH_STYLE=true
export MEDIA_OPERATING_MODE=SCANNER MEDIA_SCANNER_MODE=DEVELOPMENT_NO_OP
export SESSION_CSRF_KEY=0123456789abcdef0123456789abcdef
export ANONYMOUS_COOKIE_SIGNING_KEY=1123456789abcdef0123456789abcdef
export ANONYMOUS_CSRF_KEY=2123456789abcdef0123456789abcdef
export PLAYBACK_TOKEN_SECRET=3123456789abcdef0123456789abcdef
export OUTBOX_PROTECTED_PAYLOAD_KEY=4123456789abcdef0123456789abcdef
export OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION=dev-v1
export ADMISSION_LIMITER_HMAC_KEY=5123456789abcdef0123456789abcdef
SERVICE_ROLE=api PORT=8080 go run ./cmd/api        # terminal 1
SERVICE_ROLE=worker go run ./cmd/worker            # terminal 2

# 4. Frontend.
cd ../frontend && npm ci && GRADEX_API_ORIGIN=http://localhost:8080 npm run dev

# 5. An Instructor account to sign in with (development database, developer-chosen password).
cd ../backend && go run ./cmd/bootstrap-admin --help   # follow its instructions for the Admin,
                                                       # then create the Instructor through the
                                                       # Admin surface
```

The keys above are local development fixtures. `APP_ENV=production` rejects both these placeholder
secrets and `MEDIA_SCANNER_MODE=DEVELOPMENT_NO_OP`.

Automated equivalents:

```bash
cd frontend
npx playwright test e2e/s12-instructor-authoring.spec.ts   # persistence, structure, authorization, submission refusal
npm run test:e2e:media-authoring                            # real MP4 upload, worker, READY, attach, submit
```

Then, in the browser at `http://localhost:3000/en/instructor/courses`: create a Course, refresh, add
a Section, refresh, add a Lesson, refresh, choose an MP4 on that Lesson and wait for `Ready`,
refresh, set Major/Subject/study year, and submit for review.
