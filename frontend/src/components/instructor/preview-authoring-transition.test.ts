import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

/**
 * Guards over the preview authoring transition.
 *
 * Public preview is a Lesson permission now. The separate course-level upload
 * predates that and is retained only so courses that already have one keep
 * working. These detectors pin the part that is easy to regress: which of the
 * two surfaces an Instructor is actually taught to use.
 *
 * The regression they exist to catch is the old uploader drifting back to being
 * the obvious control — either by being rendered unconditionally again, or by
 * keeping its generic "Public preview" title while sitting beside the new panel.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function readSource(relativePath: string): string {
  const full = path.join(frontendRoot(), relativePath);
  assert.ok(fs.existsSync(full), `${relativePath} is missing; this detector would pass vacuously`);
  return fs.readFileSync(full, "utf8");
}

const BUILDER = "src/components/instructor/course-builder.tsx";
const GUIDANCE = "src/components/instructor/lesson-preview-guidance.tsx";
const UPLOADER = "src/components/instructor/public-preview-upload.tsx";

test("a course with no legacy preview is never offered the old uploader", () => {
  const builder = readSource(BUILDER);
  // The uploader is mounted only behind the presence of legacy preview data, so
  // a new course cannot reach it at all.
  assert.match(
    builder,
    /\{revision\.preview_asset_version_id \? \(\s*<PublicPreviewUpload/,
    "the legacy uploader must be conditional on existing legacy preview data",
  );
  // And when it is mounted, it is mounted as the legacy surface.
  assert.match(builder, /<PublicPreviewUpload[\s\S]{0,400}?legacy/);
});

test("the Lesson preview workflow is the unconditional primary surface", () => {
  const builder = readSource(BUILDER);
  assert.match(builder, /<LessonPreviewGuidance copy=\{instructor\.media\.preview\} \/>/);
  // It must not be hidden behind the same legacy condition as the uploader.
  const guidanceAt = builder.indexOf("<LessonPreviewGuidance");
  const conditionAt = builder.indexOf("{revision.preview_asset_version_id ? (");
  assert.ok(guidanceAt > 0 && conditionAt > 0, "both preview surfaces must be present");
  assert.ok(
    guidanceAt < conditionAt,
    "the Lesson preview workflow must lead, before the legacy compatibility surface",
  );
});

test("the guidance points at the curriculum and writes nothing itself", () => {
  const guidance = readSource(GUIDANCE);
  assert.match(guidance, /advance\("PREVIEW"\)/);
  // The Lesson permission belongs to the lesson, so this panel must not acquire
  // its own mutation path.
  assert.doesNotMatch(guidance, /fetch\(|beginPublicPreviewUpload|allow_public_preview\s*[:=]/);
});

test("the legacy surface is labelled legacy in both languages", () => {
  const uploader = readSource(UPLOADER);
  assert.match(uploader, /legacy \? t\.legacyTitle : t\.title/);
  assert.match(uploader, /legacy \? t\.legacyDescription : t\.description/);

  for (const locale of ["en", "ar"] as const) {
    const dictionary = readSource(`src/lib/i18n/dictionaries/${locale}.ts`);
    for (const key of [
      "lessonFirstTitle",
      "lessonFirstDescription",
      "lessonFirstAction",
      "legacyTitle",
      "legacyDescription",
    ]) {
      assert.match(
        dictionary,
        new RegExp(`${key}:`),
        `${locale} is missing ${key}; the transition copy must be bilingual`,
      );
    }
  }

  const en = readSource("src/lib/i18n/dictionaries/en.ts");
  assert.match(en, /legacyTitle: "Legacy course preview"/);
  // The legacy copy must tell the Instructor where new previews belong.
  assert.match(en, /legacyDescription:[\s\S]{0,200}lessons/i);
  const ar = readSource("src/lib/i18n/dictionaries/ar.ts");
  assert.match(ar, /legacyTitle: "معاينة المقرر القديمة"/);
  assert.match(ar, /legacyDescription:[\s\S]{0,220}المنهج/);
});

test("the transition changes presentation only and mutates no legacy data", () => {
  const builder = readSource(BUILDER);
  const guidance = readSource(GUIDANCE);
  // Nothing here may clear the legacy pointer, delete a PREVIEW asset, or
  // auto-map the legacy preview onto a Lesson.
  for (const source of [builder, guidance]) {
    assert.doesNotMatch(source, /preview_asset_version_id\s*[:=]\s*null/);
    assert.doesNotMatch(source, /removePublicPreview\(\s*\)/);
  }
  // The uploader retains its full management surface for legacy courses.
  const uploader = readSource(UPLOADER);
  assert.match(uploader, /t\.remove/);
  assert.match(uploader, /t\.replace/);
});

test("the guidance is reachable by keyboard and announced as a section", () => {
  const guidance = readSource(GUIDANCE);
  // A real button, not a click handler on a div.
  assert.match(guidance, /<button\s+type="button"/);
  assert.match(guidance, /focus-visible:outline/);
  // Named region rather than an anonymous block.
  assert.match(guidance, /aria-labelledby="lesson-preview-guidance-title"/);
  assert.match(guidance, /id="lesson-preview-guidance-title"/);
  // The decorative icon is hidden from assistive technology.
  assert.match(guidance, /<Clapperboard aria-hidden/);
  // The directional arrow is mirrored for Arabic; the clapper board is not.
  assert.match(guidance, /<ArrowRight aria-hidden[^/]*rtl:-scale-x-100/);
  assert.doesNotMatch(guidance, /<Clapperboard[^/]*rtl:-scale-x-100/);
});
