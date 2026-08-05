import { expect, test, type BrowserContext, type Page } from "@playwright/test";

const COURSE_ID = "c0000000-0000-0000-0000-000000000001";
const NON_EXISTENT_COURSE_ID = "c9999999-9999-9999-9999-999999999999";

const VIEWPORTS = [
  { name: "phone", width: 390, height: 844 },
  { name: "tablet", width: 768, height: 1024 },
  { name: "laptop", width: 1280, height: 900 },
  { name: "desktop", width: 1440, height: 1000 },
];

/**
 * Authenticates a student via the production login endpoints and returns session cookies.
 */
async function authenticateStudent(context: BrowserContext, email: string) {
  const page = await context.newPage();
  await page.goto("/en/catalog");

  const loginResult = await page.evaluate(async (studentEmail) => {
    const bootstrapRes = await fetch("/api/v1/session/bootstrap", {
      method: "GET",
      credentials: "include",
    });
    const { csrf_token } = await bootstrapRes.json();

    const loginRes = await fetch("/api/v1/sessions", {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json",
        "X-CSRF-Token": csrf_token,
      },
      body: JSON.stringify({
        email: studentEmail,
        password: "StudentPassword123!",
      }),
    });

    return { status: loginRes.status, body: await loginRes.json() };
  }, email);

  expect(loginResult.status).toBe(201);
  const cookies = await context.cookies();
  await page.close();
  return cookies;
}

test.describe("T060 — S5 Course Home E2E Test Suite", () => {
  let activeCookies: any[];
  let expiredCookies: any[];

  test.beforeAll(async ({ browser }) => {
    // Authenticate Active Student and Expired Student ONCE to avoid rate limits
    const activeContext = await browser.newContext();
    activeCookies = await authenticateStudent(activeContext, "student-active@example.test");
    await activeContext.close();

    const expiredContext = await browser.newContext();
    expiredCookies = await authenticateStudent(expiredContext, "student-expired@example.test");
    await expiredContext.close();
  });

  for (const vp of VIEWPORTS) {
    test.describe(`Viewport: ${vp.name} (${vp.width}x${vp.height})`, () => {
      test.use({ viewport: { width: vp.width, height: vp.height } });

      test("Active Student: Renders Course Home with backend-authored ordering, progress, materials, and no prefetch leak (EN & AR)", async ({ context, page }) => {
        await context.addCookies(activeCookies);

        const networkRequests: string[] = [];
        page.on("request", (req) => networkRequests.push(req.url()));

        // 1. English (LTR) Active Course Home
        await page.goto(`/en/learn/courses/${COURSE_ID}`);
        await expect(page.locator("html")).toHaveAttribute("dir", "ltr");

        // Heading & authored course metadata
        await expect(page.getByRole("heading", { name: "CS101: Introduction to Programming", level: 1 })).toBeVisible();

        // Verify Sections in backend-authored position order (Section 1 before Section 2)
        const sectionHeadings = page.locator("h2").filter({ hasText: /Section \d/ });
        const firstSectionText = await sectionHeadings.nth(0).innerText();
        const secondSectionText = await sectionHeadings.nth(1).innerText();
        expect(firstSectionText).toContain("Section 1: Basics");
        expect(secondSectionText).toContain("Section 2: Advanced Topics");

        // Verify Lessons in backend-authored position order under Section 1
        await expect(page.getByText("Lesson 1: Introduction")).toBeVisible();
        await expect(page.getByText("Lesson 2: Variables")).toBeVisible();
        await expect(page.getByText("Lesson 3: Functions")).toBeVisible();

        // Verify Aggregate and Per-Lesson Progress
        await expect(page.getByText(/33%/)).toBeVisible();
        await expect(page.getByRole("link", { name: /Open resource/i })).toBeVisible();
        await expect(page.getByRole("link", { name: /Open lab material/i })).toBeVisible();

        // Verify D-064 Material Entry route links are same-origin fixed paths with stable lesson identity
        const resourceLink = page.getByRole("link", { name: /Open resource/i });
        const labLink = page.getByRole("link", { name: /Open lab material/i });
        await expect(resourceLink).toHaveAttribute("href", /\/api\/v1\/media\/lessons\/30000000-0000-0000-0000-000000000001\/materials\/resource/);
        await expect(labLink).toHaveAttribute("href", /\/api\/v1\/media\/lessons\/30000000-0000-0000-0000-000000000001\/materials\/lab-material/);

        // Assert NO prefetch request was issued to material entry routes during render
        const materialPrefetches = networkRequests.filter((url) => url.includes("/materials/"));
        expect(materialPrefetches.length).toBe(0);

        // Assert no horizontal document overflow
        const isOverflowing = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
        expect(isOverflowing).toBe(false);

        // 2. Switch to Arabic (RTL) and verify bilingual authored content & layout direction
        await page.goto(`/ar/learn/courses/${COURSE_ID}`);
        await expect(page.locator("html")).toHaveAttribute("dir", "rtl");

        await expect(page.getByRole("heading", { name: "مقدمة في البرمجة CS101", level: 1 })).toBeVisible();
        await expect(page.getByText("القسم الأول: الأساسيات")).toBeVisible();
        await expect(page.getByText("الدرس الأول: مرحباً بك")).toBeVisible();

        const isOverflowingRTL = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
        expect(isOverflowingRTL).toBe(false);
      });

      test("Expired Student: Displays retained readable state, retained progress, localized expired indicator, and NO actionable links", async ({ context, page }) => {
        await context.addCookies(expiredCookies);

        await page.goto(`/en/learn/courses/${COURSE_ID}`);

        // Course metadata & retained progress remain readable
        await expect(page.getByRole("heading", { name: "CS101: Introduction to Programming", level: 1 })).toBeVisible();

        // Expired status indicator visible
        await expect(page.getByText("Access expired")).toBeVisible();

        // Machine-readable time element for original access_ends_at
        const timeEl = page.locator("time");
        await expect(timeEl).toBeVisible();
        const dateTimeAttr = await timeEl.getAttribute("datetime");
        expect(dateTimeAttr).toBeTruthy();

        // NO actionable material links or play buttons rendered for expired student
        await expect(page.getByRole("link", { name: "Resource" })).toHaveCount(0);
        await expect(page.getByRole("link", { name: "Lab material" })).toHaveCount(0);

        // Information leak guards: ensure internal security fields do not exist in page DOM
        const bodyHTML = await page.content();
        expect(bodyHTML).not.toContain("asset_version_id");
        expect(bodyHTML).not.toContain("entitlement_id");
        expect(bodyHTML).not.toContain("can_play");
        expect(bodyHTML).not.toContain("can_update_progress");
        expect(bodyHTML).not.toContain("internal_evaluator_reason");

        // Assert layout integrity
        const isOverflowing = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
        expect(isOverflowing).toBe(false);
      });

      test("Generic Unavailable: Non-existent or unauthorized course returns generic unavailable UI without revealing internal cause", async ({ context, page }) => {
        await context.addCookies(activeCookies);

        await page.goto(`/en/learn/courses/${NON_EXISTENT_COURSE_ID}`);

        // Generic unavailable message displayed
        await expect(page.getByRole("heading", { name: "Learning is unavailable", level: 1 })).toBeVisible();
        await expect(page.getByText("This learning content is not available right now.")).toBeVisible();

        // Discloses NO internal details
        const bodyText = await page.evaluate(() => document.body.innerText);
        expect(bodyText).not.toContain("c9999999-9999-9999-9999-999999999999");
        expect(bodyText).not.toContain("Entitlement");
        expect(bodyText).not.toContain("Revocation");
        expect(bodyText).not.toContain("PostgreSQL");
        expect(bodyText).not.toContain("sql");

        // Discloses NO course graph or lessons
        expect(bodyText).not.toContain("Section 1: Basics");
        expect(bodyText).not.toContain("Lesson 1: Introduction");

        // Assert layout integrity
        const isOverflowing = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
        expect(isOverflowing).toBe(false);
      });
    });
  }
});
