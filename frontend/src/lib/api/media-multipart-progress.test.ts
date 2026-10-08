import assert from "node:assert/strict";
import test from "node:test";
import {
  ResumeFileMismatchError,
  cancelResumableUpload,
  readSavedUpload,
  refreshSavedUpload,
  summarizeCheckpoint,
  uploadResumable,
  type MultipartUploadTicket,
  type ResumableInput,
  type SavedUploadSummary,
} from "./media-multipart";
import type { UploadProgress } from "./upload-progress";

/*
  A storage double whose part PUTs are driven by the test: each send() parks the request until the
  test reports byte progress, completes it, or fails it. That is what lets these tests interleave
  three parallel parts deterministically, which the real browser does not promise.
*/

const PART = 10;

type Inflight = {
  number: number;
  size: number;
  progress: (loaded: number) => void;
  load: () => void;
  fail: () => void;
  aborted: boolean;
};

function harness(options: { serverParts?: number[] } = {}) {
  const saved = {
    fetch: globalThis.fetch,
    xhr: globalThis.XMLHttpRequest,
    storage: globalThis.localStorage,
  };
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

  const sessions = new Map<string, MultipartUploadTicket>();
  const serverParts = new Map<string, Map<number, number>>();
  const puts = new Map<number, number>();
  const inflight: Inflight[] = [];
  const requests: string[] = [];
  const deletes: string[] = [];
  let begins = 0;

  class XHR {
    status = 200;
    timeout = 0;
    withCredentials = false;
    upload: { onprogress: ((event: ProgressEvent) => void) | null } = { onprogress: null };
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;
    onabort: (() => void) | null = null;
    ontimeout: (() => void) | null = null;
    private number = 0;
    private asset = "";
    private entry: Inflight | null = null;
    open(_method: string, url: string) {
      const parts = url.split("/");
      this.number = Number(parts.at(-1));
      this.asset = parts.at(-2)!;
    }
    setRequestHeader() {}
    getResponseHeader() {
      return `"etag-${this.number}"`;
    }
    abort() {
      if (this.entry) {
        this.entry.aborted = true;
        const index = inflight.indexOf(this.entry);
        if (index >= 0) inflight.splice(index, 1);
      }
      this.onabort?.();
    }
    send(blob: Blob) {
      puts.set(this.number, (puts.get(this.number) || 0) + 1);
      const entry: Inflight = {
        number: this.number,
        size: blob.size,
        aborted: false,
        progress: (loaded) =>
          this.upload.onprogress?.({ loaded, total: blob.size, lengthComputable: true } as ProgressEvent),
        load: () => {
          inflight.splice(inflight.indexOf(entry), 1);
          serverParts.get(this.asset)!.set(this.number, blob.size);
          this.onload?.();
        },
        fail: () => {
          inflight.splice(inflight.indexOf(entry), 1);
          this.onerror?.();
        },
      };
      this.entry = entry;
      inflight.push(entry);
    }
  }
  globalThis.XMLHttpRequest = XHR as unknown as typeof XMLHttpRequest;

  const ticketFor = (asset: string): MultipartUploadTicket => {
    const ticket = sessions.get(asset)!;
    return {
      ...ticket,
      parts: Array.from(serverParts.get(asset)!.entries())
        .sort((a, b) => a[0] - b[0])
        .map(([part_number, size_bytes]) => ({ part_number, etag: `"etag-${part_number}"`, size_bytes })),
    };
  };

  globalThis.fetch = async (url, init) => {
    const path = String(url);
    const method = init?.method || "GET";
    requests.push(`${method} ${path}`);
    if (path.endsWith("/uploads/multipart") && method === "POST") {
      begins++;
      const asset = `asset-${begins}`;
      sessions.set(asset, {
        asset_version_id: asset,
        upload_id: `provider-${begins}`,
        storage_object_key: `quarantine/course/${asset}/source`,
        expires_at: new Date(Date.now() + 60000).toISOString(),
        part_size_bytes: PART,
        status: "ACTIVE",
        parts: [],
      });
      serverParts.set(asset, new Map((options.serverParts || []).map((n) => [n, PART])));
      return Response.json(ticketFor(asset), { status: 201 });
    }
    const asset = path.match(/uploads\/(asset-\d+)/)?.[1];
    if (path.includes("/parts/"))
      return Response.json({ url: `https://storage.test/${asset}/${path.split("/").at(-1)}` });
    if (path.endsWith("/completions")) {
      sessions.get(asset!)!.status = "ASSEMBLED";
      return Response.json({ asset_version_id: asset, state: "QUARANTINED", duplicate: false, storage_object_version: 'etag:"whole"' });
    }
    if (method === "DELETE") {
      deletes.push(asset!);
      sessions.get(asset!)!.status = "ABORTED";
      return new Response(null, { status: 204 });
    }
    if (method === "GET" && asset) return Response.json(ticketFor(asset));
    throw new Error(`unexpected request ${method} ${path}`);
  };

  const settle = async () => {
    for (let i = 0; i < 20; i++) await new Promise((resolve) => setImmediate(resolve));
  };
  // Retries wait out a real backoff (500 ms), so this polls on wall-clock time.
  const waitInflight = async (count: number) => {
    const deadline = Date.now() + 5000;
    while (inflight.length < count && Date.now() < deadline) {
      await settle();
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
    assert.equal(inflight.length, count, `expected ${count} part request(s) in flight`);
  };
  const part = (number: number) => {
    const found = inflight.find((entry) => entry.number === number);
    assert.ok(found, `part ${number} is not in flight`);
    return found;
  };
  return {
    data,
    puts,
    inflight,
    requests,
    deletes,
    serverParts,
    sessions,
    begins: () => begins,
    settle,
    waitInflight,
    part,
    restore: () => {
      globalThis.fetch = saved.fetch;
      globalThis.XMLHttpRequest = saved.xhr;
      globalThis.localStorage = saved.storage;
    },
  };
}

const input = (storageKeyId = "video-a"): ResumableInput => ({
  courseID: "course-a",
  revisionID: "revision-a",
  lessonID: "lesson-a",
  kind: "VIDEO",
  storageKeyId,
  locale: "en",
  csrf: "csrf",
});
const file = (size: number, fill = 7, name = "lecture.mp4") =>
  new File([new Uint8Array(size).fill(fill)], name, { type: "video/mp4" });

function recorder() {
  const events: Array<{ fraction: number; progress: UploadProgress }> = [];
  return {
    events,
    listener: (fraction: number, progress: UploadProgress) => events.push({ fraction, progress }),
    last: () => events.at(-1)!,
  };
}

function assertInvariants(events: Array<{ fraction: number; progress: UploadProgress }>) {
  let previous = -1;
  for (const { fraction, progress } of events) {
    assert.ok(fraction >= 0 && fraction <= 1, `fraction ${fraction} out of range`);
    assert.ok(progress.transferredBytes >= 0 && progress.transferredBytes <= progress.totalBytes);
    assert.ok(progress.reportedBytes >= progress.transferredBytes);
    assert.ok(progress.reportedBytes >= previous, "reported progress moved backwards");
    previous = progress.reportedBytes;
  }
}

test("1. a single part reports live byte progress before it completes and ends at exactly 100%", async () => {
  const h = harness();
  try {
    const r = recorder();
    const task = uploadResumable(file(PART), input(), r.listener);
    await h.waitInflight(1);
    h.part(1).progress(3);
    h.part(1).progress(7);
    assert.deepEqual(
      r.events.map((e) => e.progress.transferredBytes),
      [0, 3, 7],
      "intermediate byte counts are reported while the PUT is still running",
    );
    h.part(1).progress(10);
    h.part(1).load();
    await task;
    assert.equal(r.last().fraction, 1);
    assert.equal(r.last().progress.reportedBytes, PART);
    assertInvariants(r.events);
  } finally {
    h.restore();
  }
});

test("2-3. parallel parts aggregate in-flight bytes once, and completion does not jump twice", async () => {
  const h = harness();
  try {
    const r = recorder();
    const task = uploadResumable(file(30), input(), r.listener);
    await h.waitInflight(3);
    h.part(1).progress(10);
    h.part(1).load();
    await h.settle();
    h.part(2).progress(6);
    h.part(3).progress(4);
    assert.equal(r.last().progress.transferredBytes, 20, "10 completed + 6 + 4 in flight");
    assert.equal(r.last().progress.completedBytes, 10);
    h.part(2).progress(10);
    const beforeCompletion = r.last().progress.transferredBytes;
    h.part(2).load();
    await h.settle();
    assert.equal(r.last().progress.transferredBytes, beforeCompletion, "active → completed is not counted twice");
    assert.equal(r.last().progress.completedBytes, 20);
    h.part(3).progress(10);
    h.part(3).load();
    await task;
    assert.equal(r.last().fraction, 1);
    assertInvariants(r.events);
  } finally {
    h.restore();
  }
});

test("4. a failed attempt's partial bytes are removed, the retry is announced, and the total stays exact", async () => {
  const h = harness();
  try {
    const r = recorder();
    const task = uploadResumable(file(20), input(), r.listener);
    await h.waitInflight(2);
    h.part(1).progress(10);
    h.part(1).load();
    await h.settle();
    h.part(2).progress(6);
    assert.equal(r.last().progress.transferredBytes, 16);
    h.part(2).fail();
    await h.settle();
    const failed = r.last().progress;
    assert.equal(failed.transferredBytes, 10, "the failed attempt's 6 bytes no longer count");
    assert.equal(failed.reportedBytes, 16, "the bar holds rather than visibly regressing");
    assert.equal(failed.retrying, true, "the UI can say it is retrying automatically");
    await h.waitInflight(1); // the automatic retry, after its backoff
    h.part(2).progress(2);
    assert.equal(r.last().progress.transferredBytes, 12);
    assert.equal(r.last().progress.retrying, false);
    h.part(2).progress(10);
    h.part(2).load();
    await task;
    assert.equal(h.puts.get(2), 2);
    assert.equal(r.last().progress.reportedBytes, 20);
    assertInvariants(r.events);
  } finally {
    h.restore();
  }
});

test("5-7. abort stops progress, keeps completed parts, survives reload, and resumes without re-sending them", async () => {
  const h = harness();
  try {
    const r = recorder();
    const controller = new AbortController();
    const task = uploadResumable(file(30), { ...input(), signal: controller.signal }, r.listener);
    await h.waitInflight(3);
    h.part(1).progress(10);
    h.part(1).load();
    await h.settle();
    const stalePart = h.part(2);
    stalePart.progress(5);
    controller.abort(new DOMException("Upload paused", "AbortError"));
    await assert.rejects(task, (error: unknown) => error instanceof DOMException && error.name === "AbortError");
    const eventsAtAbort = r.events.length;
    stalePart.progress(9); // a late event from an aborted request
    await h.settle();
    assert.equal(r.events.length, eventsAtAbort, "nothing is reported after the abort");

    // 6. The page reloads: only the checkpoint is left.
    const local = readSavedUpload(input());
    assert.ok(local);
    assert.equal(local.fileName, "lecture.mp4");
    assert.equal(local.sizeBytes, 30);
    assert.equal(local.completedBytes, 10);
    assert.equal(local.percent, 33, "the saved upload is shown at 33%, not 0%");
    assert.equal(local.status, "PAUSED");
    const reconciled = await refreshSavedUpload(input());
    assert.equal(reconciled?.source, "server");
    assert.equal(reconciled?.percent, 33);
    for (const value of Object.values(h.data)) {
      assert.doesNotMatch(value, /storage\.test/, "no signed part URL is ever persisted");
    }

    // 7. The same file is reselected and continues from the server's parts.
    const resumed = recorder();
    let resuming: SavedUploadSummary | null = null;
    const second = uploadResumable(
      file(30),
      { ...input(), onResuming: (summary) => (resuming = summary) },
      resumed.listener,
    );
    await h.waitInflight(2);
    assert.equal((resuming as SavedUploadSummary | null)?.percent, 33);
    assert.equal(resumed.events[0].progress.reportedBytes, 10, "resumed progress starts at 33%, not 0%");
    assert.deepEqual(h.inflight.map((entry) => entry.number).sort(), [2, 3]);
    for (const entry of [...h.inflight]) {
      entry.progress(10);
      entry.load();
    }
    await second;
    assert.equal(h.puts.get(1), 1, "a part the server holds is not PUT again");
    assert.equal(h.begins(), 1);
    assertInvariants(resumed.events);
  } finally {
    h.restore();
  }
});

test("8. a different file can never be resumed into the saved upload", async () => {
  const h = harness();
  try {
    const controller = new AbortController();
    const task = uploadResumable(file(30), { ...input(), signal: controller.signal });
    await h.waitInflight(3);
    controller.abort(new DOMException("Upload paused", "AbortError"));
    await assert.rejects(task);
    const before = h.requests.length;

    await assert.rejects(
      uploadResumable(file(31, 7, "other.mp4"), input()),
      (error: unknown) => error instanceof ResumeFileMismatchError && error.savedFileName === "lecture.mp4",
    );
    assert.equal(h.requests.length, before, "a different size is refused without any request");

    await assert.rejects(
      uploadResumable(file(30, 9, "lecture.mp4"), input()),
      (error: unknown) => error instanceof ResumeFileMismatchError,
      "same name and size with different bytes is refused by the SHA-256 fingerprint",
    );
    assert.equal(h.requests.length, before);
    assert.equal(h.begins(), 1, "no second upload was started");
    assert.equal(Object.keys(h.data).length, 1);
  } finally {
    h.restore();
  }
});

test("9. when the browser and server disagree, the server's part list wins", async () => {
  const h = harness();
  try {
    const controller = new AbortController();
    const task = uploadResumable(file(30), { ...input(), signal: controller.signal });
    await h.waitInflight(3);
    h.part(1).progress(10);
    h.part(1).load();
    await h.settle();
    controller.abort(new DOMException("Upload paused", "AbortError"));
    await assert.rejects(task);
    // A stale or tampered checkpoint claims every part is done.
    const key = Object.keys(h.data)[0];
    const checkpoint = JSON.parse(h.data[key]);
    checkpoint.parts = [1, 2, 3].map((n) => ({ part_number: n, etag: `"etag-${n}"`, size_bytes: 10 }));
    (globalThis.localStorage as Storage).setItem(key, JSON.stringify(checkpoint));
    assert.equal(readSavedUpload(input())?.percent, 100, "the local hint alone would say 100%");
    assert.equal((await refreshSavedUpload(input()))?.percent, 33, "the server says 33%");

    const second = uploadResumable(file(30), input());
    await h.waitInflight(2);
    assert.deepEqual(h.inflight.map((entry) => entry.number).sort(), [2, 3]);
    for (const entry of [...h.inflight]) {
      entry.progress(10);
      entry.load();
    }
    await second;
    assert.equal(h.puts.get(1), 1);
  } finally {
    h.restore();
  }
});

test("10. cancel aborts only this control's server session and clears only its checkpoint", async () => {
  const h = harness();
  try {
    for (const id of ["video-a", "video-b"]) {
      const controller = new AbortController();
      const task = uploadResumable(file(30, id === "video-a" ? 1 : 2), { ...input(id), signal: controller.signal });
      await h.waitInflight(3);
      controller.abort(new DOMException("Upload paused", "AbortError"));
      await assert.rejects(task);
    }
    assert.equal(Object.keys(h.data).length, 2);
    await cancelResumableUpload(input("video-a"));
    assert.deepEqual(h.deletes, ["asset-1"]);
    assert.equal(readSavedUpload(input("video-a")), null);
    assert.equal(readSavedUpload(input("video-b"))?.assetVersionID, "asset-2", "the other lesson's upload is untouched");
    assert.equal(h.sessions.get("asset-2")?.status, "ACTIVE");
  } finally {
    h.restore();
  }
});

test("11. three parallel workers in a shuffled order keep the aggregate exact", async () => {
  const h = harness();
  try {
    let seed = 20261008;
    const random = () => {
      seed = (seed * 1103515245 + 12345) % 2147483648;
      return seed / 2147483648;
    };
    const parts = 10;
    const r = recorder();
    const task = uploadResumable(file(parts * PART - 3), input(), r.listener);
    const sent = new Map<number, number>();
    const completed = new Set<number>();
    let guard = 0;
    while (completed.size < parts && guard++ < 10000) {
      await h.settle();
      if (!h.inflight.length) continue;
      assert.ok(h.inflight.length <= 3, "never more than three parts in flight");
      const entry = h.inflight[Math.floor(random() * h.inflight.length)];
      const current = sent.get(entry.number) || 0;
      if (current >= entry.size || random() < 0.2) {
        entry.progress(entry.size);
        entry.load();
        completed.add(entry.number);
      } else {
        const next = Math.min(entry.size, current + 1 + Math.floor(random() * 4));
        sent.set(entry.number, next);
        entry.progress(next);
        // Independent accounting of what should be shown right now.
        let expected = 0;
        for (const n of completed) expected += Math.min(PART, parts * PART - 3 - (n - 1) * PART);
        for (const live of h.inflight) if (!completed.has(live.number)) expected += sent.get(live.number) || 0;
        assert.equal(r.last().progress.transferredBytes, expected);
      }
    }
    await task;
    assert.equal(r.last().fraction, 1);
    assert.equal(r.last().progress.reportedBytes, parts * PART - 3);
    assertInvariants(r.events);
  } finally {
    h.restore();
  }
});

test("summaries cover legacy checkpoints, finished uploads and cancelled sessions", () => {
  const ticket: MultipartUploadTicket = {
    upload_id: "u",
    storage_object_key: "k",
    expires_at: new Date().toISOString(),
    asset_version_id: "asset-9",
    part_size_bytes: 10,
    status: "ACTIVE",
    parts: [],
  };
  const legacy = summarizeCheckpoint({ ticket, parts: [{ part_number: 1, etag: '"a"', size_bytes: 10 }] });
  assert.equal(legacy.fileName, null);
  assert.equal(legacy.percent, null, "an old checkpoint without a size shows bytes, not an invented percentage");
  assert.equal(legacy.completedBytes, 10);
  const file = { name: "lecture.mp4", size: 25, type: "video/mp4" };
  assert.equal(summarizeCheckpoint({ ticket: { ...ticket, status: "ASSEMBLED" }, parts: [], file }).status, "FINISHING");
  assert.equal(summarizeCheckpoint({ ticket: { ...ticket, status: "ASSEMBLED" }, parts: [], file }).percent, 100);
  assert.equal(summarizeCheckpoint({ ticket: { ...ticket, status: "ABORTED" }, parts: [], file }).status, "CANCELLED");
  assert.equal(summarizeCheckpoint({ ticket: null, parts: [], file }).status, "STARTING");
  assert.equal(summarizeCheckpoint({ ticket: null, parts: [], file }).percent, 0);
});

test("a file's own checkpoint resumes even beside an older checkpoint an earlier build left", async () => {
  const h = harness();
  try {
    const controller = new AbortController();
    const task = uploadResumable(file(30, 2), { ...input(), signal: controller.signal });
    await h.waitInflight(3);
    controller.abort(new DOMException("Upload paused", "AbortError"));
    await assert.rejects(task);
    // The previous build allowed a second checkpoint for the same control; plant a legacy one.
    const key = Object.keys(h.data)[0];
    (globalThis.localStorage as Storage).setItem(
      `${key.slice(0, -64)}${"0".repeat(64)}`,
      JSON.stringify({ requestID: "11111111-1111-4111-8111-111111111111", ticket: null, parts: [] }),
    );
    assert.equal(Object.keys(h.data).length, 2);
    const resumed = uploadResumable(file(30, 2), input());
    await h.waitInflight(3);
    for (const entry of [...h.inflight]) {
      entry.progress(10);
      entry.load();
    }
    await resumed;
    assert.equal(h.begins(), 1, "the matching file continued its own session");
    await assert.rejects(
      uploadResumable(file(30, 5), input()),
      (error: unknown) => error instanceof ResumeFileMismatchError,
      "a file matching neither checkpoint is still refused",
    );
  } finally {
    h.restore();
  }
});

test("an already assembled session is finished without announcing a resume", async () => {
  const h = harness();
  try {
    const first = uploadResumable(file(10), input());
    await h.waitInflight(1);
    h.part(1).progress(10);
    h.part(1).load();
    await first;
    // The checkpoint stays until selection is acknowledged; the session is now ASSEMBLED.
    assert.equal(readSavedUpload(input())?.status, "PAUSED", "the local hint still says ACTIVE");
    assert.equal((await refreshSavedUpload(input()))?.status, "FINISHING", "the server says it is assembled");
    let announced = false;
    await uploadResumable(file(10), { ...input(), onResuming: () => (announced = true) });
    assert.equal(announced, false);
    assert.equal(h.puts.get(1), 1);
  } finally {
    h.restore();
  }
});
