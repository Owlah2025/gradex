import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import {
  authenticateRotatingStudent,
  issueRotatingSession,
  studentFor,
  SUBJECT_DEMAND_AR_TEST_SLOT,
  SUBJECT_DEMAND_EN_TEST_SLOT,
} from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * D-106 Subject catalogue and Student demand, against the real API and a real
 * database.
 *
 * No request is mocked. Every assertion below is about what the deployed stack
 * actually does: the seeded Subjects are real rows, the served Course is really
 * published, the 409 comes from the live-unique index, and the 404 comes from
 * the route. A mocked version of this suite would have passed against the
 * pagination defect it exists to catch.
 *
 * Fixtures come from cmd/e2e-seed: one Institution holding
 * SEEDED_SUBJECTS Subjects, of which exactly one is served by a published
 * Course. That count is deliberately larger than one catalogue page.
 */

// Mirrors cmd/e2e-seed seedSubjectCatalogueFixtures.
const INSTITUTION_SLUG = "e2e-subject-university";
const INSTITUTION_EN = "E2E Subject University";
const SEEDED_SUBJECTS = 30;
const SERVED_CODE = "ZZZ 900";
const SERVED_COURSE_SLUG = "course-c00000000000000000000000000d1060";
// Sorts last among the unserved codes, so reaching it proves page two loaded.
const LAST_UNSERVED_CODE = "SUB 128";
// Mirrors SUBJECTS_PER_PAGE in subject-catalogue.tsx.
const PAGE_SIZE = 24;

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const INSTRUCTOR = { email: "instructor@example.test", accountID: "a0000000-0000-0000-0000-000000000003" };

test.describe.configure({ timeout: 180_000 });

async function installStaffSession(
  context: BrowserContext,
  account: { email: string; accountID: string },
  locale: "ar" | "en" = "en",
): Promise<void> {
  const session = issueRotatingSession(account);
  const origin = new URL(frontendOrigin());
  await context.addInitScript((value) => {
    window.localStorage.setItem("gradex.locale", value);
  }, locale);
  await context.addCookies([
    {
      name: session.cookie_name,
      value: session.cookie_value,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
    },
  ]);
}

/** Every Subject code currently rendered, in DOM order. */
async function renderedCodes(page: Page): Promise<string[]> {
  return page.locator('[data-testid="subject-card"] .font-mono').allInnerTexts();
}

async function expectNoHorizontalOverflow(page: Page, label: string): Promise<void> {
  const overflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(
    overflow.scrollWidth,
    `${label}: document scrolls horizontally (${overflow.scrollWidth} > ${overflow.clientWidth})`,
  ).toBeLessThanOrEqual(overflow.clientWidth + 1);
}

test("Every Subject is reachable through pagination, and filters reset it", async ({ page }) => {
  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}`);

  // Page one is a page, not the whole corpus.
  await expect(page.getByTestId("subject-card")).toHaveCount(PAGE_SIZE);
  await expect(page.getByTestId("subject-shown-count")).toContainText(
    `Showing ${PAGE_SIZE} of ${SEEDED_SUBJECTS}`,
  );

  // The Subject that sorts last is not on page one -- otherwise reaching it
  // later would prove nothing about pagination.
  const firstPage = await renderedCodes(page);
  expect(firstPage).not.toContain(LAST_UNSERVED_CODE);

  await page.getByTestId("subject-load-more").click();
  await expect(page.getByTestId("subject-card")).toHaveCount(SEEDED_SUBJECTS);

  const allCodes = await renderedCodes(page);
  expect(allCodes).toContain(LAST_UNSERVED_CODE);
  // Appending must not duplicate what page one already showed.
  expect(new Set(allCodes).size).toBe(allCodes.length);
  // Everything loaded: the control retires rather than paging past the end.
  await expect(page.getByTestId("subject-load-more")).toHaveCount(0);

  // Changing a filter restarts at page one rather than appending a different
  // query's second page onto this one.
  await page.getByTestId("subject-availability-filter").selectOption("served");
  await expect(page.getByTestId("subject-card")).toHaveCount(1);
  await expect(page.getByTestId("subject-shown-count")).toContainText("Showing 1 of 1");
  expect(await renderedCodes(page)).toEqual([SERVED_CODE]);
});

test("A served Subject opens its real published Course", async ({ page }) => {
  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=served`);

  const card = page.getByTestId("subject-card").first();
  await expect(card).toHaveAttribute("data-served", "true");
  await expect(card.getByTestId("subject-availability")).toHaveText("Available now");

  await card.getByTestId("subject-open-course").click();
  // The real slug the seeder's Course actually carries, not a constructed one.
  await expect(page).toHaveURL(new RegExp(`/en/catalog/${SERVED_COURSE_SLUG}$`));
});

test("An unserved Subject records demand, converges on conflict, and withdraws", async ({
  browser,
}, testInfo) => {
  const student = studentFor(testInfo, SUBJECT_DEMAND_EN_TEST_SLOT);
  const context = await browser.newContext({ locale: "en-US" });
  await authenticateRotatingStudent(context, student);
  const page = await context.newPage();

  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
  const card = page.getByTestId("subject-card").first();
  await expect(card).toHaveAttribute("data-served", "false");

  // The card must not resize between states (L1), measured on the real
  // rendered card rather than asserted from its class list.
  const beforeHeight = (await card.boundingBox())?.height ?? 0;

  await card.getByTestId("subject-demand-request").click();
  await expect(card.getByTestId("subject-demand-state")).toHaveText("Requested");

  const afterHeight = (await card.boundingBox())?.height ?? 0;
  expect(
    Math.abs(afterHeight - beforeHeight),
    "the card resized when the Subject was requested",
  ).toBeLessThanOrEqual(2);

  // A reload proves the signal is in the database, not in component state.
  await page.reload();
  await expect(
    page.getByTestId("subject-card").first().getByTestId("subject-demand-state"),
  ).toHaveText("Requested");

  // Duplicate demand is a 409 from the live-unique index; the UI converges on
  // requested rather than surfacing it as a failure.
  const conflict = await page.evaluate(async () => {
    const bootstrap = await fetch("/api/v1/session/bootstrap", { credentials: "same-origin" });
    const { csrf_token } = (await bootstrap.json()) as { csrf_token: string };
    const card = document.querySelector('[data-testid="subject-card"]');
    const id = card?.getAttribute("data-subject-id") ?? "";
    const response = await fetch("/api/v1/me/subject-demand", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf_token },
      body: JSON.stringify({ subject_id: id }),
    });
    return response.status;
  });
  expect(conflict, "a second signal for the same Subject must conflict").toBe(409);
  await expect(
    page.getByTestId("subject-card").first().getByTestId("subject-demand-state"),
  ).toHaveText("Requested");

  // Withdrawal, then a repeat withdrawal, which is a 404 the UI converges on.
  await page.getByTestId("subject-card").first().getByTestId("subject-demand-withdraw").click();
  await expect(
    page.getByTestId("subject-card").first().getByTestId("subject-demand-request"),
  ).toBeVisible();

  const repeatWithdraw = await page.evaluate(async () => {
    const bootstrap = await fetch("/api/v1/session/bootstrap", { credentials: "same-origin" });
    const { csrf_token } = (await bootstrap.json()) as { csrf_token: string };
    const card = document.querySelector('[data-testid="subject-card"]');
    const id = card?.getAttribute("data-subject-id") ?? "";
    const response = await fetch(`/api/v1/me/subject-demand/${id}`, {
      method: "DELETE",
      credentials: "same-origin",
      headers: { "X-CSRF-Token": csrf_token },
    });
    return response.status;
  });
  expect(repeatWithdraw, "withdrawing twice has nothing live to withdraw").toBe(404);
  await page.reload();
  await expect(
    page.getByTestId("subject-card").first().getByTestId("subject-demand-request"),
  ).toBeVisible();

  await context.close();
});

test("Anonymous Request routes through sign-in and returns to the same Subject", async ({
  page,
}) => {
  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);

  const detailHref = await page
    .getByTestId("subject-card")
    .first()
    .getByTestId("subject-card-link")
    .getAttribute("href");
  expect(detailHref).toBeTruthy();

  await page.goto(detailHref!);
  await page.getByTestId("subject-demand-sign-in").click();

  // The destination survives as returnTo, carrying the request intent.
  await expect(page).toHaveURL(/\/login\?returnTo=/);
  const returnTo = new URL(page.url()).searchParams.get("returnTo") ?? "";
  // Compared decoded on both sides: the anchor carries the percent-encoded
  // form of a Subject code containing a space, while the component builds the
  // destination from the decoded pathname. Those are the same route, and
  // asserting on one spelling would fail on a difference that is not a defect.
  const returnedPath = decodeURIComponent(returnTo).split("?")[0];
  expect(returnedPath, "the sign-in hop must carry the Subject route").toBe(
    decodeURIComponent(detailHref!),
  );
  expect(
    decodeURIComponent(returnTo),
    "and the intent that brought the visitor there",
  ).toContain("request=1");
});

test("An unknown Subject URL is a real HTTP 404", async ({ page }) => {
  const response = await page.goto(`/en/subjects/${INSTITUTION_SLUG}/NOSUCH999`);
  // The status, not the rendered text: a 200 carrying "not found" tells
  // crawlers and monitoring that a missing page is healthy.
  expect(response?.status(), "a missing Subject must answer 404").toBe(404);
});

test("Arabic renders right-to-left and keeps the Subject route across a locale switch", async ({
  page,
}) => {
  await page.goto(`/ar/subjects?institution=${INSTITUTION_SLUG}&availability=served`);
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(page.locator("html")).toHaveAttribute("lang", "ar");
  await expect(page.getByTestId("subject-availability").first()).toHaveText("متاحة الآن");
  await expectNoHorizontalOverflow(page, "Arabic subject catalogue");

  // The detail route survives the locale switch, pointing at the same Subject.
  const href = await page
    .getByTestId("subject-card")
    .first()
    .getByTestId("subject-card-link")
    .getAttribute("href");
  expect(href).toContain(`/ar/subjects/${INSTITUTION_SLUG}/`);
  await page.goto(href!);
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");

  const englishHref = href!.replace("/ar/", "/en/");
  await page.goto(englishHref);
  await expect(page.locator("html")).toHaveAttribute("dir", "ltr");
  await expect(page.getByTestId("subject-availability")).toHaveText("Available now");
});

test("The catalogue fits mobile and desktop without horizontal overflow", async ({ page }) => {
  for (const viewport of [
    { width: 390, height: 844, label: "mobile" },
    { width: 1280, height: 900, label: "desktop" },
  ]) {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}`);
    await expect(page.getByTestId("subject-card").first()).toBeVisible();
    await expectNoHorizontalOverflow(page, `${viewport.label} subject catalogue`);

    await page.goto(`/ar/subjects?institution=${INSTITUTION_SLUG}`);
    await expect(page.getByTestId("subject-card").first()).toBeVisible();
    await expectNoHorizontalOverflow(page, `${viewport.label} Arabic subject catalogue`);
  }
});

test("Admin sorts and filters demand; Instructor sees no Student demand control", async ({
  browser,
}, testInfo) => {
  // Two Students so the counts differ and sorting has something to order.
  const first = studentFor(testInfo, SUBJECT_DEMAND_EN_TEST_SLOT);
  const second = studentFor(testInfo, SUBJECT_DEMAND_AR_TEST_SLOT);

  for (const student of [first, second]) {
    const context = await browser.newContext({ locale: "en-US" });
    await authenticateRotatingStudent(context, student);
    const page = await context.newPage();
    await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
    await page.getByTestId("subject-card").first().getByTestId("subject-demand-request").click();
    await expect(
      page.getByTestId("subject-card").first().getByTestId("subject-demand-state"),
    ).toBeVisible();
    await context.close();
  }

  // Only the second Student also asks for a different Subject, so the two rows
  // carry different counts.
  const extraContext = await browser.newContext({ locale: "en-US" });
  await authenticateRotatingStudent(extraContext, second);
  const extraPage = await extraContext.newPage();
  await extraPage.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
  await extraPage.getByTestId("subject-card").nth(1).getByTestId("subject-demand-request").click();
  await expect(
    extraPage.getByTestId("subject-card").nth(1).getByTestId("subject-demand-state"),
  ).toBeVisible();
  await extraContext.close();

  const adminContext = await browser.newContext({ locale: "en-US" });
  await installStaffSession(adminContext, ADMIN);
  const adminPage = await adminContext.newPage();
  await adminPage.goto("/en/admin/subject-demand");

  await expect(adminPage.getByTestId("demand-row").first()).toBeVisible();
  await adminPage.getByTestId("demand-institution-filter").selectOption(INSTITUTION_SLUG);
  await expect(adminPage.getByTestId("demand-row").first()).toContainText(INSTITUTION_EN);

  // Sorted by count, highest first: the Subject two Students asked for leads.
  const counts = await adminPage.getByTestId("demand-students").allInnerTexts();
  const numeric = counts.map((value) => Number(value.trim()));
  expect(numeric.length).toBeGreaterThanOrEqual(2);
  expect(
    [...numeric].sort((a, b) => b - a),
    "demand must arrive sorted by student count, highest first",
  ).toEqual(numeric);

  // Availability narrowing: every seeded demand is against unserved Subjects.
  await adminPage.getByTestId("demand-availability-filter").selectOption("served");
  await expect(adminPage.getByTestId("demand-row")).toHaveCount(0);
  await adminPage.getByTestId("demand-availability-filter").selectOption("unserved");
  await expect(adminPage.getByTestId("demand-row").first()).toBeVisible();
  await expectNoHorizontalOverflow(adminPage, "admin subject demand");
  await adminContext.close();

  // An Instructor is signed in and holds no CapLearningAccess. The Student
  // demand control must not be actionable for them (M4).
  const instructorContext = await browser.newContext({ locale: "en-US" });
  await installStaffSession(instructorContext, INSTRUCTOR);
  const instructorPage = await instructorContext.newPage();
  await instructorPage.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
  const instructorCard = instructorPage.getByTestId("subject-card").first();
  await expect(instructorCard).toBeVisible();
  await expect(instructorCard.getByTestId("subject-demand-request")).toHaveCount(0);
  await expect(instructorCard.getByTestId("subject-demand-ineligible")).toBeVisible();
  await instructorContext.close();
});
