import { expect, test } from "@playwright/test";
import { studentFor, V2_ADMIN_TARGET_TEST_SLOT } from "./rotating-students";
import { ADMIN } from "./session/principals";
import { signInWithSeededSession } from "./session/sign-in";

const viewports = [375, 768, 1280] as const;
const locales = ["en", "ar"] as const;

test.describe("T8 V2 Admin operations", () => {
  test.describe.configure({ timeout: 90_000 });

  test("Admin completes the User 360 note and suspension flow with real controls", async ({ browser }, testInfo) => {
    const target = studentFor(testInfo, V2_ADMIN_TARGET_TEST_SLOT);
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, locale: "en-US" });
    const page = await signInWithSeededSession(context, ADMIN, "en");

    await page.goto("/en/admin");
    await expect(page.getByRole("heading", { name: "Operator home" })).toBeVisible();

    await page.goto("/en/admin/users");
    await expect(page.getByRole("heading", { name: "Users" })).toBeVisible();
    const search = page.getByPlaceholder("Name or email prefix");
    await search.fill(target.email);
    await page.getByRole("button", { name: "Apply filters" }).click();
    const accountRow = page.getByRole("link", { name: "Open account" }).first();
    await expect(accountRow).toBeVisible();
    await accountRow.click();
    await expect(page).toHaveURL(new RegExp(`/en/admin/users/${target.accountID}$`));

    await page.getByRole("tab", { name: "Notes" }).click();
    await page.getByRole("tabpanel", { name: "Notes" }).getByRole("textbox", { name: "Note" }).fill("T8 V2 operational note");
    await page.getByRole("button", { name: "Add note" }).click();
    await expect(page.getByText("T8 V2 operational note")).toBeVisible();

    await page.getByRole("button", { name: "Suspend" }).click();
    const suspensionDialog = page.getByTestId("admin-user-action-dialog");
    await expect(suspensionDialog).toBeVisible();
    await suspensionDialog.getByLabel("Reason").fill("T8 V2 suspension review");
    await suspensionDialog.getByTestId("confirm-accept").click();
    await expect(page.getByText("Account suspended.")).toBeVisible();

    await page.getByRole("button", { name: "Restore" }).click();
    const restoreDialog = page.getByTestId("admin-user-action-dialog");
    await restoreDialog.getByLabel("Reason").fill("T8 V2 restore review");
    await restoreDialog.getByTestId("confirm-accept").click();
    await expect(page.getByText("Account restored.")).toBeVisible();
    await context.close();
  });

  for (const locale of locales) {
    for (const width of viewports) {
      test(`${locale} Admin root is usable at ${width}px`, async ({ browser }) => {
        const context = await browser.newContext({
          viewport: { width, height: 900 },
          locale: locale === "ar" ? "ar-KW" : "en-US",
        });
        const page = await signInWithSeededSession(context, ADMIN, locale);
        await page.goto(`/${locale}/admin`);
        await expect(page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
        await expect(page.getByRole("heading", { name: locale === "ar" ? "الصفحة التشغيلية" : "Operator home" })).toBeVisible();
        await expect(page.locator("main")).toBeVisible();
        await expect(page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
        await context.close();
      });
    }
  }
});
