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
const TRANSLATED_BLOCKS = ["adminReviewQueue", "adminLifecycle", "instructor.studio", "adminCourses", "academicContext", "academicProfile", "courseDetail"];

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

/**
 * One word per concept, enforced.
 *
 * The product had accumulated a second Arabic word for its central object (`دورة` beside `مقرر`),
 * for the person who writes one (`مدرّب` beside `مدرّس`), and for the people who take it (`الطلاب`
 * beside `الطلبة`) — in each case because a later tranche wrote copy without checking what the
 * earlier ones had said. The choices are recorded at the top of `ar.ts`; this is what keeps the
 * next block of copy from quietly reopening them.
 *
 * `دورة حياة` is exempt and is the reason the word is stripped before matching rather than matched
 * in context: there the word means *cycle*, and "دورة حياة المقرر" is correct Arabic for a Course
 * lifecycle.
 *
 * The boundary is a lookahead for the Arabic block, not `\b`. JavaScript defines a word boundary
 * over `[A-Za-z0-9_]` only, so no Arabic letter is ever one — `/دورة\b/` matches nothing an Arabic
 * sentence contains, and a guard written that way passes over a dictionary that has not been
 * converted at all.
 */
const ARABIC = "\u0600-\u06FF";
const RETIRED: { word: RegExp; instead: string; why: string }[] = [
  {
    word: new RegExp(`دور(ة|ات|اتك)(?![${ARABIC}])`, "u"),
    instead: "مقرر",
    why: "دورة is the training-course register; Gradex teaches university courses",
  },
  { word: /مدرّب/u, instead: "مدرّس", why: "مدرّب is a trainer, not a lecturer" },
  {
    word: new RegExp(`(^|[^${ARABIC}])(ال)?طلاب(?![${ARABIC}])`, "u"),
    instead: "الطلبة",
    why: "the product settled on الطلبة",
  },
];

test("the Arabic dictionary uses one word per concept", () => {
  const offences: string[] = [];
  for (const leaf of arLeaves) {
    const text = leaf.value.replace(/دورة حياة/gu, "");
    for (const retired of RETIRED) {
      if (retired.word.test(text)) {
        offences.push(`${leaf.path}: "${leaf.value}" — use ${retired.instead} (${retired.why})`);
      }
    }
  }
  assert.deepEqual(offences, [], "retired vocabulary is back:\n" + offences.join("\n"));
});
