import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

const routes = [
  { path: "/ar/privacy", locale: "ar", title: "سياسة الخصوصية بالعربية", other: "/en/privacy" },
  { path: "/en/privacy", locale: "en", title: "English Privacy Policy", other: "/ar/privacy" },
  { path: "/ar/terms", locale: "ar", title: "شروط الاستخدام بالعربية", other: "/en/terms" },
  { path: "/en/terms", locale: "en", title: "English Terms of Use", other: "/ar/terms" },
] as const;

test.describe("LG-011 public legal policies", () => {
  for (const route of routes) {
    test(`${route.path} renders the approved public policy`, async ({ page, baseURL }) => {
      const response = await page.goto(route.path);
      expect(response?.status()).toBe(200);
      await expect(page.locator("main")).toHaveAttribute("lang", route.locale);
      await expect(page.getByRole("heading", { level: 1, name: route.title })).toBeVisible();
      await expect(page.locator("main")).toContainText("2026-08-09-v1");
      await expect(page.locator("main")).toContainText("Gradex Courses");
      await expect(page.locator("main")).toContainText("STAGING-NOT-REGISTERED");
      await expect(page.locator(`a[href="${route.other}"]`)).toBeVisible();

      const canonical = new URL(route.path, baseURL).toString();
      await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", canonical);
      await expect(page.locator('meta[name="robots"]')).not.toHaveAttribute("content", /noindex/i);

      const accessibility = await new AxeBuilder({ page }).analyze();
      expect(accessibility.violations).toEqual([]);
    });
  }
});
