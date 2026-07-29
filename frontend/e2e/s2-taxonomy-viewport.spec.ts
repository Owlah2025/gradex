import { expect, test, type Page } from "@playwright/test";

const viewports = [
  ["tablet", { width: 768, height: 1024 }],
  ["laptop", { width: 1280, height: 900 }],
  ["desktop", { width: 1440, height: 1000 }],
] as const;

const surfaces = [
  ["instructor", "/instructor/courses"],
  ["admin", "/en/admin/catalog"],
] as const;

async function mockCatalogAPI(page: Page) {
  await page.route("**/api/v1/session/bootstrap", (route) => route.fulfill({ json: { csrf_token: "csrf" } }));
  await page.route("**/api/v1/session", (route) => route.fulfill({ status: 401, json: { type: "https://api.gradex.com/problems/authentication-required" } }));
  await page.route("**/api/v1/courses", (route) => route.fulfill({ json: [] }));
  await page.route("**/api/v1/taxonomy/terms", (route) => route.fulfill({ json: [] }));
}

async function openAdminTaxonomyControls(page: Page, locale: "ar" | "en") {
  await page.getByPlaceholder(locale === "ar" ? "أدخل معرف الدورة (مثال: 00000000-0000-0000-0000-000000000000)" : "Enter Course UUID (e.g. 00000000-0000-0000-0000-000000000000)").fill("demo-course-1");
  await page.getByRole("button", { name: locale === "ar" ? "فتح إدارة التسعير" : "Manage Pricing" }).click();
}

function expectedScreenHeadings(surface: "instructor" | "admin", locale: "ar" | "en") {
  if (surface === "instructor") {
    return locale === "ar"
      ? ["منصة إعداد الدورات التعليمية", "أسعار الخادم الرسمية (قراءة فقط من وقائع الخادم)", "تصنيف المسودة المحددة"]
      : ["Course Authoring Studio", "Official Server Prices (Read-only Server State)", "Explicit Draft Taxonomy"];
  }
  return locale === "ar"
    ? ["قائمة مراجعة وتسعير الدورات", "إدارة التسعير وسجل التغييرات", "حالة الدورة وإجراءات الطوارئ", "إدارة تصنيف الدورة"]
    : ["Course Review & Pricing Admin", "Pricing Management & Audit Log", "Course Lifecycle & Emergency Controls", "Course Taxonomy Administration"];
}

for (const [locale, direction] of [["en", "ltr"], ["ar", "rtl"]] as const) {
  for (const [viewportName, viewport] of viewports) {
    for (const [surfaceName, path] of surfaces) {
      test(`${surfaceName} ${locale} ${viewportName} is directed and usable`, async ({ page }) => {
        await page.setViewportSize(viewport);
        await mockCatalogAPI(page);
        await page.addInitScript((selectedLocale) => localStorage.setItem("gradex.locale", selectedLocale), locale);
        await page.goto(path);

        await expect(page.locator("html")).toHaveAttribute("dir", direction);
        if (surfaceName === "admin") {
          await openAdminTaxonomyControls(page, locale);
        }
        for (const heading of expectedScreenHeadings(surfaceName, locale)) {
          await expect(page.getByText(heading, { exact: true })).toBeVisible();
        }
        await expect(page.locator("button, input, select").first()).toBeVisible();
        await expect(page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
      });
    }
  }
}
