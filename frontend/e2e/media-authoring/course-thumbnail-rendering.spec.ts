import { expect, test } from "@playwright/test";
import path from "node:path";

for (const locale of ["en", "ar"] as const) {
  test(`${locale} thumbnail cards preserve generated covers when absent or unavailable`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.addInitScript((language) => localStorage.setItem("gradex.locale", language), locale);
    const courses = ["custom", "empty", "broken"].map((kind, index) => ({
      id: `aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa${index}`,
      slug: `thumbnail-${kind}`,
      title: locale === "ar" ? `المنطق الرقمي ${index + 1}` : `Digital Logic ${index + 1}`,
      instructor_display_name: locale === "ar" ? "مدرس المقرر" : "Course Instructor",
      subject: { label: locale === "ar" ? "المنطق الرقمي" : "Digital Logic", code: "EE200" },
      price: { minor_units: 12500, currency: "KWD" }, has_preview: false,
      ...(kind === "empty" ? {} : { thumbnail: { asset_version_id: kind, card_url: `/api/v1/catalog/courses/fixture/thumbnails/${kind}/card`, large_url: `/api/v1/catalog/courses/fixture/thumbnails/${kind}/large` } }),
    }));
    await page.route("**/api/v1/session", (route) => route.fulfill({ status: 401, json: { code: "NOT_AUTHENTICATED" } }));
    await page.route("**/api/v1/catalog/academic-options/**", (route) => route.fulfill({ json: { items: [] } }));
    await page.route(/\/api\/v1\/catalog\/courses(?:\?.*)?$/, (route) => route.fulfill({ json: { items: courses, page: 1, page_size: 20, total: 3 } }));
    await page.route("**/api/v1/catalog/courses/fixture/thumbnails/custom/card", (route) => route.fulfill({ contentType: "image/webp", path: path.resolve(__dirname, "../../../backend/internal/media/testdata/thumbnail.webp") }));
    let failedRequests = 0;
    await page.route("**/api/v1/catalog/courses/fixture/thumbnails/broken/card", (route) => {
      failedRequests += 1;
      return route.fulfill({ status: 404, body: "" });
    });
    await page.goto("/");
    const cards = page.locator(".course-rail > li > a");
    await expect(cards).toHaveCount(3);
    await cards.first().scrollIntoViewIfNeeded();
    await expect.poll(() => cards.first().locator("img").evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1200);
    await expect(cards.nth(1).locator("img")).toHaveCount(0);
    await expect(cards.nth(2).locator("img")).toHaveCount(0);
    await expect(cards.nth(1).locator("[data-cover]")).toBeVisible();
    await expect(cards.nth(2).locator("[data-cover]")).toBeVisible();
    expect(failedRequests).toBe(1);
    await expect(page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
    await cards.first().screenshot({ path: testInfo.outputPath(`thumbnail-carousel-${locale}-desktop.png`), animations: "disabled" });
    await cards.nth(2).screenshot({ path: testInfo.outputPath(`thumbnail-fallback-${locale}.png`), animations: "disabled" });
    await page.setViewportSize({ width: 390, height: 844 });
    await cards.first().scrollIntoViewIfNeeded();
    await cards.first().screenshot({ path: testInfo.outputPath(`thumbnail-carousel-${locale}-mobile.png`), animations: "disabled" });
  });
}
