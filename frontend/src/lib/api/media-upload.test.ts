import { test } from "node:test";
import assert from "node:assert/strict";
import {
  ACCEPTED_RESOURCE_CONTENT_TYPES,
	beginPublicPreviewUpload,
  beginResourceUpload,
  beginVideoUpload,
  completeUpload,
  describeAssetState,
  isTerminalState,
  resourceContentType,
  storageObjectVersionFromUploadHeaders,
  uploadFileToStorage,
  validateSelectedResource,
} from "./media-upload";
import { addLessonFile, deleteLessonFile } from "./authoring";

const COURSE_ID = "22f215eb-42fc-4bcd-b01e-37ea967a90b8";
const REVISION_ID = "9c1f0b2a-1111-4a2b-8c3d-4e5f60718293";
const LESSON_ID = "3d5f7a91-2222-4b3c-9d4e-5f6071829304";
const ASSET_VERSION_ID = "7b8c9d01-3333-4c4d-8e5f-60718293a4b5";

const DOCX_TYPE = "application/vnd.openxmlformats-officedocument.wordprocessingml.document";

/** A stand-in for the browser File a picker hands back. */
function pickedFile(name: string, type: string, size: number): File {
  return { name, type, size } as File;
}

class HeaderResponseXMLHttpRequest {
  static responseHeaders: Record<string, string> = {};
  upload = { onprogress: null };
  status = 200;
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;

  open(): void {}

  setRequestHeader(): void {}

  getResponseHeader(name: string): string | null {
    return HeaderResponseXMLHttpRequest.responseHeaders[name.toLowerCase()] || null;
  }

  send(): void {
    this.onload?.();
  }
}

async function directUploadIdentity(
  responseHeaders: Record<string, string>,
  uploadURL = "https://storage.test/put",
) {
  const originalXMLHttpRequest = globalThis.XMLHttpRequest;
  HeaderResponseXMLHttpRequest.responseHeaders = Object.fromEntries(
    Object.entries(responseHeaders).map(([name, value]) => [name.toLowerCase(), value]),
  );
  globalThis.XMLHttpRequest = HeaderResponseXMLHttpRequest as unknown as typeof XMLHttpRequest;
  try {
    return await uploadFileToStorage(uploadURL, pickedFile("source.mp4", "video/mp4", 12), "video/mp4");
  } finally {
    globalThis.XMLHttpRequest = originalXMLHttpRequest;
  }
}

test("direct uploads choose provider-compatible immutable object identity", async () => {
  const cases: Array<{
    name: string;
    headers: Record<string, string>;
    uploadURL?: string;
    expected: string;
  }> = [
    {
      name: "legacy S3 or MinIO prefers VersionId",
      headers: { "x-amz-version-id": "minio-version-1", etag: '"legacy-etag-1"' },
      expected: "minio-version-1",
    },
    {
      name: "R2 ETag-only",
      headers: { etag: '"r2-etag-1"' },
      uploadURL: "https://account.r2.cloudflarestorage.com/bucket/source",
      expected: 'etag:"r2-etag-1"',
    },
    {
      name: "R2 ignores unusable historical VersionId and prefers strong ETag",
      headers: { "x-amz-version-id": "7e5fc59e57d9b68912790e90f788f217", etag: '"r2-etag-2"' },
      uploadURL: "https://account.r2.cloudflarestorage.com/bucket/source",
      expected: 'etag:"r2-etag-2"',
    },
  ];

  for (const testCase of cases) {
    const result = await directUploadIdentity(testCase.headers, testCase.uploadURL);
    assert.equal(result.storageObjectVersion, testCase.expected, testCase.name);
  }
});

test("direct uploads reject missing or malformed provider identity", async () => {
  await assert.rejects(() => directUploadIdentity({}), /usable object identity/);
  assert.throws(() => storageObjectVersionFromUploadHeaders(null, "unquoted-etag"), /usable object identity/);
  assert.throws(() => storageObjectVersionFromUploadHeaders(null, '""'), /usable object identity/);
});

test("the accepted Lesson Resource types are exactly PDF and DOCX", () => {
  assert.deepEqual([...ACCEPTED_RESOURCE_CONTENT_TYPES], ["application/pdf", DOCX_TYPE]);
});

test("a picked resource resolves to an allowed type, whatever the browser reported", () => {
  // Reported correctly.
  assert.equal(resourceContentType(pickedFile("notes.pdf", "application/pdf", 10)), "application/pdf");
  assert.equal(resourceContentType(pickedFile("notes.docx", DOCX_TYPE, 10)), DOCX_TYPE);
  // Browsers routinely report a generic type, or nothing, for .docx. The
  // extension only selects which allowed type to declare; the server still
  // parses the stored bytes to decide what the file really is.
  assert.equal(resourceContentType(pickedFile("notes.docx", "application/zip", 10)), DOCX_TYPE);
  assert.equal(resourceContentType(pickedFile("notes.docx", "", 10)), DOCX_TYPE);
  assert.equal(resourceContentType(pickedFile("NOTES.PDF", "", 10)), "application/pdf");
  // Anything else has no allowed type to declare.
  assert.equal(resourceContentType(pickedFile("slides.pptx", "application/vnd.ms-powerpoint", 10)), null);
  assert.equal(resourceContentType(pickedFile("macro.docm", "application/vnd.ms-word.document.macroEnabled.12", 10)), null);
  assert.equal(resourceContentType(pickedFile("archive.zip", "application/zip", 10)), null);
});

test("resource selection reports a readable reason before any request is made", () => {
  const rejected = validateSelectedResource(pickedFile("archive.zip", "application/zip", 10), "en");
  assert.ok("error" in rejected && rejected.error.includes("PDF or DOCX"));

  const empty = validateSelectedResource(pickedFile("notes.pdf", "application/pdf", 0), "en");
  assert.ok("error" in empty && empty.error.includes("empty"));

  const tooBig = validateSelectedResource(
    pickedFile("notes.pdf", "application/pdf", 51 * 1024 * 1024),
    "en",
  );
  assert.ok("error" in tooBig && tooBig.error.includes("50 MB"));

  const accepted = validateSelectedResource(pickedFile("notes.docx", "", 1024), "en");
  assert.deepEqual(accepted, { contentType: DOCX_TYPE });

  const arabic = validateSelectedResource(pickedFile("archive.zip", "application/zip", 10), "ar");
  assert.ok("error" in arabic && arabic.error.includes("PDF"));
  assert.ok("error" in arabic && !arabic.error.includes("Select"));
});

test("a resource upload intent asks the media route for a RESOURCE of the declared type", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; method?: string; body: unknown }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({
      url: String(url),
      method: init?.method,
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    });
    return new Response(
      JSON.stringify({
        asset_version_id: ASSET_VERSION_ID,
        upload_url: "https://storage.test/put",
        storage_object_key: "quarantine/course/version/source",
        expires_at: "2026-08-14T12:00:00Z",
      }),
      { status: 201 },
    );
  };

  try {
    const ticket = await beginResourceUpload({
      courseID: COURSE_ID,
      lessonID: LESSON_ID,
      contentType: DOCX_TYPE,
      sizeBytes: 4096,
      locale: "en",
      csrf: "csrf-token",
    });
    assert.equal(ticket.asset_version_id, ASSET_VERSION_ID);
    assert.equal(requests.length, 1);
    assert.equal(requests[0].url, "/api/v1/media/uploads");
    assert.equal(requests[0].method, "POST");
    assert.deepEqual(requests[0].body, {
      course_id: COURSE_ID,
      lesson_id: LESSON_ID,
      kind: "RESOURCE",
      content_type: DOCX_TYPE,
      size_bytes: 4096,
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("the video intent still asks for VIDEO, so the resource path did not change it", async () => {
  const originalFetch = globalThis.fetch;
  let body: Record<string, unknown> = {};
  globalThis.fetch = async (_url, init) => {
    body = JSON.parse(String(init?.body));
    return new Response(
      JSON.stringify({
        asset_version_id: ASSET_VERSION_ID,
        upload_url: "https://storage.test/put",
        storage_object_key: "quarantine/course/version/source",
        expires_at: "2026-08-14T12:00:00Z",
      }),
      { status: 201 },
    );
  };
  try {
    await beginVideoUpload({
      courseID: COURSE_ID,
      lessonID: LESSON_ID,
      contentType: "video/mp4",
      sizeBytes: 8192,
      locale: "en",
      csrf: "csrf-token",
    });
    assert.equal(body.kind, "VIDEO");
    assert.equal(body.content_type, "video/mp4");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("a public preview upload is a separate revision-bound PREVIEW asset, never a Lesson upload", async () => {
  const originalFetch = globalThis.fetch;
  let body: Record<string, unknown> = {};
  globalThis.fetch = async (_url, init) => {
    body = JSON.parse(String(init?.body));
    return new Response(JSON.stringify({
      asset_version_id: ASSET_VERSION_ID,
      upload_url: "https://storage.test/put",
      storage_object_key: "quarantine/course/preview/source",
      expires_at: "2026-08-21T12:05:00Z",
    }), { status: 201 });
  };

  try {
    await beginPublicPreviewUpload({
      courseID: COURSE_ID,
      revisionID: REVISION_ID,
      contentType: "video/mp4",
      sizeBytes: 8192,
      locale: "ar",
      csrf: "csrf-token",
    });
    assert.deepEqual(body, {
      course_id: COURSE_ID,
      revision_id: REVISION_ID,
      kind: "PREVIEW",
      content_type: "video/mp4",
      size_bytes: 8192,
    });
    assert.equal("lesson_id" in body, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("completion sends the exact-object evidence the API verifies against", async () => {
  const originalFetch = globalThis.fetch;
  let sent: Record<string, unknown> = {};
  let url = "";
  globalThis.fetch = async (requestURL, init) => {
    url = String(requestURL);
    sent = JSON.parse(String(init?.body));
    return new Response(
      JSON.stringify({ asset_version_id: ASSET_VERSION_ID, state: "READY", duplicate: false }),
      { status: 200 },
    );
  };
  try {
    const result = await completeUpload({
      assetVersionID: ASSET_VERSION_ID,
      providerEventID: "event-1",
      storageObjectKey: "quarantine/course/version/source",
      storageObjectVersion: "object-v1",
      contentType: DOCX_TYPE,
      sizeBytes: 4096,
      sha256: "a".repeat(64),
      locale: "en",
      csrf: "csrf-token",
    });
    // A validated Lesson Resource is READY straight out of completion: no
    // separate scan or transcode step stands between the two.
    assert.equal(result.state, "READY");
    assert.equal(url, `/api/v1/media/uploads/${ASSET_VERSION_ID}/completions`);
    assert.deepEqual(sent, {
      provider_event_id: "event-1",
      storage_object_key: "quarantine/course/version/source",
      storage_object_version: "object-v1",
      content_type: DOCX_TYPE,
      size_bytes: 4096,
      sha256_hex: "a".repeat(64),
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("the API's own failure states stop the poll and read as plain sentences", () => {
  // PROCESS_FAILED is the API's spelling. Polling past it would leave a failed
  // upload spinning until the timeout instead of reporting what happened.
  for (const state of ["READY", "SCAN_FAILED", "SCAN_ERROR", "PROCESS_FAILED"]) {
    assert.equal(isTerminalState(state), true, state);
  }
  // VALIDATED is a real state a D-088 video passes through on the way to
  // PROCESSING, so the poll must continue past it.
  for (const state of ["UPLOADED", "QUARANTINED", "VALIDATED", "PROCESSING"]) {
    assert.equal(isTerminalState(state), false, state);
  }

  assert.match(describeAssetState("PROCESS_FAILED", "en"), /could not be prepared/);
  assert.match(describeAssetState("SCAN_FAILED", "en"), /rejected/);
  assert.match(describeAssetState("SCAN_ERROR", "en"), /could not be checked/);
  assert.ok(describeAssetState("PROCESS_FAILED", "ar").length > 0);
  assert.ok(!describeAssetState("PROCESS_FAILED", "ar").includes("could not"));
});

test("attaching a resource uses the existing lesson-files authoring route", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; method?: string; body: unknown }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({
      url: String(url),
      method: init?.method,
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    });
    return new Response(
      JSON.stringify({
        id: "file-1",
        kind: "RESOURCE",
        asset_version_id: ASSET_VERSION_ID,
        display_name_ar: "notes.docx",
        display_name_en: "notes.docx",
        position: 0,
      }),
      { status: 201 },
    );
  };

  try {
    const created = await addLessonFile({
      courseID: COURSE_ID,
      revisionID: REVISION_ID,
      lessonID: LESSON_ID,
      kind: "RESOURCE",
      assetVersionID: ASSET_VERSION_ID,
      displayNameAr: "notes.docx",
      displayNameEn: "notes.docx",
      locale: "en",
      csrf: "csrf-token",
    });
    assert.equal(created.id, "file-1");
    assert.equal(requests.length, 1);
    assert.equal(
      requests[0].url,
      `/api/v1/courses/${COURSE_ID}/revisions/${REVISION_ID}/lessons/${LESSON_ID}/files`,
    );
    assert.equal(requests[0].method, "PUT");
    assert.deepEqual(requests[0].body, {
      kind: "RESOURCE",
      asset_version_id: ASSET_VERSION_ID,
      display_name_ar: "notes.docx",
      display_name_en: "notes.docx",
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("removing a resource calls the existing delete route with the file identifier", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; method?: string }> = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), method: init?.method });
    return new Response(null, { status: 204 });
  };

  try {
    await deleteLessonFile({
      courseID: COURSE_ID,
      revisionID: REVISION_ID,
      lessonID: LESSON_ID,
      fileID: "file-1",
      locale: "en",
      csrf: "csrf-token",
    });
    assert.deepEqual(requests, [
      {
        url: `/api/v1/courses/${COURSE_ID}/revisions/${REVISION_ID}/lessons/${LESSON_ID}/files?file_id=file-1`,
        method: "DELETE",
      },
    ]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("an authoring command without a CSRF token never reaches the network", async () => {
  const originalFetch = globalThis.fetch;
  let called = false;
  globalThis.fetch = async () => {
    called = true;
    return new Response(null, { status: 204 });
  };
  try {
    await assert.rejects(
      addLessonFile({
        courseID: COURSE_ID,
        revisionID: REVISION_ID,
        lessonID: LESSON_ID,
        kind: "RESOURCE",
        assetVersionID: ASSET_VERSION_ID,
        displayNameAr: "notes.docx",
        displayNameEn: "notes.docx",
        locale: "en",
        csrf: "",
      }),
      /CSRF/,
    );
    assert.equal(called, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
