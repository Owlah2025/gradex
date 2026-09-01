import assert from "node:assert/strict";
import { test } from "node:test";
import { progressReportIntervalMilliseconds } from "./progress-contract";
import { attachProgressReporter, type ReporterTargets } from "./progress-reporter-lifecycle";

type Listener = () => void;

class FakeEventTarget {
  readonly listeners = new Map<string, Listener[]>();

  addEventListener(type: string, listener: Listener): void {
    const existing = this.listeners.get(type) ?? [];
    existing.push(listener);
    this.listeners.set(type, existing);
  }

  removeEventListener(type: string, listener: Listener): void {
    const existing = this.listeners.get(type) ?? [];
    const index = existing.indexOf(listener);
    if (index >= 0) existing.splice(index, 1);
    this.listeners.set(type, existing);
  }

  countFor(type: string): number {
    return (this.listeners.get(type) ?? []).length;
  }

  get totalListeners(): number {
    return [...this.listeners.values()].reduce((total, entries) => total + entries.length, 0);
  }

  emit(type: string): void {
    for (const listener of [...(this.listeners.get(type) ?? [])]) listener();
  }
}

class FakeMedia extends FakeEventTarget {
  currentTime = 0;
}

class FakeDocument extends FakeEventTarget {
  visibilityState = "visible";
}

class FakeWindow extends FakeEventTarget {
  private nextHandle = 1;
  readonly intervals = new Map<number, { handler: Listener; milliseconds: number }>();

  setInterval(handler: Listener, milliseconds: number): number {
    const handle = this.nextHandle++;
    this.intervals.set(handle, { handler, milliseconds });
    return handle;
  }

  clearInterval(handle: number): void {
    this.intervals.delete(handle);
  }

  tickAllIntervals(): void {
    for (const { handler } of [...this.intervals.values()]) handler();
  }
}

class RecordingReporter {
  readonly positions: number[] = [];
  readonly exits: number[] = [];
  disposeCount = 0;

  constructor(readonly name: string) {}

  reportPosition(positionSeconds: number): void {
    this.positions.push(positionSeconds);
  }

  reportPageExit(positionSeconds: number): void {
    this.exits.push(positionSeconds);
  }

  dispose(): void {
    this.disposeCount += 1;
  }
}

function targets(): ReporterTargets & { media: FakeMedia; documentTarget: FakeDocument; windowTarget: FakeWindow } {
  return { media: new FakeMedia(), documentTarget: new FakeDocument(), windowTarget: new FakeWindow() };
}

test("a mount subscribes exactly one timer and one listener set", () => {
  const scope = targets();
  const reporter = new RecordingReporter("only");
  attachProgressReporter(scope, reporter);

  assert.equal(scope.windowTarget.intervals.size, 1);
  assert.equal([...scope.windowTarget.intervals.values()][0].milliseconds, progressReportIntervalMilliseconds);
  assert.equal(scope.media.countFor("pause"), 1);
  assert.equal(scope.media.countFor("seeked"), 1);
  assert.equal(scope.media.countFor("ended"), 1);
  assert.equal(scope.documentTarget.countFor("visibilitychange"), 1);
  assert.equal(scope.windowTarget.countFor("pagehide"), 1);
});

// This is the StrictMode sequence: mount, tear down, mount again against the same DOM nodes.
test("React StrictMode's double mount leaves exactly one live reporter, timer, and listener set", () => {
  const scope = targets();
  const first = new RecordingReporter("first");
  const second = new RecordingReporter("second");

  const disposeFirst = attachProgressReporter(scope, first);
  disposeFirst();
  attachProgressReporter(scope, second);

  assert.equal(first.disposeCount, 1, "the first mount's reporter is disposed");
  assert.equal(second.disposeCount, 0, "the surviving reporter stays live");

  assert.equal(scope.windowTarget.intervals.size, 1, "exactly one active periodic timer");
  assert.equal(scope.media.totalListeners, 3, "exactly one media listener set: pause, seeked, ended");
  assert.equal(scope.documentTarget.totalListeners, 1);
  assert.equal(scope.windowTarget.totalListeners, 1);

  // A single interaction must reach exactly one reporter. Two live controllers would each hold
  // their own single-flight budget and would each write, which is how one browser interaction
  // previously produced multiple successful Progress writes.
  scope.media.currentTime = 12;
  scope.media.emit("pause");
  scope.media.emit("seeked");
  scope.windowTarget.tickAllIntervals();
  scope.documentTarget.visibilityState = "hidden";
  scope.documentTarget.emit("visibilitychange");
  scope.windowTarget.emit("pagehide");

  assert.deepEqual(first.positions, [], "no stale controller reports");
  assert.deepEqual(first.exits, [], "no stale controller reports on page exit");
  assert.deepEqual(second.positions, [12, 12, 12, 12], "pause, seeked, interval, and hidden each report once");
  assert.deepEqual(second.exits, [12], "page exit reports once");
});

test("the disposer is idempotent, so a repeated teardown cannot detach the live mount", () => {
  const scope = targets();
  const first = new RecordingReporter("first");
  const disposeFirst = attachProgressReporter(scope, first);
  disposeFirst();

  const second = new RecordingReporter("second");
  attachProgressReporter(scope, second);
  disposeFirst();

  assert.equal(first.disposeCount, 1);
  assert.equal(second.disposeCount, 0);
  assert.equal(scope.windowTarget.intervals.size, 1, "the live mount keeps its timer");
  assert.equal(scope.media.totalListeners, 3, "the live mount keeps its listeners");

  scope.media.currentTime = 7;
  scope.media.emit("pause");
  assert.deepEqual(second.positions, [7]);
});

test("a visible document does not report on visibilitychange", () => {
  const scope = targets();
  const reporter = new RecordingReporter("only");
  attachProgressReporter(scope, reporter);

  scope.documentTarget.visibilityState = "visible";
  scope.documentTarget.emit("visibilitychange");
  assert.deepEqual(reporter.positions, []);

  scope.documentTarget.visibilityState = "hidden";
  scope.documentTarget.emit("visibilitychange");
  assert.deepEqual(reporter.positions, [0]);
});

test("teardown detaches every subscription and disposes the reporter once", () => {
  const scope = targets();
  const reporter = new RecordingReporter("only");
  const dispose = attachProgressReporter(scope, reporter);
  dispose();

  assert.equal(scope.windowTarget.intervals.size, 0);
  assert.equal(scope.media.totalListeners, 0);
  assert.equal(scope.documentTarget.totalListeners, 0);
  assert.equal(scope.windowTarget.totalListeners, 0);
  assert.equal(reporter.disposeCount, 1);

  scope.media.currentTime = 30;
  scope.media.emit("pause");
  scope.windowTarget.tickAllIntervals();
  assert.deepEqual(reporter.positions, [], "a disposed mount reports nothing");
});

test("reaching the end of a Lesson reports immediately rather than waiting for the interval", () => {
  const scope = targets();
  const reporter = new RecordingReporter("only");
  const dispose = attachProgressReporter(scope, reporter);

  scope.media.currentTime = 600;
  scope.media.emit("ended");

  // Without this the video stops, the Lesson is complete server-side after the
  // next tick, and the screen keeps saying "in progress" for up to fifteen
  // seconds — the exact delay the Student sees as "it didn't update".
  assert.deepEqual(reporter.positions, [600], "ended did not report the completing position");
  assert.equal(reporter.exits.length, 0, "ended must not be treated as a page exit");

  dispose();
  assert.equal(scope.media.totalListeners, 0, "the ended listener outlived its mount");
});
