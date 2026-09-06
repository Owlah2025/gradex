import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { recoverLessonVideoPhase } from "./lesson-video-upload-state";

const read = (relative: string) => readFileSync(join(process.cwd(), relative), "utf8");

test("server media state reconstructs Lesson video status after reload", () => {
  assert.equal(recoverLessonVideoPhase(undefined, undefined), "IDLE");
  assert.equal(recoverLessonVideoPhase("video-b", "PROCESSING"), "PROCESSING_BACKGROUND");
  assert.equal(recoverLessonVideoPhase("video-b", "READY"), "READY");
  assert.equal(recoverLessonVideoPhase("video-b", "PROCESS_FAILED"), "FAILED");
  assert.equal(recoverLessonVideoPhase("video-b", "SCAN_FAILED"), "FAILED");
  assert.equal(recoverLessonVideoPhase("video-b", "UPLOADED"), "FAILED");
});

test("Lesson video uses one durable completion, then one shared observation", () => {
  // Production regression: the original tab was the only actor attaching a video after processing.
  const source = read("src/components/instructor/lesson-video-upload.tsx");
  const complete = source.indexOf("await completeAndSelectLessonVideo({");
  const processing = source.indexOf('setPhase("PROCESSING")');
  assert.ok(complete >= 0 && processing > complete);
  assert.doesNotMatch(source, /await completeVideoUpload\(/);
  assert.doesNotMatch(source, /await claimLessonVideoUpload\(/);
  assert.doesNotMatch(source, /await setLessonVideo\(/);
  // Exactly one poll loop, owned by the watch and keyed on the *selected*
  // asset. An inline wait beside it would be a second loop racing the first,
  // and would keep running after this control unmounted.
  assert.match(source, /useProcessingWatch\(\{/);
  assert.doesNotMatch(source, /await waitForProcessing\(/);
});

test("the processing watch is per-asset, bounded, and cancelled on unmount", () => {
  const watch = read("src/components/instructor/use-processing-watch.ts");
  // Keyed on the asset, so two lessons processing at once cannot share a bar.
  assert.match(watch, /\[active, assetVersionID, locale, intervalMs, timeoutMs\]/);
  // The effect's teardown stops the loop; a late response is discarded.
  assert.match(watch, /return \(\) => \{\s*cancelled = true;/);
  assert.match(watch, /if \(cancelled\) return;/);
  // Terminal states end the watch rather than polling forever.
  assert.match(watch, /if \(isTerminalState\(status\.state\)\) \{/);
  assert.match(watch, /if \(Date\.now\(\) \+ intervalMs > deadline\) return;/);
});

test("a processing run that outlives the tab is background status, not a failure", () => {
  // Production regression: the observation bound was rendered as Upload failed / Try again.
  const upload = read("src/components/instructor/lesson-video-upload.tsx");
  const watch = read("src/components/instructor/use-processing-watch.ts");
  const status = read("src/components/instructor/upload-status.tsx");
  // Reaching the bound returns; it never reports a failure the server did not.
  assert.doesNotMatch(watch, /setPhase|FAILED/);
  // Only a terminal state the server actually reported produces FAILED.
  assert.match(upload, /onSettled: \(status\) => \{/);
  assert.match(upload, /if \(isReadyState\(status\.state\)\) \{/);
  assert.match(status, /\{failed && onRetry \?/);
  assert.doesNotMatch(status, /PROCESSING_BACKGROUND[\s\S]{0,100}onRetry/);
});

test("processing progress is measured, never invented", () => {
  const status = read("src/components/instructor/upload-status.tsx");
  const api = read("src/lib/api/media-upload.ts");
  // A percentage is rendered only from a server observation of an asset that
  // is genuinely PROCESSING.
  assert.match(api, /if \(!status \|\| status\.state !== "PROCESSING"\) return null;/);
  assert.match(api, /if \(!status\.processing_stage \|\| typeof percent !== "number"\) return null;/);
  // No timer anywhere advances a bar.
  assert.doesNotMatch(status, /setInterval|setTimeout/);
  // Indeterminate progress carries no aria value at all.
  assert.match(status, /aria-valuenow=\{determinate \? percent : undefined\}/);
  assert.match(status, /role="progressbar"/);
});

test("processing stage copy is localized in both dictionaries", () => {
  assert.match(
    read("src/lib/i18n/dictionaries/en.ts"),
    /TRANSCODING: "Transcoding and preparing playback"/,
  );
  assert.match(
    read("src/lib/i18n/dictionaries/ar.ts"),
    /TRANSCODING: "جارٍ تحويل الفيديو وتجهيزه للعرض"/,
  );
});

test("background-processing copy is localized in both dictionaries", () => {
  assert.match(
    read("src/lib/i18n/dictionaries/en.ts"),
    /Video uploaded successfully\. Processing continues in the background\. You can leave this page\./,
  );
  assert.match(
    read("src/lib/i18n/dictionaries/ar.ts"),
    /تم رفع الفيديو بنجاح\. تستمر المعالجة في الخلفية، ويمكنك مغادرة هذه الصفحة\./,
  );
});
