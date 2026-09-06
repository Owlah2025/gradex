import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { recoverMediaPhase } from "./media-upload-phase";

const read = (relative: string) => readFileSync(join(process.cwd(), relative), "utf8");

const source = read("src/components/instructor/public-preview-upload.tsx");

test("public preview authoring exposes selected, replace, remove, and localized absence states", () => {
  assert.match(source, /hasPreview \? t\.selected : t\.absent/);
  assert.match(source, /hasPreview \? t\.replace : t\.choose/);
  assert.match(source, /onClick=\{\(\) => void remove\(\)\}/);
  // The copy moved out of a local bilingual table and into the shared dictionaries, so both
  // languages are asserted where they now live.
  assert.match(
    read("src/lib/i18n/dictionaries/en.ts"),
    /absent: "No public preview is attached to this revision\."/,
  );
  assert.match(
    read("src/lib/i18n/dictionaries/ar.ts"),
    /absent: "لا توجد معاينة عامة مرفقة بهذه المراجعة\."/,
  );
  assert.ok(
    !/isAr \?/.test(source),
    "the public preview surface must not branch its UI copy on the locale in place",
  );
});

test("public preview authoring binds upload and commands to the revision", () => {
  assert.match(source, /beginPublicPreviewUpload\(\{[\s\S]{0,160}courseID,[\s\S]{0,160}revisionID,/);
  assert.match(
    source,
    /completeAndSelectPublicPreview\(\{[\s\S]{0,160}courseID,[\s\S]{0,160}revisionID,/,
  );
  assert.match(source, /await clearPublicPreview\(\{ courseID, revisionID,/);
});

test("public preview uses one durable completion before one shared observation", () => {
  // D-096 regression: the preview now takes the FFmpeg path, so the browser
  // must not be the thing that attaches it after processing finishes.
  const complete = source.indexOf("await completeAndSelectPublicPreview({");
  const processing = source.indexOf('setPhase("PROCESSING")');
  assert.ok(complete >= 0, "the preview must be completed and selected in one server operation");
  assert.ok(processing > complete, "observation must follow durable selection, never precede it");
  assert.doesNotMatch(source, /await setPublicPreview\(/);
  assert.doesNotMatch(source, /import \{[^}]*setPublicPreview/);
  // One poll loop, shared with the Lesson video control and keyed on this
  // revision's own preview asset.
  assert.match(source, /useProcessingWatch\(\{[\s\S]{0,120}assetVersionID: previewAssetVersionID,/);
  assert.doesNotMatch(source, /await waitForProcessing\(/);
});

test("a preview run that outlives the tab is background status, not upload failure", () => {
  // The watch simply stops at its bound; nothing turns that into a failure.
  const watch = read("src/components/instructor/use-processing-watch.ts");
  assert.doesNotMatch(watch, /FAILED/);
  assert.match(source, /setMessage\(t\.processingBackground\)/);
  // FAILED is reached only from a terminal state the server reported.
  assert.match(source, /if \(isReadyState\(settledStatus\.state\)\) \{/);
});

test("preview processing progress is the server's own measurement", () => {
  assert.match(source, /processing\s*\?\s*`\$\{t\.processing\} \$\{processing\.percent\}%`/);
  assert.match(source, /data-processing-percent=/);
  assert.doesNotMatch(source, /setInterval/);
});

test("a superseded preview upload does not replace the selected winner", () => {
  assert.match(source, /if \(!completion\.selected\)/);
  assert.match(source, /setMessage\(t\.superseded\)/);
  // Superseded is not a failure and offers no retry that would upload again.
  const branch = source.slice(source.indexOf("if (!completion.selected)"));
  assert.match(branch.slice(0, 300), /setPhase\("IDLE"\)/);
});

test("a terminal processing failure is reported as the real failure", () => {
  assert.match(source, /if \(isReadyState\(settledStatus\.state\)\) \{/);
  assert.match(source, /setMessage\(describeAssetState\(settledStatus\.state, locale\)\)/);
  assert.match(source, /setPhase\("FAILED"\)/);
});

test("server media state reconstructs public preview status after reload", () => {
  assert.equal(recoverMediaPhase(undefined, undefined), "IDLE");
  assert.equal(recoverMediaPhase("preview-b", "VALIDATED"), "PROCESSING_BACKGROUND");
  assert.equal(recoverMediaPhase("preview-b", "PROCESSING"), "PROCESSING_BACKGROUND");
  assert.equal(recoverMediaPhase("preview-b", "READY"), "READY");
  assert.equal(recoverMediaPhase("preview-b", "PROCESS_FAILED"), "FAILED");
  assert.equal(recoverMediaPhase("preview-b", "SCAN_FAILED"), "FAILED");
  assert.equal(recoverMediaPhase("preview-b", "UPLOADED"), "FAILED");
});

test("reload recovery is driven by server-projected preview state", () => {
  assert.match(source, /recoverMediaPhase\(previewAssetVersionID, previewAssetState\)/);
  assert.match(source, /previewAssetState\?: string;/);
  // The builder has to hand the projection down, or the recovery reads nothing.
  const builder = read("src/components/instructor/course-builder.tsx");
  assert.match(builder, /previewAssetVersionID=\{revision\.preview_asset_version_id\}/);
  assert.match(builder, /previewAssetState=\{revision\.preview_asset_state\}/);
  assert.match(read("src/lib/api/catalog.ts"), /preview_asset_state\?: string;/);
});

test("background-processing copy is localized in both dictionaries", () => {
  assert.match(
    read("src/lib/i18n/dictionaries/en.ts"),
    /Public preview uploaded successfully\. Processing continues in the background\. You can leave this page\./,
  );
  assert.match(
    read("src/lib/i18n/dictionaries/ar.ts"),
    /تم رفع المعاينة العامة بنجاح\. تستمر المعالجة في الخلفية، ويمكنك مغادرة هذه الصفحة\./,
  );
});

test("public preview authoring renders no raw preview identifier", () => {
  const renderedSurface = source.slice(source.indexOf("return ("));
  assert.doesNotMatch(renderedSurface, /assetVersion|asset_version_id|storage_object_key/i);
});
