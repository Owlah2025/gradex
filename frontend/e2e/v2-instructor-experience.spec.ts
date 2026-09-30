import { test, expect } from "@playwright/test";
import { INSTRUCTOR } from "./session/principals";
import { signInThroughLoginForm } from "./session/sign-in";

test.describe("V2 Instructor Experience", () => {
  test("complete instructor authoring flow", async ({ browser }) => {
    const context = await browser.newContext();
    const page = await signInThroughLoginForm(context, INSTRUCTOR);

    // dashboard
    await expect(page).toHaveURL(/\/en\/instructor(\/.*)?$/);

    // profile and public profile
    await page.goto("/en/instructor/profile");
    await expect(page.locator("h1").first()).toContainText("Profile");

    // course builder
    await page.goto("/en/instructor/courses");
    await expect(page.locator("h1").first()).toContainText("Courses");

    // sections/lessons
    await page.getByRole("button", { name: /New course/i }).click();
    await page.getByLabel(/Course title/i).fill("E2E Test Course");
    await page.getByRole("button", { name: /Create course/i }).click();
    
    // navigate to curriculum
    await page.getByRole("tab", { name: /Curriculum/i }).click();
    await page.getByRole("button", { name: /Add section/i }).click();
    
    // revision submit
    await page.getByRole("tab", { name: /Review/i }).click();
    const submitBtn = page.getByRole("button", { name: /Submit for review/i });
    if (await submitBtn.isVisible()) {
      await submitBtn.click();
    }

    await context.close();
  });
});
