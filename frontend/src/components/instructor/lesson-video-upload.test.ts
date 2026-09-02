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

test("Lesson video uses one durable completion before bounded observation", () => {
  // Production regression: the original tab was the only actor attaching a video after processing.
  const source = read("src/components/instructor/lesson-video-upload.tsx");
  const complete = source.indexOf("await completeAndSelectLessonVideo({");
  const observe = source.indexOf("await waitForProcessing(");
  assert.ok(complete >= 0 && observe > complete);
  assert.doesNotMatch(source, /await completeVideoUpload\(/);
  assert.doesNotMatch(source, /await claimLessonVideoUpload\(/);
  assert.doesNotMatch(source, /await setLessonVideo\(/);
});

test("processing observation expiry is background status without a retry action", () => {
  // Production regression: the ten-minute observation bound was rendered as Upload failed / Try again.
  const upload = read("src/components/instructor/lesson-video-upload.tsx");
  const status = read("src/components/instructor/upload-status.tsx");
  assert.match(upload, /error instanceof ProcessingObservationTimeoutError/);
  assert.match(upload, /setPhase\("PROCESSING_BACKGROUND"\)/);
  assert.match(status, /\{failed && onRetry \?/);
  assert.doesNotMatch(status, /PROCESSING_BACKGROUND[\s\S]{0,100}onRetry/);
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
