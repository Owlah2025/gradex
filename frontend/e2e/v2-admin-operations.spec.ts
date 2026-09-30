import { test, expect } from "@playwright/test";
import { ADMIN } from "./session/principals";
import { signInThroughLoginForm } from "./session/sign-in";

test.describe("V2 Admin Operations", () => {
  test("complete admin lifecycle flow", async ({ browser }) => {
    const context = await browser.newContext();
    const page = await signInThroughLoginForm(context, ADMIN);

    // operator dashboard
    await expect(page).toHaveURL(/\/en\/admin/);
    await expect(page.locator("h1").first()).toContainText("Admin");

    // search student
    await page.getByPlaceholder(/Search users/i).fill("Student");
    await page.getByPlaceholder(/Search users/i).press("Enter");

    // open User 360
    await page.getByRole("link", { name: /View user/i }).first().click();
    await expect(page).toHaveURL(/\/en\/admin\/users\//);

    // inspect access, progress, activity
    await page.getByRole("tab", { name: /Access/i }).click();
    await page.getByRole("tab", { name: /Progress/i }).click();
    await page.getByRole("tab", { name: /Activity/i }).click();

    // add internal note
    await page.getByRole("button", { name: /Add note/i }).click();
    await page.getByLabel(/Note/i).fill("E2E test note");
    await page.getByRole("button", { name: /Save note/i }).click();

    // suspend local test student
    await page.getByRole("button", { name: /Suspend user/i }).click();
    await page.getByLabel(/Reason/i).fill("E2E suspension");
    await page.getByRole("button", { name: /Confirm suspension/i }).click();

    // restore local test student
    await page.getByRole("button", { name: /Restore user/i }).click();
    await page.getByRole("button", { name: /Confirm restore/i }).click();

    // inspect audit evidence
    await page.goto("/en/admin/audit");
    await expect(page.getByText("E2E suspension").first()).toBeVisible();

    await context.close();
  });
});
