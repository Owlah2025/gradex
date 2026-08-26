import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  academicFacts,
  curriculumTotals,
  instructorInitials,
  outlineNeedsDisclosure,
  visibleSections,
  SECTION_PREVIEW_LIMIT,
} from "./course-detail-presentation";
import { en } from "../../lib/i18n/dictionaries/en";
import { ar } from "../../lib/i18n/dictionaries/ar";
import { formatFils } from "../../lib/formatters/currency";


/**
 * The rendered source of this surface, read from disk.
 *
 * These files are JSX, so the assertions below are about what the page is built out of rather than
 * about a value a function returns. Resolved from the repository root the same way the design-token
 * test does, because the compiled test does not sit beside its own source.
 */
function surface(file: string): string {
  const root = process.cwd().endsWith("/frontend")
    ? process.cwd()
    : join(process.cwd(), "frontend");
  return readFileSync(join(root, "src/components/catalog", file), "utf8");
}

const labels = {
  university: en.courseDetail.university,
  major: en.courseDetail.major,
  subject: en.courseDetail.subject,
  level: en.courseDetail.level,
};

const section = (position: number, lessonCount: number) => ({
  title: `Section ${position}`,
  position,
  lesson_count: lessonCount,
});

/**
 * The academic block is the page's answer to "is this course for me". Each term must arrive under
 * the name a student would use for it — the surface it replaces rendered all four as unlabelled
 * pills under an `sr-only` heading reading "Taxonomy".
 */
test("academic facts are named in student language, in study-plan order", () => {
  const facts = academicFacts(
    {
      university: { label: "Kuwait University" },
      major: { label: "Computer Engineering" },
      subject: { label: "Data Structures", code: "CPE-232" },
      study_year: { label: "Year 3" },
    },
    labels,
  );

  assert.deepEqual(
    facts.map((fact) => [fact.key, fact.label, fact.value]),
    [
      ["university", "University", "Kuwait University"],
      ["major", "Major", "Computer Engineering"],
      ["subject", "Subject", "Data Structures"],
      ["level", "Academic level", "Year 3"],
    ],
  );
  assert.equal(facts[2].code, "CPE-232");
});

/**
 * A public Course may legitimately carry none of these terms. Rendering "University: —" would tell
 * a reader only that the page expected something it did not get.
 */
test("an academic field the catalogue did not return is omitted, not blanked", () => {
  const facts = academicFacts({ subject: { label: "Statistics" } }, labels);
  assert.deepEqual(
    facts.map((fact) => fact.key),
    ["subject"],
  );
  assert.equal(facts[0].code, undefined);
  assert.deepEqual(academicFacts({}, labels), []);
});

/** Only Subject is code-bearing: the projection selects NULL for the university and major codes. */
test("only the subject carries an academic code", () => {
  const facts = academicFacts(
    {
      university: { label: "Kuwait University" },
      major: { label: "Computer Engineering" },
      subject: { label: "Data Structures", code: "CPE-232" },
    },
    labels,
  );
  assert.deepEqual(
    facts.filter((fact) => fact.code !== undefined).map((fact) => fact.key),
    ["subject"],
  );
});

test("curriculum totals count sections and their real lesson counts", () => {
  assert.deepEqual(curriculumTotals([section(1, 3), section(2, 2), section(3, 0)]), {
    sections: 3,
    lessons: 5,
  });
  assert.deepEqual(curriculumTotals([]), { sections: 0, lessons: 0 });
});

/**
 * The disclosure exists to shorten a long outline, not to hide a short one. A control that hides two
 * of six rows costs a click and saves nothing.
 */
test("a short outline is shown whole and offers no disclosure", () => {
  const short = Array.from({ length: SECTION_PREVIEW_LIMIT }, (_, index) =>
    section(index + 1, 2),
  );
  assert.equal(outlineNeedsDisclosure(short), false);
  assert.equal(visibleSections(short, false).length, SECTION_PREVIEW_LIMIT);
});

test("a long outline discloses the rest rather than truncating it away", () => {
  const long = Array.from({ length: SECTION_PREVIEW_LIMIT + 4 }, (_, index) =>
    section(index + 1, 1),
  );
  assert.equal(outlineNeedsDisclosure(long), true);
  assert.equal(visibleSections(long, false).length, SECTION_PREVIEW_LIMIT);
  assert.equal(visibleSections(long, true).length, long.length);
  // Collapsed shows a prefix — never a sample from the middle.
  assert.deepEqual(
    visibleSections(long, false).map((item) => item.position),
    long.slice(0, SECTION_PREVIEW_LIMIT).map((item) => item.position),
  );
});

/**
 * The avatar is the name restated, because a display name is the only thing the public contract
 * carries about this person. `toUpperCase` is a no-op on Arabic script, which is why one function
 * serves both languages.
 */
test("instructor initials come from the display name in both scripts", () => {
  assert.equal(instructorInitials("Fahd Al-Mutairi"), "FA");
  assert.equal(instructorInitials("  Layla   Nasser  Ahmed "), "LN");
  assert.equal(instructorInitials("Plato"), "P");
  assert.equal(instructorInitials("فهد المطيري"), "فا");
  assert.equal(instructorInitials("   "), "");
});

/**
 * Course Details must not carry a second opinion about money. The surface this replaces printed
 * `(minor_units / 1000).toFixed(3)` with the raw currency code beside it, which produced an
 * un-localized "12.500 KWD" in Arabic where the rest of the product reads "١٢.٥٠٠ د.ك".
 */
test("the access card prices a course through the canonical formatter", () => {
  const source = surface("course-access-summary.tsx");
  assert.ok(source.includes("formatFils("), "the access card must use formatFils");
  assert.ok(!source.includes("toFixed("), "no local price arithmetic on Course Details");
  assert.equal(formatFils(12500, "en"), "12.500 KWD");
  assert.equal(formatFils(12500, "ar"), "12.500 د.ك");
  assert.equal(formatFils(null, "ar"), "غير مخصص");
});

/**
 * The remediated surface must stay inside the token system. The page it replaces reached past it for
 * `bg-teal-800`/`text-teal-800`, a colour that exists in neither the Gradex ramp nor the semantic
 * set, so it tracked neither brand nor dark mode.
 */
test("the Course Details surface uses no raw palette outside the design tokens", () => {
  const files = [
    "course-detail.tsx",
    "course-detail-sections.tsx",
    "course-curriculum.tsx",
    "course-preview.tsx",
    "course-access-summary.tsx",
    "course-access-panel.tsx",
  ];
  // Tailwind's stock ramps. Everything on this surface must come from `gx.*` or the semantic set.
  const rawPalette =
    /\b(?:bg|text|border|ring|from|to|via|divide|outline)-(?:teal|slate|zinc|gray|neutral|stone|amber|rose|sky|emerald|indigo|violet|cyan|lime|fuchsia)-\d{2,3}\b/;

  for (const file of files) {
    const source = surface(file);
    const found = source.match(rawPalette);
    assert.equal(found, null, `${file} reaches past the design tokens: ${found?.[0]}`);
  }
});

/**
 * "Taxonomy" was rendered to screen readers in English on every Arabic page load. Nothing on this
 * surface may name an internal data structure, and no accessibility text may bypass the dictionary.
 */
test("no hardcoded Taxonomy label survives on Course Details", () => {
  for (const file of [
    "course-detail.tsx",
    "course-detail-sections.tsx",
    "course-curriculum.tsx",
  ]) {
    const source = surface(file);
    const markup = source.replace(/\/\*[\s\S]*?\*\//g, "");
    assert.ok(
      !/Taxonomy/.test(markup),
      `${file} still names a taxonomy in rendered output`,
    );
  }
});

/**
 * Every label the page draws must exist in both dictionaries. The type system already forces the
 * shape; this asserts the Arabic side is actually Arabic for the entries a visitor reads first.
 */
test("the Course Details copy is present and translated in both languages", () => {
  const keys = Object.keys(en.courseDetail) as (keyof typeof en.courseDetail)[];
  assert.ok(keys.length > 0);
  for (const key of keys) {
    assert.notEqual(ar.courseDetail[key].trim(), "", `${key} is blank in Arabic`);
    assert.ok(
      !/[A-Za-z]{4,}/.test(ar.courseDetail[key]),
      `${key} still reads as English in the Arabic dictionary`,
    );
  }
});

/**
 * The absent half of the contract, asserted as absence.
 *
 * `GET /api/v1/catalog/courses/{idOrSlug}` returns no rating, no review, no enrolment count, no
 * course duration and no last-updated date. A marketplace-shaped page is exactly where those get
 * invented, so the surface is checked for the vocabulary rather than trusted not to grow it.
 */
test("Course Details states no social proof the catalogue cannot support", () => {
  const invented =
    /\b(?:rating|ratings|review|reviews|bestseller|enrolled|enrolments|enrollments|students enrolled|stars?)\b/i;
  for (const file of [
    "course-detail.tsx",
    "course-detail-sections.tsx",
    "course-curriculum.tsx",
    "course-access-summary.tsx",
  ]) {
    const source = surface(file);
    // Comments explain why these are absent; only rendered code is judged.
    const markup = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");
    const found = markup.match(invented);
    assert.equal(found, null, `${file} introduces unsupported marketplace data: ${found?.[0]}`);
  }
});
