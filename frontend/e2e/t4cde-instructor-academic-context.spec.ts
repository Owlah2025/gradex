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

const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };
const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const MANIFEST = "kuwait-university-launch-v1";
const ANY_INSTITUTION = "00000000-0000-0000-0000-000000000000";
const LAUNCH_UNIVERSITY = "Kuwait University";
const SHARED_SUBJECT_CODE = "0418-320";
const ALT_SUBJECT_CODE = "0418-321";
const READY_ASSET_VERSION_ID = "60000000-0000-0000-0000-000000000001";
const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

type Session = ReturnType<typeof issueRotatingSession>;
type Account = typeof INSTRUCTOR | typeof ADMIN;

async function signIn(context: BrowserContext, account: Account): Promise<Session> {
  const session = issueRotatingSession(account);
  const origin = new URL(frontendOrigin());
  await context.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
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
  const response = await admin.post(`/api/v1/admin/academic/institutions/${ANY_INSTITUTION}/import`, {
    data: { manifest: MANIFEST, mode: "apply" },
  });
  expect(response.status(), await response.text()).toBe(200);
  await admin.dispose();
}

async function openStudio(page: Page): Promise<void> {
  await page.goto("/en/instructor/courses");
  await expect(page.locator("h1")).toContainText("Course Authoring Studio");
}

async function chooseSubject(page: Page, code: string, prefix = "new-course"): Promise<void> {
  const university = page.getByTestId(`${prefix}-institution`);
  await expect(university).toBeVisible();
  await university.selectOption({ label: LAUNCH_UNIVERSITY });
  await page.getByTestId(`${prefix}-subject-search`).fill(code);
  const result = page.getByTestId(`${prefix}-subject-result`).first();
  await expect(result).toBeVisible({ timeout: 15_000 });
  await result.click();
}

async function createAcademicCourse(page: Page, title: string): Promise<{ courseID: string; revisionID: string }> {
  await page.getByTestId("toggle-new-course").click();
  await chooseSubject(page, SHARED_SUBJECT_CODE);
  await expect(page.getByTestId("new-course-audience")).toContainText("Computer Science");
  await expect(page.getByTestId("new-course-audience")).toContainText("Cybersecurity");
  await page.getByTestId("new-course-title-ar").fill("كورس سياق أكاديمي");
  await page.getByTestId("new-course-title-en").fill(title);
  await page.getByTestId("new-course-description-ar").fill("وصف أكاديمي");
  await page.getByTestId("new-course-description-en").fill("Academic context journey");
  await page.getByTestId("create-course").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("Course created");
  const selected = page.getByTestId("selected-course-context");
  const courseID = (await selected.getAttribute("data-course-id"))!;
  const revisionID = (await selected.getAttribute("data-revision-id"))!;
  expect(courseID).toMatch(UUID_PATTERN);
  expect(revisionID).toMatch(UUID_PATTERN);
  await expect(page.locator("body")).not.toContainText(courseID);
  await expect(page.locator("body")).not.toContainText(revisionID);
  return { courseID, revisionID };
}

async function makeRevisionPublishable(
  instructor: APIRequestContext,
  courseID: string,
  revisionID: string,
): Promise<void> {
  const sectionResponse = await instructor.post(`/api/v1/courses/${courseID}/revisions/${revisionID}/sections`, {
    data: { title_ar: "قسم", title_en: "Academic Section" },
  });
  expect(sectionResponse.status(), await sectionResponse.text()).toBe(201);
  const section = await sectionResponse.json() as { id: string };
  const lessonResponse = await instructor.post(
    `/api/v1/courses/${courseID}/revisions/${revisionID}/sections/${section.id}/lessons`,
    { data: { title_ar: "درس", title_en: "Academic Lesson" } },
  );
  expect(lessonResponse.status(), await lessonResponse.text()).toBe(201);
  const lesson = await lessonResponse.json() as { id: string };
  const videoResponse = await instructor.put(
    `/api/v1/courses/${courseID}/revisions/${revisionID}/lessons/${lesson.id}/video`,
    { data: { video_asset_version_id: READY_ASSET_VERSION_ID } },
  );
  expect(videoResponse.status(), await videoResponse.text()).toBe(200);
}

async function openAdminReview(browser: Browser, courseID: string): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ locale: "en-US" });
  await signIn(context, ADMIN);
  const page = await context.newPage();
  await page.goto("/en/admin/catalog");
  await expect(page.getByTestId(`review-item-${courseID}`)).toBeVisible({ timeout: 15_000 });
  await page.getByTestId(`inspect-review-item-${courseID}`).click();
  await expect(page.getByTestId("submitted-revision-inspector")).toBeVisible();
  return { context, page };
}

async function approveInspected(page: Page, setPrice: boolean): Promise<void> {
  const inspector = page.getByTestId("submitted-revision-inspector");
  if (setPrice) {
    await inspector.getByTestId("pricing-amount").fill("25000");
    await inspector.getByTestId("pricing-reason").fill("T4 Academic Course launch price");
    await inspector.getByTestId("pricing-submit").click();
    await expect(inspector.getByTestId("pricing-success")).toContainText("Successfully updated Course price");
  }
  await inspector.getByTestId("approve-inspected-revision").click();
  await expect(page.getByTestId("review-action-success")).toContainText("Course published successfully");
}

async function createMissingSubjectCourse(
  page: Page,
  title: string,
  proposedCode: string,
): Promise<string> {
  await page.getByTestId("toggle-new-course").click();
  const university = page.getByTestId("new-course-institution");
  await university.selectOption({ label: LAUNCH_UNIVERSITY });
  await page.getByTestId("new-course-subject-search").fill(`missing-${Date.now()}`);
  await expect(page.getByTestId("new-course-subject-empty")).toBeVisible({ timeout: 15_000 });
  await page.getByTestId("new-course-request-subject").click();
  await page.getByTestId("subject-request-code").fill(proposedCode);
  await page.getByTestId("subject-request-title-ar").fill("مادة جديدة مطلوبة");
  await page.getByTestId("subject-request-title-en").fill(`Requested ${title}`);
  await page.getByTestId("subject-request-note").fill("Required for this draft");
  await page.getByTestId("new-course-title-ar").fill("مسودة مادة مفقودة");
  await page.getByTestId("new-course-title-en").fill(title);
  await page.getByTestId("new-course-description-en").fill("Continue drafting while pending");
  await page.getByTestId("create-course").click();
  await expect(page.getByTestId("authoring-notice")).toContainText("subject request was sent for review");
  const courseID = (await page.getByTestId("selected-course-context").getAttribute("data-course-id"))!;
  await expect(page.getByTestId("subject-request-pending")).toContainText("Pending review", { timeout: 15_000 });
  return courseID;
}

test.describe("T4-C/D/E Instructor Academic Context", () => {
  test.setTimeout(180_000);

  test.beforeAll(async () => {
    await ensureLaunchCatalog();
  });

  test("C1-C5 and E: audience follows candidate/live review semantics and the Academic Course publishes publicly", async ({ browser }) => {
    const instructorContext = await browser.newContext({ locale: "en-US" });
    const instructorSession = await signIn(instructorContext, INSTRUCTOR);
    const instructorAPI = await apiFor(instructorSession);
    const page = await instructorContext.newPage();
    await openStudio(page);

    const title = `T4 Academic Lifecycle ${Date.now()}`;
    const { courseID, revisionID } = await createAcademicCourse(page, title);
    const originalSubject = SHARED_SUBJECT_CODE;

    // C1/C2 — automatic inference, then an explicit valid subset.
    await expect(page.getByTestId("academic-course-audience-mode")).toContainText("Programs that see this course");
    await page.getByTestId("academic-course-customize-audience").click();
    const options = page.getByTestId("academic-course-audience-option");
    expect(await options.count()).toBeGreaterThan(1);
    for (let index = 2; index < await options.count(); index += 1) {
      await options.nth(index).uncheck();
    }
    await page.getByTestId("academic-course-save-audience").click();
    await expect(page.getByTestId("academic-course-audience-mode")).toContainText("Chosen programs");

    const detailResponse = await instructorAPI.get(`/api/v1/courses/${courseID}`);
    expect(detailResponse.status()).toBe(200);
    const customized = await detailResponse.json() as any;
    expect(customized.editable_revision.audience.mode).toBe("CUSTOMIZED");
    expect(customized.editable_revision.audience.programs).toHaveLength(2);

    // C5 — a same-Institution but unmapped Program is refused by the server.
    const adminAPI = await apiFor(issueRotatingSession(ADMIN));
    const institutions = await (await adminAPI.get("/api/v1/admin/academic/institutions")).json() as Array<{ id: string; name_en: string }>;
    const institutionID = institutions.find((item) => item.name_en === LAUNCH_UNIVERSITY)!.id;
    const unrelatedResponse = await adminAPI.post(`/api/v1/admin/academic/institutions/${institutionID}/programs`, {
      data: {
        slug: `t4-unrelated-${Date.now()}`,
        name_ar: "تخصص غير مرتبط", name_en: `Unrelated T4 Program ${Date.now()}`, degree_kind: "BSC",
      },
    });
    expect(unrelatedResponse.status(), await unrelatedResponse.text()).toBe(201);
    const unrelatedProgram = await unrelatedResponse.json() as { id: string };
    const tamper = await instructorAPI.put(`/api/v1/courses/${courseID}/revisions/${revisionID}/audience`, {
      data: { program_ids: [unrelatedProgram.id] },
    });
    expect(tamper.status()).toBe(422);

    await makeRevisionPublishable(instructorAPI, courseID, revisionID);
    await page.reload();
    await page.getByTestId(`owned-course-${courseID}`).click();
    await page.getByTestId("submit-for-review").click();
    await page.getByTestId("submit-confirm").getByTestId("confirm-accept").click();
    await expect(page.getByTestId("authoring-notice")).toContainText("An administrator will review it");

    const firstReview = await openAdminReview(browser, courseID);
    const inspector = firstReview.page.getByTestId("submitted-revision-inspector");
    await expect(inspector.getByTestId("submitted-academic-university")).toContainText(LAUNCH_UNIVERSITY);
    await expect(inspector.getByTestId("submitted-academic-subject")).toContainText(originalSubject);
    await expect(inspector.getByTestId("submitted-academic-audience")).toContainText("Customized");
    await expect(inspector.getByTestId("submitted-academic-audience").locator("li")).toHaveCount(2);
    await expect(inspector.getByTestId("submitted-study-year")).toHaveCount(0);
    await expect(inspector.getByTestId("submitted-major")).toHaveCount(0);
    await approveInspected(firstReview.page, true);
    await firstReview.context.close();

    let owned = await (await instructorAPI.get(`/api/v1/courses/${courseID}`)).json() as any;
    expect(owned.live_revision.audience.mode).toBe("CUSTOMIZED");
    expect(owned.live_revision.audience.programs).toHaveLength(2);
    const liveRevision1 = owned.live_revision.id;
    expect(owned.academic_context.subject.official_code).toBe(originalSubject);

    // Public compatibility and q search by normalized Subject code.
    const publicAPI = await playwrightRequest.newContext({ baseURL: frontendOrigin() });
    const publicSearch = await publicAPI.get("/api/v1/catalog/courses?q=0418320", { headers: { "Accept-Language": "en" } });
    expect(publicSearch.status()).toBe(200);
    const searchBody = await publicSearch.json() as { items: Array<any> };
    const publicCourse = searchBody.items.find((item) => item.id === courseID);
    expect(publicCourse?.university?.label).toBe(LAUNCH_UNIVERSITY);
    expect(publicCourse?.subject?.code).toBe(originalSubject);
    const publicPage = await browser.newPage();
    await publicPage.goto(`/en/catalog/${publicCourse.slug}`);
    await expect(publicPage.locator("body")).toContainText(LAUNCH_UNIVERSITY);
    await expect(publicPage.locator("body")).toContainText(originalSubject);
    await expect(publicPage.getByTestId("purchase-request-open")).toBeVisible();
    await publicPage.close();
    await publicAPI.dispose();

    // C3 — candidate clones two targets; edit to one while live R1 stays two.
    await page.reload();
    await page.getByTestId(`owned-course-${courseID}`).click();
    await page.getByTestId("start-revision").click();
    await expect(page.getByTestId("academic-course-audience-mode")).toContainText("Chosen programs");
    await page.getByTestId("academic-course-edit-audience").click();
    const checked = page.locator('[data-testid="academic-course-audience-option"]:checked');
    await expect(checked).toHaveCount(2);
    await checked.nth(1).uncheck();
    await page.getByTestId("academic-course-save-audience").click();
    owned = await (await instructorAPI.get(`/api/v1/courses/${courseID}`)).json() as any;
    expect(owned.live_revision.id).toBe(liveRevision1);
    expect(owned.live_revision.audience.programs).toHaveLength(2);
    expect(owned.editable_revision.audience.programs).toHaveLength(1);
    await page.getByTestId("submit-for-review").click();
    await page.getByTestId("submit-confirm").getByTestId("confirm-accept").click();
    const secondReview = await openAdminReview(browser, courseID);
    await expect(secondReview.page.getByTestId("submitted-academic-audience").locator("li")).toHaveCount(1);
    await approveInspected(secondReview.page, false);
    await secondReview.context.close();
    owned = await (await instructorAPI.get(`/api/v1/courses/${courseID}`)).json() as any;
    expect(owned.live_revision.audience.programs).toHaveLength(1);
    expect(owned.academic_context.subject.official_code).toBe(originalSubject);

    // C4 — a later candidate resets to zero rows and immediately re-enters
    // automatic inference without mutating the live one-target revision.
    await page.reload();
    await page.getByTestId(`owned-course-${courseID}`).click();
    await page.getByTestId("start-revision").click();
    await page.getByTestId("academic-course-use-automatic-audience").click();
    await expect(page.getByTestId("academic-course-audience-mode")).toContainText("Programs that see this course");
    owned = await (await instructorAPI.get(`/api/v1/courses/${courseID}`)).json() as any;
    expect(owned.live_revision.audience.mode).toBe("CUSTOMIZED");
    expect(owned.live_revision.audience.programs).toHaveLength(1);
    expect(owned.editable_revision.audience.mode).toBe("AUTOMATIC");
    expect(owned.editable_revision.audience.programs.length).toBeGreaterThan(1);

    await adminAPI.dispose();
    await instructorAPI.dispose();
    await instructorContext.close();
  });

  test("D1 Link Existing: request UI resolves to a canonical Subject and removes only the Subject submission block", async ({ browser }) => {
    const instructorContext = await browser.newContext({ locale: "en-US" });
    const instructorSession = await signIn(instructorContext, INSTRUCTOR);
    const instructorAPI = await apiFor(instructorSession);
    const page = await instructorContext.newPage();
    await openStudio(page);
    const title = `T4D Link ${Date.now()}`;
    const courseID = await createMissingSubjectCourse(page, title, "0418-321");

    const adminContext = await browser.newContext({ locale: "en-US" });
    await signIn(adminContext, ADMIN);
    const adminPage = await adminContext.newPage();
    await adminPage.goto("/en/admin/academic-catalog");
    const item = adminPage.getByTestId("subject-request-item").filter({ hasText: title });
    await expect(item).toBeVisible({ timeout: 15_000 });
    await item.getByTestId("subject-request-existing-search").fill(ALT_SUBJECT_CODE);
    await item.getByTestId("subject-request-existing-search-submit").click();
    const resultSelect = item.getByTestId("subject-request-existing-result");
    const existingValue = await resultSelect.locator("option").filter({ hasText: ALT_SUBJECT_CODE }).getAttribute("value");
    await resultSelect.selectOption(existingValue!);
    await item.getByTestId("subject-request-link").click();
    await expect(item).toHaveCount(0);

    await page.reload();
    await page.getByTestId(`owned-course-${courseID}`).click();
    await expect(page.getByTestId("academic-course-subject")).toContainText(ALT_SUBJECT_CODE);
    const detail = await (await instructorAPI.get(`/api/v1/courses/${courseID}`)).json() as any;
    const submit = await instructorAPI.post(`/api/v1/courses/${courseID}/revisions/${detail.editable_revision.id}/submit`);
    expect(submit.status()).toBe(422);
    expect(await submit.text()).not.toContain("ACADEMIC_SUBJECT_MISSING");

    await adminContext.close();
    await instructorAPI.dispose();
    await instructorContext.close();
  });

  test("D2 Approve New: Admin creates one canonical Subject and Instructor cannot mutate its metadata", async ({ browser }) => {
    const instructorContext = await browser.newContext({ locale: "en-US" });
    const instructorSession = await signIn(instructorContext, INSTRUCTOR);
    const instructorAPI = await apiFor(instructorSession);
    const page = await instructorContext.newPage();
    await openStudio(page);
    const title = `T4D New ${Date.now()}`;
    const code = `T4D-${Date.now()}`;
    const courseID = await createMissingSubjectCourse(page, title, code);

    const adminContext = await browser.newContext({ locale: "en-US" });
    const adminSession = await signIn(adminContext, ADMIN);
    const adminAPI = await apiFor(adminSession);
    const adminPage = await adminContext.newPage();
    await adminPage.goto("/en/admin/academic-catalog");
    const item = adminPage.getByTestId("subject-request-item").filter({ hasText: title });
    await expect(item).toBeVisible({ timeout: 15_000 });
    await item.getByTestId("subject-request-approve-new").click();
    await expect(item).toHaveCount(0);

    const course = await (await instructorAPI.get(`/api/v1/courses/${courseID}`)).json() as any;
    expect(course.subject_id).toMatch(UUID_PATTERN);
    const subjects = await (await adminAPI.get(`/api/v1/admin/academic/institutions/${course.institution_id}/subjects?q=${encodeURIComponent(code)}`)).json() as Array<any>;
    expect(subjects.filter((subject) => subject.official_code === code)).toHaveLength(1);
    const mutate = await instructorAPI.patch(`/api/v1/admin/academic/subjects/${course.subject_id}`, {
      data: { title_en: "Tampered" },
    });
    expect(mutate.status()).toBe(403);

    await adminAPI.dispose();
    await adminContext.close();
    await instructorAPI.dispose();
    await instructorContext.close();
  });

  test("D3 Reject: required reason returns to the Instructor and the Course stays a draft", async ({ browser }) => {
    const instructorContext = await browser.newContext({ locale: "en-US" });
    await signIn(instructorContext, INSTRUCTOR);
    const page = await instructorContext.newPage();
    await openStudio(page);
    const title = `T4D Reject ${Date.now()}`;
    const courseID = await createMissingSubjectCourse(page, title, `REJECT-${Date.now()}`);

    const adminContext = await browser.newContext({ locale: "en-US" });
    await signIn(adminContext, ADMIN);
    const adminPage = await adminContext.newPage();
    await adminPage.goto("/en/admin/academic-catalog");
    const item = adminPage.getByTestId("subject-request-item").filter({ hasText: title });
    await expect(item).toBeVisible({ timeout: 15_000 });
    await expect(item.getByTestId("subject-request-reject")).toBeDisabled();
    await item.getByTestId("subject-request-reject-reason").fill("Use the official university title.");
    await item.getByTestId("subject-request-reject").click();
    await expect(item).toHaveCount(0);

    await page.reload();
    await page.getByTestId(`owned-course-${courseID}`).click();
    await expect(page.getByTestId("subject-request-rejected")).toContainText("Use the official university title.");
    await expect(page.getByTestId("revision-state")).toContainText("Draft");

    await adminContext.close();
    await instructorContext.close();
  });

  test("D4 Race: Admin resolution cannot overwrite a Subject the Instructor selected first", async ({ browser }) => {
    const instructorContext = await browser.newContext({ locale: "en-US" });
    const instructorSession = await signIn(instructorContext, INSTRUCTOR);
    const instructorAPI = await apiFor(instructorSession);
    const page = await instructorContext.newPage();
    await openStudio(page);
    const title = `T4D Race ${Date.now()}`;
    const courseID = await createMissingSubjectCourse(page, title, `RACE-${Date.now()}`);

    const institutions = await (await instructorAPI.get("/api/v1/authoring/academic/institutions")).json() as Array<{ id: string; name_en: string }>;
    const institutionID = institutions.find((item) => item.name_en === LAUNCH_UNIVERSITY)!.id;
    const subjects = await (await instructorAPI.get(`/api/v1/authoring/academic/institutions/${institutionID}/subjects?q=${SHARED_SUBJECT_CODE}`)).json() as Array<{ id: string }>;
    const selectedSubjectID = subjects[0].id;
    const selected = await instructorAPI.put(`/api/v1/courses/${courseID}/subject`, { data: { subject_id: selectedSubjectID } });
    expect(selected.status(), await selected.text()).toBe(200);

    const adminAPI = await apiFor(issueRotatingSession(ADMIN));
    const requests = await (await adminAPI.get("/api/v1/admin/academic/subject-requests?status=PENDING")).json() as Array<any>;
    const pending = requests.find((request) => request.course_title_en === title);
    const alternatives = await (await adminAPI.get(`/api/v1/admin/academic/institutions/${institutionID}/subjects?q=${ALT_SUBJECT_CODE}`)).json() as Array<{ id: string }>;
    const resolution = await adminAPI.post(`/api/v1/admin/academic/subject-requests/${pending.id}/link`, {
      data: { subject_id: alternatives[0].id },
    });
    expect(resolution.status()).toBe(409);
    expect(await resolution.text()).toContain("COURSE_SUBJECT_ALREADY_SELECTED");
    const course = await (await instructorAPI.get(`/api/v1/courses/${courseID}`)).json() as any;
    expect(course.subject_id).toBe(selectedSubjectID);

    await adminAPI.dispose();
    await instructorAPI.dispose();
    await instructorContext.close();
  });
});
