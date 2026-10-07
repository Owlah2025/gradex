# 2026-10-07 incident — Instructor video uploads fail with DEPENDENCY_UNAVAILABLE

Status: **fix deployed (`9b19644`); storage path verified against production R2. The QA Instructor
browser upload, processing, playback, resumable and cancel checks are still to be run by a human.**

| Item | Value |
|---|---|
| Symptom | Course Builder → Lesson video upload: "Upload failed — A dependency is temporarily unavailable. Try again shortly." plus "A saved upload is available…" |
| Failing endpoint | `GET /api/v1/media/uploads/:id/multipart/verification` → **503 `DEPENDENCY_UNAVAILABLE`** (request ids `1c862ad7…` 20:23:42Z, `1da0eda6…` 20:30:21Z, `a158c940…` 20:34:57Z) |
| Window | From the `d082726` release (19:33Z) until hotfix `9b19644` (21:22:41Z). All video uploads use the new multipart path |
| Production SHA before / after | `d0827261e5e756d196fc80af3ff5a23dcc079bef` → `9b1964485bcd2ac68ffe13edb94e69b9d1a47b5b` (branch `hotfix/production-video-upload-20261007`) |
| Impact | 4 real uploads: 2 ASSEMBLED stuck with `UNAVAILABLE`, 2 ABORTED. No data loss. Existing READY media were unaffected |

## Root cause

Cloudflare R2 returns `x-amz-version-id` from `CompleteMultipartUpload`. `storage.CompleteMultipartUpload`
preferred that version id and recorded it as `upload_intents.multipart_object_version`. The worker's
`multipart_verification` then reads the object by that identity
(`verifyCompletedObject` → `HeadObjectVersion` / `DownloadPrefixVersion` / `HashObjectVersion` with `versionId`).
R2 refuses versioned reads with **HTTP 501 `NotImplemented` ("versionId not implemented")**. That error is
mapped to `ErrUnavailable` and retried three times (worker log `worker_failure operation=multipart_verification`).
The intent is then parked with `multipart_verification_error='UNAVAILABLE'`, and the verification endpoint
answers 503.

**Why production only:** local, CI and E2E use MinIO, which honours `versionId`. The browser single-PUT path
already ignored R2 version ids (`62fc5b1`, Aug 2026), so all 105 READY versions carry `etag:` identities. The
multipart path, which shipped in the hardening release, did not.

**Evidence:** a storage probe ran on the host with the worker's own S3 environment:
- On a real stuck object (read-only), HEAD with the recorded version id → 501. HEAD current and HEAD by
  `If-Match` on the strong ETag → OK.
- On a QA object, the same chain reproduced: completion returned version id `7e5ee7dd…`, then HEAD → 501 and
  GET → 501 "versionId not implemented".
- On the QA object, the ETag identity worked for HEAD and hash. A wrong ETag returned 412.

## Fix (`9b19644`)

`storage.Client` records objects by strong ETag on an R2 endpoint (`*.r2.cloudflarestorage.com`) or when
`Options.ETagObjectIdentity` is set. This applies to `CompleteMultipartUpload` and to the lost-response
reconciler `MultipartObjectIdentity`. Both fail closed if no strong ETag comes back. Versioned providers keep
their version id.

Nothing on the read side is relaxed. Exact reads stay bound by `If-Match`, and the worker still hashes the bytes
and compares them with the client SHA-256. Ownership, multipart binding, trusted processing and idempotency
are unchanged.

## Verification

- **Regression tests:** `backend/internal/storage/multipart_identity_test.go` fails on `d082726` and passes on
  the fix.
- **Local gates:** gofmt, `go build`, `go vet`, `go vet -tags=integration`, `go test -race`
  (storage/media/httpapi), storage integration (MinIO), media integration — full suite, 0 FAIL, 580 s.
- **Hosted CI:** run 37685660302 passed all 6 jobs.
- **Independent review:** agy **APPROVE**, no findings. Codex **APPROVE**, no findings.
- **Deploy:** `host.sh apply-release` recreated api, worker and frontend only, with roughly 5 s of API
  unavailability. Schema stays 57. `verify` passed. The running worker binary hash equals the local build
  (`51d95e65…`). Postgres, Redis and edge were not restarted. `runtime.env.before-9b1964485bcd` was kept.
- **Production R2 with the deployed image:** completion recorded `etag:"…-1"`; HEAD, prefix GET, hash and
  reconciliation all succeed.
- **Existing media (read-only):**
  - All 105 READY versions keep `etag:` identities.
  - 147 lessons are bound to READY video.
  - All 7 public previews serve video/mp4 (206).
  - 0 processing attempts and 0 active claims, so no reprocessing occurred.
  - 0 API/worker ERROR after deploy.

## Not yet done

- **QA Instructor browser flow** (upload → processing → READY → playback, interrupt/resume, cancel): the
  operating agent may not create accounts or sign in with passwords on production, so a human must run it.
- **Stuck sessions:** the 2 ASSEMBLED sessions keep their unreadable version id. The affected Instructors
  should press **Cancel upload** and upload again. Repairing those rows in place would need separate
  authorization.
- **QA objects left in R2** (64 KiB each) under `quarantine/qa-upload-incident-20261007/`: `probe-20261007T205135Z`,
  `fixed-probe-20261007T205456Z` and `fixed-probe-20261007T212257Z`.
- **Follow-up:** add a provider-tagged R2 multipart test next to `r2_provider_integration_test.go`, and log the
  verification failure reason (not only the error class) in `worker_failure`.
