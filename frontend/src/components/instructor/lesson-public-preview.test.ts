import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

/**
 * Guards over the Instructor free-preview control.
 *
 * The decision this control takes is publication: ticking it means anyone will be able to watch
 * that lesson once the version is approved. The regressions that matter are the ones that would
 * make it lie about when that happens, or make it unusable at exactly the moment an Instructor
 * reaches for it.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function readSource(relativePath: string): string {
  const full = path.join(frontendRoot(), relativePath);
  assert.ok(fs.existsSync(full), `${relativePath} is missing; this detector would pass vacuously`);
  return fs.readFileSync(full, "utf8");
}

const TOGGLE = "src/components/instructor/lesson-public-preview-toggle.tsx";

test("the control is a labelled checkbox rather than a bare clickable element", () => {
  const toggle = readSource(TOGGLE);
  assert.match(toggle, /type="checkbox"/);
  // A real label, associated by id, so the words are the accessible name and clicking them works.
  assert.match(toggle, /htmlFor=\{checkboxID\}/);
  assert.match(toggle, /id=\{checkboxID\}/);
  // The helper text explaining approval is announced with the control, not left as nearby prose.
  assert.match(toggle, /aria-describedby=\{helpID\}/);
  assert.match(toggle, /id=\{helpID\}/);
});

test("the helper text says the flag takes effect only after approval", () => {
  const en = readSource("src/lib/i18n/dictionaries/en.ts");
  const ar = readSource("src/lib/i18n/dictionaries/ar.ts");
  // The single most misleading thing this control could do is imply the lesson is public the moment
  // it is ticked. Both languages have to say otherwise.
  assert.match(en, /publicPreviewHelp:/);
  assert.match(en, /after an administrator approves this version/);
  assert.match(ar, /publicPreviewHelp:/);
  assert.match(ar, /بعد موافقة المشرف/);
  // And it says what stays paid, because "free preview" otherwise reads as the whole lesson.
  assert.match(en, /resources and lab materials stay for students only/);
});

test("a lesson with no video cannot be offered free, and says why", () => {
  const toggle = readSource(TOGGLE);
  assert.match(toggle, /disabled=\{saving \|\| !hasVideo\}/);
  assert.match(toggle, /labels\.publicPreviewNeedsVideo/);
});

test("a video that is still processing does not disable the control", () => {
  const toggle = readSource(TOGGLE);
  const builder = readSource("src/components/instructor/curriculum-builder.tsx");
  // The builder's own `hasVideo` additionally requires READY. Passing it here would force the
  // Instructor to return and tick the box again after processing, which is the step people forget —
  // and forgetting it publishes a course without the free lesson they believed they had offered.
  assert.match(builder, /hasVideo=\{Boolean\(lesson\.video_asset_version_id\)\}/);
  // Instead the state is reported, so the absence of a public player does not read as a failure.
  assert.match(toggle, /hasVideo && allowed && !ready/);
  assert.match(toggle, /labels\.publicPreviewProcessing/);
});

test("an optimistic tick is reverted when the server refuses", () => {
  const toggle = readSource(TOGGLE);
  // The control may look ahead of the server for its own appearance, but it must never leave a
  // checked box behind a write that failed.
  assert.match(toggle, /setAllowed\(!next\)/);
  assert.match(toggle, /setAllowed\(Boolean\(updated\.allow_public_preview\)\)/);
  // And it never sends an unauthenticated mutation.
  assert.match(toggle, /const csrf = currentCSRFToken\(\)/);
  assert.match(toggle, /if \(!csrf\)/);
});

test("the authoring call targets the candidate revision route", () => {
  const authoring = readSource("src/lib/api/authoring.ts");
  assert.match(authoring, /export async function setLessonPublicPreview/);
  assert.match(authoring, /lessons\/\$\{encodeURIComponent\(input\.lessonID\)\}\/public-preview/);
  assert.match(authoring, /\{ allow_public_preview: input\.allow \}/);
  // Same revision-scoped path helper every other candidate mutation uses, so it cannot be pointed
  // at a live revision by construction.
  assert.match(authoring, /path\.revision\(input\.courseID, input\.revisionID\)/);
});

test("the Admin inspector names the consequence on every flagged lesson", () => {
  const inspector = readSource("src/components/admin/submitted-revision-inspector.tsx");
  const en = readSource("src/lib/i18n/dictionaries/en.ts");
  // Per lesson, not a summary: many lessons in one version may carry it, and the Administrator is
  // approving each of them.
  assert.match(inspector, /lesson\.allow_public_preview \?/);
  assert.match(inspector, /submitted-lesson-public-preview-\$\{lesson\.id\}/);
  assert.match(inspector, /copy\.lessonPublicPreview/);
  // Worded as what will happen, not as a field name.
  assert.match(en, /anyone will be able to watch this lesson/);
});
