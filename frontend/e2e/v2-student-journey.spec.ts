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
import { installIssuedSession, issueRotatingSession } from "./rotating-students";
import { V2_STUDENT } from "./session/principals";

const COURSE_ID = "c7000000-0000-0000-0000-000000000001";
const LESSON_ONE_ID = "37000000-0000-0000-0000-000000000001";
const LESSON_TWO_ID = "37000000-0000-0000-0000-000000000002";
const ASSET_ONE_ID = "67000000-0000-0000-0000-000000000001";
const ASSET_TWO_ID = "67000000-0000-0000-0000-000000000002";
const COURSE_TITLE = "V2 Completion Course";

const viewports = [375, 768, 1280] as const;
const locales = ["en", "ar"] as const;
type Session = ReturnType<typeof issueRotatingSession>;

async function apiFor(session: Session): Promise<APIRequestContext> {
  return playwrightRequest.newContext({
    baseURL: frontendOrigin(),
    extraHTTPHeaders: {
      Accept: "application/json, application/problem+json",
      Cookie: `${session.cookie_name}=${session.cookie_value}; ${session.device_cookie_name}=${session.device_cookie_value}`,
      Origin: frontendOrigin(),
      "X-CSRF-Token": session.csrf_token,
    },
  });
}

async function studentPage(browser: Browser, locale: "ar" | "en" = "en", width = 1280): Promise<{
  context: BrowserContext;
  page: Page;
  session: Session;
}> {
  const context = await browser.newContext({
    locale: locale === "ar" ? "ar-KW" : "en-US",
    viewport: { width, height: 900 },
  });
  const session = issueRotatingSession(V2_STUDENT);
  await context.addInitScript((selectedLocale) => {
    window.localStorage.setItem("gradex.locale", selectedLocale);
  }, locale);
  await installIssuedSession(context, session);
  return { context, page: await context.newPage(), session };
}

async function saveProgress(
  api: APIRequestContext,
  lessonID: string,
  assetVersionID: string,
  positionSeconds: number,
): Promise<Record<string, unknown>> {
  const response = await api.put(`/api/v1/learn/lessons/${lessonID}/progress`, {
    data: { position_seconds: positionSeconds, asset_version_id: assetVersionID },
  });
  expect(response.status(), await response.text()).toBe(200);
  return (await response.json()) as Record<string, unknown>;
}

test.describe("T8 V2 Student journey", () => {
  test.describe.configure({ timeout: 120_000 });

  test("Student saves progress, completes lessons and verifies durable course completion", async ({ browser }) => {
    const learner = await studentPage(browser);
    const api = await apiFor(learner.session);
    let closed = false;
    try {
      await learner.page.goto("/en/learn/profile");
      await expect(learner.page.getByRole("heading", { name: "My profile" })).toBeVisible();
      await expect(learner.page.getByTestId("my-profile")).toBeVisible();
      await expect(learner.page.getByRole("link", { name: /academic profile/i })).toBeVisible();

      await learner.page.goto("/en/learn/academic-profile/edit");
      await expect(learner.page.getByTestId("academic-profile-form")).toBeVisible();
      await expect(learner.page.getByTestId("profile-save")).toBeVisible();

      await learner.page.goto("/en/learn/dashboard");
      await expect(learner.page.getByRole("heading", { name: "Your learning" })).toBeVisible();
      await expect(learner.page.getByRole("heading", { name: COURSE_TITLE })).toBeVisible();

      await learner.page.goto(`/en/learn/courses/${COURSE_ID}`);
      await expect(learner.page.getByRole("heading", { name: COURSE_TITLE })).toBeVisible();
      await expect(learner.page.getByRole("button", { name: /Completion Foundations 0\/2/ })).toBeVisible();

      await learner.page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ONE_ID}`);
      await expect(learner.page.getByRole("heading", { name: "V2 Progress Lesson" })).toBeVisible();
      await expect(learner.page.getByTestId("lesson-state")).toBeVisible();
      await expect(learner.page.getByRole("tab", { name: "Report content" })).toBeVisible();

      const saved = await saveProgress(api, LESSON_ONE_ID, ASSET_ONE_ID, 12);
      expect(saved.lesson_progress).toEqual({ position_seconds: 12, completed: false });
      await learner.page.reload();
      await expect(learner.page.getByTestId("lesson-state")).toContainText(/progress/i);

      const lessonOneCompleted = await saveProgress(api, LESSON_ONE_ID, ASSET_ONE_ID, 30);
      expect(lessonOneCompleted.lesson_progress).toEqual({ position_seconds: 30, completed: true });
      await learner.page.reload();
      await expect(learner.page.getByTestId("lesson-state")).toContainText("Completed");

      await learner.page.goto(`/en/learn/courses/${COURSE_ID}`);
      await expect(learner.page.getByRole("button", { name: /Completion Foundations 1\/2/ })).toBeVisible();

      await learner.page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_TWO_ID}`);
      await expect(learner.page.getByRole("heading", { name: "V2 Completion Lesson" })).toBeVisible();
      const lessonTwoCompleted = await saveProgress(api, LESSON_TWO_ID, ASSET_TWO_ID, 30);
      expect(lessonTwoCompleted.lesson_progress).toEqual({ position_seconds: 30, completed: true });

      await learner.page.goto(`/en/learn/courses/${COURSE_ID}`);
      await expect(learner.page.getByTestId("course-completion")).toBeVisible();
      await expect(learner.page.getByRole("button", { name: /Completion Foundations 2\/2/ })).toBeVisible();

      // A fresh browser/session proves the completion came from durable server state, not the
      // page's in-memory progress store.
      await learner.context.close();
      closed = true;
      await api.dispose();
      const fresh = await studentPage(browser);
      try {
        await fresh.page.goto(`/en/learn/courses/${COURSE_ID}`);
        await expect(fresh.page.getByTestId("course-completion")).toBeVisible();
        await fresh.page.goto("/en/learn/history");
        const completedHistory = fresh.page.locator("#completed-history-heading").locator("..");
        await expect(completedHistory.getByText(COURSE_TITLE)).toBeVisible();
        await expect(completedHistory.getByTestId("course-completion")).toBeVisible();
      } finally {
        await fresh.context.close();
      }
    } finally {
      if (!closed) await learner.context.close();
      await api.dispose().catch(() => undefined);
    }
  });

  for (const locale of locales) {
    for (const width of viewports) {
      test(`${locale} Student My Profile is RTL-safe at ${width}px`, async ({ browser }) => {
        const learner = await studentPage(browser, locale, width);
        try {
          await learner.page.goto(`/${locale}/learn/profile`);
          await expect(learner.page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
          await expect(
            learner.page.getByRole("heading", { name: locale === "ar" ? "ملفي الشخصي" : "My profile" }),
          ).toBeVisible();
          await expect(learner.page.getByTestId("my-profile")).toBeVisible();
          await expect(learner.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
        } finally {
          await learner.context.close();
        }
      });
    }
  }
});
