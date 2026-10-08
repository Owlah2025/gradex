import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { PartTransferError, ResumeFileMismatchError, UploadAlreadyRunningError, type SavedUploadSummary } from "../../lib/api/media-multipart";
import {
  isConnectionInterruption,
  describeUploadError,
  durablePercent,
  pausingLine,
  savedForResumeLine,
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
    "425 MB / 625 MB transferred · 8.4 MB/s · ~24 sec remaining",
  );
  assert.equal(transferLine({ reportedBytes: 1, totalBytes: 2 }, null, null, "en"), "1 B / 2 B transferred");
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
  assert.ok(line.startsWith("أُرسل ⁦1.6 MB / 25 MB⁩"), JSON.stringify(line));
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

test("saved-for-resume is shown only when it differs from what was sent, and never calls in-flight bytes safe", () => {
  const live = { totalBytes: 9_600_000, completedBytes: 1_211_392, transferredBytes: 7_711_392, reportedBytes: 7_711_392 };
  assert.equal(durablePercent(live), 12, "rounded down: a saved figure is never overstated");
  assert.equal(
    savedForResumeLine(live, "en"),
    "Saved for resume: 12% · 1.2 MB safely uploaded. Parts still transferring are lost if you refresh or close this page.",
  );
  const ar = savedForResumeLine(live, "ar")!;
  assert.match(ar, /محفوظ للاستكمال: 12%/);
  assert.ok(ar.includes("\u20661.2 MB\u2069"));
  // Everything sent is saved: nothing to distinguish.
  assert.equal(savedForResumeLine({ ...live, completedBytes: 7_711_392 }, "en"), null);
  // A retry dropped in-flight bytes but the bar holds: the saved line has no in-flight warning.
  assert.equal(
    savedForResumeLine({ totalBytes: 100, completedBytes: 40, transferredBytes: 40, reportedBytes: 70 }, "en"),
    "Saved for resume: 40% · 40 B safely uploaded.",
  );
  assert.equal(pausingLine("en"), "Finishing the current upload parts so your progress can be saved…");
  assert.match(pausingLine("ar"), /لحفظ تقدمك/);
});

test("the hook warns before leaving only while bytes are on the wire", () => {
  const source = readFileSync(join(process.cwd(), "src/components/instructor/resumable-upload-controls.tsx"), "utf8");
  assert.match(source, /const warnBeforeLeaving = transferInProgress\(state\);/);
  assert.match(source, /window\.addEventListener\("beforeunload", warn\);/);
  assert.match(source, /return \(\) => window\.removeEventListener\("beforeunload", warn\);/);
  assert.match(source, /onClick=\{upload\.stopNow\}/);
});

test("a pause with every part saved says so instead of 'paused at 100%'", () => {
  const all = savedUploadCopy({ ...paused, completedBytes: 625_000_000, percent: 100 }, "en", true);
  assert.equal(all.title, "Upload paused — every part is saved");
  assert.equal(all.instruction, "Resume to finish. Nothing will be uploaded again.");
  assert.match(savedUploadCopy({ ...paused, completedBytes: 625_000_000, percent: 100 }, "ar", false).title, /كل الأجزاء محفوظة/);
});

test("before any part is accepted the control says nothing is saved yet, not '0% · 0 B safely uploaded'", () => {
  const start = { totalBytes: 42_000_000, completedBytes: 0, transferredBytes: 1_200_000, reportedBytes: 1_200_000 };
  assert.equal(
    savedForResumeLine(start, "en"),
    "Nothing is saved for resume yet. Parts still transferring are lost if you refresh or close this page.",
  );
  assert.match(savedForResumeLine(start, "ar")!, /لم يُحفظ شيء للاستكمال بعد/);
});

test("the saved readout appears from the first in-flight byte, even below 1% of a large file", () => {
  const early = { totalBytes: 200_000_000, completedBytes: 0, transferredBytes: 1_000_000, reportedBytes: 1_000_000 };
  assert.equal(
    savedForResumeLine(early, "en"),
    "Nothing is saved for resume yet. Parts still transferring are lost if you refresh or close this page.",
  );
  const behindByUnderOnePercent = { totalBytes: 200_000_000, completedBytes: 100_000_000, transferredBytes: 100_500_000, reportedBytes: 100_500_000 };
  assert.match(savedForResumeLine(behindByUnderOnePercent, "en")!, /^Saved for resume: 50% · 100 MB safely uploaded\./);
});

test("a connection that gives out is an interruption with saved progress, not a raw 'timed out'", () => {
  const error = new PartTransferError("Part upload stalled: no data moved for 60 s", "stalled");
  assert.equal(isConnectionInterruption(error), true);
  assert.equal(isConnectionInterruption(new Error("Part upload failed with HTTP 403")), false);
  assert.equal(
    describeUploadError(error, "en"),
    "The upload stopped because the connection dropped or became too slow. Everything already uploaded is saved — resume to continue.",
  );
  assert.match(describeUploadError(error, "ar"), /كل ما رُفع محفوظ/);
  for (const relative of [
    "src/components/instructor/lesson-video-upload.tsx",
    "src/components/instructor/public-preview-upload.tsx",
    "src/components/instructor/lesson-resource-upload.tsx",
  ]) {
    const source = readFileSync(join(process.cwd(), relative), "utf8");
    assert.match(source, /if \(isConnectionInterruption\((error|cause)\)\) \{[^}]*setPhase\("IDLE"\)/, relative);
  }
});

test("an unavailable storage service is not blamed on the Instructor's connection", () => {
  const busy = new PartTransferError("Part upload failed with HTTP 503", "server");
  assert.equal(isConnectionInterruption(busy), true);
  assert.equal(
    describeUploadError(busy, "en"),
    "The upload stopped because the storage service is temporarily unavailable. Everything already uploaded is saved — resume shortly.",
  );
  assert.match(describeUploadError(busy, "ar"), /خدمة التخزين غير متاحة مؤقتاً/);
  assert.doesNotMatch(describeUploadError(busy, "en"), /connection/);
});
