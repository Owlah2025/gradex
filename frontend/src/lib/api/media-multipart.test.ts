import assert from "node:assert/strict";
import test from "node:test";
import { createHash } from "node:crypto";
import {
  uploadResumable,
  cancelResumableUpload,
  acknowledgeResumableUpload,
  uploadFilePart,
  ResumeFileMismatchError,
  type ResumableInput,
  type MultipartUploadTicket,
} from "./media-multipart";
import { sha256Hex } from "./media-upload";

const input: ResumableInput = {
  courseID: "course-a",
  revisionID: "revision-a",
  lessonID: "lesson-a",
  kind: "VIDEO",
  storageKeyId: "video-a",
  locale: "en",
  csrf: "csrf",
};
const picked = () =>
  new File([new Uint8Array(20).fill(7)], "lecture.mp4", { type: "video/mp4" });
type Part = { part_number: number; etag: string; size_bytes: number };

function networkFixture() {
  const saved = {
    fetch: globalThis.fetch,
    xhr: globalThis.XMLHttpRequest,
    storage: globalThis.localStorage,
  };
  const data: Record<string, string> = {};
  const storage = {
    ...data,
    getItem: (key: string) => data[key] || null,
    setItem: (key: string, value: string) => {
      data[key] = value;
      Object.defineProperty(storage, key, {
        value,
        enumerable: true,
        configurable: true,
        writable: true,
      });
    },
    removeItem: (key: string) => {
      delete data[key];
      delete (storage as unknown as Record<string, unknown>)[key];
    },
  };
  globalThis.localStorage = storage as unknown as Storage;
  let ticket: MultipartUploadTicket;
  const sessions = new Map<string, MultipartUploadTicket>();
  const uploaded = new Map<number, Part>();
  const attempts = new Map<number, number>();
  let begins = 0,
    completions = 0,
    aborts = 0;
  let failingPart = 0,
    missingETag = false,
    loseCompletion = false,
    failCancel = false,
    loseCreation = false,
    pendingVerification = false;
  class XHR {
    status = 200;
    timeout = 0;
    withCredentials = false;
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;
    onabort: (() => void) | null = null;
    ontimeout: (() => void) | null = null;
    number = 0;
    open(method: string, url: string) {
      assert.equal(method, "PUT");
      this.number = Number(url.split("/").at(-1));
    }
    setRequestHeader(name: string, value: string) {
      assert.equal(name, "Content-Type");
      assert.equal(value, "video/mp4");
    }
    getResponseHeader(name: string) {
      assert.equal(name, "ETag");
      return missingETag ? null : `"part-${this.number}"`;
    }
    abort() {
      this.onabort?.();
    }
    send(blob: Blob) {
      attempts.set(this.number, (attempts.get(this.number) || 0) + 1);
      if (this.number === failingPart) {
        queueMicrotask(() => this.onerror?.());
        return;
      }
      uploaded.set(this.number, {
        part_number: this.number,
        etag: `"part-${this.number}"`,
        size_bytes: blob.size,
      });
      queueMicrotask(() => this.onload?.());
    }
  }
  globalThis.XMLHttpRequest = XHR as unknown as typeof XMLHttpRequest;
  globalThis.fetch = async (url, init) => {
    const path = String(url);
    assert.equal(init?.credentials, "same-origin");
    if (path.endsWith("/uploads/multipart")) {
      const body = JSON.parse(init?.body as string);
      assert.match(body.client_request_id, /^[0-9a-f-]{36}$/);
      if (sessions.has(body.client_request_id)) {
        ticket = sessions.get(body.client_request_id)!;
      } else {
        begins++;
        ticket = {
          asset_version_id: `asset-${begins}`,
          upload_id: `provider-${begins}`,
          storage_object_key: `quarantine/course-a/asset-${begins}/source`,
          expires_at: new Date(Date.now() + 60000).toISOString(),
          part_size_bytes: 8,
          status: "ACTIVE",
          parts: [],
        };
        sessions.set(body.client_request_id, ticket);
      }
      if (loseCreation) {
        loseCreation = false;
        throw new TypeError("Upload created but start response lost");
      }
      return Response.json(ticket, { status: 201 });
    }
    if (path.endsWith("/verification")) {
      return Response.json({
        asset_version_id: ticket.asset_version_id,
        state: "QUARANTINED",
        storage_object_version: 'etag:"assembled"',
      });
    }
    if (init?.method === "GET") {
      return Response.json({ ...ticket, parts: Array.from(uploaded.values()) });
    }
    if (init?.method === "DELETE") {
      aborts++;
      if (failCancel)
        return Response.json(
          {
            status: 503,
            code: "DEPENDENCY_UNAVAILABLE",
            title: "Unavailable",
            type: "about:blank",
          },
          { status: 503 },
        );
      return new Response(null, { status: 204 });
    }
    if (path.includes("/parts/"))
      return Response.json({
        url: `https://storage.test/parts/${path.split("/").at(-1)}`,
      });
    assert.ok(path.endsWith("/multipart/completions"));
    assert.equal(init?.method, "POST");
    const body = JSON.parse(init?.body as string);
    assert.deepEqual(
      body.parts,
      Array.from(uploaded.values())
        .sort((a, b) => a.part_number - b.part_number)
        .map(({ part_number, etag }) => ({ part_number, etag })),
    );
    assert.equal(
      body.provider_event_id,
      `multipart:${ticket.asset_version_id}`,
    );
    completions++;
    ticket.status = "ASSEMBLED";
    if (loseCompletion) {
      loseCompletion = false;
      throw new TypeError("Server succeeded but response disconnected");
    }
    return Response.json({
      asset_version_id: ticket.asset_version_id,
      state: pendingVerification ? "UPLOADED" : "QUARANTINED",
      duplicate: completions > 1,
      storage_object_version: 'etag:"assembled"',
    });
  };
  return {
    attempts,
    data,
    uploaded,
    counts: () => ({ begins, completions, aborts }),
    failPart: (n: number) => {
      failingPart = n;
    },
    missingETag: () => {
      missingETag = true;
    },
    loseCompletion: () => {
      loseCompletion = true;
    },
    loseCreation: () => {
      loseCreation = true;
    },
    pendingVerification: () => {
      pendingVerification = true;
    },
    failCancel: (fail: boolean) => {
      failCancel = fail;
    },
    restore: () => {
      globalThis.fetch = saved.fetch;
      globalThis.XMLHttpRequest = saved.xhr;
      globalThis.localStorage = saved.storage;
    },
  };
}

test("streaming fingerprint equals SHA-256 across chunk boundaries", async () => {
  const file = new File(
    [new Uint8Array(1024 * 1024 + 3).fill(29)],
    "large.mp4",
  );
  assert.equal(
    await sha256Hex(file),
    createHash("sha256")
      .update(new Uint8Array(await file.arrayBuffer()))
      .digest("hex"),
  );
});

test("start response loss persists its request identity across a resumed invocation", async () => {
  const f = networkFixture();
  try {
    f.loseCreation();
    await assert.rejects(
      uploadResumable(picked(), input),
      /start response lost/,
    );
    const checkpoint = JSON.parse(Object.values(f.data)[0]);
    assert.equal(checkpoint.ticket, null);
    assert.match(checkpoint.requestID, /^[0-9a-f-]{36}$/);
    const result = await uploadResumable(picked(), input);
    assert.equal(result.asset_version_id, "asset-1");
    assert.equal(f.counts().begins, 1);
  } finally {
    f.restore();
  }
});

test("assembled bytes remain pending until the worker reports verification", async () => {
  const f = networkFixture();
  try {
    f.pendingVerification();
    let verifying = false;
    const result = await uploadResumable(picked(), {
      ...input,
      onVerifying: () => {
        verifying = true;
      },
    });
    assert.equal(verifying, true);
    assert.equal(result.state, "QUARANTINED");
    assert.equal(f.counts().completions, 1);
    assert.equal(Object.keys(f.data).length, 1);
  } finally {
    f.restore();
  }
});

test("interrupted transfer reconciles server parts and retries only the missing part", async () => {
  const f = networkFixture();
  try {
    f.failPart(2);
    await assert.rejects(uploadResumable(picked(), input), /network error/);
    assert.equal(f.counts().completions, 0);
    assert.equal(f.attempts.get(1), 1);
    assert.equal(f.attempts.get(3), 1);
    // Simulate provider accepting part 2 but the browser losing its response before refresh.
    f.uploaded.set(2, { part_number: 2, etag: '"part-2"', size_bytes: 8 });
    f.failPart(0);
    const result = await uploadResumable(picked(), input);
    assert.equal(f.counts().begins, 1);
    assert.equal(
      f.attempts.get(2),
      3,
      "an accepted remote part must not be uploaded again",
    );
    assert.equal(f.attempts.get(1), 1);
    assert.equal(result.storage_object_version, 'etag:"assembled"');
    assert.equal(
      Object.keys(f.data).length,
      1,
      "selection has not been acknowledged",
    );
    acknowledgeResumableUpload(input, result.sha256_hex);
    assert.equal(Object.keys(f.data).length, 0);
  } finally {
    f.restore();
  }
});

test("completion response loss retains the same session and immutable completion evidence", async () => {
  const f = networkFixture();
  try {
    f.loseCompletion();
    await assert.rejects(
      uploadResumable(picked(), input),
      /response disconnected/,
    );
    const result = await uploadResumable(picked(), input);
    assert.equal(result.duplicate, true);
    assert.deepEqual(f.counts(), { begins: 1, completions: 2, aborts: 0 });
    assert.deepEqual(Array.from(f.attempts.values()), [1, 1, 1]);
  } finally {
    f.restore();
  }
});

test("same name and size with changed bytes cannot reuse another file's upload", async () => {
  const f = networkFixture();
  try {
    const first = await uploadResumable(picked(), input);
    const different = new File([new Uint8Array(20).fill(9)], "lecture.mp4", {
      type: "video/mp4",
    });
    // While the first upload's checkpoint is unacknowledged, a different file is refused rather
    // than silently becoming a second, parallel upload for the same control.
    await assert.rejects(
      uploadResumable(different, input),
      (error: unknown) => error instanceof ResumeFileMismatchError,
    );
    assert.equal(f.counts().begins, 1);
    acknowledgeResumableUpload(input, first.sha256_hex);
    await uploadResumable(different, input);
    assert.equal(f.counts().begins, 2);
  } finally {
    f.restore();
  }
});

test("missing ETag refuses part success", async () => {
  const f = networkFixture();
  try {
    f.missingETag();
    await assert.rejects(
      uploadFilePart("https://storage.test/parts/1", picked(), "video/mp4"),
      /valid part ETag/,
    );
    assert.equal(f.counts().completions, 0);
  } finally {
    f.restore();
  }
});

test("failed cancellation retains recovery state and successful retry removes it", async () => {
  const f = networkFixture();
  try {
    f.failPart(2);
    await assert.rejects(uploadResumable(picked(), input));
    f.failCancel(true);
    await assert.rejects(cancelResumableUpload(input));
    assert.equal(Object.keys(f.data).length, 1);
    f.failCancel(false);
    await cancelResumableUpload(input);
    assert.equal(Object.keys(f.data).length, 0);
    assert.equal(f.counts().aborts, 2);
  } finally {
    f.restore();
  }
});
