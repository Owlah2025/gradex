import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";

test("material surfaces use the per-file authorization action without serializing media internals", () => {
  const root = process.cwd().endsWith("/frontend") ? process.cwd() : join(process.cwd(), "frontend");
  const views = readFileSync(join(root, "src/components/learning/learning-views.tsx"), "utf8");
  const lesson = readFileSync(join(root, "src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx"), "utf8");
  const action = readFileSync(join(root, "src/components/learning/material-download.tsx"), "utf8");
  assert.match(views, /resources: LearningMaterial\[\]/);
  assert.match(views, /labMaterials: LearningMaterial\[\]/);
  assert.match(views, /<MaterialDownload/);
  assert.match(views, /item\.title/);
  assert.match(views, /item\.file_type/);
  assert.match(views, /item\.size_bytes/);
  assert.match(lesson, /resources=\{lesson\.resources\}/);
  assert.match(lesson, /labMaterials=\{lesson\.lab_materials\}/);
  assert.match(action, /requestMaterialDownload\(authorizationPath/);
  assert.match(action, /window\.location\.assign\(issued\.url\)/);
  assert.match(action, /role="alert"/);
  assert.doesNotMatch(views, /asset_version_id|signed_url|object_key|storage_object/);
  assert.doesNotMatch(lesson, /asset_version_id|signed_url|object_key|storage_object/);
});
