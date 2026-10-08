import assert from "node:assert/strict";
import test from "node:test";
import {
  ThroughputMeter,
  UploadProgressTracker,
  formatBytes,
  progressFraction,
} from "./upload-progress";

const MB = 1_000_000;

test("in-flight bytes of parallel parts add to completed parts without double counting", () => {
  const tracker = new UploadProgressTracker(30 * MB, 10 * MB);
  tracker.partProgress(1, 10 * MB);
  tracker.partCompleted(1);
  tracker.partProgress(2, 6 * MB);
  tracker.partProgress(3, 4 * MB);
  const snapshot = tracker.snapshot();
  assert.equal(snapshot.completedBytes, 10 * MB);
  assert.equal(snapshot.transferredBytes, 20 * MB, "10 completed + 6 + 4 in flight, not 30 and not 10");
  assert.equal(snapshot.reportedBytes, 20 * MB);
});

test("a part moving from in-flight to completed does not jump twice", () => {
  const tracker = new UploadProgressTracker(30 * MB, 10 * MB);
  tracker.partProgress(2, 10 * MB);
  const before = tracker.snapshot().transferredBytes;
  tracker.partCompleted(2);
  assert.equal(tracker.snapshot().transferredBytes, before);
  // A stray late progress event for a completed part is ignored.
  tracker.partProgress(2, 3 * MB);
  assert.equal(tracker.snapshot().transferredBytes, before);
  tracker.partCompleted(2);
  assert.equal(tracker.snapshot().completedBytes, 10 * MB, "completing twice still counts once");
});

test("a failed attempt's bytes stop counting before its retry, and the bar does not regress", () => {
  const tracker = new UploadProgressTracker(20 * MB, 10 * MB);
  tracker.partProgress(1, 7 * MB);
  assert.equal(tracker.snapshot().reportedBytes, 7 * MB);
  tracker.partReset(1, true);
  const failed = tracker.snapshot();
  assert.equal(failed.transferredBytes, 0, "the exact count drops the failed attempt");
  assert.equal(failed.reportedBytes, 7 * MB, "the displayed count holds its high-water mark");
  assert.equal(failed.retrying, true);
  tracker.partProgress(1, 2 * MB);
  const retry = tracker.snapshot();
  assert.equal(retry.transferredBytes, 2 * MB);
  assert.equal(retry.reportedBytes, 7 * MB);
  assert.equal(retry.retrying, false, "bytes moving again ends the retrying state");
  tracker.partProgress(1, 10 * MB);
  tracker.partCompleted(1);
  assert.equal(tracker.snapshot().reportedBytes, 10 * MB);
});

test("a resumed run starts from the parts the server already holds", () => {
  const tracker = new UploadProgressTracker(25 * MB, 10 * MB, [1, 3]);
  const snapshot = tracker.snapshot();
  assert.equal(snapshot.completedBytes, 15 * MB, "part 3 is the 5 MB tail");
  assert.equal(snapshot.resumedFromBytes, 15 * MB);
  assert.equal(snapshot.reportedBytes, 15 * MB);
  assert.equal(progressFraction(snapshot), 0.6);
});

test("values are clamped: never negative, never above a part, never above the file", () => {
  const tracker = new UploadProgressTracker(15 * MB, 10 * MB);
  tracker.partProgress(1, -5);
  assert.equal(tracker.snapshot().transferredBytes, 0);
  tracker.partProgress(2, 99 * MB);
  assert.equal(tracker.snapshot().transferredBytes, 5 * MB, "the last part is only 5 MB");
  tracker.partProgress(1, Number.NaN);
  assert.equal(tracker.snapshot().transferredBytes, 5 * MB);
  tracker.partProgress(1, 10 * MB);
  tracker.partCompleted(1);
  tracker.partCompleted(2);
  const done = tracker.snapshot();
  assert.equal(done.transferredBytes, 15 * MB);
  assert.equal(progressFraction(done), 1);
});

test("stopping keeps completed parts and drops every in-flight byte", () => {
  const tracker = new UploadProgressTracker(30 * MB, 10 * MB);
  tracker.partProgress(1, 10 * MB);
  tracker.partCompleted(1);
  tracker.partProgress(2, 5 * MB);
  tracker.partReset(3, true);
  tracker.stopAll();
  const stopped = tracker.snapshot();
  assert.equal(stopped.transferredBytes, 10 * MB);
  assert.equal(stopped.completedBytes, 10 * MB);
  assert.equal(stopped.retrying, false);
});

test("zero-byte files report complete", () => {
  assert.equal(progressFraction({ reportedBytes: 0, totalBytes: 0 }), 1);
});

test("throughput uses a rolling window and stays silent until it means something", () => {
  const meter = new ThroughputMeter(5000, 1000);
  meter.record(0, 0);
  meter.record(4 * MB, 500);
  assert.equal(meter.bytesPerSecond(), null, "half a second is not enough to report");
  meter.record(8 * MB, 1000);
  assert.equal(meter.bytesPerSecond(), 8 * MB);
  assert.equal(meter.secondsRemaining(16 * MB), 2);
  // Old samples age out of the window.
  for (let at = 2000; at <= 10000; at += 1000) meter.record(8 * MB + (at - 1000) * 1000, at);
  assert.equal(Math.round(meter.bytesPerSecond()!), 1 * MB);
  // A restart (fewer bytes than before) begins a new measurement.
  meter.record(1 * MB, 11000);
  assert.equal(meter.bytesPerSecond(), null);
});

test("byte formatting", () => {
  assert.equal(formatBytes(0), "0 B");
  assert.equal(formatBytes(999), "999 B");
  assert.equal(formatBytes(8_400_000), "8.4 MB");
  assert.equal(formatBytes(425_000_000), "425 MB");
  assert.equal(formatBytes(1_250_000_000), "1.3 GB");
});
