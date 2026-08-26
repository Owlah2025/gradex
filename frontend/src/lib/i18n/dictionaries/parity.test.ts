import assert from "node:assert/strict";
import test from "node:test";
import { en } from "./en";
import { ar } from "./ar";

type Leaf = { path: string; value: string };

function leaves(node: unknown, prefix = ""): Leaf[] {
  if (typeof node === "string") return [{ path: prefix, value: node }];
  if (node && typeof node === "object") {
    return Object.entries(node as Record<string, unknown>).flatMap(([key, value]) =>
      leaves(value, prefix === "" ? key : `${prefix}.${key}`),
    );
  }
  return [];
}

const enLeaves = leaves(en);
const arLeaves = leaves(ar);

/**
 * The type system already forces `ar` to have `en`'s shape. What it cannot see is a key that was
 * added to one dictionary and left blank in the other, which renders as a silently missing label
 * rather than a compile error.
 */
test("both dictionaries carry the same keys, and none of them is empty", () => {
  assert.deepEqual(
    arLeaves.map((leaf) => leaf.path),
    enLeaves.map((leaf) => leaf.path),
  );
  const blank = [...enLeaves, ...arLeaves].filter((leaf) => leaf.value.trim() === "");
  assert.deepEqual(blank, [], "a dictionary entry is present but empty");
});

/**
 * The failure this guards against is specific and was made once already while writing these blocks:
 * an English string copied verbatim into the Arabic dictionary to satisfy an English test
 * assertion, which ships an untranslated label to every Arabic reader without failing anything.
 *
 * Scoped to the workspace blocks rather than the whole dictionary, because elsewhere an identical
 * string is sometimes correct — a brand name, a bare em dash, a version marker. Every entry here is
 * prose an Arabic reader is meant to read, so identity between the two is always a mistake.
 *
 * Short entries are exempt: "—" is the same placeholder in both languages by design.
 */
const TRANSLATED_BLOCKS = ["adminReviewQueue", "adminLifecycle", "instructor.studio", "adminCourses"];

test("workspace copy is actually translated, not copied across", () => {
  const untranslated: string[] = [];
  for (const [index, enLeaf] of enLeaves.entries()) {
    if (!TRANSLATED_BLOCKS.some((block) => enLeaf.path.startsWith(`${block}.`))) continue;
    // A one-word label can legitimately be a shared token; a phrase cannot.
    if (!enLeaf.value.includes(" ")) continue;
    if (arLeaves[index].value === enLeaf.value) untranslated.push(enLeaf.path);
  }
  assert.deepEqual(untranslated, [], "these entries are still in English in the Arabic dictionary");
});
