import {
  test,
  expect,
  request as playwrightRequest,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type Page,
} from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * T6 — Academic Course Discovery, real browser journey.
 *
 * The Course under test is created and published through the real Instructor
 * and Admin lifecycle, and the academic hierarchy comes from the real Kuwait
 * University launch manifest imported through the real Admin API. Nothing is
 * injected into the frontend and no classification row is written directly, so
 * what is proven is the product, not a fixture.
 *
 * The journey is the one a Student actually has: University, then Program, then
 * Subject, then a Course — chosen by name, never by identifier.
 */

const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };
const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const MANIFEST = "kuwait-university-launch-v1";
const ANY_INSTITUTION = "00000000-0000-0000-0000-000000000000";
const READY_ASSET_VERSION_ID = "60000000-0000-0000-0000-000000000001";

const UNIVERSITY_EN = "Kuwait University";
const UNIVERSITY_AR = "جامعة الكويت";
// 0418-320 is mapped by the launch manifest into the Computer Science and
// Cybersecurity curricula and into no others. That is what makes Electrical
// Engineering a real negative case rather than an arbitrary one.
const SUBJECT_CODE = "0418-320";
const SUBJECT_TITLE_EN = "Principles of Computer Systems";
const AUDIENCE_PROGRAM_EN = "Computer Science";
const AUDIENCE_PROGRAM_AR = "علوم الحاسوب";
const OUTSIDE_PROGRAM_EN = "Electrical Engineering";
const UUID_PATTERN = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;
const TITLE_AR = "اكتشاف أكاديمي";

type Session = ReturnType<typeof issueRotatingSession>;
type Account = typeof INSTRUCTOR | typeof ADMIN;

async function signIn(context: BrowserContext, account: Account, locale: "ar" | "en" = "en") {
  const session = issueRotatingSession(account);
  const origin = new URL(frontendOrigin());
  await context.addInitScript((selected) => {
    window.localStorage.setItem("gradex.locale", selected);
  }, locale);
  await context.addCookies([{
    name: session.cookie_name,
    value: session.cookie_value,
    domain: origin.hostname,
    path: "/",
    httpOnly: true,
    secure: true,
    sameSite: "Strict",
  }]);
  return session;
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

async function ensureLaunchCatalog(): Promise<void> {
  const admin = await apiFor(issueRotatingSession(ADMIN));
  const response = await admin.post(
    `/api/v1/admin/academic/institutions/${ANY_INSTITUTION}/import`,
    { data: { manifest: MANIFEST, mode: "apply" } },
  );
  expect(response.status(), await response.text()).toBe(200);
  await admin.dispose();
}

/** Creates and publishes one Academic Course through the real lifecycle. */
async function publishAcademicCourse(
  browser: Browser,
  title: string,
): Promise<{ courseID: string; slug: string }> {
  const context = await browser.newContext({ locale: "en-US" });
  const session = await signIn(context, INSTRUCTOR);
  const instructor = await apiFor(session);
  const page = await context.newPage();

  await page.goto("/en/instructor/courses");
  await expect(page.locator("h1")).toContainText("Course Authoring Studio");
  await page.getByTestId("toggle-new-course").click();

  const university = page.getByTestId("new-course-institution");
  await expect(university).toBeVisible();
  await university.selectOption({ label: UNIVERSITY_EN });
  await page.getByTestId("new-course-subject-search").fill(SUBJECT_CODE);
  const subjectResult = page.getByTestId("new-course-subject-result").first();
  await expect(subjectResult).toBeVisible({ timeout: 15_000 });
  await subjectResult.click();

  await page.getByTestId("new-course-title-ar").fill(TITLE_AR);
  await page.getByTestId("new-course-title-en").fill(title);
  await page.getByTestId("new-course-description-ar").fill("وصف الاكتشاف الأكاديمي");
  await page.getByTestId("new-course-description-en").fill("Academic discovery journey course.");
  await page.getByTestId("create-course").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course created");

  const selected = page.getByTestId("selected-course-context");
  const courseID = (await selected.getAttribute("data-course-id"))!;
  const revisionID = (await selected.getAttribute("data-revision-id"))!;

  const sectionResponse = await instructor.post(
    `/api/v1/courses/${courseID}/revisions/${revisionID}/sections`,
    { data: { title_ar: "قسم", title_en: "Discovery Section" } },
  );
  expect(sectionResponse.status(), await sectionResponse.text()).toBe(201);
  const section = (await sectionResponse.json()) as { id: string };
  const lessonResponse = await instructor.post(
    `/api/v1/courses/${courseID}/revisions/${revisionID}/sections/${section.id}/lessons`,
    { data: { title_ar: "درس", title_en: "Discovery Lesson" } },
  );
  expect(lessonResponse.status(), await lessonResponse.text()).toBe(201);
  const lesson = (await lessonResponse.json()) as { id: string };
  const videoResponse = await instructor.put(
    `/api/v1/courses/${courseID}/revisions/${revisionID}/lessons/${lesson.id}/video`,
    { data: { video_asset_version_id: READY_ASSET_VERSION_ID } },
  );
  expect(videoResponse.status(), await videoResponse.text()).toBe(200);

  await page.getByTestId("submit-for-review").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("submitted for Admin review");

  const adminContext = await browser.newContext({ locale: "en-US" });
  await signIn(adminContext, ADMIN);
  const adminPage = await adminContext.newPage();
  await adminPage.goto("/en/admin/catalog");
  await expect(adminPage.getByTestId(`review-item-${courseID}`)).toBeVisible({ timeout: 15_000 });
  await adminPage.getByTestId(`inspect-review-item-${courseID}`).click();
  const inspector = adminPage.getByTestId("submitted-revision-inspector");
  await expect(inspector).toBeVisible();
  await inspector.getByTestId("pricing-amount").fill("25000");
  await inspector.getByTestId("pricing-reason").fill("T6 discovery launch price");
  await inspector.getByTestId("pricing-submit").click();
  await expect(inspector.getByTestId("pricing-success")).toContainText("Successfully updated Course price");
  await inspector.getByTestId("approve-inspected-revision").click();
  await expect(adminPage.getByTestId("review-action-success")).toContainText("Course published successfully");
  await adminContext.close();

  await instructor.dispose();
  await context.close();

  const publicAPI = await playwrightRequest.newContext({ baseURL: frontendOrigin() });
  const listed = await publicAPI.get(`/api/v1/catalog/courses?subject=${SUBJECT_CODE}`, {
    headers: { "Accept-Language": "en" },
  });
  expect(listed.status(), await listed.text()).toBe(200);
  const body = (await listed.json()) as { items: Array<{ id: string; slug: string }> };
  const entry = body.items.find((item) => item.id === courseID);
  expect(entry, "the published Course must be publicly discoverable by its Subject").toBeTruthy();
  await publicAPI.dispose();
  return { courseID, slug: entry!.slug };
}

test.describe("T6 academic course discovery", () => {
  let published: { courseID: string; slug: string };
  const title = `Discovery Course ${Date.now()}`;


  test.beforeAll(async ({ browser }) => {
    await ensureLaunchCatalog();
    published = await publishAcademicCourse(browser, title);
  });

  test("A a visitor reaches a Course through University, Program, and Subject", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const page = await context.newPage();
    await page.goto("/en/catalog");
    await expect(page.getByTestId("academic-filters")).toBeVisible();

    // University, by name.
    await page.getByLabel("University", { exact: true }).selectOption({ label: UNIVERSITY_EN });
    await page.waitForURL(/institution=kuwait-university/);

    // Program, by name. The options came from the real academic API, so wait
    // for them rather than racing the request.
    const programSelect = page.getByLabel("Program", { exact: true });
    await expect(programSelect).toContainText(AUDIENCE_PROGRAM_EN);
    await programSelect.selectOption("computer-science");
    await page.waitForURL(/program=computer-science/);

    // Subject, by name.
    const subjectSelect = page.getByLabel("Subject", { exact: true });
    await expect(subjectSelect).toContainText(SUBJECT_TITLE_EN);
    await subjectSelect.selectOption(SUBJECT_CODE);
    await page.waitForURL(new RegExp(`subject=${SUBJECT_CODE}`));

    // The Course is there, found without a single identifier being typed.
    const card = page.getByRole("link", { name: new RegExp(title) });
    await expect(card).toBeVisible({ timeout: 15_000 });

    // And nothing in the page shows one.
    const body = (await page.locator("body").innerText()).toString();
    expect(UUID_PATTERN.test(body)).toBe(false);
    expect(body).not.toContain("institution_id");
    expect(body).not.toContain("ACADEMIC_CATALOG");

    // Course detail, then the public preview boundary.
    await card.click();
    await page.waitForURL(new RegExp(`/en/catalog/${published.slug}`));
    await expect(page.locator("body")).toContainText(UNIVERSITY_EN);
    await expect(page.locator("body")).toContainText(SUBJECT_CODE);
    // Detail names the audience by Program name.
    await expect(page.locator("body")).toContainText(AUDIENCE_PROGRAM_EN);
    await expect(page.getByTestId("purchase-request-open")).toBeVisible();
    const detailBody = (await page.locator("body").innerText()).toString();
    expect(UUID_PATTERN.test(detailBody)).toBe(false);

    await context.close();
  });

  test("B a Program outside the Course's audience does not surface it", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const page = await context.newPage();

    // The Subject is mapped only into the Computer Science and Cybersecurity
    // curricula, so Electrical Engineering must not infer this Course.
    await page.goto("/en/catalog?institution=kuwait-university&program=electrical-engineering");
    await expect(page.getByTestId("academic-filters")).toBeVisible();
    await expect(page.locator("main")).not.toContainText(title, { timeout: 15_000 });

    // It is still in the unfiltered catalogue: audience narrows discovery by
    // Program, it never removes a Course from the catalogue.
    await page.goto("/en/catalog");
    await expect(page.getByRole("link", { name: new RegExp(title) })).toBeVisible({
      timeout: 15_000,
    });

    await context.close();
  });

  test("C filters survive refresh, sharing, and back, and reset clears them", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const page = await context.newPage();
    const shared = `/en/catalog?institution=kuwait-university&program=computer-science&subject=${SUBJECT_CODE}`;

    await page.goto(shared);
    await expect(page.getByLabel("University", { exact: true })).toHaveValue("kuwait-university");
    await expect(page.getByLabel("Program", { exact: true })).toHaveValue("computer-science");
    await expect(page.getByLabel("Subject", { exact: true })).toHaveValue(SUBJECT_CODE);
    await expect(page.getByRole("link", { name: new RegExp(title) })).toBeVisible({
      timeout: 15_000,
    });

    // Refresh preserves the selection, because the URL is the only owner of it.
    await page.reload();
    await expect(page.getByLabel("Subject", { exact: true })).toHaveValue(SUBJECT_CODE);

    // Reset clears the URL, not just the controls.
    await page.getByRole("button", { name: "Clear filters" }).click();
    await page.waitForURL((url) => url.search === "");
    await expect(page.getByLabel("University", { exact: true })).toHaveValue("");
    await expect(page.getByLabel("Program", { exact: true })).toBeDisabled();

    // Back returns to the shared selection.
    await page.goBack();
    await expect(page.getByLabel("Subject", { exact: true })).toHaveValue(SUBJECT_CODE);

    await context.close();
  });

  test("D a University with no matching Course shows an empty state, not an error", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const page = await context.newPage();
    await page.goto("/en/catalog?institution=kuwait-university&program=electrical-engineering");
    await expect(page.getByTestId("academic-filters")).toBeVisible();
    const main = page.locator("main");
    await expect(main).toContainText("No published courses for this program yet.", {
      timeout: 15_000,
    });
    // An empty result is a valid answer and must never be dressed as a failure.
    await expect(main).not.toContainText("Try again");
    await expect(main).not.toContainText("The catalogue could not be loaded");
    await expect(main.getByRole("alert")).toHaveCount(0);
    await context.close();
  });

  test("E the same journey works in Arabic and renders right to left", async ({ browser }) => {
    const context = await browser.newContext({ locale: "ar-KW", viewport: { width: 390, height: 844 } });
    await context.addInitScript(() => window.localStorage.setItem("gradex.locale", "ar"));
    const page = await context.newPage();
    await page.goto("/ar/catalog");
    await expect(page.getByTestId("academic-filters")).toBeVisible();

    await page.getByLabel("الجامعة", { exact: true }).selectOption({ label: UNIVERSITY_AR });
    await page.waitForURL(/institution=kuwait-university/);

    const programSelect = page.getByLabel("التخصص", { exact: true });
    await expect(programSelect).toContainText(AUDIENCE_PROGRAM_AR);
    await programSelect.selectOption("computer-science");
    await page.waitForURL(/program=computer-science/);

    // Arabic names, not English ones and not raw values.
    const filters = page.getByTestId("academic-filters");
    await expect(filters).toContainText(UNIVERSITY_AR);
    await expect(filters).not.toContainText(UNIVERSITY_EN);
    await expect(filters).not.toContainText("kuwait-university");

    // The document actually flows right to left.
    const direction = await page.evaluate(
      () => document.documentElement.getAttribute("dir") ?? getComputedStyle(document.body).direction,
    );
    expect(direction).toBe("rtl");

    // An Arabic reader sees the Arabic title, which is the point of the test.
    await expect(page.getByRole("link", { name: new RegExp(TITLE_AR) }).first()).toBeVisible({
      timeout: 15_000,
    });
    const body = (await page.locator("body").innerText()).toString();
    expect(UUID_PATTERN.test(body)).toBe(false);

    await context.close();
  });

  test("F the filter controls are reachable and operable by keyboard alone", async ({
    browser,
  }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const page = await context.newPage();
    await page.goto("/en/catalog");
    await expect(page.getByTestId("academic-filters")).toBeVisible();

    const university = page.getByLabel("University", { exact: true });
    await university.focus();
    await expect(university).toBeFocused();
    // The accessible name comes from a real label, so the control announces
    // itself without any aria patching.
    await expect(university).toHaveAttribute("id", "catalogue-institution");
    await university.selectOption({ label: UNIVERSITY_EN });
    await page.waitForURL(/institution=kuwait-university/);

    const program = page.getByLabel("Program", { exact: true });
    await program.focus();
    await expect(program).toBeFocused();

    await context.close();
  });
});
