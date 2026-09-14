import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import {
  authenticateRotatingStudent,
  issueRotatingSession,
  studentFor,
  SUBJECT_DEMAND_ADMIN_FIRST_TEST_SLOT,
  SUBJECT_DEMAND_ADMIN_SECOND_TEST_SLOT,
  SUBJECT_DEMAND_AR_TEST_SLOT,
  SUBJECT_DEMAND_AUTH_RETURN_TEST_SLOT,
  SUBJECT_DEMAND_EN_TEST_SLOT,
  SUBJECT_DEMAND_WITHDRAW_TEST_SLOT,
} from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * D-106 Subject catalogue and Student demand, against the real API and a real
 * database.
 *
 * No response is stubbed. Where a test needs a slow or failing request it
 * delays or aborts the *real* one through `page.route` and lets the genuine
 * backend answer — the response every assertion rests on is the server's own.
 * That distinction matters: an earlier revision proved a 409 by issuing `fetch`
 * from `page.evaluate`, which tested the API and the test's own assumptions
 * rather than what the UI does when the server disagrees with it.
 *
 * Fixtures come from cmd/e2e-seed: one Institution holding SEEDED_SUBJECTS
 * Subjects, exactly one served by a published Course. That count is
 * deliberately larger than one catalogue page.
 */

// Mirrors cmd/e2e-seed seedSubjectCatalogueFixtures.
const INSTITUTION_SLUG = "e2e-subject-university";
const INSTITUTION_EN = "E2E Subject University";
const INSTITUTION_AR = "جامعة الاختبار";
const SEEDED_SUBJECTS = 30;
const SERVED_CODE = "ZZZ 900";
const SERVED_COURSE_SLUG = "course-c00000000000000000000000000d1060";
const LAST_UNSERVED_CODE = "SUB 128";
// Mirrors SUBJECTS_PER_PAGE in subject-catalogue.tsx.
const PAGE_SIZE = 24;
const STUDENT_PASSWORD = "StudentPassword123!";

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const INSTRUCTOR = {
  email: "instructor@example.test",
  accountID: "a0000000-0000-0000-0000-000000000003",
};

test.describe.configure({ timeout: 240_000 });

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
function renderedCodes(page: Page): Promise<string[]> {
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

/** The first card's Subject identifier, as the catalogue rendered it. */
function firstCardSubjectID(page: Page): Promise<string | null> {
  return page.getByTestId("subject-card").first().getAttribute("data-subject-id");
}

test("Every Subject is reachable through pagination without duplicates", async ({ page }) => {
  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}`);

  await expect(page.getByTestId("subject-card")).toHaveCount(PAGE_SIZE);
  await expect(page.getByTestId("subject-shown-count")).toContainText(
    `Showing ${PAGE_SIZE} of ${SEEDED_SUBJECTS}`,
  );

  // Not on page one, so reaching it later proves pagination rather than luck.
  expect(await renderedCodes(page)).not.toContain(LAST_UNSERVED_CODE);

  await page.getByTestId("subject-load-more").click();
  await expect(page.getByTestId("subject-card")).toHaveCount(SEEDED_SUBJECTS);

  const allCodes = await renderedCodes(page);
  expect(allCodes).toContain(LAST_UNSERVED_CODE);
  expect(new Set(allCodes).size, "an append duplicated a Subject").toBe(allCodes.length);
  await expect(page.getByTestId("subject-load-more")).toHaveCount(0);
});

/**
 * The stale-append race, proven against the real backend.
 *
 * The page-two request is delayed — not stubbed — so the filter change lands
 * while it is still outstanding. Before the generation guard, its results were
 * spliced into the filtered list and unserved Subjects appeared under
 * availability=served.
 */
test("A pending append never contaminates a newer query", async ({ page }) => {
  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}`);
  await expect(page.getByTestId("subject-card")).toHaveCount(PAGE_SIZE);

  // Delay only the page-2 request, and let the real server answer it.
  await page.route("**/api/v1/catalog/subjects?**", async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get("page") === "2") {
      await new Promise((resolve) => setTimeout(resolve, 3_000));
    }
    await route.continue();
  });

  await page.getByTestId("subject-load-more").click();
  // Change the query while page two is still in flight.
  await page.getByTestId("subject-availability-filter").selectOption("served");

  // The served filter matches exactly one seeded Subject. If the stale append
  // lands, this count grows and unserved codes appear beside it.
  await expect(page.getByTestId("subject-card")).toHaveCount(1);
  await expect(page.getByTestId("subject-shown-count")).toContainText("Showing 1 of 1");
  expect(await renderedCodes(page)).toEqual([SERVED_CODE]);

  // Hold past the delayed response's arrival and re-assert: a late append would
  // land here, after the assertions above had already passed.
  await page.waitForTimeout(4_000);
  await expect(page.getByTestId("subject-card")).toHaveCount(1);
  expect(await renderedCodes(page)).toEqual([SERVED_CODE]);
  await page.unroute("**/api/v1/catalog/subjects?**");
});

test("Changing search, institution, availability, or locale resets to page one", async ({
  page,
}) => {
  const loadBothPages = async () => {
    await expect(page.getByTestId("subject-card")).toHaveCount(PAGE_SIZE);
    await page.getByTestId("subject-load-more").click();
    await expect(page.getByTestId("subject-card")).toHaveCount(SEEDED_SUBJECTS);
  };

  // Availability.
  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}`);
  await loadBothPages();
  await page.getByTestId("subject-availability-filter").selectOption("unserved");
  await expect(page.getByTestId("subject-card")).toHaveCount(PAGE_SIZE);
  await expect(page).toHaveURL(/availability=unserved/);
  await expect(page).toHaveURL(new RegExp(`institution=${INSTITUTION_SLUG}`));

  // Search, from a fully-loaded list.
  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}`);
  await loadBothPages();
  await page.locator("#subject-search").fill("SUB 10");
  await page.getByRole("button", { name: "Search" }).click();
  await expect(page).toHaveURL(/q=SUB(?:%20|\+)10/);
  await expect(page.getByTestId("subject-card")).toHaveCount(10);
  await expect(page.getByTestId("subject-shown-count")).toContainText(
    "Showing 10 of 10",
  );
  expect((await renderedCodes(page)).every((code) => code.startsWith("SUB 10"))).toBe(true);

  // Institution: away from the seeded one resets the list.
  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}`);
  await loadBothPages();
  await page.getByTestId("subject-institution-filter").selectOption("");
  await expect(page.getByTestId("subject-card")).toHaveCount(PAGE_SIZE);
  await expect(page.getByTestId("subject-shown-count")).toContainText(`Showing ${PAGE_SIZE} of`);

  // Locale, preserving the query.
  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}`);
  await loadBothPages();
  await page.goto(`/ar/subjects?institution=${INSTITUTION_SLUG}`);
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(page.getByTestId("subject-card")).toHaveCount(PAGE_SIZE);
  await expect(page).toHaveURL(new RegExp(`institution=${INSTITUTION_SLUG}`));
});

test("A served Subject opens its real published Course", async ({ page }) => {
  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=served`);

  const card = page.getByTestId("subject-card").first();
  await expect(card).toHaveAttribute("data-served", "true");
  await expect(card.getByTestId("subject-availability")).toHaveText("Available now");

  await card.getByTestId("subject-open-course").click();
  await expect(page).toHaveURL(new RegExp(`/en/catalog/${SERVED_COURSE_SLUG}$`));
});

test("Retry re-runs a failed catalogue load and succeeds", async ({ page }) => {
  // Fail the first catalogue request at the network, then let every later one
  // reach the real server untouched. Nothing is stubbed: the successful load
  // below is the genuine backend response.
  let alreadyFailed = false;
  await page.route("**/api/v1/catalog/subjects?**", async (route) => {
    if (!alreadyFailed) {
      alreadyFailed = true;
      return route.abort("failed");
    }
    return route.continue();
  });

  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}`);
  await expect(page.getByTestId("subject-retry")).toBeVisible();
  await expect(page.getByTestId("subject-card")).toHaveCount(0);

  await page.getByTestId("subject-retry").click();
  await expect(page.getByTestId("subject-card")).toHaveCount(PAGE_SIZE);
  await expect(page.getByTestId("subject-retry")).toHaveCount(0);
  await page.unroute("**/api/v1/catalog/subjects?**");
});

/**
 * Conflict convergence, driven entirely through the UI.
 *
 * Two pages share one Student. Page A renders the Subject as unrequested, page
 * B requests it, and page A then presses its stale Request button. The 409 is
 * produced by the live-unique index because two real requests raced, and the
 * assertion is on what page A's UI does with it.
 */
test("A stale Request converges on Requested when the server answers 409", async ({
  browser,
}, testInfo) => {
  const student = studentFor(testInfo, SUBJECT_DEMAND_EN_TEST_SLOT);
  const context = await browser.newContext({ locale: "en-US" });
  await authenticateRotatingStudent(context, student);

  const pageA = await context.newPage();
  await pageA.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
  const cardA = pageA.getByTestId("subject-card").first();
  await expect(cardA.getByTestId("subject-demand-request")).toBeVisible();
  const subjectID = await firstCardSubjectID(pageA);
  const idleRegion = await cardA.locator(":scope > div.mt-auto.pt-5 > *").boundingBox();
  expect(idleRegion, "the Request action region must be measurable").not.toBeNull();

  const pageB = await context.newPage();
  await pageB.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
  const cardB = pageB.getByTestId("subject-card").first();
  expect(await firstCardSubjectID(pageB), "both pages must hold the same Subject").toBe(subjectID);
  await cardB.getByTestId("subject-demand-request").click();
  await expect(cardB.getByTestId("subject-demand-state")).toHaveText("Requested");

  // Page A still believes the Subject is unrequested. Pressing Request now is
  // the real conflict.
  const conflict = pageA.waitForResponse(
    (response) =>
      response.url().includes("/api/v1/me/subject-demand") &&
      response.request().method() === "POST",
  );
  await cardA.getByTestId("subject-demand-request").click();
  expect((await conflict).status(), "the stale Request must exercise the real 409").toBe(409);
  await expect(cardA.getByTestId("subject-demand-state")).toHaveText("Requested");
  // A conflict is a state, not a failure: no error is shown.
  await expect(cardA.getByRole("alert")).toHaveCount(0);
  const requestedRegion = await cardA.locator(":scope > div.mt-auto.pt-5 > *").boundingBox();
  expect(requestedRegion, "the Requested action region must be measurable").not.toBeNull();
  expect(Math.abs(requestedRegion!.width - idleRegion!.width)).toBeLessThanOrEqual(2);
  expect(Math.abs(requestedRegion!.height - idleRegion!.height)).toBeLessThanOrEqual(2);

  // Cleanup, so the Admin aggregate counts only what its own Students leave.
  await cardA.getByTestId("subject-demand-withdraw").click();
  await expect(cardA.getByTestId("subject-demand-request")).toBeVisible();
  const withdrawnRegion = await cardA.locator(":scope > div.mt-auto.pt-5 > *").boundingBox();
  expect(withdrawnRegion, "the restored Request action region must be measurable").not.toBeNull();
  expect(Math.abs(withdrawnRegion!.width - idleRegion!.width)).toBeLessThanOrEqual(2);
  expect(Math.abs(withdrawnRegion!.height - idleRegion!.height)).toBeLessThanOrEqual(2);
  await context.close();
});

/**
 * Withdrawal convergence, also through the UI.
 *
 * Page A holds a requested Subject, page B withdraws it, and page A then
 * presses its stale Withdraw. The server answers 404 because there is nothing
 * live to withdraw — which is the state page A was asking for.
 */
test("A stale Withdraw converges on not-requested when the server answers 404", async ({
  browser,
}, testInfo) => {
  const student = studentFor(testInfo, SUBJECT_DEMAND_WITHDRAW_TEST_SLOT);
  const context = await browser.newContext({ locale: "en-US" });
  await authenticateRotatingStudent(context, student);

  const pageA = await context.newPage();
  await pageA.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
  const cardA = pageA.getByTestId("subject-card").first();
  await cardA.getByTestId("subject-demand-request").click();
  await expect(cardA.getByTestId("subject-demand-state")).toHaveText("Requested");

  const pageB = await context.newPage();
  await pageB.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
  const cardB = pageB.getByTestId("subject-card").first();
  await expect(cardB.getByTestId("subject-demand-state")).toHaveText("Requested");
  await cardB.getByTestId("subject-demand-withdraw").click();
  await expect(cardB.getByTestId("subject-demand-request")).toBeVisible();

  // Page A still shows Requested. Its Withdraw now has nothing to withdraw.
  const missing = pageA.waitForResponse(
    (response) =>
      response.url().includes("/api/v1/me/subject-demand/") &&
      response.request().method() === "DELETE",
  );
  await cardA.getByTestId("subject-demand-withdraw").click();
  expect((await missing).status(), "the stale Withdraw must exercise the real 404").toBe(404);
  await expect(cardA.getByTestId("subject-demand-request")).toBeVisible();
  await expect(cardA.getByRole("alert")).toHaveCount(0);
  await context.close();
});

/**
 * The whole anonymous journey: find a Subject, press Request, sign in through
 * the real login form, land back on that Subject with the intent intact, and
 * complete the action that started it.
 *
 * The context starts with only the credential for a device the server already
 * trusts. It has no session cookie, so it is genuinely anonymous until the
 * login form succeeds, while still exercising the already-trusted branch.
 */
test("Anonymous Request completes through sign-in and returns to the Subject", async ({
  browser,
}, testInfo) => {
  const student = studentFor(testInfo, SUBJECT_DEMAND_AUTH_RETURN_TEST_SLOT);
  const trusted = issueRotatingSession(student);
  const context = await browser.newContext({ locale: "en-US" });
  await context.addInitScript(() => window.localStorage.setItem("gradex.locale", "en"));
  const origin = new URL(frontendOrigin());
  await context.addCookies([
    {
      name: trusted.device_cookie_name,
      value: trusted.device_cookie_value,
      domain: origin.hostname,
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
    },
  ]);
  const page = await context.newPage();

  await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
  const detailHref = await page
    .getByTestId("subject-card")
    .first()
    .getByTestId("subject-card-link")
    .getAttribute("href");
  expect(detailHref).toBeTruthy();
  const subjectPath = decodeURIComponent(detailHref!);

  await page.goto(detailHref!);
  await expect(page.getByTestId("subject-demand-sign-in-required")).toBeVisible();
  await page.getByTestId("subject-demand-sign-in").click();

  await expect(page).toHaveURL(/\/login\?returnTo=/);
  const returnTo = decodeURIComponent(new URL(page.url()).searchParams.get("returnTo") ?? "");
  expect(returnTo.split("?")[0], "the sign-in hop must carry the Subject route").toBe(subjectPath);
  expect(returnTo, "and the intent that brought the visitor there").toContain("request=1");

  await page.locator("#email").fill(student.email);
  await page.locator("#password").fill(STUDENT_PASSWORD);
  await page.getByRole("button", { name: /sign in|log in/i }).first().click();

  // Back on the exact Subject, with the intent preserved.
  await page.waitForURL(
    (url) => decodeURIComponent(url.pathname) === subjectPath.split("?")[0],
    { timeout: 60_000 },
  );
  expect(new URL(page.url()).searchParams.get("request")).toBe("1");

  // The panel resumes open rather than collapsing to a button the Student has
  // already pressed once, and the action they came to take completes.
  await expect(page.getByTestId("subject-demand-panel")).toBeVisible();
  await page.getByTestId("subject-demand-submit").click();
  await expect(page.getByTestId("subject-demand-state")).toHaveText("Requested");

  await page.getByTestId("subject-demand-withdraw").click();
  await expect(page.getByTestId("subject-demand-submit")).toBeVisible();
  await context.close();
});

test("An unknown Subject URL is a real HTTP 404 while a real one renders", async ({ page }) => {
  const missing = await page.goto(`/en/subjects/${INSTITUTION_SLUG}/NOSUCH999`);
  expect(missing?.status(), "a missing Subject must answer 404").toBe(404);

  // Asserted in the same test so the 404 above cannot pass because everything
  // 404s — which is exactly how an earlier revision hid a real defect.
  const present = await page.goto(
    `/en/subjects/${INSTITUTION_SLUG}/${encodeURIComponent(SERVED_CODE)}`,
  );
  expect(present?.status(), "a real Subject must answer 200").toBe(200);
  await expect(page.getByTestId("subject-availability")).toHaveText("Available now");
});

test("Arabic: a Student requests a Subject through the Arabic routes", async ({
  browser,
}, testInfo) => {
  const student = studentFor(testInfo, SUBJECT_DEMAND_AR_TEST_SLOT);
  const context = await browser.newContext({ locale: "ar" });
  await context.addInitScript(() => window.localStorage.setItem("gradex.locale", "ar"));
  await authenticateRotatingStudent(context, student);
  const page = await context.newPage();

  await page.goto(`/ar/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(page.locator("html")).toHaveAttribute("lang", "ar");

  const card = page.getByTestId("subject-card").first();
  await expect(card.getByTestId("subject-availability")).toHaveText("غير متاحة بعد");
  await card.getByTestId("subject-demand-request").click();
  await expect(card.getByTestId("subject-demand-state")).toHaveText("تم تسجيل طلبك");
  await expectNoHorizontalOverflow(page, "Arabic subject catalogue");

  // The Arabic detail route carries the same Subject and the same state.
  const href = await card.getByTestId("subject-card-link").getAttribute("href");
  expect(href).toContain(`/ar/subjects/${INSTITUTION_SLUG}/`);
  await page.goto(href!);
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(page.getByTestId("subject-demand-state")).toHaveText("تم تسجيل طلبك");

  await page.getByTestId("subject-demand-withdraw").click();
  await expect(page.getByTestId("subject-demand-request")).toBeVisible();
  await context.close();
});

test("The catalogue fits mobile and desktop without horizontal overflow", async ({ page }) => {
  for (const viewport of [
    { width: 390, height: 844, label: "mobile" },
    { width: 1280, height: 900, label: "desktop" },
  ]) {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    for (const locale of ["en", "ar"]) {
      await page.goto(`/${locale}/subjects?institution=${INSTITUTION_SLUG}`);
      await expect(page.getByTestId("subject-card").first()).toBeVisible();
      await expectNoHorizontalOverflow(page, `${viewport.label} ${locale} subject catalogue`);
    }
  }
});

test("Admin reads, sorts, and filters demand; staff get no Student demand control", async ({
  browser,
}, testInfo) => {
  const first = studentFor(testInfo, SUBJECT_DEMAND_ADMIN_FIRST_TEST_SLOT);
  const second = studentFor(testInfo, SUBJECT_DEMAND_ADMIN_SECOND_TEST_SLOT);

  // Both Students request the first unserved Subject; only the second also
  // requests the next one, so the two rows carry different counts and the sort
  // has something real to order.
  const requestNth = async (
    student: { email: string; accountID: string },
    indexes: number[],
  ) => {
    const context = await browser.newContext({ locale: "en-US" });
    await authenticateRotatingStudent(context, student);
    const page = await context.newPage();
    await page.goto(`/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`);
    for (const index of indexes) {
      const card = page.getByTestId("subject-card").nth(index);
      await card.getByTestId("subject-demand-request").click();
      await expect(card.getByTestId("subject-demand-state")).toBeVisible();
    }
    await context.close();
  };
  await requestNth(first, [0]);
  await requestNth(second, [0, 1]);

  const adminContext = await browser.newContext({ locale: "en-US" });
  await installStaffSession(adminContext, ADMIN);
  const adminPage = await adminContext.newPage();
  await adminPage.goto("/en/admin/subject-demand");

  await adminPage.getByTestId("demand-institution-filter").selectOption(INSTITUTION_SLUG);
  await expect(adminPage.getByTestId("demand-row").first()).toContainText(INSTITUTION_EN);

  // Default sort: by student count, highest first.
  const counts = (
    await adminPage.getByTestId("demand-students").allInnerTexts()
  ).map((value) => Number(value.trim()));
  expect(counts.length).toBeGreaterThanOrEqual(2);
  expect([...counts].sort((a, b) => b - a), "default sort is by demand, highest first").toEqual(
    counts,
  );
  expect(counts[0], "the Subject two Students asked for must lead").toBe(2);

  // Explicitly changing the sort selector reorders by Subject name.
  await adminPage.getByTestId("demand-sort").selectOption("subject");
  const names = await adminPage
    .getByTestId("demand-row")
    .locator("td:nth-child(3)")
    .allInnerTexts();
  expect(
    [...names].sort((a, b) => a.localeCompare(b, "en")),
    "selecting name sort must reorder the table",
  ).toEqual(names);

  // Availability narrowing.
  await adminPage.getByTestId("demand-availability-filter").selectOption("served");
  await expect(adminPage.getByTestId("demand-row")).toHaveCount(0);
  await adminPage.getByTestId("demand-availability-filter").selectOption("unserved");
  await expect(adminPage.getByTestId("demand-row").first()).toBeVisible();
  await expectNoHorizontalOverflow(adminPage, "admin subject demand");

  // An Admin browsing the public catalogue holds no CapLearningAccess, so the
  // demand control must not be actionable for them.
  const adminCataloguePage = await adminContext.newPage();
  await adminCataloguePage.goto(
    `/en/subjects?institution=${INSTITUTION_SLUG}&availability=unserved`,
  );
  const adminCard = adminCataloguePage.getByTestId("subject-card").first();
  await expect(adminCard).toBeVisible();
  await expect(adminCard.getByTestId("subject-demand-request")).toHaveCount(0);
  await expect(adminCard.getByTestId("subject-demand-ineligible")).toBeVisible();
  await adminContext.close();

  // The Arabic Admin view renders the Institution's Arabic name, from the
  // server's projection rather than a frontend name table.
  const arabicAdminContext = await browser.newContext({ locale: "ar" });
  await installStaffSession(arabicAdminContext, ADMIN, "ar");
  const arabicAdminPage = await arabicAdminContext.newPage();
  await arabicAdminPage.goto("/ar/admin/subject-demand");
  await arabicAdminPage.getByTestId("demand-institution-filter").selectOption(INSTITUTION_SLUG);
  await expect(arabicAdminPage.getByTestId("demand-row").first()).toContainText(INSTITUTION_AR);
  await arabicAdminContext.close();

  // And neither does an Instructor.
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
