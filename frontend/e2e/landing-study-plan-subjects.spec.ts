import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import {
  authenticateRotatingStudent,
  installIssuedSession,
  issueRotatingSession,
  studentFor,
  LANDING_STUDY_PLAN_FILTER_TEST_SLOT,
  LANDING_STUDY_PLAN_PROFILE_TEST_SLOT,
  LANDING_STUDY_PLAN_REQUEST_TEST_SLOT,
} from "./rotating-students";

const KUWAIT_UNIVERSITY = "kuwait-university";
const COMPUTER_SCIENCE = "computer-science";
const C_UNIX_CODE = "0418-220";
const C_UNIX_TITLE = "Programming in C & UNIX";

const ADMIN = {
  email: "admin@example.test",
  accountID: "a0000000-0000-0000-0000-000000000000",
};
const INSTRUCTOR = {
  email: "instructor@example.test",
  accountID: "a0000000-0000-0000-0000-000000000003",
};

test.describe.configure({ timeout: 240_000 });

async function chooseKuwaitComputerScience(page: Page): Promise<void> {
  await page.getByTestId("study-plan-institution").selectOption(KUWAIT_UNIVERSITY);
  await expect(page.getByTestId("study-plan-program")).toBeEnabled();
  await page.getByTestId("study-plan-program").selectOption(COMPUTER_SCIENCE);
  await expect(page.getByTestId("study-plan-subject-card").first()).toBeVisible();
}

async function useLocale(context: BrowserContext, locale: "ar" | "en"): Promise<void> {
  await context.addInitScript((value) => {
    window.localStorage.setItem("gradex.locale", value);
  }, locale);
}

async function expectNoPageOverflow(page: Page): Promise<void> {
  const widths = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    client: document.documentElement.clientWidth,
  }));
  expect(widths.scroll).toBeLessThanOrEqual(widths.client + 1);
}

test("anonymous English landing separates available Courses from study-plan Subjects", async ({
  context,
  page,
}, testInfo) => {
  await useLocale(context, "en");
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("/");

  await expect(page.getByRole("heading", { name: "Available now" })).toBeVisible();
  await expect(page.getByText(C_UNIX_TITLE, { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Courses for your study plan" })).toBeVisible();
  await expect(page.getByText("Choose your university", { exact: true }).last()).toBeVisible();

  await chooseKuwaitComputerScience(page);
  const served = page.locator(`[data-testid="study-plan-subject-card"][data-subject-code="${C_UNIX_CODE}"]`);
  await expect(served).toHaveAttribute("data-served", "true");
  await expect(served.getByText(C_UNIX_TITLE, { exact: true })).toBeVisible();
  await expect(served.getByText(/Dr\. Instructor/)).toBeVisible();
  await expect(served.getByText(/32\.000/)).toBeVisible();
  await expect(served.getByText("View course", { exact: true })).toBeVisible();
  await expect(served.getByTestId("subject-demand-request")).toHaveCount(0);

  const unserved = page.locator('[data-testid="study-plan-subject-card"][data-served="false"]').first();
  await expect(unserved.getByText("Not available yet", { exact: true })).toBeVisible();
  await expect(unserved.getByText(/KWD|Instructor:/)).toHaveCount(0);
  const requestLink = unserved.getByTestId("subject-demand-sign-in");
  await expect(requestLink).toHaveText("Request this course");
  const href = await requestLink.getAttribute("href");
  expect(new URL(href!, "http://gradex.test").searchParams.get("returnTo")).toBe("/?request=1");
  const storedContext = await page.evaluate(() =>
    window.localStorage.getItem("gradex.academic-context"),
  );
  expect(storedContext).toContain(KUWAIT_UNIVERSITY);
  expect(storedContext).toContain(COMPUTER_SCIENCE);

  await expect(page.getByTestId("study-plan-view-all")).toHaveAttribute(
    "href",
    `/en/subjects?institution=${KUWAIT_UNIVERSITY}&program=${COMPUTER_SCIENCE}`,
  );
  await expect(page.getByText("Course Bundles", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Available now", exact: true }).click();
  await expect(page.getByTestId("study-plan-subject-card")).toHaveCount(1);
  await expect(page.getByTestId("study-plan-subject-card")).toHaveAttribute("data-served", "true");
  await page.getByRole("button", { name: "All", exact: true }).click();
  await expect(page.getByTestId("study-plan-subject-card")).toHaveCount(12);
  await testInfo.attach("desktop-en-study-plan.png", {
    body: await page.getByTestId("study-plan-subjects").screenshot(),
    contentType: "image/png",
  });
  await expectNoPageOverflow(page);

  await page.getByTestId("study-plan-view-all").click();
  await expect(page).toHaveURL(
    new RegExp(`/en/subjects\\?institution=${KUWAIT_UNIVERSITY}&program=${COMPUTER_SCIENCE}`),
  );
  await expect(page.getByTestId("subject-institution-filter")).toHaveValue(KUWAIT_UNIVERSITY);
  await expect(page.getByTestId("subject-program-filter")).toHaveValue(COMPUTER_SCIENCE);
});

test("anonymous Arabic landing keeps RTL selectors, rail, and actions functional", async ({
  context,
  page,
}, testInfo) => {
  await useLocale(context, "ar");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");

  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(page.getByRole("heading", { name: "متاح الآن" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "مواد خطتك الدراسية" })).toBeVisible();
  await chooseKuwaitComputerScience(page);

  const served = page.locator(`[data-testid="study-plan-subject-card"][data-subject-code="${C_UNIX_CODE}"]`);
  await expect(served.getByText("البرمجة بلغة C ونظام يونكس", { exact: true })).toBeVisible();
  await expect(served.getByText("اعرض الكورس", { exact: true })).toBeVisible();
  const unserved = page.locator('[data-testid="study-plan-subject-card"][data-served="false"]').first();
  await expect(unserved.getByText("غير متاح بعد", { exact: true })).toBeVisible();
  await expect(unserved.getByTestId("subject-demand-sign-in")).toHaveText("اطلب هذا الكورس");
  await testInfo.attach("mobile-ar-study-plan.png", {
    body: await page.getByTestId("study-plan-subjects").screenshot(),
    contentType: "image/png",
  });
  await expectNoPageOverflow(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await testInfo.attach("desktop-ar-study-plan.png", {
    body: await page.getByTestId("study-plan-subjects").screenshot(),
    contentType: "image/png",
  });
  await expectNoPageOverflow(page);
});

test("a Student academic profile initializes the landing filters without onboarding", async ({
  browser,
}, testInfo) => {
  const student = studentFor(testInfo, LANDING_STUDY_PLAN_PROFILE_TEST_SLOT);
  const context = await browser.newContext({ locale: "en-US" });
  await useLocale(context, "en");
  await authenticateRotatingStudent(context, student);
  const page = await context.newPage();
  await page.goto("/");

  await expect(page.getByTestId("study-plan-institution")).toHaveValue(KUWAIT_UNIVERSITY);
  await expect(page.getByTestId("study-plan-program")).toHaveValue(COMPUTER_SCIENCE);
  await expect(page.locator(`[data-subject-code="${C_UNIX_CODE}"]`)).toBeVisible();
  await expect(page.getByText(/complete your academic profile/i)).toHaveCount(0);
  await context.close();
});

test("a Student request persists after reload and can be withdrawn", async ({ browser }, testInfo) => {
  const student = studentFor(testInfo, LANDING_STUDY_PLAN_REQUEST_TEST_SLOT);
  const context = await browser.newContext({ locale: "en-US" });
  await useLocale(context, "en");
  await authenticateRotatingStudent(context, student);
  const page = await context.newPage();
  await page.goto("/");
  await chooseKuwaitComputerScience(page);

  const card = page.locator('[data-testid="study-plan-subject-card"][data-served="false"]').first();
  const subjectCode = await card.getAttribute("data-subject-code");
  const created = page.waitForResponse(
    (response) =>
      response.url().includes("/api/v1/me/subject-demand") &&
      response.request().method() === "POST",
  );
  await card.getByTestId("subject-demand-request").click();
  expect((await created).status()).toBe(201);
  await expect(card.getByTestId("subject-demand-state")).toHaveText("Requested");

  await page.reload();
  const reloaded = page.locator(
    `[data-testid="study-plan-subject-card"][data-subject-code="${subjectCode}"]`,
  );
  await expect(reloaded.getByTestId("subject-demand-state")).toHaveText("Requested");
  const withdrawn = page.waitForResponse(
    (response) =>
      response.url().includes("/api/v1/me/subject-demand/") &&
      response.request().method() === "DELETE",
  );
  await reloaded.getByTestId("subject-demand-withdraw").click();
  expect((await withdrawn).status()).toBe(204);
  await expect(reloaded.getByTestId("subject-demand-request")).toBeVisible();
  await context.close();
});

test("Institution switching resets Programs and rejects stale Subject responses", async ({
  browser,
}, testInfo) => {
  const student = studentFor(testInfo, LANDING_STUDY_PLAN_FILTER_TEST_SLOT);
  const context = await browser.newContext({ locale: "en-US" });
  await useLocale(context, "en");
  await authenticateRotatingStudent(context, student);
  const page = await context.newPage();
  let delayedKuwaitRequest = false;
  await page.route("**/api/v1/catalog/subjects?**", async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get("program") === COMPUTER_SCIENCE && !delayedKuwaitRequest) {
      delayedKuwaitRequest = true;
      await new Promise((resolve) => setTimeout(resolve, 700));
    }
    await route.continue();
  });
  await page.goto("/");
  await page.getByTestId("study-plan-institution").selectOption(KUWAIT_UNIVERSITY);
  await page.getByTestId("study-plan-program").selectOption(COMPUTER_SCIENCE);
  await expect.poll(() => delayedKuwaitRequest).toBe(true);

  await page.getByTestId("study-plan-institution").selectOption("e2e-subject-university");
  await expect(page.getByTestId("study-plan-program")).toHaveValue("");
  await page.getByTestId("study-plan-program").selectOption("e2e-computer-science");
  await expect(page.locator('[data-subject-code="ZZZ 900"]')).toBeVisible();
  await page.waitForTimeout(900);
  await expect(page.locator(`[data-subject-code="${C_UNIX_CODE}"]`)).toHaveCount(0);
  await expect(page.getByTestId("study-plan-program")).toHaveValue("e2e-computer-science");
  await context.close();
});

test("Admin and Instructor can browse but receive no Student demand action", async ({ browser }) => {
  for (const account of [ADMIN, INSTRUCTOR]) {
    const context = await browser.newContext({ locale: "en-US" });
    await installIssuedSession(context, issueRotatingSession(account));
    const page = await context.newPage();
    await page.goto("/");
    await chooseKuwaitComputerScience(page);
    const unserved = page.locator('[data-testid="study-plan-subject-card"][data-served="false"]').first();
    await expect(unserved.getByTestId("subject-demand-request")).toHaveCount(0);
    await expect(unserved.getByTestId("subject-demand-ineligible")).toBeVisible();
    await context.close();
  }
});

test("a failed Subject request exposes retry and recovers without collapsing the section", async ({
  context,
  page,
}) => {
  await useLocale(context, "en");
  let failOnce = true;
  await page.route("**/api/v1/catalog/subjects?**", async (route) => {
    if (failOnce) {
      failOnce = false;
      await route.abort("failed");
      return;
    }
    await route.continue();
  });
  await page.goto("/");
  await page.getByTestId("study-plan-institution").selectOption(KUWAIT_UNIVERSITY);
  await page.getByTestId("study-plan-program").selectOption(COMPUTER_SCIENCE);
  await expect(page.getByTestId("study-plan-error")).toBeVisible();
  await page.getByTestId("study-plan-error").getByRole("button", { name: "Try again" }).click();
  await expect(page.getByTestId("study-plan-subject-card").first()).toBeVisible();
  await expect(page.getByTestId("study-plan-subjects")).toBeVisible();
});

test("study-plan rail exposes desktop four-plus-peek, tablet three, and mobile one-plus-peek", async ({
  context,
  page,
}, testInfo) => {
  await useLocale(context, "en");
  await page.goto("/");
  await chooseKuwaitComputerScience(page);

  for (const viewport of [
    { width: 1440, height: 900, minimumVisible: 5 },
    { width: 768, height: 900, minimumVisible: 3 },
    { width: 390, height: 844, minimumVisible: 2 },
  ]) {
    await page.setViewportSize(viewport);
    const visible = await page.getByTestId("study-plan-subject-card").evaluateAll((cards) =>
      cards.filter((card) => {
        const rect = card.getBoundingClientRect();
        return rect.right > 0 && rect.left < window.innerWidth;
      }).length,
    );
    expect(visible).toBeGreaterThanOrEqual(viewport.minimumVisible);
    await expectNoPageOverflow(page);
    await testInfo.attach(`study-plan-${viewport.width}.png`, {
      body: await page.getByTestId("study-plan-subjects").screenshot(),
      contentType: "image/png",
    });
  }
});

test("a single published Course keeps a bounded card and its catalogue link", async ({
  context,
  page,
}, testInfo) => {
  await useLocale(context, "en");
  await page.route("**/api/v1/catalog/courses", async (route) => {
    const response = await route.fetch();
    const catalogue = await response.json();
    const course = catalogue.items[0];
    expect(course).toBeTruthy();
    await route.fulfill({ response, json: { ...catalogue, items: [course], total: 1 } });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/");

  const rail = page.getByTestId("featured-courses-list");
  await expect(rail.locator("li")).toHaveCount(1);
  await expect(rail.locator("h3")).toBeVisible();
  await expect(page.getByTestId("single-course-companion")).toBeVisible();
  await expect(page.getByTestId("single-course-companion").getByRole("link")).toHaveAttribute(
    "href",
    "#study-plan",
  );
  await expect(page.getByTestId("featured-courses-view-all")).toHaveAttribute("href", "/en/catalog");
  const cardWidth = await rail.locator("li").first().evaluate((item) => item.getBoundingClientRect().width);
  expect(cardWidth).toBeLessThanOrEqual(440);
  await testInfo.attach("single-available-course.png", {
    body: await page.getByRole("region", { name: "Available now" }).screenshot(),
    contentType: "image/png",
  });
  await expectNoPageOverflow(page);
});
