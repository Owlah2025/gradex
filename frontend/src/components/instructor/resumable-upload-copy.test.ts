import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { ResumeFileMismatchError, UploadAlreadyRunningError, type SavedUploadSummary } from "../../lib/api/media-multipart";
import {
  describeUploadError,
  remainingLabel,
  resumingLine,
  savedUploadCopy,
  transferLine,
} from "./resumable-upload-copy";

const paused: SavedUploadSummary = {
  fileName: "lecture03.mp4",
  sizeBytes: 625_000_000,
  completedBytes: 425_000_000,
  percent: 68,
  assetVersionID: "asset-1",
  status: "PAUSED",
  source: "server",
};

test("after a reload the saved upload shows its real progress and asks for the same file", () => {
  const en = savedUploadCopy(paused, "en", false);
  assert.equal(en.title, "Upload paused at 68%");
  assert.equal(en.detail, "425 MB of 625 MB is already safely uploaded.");
  assert.equal(en.instruction, "Reselect “lecture03.mp4” to continue. Completed parts will not be uploaded again.");
  assert.equal(en.action, "Choose file and resume");
  const ar = savedUploadCopy(paused, "ar", false);
  assert.match(ar.title, /68%/);
  assert.match(ar.detail!, /425 MB/);
  assert.match(ar.instruction, /lecture03\.mp4/);
  assert.equal(ar.action, "اختر الملف للاستكمال");
});

test("within the same tab the file is still available, so resuming needs no picker", () => {
  const en = savedUploadCopy(paused, "en", true);
  assert.equal(en.action, "Resume upload");
  assert.doesNotMatch(en.instruction, /Reselect/);
});

test("legacy checkpoints without a size show bytes, never an invented percentage", () => {
  const legacy = savedUploadCopy({ ...paused, fileName: null, sizeBytes: null, percent: null }, "en", false);
  assert.equal(legacy.title, "Upload paused");
  assert.equal(legacy.detail, "425 MB is already safely uploaded.");
  assert.match(legacy.instruction, /the same file/);
});

test("finished and cancelled saved uploads say so", () => {
  assert.match(savedUploadCopy({ ...paused, status: "FINISHING", percent: 100 }, "en", false).title, /verification pending/);
  assert.match(savedUploadCopy({ ...paused, status: "CANCELLED" }, "en", false).instruction, /Cancel the saved upload/);
});

test("live transfer line shows bytes, speed and time remaining", () => {
  assert.equal(
    transferLine({ reportedBytes: 425_000_000, totalBytes: 625_000_000 }, 8_400_000, 24, "en"),
    "425 MB / 625 MB · 8.4 MB/s · ~24 sec remaining",
  );
  assert.equal(transferLine({ reportedBytes: 1, totalBytes: 2 }, null, null, "en"), "1 B / 2 B");
  assert.equal(remainingLabel(150, "en"), "~3 min remaining");
  assert.equal(remainingLabel(3900, "en"), "~1 h 5 min remaining");
  assert.match(remainingLabel(24, "ar"), /24 ثانية/);
  assert.equal(resumingLine(paused, "en"), "Resuming from 68%…");
});

test("the wrong-file refusal names the file to pick in both languages", () => {
  const error = new ResumeFileMismatchError("lecture03.mp4");
  assert.equal(
    describeUploadError(error, "en"),
    "This is not the same file as the paused upload. Select “lecture03.mp4” to continue, or cancel the saved upload and start a new one.",
  );
  assert.match(describeUploadError(error, "ar"), /lecture03\.mp4/);
});

test("the Lesson video control resumes with the in-tab file, checks before resuming, and shows verification as its own phase", () => {
  const source = readFileSync(join(process.cwd(), "src/components/instructor/lesson-video-upload.tsx"), "utf8");
  assert.match(source, /setPhase\(resumable\.pending \? "CHECKING" : "PREPARING"\)/);
  assert.match(source, /if \(resumable\.verifying\) setPhase\("VERIFYING"\)/);
  assert.match(source, /const file = resumable\.lastFile\(\);\s*if \(file\) void run\(file\);\s*else fileInput\.current\?\.click\(\);/);
  assert.match(source, /phase === "FAILED" && !resumable\.pending/);
});

test("Arabic byte figures are bidi-isolated so the number stays with its unit", () => {
  const line = transferLine({ reportedBytes: 1_600_000, totalBytes: 25_000_000 }, 1_100_000, 34, "ar");
  assert.ok(line.startsWith("⁦1.6 MB / 25 MB⁩"), JSON.stringify(line));
  assert.ok(line.includes("⁦1.1 MB/s⁩"));
  const detail = savedUploadCopy(paused, "ar", false).detail!;
  assert.ok(detail.includes("⁦425 MB⁩") && detail.includes("⁦625 MB⁩"), JSON.stringify(detail));
  assert.ok(!transferLine({ reportedBytes: 1, totalBytes: 2 }, null, null, "en").includes("⁦"));
});

test("file names keep their own direction inside Arabic sentences", () => {
  const ar = savedUploadCopy({ ...paused, fileName: "محاضرة.mp4" }, "ar", false);
  assert.ok(ar.instruction.includes("⁨“محاضرة.mp4”⁩"), JSON.stringify(ar.instruction));
  assert.ok(savedUploadCopy(paused, "en", false).instruction.includes("“lecture03.mp4”"));
});

test("an upload running elsewhere is explained in both languages", () => {
  assert.equal(describeUploadError(new UploadAlreadyRunningError(), "en"), "This upload is already running in another tab or window.");
  assert.match(describeUploadError(new UploadAlreadyRunningError(), "ar"), /علامة تبويب/);
});

test("every caller of the shared hook shows verification as its own phase", () => {
  for (const relative of [
    "src/components/instructor/lesson-video-upload.tsx",
    "src/components/instructor/public-preview-upload.tsx",
    "src/components/instructor/lesson-resource-upload.tsx",
  ]) {
    const source = readFileSync(join(process.cwd(), relative), "utf8");
    assert.match(source, /if \(resumable\.verifying\) setPhase\("VERIFYING"\)/, relative);
    assert.match(source, /const file = resumable\.lastFile\(\);/, relative);
  }
});

test("the hook delegates its lifecycle to the executable session", () => {
  // The lifecycle itself is exercised in resumable-upload-session.test.ts.
  const source = readFileSync(join(process.cwd(), "src/components/instructor/resumable-upload-controls.tsx"), "utf8");
  assert.match(source, /new ResumableUploadSession\(browserDependencies, setState\)/);
  assert.match(source, /useEffect\(\(\) => \(\) => current\.dispose\(\), \[current\]\)/);
});
