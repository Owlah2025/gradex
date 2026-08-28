import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import { execFileSync } from "child_process";
import fs from "fs";
import path from "path";
import { SEED_BINARY_PATH, RUN_STATE_FILE_PATH } from "../src/lib/api/e2e-infrastructure";
import { AUTHORIZATION_FLAG, expectAbsent } from "./authority-leak";

/**
 * T043 — an expired Entitlement shows retained Enrollment, retained Progress, and an explicit
 * expired state, while nothing is authorised from any of it. Progress is history, never an
 * authorisation input (FR-016, BR-029).
 *
 * Everything below runs against the real Next.js routes, the real Go API, real PostgreSQL,
 * production migrations, production authentication and session middleware, S4's production
 * entitlement evaluation, and the D-063 S5 read models. No protected route is intercepted.
 */

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const UNENTITLED_COURSE_ID = "c9999999-9999-9999-9999-999999999999";
const EXPIRED_STUDENT_ACCOUNT_ID = "a0000000-0000-0000-0000-000000000002";

/** Stable `course_lesson_identities.id` values — the only Lesson identity any public route accepts. */
const LESSON_PARTIAL_ID = "30000000-0000-0000-0000-000000000001";
const LESSON_COMPLETED_ID = "30000000-0000-0000-0000-000000000002";

/**
 * Known to the Node test runner only, so the Progress denial can send the exact production payload.
 * The DOM and read-model exclusion assertions below prove it never reaches the browser.
 */
const RUNNER_ONLY_ASSET_VERSION_ID = "60000000-0000-0000-0000-000000000001";

const RETAINED_PARTIAL_POSITION_SECONDS = 120;
const RETAINED_COMPLETED_POSITION_SECONDS = 300;

const VIEWPORTS = [
  { name: "phone", width: 390, height: 844 },
  { name: "desktop", width: 1440, height: 1000 },
];

const LOCALES = ["en", "ar"] as const;
type Locale = (typeof LOCALES)[number];

const TEXT = {
  en: {
    courseTitle: "CS101: Introduction to Programming",
    sectionOne: "Section 1: Basics",
    sectionTwo: "Section 2: Advanced Topics",
    lessonPartial: "Lesson 1: Introduction",
    lessonCompleted: "Lesson 2: Variables",
    lessonThird: "Lesson 3: Functions",
    expired: "Access expired",
    active: "Active access",
    accessUntil: "Access until",
    percent: "33%",
    completedOverTotal: "1/3",
    inProgress: "In progress",
    completed: "Completed",
    openCourse: "Open course",
    previousLesson: "Previous lesson",
    nextLesson: "Next lesson",
    firstLesson: "First lesson",
    openResource: "Open resource",
    openLabMaterial: "Open lab material",
    lessonNavigation: "Lesson navigation",
  },
  ar: {
    courseTitle: "مقدمة في البرمجة CS101",
    sectionOne: "القسم الأول: الأساسيات",
    sectionTwo: "القسم الثاني: البرمجة المتقدمة",
    lessonPartial: "الدرس الأول: مرحباً بك",
    lessonCompleted: "الدرس الثاني: المتغيرات",
    lessonThird: "الدرس الثالث: الدوال",
    expired: "انتهى الوصول",
    active: "الوصول نشط",
    accessUntil: "الوصول حتى",
    percent: "٣٣٪",
    completedOverTotal: "١/٣",
    inProgress: "قيد التقدم",
    completed: "مكتمل",
    openCourse: "فتح المقرر",
    previousLesson: "الدرس السابق",
    nextLesson: "الدرس التالي",
    firstLesson: "هذا أول درس",
    openResource: "فتح المورد",
    openLabMaterial: "فتح مادة المختبر",
    lessonNavigation: "التنقل بين الدروس",
  },
} as const;

/** The single fixed `type` URI the uniform refusal carries; it identifies no cause and no target. */
const PROBLEM_TYPE_URI = "https://api.gradex.com/problems/not-found";

/**
 * Field names, capability flags, and storage details that a retained-expired response must never
 * carry (D-063, D-064). `learning_status` is the only permitted public state enum.
 */
const PROHIBITED_READ_MODEL_FIELDS: (string | RegExp)[] = [
  "asset_version_id",
  "entitlement_id",
  "enrollment_id",
  "revision_id",
  "can_play",
  "can_update_progress",
  AUTHORIZATION_FLAG,
  "capability",
  "evaluator",
  "signed_url",
  "signedUrl",
  "object_key",
  "storage_path",
  "storage_object_key",
  "bucket",
  "manifest_url",
  "playback_session",
  "trusted_duration",
  RUNNER_ONLY_ASSET_VERSION_ID,
  "test/master.m3u8",
  "test/notes.pdf",
  "test/lab.zip",
];

/** Requests that would prove some authority was exercised or some protected target was fetched. */
function isAuthorityBearingRequest(url: string): boolean {
  return (
    url.includes("/playback") ||
    url.includes("/materials/") ||
    url.includes(".m3u8") ||
    url.includes(".ts") ||
    url.includes("/progress") ||
    url.includes("X-Amz-Signature")
  );
}

type ProgressSnapshotRow = {
  lesson_identity_id: string;
  max_position_seconds: number;
  last_position_seconds: number;
  completed: boolean;
  completed_at: string;
  updated_at: string;
};

type LearningStateSnapshot = {
  entitlement: {
    found: boolean;
    state: string;
    access_ends_at: string;
    original_access_ends_at: string;
    revoked_at: string | null;
    revision: number;
  };
  enrollment: { found: boolean; created_at: string };
  progress: ProgressSnapshotRow[];
  material_kinds: Record<string, number>;
  video_asset_version_state: string;
};

/**
 * Reads authority and learning state straight from the isolated per-run PostgreSQL database
 * through the accepted test-runner-side seeder helper. The browser never receives database
 * credentials, and no authorization SQL is duplicated here.
 */
function readLearningState(): LearningStateSnapshot {
  const state = JSON.parse(fs.readFileSync(RUN_STATE_FILE_PATH, "utf-8"));
  const output = execFileSync(
    SEED_BINARY_PATH,
    [
      "-query-learning-state",
      "-dbname",
      state.dbName,
      "-student",
      EXPIRED_STUDENT_ACCOUNT_ID,
      "-course",
      COURSE_ID,
    ],
    {
      env: {
        ...process.env,
        GRADEX_E2E_ALLOW_DATABASE_RESET: "1",
        GRADEX_E2E_ADMIN_DB_URL: "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable",
        GRADEX_E2E_TARGET_DB_NAME: state.dbName,
        GRADEX_E2E_TARGET_DB_URL: `postgres://gradex:gradex@localhost:5432/${state.dbName}?sslmode=disable`,
        DATABASE_URL: "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable",
      },
      encoding: "utf-8",
    }
  );
  return JSON.parse(output.trim()) as LearningStateSnapshot;
}

type Session = { cookies: any[]; csrfToken: string };

/**
 * Authenticates through the production flow only: `GET /api/v1/session/bootstrap` for the CSRF
 * token, then `POST /api/v1/sessions`. The session cookie is production-issued and validated by
 * production session middleware; nothing here forges or bypasses it.
 */
async function authenticateExpiredStudent(context: BrowserContext): Promise<Session> {
  const page = await context.newPage();
  await page.goto("/en/catalog");

  const loginResult = await page.evaluate(async () => {
    const bootstrapRes = await fetch("/api/v1/session/bootstrap", {
      method: "GET",
      credentials: "include",
    });
    const { csrf_token } = await bootstrapRes.json();

    const loginRes = await fetch("/api/v1/sessions", {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
        "X-CSRF-Token": csrf_token,
      },
      body: JSON.stringify({
        email: "student-expired@example.test",
        password: "StudentPassword123!",
      }),
    });

    return { status: loginRes.status, body: await loginRes.json() };
  });

  expect(loginResult.status).toBe(201);
  expect(typeof loginResult.body.csrf_token).toBe("string");
  expect(loginResult.body.csrf_token.length).toBeGreaterThan(0);

  const cookies = await context.cookies();
  await page.close();
  return { cookies, csrfToken: loginResult.body.csrf_token };
}

/** Performs a real same-origin request from the authenticated expired browser context. */
async function requestFromExpiredContext(
  page: Page,
  request: { method: string; path: string; csrfToken?: string; body?: unknown }
) {
  return page.evaluate(async (input) => {
    const response = await fetch(input.path, {
      method: input.method,
      credentials: "include",
      redirect: "manual",
      cache: "no-store",
      headers: {
        Accept: "application/json, application/problem+json",
        ...(input.body ? { "Content-Type": "application/json" } : {}),
        ...(input.csrfToken ? { "X-CSRF-Token": input.csrfToken } : {}),
      },
      ...(input.body ? { body: JSON.stringify(input.body) } : {}),
    });
    const headers: Record<string, string> = {};
    response.headers.forEach((value, key) => {
      headers[key.toLowerCase()] = value;
    });
    return {
      status: response.status,
      type: response.type,
      redirected: response.redirected,
      headers,
      text: await response.text(),
    };
  }, request);
}

/** Asserts the uniform protected-unavailable refusal, with no authority leaked in any form. */
function expectUniformRefusal(result: {
  status: number;
  type: string;
  redirected: boolean;
  headers: Record<string, string>;
  text: string;
}) {
  expect(result.status).toBe(404);
  expect(result.type).not.toBe("opaqueredirect");
  expect(result.redirected).toBe(false);
  expect(result.headers["cache-control"]).toBe("no-store");
  expect(result.headers["content-type"]).toContain("application/problem+json");
  expect(result.headers["location"]).toBeUndefined();

  const body = result.text;
  for (const field of PROHIBITED_READ_MODEL_FIELDS) {
    expectAbsent(body, field);
  }

  // The only URI the body may carry is the fixed, cause-free problem type. No signed target,
  // manifest, storage host, or presigned query parameter appears.
  expect(body.replace(PROBLEM_TYPE_URI, "")).not.toMatch(/https?:\/\//);
  for (const marker of ["X-Amz", "AWSAccessKeyId", "Signature", "Expires=", ".m3u8", "manifest"]) {
    expect(body).not.toContain(marker);
  }
  // No internal denial reason of any kind reaches the client.
  for (const reason of [
    "expired",
    "revoked",
    "suspend",
    "out_of_scope",
    "out-of-scope",
    "retired",
    "entitlement",
    "enrollment",
    "evaluator",
    "reason",
  ]) {
    expect(body.toLowerCase()).not.toContain(reason);
  }
}

function assertSnapshotsIdentical(before: LearningStateSnapshot, after: LearningStateSnapshot) {
  expect(after).toEqual(before);
}

test.describe("T043 — retained-expired Entitlement authorises nothing", () => {
  let session: Session;
  let baseline: LearningStateSnapshot;

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext();
    session = await authenticateExpiredStudent(context);
    await context.close();
    baseline = readLearningState();
  });

  test("Fixture: retained Enrollment, durable Progress, and underlying materials exist beneath the expired Entitlement", async () => {
    // Authority: an Entitlement that expired at a deterministic past UTC instant, never revoked.
    expect(baseline.entitlement.found).toBe(true);
    expect(baseline.entitlement.state).toBe("ACTIVE");
    expect(baseline.entitlement.revoked_at).toBeNull();
    expect(baseline.entitlement.access_ends_at).toMatch(/Z$/);
    expect(new Date(baseline.entitlement.access_ends_at).getTime()).toBeLessThan(Date.now());
    expect(baseline.entitlement.access_ends_at).toBe(baseline.entitlement.original_access_ends_at);

    // The authoritative Enrollment is retained.
    expect(baseline.enrollment.found).toBe(true);

    // Durable Progress: one partial, one completed.
    expect(baseline.progress).toHaveLength(2);
    const partial = baseline.progress.find((row) => row.lesson_identity_id === LESSON_PARTIAL_ID);
    const completed = baseline.progress.find((row) => row.lesson_identity_id === LESSON_COMPLETED_ID);
    expect(partial?.max_position_seconds).toBe(RETAINED_PARTIAL_POSITION_SECONDS);
    expect(partial?.completed).toBe(false);
    expect(completed?.max_position_seconds).toBe(RETAINED_COMPLETED_POSITION_SECONDS);
    expect(completed?.completed).toBe(true);

    // The video, Resource, and Lab Material exist underneath the expired state, so every denial
    // below proves current authority rather than missing content.
    expect(baseline.video_asset_version_state).toBe("READY");
    expect(baseline.material_kinds.RESOURCE).toBe(1);
    expect(baseline.material_kinds.LAB_MATERIAL).toBe(1);
  });

  for (const vp of VIEWPORTS) {
    test.describe(`Viewport: ${vp.name} (${vp.width}x${vp.height})`, () => {
      test.use({ viewport: { width: vp.width, height: vp.height } });

      for (const locale of LOCALES) {
        const t = TEXT[locale];
        const isRTL = locale === "ar";

        test(`${locale.toUpperCase()} — Dashboard, Course Home, and Lesson retain state and authorise nothing`, async ({
          context,
          page,
        }) => {
          await context.addCookies(session.cookies);

          const requestedUrls: string[] = [];
          page.on("request", (req) => requestedUrls.push(req.url()));

          // ---------------------------------------------------------------- Dashboard
          await page.goto(`/${locale}/learn/dashboard`);
          await page.waitForLoadState("networkidle");

          const main = page.locator("main");
          await expect(main).toHaveAttribute("dir", isRTL ? "rtl" : "ltr");
          await expect(page.locator("html")).toHaveAttribute("dir", isRTL ? "rtl" : "ltr");

          // The expired Course remains listed with its authored title.
          const dashboardCards = page.locator("main article");
          await expect(dashboardCards).toHaveCount(1);
          await expect(page.getByRole("heading", { name: t.courseTitle, level: 2 })).toBeVisible();

          // Retained Progress remains visible and localized.
          await expect(page.getByText(t.percent, { exact: false })).toBeVisible();
          await expect(page.getByText(t.completedOverTotal, { exact: false })).toBeVisible();

          // Explicit, localized expired state — and never the active state.
          await expect(page.locator('[data-learning-status="expired"]')).toHaveCount(1);
          await expect(page.locator('[data-learning-status="active"]')).toHaveCount(0);
          await expect(page.getByText(t.expired).first()).toBeVisible();
          await expect(page.getByText(t.active)).toHaveCount(0);

          // The authoritative expiry is displayed and machine-readable at the original instant.
          await expect(page.getByText(t.accessUntil, { exact: false })).toBeVisible();
          const dashboardTime = page.locator("main time");
          await expect(dashboardTime).toHaveCount(1);
          const dashboardDateTime = await dashboardTime.getAttribute("datetime");
          expect(dashboardDateTime).toBe(baseline.entitlement.access_ends_at);
          await expect(dashboardTime).not.toHaveText("");

          // No action implying authority.
          await expectNoProtectedActions(page, t);

          // Unrelated Courses do not leak through placeholders.
          const dashboardHtml = await page.content();
          expect(dashboardHtml).not.toContain(UNENTITLED_COURSE_ID);
          expectNoProhibitedFields(dashboardHtml);
          await expectNoHorizontalOverflow(page);

          // ----------------------------------------------- Course Home via the retained link
          await page.getByRole("link", { name: t.openCourse }).click();
          await page.waitForURL(`**/${locale}/learn/courses/${COURSE_ID}`);
          await page.waitForLoadState("networkidle");

          await expect(page.getByRole("heading", { name: t.courseTitle, level: 1 })).toBeVisible();

          // Current approved live graph, Sections in authored order.
          const sectionHeadings = page.locator("main h2");
          // `toContainText` rather than `toHaveText`: a section heading is now a disclosure that
          // also states how much of its own section is done. The authored order, which is what this
          // assertion is for, is unchanged.
          await expect(sectionHeadings.nth(0)).toContainText(t.sectionOne);
          await expect(sectionHeadings.nth(1)).toContainText(t.sectionTwo);

          // Lessons in authored order within their Section.
          // The contents are a navigation landmark rather than a bare section, and the ordered
          // lists inside it are the only ones on the page — so the Lessons are still addressed by
          // their authored order, and still only theirs.
          const firstSectionLessons = page.locator("main ol li");
          await expect(firstSectionLessons.nth(0)).toContainText(t.lessonPartial);
          await expect(firstSectionLessons.nth(1)).toContainText(t.lessonCompleted);
          await expect(page.getByText(t.lessonThird)).toBeVisible();

          // Aggregate and per-Lesson Progress, including completion state.
          //
          // The retained per-Lesson state is now read as the state itself rather than as the raw
          // second count the reporter persists: UX-F removed "120 seconds · Not completed" from
          // the Student surfaces, and a Student reading a Course whose access has ended needs to
          // know which Lessons they finished, not the value of a database column. The claim this
          // test makes is unchanged and slightly stronger — the part-watched Lesson and the
          // finished one must still be told apart, and the aggregate figures must still survive.
          await expect(page.getByText(t.percent, { exact: false })).toBeVisible();
          await expect(page.getByText(t.completedOverTotal, { exact: false })).toBeVisible();
          await expect(firstSectionLessons.nth(0)).toContainText(t.inProgress);
          await expect(firstSectionLessons.nth(0)).not.toContainText(t.completed);
          await expect(firstSectionLessons.nth(1)).toContainText(t.completed);
          await expect(firstSectionLessons.nth(1)).not.toContainText(t.inProgress);

          // Explicit localized expired state, original instant preserved.
          await expect(page.locator('[data-learning-status="expired"]')).toHaveCount(1);
          await expect(page.locator('[data-learning-status="active"]')).toHaveCount(0);
          const courseHomeDateTime = await page.locator("main time").first().getAttribute("datetime");
          expect(courseHomeDateTime).toBe(baseline.entitlement.access_ends_at);

          await expectNoProtectedActions(page, t);
          expectNoProhibitedFields(await page.content());
          await expectNoHorizontalOverflow(page);

          // ------------------------------------------------------ Lesson (stable identity route)
          await page.goto(`/${locale}/learn/courses/${COURSE_ID}/lessons/${LESSON_PARTIAL_ID}`);
          await page.waitForLoadState("networkidle");

          // Lesson metadata and Section context remain visible.
          await expect(page.getByRole("heading", { name: t.lessonPartial, level: 1 })).toBeVisible();
          // The Lesson names its Section above the title, and the Course contents beside it name it
          // again. `first()` is the Lesson's own header; both are the same authored string.
          await expect(page.getByText(t.sectionOne).first()).toBeVisible();

          // The retained state remains visible, and is the part-watched one rather than the
          // finished one.
          await expect(page.getByTestId("lesson-state")).toHaveText(t.inProgress);

          // Previous/next navigation remains reachable.
          await expect(page.getByRole("navigation", { name: t.lessonNavigation })).toBeVisible();
          await expect(page.getByRole("link", { name: t.nextLesson })).toBeVisible();
          await expect(page.getByText(t.firstLesson)).toBeVisible();

          // Explicit localized expired state and the original machine-readable instant.
          await expect(page.locator('[data-learning-status="expired"]')).toHaveCount(1);
          await expect(page.getByText(t.expired).first()).toBeVisible();
          const lessonDateTime = await page.locator("main time").first().getAttribute("datetime");
          expect(lessonDateTime).toBe(baseline.entitlement.access_ends_at);

          // No player, no controls, no material actions.
          await expect(page.locator("video")).toHaveCount(0);
          await expect(page.locator("[data-player-controls]")).toHaveCount(0);
          await expectNoProtectedActions(page, t);
          expectNoProhibitedFields(await page.content());
          await expectNoHorizontalOverflow(page);

          // The completed Lesson keeps its completion state on its own surface.
          await page.goto(`/${locale}/learn/courses/${COURSE_ID}/lessons/${LESSON_COMPLETED_ID}`);
          await page.waitForLoadState("networkidle");
          await expect(page.getByRole("heading", { name: t.lessonCompleted, level: 1 })).toBeVisible();
          await expect(page.getByTestId("lesson-state")).toHaveText(t.completed);
          await expect(page.getByRole("link", { name: t.previousLesson })).toBeVisible();
          await expect(page.locator("video")).toHaveCount(0);
          await expect(page.locator("[data-player-controls]")).toHaveCount(0);

          // Dwell past any reporter tick that a mounted player would have scheduled.
          await page.waitForTimeout(2000);

          // Observed network, not merely DOM absence: no playback, manifest, variant playlist,
          // segment, material, or Progress request occurred anywhere in the flow.
          const authorityRequests = requestedUrls.filter(isAuthorityBearingRequest);
          expect(authorityRequests).toEqual([]);
        });
      }
    });
  }

  test("Explicit playback denial: POST /playback is refused with no signed target and no mutation", async ({
    browser,
  }) => {
    const context = await browser.newContext();
    await context.addCookies(session.cookies);
    const page = await context.newPage();

    const requestedUrls: string[] = [];
    page.on("request", (req) => requestedUrls.push(req.url()));

    await page.goto("/en/catalog");
    const before = readLearningState();

    const result = await requestFromExpiredContext(page, {
      method: "POST",
      path: `/api/v1/learn/lessons/${LESSON_PARTIAL_ID}/playback`,
      csrfToken: session.csrfToken,
    });

    expectUniformRefusal(result);

    const after = readLearningState();
    assertSnapshotsIdentical(before, after);
    assertSnapshotsIdentical(baseline, after);

    // Exactly one attempt: the refusal triggered no client retry loop and no manifest fetch.
    expect(requestedUrls.filter((url) => url.includes("/playback"))).toHaveLength(1);
    expect(requestedUrls.filter((url) => url.includes(".m3u8"))).toEqual([]);

    await context.close();
  });

  test("Explicit Progress denial: PUT /progress is refused and writes nothing", async ({ browser }) => {
    const context = await browser.newContext();
    await context.addCookies(session.cookies);
    const page = await context.newPage();

    const requestedUrls: string[] = [];
    page.on("request", (req) => requestedUrls.push(req.url()));

    await page.goto("/en/catalog");
    const before = readLearningState();

    // The exact production payload, including a position that would advance the retained maximum
    // and a completion-triggering position for the untouched Lesson, had authority allowed it.
    const result = await requestFromExpiredContext(page, {
      method: "PUT",
      path: `/api/v1/learn/lessons/${LESSON_PARTIAL_ID}/progress`,
      csrfToken: session.csrfToken,
      body: { position_seconds: 29.5, asset_version_id: RUNNER_ONLY_ASSET_VERSION_ID },
    });

    expectUniformRefusal(result);

    // A Lesson with no Progress row at all must not gain one either.
    const untouchedResult = await requestFromExpiredContext(page, {
      method: "PUT",
      path: `/api/v1/learn/lessons/30000000-0000-0000-0000-000000000003/progress`,
      csrfToken: session.csrfToken,
      body: { position_seconds: 29.5, asset_version_id: RUNNER_ONLY_ASSET_VERSION_ID },
    });
    expectUniformRefusal(untouchedResult);

    const after = readLearningState();

    // No Progress row created; no position, completion, Enrollment, or Entitlement change.
    expect(after.progress).toHaveLength(before.progress.length);
    assertSnapshotsIdentical(before, after);
    assertSnapshotsIdentical(baseline, after);

    // No retry loop: exactly the two deliberate attempts.
    expect(requestedUrls.filter((url) => url.includes("/progress"))).toHaveLength(2);

    await context.close();
  });

  test("Resource and Lab Material denial: both S4 entry routes refuse with no redirect and no signed target", async ({
    browser,
  }) => {
    const context = await browser.newContext();
    await context.addCookies(session.cookies);
    const page = await context.newPage();
    await page.goto("/en/catalog");

    const before = readLearningState();

    for (const kind of ["resource", "lab-material"]) {
      const result = await requestFromExpiredContext(page, {
        method: "GET",
        path: `/api/v1/media/lessons/${LESSON_PARTIAL_ID}/materials/${kind}`,
      });

      expectUniformRefusal(result);
    }

    // Both material kinds still exist in PostgreSQL, so the refusals prove current authority.
    const after = readLearningState();
    expect(after.material_kinds.RESOURCE).toBe(1);
    expect(after.material_kinds.LAB_MATERIAL).toBe(1);
    assertSnapshotsIdentical(before, after);

    await context.close();
  });

  test("Read models: retained expired responses are no-store, expose only learning_status, and carry no materials", async ({
    browser,
  }) => {
    const context = await browser.newContext();
    await context.addCookies(session.cookies);
    const page = await context.newPage();
    await page.goto("/en/catalog");

    const reads = [
      "/api/v1/learn/dashboard",
      `/api/v1/learn/courses/${COURSE_ID}`,
      `/api/v1/learn/courses/${COURSE_ID}/lessons/${LESSON_PARTIAL_ID}`,
    ];

    for (const path of reads) {
      const result = await requestFromExpiredContext(page, { method: "GET", path });

      expect(result.status).toBe(200);
      expect(result.headers["cache-control"]).toBe("no-store");
      expectNoProhibitedFields(result.text);

      const body = JSON.parse(result.text);
      const subject = body.courses ? body.courses[0] : body;
      expect(subject.learning_status).toBe("expired");
      expect(subject.expires_at).toBe(baseline.entitlement.access_ends_at);

      if (Array.isArray(body.resources)) expect(body.resources).toEqual([]); if (Array.isArray(body.lab_materials)) expect(body.lab_materials).toEqual([]);
      if (Array.isArray(body.sections)) { for (const section of body.sections) {
          for (const lesson of section.lessons) {
            expect(lesson.resources).toEqual([]); expect(lesson.lab_materials).toEqual([]);
          }
        }
      }
    }

    // Retained Progress is present as history in the read models.
    const dashboard = JSON.parse(
      (await requestFromExpiredContext(page, { method: "GET", path: "/api/v1/learn/dashboard" })).text
    );
    expect(dashboard.courses).toHaveLength(1);
    expect(dashboard.courses[0].progress).toEqual({
      completed_lessons: 1,
      total_lessons: 3,
      percent: 33,
    });

    await context.close();
  });

  test("Locale parity: both locales expose identical authority information and an identical machine-readable instant", async ({
    browser,
  }) => {
    const context = await browser.newContext();
    await context.addCookies(session.cookies);
    const page = await context.newPage();
    await page.goto("/en/catalog");

    const byLocale: Record<Locale, any> = {} as any;
    for (const locale of LOCALES) {
      const result = await page.evaluate(async (input) => {
        const response = await fetch(input.path, {
          method: "GET",
          credentials: "include",
          cache: "no-store",
          headers: { Accept: "application/json", "Accept-Language": input.locale },
        });
        return response.text();
      }, { path: `/api/v1/learn/courses/${COURSE_ID}`, locale });
      byLocale[locale] = JSON.parse(result);
    }

    // Authority information is identical; only Instructor-authored text differs.
    expect(byLocale.ar.learning_status).toBe(byLocale.en.learning_status);
    expect(byLocale.ar.expires_at).toBe(byLocale.en.expires_at);
    expect(byLocale.ar.progress).toEqual(byLocale.en.progress);
    expect(byLocale.ar.sections.map((s: any) => s.section_id)).toEqual(
      byLocale.en.sections.map((s: any) => s.section_id)
    );
    expect(byLocale.ar.sections[0].lessons.map((l: any) => l.lesson_id)).toEqual(
      byLocale.en.sections[0].lessons.map((l: any) => l.lesson_id)
    );
    expect(byLocale.ar.title).not.toBe(byLocale.en.title);
    expect(byLocale.en.title).toBe(TEXT.en.courseTitle);
    expect(byLocale.ar.title).toBe(TEXT.ar.courseTitle);

    // The rendered machine-readable instant is identical across locales while the human-readable
    // expiry, Progress, and expired label are localized.
    const rendered: Record<Locale, { dateTime: string | null; expiryText: string }> = {} as any;
    for (const locale of LOCALES) {
      await page.goto(`/${locale}/learn/courses/${COURSE_ID}`);
      await page.waitForLoadState("networkidle");
      const timeEl = page.locator("main time").first();
      rendered[locale] = {
        dateTime: await timeEl.getAttribute("datetime"),
        expiryText: await timeEl.innerText(),
      };
      await expect(page.getByText(TEXT[locale].expired).first()).toBeVisible();
      await expect(page.getByText(TEXT[locale].percent, { exact: false })).toBeVisible();
    }
    expect(rendered.ar.dateTime).toBe(rendered.en.dateTime);
    expect(rendered.ar.dateTime).toBe(baseline.entitlement.access_ends_at);
    expect(rendered.ar.expiryText).not.toBe(rendered.en.expiryText);

    await context.close();
  });

  test("Cache and stale state: a fresh context never inherits active data and a reload keeps the expired presentation", async ({
    browser,
  }) => {
    const context = await browser.newContext();
    await context.addCookies(session.cookies);
    const page = await context.newPage();

    const requestedUrls: string[] = [];
    page.on("request", (req) => requestedUrls.push(req.url()));

    const lessonUrl = `/en/learn/courses/${COURSE_ID}/lessons/${LESSON_PARTIAL_ID}`;
    await page.goto(lessonUrl);
    await page.waitForLoadState("networkidle");
    await expect(page.getByText(TEXT.en.expired).first()).toBeVisible();
    await expect(page.locator("video")).toHaveCount(0);

    // No Active Student data bleeds into the expired context.
    const firstRender = await page.content();
    expect(firstRender).not.toContain(TEXT.en.active);
    expect(firstRender).not.toContain('data-learning-status="active"');

    // Reload retains the expired presentation and mounts no player.
    await page.reload();
    await page.waitForLoadState("networkidle");
    await expect(page.getByText(TEXT.en.expired).first()).toBeVisible();
    await expect(page.locator("video")).toHaveCount(0);
    await expect(page.locator("[data-player-controls]")).toHaveCount(0);

    // Navigating away and back does not restore stale active player state.
    await page.goto(`/en/learn/courses/${COURSE_ID}`);
    await page.waitForLoadState("networkidle");
    await page.goto(lessonUrl);
    await page.waitForLoadState("networkidle");
    await expect(page.locator("video")).toHaveCount(0);
    await expect(page.getByText(TEXT.en.expired).first()).toBeVisible();

    // A prior explicit denial is never replaced by cached successful content.
    const denial = await requestFromExpiredContext(page, {
      method: "GET",
      path: `/api/v1/media/lessons/${LESSON_PARTIAL_ID}/materials/resource`,
    });
    expectUniformRefusal(denial);
    const repeatDenial = await requestFromExpiredContext(page, {
      method: "GET",
      path: `/api/v1/media/lessons/${LESSON_PARTIAL_ID}/materials/resource`,
    });
    expectUniformRefusal(repeatDenial);

    expect(requestedUrls.filter((url) => url.includes("/playback"))).toEqual([]);
    expect(requestedUrls.filter((url) => url.includes(".m3u8"))).toEqual([]);

    assertSnapshotsIdentical(baseline, readLearningState());

    await context.close();
  });

  test("No-mock audit: no protected route is intercepted by this spec or the shared configuration", async () => {
    const files = [
      path.resolve(__dirname, "s5-expired-entitlement.spec.ts"),
      path.resolve(__dirname, "global-setup.ts"),
      path.resolve(__dirname, "global-teardown.ts"),
      path.resolve(__dirname, "../playwright.config.ts"),
    ];

    // Patterns are assembled at runtime so this audit does not match its own source text.
    const interceptPatterns = [
      new RegExp(String.raw`\b(page|context|browserContext)\s*\.\s*` + "rou" + "te" + String.raw`\s*\(`),
      new RegExp("rou" + "teFromHAR|un" + "route|ful" + "fill" + String.raw`\s*\(`),
      new RegExp(String.raw`\b` + "mo" + String.raw`ck(Response|Fetch|Api|Route)\b`, "i"),
    ];

    // The protected prefixes that must reach the real Go API unmodified.
    const protectedPrefixes = ["/api/v1/learn/", "/api/v1/media/", "/api/v1/sessions", "/api/v1/session/"];

    for (const file of files) {
      const source = fs.readFileSync(file, "utf-8");
      for (const pattern of interceptPatterns) {
        expect(source).not.toMatch(pattern);
      }
      // Nothing in these files installs a handler against a protected prefix.
      for (const prefix of protectedPrefixes) {
        expect(source).not.toMatch(new RegExp("rou" + String.raw`te\s*\([^)]*` + prefix.replace(/\//g, String.raw`\/`)));
      }
    }
  });

  test("Authority is unchanged after every rendering and denial scenario", async () => {
    assertSnapshotsIdentical(baseline, readLearningState());
  });
});

/** Asserts no action implying playback, resume, Resource, or Lab Material authority is present. */
async function expectNoProtectedActions(page: Page, t: (typeof TEXT)[Locale]) {
  await expect(page.getByRole("link", { name: t.openResource })).toHaveCount(0);
  await expect(page.getByRole("link", { name: t.openLabMaterial })).toHaveCount(0);
  await expect(page.locator('a[href*="/materials/"]')).toHaveCount(0);
  await expect(page.locator('a[href*="/playback"]')).toHaveCount(0);
  await expect(page.locator("[data-player-controls]")).toHaveCount(0);
  await expect(page.locator("video")).toHaveCount(0);
}

function expectNoProhibitedFields(markup: string) {
  for (const field of PROHIBITED_READ_MODEL_FIELDS) {
    expectAbsent(markup, field);
  }
}

async function expectNoHorizontalOverflow(page: Page) {
  const overflowing = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth
  );
  expect(overflowing).toBe(false);
}
