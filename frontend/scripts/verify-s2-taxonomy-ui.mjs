import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");

const screens = [
  ["instructor", "src/components/instructor/taxonomy-assignment-panel.tsx"],
  ["admin override", "src/components/admin/taxonomy-override-form.tsx"],
  ["admin vocabulary", "src/components/admin/taxonomy-term-management.tsx"],
];

for (const [name, relativePath] of screens) {
  const source = await readFile(resolve(root, relativePath), "utf8");
  assert.match(source, /useLocale/, `${name} must read the active locale`);
  assert.match(source, /locale === "ar"/, `${name} must provide Arabic and English copy`);
  assert.match(source, /md:grid-cols/, `${name} must reflow from tablet width upward`);
}

for (const relativePath of [
  "src/components/instructor/course-builder.tsx",
  "src/components/admin/review-queue.tsx",
]) {
  const source = await readFile(resolve(root, relativePath), "utf8");
  assert.match(source, /dir=\{dir\}/, `${relativePath} must apply locale direction to its S2 surface`);
}

console.log("S2 taxonomy structural UI evidence: Arabic/English locale, RTL/LTR direction, and responsive grids verified.");
