import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

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

test("public preview authoring binds upload and commands to the revision and rejects non-ready processing", () => {
  assert.match(source, /beginPublicPreviewUpload\(\{[\s\S]{0,160}courseID,[\s\S]{0,160}revisionID,/);
  assert.match(source, /await setPublicPreview\(\{ courseID, revisionID, assetVersionID: ticket\.asset_version_id/);
  assert.match(source, /await clearPublicPreview\(\{ courseID, revisionID,/);
  assert.match(source, /if \(!isReadyState\(status\.state\)\)/);
  assert.match(source, /setMessage\(describeAssetState\(status\.state, locale\)\)/);
});

test("public preview authoring renders no raw preview identifier", () => {
  const renderedSurface = source.slice(source.indexOf("return ("));
  assert.doesNotMatch(renderedSurface, /assetVersion|asset_version_id|storage_object_key/i);
});
