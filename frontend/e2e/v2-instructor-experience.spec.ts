import {
  expect,
  request as playwrightRequest,
  test,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type Page,
} from "@playwright/test";
import { frontendOrigin } from "../src/lib/api/e2e-ports";
import { openAuthoringSections } from "./authoring-sections";
import { installIssuedSession, issueRotatingSession } from "./rotating-students";
import { ADMIN, V2_INSTRUCTOR } from "./session/principals";
import { seedReadyVideoForCourse } from "./ready-media";

const DRAFT_COURSE_ID = "c7000000-0000-0000-0000-000000000002";
const DRAFT_REVISION_ID = "f7000000-0000-0000-0000-000000000002";

const viewports = [375, 768, 1280] as const;
const locales = ["en", "ar"] as const;
type Session = ReturnType<typeof issueRotatingSession>;

async function apiFor(session: Session): Promise<APIRequestContext> {
  return playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Cookie: `${session.cookie_name}=${session.cookie_value}`,
      Origin: frontendOrigin(),
      "X-CSRF-Token": session.csrf_token,
    },
  });
}

async function signedContext(browser: Browser, principal: typeof V2_INSTRUCTOR | typeof ADMIN, width = 1280): Promise<{
  context: BrowserContext;
  page: Page;
  session: Session;
}> {
  const context = await browser.newContext({ locale: "en-US", viewport: { width, height: 900 } });
  const session = issueRotatingSession(principal);
  await context.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
  await installIssuedSession(context, session);
  return { context, page: await context.newPage(), session };
}

async function openStudio(page: Page): Promise<void> {
  await page.goto("/en/instructor/courses");
  await expect(page.getByRole("heading", { name: "Course Authoring Studio" })).toBeVisible();
  await expect(page.getByTestId("owned-course-list")).toBeVisible();
}

async function readDraft(api: APIRequestContext): Promise<Record<string, any>> {
  const response = await api.get(`/api/v1/courses/${DRAFT_COURSE_ID}`);
  expect(response.status(), await response.text()).toBe(200);
  return (await response.json()) as Record<string, any>;
}

test.describe("T8 V2 Instructor experience", () => {
  test.describe.configure({ timeout: 120_000 });

  test("Instructor creates a section and lesson, submits the revision, and sees persisted review state", async ({ browser }) => {
    const instructor = await signedContext(browser, V2_INSTRUCTOR);
    const instructorAPI = await apiFor(instructor.session);
    try {
      await openStudio(instructor.page);
      await instructor.page.getByTestId(`owned-course-${DRAFT_COURSE_ID}`).click();
      await expect(instructor.page.getByTestId("selected-course-context")).toHaveAttribute(
        "data-revision-id",
        DRAFT_REVISION_ID,
      );
      await openAuthoringSections(instructor.page);

      await expect(instructor.page.getByTestId("curriculum")).toBeVisible();
      await instructor.page.getByTestId("section-title-ar").fill("قسم V2");
      await instructor.page.getByTestId("section-title-en").fill("V2 Authoring Section");
      await instructor.page.getByTestId("add-section").click();
      await expect(instructor.page.getByTestId("curriculum").getByText("V2 Authoring Section")).toBeVisible();

      const sectionBlock = instructor.page.locator('[data-testid^="section-"]').filter({ hasText: "V2 Authoring Section" }).first();
      const sectionTestID = await sectionBlock.getAttribute("data-testid");
      expect(sectionTestID).toMatch(/^section-[0-9a-f-]+$/i);
      const sectionID = sectionTestID!.replace("section-", "");
      await instructor.page.getByTestId(`lesson-title-ar-${sectionID}`).fill("درس V2");
      await instructor.page.getByTestId(`lesson-title-en-${sectionID}`).fill("V2 Authoring Lesson");
      await instructor.page.getByTestId(`add-lesson-${sectionID}`).click();
      await expect(instructor.page.getByTestId("curriculum").getByText("V2 Authoring Lesson")).toBeVisible();

      const persistedAfterLesson = await readDraft(instructorAPI);
      const persistedSections = persistedAfterLesson.editable_revision.sections as Array<{ id: string; lessons: Array<{ id: string; title_en: string }> }>;
      expect(persistedSections).toHaveLength(1);
      expect(persistedSections[0].lessons).toHaveLength(1);
      expect(persistedSections[0].lessons[0].title_en).toBe("V2 Authoring Lesson");

      const lessonID = persistedSections[0].lessons[0].id;
      const readyAssetVersionID = seedReadyVideoForCourse(DRAFT_COURSE_ID, V2_INSTRUCTOR.accountID);
      const attachVideo = await instructorAPI.put(
        `/api/v1/courses/${DRAFT_COURSE_ID}/revisions/${DRAFT_REVISION_ID}/lessons/${lessonID}/video`,
        { data: { video_asset_version_id: readyAssetVersionID } },
      );
      expect(attachVideo.status(), await attachVideo.text()).toBe(200);

      await instructor.page.reload();
      await instructor.page.getByTestId(`owned-course-${DRAFT_COURSE_ID}`).click();
      await openAuthoringSections(instructor.page);
      await expect(instructor.page.getByTestId("curriculum").getByText("V2 Authoring Section")).toBeVisible();
      await expect(instructor.page.getByTestId("curriculum").getByText("V2 Authoring Lesson")).toBeVisible();

      await instructor.page.getByTestId("submit-for-review").click();
      await instructor.page.getByTestId("submit-confirm").getByTestId("confirm-accept").click();
      await expect(instructor.page.getByTestId("authoring-notice")).toContainText("administrator will review");

      const submitted = await readDraft(instructorAPI);
      expect(submitted.editable_revision.state).toBe("PENDING_REVIEW");
      expect(submitted.editable_revision.sections[0].lessons[0].title_en).toBe("V2 Authoring Lesson");

      const admin = await signedContext(browser, ADMIN);
      try {
        await admin.page.goto("/en/admin/catalog");
        await expect(admin.page.getByTestId(`review-item-${DRAFT_COURSE_ID}`)).toBeVisible({ timeout: 15_000 });
        const reviewLink = admin.page.getByTestId(`inspect-review-item-${DRAFT_COURSE_ID}`);
        await Promise.all([
          admin.page.waitForURL(new RegExp(`/en/admin/courses/${DRAFT_COURSE_ID}/review$`)),
          reviewLink.click(),
        ]);
        await expect(admin.page.getByTestId("submitted-revision-inspector")).toBeVisible({ timeout: 20_000 });
        await expect(admin.page.getByTestId("submitted-revision-inspector")).toContainText("V2 Authoring Lesson");
      } finally {
        await admin.context.close();
      }
    } finally {
      await instructorAPI.dispose();
      await instructor.context.close();
    }
  });

  for (const locale of locales) {
    for (const width of viewports) {
      test(`${locale} Instructor workspace is usable at ${width}px`, async ({ browser }) => {
        const instructor = await signedContext(browser, V2_INSTRUCTOR, width);
        try {
          await instructor.page.goto(`/${locale}/instructor`);
          await expect(instructor.page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
          await expect(instructor.page.locator("main")).toBeVisible();
          await instructor.page.goto(`/${locale}/instructor/profile`);
          await expect(instructor.page.getByTestId("instructor-profile-editor")).toBeVisible();
          await instructor.page.goto(`/${locale}/instructor/courses`);
          await expect(instructor.page.getByTestId("owned-course-list")).toBeVisible();
          await expect(instructor.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
        } finally {
          await instructor.context.close();
        }
      });
    }
  }
});
