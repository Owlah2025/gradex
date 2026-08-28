import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

const read = (relative: string) => fs.readFileSync(path.join(process.cwd(), relative), "utf8");

const component = read("src/components/instructor/lesson-resource-upload.tsx");
// Phase reporting, the retry affordance and the failure role are shared with the Lesson video
// control now, so that is where those guarantees are asserted.
const status = read("src/components/instructor/upload-status.tsx");

test("Lesson Resource authoring keeps the D-088 upload lifecycle and supports removal without identifier entry", () => {
  for (const required of [
    "validateSelectedResource(file, locale)",
    "beginResourceUpload({",
    "sha256Hex(file)",
    "completeUpload({",
    "waitForProcessing(ticket.asset_version_id, locale)",
    "addLessonFile({",
    "deleteLessonFile({",
    "ACCEPTED_RESOURCE_CONTENT_TYPES",
    "ACCEPTED_RESOURCE_EXTENSIONS",
    "phaseTestID={`lesson-resource-phase-${lessonID}`}",
    "data-testid={`remove-lesson-resource-${file.id}`}",
  ]) {
    assert.ok(component.includes(required), `missing Resource authoring lifecycle step: ${required}`);
  }
  assert.match(
    status,
    /role=\{failed \? "alert" : "status"\}/,
    "validation/upload failures must be announced",
  );
  assert.match(status, /labels\.retry/, "a failed upload must offer a retry");
  assert.match(
    status,
    /text-destructive/,
    "a failed upload must not be painted like a successful one",
  );
  for (const dictionary of ["src/lib/i18n/dictionaries/en.ts", "src/lib/i18n/dictionaries/ar.ts"]) {
    assert.ok(read(dictionary).includes("retry:"), `${dictionary} is missing the retry label`);
  }
  assert.doesNotMatch(
    component,
    /(?:placeholder|aria-label)=\{[^}]*?(?:Asset Version|UUID)/i,
    "the Instructor must never be asked to enter a raw identifier",
  );
});
