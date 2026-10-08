import assert from "node:assert/strict";
import test from "node:test";
import {
  ResumeFileMismatchError,
  type MultipartCompletionResult,
  type ResumableInput,
  type SavedUploadSummary,
  type UploadProgressListener,
} from "../../lib/api/media-multipart";
import {
  ResumableUploadSession,
  type ResumableState,
  type SessionDependencies,
  type SessionInput,
} from "./resumable-upload-session";

/*
  Executable lifecycle tests for the upload control, driven without React or a browser. The fake
  upload parks every run until the test settles it, so control switches, late answers and pauses can
  be interleaved deterministically.
*/

type Pending = {
  input: ResumableInput;
  file: File;
  onProgress?: UploadProgressListener;
  resolve: (result: MultipartCompletionResult) => void;
  reject: (error: unknown) => void;
};

const summary = (storageKeyId: string, fileName: string, percent = 40): SavedUploadSummary => ({
  fileName,
  sizeBytes: 100,
  completedBytes: percent,
  percent,
  assetVersionID: `asset-${storageKeyId}`,
  status: "PAUSED",
  source: "local",
});

function fixture() {
  const saved = new Map<string, SavedUploadSummary>();
  const refreshes: Array<{ storageKeyId: string; resolve: (value: SavedUploadSummary | null) => void }> = [];
  const runs: Pending[] = [];
  const cancelled: string[] = [];
  const deps: SessionDependencies = {
    upload: (file, input, onProgress) =>
      new Promise((resolve, reject) => {
        runs.push({ input, file, onProgress, resolve, reject });
        input.signal?.addEventListener("abort", () => reject(new DOMException("Upload paused", "AbortError")));
      }),
    hasSaved: (input) => saved.has(input.storageKeyId),
    readSaved: (input) => saved.get(input.storageKeyId) ?? null,
    refreshSaved: (input) =>
      new Promise((resolve) => refreshes.push({ storageKeyId: input.storageKeyId, resolve })),
    cancelSaved: async (input) => {
      cancelled.push(input.storageKeyId);
      saved.delete(input.storageKeyId);
    },
    acknowledgeSaved: (input) => {
      saved.delete(input.storageKeyId);
    },
    csrf: () => "csrf",
    describeError: (error) => String(error),
    now: () => Date.now(),
  };
  let state: ResumableState | null = null;
  const session = new ResumableUploadSession(deps, (next) => (state = next));
  return { saved, refreshes, runs, cancelled, session, state: () => state!, deps };
}

const control = (storageKeyId: string): SessionInput => ({
  courseID: "course-a",
  revisionID: "revision-a",
  lessonID: storageKeyId,
  kind: "VIDEO",
  storageKeyId,
  locale: "en",
});
const fileNamed = (name: string) => new File([new Uint8Array(100)], name, { type: "video/mp4" });
const settle = () => new Promise((resolve) => setImmediate(resolve));
const progress = (reportedBytes: number) => ({
  totalBytes: 100,
  completedBytes: 0,
  transferredBytes: reportedBytes,
  reportedBytes,
  resumedFromBytes: 0,
  retrying: false,
});

test("switching the control mid-upload: the old run's results and File never reach the new Lesson", async () => {
  const f = fixture();
  f.saved.set("video-b", summary("video-b", "b.mp4", 70));
  f.session.setInput(control("video-a"));
  const fractions: number[] = [];
  const runA = f.session.run(fileNamed("a.mp4"), (fraction) => fractions.push(fraction));
  await settle();
  assert.equal(f.state().running, true);
  assert.equal(f.session.lastFile()?.name, "a.mp4");
  const parked = f.runs[0];

  // The mounted control is now Lesson B.
  f.session.setInput(control("video-b"));
  assert.equal(f.state().running, false);
  assert.equal(f.state().fileAvailable, false, "A's File is not B's in-tab file");
  assert.equal(f.session.lastFile(), null);
  assert.equal(f.state().saved?.fileName, "b.mp4");
  assert.ok(parked.input.signal?.aborted, "A's transfer was stopped");

  // A's run settles late and keeps reporting: none of it reaches B.
  parked.onProgress?.(0.9, progress(90));
  await assert.rejects(runA, (error: unknown) => error instanceof DOMException && error.name === "AbortError");
  await settle();
  assert.deepEqual(fractions, [], "no progress from A after the switch");
  assert.equal(f.state().saved?.fileName, "b.mp4", "A's settling run did not repaint B's saved upload");
  assert.equal(f.state().pending, true);
  assert.equal(f.state().transfer, null);
  assert.equal(f.session.lastFile(), null, "Resume on B cannot send A's file");

  // A late server answer for A, and a stale one for B, are both ignored.
  for (const refresh of f.refreshes.filter((r) => r.storageKeyId === "video-a")) refresh.resolve(summary("video-a", "a.mp4", 10));
  await settle();
  assert.equal(f.state().saved?.fileName, "b.mp4");
});

test("a recovery answer is applied only if nothing newer happened", async () => {
  const f = fixture();
  f.saved.set("video-a", summary("video-a", "a.mp4", 40));
  f.session.setInput(control("video-a"));
  const first = f.refreshes[0];
  // A run starts before the server answers.
  const run = f.session.run(fileNamed("a.mp4"), () => undefined);
  first.resolve({ ...summary("video-a", "a.mp4", 40), status: "CANCELLED", source: "server" });
  await settle();
  assert.notEqual(f.state().saved?.status, "CANCELLED", "a pre-run answer cannot overwrite the running state");
  f.session.pause();
  await assert.rejects(run);
  await settle();
  // The pause re-read is current and is applied.
  const latest = f.refreshes.at(-1)!;
  latest.resolve({ ...summary("video-a", "a.mp4", 55), source: "server" });
  await settle();
  assert.equal(f.state().saved?.percent, 55);
  assert.equal(f.state().saved?.source, "server");
});

test("pause keeps the in-tab File for Resume; acknowledgement and a wrong file clear it", async () => {
  const f = fixture();
  f.session.setInput(control("video-a"));
  const run = f.session.run(fileNamed("a.mp4"), () => undefined);
  await settle();
  f.session.pause();
  await assert.rejects(run);
  assert.equal(f.state().fileAvailable, true);
  assert.equal(f.session.lastFile()?.name, "a.mp4");

  const wrong = f.session.run(fileNamed("other.mp4"), () => undefined);
  await settle();
  f.runs.at(-1)!.reject(new ResumeFileMismatchError("a.mp4"));
  await assert.rejects(wrong, (error: unknown) => error instanceof ResumeFileMismatchError);
  assert.equal(f.state().fileAvailable, false, "a refused file is never offered as the resume file");
  assert.equal(f.session.lastFile(), null);

  const ok = f.session.run(fileNamed("a.mp4"), () => undefined);
  await settle();
  f.runs.at(-1)!.resolve({
    state: "QUARANTINED",
    duplicate: false,
    asset_version_id: "asset-1",
    storage_object_key: "k",
    storage_object_version: 'etag:"x"',
    sha256_hex: "d".repeat(64),
    provider_event_id: "multipart:asset-1",
  });
  await ok;
  f.session.acknowledge("d".repeat(64));
  assert.equal(f.session.lastFile(), null);
  assert.equal(f.state().pending, false);
});

test("cancel clears only its own control, even if the control switches while cancelling", async () => {
  const f = fixture();
  f.saved.set("video-a", summary("video-a", "a.mp4"));
  f.saved.set("video-b", summary("video-b", "b.mp4"));
  f.session.setInput(control("video-a"));
  const cancelling = f.session.cancel();
  f.session.setInput(control("video-b"));
  await cancelling;
  assert.deepEqual(f.cancelled, ["video-a"]);
  assert.equal(f.state().cancelled, false, "B is not shown as cancelled");
  assert.equal(f.state().saved?.fileName, "b.mp4");
  assert.equal(f.state().cancelling, false);
});

test("an unmount stops the transfer, and a remount of the same control starts cleanly", async () => {
  const f = fixture();
  f.saved.set("video-a", summary("video-a", "a.mp4"));
  f.session.setInput(control("video-a"));
  const run = f.session.run(fileNamed("a.mp4"), () => undefined);
  await settle();
  f.session.dispose();
  await assert.rejects(run);
  assert.equal(f.session.lastFile(), null);
  const refreshesBefore = f.refreshes.length;
  f.session.setInput(control("video-a"));
  assert.equal(f.refreshes.length, refreshesBefore + 1, "the remount re-reads the saved upload");
  assert.equal(f.state().running, false);
  assert.equal(f.state().saved?.fileName, "a.mp4");
});
