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
  await page.route("**/api/v1/courses", (route) =>
    route.fulfill({
      json: [
        {
          id: "00000000-0000-0000-0000-0000000000c1",
          owner_account_id: "00000000-0000-0000-0000-0000000000a1",
          lifecycle: "DRAFT",
          classification_model: "LEGACY_TAXONOMY",
          editable_revision: {
            id: "00000000-0000-0000-0000-0000000000r1",
            state: "DRAFT",
            revision_number: 1,
            title_ar: "دورة قديمة",
            title_en: "Legacy Course",
            description_ar: "",
            description_en: "",
            sections: [],
          },
        },
      ],
    }),
  );
  await page.route("**/api/v1/taxonomy/terms", (route) => route.fulfill({ json: [] }));
  await page.route("**/api/v1/admin/review/queue", (route) => route.fulfill({ json: [] }));
}

function expectedScreenHeadings(surface: "instructor" | "admin", locale: "ar" | "en") {
  if (surface === "instructor") {
    return locale === "ar"
      ? ["منصة إعداد الدورات التعليمية", "أسعار الخادم الرسمية (قراءة فقط من وقائع الخادم)", "تصنيف المسودة المحددة"]
      : ["Course Authoring Studio", "Official Server Prices (Read-only Server State)", "Explicit Draft Taxonomy"];
  }
  return locale === "ar"
    ? ["مراجعة الدورات وإدارتها", "إدارة قاموس التصنيف", "قاموس التصنيف"]
    : ["Course review & administration", "Taxonomy Vocabulary Administration", "Taxonomy Vocabulary"];
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
        // Scoped to the page content rather than the whole document: the workspace navigation
        // entry that leads to a screen now carries the same words as that screen's own heading,
        // which is the intent, but leaves a bare document-wide text match ambiguous.
        const content = page.locator("#main");
        for (const heading of expectedScreenHeadings(surfaceName, locale)) {
          await expect(content.getByText(heading, { exact: true })).toBeVisible();
        }
        await expect(page.locator("button, input, select").first()).toBeVisible();
        await expect(page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).resolves.toBe(true);
      });
    }
  }
}
