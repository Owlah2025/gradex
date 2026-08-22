import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  academicLevelLabels,
  getAcademicProfile,
  listCollegeOptions,
  listInstitutionOptions,
  listProgramOptions,
  saveAcademicProfile,
  shouldPromptOnboarding,
  skipAcademicOnboarding,
  type AcademicProfile,
} from "../../lib/api/academic-profile";

/**
 * Student academic profile (D-092, T3).
 *
 * Behavioural assertions drive the real client against a stubbed transport, so
 * route, method, CSRF, and body shape are proved. Structural assertions hold
 * for the shipped source, so no regression can reintroduce a hardcoded Program
 * list, a Department step, or an onboarding route guard through a path no test
 * happened to render.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function readSource(relativePath: string): string {
  const full = path.join(frontendRoot(), relativePath);
  assert.ok(fs.existsSync(full), `${relativePath} is missing; this detector would pass vacuously`);
  return fs.readFileSync(full, "utf8");
}

const FORM = "src/components/learning/academic-profile-form.tsx";
const CLIENT = "src/lib/api/academic-profile.ts";
const PROMPT = "src/components/learning/academic-profile-prompt.tsx";

type Captured = { url: string; method?: string; csrf: string | null; body: unknown };

async function withStub(
  responder: (url: string) => Response,
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
    return responder(request);
  };
  try {
    await run();
  } finally {
    globalThis.fetch = originalFetch;
  }
  return captured.filter((entry) => !entry.url.endsWith("/session/bootstrap"));
}

const ok = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });

test("a Student with no decision is NOT_STARTED and is invited to onboard", async () => {
  let profile: AcademicProfile | null = null;
  await withStub(
    () => ok({ setup_state: "NOT_STARTED" }),
    async () => {
      profile = await getAcademicProfile("ar");
    },
  );
  assert.equal(profile!.setup_state, "NOT_STARTED");
  assert.equal(shouldPromptOnboarding(profile), true);
});

test("a Student who deferred is never nagged again", () => {
  // The whole reason SKIPPED is its own state rather than an empty profile.
  assert.equal(shouldPromptOnboarding({ setup_state: "SKIPPED" }), false);
  assert.equal(shouldPromptOnboarding({ setup_state: "COMPLETED" }), false);
  assert.equal(shouldPromptOnboarding(null), false);
});

test("onboarding options cascade University → College → Major", async () => {
  const calls = await withStub(
    () => ok([]),
    async () => {
      await listInstitutionOptions("en");
      await listCollegeOptions("inst-1", "en");
      await listProgramOptions("inst-1", "college-1", "en");
    },
  );
  assert.deepEqual(
    calls.map((entry) => `${entry.method} ${entry.url}`),
    [
      "GET /api/v1/me/academic-options/institutions",
      "GET /api/v1/me/academic-options/institutions/inst-1/colleges",
      "GET /api/v1/me/academic-options/institutions/inst-1/programs?college_id=college-1",
    ],
  );
  // Majors are always scoped to a College; there is no unscoped Program listing.
  assert.ok(!calls.some((entry) => entry.url.endsWith("/programs")));
});

test("saving an enrolled profile never sends a curriculum", async () => {
  const calls = await withStub(
    () => ok({ setup_state: "COMPLETED" }),
    async () => {
      await saveAcademicProfile({
        locale: "en",
        csrf: "csrf-1",
        institutionID: "inst-1",
        enrollmentStatus: "ENROLLED",
        programID: "program-1",
        currentLevel: 3,
      });
    },
  );
  assert.equal(calls[0].url, "/api/v1/me/academic-profile");
  assert.equal(calls[0].method, "PUT");
  assert.equal(calls[0].csrf, "csrf-1");
  const body = calls[0].body as Record<string, unknown>;
  // The study plan is the server's to resolve (D-092 §6).
  assert.ok(!("curriculum_id" in body), "the client sent a curriculum");
  assert.deepEqual(body, {
    institution_id: "inst-1",
    enrollment_status: "ENROLLED",
    program_id: "program-1",
    academic_unit_id: "",
    current_level: 3,
  });
});

test("an undeclared Student sends their College and no Major", async () => {
  const calls = await withStub(
    () => ok({ setup_state: "COMPLETED" }),
    async () => {
      await saveAcademicProfile({
        locale: "ar",
        csrf: "c",
        institutionID: "inst-1",
        enrollmentStatus: "UNDECLARED",
        academicUnitID: "college-1",
      });
    },
  );
  const body = calls[0].body as Record<string, unknown>;
  assert.equal(body.enrollment_status, "UNDECLARED");
  assert.equal(body.academic_unit_id, "college-1");
  assert.equal(body.program_id, "");
  // Level stays genuinely optional.
  assert.equal(body.current_level, null);
});

test("skip is its own command, not an empty save", async () => {
  const calls = await withStub(
    () => ok({ setup_state: "SKIPPED" }),
    async () => {
      await skipAcademicOnboarding({ locale: "en", csrf: "c" });
    },
  );
  assert.equal(calls[0].method, "POST");
  assert.equal(calls[0].url, "/api/v1/me/academic-profile/skip");
  assert.equal(calls[0].body, undefined);
});

test("academic levels are generated from the institution's own bound", () => {
  // Kuwait University's five, from data — never a constant in the UI.
  const five = academicLevelLabels(5, "ar");
  assert.equal(five.length, 5);
  assert.equal(five[0].label, "المستوى الأول");
  assert.equal(five[4].label, "المستوى الخامس");
  const english = academicLevelLabels(5, "en");
  assert.equal(english[2].label, "Level 3");
  // A different institution gets a different range, with no special-casing.
  assert.equal(academicLevelLabels(4, "en").length, 4);
  assert.equal(academicLevelLabels(0, "en").length, 0);
});

test("the onboarding form hardcodes no institution, college, major, or level range", () => {
  const source = readSource(FORM) + readSource(CLIENT) + readSource(PROMPT);
  for (const hardcoded of [
    "Kuwait University", "جامعة الكويت",
    "College of Science", "College of Life Sciences", "College of Engineering",
    "Computer Science", "Cybersecurity", "Electrical Engineering",
    "Data Science and Artificial Intelligence",
  ]) {
    assert.ok(
      !source.includes(hardcoded),
      `the Student surface hardcodes ${hardcoded}; options must come from the Academic Catalog API`,
    );
  }
  // Mathematics majors are out of launch scope by Founder decision, and would
  // only ever appear here by being hardcoded.
  assert.ok(!source.includes("Financial Mathematics"));
  assert.ok(!source.includes("Software Engineering"));
  assert.ok(!source.includes("Cybersecurity Engineering"));
  // The level range comes from the institution.
  assert.ok(
    readSource(FORM).includes("max_academic_level"),
    "the level list is not derived from the institution's own maximum",
  );
});

test("Department is context, never a required selector", () => {
  const source = readSource(FORM);
  // Exactly four choosers: University, College, Major, level.
  const selectors = source.match(/data-testid="profile-(university|college|program|level)"/g) ?? [];
  assert.equal(new Set(selectors).size, 4, "the Student surface does not offer exactly four choices");
  assert.ok(
    !/data-testid="profile-department"/.test(source),
    "a Department selector was added; Department is derived context only",
  );
  assert.ok(!source.includes("اختر القسم"), "the Student is asked to choose a Department");
  // It may still be shown as a subtitle.
  assert.ok(source.includes("profile-program-context"), "the Department context line is missing");
});

test("undeclared and non-degree are Student states, not Program rows", () => {
  const source = readSource(FORM);
  assert.ok(source.includes("لم أحدد تخصصي بعد"), "the undeclared option is missing in Arabic");
  assert.ok(source.includes("طالب غير مقيد"), "the non-degree option is missing in Arabic");
  // Both are sentinels resolved into an enrollment status, never sent as a
  // program_id, so no placeholder Program is ever needed.
  assert.ok(source.includes('const UNDECLARED = "__undeclared__"'));
  assert.ok(source.includes('status = "UNDECLARED"'));
  assert.ok(source.includes('status = "NON_DEGREE"'));
});

test("onboarding is never a route guard and never blocks access", () => {
  const prompt = readSource(PROMPT);
  const form = readSource(FORM);
  // The prompt is a card; it must not redirect anyone anywhere.
  assert.ok(!prompt.includes("redirect("), "the onboarding prompt redirects");
  assert.ok(!prompt.includes("useRouter"), "the onboarding prompt navigates on its own");
  assert.ok(prompt.includes("if (!prompt) return null"), "the prompt is not conditional on state");

  // No middleware anywhere consults onboarding state.
  const middlewarePath = path.join(frontendRoot(), "src/middleware.ts");
  if (fs.existsSync(middlewarePath)) {
    const middleware = fs.readFileSync(middlewarePath, "utf8");
    for (const forbidden of ["academic-profile", "setup_state", "NOT_STARTED"]) {
      assert.ok(
        !middleware.includes(forbidden),
        `middleware consults onboarding state (${forbidden}); that would gate invitations and access`,
      );
    }
  }
  // The only navigation is the Student's own save or skip, back to their dashboard.
  const routerPushes = form.match(/router\.push\([^)]*\)/g) ?? [];
  for (const push of routerPushes) {
    assert.ok(
      push.includes("learn/dashboard"),
      `onboarding navigates to ${push}, which is not the Student's normal destination`,
    );
  }
});

test("the access promise is shown and is stated in both languages", () => {
  const source = readSource(FORM);
  assert.ok(source.includes("profile-access-promise"), "the access promise is not rendered");
  assert.ok(
    source.includes("كورساتك ومشترياتك لا تتأثر"),
    "the Arabic access promise is missing",
  );
  assert.ok(
    source.includes("Your courses and purchases are unaffected"),
    "the English access promise is missing",
  );
});

test("the Student surface renders no raw identifier", () => {
  const source = readSource(FORM) + readSource(PROMPT);
  // Attribute bindings such as value={institutionID} are form plumbing; only
  // JSX text children would put an identifier in front of a Student.
  const jsxText = source.replace(/[A-Za-z-]+=\{[^{}]*(\{[^{}]*\}[^{}]*)*\}/g, "");
  assert.ok(
    !/\{\s*(institutionID|collegeID|programChoice|[A-Za-z]+\.id)\s*\}/.test(jsxText),
    "an identifier is rendered as visible text",
  );
  assert.ok(!/uuid|UUID/.test(source), "the Student surface mentions UUIDs");
});

test("the client exposes no other Student's profile and no bulk listing", () => {
  const client = readSource(CLIENT);
  // Every route is /me: the server derives the account from the session.
  const routes = client.match(/`\$\{base\}[^`]*`/g) ?? [];
  assert.ok(routes.length > 0, "no routes found; this detector would pass vacuously");
  assert.ok(client.includes('const base = "/me"'), "the client no longer targets the Student's own scope");
  for (const forbidden of ["account_id", "student_id", "/admin/", "profiles?", "student-profiles"]) {
    assert.ok(!client.includes(forbidden), `the client exposes ${forbidden}`);
  }
});
