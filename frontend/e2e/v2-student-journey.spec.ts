import { expect, test } from "@playwright/test";
import {
  studentFor,
  V2_STUDENT_MATRIX_TEST_SLOT,
  V2_STUDENT_TEST_SLOT,
} from "./rotating-students";
import { signInWithSeededSession } from "./session/sign-in";

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const LESSON_ID = "30000000-0000-0000-0000-000000000001";
const viewports = [375, 768, 1280] as const;
const locales = ["en", "ar"] as const;

test.describe("T8 V2 Student journey", () => {
  test.describe.configure({ timeout: 90_000 });

  test("Student reaches profile, learning, reporting, and history surfaces", async ({ browser }, testInfo) => {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, locale: "en-US" });
    const student = studentFor(testInfo, V2_STUDENT_TEST_SLOT);
    const page = await signInWithSeededSession(context, student, "en");

    await page.goto("/en/learn/dashboard");
    await expect(page.getByRole("heading", { name: "Your learning" })).toBeVisible();

    await page.goto("/en/learn/profile");
    await expect(page.getByRole("heading", { name: "My profile" })).toBeVisible();
    await expect(page.getByTestId("my-profile")).toBeVisible();
    await expect(page.getByRole("link", { name: /academic profile/i })).toBeVisible();

    await page.goto("/en/learn/academic-profile/edit");
    await expect(page.getByTestId("academic-profile-form")).toBeVisible();
    await expect(page.getByTestId("profile-save")).toBeVisible();

    await page.goto(`/en/learn/courses/${COURSE_ID}`);
    await expect(page.getByRole("heading", { name: /CS101/ })).toBeVisible();

    await page.goto(`/en/learn/courses/${COURSE_ID}/lessons/${LESSON_ID}`);
    await expect(page.getByRole("heading").first()).toBeVisible();
    await expect(page.getByTestId("lesson-state")).toBeVisible();
    await expect(page.getByRole("tab", { name: "Report content" })).toBeVisible();
    await page.getByRole("tab", { name: "Report content" }).click();
    await page.getByRole("button", { name: "Report this lesson" }).click();
    const reportDialog = page.getByRole("dialog");
    await expect(reportDialog.getByRole("heading", { name: "Report content" })).toBeVisible();
    await reportDialog.getByLabel("Reason").selectOption({ label: "Inaccurate" });
    await reportDialog.getByLabel("Details").fill("T8 V2 report flow");
    await reportDialog.getByRole("button", { name: "Send report" }).click();
    await expect(reportDialog.getByRole("status")).toContainText("Report received");

    await page.goto("/en/learn/history");
    await expect(page.getByRole("heading", { name: "Learning history" })).toBeVisible();
    await context.close();
  });

  for (const locale of locales) {
    for (const width of viewports) {
      test(`${locale} Student profile is RTL-safe at ${width}px`, async ({ browser }, testInfo) => {
        const context = await browser.newContext({
          viewport: { width, height: 900 },
          locale: locale === "ar" ? "ar-KW" : "en-US",
        });
        const student = studentFor(testInfo, V2_STUDENT_MATRIX_TEST_SLOT);
        const page = await signInWithSeededSession(context, student, locale);
        await page.goto(`/${locale}/learn/profile`);
        await expect(page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
        await expect(page.getByRole("heading", { name: locale === "ar" ? "ملفي الشخصي" : "My profile" })).toBeVisible();
        await expect(page.getByTestId("my-profile")).toBeVisible();
        await expect(page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
        await context.close();
      });
    }
  }
});
