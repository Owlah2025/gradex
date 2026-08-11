# T033 — media-authoring E2E configuration diagnosis

**Date:** 2026-08-11
**Authority:** D-084; `specs/005-media-and-entitlement-evaluation/tasks.md` T033
**Classification:** `NOT_YET_ISOLATED`

## Scope and method

T033 permits diagnosis only. No production media code changed. The E2E setup now reads the six
non-secret configuration values from `/proc/<pid>/environ` immediately after each run-owned API and
worker process is spawned, fails if any is missing, and writes the values to the test output. This is
runtime observation of the child processes, not an inference from an environment file.

The prescribed command was run against an isolated local PostgreSQL, Redis, and version-enabled,
private MinIO bucket. The API used a per-run database named
`gradex_playwright_e2e_msp2u7tcwd438raf` and the harness removed it during normal teardown.

```text
cd frontend && npm run test:e2e:media-authoring
```

Result: `1 passed (23.3s)`.

## Effective runtime configuration

| Process | APP_ENV | MEDIA_SCANNER_MODE | MEDIA_OPERATING_MODE | REDIS_ADDR | Object storage endpoint | Bucket |
|---|---|---|---|---|---|---|
| API | `development` | `DEVELOPMENT_NO_OP` | `SCANNER` | `localhost:6379` | `http://localhost:9000` | `gradex-video` |
| Worker | `development` | `DEVELOPMENT_NO_OP` | `SCANNER` | `localhost:6379` | `http://localhost:9000` | `gradex-video` |

Both processes therefore resolved the intended local Instructor-authoring configuration. The
development-only scanner was accepted only because both were running with `APP_ENV=development`;
this evidence does not enable it in staging or production.

## Pipeline evidence

The real-browser test generated a genuine MP4 with ffmpeg, uploaded it through the real presigned
upload contract, and received `201` from `POST /api/v1/media/uploads` then `200` from its completion
endpoint. The run-owned API log records status polling after completion and the test's assertion
received `state: READY` from `GET /api/v1/media/assets/:id` before attaching the resulting version to
the Lesson.

The worker log records lifecycle `STARTING` then `READY` before upload, and no `worker_failed`, queue,
scanner, or processing error before orderly `DRAINING` and `STOPPED` teardown. The pass proves the
complete persisted/outbox/Redis/asynq/worker/scan/transcode path for this run: a video can only become
`READY` after scan evidence, transcoding, and persisted rendition evidence under the existing media
state machine. The harness drops its per-run database at teardown, so no post-teardown outbox or
queue rows are retained; this run did not add intrusive database polling to a passing product path.

Observed chain:

```text
real MP4 upload → completion accepted → status polling → worker READY → asset READY
→ Lesson attachment → Course submission → Admin queue and approval
```

## Diagnosis

The previous `Processing` stall did not reproduce in three automated runs; the final captured run
was green. The API and worker had matching Redis and MinIO targets and the correct scanner and
operating modes, so the available evidence does **not** prove `CONFIGURATION_DEFECT`. It also does
not prove a worker, outbox, dispatcher, Redis, scanner, or transcoding defect because the complete
path succeeded.

The original failure's effective process configuration and ephemeral per-run state were not
captured, so its cause cannot be identified from this green reproduction. The correct T033 result is
`NOT_YET_ISOLATED`, not a confirmed pipeline finding.

## Required next authority

Do not modify production media code. A task amendment is required before a new diagnostic that
retains/polls the failing run's database and asynq state, or before any production pipeline change.
