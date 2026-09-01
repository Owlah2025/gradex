import assert from "node:assert/strict";
import { test } from "node:test";
import { ProgressReporter } from "./progress-reporter-controller";
import { progressPath, progressReport, progressReportIntervalMilliseconds } from "./progress-contract";

type Deferred<T> = {
  promise: Promise<T>;
  resolve(value: T): void;
  reject(error: unknown): void;
};

type RecordedRequest = {
  url: string;
  init?: RequestInit;
  deferred: Deferred<Response>;
};

class FakeTimer {
  private nextID = 1;
  private scheduled = new Map<number, { callback: () => void; delayMilliseconds: number }>();

  schedule = (callback: () => void, delayMilliseconds: number): number => {
    const id = this.nextID++;
    this.scheduled.set(id, { callback, delayMilliseconds });
    return id;
  };

  cancel = (id: number): void => { this.scheduled.delete(id); };

  nextDelay(): number | null {
    return this.scheduled.values().next().value?.delayMilliseconds ?? null;
  }

  runNext(): void {
    const next = this.scheduled.entries().next().value as [number, { callback: () => void }] | undefined;
    if (!next) throw new Error("no timer scheduled");
    this.scheduled.delete(next[0]);
    next[1].callback();
  }

  get count(): number { return this.scheduled.size; }
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function requestRecorder(): { fetch: typeof fetch; requests: RecordedRequest[] } {
  const requests: RecordedRequest[] = [];
  const fetch = ((url: string | URL | Request, init?: RequestInit) => {
    const response = deferred<Response>();
    requests.push({ url: String(url), init, deferred: response });
    return response.promise;
  }) as typeof globalThis.fetch;
  return { fetch, requests };
}

function reporter(timer: FakeTimer, recorded = requestRecorder(), now = Date.parse("2026-08-01T12:00:00Z")) {
  return {
    recorded,
    reporter: new ProgressReporter({
      lessonID: "lesson-1", assetVersionID: "asset-1", fetchImplementation: recorded.fetch,
      timer, now: () => now, random: () => 0,
    }),
  };
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

function requestPosition(request: RecordedRequest): number {
  return JSON.parse(String(request.init?.body)).position_seconds;
}

test("progress reporter preserves the server-authoritative progress contract", () => {
  assert.equal(progressReportIntervalMilliseconds, 15_000);
  assert.equal(progressPath("lesson / id"), "/api/v1/learn/lessons/lesson%20%2F%20id/progress");
  assert.deepEqual(progressReport(12.5, "asset-version-1"), {
    position_seconds: 12.5,
    asset_version_id: "asset-version-1",
  });
  assert.equal(progressReport(-1, "asset-version-1"), null);
  assert.equal(progressReport(Number.NaN, "asset-version-1"), null);
  assert.equal(progressReport(12, ""), null);
});

test("network failures use bounded jittered exponential retries", async () => {
  const timer = new FakeTimer();
  const { reporter: subject, recorded } = reporter(timer);
  subject.reportPosition(10);
  recorded.requests[0].deferred.reject(new TypeError("network unavailable"));
  await settle();
  assert.equal(timer.nextDelay(), 1_600);

  timer.runNext();
  recorded.requests[1].deferred.reject(new TypeError("network unavailable"));
  await settle();
  assert.equal(timer.nextDelay(), 3_200);

  timer.runNext();
  recorded.requests[2].deferred.reject(new TypeError("network unavailable"));
  await settle();
  assert.equal(recorded.requests.length, 3);
  assert.equal(timer.count, 0);
});

test("only listed HTTP failures retry and success resets the chain", async () => {
  for (const status of [408, 429, 500, 502, 503, 504]) {
    const timer = new FakeTimer();
    const { reporter: subject, recorded } = reporter(timer);
    subject.reportPosition(10);
    recorded.requests[0].deferred.resolve(new Response(null, { status }));
    await settle();
    assert.notEqual(timer.nextDelay(), null, `status ${status} did not retry`);
    timer.runNext();
    assert.equal(recorded.requests[1].url, "/api/v1/learn/lessons/lesson-1/progress");
  }

  const timer = new FakeTimer();
  const { reporter: subject, recorded } = reporter(timer);
  subject.reportPosition(10);
  recorded.requests[0].deferred.resolve(new Response(null, { status: 500 }));
  await settle();
  timer.runNext();
  recorded.requests[1].deferred.resolve(new Response(null, { status: 204 }));
  await settle();
  subject.reportPosition(11);
  assert.equal(recorded.requests.length, 3);
  recorded.requests[2].deferred.resolve(new Response(null, { status: 204 }));
});

test("permanent HTTP failures and lifecycle aborts do not retry", async () => {
  for (const status of [400, 401, 403, 404, 405, 409, 413, 415, 422, 501]) {
    const timer = new FakeTimer();
    const { reporter: subject, recorded } = reporter(timer);
    subject.reportPosition(10);
    recorded.requests[0].deferred.resolve(new Response(null, { status }));
    await settle();
    assert.equal(timer.count, 0, `status ${status} unexpectedly retried`);
  }

  const timer = new FakeTimer();
  const { reporter: subject, recorded } = reporter(timer);
  subject.reportPosition(10);
  recorded.requests[0].deferred.reject(Object.assign(new Error("aborted"), { name: "AbortError" }));
  await settle();
  assert.equal(timer.count, 0);
});

test("coalesced periodic and player reports retain the greatest pending position", async () => {
  const timer = new FakeTimer();
  const { reporter: subject, recorded } = reporter(timer);
  subject.reportPosition(5);
  subject.reportPosition(11);
  subject.reportPosition(7);
  assert.equal(recorded.requests.length, 1);
  assert.equal(requestPosition(recorded.requests[0]), 5);
  recorded.requests[0].deferred.resolve(new Response(null, { status: 204 }));
  await settle();
  assert.equal(recorded.requests.length, 2);
  assert.equal(requestPosition(recorded.requests[1]), 11);
});

test("new samples use the active chain budget and cannot create a fourth request", async () => {
  const timer = new FakeTimer();
  const { reporter: subject, recorded } = reporter(timer);
  subject.reportPosition(5);
  recorded.requests[0].deferred.resolve(new Response(null, { status: 500 }));
  await settle();
  subject.reportPosition(12);
  timer.runNext();
  assert.equal(requestPosition(recorded.requests[1]), 12);
  recorded.requests[1].deferred.resolve(new Response(null, { status: 500 }));
  await settle();
  timer.runNext();
  recorded.requests[2].deferred.resolve(new Response(null, { status: 500 }));
  await settle();
  assert.equal(recorded.requests.length, 3);
  assert.equal(timer.count, 0);
});

test("Retry-After accepts delta seconds and HTTP dates without jitter", async () => {
  const now = Date.parse("2026-08-01T12:00:00Z");
  for (const [header, expectedDelay] of [["7", 7_000], ["Sat, 01 Aug 2026 12:00:05 GMT", 5_000]] as const) {
    const timer = new FakeTimer();
    const { reporter: subject, recorded } = reporter(timer, requestRecorder(), now);
    subject.reportPosition(10);
    recorded.requests[0].deferred.resolve(new Response(null, { status: 429, headers: { "Retry-After": header } }));
    await settle();
    assert.equal(timer.nextDelay(), expectedDelay);
  }
});

test("invalid Retry-After values use the fifteen-second fallback", async () => {
  const now = Date.parse("2026-08-01T12:00:00Z");
  for (const header of [null, "bad", "-1", "Sat, 01 Aug 2026 11:59:59 GMT"]) {
    const timer = new FakeTimer();
    const { reporter: subject, recorded } = reporter(timer, requestRecorder(), now);
    subject.reportPosition(10);
    const headers = header === null ? undefined : { "Retry-After": header };
    recorded.requests[0].deferred.resolve(new Response(null, { status: 429, headers }));
    await settle();
    assert.equal(timer.nextDelay(), 15_000, `header ${header} did not use fallback`);
  }
});

test("429 retries consume the normal three-attempt budget", async () => {
  const timer = new FakeTimer();
  const { reporter: subject, recorded } = reporter(timer);
  subject.reportPosition(10);
  for (let attempt = 0; attempt < 3; attempt += 1) {
    recorded.requests[attempt].deferred.resolve(new Response(null, { status: 429, headers: { "Retry-After": "0" } }));
    await settle();
    if (attempt < 2) timer.runNext();
  }
  assert.equal(recorded.requests.length, 3);
  assert.equal(timer.count, 0);
});

test("replacing a Lesson or Asset Version cannot replay disposed retry state", async () => {
  const timer = new FakeTimer();
  const first = reporter(timer);
  first.reporter.reportPosition(10);
  first.recorded.requests[0].deferred.reject(new TypeError("network unavailable"));
  await settle();
  first.reporter.dispose();
  assert.equal(timer.count, 0);

  const secondRecorded = requestRecorder();
  const second = new ProgressReporter({
    lessonID: "lesson-2", assetVersionID: "asset-1", fetchImplementation: secondRecorded.fetch,
    timer, now: Date.now, random: () => 0,
  });
  second.reportPosition(20);
  assert.equal(first.recorded.requests.length, 1);
  assert.equal(secondRecorded.requests.length, 1);
  assert.equal(secondRecorded.requests[0].url, "/api/v1/learn/lessons/lesson-2/progress");
  assert.equal(JSON.parse(String(secondRecorded.requests[0].init?.body)).asset_version_id, "asset-1");
  second.dispose();

  const replacementRecorded = requestRecorder();
  const replacement = new ProgressReporter({
    lessonID: "lesson-2", assetVersionID: "asset-2", fetchImplementation: replacementRecorded.fetch,
    timer, now: Date.now, random: () => 0,
  });
  replacement.reportPosition(30);
  assert.equal(secondRecorded.requests.length, 1);
  assert.equal(replacementRecorded.requests.length, 1);
  assert.equal(JSON.parse(String(replacementRecorded.requests[0].init?.body)).asset_version_id, "asset-2");
});

test("disposing an in-flight request aborts it and blocks late retry scheduling", async () => {
  const timer = new FakeTimer();
  const { reporter: subject, recorded } = reporter(timer);
  subject.reportPosition(10);
  subject.dispose();
  assert.equal(recorded.requests[0].init?.signal?.aborted, true);
  recorded.requests[0].deferred.reject(new TypeError("network unavailable"));
  await settle();
  assert.equal(timer.count, 0);
});

test("page exit uses one same-origin keepalive PUT and never schedules a retry", async () => {
  const timer = new FakeTimer();
  const { reporter: subject, recorded } = reporter(timer);
  subject.reportPageExit(10);
  assert.equal(recorded.requests.length, 1);
  assert.deepEqual(recorded.requests[0].init?.method, "PUT");
  assert.equal(recorded.requests[0].init?.credentials, "same-origin");
  assert.equal(recorded.requests[0].init?.keepalive, true);
  assert.equal(new Headers(recorded.requests[0].init?.headers).get("Content-Type"), "application/json");
  assert.deepEqual(JSON.parse(String(recorded.requests[0].init?.body)), { position_seconds: 10, asset_version_id: "asset-1" });
  recorded.requests[0].deferred.resolve(new Response(null, { status: 500 }));
  await settle();
  assert.equal(timer.count, 0);
});

test("page exit does not duplicate an in-flight ordinary submission", () => {
  const timer = new FakeTimer();
  const { reporter: subject, recorded } = reporter(timer);
  subject.reportPosition(10);
  subject.reportPageExit(10);
  assert.equal(recorded.requests.length, 1);
  assert.equal(recorded.requests[0].init?.keepalive, false);
});

// Every other case injects `fetchImplementation`, so the default was never exercised. In a
// browser, a bare `fetch` reference called as `this.fetchImplementation(...)` receives the
// reporter as `this` and throws "Illegal invocation" before any request is issued — which the
// retry path then swallows, so Progress silently never persists.
//
// Node's `fetch` accepts any receiver, so these cases install a stub that enforces the browser's
// rule: `fetch` called on anything other than the global object throws `Illegal invocation`
// synchronously. Against the previous bare-reference implementation every one of these fails —
// the stub throws, the controller's catch classifies it as a retryable network error, and the
// write disappears after the retry budget without ever reaching the network.
//
type DefaultPathHarness = {
  requests: RecordedRequest[];
  receivers: unknown[];
  restore: () => void;
};

function browserStrictFetch(): DefaultPathHarness {
  const originalFetch = globalThis.fetch;
  const requests: RecordedRequest[] = [];
  const receivers: unknown[] = [];
  globalThis.fetch = function (this: unknown, url: string | URL | Request, init?: RequestInit) {
    receivers.push(this);
    if (this !== undefined && this !== globalThis) {
      throw new TypeError("Failed to execute 'fetch' on 'Window': Illegal invocation");
    }
    const response = deferred<Response>();
    requests.push({ url: String(url), init, deferred: response });
    return response.promise;
  } as unknown as typeof fetch;
  return { requests, receivers, restore: () => { globalThis.fetch = originalFetch; } };
}

function defaultPathReporter(timer: FakeTimer, csrfToken: () => string | null = () => "csrf-token-value") {
  // Deliberately no `fetchImplementation`: this is the production default path.
  return new ProgressReporter({
    lessonID: "lesson-1", assetVersionID: "asset-1", timer, now: () => 0, random: () => 0, csrfToken,
  });
}

test("the default fetch path dispatches without Illegal invocation and sends the correct request", async () => {
  const harness = browserStrictFetch();
  const timer = new FakeTimer();
  try {
    const subject = defaultPathReporter(timer);
    assert.doesNotThrow(() => subject.reportPosition(42.5));
    await settle();

    assert.equal(harness.requests.length, 1, "exactly one real dispatch chain starts");
    assert.ok(
      !(harness.receivers[0] instanceof ProgressReporter),
      "fetch received the reporter as `this`, which is Illegal invocation in a browser"
    );

    const request = harness.requests[0];
    assert.equal(request.url, progressPath("lesson-1"));
    assert.deepEqual(JSON.parse(String(request.init?.body)), progressReport(42.5, "asset-1"));
    assert.equal(request.init?.method, "PUT");
    assert.equal(request.init?.credentials, "same-origin", "the session cookie must travel same-origin");
    assert.equal(request.init?.cache, "no-store");
    assert.equal((request.init?.headers as Record<string, string>)["X-CSRF-Token"], "csrf-token-value");
    assert.equal((request.init?.headers as Record<string, string>)["Content-Type"], "application/json");
    assert.equal(timer.count, 0, "a first attempt schedules no retry");
  } finally {
    harness.restore();
  }
});

test("the default fetch path omits the CSRF header when no token is held", async () => {
  const harness = browserStrictFetch();
  try {
    defaultPathReporter(new FakeTimer(), () => null).reportPosition(10);
    await settle();
    assert.equal(harness.requests.length, 1);
    assert.ok(!("X-CSRF-Token" in (harness.requests[0].init?.headers as Record<string, string>)));
  } finally {
    harness.restore();
  }
});

test("the default fetch path retries only retryable failures and never interrupts playback", async () => {
  const harness = browserStrictFetch();
  const timer = new FakeTimer();
  try {
    const subject = defaultPathReporter(timer);
    subject.reportPosition(10);
    await settle();
    assert.equal(harness.requests.length, 1);

    // A 503 is retryable: the chain continues on the same default path.
    harness.requests[0].deferred.resolve({ ok: false, status: 503, headers: new Headers() } as Response);
    await settle();
    assert.equal(timer.count, 1, "a retryable failure schedules exactly one retry");
    assert.equal(timer.nextDelay(), 1_600, "the 2 s first retry at the -20% jitter floor");
    timer.runNext();
    await settle();
    assert.equal(harness.requests.length, 2, "the retry also dispatches without Illegal invocation");

    // A 403 is not retryable: the chain ends quietly rather than surfacing to the player.
    harness.requests[1].deferred.resolve({ ok: false, status: 403, headers: new Headers() } as Response);
    await settle();
    assert.equal(timer.count, 0, "a permanent failure schedules no retry");
    assert.equal(harness.requests.length, 2);

    // A generic failure leaves the reporter usable; it neither throws nor pauses media. The
    // reporter holds no media handle at all, which is what makes that structurally true.
    assert.doesNotThrow(() => subject.reportPosition(20));
    await settle();
    assert.equal(harness.requests.length, 3, "playback continues reporting after a failed write");
  } finally {
    harness.restore();
  }
});

test("disposing a default-path reporter cancels pending retries and sends nothing afterwards", async () => {
  const harness = browserStrictFetch();
  const timer = new FakeTimer();
  try {
    const subject = defaultPathReporter(timer);
    subject.reportPosition(10);
    await settle();
    harness.requests[0].deferred.resolve({ ok: false, status: 500, headers: new Headers() } as Response);
    await settle();
    assert.equal(timer.count, 1, "a retry is pending before disposal");

    subject.dispose();
    assert.equal(timer.count, 0, "disposal cancels the pending retry timer");

    subject.reportPosition(30);
    await settle();
    assert.equal(harness.requests.length, 1, "no request is sent after disposal");
  } finally {
    harness.restore();
  }
});

test("a successful write hands back the canonical state the server computed", async () => {
  const recorder = requestRecorder();
  const timer = new FakeTimer();
  const confirmations: unknown[] = [];
  const reporter = new ProgressReporter({
    lessonID: "lesson-1",
    assetVersionID: "asset-1",
    fetchImplementation: recorder.fetch,
    timer,
    onConfirmed: (confirmation) => confirmations.push(confirmation),
  });

  reporter.reportPosition(54);
  recorder.requests[0].deferred.resolve(
    new Response(
      JSON.stringify({
        lesson_progress: { position_seconds: 54, completed: true },
        course_progress: { completed_lessons: 1, total_lessons: 4, percent: 25 },
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    ),
  );
  // Reading the body is asynchronous, so this waits a full turn of the event
  // loop rather than a fixed number of microtasks.
  await new Promise((resolve) => setTimeout(resolve, 0));

  assert.deepEqual(confirmations, [
    {
      lessonID: "lesson-1",
      lesson: { position_seconds: 54, completed: true },
      course: { completed_lessons: 1, total_lessons: 4, percent: 25 },
    },
  ]);
  reporter.dispose();
});

test("a server that returns no body is still a successful write", async () => {
  const recorder = requestRecorder();
  const timer = new FakeTimer();
  const confirmations: unknown[] = [];
  const reporter = new ProgressReporter({
    lessonID: "lesson-1",
    assetVersionID: "asset-1",
    fetchImplementation: recorder.fetch,
    timer,
    onConfirmed: (confirmation) => confirmations.push(confirmation),
  });

  reporter.reportPosition(12);
  // An older server answering 204, or any unparseable body: nothing new to
  // render, and emphatically not a retry — the write already happened.
  recorder.requests[0].deferred.resolve(new Response(null, { status: 204 }));
  await new Promise((resolve) => setTimeout(resolve, 0));

  assert.equal(confirmations.length, 0);
  assert.equal(recorder.requests.length, 1, "a bodyless success was retried");
  assert.equal(timer.count, 0, "a bodyless success scheduled a retry");
  reporter.dispose();
});
