import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  createCurriculum,
  createInstitution,
  createProgram,
  createSubject,
  createUnit,
  duplicateSubjectConflict,
  listCurriculumSubjects,
  listInstitutions,
  listSubjects,
  listUnits,
  mapSubjectToCurriculum,
  retireSubject,
  subjectLabel,
  unmapSubjectFromCurriculum,
} from "../../lib/api/academic";
import { ProblemError } from "../../lib/api/problem";

/**
 * Admin Academic Catalog (AD13, D-091 T1).
 *
 * Behavioural assertions drive the real client against a stubbed transport, so
 * route, method, CSRF, and body shape are proved rather than assumed.
 * Structural assertions hold for the shipped source, so a regression cannot
 * sneak a UUID or a legacy taxonomy term back into the Admin workflow through a
 * path no test happened to render.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function readSource(relativePath: string): string {
  const full = path.join(frontendRoot(), relativePath);
  assert.ok(fs.existsSync(full), `${relativePath} is missing; this detector would pass vacuously`);
  return fs.readFileSync(full, "utf8");
}

const COMPONENT = "src/components/admin/academic-catalog.tsx";
const CLIENT = "src/lib/api/academic.ts";

type Captured = { url: string; method?: string; csrf: string | null; body: unknown };

async function withStub(
  responder: (url: string, init?: RequestInit) => Response,
  run: () => Promise<void>,
): Promise<Captured[]> {
  const originalFetch = globalThis.fetch;
  const captured: Captured[] = [];
  globalThis.fetch = async (url, init) => {
    const request = String(url);
    captured.push({
      url: request,
      method: init?.method,
      csrf: new Headers(init?.headers).get("X-CSRF-Token"),
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    });
    // The client bootstraps an anonymous browser session before any call.
    if (request.endsWith("/session/bootstrap")) {
      return new Response(JSON.stringify({ csrf_token: "csrf-boot" }), { status: 200 });
    }
    return responder(request, init);
  };
  try {
    await run();
  } finally {
    globalThis.fetch = originalFetch;
  }
  return captured.filter((entry) => !entry.url.endsWith("/session/bootstrap"));
}

const ok = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

test("an empty catalog is a normal state, not an error", async () => {
  let received: unknown = "unset";
  await withStub(
    () => ok([]),
    async () => {
      received = await listInstitutions("en");
    },
  );
  assert.deepEqual(received, [], "an empty catalog must resolve to an empty list");
});

test("creating a university sends the Admin route, CSRF, and level bounds", async () => {
  const calls = await withStub(
    () => ok({ id: "i1" }, 201),
    async () => {
      await createInstitution({
        locale: "en",
        csrf: "csrf-1",
        countryCode: "KW",
        slug: "kuwait-university",
        nameAr: "جامعة الكويت",
        nameEn: "Kuwait University",
        maxAcademicLevel: 5,
        hasFoundationStage: false,
      });
    },
  );
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "/api/v1/admin/academic/institutions");
  assert.equal(calls[0].method, "POST");
  assert.equal(calls[0].csrf, "csrf-1");
  assert.deepEqual(calls[0].body, {
    country_code: "KW",
    slug: "kuwait-university",
    name_ar: "جامعة الكويت",
    name_en: "Kuwait University",
    // Five levels, not four: the bound is institution data, never a constant.
    max_academic_level: 5,
    has_foundation_stage: false,
  });
});

test("hierarchy navigation reads units, programs, and subjects per institution", async () => {
  const calls = await withStub(
    () => ok([]),
    async () => {
      await listUnits("i1", "en");
      await listSubjects("i1", "en", "0410-101");
      await listCurriculumSubjects("c1", "ar");
    },
  );
  assert.deepEqual(
    calls.map((entry) => `${entry.method} ${entry.url}`),
    [
      "GET /api/v1/admin/academic/institutions/i1/units",
      "GET /api/v1/admin/academic/institutions/i1/subjects?q=0410-101",
      "GET /api/v1/admin/academic/curricula/c1/subjects",
    ],
  );
});

test("a unit with no parent is sent as an explicit null, not omitted", async () => {
  const calls = await withStub(
    () => ok({ id: "u1" }, 201),
    async () => {
      await createUnit({
        locale: "en",
        csrf: "csrf-2",
        institutionID: "i1",
        kind: "SERVICE_UNIT",
        slug: "integrative-studies",
        nameAr: "الدراسات التكاملية",
        nameEn: "Integrative Studies",
      });
    },
  );
  assert.equal(calls[0].url, "/api/v1/admin/academic/institutions/i1/units");
  assert.deepEqual((calls[0].body as { parent_unit_id: unknown }).parent_unit_id, null);
});

test("creating a major and a study plan use their own semantic routes", async () => {
  const calls = await withStub(
    () => ok({ id: "x" }, 201),
    async () => {
      await createProgram({
        locale: "en",
        csrf: "c",
        institutionID: "i1",
        slug: "computer-engineering",
        nameAr: "هندسة الحاسوب",
        nameEn: "Computer Engineering",
        degreeKind: "BSC",
        owningUnitID: "u2",
      });
      await createCurriculum({
        locale: "en",
        csrf: "c",
        programID: "p1",
        versionLabel: "2026",
        supersedeActive: true,
      });
    },
  );
  assert.equal(calls[0].url, "/api/v1/admin/academic/institutions/i1/programs");
  assert.equal(calls[1].url, "/api/v1/admin/academic/programs/p1/curricula");
  assert.equal((calls[1].body as { supersede_active: boolean }).supersede_active, true);
});

test("a duplicate Subject conflict surfaces the existing Subject", async () => {
  const existing = {
    id: "s-existing",
    institution_id: "i1",
    official_code: "0410-101",
    title_ar: "حساب ١",
    title_en: "Calculus I",
  };
  let caught: unknown = null;
  await withStub(
    () =>
      ok(
        {
          type: "https://api.gradex.com/problems/state-conflict",
          title: "State conflict",
          status: 409,
          code: "SUBJECT_ALREADY_EXISTS",
          existing_subject: existing,
        },
        409,
      ),
    async () => {
      try {
        await createSubject({
          locale: "en",
          csrf: "c",
          institutionID: "i1",
          titleAr: "حساب ١",
          titleEn: "Calculus I",
          officialCode: "0410101",
        });
      } catch (error) {
        caught = error;
      }
    },
  );
  assert.ok(caught instanceof ProblemError, "a duplicate must reject, never silently succeed");
  const named = duplicateSubjectConflict(caught);
  assert.ok(named, "the conflict must name the existing Subject so the Admin can act on it");
  assert.equal(named?.id, "s-existing");
  // An unrelated failure must not be misread as a duplicate.
  assert.equal(duplicateSubjectConflict(new Error("network down")), null);
});

test("retiring a Subject and unmapping it are distinct commands", async () => {
  const calls = await withStub(
    (url) => (url.includes("/retire") ? ok({ id: "s1", retired_at: "now" }) : new Response(null, { status: 204 })),
    async () => {
      await retireSubject({ locale: "en", csrf: "c", subjectID: "s1" });
      await unmapSubjectFromCurriculum({ locale: "en", csrf: "c", curriculumID: "c1", subjectID: "s1" });
    },
  );
  assert.deepEqual(
    calls.map((entry) => `${entry.method} ${entry.url}`),
    [
      "POST /api/v1/admin/academic/subjects/s1/retire",
      "DELETE /api/v1/admin/academic/curricula/c1/subjects/s1",
    ],
  );
});

test("mapping a Subject carries requirement metadata and an explicit null level", async () => {
  const calls = await withStub(
    () => ok({ id: "m1" }, 201),
    async () => {
      await mapSubjectToCurriculum({
        locale: "en",
        csrf: "c",
        curriculumID: "c1",
        subjectID: "s1",
        requirementKind: "MAJOR_CORE",
      });
    },
  );
  assert.deepEqual(calls[0].body, {
    subject_id: "s1",
    requirement_kind: "MAJOR_CORE",
    recommended_level: null,
    recommended_semester: null,
    credits: null,
  });
});

test("a Subject label leads with the university's own official code", () => {
  const coded = {
    id: "s1",
    institution_id: "i1",
    official_code: "0410-101",
    title_ar: "حساب ١",
    title_en: "Calculus I",
  };
  assert.equal(subjectLabel(coded, "en"), "0410-101 · Calculus I");
  assert.equal(subjectLabel(coded, "ar"), "0410-101 · حساب ١");
  const codeless = { id: "s2", institution_id: "i1", title_ar: "ندوة", title_en: "Seminar" };
  assert.equal(subjectLabel(codeless, "en"), "Seminar");
});

/**
 * The vocabulary is asserted where it now lives.
 *
 * These labels used to be 53 `isAr ?` ternaries inside the component, which is exactly the shape
 * that let a third Arabic word for Course grow unnoticed elsewhere in this tranche. They are in the
 * dictionaries, so this reads the dictionaries — and additionally holds the component to having no
 * inline UI copy left to drift.
 */
test("the Admin surface renders localized academic vocabulary in both languages", () => {
  const arabicDictionary = readSource("src/lib/i18n/dictionaries/ar.ts");
  const englishDictionary = readSource("src/lib/i18n/dictionaries/en.ts");
  for (const arabic of ["الجامعة", "الكلية", "القسم", "التخصص", "الخطة الدراسية", "المادة"]) {
    assert.ok(arabicDictionary.includes(arabic), `the Arabic label ${arabic} is missing`);
  }
  for (const english of ["University", "College", "Department", "Major", "Study plan", "Subject"]) {
    assert.ok(englishDictionary.includes(english), `the English label ${english} is missing`);
  }

  const source = readSource(COMPONENT);
  assert.ok(
    source.includes("dictionary.adminCatalog"),
    "the Admin surface must read its copy from the dictionary",
  );
  const inlineCopy = source.match(/isAr \? "/g) ?? [];
  assert.deepEqual(
    inlineCopy,
    [],
    "the Admin surface still carries inline bilingual copy, which is how vocabulary drifts",
  );
});

test("the Admin surface never exposes identifiers or legacy taxonomy vocabulary as workflow", () => {
  const source = readSource(COMPONENT);

  // The prior taxonomy panel printed `revision_id: {uuid}` on screen. Nothing
  // here may reintroduce a *rendered* identifier. Attribute bindings such as
  // value={institutionID} and key={unit.id} are form/React plumbing, not
  // workflow the Admin reads, so only JSX text children are inspected.
  const jsxText = source.replace(/[A-Za-z-]+=\{[^{}]*(\{[^{}]*\}[^{}]*)*\}/g, "");
  assert.ok(
    !/\{\s*(institutionID|programID|curriculumID|[A-Za-z]+\.id)\s*\}/.test(jsxText),
    "the Admin surface renders a raw identifier as visible text",
  );
  assert.ok(!/revision_id\s*:/.test(source), "the Admin surface renders a revision identifier");
  assert.ok(
    !/uuid|UUID/.test(source),
    "the Admin surface mentions UUIDs, which must never be user workflow",
  );

  // The rule governs user-facing copy, so code comments are excluded: a comment
  // may legitimately explain what the surface deliberately does not do.
  const visible = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");
  for (const legacy of ["taxonomy", "Taxonomy", "تصنيف", "study_year", "major_term", "subject_term"]) {
    assert.ok(
      !visible.includes(legacy),
      `the Admin surface uses the superseded term "${legacy}" in user-facing copy; D-091 retired it`,
    );
  }

  // Every empty state must be handled, because T1 ships no catalog data at all.
  for (const emptyState of ["emptyCatalog", "emptyUnits", "emptyPrograms", "emptySubjects", "emptyCurriculum"]) {
    assert.ok(source.includes(emptyState), `the empty state ${emptyState} is not handled`);
  }
});

test("the Admin surface hardcodes no institution and no fixed academic-year model", () => {
  const source = readSource(COMPONENT) + readSource(CLIENT);
  for (const name of ["Kuwait University", "جامعة الكويت", "KUWAIT_UNIVERSITY"]) {
    assert.ok(!source.includes(name), `T1 hardcodes ${name}; launch catalog data belongs to T2`);
  }
  for (const forbidden of ["YEAR_1", "YEAR_2", "YEAR_3", "YEAR_4", "PREP"]) {
    assert.ok(!source.includes(forbidden), `the surface reintroduces the retired ${forbidden} enumeration`);
  }
  // The level bound must come from the selected institution, never a literal.
  assert.ok(
    readSource(COMPONENT).includes("selectedInstitution.max_academic_level"),
    "the level input is not bounded by the selected institution's own maximum",
  );
});

test("the Academic Catalog is reachable from Admin navigation and separate from Course review", () => {
  const navigation = readSource("src/components/layout/role-workspace-navigation.ts");
  assert.ok(navigation.includes("academicCatalog"), "Admin navigation has no Academic Catalog entry");
  assert.ok(
    navigation.includes("/admin/academic-catalog"),
    "the Academic Catalog entry does not point at its own route",
  );
  assert.ok(navigation.includes("courseReview"), "the Course review entry was removed");

  const page = readSource("src/app/[locale]/admin/academic-catalog/page.tsx");
  assert.ok(page.includes("AcademicCatalog"), "the Academic Catalog route renders nothing");
  assert.ok(
    !page.includes("ReviewQueue"),
    "the Academic Catalog route renders Course review; the two surfaces must stay separate",
  );

  const ar = readSource("src/lib/i18n/dictionaries/ar.ts");
  const en = readSource("src/lib/i18n/dictionaries/en.ts");
  assert.ok(ar.includes("الكتالوج الأكاديمي"), "the Arabic navigation label is missing");
  assert.ok(en.includes("Academic Catalog"), "the English navigation label is missing");
});

test("the Academic Catalog client exposes no public or Instructor surface in T1", () => {
  const client = readSource(CLIENT);
  const routes = client.match(/`\$\{base\}[^`]*`/g) ?? [];
  assert.ok(routes.length > 0, "no routes were found; this detector would pass vacuously");
  assert.ok(
    client.includes('const base = "/admin/academic"'),
    "the client no longer targets the Admin-only base path",
  );
  // authenticatedRequest supplies /api/v1; repeating it here 404s every call.
  assert.ok(!client.includes('"/api/v1/admin/academic"'), "the client double-prefixes /api/v1");
  // T3/T4/T6 surfaces must not appear early.
  for (const premature of ["/me/academic-profile", "subject-requests", "instructor/subjects"]) {
    assert.ok(!client.includes(premature), `the client opens ${premature}, which belongs to a later tranche`);
  }
});
