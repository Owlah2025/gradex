import { test, expect } from "@playwright/test";
import { STUDENT } from "./session/principals";
import { signInThroughLoginForm } from "./session/sign-in";

test.describe("V2 Student Journey", () => {
  test("complete student learning flow in English", async ({ browser }) => {
    const context = await browser.newContext();
    const page = await signInThroughLoginForm(context, STUDENT);

    // My Learning
    await expect(page).toHaveURL(/\/en\/learn\/dashboard/);
    
    // My Profile
    await page.goto("/en/learn/academic-profile");
    await expect(page.locator("h1").first()).toContainText("Profile");

    // return to dashboard and continue course
    await page.goto("/en/learn/dashboard");
    const continueBtn = page.getByRole("link", { name: /Continue/i }).first();
    if (await continueBtn.isVisible()) {
      await continueBtn.click();

      // player & curriculum navigation
      await expect(page).toHaveURL(/\/en\/learn\/courses\//);
      
      // lesson completion
      const completeBtn = page.getByRole("button", { name: /Complete lesson/i });
      if (await completeBtn.isVisible()) {
        await completeBtn.click();
      }
    }

    // learning history
    await page.goto("/en/learn/history");
    await expect(page.locator("h1").first()).toContainText("History");

    await context.close();
  });

  test("student UI respects Arabic RTL", async ({ browser }) => {
    const context = await browser.newContext({ locale: "ar-EG" });
    const page = await signInThroughLoginForm(context, STUDENT);

    await expect(page).toHaveURL(/\/ar\/learn\/dashboard/);
    const dir = await page.evaluate(() => document.documentElement.dir);
    expect(dir).toBe("rtl");

    await context.close();
  });
});
