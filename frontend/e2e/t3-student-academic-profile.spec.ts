import {
  test,
  expect,
  request as playwrightRequest,
  type APIRequestContext,
  type BrowserContext,
  type TestInfo,
} from "@playwright/test";
import {
  ACADEMIC_ACCESS_PRESERVED_TEST_SLOT,
  ACADEMIC_DSAI_TEST_SLOT,
  ACADEMIC_INVITATION_TEST_SLOT,
  ACADEMIC_ONBOARDING_TEST_SLOT,
  ACADEMIC_SKIP_TEST_SLOT,
  ACADEMIC_UNDECLARED_TEST_SLOT,
  issueRotatingSession,
  studentFor,
  type RotatingStudent,
} from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * T3 (MVP-F19) Student Academic Profile — real browser journeys.
 *
 * The profile is discovery-only. Two of these journeys exist specifically to
 * prove that: an unprofiled Student completes a real access flow (E), and an
 * entitled Student changes their major without losing anything (F).
 *
 * The Kuwait University launch catalog is imported through the real Admin API,
 * so every option a Student sees comes from the Academic Catalog rather than
 * from anything written in the frontend.
 *
 * Nothing here touches Instructor academic context (T4), the legacy taxonomy
 * migration (T5), or catalogue filtering and ranking (T6).
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const MANIFEST = "kuwait-university-launch-v1";
const anyInstitution = "00000000-0000-0000-0000-000000000000";

type Session = ReturnType<typeof issueRotatingSession>;

async function attach(context: BrowserContext, session: Session, locale: "ar" | "en" = "en") {
  const origin = new URL(frontendOrigin());
  await context.addInitScript((selected) => {
    window.localStorage.setItem("gradex.locale", selected);
  }, locale);
  await context.addCookies([
    {
      name: session.cookie_name,
      value: session.cookie_value,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
    },
  ]);
}

async function apiFor(session: Session): Promise<APIRequestContext> {
  return playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Origin: frontendOrigin(),
      Cookie: `${session.cookie_name}=${session.cookie_value}`,
      "X-CSRF-Token": session.csrf_token,
    },
  });
}

async function signInStudent(
  context: BrowserContext,
  testInfo: TestInfo,
  slot: number,
  locale: "ar" | "en" = "en",
): Promise<{ session: Session; student: RotatingStudent }> {
  const student = studentFor(testInfo, slot);
  const session = issueRotatingSession(student);
  await attach(context, session, locale);
  return { session, student };
}

/** The launch catalog, imported the way production imports it. */
async function ensureLaunchCatalog(): Promise<void> {
  const adminSession = issueRotatingSession(ADMIN);
  const admin = await apiFor(adminSession);
  const applied = await admin.post(`/api/v1/admin/academic/institutions/${anyInstitution}/import`, {
    data: { manifest: MANIFEST, mode: "apply" },
  });
  expect(applied.status()).toBe(200);
  await admin.dispose();
}

test.describe("T3 Student academic profile", () => {
  test.beforeAll(async () => {
    await ensureLaunchCatalog();
  });

  test("A a Student onboards into Computer Science and the server resolves the plan", async ({
    browser,
  }, testInfo) => {
    const context = await browser.newContext({ locale: "en-US" });
    const { session } = await signInStudent(context, testInfo, ACADEMIC_ONBOARDING_TEST_SLOT);
    const api = await apiFor(session);

    // A Student who has made no decision is NOT_STARTED and is invited, never
    // redirected: the dashboard renders normally with a card on it.
    const before = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(before.setup_state).toBe("NOT_STARTED");

    const page = await context.newPage();
    await page.goto("/en/learn/dashboard");
    await expect(page.getByTestId("academic-profile-prompt")).toBeVisible();
    await page.getByTestId("academic-profile-prompt-action").click();
    await expect(page.getByTestId("academic-profile-form")).toBeVisible();

    await page.getByTestId("profile-university").selectOption({ label: "Kuwait University" });
    await page.getByTestId("profile-college").selectOption({ label: "College of Science" });
    await page.getByTestId("profile-program").selectOption({ label: "Computer Science" });
    // The Department is shown as context, never chosen.
    await expect(page.getByTestId("profile-program-context")).toContainText("Computer Science");
    await page.getByTestId("profile-level").selectOption({ label: "Level 2" });
    await page.getByTestId("profile-save").click();

    // Onboarding hands the Student back to their normal destination.
    await page.waitForURL(/\/en\/learn\/dashboard/);
    // And having decided, they are not asked again.
    await expect(page.getByTestId("academic-profile-prompt")).toHaveCount(0);

    const after = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(after.setup_state).toBe("COMPLETED");
    expect(after.enrollment_status).toBe("ENROLLED");
    expect(after.institution_name).toBe("Kuwait University");
    expect(after.program_name).toBe("Computer Science");
    expect(after.current_level).toBe(2);
    // The study plan was resolved by the server, never chosen by the browser.
    expect(after.curriculum_version_label).toBe("2024");
    expect(after.college_name).toBe("College of Science");

    await api.dispose();
    await context.close();
  });

  test("B the Data Science degree is offered from real catalog data", async ({ browser }, testInfo) => {
    const context = await browser.newContext({ locale: "en-US" });
    const { session } = await signInStudent(context, testInfo, ACADEMIC_DSAI_TEST_SLOT);
    const api = await apiFor(session);

    const page = await context.newPage();
    await page.goto("/en/learn/academic-profile");
    await page.getByTestId("profile-university").selectOption({ label: "Kuwait University" });
    await page.getByTestId("profile-college").selectOption({ label: "College of Life Sciences" });

    // The T2.2 Program reaches the Student through the Academic Catalog API, so
    // wait for the option to arrive rather than racing the request.
    const programSelect = page.getByTestId("profile-program");
    await expect(programSelect).toContainText("Data Science and Artificial Intelligence");
    const options = await programSelect.innerText();
    expect(options).toContain("Data Science and Artificial Intelligence");
    // Founder scope: Mathematics majors are not launch Programs anywhere.
    expect(options).not.toContain("Financial Mathematics");
    expect(options).not.toContain("Software Engineering");

    await page.getByTestId("profile-program").selectOption({
      label: "Data Science and Artificial Intelligence",
    });
    await page.getByTestId("profile-level").selectOption({ label: "Level 1" });
    await page.getByTestId("profile-save").click();
    await page.waitForURL(/\/en\/learn\/dashboard/);

    const saved = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(saved.program_name).toBe("Data Science and Artificial Intelligence");
    expect(saved.college_name).toBe("College of Life Sciences");
    expect(saved.current_level).toBe(1);
    expect(saved.curriculum_version_label).toBeTruthy();

    await api.dispose();
    await context.close();
  });

  test("C an undeclared Student keeps their College and gains no fake major", async ({
    browser,
  }, testInfo) => {
    const context = await browser.newContext({ locale: "ar-KW" });
    const { session } = await signInStudent(context, testInfo, ACADEMIC_UNDECLARED_TEST_SLOT, "ar");
    const api = await apiFor(session);

    const page = await context.newPage();
    await page.goto("/ar/learn/academic-profile");
    await expect(page.getByTestId("academic-profile-form")).toBeVisible();
    await page.getByTestId("profile-university").selectOption({ label: "جامعة الكويت" });
    await page.getByTestId("profile-college").selectOption({ label: "كلية الهندسة والبترول" });
    await page.getByTestId("profile-program").selectOption({ label: "لم أحدد تخصصي بعد" });
    await page.getByTestId("profile-save").click();
    await page.waitForURL(/\/ar\/learn\/dashboard/);

    const saved = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(saved.setup_state).toBe("COMPLETED");
    expect(saved.enrollment_status).toBe("UNDECLARED");
    // The College context is retained — the correction D-092 §2 exists for.
    expect(saved.academic_unit_name).toBe("College of Engineering and Petroleum");
    expect(saved.program_id).toBeUndefined();
    expect(saved.curriculum_version_label).toBeUndefined();

    await api.dispose();
    await context.close();
  });

  test("D skipping is respected and never repeated", async ({ browser }, testInfo) => {
    const context = await browser.newContext({ locale: "ar-KW" });
    const { session } = await signInStudent(context, testInfo, ACADEMIC_SKIP_TEST_SLOT, "ar");
    const api = await apiFor(session);

    const page = await context.newPage();
    await page.goto("/ar/learn/dashboard");
    await expect(page.getByTestId("academic-profile-prompt")).toBeVisible();
    await page.getByTestId("academic-profile-prompt-action").click();
    await page.getByTestId("profile-skip").click();
    await page.waitForURL(/\/ar\/learn\/dashboard/);

    const skipped = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(skipped.setup_state).toBe("SKIPPED");

    // The dashboard still works, and the Student is not asked again.
    await page.reload();
    await expect(page.getByTestId("academic-profile-prompt")).toHaveCount(0);

    // Editing remains available whenever they want it.
    await page.goto("/ar/learn/profile");
    await expect(page.getByTestId("academic-profile-form")).toBeVisible();

    await api.dispose();
    await context.close();
  });

  test("E an unprofiled Student completes a real access flow before onboarding", async ({
    browser,
  }, testInfo) => {
    const context = await browser.newContext({ locale: "en-US" });
    const { session } = await signInStudent(context, testInfo, ACADEMIC_INVITATION_TEST_SLOT);
    const api = await apiFor(session);

    // This Student has made no academic decision at all.
    const profile = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(profile.setup_state).toBe("NOT_STARTED");

    // The real access surfaces must all work regardless. Nothing redirects to
    // onboarding, and no route guard intercepts an access flow.
    const accessHistory = await api.get("/api/v1/me/course-access");
    expect(accessHistory.status()).toBe(200);
    const invitations = await api.get("/api/v1/me/course-access-invitations");
    expect(invitations.status()).toBe(200);

    const page = await context.newPage();
    await page.goto("/en/access");
    await expect(page).toHaveURL(/\/en\/access/);
    expect(await page.locator("body").innerText()).not.toContain("Tell us about your studies");

    // The Student's entitled Course is reachable with no profile whatsoever.
    const dashboard = await api.get("/api/v1/learn/dashboard");
    expect(dashboard.status()).toBe(200);
    const courses = ((await dashboard.json()) as { courses: { course_id: string }[] }).courses;
    expect(courses.length, "the rotating Student must hold real access").toBeGreaterThan(0);

    await page.goto(`/en/learn/courses/${courses[0].course_id}`);
    await expect(page).toHaveURL(new RegExp(courses[0].course_id));
    // Still NOT_STARTED: reaching a Course never consumed or forced onboarding.
    const unchanged = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(unchanged.setup_state).toBe("NOT_STARTED");

    await api.dispose();
    await context.close();
  });

  test("F changing major never revokes access", async ({ browser }, testInfo) => {
    const context = await browser.newContext({ locale: "en-US" });
    const { session } = await signInStudent(context, testInfo, ACADEMIC_ACCESS_PRESERVED_TEST_SLOT);
    const api = await apiFor(session);

    const before = ((await (await api.get("/api/v1/learn/dashboard")).json()) as {
      courses: { course_id: string }[];
    }).courses;
    expect(before.length, "the fixture Student must hold access for this to mean anything").toBeGreaterThan(0);
    const courseID = before[0].course_id;

    const page = await context.newPage();
    await page.goto(`/en/learn/courses/${courseID}`);
    await expect(page).toHaveURL(new RegExp(courseID));

    // Complete a Computer Science profile, then switch to a different College's
    // Program entirely.
    await page.goto("/en/learn/academic-profile");
    await page.getByTestId("profile-university").selectOption({ label: "Kuwait University" });
    await page.getByTestId("profile-college").selectOption({ label: "College of Science" });
    await page.getByTestId("profile-program").selectOption({ label: "Computer Science" });
    await page.getByTestId("profile-save").click();
    await page.waitForURL(/\/en\/learn\/dashboard/);

    await page.goto("/en/learn/profile");
    // The promise the Student is shown must be technically true.
    await expect(page.getByTestId("profile-access-promise")).toContainText(
      "Your courses and purchases are unaffected",
    );
    await page.getByTestId("profile-college").selectOption({ label: "College of Life Sciences" });
    await page.getByTestId("profile-program").selectOption({
      label: "Data Science and Artificial Intelligence",
    });
    await page.getByTestId("profile-save").click();
    await expect(page.getByTestId("profile-message")).toBeVisible();

    const saved = await (await api.get("/api/v1/me/academic-profile")).json();
    expect(saved.program_name).toBe("Data Science and Artificial Intelligence");

    // Access is untouched: the same Course, still on the dashboard, still open.
    const after = ((await (await api.get("/api/v1/learn/dashboard")).json()) as {
      courses: { course_id: string }[];
    }).courses;
    expect(after.map((course) => course.course_id)).toEqual(before.map((course) => course.course_id));

    await page.goto(`/en/learn/courses/${courseID}`);
    await expect(page).toHaveURL(new RegExp(courseID));
    const courseAccess = await api.get("/api/v1/me/course-access");
    expect(courseAccess.status()).toBe(200);

    await api.dispose();
    await context.close();
  });
});
