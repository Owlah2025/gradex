import assert from "node:assert/strict";
import test from "node:test";
import {
  readSavedUpload,
  refreshSavedUpload,
  uploadResumable,
  type MultipartUploadTicket,
  type ResumableInput,
} from "./media-multipart";
import { UploadProgressTracker, progressFraction, type UploadProgress } from "./upload-progress";
import { savedForResumeLine, durablePercent } from "../../components/instructor/resumable-upload-copy";

/*
  Transferred progress versus durable (resumable) progress.

  Transferred = completed parts + bytes of parts still in flight. Durable = only parts the storage
  provider has accepted with an ETag; that is all R2 keeps, and all a reload can recover. These
  tests pin the production report: the bar reached ~80%, a reload showed "paused at 12%".
*/

const MiB = 1024 * 1024;
const PART_BYTES = 8 * MiB; // backend/internal/media/multipart.go MultipartPartBytes

type Inflight = { number: number; size: number; progress: (loaded: number) => void; load: () => void; fail: () => void };

function harness(partBytes: number) {
  const saved = { fetch: globalThis.fetch, xhr: globalThis.XMLHttpRequest, storage: globalThis.localStorage };
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
  let ticket: MultipartUploadTicket | null = null;
  const stored = new Map<number, number>();
  const puts = new Map<number, number>();
  const inflight: Inflight[] = [];
  const completions: Array<{ parts: Array<{ part_number: number }>; size_bytes: number }> = [];
  class XHR {
    status = 200;
    upload: { onprogress: ((event: ProgressEvent) => void) | null } = { onprogress: null };
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;
    onabort: (() => void) | null = null;
    ontimeout: (() => void) | null = null;
    timeout = 0;
    withCredentials = false;
    private number = 0;
    private entry: Inflight | null = null;
    open(_method: string, url: string) {
      this.number = Number(url.split("/").at(-1));
    }
    setRequestHeader() {}
    getResponseHeader() {
      return `"etag-${this.number}"`;
    }
    abort() {
      if (this.entry) inflight.splice(inflight.indexOf(this.entry), 1);
      this.onabort?.();
    }
    send(blob: Blob) {
      puts.set(this.number, (puts.get(this.number) || 0) + 1);
      const entry: Inflight = {
        number: this.number,
        size: blob.size,
        progress: (loaded) => this.upload.onprogress?.({ loaded, total: blob.size } as ProgressEvent),
        load: () => {
          inflight.splice(inflight.indexOf(entry), 1);
          stored.set(this.number, blob.size); // the provider accepted the whole part
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
  const current = (): MultipartUploadTicket => ({
    ...ticket!,
    parts: Array.from(stored.entries())
      .sort((a, b) => a[0] - b[0])
      .map(([part_number, size_bytes]) => ({ part_number, etag: `"etag-${part_number}"`, size_bytes })),
  });
  globalThis.fetch = async (url, init) => {
    const path = String(url);
    const method = init?.method || "GET";
    if (path.endsWith("/uploads/multipart") && method === "POST") {
      ticket = {
        asset_version_id: "asset-1",
        upload_id: "provider-1",
        storage_object_key: "quarantine/course/asset-1/source",
        expires_at: new Date(Date.now() + 60000).toISOString(),
        part_size_bytes: partBytes,
        status: "ACTIVE",
        parts: [],
      };
      return Response.json(current(), { status: 201 });
    }
    if (path.includes("/parts/")) return Response.json({ url: `https://storage.test/asset-1/${path.split("/").at(-1)}` });
    if (path.endsWith("/completions")) {
      completions.push(JSON.parse(init?.body as string));
      ticket!.status = "ASSEMBLED";
      return Response.json({ asset_version_id: "asset-1", state: "QUARANTINED", duplicate: false, storage_object_version: 'etag:"whole-2"' });
    }
    if (method === "GET") return Response.json(current());
    throw new Error(`unexpected ${method} ${path}`);
  };
  const settle = async () => {
    for (let i = 0; i < 30; i++) await new Promise((resolve) => setImmediate(resolve));
  };
  const waitInflight = async (count: number) => {
    const deadline = Date.now() + 5000;
    while (inflight.length < count && Date.now() < deadline) {
      await settle();
      await new Promise((resolve) => setTimeout(resolve, 5));
    }
    assert.equal(inflight.length, count, `expected ${count} part(s) in flight`);
  };
  const part = (n: number) => {
    const found = inflight.find((entry) => entry.number === n);
    assert.ok(found, `part ${n} is not in flight`);
    return found;
  };
  return {
    data,
    puts,
    stored,
    inflight,
    completions,
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

const input: ResumableInput = {
  courseID: "course-a",
  revisionID: "revision-a",
  lessonID: "lesson-a",
  kind: "VIDEO",
  storageKeyId: "video-a",
  locale: "en",
  csrf: "csrf",
};
const sized = (size: number) => new File([new Uint8Array(size).fill(3)], "lecture.mp4", { type: "video/mp4" });
const checkpointParts = (data: Record<string, string>) =>
  (JSON.parse(Object.values(data)[0]).parts as Array<{ part_number: number }>).map((p) => p.part_number);

test("9.6 MB: the small last part finishes first, 80% is transferred, 12% is saved, and a reload recovers exactly 12%", async () => {
  const total = 9_600_000;
  const lastPart = total - PART_BYTES; // 1,211,392 bytes
  const h = harness(PART_BYTES);
  try {
    const events: UploadProgress[] = [];
    const controller = new AbortController();
    const run = uploadResumable(sized(total), { ...input, signal: controller.signal }, (_f, p) => events.push(p));
    await h.waitInflight(2); // both parts start together

    // Part 2 (~1.2 MB) is accepted first. Durable progress moves the moment it is, and the
    // checkpoint already lists it.
    h.part(2).progress(lastPart);
    h.part(2).load();
    await h.settle();
    assert.equal(events.at(-1)!.completedBytes, lastPart);
    assert.deepEqual(checkpointParts(h.data), [2]);

    // Part 1 (8 MiB) is still on the wire.
    h.part(1).progress(6_500_000);
    const live = events.at(-1)!;
    assert.equal(live.transferredBytes, lastPart + 6_500_000);
    assert.equal(Math.round(progressFraction(live) * 100), 80, "the bar honestly shows ~80% transferred");
    assert.equal(durablePercent(live), 12, "but only ~12% is saved");
    assert.equal(
      savedForResumeLine(live, "en"),
      "Saved for resume: 12% · 1.2 MB safely uploaded. Parts still transferring are lost if you refresh or close this page.",
    );

    // Abrupt reload: the unfinished XHR dies with the page.
    controller.abort(new DOMException("Upload paused", "AbortError"));
    await assert.rejects(run);
    assert.equal(readSavedUpload(input)?.percent, 12);
    const recovered = await refreshSavedUpload(input);
    assert.equal(recovered?.source, "server");
    assert.equal(recovered?.percent, 12, "server ListParts is the truth: no false 80% after reload");
    assert.equal(recovered?.completedBytes, lastPart);

    // Resume: part 2 is not sent again; part 1 restarts from its first byte.
    const resumed: UploadProgress[] = [];
    const second = uploadResumable(sized(total), input, (_f, p) => resumed.push(p));
    await h.waitInflight(1);
    assert.equal(h.inflight[0].number, 1);
    assert.equal(resumed[0].reportedBytes, lastPart, "resumed progress starts at the saved 12%");
    h.part(1).progress(PART_BYTES);
    h.part(1).load();
    await second;
    assert.equal(h.puts.get(2), 1, "the accepted part is not uploaded again");
    assert.equal(h.puts.get(1), 2, "the unfinished part restarts");
    assert.equal(h.stored.get(1), PART_BYTES, "the restarted part is stored whole");
    assert.deepEqual(h.completions.at(-1)!.parts.map((p) => p.part_number), [1, 2], "completion is not corrupted");
    assert.equal(h.completions.at(-1)!.size_bytes, total);
  } finally {
    h.restore();
  }
});

test("a graceful pause lets in-flight parts finish and saves far more than an abrupt stop", async () => {
  const PART = 10;
  const h = harness(PART);
  try {
    const drain = new AbortController();
    const events: UploadProgress[] = [];
    const run = uploadResumable(sized(40), { ...input, drain: drain.signal }, (_f, p) => events.push(p));
    const outcome = run.then(() => null, (error: unknown) => error);
    await h.waitInflight(3);
    for (const n of [1, 2, 3]) h.part(n).progress(6);
    assert.equal(events.at(-1)!.completedBytes, 0, "nothing is saved yet: an abrupt reload now would keep 0%");

    drain.abort(); // the Instructor pressed Pause
    for (const n of [1, 2, 3]) {
      h.part(n).progress(PART);
      h.part(n).load();
      await h.settle();
    }
    const ended = await outcome;
    assert.ok(ended instanceof DOMException && ended.name === "AbortError", String(ended));
    assert.equal(h.puts.get(4), undefined, "no new part starts after Pause");
    assert.deepEqual(checkpointParts(h.data), [1, 2, 3]);
    assert.equal(readSavedUpload(input)?.percent, 75, "paused at 75% instead of 0%");
    assert.equal((await refreshSavedUpload(input))?.percent, 75);
  } finally {
    h.restore();
  }
});

test("a part failing while a pause finishes is not retried and the run ends paused, not failed", async () => {
  const h = harness(10);
  try {
    const drain = new AbortController();
    const run = uploadResumable(sized(40), { ...input, drain: drain.signal });
    const outcome = run.then(() => null, (error: unknown) => error);
    await h.waitInflight(3);
    drain.abort();
    h.part(1).fail();
    h.part(2).progress(10);
    h.part(2).load();
    h.part(3).progress(10);
    h.part(3).load();
    const ended = await outcome;
    assert.ok(ended instanceof DOMException && ended.name === "AbortError", `ended as ${String(ended)}, not as a failure`);
    assert.equal(h.puts.get(1), 1, "no retry once Pause was requested");
    assert.equal(readSavedUpload(input)?.percent, 50);
  } finally {
    h.restore();
  }
});

test("a pause requested while the last parts are in flight still pauses; Resume sends nothing and completes", async () => {
  const h = harness(10);
  try {
    const drain = new AbortController();
    const run = uploadResumable(sized(20), { ...input, drain: drain.signal });
    const outcome = run.then(() => null, (error: unknown) => error);
    await h.waitInflight(2);
    drain.abort();
    for (const n of [1, 2]) {
      h.part(n).progress(10);
      h.part(n).load();
      await h.settle();
    }
    const ended = await outcome;
    assert.ok(ended instanceof DOMException && ended.name === "AbortError", String(ended));
    assert.equal(h.completions.length, 0, "paused means paused, even with every part saved");
    assert.equal(readSavedUpload(input)?.percent, 100);
    const result = await uploadResumable(sized(20), input);
    assert.equal(result.state, "QUARANTINED");
    assert.equal(h.puts.get(1), 1);
    assert.equal(h.puts.get(2), 1, "Resume sent no part again");
    assert.equal(h.completions.length, 1);
  } finally {
    h.restore();
  }
});

test("lecture-sized files: an abrupt reload can discard at most three in-flight 8 MiB parts", () => {
  // Production READY videos on 2026-10-08: median 51 MB, p90 94 MB, max 428 MB. Browser cap 2 GiB.
  const maxInFlightLoss = 3 * PART_BYTES;
  assert.equal(maxInFlightLoss, 25_165_824);
  for (const size of [51_100_000, 93_500_000, 427_800_000, 2 * 1024 * MiB]) {
    const tracker = new UploadProgressTracker(size, PART_BYTES);
    const parts = Math.ceil(size / PART_BYTES);
    assert.ok(parts <= 10_000, "within the S3/R2 part limit");
    // Halfway through, with three parts each one byte short of finishing.
    const done = Math.floor(parts / 2);
    for (let n = 1; n <= done; n++) {
      tracker.partProgress(n, tracker.partSize(n));
      tracker.partCompleted(n);
    }
    for (let n = done + 1; n <= Math.min(parts, done + 3); n++) tracker.partProgress(n, tracker.partSize(n) - 1);
    const snapshot = tracker.snapshot();
    const atRisk = snapshot.transferredBytes - snapshot.completedBytes;
    assert.ok(atRisk < maxInFlightLoss, `${size}: ${atRisk} bytes at risk`);
    assert.ok(atRisk > 0);
    // After an abrupt stop only completed parts remain.
    tracker.stopAll();
    assert.equal(tracker.snapshot().transferredBytes, snapshot.completedBytes);
  }
});
