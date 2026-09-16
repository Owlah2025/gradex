import { expect, test, type Page } from "@playwright/test";

const isExternal = Boolean(process.env.GRADEX_E2E_EXTERNAL_ORIGIN);
const PRODUCTION_ORIGIN = "https://gradexcourses.com";

test.describe("production landing study-plan smoke", () => {
  test.skip(!isExternal, "This suite is read-only and runs only with GRADEX_E2E_EXTERNAL_ORIGIN.");

  test("English landing discovers Kuwait University Subjects and preserves filters", async ({ context, page }) => {
    await context.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Available now" })).toBeVisible();
    await expect(page.getByText("Programming in C & UNIX", { exact: true }).first()).toBeVisible();
    await expect(page.getByRole("heading", { name: "Courses for your study plan" })).toBeVisible();

    await page.getByTestId("study-plan-institution").selectOption("kuwait-university");
    await page.getByTestId("study-plan-program").selectOption("computer-science");
    await expect(page.locator('[data-subject-code="0418-220"]')).toHaveAttribute("data-served", "true");
    await expect(page.locator('[data-subject-code="0418-220"]').getByText("Programming in C & UNIX", { exact: true })).toBeVisible();
    await expect(page.locator('[data-subject-code="0418-220"]').getByText("View course", { exact: true })).toBeVisible();
    await expect(page.locator('[data-testid="study-plan-subject-card"][data-served="false"]').first()).toBeVisible();
    await expect(page.getByTestId("study-plan-view-all")).toHaveAttribute(
      "href",
      "/en/subjects?institution=kuwait-university&program=computer-science",
    );
  });

  test("Arabic landing stays RTL and the existing public catalogue counts remain unchanged", async ({ context, page }) => {
    await context.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
    await page.goto("/", { waitUntil: "networkidle" });
    await page.getByRole("button", { name: "Switch to Arabic" }).click();
    await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
    await expect(page.getByRole("heading", { name: "متاح الآن" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "مواد خطتك الدراسية" })).toBeVisible();
    await page.getByTestId("study-plan-institution").selectOption("kuwait-university");
    await page.getByTestId("study-plan-program").selectOption("computer-science");
    await expect(page.locator('[data-subject-code="0418-220"]')).toHaveAttribute("data-served", "true");
    await expect(page.getByTestId("study-plan-served-course").getByText("اعرض الكورس", { exact: true })).toBeVisible();
    await expect(page.locator('[data-testid="study-plan-subject-card"][data-served="false"]').first().getByText("اطلب هذا الكورس", { exact: true })).toBeVisible();

    const institutions = await page.request.get(`${PRODUCTION_ORIGIN}/api/v1/catalog/academic-options/institutions`);
    const subjects = await page.request.get(`${PRODUCTION_ORIGIN}/api/v1/catalog/subjects?page_size=1`);
    const kuSubjects = await page.request.get(`${PRODUCTION_ORIGIN}/api/v1/catalog/subjects?institution=kuwait-university&page_size=1`);
    const paaetSubjects = await page.request.get(`${PRODUCTION_ORIGIN}/api/v1/catalog/subjects?institution=paaet&page_size=1`);
    expect(institutions.ok()).toBe(true);
    expect(subjects.ok()).toBe(true);
    expect(kuSubjects.ok()).toBe(true);
    expect(paaetSubjects.ok()).toBe(true);
    expect((await institutions.json()).items.length).toBe(15);
    expect((await subjects.json()).total).toBe(329);
    expect((await kuSubjects.json()).total).toBe(84);
    expect((await paaetSubjects.json()).total).toBe(26);
    await expectNoOverflow(page);
  });
});

async function expectNoOverflow(page: Page): Promise<void> {
  const dimensions = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth + 1);
}
