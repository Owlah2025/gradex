import assert from "node:assert/strict";
import test from "node:test";
import { PartTransferError, uploadFilePart, uploadResumable, type MultipartUploadTicket, type ResumableInput } from "./media-multipart";

/*
  Production, 2026-10-08: an Instructor on a ~200 KB/s uplink saw "Part upload timed out". Each
  8 MiB part PUT had a 120 s whole-request deadline; with three parts sharing the link every part
  needed ~210 KB/s, so parts that were still moving were killed and restarted from byte zero until
  the retries ran out. A part is now abandoned only when no byte moves for the stall window.
*/

type Fake = {
  timeout: number;
  aborted: boolean;
  progress: (loaded: number) => void;
  bodySent: () => void;
  respond: (status?: number) => void;
};

function fakeXHR() {
  const saved = globalThis.XMLHttpRequest;
  const instances: Fake[] = [];
  class XHR {
    status = 200;
    timeout = -1;
    withCredentials = false;
    upload: { onprogress: ((event: ProgressEvent) => void) | null; onload: (() => void) | null } = { onprogress: null, onload: null };
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;
    onabort: (() => void) | null = null;
    ontimeout: (() => void) | null = null;
    private record!: Fake;
    open() {}
    setRequestHeader() {}
    getResponseHeader() {
      return '"part-etag"';
    }
    abort() {
      this.record.aborted = true;
      this.onabort?.();
    }
    send(blob: Blob) {
      const xhr = this;
      this.record = {
        get timeout() {
          return xhr.timeout;
        },
        aborted: false,
        progress: (loaded) => xhr.upload.onprogress?.({ loaded, total: blob.size } as ProgressEvent),
        bodySent: () => xhr.upload.onload?.(),
        respond: (status = 200) => {
          xhr.status = status;
          xhr.onload?.();
        },
      };
      instances.push(this.record);
    }
  }
  globalThis.XMLHttpRequest = XHR as unknown as typeof XMLHttpRequest;
  return { instances, restore: () => (globalThis.XMLHttpRequest = saved) };
}

const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
const chunk = new Blob([new Uint8Array(100)]);

test("a slow part that keeps moving is never cut off for taking long", async () => {
  const x = fakeXHR();
  try {
    const loaded: number[] = [];
    const put = uploadFilePart("https://storage.test/p/1", chunk, "video/mp4", undefined, (n) => loaded.push(n), 80);
    const request = x.instances[0];
    assert.equal(request.timeout, 0, "no whole-request deadline");
    // 10 steps of 30 ms: 300 ms in total, almost four stall windows, but never 80 ms without a byte.
    for (let step = 1; step <= 10; step++) {
      await wait(30);
      request.progress(step * 10);
    }
    request.bodySent();
    await wait(40); // the storage response arrives within the window after the body was sent
    request.respond();
    assert.equal(await put, '"part-etag"');
    assert.equal(request.aborted, false);
    assert.equal(loaded.at(-1), 100);
  } finally {
    x.restore();
  }
});

test("a part with no bytes moving for the stall window is abandoned as a connection problem", async () => {
  const x = fakeXHR();
  try {
    const put = uploadFilePart("https://storage.test/p/1", chunk, "video/mp4", undefined, undefined, 60);
    const outcome = put.then(() => null, (error: unknown) => error);
    x.instances[0].progress(40);
    await wait(120);
    const error = await outcome;
    assert.ok(error instanceof PartTransferError, String(error));
    assert.equal(error.reason, "stalled");
    assert.equal(x.instances[0].aborted, true);
  } finally {
    x.restore();
  }
});

test("a storage response that never comes after the body was sent is a stall too", async () => {
  const x = fakeXHR();
  try {
    const outcome = uploadFilePart("https://storage.test/p/1", chunk, "video/mp4", undefined, undefined, 60).then(
      () => null,
      (error: unknown) => error,
    );
    x.instances[0].progress(100);
    x.instances[0].bodySent();
    await wait(120);
    const error = await outcome;
    assert.ok(error instanceof PartTransferError && error.reason === "stalled", String(error));
  } finally {
    x.restore();
  }
});

test("Pause and storage errors keep their own meaning", async () => {
  const x = fakeXHR();
  try {
    const controller = new AbortController();
    const paused = uploadFilePart("https://storage.test/p/1", chunk, "video/mp4", controller.signal, undefined, 60).then(
      () => null,
      (error: unknown) => error,
    );
    controller.abort();
    const pausedError = await paused;
    assert.ok(pausedError instanceof DOMException && pausedError.name === "AbortError", "a pause is not a stall");

    const busy = uploadFilePart("https://storage.test/p/2", chunk, "video/mp4", undefined, undefined, 60).then(
      () => null,
      (error: unknown) => error,
    );
    x.instances[1].respond(503);
    const busyError = await busy;
    assert.ok(busyError instanceof PartTransferError && busyError.reason === "server");

    const refused = uploadFilePart("https://storage.test/p/3", chunk, "video/mp4", undefined, undefined, 60).then(
      () => null,
      (error: unknown) => error,
    );
    x.instances[2].respond(403);
    const refusedError = await refused;
    assert.ok(refusedError instanceof Error && !(refusedError instanceof PartTransferError), "a 403 is not a connection problem");
  } finally {
    x.restore();
  }
});

test("a stalled part is retried automatically and the upload completes", async () => {
  const saved = { fetch: globalThis.fetch, storage: globalThis.localStorage };
  const x = fakeXHR();
  const data: Record<string, string> = {};
  const storage = {
    getItem: (key: string) => (key in data ? data[key] : null),
    setItem: (key: string, value: string) => {
      data[key] = value;
      Object.defineProperty(storage, key, { value, enumerable: true, configurable: true, writable: true });
    },
    removeItem: (key: string) => {
      delete data[key];
      delete (storage as unknown as Record<string, unknown>)[key];
    },
  };
  globalThis.localStorage = storage as unknown as Storage;
  const ticket: MultipartUploadTicket = {
    asset_version_id: "asset-1",
    upload_id: "u",
    storage_object_key: "k",
    expires_at: new Date(Date.now() + 60000).toISOString(),
    part_size_bytes: 100,
    status: "ACTIVE",
    parts: [],
  };
  globalThis.fetch = async (url, init) => {
    const path = String(url);
    if (path.endsWith("/uploads/multipart")) return Response.json(ticket, { status: 201 });
    if (path.includes("/parts/")) return Response.json({ url: "https://storage.test/asset-1/1" });
    if (path.endsWith("/completions"))
      return Response.json({ asset_version_id: "asset-1", state: "QUARANTINED", duplicate: false, storage_object_version: 'etag:"w"' });
    throw new Error(`unexpected ${init?.method} ${path}`);
  };
  try {
    const input: ResumableInput = {
      courseID: "c",
      revisionID: "r",
      lessonID: "l",
      kind: "VIDEO",
      storageKeyId: "video-stall",
      locale: "en",
      csrf: "csrf",
      partStallTimeoutMs: 40,
    };
    const retrying: boolean[] = [];
    const run = uploadResumable(new File([new Uint8Array(100)], "a.mp4", { type: "video/mp4" }), input, (_f, p) => retrying.push(p.retrying));
    for (let i = 0; i < 100 && x.instances.length < 1; i++) await wait(5);
    x.instances[0].progress(30); // then nothing: the connection stalls
    for (let i = 0; i < 400 && x.instances.length < 2; i++) await wait(5); // stall window + retry backoff
    assert.equal(x.instances.length, 2, "the stalled part was retried without the Instructor pressing anything");
    assert.ok(retrying.includes(true), "the retrying state was shown");
    x.instances[1].progress(100);
    x.instances[1].respond();
    const result = await run;
    assert.equal(result.state, "QUARANTINED");
  } finally {
    globalThis.fetch = saved.fetch;
    globalThis.localStorage = saved.storage;
    x.restore();
  }
});
