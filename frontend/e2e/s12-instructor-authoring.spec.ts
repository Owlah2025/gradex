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
const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };

// T4-B: the Academic Catalog an ordinary Course is now authored against.
const AUTHORING_UNIVERSITY = "S12 Authoring University";
const AUTHORING_SUBJECT_CODE = "S12-101";

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

/**
 * Creates a Course through the studio.
 *
 * T4-B (§48): ordinary Instructor creation is Academic Catalog based, so the
 * flow now begins with the university and a canonical Subject. The rest of this
 * helper — and every persistence assertion built on it — is unchanged.
 */
async function createCourse(page: Page, titleEn: string, titleAr: string): Promise<string> {
  await page.getByTestId("toggle-new-course").click();
  await selectAcademicSubject(page);
  await page.getByTestId("new-course-title-ar").fill(titleAr);
  await page.getByTestId("new-course-title-en").fill(titleEn);
  await page.getByTestId("new-course-description-ar").fill("وصف اختبار القبول");
  await page.getByTestId("new-course-description-en").fill("Acceptance test description");
  await page.getByTestId("create-course").click();

  await expect(page.getByTestId("authoring-notice")).toContainText("Course created");
  const courseID = await page.getByTestId("selected-course-context").getAttribute("data-course-id");
  expect(courseID, "the studio must retain a server-issued Course ID without displaying it").toMatch(UUID_PATTERN);
  return courseID!;
}

/**
 * University then Subject, exactly as an Instructor does it: pick the
 * university, type a Subject code, choose the result. No identifier is typed.
 */
async function selectAcademicSubject(page: Page, code = AUTHORING_SUBJECT_CODE): Promise<void> {
  const institution = page.getByTestId("new-course-institution");
  await expect(institution).toBeVisible();
  await institution.selectOption({ label: AUTHORING_UNIVERSITY });
  await page.getByTestId("new-course-subject-search").fill(code);
  const firstResult = page.getByTestId("new-course-subject-result").first();
  await expect(firstResult).toBeVisible({ timeout: 15_000 });
  await firstResult.click();
  await expect(page.getByTestId("new-course-selected-subject")).toBeVisible();
}

/**
 * Creates a small catalog owned by this spec.
 *
 * Deliberately NOT the Kuwait University manifest: this spec runs before
 * `t2-launch-catalog-data`, whose dry run must find the launch catalog
 * unimported. These cases only need one university and one Subject to author
 * against, so they create their own and leave the launch manifest alone.
 */
async function ensureAuthoringCatalog(): Promise<void> {
  const admin = await apiContext(issueRotatingSession(ADMIN));
  const existing = await admin.get("/api/v1/admin/academic/institutions");
  const institutions = existing.ok() ? await existing.json() : [];
  const already = Array.isArray(institutions)
    ? institutions.find((entry: { name_en?: string }) => entry.name_en === AUTHORING_UNIVERSITY)
    : undefined;
  let institutionID: string = already?.id ?? "";
  if (!institutionID) {
    const created = await admin.post("/api/v1/admin/academic/institutions", {
      data: {
        country_code: "KW",
        slug: "s12-authoring-university",
        name_ar: "جامعة التأليف",
        name_en: AUTHORING_UNIVERSITY,
        max_academic_level: 4,
      },
    });
    expect(created.status()).toBe(201);
    institutionID = (await created.json()).id;
    const subject = await admin.post(`/api/v1/admin/academic/institutions/${institutionID}/subjects`, {
      data: {
        official_code: AUTHORING_SUBJECT_CODE,
        title_ar: "مادة التأليف",
        title_en: "Authoring Subject",
      },
    });
    expect(subject.status()).toBe(201);
  }
  await admin.dispose();
}

test.describe("S12 Instructor authoring persistence", () => {
  test.beforeAll(async () => {
    await ensureAuthoringCatalog();
  });

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
    await expect(page.getByTestId("selected-course-context")).toHaveAttribute("data-course-id", courseID);
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
    const revisionID = (await page.getByTestId("selected-course-context").getAttribute("data-revision-id"))!;
    expect(revisionID).toMatch(UUID_PATTERN);

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

    // The Course has a canonical Subject but no Sections and no Lesson video,
    // so the server refuses it. The Instructor is shown which requirements
    // failed rather than a generic error.
    //
    // T4-B (§48): this previously also asserted TAXONOMY_DIMENSION_MISSING.
    // That gate belongs to the legacy classification, which an Academic Course
    // does not carry and must never be asked for — the property is unchanged,
    // the dimension that fails is. The legacy gate itself stays proven for
    // legacy Courses in the backend suite until T5.
    const failure = page.getByTestId("authoring-error");
    await expect(failure).toBeVisible();
    await expect(failure).toContainText("COURSE_EMPTY");
    await expect(failure).not.toContainText("TAXONOMY_DIMENSION_MISSING");

    await context.close();
  });
});
