import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type BrowserContext,
  type Page,
  type TestInfo,
} from "@playwright/test";
import {
  authenticateRotatingStudent,
  expiredStudentFor,
  issueRotatingSession,
  queryInvitationToken,
  ACCESS_A11Y_EXPIRED_AR_TEST_SLOT,
  ACCESS_A11Y_EXPIRED_EN_TEST_SLOT,
  ACCESS_A11Y_INVITED_AR_TEST_SLOT,
  ACCESS_A11Y_INVITED_EN_TEST_SLOT,
} from "./rotating-students";
import { frontendOrigin } from "../src/lib/api/e2e-ports";

/**
 * UX-I — the Student's Course-access surface, scanned for accessibility in both languages.
 *
 * This is the gap Tranche H left open and recorded as debt. Every other tranche surface carries an
 * axe scan; `/access` did not, because it is the one Student screen that says nothing until real
 * access exists behind it. Scanning it empty would have proved only that an empty page is
 * accessible.
 *
 * So the fixture is real, and built the way the product builds it: an Administrator creates an
 * invitation through the Admin workspace, the delivery token is read from the outbox the way S6
 * reads it, and the Student arrives on the link they would actually be sent. Nothing here reaches
 * around the product to seed a state through SQL, and nothing relaxes an access rule to make a
 * screen render — an accessible screen that only exists under a weakened contract is not evidence
 * about the product.
 *
 * Three states are covered, because they are three different pages:
 *
 *  - **the invitation**, where the Student is being asked to do something;
 *  - **the record list after acceptance**, where they are being told what happened; and
 *  - **ended access**, the refusal state, where the answer is no.
 */

const ADMIN = { email: "admin@example.test", accountID: "a0000000-0000-0000-0000-000000000000" };
const COURSE_ID = "c0000000-0000-0000-0000-000000000001";

const LOCALES = ["en", "ar"] as const;
type Locale = (typeof LOCALES)[number];

/**
 * The scan itself.
 *
 * WCAG A and AA, which is the bar the rest of the tranche suites hold, and the whole document
 * rather than a region: a skip link, a landmark and a heading order are properties of the page, and
 * scanning `main` alone would step over exactly the parts this tranche is about.
 */
async function axeClean(page: Page, label: string): Promise<void> {
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(
    results.violations.map((v) => `${v.id} (${v.nodes.length}): ${v.help}`),
    `axe violations on ${label}`,
  ).toEqual([]);
}

async function signIn(
  context: BrowserContext,
  identity: { email: string; accountID: string },
  locale: Locale,
): Promise<void> {
  const session = issueRotatingSession(identity);
  const origin = new URL(frontendOrigin());
  await context.addInitScript((v) => window.localStorage.setItem("gradex.locale", v), locale);
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

/**
 * Create one invitation through the Admin workspace and return the link the Student is sent.
 *
 * The identifier comes from the row's test hook rather than from rendered text, for the same reason
 * S6 takes it from there: the queue deliberately no longer prints it to a human, so reading it off
 * the screen would assert the presence of the leak the product removed.
 */
async function inviteThroughAdminWorkspace(page: Page, email: string): Promise<string> {
  await page.goto("/en/admin/course-access");
  await page.getByTestId("course-access-course-select").selectOption(COURSE_ID);
  await page.locator("#access-invite-email").fill(email);
  await page.getByTestId("access-invite-submit").click();

  const hook = page
    .locator(`tr:has(td:has-text("${email}")) [data-testid^="invitation-course-"]`)
    .first();
  await expect(hook, `no invitation row appeared for ${email}`).toBeVisible({ timeout: 20_000 });
  const invitationID = ((await hook.getAttribute("data-testid")) ?? "").replace(
    "invitation-course-",
    "",
  );
  expect(invitationID).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
  );

  const token = queryInvitationToken(invitationID);
  expect(token, "the invitation produced no delivery token").toBeTruthy();
  return `?invitation_id=${invitationID}#token=${token}`;
}

test.describe("UX-I Student course access accessibility", () => {
  test.describe.configure({ timeout: 120_000 });

  for (const locale of LOCALES) {
    test(`the invitation and the record it becomes carry no accessibility violation in ${locale}`, async ({
      browser,
    }, testInfo: TestInfo) => {
      // An expired-pool Student, invited fresh. Their page then carries ended access and a live
      // invitation at once, which is a harder document to get right than either state alone.
      const student = expiredStudentFor(
        testInfo,
        locale === "ar" ? ACCESS_A11Y_INVITED_AR_TEST_SLOT : ACCESS_A11Y_INVITED_EN_TEST_SLOT,
      );

      const adminContext = await browser.newContext({ locale: "en-US" });
      await signIn(adminContext, ADMIN, "en");
      const adminPage = await adminContext.newPage();
      const invitationQuery = await inviteThroughAdminWorkspace(adminPage, student.email);

      const studentContext = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
      });
      await studentContext.addInitScript(
        (v) => window.localStorage.setItem("gradex.locale", v),
        locale,
      );
      await authenticateRotatingStudent(studentContext, student);
      const studentPage = await studentContext.newPage();

      // State one: the Student is being asked to do something.
      await studentPage.goto(`/${locale}/access${invitationQuery}`);
      await expect(studentPage.locator("h1")).toBeVisible({ timeout: 20_000 });
      await expect(studentPage.getByTestId("accept-invitation")).toBeVisible();
      await expect(studentPage.locator("html")).toHaveAttribute(
        "dir",
        locale === "ar" ? "rtl" : "ltr",
      );
      await axeClean(studentPage, `access invitation in ${locale}`);

      // State two: they have answered, and the page is telling them what happened.
      await studentPage.getByTestId("accept-invitation").click();
      await expect(studentPage.getByTestId(`access-record-${COURSE_ID}`)).toBeVisible({
        timeout: 20_000,
      });
      await axeClean(studentPage, `access record list in ${locale}`);

      await studentContext.close();
      await adminContext.close();
    });

    test(`ended access carries no accessibility violation in ${locale}`, async ({
      browser,
    }, testInfo: TestInfo) => {
      const student = expiredStudentFor(
        testInfo,
        locale === "ar" ? ACCESS_A11Y_EXPIRED_AR_TEST_SLOT : ACCESS_A11Y_EXPIRED_EN_TEST_SLOT,
      );
      const context = await browser.newContext({
        locale: locale === "ar" ? "ar-KW" : "en-US",
      });
      await context.addInitScript((v) => window.localStorage.setItem("gradex.locale", v), locale);
      await authenticateRotatingStudent(context, student);
      const page = await context.newPage();

      // The refusal state, reached with no invitation in hand: this Student's access has ended, and
      // the page's job is to say so.
      await page.goto(`/${locale}/access`);
      await expect(page.locator("h1")).toBeVisible({ timeout: 20_000 });
      // The fixture has to be the refusal it claims to be. An empty page would pass an axe scan
      // while proving nothing about how ended access is presented, so the record list is asserted
      // present and the empty state asserted absent before anything is scanned.
      await expect(page.getByTestId("access-records")).toBeVisible({ timeout: 20_000 });
      await expect(page.getByTestId("access-empty")).toHaveCount(0);
      await expect(page.locator("html")).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
      await axeClean(page, `ended access in ${locale}`);

      await context.close();
    });
  }
});
