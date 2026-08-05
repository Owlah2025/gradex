import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { materialEntryPath } from "./material-links";

test("material links use fixed same-origin S4 routes", () => {
  assert.equal(
    materialEntryPath("lesson/id", "resource"),
    "/api/v1/media/lessons/lesson%2Fid/materials/resource",
  );
  assert.equal(
    materialEntryPath("lesson/id", "lab_material"),
    "/api/v1/media/lessons/lesson%2Fid/materials/lab-material",
  );
});

test("material surfaces use ordinary fixed anchors without prefetch or signed data", () => {
  const root = process.cwd().endsWith("/frontend") ? process.cwd() : join(process.cwd(), "frontend");
  const views = readFileSync(join(root, "src/components/learning/learning-views.tsx"), "utf8");
  const lesson = readFileSync(join(root, "src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx"), "utf8");
  assert.match(views, /const href = materialEntryPath\(lessonID, kind\)/);
  assert.match(views, /<a\s+href=\{href\}/);
  assert.doesNotMatch(views, /prefetch|requestDownload|asset_version_id|signed_url|object_key/);
  assert.doesNotMatch(lesson, /requestDownload|asset_version_id|signed_url|object_key/);
});
