import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

const component = fs.readFileSync(path.join(process.cwd(), "src/components/instructor/lesson-resource-upload.tsx"), "utf8");

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
    "data-testid={`lesson-resource-phase-${lessonID}`}",
    "data-testid={`remove-lesson-resource-${file.id}`}",
    "Retry upload",
  ]) {
    assert.ok(component.includes(required), `missing Resource authoring lifecycle step: ${required}`);
  }
  assert.match(component, /role=\{phase === "FAILED" \? "alert" : "status"\}/, "validation/upload failures must be announced");
  assert.doesNotMatch(
    component,
    /(?:placeholder|aria-label)=\{[^}]*?(?:Asset Version|UUID)/i,
    "the Instructor must never be asked to enter a raw identifier",
  );
});
