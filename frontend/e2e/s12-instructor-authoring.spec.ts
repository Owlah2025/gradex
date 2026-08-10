import { test, expect, request as playwrightRequest, type APIRequestContext, type Page } from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * Instructor Course Authoring Studio — persisted-workflow acceptance.
 *
 * The founder's manual test found that a Course created in this UI vanished on
 * refresh: the page held local demo state and called no authoring API. These
 * cases prove the opposite property — that every authored object survives a
 * full page reload because it lives in the database — and that ownership and
 * role boundaries still refuse everyone else.
 *
 * The real MP4 upload, worker processing, video attachment, and successful
 * submission are proved separately in `e2e/media-authoring`, which needs real
 * object storage and a running worker.
 */

const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };
const OTHER_INSTRUCTOR = { email: "instructor-other@example.test", accountID: "a0000000-0000-0000-0000-000000000004" };
const STUDENT = { email: "student-unentitled@example.test", accountID: "a0000000-0000-0000-0000-000000000099" };

const UUID_PATTERN = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

type Session = ReturnType<typeof issueRotatingSession>;

async function signIn(context: import("@playwright/test").BrowserContext, account: typeof INSTRUCTOR): Promise<Session> {
  const session = issueRotatingSession(account);
  const origin = new URL(frontendOrigin());
  // The studio is not a language-addressable route: it renders the visitor's
  // saved language, which defaults to Arabic. These assertions read English
  // copy, so the run states the same preference a founder would set with the
  // language toggle.
  await context.addInitScript(() => {
    window.localStorage.setItem("gradex.locale", "en");
  });
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
  return session;
}

/** Same-origin authenticated API caller, exactly as the browser makes them. */
async function apiContext(session: Session): Promise<APIRequestContext> {
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

async function openStudio(page: Page): Promise<void> {
  await page.goto("/en/instructor/courses");
  await expect(page.locator("h1")).toContainText("Course Authoring Studio");
  await expect(page.getByTestId("owned-course-list")).toBeVisible();
}

async function createCourse(page: Page, titleEn: string, titleAr: string): Promise<string> {
  await page.getByTestId("toggle-new-course").click();
  await page.getByTestId("new-course-title-ar").fill(titleAr);
  await page.getByTestId("new-course-title-en").fill(titleEn);
  await page.getByTestId("new-course-description-ar").fill("وصف اختبار القبول");
  await page.getByTestId("new-course-description-en").fill("Acceptance test description");
  await page.getByTestId("create-course").click();

  await expect(page.getByTestId("authoring-notice")).toContainText("Course created on the server");
  const rendered = await page.getByTestId("selected-course-id").innerText();
  const match = rendered.match(UUID_PATTERN);
  expect(match, "the studio must show a server-issued Course ID").not.toBeNull();
  return match![0];
}

test.describe("S12 Instructor authoring persistence", () => {
  test("A Course created in the studio survives a page reload", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signIn(context, INSTRUCTOR);
    const page = await context.newPage();

    await openStudio(page);
    const title = `Video Upload Manual Test ${Date.now()}`;
    const courseID = await createCourse(page, title, "اختبار رفع الفيديو");

    // The founder's finding, inverted: reload and the Course is still there,
    // with the same server-issued identifier.
    await page.reload();
    await expect(page.getByTestId(`owned-course-${courseID}`)).toContainText(title);
    await page.getByTestId(`owned-course-${courseID}`).click();
    await expect(page.getByTestId("selected-course-id")).toContainText(courseID);
    // The removed local-demo fixture must not reappear anywhere in production UI.
    await expect(page.locator("body")).not.toContainText("Local Demo Drafts");
    await expect(page.locator("body")).not.toContainText("course-demo-1");

    await context.close();
  });

  test("B Sections and Lessons persist with their exact authored structure", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signIn(context, INSTRUCTOR);
    const page = await context.newPage();

    await openStudio(page);
    const courseID = await createCourse(page, `Structured Course ${Date.now()}`, "دورة ذات بنية");

    await page.getByTestId("section-title-ar").fill("القسم الأول");
    await page.getByTestId("section-title-en").fill("Section One");
    await page.getByTestId("add-section").click();
    await expect(page.getByText("Section One")).toBeVisible();

    await page.reload();
    await page.getByTestId(`owned-course-${courseID}`).click();
    await expect(page.getByText("Section One")).toBeVisible();

    const sectionBlock = page.locator('[data-testid^="section-"]').first();
    const sectionTestID = await sectionBlock.getAttribute("data-testid");
    const sectionID = sectionTestID!.replace("section-", "");

    await page.getByTestId(`lesson-title-ar-${sectionID}`).fill("الدرس الأول");
    await page.getByTestId(`lesson-title-en-${sectionID}`).fill("Lesson One");
    await page.getByTestId(`add-lesson-${sectionID}`).click();
    await expect(page.getByText("Lesson One")).toBeVisible();

    await page.reload();
    await page.getByTestId(`owned-course-${courseID}`).click();
    await expect(page.getByText("Section One")).toBeVisible();
    await expect(page.getByText("Lesson One")).toBeVisible();
    await expect(page.locator('[data-testid^="lesson-video-none-"]').first()).toBeVisible();

    await context.close();
  });

  test("D another Instructor and a Student are refused the authoring surface", async ({ browser }) => {
    const ownerContext = await browser.newContext({ locale: "en-US" });
    await signIn(ownerContext, INSTRUCTOR);
    const page = await ownerContext.newPage();

    await openStudio(page);
    const courseID = await createCourse(page, `Ownership Course ${Date.now()}`, "دورة الملكية");
    const revisionID = (await page.getByTestId("selected-revision-id").innerText()).match(UUID_PATTERN)![0];

    const otherInstructor = await apiContext(issueRotatingSession(OTHER_INSTRUCTOR));
    const intrusion = await otherInstructor.post(
      `/api/v1/courses/${courseID}/revisions/${revisionID}/sections`,
      { data: { title_ar: "قسم دخيل", title_en: "Intruding section" } },
    );
    expect(intrusion.status(), "another Instructor must not mutate this Course").toBeGreaterThanOrEqual(400);

    const intrusionRead = await otherInstructor.get(`/api/v1/courses/${courseID}`);
    expect(intrusionRead.status(), "another Instructor must not read this Course's draft").toBeGreaterThanOrEqual(400);

    const student = await apiContext(issueRotatingSession(STUDENT));
    const studentCreate = await student.post("/api/v1/courses", {
      data: { title_ar: "دورة طالب", title_en: "Student course" },
    });
    expect(studentCreate.status(), "a Student must not use the authoring API").toBeGreaterThanOrEqual(400);

    const studentList = await student.get("/api/v1/courses");
    expect(studentList.status(), "a Student must not list authored Courses").toBeGreaterThanOrEqual(400);

    // The refusals changed nothing: the owner still sees exactly what it authored.
    await page.reload();
    await page.getByTestId(`owned-course-${courseID}`).click();
    await expect(page.locator("body")).not.toContainText("Intruding section");

    await otherInstructor.dispose();
    await student.dispose();
    await ownerContext.close();
  });

  test("E an incomplete submission is refused with the server's own reason", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    await signIn(context, INSTRUCTOR);
    const page = await context.newPage();

    await openStudio(page);
    await createCourse(page, `Incomplete Course ${Date.now()}`, "دورة غير مكتملة");

    await page.getByTestId("submit-for-review").click();

    // The Course has no taxonomy, no Sections, and no Lesson video, so the
    // server refuses it. The Instructor is shown which requirements failed
    // rather than a generic error.
    const failure = page.getByTestId("authoring-error");
    await expect(failure).toBeVisible();
    await expect(failure).toContainText("TAXONOMY_DIMENSION_MISSING");
    await expect(failure).toContainText("COURSE_EMPTY");

    await context.close();
  });
});
