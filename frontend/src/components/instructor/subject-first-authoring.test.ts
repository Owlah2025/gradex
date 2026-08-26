import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  getAuthoringSubject,
  listAuthoringInstitutions,
  programName,
  searchAuthoringSubjects,
  subjectContext,
  subjectLabel,
  type AuthoringSubject,
} from "../../lib/api/authoring-academic";
import { createCourse, resetRevisionAudience, setCourseSubject, setRevisionAudience } from "../../lib/api/authoring";
import { createSubjectRequest, listOwnSubjectRequests } from "../../lib/api/subject-requests";
import { isAcademicCourse } from "../../lib/api/catalog";

/**
 * Instructor Subject-first authoring (T4-B, D-091 §9, D-093 §1).
 *
 * Behavioural assertions drive the real client against a stubbed transport, so
 * route, method, CSRF, and body shape are proved rather than assumed.
 * Structural assertions hold for the shipped source, so a regression cannot
 * reintroduce a legacy taxonomy control or raw identifier through a path no
 * test happened to render.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function readSource(relativePath: string): string {
  const full = path.join(frontendRoot(), relativePath);
  assert.ok(fs.existsSync(full), `${relativePath} is missing; this detector would pass vacuously`);
  return fs.readFileSync(full, "utf8");
}

/**
 * Source with comments removed.
 *
 * The "not implemented" detectors below assert about code, and these files
 * deliberately explain in comments what they do NOT do — a comment saying a
 * preview must never write `course_program_targets` would otherwise read as an
 * implementation of it.
 */
function readCode(relativePath: string): string {
  return readSource(relativePath)
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/^\s*\/\/.*$/gm, " ");
}

const PICKER = "src/components/instructor/academic-subject-picker.tsx";
const CONTEXT = "src/components/instructor/academic-course-context.tsx";
const AUDIENCE = "src/components/instructor/revision-audience-editor.tsx";
const REQUEST_STATE = "src/components/instructor/subject-request-state.tsx";
const BUILDER = "src/components/instructor/course-builder.tsx";
const CLIENT = "src/lib/api/authoring-academic.ts";

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

const mappedSubject: AuthoringSubject = {
  id: "s-1",
  official_code: "0418-320",
  title_ar: "مبادئ نظم الحاسوب",
  title_en: "Principles of Computer Systems",
  unit_name_ar: "قسم علوم الحاسوب",
  unit_name_en: "Computer Science Department",
  college_name_ar: "كلية العلوم",
  college_name_en: "College of Science",
  programs: [
    { program_id: "p-1", name_ar: "علوم الحاسوب", name_en: "Computer Science", recommended_level: 2 },
    { program_id: "p-2", name_ar: "الأمن السيبراني", name_en: "Cybersecurity" },
  ],
};

const unmappedSubject: AuthoringSubject = {
  id: "s-2",
  official_code: "0418-466",
  title_ar: "مواضيع مختارة",
  title_en: "Selected Topics",
  programs: [],
};

// --- Instructor reads -----------------------------------------------------

test("the university list comes from the Instructor's own academic projection", async () => {
  const calls = await withStub(
    () => ok([{ id: "i-1", name_ar: "جامعة الكويت", name_en: "Kuwait University", country_code: "KW" }]),
    async () => {
      const institutions = await listAuthoringInstitutions("en");
      assert.equal(institutions.length, 1);
      assert.equal(institutions[0].name_en, "Kuwait University");
    },
  );
  assert.equal(calls.length, 1);
  assert.match(calls[0].url, /\/authoring\/academic\/institutions$/,
    "the Instructor must read its own projection, never the Admin catalog route");
  assert.ok(!calls[0].url.includes("/admin/"), "an Instructor must not reach an Admin route");
});

test("Subject search is scoped to the university and passes the query through", async () => {
  const calls = await withStub(
    () => ok([mappedSubject]),
    async () => {
      const found = await searchAuthoringSubjects({ institutionID: "i-1", query: "0418-320", locale: "en" });
      assert.equal(found[0].id, "s-1");
    },
  );
  assert.match(calls[0].url, /\/authoring\/academic\/institutions\/i-1\/subjects\?q=0418-320$/);
  assert.equal(calls[0].method, "GET", "search must be a GET, never a mutation");
});

test("a code and its normalized form reach the same server query", async () => {
  for (const query of ["0418-320", "0418320", "Principles of Computer Systems"]) {
    const calls = await withStub(
      () => ok([mappedSubject]),
      async () => {
        await searchAuthoringSubjects({ institutionID: "i-1", query, locale: "en" });
      },
    );
    // The client never normalizes: matching is the server's single
    // implementation, so the two surfaces cannot drift apart.
    assert.ok(calls[0].url.includes(encodeURIComponent(query)),
      `the raw query ${query} must reach the server unchanged`);
  }
});

test("a Subject reads as code and title, never as an identifier", () => {
  assert.equal(subjectLabel(mappedSubject, "en"), "0418-320 · Principles of Computer Systems");
  assert.equal(subjectLabel(mappedSubject, "ar"), "0418-320 · مبادئ نظم الحاسوب");
  assert.equal(subjectContext(mappedSubject, "en"), "Computer Science Department · College of Science");
  assert.equal(subjectContext(mappedSubject, "ar"), "قسم علوم الحاسوب · كلية العلوم");
  assert.ok(!subjectLabel(mappedSubject, "en").includes(mappedSubject.id),
    "an identifier must never appear in a label a person reads");
});

test("a codeless Subject still reads as its title", () => {
  const codeless: AuthoringSubject = { id: "s-3", title_ar: "مادة", title_en: "Untitled Code", programs: [] };
  assert.equal(subjectLabel(codeless, "en"), "Untitled Code");
});

test("the automatic audience is the Programs the catalog maps, in both languages", () => {
  assert.deepEqual(
    mappedSubject.programs.map((program) => programName(program, "en")),
    ["Computer Science", "Cybersecurity"],
  );
  assert.deepEqual(
    mappedSubject.programs.map((program) => programName(program, "ar")),
    ["علوم الحاسوب", "الأمن السيبراني"],
  );
  // Placement is present only where the catalog carries it.
  assert.equal(mappedSubject.programs[0].recommended_level, 2);
  assert.equal(mappedSubject.programs[1].recommended_level, undefined);
});

test("an unmapped Subject reports zero Programs rather than failing", () => {
  assert.deepEqual(unmappedSubject.programs, [],
    "a Subject with no Curriculum mapping is a legitimate Course Subject");
});

// --- Creation -------------------------------------------------------------

test("creating a Course sends the university and Subject and never a classification", async () => {
  const calls = await withStub(
    () => ok({ id: "c-1", classification_model: "ACADEMIC_CATALOG" }, 201),
    async () => {
      await createCourse({
        titleAr: "كورس", titleEn: "Course", descriptionAr: "وصف", descriptionEn: "Description",
        institutionID: "i-1", subjectID: "s-1", locale: "en", csrf: "csrf-1",
      });
    },
  );
  assert.equal(calls.length, 1);
  assert.equal(calls[0].method, "POST");
  assert.equal(calls[0].csrf, "csrf-1");
  const body = calls[0].body as Record<string, unknown>;
  assert.equal(body.institution_id, "i-1");
  assert.equal(body.subject_id, "s-1");
  assert.ok(!("classification_model" in body),
    "the client must never name a classification: the server derives it");
  for (const legacy of ["major_term_id", "subject_term_id", "study_year"]) {
    assert.ok(!(legacy in body), `a new Course must not carry ${legacy}`);
  }
});

test("correcting a Subject uses the Course's own route and carries CSRF", async () => {
  const calls = await withStub(
    () => ok({ id: "c-1", classification_model: "ACADEMIC_CATALOG", subject_id: "s-2" }),
    async () => {
      await setCourseSubject({ courseID: "c-1", subjectID: "s-2", locale: "en", csrf: "csrf-2" });
    },
  );
  assert.match(calls[0].url, /\/courses\/c-1\/subject$/);
  assert.equal(calls[0].method, "PUT");
  assert.equal(calls[0].csrf, "csrf-2");
  assert.deepEqual(calls[0].body, { subject_id: "s-2" });
});

test("a Course's stored Subject is resolvable without searching again", async () => {
  const calls = await withStub(
    () => ok(mappedSubject),
    async () => {
      const subject = await getAuthoringSubject({ institutionID: "i-1", subjectID: "s-1", locale: "en" });
      assert.equal(subject?.official_code, "0418-320");
    },
  );
  assert.match(calls[0].url, /\/authoring\/academic\/institutions\/i-1\/subjects\/s-1$/);
});

// --- Classification branching ---------------------------------------------

test("presentation branches on the server's classification, never on a null field", () => {
  assert.equal(isAcademicCourse({ classification_model: "ACADEMIC_CATALOG" }), true);
  assert.equal(isAcademicCourse({ classification_model: "LEGACY_TAXONOMY" }), false);
  // A Course with no classification is not assumed to be Academic.
  assert.equal(isAcademicCourse({}), false);

  const builder = readSource(BUILDER);
  assert.match(builder, /isAcademicCourse/,
    "the legacy panel must be gated on the classification the server returned");
  assert.ok(!/subject_id\s*===?\s*null|!course\.subject_id/.test(builder),
    "classification must never be inferred from whether a Subject happens to be null");
});

// --- Structural guarantees -------------------------------------------------

test("the new Course flow carries no legacy taxonomy vocabulary", () => {
  const picker = readSource(PICKER);
  for (const legacy of ["major_term_id", "subject_term_id", "study_year", "Major Term", "Study Year", "taxonomy"]) {
    assert.ok(!picker.includes(legacy),
      `the Subject-first flow must not mention ${legacy}`);
  }
});

test("the Instructor academic client exposes no Subject mutation", () => {
  const client = readSource(CLIENT);
  for (const forbidden of ["createSubject", "updateSubject", "retireSubject", "mapSubjectToCurriculum"]) {
    assert.ok(!client.includes(forbidden),
      `an Instructor must not be able to ${forbidden}`);
  }
  assert.ok(!client.includes("/admin/"),
    "the Instructor client must never address an Admin route");
  // Read-only by construction: no write verb appears at all.
  for (const verb of ['"POST"', '"PUT"', '"PATCH"', '"DELETE"']) {
    assert.ok(!client.includes(verb), `the academic read client must not issue ${verb}`);
  }
});

test("T4-C customization and reset use the revision-scoped API", async () => {
  const calls = await withStub(
    () => ok({ mode: "CUSTOMIZED", programs: [] }),
    async () => {
      await setRevisionAudience({
        courseID: "course-1", revisionID: "revision-1", programIDs: ["program-1"],
        locale: "en", csrf: "csrf-c",
      });
      await resetRevisionAudience({
        courseID: "course-1", revisionID: "revision-1", locale: "en", csrf: "csrf-c",
      });
    },
  );
  assert.equal(calls[0].method, "PUT");
  assert.deepEqual(calls[0].body, { program_ids: ["program-1"] });
  assert.equal(calls[1].method, "DELETE");
  assert.match(calls[0].url, /\/courses\/course-1\/revisions\/revision-1\/audience$/);

  // The controls are still offered and still bilingual; the copy now lives in the dictionaries
  // rather than as a pair of literals inside the component, so that is where it is asserted.
  const audience = readCode(AUDIENCE);
  assert.match(audience, /type="checkbox"/);
  assert.match(audience, /labels\.customize/);
  assert.match(audience, /labels\.useAutomatic/);
  assert.ok(
    !/isAr \?/.test(audience),
    "the audience editor must not branch its UI copy on the locale in place",
  );
  for (const dictionary of ["src/lib/i18n/dictionaries/en.ts", "src/lib/i18n/dictionaries/ar.ts"]) {
    const source = readSource(dictionary);
    for (const key of ["customize:", "useAutomatic:", "legend:"]) {
      assert.ok(source.includes(key), `${dictionary} is missing the audience ${key} vocabulary`);
    }
  }
});

test("T4-D Instructor request create/read are scoped to the Instructor routes", async () => {
  const calls = await withStub(
    (url) => ok(url.includes("course_id=") ? [] : { id: "request-1", status: "PENDING" }, url.includes("course_id=") ? 200 : 201),
    async () => {
      await createSubjectRequest({
        institutionID: "institution-1", courseID: "course-1",
        proposedTitleAr: "مادة", proposedTitleEn: "Subject",
        locale: "en", csrf: "csrf-d",
      });
      await listOwnSubjectRequests("en", "course-1");
    },
  );
  assert.equal(calls[0].method, "POST");
  assert.equal(calls[0].csrf, "csrf-d");
  assert.match(calls[0].url, /\/authoring\/academic\/subject-requests$/);
  assert.match(calls[1].url, /\/authoring\/academic\/subject-requests\?course_id=course-1$/);
  assert.ok(calls.every((call) => !call.url.includes("/admin/")));
});

test("the empty search state offers the live missing-Subject request action", () => {
  const picker = readSource(PICKER);
  assert.match(picker, /subject-empty/, "an empty search must have a visible state");
  const emptyRegion = picker.slice(picker.indexOf("subject-empty"));
  assert.match(emptyRegion, /request-subject/);
  assert.match(emptyRegion, /I can't find my Subject/);
  assert.match(emptyRegion, /لم أجد مادتي/);
  const requestState = readCode(REQUEST_STATE);
  assert.match(requestState, /Pending review/);
  assert.match(requestState, /subject-request-rejected/);
});

test("the published Subject is read-only context, not a disabled selector", () => {
  const context = readSource(CONTEXT);
  assert.match(context, /academic-course-subject-locked/,
    "a published Course must explain that its Subject is identity");
  assert.match(context, /live_revision_id/,
    "publication history must be read from the server's own field");
  assert.match(context, /academic-course-subject-in-review/,
    "a Course under review must explain why the Subject is frozen");
  // The correction control is rendered only when the lifecycle allows it.
  assert.match(context, /correctable && !editing/,
    "the Change Subject control must be conditional, not disabled");
});

test("Arabic and English product vocabulary is used, and identifiers are not", () => {
  const sources = [readSource(PICKER), readSource(CONTEXT), readSource(AUDIENCE), readSource(REQUEST_STATE)].join("\n");
  for (const term of ["الجامعة", "المادة", "التخصصات المرتبطة", "الجمهور التلقائي"]) {
    assert.ok(sources.includes(term), `Arabic product vocabulary must include ${term}`);
  }
  for (const english of ["University", "Subject", "Associated Programs", "Automatic audience"]) {
    assert.ok(sources.includes(english), `English vocabulary must include ${english}`);
  }
  for (const leak of ["UUID", "Revision ID", "Course ID", "معرّف"]) {
    assert.ok(!sources.includes(leak), `${leak} must never be product copy`);
  }
  // And no identifier is rendered as text. React list keys legitimately use an
  // id and are never displayed, so they are excluded before the check.
  const rendered = sources
    .replace(/key=\{[^}]*\}/g, " ")
    .replace(/[A-Za-z-]+=\{[^{}]*(\{[^{}]*\}[^{}]*)*\}/g, " ");
  for (const leak of ["{subject.id}", "{course.id}", "{revision.id}", "{course.live_revision_id}"]) {
    assert.ok(!rendered.includes(leak), `${leak} must never be rendered to a person`);
  }
});
