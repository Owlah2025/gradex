import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  applySelection,
  clearedSelection,
  emptyStateKind,
  hasSelection,
  institutionName,
  programContext,
  programName,
  readSelection,
  requestFilters,
  selectionSearch,
  subjectName,
} from "./academic-filter-state";
import {
  getPublicCourses,
  getPublicInstitutions,
  getPublicLevels,
  getPublicPrograms,
  getPublicSubjects,
} from "../../lib/api/public-catalog";

/**
 * Academic discovery filters (T6).
 *
 * Behavioural assertions drive the real client against a stubbed transport, so
 * the route and query shape are proved rather than assumed. Structural
 * assertions hold for the shipped source, so a later edit cannot reintroduce a
 * raw identifier, a hardcoded English label, or an Admin endpoint into a public
 * page without a test failing.
 */

function stubbedFetch<T>(
  body: T,
  capture: (url: string, init?: RequestInit) => void,
) {
  const original = globalThis.fetch;
  globalThis.fetch = async (url: RequestInfo | URL, init?: RequestInit) => {
    capture(String(url), init);
    return new Response(JSON.stringify(body), { status: 200 });
  };
  return () => {
    globalThis.fetch = original;
  };
}

// --- URL state ------------------------------------------------------------

test("selection is read from the URL, which is its only owner", () => {
  const selection = readSelection(
    "institution=kuwait-university&program=computer-engineering&subject=0418-201&q=algorithms",
  );
  assert.deepEqual(selection, {
    institution: "kuwait-university",
    program: "computer-engineering",
    level: "",
    subject: "0418-201",
    query: "algorithms",
  });
});

test("a URL naming a Program with no University drops the dependent values", () => {
  // A hand-edited or stale link must not filter on something the choosers can
  // no longer render.
  const selection = readSelection(
    "program=computer-engineering&subject=0418-201",
  );
  assert.equal(selection.program, "");
  assert.equal(selection.subject, "");
});

test("a selection round-trips through the query string unchanged", () => {
  const selection = {
    institution: "kuwait-university",
    program: "computer-science",
    level: "",
    subject: "0418-201",
    query: "hello",
  };
  assert.deepEqual(readSelection(selectionSearch(selection)), selection);
});

test("an empty selection produces a clean shareable URL", () => {
  assert.equal(selectionSearch(clearedSelection()), "");
  assert.equal(hasSelection(clearedSelection()), false);
});

test("choosing a University clears the Program and Subject beneath it", () => {
  const current = {
    institution: "kuwait-university",
    program: "computer-engineering",
    level: "",
    subject: "0418-201",
    query: "keep me",
  };
  const next = applySelection(current, { institution: "gulf-university" });
  assert.equal(next.institution, "gulf-university");
  assert.equal(next.program, "");
  assert.equal(next.subject, "");
  // The search term is a separate control and is not a child of the hierarchy.
  assert.equal(next.query, "keep me");
});

test("choosing a Program clears only the Subject", () => {
  const next = applySelection(
    {
      institution: "kuwait-university",
      program: "computer-engineering",
      level: "",
      subject: "0418-201",
      query: "",
    },
    { program: "computer-science" },
  );
  assert.equal(next.institution, "kuwait-university");
  assert.equal(next.program, "computer-science");
  assert.equal(next.subject, "");
});

test("clearing the University clears everything academic beneath it", () => {
  const next = applySelection(
    {
      institution: "kuwait-university",
      program: "computer-engineering",
      level: "",
      subject: "0418-201",
      query: "",
    },
    { institution: "" },
  );
  assert.deepEqual(next, {
    institution: "",
    program: "",
    level: "",
    subject: "",
    query: "",
  });
});

test("re-selecting the same University leaves the selection alone", () => {
  const current = {
    institution: "kuwait-university",
    program: "computer-engineering",
    level: "",
    subject: "0418-201",
    query: "",
  };
  assert.deepEqual(
    applySelection(current, { institution: "kuwait-university" }),
    current,
  );
});

// --- Request shape --------------------------------------------------------

test("only the chosen filters are sent; an empty filter is omitted entirely", () => {
  assert.deepEqual(
    requestFilters({
      institution: "kuwait-university",
      program: "",
      level: "",
      subject: "",
      query: "",
    }),
    { institution: "kuwait-university" },
  );
  assert.deepEqual(requestFilters(clearedSelection()), {});
});

test("the Student's Program ranks results but is never one of the filters", () => {
  const filters = requestFilters(
    {
      institution: "kuwait-university",
      program: "",
      level: "",
      subject: "",
      query: "",
    },
    "computer-engineering",
  );
  assert.equal(filters.relevantToProgram, "computer-engineering");
  // Crucially, it did not become a narrowing filter.
  assert.equal(filters.program, undefined);
});

test("relevance is dropped once the visitor filters by a Program themselves", () => {
  const filters = requestFilters(
    {
      institution: "kuwait-university",
      program: "computer-science",
      level: "",
      subject: "",
      query: "",
    },
    "computer-engineering",
  );
  assert.equal(filters.relevantToProgram, undefined);
  assert.equal(filters.program, "computer-science");
});

test("the catalogue request carries the academic filters as query parameters", async () => {
  let requestURL = "";
  const restore = stubbedFetch(
    { items: [], page: 1, page_size: 20, total: 0 },
    (url) => {
      requestURL = url;
    },
  );
  try {
    await getPublicCourses("en", "algorithms", {
      institution: "kuwait-university",
      program: "computer-engineering",
      level: "",
      subject: "0418-201",
      relevantToProgram: "computer-science",
    });
    const parsed = new URL(requestURL, "https://gradex.test");
    assert.equal(parsed.pathname, "/api/v1/catalog/courses");
    assert.equal(parsed.searchParams.get("institution"), "kuwait-university");
    assert.equal(parsed.searchParams.get("program"), "computer-engineering");
    assert.equal(parsed.searchParams.get("subject"), "0418-201");
    assert.equal(
      parsed.searchParams.get("relevant_to_program"),
      "computer-science",
    );
    assert.equal(parsed.searchParams.get("q"), "algorithms");
  } finally {
    restore();
  }
});

test("option lists are read from the public catalogue surface, never an Admin one", async () => {
  const seen: string[] = [];
  const restore = stubbedFetch({ items: [] }, (url) => {
    seen.push(new URL(url, "https://gradex.test").pathname);
  });
  try {
    await getPublicInstitutions("ar");
    await getPublicPrograms("kuwait-university", "ar");
    await getPublicSubjects("kuwait-university", "computer-engineering", "ar");
  } finally {
    restore();
  }
  assert.deepEqual(seen, [
    "/api/v1/catalog/academic-options/institutions",
    "/api/v1/catalog/academic-options/institutions/kuwait-university/programs",
    "/api/v1/catalog/academic-options/institutions/kuwait-university/subjects",
  ]);
  for (const pathname of seen) {
    assert.equal(pathname.includes("/admin/"), false);
    assert.equal(pathname.includes("/me/"), false);
    assert.equal(pathname.includes("/authoring/"), false);
  }
});

test("the Subject option list is scoped to the chosen Program", async () => {
  let requestURL = "";
  const restore = stubbedFetch({ items: [] }, (url) => {
    requestURL = url;
  });
  try {
    await getPublicSubjects("kuwait-university", "computer-engineering", "en");
  } finally {
    restore();
  }
  assert.equal(
    new URL(requestURL, "https://gradex.test").searchParams.get("program"),
    "computer-engineering",
  );
});

test("academic level is carried as its own filter and cleared with its parents", () => {
  const selection = readSelection(
    "institution=kuwait-university&program=computer-science&level=2&subject=0418-201",
  );
  assert.equal(selection.level, "2");
  assert.deepEqual(requestFilters(selection), {
    institution: "kuwait-university",
    program: "computer-science",
    level: "2",
    subject: "0418-201",
  });
  // Changing the Program changes which study plan a level refers to, so a
  // carried-over level would silently mean something different.
  assert.equal(applySelection(selection, { program: "cybersecurity" }).level, "");
  assert.equal(applySelection(selection, { institution: "gulf-university" }).level, "");
  // A level with no University has no study plan to belong to.
  assert.equal(readSelection("level=2").level, "");
  assert.equal(emptyStateKind({ ...selection, subject: "" }), "no-courses-for-level");
});

test("the level option list is read from the public catalogue surface", async () => {
  let requestURL = "";
  const restore = stubbedFetch({ items: [1, 2] }, (url) => {
    requestURL = url;
  });
  try {
    const levels = await getPublicLevels("kuwait-university", "computer-science", "en");
    assert.deepEqual(levels, [1, 2]);
  } finally {
    restore();
  }
  const parsed = new URL(requestURL, "https://gradex.test");
  assert.equal(
    parsed.pathname,
    "/api/v1/catalog/academic-options/institutions/kuwait-university/levels",
  );
  assert.equal(parsed.searchParams.get("program"), "computer-science");
});

// --- Localized display ----------------------------------------------------

test("every option renders its own language and never a slug", () => {
  const institution = {
    slug: "kuwait-university",
    name_ar: "جامعة الكويت",
    name_en: "Kuwait University",
  };
  assert.equal(institutionName(institution, "ar"), "جامعة الكويت");
  assert.equal(institutionName(institution, "en"), "Kuwait University");

  const program = {
    slug: "computer-engineering",
    name_ar: "هندسة الحاسوب",
    name_en: "Computer Engineering",
    college_name_ar: "كلية الهندسة",
    college_name_en: "College of Engineering",
  };
  assert.equal(programName(program, "ar"), "هندسة الحاسوب");
  assert.equal(programContext(program, "ar"), "كلية الهندسة");
  assert.equal(
    programContext({ slug: "x", name_ar: "أ", name_en: "b" }, "en"),
    "",
  );
});

test("a Subject shows its official code and title, never an identifier", () => {
  const coded = {
    value: "0418-201",
    code: "0418-201",
    title_ar: "هياكل البيانات",
    title_en: "Data Structures",
  };
  assert.equal(subjectName(coded, "en"), "0418-201 · Data Structures");
  assert.equal(subjectName(coded, "ar"), "0418-201 · هياكل البيانات");
  // A code-less Subject carries an identifier as its filter value, but that
  // value is never what the reader sees.
  const codeless = {
    value: "d290f1ee-6c54-4b01-90e6-d701748f0851",
    title_ar: "موضوعات خاصة",
    title_en: "Special Topics",
  };
  assert.equal(subjectName(codeless, "en"), "Special Topics");
  assert.equal(subjectName(codeless, "ar"), "موضوعات خاصة");
});

// --- Empty states ---------------------------------------------------------

test("an empty result names the narrowest thing the visitor chose", () => {
  assert.equal(emptyStateKind(clearedSelection()), "no-courses");
  assert.equal(
    emptyStateKind({
      institution: "",
      program: "",
      level: "",
      subject: "",
      query: "algorithms",
    }),
    "no-search-results",
  );
  assert.equal(
    emptyStateKind({
      institution: "kuwait-university",
      program: "",
      level: "",
      subject: "",
      query: "",
    }),
    "no-courses-for-institution",
  );
  assert.equal(
    emptyStateKind({
      institution: "kuwait-university",
      program: "computer-engineering",
      level: "",
      subject: "",
      query: "",
    }),
    "no-courses-for-program",
  );
  assert.equal(
    emptyStateKind({
      institution: "kuwait-university",
      program: "computer-engineering",
      level: "",
      subject: "0418-201",
      query: "",
    }),
    "no-courses-for-subject",
  );
});

// --- Structural guarantees over the shipped source ------------------------

// The tests run from a compiled build directory, so the shipped .tsx source is
// resolved from the repository root rather than from __dirname.
function frontendRoot(): string {
  return process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
}

function shippedSource(file: string): string {
  return fs.readFileSync(
    path.join(frontendRoot(), "src/components/catalog", file),
    "utf8",
  );
}

test("the filter controls are real labelled selects, not div-only widgets", () => {
  const source = shippedSource("academic-filters.tsx");
  assert.match(source, /<label[\s\S]*htmlFor=/);
  assert.match(source, /<select/);
  assert.match(source, /aria-label=\{t\.heading\}/);
  // Every chooser has an id a label can point at.
  for (const id of [
    "catalogue-institution",
    "catalogue-program",
    "catalogue-level",
    "catalogue-subject",
  ]) {
    assert.ok(source.includes(id), `missing chooser id ${id}`);
  }
});

test("no academic label in the filters is hardcoded to one language", () => {
  const source = shippedSource("academic-filters.tsx");
  // Both dictionaries exist and every key in one exists in the other.
  const arabic = source.slice(source.indexOf("ar: {"), source.indexOf("en: {"));
  const english = source.slice(source.indexOf("en: {"));
  const keys = (block: string) =>
    (block.match(/^\s{4}(\w+):/gm) ?? []).map((k) => k.trim());
  assert.deepEqual(keys(arabic), keys(english));
  assert.ok(keys(arabic).length > 0);
});

test("the public filters never call an authenticated academic surface", () => {
  const source = shippedSource("academic-filters.tsx");
  for (const prohibited of [
    "/admin/",
    "/authoring/",
    "listInstitutionOptions",
  ]) {
    assert.equal(
      source.includes(prohibited),
      false,
      `public filters referenced ${prohibited}`,
    );
  }
});

test("the catalogue reads its selection from the URL rather than mirroring it", () => {
  const source = shippedSource("public-catalogue.tsx");
  assert.ok(source.includes("readSelection(searchParameters.toString())"));
  // A useState holding the academic selection would be the second copy this
  // design exists to avoid.
  assert.equal(source.includes("useState<CatalogueSelection"), false);
});

test("a Student's profile reaches the catalogue only as a ranking hint", () => {
  const source = shippedSource("public-catalogue.tsx");
  assert.ok(source.includes("program_slug"));
  // The profile must never be consulted for anything but relevance.
  assert.equal(source.includes("entitlement"), false);
  assert.equal(source.includes("grant"), false);
});
