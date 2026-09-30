import { expect, test } from "@playwright/test";
import { INSTRUCTOR } from "./session/principals";
import { signInWithSeededSession } from "./session/sign-in";

const viewports = [375, 768, 1280] as const;
const locales = ["en", "ar"] as const;

test.describe("T8 V2 Instructor experience", () => {
  test.describe.configure({ timeout: 90_000 });

  for (const locale of locales) {
    for (const width of viewports) {
      test(`${locale} Instructor workspace is usable at ${width}px`, async ({ browser }) => {
        const context = await browser.newContext({
          viewport: { width, height: 900 },
          locale: locale === "ar" ? "ar-KW" : "en-US",
        });
        const page = await signInWithSeededSession(context, INSTRUCTOR, locale);

        await page.goto(`/${locale}/instructor`);
        await expect(page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
        await expect(page.getByRole("heading", { name: locale === "ar" ? "الصفحة الرئيسية للمدرّس" : "Instructor home" })).toBeVisible();

        await page.goto(`/${locale}/instructor/profile`);
        await expect(page.getByRole("heading", { name: locale === "ar" ? "ملف المدرّس" : "Instructor profile" })).toBeVisible();
        await expect(page.getByTestId("instructor-profile-editor")).toBeVisible();

        await page.goto(`/${locale}/instructor/courses`);
        await expect(page.getByRole("heading", { name: locale === "ar" ? "منصة إعداد المقررات التعليمية" : "Course Authoring Studio" })).toBeVisible();
        const courseList = page.getByTestId("owned-course-list");
        await expect(courseList).toBeVisible();
        const firstCourse = courseList.getByRole("button").first();
        await expect(firstCourse).toBeVisible();
        await firstCourse.click();
        await expect(page.getByTestId("selected-course-context")).toBeVisible();
        await expect(page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
        await context.close();
      });
    }
  }
});
