import {
  test,
  expect,
  request as playwrightRequest,
  type APIRequestContext,
  type BrowserContext,
  type Page,
} from "@playwright/test";
import { issueRotatingSession } from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * UX Tranche A — Admin Course discovery without a Course identifier.
 *
 * Founder acceptance found that reaching a newly created Course from the Admin portal meant reading
 * its UUID off another screen and carrying it across by hand. The cause was that Admin Course
 * discovery was keyed to `PENDING_REVIEW` and nothing else: `GET /admin/review/queue` returns only
 * submitted revisions, so a Course an Instructor had just created appeared in no Admin list at all.
 *
 * These cases prove the property the remediation claims: an Admin starting from the Admin UI, with
 * nothing in the clipboard, can find a Course by the words a human knows it by, open the right
 * administrative surface, and act — and that a DRAFT Course, which requires no Admin action, is
 * discoverable without being presented as work waiting on the Admin.
 *
 * Identifiers in the URL are fine and expected. What must never happen is a human handling one.
 */

const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };
const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const MANIFEST = "kuwait-university-launch-v1";
const ANY_INSTITUTION = "00000000-0000-0000-0000-000000000000";
const LAUNCH_UNIVERSITY = "Kuwait University";
const SHARED_SUBJECT_CODE = "0418-320";
const READY_ASSET_VERSION_ID = "60000000-0000-0000-0000-000000000001";
/** Distinctive so no other spec's title search can match these. */
const FILLER_TITLE_PREFIX = "UXA Directory Filler";
/** Mirrors catalog.LifecycleDirectoryLimit, the server's bound on one directory read. */
const DIRECTORY_PAGE_LIMIT = 50;

/** Matches a UUID anywhere in rendered text, which is what must not reach a reader. */
const UUID_ANYWHERE = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

type Session = ReturnType<typeof issueRotatingSession>;
type Account = typeof INSTRUCTOR | typeof ADMIN;

async function signIn(context: BrowserContext, account: Account, locale: "ar" | "en" = "en"): Promise<Session> {
  const session = issueRotatingSession(account);
  const origin = new URL(frontendOrigin());
  await context.addInitScript(
    (value) => window.localStorage.setItem("gradex.locale", value),
    locale,
  );
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

/**
 * Authors one Course through the Instructor's own API, up to the point where it is submittable.
 *
 * The Instructor owns academic identity and submission; an Admin is required to configure nothing
 * before this point, which is exactly why a DRAFT Course is not Admin work.
 */
async function authorCourse(
  instructor: APIRequestContext,
  titleEn: string,
): Promise<{ courseID: string; revisionID: string }> {
  // The Instructor's own authoring options, which is where the identifiers a Course is created
  // against actually come from. A Course must name its University (`ACADEMIC_INSTITUTION_REQUIRED`).
  const institutions = await instructor.get("/api/v1/authoring/academic/institutions");
  expect(institutions.ok(), await institutions.text()).toBeTruthy();
  const institutionID = (await institutions.json() as { id: string; name_en: string }[]).find(
    (entry) => entry.name_en === LAUNCH_UNIVERSITY,
  )!.id;

  const subjects = await instructor.get(
    `/api/v1/authoring/academic/institutions/${institutionID}/subjects?q=${SHARED_SUBJECT_CODE}`,
  );
  expect(subjects.ok(), await subjects.text()).toBeTruthy();
  const subjectID = (await subjects.json() as { id: string }[])[0]!.id;

  const created = await instructor.post("/api/v1/courses", {
    data: {
      title_ar: "دورة اكتشاف المشرف",
      title_en: titleEn,
      description_ar: "وصف",
      description_en: "Admin discovery journey",
      institution_id: institutionID,
      subject_id: subjectID,
    },
  });
  expect(created.status(), await created.text()).toBe(201);
  const course = await created.json() as { id: string; editable_revision: { id: string } };
  const courseID = course.id;
  const revisionID = course.editable_revision.id;

  const section = await instructor.post(`/api/v1/courses/${courseID}/revisions/${revisionID}/sections`, {
    data: { title_ar: "قسم", title_en: "Discovery Section" },
  });
  expect(section.status(), await section.text()).toBe(201);
  const sectionID = (await section.json() as { id: string }).id;

  const lesson = await instructor.post(
    `/api/v1/courses/${courseID}/revisions/${revisionID}/sections/${sectionID}/lessons`,
    { data: { title_ar: "درس", title_en: "Discovery Lesson" } },
  );
  expect(lesson.status(), await lesson.text()).toBe(201);
  const lessonID = (await lesson.json() as { id: string }).id;

  const video = await instructor.put(
    `/api/v1/courses/${courseID}/revisions/${revisionID}/lessons/${lessonID}/video`,
    { data: { video_asset_version_id: READY_ASSET_VERSION_ID } },
  );
  expect(video.status(), await video.text()).toBe(200);

  return { courseID, revisionID };
}

/**
 * Creates one bare draft Course. Fast on purpose: these exist only to push older Courses out of the
 * bounded lifecycle-directory page, so they need no curriculum and are never submitted.
 */
async function createFillerCourse(
  instructor: APIRequestContext,
  institutionID: string,
  subjectID: string,
  index: number,
): Promise<void> {
  const created = await instructor.post("/api/v1/courses", {
    data: {
      title_ar: `دورة حشو ${index}`,
      title_en: `${FILLER_TITLE_PREFIX} ${index}`,
      description_ar: "وصف",
      description_en: "Directory page filler",
      institution_id: institutionID,
      subject_id: subjectID,
    },
  });
  expect(created.status(), await created.text()).toBe(201);
}

/** The Instructor's own authoring identifiers, read once and reused by the filler loop. */
async function authoringIdentifiers(
  instructor: APIRequestContext,
): Promise<{ institutionID: string; subjectID: string }> {
  const institutions = await instructor.get("/api/v1/authoring/academic/institutions");
  expect(institutions.ok(), await institutions.text()).toBeTruthy();
  const institutionID = (await institutions.json() as { id: string; name_en: string }[]).find(
    (entry) => entry.name_en === LAUNCH_UNIVERSITY,
  )!.id;
  const subjects = await instructor.get(
    `/api/v1/authoring/academic/institutions/${institutionID}/subjects?q=${SHARED_SUBJECT_CODE}`,
  );
  expect(subjects.ok(), await subjects.text()).toBeTruthy();
  const subjectID = (await subjects.json() as { id: string }[])[0]!.id;
  return { institutionID, subjectID };
}

async function openDirectory(page: Page, filter: string): Promise<void> {
  await page.goto("/en/admin/courses");
  await expect(page.getByTestId("admin-course-loading")).toHaveCount(0, { timeout: 20_000 });
  await page.getByTestId(`admin-course-filter-${filter}`).click();
}

/** The row for one Course, found the way a human finds it: by its title. */
function rowByTitle(page: Page, title: string) {
  return page.getByTestId("admin-course-row").filter({ hasText: title });
}

test.describe("UX-A Admin Course discovery without an identifier", () => {
  test.beforeAll(async () => {
    await ensureLaunchCatalog();
  });

  test("A a draft course is discoverable by title and owner, and is not presented as review work", async ({
    browser,
  }) => {
    const instructorAPI = await apiFor(issueRotatingSession(INSTRUCTOR));
    const title = `Draft Discovery ${Date.now()}`;
    await authorCourse(instructorAPI, title);
    await instructorAPI.dispose();

    const context = await browser.newContext({ locale: "en-US" });
    await signIn(context, ADMIN);
    const page = await context.newPage();

    // The Admin finds it under Drafts, by the words a human knows it by.
    await openDirectory(page, "DRAFT");
    const row = rowByTitle(page, title);
    await expect(row).toBeVisible({ timeout: 20_000 });
    await expect(row).toContainText("instructor");
    await expect(row.getByTestId("admin-course-status")).toContainText("Draft");

    // The correction that matters: a draft requires no Admin action, so it must be discoverable
    // without being counted as work waiting on the Admin.
    await expect(row.getByTestId("admin-course-awaiting")).toContainText("Waiting on the instructor");
    await page.getByTestId("admin-course-filter-NEEDS_REVIEW").click();
    await expect(rowByTitle(page, title)).toHaveCount(0);

    await context.close();
  });

  test("B a submitted course becomes review work and opens directly from the directory", async ({ browser }) => {
    const instructorSession = issueRotatingSession(INSTRUCTOR);
    const instructorAPI = await apiFor(instructorSession);
    const title = `Submitted Discovery ${Date.now()}`;
    const { courseID, revisionID } = await authorCourse(instructorAPI, title);
    const submitted = await instructorAPI.post(
      `/api/v1/courses/${courseID}/revisions/${revisionID}/submit`,
    );
    expect(submitted.status(), await submitted.text()).toBe(200);
    await instructorAPI.dispose();

    const context = await browser.newContext({ locale: "en-US" });
    await signIn(context, ADMIN);
    const page = await context.newPage();

    await openDirectory(page, "NEEDS_REVIEW");
    const row = rowByTitle(page, title);
    await expect(row).toBeVisible({ timeout: 20_000 });
    await expect(row.getByTestId("admin-course-awaiting")).toContainText("Waiting on you");

    // Nothing on this screen shows an identifier to copy — this is the founder's original failure.
    const directoryText = await page.locator("main").innerText();
    expect(directoryText).not.toMatch(UUID_ANYWHERE);

    // The row's own action opens the review workspace for exactly this Course. The identifier
    // travels in the URL, which is where it belongs; no human typed or read it.
    await row.getByRole("link").click();
    await expect(page).toHaveURL(new RegExp(`/en/admin/courses/${courseID}/review$`));
    await expect(page.getByTestId("submitted-revision-inspector")).toBeVisible({ timeout: 20_000 });
    await expect(page.getByTestId("submitted-title-en")).toContainText(title);

    // The workspace is a real route: reloading it keeps the review rather than discarding it.
    await page.reload();
    await expect(page.getByTestId("submitted-revision-inspector")).toBeVisible({ timeout: 20_000 });

    // The approval blocker the Admin owns is named before Approve is pressed, not only as a
    // refusal afterwards. Publication requires an Admin-set launch price (BR-019).
    await expect(page.getByTestId("review-launch-price-required")).toBeVisible({ timeout: 20_000 });

    const inspector = page.getByTestId("submitted-revision-inspector");
    await inspector.getByTestId("pricing-amount").fill("25000");
    await inspector.getByTestId("pricing-reason").fill("UX-A launch price");
    await inspector.getByTestId("pricing-submit").click();
    await expect(inspector.getByTestId("pricing-success")).toBeVisible({ timeout: 20_000 });
    await expect(page.getByTestId("review-launch-price-required")).toHaveCount(0);

    await inspector.getByTestId("approve-inspected-revision").click();
    // Publishing puts a Course in front of students and closes the version to its author, so it is
    // confirmed — and the confirmation states that, rather than repeating the button.
    const publishDialog = page.getByTestId("review-decision-confirm");
    await expect(publishDialog).toBeVisible();
    await expect(publishDialog).toContainText("students can be granted access");
    await publishDialog.getByTestId("confirm-accept").click();
    // The decision is confirmed where it was taken. Redirecting on success would destroy the
    // message that tells the Admin it landed.
    await expect(page.getByTestId("review-action-success")).toBeVisible({ timeout: 20_000 });
    await expect(page.getByTestId("review-decision-recorded")).toBeVisible();

    // Returning re-reads the server, and the Course is no longer review work.
    await page.getByTestId("review-decision-recorded").getByRole("link").click();
    await expect(page).toHaveURL(/\/en\/admin\/courses$/);
    await expect(page.getByTestId("admin-course-loading")).toHaveCount(0, { timeout: 20_000 });
    await expect(rowByTitle(page, title)).toHaveCount(0);
    await page.getByTestId("admin-course-filter-PUBLISHED").click();
    await expect(rowByTitle(page, title)).toBeVisible({ timeout: 20_000 });

    await context.close();
  });

  test("C “Needs review” is the server's review queue, not a lifecycle guess", async ({ browser }) => {
    const context = await browser.newContext({ locale: "en-US" });
    const session = await signIn(context, ADMIN);
    const page = await context.newPage();
    await openDirectory(page, "NEEDS_REVIEW");

    const api = await apiFor(session);
    const queue = await api.get("/api/v1/admin/review/queue");
    expect(queue.ok()).toBeTruthy();
    const pendingCourseIDs = new Set(
      (await queue.json() as { course_id: string }[]).map((entry) => entry.course_id),
    );
    await api.dispose();

    // Every row under Needs review is a Course the server itself is holding a decision on. Deriving
    // this set from `lifecycle === "PENDING_REVIEW"` instead would both pull in drafts and hide a
    // published Course whose new revision is awaiting review.
    const rows = page.getByTestId("admin-course-row");
    const count = await rows.count();
    expect(count).toBe(pendingCourseIDs.size);
    for (let index = 0; index < count; index += 1) {
      const href = await rows.nth(index).getByRole("link").getAttribute("href");
      const id = href!.split("/courses/")[1].replace("/review", "");
      expect(pendingCourseIDs.has(id)).toBe(true);
    }

    await context.close();
  });

  test("D the directory renders in Arabic right-to-left and fits a 390px viewport", async ({ browser }) => {
    const context = await browser.newContext({ locale: "ar", viewport: { width: 390, height: 780 } });
    await signIn(context, ADMIN, "ar");
    const page = await context.newPage();
    await page.goto("/ar/admin/courses");
    await expect(page.getByTestId("admin-course-loading")).toHaveCount(0, { timeout: 20_000 });

    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    await expect(page.locator("h1")).toContainText("المقررات");
    // Arabic must be real copy, not an English screen flipped: the filters are translated too.
    await expect(page.getByTestId("admin-course-filter-NEEDS_REVIEW")).toContainText("بانتظار المراجعة");

    // No horizontal overflow at the narrowest supported width.
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(0);

    await context.close();
  });
  test("E a pending decision outside the bounded directory page is still review work", async ({
    browser,
  }) => {
    test.setTimeout(240_000);

    const instructorSession = issueRotatingSession(INSTRUCTOR);
    const instructorAPI = await apiFor(instructorSession);

    // 1. Author and submit the Course whose decision must never be lost.
    const title = `Stranded Review ${Date.now()}`;
    const { courseID, revisionID } = await authorCourse(instructorAPI, title);
    const submitted = await instructorAPI.post(
      `/api/v1/courses/${courseID}/revisions/${revisionID}/submit`,
    );
    expect(submitted.status(), await submitted.text()).toBe(200);

    // 2. Touch enough newer Courses to push it off the directory page. The directory is ordered by
    //    updated_at descending and bounded, so a full page of newer Courses is what strands it.
    const { institutionID, subjectID } = await authoringIdentifiers(instructorAPI);
    for (let index = 0; index < DIRECTORY_PAGE_LIMIT + 5; index += 1) {
      await createFillerCourse(instructorAPI, institutionID, subjectID, index);
    }
    await instructorAPI.dispose();

    const context = await browser.newContext({ locale: "en-US" });
    const adminSession = await signIn(context, ADMIN);
    const page = await context.newPage();

    // 3. Establish the precondition against the server rather than assuming it: the Course really
    //    is absent from the directory read the surface makes.
    const adminAPI = await apiFor(adminSession);
    const directory = await adminAPI.get("/api/v1/admin/courses");
    expect(directory.ok()).toBeTruthy();
    const listed = (await directory.json() as { items: { id: string }[] }).items;
    expect(listed.length).toBe(DIRECTORY_PAGE_LIMIT);
    expect(
      listed.some((entry) => entry.id === courseID),
      "precondition: the submitted Course must have fallen outside the directory page",
    ).toBe(false);

    // The review queue, by contrast, still holds it. That is what Needs review is built from.
    const queue = await adminAPI.get("/api/v1/admin/review/queue");
    expect(queue.ok()).toBeTruthy();
    expect(
      (await queue.json() as { course_id: string }[]).some((entry) => entry.course_id === courseID),
    ).toBe(true);
    await adminAPI.dispose();

    // 4. The Admin sees it in Needs review regardless.
    await openDirectory(page, "NEEDS_REVIEW");
    const row = page.locator(`[data-testid="admin-course-row"][data-course-id="${courseID}"]`);
    await expect(row).toBeVisible({ timeout: 20_000 });
    await expect(row).toContainText(title);
    await expect(row.getByTestId("admin-course-awaiting")).toContainText("Waiting on you");
    // It is shown from its queue entry, which is what makes it independent of the directory page.
    await expect(row).toHaveAttribute("data-from-queue-only", "true");

    // The bound is stated rather than presented as a complete catalogue.
    await expect(page.getByTestId("admin-course-capped")).toBeVisible();

    // 5. Still no identifier anywhere a human reads, and the row opens its own workspace.
    expect(await page.locator("main").innerText()).not.toMatch(UUID_ANYWHERE);
    await row.getByRole("link").click();
    await expect(page).toHaveURL(new RegExp(`/en/admin/courses/${courseID}/review$`));
    await expect(page.getByTestId("submitted-revision-inspector")).toBeVisible({ timeout: 20_000 });
    await expect(page.getByTestId("submitted-title-en")).toContainText(title);

    await context.close();
  });
});
